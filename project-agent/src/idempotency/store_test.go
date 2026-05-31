package idempotency

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInMemory_GetOrSet_FirstMiss(t *testing.T) {
	s := NewInMemory()
	calls := 0
	v, hit, err := s.GetOrSet(context.Background(), "k1", time.Minute, func() (any, error) {
		calls++
		return "v1", nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if hit {
		t.Fatal("first call should not be a hit")
	}
	if v != "v1" || calls != 1 {
		t.Fatalf("v=%v calls=%d", v, calls)
	}
}

func TestInMemory_GetOrSet_SecondHit(t *testing.T) {
	s := NewInMemory()
	calls := 0
	for i := 0; i < 3; i++ {
		_, _, _ = s.GetOrSet(context.Background(), "k1", time.Minute, func() (any, error) {
			calls++
			return "v1", nil
		})
	}
	if calls != 1 {
		t.Fatalf("expected fn called once, got %d", calls)
	}
}

func TestInMemory_TTLExpiry(t *testing.T) {
	s := NewInMemory()
	now := time.Now()
	s.now = func() time.Time { return now }

	_, _, _ = s.GetOrSet(context.Background(), "k1", time.Second, func() (any, error) {
		return "v1", nil
	})

	// 超过 TTL
	now = now.Add(2 * time.Second)
	calls := 0
	v, hit, _ := s.GetOrSet(context.Background(), "k1", time.Second, func() (any, error) {
		calls++
		return "v2", nil
	})
	if hit {
		t.Fatal("after TTL expiry it should miss")
	}
	if v != "v2" || calls != 1 {
		t.Fatalf("v=%v calls=%d", v, calls)
	}
}

func TestInMemory_FnErrorNotCached(t *testing.T) {
	s := NewInMemory()
	want := errors.New("transient")
	_, _, err := s.GetOrSet(context.Background(), "k1", time.Minute, func() (any, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected want, got %v", err)
	}
	// 第二次仍然 miss（错误未缓存）
	calls := 0
	_, hit, _ := s.GetOrSet(context.Background(), "k1", time.Minute, func() (any, error) {
		calls++
		return "ok", nil
	})
	if hit || calls != 1 {
		t.Fatalf("error should not be cached; hit=%v calls=%d", hit, calls)
	}
}

func TestInMemory_HasAndForget(t *testing.T) {
	s := NewInMemory()
	_, _, _ = s.GetOrSet(context.Background(), "k1", time.Minute, func() (any, error) { return 1, nil })
	if ok, _ := s.Has(context.Background(), "k1"); !ok {
		t.Fatal("Has should return true")
	}
	_ = s.Forget(context.Background(), "k1")
	if ok, _ := s.Has(context.Background(), "k1"); ok {
		t.Fatal("Has should return false after Forget")
	}
}

func TestNoop_AlwaysExecutes(t *testing.T) {
	s := New(Config{Backend: "noop"})
	calls := 0
	for i := 0; i < 3; i++ {
		_, hit, _ := s.GetOrSet(context.Background(), "k1", time.Minute, func() (any, error) {
			calls++
			return i, nil
		})
		if hit {
			t.Fatal("noop should never hit")
		}
	}
	if calls != 3 {
		t.Fatalf("expected 3 invocations, got %d", calls)
	}
}
