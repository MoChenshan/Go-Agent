package converters

import (
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ConvertEinoMessageToEvent converts an eino schema Message to a trpc-agent-go Event.
// This is used for streaming conversion where each eino message chunk becomes an event.
func ConvertEinoMessageToEvent(einoMsg *schema.Message, invocationID, author string) *event.Event {
	// Convert the eino message to trpc-agent message
	trpcMsg := ConvertFromEinoMessage(einoMsg)

	// Create the event with proper structure
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{
					Index:   0,
					Message: trpcMsg,
					Delta: model.Message{
						Content: einoMsg.Content,
						Role:    trpcMsg.Role,
					},
				},
			},
			Done: false, // Will be set to true on completion
		},
		InvocationID: invocationID,
		Author:       author,
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
	}

	// Handle tool calls in the event
	if len(einoMsg.ToolCalls) > 0 {
		evt.Response.Choices[0].Delta.ToolCalls = trpcMsg.ToolCalls
	}

	return evt
}

// CreateCompletionEvent creates a completion event to signal the end of processing.
func CreateCompletionEvent(invocationID, author string, finalMessage model.Message) *event.Event {
	return &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{
					Index:        0,
					Message:      finalMessage,
					FinishReason: stringPtr("stop"),
				},
			},
			Done: true,
		},
		InvocationID: invocationID,
		Author:       author,
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
	}
}

// CreateErrorEvent creates an error event when something goes wrong.
func CreateErrorEvent(invocationID, author string, err error) *event.Event {
	return &event.Event{
		Response: &model.Response{
			Error: &model.ResponseError{
				Type:    "eino_adapter_error",
				Message: err.Error(),
			},
			Done: true,
		},
		InvocationID: invocationID,
		Author:       author,
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
	}
}

// stringPtr returns a pointer to the given string.
func stringPtr(s string) *string {
	return &s
}
