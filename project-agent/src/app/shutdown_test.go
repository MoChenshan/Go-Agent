package app

import (
	"context"
	"testing"
	"time"
)

func TestShutdown_NilSafe(t *testing.T) {
	// 对 nil 接收者必须 no-op，不能 panic
	var a *App
	a.Shutdown(context.Background())
}

func TestShutdown_EmptyAppNoop(t *testing.T) {
	a := &App{}
	a.Shutdown(context.Background())
}

func TestRemainingTimeout(t *testing.T) {
	// 无 deadline → fallback
	if got := remainingTimeout(context.Background(), 5*time.Second); got != 5*time.Second {
		t.Fatalf("expected fallback 5s, got %v", got)
	}
	// 有较短 deadline → 取剩余
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if got := remainingTimeout(ctx, 10*time.Second); got > 100*time.Millisecond {
		t.Fatalf("expected <=100ms, got %v", got)
	}
	// 较长 deadline → fallback
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel2()
	if got := remainingTimeout(ctx2, 5*time.Second); got != 5*time.Second {
		t.Fatalf("expected fallback 5s when deadline far away, got %v", got)
	}
}
