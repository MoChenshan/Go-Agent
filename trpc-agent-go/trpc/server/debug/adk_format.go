package debug

import (
	"encoding/json"
	"fmt"
	"net/http"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug/internal/schema"
	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// convertToolCallToFunctionCall converts model.ToolCall to a
// genai.FunctionCall.
func convertToolCallToFunctionCall(tc *model.ToolCall) *genai.FunctionCall {
	if tc == nil || tc.Function.Name == "" {
		return nil
	}
	var args map[string]any
	if len(tc.Function.Arguments) > 0 {
		if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
			args = map[string]any{"raw": string(tc.Function.Arguments)}
		}
	}
	return &genai.FunctionCall{ID: tc.ID, Name: tc.Function.Name, Args: args}
}

// convertSessionToADKFormat converts an internal session object to the
// flattened structure the ADK Web UI expects.
func convertSessionToADKFormat(s *session.Session) schema.ADKSession {
	events := s.GetEvents()
	adkEvents := make([]map[string]any, 0, len(events))
	for _, e := range events {
		// Create a local copy to avoid implicit memory aliasing.
		e := e
		if ev := convertEventToADKFormat(&e, false); ev != nil {
			adkEvents = append(adkEvents, ev)
		}
	}
	return schema.ADKSession{
		AppName:        s.AppName,
		UserID:         s.UserID,
		ID:             s.ID,
		CreateTime:     s.CreatedAt.Unix(),
		LastUpdateTime: s.UpdatedAt.Unix(),
		State:          map[string][]byte(s.State),
		Events:         adkEvents,
	}
}

// buildADKEventEnvelope creates the basic ADK event envelope.
func buildADKEventEnvelope(e *event.Event) map[string]any {
	return map[string]any{
		"invocationId": e.InvocationID,
		"author":       e.Author,
		"actions": map[string]any{
			"stateDelta":           map[string]any{},
			"artifactDelta":        map[string]any{},
			"requestedAuthConfigs": map[string]any{},
		},
		"id":        e.ID,
		"timestamp": e.Timestamp.Unix(),
	}
}

// determineEventRole determines the role for the event content.
func determineEventRole(e *event.Event) string {
	role := e.Author // fallback
	if e.Response != nil {
		if e.Response.Object == model.ObjectTypeToolResponse {
			role = string(model.RoleTool)
		} else if len(e.Response.Choices) > 0 {
			role = string(e.Response.Choices[0].Message.Role)
		}
	}
	return role
}

// buildEventParts constructs the parts array for the event content.
func buildEventParts(e *event.Event) []map[string]any {
	var parts []map[string]any

	// Early separation: Handle Graph events completely separately
	if isGraphEvent(e) {
		return buildGraphEventParts(e) // Graph events use their own logic
	}

	// Handle LLM Agent events only (chat.completion, tool.response, etc.)
	if e.Response == nil {
		return parts
	}

	// Handle normal / streaming assistant or model messages.
	for _, choice := range e.Response.Choices {
		// Regular text (full message).
		if choice.Message.Content != "" {
			// For tool response events, we do NOT include the raw JSON string as a
			// separate text part, otherwise the ADK Web UI will render duplicated
			// information (both as plain text and as function_response). Keeping
			// only the structured function_response part provides a cleaner view.
			if e.Response.Object != model.ObjectTypeToolResponse {
				parts = append(parts, map[string]any{keyText: choice.Message.Content})
			}
		}

		// Tool calls in full message.
		for _, tc := range choice.Message.ToolCalls {
			parts = append(parts, buildFunctionCallPart(tc))
		}

		// Streaming delta text.
		if choice.Delta.Content != "" {
			parts = append(parts, map[string]any{keyText: choice.Delta.Content})
		}
		// Tool calls in streaming delta.
		for _, tc := range choice.Delta.ToolCalls {
			parts = append(parts, buildFunctionCallPart(tc))
		}
	}

	// Tool response events.
	if e.Response.Object == model.ObjectTypeToolResponse {
		for _, choice := range e.Response.Choices {
			var respObj any
			if choice.Message.Content != "" {
				contentBts := []byte(choice.Message.Content)
				if err := json.Unmarshal(contentBts, &respObj); err != nil {
					respObj = choice.Message.Content // raw string fallback
				}
			}
			parts = append(
				parts,
				buildFunctionResponsePart(
					respObj,
					choice.Message.ToolID,
					choice.Message.ToolName,
				),
			)
		}
	}

	return parts
}

