package zhiyanllm

import (
	"context"
	"fmt"
	"strings"

	zhiyanllm "git.woa.com/zhiyan-monitor/sdk/llm_go_sdk"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace/noop"
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

// Start creates a new client with the given options
func Start(ctx context.Context, opts ...Option) (*zhiyanllm.Zhiyanllm, error) {
	config := newDefaultClientConfig()
	for _, opt := range opts {
		opt(config)
	}
	config = fixConfig(config)

	// Convert to zhiyanllm.Config
	c, err := zhiyanllm.NewClient(ctx, zhiyanllm.Config{
		APIEndpoint:       config.apiEndpoint,
		APIKey:            config.apiKey,
		APPName:           config.appName,
		NewTracerProvider: func(ctx context.Context) (*sdktrace.TracerProvider, error) { return newTracerProvider(ctx, config) },
	})
	if err != nil {
		return nil, err
	}
	atrace.Tracer = c.Tracer()
	return c, nil
}

func newTracerProvider(ctx context.Context, cfg *config) (*sdktrace.TracerProvider, error) {
	p := atrace.TracerProvider
	_, ok := p.(noop.TracerProvider)
	var provider *sdktrace.TracerProvider
	if !ok {
		provider, ok = p.(*sdktrace.TracerProvider)
		if !ok {
			return nil, fmt.Errorf("otel.GetTracerProvider() returned a non-SDK trace p")
		}

	}

	opts, err := newOTLPTraceHTPPOptions(cfg)
	if err != nil {
		return nil, err
	}

	exp, err := newExporter(ctx, cfg.attributeValueLengthLimit, opts...)
	if err != nil {
		return nil, err
	}

	processor := newSpanProcessor(exp)
	if provider == nil {
		res, err := newResource(ctx, cfg)
		if err != nil {
			return nil, err
		}
		provider = sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithResource(res),
			sdktrace.WithRawSpanLimits(newSpanLimits(cfg)),
			sdktrace.WithSpanProcessor(processor),
		)
		atrace.TracerProvider = provider
	} else {
		provider.RegisterSpanProcessor(processor)
	}

	return provider, nil
}

func newSpanProcessor(e sdktrace.SpanExporter) sdktrace.SpanProcessor {
	return sdktrace.NewBatchSpanProcessor(e)
}

func newResource(ctx context.Context, cfg *config) (*resource.Resource, error) {
	return resource.New(ctx, resource.WithAttributes(
		semconv.ServiceNameKey.String(cfg.appName),
		attribute.String(zhiyanllm.TPS_TENANT_ID, cfg.apiKey),
	))
}

func newSpanLimits(c *config) sdktrace.SpanLimits {
	return sdktrace.SpanLimits{
		AttributeValueLengthLimit:   c.attributeValueLengthLimit,
		AttributeCountLimit:         c.attributeCountLimit,
		EventCountLimit:             c.eventCountLimit,
		AttributePerEventCountLimit: c.attributePerEventCountLimit,
	}
}

func newOTLPTraceHTPPOptions(c *config) ([]otlptracehttp.Option, error) {
	protocol, host := parseEndpoint(c.apiEndpoint)

	headers := map[string]string{
		"Authorization": c.apiKey,
	}

	switch protocol {
	case zhiyanllm.HTTPS:
		return []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(host),
			otlptracehttp.WithHeaders(headers),
		}, nil
	case zhiyanllm.HTTP:
		return []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(host),
			otlptracehttp.WithHeaders(headers),
			otlptracehttp.WithInsecure(),
		}, nil
	default:
		return nil, fmt.Errorf("invalid endpoint: %s, unsupported protocol, only http/https is supported", c.apiEndpoint)
	}
}

func parseEndpoint(endpoint string) (protocol, host string) {
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		return zhiyanllm.HTTPS, strings.TrimPrefix(endpoint, "https://")
	case strings.HasPrefix(endpoint, "http://"):
		return zhiyanllm.HTTP, strings.TrimPrefix(endpoint, "http://")
	default:
		return "", endpoint
	}
}
