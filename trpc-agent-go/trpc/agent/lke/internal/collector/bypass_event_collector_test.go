package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	lkeevent "github.com/tencent-lke/lke-sdk-go/event"
	lkeeventhandler "github.com/tencent-lke/lke-sdk-go/eventhandler"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type testEventHandler struct {
	replyCount     int
	errorCount     int
	thoughtCount   int
	referenceCount int
	tokenStatCount int
	beforeTool     int
	afterTool      int
}

func (h *testEventHandler) OnError(_ *lkeevent.ErrorEvent) {
	h.errorCount++
}

func (h *testEventHandler) OnReply(_ *lkeevent.ReplyEvent) {
	h.replyCount++
}

func (h *testEventHandler) OnThought(_ *lkeevent.AgentThoughtEvent) {
	h.thoughtCount++
}

func (h *testEventHandler) OnReference(_ *lkeevent.ReferenceEvent) {
	h.referenceCount++
}

func (h *testEventHandler) OnTokenStat(_ *lkeevent.TokenStatEvent) {
	h.tokenStatCount++
}

func (h *testEventHandler) BeforeToolCallHook(_ lkeeventhandler.ToolCallContext) {
	h.beforeTool++
}

func (h *testEventHandler) AfterToolCallHook(_ lkeeventhandler.ToolCallContext) {
	h.afterTool++
}

func newTestInvocation() *agent.Invocation {
	return agent.NewInvocation(
		agent.WithInvocationID("inv-collector"),
		agent.WithInvocationBranch("branch-collector"),
		agent.WithInvocationEventFilterKey("filter-collector"),
		agent.WithInvocationRunOptions(agent.RunOptions{
			RequestID: "req-collector",
		}),
	)
}

