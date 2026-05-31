package taiji

import (
	"net/http"

	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// option holds the configuration for creating a Taiji model.
type option struct {
	baseURL           string
	apiKey            string
	httpClientName    string
	httpTransport     http.RoundTripper
	enableOpenAIInfer *bool
	enableToolChoice  bool
	enableThinking    *bool
	queryID           string
	openaiOpts        []openai.Option
}

// Option configures the Taiji model.
type Option func(*option)

// WithOpenAIInfer sets openai_infer field in the request.
// This is required for most Taiji models.
func WithOpenAIInfer(enabled bool) Option {
	return func(o *option) { o.enableOpenAIInfer = &enabled }
}

// WithToolChoice enables tool_choice: "auto" in the request.
// Use this when your agent uses tools.
func WithToolChoice() Option {
	return func(o *option) { o.enableToolChoice = true }
}

// WithThinking sets thinking mode for supported Taiji models.
//
// For DeepSeek V3.1/V3.2 models, it writes the top-level `thinking` field.
// For `GLM-5-FP8-Online-128K` and `GLM-5-FP8-Online-32K`, it writes
// `chat_template_kwargs.enable_thinking`.
func WithThinking(enabled bool) Option {
	return func(o *option) { o.enableThinking = &enabled }
}

// WithBaseURL overrides the default Taiji BaseURL.
func WithBaseURL(url string) Option {
	return func(o *option) { o.baseURL = url }
}

// WithAPIKey sets the API key.
func WithAPIKey(key string) Option {
	return func(o *option) { o.apiKey = key }
}

// WithHTTPClientName sets the tRPC HTTP client service name.
// This name should match the client service name in trpc_go.yaml.
func WithHTTPClientName(name string) Option {
	return func(o *option) { o.httpClientName = name }
}

// WithHTTPClientTransport sets a custom HTTP transport.
func WithHTTPClientTransport(transport http.RoundTripper) Option {
	return func(o *option) { o.httpTransport = transport }
}

// WithQueryID sets query_id field for self-deployed models on Taiji platform.
// Some self-deployed models require this field, otherwise returns error:
// "should include query_id && model && message".
func WithQueryID(id string) Option {
	return func(o *option) { o.queryID = id }
}

// WithOpenAIOption appends an openai.Option for advanced configuration.
func WithOpenAIOption(opt openai.Option) Option {
	return func(o *option) { o.openaiOpts = append(o.openaiOpts, opt) }
}
