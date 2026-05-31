package zhiyanllm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	itelemetry "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm/internal/telemetry"
	"git.woa.com/zhiyan-monitor/sdk/llm_go_sdk/semconvai"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"trpc.group/trpc-go/trpc-agent-go/model"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/tracetransform"
)

var _ trace.SpanExporter = (*exporter)(nil)

const (
	genAIPromptContentKeyFormat     = "gen_ai.prompts.%d.content"
	genAIPromptRoleKeyFormat        = "gen_ai.prompts.%d.role"
	genAIPromptToolCallIDKeyFormat  = "gen_ai.prompts.%d.tool_call_id"
	genAIRequestFunctionNameKey     = "gen_ai.request.functions.%d.name"
	genAIRequestFunctionDescKey     = "gen_ai.request.functions.%d.description"
	genAIRequestFunctionParamsKey   = "gen_ai.request.functions.%d.parameters"
	genAICompletionFinishReasonKey  = "gen_ai.completions.finish_reason"
	genAICompletionContentKeyFormat = "gen_ai.completions.%d.content"
	genAICompletionRoleKeyFormat    = "gen_ai.completions.%d.role"
	genAIToolCallIDKeyFormat        = "%s.tool_calls.%d.id"
	genAIToolCallNameKeyFormat      = "%s.tool_calls.%d.name"
	genAIToolCallArgumentsKeyFormat = "%s.tool_calls.%d.arguments"
)

type exporter struct {
	client                    otlptrace.Client
	attributeValueLengthLimit int

	mu      sync.RWMutex
	started bool

	startOnce sync.Once
	stopOnce  sync.Once
}

type requestFunctionDefinition struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type requestToolDefinitionsPayload struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type requestToolsPayload struct {
	Tools []requestToolPayload `json:"tools,omitempty"`
}

type requestToolPayload struct {
	Function requestToolFunctionPayload `json:"function,omitempty"`
}

type requestToolFunctionPayload struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type llmOutputValue struct {
	Index int    `json:"index"`
	Type  string `json:"type"`
	Name  string `json:"name,omitempty"`
	ID    string `json:"id,omitempty"`
	Input string `json:"input,omitempty"`
	Text  string `json:"text,omitempty"`
}

