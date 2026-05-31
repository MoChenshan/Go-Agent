//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/server"
	a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	a2a "trpc.group/trpc-go/trpc-agent-go/server/a2a"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

var (
	modelName = flag.String("model", "deepseek-chat", "Model to use")
	streaming = flag.Bool("streaming", true, "Streaming to use")
)

func main() {
	flag.Parse()

	server := trpc.NewServer()

	// Start remote a2a server
	host := runRemoteAgent(server, "agent_joker", "i am a remote agent, i can tell a joke")

	go func() {
		if err := server.Serve(); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)
	startChat(host)
}

func startChat(host string) {
	// build a2a trpc http handler
	trpcHTTPHandler := a2atrpc.NewA2ATRPCHTTPReqHandler("trpc.app.client.joker")

	// build a2a client options
	a2aClientOptions := []a2aclient.Option{a2aclient.WithHTTPReqHandler(trpcHTTPHandler)}

	// build a2a agent with custom a2a client options
	httpURL := fmt.Sprintf("http://%s", host)
	a2aAgent, err := a2aagent.New(
		a2aagent.WithAgentCardURL(httpURL),
		a2aagent.WithA2AClientExtraOptions(a2aClientOptions...),
	)

	if err != nil {
		fmt.Printf("Failed to create a2a agent: %v", err)
		return
	}

	card := a2aAgent.GetAgentCard()
	fmt.Printf("🤖 A2A Agent Connected\n")
	fmt.Printf("==================================================\n")
	fmt.Printf("Name:        %s\n", card.Name)
	fmt.Printf("Description: %s\n", card.Description)
	fmt.Printf("URL:         %s\n", httpURL)
	fmt.Printf("==================================================\n\n")

	sessionService := inmemory.NewSessionService()
	agentRunner := runner.NewRunner("a2a-chat", a2aAgent, runner.WithSessionService(sessionService))

	userID := "user1"
	sessionID := "session1"

	fmt.Printf("💬 Chat with the remote agent\n")
	fmt.Printf("Commands:\n")
	fmt.Printf("  /new  - Start a new session\n")
	fmt.Printf("  /exit - Quit the chat\n\n")

	for {
		if err := processMessage(agentRunner, userID, &sessionID); err != nil {
			if err.Error() == "exit" {
				fmt.Println("👋 Goodbye!")
				return
			}
			fmt.Printf("❌ Error: %v\n", err)
		}

		fmt.Println() // Add spacing between turns
	}
}

func processMessage(agentRunner runner.Runner, userID string, sessionID *string) error {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("You: ")
	if !scanner.Scan() {
		return fmt.Errorf("exit")
	}

	userInput := strings.TrimSpace(scanner.Text())
	if userInput == "" {
		return nil
	}

	switch strings.ToLower(userInput) {
	case "/exit", "exit":
		return fmt.Errorf("exit")
	case "/new", "new":
		*sessionID = startNewSession()
		return nil
	}

	fmt.Print("🤖 Agent: ")
	events, err := agentRunner.Run(context.Background(), userID, *sessionID, model.NewUserMessage(userInput))
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}
	if err := processResponse(events); err != nil {
		return fmt.Errorf("failed to process response: %w", err)
	}

	return nil
}

func startNewSession() string {
	newSessionID := fmt.Sprintf("session_%d", time.Now().UnixNano())
	fmt.Printf("🆕 Started new session: %s\n", newSessionID)
	fmt.Printf("   (Conversation history has been reset)\n")
	fmt.Println()
	return newSessionID
}

func runRemoteAgent(server *server.Server, agentName, desc string) string {
	// build a simple agent
	remoteAgent := buildRemoteAgent(agentName, desc)

	// get the host from the trpc config
	host := a2atrpc.GetServiceHost("trpc.app.agent.joker")

	// create a2a server
	a2aServer, err := a2a.New(
		a2a.WithHost(host),
		a2a.WithAgent(remoteAgent, *streaming),
	)
	if err != nil {
		log.Fatalf("Failed to create a2a server: %v", err)
	}

	// register the a2a server to the trpc server
	a2atrpc.RegisterA2AServer(server, "trpc.app.agent.joker", a2aServer)

	fmt.Printf("🚀 Remote A2A Agent Server Started\n")
	fmt.Printf("==================================================\n")
	fmt.Printf("Service:     trpc.app.agent.joker\n")
	fmt.Printf("Host:        %s\n", host)
	fmt.Printf("Agent Name:  %s\n", agentName)
	fmt.Printf("Description: %s\n", desc)
	fmt.Printf("==================================================\n\n")

	return host
}

