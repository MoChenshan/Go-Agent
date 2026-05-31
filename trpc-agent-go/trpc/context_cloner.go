// Package trpc provides internal enhancements for trpc-agent-go.
package trpc

import (
	"context"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"go.opentelemetry.io/otel/trace"
)

// cloneContextWithSpan clones the context using tRPC's CloneContextWithTimeout
// while preserving OpenTelemetry span information.
//
// This ensures that when goroutines are spawned with cloned contexts,
// the OpenTelemetry tracing hierarchy is maintained correctly.
func cloneContextWithSpan(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}

	// First, clone the context using tRPC's CloneContextWithTimeout
	// This creates a new tRPC message context with isolated metadata
	clonedCtx := trpc.CloneContextWithTimeout(ctx)

	// Extract the OpenTelemetry span from the original context
	span := trace.SpanFromContext(ctx)

	// If a valid span exists, inject it into the cloned context
	// This ensures the span hierarchy is preserved across goroutines
	if span != nil && span.SpanContext().IsValid() {
		clonedCtx = trace.ContextWithSpan(clonedCtx, span)
	}

	return clonedCtx
}
