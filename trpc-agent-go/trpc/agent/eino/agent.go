// Package eino provides adapters to integrate eino framework components with trpc-agent-go.
// This package enables seamless migration from eino to trpc-agent-go with minimal code changes.
package eino

import (
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/adapters"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"trpc.group/trpc-go/trpc-agent-go/agent"
)

// New wraps an eino compiled Runnable (Chain, Graph, Workflow) as a trpc-agent-go Agent.
// This is the primary entry point for migrating eino Runnable-based applications.
//
// Supports all eino compiled components:
//   - Chain: Linear composition of components
//   - Graph: Complex graph execution with branches and routing
//   - Workflow: Advanced data flow orchestration
//
// Example usage:
//
//	// Chain example
//	chain := compose.NewChain[map[string]any, *schema.Message]()
//	chain.AppendChatTemplate(template).AppendChatModel(model)
//	compiledChain, _ := chain.Compile(ctx)
//	agent := eino.New(compiledChain, "chain-assistant")
//
//	// Graph example
//	graph := compose.NewGraph[map[string]any, *schema.Message]()
//	// ... configure graph nodes and edges
//	compiledGraph, _ := graph.Compile(ctx)
//	agent := eino.New(compiledGraph, "graph-agent")
//
//	// With options
//	agent := eino.New(compiledChain, "assistant",
//		eino.WithChunkSize(2048),
//	)
func New(runnable compose.Runnable[map[string]any, any], name string, options ...Option) agent.Agent {
	config := buildConfig(options...)
	adapterConfig := &adapters.Config{
		Debug:      config.Debug,
		ChunkSize:  config.ChunkSize,
		BufferSize: config.BufferSize,
	}
	return adapters.NewBaseAgent(runnable, name, adapterConfig)
}

// NewReAct wraps an eino ReAct Agent as a trpc-agent-go Agent.
// ReAct (Reasoning + Acting) agents have special handling for their unique execution pattern.
//
// Example usage:
//
//	// Create eino ReAct Agent
//	reactConfig := &react.AgentConfig{
//		Model: chatModel,
//		ToolsConfig: compose.ToolsConfig{
//			Tools: []tool.BaseTool{weatherTool, calculatorTool},
//		},
//		MaxStep: 10,
//	}
//	reactAgent, _ := react.NewAgent(ctx, reactConfig)
//
//	// Wrap as trpc-agent-go Agent
//	agent := eino.NewReAct(reactAgent, "react-agent")
//
//	// With options
//	agent := eino.NewReAct(reactAgent, "reasoning-agent",
//		eino.WithBufferSize(200),
//	)
func NewReAct(reactAgent *react.Agent, name string, options ...Option) agent.Agent {
	config := buildConfig(options...)
	adapterConfig := &adapters.Config{
		Debug:      config.Debug,
		ChunkSize:  config.ChunkSize,
		BufferSize: config.BufferSize,
	}
	return adapters.NewReActAgent(reactAgent, name, adapterConfig)
}
