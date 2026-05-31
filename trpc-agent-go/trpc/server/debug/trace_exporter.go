package debug

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
)

const (
	instrumentName = "trpc.agent.go"

	operationChat        = "chat"
	operationExecuteTool = "execute_tool"

	keyEventID      = semconvtrace.KeyEventID
	keySessionID    = semconvtrace.KeyGenAIConversationID
	keyInvocationID = semconvtrace.KeyInvocationID
	keyLLMRequest   = semconvtrace.KeyLLMRequest
	keyLLMResponse  = semconvtrace.KeyLLMResponse

	adkKeyEventID      = "gcp.vertex.agent.event_id"
	adkKeySessionID    = "gcp.vertex.agent.session_id"
	adkKeyInvocationID = "gcp.vertex.agent.invocation_id"
	adkKeyLLMRequest   = "gcp.vertex.agent.llm_request"
	adkKeyLLMResponse  = "gcp.vertex.agent.llm_response"
)

type apiServerSpanExporter struct {
	traces map[string]attribute.Set
}

func newApiServerSpanExporter(
	ts map[string]attribute.Set,
) *apiServerSpanExporter {
	return &apiServerSpanExporter{traces: ts}
}

func (e *apiServerSpanExporter) ExportSpans(
	_ context.Context,
	spans []sdktrace.ReadOnlySpan,
) error {
	for _, span := range spans {
		if name := span.Name(); !strings.HasPrefix(name, operationChat) &&
			!strings.HasPrefix(name, operationExecuteTool) {
			continue
		}
		baseAttrs := []attribute.KeyValue{
			attribute.String("trace_id", span.SpanContext().TraceID().String()),
			attribute.String("span_id", span.SpanContext().SpanID().String()),
		}
		allAttrs := append(baseAttrs, span.Attributes()...)
		attributes := attribute.NewSet(allAttrs...)

		if eventID, ok := attributes.Value(keyEventID); ok {
			e.traces[eventID.AsString()] = attributes
		}
	}
	return nil
}

func (e *apiServerSpanExporter) Shutdown(_ context.Context) error {
	return nil
}

type inMemoryExporter struct {
	// key: session_id -> trace_id set.
	sessionTraces map[string]map[string]struct{}
	spans         []sdktrace.ReadOnlySpan
}

func newInMemoryExporter() *inMemoryExporter {
	return &inMemoryExporter{sessionTraces: make(map[string]map[string]struct{})}
}

func (e *inMemoryExporter) ExportSpans(
	_ context.Context,
	spans []sdktrace.ReadOnlySpan,
) error {
	for _, span := range spans {
		if !strings.HasPrefix(span.Name(), operationChat) {
			continue
		}
		for _, attr := range span.Attributes() {
			if attr.Key != keySessionID {
				continue
			}
			sessionID := attr.Value.AsString()
			traceID := span.SpanContext().TraceID().String()
			if _, ok := e.sessionTraces[sessionID]; !ok {
				e.sessionTraces[sessionID] = map[string]struct{}{
					traceID: {},
				}
			} else {
				e.sessionTraces[sessionID][traceID] = struct{}{}
			}
			break
		}
	}
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *inMemoryExporter) Shutdown(_ context.Context) error {
	return nil
}

func (e *inMemoryExporter) getFinishedSpans(
	sessionID string,
) []sdktrace.ReadOnlySpan {
	traceIDs := e.sessionTraces[sessionID]
	var spans []sdktrace.ReadOnlySpan
	for traceID := range traceIDs {
		for _, s := range e.spans {
			if s.SpanContext().TraceID().String() == traceID {
				spans = append(spans, s)
			}
		}
	}
	return spans
}

func (e *inMemoryExporter) clear() {
	e.spans = make([]sdktrace.ReadOnlySpan, 0)
}
