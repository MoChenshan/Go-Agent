// Package main demonstrates comprehensive Eino Agent migration patterns.
// Shows Chain, Graph, and ReAct Agent migration with configuration options.
package main

import (
	"context"
	"fmt"
	"log"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	teino "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

func main() {
	fmt.Println("🔄 Complete Agent Migration Guide")
	fmt.Println("================================")

	ctx := context.Background()

	// Demo 1: Chain Migration
	fmt.Println("\n1️⃣ Migrating Eino Chain")
	fmt.Println("------------------------")
	migrateChain(ctx)

	// Demo 2: Graph Migration
	fmt.Println("\n2️⃣ Migrating Eino Graph")
	fmt.Println("------------------------")
	migrateGraph(ctx)

	// Demo 3: WorkFlow Migration
	fmt.Println("\n3️⃣ Migrating Eino WorkFlow")
	fmt.Println("---------------------------")
	migrateWorkFlow(ctx)

	// Demo 4: ReAct Agent Migration
	fmt.Println("\n4️⃣ Migrating ReAct Agent")
	fmt.Println("-------------------------")
	migrateReActAgent(ctx)

	fmt.Println("\n✅ All migrations complete!")
	fmt.Println("💡 Next: See 02_multiagent_integration for mixed environments")
}

func migrateChain(ctx context.Context) {
	// Standard Eino Chain
	chain := compose.NewChain[map[string]any, any]()
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, input map[string]any) (any, error) {
		// Handle tRPC data format
		var userMsg string
		if q, ok := input["question"]; ok && q != nil {
			userMsg = q.(string)
		} else if content, ok := input["content"]; ok && content != nil {
			userMsg = content.(string)
		} else {
			userMsg = "your question"
		}
		return &schema.Message{
			Role:    schema.Assistant,
			Content: fmt.Sprintf("Chain processed: %s", userMsg),
		}, nil
	}))

	compiled, _ := chain.Compile(ctx)

	// Migrate to tRPC Agent with options
	agent := teino.New(compiled, "chain-agent",
		teino.WithChunkSize(2048),
		teino.WithBufferSize(150),
	)

	// Use with Runner
	runAgent(ctx, agent, "Chain migration test")
}

func migrateGraph(ctx context.Context) {
	// Standard Eino Graph
	graph := compose.NewGraph[map[string]any, any]()

	// Add a simple node
	graph.AddLambdaNode("processor", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (any, error) {
		// Handle tRPC data format
		var userMsg string
		if q, ok := input["question"]; ok && q != nil {
			userMsg = q.(string)
		} else if content, ok := input["content"]; ok && content != nil {
			userMsg = content.(string)
		} else {
			userMsg = "your question"
		}
		return &schema.Message{
			Role:    schema.Assistant,
			Content: fmt.Sprintf("Graph processed: %s", userMsg),
		}, nil
	}))

	graph.AddEdge("start", "processor")
	graph.AddEdge("processor", "end")

	compiled, _ := graph.Compile(ctx)

	// Migrate to tRPC Agent
	agent := teino.New(compiled, "graph-agent")

	runAgent(ctx, agent, "Graph migration test")
}

func migrateWorkFlow(ctx context.Context) {
	// Standard Eino WorkFlow
	workflow := compose.NewWorkflow[map[string]any, any]()

	// Add workflow nodes with proper connections
	workflow.AddLambdaNode("input_validation", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (any, error) {
		// Handle tRPC data format
		var userMsg string
		if q, ok := input["question"]; ok && q != nil {
			userMsg = q.(string)
		} else if content, ok := input["content"]; ok && content != nil {
			userMsg = content.(string)
		} else {
			userMsg = "your question"
		}

		return map[string]any{
			"validated_input": userMsg,
			"status":          "validated",
		}, nil
	})).AddInput(compose.START)

	workflow.AddLambdaNode("processing", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (any, error) {
		validatedInput := input["validated_input"].(string)
		return &schema.Message{
			Role:    schema.Assistant,
			Content: fmt.Sprintf("WorkFlow processed: %s with step-by-step logic", validatedInput),
		}, nil
	})).AddInput("input_validation")

	// Set workflow output
	workflow.End().AddInput("processing")

	compiled, err := workflow.Compile(ctx)
	if err != nil {
		log.Printf("  ❌ Failed to compile workflow: %v", err)
		return
	}

	// Migrate to tRPC Agent with workflow-specific options
	agent := teino.New(compiled, "workflow-agent",
		teino.WithChunkSize(2048), // Larger chunks for workflow
		teino.WithBufferSize(150), // More buffering for steps
	)

	runAgent(ctx, agent, "WorkFlow migration test")
}

