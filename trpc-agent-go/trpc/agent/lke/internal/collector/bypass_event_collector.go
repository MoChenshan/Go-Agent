// Package collector provides internal event collection functionality for LKE Agent.
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	lkeevent "github.com/tencent-lke/lke-sdk-go/event"
	lkeeventhandler "github.com/tencent-lke/lke-sdk-go/eventhandler"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// BypassEventCollector implements eventhandler.EventHandler interface
// and bridges LKE callback events to trpc-agent-go event streams.
// This allows the original LKE callback logic to continue working
// while simultaneously providing events to the trpc-agent-go ecosystem.
type BypassEventCollector struct {
	// original wraps the user's existing EventHandler
	original lkeeventhandler.EventHandler
	// ctx is current run context used for event emission/cancellation.
	ctx context.Context
	// invocation is current invocation used for metadata injection.
	invocation *agent.Invocation
	// eventChan is the bypass channel to send events to trpc-agent-go
	eventChan chan *event.Event
	// processing indicates if we're currently processing a request
	processing bool
	// invocationID tracks the current invocation
	invocationID string
	// enableBypass controls whether to send events to trpc-agent-go
	enableBypass bool
	// agentName is the name of the agent for event identification
	agentName string
	// debug enables debug logging for event handling
	debug bool
	// finalReplyObserved tracks whether a final reply event was observed in current processing
	finalReplyObserved bool
	mu                 sync.RWMutex
}

// New creates a new bypass event collector.
func New(original lkeeventhandler.EventHandler, enableBypass bool, agentName string, debug bool) *BypassEventCollector {
	return &BypassEventCollector{
		original:     original,
		enableBypass: enableBypass,
		agentName:    agentName,
		debug:        debug,
	}
}

// StartProcessing enables event collection to the bypass channel.
func (b *BypassEventCollector) StartProcessing(
	ctx context.Context,
	eventChan chan *event.Event,
	invocation *agent.Invocation,
) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	b.ctx = ctx
	b.invocation = invocation
	b.eventChan = eventChan
	b.invocationID = ""
	if invocation != nil {
		b.invocationID = invocation.InvocationID
	}
	b.processing = true
	b.finalReplyObserved = false
}

// StopProcessing disables event collection.
func (b *BypassEventCollector) StopProcessing() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.processing = false
	b.ctx = nil
	b.invocation = nil
	b.eventChan = nil
	b.invocationID = ""
	b.finalReplyObserved = false
}

// Cleanup performs resource cleanup for the event collector.
// This should be called when the collector is no longer needed.
func (b *BypassEventCollector) Cleanup() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Stop processing if still active
	b.processing = false
	b.ctx = nil
	b.invocation = nil
	b.eventChan = nil
	b.invocationID = ""
	b.finalReplyObserved = false

	// Clear original handler reference to help garbage collection
	b.original = nil

	if b.debug {
		log.Debugf("[LKEAgent:%s] Event collector cleanup completed", b.agentName)
	}
}

// HasFinalReplyEvent reports whether a final reply event has already been observed in current processing.
func (b *BypassEventCollector) HasFinalReplyEvent() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.finalReplyObserved
}

// sendEvent safely sends an event to the bypass channel if processing is active and bypass is enabled.
func (b *BypassEventCollector) sendEvent(evt *event.Event) {
	if evt == nil {
		return
	}

	b.mu.RLock()
	// Create local copies of the fields we need to check to avoid holding the lock longer than necessary
	enableBypass := b.enableBypass
	processing := b.processing
	eventChan := b.eventChan
	ctx := b.ctx
	invocation := b.invocation
	debug := b.debug
	agentName := b.agentName
	b.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}

	// Only send to bypass channel if bypass is enabled and processing is active
	if enableBypass && processing && eventChan != nil {
		eventObject := "<nil>"
		if evt.Response != nil {
			eventObject = evt.Response.Object
		}

		defer func() {
			if r := recover(); r != nil && debug {
				log.Warnf("[LKEAgent:%s] emit panic recovered: %v", agentName, r)
			}
		}()
		if err := agent.EmitEvent(ctx, invocation, eventChan, evt); err != nil {
			if debug {
				log.Warnf("[LKEAgent:%s] emit failed: %v, event=%s", agentName, err, eventObject)
			}
			return
		}
		if debug {
			log.Debugf("[LKEAgent:%s] Event sent: %s", agentName, eventObject)
		}
	} else if debug && enableBypass {
		// Debug log for why event was not sent
		if !processing {
			log.Debugf("[LKEAgent:%s] Event not sent: processing not active", agentName)
		} else if eventChan == nil {
			log.Debugf("[LKEAgent:%s] Event not sent: eventChan is nil", agentName)
		}
	}
}

// OnError implements eventhandler.EventHandler interface.
func (b *BypassEventCollector) OnError(err *lkeevent.ErrorEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnError(err)
	}
	if err == nil {
		return
	}

	// Get invocation ID safely
	b.mu.RLock()
	invocationID := b.invocationID
	b.mu.RUnlock()

	// Send to bypass channel
	errorEvent := event.NewErrorEvent(invocationID, b.agentName,
		model.ErrorTypeAPIError,
		fmt.Sprintf("LKE error: %s (code: %d)", err.Error.Message, err.Error.Code))
	b.sendEvent(errorEvent)
}

