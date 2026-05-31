// Package trag provides tRAG tool integration for agent systems.
//
//	https://ai.woa.com/#/trag/tools/list
//
// It enables agents to execute remote tools hosted on the tRAG platform.
package trag

import (
	"context"
	"errors"
	"fmt"
	"os"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/tool/trag/internal/client"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	defaultNamePrefix = "tRAG"
	envAPIKey         = "TRAG_API_KEY"
)

// NewToolSet creates a new TRAG toolset with the specified name and options.
// The toolset queries available tools from the TRAG platform and returns them
// as a collection that implements the standard tool.Tool interface.
//
// Parameters:
//   - ctx: Context for the operation
//   - name: The toolset name (tools code) to query from TRAG
//   - opts: Optional configuration options (WithAPIKey, WithFuncNames)
//
// The API key can be provided via WithAPIKey option or TRAG_API_KEY environment variable.
// If no function names are specified, all available functions in the toolset are loaded.
func NewToolSet(ctx context.Context, name string, opts ...Option) (*ToolSet, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}

	cfg := Options{
		apiKey: os.Getenv(envAPIKey),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	c, err := client.NewClient(cfg.apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	tools, err := c.GetTools(ctx, name, cfg.funcNames)
	if err != nil {
		return nil, fmt.Errorf("failed to get tools: %w", err)
	}

	return &ToolSet{name: fmt.Sprintf("%s_%s", defaultNamePrefix, name), tools: tools}, nil
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
// Currently this is a no-op as TRAG tools don't maintain persistent connections,
// but it's provided for interface compatibility and future extensibility.
func (t *ToolSet) Close() error {
	return nil
}

// Name returns the name of this toolset.
// The name is prefixed with "trag_" to distinguish it from other toolsets.
func (t *ToolSet) Name() string {
	return t.name
}