// filterEventParts filters parts based on streaming mode and event type.
func filterEventParts(
	e *event.Event,
	parts []map[string]any,
	isStreaming bool,
) []map[string]any {
	// Early separation: Handle Graph events completely separately
	if isGraphEvent(e) {
		return filterGraphEventParts(e, parts, isStreaming)
	}

	// Handle LLM Agent events only (chat.completion, tool.response, etc.)
	if e.Response == nil {
		return parts // Non-LLM events without Response, return as-is
	}

	if isStreaming {
		// Drop aggregated final messages to avoid duplication with
		// the already streamed deltas. Graph final text is handled in
		// filterGraphEventParts.
		if !e.Response.IsPartial && e.Response.Done {
			return nil
		}
	} else {
		// Non-streaming endpoint should include:
		//   1. Final assistant messages (IsFinalResponse)
		//   2. Tool result events (object == tool.response)
		//   3. Function call events (IsToolCallResponse)
		//   4. User messages (IsUserMessage) for session replay.
		toolResp := isToolResponse(e)
		hasToolCall := e.Response.IsToolCallResponse()
		isUser := e.Response.IsUserMessage()
		isFinal := e.Response.IsFinalResponse()
		if !isFinal && !toolResp && !hasToolCall && !isUser {
			return nil
		}
	}

	return parts
}

// addResponseMetadata adds response-level metadata to the ADK event.
func addResponseMetadata(adkEvent map[string]any, e *event.Event) {
	if e.Response == nil {
		return
	}

	adkEvent["done"] = e.Response.Done
	adkEvent["partial"] = e.Response.IsPartial
	if e.Response.Object != "" {
		adkEvent["object"] = e.Response.Object
	}
	if e.Response.Created != 0 {
		adkEvent["created"] = e.Response.Created
	}
	if e.Response.Model != "" {
		adkEvent["model"] = e.Response.Model
	}
}

// addUsageMetadata adds usage metadata to the ADK event.
func addUsageMetadata(adkEvent map[string]any, e *event.Event) {
	if e.Usage == nil {
		return
	}

	adkEvent["usageMetadata"] = map[string]any{
		"promptTokenCount":     e.Usage.PromptTokens,
		"candidatesTokenCount": e.Usage.CompletionTokens,
		"totalTokenCount":      e.Usage.TotalTokens,
	}
}

// convertEventToADKFormat converts trpc-agent Event to ADK Web UI expected
// format. The isStreaming flag indicates whether the UI is currently
// displaying token-level streaming (true) or expecting a single complete
// response (false). In streaming mode we suppress the final aggregated
// "done" event content to avoid duplication.
func convertEventToADKFormat(
	e *event.Event,
	isStreaming bool,
) map[string]any {
	// Build basic envelope.
	adkEvent := buildADKEventEnvelope(e)

	// Determine role and build content.
	role := determineEventRole(e)
	content := map[string]any{
		"role": role,
	}

	// Build parts.
	parts := buildEventParts(e)

	// Filter parts based on streaming mode.
	parts = filterEventParts(e, parts, isStreaming)

	// Skip event if no meaningful parts.
	if len(parts) == 0 {
		return nil
	}

	content["parts"] = parts
	adkEvent["content"] = content

	// Add metadata.
	addResponseMetadata(adkEvent, e)
	addUsageMetadata(adkEvent, e)

	return adkEvent
}

// ---- helpers ------------------------------------------------------------

func (s *Server) getRunner(appName string) (runner.Runner, error) {
	s.mu.RLock()
	if r, ok := s.runners[appName]; ok {
		s.mu.RUnlock()
		return r, nil
	}
	s.mu.RUnlock()

	ag, ok := s.agents[appName]
	if !ok {
		return nil, fmt.Errorf("agent not found")
	}

	// Compose runner options: user-supplied first, then mandatory sessionSvc.
	allOpts := append([]runner.Option{}, s.runnerOpts...)
	allOpts = append(allOpts, runner.WithSessionService(s.sessionSvc))

	r := runner.NewRunner(appName, ag, allOpts...)
	s.mu.Lock()
	s.runners[appName] = r
	s.mu.Unlock()
	return r, nil
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------
// Internal helpers for event conversion --------------------------------
// ---------------------------------------------------------------------

// ADK Web payload JSON keys. Keeping them as constants helps avoid
// typographical errors and makes refactoring easier.
const (
	keyText             = "text"             // Plain textual content part.
	keyFunctionCall     = "functionCall"     // Function call part key.
	keyFunctionResponse = "functionResponse" // Function response part key.
)

// isToolResponse reports whether the supplied event represents a tool
// response produced by the LLM flow.
func isToolResponse(e *event.Event) bool {
	return e.Response != nil && e.Response.Object == model.ObjectTypeToolResponse
}

// buildFunctionCallPart converts a model.ToolCall into the ADK Web part map.
// The returned map follows the schema expected by the Web UI.
func buildFunctionCallPart(tc model.ToolCall) map[string]any {
	var args any
	if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
		// Preserve raw string if not valid JSON.
		args = map[string]any{"raw": string(tc.Function.Arguments)}
	}
	return map[string]any{
		keyFunctionCall: map[string]any{
			"name": tc.Function.Name,
			"args": args,
			"id":   tc.ID,
		},
	}
}

// buildFunctionResponsePart builds a single functionResponse part.
// respObj can be either a structured object (decoded JSON) or the original
// raw string when JSON decoding fails. The name field is currently unknown
// from the upstream payload, so we intentionally leave it blank.
func buildFunctionResponsePart(
	respObj any,
	id string,
	name string,
) map[string]any {
	return map[string]any{
		keyFunctionResponse: map[string]any{
			"name":     name,
			"response": respObj,
			"id":       id,
		},
	}
}
