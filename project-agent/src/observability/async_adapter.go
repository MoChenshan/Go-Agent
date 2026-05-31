// async_adapter.go D19.4 —— AsyncRunner 的 observability 适配器。
//
// # 为什么独立成文件
//
// async 包为了保持"零项目内依赖"的美学，通过 MetricsHook 接口暴露观测点。
// 本文件是这个接口的**默认实现**，把三个核心事件桥接到 OTel metrics：
//
//	OnSubmit(tool, accepted)    → IncAsyncJobSubmitted(tool, "accepted")
//	OnSubmit(tool, rejected)    → IncAsyncJobSubmitted(tool, "rejected")
//	OnSubmit(tool, dedup_hit)   → IncAsyncJobSubmitted(tool, "dedup_hit")
//	OnFinish(tool, status, dt)  → IncAsyncJobFinished + ObserveAsyncJobDuration
//
// # 为什么不把这段逻辑写在 async 包里
//
// async 包要在单测、集成测试、其他组合场景下被复用，绝不能强依赖 OTel SDK。
// 这是"核心能力 / 观测层"分离的典型案例：核心包定义接口，观测包提供实现。
//
// # 使用方式
//
//	cfg.Metrics = observability.NewAsyncMetricsAdapter()
//	runner := async.New(cfg, store, executor)
//
// app 层装配时一行注入即可。
package observability

import (
	"context"
	"time"
)

// AsyncMetricsAdapter 是 async.MetricsHook 的 OTel 实现。
//
// 本类型**无状态**，可安全并发使用。ctx 用 context.Background() 是故意的：
// hook 调用点在 Runner 内部（Submit 路径 / finish 路径），
// 业务 ctx 可能已经被 cancel（比如请求已返回），但指标仍应上报成功。
type AsyncMetricsAdapter struct{}

// NewAsyncMetricsAdapter 构造一个适配器。纯值类型，零配置。
func NewAsyncMetricsAdapter() *AsyncMetricsAdapter {
	return &AsyncMetricsAdapter{}
}

// OnSubmit 实现 async.MetricsHook.OnSubmit。
func (a *AsyncMetricsAdapter) OnSubmit(tool, outcome string) {
	IncAsyncJobSubmitted(context.Background(), tool, outcome)
}

// OnFinish 实现 async.MetricsHook.OnFinish。
//
// 同时上报两条指标：
//   - Counter  —— 终态分布（供告警：timed_out 比例 / cancelled 比例）
//   - Histogram —— 总耗时（供 SLO：p95 / p99）
func (a *AsyncMetricsAdapter) OnFinish(tool, status string, total time.Duration) {
	ctx := context.Background()
	IncAsyncJobFinished(ctx, tool, status)
	ObserveAsyncJobDuration(ctx, tool, status, total.Seconds())
}
