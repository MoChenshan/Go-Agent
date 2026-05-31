package debug

import (
	"encoding/json"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

// ---------------------------------------------------------------------
// Graph Agent Event Processing ----------------------------------------
// ---------------------------------------------------------------------

// buildGraphEventParts handles Graph events that store information in
// StateDelta.
// It returns the parts array for Graph tool execution events, or an empty
// slice for non-Graph tool events.
func buildGraphEventParts(e *event.Event) []map[string]any {
	var parts []map[string]any

	// Handle graph.execution events (final results)
	if e.Object == graph.ObjectTypeGraphExecution {
		if e.Response == nil || len(e.Response.Choices) == 0 {
			return parts
		}
		content := e.Response.Choices[0].Message.Content
		if content == "" {
			return parts
		}
		parts = append(parts, map[string]any{"text": content})
		return parts
	}

	// Only process Graph node execution events for tool calls
	if e.Object == graph.ObjectTypeGraphNodeExecution {
		// Continue to tool execution processing below
	} else if strings.HasPrefix(e.Object, "graph.") {
		// Other graph events, return empty parts unless they have special handling
		return parts
	} else {
		// Not a graph event
		return parts
	}

	// Check for tool execution metadata in StateDelta
	if e.StateDelta == nil {
		return parts
	}

	toolMetadataBytes, exists := e.StateDelta[graph.MetadataKeyTool]
	if !exists {
		return parts
	}

	// Parse tool execution metadata
	var toolMetadata struct {
		ToolName string `json:"toolName"`
		ToolID   string `json:"toolId"`
		Phase    string `json:"phase"`
		Input    string `json:"input,omitempty"`
		Output   string `json:"output,omitempty"`
	}

	if err := json.Unmarshal(toolMetadataBytes, &toolMetadata); err != nil {
		return parts
	}

	// Convert Graph tool events to ADK format
	// Strategy: Only show tool responses (complete/error), skip tool calls
	// (start). This avoids duplication with LLM Agent tool calls while still
	// showing results.
	switch toolMetadata.Phase {
	case "start":
		// Skip tool execution start to avoid duplication with LLM
		// chat.completion tool calls.

	case "complete":
		// Tool execution complete -> functionResponse
		parts = append(parts, buildGraphFunctionResponsePart(toolMetadata))

	case "error":
		// Tool execution error -> functionResponse with error
		parts = append(parts, buildGraphFunctionErrorPart(toolMetadata))
	}
	return parts
}

// buildGraphFunctionResponsePart builds a functionResponse part from Graph
// tool metadata.
func buildGraphFunctionResponsePart(toolMetadata struct {
	ToolName string `json:"toolName"`
	ToolID   string `json:"toolId"`
	Phase    string `json:"phase"`
	Input    string `json:"input,omitempty"`
	Output   string `json:"output,omitempty"`
}) map[string]any {
	var respObj any
	if toolMetadata.Output != "" {
		outputBts := []byte(toolMetadata.Output)
		if err := json.Unmarshal(outputBts, &respObj); err != nil {
			// Preserve raw string if not valid JSON
			respObj = toolMetadata.Output
		}
	} else {
		respObj = "No output"
	}

	return map[string]any{
		keyFunctionResponse: map[string]any{
			"name":     toolMetadata.ToolName,
			"response": respObj,
			"id":       toolMetadata.ToolID,
		},
	}
}

// buildGraphFunctionErrorPart builds a functionResponse part for Graph tool
// errors.
func buildGraphFunctionErrorPart(toolMetadata struct {
	ToolName string `json:"toolName"`
	ToolID   string `json:"toolId"`
	Phase    string `json:"phase"`
	Input    string `json:"input,omitempty"`
	Output   string `json:"output,omitempty"`
}) map[string]any {
	errorMsg := "Tool execution failed"
	if toolMetadata.Output != "" {
		errorMsg = toolMetadata.Output
	}

	return map[string]any{
		keyFunctionResponse: map[string]any{
			"name":     toolMetadata.ToolName,
			"response": map[string]any{"error": errorMsg},
			"id":       toolMetadata.ToolID,
		},
	}
}

// isGraphEvent checks if the event is a Graph-related event.
func isGraphEvent(e *event.Event) bool {
	return strings.HasPrefix(e.Object, "graph.")
}

// filterGraphEventParts handles filtering for Graph events only.
func filterGraphEventParts(
	e *event.Event,
	parts []map[string]any,
	isStreaming bool,
) []map[string]any {
	// For Graph tool execution events, always include them (they have
	// functionCall/functionResponse).
	if isGraphToolEvent(e) && len(parts) > 0 {
		return parts
	}

	// For Graph execution completion events (final result), always include
	// so that streaming UIs receive the terminal text as well.
	if e.Object == graph.ObjectTypeGraphExecution && len(parts) > 0 {
		return parts
	}

	// Skip all other Graph events to avoid duplicates
	return nil
}

// isGraphToolEvent checks if the event is a Graph tool execution event.
func isGraphToolEvent(e *event.Event) bool {
	if e.Object != graph.ObjectTypeGraphNodeExecution {
		return false
	}
	if e.StateDelta == nil {
		return false
	}
	_, exists := e.StateDelta[graph.MetadataKeyTool]
	return exists
}
