// metrics_more.go D17.4 — OTLP Metric 扩展指标集合。
//
// 本文件扩展了 metrics.go 在 D13/D16 建立的"集中注册 + 惰性缓存"模式，新增了
// 四条前序里程碑所需、但之前一直缺位的生产级可观测指标：
//
//	gameops.audit.remote.enqueued.total    Counter{}       D17.3 → 本轮
//	gameops.audit.remote.delivered.total   Counter{}
//	gameops.audit.remote.dropped.total     Counter{}       告警重点
//	gameops.audit.remote.failed.total      Counter{}       告警重点
//	gameops.judge.calls.total              Counter{status} D17.2 → 本轮
//	gameops.judge.latency.seconds          Histogram{}     D17.2 → 本轮
//	gameops.rule.reload.total              Counter{kind,status} D17.1 → 本轮
//
// 设计决策（关键）：
//  1. **为什么 RemoteSink 走"差值 Pump"而不是直接在 Write 里 Inc**？
//     RemoteSink.Write 是业务热路径，多一次 OTel Counter.Add（即便是 Noop）就是
//     额外的 mutex + attribute allocator 开销；且 Dropped/Failed 发生在**后台
//     worker**里，在 Write 里根本拿不到。正确方式：RemoteSink.Stats() 已提供
//     atomic 快照，外部周期性（~10s）拉一次"差值"转为 Counter.Add。这是 Prometheus
//     pull / OTel push 模式下与 atomic counter 对接的标准桥接手法。
//  2. **为什么 Judge 用 Histogram 而非 Counter**？
//     Counter 只能算 QPS，不能回答"P95 评审耗时多久"。Histogram 原生支持分位数，
//     是延迟指标的标准类型。这里为 ObserveJudgeLatency 预留 ~50ms ~ 60s 的桶，
//     覆盖本地 Mock 到真实 LLM 的全区间。
//  3. **为什么新增 kind=input|output|guard|audit|judge 的 rule.reload 指标**？
//     D17.1 引入了规则热加载，但 watcher 默认只打日志；有指标后 SRE 能告警
//     "规则重载连续失败 > 3 次"—— 静默错配置导致的安全漏洞风险被前置暴露。
//
// 测试要点：全部通过 OTel SDK 的 ManualReader 断言，不走真实 collector。

package observability

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ---------------------------------------------------------------------------
// 指标名常量（对外稳定 schema）
// ---------------------------------------------------------------------------

const (
	// D17.3 审计远端汇聚
	MetricAuditRemoteEnqueued  = "gameops.audit.remote.enqueued.total"
	MetricAuditRemoteDelivered = "gameops.audit.remote.delivered.total"
	MetricAuditRemoteDropped   = "gameops.audit.remote.dropped.total"
	MetricAuditRemoteFailed    = "gameops.audit.remote.failed.total"
	// D17.2 LLM Judge
	MetricJudgeCalls   = "gameops.judge.calls.total"
	MetricJudgeLatency = "gameops.judge.latency.seconds"
	// D17.1 规则热加载
	MetricRuleReload = "gameops.rule.reload.total"
	// D19.4 async 异步任务框架（配套 D19.2）
	MetricAsyncJobsSubmitted = "gameops.async.jobs.submitted.total" // {tool, outcome=accepted/rejected/dedup_hit}
	MetricAsyncJobsFinished  = "gameops.async.jobs.finished.total"  // {tool, status=succeeded/failed/cancelled/timed_out}
	MetricAsyncJobDuration   = "gameops.async.jobs.duration.seconds" // {tool, status} Histogram
	// D19.5 pod_restart.wait_for_ready 真实化
	MetricPodReadyWaitTotal    = "gameops.pod_ready_wait.total"            // {mode, status=ready/timeout/cancelled/error/skipped}
	MetricPodReadyWaitDuration = "gameops.pod_ready_wait.duration.seconds" // {mode, status} Histogram
	// D19.8 FastPollReadyWaiter（抽象可替换性验证）
	MetricFastPollFinished    = "gameops.ready_waiter.fast_poll.finished.total"    // {mode, reason=ready/timeout/canceled/error/bad_spec}
	MetricFastPollProbeBucket = "gameops.ready_waiter.fast_poll.ready_probe.total" // {mode, bucket=first/fast_stage/steady} —— 首探命中率观测
	MetricFastPollProbesPer   = "gameops.ready_waiter.fast_poll.probes_per_wait"   // {mode, reason} Histogram(次数)
)

