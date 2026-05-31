package taiji

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

func TestNewOpenAI(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		opts      []Option
		wantName  string
	}{
		{
			name:      "basic model creation",
			modelName: "deepseek-chat",
			opts:      nil,
			wantName:  "deepseek-chat",
		},
		{
			name:      "with base url",
			modelName: "deepseek-chat",
			opts: []Option{
				WithBaseURL("http://api.taiji.woa.com/openapi"),
			},
			wantName: "deepseek-chat",
		},
		{
			name:      "with api key",
			modelName: "deepseek-chat",
			opts: []Option{
				WithAPIKey("test-key"),
			},
			wantName: "deepseek-chat",
		},
		{
			name:      "with http client name",
			modelName: "deepseek-chat",
			opts: []Option{
				WithHTTPClientName("trpc.test.llm.openai"),
			},
			wantName: "deepseek-chat",
		},
		{
			name:      "with http client transport",
			modelName: "deepseek-chat",
			opts: []Option{
				WithHTTPClientTransport(http.DefaultTransport),
			},
			wantName: "deepseek-chat",
		},
		{
			name:      "with openai infer",
			modelName: "deepseek-chat",
			opts: []Option{
				WithOpenAIInfer(true),
			},
			wantName: "deepseek-chat",
		},
		{
			name:      "with tool choice",
			modelName: "deepseek-chat",
			opts: []Option{
				WithToolChoice(),
			},
			wantName: "deepseek-chat",
		},
		{
			name:      "with thinking",
			modelName: "DeepSeek-V3_2-Online-32k",
			opts: []Option{
				WithThinking(true),
			},
			wantName: "DeepSeek-V3_2-Online-32k",
		},
		{
			name:      "with query id",
			modelName: "self-deployed-model",
			opts: []Option{
				WithQueryID("test-query-id"),
			},
			wantName: "self-deployed-model",
		},
		{
			name:      "with all options",
			modelName: "deepseek-chat",
			opts: []Option{
				WithBaseURL("http://api.taiji.woa.com/openapi"),
				WithAPIKey("test-key"),
				WithHTTPClientName("trpc.test.llm.openai"),
				WithHTTPClientTransport(http.DefaultTransport),
				WithOpenAIInfer(true),
				WithToolChoice(),
				WithThinking(true),
				WithQueryID("test-query-id"),
			},
			wantName: "deepseek-chat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewOpenAI(tt.modelName, tt.opts...)
			assert.NotNil(t, m)
			assert.Equal(t, tt.wantName, m.Info().Name)
		})
	}
}

func TestBuildExtraFields(t *testing.T) {
	t.Run("deepseek thinking uses top level field", func(t *testing.T) {
		o := &option{}
		WithOpenAIInfer(true)(o)
		WithToolChoice()(o)
		WithThinking(true)(o)

		extraFields := buildExtraFields("DeepSeek-V3_2-Online-32k", o)
		assert.Equal(t, true, extraFields[extraFieldOpenAIInfer])
		assert.Equal(t, toolChoiceAuto, extraFields[extraFieldToolChoice])
		assert.Equal(t, true, extraFields[extraFieldThinking])
		assert.NotContains(t, extraFields, extraFieldChatTemplateKwargs)
	})

	t.Run("glm 128k thinking uses chat template kwargs", func(t *testing.T) {
		o := &option{}
		WithThinking(false)(o)

		extraFields := buildExtraFields("GLM-5-FP8-Online-128K", o)
		assert.NotContains(t, extraFields, extraFieldThinking)
		assert.Equal(t, map[string]any{
			extraFieldEnableThinking: false,
		}, extraFields[extraFieldChatTemplateKwargs])
	})

	t.Run("glm 32k thinking uses chat template kwargs", func(t *testing.T) {
		o := &option{}
		WithThinking(true)(o)

		extraFields := buildExtraFields("GLM-5-FP8-Online-32K", o)
		assert.NotContains(t, extraFields, extraFieldThinking)
		assert.Equal(t, map[string]any{
			extraFieldEnableThinking: true,
		}, extraFields[extraFieldChatTemplateKwargs])
	})
}

func TestOptions(t *testing.T) {
	t.Run("WithOpenAIInfer", func(t *testing.T) {
		o := &option{}
		WithOpenAIInfer(true)(o)
		assert.NotNil(t, o.enableOpenAIInfer)
		assert.True(t, *o.enableOpenAIInfer)
	})

	t.Run("WithToolChoice", func(t *testing.T) {
		o := &option{}
		WithToolChoice()(o)
		assert.True(t, o.enableToolChoice)
	})

	t.Run("WithThinking", func(t *testing.T) {
		o := &option{}
		WithThinking(true)(o)
		assert.NotNil(t, o.enableThinking)
		assert.True(t, *o.enableThinking)
	})

	t.Run("WithBaseURL", func(t *testing.T) {
		o := &option{}
		WithBaseURL("http://test.url")(o)
		assert.Equal(t, "http://test.url", o.baseURL)
	})

	t.Run("WithAPIKey", func(t *testing.T) {
		o := &option{}
		WithAPIKey("test-key")(o)
		assert.Equal(t, "test-key", o.apiKey)
	})

	t.Run("WithHTTPClientName", func(t *testing.T) {
		o := &option{}
		WithHTTPClientName("trpc.test.service")(o)
		assert.Equal(t, "trpc.test.service", o.httpClientName)
	})

	t.Run("WithHTTPClientTransport", func(t *testing.T) {
		o := &option{}
		WithHTTPClientTransport(http.DefaultTransport)(o)
		assert.Equal(t, http.DefaultTransport, o.httpTransport)
	})

	t.Run("WithOpenAIOption", func(t *testing.T) {
		o := &option{}
		opt := openai.WithBaseURL("http://test.url")
		WithOpenAIOption(opt)(o)
		assert.Len(t, o.openaiOpts, 1)
	})

	t.Run("WithQueryID", func(t *testing.T) {
		o := &option{}
		WithQueryID("test-query-id")(o)
		assert.Equal(t, "test-query-id", o.queryID)
	})
}
