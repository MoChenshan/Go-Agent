// metrics_middleware.go —— D28 工具调用可观测性中间件。
//
// # 设计思路
//
// 本文件实现一个"装饰器"模式的 tool.CallableTool 包装器：
//
//	WithMetrics(inner tool.CallableTool, toolName string) tool.CallableTool
//
// 它在 inner.Call 前后自动打 4 个 observability 指标（D28 新增），
// 无需修改任何具体工具的实现代码。
//
// 埋点位置：bcs_tools.go 的 NewAllTargeted / NewAllTargetedWithWaiter 工厂函数，
// 在 TargetedTool 列表构造完成后，对每个工具做一次 WithMetrics 包装。
// 这样所有 13 个 BCS 工具都自动获得指标，未来新增工具也无需额外操作。
//
// # 指标打法
//
//  1. ObserveToolCallDuration：每次 Call 结束后记录耗时，status 由返回值决定
//  2. IncToolHITLStage：检测返回结果里是否含 "awaiting_confirmation" 字段
//     → 是则打 stage=plan；若入参含 confirmed=true 则打 stage=confirmed
//  3. IncToolReject：Call 返回 error 时，从 error.Error() 里提取 reason_code
//  4. IncToolInputAnomaly：Call 返回 error 且 error 含 "missing" / "required" 等关键词时打
//
// # 关于 HITL stage 的识别
//
// 工具返回 PendingResult 时，Call 本身**不返回 error**（Plan 是正常响应）。
// 中间件通过把 any 结果序列化为 JSON 后检查 "awaiting_confirmation" 字符串来判定。
// 这是一个"黑盒"识别，不依赖 hitl 包的具体类型，避免循环导入。
package bcstools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/observability"
)

// metricsMiddleware 是 tool.CallableTool 的装饰器，自动打 D28 指标。
type metricsMiddleware struct {
	inner    tool.CallableTool
	toolName string
}

// WithMetrics 包装一个 CallableTool，使其自动打 D28 可观测指标。
//
// toolName 应与工具的 Declaration().Name 一致（如 "bcs_scale_deployment"），
// 用于指标的 {tool} 标签。
func WithMetrics(inner tool.CallableTool, toolName string) tool.CallableTool {
	return &metricsMiddleware{inner: inner, toolName: toolName}
}

// Declaration 透传内层工具的声明（中间件对 LLM 透明）。
//
// 注意：trpc-agent-go 的 tool.Tool 接口签名为 Declaration() *Declaration，
// 这里必须返回指针以满足 CallableTool（含 Tool）接口约束。
func (m *metricsMiddleware) Declaration() *tool.Declaration {
	return m.inner.Declaration()
}

// Call 执行内层工具并在前后打指标。
func (m *metricsMiddleware) Call(ctx context.Context, argsJSON []byte) (any, error) {
	start := time.Now()

	// ---- 入参预分析：检测 confirmed 字段（用于 HITL stage 判定）----
	confirmedInInput := extractConfirmed(argsJSON)

	// ---- 执行内层工具 ----
	result, err := m.inner.Call(ctx, argsJSON)
	elapsed := time.Since(start).Seconds()

	// ---- 指标 1：耗时分布 ----
	status := observability.StatusOK
	if err != nil {
		status = observability.StatusError
	} else if isPendingResult(result) {
		status = "pending"
	}
	observability.ObserveToolCallDuration(ctx, m.toolName, status, elapsed)

	// ---- 指标 2：HITL 漏斗 ----
	if err == nil {
		if isPendingResult(result) {
			// 工具返回了 Plan，等待用户确认
			observability.IncToolHITLStage(ctx, m.toolName, observability.HITLStagePlan)
		} else if confirmedInInput {
			// 用户已 confirmed，工具真正执行了
			observability.IncToolHITLStage(ctx, m.toolName, observability.HITLStageConfirmed)
		}
	}

	// ---- 指标 3：拒绝原因 ----
	if err != nil {
		reason := extractRejectReason(err.Error())
		observability.IncToolReject(ctx, m.toolName, reason)
	}

	// ---- 指标 4：入参异常（从 error 里识别 LLM 参数构造问题）----
	if err != nil {
		if anomaly := extractInputAnomaly(err.Error()); anomaly != "" {
			observability.IncToolInputAnomaly(ctx, m.toolName, anomaly)
		}
	}

	return result, err
}

