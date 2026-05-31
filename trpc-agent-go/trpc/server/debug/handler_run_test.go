package debug

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug/internal/schema"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

const (
	runPath    = "/run"
	runSSEPath = "/run_sse"
)

func TestServer_handleRun(t *testing.T) {
	// Create a real LLM agent for this test.
	modelInstance := openai.New("test-model")
	llmAgent := llmagent.New(
		"test-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("test agent"),
	)

	agents := map[string]agent.Agent{
		"test-agent": llmAgent,
	}

	server := New(agents)

	// Create a test request.
	requestBody := schema.AgentRunRequest{
		AppName:   "test-agent",
		UserID:    "test-user",
		SessionID: "test-session",
		NewMessage: schema.Content{
			Role: "user",
			Parts: []schema.Part{
				{Text: "Hello, world!"},
			},
		},
		Streaming: false,
	}

	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(
		http.MethodPost,
		runPath,
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleRun(w, req)

	// The request should fail because the model is not properly configured,
	// but we can verify the request was processed.
	if w.Code == http.StatusOK {
		// If it succeeded, verify the response structure.
		var response []any
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	} else {
		// Expected to fail due to model configuration.
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	}
}

func TestServer_handleRun_InvalidJSON(t *testing.T) {
	server := &Server{}

	req := httptest.NewRequest(
		http.MethodPost,
		runPath,
		bytes.NewReader([]byte("{invalid-json")),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleRun(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_handleRun_GetRunnerError(t *testing.T) {
	server := &Server{
		runners: map[string]runner.Runner{},
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "missing",
		UserID:    "user",
		SessionID: "session",
		NewMessage: schema.Content{
			Role:  "user",
			Parts: []schema.Part{{Text: "hi"}},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		runPath,
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleRun(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_handleRun_RunError(t *testing.T) {
	server := &Server{
		runners: map[string]runner.Runner{
			"app": &fakeRunner{err: errors.New("run failed")},
		},
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "session",
		NewMessage: schema.Content{
			Role:  "user",
			Parts: []schema.Part{{Text: "hi"}},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		runPath,
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleRun(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestServer_handleRun_NonStreamingReturnsEvents(t *testing.T) {
	partial := &event.Event{
		InvocationID: "inv",
		Author:       "assistant",
		ID:           "partial-id",
		Timestamp:    time.Unix(0, 0),
		Response: &model.Response{
			IsPartial: true,
			Choices: []model.Choice{
				{Delta: model.Message{Content: "partial"}},
			},
		},
	}
	final := newRunnerFinalEvent("inv", "final output")

	server := &Server{
		runners: map[string]runner.Runner{
			"app": &fakeRunner{events: []*event.Event{partial, final}},
		},
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "session",
		NewMessage: schema.Content{
			Role:  "user",
			Parts: []schema.Part{{Text: "hi"}},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		runPath,
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var eventsPayload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &eventsPayload))
	assert.Len(t, eventsPayload, 1)
}

func TestConvertContentToMessage(t *testing.T) {
	content := schema.Content{
		Role: "user",
		Parts: []schema.Part{
			{Text: "Hello, world!"},
		},
	}

	message := convertContentToMessage(content)

	assert.Equal(t, model.RoleUser, message.Role)
	assert.Equal(t, "Hello, world!", message.Content)
}

func TestConvertContentToMessage_Func(t *testing.T) {
	content := schema.Content{
		Role: "assistant",
		Parts: []schema.Part{
			{
				FunctionCall: &schema.FunctionCall{
					Name: "test_function",
					Args: map[string]any{
						"param1": "value1",
						"param2": 42,
					},
				},
			},
		},
	}

	message := convertContentToMessage(content)

	assert.Equal(t, model.RoleAssistant, message.Role)
	assert.Len(t, message.ToolCalls, 1)

	toolCall := message.ToolCalls[0]
	assert.Equal(t, "function", toolCall.Type)
	assert.Equal(t, "test_function", toolCall.Function.Name)
}

func TestConvertContentToMessage_InlineDataAndFunctionResponse(t *testing.T) {
	content := schema.Content{
		Role: "assistant",
		Parts: []schema.Part{
			{
				InlineData: &schema.InlineData{
					MimeType:    "image/png",
					DisplayName: "diagram.png",
				},
			},
			{
				InlineData: &schema.InlineData{
					MimeType: "audio/mpeg",
				},
			},
			{
				FunctionResponse: &schema.FunctionResponse{
					Name:     "tool",
					Response: map[string]any{"ok": true},
				},
			},
		},
	}

	msg := convertContentToMessage(content)

	assert.Equal(t, model.RoleAssistant, msg.Role)
	expectedSnippets := []string{
		"[image: diagram.png (image/png)]",
		"[audio: attachment (audio/mpeg)]",
		`[Function tool responded: {"ok":true}]`,
	}
	for _, snippet := range expectedSnippets {
		assert.True(t, strings.Contains(msg.Content, snippet))
	}
}

func TestHandleRunSSE_StreamingWritesEvents(t *testing.T) {
	e := &event.Event{
		InvocationID: "inv",
		Author:       "assistant",
		ID:           "event-id",
		Timestamp:    time.Unix(0, 0),
		Response: &model.Response{
			IsPartial: true,
			Done:      false,
			Choices: []model.Choice{
				{
					Delta: model.Message{
						Content: "partial",
						Role:    model.RoleAssistant,
					},
				},
			},
		},
	}

	server := &Server{
		agents: map[string]agent.Agent{},
		router: mux.NewRouter(),
		runners: map[string]runner.Runner{
			"app": &fakeRunner{events: []*event.Event{e}},
		},
		sessionSvc:     sessioninmemory.NewSessionService(),
		traces:         map[string]attribute.Set{},
		memoryExporter: newInMemoryExporter(),
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "sess",
		NewMessage: schema.Content{
			Role: "user",
			Parts: []schema.Part{
				{Text: "hi"},
			},
		},
		Streaming: true,
	}
	bodyBytes, err := json.Marshal(reqBody)
	assert.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		runSSEPath,
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	server.handleRunSSE(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data: ")
}

func TestHandleRunSSE_NoFlusher(t *testing.T) {
	server := &Server{
		agents:         map[string]agent.Agent{},
		router:         mux.NewRouter(),
		runners:        map[string]runner.Runner{"app": &fakeRunner{}},
		sessionSvc:     sessioninmemory.NewSessionService(),
		traces:         map[string]attribute.Set{},
		memoryExporter: newInMemoryExporter(),
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "sess",
		NewMessage: schema.Content{
			Role:  "user",
			Parts: []schema.Part{{Text: "hi"}},
		},
		Streaming: true,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(
		http.MethodPost,
		runSSEPath,
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	w := newNoFlusherRecorder()

	server.handleRunSSE(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.StatusCode())
	assert.Contains(t, w.BodyString(), "Streaming unsupported")
}

func TestHandleRunSSE_GetRunnerError(t *testing.T) {
	server := &Server{
		agents:         map[string]agent.Agent{},
		router:         mux.NewRouter(),
		runners:        map[string]runner.Runner{},
		sessionSvc:     sessioninmemory.NewSessionService(),
		traces:         map[string]attribute.Set{},
		memoryExporter: newInMemoryExporter(),
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "missing",
		UserID:    "user",
		SessionID: "sess",
		NewMessage: schema.Content{
			Role:  "user",
			Parts: []schema.Part{{Text: "hi"}},
		},
		Streaming: true,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(
		http.MethodPost,
		runSSEPath,
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	server.handleRunSSE(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRunSSE_RunError(t *testing.T) {
	server := &Server{
		agents: map[string]agent.Agent{},
		router: mux.NewRouter(),
		runners: map[string]runner.Runner{
			"app": &fakeRunner{err: errors.New("run failed")},
		},
		sessionSvc:     sessioninmemory.NewSessionService(),
		traces:         map[string]attribute.Set{},
		memoryExporter: newInMemoryExporter(),
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "sess",
		NewMessage: schema.Content{
			Role:  "user",
			Parts: []schema.Part{{Text: "hi"}},
		},
		Streaming: true,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(
		http.MethodPost,
		runSSEPath,
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	server.handleRunSSE(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleRunSSE_NonStreaming(t *testing.T) {
	e := &event.Event{
		InvocationID: "inv",
		Author:       "assistant",
		ID:           "event-id",
		Timestamp:    time.Unix(0, 0),
		Response: &model.Response{
			Done: true,
			Choices: []model.Choice{
				{Message: model.Message{Content: "final", Role: model.RoleAssistant}},
			},
		},
	}

	server := &Server{
		agents: map[string]agent.Agent{},
		router: mux.NewRouter(),
		runners: map[string]runner.Runner{
			"app": &fakeRunner{events: []*event.Event{e}},
		},
		sessionSvc:     sessioninmemory.NewSessionService(),
		traces:         map[string]attribute.Set{},
		memoryExporter: newInMemoryExporter(),
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "sess",
		NewMessage: schema.Content{
			Role:  "user",
			Parts: []schema.Part{{Text: "hi"}},
		},
		Streaming: false,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(
		http.MethodPost,
		runSSEPath,
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	server.handleRunSSE(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data: ")
}

func TestNewDetachedContextPreservesValues(t *testing.T) {
	type ctxKey struct{}
	key := ctxKey{}
	parent, cancel := context.WithCancel(
		context.WithValue(context.Background(), key, "trace-id"),
	)
	cancel()

	ctx := newDetachedContext(parent)

	assert.Equal(t, "trace-id", ctx.Value(key))
	_, ok := ctx.Deadline()
	assert.False(t, ok)
	assert.Nil(t, ctx.Done())
	assert.Nil(t, ctx.Err())

	err := agent.CheckContextCancelled(ctx)
	assert.NoError(t, err)
}

func TestHandleRunSSE_UsesDetachedContext(t *testing.T) {
	type ctxKey struct{}
	key := ctxKey{}

	ctxRunner := &ctxCapturingRunner{}
	server := &Server{
		agents:     map[string]agent.Agent{},
		router:     mux.NewRouter(),
		runners:    map[string]runner.Runner{"app": ctxRunner},
		sessionSvc: sessioninmemory.NewSessionService(),
		traces:     map[string]attribute.Set{},

		memoryExporter: newInMemoryExporter(),
	}

	reqBody := schema.AgentRunRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "sess",
		NewMessage: schema.Content{
			Role:  "user",
			Parts: []schema.Part{{Text: "hi"}},
		},
		Streaming: true,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	baseCtx, cancel := context.WithCancel(
		context.WithValue(context.Background(), key, "trace-id"),
	)
	cancel()

	req := httptest.NewRequest(
		http.MethodPost,
		runSSEPath,
		bytes.NewReader(body),
	).WithContext(baseCtx)
	req.Header.Set("Content-Type", "application/json")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	server.handleRunSSE(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	if assert.NotNil(t, ctxRunner.ctx) {
		assert.Equal(t, "trace-id", ctxRunner.ctx.Value(key))
		_, ok := ctxRunner.ctx.Deadline()
		assert.False(t, ok)
		assert.Nil(t, ctxRunner.ctx.Done())
		assert.Nil(t, ctxRunner.ctx.Err())

		err = agent.CheckContextCancelled(ctxRunner.ctx)
		assert.NoError(t, err)
	}
}
