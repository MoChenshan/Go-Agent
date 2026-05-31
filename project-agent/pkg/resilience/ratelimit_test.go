package resilience

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type tickClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *tickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *tickClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestRateLimiter_BurstThenRefill(t *testing.T) {
	clock := &tickClock{now: time.Unix(1700000000, 0)}
	r := NewRateLimiter(RateLimitConfig{
		Capacity:      3,
		RatePerSecond: 1,
		Now:           clock.Now,
	})
	// 桶满 → 连续 3 次都通过
	for i := 0; i < 3; i++ {
		if !r.Allow() {
			t.Fatalf("burst attempt %d should pass", i)
		}
	}
	// 第 4 次被拒
	if r.Allow() {
		t.Fatal("4th attempt should be denied")
	}
	// 经过 2 秒补 2 个 token
	clock.Advance(2 * time.Second)
	for i := 0; i < 2; i++ {
		if !r.Allow() {
			t.Fatalf("after refill attempt %d should pass", i)
		}
	}
	if r.Allow() {
		t.Fatal("should be empty again")
	}
}

func TestRateLimiter_DoFailFast(t *testing.T) {
	clock := &tickClock{now: time.Unix(1700000000, 0)}
	r := NewRateLimiter(RateLimitConfig{Capacity: 1, RatePerSecond: 1, Now: clock.Now})
	// 第 1 次成功
	if err := r.Do(context.Background(), func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("first should pass, got %v", err)
	}
	// 第 2 次（无补充）→ ErrRateLimited
	err := r.Do(context.Background(), func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestKeyedRateLimiter_PerKeyIndependent(t *testing.T) {
	clock := &tickClock{now: time.Unix(1700000000, 0)}
	k := NewKeyedRateLimiter(RateLimitConfig{Capacity: 1, RatePerSecond: 1, Now: clock.Now})

	if !k.Allow("user-a") {
		t.Fatal("user-a first call should pass")
	}
	if k.Allow("user-a") {
		t.Fatal("user-a second call should be denied")
	}
	// user-b 与 user-a 桶独立
	if !k.Allow("user-b") {
		t.Fatal("user-b first call should pass independently")
	}
}

func TestRateLimiter_WaitRespectsCtx(t *testing.T) {
	r := NewRateLimiter(RateLimitConfig{Capacity: 1, RatePerSecond: 0.1}) // 10s/token
	_ = r.Allow()                                                          // 把桶用空

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := r.Wait(ctx)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Wait should respect ctx deadline, elapsed=%v", elapsed)
	}
}
