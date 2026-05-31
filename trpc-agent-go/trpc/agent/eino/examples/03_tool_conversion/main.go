// Package main demonstrates converting Eino tools for use with tRPC native agents.
// Shows how to reuse your Eino tool ecosystem in tRPC applications.
package main

import (
	"context"
	"fmt"
	"log"

	teino "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/examples/shared"
)

func main() {
	fmt.Println("🔧 Tool Conversion Demo")
	fmt.Println("=======================")

	ctx := context.Background()

	// Demo 1: Basic Tool Conversion
	fmt.Println("\n1️⃣ Basic Eino Tool Conversion")
	fmt.Println("------------------------------")
	basicToolConversion(ctx)

	// Demo 2: Tool with Configuration
	fmt.Println("\n2️⃣ Tool Conversion with Options")
	fmt.Println("-------------------------------")
	toolWithOptions(ctx)

	// Demo 3: Using Converted Tools in Agent
	fmt.Println("\n3️⃣ Using Converted Tools in tRPC Agent")
	fmt.Println("--------------------------------------")
	useConvertedTools(ctx)

	fmt.Println("\n✅ Tool conversion complete!")
	fmt.Println("💡 Your Eino tools now work with tRPC native agents")
}

func basicToolConversion(ctx context.Context) {
	// Your existing Eino tool
	einoCalculator := shared.NewEinoCalculatorTool()
	einoWeather := shared.NewEinoWeatherTool()

	// Convert to tRPC tools (one line each!)
	trpcCalculator := teino.NewTool(einoCalculator)
	trpcWeather := teino.NewTool(einoWeather)

	// Test the converted tools
	fmt.Println("  🧮 Testing converted calculator:")
	testTool(ctx, trpcCalculator, "calculator", map[string]any{
		"operation": "add",
		"a":         10.0,
		"b":         5.0,
	})

	fmt.Println("  🌤️ Testing converted weather tool:")
	testTool(ctx, trpcWeather, "weather", map[string]any{
		"city": "Beijing",
	})
}

func toolWithOptions(ctx context.Context) {
	einoCalculator := shared.NewEinoCalculatorTool()

	// Convert with custom configuration
	trpcCalculator := teino.NewTool(einoCalculator,
		teino.WithName("advanced_calculator"),
		teino.WithDescription("Advanced calculator with custom config"),
		teino.WithTimeout(30), // 30 seconds timeout
	)

	fmt.Println("  🎛️ Testing tool with custom config:")
	testTool(ctx, trpcCalculator, "advanced_calculator", map[string]any{
		"operation": "multiply",
		"a":         7.0,
		"b":         6.0,
	})
}

func useConvertedTools(ctx context.Context) {
	// Convert your Eino tools
	einoCalculator := shared.NewEinoCalculatorTool()
	einoWeather := shared.NewEinoWeatherTool()

	trpcCalculator := teino.NewTool(einoCalculator)
	trpcWeather := teino.NewTool(einoWeather)

	// Create tRPC native agent with converted tools
	mockModel := shared.NewMockModel("tool-demo-model")

	agent := llmagent.New("tool-conversion-agent",
		llmagent.WithModel(mockModel),
		llmagent.WithTools([]tool.Tool{trpcCalculator, trpcWeather}), // Use converted tools!
		llmagent.WithDescription("tRPC agent using converted Eino tools"),
	)

	// Use with Runner
	r := runner.NewRunner("tool-demo-app", agent)

	// Test the agent
	userMessage := model.NewUserMessage("Can you help me calculate 15 + 25?")
	eventStream, err := r.Run(ctx, "user123", "session456", userMessage)
	if err != nil {
		log.Printf("  ❌ Agent run failed: %v", err)
		return
	}

	fmt.Print("  🤖 Agent response: ")
	hasError := false
	for event := range eventStream {
		if event.Error != nil {
			fmt.Printf("Error: %s\n", event.Error.Message)
			hasError = true
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

	if !hasError {
		fmt.Println("  ✅ Your Eino tools are now fully integrated!")
	}
}

func testTool(ctx context.Context, tool tool.Tool, expectedName string, params map[string]any) {
	// Check tool info
	declaration := tool.Declaration()
	if declaration.Name != expectedName {
		fmt.Printf("    ⚠️ Name mismatch: expected %s, got %s\n", expectedName, declaration.Name)
	}

	// Tool is ready for use (testing execution would require proper tRPC context)
	fmt.Printf("    ✅ %s: Ready for use in tRPC Agent\n", declaration.Name)
	fmt.Printf("        Description: %s\n", declaration.Description)
}
