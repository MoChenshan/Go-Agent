//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package main demonstrates multi-turn chat using the Taiji Agent with streaming
// output and session management.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/client"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/taiji"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/taiji/sdk"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"

	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/session/redis"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
)

var (
	redisAddr       = flag.String("redis-addr", "localhost:6379", "Redis address")
	sessServiceName = flag.String("session", "inmemory", "Name of the session service to use, inmemory / redis")
	streaming       = flag.Bool("streaming", true, "Enable streaming mode for responses")
)

// environment variables to configure Taiji
var (
	// refer https://iwiki.woa.com/p/4008515885, is devcloud environment URL here
	taijiURL         = getEnvOrDefault("TAIJI_URL", "http://stream-server-online-openapi.turbotke.production.polaris:1081")
	taijiServiceName = getEnvOrDefault("TAIJI_SERVICE", "trpc.test.taijiagent.taiji")
	taijiToken       = getEnvOrDefault("TAIJI_TOKEN", "7auxxxx")
	taijiAppID       = getEnvOrDefault("TAIJI_APP_ID", "18886")
)

func main() {
	// Parse command line flags.
	flag.Parse()

	fmt.Printf("🚀 Multi-turn Chat with Taiji Agent\n")
	fmt.Printf("Taiji Service Name: %s\n", taijiServiceName)
	fmt.Printf("Streaming: %t\n", *streaming)
	fmt.Printf("Session Service: %s\n", *sessServiceName)

	fmt.Printf("Type 'exit' to end the conversation\n")
	fmt.Println(strings.Repeat("=", 50))

	// Create and run the chat.
	chat := &multiTurnChat{
		streaming: *streaming,
	}

	_ = trpc.NewServer()
	if err := chat.run(); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

// multiTurnChat manages the conversation.
type multiTurnChat struct {
	streaming bool
	runner    runner.Runner
	userID    string
	sessionID string
}

// run starts the interactive chat session.
func (c *multiTurnChat) run() error {
	ctx := context.Background()

	// Setup the runner.
	if err := c.setup(ctx); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}

	// Start interactive chat.
	return c.startChat(ctx)
}

// setup creates the runner with Taiji agent.
func (c *multiTurnChat) setup(_ context.Context) error {
	// Create session service based on configuration.
	var (
		sessionService session.Service
		err            error
	)
	switch *sessServiceName {
	case "inmemory":
		sessionService = sessioninmemory.NewSessionService()

	case "redis":
		redisURL := fmt.Sprintf("redis://%s", *redisAddr)
		sessionService, err = redis.NewService(redis.WithRedisClientURL(redisURL))
		if err != nil {
			return fmt.Errorf("failed to create session service: %w", err)
		}
	default:
		return fmt.Errorf("invalid session service name: %s", *sessServiceName)
	}

	// Create Taiji options
	taijiOpts := sdk.NewTaijiOption(
		sdk.WithToken(taijiToken),
		sdk.WithApplicationID(taijiAppID),

		// you can specify Taiji Host By target of trpc_go.yaml
		// or you can specify Taiji Host By WithURL
		// WithURL has higher priority than WithServiceName
		sdk.WithServiceName(taijiServiceName),
		sdk.WithTRPCClientOptions(client.WithTimeout(time.Minute*10)),
		// sdk.WithURL("http://stream-server-online-openapi.turbotke.production.polaris:1081"),
	)

	// Create Taiji agent
	appName := "multi-turn-chat"
	agentName := "taiji-assistant"
	taijiAgent, err := taiji.New(
		taiji.WithAgentName(agentName),
		taiji.WithAgentDescription("A helpful AI assistant powered by Taiji."),
		taiji.WithTaijiOption(taijiOpts),
		taiji.WithStreaming(c.streaming),
	)
	if err != nil {
		return fmt.Errorf("failed to create taiji agent: %w", err)
	}

	// Create runner.
	c.runner = runner.NewRunner(
		appName,
		taijiAgent,
		runner.WithSessionService(sessionService),
	)

	// Setup identifiers.
	c.userID = "user"
	c.sessionID = fmt.Sprintf("taiji-chat-session-%d", time.Now().Unix())

	fmt.Printf("✅ Chat ready! Session: %s\n\n", c.sessionID)

	return nil
}

