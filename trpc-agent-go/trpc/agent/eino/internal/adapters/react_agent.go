package adapters

import (
	"context"
	"fmt"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/converters"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/streaming"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ReActAgent adapts an eino ReAct Agent to implement the trpc-agent-go Agent interface.
// ReAct (Reasoning + Acting) agents have special handling for their unique execution pattern.
type ReActAgent struct {
	name       string
	reactAgent *react.Agent
	config     *Config
}

// NewReActAgent creates a new ReActAgent.
func NewReActAgent(reactAgent *react.Agent, name string, config *Config) *ReActAgent {
	return &ReActAgent{
		name:       name,
		reactAgent: reactAgent,
		config:     config,
	}
}

// Run executes the eino ReAct Agent and converts the result to trpc-agent-go event stream.
func (r *ReActAgent) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
	if invocation == nil {
		return nil, fmt.Errorf("invocation cannot be nil")
	}

	// Create the event channel with configured buffer size
	eventChan := make(chan *event.Event, r.config.BufferSize)

	// Start a goroutine to handle the ReAct agent execution
	go func() {
		defer close(eventChan)

		// 1. Convert trpc-agent-go message to eino message
		einoMessage := converters.ConvertToEinoMessage(invocation.Message)

		// 2. Create stream bridge for ReAct agent handling
		streamBridge := streaming.NewStreamBridge(r.name, invocation, eventChan).
			WithDebug(r.config.Debug).
			WithMaxChunkSize(r.config.ChunkSize)

		// 3. Handle ReAct agent execution
		// Convert single message to message slice for ReAct Agent API
		messages := []*schema.Message{einoMessage}
		if err := streamBridge.HandleReactAgentExecution(ctx, r.reactAgent, messages); err != nil {
			// Error is sent to event stream in streamBridge, but we also log for server-side debugging
			log.Errorf("ReActAgent execution failed for agent '%s': %v", r.name, err)
			return
		}
	}()

	return eventChan, nil
}

// Name returns the agent's name.
func (r *ReActAgent) Name() string {
	return r.name
}

// Tools returns the tools available to this agent.
// For ReAct agents, tools are managed internally and are not exposed directly.
func (r *ReActAgent) Tools() []tool.Tool {
	// ReAct agents manage their tools internally through their configuration
	return nil
}

// Info returns basic information about this ReActAgent.
func (r *ReActAgent) Info() agent.Info {
	return agent.Info{
		Name:        r.name,
		Description: "Eino ReAct Agent adapted for trpc-agent-go",
	}
}

// SubAgents returns empty slice as ReActAgent doesn't support sub-agents.
func (r *ReActAgent) SubAgents() []agent.Agent {
	return nil
}

// FindSubAgent returns a sub-agent by name.
// ReActAgent does not support sub-agents, so this always returns nil.
func (r *ReActAgent) FindSubAgent(name string) agent.Agent {
	return nil
}
