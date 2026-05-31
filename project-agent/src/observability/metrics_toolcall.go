// metrics_toolcall.go —— D28 工具调用可观测性埋点。
//
// # 为什么新增这批指标
//
// D26（prompt 重构）与 D27（E2E 真实场景）分别从"静态"和"跑得通"两个角度
// 保证了 LLM 选对工具；但它们都是**离线证据**，回答不了生产流量里：
//
//	"LLM 在真实告警场景下选工具的准确率是多少？"
//	"HITL 两段式的首次通过率有多少？Critical 被频繁触发吗？"
//	"R3 主键保护、R5 并发守护、reason-missing 哪个规则被触发最多？"
//
// 回答上面三个问题，需要给工具调用路径上 4 个关键节点打标：
//
//	1. 耗时分布            —— 回答"LLM 调用哪个工具最慢 / 最容易卡"
//	2. HITL 漏斗           —— 回答"Plan 出来后多少比例被用户 confirm，多少超时失效"
//	3. 拒绝原因              —— 回答"哪条兜底规则最常被触发 → 该条规则的 prompt 是否要改进"
//	4. 入参异常              —— 回答"LLM 构造参数的错误模式 → prompt 参数文档是否要改进"
//
// # 埋点策略
//
// 本文件**只定义指标与 Emit 函数**，具体埋点发生在 bcs_tools 包的中间件
// （bcstools.WithMetrics 装饰器）。指标命名与 D17+ 的"gameops.* + snake_case + 点分组"
// 风格保持一致，tag 数量严格控制（cardinality 安全）。
//
// # 与已有指标的分工
//
//	IncAgentToolCall (metrics.go)       —— 粗粒度：每个 agent 调哪些工具、成功失败计数
//	IncToolCallDuration (本文件)         —— 细粒度：每个工具调用的耗时分布（Histogram）
//	IncToolHITLStage   (本文件)         —— HITL 两段式的漏斗细分（plan/confirmed/rejected）
//	IncToolReject      (本文件)         —— 按 reason_code 细分的拒绝次数（R3/R5/validate/...）
//	IncToolInputAnomaly(本文件)         —— LLM 参数构造异常（missing_required/wrong_type/...）
//
// 这 4 者**正交**：同一次调用可以同时打多个指标。例如一次 set_tls 无 reason 的调用：
//   IncAgentToolCall(status="error") + IncToolReject(reason="critical_noreason")

package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ---------------------------------------------------------------------------
// 指标名常量（对外稳定 schema）—— D28 新增
// ---------------------------------------------------------------------------

const (
	// MetricToolCallDuration 工具调用耗时分布。
	// 维度：{tool, status} —— 与 IncAgentToolCall 的维度保持一致便于 join 查询。
	MetricToolCallDuration = "gameops.tool_call.duration.seconds"

	// MetricToolHITLStage HITL 两段式的漏斗计数。
	// 维度：{tool, stage}，stage ∈ plan / confirmed / rejected / disabled
	MetricToolHITLStage = "gameops.tool_call.hitl_stage.total"

	// MetricToolReject 工具显式拒绝某次调用时的原因细分。
	// 维度：{tool, reason} —— 典型 reason: r3_primary_key / r5_rv_conflict /
	//                        critical_noreason / validate_missing_field / hard_limit_exceeded
	MetricToolReject = "gameops.tool_call.reject.total"

	// MetricToolInputAnomaly LLM 调用工具时的参数构造异常（非拒绝但值得关注）。
	// 维度：{tool, anomaly}，anomaly ∈ missing_required / wrong_type / unknown_field / empty_required
	MetricToolInputAnomaly = "gameops.tool_call.input_anomaly.total"
)

// 新增标签键（与 LabelTool/LabelStatus/LabelAgent 复用 metrics.go 已定义常量）。
const (
	LabelHITLStage = "stage"   // HITL 漏斗阶段
	LabelReason    = "reason"  // 拒绝原因码
	LabelAnomaly   = "anomaly" // 参数异常类型
)