// startChat runs the interactive conversation loop.
func (c *multiTurnChat) startChat(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("💡 Special commands:")
	fmt.Println("   /history  - Show conversation history")
	fmt.Println("   /new      - Start a new session")
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
		case "/history":
			userInput = "show our conversation history"
		case "/new":
			c.startNewSession()
			continue
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
func (c *multiTurnChat) processMessage(ctx context.Context, userMessage string) error {
	message := model.NewUserMessage(userMessage)

	// Run the agent through the runner.
	eventChan, err := c.runner.Run(ctx, c.userID, c.sessionID, message, agent.WithCustomAgentConfigs(map[string]any{
		taiji.RunOptionsKey: &taiji.RunOptions{
			TaijiContext: map[string]any{"test": "test"},
		},
	}))
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}

	// Process response.
	return c.processResponse(eventChan)
}

// processResponse handles both streaming and non-streaming responses.
func (c *multiTurnChat) processResponse(eventChan <-chan *event.Event) error {
	fmt.Print("🤖 Assistant: ")

	var (
		fullContent      string
		assistantStarted bool
		lastEvent        *event.Event
	)

	for event := range eventChan {
		if err := c.handleEvent(event, &assistantStarted, &fullContent); err != nil {
			return err
		}

		lastEvent = event

		// Check if this is the final event.
		if event.Done {
			fmt.Printf("\n")
			break
		}
	}

	// Print timing info after response is complete
	c.printTimingInfo(lastEvent)

	return nil
}

// handleEvent processes a single event from the event channel.
func (c *multiTurnChat) handleEvent(
	evt *event.Event,
	assistantStarted *bool,
	fullContent *string,
) error {
	// Handle errors - check Response.Error explicitly to handle embedded struct properly.
	if evt.Response != nil && evt.Response.Error != nil {
		return errors.New(evt.Response.Error.Message)
	}

	// Handle content.
	c.handleContent(evt, assistantStarted, fullContent)

	return nil
}

// handleContent processes and displays content.
func (c *multiTurnChat) handleContent(
	evt *event.Event,
	assistantStarted *bool,
	fullContent *string,
) {
	if len(evt.Choices) > 0 {
		choice := evt.Choices[0]
		content, reasoningContent := c.extractContent(choice)
		c.displayContent(content, reasoningContent, assistantStarted, fullContent)
	}
}

// extractContent extracts content based on streaming mode.
func (c *multiTurnChat) extractContent(choice model.Choice) (string, string) {
	if c.streaming {
		// Streaming mode: use delta content.
		return choice.Delta.Content, choice.Delta.ReasoningContent
	}
	// Non-streaming mode: use full message content.
	return choice.Message.Content, choice.Message.ReasoningContent
}

// displayContent prints content to console.
func (c *multiTurnChat) displayContent(
	content string,
	reasoningContent string,
	assistantStarted *bool,
	fullContent *string,
) {
	if !*assistantStarted {
		*assistantStarted = true
	}

	// Print reasoning content in gray color
	if reasoningContent != "" {
		fmt.Printf("\033[90m%s\033[0m", reasoningContent)
	}

	fmt.Print(content)
	*fullContent += content
}

// startNewSession creates a new session ID.
func (c *multiTurnChat) startNewSession() {
	oldSessionID := c.sessionID
	c.sessionID = fmt.Sprintf("taiji-chat-session-%d", time.Now().Unix())
	fmt.Printf("🆕 Started new session!\n")
	fmt.Printf("   Previous: %s\n", oldSessionID)
	fmt.Printf("   Current:  %s\n", c.sessionID)
	fmt.Printf("   (Conversation history has been reset)\n")
	fmt.Println()
}

// printTimingInfo displays timing information from the final event.
func (c *multiTurnChat) printTimingInfo(event *event.Event) {
	if event == nil || event.Response == nil || event.Response.Usage == nil || event.Response.Usage.TimingInfo == nil {
		return
	}

	timing := event.Response.Usage.TimingInfo
	colorYellow := "\033[33m"
	colorReset := "\033[0m"

	fmt.Printf("\n%s⏱️  Timing Info:%s\n", colorYellow, colorReset)

	if timing.FirstTokenDuration > 0 {
		fmt.Printf("%s   • Time to first token: %v%s\n", colorYellow, timing.FirstTokenDuration, colorReset)
	}

	if timing.ReasoningDuration > 0 {
		fmt.Printf("%s   • Reasoning duration: %v%s\n", colorYellow, timing.ReasoningDuration, colorReset)
	}
}

func getEnvOrDefault(envVar string, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}
