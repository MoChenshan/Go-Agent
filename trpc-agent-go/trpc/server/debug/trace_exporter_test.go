package debug

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestInMemoryExporterClearAndShutdown(t *testing.T) {
	exp := newInMemoryExporter()
	sessionID := "s1"
	traceID, err := trace.TraceIDFromHex("11111111111111111111111111111111")
	assert.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("1111111111111111")
	assert.NoError(t, err)
	exp.sessionTraces[sessionID] = map[string]struct{}{
		traceID.String(): {},
	}
	exp.spans = []sdktrace.ReadOnlySpan{
		tracetest.SpanStub{
			Name: "chat.completion",
			SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: traceID,
				SpanID:  spanID,
			}),
		}.Snapshot(),
	}

	exp.clear()
	assert.Empty(t, exp.spans)
	assert.NoError(t, exp.Shutdown(context.Background()))
}

func TestApiServerSpanExporterShutdown(t *testing.T) {
	exp := newApiServerSpanExporter(map[string]attribute.Set{})
	assert.NoError(t, exp.Shutdown(context.Background()))
}
