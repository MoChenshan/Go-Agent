// Package plugin 提供 GameOps Agent 的 Agent 级插件（基于框架 tool.Callbacks）。
//
// 插件清单：
//   - SafetyGuard  在 BeforeTool 阶段按规则拦截高危工具调用；
//   - AuditHook    在 AfterTool 阶段把"写操作"的关键字段落盘到 audit。
//
// 为什么不走全局 plugin.Plugin？
//   Agent 差异化挂载：RepairAgent 需要 SafetyGuard（写操作多），
//   KnowledgeAgent 则只读、无需拦截。tool.Callbacks 贴合这种按 Agent
//   配置的场景，并且与 Agent 层 Option `WithToolCallbacks` 天然配套。
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ----------------- SafetyGuard -----------------

// SafetyRule 单条拦截规则。
type SafetyRule struct {
	// Name 规则名，出现在拦截响应里供 LLM 自查。
	Name string
	// ToolName 匹配的工具名（完整匹配，空串视为"任何工具"）。
	ToolName string
	// Match 额外的参数匹配函数；返回 true 则视为命中该条规则。
	//   - raw 为工具入参 JSON 字节（可能为 nil）
	//   - args 为 lazy-unmarshal 的结果（失败时为 nil 的 map）
	Match func(raw []byte, args map[string]any) bool
	// Reason 拦截时回传给 LLM 的自然语言提示。
	Reason string
}

// SafetyConfig SafetyGuard 配置。
type SafetyConfig struct {
	// Rules 自定义规则；空则使用 DefaultRules()。
	Rules []SafetyRule
	// Logger 可选日志 hook；非 nil 时每次拦截都会被调用。
	Logger func(toolName, ruleName, reason string)
}

// SafetyGuard 在 BeforeTool 阶段按规则拦截，命中任一规则即以非 nil
// BeforeToolResult 返回，从而阻止真实工具执行。
type SafetyGuard struct {
	rules  []SafetyRule
	logger func(toolName, ruleName, reason string)
}

// NewSafetyGuard 构造 SafetyGuard，规则为空时落默认策略。
func NewSafetyGuard(cfg SafetyConfig) *SafetyGuard {
	rules := cfg.Rules
	if len(rules) == 0 {
		rules = DefaultRules()
	}
	return &SafetyGuard{rules: rules, logger: cfg.Logger}
}

// Register 把 SafetyGuard 挂到 tool.Callbacks 上。
func (g *SafetyGuard) Register(cb *tool.Callbacks) *tool.Callbacks {
	if cb == nil {
		cb = tool.NewCallbacks()
	}
	cb.RegisterBeforeTool(g.before)
	return cb
}

// before 实现 tool.BeforeToolCallbackStructured 签名。
func (g *SafetyGuard) before(_ context.Context, args *tool.BeforeToolArgs) (*tool.BeforeToolResult, error) {
	if args == nil {
		return nil, nil
	}
	raw := args.Arguments
	parsed := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &parsed)
	}
	for _, r := range g.rules {
		if r.ToolName != "" && r.ToolName != args.ToolName {
			continue
		}
		if r.Match == nil || !r.Match(raw, parsed) {
			continue
		}
		if g.logger != nil {
			g.logger(args.ToolName, r.Name, r.Reason)
		}
		// 以 CustomResult 形式把拒绝理由回传给 LLM，
		// LLM 看到后应改走安全通道（HITL / MR 等）。
		return &tool.BeforeToolResult{
			CustomResult: map[string]any{
				"blocked": true,
				"rule":    r.Name,
				"reason":  r.Reason,
			},
		}, nil
	}
	return nil, nil
}

// DefaultRules 内置的默认黑名单，覆盖执行方案提到的三类高危操作。
//
// 1. 工蜂 Git：`force_push=true` 一律拒绝，必须走普通 push + MR；
// 2. 工蜂 Git：`target_branch=master|main` 禁止 MR 直合（需人工确认合并）；
// 3. BCS Helm：`uninstall` 且无 `reason` 拒绝，防止误删线上集群；
// 4. 蓝盾 CI/CD：`pipeline_id` 为空时的 `devops_pipeline_rerun` 拒绝（避免打错流水线）。
//
// 业务侧可通过 SafetyConfig.Rules 覆盖/追加。
func DefaultRules() []SafetyRule {
	return []SafetyRule{
		{
			Name:     "block_force_push",
			ToolName: "",
			Match: func(_ []byte, args map[string]any) bool {
				v, ok := args["force_push"]
				return ok && asBool(v)
			},
			Reason: "force push 被 safety_guard 拦截；请走普通 push + 人工合并 MR。",
		},
		{
			Name:     "block_mr_auto_merge_to_main",
			ToolName: "gongfeng_mr_merge",
			Match: func(_ []byte, args map[string]any) bool {
				return isMainBranch(args["target_branch"])
			},
			Reason: "禁止自动合并 MR 到 master/main 分支；必须由人工点按合并。",
		},
		{
			Name:     "block_helm_uninstall_without_reason",
			ToolName: "bcs_helm_manage",
			Match: func(_ []byte, args map[string]any) bool {
				action := strings.ToLower(asString(args["action"]))
				if action != "uninstall" {
					return false
				}
				return strings.TrimSpace(asString(args["reason"])) == ""
			},
			Reason: "uninstall 必须提供 reason；已拦截，请补充原因后重试（或走 HITL 确认）。",
		},
		{
			Name:     "block_pipeline_rerun_empty_id",
			ToolName: "devops_pipeline_rerun",
			Match: func(_ []byte, args map[string]any) bool {
				return strings.TrimSpace(asString(args["pipeline_id"])) == ""
			},
			Reason: "pipeline_id 为空，禁止重跑；请先定位具体流水线。",
		},
	}
}

// ----------------- helpers -----------------

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "yes" || s == "on"
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return false
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	}
	return ""
}

func isMainBranch(v any) bool {
	s := strings.ToLower(strings.TrimSpace(asString(v)))
	return s == "master" || s == "main"
}