func newExporter(ctx context.Context, attributeValueLengthLimit int, opts ...otlptracehttp.Option) (*exporter, error) {
	e := &exporter{
		client:                    otlptracehttp.NewClient(opts...),
		attributeValueLengthLimit: attributeValueLengthLimit,
	}
	if err := e.Start(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *exporter) ExportSpans(ctx context.Context, ss []trace.ReadOnlySpan) error {
	protoSpans := tracetransform.Spans(ss)

	protoSpans = transformWithDiagnostics(protoSpans, e.attributeValueLengthLimit)

	err := e.client.UploadTraces(ctx, protoSpans)
	if err != nil {
		return fmt.Errorf("exporting spans: uploading traces: %w", err)
	}
	return nil
}

func transform(ss []*tracepb.ResourceSpans) []*tracepb.ResourceSpans {
	return transformWithDiagnostics(ss, 0)
}

func transformWithDiagnostics(
	ss []*tracepb.ResourceSpans,
	attributeValueLengthLimit int,
) []*tracepb.ResourceSpans {
	if len(ss) == 0 {
		return ss
	}

	for _, rs := range ss {
		if rs == nil {
			continue
		}

		for _, scopeSpans := range rs.ScopeSpans {
			if scopeSpans == nil {
				continue
			}

			for _, span := range scopeSpans.Spans {
				if span == nil {
					continue
				}

				transformSpanWithDiagnostics(span, transformDiagnostics{
					spanName:                  span.Name,
					attributeValueLengthLimit: attributeValueLengthLimit,
				})
			}
		}
	}

	return ss
}

func transformSpan(span *tracepb.Span) {
	transformSpanWithDiagnostics(span, transformDiagnostics{})
}

func transformSpanWithDiagnostics(span *tracepb.Span, diagnostics transformDiagnostics) {
	if span.Attributes == nil {
		return
	}

	// Find the operation name
	var operationName string
	for _, attr := range span.Attributes {
		if attr.Key == itelemetry.KeyGenAIOperationName {
			if attr.Value != nil && attr.Value.GetStringValue() != "" {
				operationName = attr.Value.GetStringValue()
				break
			}
		}
	}

	switch operationName {
	case itelemetry.OperationChat:
		transformCallLLM(span, diagnostics)
	case itelemetry.OperationExecuteTool:
		transformExecuteTool(span)
	case itelemetry.OperationInvokeAgent:
		transformInvokeAgent(span)
	}
}

func transformCallLLM(span *tracepb.Span, diagnostics transformDiagnostics) {
	var newAttributes []*commonpb.KeyValue

	// Add observation type
	newAttributes = append(newAttributes, &commonpb.KeyValue{
		Key: string(semconvai.LLMSpanKind),
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: semconvai.LLM},
		},
	})

	// Process existing attributes
	var llmSessionID *commonpb.AnyValue
	var timeToFirstTokenNS int64
	var hasMappedTTFT bool
	var hasSDKTTFT bool
	var llmRequestAttr *commonpb.KeyValue
	var llmResponseAttr *commonpb.KeyValue
	var toolDefinitionsAttr *commonpb.KeyValue
	var hasInputValue bool
	var hasOutputValue bool
	for _, attr := range span.Attributes {
		switch attr.Key {
		case itelemetry.KeyLLMRequest:
			llmRequestAttr = attr
		case itelemetry.KeyLLMResponse:
			llmResponseAttr = attr
		case semconvtrace.KeyGenAIRequestToolDefinitions:
			toolDefinitionsAttr = attr
		case itelemetry.KeyGenAIInputMessages:
			if llmRequestAttr == nil {
				llmRequestAttr = inputMessagesAsLLMRequestAttr(attr)
			}
			newAttributes = append(newAttributes, attr)
		case semconvtrace.KeyTRPCAgentGoClientTimeToFirstToken:
			newAttributes = append(newAttributes, attr) // keep original key for compatibility
			if ns, ok := secondsAnyValueToNanos(attr.Value); ok {
				timeToFirstTokenNS = ns
				hasMappedTTFT = true
			}
		case string(semconvai.LLMResponseTimeToFirstToken):
			hasSDKTTFT = true
			newAttributes = append(newAttributes, attr)
		case string(semconvai.LLMInputValues):
			hasInputValue = true
			newAttributes = append(newAttributes, attr)
		case string(semconvai.LLMOutputValues):
			hasOutputValue = true
			newAttributes = append(newAttributes, attr)
		case itelemetry.KeyGenAIConversationID, itelemetry.KeyRunnerSessionID, string(semconvai.LLMSessionID):
			llmSessionID = attr.Value
		case itelemetry.KeyRunnerUserID:
			newAttributes = append(newAttributes, &commonpb.KeyValue{Key: string(semconvai.LLMUserID), Value: attr.Value})
		default:
			newAttributes = append(newAttributes, attr)
		}
	}
	if llmRequestAttr != nil {
		newAttributes = append(newAttributes,
			extractLLMRequestAttributesWithDiagnostics(diagnostics, llmRequestAttr, toolDefinitionsAttr, !hasInputValue)...)
	} else if toolDefinitionsAttr != nil {
		newAttributes = append(newAttributes, extractRequestFunctionAttributesWithDiagnostics(diagnostics, toolDefinitionsAttr)...)
	}
	if llmResponseAttr != nil {
		newAttributes = append(newAttributes,
			extractLLMResponseAttributesWithDiagnostics(diagnostics, llmResponseAttr, !hasOutputValue)...)
	}
	if !hasSDKTTFT && hasMappedTTFT {
		newAttributes = append(newAttributes, &commonpb.KeyValue{
			Key: string(semconvai.LLMResponseTimeToFirstToken),
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_IntValue{IntValue: timeToFirstTokenNS},
			},
		})
	}
	if llmSessionID != nil { // use post set session id
		newAttributes = append(newAttributes, &commonpb.KeyValue{Key: string(semconvai.LLMSessionID), Value: llmSessionID})
	}

	// Replace span attributes
	span.Attributes = newAttributes
}

func secondsAnyValueToNanos(v *commonpb.AnyValue) (int64, bool) {
	if v == nil {
		return 0, false
	}

	var seconds float64
	switch vv := v.Value.(type) {
	case *commonpb.AnyValue_DoubleValue:
		seconds = vv.DoubleValue
	case *commonpb.AnyValue_IntValue:
		seconds = float64(vv.IntValue)
	case *commonpb.AnyValue_StringValue:
		f, err := strconv.ParseFloat(strings.TrimSpace(vv.StringValue), 64)
		if err != nil {
			return 0, false
		}
		seconds = f
	default:
		return 0, false
	}

	if seconds < 0 {
		return 0, false
	}
	return int64(math.Round(seconds * 1e9)), true
}

