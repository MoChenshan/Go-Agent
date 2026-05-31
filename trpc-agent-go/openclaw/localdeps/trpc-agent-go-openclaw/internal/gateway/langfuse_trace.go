//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gateway

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

const (
	langfuseGatewayTraceInstrumentationName = "trpc.group/trpc-go/" +
		"trpc-agent-go/openclaw/internal/gateway"
	langfuseGatewayTraceSpanName = "openclaw.gateway"

	langfuseTraceNameAttribute   = "langfuse.trace.name"
	langfuseTraceInputAttribute  = "langfuse.trace.input"
	langfuseTraceOutputAttribute = "langfuse.trace.output"
	langfuseInternalAsRoot       = "langfuse.internal.as_root"
	langfuseObservationInput     = "langfuse.observation.input"
	langfuseObservationOutput    = "langfuse.observation.output"
	langfuseUserIDAttribute      = "langfuse.user.id"
	langfuseSessionIDAttribute   = "langfuse.session.id"
	langfuseMetadataPrefix       = "langfuse.trace.metadata."
	langfuseMetadataAppName      = langfuseMetadataPrefix + "app_name"
	langfuseMetadataChannel      = langfuseMetadataPrefix + "channel"
	langfuseMetadataRequestID    = langfuseMetadataPrefix + "request_id"
	langfuseMetadataMessageID    = langfuseMetadataPrefix + "message_id"

	langfuseTraceDefaultOwner = "openclaw"
	langfuseTraceDefaultName  = "request"
	langfuseTraceNameSep      = "-"
	langfuseClawIDEnv         = "CLAW_ID"
	langfuseChannelWeChat     = "wechat"
	langfuseChannelWeixin     = "weixin"
)

func startLangfuseGatewayTraceSpan(
	ctx context.Context,
	server *Server,
	run preparedMessageRun,
) (context.Context, oteltrace.Span) {
	if !shouldStartLangfuseGatewayTrace(run.inbound.Channel) {
		return ctx, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tracer := atrace.TracerProvider.Tracer(
		langfuseGatewayTraceInstrumentationName,
	)
	ctx, span := tracer.Start(ctx, langfuseGatewayTraceSpanName)
	span.SetAttributes(langfuseGatewayTraceAttributes(server, run)...)
	return ctx, span
}

func finishLangfuseGatewayTraceSpan(
	span oteltrace.Span,
	output string,
	err error,
) {
	if span == nil {
		return
	}
	if output = strings.TrimSpace(output); output != "" {
		span.SetAttributes(
			attribute.String(langfuseTraceOutputAttribute, output),
			attribute.String(langfuseObservationOutput, output),
		)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func langfuseGatewayTraceAttributes(
	server *Server,
	run preparedMessageRun,
) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(
			langfuseTraceNameAttribute,
			buildLangfuseGatewayTraceName(server, run.inbound.Channel),
		),
		attribute.Bool(langfuseInternalAsRoot, true),
	}
	appendStringAttribute := func(key string, value string) {
		if value = strings.TrimSpace(value); value != "" {
			attrs = append(attrs, attribute.String(key, value))
		}
	}
	input := strings.TrimSpace(run.inbound.Text)
	if input == "" {
		input = strings.TrimSpace(run.userMsg.Content)
	}
	appendStringAttribute(langfuseTraceInputAttribute, input)
	appendStringAttribute(langfuseObservationInput, input)
	appendStringAttribute(langfuseUserIDAttribute, run.userID)
	appendStringAttribute(langfuseSessionIDAttribute, run.sessionID)
	appendStringAttribute(langfuseMetadataAppName, serverAppName(server))
	appendStringAttribute(langfuseMetadataChannel, run.inbound.Channel)
	appendStringAttribute(langfuseMetadataRequestID, run.requestID)
	appendStringAttribute(langfuseMetadataMessageID, run.inbound.MessageID)
	return attrs
}

func shouldStartLangfuseGatewayTrace(channel string) bool {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case langfuseChannelWeChat, langfuseChannelWeixin:
		return true
	default:
		return false
	}
}

func buildLangfuseGatewayTraceName(server *Server, channel string) string {
	owner := firstLangfuseGatewayTraceNameValue(
		os.Getenv(langfuseClawIDEnv),
		serverAppName(server),
		langfuseTraceDefaultOwner,
	)
	transport := langfuseGatewayTraceTransport(channel)
	if transport == "" {
		transport = langfuseTraceDefaultName
	}
	return joinLangfuseGatewayTraceName(owner, transport)
}

func langfuseGatewayTraceTransport(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case langfuseChannelWeChat, langfuseChannelWeixin:
		return langfuseChannelWeChat
	default:
		return strings.ToLower(strings.TrimSpace(channel))
	}
}

func serverAppName(server *Server) string {
	if server == nil {
		return ""
	}
	return strings.TrimSpace(server.appName)
}

func joinLangfuseGatewayTraceName(parts ...string) string {
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			trimmed = append(trimmed, part)
		}
	}
	return strings.Join(trimmed, langfuseTraceNameSep)
}

func firstLangfuseGatewayTraceNameValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
