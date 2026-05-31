// async_adapter_test.go D19.4 —— Adapter + async 集成行为。
//
// 这个测试和 async.MetricsHook 接口做 compile-time 契合：
// 用 observability.NewAsyncMetricsAdapter() 直接当 async.Config.Metrics 用。
// 若接口签名漂移，这个测试编不过——充当 SSOT 守卫。
package observability_test

import (
	"context"
	"testing"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/async"
	"git.woa.com/trpc-go/gameops-agent/src/observability"
)

// TestAsyncMetricsAdapter_ImplementsHook 保证 Adapter 满足 async.MetricsHook。
//
// 编译通过本身就是测试；运行时确认 Submit→Finish 链路能打点不 panic。
func TestAsyncMetricsAdapter_ImplementsHook(t *testing.T) {
	var _ async.MetricsHook = (*observability.AsyncMetricsAdapter)(nil)

	exec := async.ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return "ok", nil
	})
	r := async.New(async.Config{
		MaxConcurrentJobs: 1,
		MaxQueuedJobs:     4,
		DefaultTimeout:    time.Second,
		JanitorInterval:   time.Hour,
		Metrics:           observability.NewAsyncMetricsAdapter(),
	}, async.NewMemStore(), exec)
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	id, err := r.Submit(context.Background(), "probe_tool", nil, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := r.Wait(wctx, id); err != nil {
		t.Fatal(err)
	}
	// 不 panic 即过；OTel SDK 行为由其他更底层的测试（metrics_test）覆盖
}
