package lke

import (
	"context"
	"testing"
	"time"
)

func TestKeyedSemaphoreReleaseIsIdempotent(t *testing.T) {
	sem := newKeyedSemaphore()

	release, err := sem.Acquire(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if release == nil {
		t.Fatal("Acquire() returned nil release")
	}

	release()

	done := make(chan struct{})
	go func() {
		release()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("second release blocked")
	}

	releaseAgain, err := sem.Acquire(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
	if releaseAgain == nil {
		t.Fatal("Acquire() after release returned nil release")
	}
	releaseAgain()
}
