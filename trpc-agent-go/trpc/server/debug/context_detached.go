package debug

import (
	"context"
	"time"
)

// detachedContext wraps a parent context but disables cancellation and
// deadlines while preserving all values. This allows us to keep trace and
// logging metadata from the incoming request context without being affected
// by HTTP‑level timeouts or client disconnects.
type detachedContext struct {
	context.Context
}

func (detachedContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (detachedContext) Done() <-chan struct{} {
	return nil
}

func (detachedContext) Err() error {
	return nil
}

func newDetachedContext(ctx context.Context) context.Context {
	return detachedContext{Context: ctx}
}
