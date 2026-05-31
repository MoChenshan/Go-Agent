// Package plugin 中的 input_guard 在 BeforeModel 阶段扫描用户输入，
// 命中规则即短路返回拒绝响应，阻止把潜在危险 prompt 交给 LLM。
//
// 与 safety_guard 的区别：
//   - safety_guard：作用在 *工具调用* 层（BeforeTool），拦截「LLM 要做坏事」。
//   - input_guard：作用在 *模型请求* 层（BeforeModel），拦截「用户（或上游）送进来的坏 prompt」。
//     两者互补，构成输入/执行双层防御。
//
// 默认规则集对齐 OWASP LLM Top 10 - LLM01 Prompt Injection：
//  1. 角色越狱（"忽略以上所有指令" / "ignore previous" / "jailbreak" 等）
//  2. 泄露系统提示（"print your system prompt" / "reveal the instructions"）
//  3. 危险 Shell 命令注入（rm -rf / :(){:|:&};: fork bomb / curl | sh 管道）
//  4. 可疑 base64 长串（长度 ≥ 80 且纯 base64 字符集）— 常见的 payload 走私载荷
//  5. 过长输入（> 32KB）— 防止提示稀释攻击把系统提示挤出上下文
//  6. 危险 URL 载荷（file:// / data:text/html 等非常规协议）
package plugin

import (
	"context"
	"regexp"
	"strings"
	"sync/atomic"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// InputRule 单条 Prompt Injection 检测规则。
type InputRule struct {
	// Name 规则名，出现在拒绝响应里便于审计。
	Name string
	// Match 返回 true 表示命中（输入危险）。lastUser 是最后一条 user 消息的正文。
	Match func(lastUser string, full *model.Request) bool
	// Reason 回显给用户/LLM 的自然语言理由。
	Reason string
}

// InputGuardConfig 配置。
type InputGuardConfig struct {
	// Rules 自定义规则；空则使用 DefaultInputRules()。
	Rules []InputRule
	// Logger 命中时的日志钩子（可选）。
	Logger func(ruleName, reason, snippet string)
	// MaxUserChars 超过该长度的 user 消息直接视为危险（0 表示关闭）。
	//   默认 32768（32KB）。
	MaxUserChars int
}

// InputGuard 输入阶段 Prompt Injection 检测器。
//
// D17.1：rules 改为 atomic.Pointer，读路径无锁，写路径（ReplaceRules）
// 原子替换整组规则。支持 rule_watcher 运行期热加载 YAML 配置。
type InputGuard struct {
	rules  atomic.Pointer[[]InputRule]
	logger func(rule, reason, snippet string)
	maxLen int
}

// NewInputGuard 构造 InputGuard，空配置走默认规则。
func NewInputGuard(cfg InputGuardConfig) *InputGuard {
	rules := cfg.Rules
	if len(rules) == 0 {
		rules = DefaultInputRules()
	}
	maxLen := cfg.MaxUserChars
	if maxLen == 0 {
		maxLen = 32 * 1024
	}
	g := &InputGuard{logger: cfg.Logger, maxLen: maxLen}
	g.rules.Store(&rules)
	return g
}

// ReplaceRules 原子替换整组规则；传入 nil/空切片会降级为默认规则集，
// 避免把 guard 打成"零规则裸奔"状态。供 rule_watcher 热加载调用。
func (g *InputGuard) ReplaceRules(rules []InputRule) {
	if len(rules) == 0 {
		rules = DefaultInputRules()
	}
	g.rules.Store(&rules)
}

// loadRules 原子读取当前规则快照；永远不会返回 nil 切片。
func (g *InputGuard) loadRules() []InputRule {
	p := g.rules.Load()
	if p == nil {
		return nil
	}
	return *p
}

// Register 把 InputGuard 挂到 model.Callbacks 的 BeforeModel 上。
//   - cb 为 nil 时自动新建。
//   - 返回 cb 便于链式调用。
func (g *InputGuard) Register(cb *model.Callbacks) *model.Callbacks {
	if cb == nil {
		cb = model.NewCallbacks()
	}
	cb.RegisterBeforeModel(g.beforeModel)
	return cb
}

// Check 对字符串做一次规则判定（便于单测直接驱动，不走 callback）。
//   - 命中：返回 (匹配规则, true)
//   - 未命中：返回 (InputRule{}, false)
func (g *InputGuard) Check(lastUser string) (InputRule, bool) {
	// 内置长度上限兜底
	if g.maxLen > 0 && len(lastUser) > g.maxLen {
		return InputRule{
			Name:   "input_too_long",
			Reason: "用户输入超过长度上限，疑似提示稀释攻击；已拒绝。",
		}, true
	}
	for _, r := range g.loadRules() {
		if r.Match != nil && r.Match(lastUser, nil) {
			return r, true
		}
	}
	return InputRule{}, false
}

// beforeModel 实现 model.BeforeModelCallbackStructured 签名。
func (g *InputGuard) beforeModel(_ context.Context,
	args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
	if args == nil || args.Request == nil {
		return nil, nil
	}
	lastUser := extractLastUser(args.Request.Messages)
	if lastUser == "" {
		return nil, nil
	}
	// 长度上限
	if g.maxLen > 0 && len(lastUser) > g.maxLen {
		return g.block("input_too_long",
			"用户输入超过长度上限，疑似提示稀释攻击；已拒绝。",
			lastUser), nil
	}
	// 规则命中
	for _, r := range g.loadRules() {
		if r.Match == nil {
			continue
		}
		if r.Match(lastUser, args.Request) {
			return g.block(r.Name, r.Reason, lastUser), nil
		}
	}
	return nil, nil
}

// block 构造短路返回的 BeforeModelResult。
func (g *InputGuard) block(rule, reason, snippet string) *model.BeforeModelResult {
	if g.logger != nil {
		g.logger(rule, reason, shortSnippet(snippet, 120))
	}
	finish := "content_filter"
	return &model.BeforeModelResult{
		CustomResponse: &model.Response{
			Object: model.ObjectTypeChatCompletion,
			Done:   true,
			Choices: []model.Choice{{
				Index: 0,
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "【安全拦截】" + reason + "（rule=" + rule + "）",
				},
				FinishReason: &finish,
			}},
		},
	}
}

