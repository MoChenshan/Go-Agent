// Package main demonstrates converting basic Eino callbacks (OnStart/OnEnd/OnError)
// for use with tRPC native agents. Perfect for monitoring and logging.
package main

import (
	"context"
	"fmt"
	"log"

	teino "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/examples/shared"
)

func main() {
	fmt.Println("📊 Basic Callbacks Integration")
	fmt.Println("==============================")

	ctx := context.Background()

	// Demo 1: Tool Callbacks
	fmt.Println("\n1️⃣ Tool Callbacks (OnStart/OnEnd/OnError)")
	fmt.Println("------------------------------------------")
	toolCallbacksDemo(ctx)

	// Demo 2: Model Callbacks
	fmt.Println("\n2️⃣ Model Callbacks (OnStart/OnEnd/OnError)")
	fmt.Println("-------------------------------------------")
	modelCallbacksDemo(ctx)

	// Demo 3: Combined Callbacks
	fmt.Println("\n3️⃣ Combined Tool + Model Callbacks")
	fmt.Println("-----------------------------------")
	combinedCallbacksDemo(ctx)

	fmt.Println("\n✅ Basic callbacks integration complete!")
	fmt.Println("💡 Your Eino monitoring logic now works with tRPC agents")
}

func toolCallbacksDemo(ctx context.Context) {
	// Your existing Eino callback handler
	einoCallback := shared.NewSimpleEinoCallback("ToolMonitor")

	// Convert to tRPC tool callbacks (one line!)
	trpcToolCallbacks := teino.NewToolCallbacks(einoCallback)

	// Create agent with tool callbacks
	mockModel := shared.NewMockModel("tool-callback-model")
	calculatorTool := shared.NewCalculatorTool()

	agent := llmagent.New("tool-callback-agent",
		llmagent.WithModel(mockModel),
		llmagent.WithTools([]tool.Tool{calculatorTool}),
		llmagent.WithToolCallbacks(trpcToolCallbacks), // Your Eino callbacks!
		llmagent.WithDescription("Agent with Eino tool monitoring"),
	)

	// Test the agent
	runAgentDemo(ctx, agent, "Tool callback demo", "Please calculate 10 + 5")
}

func modelCallbacksDemo(ctx context.Context) {
	// Your existing Eino callback handler
	einoCallback := shared.NewSimpleEinoCallback("ModelMonitor")

	// Convert to tRPC model callbacks (one line!)
	trpcModelCallbacks := teino.NewModelCallbacks(einoCallback)

	// Create agent with model callbacks
	mockModel := shared.NewMockModel("model-callback-model")

	agent := llmagent.New("model-callback-agent",
		llmagent.WithModel(mockModel),
		llmagent.WithModelCallbacks(trpcModelCallbacks), // Your Eino callbacks!
		llmagent.WithDescription("Agent with Eino model monitoring"),
	)

	// Test the agent
	runAgentDemo(ctx, agent, "Model callback demo", "Hello, how are you?")
}

func combinedCallbacksDemo(ctx context.Context) {
	// Your existing Eino callback handler (same one for both!)
	einoCallback := shared.NewSimpleEinoCallback("FullMonitor")

	// Convert to both types of tRPC callbacks
	trpcToolCallbacks := teino.NewToolCallbacks(einoCallback,
		teino.WithCallbackNodeFilter("calculator"), // Only monitor calculator tool
	)
	trpcModelCallbacks := teino.NewModelCallbacks(einoCallback)

	// Create agent with both callbacks
	mockModel := shared.NewMockModel("combined-callback-model")
	calculatorTool := shared.NewCalculatorTool()
	weatherTool := shared.NewWeatherTool()

	agent := llmagent.New("combined-callback-agent",
		llmagent.WithModel(mockModel),
		llmagent.WithTools([]tool.Tool{calculatorTool, weatherTool}),
		llmagent.WithToolCallbacks(trpcToolCallbacks),   // Monitor tools
		llmagent.WithModelCallbacks(trpcModelCallbacks), // Monitor model
		llmagent.WithDescription("Agent with full Eino monitoring"),
	)

	// Test the agent
	runAgentDemo(ctx, agent, "Combined callback demo", "Calculate 7 * 8 and tell me the weather")

	fmt.Println("  💡 Notice: Only calculator tool is monitored (due to node filter)")
}

func runAgentDemo(ctx context.Context, agent agent.Agent, demoName, userInput string) {
	r := runner.NewRunner("callback-demo-app", agent)

	fmt.Printf("  🎬 Running: %s\n", demoName)
	fmt.Printf("  📝 Input: %s\n", userInput)

	userMessage := model.NewUserMessage(userInput)
	eventStream, err := r.Run(ctx, "user123", "session456", userMessage)
	if err != nil {
		log.Printf("  ❌ Demo failed: %v", err)
		return
	}

	fmt.Print("  🤖 Response: ")
	hasResponse := false
	for event := range eventStream {
		if event.Error != nil {
			fmt.Printf("Error: %s\n", event.Error.Message)
			continue
		}
		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			if choice.Message.Content != "" {
				fmt.Printf("%s\n", choice.Message.Content)
				hasResponse = true
				break
			}
		}
	}

	if !hasResponse {
		fmt.Println("(No response content)")
	}

	fmt.Println("  📊 Check logs above for callback monitoring output")
}
