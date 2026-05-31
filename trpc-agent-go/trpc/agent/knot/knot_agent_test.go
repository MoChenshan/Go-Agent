package knotagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func collectKnotEvents(eventChan <-chan *event.Event) []*event.Event {
	events := make([]*event.Event, 0)
	for evt := range eventChan {
		events = append(events, evt)
	}
	return events
}

func TestRunTrimsSSEDataPayload(t *testing.T) {
	payload, err := json.Marshal(&knotChatResponse{
		Type:  events.EventTypeTextMessageContent,
		Delta: "hello",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: \t%s  \n\n", payload)
		fmt.Fprint(w, "data:  [DONE]\t \n\n")
	}))
	defer server.Close()

	ag := New(
		"knot-test",
		WithKnotApiUrl(server.URL),
		WithKnotApiKey("token"),
		WithKnotApiUser("user"),
		WithKnotModel("model"),
	)
	invocation := &agent.Invocation{
		InvocationID: "inv-1",
		Message:      model.Message{Role: model.RoleUser, Content: "hi"},
	}

	eventChan, err := ag.Run(context.Background(), invocation)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gotEvents := collectKnotEvents(eventChan)

	if len(gotEvents) != 1 {
		t.Fatalf("len(events) = %d, want 1: %+v", len(gotEvents), gotEvents)
	}
	if gotEvents[0].Error != nil {
		t.Fatalf("unexpected error event: %+v", gotEvents[0].Error)
	}
	if got := gotEvents[0].Response.Choices[0].Delta.Content; got != "hello" {
		t.Fatalf("delta content = %q, want %q", got, "hello")
	}
}
