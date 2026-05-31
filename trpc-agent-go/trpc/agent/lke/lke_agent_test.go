package lke

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	lkesdk "github.com/tencent-lke/lke-sdk-go"
	"github.com/tencent-lke/lke-sdk-go/agentastool"
	lkeevent "github.com/tencent-lke/lke-sdk-go/event"
	lkeeventhandler "github.com/tencent-lke/lke-sdk-go/eventhandler"
	"github.com/tencent-lke/lke-sdk-go/mcpserversse"
	lkemodel "github.com/tencent-lke/lke-sdk-go/model"
	"github.com/tencent-lke/lke-sdk-go/runlog"
	lketool "github.com/tencent-lke/lke-sdk-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

type fakeLKEClient struct {
	handler            lkeeventhandler.EventHandler
	runWithContextFunc func(ctx context.Context, query string, options *lkemodel.Options) (*lkeevent.ReplyEvent, error)
	closeCount         int32
}

func (f *fakeLKEClient) AddFunctionTools(_ string, _ []*lketool.FunctionTool) {}

func (f *fakeLKEClient) AddMcpTools(_ string, _ *mcpserversse.McpServerSse,
	_ []string) (addTools []*lketool.McpTool, err error) {
	return nil, nil
}

func (f *fakeLKEClient) AddAgentAsTool(_ string, _ string, _ string,
	_ string) (addtool *agentastool.AgentAsTool, err error) {
	return nil, nil
}

func (f *fakeLKEClient) AddAgents(_ []lkemodel.Agent) {}

func (f *fakeLKEClient) AddHandoffs(_ string, _ []string) {}

func (f *fakeLKEClient) Run(query string,
	options *lkemodel.Options) (finalReply *lkeevent.ReplyEvent, err error) {
	return f.RunWithContext(context.Background(), query, options)
}

func (f *fakeLKEClient) RunWithContext(ctx context.Context, query string,
	options *lkemodel.Options) (finalReply *lkeevent.ReplyEvent, err error) {
	if f.runWithContextFunc != nil {
		return f.runWithContextFunc(ctx, query, options)
	}
	return &lkeevent.ReplyEvent{IsFinal: true, Content: "ok"}, nil
}

func (f *fakeLKEClient) Close() {
	atomic.AddInt32(&f.closeCount, 1)
}

func (f *fakeLKEClient) Open() {}

func (f *fakeLKEClient) GetBotAppKey() string { return "" }

func (f *fakeLKEClient) GetEndpoint() string { return "" }

func (f *fakeLKEClient) SetBotAppKey(_ string) {}

func (f *fakeLKEClient) SetEndpoint(_ string) {}

func (f *fakeLKEClient) SetEventHandler(eventHandler lkeeventhandler.EventHandler) {
	f.handler = eventHandler
}

func (f *fakeLKEClient) SetMock(_ bool) {}

func (f *fakeLKEClient) SetEnableSystemOpt(_ bool) {}

func (f *fakeLKEClient) SetStartAgent(_ string) {}

func (f *fakeLKEClient) SetHttpClient(_ *http.Client) {}

func (f *fakeLKEClient) SetMaxToolTurns(_ uint) {}

func (f *fakeLKEClient) SetToolRunTimeout(_ time.Duration) {}

func (f *fakeLKEClient) SetRunLogger(_ runlog.RunLogger) {}

func collectEvents(eventChan <-chan *event.Event) []*event.Event {
	events := make([]*event.Event, 0)
	for evt := range eventChan {
		events = append(events, evt)
	}
	return events
}

