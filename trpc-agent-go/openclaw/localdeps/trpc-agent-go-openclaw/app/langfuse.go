//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package app

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	oteltrace "go.opentelemetry.io/otel/trace"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/conversation"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/internal/gateway"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/runtimeprofile"
	langfuseobs "trpc.group/trpc-go/trpc-agent-go/telemetry/langfuse"
)

const (
	langfuseHostEnv        = "LANGFUSE_HOST"
	langfuseInsecureEnv    = "LANGFUSE_INSECURE"
	langfuseInitProjectEnv = "LANGFUSE_INIT_PROJECT_ID"

	langfuseTraceIDPlaceholder = "{{trace_id}}"

	langfuseTraceNameKey         = "langfuse.trace.name"
	langfuseTraceInputKey        = "langfuse.trace.input"
	langfuseUserIDKey            = "langfuse.user.id"
	langfuseSessionIDKey         = "langfuse.session.id"
	langfuseMetadataPrefix       = "langfuse.trace.metadata."
	langfuseMetadataAppName      = langfuseMetadataPrefix + "app_name"
	langfuseMetadataClawID       = langfuseMetadataPrefix + "claw_id"
	langfuseMetadataChannel      = langfuseMetadataPrefix + "channel"
	langfuseMetadataRequestID    = langfuseMetadataPrefix + "request_id"
	langfuseMetadataMessageID    = langfuseMetadataPrefix + "message_id"
	langfuseMetadataTransportSID = langfuseMetadataPrefix +
		"transport_session_id"
	langfuseMetadataProfileID      = langfuseMetadataPrefix + "profile_id"
	langfuseMetadataProfileVersion = langfuseMetadataPrefix +
		"profile_version"

	langfuseTraceDefaultName = "request"
	langfuseClawIDEnv        = "CLAW_ID"

	langfuseTraceTransportWeComPerson = "wecom-person"
	langfuseTraceTransportWeComGroup  = "wecom-group"
	langfuseTraceTransportWeChat      = "wechat"
	langfuseTraceNameSeparator        = "-"
	langfuseChannelWeCom              = "wecom"
	langfuseChannelWeChat             = "wechat"
	langfuseChannelWeixin             = "weixin"
	langfuseSessionKindDM             = "dm"
	langfuseChatSessionMarker         = ":chat:"
	langfuseThreadSessionMarker       = ":thread:"
)

var langfuseStart = langfuseobs.Start

type langfuseRuntime struct {
	adminStatus       admin.LangfuseStatus
	runOptionResolver gateway.RunOptionResolver
	shutdown          func(context.Context) error
}

func maybeEnableLangfuse(
	ctx context.Context,
	opts runOptions,
) (*langfuseRuntime, error) {
	status := buildLangfuseAdminStatus(opts)
	if !opts.LangfuseEnabled {
		return &langfuseRuntime{
			adminStatus: status,
		}, nil
	}

	shutdown, err := langfuseStart(
		ctx,
		langfuseStartOptions(opts)...,
	)
	if err != nil {
		status.Error = err.Error()
		if opts.LangfuseRequired {
			return nil, err
		}
		log.Warnf("openclaw: langfuse disabled: %v", err)
		return &langfuseRuntime{
			adminStatus: status,
		}, nil
	}

	status.Ready = true
	return &langfuseRuntime{
		adminStatus:       status,
		runOptionResolver: buildLangfuseRunOptionResolver(opts),
		shutdown:          shutdown,
	}, nil
}

func langfuseStartOptions(
	opts runOptions,
) []langfuseobs.Option {
	if opts.LangfuseObservationLeafValueMaxBytes == nil {
		return nil
	}
	return []langfuseobs.Option{
		langfuseobs.WithObservationLeafValueMaxBytes(
			*opts.LangfuseObservationLeafValueMaxBytes,
		),
	}
}

