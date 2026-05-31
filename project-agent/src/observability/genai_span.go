// Package observability 的 GenAI Semantic Conventions 实现。
//
// 基于 OpenTelemetry GenAI Semantic Conventions v1.30（Draft）：
//   https://opentelemetry.io/docs/specs/semconv/gen-ai/
//
// 本文件约定两类 Span：
//
//   1) gen_ai.{operation}    — 针对 LLM 调用（execute_tool 除外）
//      典型 operation：chat / text_completion / embedding / agent
//
//   2) execute_tool          — 针对模型驱动的函数/工具调用
//
// 属性键均使用 gen_ai.* 前缀，保证在 Langfuse / Jaeger / Datadog 等
// GenAI 感知型后端能直接聚合成"LLM 会话视图"。
package observability

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// GenAI Semantic Convention 属性键常量（v1.30）。
const (
	AttrGenAISystem               = "gen_ai.system"
	AttrGenAIOperationName        = "gen_ai.operation.name"
	AttrGenAIRequestModel         = "gen_ai.request.model"
	AttrGenAIResponseModel        = "gen_ai.response.model"
	AttrGenAIRequestTemperature   = "gen_ai.request.temperature"
	AttrGenAIRequestTopP          = "gen_ai.request.top_p"
	AttrGenAIRequestMaxTokens     = "gen_ai.request.max_tokens"
	AttrGenAIUsageInputTokens     = "gen_ai.usage.input_tokens"
	AttrGenAIUsageOutputTokens    = "gen_ai.usage.output_tokens"
	AttrGenAIResponseFinishReason = "gen_ai.response.finish_reasons"
	AttrGenAIAgentName            = "gen_ai.agent.name"
	AttrGenAIToolName             = "gen_ai.tool.name"
	AttrGenAIToolCallID           = "gen_ai.tool.call.id"
	AttrGenAIConversationID       = "gen_ai.conversation.id"
)

// 业务自定义属性键（gameops.* 前缀，避免和 OTel 规范冲突）。
const (
	AttrGameOpsAgentKind       = "gameops.agent.kind"        // coordinator / diagnosis / ...
	AttrGameOpsToolTarget      = "gameops.tool.target"       // bk-monitor / bcs-read / ...
	AttrGameOpsRedactRule      = "gameops.guard.rule"        // token_like_secret / ...
	AttrGameOpsRedactHits      = "gameops.guard.redact_hits" // int
	AttrGameOpsWebhookSource   = "gameops.webhook.source"    // bk_alarm / tapd
	AttrGameOpsWebhookOutcome  = "gameops.webhook.outcome"   // accepted / rejected / signature_failed
	AttrGameOpsCaseID          = "gameops.case_id"
	AttrGameOpsInjectionSource = "gameops.input_guard.rule"
)

// System 常量（gen_ai.system 值）。
const (
	SystemHunyuan  = "hunyuan"
	SystemOpenAI   = "openai"
	SystemUnknown  = "unknown"
	OperationChat  = "chat"
	OperationAgent = "agent"
	OperationTool  = "execute_tool"
)

