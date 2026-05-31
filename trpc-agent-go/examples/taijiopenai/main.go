//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package main demonstrates using external model/openai with
// taiji.WithOpenAIErrorCompat in an interactive multi-turn chat.
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

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

const (
	defaultBaseURL   = "http://api.taiji.woa.com/openapi"
	defaultModelName = "DeepSeek-V3_1-Online-64k"
)

var (
	modelName = flag.String("model", getEnvOrDefault("TAIJI_MODEL", defaultModelName), "Name of the model to use")
	baseURL   = flag.String("base-url", getEnvOrDefault("TAIJI_BASE_URL", defaultBaseURL), "Taiji OpenAI-compatible base URL")
	apiKey    = flag.String("api-key", getEnvOrDefault("TAIJI_API_KEY", ""), "Taiji API key")
	streaming = flag.Bool("streaming", true, "Enable streaming mode for responses")
)

func main() {
	flag.Parse()
	fmt.Printf("🚀 Interactive Chat with Taiji OpenAI Model\n")
	fmt.Printf("Model: %s\n", *modelName)
	fmt.Printf("Base URL: %s\n", *baseURL)
	fmt.Printf("Streaming: %t\n", *streaming)
	fmt.Printf("Type '/exit' to end the conversation\n")
	fmt.Println(strings.Repeat("=", 50))
	chat := &taijiOpenAIChat{
		modelName: *modelName,
		baseURL:   *baseURL,
		apiKey:    *apiKey,
		streaming: *streaming,
	}
	if err := chat.run(); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

// taijiOpenAIChat manages the conversation.
type taijiOpenAIChat struct {
	modelName string
	baseURL   string
	apiKey    string
	streaming bool
	runner    runner.Runner
	userID    string
	sessionID string
}

// run starts the interactive chat session.
func (c *taijiOpenAIChat) run() error {
	ctx := context.Background()
	if err := c.setup(); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}
	defer c.runner.Close()
	return c.startChat(ctx)
}

// setup creates the runner with external model/openai plus Taiji compatibility.
func (c *taijiOpenAIChat) setup() error {
	modelInstance := openai.New(
		c.modelName,
		openai.WithBaseURL(c.baseURL),
		openai.WithAPIKey(c.apiKey),
		taiji.WithOpenAIErrorCompat(),
	)
	genConfig := model.GenerationConfig{Stream: c.streaming}
	llmAgent := llmagent.New(
		"taiji-openai-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("A helpful AI assistant powered by Taiji through the OpenAI-compatible model."),
		llmagent.WithInstruction("You are a helpful AI assistant. Be concise and clear."),
		llmagent.WithGenerationConfig(genConfig),
	)
	c.runner = runner.NewRunner(
		"taiji-openai-demo",
		llmAgent,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)
	c.userID = "user"
	c.sessionID = fmt.Sprintf("taiji-openai-session-%d", time.Now().Unix())
	fmt.Printf("✅ Chat ready! Session: %s\n\n", c.sessionID)
	return nil
}

// startChat runs the interactive conversation loop.
func (c *taijiOpenAIChat) startChat(ctx context.Context) error {
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
		if strings.ToLower(userInput) == "/exit" {
			fmt.Println("👋 Goodbye!")
			return nil
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

// processMessage handles a single message exchange.
func (c *taijiOpenAIChat) processMessage(ctx context.Context, userMessage string) error {
	message := model.NewUserMessage(userMessage)
	eventChan, err := c.runner.Run(ctx, c.userID, c.sessionID, message)
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}
	return c.processResponse(eventChan)
}

// processResponse handles the streaming response.
func (c *taijiOpenAIChat) processResponse(eventChan <-chan *event.Event) error {
	fmt.Print("🤖 Assistant: ")
	for evt := range eventChan {
		if err := c.handleEvent(evt); err != nil {
			return err
		}
		if evt != nil && evt.IsFinalResponse() {
			fmt.Printf("\n")
			break
		}
	}
	return nil
}

// handleEvent processes a single event from the event channel.
func (c *taijiOpenAIChat) handleEvent(evt *event.Event) error {
	if evt == nil {
		return nil
	}
	if evt.Error != nil {
		fmt.Printf("\n❌ Error: %s\n", evt.Error.Message)
		return nil
	}
	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return nil
	}
	content := c.extractContent(evt.Response.Choices[0])
	if content != "" {
		fmt.Print(content)
	}
	return nil
}

// extractContent extracts content based on streaming mode.
func (c *taijiOpenAIChat) extractContent(choice model.Choice) string {
	if c.streaming {
		return choice.Delta.Content
	}
	return choice.Message.Content
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}