// 新增标签键。
const (
	LabelKind = "kind" // 规则类型：input / output / judge / ...
)

// ---------------------------------------------------------------------------
// Histogram 缓存（与 countersCache 呼应的独立缓存）
// ---------------------------------------------------------------------------

type histogramsCache struct {
	mu    sync.Mutex
	cache map[string]metric.Float64Histogram
}

var histos = &histogramsCache{cache: map[string]metric.Float64Histogram{}}

// get 惰性创建 Float64Histogram。
//
// buckets 为显式桶边界（秒），nil 时 SDK 走默认桶。
func (h *histogramsCache) get(name, desc string, buckets []float64) metric.Float64Histogram {
	h.mu.Lock()
	defer h.mu.Unlock()
	if hg, ok := h.cache[name]; ok {
		return hg
	}
	opts := []metric.Float64HistogramOption{metric.WithDescription(desc)}
	if len(buckets) > 0 {
		opts = append(opts, metric.WithExplicitBucketBoundaries(buckets...))
	}
	hg, err := Meter().Float64Histogram(name, opts...)
	if err != nil {
		// 同 countersCache 兜底策略：失败走 Noop，调用方无需判空。
		hg, _ = Meter().Float64Histogram("gameops.noop_histogram")
	}
	h.cache[name] = hg
	return hg
}

// ResetHistogramsForTest 清空 histogram 缓存（单测用）。
func ResetHistogramsForTest() {
	histos.mu.Lock()
	defer histos.mu.Unlock()
	histos.cache = map[string]metric.Float64Histogram{}
}

// ---------------------------------------------------------------------------
// 对外 API - 直接打点
// ---------------------------------------------------------------------------

// IncJudgeCall 记录一次 LLM Judge 调用。status ∈ {ok, error, timeout, parse_error}。
func IncJudgeCall(ctx context.Context, status string) {
	if status == "" {
		status = StatusOK
	}
	ctrs.get(MetricJudgeCalls, "Total LLM judge calls by status").
		Add(ctx, 1, metric.WithAttributes(attribute.String(LabelStatus, status)))
}

// ObserveJudgeLatency 记录一次 Judge 耗时（秒）。
//
// 显式桶覆盖 50ms ~ 60s：前段适配 Mock/小模型，后段适配大模型/高延迟场景。
func ObserveJudgeLatency(ctx context.Context, seconds float64) {
	if seconds < 0 {
		return
	}
	hg := histos.get(MetricJudgeLatency,
		"LLM judge latency distribution in seconds",
		[]float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60})
	hg.Record(ctx, seconds)
}

// IncRuleReload 记录一次规则热加载事件。
//
//	kind   ∈ input_guard / output_guard / judge_prompt / ...
//	status ∈ ok / parse_error / read_error / unchanged
func IncRuleReload(ctx context.Context, kind, status string) {
	if kind == "" {
		kind = "unknown"
	}
	if status == "" {
		status = StatusOK
	}
	ctrs.get(MetricRuleReload, "Total rule hot-reload attempts by kind & status").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelKind, kind),
				attribute.String(LabelStatus, status),
			),
		)
}

// ---------------------------------------------------------------------------
// D19.4 async 异步任务指标（配套 D19.2 async-tool 框架）
// ---------------------------------------------------------------------------

