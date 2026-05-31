// Package main demonstrates the quickest way to migrate an Eino Chain to tRPC Agent.
// This is a 30-second demo showing the core value proposition.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	teino "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

func main() {
	fmt.Println("🚀 Quickstart: Eino to tRPC Agent in 30 seconds")
	fmt.Println("============================================")

	ctx := context.Background()

	// 1. Create an Eino Chain (your existing code)
	chain := compose.NewChain[map[string]any, any]()
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, input map[string]any) (any, error) {
		// tRPC sends user message content directly
		question := "your question" // default value

		// Try to extract string from "question" or "content" fields
		for _, key := range []string{"question", "content"} {
			if value, ok := input[key]; ok && value != nil {
				if str, ok := value.(string); ok {
					question = str
					break
				}
			}
		}
		return &schema.Message{
			Role:    schema.Assistant,
			Content: fmt.Sprintf("Eino says: Hello! You asked about '%s'", question),
		}, nil
	}))

	// 2. Compile your Chain (standard Eino)
	compiled, err := chain.Compile(ctx)
	if err != nil {
		log.Fatalf("Failed to compile chain: %v", err)
	}

	// 3. Wrap as tRPC Agent (ONE LINE!)
	agent := teino.New(compiled, "quickstart-agent")

	// 4. Use with tRPC Runner (standard tRPC)
	r := runner.NewRunner("quickstart-app", agent)

	// 5. Test it!
	userMessage := model.NewUserMessage("How does this work?")
	eventStream, err := r.Run(ctx, "user123", "session456", userMessage)
	if err != nil {
		log.Fatalf("Failed to run: %v", err)
	}

	// 6. Process results
	fmt.Println("\n📝 Response:")
	for event := range eventStream {
		if event.Error != nil {
			fmt.Printf("❌ Error: %s\n", event.Error.Message)
			continue
		}
		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			if choice.Message.Content != "" {
				fmt.Printf("🤖 %s\n", choice.Message.Content)
			}
			if choice.Delta.Content != "" {
				fmt.Print(choice.Delta.Content)
			}
			if event.Done {
				fmt.Println() // New line after completion
				break
			}
		}
	}

	fmt.Println("\n✅ Done! Your Eino Chain is now a tRPC Agent!")
	fmt.Println("💡 Next: See 01_agent_migration for more details")
}