// extractLLMRequestAttributes extracts and processes LLM request attributes
func extractLLMRequestAttributes(
	attr *commonpb.KeyValue,
	toolDefinitionsAttr *commonpb.KeyValue,
	includeInputValue bool,
) []*commonpb.KeyValue {
	return extractLLMRequestAttributesWithDiagnostics(
		transformDiagnostics{},
		attr,
		toolDefinitionsAttr,
		includeInputValue,
	)
}

func extractLLMRequestAttributesWithDiagnostics(
	diagnostics transformDiagnostics,
	attr *commonpb.KeyValue,
	toolDefinitionsAttr *commonpb.KeyValue,
	includeInputValue bool,
) []*commonpb.KeyValue {
	var attributes []*commonpb.KeyValue

	if attr.Value != nil {
		request := attr.Value.GetStringValue()
		var req model.Request
		requestParsed := false
		if err := json.Unmarshal([]byte(request), &req); err == nil {
			requestParsed = true
			attributes = append(attributes, extractPromptAttributes(req)...)
			attributes = append(attributes, extractGenerationConfigAttributes(req)...)
			if includeInputValue {
				attributes = append(attributes,
					newStringAttribute(string(semconvai.LLMInputValues), buildPromptInputValue(req.Messages, request)))
			}
		} else {
			logJSONUnmarshalFailure(diagnostics, attr.Key, len(request), err)
			if includeInputValue {
				attributes = append(attributes, newStringAttribute(string(semconvai.LLMInputValues), request))
			}
		}

		requestFunctionAttributes := extractRequestFunctionAttributesWithDiagnostics(diagnostics, toolDefinitionsAttr)
		if len(requestFunctionAttributes) == 0 && requestParsed {
			requestFunctionAttributes = extractRequestFunctionAttributesFromRequestJSONWithDiagnostics(diagnostics, request)
		}
		attributes = append(attributes, requestFunctionAttributes...)
	} else {
		if includeInputValue {
			attributes = append(attributes, newStringAttribute(string(semconvai.LLMInputValues), "N/A"))
		}
	}
	return attributes
}

func inputMessagesAsLLMRequestAttr(attr *commonpb.KeyValue) *commonpb.KeyValue {
	if attr == nil || attr.Value == nil {
		return attr
	}
	raw := strings.TrimSpace(attr.Value.GetStringValue())
	if raw == "" {
		return attr
	}
	return newStringAttribute(itelemetry.KeyLLMRequest, fmt.Sprintf(`{"messages":%s}`, raw))
}

// extractLLMResponseAttributes extracts and processes LLM response attributes
func extractLLMResponseAttributes(attr *commonpb.KeyValue, includeOutputValue bool) []*commonpb.KeyValue {
	return extractLLMResponseAttributesWithDiagnostics(
		transformDiagnostics{},
		attr,
		includeOutputValue,
	)
}

func extractLLMResponseAttributesWithDiagnostics(
	diagnostics transformDiagnostics,
	attr *commonpb.KeyValue,
	includeOutputValue bool,
) []*commonpb.KeyValue {
	var attributes []*commonpb.KeyValue

	if attr.Value != nil {
		response := attr.Value.GetStringValue()
		var resp model.Response
		if err := json.Unmarshal([]byte(response), &resp); err == nil {
			attributes = append(attributes, extractCompletionAttributes(resp)...)
			attributes = append(attributes, extractTokenUsageAttributes(resp.Usage)...)
			if includeOutputValue {
				attributes = append(attributes,
					newStringAttribute(
						string(semconvai.LLMOutputValues),
						buildCompletionOutputValueWithDiagnostics(diagnostics, resp, response),
					))
			}
		} else {
			logJSONUnmarshalFailure(diagnostics, attr.Key, len(response), err)
			if includeOutputValue {
				attributes = append(attributes, newStringAttribute(string(semconvai.LLMOutputValues), response))
			}
		}
	} else {
		if includeOutputValue {
			attributes = append(attributes, newStringAttribute(string(semconvai.LLMOutputValues), "N/A"))
		}
	}
	return attributes
}

func extractPromptAttributes(req model.Request) []*commonpb.KeyValue {
	return buildPromptAttributes(req.Messages)
}

