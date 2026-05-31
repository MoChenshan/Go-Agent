package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool/transfer"
)

var (
	botAppKey = flag.String("bot_app_key", "lke-basic-demo", "LKE bot app key")
	endpoint  = flag.String("endpoint", "", "LKE endpoint override (optional)")
	mock      = flag.Bool("mock", true, "enable LKE SDK mock mode")

	userID    = flag.String("user_id", "demo-user", "user id")
	sessionID = flag.String("session_id", "", "session id (optional)")
	query     = flag.String("query", "请使用 local_action 工具处理输入 {\"input\":\"hello\"}，并返回结果。", "user query")
)

func main() {
	flag.Parse()

	ctx := context.Background()
	sid := *sessionID
	if sid == "" {
		sid = fmt.Sprintf("lke-basic-%d", time.Now().UnixNano())
	}

	setup, err := newLKEClientSetup(*endpoint)
	if err != nil {
		log.Fatalf("init setup failed: %v", err)
	}

	subAgent := newLKESubAgent(*botAppKey, *mock, setup)
	mainAgent := llmagent.New(
		"main-agent",
		llmagent.WithDescription("Main agent that delegates tasks to a sub agent"),
		llmagent.WithInstruction("Delegate the user request to an appropriate sub agent."),
		llmagent.WithEnableCodeExecutionResponseProcessor(false),
		llmagent.WithModel(&mockTransferOnlyModel{targetAgent: subAgent.Info().Name}),
		llmagent.WithSubAgents([]agent.Agent{subAgent}),
	)

	r := runner.NewRunner("lke-basic", mainAgent)
	eventChan, err := r.Run(ctx, *userID, sid, model.NewUserMessage(*query), agent.WithRequestID(sid))
	if err != nil {
		log.Fatalf("runner execution failed: %v", err)
	}

	printEvents(eventChan)
}

func printEvents(eventChan <-chan *event.Event) {
	for evt := range eventChan {
		if evt == nil {
			continue
		}
		if evt.Error != nil {
			log.Printf("error: %s", evt.Error.Message)
			continue
		}
		if evt.Response == nil || len(evt.Response.Choices) == 0 {
			continue
		}

		choice := evt.Response.Choices[0]
		switch evt.Response.Object {
		case model.ObjectTypeToolResponse:
			if len(choice.Message.ToolCalls) > 0 {
				log.Printf("tool call: %s", choice.Message.ToolCalls[0].Function.Name)
			} else if choice.Message.Content != "" {
				log.Printf("tool result: %s", choice.Message.Content)
			}
		case model.ObjectTypeTransfer:
			if choice.Message.Content != "" {
				log.Printf("%s", choice.Message.Content)
			}
		default:
			if choice.Delta.Content != "" {
				fmt.Print(choice.Delta.Content)
			} else if choice.Message.Content != "" {
				fmt.Print(choice.Message.Content)
			}
		}

		if evt.Done {
			fmt.Println()
			break
		}
	}
}

type mockTransferOnlyModel struct {
	targetAgent string
}

func (m *mockTransferOnlyModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	args, err := json.Marshal(&transfer.Request{
		AgentName: m.targetAgent,
	})
	if err != nil {
		return nil, err
	}

	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		ID:        fmt.Sprintf("mock-%d", time.Now().UnixNano()),
		Object:    model.ObjectTypeChatCompletion,
		Created:   time.Now().Unix(),
		Model:     m.Info().Name,
		Timestamp: time.Now(),
		Choices: []model.Choice{
			{
				Index: 0,
				Message: model.Message{
					Role: model.RoleAssistant,
					ToolCalls: []model.ToolCall{
						{
							Type: "function",
							ID:   fmt.Sprintf("toolcall-%d", time.Now().UnixNano()),
							Function: model.FunctionDefinitionParam{
								Name:      transfer.TransferToolName,
								Arguments: args,
							},
						},
					},
				},
			},
		},
	}
	close(ch)
	return ch, nil
}

func (m *mockTransferOnlyModel) Info() model.Info { return model.Info{Name: "mock-transfer-model"} }
