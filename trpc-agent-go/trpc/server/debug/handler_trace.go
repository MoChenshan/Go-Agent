package debug

import (
	"encoding/json"
	"net/http"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug/internal/schema"
	"github.com/gorilla/mux"
	"go.opentelemetry.io/otel/attribute"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func (s *Server) handleEventTrace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log.InfofContext(
		ctx,
		"handleEventTrace called: path=%s",
		r.URL.Path,
	)
	vars := mux.Vars(r)
	eventID := vars["event_id"]
	trace, ok := s.traces[eventID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Trace not found"))
		return
	}
	s.writeJSON(w, buildTraceAttributes(trace))
}

func (s *Server) handleSessionTrace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log.InfofContext(
		ctx,
		"handleSessionTrace called: path=%s",
		r.URL.Path,
	)
	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	var spans []schema.Span
	for _, span := range s.memoryExporter.getFinishedSpans(sessionID) {
		result := buildTraceAttributes(attribute.NewSet(span.Attributes()...))
		spans = append(spans, schema.Span{
			Name:         span.Name(),
			SpanID:       span.SpanContext().SpanID().String(),
			TraceID:      span.SpanContext().TraceID().String(),
			StartTime:    span.StartTime().UnixNano(),
			EndTime:      span.EndTime().UnixNano(),
			Attributes:   result,
			ParentSpanID: span.Parent().SpanID().String(),
		})
	}
	s.writeJSON(w, spans)
}

func buildTraceAttributes(attributes attribute.Set) map[string]any {
	result := make(map[string]any)
	for iter := attributes.Iter(); iter.Next(); {
		attr := iter.Attribute()
		value := attr.Value.AsString()
		result[string(attr.Key)] = value
		switch attr.Key {
		case keyEventID:
			result[adkKeyEventID] = value
		case keySessionID:
			result[adkKeySessionID] = value
		case keyInvocationID:
			result[adkKeyInvocationID] = value
		case keyLLMResponse:
			result[adkKeyLLMResponse] = value
		case keyLLMRequest:
			formatted, ok := formatLLMRequest(value)
			if ok {
				result[adkKeyLLMRequest] = formatted
			} else {
				result[adkKeyLLMRequest] = value
			}
		}
	}
	return result
}

func formatLLMRequest(value string) (string, bool) {
	var req model.Request
	if err := json.Unmarshal([]byte(value), &req); err != nil {
		log.Debugf("failed to unmarshal LLM request: %s", value)
		return "", false
	}

	var contents []schema.Content
	for _, msg := range req.Messages {
		contents = append(contents, schema.Content{
			Role: msg.Role.String(),
			Parts: []schema.Part{
				{Text: msg.Content},
			},
		})
	}

	bts, err := json.Marshal(&schema.TraceLLMRequest{Contents: contents})
	if err != nil {
		return "", false
	}

	return string(bts), true
}
