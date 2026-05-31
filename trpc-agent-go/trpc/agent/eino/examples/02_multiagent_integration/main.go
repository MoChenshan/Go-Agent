// Package main demonstrates integrating Eino agents into existing multi-agent systems.
// Shows how to gradually migrate from pure tRPC to mixed Eino+tRPC environments.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	teino "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/examples/shared"
)

func main() {
	fmt.Println("🏢 Multi-Agent Integration Demo")
	fmt.Println("===============================")

	ctx := context.Background()

	// Scenario: You have existing tRPC agents and want to add Eino agents
	fmt.Println("\n1️⃣ Existing tRPC Agent")
	nativeAgent := createNativeTRPCAgent()
	testAgent(ctx, nativeAgent, "Native tRPC Agent", "Hello from native!")

	fmt.Println("\n2️⃣ New Eino Agent (migrated)")
	einoAgent := createEinoAgent(ctx)
	testAgent(ctx, einoAgent, "Migrated Eino Agent", "Hello from Eino!")

	fmt.Println("\n3️⃣ Mixed Environment Demo")
	demonstrateMixedEnvironment(ctx, nativeAgent, einoAgent)

	fmt.Println("\n✅ Integration complete!")
	fmt.Println("💡 This shows how to gradually migrate existing systems")
}

func createNativeTRPCAgent() agent.Agent {
	// Your existing tRPC agent (unchanged)
	mockModel := shared.NewMockModel("native-model")
	calculatorTool := shared.NewCalculatorTool()
	weatherTool := shared.NewWeatherTool()

	return llmagent.New("native-agent",
		llmagent.WithModel(mockModel),
		llmagent.WithTools([]tool.Tool{calculatorTool, weatherTool}),
		llmagent.WithDescription("Existing tRPC native agent"),
	)
}

func createEinoAgent(ctx context.Context) agent.Agent {
	// New Eino agent (migrated from existing Eino code)
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
			Content: fmt.Sprintf("Eino chain processed: %s", userMsg),
		}, nil
	}))

	compiled, err := chain.Compile(ctx)
	if err != nil {
		log.Fatal("Failed to compile Eino chain:", err)
	}

	// Migrate to tRPC (one line!)
	return teino.New(compiled, "eino-agent",
		teino.WithChunkSize(1024),
	)
}

func demonstrateMixedEnvironment(ctx context.Context, nativeAgent, einoAgent agent.Agent) {
	fmt.Println("  🔄 Running both agents in same system...")

	// Both agents can be used in the same runner/orchestrator

	// Test native agent
	r1 := runner.NewRunner("mixed-app-native", nativeAgent)

	// Test Eino agent
	r2 := runner.NewRunner("mixed-app-eino", einoAgent)

	// Run both in same context
	testMessage := model.NewUserMessage("Process this request")

	// Test both agents using the existing helper function
	runAgentWithPrefix(ctx, r1, "📱 Native Agent", testMessage)
	runAgentWithPrefix(ctx, r2, "📱 Eino Agent", testMessage)

	fmt.Println("  ✅ Both agents work seamlessly together!")
}

// runAgentWithPrefix runs an agent with a custom prefix
func runAgentWithPrefix(ctx context.Context, r runner.Runner, prefix string, message model.Message) {
	fmt.Printf("  %s: ", prefix)

	eventStream, err := r.Run(ctx, "user", "session", message)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	processEventStream(eventStream)
}

// processEventStream handles the common event processing logic
func processEventStream(eventStream <-chan *event.Event) {
	for event := range eventStream {
		if event.Error != nil {
			fmt.Printf("Error: %s\n", event.Error.Message)
			continue
		}
		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			if choice.Message.Content != "" {
				fmt.Printf("%s\n", choice.Message.Content)
				break
			}
		}
	}
}

func testAgent(ctx context.Context, agent agent.Agent, name, testMsg string) {
	r := runner.NewRunner("test-app", agent)
	userMessage := model.NewUserMessage(testMsg)
	runAgentWithPrefix(ctx, r, name, userMessage)
}
