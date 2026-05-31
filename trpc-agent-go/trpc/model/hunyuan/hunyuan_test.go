package hunyuan

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
			modelName: "hunyuan-a13b",
			opts:      nil,
			wantName:  "hunyuan-a13b",
		},
		{
			name:      "with base url",
			modelName: "hunyuan-a13b",
			opts: []Option{
				WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
			},
			wantName: "hunyuan-a13b",
		},
		{
			name:      "with api key",
			modelName: "hunyuan-a13b",
			opts: []Option{
				WithAPIKey("test-key"),
			},
			wantName: "hunyuan-a13b",
		},
		{
			name:      "with http client name",
			modelName: "hunyuan-a13b",
			opts: []Option{
				WithHTTPClientName("trpc.test.llm.hunyuan"),
			},
			wantName: "hunyuan-a13b",
		},
		{
			name:      "with http client transport",
			modelName: "hunyuan-a13b",
			opts: []Option{
				WithHTTPClientTransport(http.DefaultTransport),
			},
			wantName: "hunyuan-a13b",
		},
		{
			name:      "with thinking",
			modelName: "hunyuan-a13b",
			opts: []Option{
				WithThinking(),
			},
			wantName: "hunyuan-a13b",
		},
		{
			name:      "with disable thinking",
			modelName: "hunyuan-a13b",
			opts: []Option{
				WithDisableThinking(),
			},
			wantName: "hunyuan-a13b",
		},
		{
			name:      "with enhancement options",
			modelName: "hunyuan-2.0-instruct-20251111",
			opts: []Option{
				WithEnableEnhancement(true),
				WithForceSearchEnhancement(true),
				WithSearchScene(SearchSceneSafe),
			},
			wantName: "hunyuan-2.0-instruct-20251111",
		},
		{
			name:      "with all options",
			modelName: "hunyuan-a13b",
			opts: []Option{
				WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
				WithAPIKey("test-key"),
				WithHTTPClientName("trpc.test.llm.hunyuan"),
				WithHTTPClientTransport(http.DefaultTransport),
				WithThinking(),
				WithEnableEnhancement(true),
				WithSearchScene(SearchSceneSafe),
			},
			wantName: "hunyuan-a13b",
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
	t.Run("thinking and enhancement fields", func(t *testing.T) {
		o := &option{}
		WithThinking()(o)
		WithEnableEnhancement(true)(o)
		WithSearchScene(SearchSceneSafe)(o)

		extraFields := buildExtraFields(o)
		assert.Equal(t, true, extraFields[extraFieldEnableThinking])
		assert.Equal(t, true, extraFields[extraFieldEnableEnhancement])
		assert.Equal(t, SearchSceneSafe, extraFields[extraFieldSearchScene])
		assert.NotContains(t, extraFields, extraFieldForceSearchEnhancement)
	})

	t.Run("disable thinking writes false", func(t *testing.T) {
		o := &option{}
		WithDisableThinking()(o)

		extraFields := buildExtraFields(o)
		assert.Equal(t, false, extraFields[extraFieldEnableThinking])
	})

	t.Run("force search enables enhancement automatically", func(t *testing.T) {
		o := &option{}
		WithEnableEnhancement(false)(o)
		WithForceSearchEnhancement(true)(o)

		extraFields := buildExtraFields(o)
		assert.Equal(t, true,
			extraFields[extraFieldForceSearchEnhancement])
		assert.Equal(t, true, extraFields[extraFieldEnableEnhancement])
	})

	t.Run("force search false keeps enhancement unchanged", func(t *testing.T) {
		o := &option{}
		WithEnableEnhancement(false)(o)
		WithForceSearchEnhancement(false)(o)

		extraFields := buildExtraFields(o)
		assert.Equal(t, false,
			extraFields[extraFieldForceSearchEnhancement])
		assert.Equal(t, false, extraFields[extraFieldEnableEnhancement])
	})
}

func TestOptions(t *testing.T) {
	t.Run("WithThinking", func(t *testing.T) {
		o := &option{}
		WithThinking()(o)
		assert.NotNil(t, o.enableThinking)
		assert.True(t, *o.enableThinking)
	})

	t.Run("WithDisableThinking", func(t *testing.T) {
		o := &option{}
		WithDisableThinking()(o)
		assert.NotNil(t, o.enableThinking)
		assert.False(t, *o.enableThinking)
	})

	t.Run("WithEnableEnhancement", func(t *testing.T) {
		o := &option{}
		WithEnableEnhancement(true)(o)
		assert.NotNil(t, o.enableEnhancement)
		assert.True(t, *o.enableEnhancement)
	})

	t.Run("WithForceSearchEnhancement", func(t *testing.T) {
		o := &option{}
		WithForceSearchEnhancement(true)(o)
		assert.NotNil(t, o.forceSearchEnhancement)
		assert.True(t, *o.forceSearchEnhancement)
	})

	t.Run("WithSearchScene", func(t *testing.T) {
		o := &option{}
		WithSearchScene(SearchSceneSafe)(o)
		assert.Equal(t, SearchSceneSafe, o.searchScene)
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
}
