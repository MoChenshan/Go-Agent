// Package main implements a simple A2A server example.
package main

import (
	"context"
	"flag"
	"fmt"

	"git.code.oa.com/trpc-go/trpc-go"
	a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
	"trpc.group/trpc-go/trpc-agent-go/log"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	// Import galileo for metric and trace OpenTelemetry integration
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
)

func main() {
	// Parse command line flags.
	modelName := flag.String("model", "deepseek-chat", "Name of the model to use")
	flag.Parse()
	s := trpc.NewServer()
	url := "http://localhost:8080/"
	if host := a2atrpc.GetServiceHost("trpc.app.app.agent"); host != "" {
		url = fmt.Sprintf("http://%s/", host)
	}
	c, err := newChainChat(context.Background(), *modelName)
	if err != nil {
		log.Fatal(err)
	}
	if err := a2atrpc.RegisterA2AServer(s, "trpc.app.app.agent", createA2AServer(url, c)); err != nil {
		log.Fatal(err)
	}
	// Attributes represent additional key-value descriptors that can be bound
	// to a metric observer or recorder.
	fmt.Printf("log.Default Type: %T", log.Default)
	if err != nil {
		log.Fatal(err)
	}

	if err := s.Serve(); err != nil {
		log.Fatal(err)
	}
}

var _ taskmanager.MessageProcessor = (*simpleTaskProcessor)(nil)

// simpleTaskProcessor implements the taskmanager.TaskProcessor interface.
type simpleTaskProcessor struct {
	c *chainChat
}

func (p *simpleTaskProcessor) ProcessMessage(
	ctx context.Context,
	message protocol.Message,
	options taskmanager.ProcessOptions,
	taskHandler taskmanager.TaskHandler,
) (*taskmanager.MessageProcessingResult, error) {
	text := extractText(message)
	log.Infof("Processing message %s with input: %s", message.MessageID, text)

	result, err := p.c.startChat(ctx, text)
	msg := protocol.NewMessage(
		protocol.MessageRoleAgent,
		[]protocol.Part{protocol.NewTextPart(fmt.Sprintf("Processed result: %s, err: %v", result, err))},
	)

	return &taskmanager.MessageProcessingResult{
		Result:          &msg,
		StreamingEvents: nil,
	}, nil
}

// extractText extracts the text content from a message.
func extractText(message protocol.Message) string {
	for _, part := range message.Parts {
		if textPart, ok := part.(*protocol.TextPart); ok {
			return textPart.Text
		}
	}
	return ""
}

// Helper function to create string pointers.
func stringPtr(s string) *string {
	return &s
}

// Helper function to create bool pointers.
func boolPtr(b bool) *bool {
	return &b
}

func createA2AServer(url string, cc *chainChat) *server.A2AServer {
	// Create the agent card.
	agentCard := server.AgentCard{
		Name:        "Multi-Agent Chain Server",
		Description: "A2A example server that writes Research",
		URL:         url,
		Version:     "1.0.0",
		Provider: &server.AgentProvider{
			Organization: "tRPC-A2A-Go Examples",
			URL:          &url,
		},
		Capabilities: server.AgentCapabilities{
			Streaming:              boolPtr(false),
			StateTransitionHistory: boolPtr(true),
		},
		DefaultInputModes:  []string{string(protocol.KindText)},
		DefaultOutputModes: []string{string(protocol.KindText)},
		Skills: []server.AgentSkill{
			{
				ID:          "write_research",
				Name:        "Write Research",
				Description: stringPtr("Write Research"),
				Tags:        []string{"text", "processing"},
				Examples:    []string{"write large language model summary"},
				InputModes:  []string{string(protocol.KindText)},
				OutputModes: []string{string(protocol.KindText)},
			},
		},
	}

	// Create the task processor.
	processor := &simpleTaskProcessor{c: cc}

	// Create task manager and inject processor.
	taskManager, err := taskmanager.NewMemoryTaskManager(processor)
	if err != nil {
		log.Fatalf("Failed to create task manager: %v", err)
	}

	// Create the server.
	srv, err := server.NewA2AServer(agentCard, taskManager)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	return srv
}