func buildRemoteAgent(agentName, desc string) agent.Agent {
	// Create OpenAI model.
	modelInstance := openai.New(*modelName)

	// Create LLM agent with tools.
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(2000),
		Temperature: floatPtr(0.7),
		Stream:      *streaming,
	}
	llmAgent := llmagent.New(
		agentName,
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription(desc),
		llmagent.WithInstruction(desc),
		llmagent.WithGenerationConfig(genConfig),
	)

	return llmAgent
}

// processResponse handles both streaming and non-streaming responses with tool call visualization.
func processResponse(eventChan <-chan *event.Event) error {
	var (
		fullContent       string
		toolCallsDetected bool
		assistantStarted  bool
	)

	for event := range eventChan {
		if err := handleEvent(event, &toolCallsDetected, &assistantStarted, &fullContent); err != nil {
			return err
		}

		// Check if this is the final event.
		if event.Done && !isToolEvent(event) {
			fmt.Printf("\n")
			break
		}
	}

	return nil
}

// handleEvent processes a single event from the event channel.
func handleEvent(
	event *event.Event,
	toolCallsDetected *bool,
	assistantStarted *bool,
	fullContent *string,
) error {
	// Handle errors.
	if event.Error != nil {
		fmt.Printf("\n❌ Error: %s\n", event.Error.Message)
		return nil
	}

	// Handle tool calls.
	if handleToolCalls(event, toolCallsDetected, assistantStarted) {
		return nil
	}

	// Handle tool responses.
	if handleToolResponses(event) {
		return nil
	}

	// Handle content.
	handleContent(event, toolCallsDetected, assistantStarted, fullContent)

	return nil
}

// handleToolCalls detects and displays tool calls.
func handleToolCalls(
	event *event.Event,
	toolCallsDetected *bool,
	assistantStarted *bool,
) bool {
	if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
		*toolCallsDetected = true
		if *assistantStarted {
			fmt.Printf("\n")
		}
		fmt.Printf("🔧 CallableTool calls initiated:\n")
		for _, toolCall := range event.Choices[0].Message.ToolCalls {
			fmt.Printf("   • %s (ID: %s)\n", toolCall.Function.Name, toolCall.ID)
			if len(toolCall.Function.Arguments) > 0 {
				fmt.Printf("     Args: %s\n", string(toolCall.Function.Arguments))
			}
		}
		fmt.Printf("\n🔄 Executing tools...\n")
		return true
	}
	return false
}

// handleToolResponses detects and displays tool responses.
func handleToolResponses(event *event.Event) bool {
	if event.Response != nil && len(event.Response.Choices) > 0 {
		hasToolResponse := false
		for _, choice := range event.Response.Choices {
			// Handle traditional tool responses (Role: tool)
			if choice.Message.Role == model.RoleTool && choice.Message.ToolID != "" {
				fmt.Printf("✅ CallableTool response (ID: %s): %s\n",
					choice.Message.ToolID,
					strings.TrimSpace(choice.Message.Content))
				hasToolResponse = true
			}
		}
		if hasToolResponse {
			return true
		}
	}
	return false
}

// handleContent processes and displays content.
func handleContent(
	event *event.Event,
	toolCallsDetected *bool,
	assistantStarted *bool,
	fullContent *string,
) {
	if len(event.Choices) > 0 {
		choice := event.Choices[0]
		content := extractContent(choice)

		if content != "" {
			displayContent(content, toolCallsDetected, assistantStarted, fullContent)
		}
	}
}

// extractContent extracts content based on streaming mode.
func extractContent(choice model.Choice) string {
	if *streaming {
		return choice.Delta.Content
	}
	return choice.Message.Content
}

// displayContent prints content to console.
func displayContent(
	content string,
	toolCallsDetected *bool,
	assistantStarted *bool,
	fullContent *string,
) {
	if !*assistantStarted {
		if *toolCallsDetected {
			fmt.Printf("\n")
		}
		*assistantStarted = true
	}
	fmt.Print(content)
	*fullContent += content
}

// isToolEvent checks if an event is a tool response (not a final response).
func isToolEvent(event *event.Event) bool {
	if event.Response == nil {
		return false
	}
	if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
		return true
	}
	if len(event.Choices) > 0 && event.Choices[0].Message.ToolID != "" {
		return true
	}

	// Check if this is a tool response by examining choices.
	for _, choice := range event.Response.Choices {
		if choice.Message.Role == model.RoleTool {
			return true
		}
	}

	return false
}

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}
