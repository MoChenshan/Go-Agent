package debug

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug/internal/schema"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// convertContentToMessage converts Google GenAI Content to a trpc-agent
// model.Message.
func convertContentToMessage(content schema.Content) model.Message {
	log.Debugf(
		"convertContentToMessage: role=%s parts=%+v",
		content.Role,
		content.Parts,
	)
	var textParts []string
	var toolCalls []model.ToolCall
	for _, part := range content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}

		if part.FunctionCall != nil {
			argsBytes, _ := json.Marshal(part.FunctionCall.Args)
			toolCall := model.ToolCall{
				Type: "function",
				Function: model.FunctionDefinitionParam{
					Name:      part.FunctionCall.Name,
					Arguments: argsBytes,
				},
			}
			toolCalls = append(toolCalls, toolCall)
		}

		if part.InlineData != nil {
			dataType := "file"
			if part.InlineData.MimeType != "" {
				if strings.HasPrefix(part.InlineData.MimeType, "image") {
					dataType = "image"
				} else if strings.HasPrefix(part.InlineData.MimeType, "audio") {
					dataType = "audio"
				} else if strings.HasPrefix(part.InlineData.MimeType, "video") {
					dataType = "video"
				}
			}
			fileName := part.InlineData.DisplayName
			if fileName == "" {
				fileName = "attachment"
			}
			attachmentText := fmt.Sprintf(
				"[%s: %s (%s)]",
				dataType,
				fileName,
				part.InlineData.MimeType,
			)
			textParts = append(textParts, attachmentText)
		}

		if part.FunctionResponse != nil {
			responseJSON, _ := json.Marshal(part.FunctionResponse.Response)
			responseText := fmt.Sprintf(
				"[Function %s responded: %s]",
				part.FunctionResponse.Name,
				string(responseJSON),
			)
			textParts = append(textParts, responseText)
		}
	}
	var combinedText string
	if len(textParts) > 0 {
		combinedText = strings.Join(textParts, "\n")
	}
	msg := model.Message{
		Role:    model.Role(content.Role),
		Content: combinedText,
	}

	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	return msg
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log.InfofContext(
		ctx,
		"handleRun called: path=%s",
		r.URL.Path,
	)

	var req schema.AgentRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// If the request is for streaming, delegate to the SSE handler.
	if req.Streaming {
		// As we can't directly pass the decoded body, the SSE handler
		// will re-decode.
		// A more optimized approach might pass the decoded struct via
		// context.
		s.handleRunSSE(w, r)
		return
	}

	rn, err := s.getRunner(req.AppName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	runCtx := newDetachedContext(ctx)
	out, err := rn.Run(runCtx, req.UserID, req.SessionID,
		convertContentToMessage(req.NewMessage))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// For non-streaming, collect all events and return them as JSON.
	// ADK web might expect a list of events. Let's send all of them.
	var events []map[string]any
	for e := range out {
		if e.Response != nil && e.Response.IsPartial {
			continue // skip streaming chunks in non-streaming endpoint
		}
		if ev := convertEventToADKFormat(e, false); ev != nil {
			events = append(events, ev)
		}
	}
	s.writeJSON(w, events)
}

func (s *Server) handleRunSSE(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log.InfofContext(
		ctx,
		"handleRunSSE called: path=%s",
		r.URL.Path,
	)

	var req schema.AgentRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	rn, err := s.getRunner(req.AppName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	runCtx := newDetachedContext(ctx)
	out, err := rn.Run(runCtx, req.UserID, req.SessionID,
		convertContentToMessage(req.NewMessage))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Streaming {
		for e := range out {
			sseEvent := convertEventToADKFormat(e, req.Streaming)
			if sseEvent == nil {
				continue
			}
			data, err := json.Marshal(sseEvent)
			if err != nil {
				log.ErrorfContext(
					ctx,
					"Error marshalling SSE event: %v",
					err,
				)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	} else {
		// Non-streaming mode: wait for the first complete event and send only that.
		for e := range out {
			sseEvent := convertEventToADKFormat(e, req.Streaming)
			if sseEvent == nil {
				continue
			}
			data, err := json.Marshal(sseEvent)
			if err != nil {
				log.ErrorfContext(
					ctx,
					"Error marshalling SSE event: %v",
					err,
				)
				break
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	log.InfofContext(
		ctx,
		"handleRunSSE finished for session %s",
		req.SessionID,
	)
}