func buildLangfuseAdminStatus(
	opts runOptions,
) admin.LangfuseStatus {
	uiBaseURL := resolvedLangfuseUIBaseURL(opts)
	return admin.LangfuseStatus{
		Enabled:   opts.LangfuseEnabled,
		UIBaseURL: uiBaseURL,
		TraceURLTemplate: resolvedLangfuseTraceURLTemplate(
			opts,
			uiBaseURL,
		),
	}
}

func resolvedLangfuseUIBaseURL(opts runOptions) string {
	if baseURL := strings.TrimSpace(opts.LangfuseUIBaseURL); baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}

	host := strings.TrimSpace(os.Getenv(langfuseHostEnv))
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		return strings.TrimRight(host, "/")
	}

	scheme := "https"
	if strings.EqualFold(
		strings.TrimSpace(os.Getenv(langfuseInsecureEnv)),
		"true",
	) {
		scheme = "http"
	}
	return scheme + "://" + host
}

func resolvedLangfuseTraceURLTemplate(
	opts runOptions,
	uiBaseURL string,
) string {
	if template := strings.TrimSpace(
		opts.LangfuseTraceURLTemplate,
	); template != "" {
		return template
	}
	projectID := strings.TrimSpace(os.Getenv(langfuseInitProjectEnv))
	if uiBaseURL == "" || projectID == "" {
		return ""
	}
	return strings.TrimRight(uiBaseURL, "/") +
		"/project/" + projectID + "/traces/" +
		langfuseTraceIDPlaceholder
}

func buildLangfuseRunOptionResolver(
	opts runOptions,
) gateway.RunOptionResolver {
	appName := strings.TrimSpace(opts.AppName)
	return func(
		ctx context.Context,
		input gateway.RunOptionInput,
	) (context.Context, []agent.RunOption, error) {
		ctx = withLangfuseBaggage(ctx, appName, input)

		runOpts := make([]agent.RunOption, 0, 2)
		resolvedAppName := runtimeprofile.AppNameFromContext(ctx, appName)
		spanAttrs := langfuseTraceSpanAttributes(
			resolvedAppName,
			input,
		)
		if len(spanAttrs) > 0 {
			runOpts = append(runOpts, agent.WithSpanAttributes(spanAttrs...))
		}
		if input.Trace != nil {
			traceRef := input.Trace
			runOpts = append(
				runOpts,
				agent.WithTraceStartedCallback(
					func(spanCtx oteltrace.SpanContext) {
						if !spanCtx.IsValid() {
							return
						}
						if err := traceRef.SetTraceID(
							spanCtx.TraceID().String(),
						); err != nil {
							log.Warnf(
								"openclaw: persist trace id failed: %v",
								err,
							)
						}
					},
				),
			)
		}
		return ctx, runOpts, nil
	}
}

func withLangfuseBaggage(
	ctx context.Context,
	appName string,
	input gateway.RunOptionInput,
) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	bag := baggage.FromContext(ctx)
	bag = setLangfuseBaggageMember(
		bag,
		langfuseUserIDKey,
		input.UserID,
	)
	bag = setLangfuseBaggageMember(
		bag,
		langfuseSessionIDKey,
		buildLangfuseSessionID(input),
	)
	bag = setLangfuseBaggageMember(
		bag,
		langfuseMetadataTransportSID,
		input.SessionID,
	)
	bag = setLangfuseBaggageMember(
		bag,
		langfuseMetadataAppName,
		runtimeprofile.AppNameFromContext(ctx, appName),
	)
	bag = setLangfuseBaggageMember(
		bag,
		langfuseMetadataClawID,
		os.Getenv(langfuseClawIDEnv),
	)
	if profile, ok := runtimeprofile.ProfileFromContext(ctx); ok {
		bag = setLangfuseBaggageMember(
			bag,
			langfuseMetadataProfileID,
			profile.ID,
		)
		bag = setLangfuseBaggageMember(
			bag,
			langfuseMetadataProfileVersion,
			profile.Version,
		)
	}
	bag = setLangfuseBaggageMember(
		bag,
		langfuseMetadataChannel,
		input.Inbound.Channel,
	)
	bag = setLangfuseBaggageMember(
		bag,
		langfuseMetadataRequestID,
		input.RequestID,
	)
	bag = setLangfuseBaggageMember(
		bag,
		langfuseMetadataMessageID,
		input.Inbound.MessageID,
	)
	return baggage.ContextWithBaggage(ctx, bag)
}

