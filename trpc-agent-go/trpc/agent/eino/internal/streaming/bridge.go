package streaming

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/converters"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// StreamBridge 桥接 eino 流式执行到 trpc-agent-go 事件流。
type StreamBridge struct {
	name         string
	invocation   *agent.Invocation
	eventChan    chan<- *event.Event
	enableDebug  bool
	maxChunkSize int
}

// NewStreamBridge creates a new StreamBridge.
func NewStreamBridge(name string, invocation *agent.Invocation, eventChan chan<- *event.Event) *StreamBridge {
	return &StreamBridge{
		name:         name,
		invocation:   invocation,
		eventChan:    eventChan,
		enableDebug:  false,
		maxChunkSize: 1024, // Default chunk size
	}
}

// WithDebug enables debug logging for the stream bridge.
func (sb *StreamBridge) WithDebug(enable bool) *StreamBridge {
	sb.enableDebug = enable
	return sb
}

// WithMaxChunkSize sets the maximum chunk size for streaming.
func (sb *StreamBridge) WithMaxChunkSize(size int) *StreamBridge {
	sb.maxChunkSize = size
	return sb
}

// HandleExecution handles eino component execution with proper streaming support.
// This unified method works for Chain, Graph, Workflow, and any other eino Runnable.
func (sb *StreamBridge) HandleExecution(
	ctx context.Context,
	runnable compose.Runnable[map[string]any, any],
	input map[string]any,
) error {
	if sb.enableDebug {
		sb.sendEvent(ctx, &event.Event{
			Response: &model.Response{
				Object: "debug",
				Done:   false,
			},
			InvocationID: sb.invocation.InvocationID,
			Author:       sb.name,
			Timestamp:    time.Now(),
		})
	}

	// Try eino's native Stream interface first
	streamReader, err := runnable.Stream(ctx, input)
	if err != nil {
		if sb.enableDebug {
			sb.sendEvent(ctx, &event.Event{
				Response: &model.Response{
					Object: "debug",
					Done:   false,
				},
				InvocationID: sb.invocation.InvocationID,
				Author:       sb.name,
				Timestamp:    time.Now(),
			})
		}
		// Fallback to non-streaming
		return sb.handleNonStreamingFallback(ctx, runnable, input)
	}

	// Process the eino stream
	if streamReader != nil {
		return sb.processEinoStream(ctx, streamReader)
	}

	// If streamReader is nil, fallback to non-streaming
	return sb.handleNonStreamingFallback(ctx, runnable, input)
}

// processEinoStream processes the eino stream and converts to trpc-agent events.
func (sb *StreamBridge) processEinoStream(ctx context.Context, streamReader *schema.StreamReader[any]) error {
	defer streamReader.Close()

	chunkCount := 0
	var accumulatedContent string

	for {
		select {
		case <-ctx.Done():
			sb.sendErrorEvent(ctx, ctx.Err())
			return ctx.Err()
		default:
			// Continue processing
		}

		// Receive next chunk from eino stream
		chunk, err := streamReader.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// End of stream - send final completion event
				sb.sendCompletionEvent(ctx, accumulatedContent, chunkCount)
				return nil
			}
			// Stream error
			sb.sendErrorEvent(ctx, err)
			return err
		}

		// Convert chunk to trpc-agent event
		chunkEvent := sb.convertChunkToEvent(chunk, chunkCount, false)
		if chunkEvent != nil {
			sb.sendEvent(ctx, chunkEvent)

			// Track accumulated content for final message
			if msg := sb.extractMessageFromChunk(chunk); msg != nil {
				accumulatedContent += msg.Content
			}

			chunkCount++
		}

		// Optional: Add small delay to prevent overwhelming the consumer
		if chunkCount%10 == 0 {
			time.Sleep(1 * time.Millisecond)
		}
	}
}

