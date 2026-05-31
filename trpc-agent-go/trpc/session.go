// Package trpc is imported to modify the opensource trpc-group/trpc-agent-go
// as a side effect to automatically serve for the internal version.
package trpc

import (
	"trpc.group/trpc-go/trpc-agent-go/agent"
)

func init() {
	// Use cloneContextWithSpan instead of trpc.CloneContextWithTimeout
	// to preserve OpenTelemetry span information across goroutines.
	// This fixes the issue where LLM request spans were not correctly
	// associated with the invoke_agent span.
	agent.SetGoroutineContextCloner(cloneContextWithSpan)
}