// ---------------------------------------------------------------------------
// 辅助函数（包内私有）
// ---------------------------------------------------------------------------

// isPendingResult 检测工具返回值是否是 HITL Plan 阶段的 PendingResult。
//
// 识别方式：把 result 序列化为 JSON，检查是否含 "awaiting_confirmation" 字符串。
// 这是黑盒识别，不依赖 hitl 包类型，避免循环导入。
func isPendingResult(result any) bool {
	if result == nil {
		return false
	}
	bs, err := json.Marshal(result)
	if err != nil {
		return false
	}
	return strings.Contains(string(bs), `"awaiting_confirmation"`)
}

// extractConfirmed 从 argsJSON 里提取 confirmed 字段（bool）。
// 若解析失败或字段不存在，返回 false。
func extractConfirmed(argsJSON []byte) bool {
	if len(argsJSON) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(argsJSON, &m); err != nil {
		return false
	}
	raw, ok := m["confirmed"]
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
}

// extractRejectReason 从 error 字符串里提取稳定的 reason_code。
//
// 规则（按优先级）：
//  1. 含 "R3" → r3_primary_key
//  2. 含 "R5" → r5_rv_conflict
//  3. 含 "reason" 且含 "critical" / "Critical" → critical_noreason
//  4. 含 "hard limit" / "hardLimit" / "HardLimit" → hard_limit_exceeded
//  5. 含 "HPA" 且含 "block" → hpa_conflict_block
//  6. 含 "immutable" → immutable_resource
//  7. 含 "TLS" 且含 "cert" / "key" → tls_cert_mismatch
//  8. 其他 → unknown
//
// 注意：这里做的是"尽力而为"的模式匹配，不是精确解析。
// 如果某条规则的 error 文案改变，只需更新这里的匹配条件，不影响指标 schema。
func extractRejectReason(errMsg string) string {
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(errMsg, "R3"):
		return "r3_primary_key"
	case strings.Contains(errMsg, "R5"):
		return "r5_rv_conflict"
	case strings.Contains(lower, "reason") && strings.Contains(lower, "critical"):
		return "critical_noreason"
	case strings.Contains(lower, "hard limit") || strings.Contains(lower, "hardlimit"):
		return "hard_limit_exceeded"
	case strings.Contains(lower, "hpa") && strings.Contains(lower, "block"):
		return "hpa_conflict_block"
	case strings.Contains(lower, "immutable"):
		return "immutable_resource"
	case strings.Contains(lower, "tls") && (strings.Contains(lower, "cert") || strings.Contains(lower, "key")):
		return "tls_cert_mismatch"
	default:
		return "unknown"
	}
}

// extractInputAnomaly 从 error 字符串里识别 LLM 参数构造异常。
//
// 返回空字符串表示"不是参数构造问题"（可能是规则拒绝或 API 错误）。
// 只有确认是 LLM 参数问题时才打 anomaly 指标，避免误报。
func extractInputAnomaly(errMsg string) string {
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(lower, "missing") && strings.Contains(lower, "required"):
		return "missing_required"
	case strings.Contains(lower, "must not be empty") || strings.Contains(lower, "不能为空") ||
		strings.Contains(lower, "必填"):
		return "empty_required"
	case strings.Contains(lower, "invalid") && strings.Contains(lower, "type"):
		return "wrong_type"
	case strings.Contains(lower, "unknown field") || strings.Contains(lower, "未知字段"):
		return "unknown_field"
	default:
		return ""
	}
}
