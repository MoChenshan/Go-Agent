package hunyuan

import (
	"net/http"

	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// option holds the configuration for creating a Hunyuan model.
type option struct {
	baseURL                string
	apiKey                 string
	httpClientName         string
	httpTransport          http.RoundTripper
	enableThinking         *bool
	enableEnhancement      *bool
	forceSearchEnhancement *bool
	searchScene            string
	openaiOpts             []openai.Option
}

// Option configures the Hunyuan model.
type Option func(*option)

// WithThinking enables thinking mode (enable_thinking: true).
// Supported models: hunyuan-a13b, hunyuan-0.5b, hunyuan-1.8b, hunyuan-4b, hunyuan-7b.
func WithThinking() Option {
	t := true
	return func(o *option) { o.enableThinking = &t }
}

// WithDisableThinking disables thinking mode (enable_thinking: false).
// Supported models: hunyuan-a13b, hunyuan-0.5b, hunyuan-1.8b, hunyuan-4b,
// hunyuan-7b.
func WithDisableThinking() Option {
	t := false
	return func(o *option) { o.enableThinking = &t }
}

// WithEnableEnhancement sets enable_enhancement in the request.
func WithEnableEnhancement(enabled bool) Option {
	return func(o *option) { o.enableEnhancement = &enabled }
}

// WithForceSearchEnhancement sets force_search_enhancement in the request.
func WithForceSearchEnhancement(enabled bool) Option {
	return func(o *option) { o.forceSearchEnhancement = &enabled }
}

// WithSearchScene sets search_scene in the request.
func WithSearchScene(scene string) Option {
	return func(o *option) { o.searchScene = scene }
}

// WithBaseURL overrides the default Hunyuan BaseURL.
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

// WithOpenAIOption appends an openai.Option for advanced configuration.
func WithOpenAIOption(opt openai.Option) Option {
	return func(o *option) { o.openaiOpts = append(o.openaiOpts, opt) }
}
