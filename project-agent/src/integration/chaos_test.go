// Package integration — 故障注入（chaos）测试
//
// 验证：
//   - 突发慢响应 → Bulkhead 快速 fail，避免 goroutine 堆积
//   - 持续错误 → Breaker 打开，快速失败，并自动半开探测恢复
//   - 突发流量 → RateLimit 起作用，符合容量预期
//   - 链路组合 RateLimit → Bulkhead → Breaker → Retry，错误优先级正确
//
// 这些场景在生产环境下出现频率高、定位难，单元测试又难以覆盖跨多个原语的交互，
// 因此放在 integration 包专门验证。
package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.woa.com/trpc-go/gameops-agent/pkg/resilience"
)

// fakeUpstream 模拟一个外部依赖：可被设置为"全部失败/全部成功/慢响应"。
type fakeUpstream struct {
	mu          sync.Mutex
	failUntil   time.Time      // 在此时间之前的请求一律失败
	delay       time.Duration  // 每次请求强制延迟
	calls       atomic.Int64
	concurrency atomic.Int64
	maxSeen     atomic.Int64
}

func (u *fakeUpstream) failFor(d time.Duration) {
	u.mu.Lock()
	u.failUntil = time.Now().Add(d)
	u.mu.Unlock()
}

func (u *fakeUpstream) heal() {
	u.mu.Lock()
	u.failUntil = time.Time{}
	u.mu.Unlock()
}

func (u *fakeUpstream) call(ctx context.Context) error {
	u.calls.Add(1)
	cur := u.concurrency.Add(1)
	defer u.concurrency.Add(-1)
	for {
		old := u.maxSeen.Load()
		if cur <= old || u.maxSeen.CompareAndSwap(old, cur) {
			break
		}
	}
	if u.delay > 0 {
		t := time.NewTimer(u.delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	u.mu.Lock()
	failing := time.Now().Before(u.failUntil)
	u.mu.Unlock()
	if failing {
		return errors.New("upstream error")
	}
	return nil
}

// TestChaos_BulkheadProtectsFromSlowUpstream 验证 bulkhead 限制并发。
//
// 场景：上游每次响应耗时 200ms；20 个并发请求在 bulkhead 容量=5 下，
//
//	- 最多 5 个并发真正打到上游
//	- 其余 15 个在 bulkhead 入口被快速失败（fail-fast），不会"假死"
func TestChaos_BulkheadProtectsFromSlowUpstream(t *testing.T) {
	up := &fakeUpstream{delay: 200 * time.Millisecond}
	bh := resilience.NewBulkhead(resilience.BulkheadConfig{
		Name:           "upstream",
		MaxConcurrent:  5,
		AcquireTimeout: 0, // fail-fast
	})

	const N = 20
	var (
		wg       sync.WaitGroup
		ok       atomic.Int64
		rejected atomic.Int64
	)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := bh.Do(context.Background(), up.call)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, resilience.ErrBulkheadFull):
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := up.maxSeen.Load(); got > 5 {
		t.Fatalf("bulkhead breached: max concurrency=%d", got)
	}
	if rejected.Load() == 0 {
		t.Fatal("expected some requests rejected by bulkhead")
	}
	t.Logf("ok=%d rejected=%d max_concurrency=%d", ok.Load(), rejected.Load(), up.maxSeen.Load())
}

// TestChaos_BreakerOpensThenRecovers 验证熔断器在持续错误下打开，
// 并在 OpenTimeout 后通过半开探测自动恢复。
func TestChaos_BreakerOpensThenRecovers(t *testing.T) {
	up := &fakeUpstream{}
	br := resilience.NewBreaker(resilience.BreakerConfig{
		Name:                "chaos",
		ConsecutiveFailures: 3,
		MinRequests:         100, // 不靠率统计；只看连续失败
		OpenTimeout:         200 * time.Millisecond,
		HalfOpenMaxCalls:    1,
	})

	// 阶段 1：持续 1s 失败 → 应触发熔断
	up.failFor(time.Second)
	for i := 0; i < 5; i++ {
		_ = br.Do(context.Background(), up.call)
	}
	if br.State() != resilience.StateOpen {
		t.Fatalf("expected breaker Open, got %s", br.State())
	}

	// 阶段 2：Open 期内请求被拒
	if err := br.Do(context.Background(), up.call); !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen during open window, got %v", err)
	}

	// 阶段 3：上游恢复，等待 OpenTimeout 后允许半开探测
	up.heal()
	time.Sleep(250 * time.Millisecond)
	if err := br.Do(context.Background(), up.call); err != nil {
		t.Fatalf("half-open probe should succeed, got %v", err)
	}
	if br.State() != resilience.StateClosed {
		t.Fatalf("expected breaker Closed after probe, got %s", br.State())
	}
}

// TestChaos_FullChainErrorPriority 验证 Chain 顺序：限流先于熔断先于重试。
//
// 场景：rate limit=1QPS（capacity=1），瞬间打 5 个请求，
// 第 2~5 个应得到 ErrRateLimited，而不是被 retry 反复重试。
func TestChaos_FullChainErrorPriority(t *testing.T) {
	up := &fakeUpstream{}
	chain := resilience.Chain{
		Limiter: resilience.NewRateLimiter(resilience.RateLimitConfig{
			Capacity:      1,
			RatePerSecond: 1,
		}),
		Bulk: resilience.NewBulkhead(resilience.BulkheadConfig{MaxConcurrent: 10}),
		Breaker: resilience.NewBreaker(resilience.BreakerConfig{
			ConsecutiveFailures: 100,
			MinRequests:         1000,
		}),
		Retry: &resilience.RetryConfig{MaxAttempts: 3, InitialInterval: time.Millisecond},
	}

	var rateLimited atomic.Int64
	var ok atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := chain.Do(context.Background(), up.call)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, resilience.ErrRateLimited):
				rateLimited.Add(1)
			}
		}()
	}
	wg.Wait()

	if ok.Load() != 1 {
		t.Fatalf("expected exactly 1 success, got %d", ok.Load())
	}
	if rateLimited.Load() != 4 {
		t.Fatalf("expected 4 rate-limited rejections, got %d", rateLimited.Load())
	}
	if up.calls.Load() != 1 {
		t.Fatalf("upstream should be called only once (others rejected before reach), got %d", up.calls.Load())
	}
}

// TestChaos_RetryDoesNotAmplifyOnNonRetryable 验证不可重试错误不会被重试放大。
func TestChaos_RetryDoesNotAmplifyOnNonRetryable(t *testing.T) {
	want := errors.New("4xx-bad-request")
	cfg := resilience.RetryConfig{
		MaxAttempts:     5,
		InitialInterval: time.Millisecond,
		Retryable:       func(e error) bool { return !errors.Is(e, want) },
	}
	calls := atomic.Int32{}
	err := resilience.Do(context.Background(), cfg, func(ctx context.Context) error {
		calls.Add(1)
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected want, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("non-retryable error should not be retried, got %d calls", calls.Load())
	}
}