func langfuseTraceSpanAttributes(
	fallbackAppName string,
	input gateway.RunOptionInput,
) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2)
	if traceName := buildLangfuseTraceName(
		fallbackAppName,
		input,
	); traceName != "" {
		attrs = append(attrs, attribute.String(langfuseTraceNameKey, traceName))
	}
	if traceInput := buildLangfuseTraceInput(input); traceInput != "" {
		attrs = append(
			attrs,
			attribute.String(langfuseTraceInputKey, traceInput),
		)
	}
	return attrs
}

func buildLangfuseTraceInput(input gateway.RunOptionInput) string {
	if text := strings.TrimSpace(input.Inbound.Text); text != "" {
		return text
	}
	return strings.TrimSpace(input.Message.Content)
}

func buildLangfuseSessionID(input gateway.RunOptionInput) string {
	sessionID := strings.TrimSpace(input.SessionID)
	annotation, ok, err := conversation.AnnotationFromRequestExtensions(
		input.Extensions,
	)
	if err != nil || !ok {
		return sessionID
	}
	actorLabel := strings.TrimSpace(annotation.ActorLabel)
	if actorLabel == "" {
		return sessionID
	}
	if strings.EqualFold(
		strings.TrimSpace(input.Inbound.Channel),
		langfuseChannelWeCom,
	) {
		return replaceDirectSessionUser(sessionID, actorLabel)
	}
	return sessionID
}

func replaceDirectSessionUser(sessionID string, userLabel string) string {
	userLabel = strings.TrimSpace(userLabel)
	if sessionID == "" || userLabel == "" {
		return sessionID
	}
	parts := strings.Split(sessionID, ":")
	if len(parts) < 3 || parts[1] != langfuseSessionKindDM {
		return sessionID
	}
	parts[2] = userLabel
	return strings.Join(parts, ":")
}

func setLangfuseBaggageMember(
	bag baggage.Baggage,
	key string,
	value string,
) baggage.Baggage {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return bag
	}

	member, err := baggage.NewMemberRaw(key, value)
	if err != nil {
		return bag
	}
	next, err := bag.SetMember(member)
	if err != nil {
		return bag
	}
	return next
}

func buildLangfuseTraceName(
	fallbackAppName string,
	input gateway.RunOptionInput,
) string {
	owner := firstLangfuseTraceNameValue(
		os.Getenv(langfuseClawIDEnv),
		fallbackAppName,
		appName,
	)
	transport := resolveLangfuseTraceTransport(input)
	if transport == "" {
		transport = langfuseTraceDefaultName
	}
	return joinLangfuseTraceName(owner, transport)
}

func resolveLangfuseTraceTransport(
	input gateway.RunOptionInput,
) string {
	channel := strings.ToLower(strings.TrimSpace(input.Inbound.Channel))
	switch channel {
	case langfuseChannelWeCom:
		if isLangfuseWeComGroup(input) {
			return langfuseTraceTransportWeComGroup
		}
		return langfuseTraceTransportWeComPerson
	case langfuseChannelWeChat, langfuseChannelWeixin:
		return langfuseTraceTransportWeChat
	case "":
		return ""
	default:
		return channel
	}
}

func isLangfuseWeComGroup(input gateway.RunOptionInput) bool {
	sessionID := strings.ToLower(strings.TrimSpace(input.SessionID))
	return strings.Contains(sessionID, langfuseChatSessionMarker) ||
		strings.Contains(sessionID, langfuseThreadSessionMarker)
}

func joinLangfuseTraceName(parts ...string) string {
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			trimmed = append(trimmed, part)
		}
	}
	return strings.Join(trimmed, langfuseTraceNameSeparator)
}

func firstLangfuseTraceNameValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
