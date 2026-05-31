// Package main implements a simple A2A server example (configured by trpc_go.yaml).
package main

import (
	"context"
	"flag"
	"fmt"

	"git.code.oa.com/trpc-go/trpc-go"
	a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	// Import zhiyan-llm plugin for telemetry (plugin init registers telemetry.zhiyan-llm).
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

func main() {
	modelName := flag.String("model", "deepseek-chat", "Name of the model to use")
	flag.Parse()

	s := trpc.NewServer() // reads ./trpc_go.yaml by default

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

	if err := s.Serve(); err != nil {
		log.Fatal(err)
	}
}

var _ taskmanager.MessageProcessor = (*simpleTaskProcessor)(nil)

// simpleTaskProcessor implements the taskmanager.MessageProcessor interface.
type simpleTaskProcessor struct {
	c *chainChat
}

func (p *simpleTaskProcessor) ProcessMessage(
	ctx context.Context,
	message protocol.Message,
	_ taskmanager.ProcessOptions,
	_ taskmanager.TaskHandler,
) (*taskmanager.MessageProcessingResult, error) {
	text := extractText(message)
	log.Infof("Processing message %s with input: %s", message.MessageID, text)

	result, err := p.c.startChat(ctx, text)
	msg := protocol.NewMessage(
		protocol.MessageRoleAgent,
		[]protocol.Part{protocol.NewTextPart(fmt.Sprintf("Processed result: %s, err: %v", result, err))},
	)
	return &taskmanager.MessageProcessingResult{Result: &msg}, nil
}

func extractText(message protocol.Message) string {
	for _, part := range message.Parts {
		if textPart, ok := part.(*protocol.TextPart); ok {
			return textPart.Text
		}
	}
	return ""
}

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }

func createA2AServer(url string, cc *chainChat) *server.A2AServer {
	agentCard := server.AgentCard{
		Name:        "Multi-Agent Chain Server (Zhiyan LLM telemetry)",
		Description: "A2A example server exporting LLM traces via telemetry.zhiyan-llm",
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

	processor := &simpleTaskProcessor{c: cc}
	taskManager, err := taskmanager.NewMemoryTaskManager(processor)
	if err != nil {
		log.Fatalf("Failed to create task manager: %v", err)
	}

	srv, err := server.NewA2AServer(agentCard, taskManager)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	return srv
}
