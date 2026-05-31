package trag_test

import (
	"context"
	"testing"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/tool/trag"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestNewToolSet_withFunctions(t *testing.T) {
	t.Skip("skip test")
	ctx := context.Background()

	// Create a toolset with specific functions
	toolset, err := trag.NewToolSet(
		ctx,
		"doc_format_transformer",
		trag.WithFuncNames("iwiki2md", "csv2md"),
	)
	require.NoError(t, err)
	defer toolset.Close()

	tools := toolset.Tools(ctx)
	require.Len(t, tools, 2)
	require.Equal(t, "csv2md", tools[0].Declaration().Name)
	t.Logf("tools[0].Declaration().InputSchema(): %+v", tools[0].Declaration().InputSchema)

	require.Equal(t, "iwiki2md", tools[1].Declaration().Name)
	t.Logf("tools[1].Declaration().InputSchema(): %+v", tools[1].Declaration().InputSchema)
}

func TestNewToolSet_FunctionCall(t *testing.T) {
	t.Skip("skip test")
	ctx := context.Background()

	// Create a toolset with specific functions
	toolset, err := trag.NewToolSet(
		ctx,
		"weather_search",
		trag.WithFuncNames("weather_search"),
	)
	require.NoError(t, err)
	defer toolset.Close()

	tools := toolset.Tools(ctx)
	require.Len(t, tools, 1)
	require.Equal(t, "weather_search", tools[0].Declaration().Name)
	weatherTool := tools[0]

	result, err := weatherTool.(tool.CallableTool).Call(ctx, []byte(`{"keyword": "Beijing"}`))
	require.NoError(t, err)
	t.Logf("result: %+v", result)

	t.Logf("tools[0].Declaration().InputSchema(): %+v", tools[0].Declaration().InputSchema)
}
