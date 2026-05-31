package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestChain_OrderRateLimitFirst(t *testing.T) {
	clock := &tickClock{now: time.Unix(1700000000, 0)}
	rl := NewRateLimiter(RateLimitConfig{Capacity: 1, RatePerSecond: 1, Now: clock.Now})
	bh := NewBulkhead(BulkheadConfig{MaxConcurrent: 10})

	chain := Chain{Limiter: rl, Bulk: bh}

	ctx := context.Background()
	if err := chain.Do(ctx, func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("first call should pass, got %v", err)
	}
	// 第二次：rate limit 命中，应该是 ErrRateLimited 而不是 ErrBulkheadFull
	err := chain.Do(ctx, func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited (limit comes first), got %v", err)
	}
}

func TestChain_RetryWrappedInsideBreaker(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	br := NewBreaker(BreakerConfig{
		ConsecutiveFailures: 100, // 不会因连续失败开断
		MinRequests:         1000,
		Now:                 clock.Now,
	})
	calls := atomic.Int32{}
	chain := Chain{
		Breaker: br,
		Retry: &RetryConfig{
			MaxAttempts:     3,
			InitialInterval: time.Millisecond,
		},
	}
	want := errors.New("transient")
	err := chain.Do(context.Background(), func(ctx context.Context) error {
		calls.Add(1)
		if calls.Load() < 3 {
			return want
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil after retry recovery, got %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls (retry inside breaker), got %d", calls.Load())
	}
}

func TestChain_NilLayersAreSkipped(t *testing.T) {
	chain := Chain{}
	called := false
	err := chain.Do(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("empty chain should pass through, err=%v called=%v", err, called)
	}
}
