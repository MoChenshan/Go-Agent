// Package plugin 的 ruleset_loader 负责从 YAML 文件加载 input/output guard 的
// 规则集，并编译为运行期 InputRule/OutputRule。
//
// 背景：D14 input_guard/output_guard 规则硬编码在 DefaultInputRules /
// DefaultOutputRules。D17.1 引入"YAML 规则集 + 运行期热加载"，让 SRE 能
// 在不重启服务的前提下增删改拦截/打码规则。
//
// YAML 可表达的规则语言设计：
//
//	input:
//	  max_user_chars: 32768         # 可选，<=0 关闭长度兜底
//	  rules:
//	    - name: jailbreak           # 必填，出现在拦截响应里便于审计
//	      pattern: "(?i)ignore..."  # 必填，Go regexp 语法
//	      reason: "越狱指令"          # 必填，命中时回显的理由
//	      require_contains:         # 可选，正则命中且文本包含任一关键词才算命中
//	        - decode
//	        - execute
//	output:
//	  rules:
//	    - name: token_like_secret
//	      pattern: "sk-[A-Za-z0-9]{20,}"
//	      replacement: "[REDACTED_TOKEN]"
//
// 兼容原则：
//  1. 已有的 Go 闭包规则形态完全保留（InputRule.Match 仍为函数指针）；
//     YAML 路径只是"另一条通道"，通过 compileInput/compileOutput 把 DTO
//     编译成同一份运行时结构。
//  2. 解析失败永远不会把 guard 打成"零规则"裸奔状态 —— 由上层
//     （rule_watcher / guard 构造器）决定是保留旧集还是走默认集。
package plugin

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// RulesetYAML YAML 根结构。
type RulesetYAML struct {
	Input  InputRulesetYAML  `yaml:"input"`
	Output OutputRulesetYAML `yaml:"output"`
}

// InputRulesetYAML 输入防护规则集 DTO。
type InputRulesetYAML struct {
	// MaxUserChars 用户输入长度兜底；0 表示启用默认值（32KB）；负数关闭。
	MaxUserChars int `yaml:"max_user_chars"`
	// Rules 规则列表；空切片视为"无自定义规则，使用内置默认"。
	Rules []InputRuleYAML `yaml:"rules"`
}

// InputRuleYAML 单条输入规则 DTO。
type InputRuleYAML struct {
	// Name 规则名（必填，规则内唯一）。
	Name string `yaml:"name"`
	// Pattern Go regexp 语法字符串（必填）。
	Pattern string `yaml:"pattern"`
	// Reason 命中时的自然语言理由（必填，用于拦截响应）。
	Reason string `yaml:"reason"`
	// RequireContains 正则命中后，文本还必须包含以下任一关键词才算命中。
	// 用途：base64_payload 类规则降低误伤（只有带 decode/execute 语义才拦）。
	// 空切片表示不做二次判定。匹配不区分大小写。
	RequireContains []string `yaml:"require_contains"`
}

// OutputRulesetYAML 输出打码规则集 DTO。
type OutputRulesetYAML struct {
	Rules []OutputRuleYAML `yaml:"rules"`
}

// OutputRuleYAML 单条输出规则 DTO。
type OutputRuleYAML struct {
	// Name 规则名（必填）。
	Name string `yaml:"name"`
	// Pattern 正则，命中部分会被 Replacement 整体替换（必填）。
	Pattern string `yaml:"pattern"`
	// Replacement 占位符（必填，建议形如 [REDACTED_XXX]）。
	Replacement string `yaml:"replacement"`
}

// LoadRulesetFromFile 读取并解析 YAML。
//   - path 为空返回 (nil, nil)，调用方按"无自定义规则"处理。
//   - 文件不存在返回 (nil, nil) 同样视为"无规则"，不视为错误（方便
//     开发环境忽略配置）。
//   - 解析失败返回 error，由调用方决定降级策略。
func LoadRulesetFromFile(path string) (*RulesetYAML, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read guard rules file %q: %w", path, err)
	}
	var rs RulesetYAML
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("unmarshal guard rules %q: %w", path, err)
	}
	return &rs, nil
}

// CompileInputRules 把 DTO 编译成运行期 InputRule 切片。
// 对每条规则做三项校验：Name/Pattern/Reason 非空、正则可编译、Name 唯一。
// 任意一条失败直接返回 error，调用方负责决定是否回滚到旧规则集。
func CompileInputRules(in []InputRuleYAML) ([]InputRule, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(in))
	rules := make([]InputRule, 0, len(in))
	for i, r := range in {
		if r.Name == "" {
			return nil, fmt.Errorf("input rule[%d]: name 不能为空", i)
		}
		if _, dup := seen[r.Name]; dup {
			return nil, fmt.Errorf("input rule[%d]: name %q 重复", i, r.Name)
		}
		seen[r.Name] = struct{}{}
		if r.Pattern == "" {
			return nil, fmt.Errorf("input rule[%q]: pattern 不能为空", r.Name)
		}
		if r.Reason == "" {
			return nil, fmt.Errorf("input rule[%q]: reason 不能为空", r.Name)
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("input rule[%q]: pattern 非法正则: %w",
				r.Name, err)
		}
		// 闭包捕获当前 re + 小写关键词切片，避免每次调用都 ToLower。
		var needles []string
		if len(r.RequireContains) > 0 {
			needles = make([]string, 0, len(r.RequireContains))
			for _, n := range r.RequireContains {
				if n == "" {
					continue
				}
				needles = append(needles, strings.ToLower(n))
			}
		}
		rules = append(rules, InputRule{
			Name:   r.Name,
			Reason: r.Reason,
			Match: func(s string, _ *model.Request) bool {
				if !re.MatchString(s) {
					return false
				}
				if len(needles) == 0 {
					return true
				}
				lc := strings.ToLower(s)
				for _, n := range needles {
					if strings.Contains(lc, n) {
						return true
					}
				}
				return false
			},
		})
	}
	return rules, nil
}

// CompileOutputRules 把 DTO 编译成 OutputRule 切片，校验同 CompileInputRules。
func CompileOutputRules(in []OutputRuleYAML) ([]OutputRule, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(in))
	rules := make([]OutputRule, 0, len(in))
	for i, r := range in {
		if r.Name == "" {
			return nil, fmt.Errorf("output rule[%d]: name 不能为空", i)
		}
		if _, dup := seen[r.Name]; dup {
			return nil, fmt.Errorf("output rule[%d]: name %q 重复", i, r.Name)
		}
		seen[r.Name] = struct{}{}
		if r.Pattern == "" {
			return nil, fmt.Errorf("output rule[%q]: pattern 不能为空", r.Name)
		}
		if r.Replacement == "" {
			return nil, fmt.Errorf("output rule[%q]: replacement 不能为空",
				r.Name)
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("output rule[%q]: pattern 非法正则: %w",
				r.Name, err)
		}
		rules = append(rules, OutputRule{
			Name:        r.Name,
			Pattern:     re,
			Replacement: r.Replacement,
		})
	}
	return rules, nil
}
