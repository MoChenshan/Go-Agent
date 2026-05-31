package debug

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

func TestConvertSessionToADKFormat(t *testing.T) {
	now := time.Now()
	sess := &session.Session{
		ID:        "test-session-id",
		AppName:   "test-app",
		UserID:    "test-user",
		CreatedAt: now,
		UpdatedAt: now,
		State:     map[string][]byte{"key1": []byte("value1")},
	}

	adkSession := convertSessionToADKFormat(sess)

	assert.Equal(t, "test-session-id", adkSession.ID)
	assert.Equal(t, "test-app", adkSession.AppName)
	assert.Equal(t, "test-user", adkSession.UserID)
	assert.NotZero(t, adkSession.CreateTime)
	assert.NotZero(t, adkSession.LastUpdateTime)
	assert.Len(t, adkSession.State, 1)
}

func TestConvertSessionToADKFormat_WithEvents(t *testing.T) {
	now := time.Unix(0, 0)
	sess := &session.Session{
		ID:        "test-session-id",
		AppName:   "test-app",
		UserID:    "test-user",
		CreatedAt: now,
		UpdatedAt: now,
	}
	sess.Events = append(
		sess.Events,
		*newAssistantFinalEvent("invocation-1", "hello"),
	)

	adkSession := convertSessionToADKFormat(sess)

	assert.Len(t, adkSession.Events, 1)
	assert.Equal(
		t,
		"invocation-1",
		adkSession.Events[0]["invocationId"],
	)
}

func TestBuildFunctionCallPart_InvalidJSON(t *testing.T) {
	part := buildFunctionCallPart(model.ToolCall{
		ID: "tool-1",
		Function: model.FunctionDefinitionParam{
			Name:      "fn",
			Arguments: []byte("invalid"),
		},
	})

	call, ok := part[keyFunctionCall].(map[string]any)
	assert.True(t, ok)

	args, ok := call["args"].(map[string]any)
	assert.True(t, ok)

	assert.Equal(t, "invalid", args["raw"])
}

func TestConvertEventToADKFormat_StreamingSkipsDoneEvent(t *testing.T) {
	evt := &event.Event{
		InvocationID: "inv",
		Author:       "assistant",
		ID:           "event-id",
		Timestamp:    time.Unix(0, 0),
		Response: &model.Response{
			Done:      true,
			IsPartial: false,
			Choices: []model.Choice{
				{
					Message: model.Message{
						Content: "final output",
						Role:    model.RoleAssistant,
					},
				},
			},
		},
	}

	res := convertEventToADKFormat(evt, true)
	assert.Nil(t, res)
}

func TestConvertEventToADKFormat_NonStreamingKeepsToolCall(t *testing.T) {
	evt := &event.Event{
		InvocationID: "inv",
		Author:       "assistant",
		ID:           "event-id",
		Timestamp:    time.Unix(0, 0),
		Response: &model.Response{
			Done: false,
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role: model.RoleAssistant,
						ToolCalls: []model.ToolCall{
							{
								ID: "tool-1",
								Function: model.FunctionDefinitionParam{
									Name:      "fn",
									Arguments: []byte(`{"x":1}`),
								},
							},
						},
					},
				},
			},
		},
	}

	res := convertEventToADKFormat(evt, false)
	assert.NotNil(t, res)

	content, ok := res["content"].(map[string]any)
	assert.True(t, ok)
	parts, ok := content["parts"].([]map[string]any)
	assert.True(t, ok)
	assert.Len(t, parts, 1)
	call, ok := parts[0][keyFunctionCall].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "fn", call["name"])
}

func TestConvertEventToADKFormat_ToolResponseIncludesMetadata(t *testing.T) {
	evt := &event.Event{
		InvocationID: "inv",
		Author:       "assistant",
		ID:           "event-id",
		Timestamp:    time.Unix(0, 0),
		Response: &model.Response{
			Done:   true,
			Object: model.ObjectTypeToolResponse,
			Model:  "test-model",
			Usage: &model.Usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
			Choices: []model.Choice{
				{
					Message: model.Message{
						Content:  `{"result":"ok"}`,
						ToolID:   "tool-1",
						ToolName: "tool",
					},
				},
			},
		},
	}

	res := convertEventToADKFormat(evt, false)
	assert.NotNil(t, res)
	assert.Equal(t, true, res["done"])
	assert.Equal(t, "tool.response", res["object"])
	assert.Equal(t, "test-model", res["model"])
	usage, ok := res["usageMetadata"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, 10, usage["promptTokenCount"])

	content, ok := res["content"].(map[string]any)
	assert.True(t, ok)
	parts, ok := content["parts"].([]map[string]any)
	assert.True(t, ok)
	assert.Len(t, parts, 1)
	respPart, ok := parts[0][keyFunctionResponse].(map[string]any)
	assert.True(t, ok)
	responsePayload, ok := respPart["response"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "ok", responsePayload["result"])
}
