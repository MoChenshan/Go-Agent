//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package main shows a session chat that persists sessions in Redis.
// The Redis target is resolved from trpc_go.yaml via the service name
// (trpc.test.helloworld.redis). Commands: /new to start a fresh session, /history to
// ask the assistant to show the conversation history.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/client"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/redis"
)

var (
	modelName = flag.String("model", "deepseek-chat", "Name of the model to use")
)

func main() {
	// Load config from trpc_go.yaml
	_ = trpc.NewServer()
	// Parse command line flags.
	flag.Parse()
	fmt.Printf("🚀 Redis Session Agent\n")
	fmt.Printf("Model: %s\n", *modelName)
	fmt.Printf("Type 'exit' to end the conversation\n")
	fmt.Println(strings.Repeat("=", 50))
	// Create and run the chat.
	chat := &sessionChat{modelName: *modelName}
	if err := chat.run(); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

// sessionChat manages the conversation and session wiring.
type sessionChat struct {
	modelName string
	runner    runner.Runner
	userID    string
	sessionID string
}

// run starts the interactive chat session.
func (c *sessionChat) run() error {
	ctx := context.Background()
	// Setup the runner.
	if err := c.setup(ctx); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}
	// Start interactive chat.
	return c.startChat(ctx)
}

// setup creates a simple LLM agent and configures session storage
// using the target from trpc_go.yaml (via service name resolution).
func (c *sessionChat) setup(_ context.Context) error {
	// Create the model and a minimal agent (no tools/parallel/streaming).
	modelInstance := openai.New(c.modelName)
	llmAgent := llmagent.New(
		"chat-assistant",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("A simple helpful AI assistant"),
		llmagent.WithGenerationConfig(model.GenerationConfig{ // non-streaming to keep console output simple
			MaxTokens:   intPtr(2000),
			Temperature: floatPtr(0.7),
			Stream:      true,
		}),
	)
	// Resolve backend by service name. The actual target (e.g., redis://...) is read from trpc_go.yaml.
	sessionService, err := redis.NewService(
		redis.WithExtraOptions(client.WithServiceName("trpc.test.helloworld.redis")),
	)
	if err != nil {
		return fmt.Errorf("failed to create session service: %w", err)
	}
	// Create Runner and inject the session service.
	appName := "session-redis"
	c.runner = runner.NewRunner(
		appName,
		llmAgent,
		runner.WithSessionService(sessionService),
	)
	// Setup identifiers.
	c.userID = "user"
	c.sessionID = fmt.Sprintf("chat-session-%d", time.Now().Unix())
	fmt.Printf("✅ Chat ready! Session: %s\n", c.sessionID)
	fmt.Printf("🔗 Session backend: service '%s' (target from trpc_go.yaml)\n\n", "trpc.test.helloworld.redis")

	return nil
}

// startChat runs the interactive conversation loop.
func (c *sessionChat) startChat(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("💡 Commands:")
	fmt.Println("   /new      - Start a new session")
	fmt.Println("   /history  - Show conversation history")
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
		// Handle command.
		switch strings.ToLower(userInput) {
		case "/exit":
			fmt.Println("👋 Goodbye!")
			return nil
		case "/new":
			c.startNewSession()
			continue
		case "/history":
			// Ask the assistant to show conversation history for the current session.
			userInput = "show our conversation history"
		}
		// Process the user message.
		if err := c.processMessage(ctx, userInput); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
		fmt.Println() // Add spacing between turns
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("input scanner error: %w", err)
	}
	return nil
}

// processMessage handles a single message exchange.
func (c *sessionChat) processMessage(ctx context.Context, userMessage string) error {
	message := model.NewUserMessage(userMessage)
	eventChan, err := c.runner.Run(ctx, c.userID, c.sessionID, message)
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}
	return c.processSimpleResponse(eventChan)
}

// processSimpleResponse prints the final assistant content only,
// keeping the example focused and uncluttered.
func (c *sessionChat) processSimpleResponse(eventChan <-chan *event.Event) error {
	fmt.Print("🤖 Assistant: ")
	for event := range eventChan {
		if event.Error != nil {
			fmt.Printf("\n❌ Error: %s\n", event.Error.Message)
			break
		}
		if len(event.Choices) > 0 {
			ch := event.Choices[0]
			if ch.Message.Content != "" {
				fmt.Print(ch.Message.Content)
			} else if ch.Delta.Content != "" {
				fmt.Print(ch.Delta.Content)
			}
		}
		if event.Done {
			break
		}
	}
	fmt.Println()
	return nil
}

// startNewSession resets the session ID for a fresh conversation context.
func (c *sessionChat) startNewSession() {
	old := c.sessionID
	c.sessionID = fmt.Sprintf("chat-session-%d", time.Now().Unix())
	fmt.Println("🆕 Started new session")
	fmt.Printf("   Previous: %s\n", old)
	fmt.Printf("   Current:  %s\n", c.sessionID)
}

// Helper functions for creating pointers to primitive types.
func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }
