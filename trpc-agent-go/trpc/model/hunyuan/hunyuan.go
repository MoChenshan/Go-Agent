// Package hunyuan provides convenient constructors for creating models configured
// for the Hunyuan platform with tRPC integration.
//
// The package automatically configures:
//   - tRPC HTTP client (supports Polaris, monitoring, interceptors, etc.)
//
// Optional features can be enabled via Options:
//   - WithThinking(): enables enable_thinking: true
//   - WithEnhancement(true): enables enhancement features such as search
//   - WithForceSearchEnhancement(true): forces search enhancement and also
//     enables enhancement automatically
//   - WithSearchScene(SearchSceneSafe): uses the safe search scene
//
// Example usage:
//
//	// Basic usage.
//	m := hunyuan.NewOpenAI("hunyuan-a13b", hunyuan.WithBaseURL("http://..."))
//
//	// With thinking mode.
//	m := hunyuan.NewOpenAI(
//	    "hunyuan-a13b",
//	    hunyuan.WithBaseURL(baseURL),
//	    hunyuan.WithThinking(),
//	)
//
//	// With search enhancement.
//	m := hunyuan.NewOpenAI(
//	    "hunyuan-2.0-instruct-20251111",
//	    hunyuan.WithBaseURL(baseURL),
//	    hunyuan.WithEnableEnhancement(true),
//	    hunyuan.WithSearchScene(hunyuan.SearchSceneSafe),
//	)
package hunyuan

import (
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"

	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

const (
	// extraFieldEnableThinking is the key for enable_thinking field.
	extraFieldEnableThinking = "enable_thinking"
	// extraFieldEnableEnhancement is the key for enable_enhancement field.
	extraFieldEnableEnhancement = "enable_enhancement"
	// extraFieldForceSearchEnhancement is the key for
	// force_search_enhancement field.
	extraFieldForceSearchEnhancement = "force_search_enhancement"
	// extraFieldSearchScene is the key for search_scene field.
	extraFieldSearchScene = "search_scene"

	// SearchSceneSafe disables the Bing data source for search enhancement.
	SearchSceneSafe = "safe"
)

// NewOpenAI creates an OpenAI-compatible model configured for the Hunyuan platform.
//
// It automatically configures:
//   - tRPC HTTP client for service discovery, monitoring, etc.
//
// Use WithThinking() to enable thinking mode for supported models
// (hunyuan-a13b, hunyuan-0.5b, hunyuan-1.8b, hunyuan-4b, hunyuan-7b).
// Use WithEnableEnhancement(true) to enable search enhancement, and
// WithForceSearchEnhancement(true) to force the search path when supported.
func NewOpenAI(modelName string, opts ...Option) *openai.Model {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	extraFields := buildExtraFields(o)

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
		openaiOpts = append(openaiOpts, openai.WithHTTPClientOptions(httpClientOpts...))
	}

	openaiOpts = append(openaiOpts, o.openaiOpts...)
	return openai.New(modelName, openaiOpts...)
}

func buildExtraFields(o *option) map[string]any {
	extraFields := make(map[string]any)
	if o.enableThinking != nil {
		extraFields[extraFieldEnableThinking] = *o.enableThinking
	}
	if o.enableEnhancement != nil {
		extraFields[extraFieldEnableEnhancement] = *o.enableEnhancement
	}
	if o.forceSearchEnhancement != nil {
		forceEnabled := *o.forceSearchEnhancement
		extraFields[extraFieldForceSearchEnhancement] = forceEnabled
		if forceEnabled {
			extraFields[extraFieldEnableEnhancement] = true
		}
	}
	if o.searchScene != "" {
		extraFields[extraFieldSearchScene] = o.searchScene
	}
	return extraFields
}
