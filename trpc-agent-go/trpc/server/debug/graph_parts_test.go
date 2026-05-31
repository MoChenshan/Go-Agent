package debug

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestBuildGraphEventParts_ToolPhases(t *testing.T) {
	metadataComplete := map[string]string{
		"toolName": "tool",
		"toolId":   "tool-1",
		"phase":    "complete",
		"output":   `{"answer":42}`,
	}
	completeBytes, err := json.Marshal(metadataComplete)
	assert.NoError(t, err)

	eComplete := &event.Event{
		Response: &model.Response{
			Object: graph.ObjectTypeGraphNodeExecution,
		},
		StateDelta: map[string][]byte{
			graph.MetadataKeyTool: completeBytes,
		},
	}

	partsComplete := buildGraphEventParts(eComplete)
	assert.Len(t, partsComplete, 1)
	compResp, ok := partsComplete[0][keyFunctionResponse].(map[string]any)
	assert.True(t, ok)
	compData, ok := compResp["response"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, float64(42), compData["answer"])

	metadataError := map[string]string{
		"toolName": "tool",
		"toolId":   "tool-1",
		"phase":    "error",
		"output":   "failed",
	}
	errorBytes, err := json.Marshal(metadataError)
	assert.NoError(t, err)

	eError := &event.Event{
		Response: &model.Response{
			Object: graph.ObjectTypeGraphNodeExecution,
		},
		StateDelta: map[string][]byte{
			graph.MetadataKeyTool: errorBytes,
		},
	}

	partsError := buildGraphEventParts(eError)
	assert.Len(t, partsError, 1)
	errResp, ok := partsError[0][keyFunctionResponse].(map[string]any)
	assert.True(t, ok)
	errData, ok := errResp["response"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "failed", errData["error"])

	eStart := &event.Event{
		Response: &model.Response{
			Object: graph.ObjectTypeGraphNodeExecution,
		},
		StateDelta: map[string][]byte{
			graph.MetadataKeyTool: []byte(`{"phase":"start"}`),
		},
	}
	partsStart := buildGraphEventParts(eStart)
	assert.Len(t, partsStart, 0)
}

func TestBuildGraphEventParts_GraphExecution(t *testing.T) {
	e := &event.Event{
		Response: &model.Response{
			Object: graph.ObjectTypeGraphExecution,
			Choices: []model.Choice{
				{Message: model.Message{Content: "graph final"}},
			},
		},
	}

	parts := buildGraphEventParts(e)
	assert.Len(t, parts, 1)
	assert.Equal(t, "graph final", parts[0][keyText])
}

func TestBuildGraphEventParts_InvalidMetadata(t *testing.T) {
	e := &event.Event{
		Response: &model.Response{
			Object: graph.ObjectTypeGraphNodeExecution,
		},
		StateDelta: map[string][]byte{
			graph.MetadataKeyTool: []byte(`not-json`),
		},
	}

	parts := buildGraphEventParts(e)
	assert.Len(t, parts, 0)
}
func TestFilterGraphEventPartsVariants(t *testing.T) {
	parts := []map[string]any{{"key": "value"}}
	toolEvent := &event.Event{
		Response: &model.Response{Object: graph.ObjectTypeGraphNodeExecution},
		StateDelta: map[string][]byte{
			graph.MetadataKeyTool: []byte(`{}`),
		},
	}
	result := filterGraphEventParts(toolEvent, parts, true)
	assert.Equal(t, parts, result)

	execEvent := &event.Event{
		Response: &model.Response{Object: graph.ObjectTypeGraphExecution},
	}
	result = filterGraphEventParts(execEvent, parts, false)
	assert.Equal(t, parts, result)

	otherEvent := &event.Event{
		Response: &model.Response{Object: graph.ObjectTypeGraphNodeExecution},
	}
	result = filterGraphEventParts(otherEvent, parts, true)
	assert.Nil(t, result)
}

func TestIsGraphToolEventBranches(t *testing.T) {
	meta := map[string][]byte{
		graph.MetadataKeyTool: []byte(`{}`),
	}
	eventWithTool := &event.Event{
		Response:   &model.Response{Object: graph.ObjectTypeGraphNodeExecution},
		StateDelta: meta,
	}
	assert.True(t, isGraphToolEvent(eventWithTool))

	eventWithoutTool := &event.Event{
		Response: &model.Response{Object: graph.ObjectTypeGraphNodeExecution},
	}
	assert.False(t, isGraphToolEvent(eventWithoutTool))

	eventWrongType := &event.Event{
		Response: &model.Response{Object: graph.ObjectTypeGraphExecution},
		StateDelta: map[string][]byte{
			graph.MetadataKeyTool: []byte(`{}`),
		},
	}
	assert.False(t, isGraphToolEvent(eventWrongType))
}
