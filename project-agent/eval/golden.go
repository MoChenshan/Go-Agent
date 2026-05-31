// Package eval 提供 GameOps Agent 的离线评估（golden set）辅助能力。
//
// 本文件不依赖 trpc-agent-go/evaluation 独立 module，只做最朴素的：
//  1. 解析 golden set 的 JSON 结构；
//  2. 校验所有 case 引用的工具名在当前 App 的工具清单中存在；
//  3. 校验 appName 与运行期一致；
//  4. 统计 case 数、tool_call 次数。
//
// 真实的评测执行（Agent 推理 + Metric 打分）走 build tag `eval` 下的
// `eval/cmd/evalrun`，避免默认构建拖入 evaluation 独立 module 依赖。
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// EvalSet 对应 trpc-agent-go/evaluation/evalset JSON 结构的最小子集。
type EvalSet struct {
	EvalSetID         string     `json:"evalSetId"`
	Name              string     `json:"name"`
	EvalCases         []EvalCase `json:"evalCases"`
	CreationTimestamp float64    `json:"creationTimestamp,omitempty"`
}

// EvalCase 单个评测用例。
type EvalCase struct {
	EvalID       string         `json:"evalId"`
	Conversation []Invocation   `json:"conversation"`
	SessionInput SessionInput   `json:"sessionInput"`
}

// SessionInput 会话初始化参数。
type SessionInput struct {
	AppName string `json:"appName"`
	UserID  string `json:"userId"`
}

// Invocation 单轮（用户一问 → Agent 一答）的完整轨迹。
type Invocation struct {
	InvocationID  string    `json:"invocationId"`
	UserContent   Content   `json:"userContent"`
	FinalResponse Content   `json:"finalResponse"`
	Tools         []ToolUse `json:"tools"`
}

// Content OpenAI 风格的 role+content。
type Content struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolUse 期望的工具调用快照。
type ToolUse struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Result    any            `json:"result"`
}

// LoadEvalSet 从指定路径读取并解析 evalset JSON。
func LoadEvalSet(path string) (*EvalSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read evalset %s: %w", path, err)
	}
	var set EvalSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return nil, fmt.Errorf("unmarshal evalset %s: %w", path, err)
	}
	if set.EvalSetID == "" {
		return nil, fmt.Errorf("evalset %s: missing evalSetId", path)
	}
	if len(set.EvalCases) == 0 {
		return nil, fmt.Errorf("evalset %s: evalCases is empty", path)
	}
	return &set, nil
}

// MetricConfig 对应 metric 配置文件中的单条度量。
type MetricConfig struct {
	MetricName string         `json:"metricName"`
	Threshold  float64        `json:"threshold"`
	Criterion  map[string]any `json:"criterion"`
}

// LoadMetrics 从指定路径读取并解析 metrics JSON（根为数组）。
func LoadMetrics(path string) ([]MetricConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metrics %s: %w", path, err)
	}
	var metrics []MetricConfig
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return nil, fmt.Errorf("unmarshal metrics %s: %w", path, err)
	}
	if len(metrics) == 0 {
		return nil, fmt.Errorf("metrics %s: no metric declared", path)
	}
	for i, m := range metrics {
		if m.MetricName == "" {
			return nil, fmt.Errorf("metrics %s[%d]: missing metricName", path, i)
		}
	}
	return metrics, nil
}

// ValidateAgainstToolRegistry 校验 evalset 中所有 tool_call 名称都在 allowed 集合里。
//
// 返回 missing 列表（保持首次出现顺序去重）。 missing 为空代表校验通过。
func (s *EvalSet) ValidateAgainstToolRegistry(allowed map[string]struct{}) []string {
	seen := map[string]bool{}
	var missing []string
	for _, c := range s.EvalCases {
		for _, inv := range c.Conversation {
			for _, t := range inv.Tools {
				if _, ok := allowed[t.Name]; ok {
					continue
				}
				if seen[t.Name] {
					continue
				}
				seen[t.Name] = true
				missing = append(missing, t.Name)
			}
		}
	}
	sort.Strings(missing)
	return missing
}

// ValidateAppName 校验所有 case 的 sessionInput.appName 与 expected 一致。
func (s *EvalSet) ValidateAppName(expected string) []string {
	var bad []string
	for _, c := range s.EvalCases {
		if c.SessionInput.AppName != expected {
			bad = append(bad, fmt.Sprintf("%s:%s", c.EvalID, c.SessionInput.AppName))
		}
	}
	return bad
}

// Summary 统计摘要：case 数、总 tool_call 次数、涉及工具名去重列表。
type Summary struct {
	CaseCount  int
	InvCount   int
	ToolCalls  int
	ToolNames  []string
}

// Summarize 输出 evalset 的统计摘要。
func (s *EvalSet) Summarize() Summary {
	sum := Summary{CaseCount: len(s.EvalCases)}
	seen := map[string]struct{}{}
	for _, c := range s.EvalCases {
		sum.InvCount += len(c.Conversation)
		for _, inv := range c.Conversation {
			sum.ToolCalls += len(inv.Tools)
			for _, t := range inv.Tools {
				if _, ok := seen[t.Name]; !ok {
					seen[t.Name] = struct{}{}
					sum.ToolNames = append(sum.ToolNames, t.Name)
				}
			}
		}
	}
	sort.Strings(sum.ToolNames)
	return sum
}

// DefaultDataDir 返回默认的 golden 数据目录（相对于模块根的 eval/data）。
func DefaultDataDir() string {
	// 允许通过环境变量覆盖，便于 CI / 本地定制
	if v := os.Getenv("GAMEOPS_EVAL_DATA_DIR"); v != "" {
		return v
	}
	return filepath.Join("eval", "data")
}

// DefaultAppName 与 src/app/app.go 中 AppName 保持同步（单一真实来源）。
const DefaultAppName = "gameops-agent"

// DefaultEvalSetID 当前唯一的核心评测集 ID。
const DefaultEvalSetID = "gameops-core"
