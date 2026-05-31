package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestAppendToolCallsDeduplicates(t *testing.T) {
	call := model.ToolCall{
		ID: "call-1",
		Function: model.FunctionDefinitionParam{
			Name:      transferToolName,
			Arguments: []byte(`{"target":"sandbox"}`),
		},
	}

	trace := appendToolCalls(
		nil,
		"primary",
		[]model.ToolCall{call},
		map[string]struct{}{},
	)
	trace = appendToolCalls(
		trace,
		"primary",
		[]model.ToolCall{call},
		map[string]struct{}{toolCallKey("primary", call): {}},
	)
	require.Len(t, trace, 1)
	require.Contains(t, trace[0], transferToolName)
}

func TestFormatToolCall(t *testing.T) {
	call := model.ToolCall{
		ID: "call-2",
		Function: model.FunctionDefinitionParam{
			Name:      transferToolName,
			Arguments: []byte(`{"target":"weather"}`),
		},
	}

	formatted := formatToolCall("primary", call)
	require.Contains(t, formatted, "primary")
	require.Contains(t, formatted, transferToolName)
	require.Contains(t, formatted, "weather")
}

func TestTrimLeadingJSONEnvelope(t *testing.T) {
	answer := `{"stdout":"raw"}Final answer`
	require.Equal(t, "Final answer", trimLeadingJSONEnvelope(answer))

	require.Equal(
		t,
		`{"stdout":"raw"}`,
		trimLeadingJSONEnvelope(`{"stdout":"raw"}`),
	)
}
