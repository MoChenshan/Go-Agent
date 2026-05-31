package observability

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// collectFindSum 从 ResourceMetrics 里按 name 找出 Sum[int64] 的总量（合并所有数据点）。
// 找不到返回 (-1, false)。
func collectFindSum(rm *metricdata.ResourceMetrics, name string) (int64, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				return -1, false
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total, true
		}
	}
	return -1, false
}

// collectFindHistogram 找出 Histogram[float64] 并返回总 count + 总 sum。
func collectFindHistogram(rm *metricdata.ResourceMetrics, name string) (uint64, float64, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hg, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				return 0, 0, false
			}
			var cnt uint64
			var sum float64
			for _, dp := range hg.DataPoints {
				cnt += dp.Count
				sum += dp.Sum
			}
			return cnt, sum, true
		}
	}
	return 0, 0, false
}

func TestIncJudgeCall_Basic(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	IncJudgeCall(ctx, "ok")
	IncJudgeCall(ctx, "ok")
	IncJudgeCall(ctx, "error")
	IncJudgeCall(ctx, "") // 默认补 ok

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	got, ok := collectFindSum(&rm, MetricJudgeCalls)
	if !ok {
		t.Fatalf("metric %s 未找到", MetricJudgeCalls)
	}
	if got != 4 {
		t.Fatalf("Judge calls 期望 4，实际 %d", got)
	}
}

func TestObserveJudgeLatency_Histogram(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	for _, s := range []float64{0.03, 0.08, 0.2, 1.5, 8} {
		ObserveJudgeLatency(ctx, s)
	}
	// 负值应被忽略
	ObserveJudgeLatency(ctx, -1)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	cnt, sum, ok := collectFindHistogram(&rm, MetricJudgeLatency)
	if !ok {
		t.Fatalf("histogram %s 未找到", MetricJudgeLatency)
	}
	if cnt != 5 {
		t.Fatalf("期望 5 次 record，实际 %d", cnt)
	}
	// 浮点累加允许一点点误差
	if sum < 9.8 || sum > 9.82 {
		t.Fatalf("期望 sum ~= 9.81, 实际 %v", sum)
	}
}

func TestIncRuleReload(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	IncRuleReload(ctx, "input_guard", "ok")
	IncRuleReload(ctx, "output_guard", "parse_error")
	IncRuleReload(ctx, "", "") // 空值回退

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	got, ok := collectFindSum(&rm, MetricRuleReload)
	if !ok {
		t.Fatal("rule reload 指标未找到")
	}
	if got != 3 {
		t.Fatalf("期望 3，实际 %d", got)
	}
}

// fakeStatsProvider 模拟 RemoteSink.SnapshotStats。
type fakeStatsProvider struct {
	enq, del, drp, fail atomic.Int64
}

func (f *fakeStatsProvider) SnapshotStats() (int64, int64, int64, int64) {
	return f.enq.Load(), f.del.Load(), f.drp.Load(), f.fail.Load()
}

func TestAuditRemoteMetricsPump_DiffsToCounter(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	provider := &fakeStatsProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 50ms 间隔，让 tick 频繁便于断言
	pump := StartAuditRemoteMetricsPump(ctx, provider, 50*time.Millisecond)
	t.Cleanup(func() { pump.Stop() })

	// 初值 = 0：tick 时不应产生任何 Add（因为 diff == 0）
	// 第一次 bump
	provider.enq.Store(10)
	provider.del.Store(7)
	provider.drp.Store(2)
	provider.fail.Store(1)
	// 等至少 2 次 tick
	waitPumpTicks(t, pump, 2, time.Second)

	// 第二次 bump（再加）
	provider.enq.Store(15)
	provider.del.Store(12)
	provider.drp.Store(3)
	provider.fail.Store(1) // 不变
	waitPumpTicks(t, pump, 4, time.Second)

	pump.Stop()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}

	check := func(name string, want int64) {
		got, ok := collectFindSum(&rm, name)
		if !ok {
			t.Fatalf("%s 指标未找到", name)
		}
		if got != want {
			t.Fatalf("%s 期望 %d，实际 %d", name, want, got)
		}
	}
	check(MetricAuditRemoteEnqueued, 15) // 10 + 5
	check(MetricAuditRemoteDelivered, 12)
	check(MetricAuditRemoteDropped, 3)
	check(MetricAuditRemoteFailed, 1)
}

// TestAuditRemoteMetricsPump_NilProvider nil provider 应安全 no-op。
func TestAuditRemoteMetricsPump_NilProvider(t *testing.T) {
	pump := StartAuditRemoteMetricsPump(context.Background(), nil, 10*time.Millisecond)
	// 不应 panic
	pump.Stop()
	pump.Stop() // 幂等
}

// TestAuditRemoteMetricsPump_StopFlush Stop 时应补一次 flush。
func TestAuditRemoteMetricsPump_StopFlush(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	provider := &fakeStatsProvider{}
	// interval 设得很大，确保 Stop 前正常 tick 不会发生
	pump := StartAuditRemoteMetricsPump(context.Background(), provider, time.Hour)

	// 在 Stop 前修改值；Stop 应补上这次增量
	provider.enq.Store(42)
	pump.Stop()

	var rm metricdata.ResourceMetrics
	_ = reader.Collect(context.Background(), &rm)
	got, ok := collectFindSum(&rm, MetricAuditRemoteEnqueued)
	if !ok {
		t.Fatal("enqueued 指标未找到")
	}
	if got != 42 {
		t.Fatalf("Stop 应补 flush，期望 42，实际 %d", got)
	}
}

// TestAuditRemoteMetricsPump_CtxCancel ctx 取消时应退出。
func TestAuditRemoteMetricsPump_CtxCancel(t *testing.T) {
	_, restore := SetMeterForTest()
	t.Cleanup(restore)

	provider := &fakeStatsProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	pump := StartAuditRemoteMetricsPump(ctx, provider, time.Hour)
	cancel()
	// Stop 应立即返回（ctx 已退出后台 goroutine）
	done := make(chan struct{})
	go func() { pump.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Stop 仍阻塞")
	}
}

// waitPumpTicks 轮询等 pump 累计 tick >= want。
func waitPumpTicks(t *testing.T, p *AuditRemoteMetricsPump, want int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.Ticks() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待 pump ticks >= %d 超时（实际 %d）", want, p.Ticks())
}