// convertChunkToEvent converts an eino stream chunk to a trpc-agent event.
func (sb *StreamBridge) convertChunkToEvent(chunk any, chunkIndex int, isComplete bool) *event.Event {
	// Use centralized chunk conversion logic
	msg := converters.ConvertChunkToEinoMessage(chunk)
	return sb.createMessageEvent(msg, chunkIndex, isComplete)
}

// createMessageEvent creates a trpc-agent event from an eino message.
func (sb *StreamBridge) createMessageEvent(msg *schema.Message, chunkIndex int, isComplete bool) *event.Event {
	// Create streaming event with delta content
	evt := converters.ConvertEinoMessageToEvent(msg, sb.invocation.InvocationID, sb.name)

	// Set streaming properties
	evt.Response.Done = isComplete

	// For streaming, we want delta content in the first choice
	if len(evt.Response.Choices) > 0 {
		choice := &evt.Response.Choices[0]
		choice.Delta.Content = msg.Content
		choice.Delta.Role = converters.ConvertEinoRoleToModelRole(msg.Role)

		// Handle tool calls in streaming
		if len(msg.ToolCalls) > 0 {
			choice.Delta.ToolCalls = converters.ConvertFromEinoToolCalls(msg.ToolCalls)
		}
	}

	// Add timing metadata for debugging if enabled
	if sb.enableDebug {
		evt.Timestamp = time.Now()
	}

	return evt
}

// handleNonStreamingFallback handles non-streaming execution as a fallback.
func (sb *StreamBridge) handleNonStreamingFallback(
	ctx context.Context,
	runnable compose.Runnable[map[string]any, any],
	input map[string]any,
) error {
	// Execute non-streaming
	result, err := runnable.Invoke(ctx, input)
	if err != nil {
		sb.sendErrorEvent(ctx, err)
		return err
	}

	// Convert result to streaming-like events
	chunkEvent := sb.convertChunkToEvent(result, 0, true)
	if chunkEvent != nil {
		sb.sendEvent(ctx, chunkEvent)
	}

	// Send completion
	sb.sendCompletionEvent(ctx, sb.extractContentFromResult(result), 1)
	return nil
}

// HandleReactAgentExecution handles execution of eino ReAct Agent with streaming support.
func (sb *StreamBridge) HandleReactAgentExecution(ctx context.Context, reactAgent *react.Agent, input []*schema.Message) error {
	// Try streaming first
	streamReader, err := reactAgent.Stream(ctx, input)
	if err != nil {
		// Fallback to non-streaming
		if sb.enableDebug {
			sb.sendEvent(ctx, &event.Event{
				Response: &model.Response{
					Object: "error",
					Done:   false,
					Error: &model.ResponseError{
						Type:    "STREAM_FALLBACK",
						Message: "ReAct Agent streaming failed, falling back to non-streaming",
					},
				},
				InvocationID: sb.invocation.InvocationID,
				Author:       sb.name,
				Timestamp:    time.Now(),
			})
		}
		return sb.handleReactAgentNonStreamingFallback(ctx, reactAgent, input)
	}

	// Process the stream
	if streamReader != nil {
		defer streamReader.Close()
		return sb.processReactAgentStream(ctx, streamReader)
	}

	return errors.New("react agent returned nil stream reader")
}

