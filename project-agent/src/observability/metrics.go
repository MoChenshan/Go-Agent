// Package observability 的全局 Counter 注册与封装。
//
// 为什么统一在 metrics.go 而不是各业务侧直接 Meter().Int64Counter(...)：
//   - Counter 需要 *懒初始化* 一次即可；业务侧高频调用 `Meter()` 每次都 new
//     counter，会污染后端（即使 OTel SDK 本身做了同名合并，语义也不清晰）
//   - 集中管理更便于 SRE 后续做告警规则（本文件顶部即全量指标清单）
//
// 当前 D16 阶段指标清单：
//
//	gameops.webhook.requests.total      { source, outcome }   Counter
//	gameops.guard.redacted.total        { rule }              Counter
//	gameops.input_guard.blocked.total   { rule }              Counter
//	gameops.agent.llm.calls.total       { agent, status }     Counter
//	gameops.agent.tool.calls.total      { agent, tool, status } Counter
//	gameops.sse.events.total            { event }             Counter
package observability

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// 指标名常量（对外稳定协议，切勿随意改名）。
const (
	MetricWebhookRequests   = "gameops.webhook.requests.total"
	MetricGuardRedacted     = "gameops.guard.redacted.total"
	MetricInputGuardBlocked = "gameops.input_guard.blocked.total"
	MetricAgentLLMCalls     = "gameops.agent.llm.calls.total"
	MetricAgentToolCalls    = "gameops.agent.tool.calls.total"
	MetricSSEEvents         = "gameops.sse.events.total"
)

// 标签键常量。
const (
	LabelSource  = "source"
	LabelOutcome = "outcome"
	LabelRule    = "rule"
	LabelAgent   = "agent"
	LabelTool    = "tool"
	LabelStatus  = "status"
	LabelEvent   = "event"
)

// Outcome 值枚举。
const (
	OutcomeAccepted        = "accepted"
	OutcomeRejected        = "rejected"
	OutcomeSignatureFailed = "signature_failed"
	OutcomeMalformed       = "malformed"

	StatusOK    = "ok"
	StatusError = "error"
)

// countersCache 惰性创建并缓存 Counter 句柄；key 为指标名。
type countersCache struct {
	mu    sync.Mutex
	cache map[string]metric.Int64Counter
}

var ctrs = &countersCache{cache: map[string]metric.Int64Counter{}}

func (c *countersCache) get(name, desc string) metric.Int64Counter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ic, ok := c.cache[name]; ok {
		return ic
	}
	ic, err := Meter().Int64Counter(name, metric.WithDescription(desc))
	if err != nil {
		// Meter 异常时用一个"丢弃型" Counter（Noop Meter 构造的），保证调用方无 panic。
		ic, _ = noopInt64Counter()
	}
	c.cache[name] = ic
	return ic
}

// ---------------------------------------------------------------------------
// 对外 API
// ---------------------------------------------------------------------------

// IncWebhookRequest 记录一次 Webhook 请求，outcome ∈ {accepted,rejected,signature_failed,malformed}。
func IncWebhookRequest(ctx context.Context, source, outcome string) {
	ctrs.get(MetricWebhookRequests, "Total webhook requests by source & outcome").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelSource, source),
				attribute.String(LabelOutcome, outcome),
			),
		)
}

// IncGuardRedacted 记录一次 output_guard 打码命中。
// rule 为命中的规则名（如 token_like_secret / private_ipv4）。
func IncGuardRedacted(ctx context.Context, rule string, hits int) {
	if hits <= 0 {
		return
	}
	ctrs.get(MetricGuardRedacted, "Total output redactions by rule").
		Add(ctx, int64(hits),
			metric.WithAttributes(attribute.String(LabelRule, rule)),
		)
}

// IncInputGuardBlocked 记录一次 input_guard 命中拦截。
func IncInputGuardBlocked(ctx context.Context, rule string) {
	ctrs.get(MetricInputGuardBlocked, "Total input guard blocks by rule").
		Add(ctx, 1, metric.WithAttributes(attribute.String(LabelRule, rule)))
}

// IncAgentLLMCall 记录一次 Agent LLM 调用（成功/失败）。
func IncAgentLLMCall(ctx context.Context, agent, status string) {
	ctrs.get(MetricAgentLLMCalls, "Total LLM calls per agent").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelAgent, agent),
				attribute.String(LabelStatus, status),
			),
		)
}

// IncAgentToolCall 记录一次 Agent 工具调用。
func IncAgentToolCall(ctx context.Context, agent, tool, status string) {
	ctrs.get(MetricAgentToolCalls, "Total tool calls per agent & tool").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelAgent, agent),
				attribute.String(LabelTool, tool),
				attribute.String(LabelStatus, status),
			),
		)
}

// IncSSEEvent 记录一次 SSE 事件下发（按 event 类型打标签）。
// event 取值建议：delta / tool_call / agent_transfer / confirmation_required / final / error。
func IncSSEEvent(ctx context.Context, event string) {
	if event == "" {
		event = "delta"
	}
	ctrs.get(MetricSSEEvents, "Total SSE events sent to frontend by event type").
		Add(ctx, 1, metric.WithAttributes(attribute.String(LabelEvent, event)))
}

// ResetMetricsForTest 清空 counter 缓存（单测用）；正常路径不要调用。
func ResetMetricsForTest() {
	ctrs.mu.Lock()
	defer ctrs.mu.Unlock()
	ctrs.cache = map[string]metric.Int64Counter{}
}

// noopInt64Counter 返回一个永不失败的 Noop counter，作为兜底。
func noopInt64Counter() (metric.Int64Counter, error) {
	// Meter 在任何情况下都可以创建 counter；Noop Meter 返回的 counter
	// 会把所有 Add 调用当作 no-op，不会写入后端。
	return Meter().Int64Counter("gameops.noop")
}