func buildPromptInputValue(messages []model.Message, fallback string) string {
	if len(messages) == 0 {
		return fallback
	}

	lastContent := ""
	for _, msg := range messages {
		lastContent = promptMessageContent(msg)
	}
	return lastContent
}

func buildPromptAttributes(messages []model.Message) []*commonpb.KeyValue {
	var attributes []*commonpb.KeyValue
	index := 0
	for _, msg := range messages {
		content := promptMessageContent(msg)
		role := msg.Role.String()
		toolCallID := msg.ToolID
		if role == "" && content == "" && toolCallID == "" && len(msg.ToolCalls) == 0 {
			continue
		}
		attrsPrefix := fmt.Sprintf("%s.%d", semconvai.LLMPrompts, index)
		attributes = append(attributes,
			newStringAttribute(fmt.Sprintf(genAIPromptContentKeyFormat, index), content),
			newStringAttribute(fmt.Sprintf(genAIPromptRoleKeyFormat, index), role),
			newStringAttribute(fmt.Sprintf(genAIPromptToolCallIDKeyFormat, index), toolCallID),
		)
		attributes = append(attributes, buildToolCallAttributes(attrsPrefix, msg.ToolCalls)...)
		index++
	}
	return attributes
}

func extractCompletionAttributes(resp model.Response) []*commonpb.KeyValue {
	var attributes []*commonpb.KeyValue
	index := 0
	var lastFinishReason string
	for _, choice := range resp.Choices {
		message := completionMessage(choice)
		role := message.Role.String()
		content := completionMessageContent(message)
		if choice.FinishReason != nil {
			lastFinishReason = *choice.FinishReason
		}
		if role == "" && content == "" && len(message.ToolCalls) == 0 {
			continue
		}
		attrsPrefix := fmt.Sprintf("%s.%d", semconvai.LLMCompletions, index)
		attributes = append(attributes,
			newStringAttribute(fmt.Sprintf(genAICompletionContentKeyFormat, index), content),
			newStringAttribute(fmt.Sprintf(genAICompletionRoleKeyFormat, index), role),
		)
		attributes = append(attributes, buildToolCallAttributes(attrsPrefix, message.ToolCalls)...)
		index++
	}
	if lastFinishReason != "" {
		attributes = append(attributes, newStringAttribute(genAICompletionFinishReasonKey, lastFinishReason))
	}
	return attributes
}

func completionMessage(choice model.Choice) model.Message {
	message := choice.Message
	if message.Role == "" {
		message.Role = choice.Delta.Role
	}
	if message.Content == "" {
		message.Content = choice.Delta.Content
	}
	if len(message.ContentParts) == 0 {
		message.ContentParts = choice.Delta.ContentParts
	}
	if len(message.ToolCalls) == 0 {
		message.ToolCalls = choice.Delta.ToolCalls
	}
	if message.ToolID == "" {
		message.ToolID = choice.Delta.ToolID
	}
	if message.ToolName == "" {
		message.ToolName = choice.Delta.ToolName
	}
	if message.ReasoningContent == "" {
		message.ReasoningContent = choice.Delta.ReasoningContent
	}
	return message
}

func completionMessageContent(msg model.Message) string {
	switch {
	case msg.Content != "":
		return msg.Content
	case len(msg.ContentParts) > 0:
		return marshalValue(msg.ContentParts, "")
	case msg.ReasoningContent != "":
		return msg.ReasoningContent
	default:
		return ""
	}
}

func promptMessageContent(msg model.Message) string {
	switch {
	case msg.Content != "":
		return msg.Content
	case len(msg.ContentParts) > 0:
		return marshalValue(msg.ContentParts, "")
	case msg.ReasoningContent != "":
		return msg.ReasoningContent
	default:
		return ""
	}
}

func buildToolCallAttributes(attrsPrefix string, toolCalls []model.ToolCall) []*commonpb.KeyValue {
	attributes := make([]*commonpb.KeyValue, 0, len(toolCalls)*3)
	for i, toolCall := range toolCalls {
		attributes = append(attributes,
			newStringAttribute(fmt.Sprintf(genAIToolCallIDKeyFormat, attrsPrefix, i), toolCall.ID),
			newStringAttribute(fmt.Sprintf(genAIToolCallNameKeyFormat, attrsPrefix, i), toolCall.Function.Name),
			newStringAttribute(fmt.Sprintf(genAIToolCallArgumentsKeyFormat, attrsPrefix, i), string(toolCall.Function.Arguments)),
		)
	}
	return attributes
}

