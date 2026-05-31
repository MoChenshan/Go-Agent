package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/tool/trag"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func setEnv() {
	os.Setenv("TRAG_API_KEY", "your-trag-api-key")
	os.Setenv("OPENAI_BASE_URL", "your-openai-base-url")
	os.Setenv("OPENAI_API_KEY", "your-openai-api-key")
}

const (
	modelName   = "your-model-name"
	toolsetName = "your-toolset-name"
)

func main() {
	setEnv()

	ctx := context.Background()
	toolset, err := trag.NewToolSet(ctx, toolsetName)
	if err != nil {
		log.Fatalf("Failed to create toolset: %v", err)
	}
	defer toolset.Close()

	chatterer, err := newLLMAgentChat(modelName, toolset)
	if err != nil {
		log.Fatalf("Failed to create chatterer: %v", err)
	}

	if err := chatterer.startChat(context.Background()); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

// llmAgentChat manages the conversation.
type llmAgentChat struct {
	runner runner.Runner
}

func newLLMAgentChat(modelName string, toolset tool.ToolSet) (*llmAgentChat, error) {
	// Create a model instance.
	modelInstance := openai.New(modelName)

	// Create generation config.
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(1000),
		Temperature: floatPtr(0.7),
		Stream:      true,
	}

	// Create an LLMAgent with configuration.
	llmAgent := llmagent.New(
		"chat-with-trag-toolset-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("A helpful AI assistant for cat with tRAG toolset"),
		llmagent.WithInstruction("You are a helpful AI assistant. Be conversational and engaging. "+
			"Answer questions clearly and provide helpful information. Use the tools in tRAG toolset if needed."),
		llmagent.WithToolSets([]tool.ToolSet{toolset}),
		llmagent.WithGenerationConfig(genConfig),
	)

	return &llmAgentChat{
		runner: runner.NewRunner(
			"your-app-name",
			llmAgent,
		),
	}, nil
}

// startChat runs the interactive conversation loop.
func (c *llmAgentChat) startChat(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("💡 Commands:")
	fmt.Println("   /exit     - End the conversation")
	fmt.Println()

	for {
		fmt.Print("👤 You: ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		// Handle special commands.
		switch strings.ToLower(userInput) {
		case "/exit":
			fmt.Println("👋 Goodbye!")
			return nil
		}

		// Process the user message.
		if err := c.processMessage(ctx, userInput); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}

		fmt.Println() // Add spacing between turns.
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("input scanner error: %w", err)
	}

	return nil
}

// processMessage handles a single message exchange.
func (c *llmAgentChat) processMessage(ctx context.Context, userMessage string) error {

	// Run the agent.
	eventChan, err := c.runner.Run(ctx, "user", "session-id", model.NewUserMessage(userMessage))
	if err != nil {
		return fmt.Errorf("failed to run LLMAgent: %w", err)
	}

	// Process response.
	return c.processStreamingResponse(eventChan)
}

// processStreamingResponse processes streaming response, including tool call visualization
func (c *llmAgentChat) processStreamingResponse(eventChan <-chan *event.Event) error {
	fmt.Print("🤖 Assistant: ")

	var (
		fullContent       string
		toolCallsDetected bool
		assistantStarted  bool
	)

	for event := range eventChan {
		// Handle errors
		if event.Error != nil {
			fmt.Printf("\n❌ Error: %s\n", event.Error.Message)
			continue
		}

		// Detect and display tool calls
		if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
			toolCallsDetected = true
			if assistantStarted {
				fmt.Printf("\n")
			}
			fmt.Printf("🔧 Tool calls:\n")
			for _, toolCall := range event.Choices[0].Message.ToolCalls {
				fmt.Printf("   %s (ID: %s)\n", toolCall.Function.Name, toolCall.ID)
				if len(toolCall.Function.Arguments) > 0 {
					fmt.Printf("     Arguments: %s\n", string(toolCall.Function.Arguments))
				}
			}
			fmt.Printf("\n⚡ Executing...\n")
		}

		// Detect tool responses
		if event.Response != nil && len(event.Response.Choices) > 0 {
			hasToolResponse := false
			for _, choice := range event.Response.Choices {
				if choice.Message.Role == model.RoleTool && choice.Message.ToolID != "" {
					fmt.Printf("✅ Tool result (ID: %s): %s\n",
						choice.Message.ToolID,
						formatToolResult(choice.Message.Content))
					hasToolResponse = true
				}
			}
			if hasToolResponse {
				continue
			}
		}

		// Process streaming content
		if len(event.Choices) > 0 {
			choice := event.Choices[0]

			// Process streaming delta content
			if choice.Delta.Content != "" {
				if !assistantStarted {
					if toolCallsDetected {
						fmt.Printf("\n🤖 Assistant: ")
					}
					assistantStarted = true
				}
				fmt.Print(choice.Delta.Content)
				fullContent += choice.Delta.Content
			}
		}

		// Check if this is the final event
		if event.IsFinalResponse() {
			fmt.Printf("\n")
			break
		}
	}

	return nil
}

// intPtr returns a pointer to the given int value.
func intPtr(i int) *int {
	return &i
}

// floatPtr returns a pointer to the given float64 value.
func floatPtr(f float64) *float64 {
	return &f
}

// formatToolResult formats the display of tool results
func formatToolResult(content string) string {
	if len(content) > 200 {
		return content[:200] + "..."
	}
	return strings.TrimSpace(content)
}
