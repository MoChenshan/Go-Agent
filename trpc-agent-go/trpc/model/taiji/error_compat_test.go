package taiji

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	coremodel "trpc.group/trpc-go/trpc-agent-go/model"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

const taijiErrorBody = `{"error":{"code":"messages array size error: 3","message":"messages array size error: 3","ret_code":-2001,"type":"RequestFormatError"}}`

func TestNormalizeTaijiErrorResponse(t *testing.T) {
	t.Run("rewrites 200 error envelope", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(taijiErrorBody)),
		}
		normalized, err := normalizeTaijiErrorResponse(resp)
		require.NoError(t, err)
		body, err := io.ReadAll(normalized.Body)
		require.NoError(t, err)
		var envelope map[string]map[string]any
		require.NoError(t, json.Unmarshal(body, &envelope))
		assert.Equal(t, http.StatusBadRequest, normalized.StatusCode)
		assert.Equal(t, "400 Bad Request", normalized.Status)
		assert.Equal(t, "messages array size error: 3", envelope["error"]["code"])
		assert.Equal(t, "messages array size error: 3", envelope["error"]["message"])
		assert.Equal(t, "RequestFormatError", envelope["error"]["type"])
		assert.Equal(t, "", envelope["error"]["param"])
		assert.Equal(t, float64(-2001), envelope["error"]["ret_code"])
	})
	t.Run("keeps successful response untouched", func(t *testing.T) {
		successBody := `{"id":"chatcmpl-ok","object":"chat.completion","created":1,"model":"DeepSeek-V3_1-Online-64k","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(successBody)),
		}
		normalized, err := normalizeTaijiErrorResponse(resp)
		require.NoError(t, err)
		body, err := io.ReadAll(normalized.Body)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, normalized.StatusCode)
		assert.Equal(t, successBody, string(body))
	})
}

func TestWithOpenAIErrorCompat_GenerateContentHandlesTaijiErrorBody(t *testing.T) {
	server := newTaijiErrorServer()
	defer server.Close()
	m := newOpenAIModelWithErrorCompat(server.URL)
	request := &coremodel.Request{
		Messages: []coremodel.Message{coremodel.NewUserMessage("hello")},
		GenerationConfig: coremodel.GenerationConfig{
			Stream: false,
		},
	}
	responseChan, err := m.GenerateContent(context.Background(), request)
	require.NoError(t, err)
	response := collectLastResponse(responseChan)
	require.NotNil(t, response)
	require.NotNil(t, response.Error)
	assert.Equal(t, coremodel.ErrorTypeAPIError, response.Error.Type)
	assert.Contains(t, response.Error.Message, "messages array size error: 3")
	assert.Contains(t, response.Error.Message, "400 Bad Request")
	assert.True(t, response.Done)
}

func TestWithOpenAIErrorCompat_StreamingReturnsErrorResponseForTaijiErrorBody(t *testing.T) {
	server := newTaijiErrorServer()
	defer server.Close()
	m := newOpenAIModelWithErrorCompat(server.URL)
	request := &coremodel.Request{
		Messages: []coremodel.Message{coremodel.NewUserMessage("hello")},
		GenerationConfig: coremodel.GenerationConfig{
			Stream: true,
		},
	}
	responseChan, err := m.GenerateContent(context.Background(), request)
	require.NoError(t, err)
	response := collectLastResponse(responseChan)
	require.NotNil(t, response)
	require.NotNil(t, response.Error)
	assert.Equal(t, coremodel.ErrorTypeStreamError, response.Error.Type)
	assert.Contains(t, response.Error.Message, "messages array size error: 3")
	assert.Contains(t, response.Error.Message, "400 Bad Request")
	assert.True(t, response.Done)
}

func TestWithOpenAIErrorCompat_RunnerEmitsErrorEventForTaijiErrorBody(t *testing.T) {
	server := newTaijiErrorServer()
	defer server.Close()
	modelInstance := newOpenAIModelWithErrorCompat(server.URL)
	agent := llmagent.New("taiji-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithGenerationConfig(coremodel.GenerationConfig{Stream: true}),
	)
	run := runner.NewRunner("taiji-app", agent, runner.WithSessionService(sessioninmemory.NewSessionService()))
	events, err := run.Run(context.Background(), "user-1", "session-1", coremodel.NewUserMessage("hello"))
	require.NoError(t, err)
	var errorEventFound bool
	var runnerCompletionFound bool
	for evt := range events {
		if evt != nil && evt.Response != nil && evt.Response.Error != nil {
			errorEventFound = true
			assert.Contains(t, evt.Response.Error.Message, "messages array size error: 3")
		}
		if evt != nil && evt.IsRunnerCompletion() {
			runnerCompletionFound = true
		}
	}
	assert.True(t, errorEventFound)
	assert.True(t, runnerCompletionFound)
}

func TestNewOpenAI_UsesOpenAIErrorCompatByDefault(t *testing.T) {
	server := newTaijiErrorServer()
	defer server.Close()
	m := NewOpenAI("DeepSeek-V3_1-Online-64k", WithBaseURL(server.URL), WithAPIKey("test-key"))
	request := &coremodel.Request{
		Messages: []coremodel.Message{coremodel.NewUserMessage("hello")},
		GenerationConfig: coremodel.GenerationConfig{
			Stream: false,
		},
	}
	responseChan, err := m.GenerateContent(context.Background(), request)
	require.NoError(t, err)
	response := collectLastResponse(responseChan)
	require.NotNil(t, response)
	require.NotNil(t, response.Error)
	assert.Equal(t, coremodel.ErrorTypeAPIError, response.Error.Type)
	assert.Contains(t, response.Error.Message, "messages array size error: 3")
}

func newTaijiErrorServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, taijiErrorBody)
	}))
}

func newOpenAIModelWithErrorCompat(baseURL string) *openaimodel.Model {
	return openaimodel.New(
		"DeepSeek-V3_1-Online-64k",
		openaimodel.WithBaseURL(baseURL),
		openaimodel.WithAPIKey("test-key"),
		WithOpenAIErrorCompat(),
	)
}

func collectLastResponse(responseChan <-chan *coremodel.Response) *coremodel.Response {
	var response *coremodel.Response
	for resp := range responseChan {
		response = resp
	}
	return response
}