// HITL 阶段取值枚举（字符串对齐指标标签）。
const (
	HITLStagePlan      = "plan"       // 返回了 Plan，等待用户确认
	HITLStageConfirmed = "confirmed"  // 用户确认后真正执行
	HITLStageRejected  = "rejected"   // Plan 返回后用户拒绝 / 超时未回
	HITLStageDisabled  = "disabled"   // HITL_DISABLE=1 直通（测试 / CI 模式）
)

// ---------------------------------------------------------------------------
// Emit 函数
// ---------------------------------------------------------------------------

// ObserveToolCallDuration 记录一次工具调用的耗时（秒，允许 0）。
//
//	tool   ∈ bcs_pod_describe / bcs_scale_deployment / ...
//	status ∈ ok / error / pending  —— 与 IncAgentToolCall 保持一致
//
// 显式桶覆盖 1ms ~ 60s：
//   - 前段（1ms~50ms）适配 Mock 调用、schema 校验路径
//   - 中段（100ms~5s）覆盖绝大多数 BCS API 往返
//   - 后段（10s~60s）覆盖 helm/wait_for_ready 同步等待长尾
func ObserveToolCallDuration(ctx context.Context, tool, status string, seconds float64) {
	if seconds < 0 {
		return
	}
	if tool == "" {
		tool = "unknown"
	}
	if status == "" {
		status = StatusOK
	}
	hg := histos.get(MetricToolCallDuration,
		"Tool call latency distribution in seconds (per tool & status)",
		[]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60})
	hg.Record(ctx, seconds,
		metric.WithAttributes(
			attribute.String(LabelTool, tool),
			attribute.String(LabelStatus, status),
		))
}

// IncToolHITLStage 记录一次 HITL 漏斗阶段事件。
//
// 典型事件轨迹（以 bcs_scale_deployment 为例）：
//
//	[LLM 第 1 次调用，未 confirmed] → IncToolHITLStage(tool, "plan")
//	[LLM 第 2 次调用，带 confirmed=true] → IncToolHITLStage(tool, "confirmed")
//
// 单 stage 比率 = confirmed_count / plan_count = "HITL 首次通过率"
func IncToolHITLStage(ctx context.Context, tool, stage string) {
	if tool == "" {
		tool = "unknown"
	}
	if stage == "" {
		stage = "unknown"
	}
	ctrs.get(MetricToolHITLStage,
		"HITL funnel stage counter per tool (plan/confirmed/rejected/disabled)").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelTool, tool),
				attribute.String(LabelHITLStage, stage),
			))
}

// IncToolReject 记录一次工具侧显式拒绝。
//
//	reason 应使用稳定短码（不要把 error.Error() 整段 hash 进去，cardinality 会爆）
//	推荐取值：
//	  r3_primary_key        网络层 patch_spec 含主键被拒
//	  r5_rv_conflict        expected_resource_version 不匹配
//	  critical_noreason     Critical 操作未带 reason
//	  hard_limit_exceeded   scale |Δ| 超硬上限
//	  hpa_conflict_block    HPA 防护策略 block
//	  validate_missing      必填字段缺失
//	  validate_bad_value    字段值非法
func IncToolReject(ctx context.Context, tool, reason string) {
	if tool == "" {
		tool = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}
	ctrs.get(MetricToolReject,
		"Tool call explicit reject counter per tool & reason_code").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelTool, tool),
				attribute.String(LabelReason, reason),
			))
}

// IncToolInputAnomaly 记录 LLM 调用工具时的参数构造异常。
//
// 与 IncToolReject 的区别：
//   - Reject = 参数合法但规则拒绝（如 R3/R5/critical/hpa_block）
//   - Anomaly = 参数不合法 / schema 不符（LLM 构造问题，对应 prompt 文档要改进）
//
//	anomaly 典型取值：
//	  missing_required   必填字段为空
//	  wrong_type         字段类型不符（期望 int 传了 string）
//	  unknown_field      传入了 schema 未定义的字段
//	  empty_required     字段存在但值为空字符串/空列表
func IncToolInputAnomaly(ctx context.Context, tool, anomaly string) {
	if tool == "" {
		tool = "unknown"
	}
	if anomaly == "" {
		anomaly = "unknown"
	}
	ctrs.get(MetricToolInputAnomaly,
		"LLM tool input construction anomaly counter per tool & anomaly_type").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelTool, tool),
				attribute.String(LabelAnomaly, anomaly),
			))
}
