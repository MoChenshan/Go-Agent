// ready_waiter_glue.go D19.8 —— FastPollReadyWaiter 的 observability 胶水。
//
// # 本文件存在的理由（架构决策）
//
// 详细背景见 src/observability/ready_waiter_adapter.go 顶部注释。
// 简要版：
//
//	bcs_tools  →（正向 import）→ observability   ✅（D19.5 起）
//	observability →（反向 import）→ bcs_tools    ❌（会形成循环 import，Go 拒绝编译）
//
// 解决方案：bcstools.FastPollMetricsHook 接口的唯一 OTel 实现放在 app 包里。
// app 本来就是"装配胶水层"，双向依赖业务包与观测包，承担这里的"翻译责任"最合适。
//
// # 实现的极简原则
//
// 本文件只做一件事：把 bcstools.FastPollMetricsHook 的事件翻译为 observability
// 的三个打点函数调用。不做状态、不做聚合、不做缓存。
//
// 这样保证：
//   - 指标语义由 observability 掌握（对齐 Prometheus 风格）
//   - 调用时机由 bcs_tools 掌握（最贴近真实业务事件）
//   - app 层只负责"1:1 翻译"，几乎不可能出 bug
package app

import (
	"context"

	"git.woa.com/trpc-go/gameops-agent/src/observability"
	"git.woa.com/trpc-go/gameops-agent/src/tools/bcs_tools"
)

// fastPollMetricsGlue 实现 bcstools.FastPollMetricsHook。
//
// 无状态类型，通过值传递即可并发安全使用。
type fastPollMetricsGlue struct{}

// newFastPollMetricsGlue 返回一个可直接注入 bcstools.NewFastPollReadyWaiter 的 hook。
func newFastPollMetricsGlue() bcstools.FastPollMetricsHook {
	return fastPollMetricsGlue{}
}

// OnWaitFinished 把一次 Wait 生命周期翻译为 3 条指标：
//
//  1. IncFastPollFinished —— 终态 Counter（供告警）
//  2. IncFastPollProbeBucket —— 仅在 ready 时按首探/fast_stage/steady 分桶（供"FastPoll 的增量价值"观测）
//  3. ObserveFastPollProbesPer —— probe 次数直方图（监控 BCS 查询压力）
//
// 第 4 个"耗时 Histogram"复用 D19.5 的 MetricPodReadyWaitDuration —— 这样 D19.5 已部署
// 的 Grafana 面板自动覆盖 D19.8 新实现的数据，运维无需改 dashboard。
func (fastPollMetricsGlue) OnWaitFinished(mode string, stats bcstools.FastPollStats) {
	ctx := context.Background()
	observability.IncFastPollFinished(ctx, mode, stats.Reason)
	if stats.Reason == "ready" && stats.ProbeIndexWhenReady > 0 {
		observability.IncFastPollProbeBucket(ctx, mode, stats.ProbeIndexWhenReady)
	}
	observability.ObserveFastPollProbesPer(ctx, mode, stats.Reason, stats.TotalProbes)

	// 复用 D19.5 指标，让 D19.5 已部署的 dashboard 自动覆盖 D19.8 新实现的数据
	if stats.Elapsed > 0 {
		observability.ObservePodReadyWaitDuration(ctx, mode, stats.Reason, stats.Elapsed.Seconds())
		observability.IncPodReadyWait(ctx, mode, stats.Reason)
	}
}