// IncAsyncJobSubmitted 记录一次 Submit 尝试的结局。
//
//	tool    — 被提交的工具名（如 bcs_pod_restart）
//	outcome ∈ accepted        提交成功进入队列
//	         rejected         被限流拒绝（queue full / 非法参数）
//	         dedup_hit        幂等键命中，复用已有 Job
//
// 为什么和 Finished 拆成两个 Counter？
//   - Submitted 关注"入口压力"，Finished 关注"出口产能"；
//     两者的 rate 差能推导当前积压（面板上 `sum(rate(submitted)) - sum(rate(finished))`），
//     比 ObservableGauge 成本更低、语义更稳。
func IncAsyncJobSubmitted(ctx context.Context, tool, outcome string) {
	if tool == "" {
		tool = "unknown"
	}
	if outcome == "" {
		outcome = "accepted"
	}
	ctrs.get(MetricAsyncJobsSubmitted, "Total async jobs submitted by tool & outcome").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelTool, tool),
				attribute.String(LabelOutcome, outcome),
			),
		)
}

// IncAsyncJobFinished 记录一次 Job 终态。务必在 finish() 里唯一调用一次。
//
//	status ∈ succeeded / failed / cancelled / timed_out
//
// timed_out 与 cancelled 分开上报至关重要：
//   - timed_out = 工具本身慢，SRE 需扩 timeout 或优化工具
//   - cancelled = 用户主动取消，无需告警
// 混淆两者会让面板失信（见 D19.3 场景 6）。
func IncAsyncJobFinished(ctx context.Context, tool, status string) {
	if tool == "" {
		tool = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	ctrs.get(MetricAsyncJobsFinished, "Total async jobs finished by tool & terminal status").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String(LabelTool, tool),
				attribute.String(LabelStatus, status),
			),
		)
}

// ObserveAsyncJobDuration 记录 submit→finish 的总耗时（含排队）。
//
// 桶设计：覆盖 50ms（内部快工具）~ 5min（pod 重启 + 就绪等待上限）。
// 生产级 SLO：p95 应落在工具各自的 timeout 的 60% 以内。
func ObserveAsyncJobDuration(ctx context.Context, tool, status string, seconds float64) {
	if seconds < 0 {
		return
	}
	if tool == "" {
		tool = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	hg := histos.get(MetricAsyncJobDuration,
		"Async job total duration from submit to terminal state, in seconds",
		[]float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120, 300})
	hg.Record(ctx, seconds,
		metric.WithAttributes(
			attribute.String(LabelTool, tool),
			attribute.String(LabelStatus, status),
		),
	)
}

// ---------------------------------------------------------------------------
// D19.5 pod_restart wait_for_ready 指标
// ---------------------------------------------------------------------------

// IncPodReadyWait 记录一次 ReadyWaiter 的最终结局。
//
// mode   ∈ delete_pod / rollout_restart / evict_pod
// status ∈ ready / timeout / cancelled / error / skipped
//
// 为什么不复用 async 指标？
//   - async 指标关注"任务提交/完成"的粒度；
//   - ready_wait 是任务内部的子阶段，需要独立分析（有的任务不带 wait，有的带）。
//   把两者拉通会污染 async 的 SLO 信号。
func IncPodReadyWait(ctx context.Context, mode, status string) {
	if mode == "" {
		mode = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	ctrs.get(MetricPodReadyWaitTotal,
		"Total pod_restart wait_for_ready invocations by mode & status").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("mode", mode),
				attribute.String(LabelStatus, status),
			),
		)
}

// ObservePodReadyWaitDuration 记录一次 ReadyWaiter 实际耗时。
//
// 桶设计：
//   - 1s~5s  单 Pod 拉起
//   - 10s~60s 滚动重启典型区间
//   - 2min~5min rollout_restart 大 Deployment 的上限
// p95 > 60s 即触发 GameOpsPodReadyWaitSlow 告警（下一轮补）。
func ObservePodReadyWaitDuration(ctx context.Context, mode, status string, seconds float64) {
	if seconds < 0 {
		return
	}
	if mode == "" {
		mode = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	hg := histos.get(MetricPodReadyWaitDuration,
		"Pod ready_wait duration from probe start to terminal state, in seconds",
		[]float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120, 180, 300})
	hg.Record(ctx, seconds,
		metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.String(LabelStatus, status),
		),
	)
}