// StartLLMSpan 按 GenAI 规范启动一条 `chat` Span。
//
// 约定的 span name 格式："{operation} {model}"，例如 `chat hunyuan-turbo-s`；
// 符合 OTel GenAI 规范"span 名应尽量信息量大、基数可控"的要求。
func StartLLMSpan(ctx context.Context, opts LLMSpanOptions) (context.Context, trace.Span) {
	name := opts.Operation
	if name == "" {
		name = OperationChat
	}
	if opts.Model != "" {
		name = name + " " + opts.Model
	}
	ctx, span := Tracer().Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	span.SetAttributes(
		attribute.String(AttrGenAISystem, systemFromModel(opts.Model, opts.System)),
		attribute.String(AttrGenAIOperationName, nonEmpty(opts.Operation, OperationChat)),
		attribute.String(AttrGenAIRequestModel, opts.Model),
	)
	if opts.AgentName != "" {
		span.SetAttributes(
			attribute.String(AttrGenAIAgentName, opts.AgentName),
			attribute.String(AttrGameOpsAgentKind, opts.AgentName),
		)
	}
	if opts.ConversationID != "" {
		span.SetAttributes(attribute.String(AttrGenAIConversationID, opts.ConversationID))
	}
	if opts.Temperature != nil {
		span.SetAttributes(attribute.Float64(AttrGenAIRequestTemperature, *opts.Temperature))
	}
	if opts.TopP != nil {
		span.SetAttributes(attribute.Float64(AttrGenAIRequestTopP, *opts.TopP))
	}
	if opts.MaxTokens > 0 {
		span.SetAttributes(attribute.Int(AttrGenAIRequestMaxTokens, opts.MaxTokens))
	}
	return ctx, span
}

// LLMSpanOptions LLM Span 入参。
type LLMSpanOptions struct {
	Operation      string // chat / text_completion / agent；空默认 chat
	System         string // openai / hunyuan；空按 Model 推断
	Model          string
	AgentName      string
	ConversationID string
	Temperature    *float64
	TopP           *float64
	MaxTokens      int
}

// FinishLLMSpan 结束 LLM Span，写入 usage / finish_reason / error。
func FinishLLMSpan(span trace.Span, result LLMSpanResult) {
	if span == nil {
		return
	}
	if result.ResponseModel != "" {
		span.SetAttributes(attribute.String(AttrGenAIResponseModel, result.ResponseModel))
	}
	if result.InputTokens > 0 {
		span.SetAttributes(attribute.Int(AttrGenAIUsageInputTokens, result.InputTokens))
	}
	if result.OutputTokens > 0 {
		span.SetAttributes(attribute.Int(AttrGenAIUsageOutputTokens, result.OutputTokens))
	}
	if len(result.FinishReasons) > 0 {
		span.SetAttributes(attribute.StringSlice(AttrGenAIResponseFinishReason, result.FinishReasons))
	}
	if result.Err != nil {
		span.RecordError(result.Err)
		span.SetStatus(codes.Error, result.Err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// LLMSpanResult 结束 LLM Span 时的结果信息。
type LLMSpanResult struct {
	ResponseModel string
	InputTokens   int
	OutputTokens  int
	FinishReasons []string
	Err           error
}

// StartToolSpan 按 GenAI 规范启动一条 `execute_tool` Span。
func StartToolSpan(ctx context.Context, opts ToolSpanOptions) (context.Context, trace.Span) {
	name := OperationTool + " " + opts.ToolName
	ctx, span := Tracer().Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	span.SetAttributes(
		attribute.String(AttrGenAIOperationName, OperationTool),
		attribute.String(AttrGenAIToolName, opts.ToolName),
	)
	if opts.AgentName != "" {
		span.SetAttributes(
			attribute.String(AttrGenAIAgentName, opts.AgentName),
			attribute.String(AttrGameOpsAgentKind, opts.AgentName),
		)
	}
	if opts.Target != "" {
		span.SetAttributes(attribute.String(AttrGameOpsToolTarget, opts.Target))
	}
	if opts.CallID != "" {
		span.SetAttributes(attribute.String(AttrGenAIToolCallID, opts.CallID))
	}
	return ctx, span
}

// ToolSpanOptions Tool Span 入参。
type ToolSpanOptions struct {
	ToolName  string
	AgentName string
	Target    string
	CallID    string
}

// FinishToolSpan 结束 Tool Span，写入 error / duration。
func FinishToolSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// systemFromModel 按模型名推断 gen_ai.system 值。
func systemFromModel(model, hint string) string {
	if hint != "" {
		return hint
	}
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "hunyuan"):
		return SystemHunyuan
	case strings.HasPrefix(m, "gpt"), strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"):
		return SystemOpenAI
	default:
		return SystemUnknown
	}
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