func mustReadEvent(t *testing.T, ch <-chan *event.Event) *event.Event {
	t.Helper()
	select {
	case evt := <-ch:
		if evt == nil {
			t.Fatal("received nil event")
		}
		return evt
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

func mustNoEvent(t *testing.T, ch <-chan *event.Event) {
	t.Helper()
	select {
	case evt := <-ch:
		t.Fatalf("unexpected event: %+v", evt)
	case <-time.After(120 * time.Millisecond):
	}
}

func assertEventMeta(t *testing.T, evt *event.Event) {
	t.Helper()
	if evt.RequestID != "req-collector" {
		t.Fatalf("requestID = %q, want %q", evt.RequestID, "req-collector")
	}
	if evt.InvocationID != "inv-collector" {
		t.Fatalf("invocationID = %q, want %q", evt.InvocationID, "inv-collector")
	}
	if evt.Branch != "branch-collector" {
		t.Fatalf("branch = %q, want %q", evt.Branch, "branch-collector")
	}
	if evt.FilterKey != "filter-collector" {
		t.Fatalf("filterKey = %q, want %q", evt.FilterKey, "filter-collector")
	}
}

func TestBypassEventCollectorOnReplyEmitsFinalEvent(t *testing.T) {
	original := &testEventHandler{}
	collector := New(original, true, "lke-test", false)
	eventChan := make(chan *event.Event, 8)
	collector.StartProcessing(context.Background(), eventChan, newTestInvocation())

	collector.OnReply(&lkeevent.ReplyEvent{
		IsFinal:    true,
		IsFromSelf: false,
		Content:    "final-reply",
	})

	evt := mustReadEvent(t, eventChan)
	if evt.Response == nil || evt.Response.Object != model.ObjectTypeChatCompletion {
		t.Fatalf("unexpected response object: %+v", evt.Response)
	}
	if len(evt.Response.Choices) == 0 || evt.Response.Choices[0].Message.Content != "final-reply" {
		t.Fatalf("unexpected response content: %+v", evt.Response.Choices)
	}
	assertEventMeta(t, evt)

	if !collector.HasFinalReplyEvent() {
		t.Fatal("HasFinalReplyEvent() = false, want true")
	}
	if original.replyCount != 1 {
		t.Fatalf("original OnReply count = %d, want 1", original.replyCount)
	}

	collector.StopProcessing()
	if collector.HasFinalReplyEvent() {
		t.Fatal("HasFinalReplyEvent() = true after StopProcessing, want false")
	}
}

func TestBypassEventCollectorOnReplySkipsNonFinalAndSelf(t *testing.T) {
	original := &testEventHandler{}
	collector := New(original, true, "lke-test", false)
	eventChan := make(chan *event.Event, 8)
	collector.StartProcessing(context.Background(), eventChan, newTestInvocation())
	defer collector.StopProcessing()

	collector.OnReply(&lkeevent.ReplyEvent{
		IsFinal:    false,
		IsFromSelf: false,
		Content:    "partial",
	})
	collector.OnReply(&lkeevent.ReplyEvent{
		IsFinal:    true,
		IsFromSelf: true,
		Content:    "self-final",
	})

	mustNoEvent(t, eventChan)
	if collector.HasFinalReplyEvent() {
		t.Fatal("HasFinalReplyEvent() = true, want false")
	}
	if original.replyCount != 2 {
		t.Fatalf("original OnReply count = %d, want 2", original.replyCount)
	}
}

func TestBypassEventCollectorOnErrorEmitsEvent(t *testing.T) {
	original := &testEventHandler{}
	collector := New(original, true, "lke-test", false)
	eventChan := make(chan *event.Event, 8)
	collector.StartProcessing(context.Background(), eventChan, newTestInvocation())

	collector.OnError(&lkeevent.ErrorEvent{
		Error: lkeevent.Error{
			Code:    418,
			Message: "teapot",
		},
	})

	evt := mustReadEvent(t, eventChan)
	if evt.Response == nil || evt.Response.Object != model.ObjectTypeError || evt.Response.Error == nil {
		t.Fatalf("unexpected error event: %+v", evt.Response)
	}
	if !strings.Contains(evt.Response.Error.Message, "teapot") || !strings.Contains(evt.Response.Error.Message, "418") {
		t.Fatalf("error message = %q, want contain teapot and 418", evt.Response.Error.Message)
	}
	assertEventMeta(t, evt)

	if original.errorCount != 1 {
		t.Fatalf("original OnError count = %d, want 1", original.errorCount)
	}

	collector.StopProcessing()
	collector.OnError(&lkeevent.ErrorEvent{Error: lkeevent.Error{Code: 500, Message: "after-stop"}})
	mustNoEvent(t, eventChan)
}

func TestBypassEventCollectorOnThoughtEmitsEvent(t *testing.T) {
	original := &testEventHandler{}
	collector := New(original, true, "lke-test", false)
	eventChan := make(chan *event.Event, 8)
	collector.StartProcessing(context.Background(), eventChan, newTestInvocation())
	defer collector.StopProcessing()

	collector.OnThought(&lkeevent.AgentThoughtEvent{
		Procedures: []lkeevent.AgentProcedure{
			{
				Debugging: lkeevent.AgentProcedureDebugging{
					Content: "thinking-step",
				},
			},
		},
	})

	evt := mustReadEvent(t, eventChan)
	if evt.Response == nil || evt.Response.Object != model.ObjectTypePreprocessingBasic {
		t.Fatalf("unexpected response object: %+v", evt.Response)
	}
	if len(evt.Response.Choices) == 0 || evt.Response.Choices[0].Message.Content != "thinking-step" {
		t.Fatalf("unexpected thought content: %+v", evt.Response.Choices)
	}
	assertEventMeta(t, evt)

	collector.OnThought(&lkeevent.AgentThoughtEvent{})
	mustNoEvent(t, eventChan)

	if original.thoughtCount != 2 {
		t.Fatalf("original OnThought count = %d, want 2", original.thoughtCount)
	}
}

func TestBypassEventCollectorToolHooksEmitEvents(t *testing.T) {
	original := &testEventHandler{}
	collector := New(original, true, "lke-test", false)
	eventChan := make(chan *event.Event, 8)
	collector.StartProcessing(context.Background(), eventChan, newTestInvocation())
	defer collector.StopProcessing()

	collector.BeforeToolCallHook(lkeeventhandler.ToolCallContext{
		CallToolName: "download_file",
		CallId:       "call-1",
		Input: map[string]any{
			"url": "https://example.com/a.txt",
		},
	})

	startEvt := mustReadEvent(t, eventChan)
	if startEvt.Response == nil || startEvt.Response.Object != model.ObjectTypeToolResponse {
		t.Fatalf("unexpected start tool event: %+v", startEvt.Response)
	}
	if len(startEvt.Response.Choices) == 0 || len(startEvt.Response.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("unexpected tool call payload: %+v", startEvt.Response.Choices)
	}
	if startEvt.Response.Choices[0].Message.ToolCalls[0].Function.Name != "download_file" {
		t.Fatalf("tool name = %q, want %q",
			startEvt.Response.Choices[0].Message.ToolCalls[0].Function.Name, "download_file")
	}
	assertEventMeta(t, startEvt)

	collector.AfterToolCallHook(lkeeventhandler.ToolCallContext{
		CallToolName: "download_file",
		CallId:       "call-1",
		Output:       "ok",
	})

	successEvt := mustReadEvent(t, eventChan)
	if successEvt.Response == nil || successEvt.Response.Object != model.ObjectTypeToolResponse {
		t.Fatalf("unexpected success tool event: %+v", successEvt.Response)
	}
	if len(successEvt.Response.Choices) == 0 {
		t.Fatalf("missing success choice: %+v", successEvt.Response)
	}
	successMessage := successEvt.Response.Choices[0].Message
	if successMessage.ToolID != "call-1" || successMessage.ToolName != "download_file" {
		t.Fatalf("unexpected tool result metadata: %+v", successMessage)
	}
	if !strings.Contains(successMessage.Content, "succeeded") {
		t.Fatalf("success content = %q, want contain %q", successMessage.Content, "succeeded")
	}

	collector.AfterToolCallHook(lkeeventhandler.ToolCallContext{
		CallToolName: "download_file",
		CallId:       "call-2",
		Err:          errors.New("boom"),
	})
	failEvt := mustReadEvent(t, eventChan)
	if len(failEvt.Response.Choices) == 0 || !strings.Contains(failEvt.Response.Choices[0].Message.Content, "failed") {
		t.Fatalf("unexpected failed tool content: %+v", failEvt.Response.Choices)
	}

	if original.beforeTool != 1 {
		t.Fatalf("original BeforeToolCallHook count = %d, want 1", original.beforeTool)
	}
	if original.afterTool != 2 {
		t.Fatalf("original AfterToolCallHook count = %d, want 2", original.afterTool)
	}
}

func TestBypassEventCollectorClosedChannelDoesNotPanic(t *testing.T) {
	collector := New(&testEventHandler{}, true, "lke-test", false)
	eventChan := make(chan *event.Event, 1)
	close(eventChan)
	collector.StartProcessing(context.Background(), eventChan, newTestInvocation())
	defer collector.StopProcessing()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("unexpected panic: %v", r)
			}
		}()
		collector.OnError(&lkeevent.ErrorEvent{
			Error: lkeevent.Error{Code: 500, Message: "closed channel"},
		})
	}()
}

func TestBypassEventCollectorBypassDisabledSkipsEmit(t *testing.T) {
	original := &testEventHandler{}
	collector := New(original, false, "lke-test", false)
	eventChan := make(chan *event.Event, 4)
	collector.StartProcessing(context.Background(), eventChan, newTestInvocation())
	defer collector.StopProcessing()

	collector.OnError(&lkeevent.ErrorEvent{
		Error: lkeevent.Error{Code: 400, Message: "no bypass"},
	})
	mustNoEvent(t, eventChan)

	if original.errorCount != 1 {
		t.Fatalf("original OnError count = %d, want 1", original.errorCount)
	}
}