// ---------------------------------------------------------------------------
// D19.8 FastPollReadyWaiter 指标
// ---------------------------------------------------------------------------

// IncFastPollFinished 上报一次 FastPollWaiter 的最终结局。
//
// mode   与 MetricPodReadyWaitTotal 共用字典（delete_pod/rollout_restart/evict_pod/
//        scale_deployment/helm_rollback/helm_install）—— 这让面板可以直接对比
//        "同一场景下 FastPoll vs 传统 Poll 的耗时差异"。
// reason ∈ ready / timeout / canceled / error / bad_spec
//
// 注意：本指标与 MetricPodReadyWaitTotal 互补而非重叠——后者统一统计"任意 Waiter
// 实现"的终态，前者专门统计 FastPoll 实现内部观测到的原始事件。运维可通过两者对比
// 排查"Waiter 说 ready 但上层感知不到"这类边界 bug。
func IncFastPollFinished(ctx context.Context, mode, reason string) {
	if mode == "" {
		mode = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}
	ctrs.get(MetricFastPollFinished,
		"Total FastPollReadyWaiter terminations by mode & reason").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("mode", mode),
				attribute.String("reason", reason),
			),
		)
}

// IncFastPollProbeBucket 把"首次判定 ready 时的 probe 序号"映射成分桶标签，
// 这是 FastPoll 是否真的比传统 Poll 更快的**唯一核心指标**。
//
// 分桶策略：
//
//	first     → probeIdx == 1（首探即命中，说明目标本来就 ready，快路径生效）
//	fast_stage→ probeIdx ∈ [2, 5]（阶梯退避区间命中，FastPoll 相比传统 Poll 的增量价值）
//	steady    → probeIdx >  5（进入稳态轮询后命中，FastPoll 与传统 Poll 表现等同）
//
// 运维判断依据：
//   - first + fast_stage 占比 >60% → FastPoll 实质提升用户感知延迟
//   - steady 占比接近 100%         → 业务普遍是慢场景，FastPoll 无增量价值，但也无回归
func IncFastPollProbeBucket(ctx context.Context, mode string, probeIdx int) {
	if mode == "" {
		mode = "unknown"
	}
	bucket := "steady"
	switch {
	case probeIdx == 1:
		bucket = "first"
	case probeIdx >= 2 && probeIdx <= 5:
		bucket = "fast_stage"
	}
	ctrs.get(MetricFastPollProbeBucket,
		"FastPollReadyWaiter first-ready probe bucketed by schedule stage").
		Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("mode", mode),
				attribute.String("bucket", bucket),
			),
		)
}

// ObserveFastPollProbesPer 记录一次 Wait 生命周期内发出的 probe 总次数（含失败重试）。
// 用于回答"FastPoll 是否在等待超时前做了过多的无效查询压测 BCS"。
func ObserveFastPollProbesPer(ctx context.Context, mode, reason string, probes int) {
	if probes < 0 {
		return
	}
	if mode == "" {
		mode = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}
	hg := histos.get(MetricFastPollProbesPer,
		"Number of probes emitted per FastPoll Wait cycle",
		[]float64{1, 2, 3, 5, 10, 20, 50, 100, 200})
	hg.Record(ctx, float64(probes),
		metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.String("reason", reason),
		),
	)
}

// ---------------------------------------------------------------------------
// AuditRemote Pump — 周期性把 atomic Stats 差值转成 Counter
// ---------------------------------------------------------------------------