func extractRequestFunctionAttributes(attr *commonpb.KeyValue) []*commonpb.KeyValue {
	return extractRequestFunctionAttributesWithDiagnostics(transformDiagnostics{}, attr)
}

func extractRequestFunctionAttributesWithDiagnostics(
	diagnostics transformDiagnostics,
	attr *commonpb.KeyValue,
) []*commonpb.KeyValue {
	if attr == nil || attr.Value == nil {
		return nil
	}
	return buildRequestFunctionAttributesFromToolDefinitionsJSONWithDiagnostics(
		diagnostics,
		attr.Key,
		attr.Value.GetStringValue(),
	)
}

func buildRequestFunctionAttributesFromToolDefinitionsJSON(raw string) []*commonpb.KeyValue {
	return buildRequestFunctionAttributesFromToolDefinitionsJSONWithDiagnostics(
		transformDiagnostics{},
		semconvtrace.KeyGenAIRequestToolDefinitions,
		raw,
	)
}

func buildRequestFunctionAttributesFromToolDefinitionsJSONWithDiagnostics(
	diagnostics transformDiagnostics,
	attributeKey string,
	raw string,
) []*commonpb.KeyValue {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var payload []requestToolDefinitionsPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		logJSONUnmarshalFailure(diagnostics, attributeKey, len(raw), err)
		return nil
	}

	definitions := make([]requestFunctionDefinition, 0, len(payload))
	for _, item := range payload {
		definitions = append(definitions, requestFunctionDefinition{
			Name:        item.Name,
			Description: item.Description,
			Parameters:  item.InputSchema,
		})
	}
	return buildRequestFunctionAttributes(definitions)
}

func extractRequestFunctionAttributesFromRequestJSON(raw string) []*commonpb.KeyValue {
	return extractRequestFunctionAttributesFromRequestJSONWithDiagnostics(
		transformDiagnostics{},
		raw,
	)
}

func extractRequestFunctionAttributesFromRequestJSONWithDiagnostics(
	diagnostics transformDiagnostics,
	raw string,
) []*commonpb.KeyValue {
	var payload requestToolsPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		logJSONUnmarshalFailure(diagnostics, itelemetry.KeyLLMRequest, len(raw), err)
		return nil
	}

	definitions := make([]requestFunctionDefinition, 0, len(payload.Tools))
	for _, tool := range payload.Tools {
		definitions = append(definitions, requestFunctionDefinition{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	return buildRequestFunctionAttributes(definitions)
}

func buildRequestFunctionAttributes(definitions []requestFunctionDefinition) []*commonpb.KeyValue {
	attributes := make([]*commonpb.KeyValue, 0, len(definitions)*3)
	for i, definition := range definitions {
		attributes = append(attributes,
			newStringAttribute(fmt.Sprintf(genAIRequestFunctionNameKey, i), definition.Name),
			newStringAttribute(fmt.Sprintf(genAIRequestFunctionDescKey, i), definition.Description),
			newStringAttribute(fmt.Sprintf(genAIRequestFunctionParamsKey, i), compactJSONRaw(definition.Parameters)),
		)
	}
	return attributes
}

func compactJSONRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err == nil {
		return buf.String()
	}
	return string(raw)
}

func buildCompletionOutputValue(resp model.Response, fallback string) string {
	return buildCompletionOutputValueWithDiagnostics(transformDiagnostics{}, resp, fallback)
}

func buildCompletionOutputValueWithDiagnostics(
	diagnostics transformDiagnostics,
	resp model.Response,
	fallback string,
) string {
	if len(resp.Choices) == 0 {
		return fallback
	}

	var lastContent string
	var lastFinishReason string
	outputs := make([]llmOutputValue, 0)
	for _, choice := range resp.Choices {
		message := completionMessage(choice)
		lastContent = completionMessageContent(message)
		if choice.FinishReason != nil {
			lastFinishReason = *choice.FinishReason
		}
		for _, toolCall := range message.ToolCalls {
			outputs = append(outputs, llmOutputValue{
				Type:  "tool_use",
				Name:  toolCall.Function.Name,
				ID:    toolCall.ID,
				Input: string(toolCall.Function.Arguments),
			})
		}
	}

	if lastFinishReason == "tool_calls" {
		outputs = append([]llmOutputValue{{Type: "text", Text: lastContent}}, outputs...)
		for i := range outputs {
			outputs[i].Index = i
		}
		if bts, err := json.Marshal(outputs); err == nil {
			return string(bts)
		} else {
			logJSONMarshalFailure(diagnostics, string(semconvai.LLMOutputValues), len(outputs), err)
		}
		return fallback
	}

	if lastContent != "" {
		return lastContent
	}

	if bts, err := json.Marshal(resp.Choices); err == nil {
		return string(bts)
	} else {
		logJSONMarshalFailure(diagnostics, string(semconvai.LLMOutputValues), len(resp.Choices), err)
	}
	return fallback
}

func marshalValue(v any, attributeKey string) string {
	bts, err := json.Marshal(v)
	if err != nil {
		logJSONMarshalFailure(transformDiagnostics{}, attributeKey, 0, err)
		return ""
	}
	return string(bts)
}

func newStringAttribute(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value},
		},
	}
}

