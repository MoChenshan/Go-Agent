// Package main demonstrates tMemory integration for trpc-agent-go.
//
// tMemory is a managed memory service that stores and recalls user memories
// via HTTP API. Unlike local memory backends (sqlite, redis, etc.), tMemory
// performs server-side memory extraction asynchronously after data ingestion.
// When memories become recallable depends on the configured extraction strategy.
//
// This example shows:
//   - Creating a tmemory.Service with API key authentication
//   - Registering the memory_search tool with the LLM agent
//   - Using runner.WithSessionIngestor(svc) for automatic session ingestion after each turn
//   - Recalling memories through the agent's tool-calling flow
//
// Usage:
//
//	export TMEMORY_API_KEY="your-api-key"
//	go run main.go
//	go run main.go -model gpt-5.2 -biz-id my-app
//
// Environment variables:
//
//	TMEMORY_API_KEY   - Required. Bearer token for tMemory API.
//	TMEMORY_HOST      - Optional. tMemory server URL (default: http://test-tmemory.woa.com).
//	OPENAI_API_KEY    - Required. API key for the chat model.
//	OPENAI_BASE_URL   - Optional. Base URL for the chat model API.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/memory/tmemory"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

var (
	modelName = flag.String(
		"model",
		"gpt-5.2",
		"Chat model name",
	)
	bizID = flag.String(
		"biz-id",
		"public",
		"Business ID for tMemory requests",
	)
	strategyID = flag.String(
		"strategy-id",
		"1",
		"Strategy ID for tMemory ingest/recall",
	)
	userID = flag.String(
		"user-id",
		"",
		"User ID for tMemory (defaults to env TMEMORY_USER_ID or 'user-example')",
	)
	streaming = flag.Bool(
		"streaming",
		true,
		"Enable streaming mode for responses",
	)
)

const (
	appName   = "tmemory-chat"
	agentName = "tmemory-assistant"
)

func main() {
	flag.Parse()

	chat := &tmemoryChat{
		modelName:  *modelName,
		bizID:      *bizID,
		strategyID: *strategyID,
		userID:     resolveUserID(*userID),
		streaming:  *streaming,
	}
	if err := chat.run(); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

type tmemoryChat struct {
	modelName  string
	bizID      string
	strategyID string
	streaming  bool

	svc       *tmemory.Service
	runner    runner.Runner
	userID    string
	sessionID string
}

func (c *tmemoryChat) run() error {
	ctx := context.Background()
	if err := c.setup(ctx); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}
	defer c.svc.Close()
	defer c.runner.Close()

	return c.startChat(ctx)
}

func (c *tmemoryChat) setup(_ context.Context) error {
	svc, err := tmemory.NewService(
		tmemory.WithBizID(c.bizID),
		tmemory.WithStrategyID(c.strategyID),
	)
	if err != nil {
		return fmt.Errorf("failed to create tmemory service: %w", err)
	}
	c.svc = svc

	// userID is resolved in main() from -user-id flag, TMEMORY_USER_ID
	// env var, or a deterministic default. We avoid hardcoding it here
	// so that the same example can be run for different users.
	c.sessionID = fmt.Sprintf("tmemory-session-%d", time.Now().Unix())

	chatModel := openai.New(c.modelName)
	genConfig := model.GenerationConfig{
		MaxTokens: intPtr(2000),
		Stream:    c.streaming,
	}

	llmAgent := llmagent.New(
		agentName,
		llmagent.WithModel(chatModel),
		llmagent.WithDescription(
			"A helpful AI assistant with tMemory-powered memory. "+
				"I can recall information from past conversations using the memory_search tool."),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools(svc.Tools()),
	)

	c.runner = runner.NewRunner(
		appName,
		llmAgent,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
		runner.WithSessionIngestor(svc), // Runner automatically calls svc.IngestSession after each turn.
	)

	fmt.Println("🧠 tMemory Chat Example")
	fmt.Printf("Model: %s\n", c.modelName)
	fmt.Printf("BizID: %s\n", c.bizID)
	fmt.Printf("StrategyID: %s\n", c.strategyID)
	fmt.Printf("UserID: %s\n", c.userID)
	fmt.Printf("Streaming: %t\n", c.streaming)
	fmt.Printf("tMemory Host: %s\n", os.Getenv("TMEMORY_HOST"))
	fmt.Printf("Session: %s\n", c.sessionID)
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()
	fmt.Println("💡 tMemory extracts memories server-side after ingestion.")
	fmt.Println("   New memories become available according to the configured extraction strategy,")
	fmt.Println("   so you may need enough dialogue items plus some async processing time before")
	fmt.Println("   they are available via recall after you tell me something.")
	fmt.Println()

	return nil
}