// processReactAgentStream processes the ReAct Agent stream and converts to events.
func (sb *StreamBridge) processReactAgentStream(ctx context.Context, streamReader *schema.StreamReader[*schema.Message]) error {
	chunkIndex := 0
	normalCompletion := false

	for {
		msg, err := streamReader.Recv()
		if err == io.EOF {
			// Stream completed successfully
			normalCompletion = true
			break
		}
		if err != nil {
			sb.sendEvent(ctx, &event.Event{
				Response: &model.Response{
					Object: "error",
					Error: &model.ResponseError{
						Type:    "REACT_STREAM_ERROR",
						Message: fmt.Sprintf("Failed to read from ReAct agent stream: %v", err),
					},
				},
				InvocationID: sb.invocation.InvocationID,
				Author:       sb.name,
				Timestamp:    time.Now(),
			})
			return err
		}

		// Create and send event for this chunk
		evt := sb.createMessageEvent(msg, chunkIndex, false)
		sb.sendEvent(ctx, evt)

		chunkIndex++

		// Apply chunk size limit if configured
		if sb.maxChunkSize > 0 && chunkIndex >= sb.maxChunkSize {
			// Always send notification when chunk limit is reached, regardless of debug mode
			sb.sendEvent(ctx, &event.Event{
				Response: &model.Response{
					Object: "error",
					Done:   false,
					Error: &model.ResponseError{
						Type:    "CHUNK_LIMIT_REACHED",
						Message: "Maximum chunk size reached for ReAct Agent stream",
					},
				},
				InvocationID: sb.invocation.InvocationID,
				Author:       sb.name,
				Timestamp:    time.Now(),
			})
			// Don't set normalCompletion = true, as this is an abnormal termination
			break
		}
	}

	// Only send completion event if stream completed normally
	if normalCompletion {
		sb.sendCompletionEvent(ctx, "", chunkIndex)
	}
	return nil
}

// handleReactAgentNonStreamingFallback handles non-streaming execution as fallback.
func (sb *StreamBridge) handleReactAgentNonStreamingFallback(ctx context.Context, reactAgent *react.Agent, input []*schema.Message) error {
	// Use the Generate method for non-streaming
	result, err := reactAgent.Generate(ctx, input)
	if err != nil {
		sb.sendEvent(ctx, &event.Event{
			Response: &model.Response{
				Object: "error",
				Error: &model.ResponseError{
					Type:    "REACT_GENERATE_ERROR",
					Message: fmt.Sprintf("Failed to generate response from ReAct agent: %v", err),
				},
			},
			InvocationID: sb.invocation.InvocationID,
			Author:       sb.name,
			Timestamp:    time.Now(),
		})
		return err
	}

	// Convert the result to event and send
	evt := sb.createMessageEvent(result, 0, true)
	sb.sendEvent(ctx, evt)

	// Send completion event
	sb.sendCompletionEvent(ctx, "", 0)
	return nil
}

// extractMessageFromChunk extracts a message from a chunk for content tracking.
func (sb *StreamBridge) extractMessageFromChunk(chunk any) *schema.Message {
	// Reuse the content extraction logic and wrap in Message
	content := sb.extractContentFromResult(chunk)
	if content == "" {
		return nil
	}
	return &schema.Message{Content: content}
}

// extractContentFromResult extracts string content from a result.
func (sb *StreamBridge) extractContentFromResult(result any) string {
	switch v := result.(type) {
	case *schema.Message:
		return v.Content
	case string:
		return v
	case map[string]any:
		if content, ok := v["content"].(string); ok {
			return content
		}
		return converters.FormatStructuredData(v)
	default:
		return converters.SafeStringify(v)
	}
}

// sendEvent sends an event to the event channel.
func (sb *StreamBridge) sendEvent(ctx context.Context, evt *event.Event) {
	_ = agent.EmitEvent(ctx, sb.invocation, sb.eventChan, evt)
}

// sendErrorEvent sends an error event.
func (sb *StreamBridge) sendErrorEvent(ctx context.Context, err error) {
	evt := converters.CreateErrorEvent(sb.invocation.InvocationID, sb.name, err)
	sb.sendEvent(ctx, evt)
}

// sendCompletionEvent sends a completion event.
func (sb *StreamBridge) sendCompletionEvent(ctx context.Context, finalContent string, chunkCount int) {
	finalMessage := converters.CreateFinalMessage(finalContent)
	evt := converters.CreateCompletionEvent(sb.invocation.InvocationID, sb.name, finalMessage)

	if sb.enableDebug {
		evt.Timestamp = time.Now()
	}

	sb.sendEvent(ctx, evt)
}
