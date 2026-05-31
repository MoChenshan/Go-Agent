package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDo_SuccessFirstTry(t *testing.T) {
	calls := 0
	err := Do(context.Background(), RetryConfig{}, func(ctx context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDo_RetriesUntilSuccess(t *testing.T) {
	calls := 0
	err := Do(context.Background(), RetryConfig{
		MaxAttempts:     5,
		InitialInterval: time.Millisecond,
		MaxInterval:     2 * time.Millisecond,
		Multiplier:      2,
	}, func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDo_ExhaustsAttempts(t *testing.T) {
	want := errors.New("boom")
	calls := 0
	err := Do(context.Background(), RetryConfig{
		MaxAttempts:     3,
		InitialInterval: time.Millisecond,
	}, func(ctx context.Context) error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected boom, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDo_NotRetryable(t *testing.T) {
	want := errors.New("4xx")
	calls := 0
	err := Do(context.Background(), RetryConfig{
		MaxAttempts:     5,
		InitialInterval: time.Millisecond,
		Retryable: func(e error) bool {
			return !errors.Is(e, want)
		},
	}, func(ctx context.Context) error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected want, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (not retryable), got %d", calls)
	}
}

func TestDo_RespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := Do(ctx, RetryConfig{
		MaxAttempts:     5,
		InitialInterval: 10 * time.Millisecond,
	}, func(ctx context.Context) error {
		calls++
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error from cancelled ctx")
	}
	// 已取消时，开始就应直接返回，不该实际调用 fn
	if calls != 0 {
		t.Fatalf("expected 0 calls (ctx cancelled), got %d", calls)
	}
}

func TestDo_NoSleepAfterLastAttempt(t *testing.T) {
	start := time.Now()
	want := errors.New("always")
	_ = Do(context.Background(), RetryConfig{
		MaxAttempts:     2,
		InitialInterval: 200 * time.Millisecond,
	}, func(ctx context.Context) error {
		return want
	})
	elapsed := time.Since(start)
	// 共 2 次：第一次失败 → sleep ~200ms → 第二次失败 → 立刻返回
	// 加 jitter ±20%，预期总耗时在 (160ms, 360ms) 范围内
	if elapsed > 400*time.Millisecond {
		t.Fatalf("elapsed %v too long; should not sleep after last attempt", elapsed)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("elapsed %v too short; should sleep before second attempt", elapsed)
	}
}

func TestDo_OnRetryCallback(t *testing.T) {
	var notifs []int
	_ = Do(context.Background(), RetryConfig{
		MaxAttempts:     3,
		InitialInterval: time.Millisecond,
		OnRetry: func(attempt int, err error, next time.Duration) {
			notifs = append(notifs, attempt)
		},
	}, func(ctx context.Context) error {
		return errors.New("boom")
	})
	// 3 次尝试，重试发生 2 次，OnRetry 调用 2 次（attempt 2 和 3）
	if len(notifs) != 2 || notifs[0] != 2 || notifs[1] != 3 {
		t.Fatalf("unexpected OnRetry calls: %v", notifs)
	}
}
