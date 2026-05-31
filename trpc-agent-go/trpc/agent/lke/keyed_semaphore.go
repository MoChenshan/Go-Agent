package lke

import (
	"context"
	"fmt"
	"sync"
)

// keyedSemaphore provides a context-aware, per-key binary semaphore.
//
// It is used to serialize concurrent invocations for the same session (or other logical key),
// without blocking unrelated sessions. Callers can disable it by returning an empty key.
type keyedSemaphore struct {
	m sync.Map // string -> chan struct{}
}

func newKeyedSemaphore() *keyedSemaphore {
	return &keyedSemaphore{}
}

func (s *keyedSemaphore) Acquire(ctx context.Context, key string) (release func(), err error) {
	if key == "" {
		return nil, nil
	}
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}

	chAny, _ := s.m.LoadOrStore(key, make(chan struct{}, 1))
	ch := chAny.(chan struct{})

	select {
	case ch <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-ch
			})
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
