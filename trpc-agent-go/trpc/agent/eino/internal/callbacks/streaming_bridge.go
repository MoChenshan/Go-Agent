package callbacks

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/schema"
	"trpc.group/trpc-go/trpc-agent-go/event"
)

// StreamingCallbackBridge bridges tRPC event streams to Eino streaming callbacks.
// This enables complex streaming callback logic like ChatBuffer to work with tRPC agents.
type StreamingCallbackBridge struct {
	einoHandler callbacks.Handler
	config      *CallbackConfig
}

// NewStreamingCallbackBridge creates a new streaming callback bridge.
func NewStreamingCallbackBridge(handler callbacks.Handler, config *CallbackConfig) *StreamingCallbackBridge {
	return &StreamingCallbackBridge{
		einoHandler: handler,
		config:      config,
	}
}

// InterceptEventStream intercepts a tRPC event stream and bridges it to Eino streaming callbacks.
// This allows Eino OnEndWithStreamOutput callbacks to process tRPC event streams.
func (bridge *StreamingCallbackBridge) InterceptEventStream(
	ctx context.Context,
	originalStream <-chan *event.Event,
) <-chan *event.Event {
	bridgedStream := make(chan *event.Event, 256)

	go func() {
		defer close(bridgedStream)

		// Create Eino stream using the official Pipe function
		streamReader, streamWriter := schema.Pipe[callbacks.CallbackOutput](256)

		// Start Eino streaming callback in a separate goroutine
		runInfo := &callbacks.RunInfo{
			Name: "streaming_model",
			Type: "model",
		}

		go func() {
			defer streamReader.Close()
			bridge.einoHandler.OnEndWithStreamOutput(ctx, runInfo, streamReader)
		}()

		// Process tRPC event stream
		streamClosed := false
		for event := range originalStream {

			// Forward event to downstream
			bridgedStream <- event

			// Convert event to Eino format and send to streaming callback
			if einoChunk := bridge.convertEventToEinoChunk(event); einoChunk != nil {
				if closed := streamWriter.Send(einoChunk, nil); closed {
					// Stream was closed, stop processing further events
					streamClosed = true
					break
				}
			}
		}

		// Stream ended, close Eino stream if not already closed
		if !streamClosed {
			streamWriter.Close()
		}

	}()

	return bridgedStream
}

// convertEventToEinoChunk converts a tRPC event to Eino CallbackOutput.
func (bridge *StreamingCallbackBridge) convertEventToEinoChunk(event *event.Event) callbacks.CallbackOutput {
	if event == nil || len(event.Choices) == 0 {
		return nil
	}

	choice := event.Choices[0]

	// Determine content from either Delta (streaming) or Message (non-streaming)
	content := choice.Delta.Content
	role := choice.Delta.Role
	if content == "" && choice.Message.Content != "" {
		content = choice.Message.Content
		role = choice.Message.Role
	}

	// Create Eino-compatible message structure
	einoMessage := map[string]any{
		"role":    role,
		"content": content,
	}

	// Add tool calls if present
	if len(choice.Message.ToolCalls) > 0 {
		toolCalls := make([]map[string]any, len(choice.Message.ToolCalls))
		for i, toolCall := range choice.Message.ToolCalls {
			toolCalls[i] = map[string]any{
				"id":   toolCall.ID,
				"type": toolCall.Type,
				"function": map[string]any{
					"name":      toolCall.Function.Name,
					"arguments": string(toolCall.Function.Arguments),
				},
			}
		}
		einoMessage["tool_calls"] = toolCalls
	}

	// Add tool ID if present (for tool responses)
	if choice.Message.ToolID != "" {
		einoMessage["tool_id"] = choice.Message.ToolID
	}

	// Create the callback output structure that mimics Eino's schema.Message
	return einoMessage
}

// IsStreamingCapable checks if the Eino handler supports streaming callbacks.
func (bridge *StreamingCallbackBridge) IsStreamingCapable() bool {
	// For now, we assume all handlers support streaming.
	// This could be enhanced by checking if the handler implements
	// the OnEndWithStreamOutput method properly.
	return true
}
