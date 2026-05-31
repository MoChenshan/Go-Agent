// Package main demonstrates advanced streaming callback integration using StreamCallbackAgent.
// Perfect for complex streaming scenarios like ChatBuffer implementations.
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
	fmt.Println("🌊 Streaming Callbacks Integration")
	fmt.Println("==================================")

	ctx := context.Background()

	// Demo 1: Simple Streaming Callback
	fmt.Println("\n1️⃣ Simple Streaming Callback")
	fmt.Println("-----------------------------")
	simpleStreamingDemo(ctx)

	// Demo 2: ChatBuffer-style Streaming
	fmt.Println("\n2️⃣ ChatBuffer-style Streaming")
	fmt.Println("------------------------------")
	chatBufferDemo(ctx)

	// Demo 3: Streaming with Node Filtering
	fmt.Println("\n3️⃣ Streaming with Node Filtering")
	fmt.Println("---------------------------------")
	filteredStreamingDemo(ctx)

	fmt.Println("\n✅ Streaming callbacks integration complete!")
	fmt.Println("💡 Your complex Eino streaming logic (like ChatBuffer) now works with tRPC!")
}

func simpleStreamingDemo(ctx context.Context) {
	// Your existing Eino streaming callback
	streamingCallback := shared.NewSimpleEinoCallback("SimpleStreaming")

	// Create base tRPC agent
	mockModel := shared.NewMockModel("streaming-model")
	baseAgent := llmagent.New("base-streaming-agent",
		llmagent.WithModel(mockModel),
		llmagent.WithDescription("Base agent for streaming demo"),
	)

	// Wrap with StreamCallbackAgent for streaming callbacks
	streamCallbackAgent := teino.NewStreamCallbackAgent(baseAgent,
		teino.WithEinoCallbacks(streamingCallback),
	)

	// Test the streaming agent
	runStreamingDemo(ctx, streamCallbackAgent, "Simple streaming demo", "Tell me a story")
}

func chatBufferDemo(ctx context.Context) {
	// Your complex ChatBuffer-style callback
	chatBufferCallback := shared.NewChatBufferCallback("ChatBuffer")

	// Create base agent
	mockModel := shared.NewMockModel("chatbuffer-model")
	baseAgent := llmagent.New("chatbuffer-agent",
		llmagent.WithModel(mockModel),
		llmagent.WithDescription("Agent with ChatBuffer-style processing"),
	)

	// Wrap with streaming callback support
	streamCallbackAgent := teino.NewStreamCallbackAgent(baseAgent,
		teino.WithEinoCallbacks(chatBufferCallback),
	)

	// Test the ChatBuffer agent
	runStreamingDemo(ctx, streamCallbackAgent, "ChatBuffer demo", "Generate a detailed response about AI")

	fmt.Println("  💡 Check logs above for intelligent buffering output")
}

func filteredStreamingDemo(ctx context.Context) {
	// Callback that only processes specific nodes
	filteredCallback := shared.NewChatBufferCallback("FilteredBuffer")

	// Create base agent with tools
	mockModel := shared.NewMockModel("filtered-model")
	calculatorTool := shared.NewCalculatorTool()

	baseAgent := llmagent.New("filtered-agent",
		llmagent.WithModel(mockModel),
		llmagent.WithTools([]tool.Tool{calculatorTool}),
		llmagent.WithDescription("Agent with filtered streaming callbacks"),
	)

	// Wrap with node filtering
	streamCallbackAgent := teino.NewStreamCallbackAgent(baseAgent,
		teino.WithEinoCallbacks(filteredCallback),
		teino.WithNodeFilter("chat_model", "filtered-model"), // Only process specific nodes
	)

	// Test the filtered agent
	runStreamingDemo(ctx, streamCallbackAgent, "Filtered streaming demo", "Process this complex request")

	fmt.Println("  🎯 Only specified nodes are processed by the streaming callback")
}

func runStreamingDemo(ctx context.Context, agent agent.Agent, demoName, userInput string) {
	r := runner.NewRunner("streaming-demo-app", agent)

	fmt.Printf("  🎬 Running: %s\n", demoName)
	fmt.Printf("  📝 Input: %s\n", userInput)

	userMessage := model.NewUserMessage(userInput)
	eventStream, err := r.Run(ctx, "user123", "session456", userMessage)
	if err != nil {
		log.Printf("  ❌ Demo failed: %v", err)
		return
	}

	fmt.Print("  🤖 Response: ")
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

	fmt.Println("  🌊 Streaming callback processing completed")
}