// OnReply implements eventhandler.EventHandler interface.
func (b *BypassEventCollector) OnReply(reply *lkeevent.ReplyEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnReply(reply)
	}
	if reply == nil {
		return
	}

	// Send to bypass channel
	if !reply.IsFromSelf && reply.IsFinal {
		b.mu.Lock()
		b.finalReplyObserved = true
		invocationID := b.invocationID
		b.mu.Unlock()

		// Get invocation ID safely
		response := &model.Response{
			Object:  model.ObjectTypeChatCompletion,
			Created: time.Now().Unix(),
			Done:    true,
			Choices: []model.Choice{{
				Index: 0,
				Message: model.Message{
					Content: reply.Content,
					Role:    model.RoleAssistant,
				},
			}},
		}
		replyEvent := event.NewResponseEvent(invocationID, b.agentName, response)
		b.sendEvent(replyEvent)
	}
}

// OnThought implements eventhandler.EventHandler interface.
func (b *BypassEventCollector) OnThought(thought *lkeevent.AgentThoughtEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnThought(thought)
	}
	if thought == nil {
		return
	}

	// Send to bypass channel
	if len(thought.Procedures) > 0 {
		// Get invocation ID safely
		b.mu.RLock()
		invocationID := b.invocationID
		b.mu.RUnlock()

		procedure := thought.Procedures[len(thought.Procedures)-1]
		response := &model.Response{
			Object:  model.ObjectTypePreprocessingBasic,
			Created: time.Now().Unix(),
			Choices: []model.Choice{{
				Index: 0,
				Message: model.Message{
					Content: procedure.Debugging.Content,
					Role:    model.RoleAssistant,
				},
			}},
		}
		thoughtEvent := event.NewResponseEvent(invocationID, b.agentName, response)
		b.sendEvent(thoughtEvent)
	}
}

// OnReference implements eventhandler.EventHandler interface.
func (b *BypassEventCollector) OnReference(refer *lkeevent.ReferenceEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnReference(refer)
	}

	// Send to bypass channel if needed
}

// OnTokenStat implements eventhandler.EventHandler interface.
func (b *BypassEventCollector) OnTokenStat(stat *lkeevent.TokenStatEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnTokenStat(stat)
	}

	// Send to bypass channel if needed
}

// BeforeToolCallHook implements eventhandler.EventHandler interface.
func (b *BypassEventCollector) BeforeToolCallHook(toolCallCtx lkeeventhandler.ToolCallContext) {
	// Call original handler first
	if b.original != nil {
		b.original.BeforeToolCallHook(toolCallCtx)
	}

	// Get invocation ID safely
	b.mu.RLock()
	invocationID := b.invocationID
	b.mu.RUnlock()

	// Send to bypass channel - tool calls use a different structure in trpc-agent-go
	argsJSON, err := json.Marshal(toolCallCtx.Input)
	if err != nil {
		// If serialization fails, use empty JSON object
		argsJSON = []byte("{}")
	}
	toolName := toolCallCtx.CallToolName

	response := &model.Response{
		Object:  model.ObjectTypeToolResponse,
		Created: time.Now().Unix(),
		Choices: []model.Choice{{
			Index: 0,
			Message: model.Message{
				Content: fmt.Sprintf("Tool call started: %s with args: %s",
					toolName, string(argsJSON)),
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID:   toolCallCtx.CallId,
					Type: "function",
					Function: model.FunctionDefinitionParam{
						Name: toolName,
					},
				}},
			},
		}},
	}
	toolCallEvent := event.NewResponseEvent(invocationID, b.agentName, response)
	b.sendEvent(toolCallEvent)
}

// AfterToolCallHook implements eventhandler.EventHandler interface.
func (b *BypassEventCollector) AfterToolCallHook(toolCallCtx lkeeventhandler.ToolCallContext) {
	// Call original handler first
	if b.original != nil {
		b.original.AfterToolCallHook(toolCallCtx)
	}

	// Get invocation ID safely
	b.mu.RLock()
	invocationID := b.invocationID
	b.mu.RUnlock()

	toolName := toolCallCtx.CallToolName

	// Send to bypass channel
	var content string
	if toolCallCtx.Err != nil {
		content = fmt.Sprintf("Tool call failed: %s (error: %v)",
			toolName, toolCallCtx.Err)
	} else {
		resultStr := fmt.Sprintf("%v", toolCallCtx.Output)
		content = fmt.Sprintf("Tool call succeeded: %s (result: %s)",
			toolName, resultStr)
	}

	response := &model.Response{
		Object:  model.ObjectTypeToolResponse,
		Created: time.Now().Unix(),
		Choices: []model.Choice{{
			Index: 0,
			Message: model.Message{
				Content:  content,
				Role:     model.RoleTool,
				ToolID:   toolCallCtx.CallId,
				ToolName: toolName,
			},
		}},
	}
	toolResultEvent := event.NewResponseEvent(invocationID, b.agentName, response)
	b.sendEvent(toolResultEvent)
}