func (c *tmemoryChat) startChat(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("💡 Special commands:")
	fmt.Println("   /new   - Start a new session")
	fmt.Println("   /exit  - End the conversation")
	fmt.Println()
	fmt.Println("💡 To exercise tMemory recall, just ask the assistant questions")
	fmt.Println("   like \"What do you remember about me?\" — it will call the")
	fmt.Println("   memory_search tool under the hood.")
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

		switch strings.ToLower(userInput) {
		case "/exit":
			fmt.Println("👋 Goodbye!")
			return nil
		case "/new":
			c.startNewSession()
			continue
		}

		if err := c.processMessage(ctx, userInput); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("input scanner error: %w", err)
	}
	return nil
}

func (c *tmemoryChat) processMessage(ctx context.Context, userMessage string) error {
	message := model.NewUserMessage(userMessage)

	eventChan, err := c.runner.Run(ctx, c.userID, c.sessionID, message)
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}

	return c.processResponse(eventChan)
}

func (c *tmemoryChat) processResponse(eventChan <-chan *event.Event) error {
	fmt.Print("🤖 Assistant: ")

	var (
		assistantStarted bool
		finalSeen        bool
	)

	for evt := range eventChan {
		if evt.Error != nil {
			fmt.Printf("\n❌ Error: %s\n", evt.Error.Message)
			continue
		}
		if finalSeen {
			continue
		}

		if c.hasToolCalls(evt) {
			c.handleToolCalls(evt, assistantStarted)
			assistantStarted = true
			continue
		}

		if c.hasToolResponses(evt) {
			c.handleToolResponses(evt)
			continue
		}

		if content := c.extractContent(evt); content != "" {
			if !assistantStarted {
				assistantStarted = true
			}
			fmt.Print(content)
		}

		if evt.IsFinalResponse() {
			fmt.Printf("\n")
			finalSeen = true
		}
	}

	return nil
}

func (c *tmemoryChat) hasToolCalls(evt *event.Event) bool {
	return len(evt.Response.Choices) > 0 &&
		len(evt.Response.Choices[0].Message.ToolCalls) > 0
}

func (c *tmemoryChat) hasToolResponses(evt *event.Event) bool {
	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return false
	}
	for _, choice := range evt.Response.Choices {
		if choice.Message.Role == model.RoleTool && choice.Message.ToolID != "" {
			return true
		}
	}
	return false
}

func (c *tmemoryChat) handleToolCalls(evt *event.Event, assistantStarted bool) {
	if assistantStarted {
		fmt.Printf("\n")
	}
	fmt.Printf("🔧 Memory tool calls:\n")
	for _, toolCall := range evt.Response.Choices[0].Message.ToolCalls {
		fmt.Printf("   • %s (ID: %s)\n", toolCall.Function.Name, toolCall.ID)
		if len(toolCall.Function.Arguments) > 0 {
			// Pretty-print the arguments.
			var args map[string]any
			if err := json.Unmarshal(toolCall.Function.Arguments, &args); err == nil {
				fmt.Printf("     Args: %v\n", args)
			} else {
				fmt.Printf("     Args: %s\n", string(toolCall.Function.Arguments))
			}
		}
	}
	fmt.Printf("\n🔄 Executing...\n")
}

func (c *tmemoryChat) handleToolResponses(evt *event.Event) {
	for _, choice := range evt.Response.Choices {
		if choice.Message.Role == model.RoleTool && choice.Message.ToolID != "" {
			content := strings.TrimSpace(choice.Message.Content)
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Printf("✅ Tool response (ID: %s): %s\n", choice.Message.ToolID, content)
		}
	}
}

func (c *tmemoryChat) extractContent(evt *event.Event) string {
	if len(evt.Response.Choices) == 0 {
		return ""
	}
	choice := evt.Response.Choices[0]
	if c.streaming {
		return choice.Delta.Content
	}
	return choice.Message.Content
}

func (c *tmemoryChat) startNewSession() {
	oldSessionID := c.sessionID
	c.sessionID = fmt.Sprintf("tmemory-session-%d", time.Now().Unix())
	fmt.Printf("🆕 Started new session!\n")
	fmt.Printf("   Previous: %s\n", oldSessionID)
	fmt.Printf("   Current:  %s\n", c.sessionID)
	fmt.Printf("   (Memories persist across sessions in tMemory)\n")
	fmt.Println()
}

func intPtr(i int) *int {
	return &i
}

// resolveUserID picks the userID in this priority order:
//  1. -user-id command-line flag (if non-empty)
//  2. TMEMORY_USER_ID environment variable (if non-empty)
//  3. fallback default "user-example"
func resolveUserID(flagVal string) string {
	if v := strings.TrimSpace(flagVal); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("TMEMORY_USER_ID")); v != "" {
		return v
	}
	return "user-example"
}