// extractGenerationConfigAttributes extracts generation config parameters as attributes
func extractGenerationConfigAttributes(req model.Request) []*commonpb.KeyValue {
	var attributes []*commonpb.KeyValue

	if req.GenerationConfig.MaxTokens != nil {
		attributes = append(attributes, &commonpb.KeyValue{
			Key: string(semconvai.LLMRequestMaxTokens),
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_IntValue{IntValue: int64(*req.GenerationConfig.MaxTokens)},
			},
		})
	}

	if req.GenerationConfig.Temperature != nil {
		attributes = append(attributes, &commonpb.KeyValue{
			Key: string(semconvai.LLMTemperature),
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_DoubleValue{DoubleValue: *req.GenerationConfig.Temperature},
			},
		})
	}

	if req.GenerationConfig.TopP != nil {
		attributes = append(attributes, &commonpb.KeyValue{
			Key: string(semconvai.LLMTopP),
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_DoubleValue{DoubleValue: *req.GenerationConfig.TopP},
			},
		})
	}

	if req.GenerationConfig.FrequencyPenalty != nil {
		attributes = append(attributes, &commonpb.KeyValue{
			Key: string(semconvai.LLMFrequencyPenalty),
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_DoubleValue{DoubleValue: *req.GenerationConfig.FrequencyPenalty},
			},
		})
	}

	if req.GenerationConfig.PresencePenalty != nil {
		attributes = append(attributes, &commonpb.KeyValue{
			Key: string(semconvai.LLMPresencePenalty),
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_DoubleValue{DoubleValue: *req.GenerationConfig.PresencePenalty},
			},
		})
	}

	return attributes
}

// extractTokenUsageAttributes extracts token usage metrics as attributes
func extractTokenUsageAttributes(usage *model.Usage) []*commonpb.KeyValue {
	var attributes []*commonpb.KeyValue

	if usage == nil {
		return attributes
	}

	attributes = append(attributes, &commonpb.KeyValue{
		Key: string(semconvai.LLMUsageTotalTokens),
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: int64(usage.TotalTokens)},
		},
	})
	//  output and input tokens attributes have been set at github trpc-agent-go.
	return attributes
}

func transformExecuteTool(span *tracepb.Span) {
	var newAttributes []*commonpb.KeyValue

	// Add observation type
	newAttributes = append(newAttributes,
		&commonpb.KeyValue{
			Key: string(semconvai.LLMSpanKind),
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: semconvai.TOOL},
			},
		},
		&commonpb.KeyValue{
			Key: "tool.name",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: span.Name},
			},
		},
	)

	// Process existing attributes
	var llmSessionID *commonpb.AnyValue
	for _, attr := range span.Attributes {
		switch attr.Key {
		case semconvtrace.KeyGenAIToolCallArguments:
			if attr.Value != nil {
				newAttributes = append(newAttributes, &commonpb.KeyValue{
					Key: string(semconvai.LLMInputValues),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: attr.Value.GetStringValue()},
					},
				})
			} else {
				newAttributes = append(newAttributes, &commonpb.KeyValue{
					Key: string(semconvai.LLMInputValues),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "N/A"},
					},
				})
			}
			// Skip this attribute (delete it)
		case semconvtrace.KeyGenAIToolCallResult:
			if attr.Value != nil {
				newAttributes = append(newAttributes, &commonpb.KeyValue{
					Key: string(semconvai.LLMOutputValues),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: attr.Value.GetStringValue()},
					},
				})
			} else {
				newAttributes = append(newAttributes, &commonpb.KeyValue{
					Key: string(semconvai.LLMOutputValues),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "N/A"},
					},
				})
			}
			// Skip this attribute (delete it)
		case itelemetry.KeyGenAIToolName:
			newAttributes = append(newAttributes, attr)
		case itelemetry.KeyGenAIConversationID, itelemetry.KeyRunnerSessionID, string(semconvai.LLMSessionID):
			llmSessionID = attr.Value
		case itelemetry.KeyRunnerUserID:
			newAttributes = append(newAttributes, &commonpb.KeyValue{Key: string(semconvai.LLMUserID), Value: attr.Value})
		default:
			// Keep other attributes
			newAttributes = append(newAttributes, attr)
		}
	}
	if llmSessionID != nil { // use post set session id
		newAttributes = append(newAttributes, &commonpb.KeyValue{Key: string(semconvai.LLMSessionID), Value: llmSessionID})
	}

	// Replace span attributes
	span.Attributes = newAttributes
}

