// Package taiji provides convenient constructors for creating models configured
// for the Taiji platform with tRPC integration.
//
// The package automatically configures:
//   - tRPC HTTP client (supports Polaris, monitoring, interceptors, etc.)
//
// Optional features can be enabled via Options:
//   - WithOpenAIInfer(true): enables openai_infer: true (required for most
//     models)
//   - WithToolChoice(): enables tool_choice: "auto"
//   - WithThinking(true): enables thinking mode for supported Taiji models
//   - WithQueryID(id): sets query_id for self-deployed models
//
// Example usage:
//
//	// Basic usage with OpenAI inference mode.
//	m := taiji.NewOpenAI("deepseek-chat",
//	    taiji.WithBaseURL("http://..."),
//	    taiji.WithOpenAIInfer(true),
//	)
//
//	// With tool calling.
//	m := taiji.NewOpenAI("deepseek-chat",
//	    taiji.WithBaseURL(baseURL),
//	    taiji.WithOpenAIInfer(true),
//	    taiji.WithToolChoice(),
//	)
//
//	// With DeepSeek thinking mode.
//	m := taiji.NewOpenAI("DeepSeek-V3_2",
//	    taiji.WithBaseURL(baseURL),
//	    taiji.WithOpenAIInfer(true),
//	    taiji.WithThinking(true),
//	)
//
//	// With GLM thinking mode.
//	m := taiji.NewOpenAI("GLM-5-FP8-Online-128K",
//	    taiji.WithBaseURL(baseURL),
//	    taiji.WithOpenAIInfer(true),
//	    taiji.WithThinking(false),
//	)
//
//	// With query_id for self-deployed models.
//	m := taiji.NewOpenAI("your-model",
//	    taiji.WithBaseURL(baseURL),
//	    taiji.WithQueryID("your-query-id"),
//	)
package taiji

import (
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"

	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

const (
	// extraFieldOpenAIInfer is the key for openai_infer field.
	extraFieldOpenAIInfer = "openai_infer"
	// extraFieldToolChoice is the key for tool_choice field.
	extraFieldToolChoice = "tool_choice"
	// extraFieldThinking is the key for thinking field.
	extraFieldThinking = "thinking"
	// extraFieldChatTemplateKwargs is the key for chat_template_kwargs field.
	extraFieldChatTemplateKwargs = "chat_template_kwargs"
	// extraFieldEnableThinking is the key for enable_thinking field.
	extraFieldEnableThinking = "enable_thinking"
	// extraFieldQueryID is the key for query_id field.
	extraFieldQueryID = "query_id"

	// toolChoiceAuto is the value for auto tool choice.
	toolChoiceAuto = "auto"

	glm5FP8Online128K = "GLM-5-FP8-Online-128K"
	glm5FP8Online32K  = "GLM-5-FP8-Online-32K"
)

// NewOpenAI creates an OpenAI-compatible model configured for the Taiji
// platform.
//
// It automatically configures the tRPC HTTP client for service discovery,
// monitoring, and other infrastructure concerns.
//
// It also enables WithOpenAIErrorCompat by default.
//
// Use WithOpenAIInfer(true) to enable openai_infer mode (required for most
// models), WithToolChoice() to enable tool calling, WithThinking(true) to
// enable thinking mode for supported models, and WithQueryID(id) for
// self-deployed models that require query_id.
func NewOpenAI(modelName string, opts ...Option) *openai.Model {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	extraFields := buildExtraFields(modelName, o)

	// Build openai.Option list.
	var openaiOpts []openai.Option
	if len(extraFields) > 0 {
		openaiOpts = append(openaiOpts, openai.WithExtraFields(extraFields))
	}
	if o.baseURL != "" {
		openaiOpts = append(openaiOpts, openai.WithBaseURL(o.baseURL))
	}
	if o.apiKey != "" {
		openaiOpts = append(openaiOpts, openai.WithAPIKey(o.apiKey))
	}

	// Build HTTP client options.
	var httpClientOpts []openai.HTTPClientOption
	if o.httpClientName != "" {
		httpClientOpts = append(httpClientOpts,
			openai.WithHTTPClientName(o.httpClientName))
	}
	if o.httpTransport != nil {
		httpClientOpts = append(httpClientOpts,
			openai.WithHTTPClientTransport(o.httpTransport))
	}
	if len(httpClientOpts) > 0 {
		openaiOpts = append(openaiOpts,
			openai.WithHTTPClientOptions(httpClientOpts...))
	}

	openaiOpts = append(openaiOpts, WithOpenAIErrorCompat())
	openaiOpts = append(openaiOpts, o.openaiOpts...)
	return openai.New(modelName, openaiOpts...)
}

func buildExtraFields(modelName string, o *option) map[string]any {
	extraFields := make(map[string]any)
	if o.enableOpenAIInfer != nil {
		extraFields[extraFieldOpenAIInfer] = *o.enableOpenAIInfer
	}
	if o.enableToolChoice {
		extraFields[extraFieldToolChoice] = toolChoiceAuto
	}
	if o.enableThinking != nil {
		for key, value := range buildThinkingExtraFields(modelName,
			*o.enableThinking) {
			extraFields[key] = value
		}
	}
	if o.queryID != "" {
		extraFields[extraFieldQueryID] = o.queryID
	}
	return extraFields
}

func buildThinkingExtraFields(modelName string, enabled bool) map[string]any {
	switch modelName {
	case glm5FP8Online128K, glm5FP8Online32K:
		return map[string]any{
			extraFieldChatTemplateKwargs: map[string]any{
				extraFieldEnableThinking: enabled,
			},
		}
	default:
		return map[string]any{
			extraFieldThinking: enabled,
		}
	}
}
