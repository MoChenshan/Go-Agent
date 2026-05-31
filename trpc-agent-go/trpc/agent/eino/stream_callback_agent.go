package eino

import (
	"context"

	icallbacks "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/callbacks"
	"github.com/cloudwego/eino/callbacks"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// StreamCallbackAgent wraps a tRPC Agent to support Eino streaming icallbacks.
// This provides a seamless way to integrate complex Eino callbacks
// with tRPC native agents.
type StreamCallbackAgent struct {
	agent           agent.Agent
	streamingBridge *icallbacks.StreamingCallbackBridge
}

// NewStreamCallbackAgent creates a stream callback agent that supports Eino streaming icallbacks.
// This is the recommended way to use complex Eino callbacks with tRPC native agents.
//
// Example:
//
//	streamCallbackAgent := teino.NewStreamCallbackAgent(
//		originalAgent,
//		teino.WithEinoCallbacks(einoHandler),
//	)
//
//	// Use with runner
//	runner := runner.NewRunner("app", streamCallbackAgent)
func NewStreamCallbackAgent(baseAgent agent.Agent, options ...StreamingOption) *StreamCallbackAgent {
	config := &StreamingConfig{}
	for _, opt := range options {
		opt(config)
	}

	var streamingBridge *icallbacks.StreamingCallbackBridge
	if config.EinoHandler != nil {
		callbackConfig := &icallbacks.CallbackConfig{
			NodeFilter: config.NodeFilter,
		}
		streamingBridge = icallbacks.NewStreamingCallbackBridge(config.EinoHandler, callbackConfig)
	}

	return &StreamCallbackAgent{
		agent:           baseAgent,
		streamingBridge: streamingBridge,
	}
}

// StreamingConfig contains configuration for streaming agent.
type StreamingConfig struct {
	EinoHandler callbacks.Handler
	NodeFilter  map[string]bool
}

// StreamingOption configures the streaming agent.
type StreamingOption func(*StreamingConfig)

// WithEinoCallbacks sets the Eino streaming callback handler.
// This enables complex streaming callback logic.
func WithEinoCallbacks(handler callbacks.Handler) StreamingOption {
	return func(config *StreamingConfig) {
		config.EinoHandler = handler
	}
}

// WithNodeFilter sets which Eino nodes to handle in streaming icallbacks.
func WithNodeFilter(nodes ...string) StreamingOption {
	return func(config *StreamingConfig) {
		config.NodeFilter = make(map[string]bool)
		for _, node := range nodes {
			config.NodeFilter[node] = true
		}
	}
}

// Run implements the agent.Agent interface.
// If streaming callbacks are configured, it intercepts the event stream.
func (s *StreamCallbackAgent) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
	// Run the underlying agent
	originalStream, err := s.agent.Run(ctx, invocation)
	if err != nil {
		return nil, err
	}

	// If no streaming bridge is configured, return the original stream
	if s.streamingBridge == nil {
		return originalStream, nil
	}

	// Intercept the stream with Eino streaming callbacks
	return s.streamingBridge.InterceptEventStream(ctx, originalStream), nil
}

// Tools implements the agent.Agent interface.
func (s *StreamCallbackAgent) Tools() []tool.Tool {
	return s.agent.Tools()
}

// Info implements the agent.Agent interface.
func (s *StreamCallbackAgent) Info() agent.Info {
	return s.agent.Info()
}

// SubAgents implements the agent.Agent interface.
func (s *StreamCallbackAgent) SubAgents() []agent.Agent {
	return s.agent.SubAgents()
}

// FindSubAgent implements the agent.Agent interface.
func (s *StreamCallbackAgent) FindSubAgent(name string) agent.Agent {
	return s.agent.FindSubAgent(name)
}