// RemoteSinkStatsProvider 抽象 audit.RemoteSink.Stats()，
// 避免 observability 包反向依赖 audit 包（防止循环 import）。
//
// audit.RemoteSinkStats 结构体字段与本接口返回值一一对应，
// 由 app 层适配即可。
type RemoteSinkStatsProvider interface {
	// SnapshotStats 返回当前累计计数（调用安全、非阻塞、无锁）。
	SnapshotStats() (enqueued, delivered, dropped, failed int64)
}

// AuditRemoteMetricsPump 后台 goroutine：周期性读差值，上报 Counter。
//
// 用法：
//
//	pump := observability.StartAuditRemoteMetricsPump(ctx, sink, 10*time.Second)
//	defer pump.Stop()
//
// 即便 ctx 一直不 cancel，Stop 也能独立停；防止调用方忘记 defer。
type AuditRemoteMetricsPump struct {
	provider RemoteSinkStatsProvider
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}

	// last* 保存上一次快照值，用于计算差值。
	lastEnq, lastDel, lastDrp, lastFail int64
	// 额外计数（便于单测断言 pump 被 tick 了几次）。
	ticks atomic.Int64
	// 幂等关闭保护。
	stopped atomic.Bool
}

// StartAuditRemoteMetricsPump 启动并返回 pump；立即开始运行。
// interval <= 0 时走默认 15s。provider 为 nil 时返回一个 no-op pump（Stop 仍可调用）。
func StartAuditRemoteMetricsPump(ctx context.Context,
	provider RemoteSinkStatsProvider, interval time.Duration) *AuditRemoteMetricsPump {

	if interval <= 0 {
		interval = 15 * time.Second
	}
	p := &AuditRemoteMetricsPump{
		provider: provider,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	if provider == nil {
		// no-op：直接关闭 done 让 Stop 幂等。
		close(p.done)
		return p
	}
	go p.run(ctx)
	return p
}

// Stop 停止后台 goroutine；幂等。
func (p *AuditRemoteMetricsPump) Stop() {
	if p == nil || !p.stopped.CompareAndSwap(false, true) {
		return
	}
	close(p.stop)
	<-p.done
}

// Ticks 已执行的 tick 次数（测试断言用）。
func (p *AuditRemoteMetricsPump) Ticks() int64 {
	if p == nil {
		return 0
	}
	return p.ticks.Load()
}

// run 主循环。
//
// 差值计算必须严格 >= 0，否则上报负数给 Counter 会被 SDK panic：
// RemoteSink.Stats 是 atomic 单调递增的，理论上不会回退，但保险起见做 max(0, diff)。
func (p *AuditRemoteMetricsPump) run(ctx context.Context) {
	defer close(p.done)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			// 最后一次 flush，避免丢最后 interval 内的增量
			p.pumpOnce(ctx)
			return
		case <-ctx.Done():
			p.pumpOnce(ctx)
			return
		case <-t.C:
			p.pumpOnce(ctx)
		}
	}
}

// pumpOnce 读一次快照，上报差值。
func (p *AuditRemoteMetricsPump) pumpOnce(ctx context.Context) {
	p.ticks.Add(1)
	enq, del, drp, fail := p.provider.SnapshotStats()

	if d := enq - p.lastEnq; d > 0 {
		ctrs.get(MetricAuditRemoteEnqueued,
			"Total audit records enqueued to remote sink").Add(ctx, d)
	}
	if d := del - p.lastDel; d > 0 {
		ctrs.get(MetricAuditRemoteDelivered,
			"Total audit records delivered to remote gateway").Add(ctx, d)
	}
	if d := drp - p.lastDrp; d > 0 {
		ctrs.get(MetricAuditRemoteDropped,
			"Total audit records dropped due to backpressure").Add(ctx, d)
	}
	if d := fail - p.lastFail; d > 0 {
		ctrs.get(MetricAuditRemoteFailed,
			"Total audit records failed after retries").Add(ctx, d)
	}

	p.lastEnq, p.lastDel, p.lastDrp, p.lastFail = enq, del, drp, fail
}