func transformInvokeAgent(span *tracepb.Span) {
	if span.Kind == tracepb.Span_SPAN_KIND_UNSPECIFIED || span.Kind == tracepb.Span_SPAN_KIND_INTERNAL {
		span.Kind = tracepb.Span_SPAN_KIND_SERVER
	}

	var newAttributes []*commonpb.KeyValue

	newAttributes = append(newAttributes, &commonpb.KeyValue{
		Key: string(semconvai.LLMSpanKind),
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: semconvai.AGENT},
		},
	})
	// Process existing attributes
	var llmSessionID *commonpb.AnyValue
	for _, attr := range span.Attributes {
		switch attr.Key {
		case itelemetry.KeyGenAIInputMessages:
			if attr.Value != nil {
				newAttributes = append(newAttributes, &commonpb.KeyValue{
					Key: string(semconvai.LLMInputValues),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: attr.Value.GetStringValue()},
					},
				})
			} else {
				newAttributes = append(newAttributes, &commonpb.KeyValue{
					Key: string(semconvai.LLMInputValues),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "N/A"},
					},
				})
			}
		case itelemetry.KeyGenAIOutputMessages:
			if attr.Value != nil {
				newAttributes = append(newAttributes, &commonpb.KeyValue{
					Key: string(semconvai.LLMOutputValues),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: attr.Value.GetStringValue()},
					},
				})
			} else {
				newAttributes = append(newAttributes, &commonpb.KeyValue{
					Key: string(semconvai.LLMOutputValues),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "N/A"},
					},
				})
			}
			// Skip this attribute (delete it)
		case itelemetry.KeyGenAIConversationID, itelemetry.KeyRunnerSessionID, string(semconvai.LLMSessionID):
			llmSessionID = attr.Value
		case itelemetry.KeyRunnerUserID:
			newAttributes = append(newAttributes, &commonpb.KeyValue{Key: string(semconvai.LLMUserID), Value: attr.Value})
		case semconvtrace.KeyGenAIUsageOutputTokens, semconvtrace.KeyGenAIUsageInputTokens:
			// Skip this attribute (delete it)
			continue
		default:
			// Keep other attributes
			newAttributes = append(newAttributes, attr)
		}
	}
	if llmSessionID != nil { // use post set session id
		newAttributes = append(newAttributes, &commonpb.KeyValue{Key: string(semconvai.LLMSessionID), Value: llmSessionID})
	}

	// Replace span attributes
	span.Attributes = newAttributes
}

func (e *exporter) Shutdown(ctx context.Context) error {
	e.mu.RLock()
	started := e.started
	e.mu.RUnlock()

	if !started {
		return nil
	}

	var err error

	e.stopOnce.Do(func() {
		err = e.client.Stop(ctx)
		e.mu.Lock()
		e.started = false
		e.mu.Unlock()
	})

	return err
}

var errAlreadyStarted = errors.New("already started")

func (e *exporter) Start(ctx context.Context) error {
	var err = errAlreadyStarted
	e.startOnce.Do(func() {
		e.mu.Lock()
		e.started = true
		e.mu.Unlock()
		err = e.client.Start(ctx)
	})

	return err
}

// MarshalLog is the marshaling function used by the logging system to represent this exporter.
func (e *exporter) MarshalLog() any {
	return struct {
		Type   string
		Client otlptrace.Client
	}{
		Type:   "otlptrace",
		Client: e.client,
	}
}