func migrateReActAgent(ctx context.Context) {
	// Create a real ReAct Agent with proper model and tools
	reactAgent := createRealReActAgent(ctx)

	// Migrate to tRPC Agent using the dedicated ReAct wrapper
	agent := teino.NewReAct(reactAgent, "react-agent",
		teino.WithChunkSize(1024),
		teino.WithBufferSize(100),
		teino.WithDebug(false),
	)

	runAgent(ctx, agent, "ReAct Agent migration test")
}

// createRealReActAgent creates a working ReAct Agent with proper interfaces
func createRealReActAgent(ctx context.Context) *react.Agent {
	// Create mock model that correctly implements eino ChatModel interface
	mockModel := &properChatModel{}

	// Create mock calculator tool
	calculatorTool := &calculatorTool{}

	// Configure ReAct Agent with proper interfaces
	config := &react.AgentConfig{
		Model: mockModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []einotool.BaseTool{calculatorTool},
		},
		MaxStep: 3, // Limit steps for demo
	}

	agent, err := react.NewAgent(ctx, config)
	if err != nil {
		log.Printf("  ❌ Failed to create ReAct Agent: %v", err)
		// Create a fallback simple agent
		log.Fatal("Cannot create ReAct Agent - check eino version compatibility")
	}

	return agent
}

// properChatModel implements eino ChatModel interface correctly (copied from working example)
type properChatModel struct{}

func (m *properChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	if len(messages) == 0 {
		return &schema.Message{
			Role:    schema.Assistant,
			Content: "ReAct Agent: I'm ready to help with reasoning and tool usage.",
		}, nil
	}

	lastMessage := messages[len(messages)-1]

	// Simple ReAct reasoning pattern
	return &schema.Message{
		Role:    schema.Assistant,
		Content: fmt.Sprintf("ReAct Agent reasoning:\nThought: User asked '%s'\nAction: Processing request\nObservation: Request analyzed\nFinal Answer: ReAct processed successfully", lastMessage.Content),
	}, nil
}

func (m *properChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *properChatModel) BindTools(tools []*schema.ToolInfo) error {
	// ReAct Agent handles tool binding automatically
	return nil
}

// calculatorTool implements eino BaseTool interface correctly (copied from working example)
type calculatorTool struct{}

func (t *calculatorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "calculator",
	}, nil
}

func (t *calculatorTool) InvokableRun(ctx context.Context, args string, opts ...einotool.Option) (string, error) {
	// Simplified calculation logic for demo
	return "42 (calculation result)", nil
}

func runAgent(ctx context.Context, agent agent.Agent, testName string) {
	r := runner.NewRunner("migration-app", agent)

	userMessage := model.NewUserMessage(testName)
	eventStream, err := r.Run(ctx, "user123", "session456", userMessage)
	if err != nil {
		log.Printf("  ❌ Run failed: %v", err)
		return
	}

	fmt.Print("  📝 Response: ")
	for event := range eventStream {
		if event.Error != nil {
			fmt.Printf("Error: %s\n", event.Error.Message)
			continue
		}
		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			if choice.Message.Content != "" {
				fmt.Printf("%s\n", choice.Message.Content)
				break // Just show first response for demo
			}
		}
	}
}
