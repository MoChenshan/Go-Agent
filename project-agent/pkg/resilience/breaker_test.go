package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock 用于测试中精确控制时间。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestBreaker_ConsecutiveFailuresOpens(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	b := NewBreaker(BreakerConfig{
		Name:                "test",
		ConsecutiveFailures: 3,
		MinRequests:         100,
		OpenTimeout:         time.Second,
		Now:                 clock.Now,
	})

	want := errors.New("boom")
	for i := 0; i < 3; i++ {
		err := b.Do(context.Background(), func(ctx context.Context) error { return want })
		if !errors.Is(err, want) {
			t.Fatalf("attempt %d: want %v, got %v", i, want, err)
		}
	}
	if got := b.State(); got != StateOpen {
		t.Fatalf("expected Open after 3 consecutive failures, got %s", got)
	}

	// Open 状态拒绝请求
	err := b.Do(context.Background(), func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestBreaker_HalfOpenSuccessClosesAgain(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	b := NewBreaker(BreakerConfig{
		ConsecutiveFailures: 2,
		MinRequests:         100,
		OpenTimeout:         500 * time.Millisecond,
		HalfOpenMaxCalls:    1,
		Now:                 clock.Now,
	})

	// 触发 Open
	for i := 0; i < 2; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error { return errors.New("x") })
	}
	if b.State() != StateOpen {
		t.Fatal("expected Open")
	}

	// 时间未到 → 仍 Open
	if err := b.Do(context.Background(), func(ctx context.Context) error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected still open, got %v", err)
	}

	// 推进时间，进入 HalfOpen 探测
	clock.Advance(time.Second)
	err := b.Do(context.Background(), func(ctx context.Context) error { return nil })
	if err != nil {
		t.Fatalf("half-open probe should pass, got %v", err)
	}
	if b.State() != StateClosed {
		t.Fatalf("expected Closed after successful probe, got %s", b.State())
	}
}

func TestBreaker_HalfOpenFailureGoesBackToOpen(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	b := NewBreaker(BreakerConfig{
		ConsecutiveFailures: 2,
		MinRequests:         100,
		OpenTimeout:         500 * time.Millisecond,
		HalfOpenMaxCalls:    1,
		Now:                 clock.Now,
	})

	want := errors.New("boom")
	_ = b.Do(context.Background(), func(ctx context.Context) error { return want })
	_ = b.Do(context.Background(), func(ctx context.Context) error { return want })

	clock.Advance(time.Second)
	err := b.Do(context.Background(), func(ctx context.Context) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("expected boom, got %v", err)
	}
	if b.State() != StateOpen {
		t.Fatalf("expected back to Open, got %s", b.State())
	}
}

func TestBreaker_HalfOpenLimitsConcurrency(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	b := NewBreaker(BreakerConfig{
		ConsecutiveFailures: 2,
		MinRequests:         100,
		OpenTimeout:         100 * time.Millisecond,
		HalfOpenMaxCalls:    1,
		Now:                 clock.Now,
	})

	// 触发 Open
	_ = b.Do(context.Background(), func(ctx context.Context) error { return errors.New("x") })
	_ = b.Do(context.Background(), func(ctx context.Context) error { return errors.New("x") })

	clock.Advance(200 * time.Millisecond)

	var (
		wg          sync.WaitGroup
		ranProbe    atomic.Int32
		rejected    atomic.Int32
		probeBlock  = make(chan struct{})
		probeStarted = make(chan struct{}, 1)
	)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := b.Do(context.Background(), func(ctx context.Context) error {
				ranProbe.Add(1)
				probeStarted <- struct{}{}
				<-probeBlock
				return nil
			})
			if errors.Is(err, ErrCircuitOpen) {
				rejected.Add(1)
			}
		}()
	}
	// 等第一个探测真的进入 fn
	<-probeStarted
	// 短暂等待后续 4 个 goroutine 都到达 allow（应被立即拒绝）
	deadline := time.After(200 * time.Millisecond)
loop:
	for {
		if rejected.Load() == 4 {
			break
		}
		select {
		case <-deadline:
			break loop
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(probeBlock)
	wg.Wait()

	if got := ranProbe.Load(); got != 1 {
		t.Fatalf("expected exactly 1 probe call, got %d", got)
	}
	if got := rejected.Load(); got != 4 {
		t.Fatalf("expected 4 rejections, got %d", got)
	}
}

func TestBreaker_StateChangeCallback(t *testing.T) {
	var changes []string
	var mu sync.Mutex

	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	b := NewBreaker(BreakerConfig{
		Name:                "T",
		ConsecutiveFailures: 1,
		MinRequests:         100,
		OpenTimeout:         100 * time.Millisecond,
		HalfOpenMaxCalls:    1,
		OnStateChange: func(name string, from, to State) {
			mu.Lock()
			changes = append(changes, name+":"+from.String()+"->"+to.String())
			mu.Unlock()
		},
		Now: clock.Now,
	})
	_ = b.Do(context.Background(), func(ctx context.Context) error { return errors.New("x") })
	clock.Advance(200 * time.Millisecond)
	_ = b.Do(context.Background(), func(ctx context.Context) error { return nil })

	// 异步回调，等一下
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(changes) < 2 {
		t.Fatalf("expected >=2 state changes, got %v", changes)
	}
}
