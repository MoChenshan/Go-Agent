// testing_support.go 仅供 observability 包自身及配套业务单测使用的辅助 API。
//
// 为什么独立一个文件而不是 _test.go：
//   - 其他包（如 src/audit 的将来测试）也可能想在测试里注入 ManualReader，
//     如果放在 _test.go，外部包无法 import。
//   - 仍以 "testing_" 前缀命名，运行期代码扫描时易于识别。
//
// 生产代码不要调用本文件的任何函数；除非在测试里明确知道自己在做什么。

package observability

import (
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// SetMeterForTest 把当前全局 Provider 的 meter 替换为一个 SDK MeterProvider
// 产出的 meter，返回 reader 与 restore 闭包（t.Cleanup(restore) 即可）。
//
// 典型用法（_test.go 内）：
//
//	reader, restore := observability.SetMeterForTest()
//	t.Cleanup(restore)
//	observability.IncJudgeCall(context.Background(), "ok")
//	var rm metricdata.ResourceMetrics
//	_ = reader.Collect(context.Background(), &rm)
//	// 断言 rm.ScopeMetrics[0].Metrics[0].Data ...
func SetMeterForTest() (*sdkmetric.ManualReader, func()) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m := mp.Meter(InstrumentationName, metric.WithInstrumentationVersion(InstrumentationVersion))

	globalMu.Lock()
	prev := globalPrv
	globalPrv = &Provider{
		tracer:   prev.tracer,
		meter:    m,
		shutdown: prev.shutdown,
		enabled:  prev.enabled,
	}
	globalMu.Unlock()

	// 重置 counter/histogram 缓存 — 否则会使用 prev.meter 创建过的 counter，
	// 那些 counter 不会被新的 reader 观察到，断言必失败。
	ResetMetricsForTest()
	ResetHistogramsForTest()

	restore := func() {
		globalMu.Lock()
		globalPrv = prev
		globalMu.Unlock()
		ResetMetricsForTest()
		ResetHistogramsForTest()
	}
	return reader, restore
}
