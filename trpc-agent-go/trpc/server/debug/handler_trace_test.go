package debug

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug/internal/schema"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func TestHandleEventTrace_LLMRequestFormatted(t *testing.T) {
	reqModel := model.Request{
		Messages: []model.Message{
			{
				Role:    model.RoleUser,
				Content: "hello world",
			},
		},
	}
	payload, err := json.Marshal(reqModel)
	assert.NoError(t, err)

	attrs := attribute.NewSet(
		attribute.String("trace_id", "trace-1"),
		attribute.String("span_id", "span-1"),
		attribute.String(keyEventID, "event-1"),
		attribute.String(keyLLMRequest, string(payload)),
	)

	server := &Server{
		agents:         map[string]agent.Agent{},
		router:         mux.NewRouter(),
		runners:        map[string]runner.Runner{},
		sessionSvc:     sessioninmemory.NewSessionService(),
		traces:         map[string]attribute.Set{"event-1": attrs},
		memoryExporter: newInMemoryExporter(),
	}

	req := httptest.NewRequest(http.MethodGet, "/debug/trace/event-1", nil)
	req = mux.SetURLVars(req, map[string]string{"event_id": "event-1"})
	w := httptest.NewRecorder()

	server.handleEventTrace(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	traceID, ok := resp["trace_id"].(string)
	assert.True(t, ok)
	assert.Equal(t, "trace-1", traceID)

	llmJSON, ok := resp[adkKeyLLMRequest].(string)
	assert.True(t, ok)

	var traceReq schema.TraceLLMRequest
	assert.NoError(t, json.Unmarshal([]byte(llmJSON), &traceReq))
	assert.Equal(t, 1, len(traceReq.Contents))
	assert.Equal(t, "user", traceReq.Contents[0].Role)
	assert.Equal(t, "hello world", traceReq.Contents[0].Parts[0].Text)
}

func TestHandleEventTrace_NotFound(t *testing.T) {
	server := &Server{
		agents:         map[string]agent.Agent{},
		router:         mux.NewRouter(),
		runners:        map[string]runner.Runner{},
		sessionSvc:     sessioninmemory.NewSessionService(),
		traces:         map[string]attribute.Set{},
		memoryExporter: newInMemoryExporter(),
	}

	req := httptest.NewRequest(http.MethodGet, "/debug/trace/missing", nil)
	req = mux.SetURLVars(req, map[string]string{"event_id": "missing"})
	w := httptest.NewRecorder()

	server.handleEventTrace(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleSessionTrace_ReturnsSpans(t *testing.T) {
	server := &Server{
		agents:         map[string]agent.Agent{},
		router:         mux.NewRouter(),
		runners:        map[string]runner.Runner{},
		sessionSvc:     sessioninmemory.NewSessionService(),
		traces:         map[string]attribute.Set{},
		memoryExporter: newInMemoryExporter(),
	}

	sessionID := "session-1"
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	assert.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	assert.NoError(t, err)

	span := tracetest.SpanStub{
		Name: "chat.completion",
		SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  spanID,
		}),
		StartTime: time.Unix(100, 0),
		EndTime:   time.Unix(101, 0),
		Attributes: []attribute.KeyValue{
			attribute.String(keySessionID, sessionID),
			attribute.String(keyEventID, "event-1"),
		},
	}.Snapshot()

	server.memoryExporter.sessionTraces[sessionID] = map[string]struct{}{
		traceID.String(): {},
	}
	server.memoryExporter.spans = []sdktrace.ReadOnlySpan{span}

	path := "/debug/trace/session/" + sessionID
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	w := httptest.NewRecorder()

	server.handleSessionTrace(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var spans []schema.Span
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &spans))
	assert.Equal(t, 1, len(spans))
	assert.Equal(t, traceID.String(), spans[0].TraceID)
	assert.Equal(t, spanID.String(), spans[0].SpanID)
	assert.Equal(t, "event-1", spans[0].Attributes[keyEventID])
	assert.Equal(t, sessionID, spans[0].Attributes[keySessionID])
}
