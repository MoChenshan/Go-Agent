package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBulkhead_AllowsUpToCapacity(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{MaxConcurrent: 2})
	var wg sync.WaitGroup
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	block := make(chan struct{})

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bh.Do(context.Background(), func(ctx context.Context) error {
				cur := inFlight.Add(1)
				for {
					old := maxInFlight.Load()
					if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
						break
					}
				}
				<-block
				inFlight.Add(-1)
				return nil
			})
		}()
	}
	// 等到两个都进入
	for inFlight.Load() != 2 {
		time.Sleep(time.Millisecond)
	}
	if got := bh.InFlight(); got != 2 {
		t.Fatalf("InFlight expected 2, got %d", got)
	}
	close(block)
	wg.Wait()

	if maxInFlight.Load() != 2 {
		t.Fatalf("max in-flight expected 2, got %d", maxInFlight.Load())
	}
}

func TestBulkhead_FailFastWhenFull(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{MaxConcurrent: 1})
	block := make(chan struct{})
	started := make(chan struct{})

	go func() {
		_ = bh.Do(context.Background(), func(ctx context.Context) error {
			close(started)
			<-block
			return nil
		})
	}()
	<-started

	err := bh.Do(context.Background(), func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrBulkheadFull) {
		t.Fatalf("expected ErrBulkheadFull, got %v", err)
	}
	close(block)
}

func TestBulkhead_AcquireTimeout(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{MaxConcurrent: 1, AcquireTimeout: 50 * time.Millisecond})
	block := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_ = bh.Do(context.Background(), func(ctx context.Context) error {
			close(started)
			<-block
			return nil
		})
	}()
	<-started

	start := time.Now()
	err := bh.Do(context.Background(), func(ctx context.Context) error { return nil })
	elapsed := time.Since(start)

	if !errors.Is(err, ErrBulkheadFull) {
		t.Fatalf("expected ErrBulkheadFull, got %v", err)
	}
	if elapsed < 40*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Fatalf("elapsed %v not within expected window", elapsed)
	}
	close(block)
}

func TestBulkhead_ContextCancellationReturns(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{MaxConcurrent: 1, AcquireTimeout: time.Second})
	block := make(chan struct{})
	defer close(block)
	started := make(chan struct{})

	go func() {
		_ = bh.Do(context.Background(), func(ctx context.Context) error {
			close(started)
			<-block
			return nil
		})
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := bh.Do(ctx, func(ctx context.Context) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected ctx.Canceled, got %v", err)
	}
}
