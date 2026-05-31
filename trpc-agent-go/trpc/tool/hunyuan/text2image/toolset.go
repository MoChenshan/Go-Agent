// Package text2image provides a toolset for text-to-image generation.
package text2image

import (
	"context"
	"fmt"
	"os"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/tool/hunyuan/text2image/internal/client"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	defaultName = "hunyuan-text2image"

	envAPIKey = "HUNYUAN_IMAGE_API_KEY"
	envModel  = "HUNYUAN_IMAGE_MODEL"
	envAPIURL = "HUNYUAN_IMAGE_API_URL"
	envPath   = "HUNYUAN_IMAGE_PATH"
)

// NewToolSet creates a new ToolSet instance.
// modelName specifies which model's tools to load.
func NewToolSet(ctx context.Context, opts ...Option) (*ToolSet, error) {
	var tools []tool.Tool

	opts = append(defaultOptions(), opts...)

	cfg := Options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.apiKey == "" {
		return nil, fmt.Errorf("apiKey is required")
	}

	c, err := client.NewClient(cfg.apiKey, initClientOpts(cfg)...)
	if err != nil {
		return nil, err
	}
	tools = append(tools, function.NewFunctionTool(
		c.Generate,
		function.WithName("text2image"),
		function.WithDescription("Based on the hunyuan text-to-image generation model, it supports obtaining images through text"),
		function.WithInputSchema(&tool.Schema{
			Type:     "object",
			Required: []string{"prompt", "size", "seed", "footnote"},
			Properties: map[string]*tool.Schema{
				"prompt": {
					Type:        "string",
					Description: "The text used for generating images has a string length not exceeding 8192",
				},
				"size": {
					Type:        "string",
					Description: "Format: \\\"${Width}x${height}\\\".",
					Default:     "1024x1024",
				},
				"seed": {
					Type:        "integer",
					Description: "Generate seeds, which only take effect when the number of generated images is 1. The range is [1, 4294967295]. If no image is transmitted or it is 0, it will be random by default",
				},
				"footnote": {
					Type:        "string",
					Description: "Customize the watermark content for the business, with a length limit of 16 characters (regardless of Chinese or English), and generate it at the lower right corner of the image",
				},
			},
		}),
	))
	return &ToolSet{name: cfg.name, tools: tools}, nil
}

// ToolSet represents a collection of TRAG tools that can be used by agents.
// It implements a toolset interface that provides access to loaded tools
// and manages their lifecycle.
type ToolSet struct {
	name  string
	tools []tool.Tool
}

// Tools returns all tools available in this toolset.
// The returned tools implement the tool.Tool interface and can be used
// with agent systems.
func (t *ToolSet) Tools(ctx context.Context) []tool.Tool {
	return t.tools
}

// Close releases any resources held by the toolset.
func (t *ToolSet) Close() error {
	return nil
}

// Name returns the name of this toolset.
func (t *ToolSet) Name() string {
	return t.name
}

func defaultOptions() []Option {
	defaults := []Option{
		WithName(defaultName),
	}
	if o, ok := os.LookupEnv(envAPIKey); ok {
		defaults = append(defaults, WithAPIKey(o))
	}
	if o, ok := os.LookupEnv(envAPIURL); ok {
		defaults = append(defaults, WithBaseURL(o))
	}
	if o, ok := os.LookupEnv(envModel); ok {
		defaults = append(defaults, WithModel(o))
	}
	if o, ok := os.LookupEnv(envPath); ok {
		defaults = append(defaults, WithImagePath(o))
	}
	return defaults
}

func initClientOpts(cfg Options) []client.Option {
	var clientOpts []client.Option
	if cfg.baseURL != "" {
		clientOpts = append(clientOpts, client.WithBaseURL(cfg.baseURL))
	}
	if cfg.model != "" {
		clientOpts = append(clientOpts, client.WithModel(cfg.model))
	}
	if cfg.imagePath != "" {
		clientOpts = append(clientOpts, client.WithPath(cfg.imagePath))
	}
	if cfg.timeout != 0 {
		clientOpts = append(clientOpts, client.WithTimeout(cfg.timeout))
	}
	return clientOpts
}
