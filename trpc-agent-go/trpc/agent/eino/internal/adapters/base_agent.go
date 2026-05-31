package adapters

import (
	"context"
	"fmt"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/converters"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/streaming"
	"github.com/cloudwego/eino/compose"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Config represents the configuration for eino adapters.
type Config struct {
	Debug      bool
	ChunkSize  int
	BufferSize int
}

// BaseAgent adapts eino Runnable components (Chain, Graph, Workflow) to implement the trpc-agent-go Agent interface.
// This adapter handles all compiled Runnable components uniformly.
type BaseAgent struct {
	name     string
	runnable compose.Runnable[map[string]any, any]
	config   *Config
}

// NewBaseAgent creates a new BaseAgent.
func NewBaseAgent(runnable compose.Runnable[map[string]any, any], name string, config *Config) *BaseAgent {
	return &BaseAgent{
		name:     name,
		runnable: runnable,
		config:   config,
	}
}

// Run executes the eino Runnable and converts the result to trpc-agent-go event stream.
// This implementation uses eino's native Stream interface for real streaming support.
func (b *BaseAgent) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
	if invocation == nil {
		return nil, fmt.Errorf("invocation cannot be nil")
	}

	// Create the event channel with configured buffer size
	eventChan := make(chan *event.Event, b.config.BufferSize)

	// Start a goroutine to handle the eino execution
	go func() {
		defer close(eventChan)

		// 1. Build input for eino Runnable
		einoInput := converters.BuildEinoInput(invocation.Message)

		// 2. Create stream bridge for unified handling
		streamBridge := streaming.NewStreamBridge(b.name, invocation, eventChan).
			WithDebug(b.config.Debug).
			WithMaxChunkSize(b.config.ChunkSize)

		// 3. Handle execution with proper streaming support
		if err := streamBridge.HandleExecution(ctx, b.runnable, einoInput); err != nil {
			// Error is sent to event stream in streamBridge, but we also log for server-side debugging
			log.Errorf("BaseAgent execution failed for agent '%s': %v", b.name, err)
			return
		}
	}()

	return eventChan, nil
}

// Name returns the agent's name.
func (b *BaseAgent) Name() string {
	return b.name
}

// Tools returns the tools available to this agent.
// For Runnable components, tools are managed internally by eino and are not exposed directly.
func (b *BaseAgent) Tools() []tool.Tool {
	// Runnable components handle tools internally through their composition
	return nil
}

// Info returns basic information about this BaseAgent.
func (b *BaseAgent) Info() agent.Info {
	return agent.Info{
		Name:        b.name,
		Description: "Eino Runnable (Chain/Graph/Workflow) adapted for trpc-agent-go",
	}
}

// SubAgents returns empty slice as BaseAgent doesn't support sub-agents.
func (b *BaseAgent) SubAgents() []agent.Agent {
	return nil
}

// FindSubAgent returns a sub-agent by name.
// BaseAgent does not support sub-agents, so this always returns nil.
func (b *BaseAgent) FindSubAgent(name string) agent.Agent {
	return nil
}