func TestRunForwardsQueryAndEmitsEvent(t *testing.T) {
	client := &fakeLKEClient{}
	var gotQuery string
	client.runWithContextFunc = func(ctx context.Context, query string,
		options *lkemodel.Options) (*lkeevent.ReplyEvent, error) {
		gotQuery = query
		if client.handler != nil {
			client.handler.OnReply(&lkeevent.ReplyEvent{
				IsFinal:    true,
				IsFromSelf: false,
				Content:    "hello",
			})
		}
		return &lkeevent.ReplyEvent{
			IsFinal:    true,
			IsFromSelf: false,
			Content:    "hello",
		}, nil
	}

	ag := New(
		"test-app-key",
		WithName("test-lke"),
		WithEventBypass(true),
		WithClientBuilder(func(ctx context.Context, invocation *agent.Invocation) (lkesdk.LkeClient, error) {
			return client, nil
		}),
	)
	invocation := &agent.Invocation{
		InvocationID: "inv-1",
		Session:      session.NewSession("app", "user-1", "sess-1"),
		Message:      model.NewUserMessage("hi"),
	}

	eventChan, err := ag.Run(context.Background(), invocation)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	events := collectEvents(eventChan)

	if gotQuery != "hi" {
		t.Fatalf("query = %q, want %q", gotQuery, "hi")
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Response == nil || events[0].Response.Object != model.ObjectTypeChatCompletion {
		t.Fatalf("unexpected response object: %+v", events[0].Response)
	}
}

func TestRunEmitsFallbackFinalReplyWhenNoReplyCallback(t *testing.T) {
	client := &fakeLKEClient{}
	client.runWithContextFunc = func(ctx context.Context, query string,
		options *lkemodel.Options) (*lkeevent.ReplyEvent, error) {
		return &lkeevent.ReplyEvent{
			IsFinal:    true,
			IsFromSelf: false,
			Content:    "fallback-final",
		}, nil
	}

	ag := New(
		"test-app-key",
		WithName("test-lke"),
		WithEventBypass(true),
		WithClientBuilder(func(ctx context.Context, invocation *agent.Invocation) (lkesdk.LkeClient, error) {
			return client, nil
		}),
	)
	invocation := &agent.Invocation{
		InvocationID: "inv-2",
		Session:      session.NewSession("app", "user-2", "sess-2"),
		Message:      model.NewUserMessage("hi"),
	}

	eventChan, err := ag.Run(context.Background(), invocation)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	events := collectEvents(eventChan)

	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Response == nil || len(events[0].Response.Choices) == 0 {
		t.Fatalf("unexpected response: %+v", events[0].Response)
	}
	if events[0].Response.Choices[0].Message.Content != "fallback-final" {
		t.Fatalf("final content = %q, want %q", events[0].Response.Choices[0].Message.Content, "fallback-final")
	}
}

func TestRunWithEmptyQueryReturnsErrorEvent(t *testing.T) {
	client := &fakeLKEClient{}
	runCalls := int32(0)
	client.runWithContextFunc = func(ctx context.Context, query string,
		options *lkemodel.Options) (*lkeevent.ReplyEvent, error) {
		atomic.AddInt32(&runCalls, 1)
		return &lkeevent.ReplyEvent{IsFinal: true, Content: "should-not-run"}, nil
	}

	ag := New(
		"test-app-key",
		WithName("test-lke"),
		WithEventBypass(true),
		WithClientBuilder(func(ctx context.Context, invocation *agent.Invocation) (lkesdk.LkeClient, error) {
			return client, nil
		}),
	)
	invocation := &agent.Invocation{
		InvocationID: "inv-3",
		Session:      session.NewSession("app", "user-3", "sess-3"),
		Message:      model.Message{Role: model.RoleUser},
	}

	eventChan, err := ag.Run(context.Background(), invocation)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	events := collectEvents(eventChan)

	if atomic.LoadInt32(&runCalls) != 0 {
		t.Fatalf("runWithContext was called, want 0")
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Response == nil || events[0].Response.Object != model.ObjectTypeError {
		t.Fatalf("unexpected response object: %+v", events[0].Response)
	}
}

func TestRunReturnsErrorWhenAgentClosed(t *testing.T) {
	client := &fakeLKEClient{}
	ag := New(
		"test-app-key",
		WithName("test-lke"),
		WithEventBypass(true),
		WithClientBuilder(func(ctx context.Context, invocation *agent.Invocation) (lkesdk.LkeClient, error) {
			return client, nil
		}),
	)

	lkeAg, ok := ag.(*Agent)
	if !ok {
		t.Fatal("type assertion to *Agent failed")
	}
	if err := lkeAg.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err := ag.Run(context.Background(), &agent.Invocation{
		InvocationID: "inv-4",
		Session:      session.NewSession("app", "user-4", "sess-4"),
		Message:      model.NewUserMessage("hi"),
	})
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
}

func TestRunOptionsOverrideAndEventMetadata(t *testing.T) {
	client := &fakeLKEClient{}
	runtimeLKEOpts := &lkemodel.Options{
		RequestID: "lke-request-id",
	}

	var gotOptions *lkemodel.Options
	client.runWithContextFunc = func(ctx context.Context, query string,
		options *lkemodel.Options) (*lkeevent.ReplyEvent, error) {
		gotOptions = options
		if client.handler != nil {
			client.handler.OnReply(&lkeevent.ReplyEvent{
				IsFinal:    true,
				IsFromSelf: false,
				Content:    "hello-metadata",
			})
		}
		return &lkeevent.ReplyEvent{
			IsFinal:    true,
			IsFromSelf: false,
			Content:    "hello-metadata",
		}, nil
	}

	invocation := agent.NewInvocation(
		agent.WithInvocationID("inv-meta"),
		agent.WithInvocationBranch("branch-main"),
		agent.WithInvocationEventFilterKey("app-main"),
		agent.WithInvocationSession(session.NewSession("app", "user-default", "sess-meta")),
		agent.WithInvocationMessage(model.NewUserMessage("hi")),
		agent.WithInvocationRunOptions(agent.RunOptions{
			RequestID: "req-meta",
		}),
	)

	ag := New(
		"test-app-key",
		WithName("test-lke"),
		WithDefaultRunOptions(&lkemodel.Options{RequestID: "default"}),
		WithRunOptionsFactory(func(ctx context.Context, invocation *agent.Invocation) (*lkemodel.Options, error) {
			return runtimeLKEOpts, nil
		}),
		WithEventBypass(true),
		WithClientBuilder(func(ctx context.Context, invocation *agent.Invocation) (lkesdk.LkeClient, error) {
			return client, nil
		}),
	)

	eventChan, err := ag.Run(context.Background(), invocation)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	events := collectEvents(eventChan)

	if gotOptions != runtimeLKEOpts {
		t.Fatalf("run options LKEOptions not applied")
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].RequestID != "req-meta" {
		t.Fatalf("event requestID = %q, want %q", events[0].RequestID, "req-meta")
	}
	if events[0].InvocationID != "inv-meta" {
		t.Fatalf("event invocationID = %q, want %q", events[0].InvocationID, "inv-meta")
	}
	if events[0].Branch != "branch-main" {
		t.Fatalf("event branch = %q, want %q", events[0].Branch, "branch-main")
	}
	if events[0].FilterKey != "app-main" {
		t.Fatalf("event filterKey = %q, want %q", events[0].FilterKey, "app-main")
	}
}
