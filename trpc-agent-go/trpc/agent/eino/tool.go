package eino

import (
	itool "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/tool"
	einotool "github.com/cloudwego/eino/components/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolConfig represents configuration options for tool conversion.
type ToolConfig struct {
	Name        string
	Description string
	Timeout     int // in seconds, 0 means no timeout
}

// ToolOption defines a functional option for configuring tool conversion.
type ToolOption func(*ToolConfig)

// WithName sets the name for the converted tool.
func WithName(name string) ToolOption {
	return func(c *ToolConfig) {
		c.Name = name
	}
}

// WithDescription sets the description for the converted tool.
func WithDescription(description string) ToolOption {
	return func(c *ToolConfig) {
		c.Description = description
	}
}

// WithTimeout sets the timeout for the converted tool in seconds.
func WithTimeout(seconds int) ToolOption {
	return func(c *ToolConfig) {
		c.Timeout = seconds
	}
}

// NewTool converts an eino BaseTool to a trpc-agent-go Tool.
// This function provides explicit control over tool conversion with flexible configuration options.
func NewTool(baseTool einotool.BaseTool, options ...ToolOption) tool.Tool {
	config := &ToolConfig{}
	for _, opt := range options {
		opt(config)
	}

	// Convert to internal config
	internalConfig := &itool.Config{
		Name:        config.Name,
		Description: config.Description,
		Timeout:     config.Timeout,
	}

	// Check if the eino tool is invokable (check this first since InvokableTool is more specific)
	if invokeTool, ok := baseTool.(einotool.InvokableTool); ok {
		// Check if it also supports streaming
		if streamTool, ok := baseTool.(einotool.StreamableTool); ok {
			return itool.NewStreamable(baseTool, streamTool, internalConfig)
		}

		// Only invokable, not streamable
		return itool.NewCallable(baseTool, invokeTool, internalConfig)
	}

	// Fallback: treat as basic tool (this should rarely happen in practice)
	return itool.NewReadOnly(baseTool, internalConfig)
}