// extractLastUser 拿最后一条 user 消息正文。
func extractLastUser(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

// shortSnippet 截断日志摘录。
func shortSnippet(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// DefaultInputRules 默认 6 条规则集。
//
// 业务侧可通过 InputGuardConfig.Rules 覆盖。
func DefaultInputRules() []InputRule {
	// 预编译正则（模块级 init 性能更好，但此处规则数小，函数内即可）
	jailbreak := regexp.MustCompile(
		`(?i)(ignore\s+(all\s+|the\s+)?(previous|above)\s+(instruction|prompt)s?` +
			`|disregard\s+(the\s+)?system\s+prompt` +
			`|jailbreak\s+mode` +
			`|忽略(以上|前面|所有)的?(指令|提示|规则)` +
			`|请忽略系统提示)`)
	leakPrompt := regexp.MustCompile(
		`(?i)(print\s+(your\s+)?(system\s+)?prompt` +
			`|reveal\s+(the\s+)?(system\s+)?instructions?` +
			`|show\s+me\s+your\s+system\s+message` +
			`|显示(你的)?系统提示|告诉我系统提示词|输出系统提示词)`)
	shellInj := regexp.MustCompile(
		`(?i)(\brm\s+-rf\s+/` +
			`|:\(\)\{:\|:&\};:` + // fork bomb
			`|\bcurl\s+[^\s]+\s*\|\s*(sh|bash)\b` +
			`|\bwget\s+[^\s]+\s*\|\s*(sh|bash)\b)`)
	base64Big := regexp.MustCompile(`[A-Za-z0-9+/=]{80,}`)
	badURL := regexp.MustCompile(`(?i)(file://|data:text/html|javascript:)`)

	return []InputRule{
		{
			Name: "prompt_injection_jailbreak",
			Match: func(s string, _ *model.Request) bool {
				return jailbreak.MatchString(s)
			},
			Reason: "检测到疑似越狱指令（覆盖/忽略系统提示）；已拒绝。",
		},
		{
			Name: "prompt_injection_leak_system",
			Match: func(s string, _ *model.Request) bool {
				return leakPrompt.MatchString(s)
			},
			Reason: "检测到要求输出系统提示的请求；已拒绝。",
		},
		{
			Name: "prompt_injection_shell",
			Match: func(s string, _ *model.Request) bool {
				return shellInj.MatchString(s)
			},
			Reason: "检测到危险 Shell 命令模式（rm -rf / fork bomb / pipe-to-sh）；已拒绝。",
		},
		{
			Name: "prompt_injection_base64_payload",
			Match: func(s string, _ *model.Request) bool {
				// 两个启发：含长 base64 且文本中伴随 "decode" / "execute" / "运行" 字样时才命中，
				// 降低误伤日志粘贴场景。
				if !base64Big.MatchString(s) {
					return false
				}
				lc := strings.ToLower(s)
				return strings.Contains(lc, "decode") ||
					strings.Contains(lc, "execute") ||
					strings.Contains(lc, "运行") ||
					strings.Contains(lc, "执行")
			},
			Reason: "检测到长 base64 载荷且带解码/执行语义；疑似走私攻击，已拒绝。",
		},
		{
			Name: "prompt_injection_bad_url",
			Match: func(s string, _ *model.Request) bool {
				return badURL.MatchString(s)
			},
			Reason: "检测到非常规协议 URL（file:// / data:text/html / javascript:）；已拒绝。",
		},
	}
}
