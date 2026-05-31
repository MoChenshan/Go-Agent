package wecom

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

const (
	wecomTraceInstrumentationName = "git.woa.com/trpc-go/trpc-agent-go/" +
		"openclaw/channel/wecom"
	wecomGatewayTraceSpanName = "wecom.gateway"

	langfuseTraceNameAttribute    = "langfuse.trace.name"
	langfuseTraceInputAttribute   = "langfuse.trace.input"
	langfuseTraceOutputAttribute  = "langfuse.trace.output"
	langfuseInternalAsRoot        = "langfuse.internal.as_root"
	langfuseObservationInput      = "langfuse.observation.input"
	langfuseObservationOutput     = "langfuse.observation.output"
	langfuseUserIDAttribute       = "langfuse.user.id"
	langfuseSessionIDAttribute    = "langfuse.session.id"
	langfuseMetadataPrefix        = "langfuse.trace.metadata."
	langfuseMetadataClawID        = langfuseMetadataPrefix + "claw_id"
	langfuseMetadataTraceOwner    = langfuseMetadataPrefix + "trace_owner"
	langfuseMetadataTransportKind = langfuseMetadataPrefix + "transport_kind"
	langfuseMetadataActorID       = langfuseMetadataPrefix + "actor_id"
	langfuseMetadataActorLabel    = langfuseMetadataPrefix + "actor_label"
	langfuseMetadataMessageID     = langfuseMetadataPrefix + "message_id"
	langfuseMetadataRequestID     = langfuseMetadataPrefix + "request_id"
	langfuseMetadataTransportSID  = langfuseMetadataPrefix +
		"transport_session_id"
	langfuseMetadataTransportFrom = langfuseMetadataPrefix + "transport_from"
	langfuseMetadataThread        = langfuseMetadataPrefix + "thread"

	defaultGatewayTraceOwner = "openclaw"
	traceNameSeparator       = "-"
	traceSessionSeparator    = ":"

	traceNameClawIDEnv = "CLAW_ID"
	traceSessionKindDM = "dm"

	wecomTraceChatTypeGroup    = pushTargetKindGroup
	wecomTraceChatTypeSingle   = pushTargetKindSingle
	wecomTraceTransportGroup   = "wecom-group"
	wecomTraceTransportPerson  = "wecom-person"
	wecomTraceTransportDefault = wecomTraceTransportPerson
)

type gatewayTraceIdentity struct {
	TraceOwner    string
	ClawID        string
	TransportKind string
	ActorLabel    string
}

func startGatewayTraceSpan(
	ctx context.Context,
	req gwclient.MessageRequest,
	identity gatewayTraceIdentity,
) (context.Context, oteltrace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	tracer := atrace.TracerProvider.Tracer(wecomTraceInstrumentationName)
	ctx, span := tracer.Start(ctx, wecomGatewayTraceSpanName)
	span.SetAttributes(gatewayTraceAttributes(req, identity)...)
	return ctx, span
}

func finishGatewayTraceSpan(
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

func gatewayTraceAttributes(
	req gwclient.MessageRequest,
	identity gatewayTraceIdentity,
) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(
			langfuseTraceNameAttribute,
			buildGatewayTraceName(
				identity.TraceOwner,
				identity.TransportKind,
			),
		),
		attribute.Bool(langfuseInternalAsRoot, true),
	}
	appendStringAttribute := func(key string, value string) {
		if value = strings.TrimSpace(value); value != "" {
			attrs = append(attrs, attribute.String(key, value))
		}
	}
	appendStringAttribute(langfuseTraceInputAttribute, req.Text)
	appendStringAttribute(langfuseObservationInput, req.Text)
	appendStringAttribute(langfuseUserIDAttribute, req.UserID)
	appendStringAttribute(
		langfuseSessionIDAttribute,
		buildGatewayTraceSessionID(req.SessionID, identity.ActorLabel),
	)
	appendStringAttribute(langfuseMetadataTransportSID, req.SessionID)
	appendStringAttribute(langfuseMetadataClawID, identity.ClawID)
	appendStringAttribute(langfuseMetadataTraceOwner, identity.TraceOwner)
	appendStringAttribute(
		langfuseMetadataTransportKind,
		identity.TransportKind,
	)
	appendStringAttribute(langfuseMetadataActorID, req.From)
	appendStringAttribute(langfuseMetadataActorLabel, identity.ActorLabel)
	appendStringAttribute(langfuseMetadataMessageID, req.MessageID)
	appendStringAttribute(langfuseMetadataRequestID, req.RequestID)
	appendStringAttribute(langfuseMetadataTransportFrom, req.From)
	appendStringAttribute(langfuseMetadataThread, req.Thread)
	return attrs
}

func buildGatewayTraceSessionID(sessionID string, actorLabel string) string {
	sessionID = strings.TrimSpace(sessionID)
	actorLabel = strings.TrimSpace(actorLabel)
	if sessionID == "" || actorLabel == "" {
		return sessionID
	}
	parts := strings.Split(sessionID, traceSessionSeparator)
	if len(parts) < 3 || parts[1] != traceSessionKindDM {
		return sessionID
	}
	parts[2] = actorLabel
	return strings.Join(parts, traceSessionSeparator)
}

func buildGatewayTraceName(owner string, transportKind string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = defaultGatewayTraceOwner
	}
	transportKind = strings.TrimSpace(transportKind)
	if transportKind == "" {
		transportKind = wecomTraceTransportDefault
	}
	return joinTraceName(owner, transportKind)
}

func joinTraceName(parts ...string) string {
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			trimmed = append(trimmed, part)
		}
	}
	return strings.Join(trimmed, traceNameSeparator)
}

func resolveGatewayTraceIdentity(
	actorLabel string,
	chatID string,
	chatType string,
	appName string,
	botName string,
	channelName string,
) gatewayTraceIdentity {
	clawID := traceEnvValue(traceNameClawIDEnv)
	traceOwner := firstTraceNameValue(
		clawID,
		appName,
		botName,
		channelName,
	)
	return gatewayTraceIdentity{
		TraceOwner:    traceOwner,
		ClawID:        clawID,
		TransportKind: resolveWeComTraceTransportKind(chatID, chatType),
		ActorLabel:    strings.TrimSpace(actorLabel),
	}
}

func resolveWeComTraceTransportKind(chatID string, chatType string) string {
	switch strings.ToLower(strings.TrimSpace(chatType)) {
	case wecomTraceChatTypeGroup:
		return wecomTraceTransportGroup
	case wecomTraceChatTypeSingle:
		return wecomTraceTransportPerson
	}
	return wecomTraceTransportPerson
}

func traceEnvValue(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func firstTraceNameValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
