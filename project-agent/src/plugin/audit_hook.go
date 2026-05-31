package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
)

// AuditHookConfig 审计钩子配置。
type AuditHookConfig struct {
	// AgentName 默认 Agent 名（ctx 里取不到 Invocation 时兜底）。
	AgentName string
	// WriteTools 视为"写操作"的工具名前缀/全名集合；只有命中才会 Emit。
	//   默认：gongfeng_mr_* / devops_pipeline_* / bcs_helm_manage /
	//        tapd_bug_create / tapd_issue_update / devops_build_cancel
	WriteTools []string
	// Severity 按工具名路由严重度（可选）；未命中则 "medium"。
	Severity func(toolName string) string
}

// DefaultWriteTools 返回默认的"写操作"工具名（前缀匹配）列表。
func DefaultWriteTools() []string {
	return []string{
		"gongfeng_mr_",         // mr_create / mr_merge
		"gongfeng_push",        // 直接 push
		"devops_pipeline_",     // pipeline_rerun / pipeline_trigger
		"devops_build_cancel",  // 取消构建
		"bcs_helm_manage",      // deploy/upgrade/rollback/uninstall
		"tapd_bug_create",      // 创建缺陷单
		"tapd_issue_update",    // 更新工单
	}
}

// AuditHook 在 AfterTool 阶段把写操作记录到 audit 包。
type AuditHook struct {
	agentName  string
	writeTools []string
	severity   func(string) string
}

// NewAuditHook 构造钩子。空配置走默认。
func NewAuditHook(cfg AuditHookConfig) *AuditHook {
	if len(cfg.WriteTools) == 0 {
		cfg.WriteTools = DefaultWriteTools()
	}
	if cfg.Severity == nil {
		cfg.Severity = defaultSeverity
	}
	return &AuditHook{
		agentName:  cfg.AgentName,
		writeTools: cfg.WriteTools,
		severity:   cfg.Severity,
	}
}

// Register 把钩子挂载到 tool.Callbacks（AfterTool 阶段）。
func (h *AuditHook) Register(cb *tool.Callbacks) *tool.Callbacks {
	if cb == nil {
		cb = tool.NewCallbacks()
	}
	cb.RegisterAfterTool(h.after)
	return cb
}

// after 实现 AfterToolCallbackStructured 签名。
func (h *AuditHook) after(ctx context.Context, args *tool.AfterToolArgs) (*tool.AfterToolResult, error) {
	if args == nil {
		return nil, nil
	}
	if !h.isWriteTool(args.ToolName) {
		return nil, nil
	}

	params := map[string]any{}
	if len(args.Arguments) > 0 {
		_ = json.Unmarshal(args.Arguments, &params)
	}

	agentName := h.agentName
	if inv, ok := agent.InvocationFromContext(ctx); ok && inv != nil && inv.AgentName != "" {
		agentName = inv.AgentName
	}

	ev := audit.Event{
		Agent:    agentName,
		Action:   "tool." + args.ToolName,
		Severity: h.severity(args.ToolName),
		Target:   extractTarget(params),
		Params:   shrinkParams(params),
		Success:  args.Error == nil,
		Err:      args.Error,
	}
	audit.Emit(ev)
	// 记录一次调用耗时戳，方便与 OTel 指标对照（不影响下游）。
	_ = time.Now()
	return nil, nil
}

// isWriteTool 判断工具名是否在"写操作"白名单内（前缀或全名匹配）。
func (h *AuditHook) isWriteTool(name string) bool {
	for _, p := range h.writeTools {
		if p == name || strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// defaultSeverity 根据工具名粗分破坏等级。
func defaultSeverity(toolName string) string {
	switch {
	case strings.Contains(toolName, "uninstall"),
		strings.HasPrefix(toolName, "gongfeng_mr_merge"),
		strings.HasPrefix(toolName, "gongfeng_push"):
		return "critical"
	case strings.Contains(toolName, "helm_manage"),
		strings.HasPrefix(toolName, "devops_pipeline_"):
		return "high"
	default:
		return "medium"
	}
}

// extractTarget 尝试从常见入参里挑一个用作 Target 显示。
func extractTarget(params map[string]any) string {
	for _, k := range []string{"target", "cluster", "release", "project_id", "pipeline_id", "iid"} {
		if v, ok := params[k]; ok {
			s := toString(v)
			if s != "" {
				return s
			}
		}
	}
	return ""
}

// shrinkParams 只保留关键字段，避免日志被参数淹没 / 无意间记录敏感信息。
// 默认把一切带 "token" / "secret" / "password" 字样的字段剔除。
func shrinkParams(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "token") ||
			strings.Contains(lk, "secret") ||
			strings.Contains(lk, "password") ||
			strings.Contains(lk, "api_key") {
			continue
		}
		out[k] = v
	}
	return out
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		// JSON 数字统一走这里；取整/带小数都能接受。
		return trimFloat(x)
	case int:
		return trimFloat(float64(x))
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func trimFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
