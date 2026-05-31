package zhiyanllm

import (
	"fmt"
	"strings"
	"testing"

	itelemetry "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm/internal/telemetry"
	"git.woa.com/zhiyan-monitor/sdk/llm_go_sdk/semconvai"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"trpc.group/trpc-go/trpc-agent-go/log"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
)

func TestExtractLLMRequestAttributesExpandsPrompts(t *testing.T) {
	request := `{
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Explain OTEL."}
		],
		"generation_config": {
			"max_tokens": 128,
			"temperature": 0.7,
			"top_p": 0.9
		}
	}`

	attributes := extractLLMRequestAttributes(newStringAttribute("request", request), nil, true)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMInputValues)], "Explain OTEL.")
	assertStringValue(t, got["gen_ai.prompts.0.content"], "You are a helpful assistant.")
	assertStringValue(t, got["gen_ai.prompts.0.role"], "system")
	assertStringValue(t, got["gen_ai.prompts.0.tool_call_id"], "")
	assertStringValue(t, got["gen_ai.prompts.1.content"], "Explain OTEL.")
	assertStringValue(t, got["gen_ai.prompts.1.role"], "user")
	assertStringValue(t, got["gen_ai.prompts.1.tool_call_id"], "")
	assertIntValue(t, got[string(semconvai.LLMRequestMaxTokens)], 128)
	assertDoubleValue(t, got[string(semconvai.LLMTemperature)], 0.7)
	assertDoubleValue(t, got[string(semconvai.LLMTopP)], 0.9)
}

func TestExtractLLMResponseAttributesExpandsCompletions(t *testing.T) {
	response := `{
		"id": "resp-1",
		"choices": [
			{"index": 0, "finish_reason": "stop", "message": {"role": "assistant", "content": "OTEL is an observability framework."}},
			{"index": 1, "finish_reason": "length", "delta": {"role": "assistant", "content": "It provides traces, metrics, and logs."}}
		],
		"usage": {
			"total_tokens": 42
		}
	}`

	attributes := extractLLMResponseAttributes(newStringAttribute("response", response), true)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMOutputValues)], "It provides traces, metrics, and logs.")
	assertStringValue(t, got["gen_ai.completions.0.content"], "OTEL is an observability framework.")
	assertStringValue(t, got["gen_ai.completions.0.role"], "assistant")
	assertStringValue(t, got["gen_ai.completions.1.content"], "It provides traces, metrics, and logs.")
	assertStringValue(t, got["gen_ai.completions.1.role"], "assistant")
	assertStringValue(t, got["gen_ai.completions.finish_reason"], "length")
	assertIntValue(t, got[string(semconvai.LLMUsageTotalTokens)], 42)
}

func TestExtractLLMRequestAttributesKeepsInputValueOnInvalidJSON(t *testing.T) {
	logs := captureWarnLogs(t)
	request := "{invalid json"

	attributes := extractLLMRequestAttributes(newStringAttribute("request", request), nil, true)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMInputValues)], request)
	if _, exists := got["gen_ai.prompts.0.content"]; exists {
		t.Fatal("unexpected prompt attributes for invalid request JSON")
	}
	assertLogContains(t, *logs, "failed to parse span attribute as JSON")
}

func TestExtractLLMRequestAttributesWarnsOnLikelyTruncatedJSON(t *testing.T) {
	logs := captureWarnLogs(t)
	request := `{"messages":[{"role":"user","content":"Explain OTEL."}`

	attributes := extractLLMRequestAttributesWithDiagnostics(
		transformDiagnostics{
			spanName:                  "chat",
			attributeValueLengthLimit: len(request),
		},
		newStringAttribute(itelemetry.KeyLLMRequest, request),
		nil,
		true,
	)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMInputValues)], request)
	if _, exists := got["gen_ai.prompts.0.content"]; exists {
		t.Fatal("unexpected prompt attributes for truncated request JSON")
	}
	assertLogContains(t, *logs, "may be truncated by AttributeValueLengthLimit")
	assertLogContains(t, *logs, "attribute_value_length_limit")
}

func TestExtractLLMRequestAttributesWarnsOnTruncatedToolDefinitions(t *testing.T) {
	logs := captureWarnLogs(t)
	request := `{
		"messages": [
			{"role": "user", "content": "Explain OTEL."}
		]
	}`
	toolDefinitions := `[{"name":"search_docs","description":"Search documents","inputSchema":{"type":"object"}`

	attributes := extractLLMRequestAttributesWithDiagnostics(
		transformDiagnostics{
			spanName:                  "chat",
			attributeValueLengthLimit: len(toolDefinitions),
		},
		newStringAttribute(itelemetry.KeyLLMRequest, request),
		newStringAttribute(semconvtrace.KeyGenAIRequestToolDefinitions, toolDefinitions),
		true,
	)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMInputValues)], "Explain OTEL.")
	if _, exists := got["gen_ai.request.functions.0.name"]; exists {
		t.Fatal("unexpected function attributes for truncated tool definitions JSON")
	}
	assertLogContains(t, *logs, semconvtrace.KeyGenAIRequestToolDefinitions)
	assertLogContains(t, *logs, "may be truncated by AttributeValueLengthLimit")
}

func TestExtractLLMResponseAttributesWarnsOnLikelyTruncatedJSON(t *testing.T) {
	logs := captureWarnLogs(t)
	response := `{"choices":[{"message":{"role":"assistant","content":"OTEL`

	attributes := extractLLMResponseAttributesWithDiagnostics(
		transformDiagnostics{
			spanName:                  "chat",
			attributeValueLengthLimit: len(response),
		},
		newStringAttribute(itelemetry.KeyLLMResponse, response),
		true,
	)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMOutputValues)], response)
	if _, exists := got["gen_ai.completions.0.content"]; exists {
		t.Fatal("unexpected completion attributes for truncated response JSON")
	}
	assertLogContains(t, *logs, itelemetry.KeyLLMResponse)
	assertLogContains(t, *logs, "may be truncated by AttributeValueLengthLimit")
}

func TestExtractLLMRequestAttributesIgnoresNonSDKQueryField(t *testing.T) {
	request := `{
		"query": "Explain OTEL."
	}`

	attributes := extractLLMRequestAttributes(newStringAttribute("request", request), nil, true)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMInputValues)], request)
	if _, exists := got["gen_ai.prompts.0.content"]; exists {
		t.Fatal("unexpected prompt attributes from non-SDK query field")
	}
}

func TestTransformCallLLMMapsInputMessagesToPrompts(t *testing.T) {
	span := &tracepb.Span{Attributes: []*commonpb.KeyValue{
		newStringAttribute(itelemetry.KeyGenAIOperationName, itelemetry.OperationChat),
		newStringAttribute(itelemetry.KeyGenAIInputMessages, `[
			{"role":"system","content":"You are concise."},
			{"role":"user","content":"Explain OTEL."}
		]`),
	}}

	transformSpan(span)
	got := keyValueMap(span.Attributes)

	assertStringValue(t, got["gen_ai.prompts.0.content"], "You are concise.")
	assertStringValue(t, got["gen_ai.prompts.0.role"], "system")
	assertStringValue(t, got["gen_ai.prompts.1.content"], "Explain OTEL.")
	assertStringValue(t, got["gen_ai.prompts.1.role"], "user")
}

func TestExtractLLMResponseAttributesExpandsToolCalls(t *testing.T) {
	response := `{
		"id": "resp-tool",
		"choices": [
			{
				"index": 0,
				"finish_reason": "tool_calls",
				"message": {
					"role": "assistant",
					"tool_calls": [
						{
							"type": "function",
							"id": "call_1",
							"function": {
								"name": "search_docs",
								"arguments": "{\"query\":\"otel\"}"
							}
						}
					]
				}
			}
		]
	}`

	attributes := extractLLMResponseAttributes(newStringAttribute("response", response), true)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMOutputValues)],
		`[{"index":0,"type":"text"},{"index":1,"type":"tool_use","name":"search_docs","id":"call_1","input":"{\"query\":\"otel\"}"}]`)
	assertStringValue(t, got["gen_ai.completions.0.content"], "")
	assertStringValue(t, got["gen_ai.completions.0.role"], "assistant")
	assertStringValue(t, got["gen_ai.completions.finish_reason"], "tool_calls")
	assertStringValue(t, got["gen_ai.completions.0.tool_calls.0.id"], "call_1")
	assertStringValue(t, got["gen_ai.completions.0.tool_calls.0.name"], "search_docs")
	assertStringValue(t, got["gen_ai.completions.0.tool_calls.0.arguments"], `{"query":"otel"}`)
}

func TestExtractLLMRequestAttributesExpandsPromptToolCalls(t *testing.T) {
	request := `{
		"messages": [
			{
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{
						"type": "function",
						"id": "call_1",
						"function": {
							"name": "search_docs",
							"arguments": "{\"query\":\"otel\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"content": "doc result",
				"tool_id": "call_1"
			}
		]
	}`

	attributes := extractLLMRequestAttributes(newStringAttribute("request", request), nil, true)
	got := keyValueMap(attributes)

	assertStringValue(t, got[string(semconvai.LLMInputValues)], "doc result")
	assertStringValue(t, got["gen_ai.prompts.0.content"], "")
	assertStringValue(t, got["gen_ai.prompts.0.role"], "assistant")
	assertStringValue(t, got["gen_ai.prompts.0.tool_calls.0.id"], "call_1")
	assertStringValue(t, got["gen_ai.prompts.0.tool_calls.0.name"], "search_docs")
	assertStringValue(t, got["gen_ai.prompts.0.tool_calls.0.arguments"], `{"query":"otel"}`)
	assertStringValue(t, got["gen_ai.prompts.1.content"], "doc result")
	assertStringValue(t, got["gen_ai.prompts.1.role"], "tool")
	assertStringValue(t, got["gen_ai.prompts.1.tool_call_id"], "call_1")
}

func TestExtractLLMRequestAttributesExpandsRequestFunctions(t *testing.T) {
	request := `{
		"messages": [
			{"role": "user", "content": "Explain OTEL."}
		]
	}`
	toolDefinitions := `[
		{
			"name": "search_docs",
			"description": "Search documents",
			"inputSchema": {
				"type": "object",
				"properties": {
					"query": {"type": "string"}
				},
				"required": ["query"]
			}
		}
	]`

	attributes := extractLLMRequestAttributes(
		newStringAttribute("request", request),
		newStringAttribute("tool_definitions", toolDefinitions),
		true,
	)
	got := keyValueMap(attributes)

	assertStringValue(t, got["gen_ai.request.functions.0.name"], "search_docs")
	assertStringValue(t, got["gen_ai.request.functions.0.description"], "Search documents")
	assertStringValue(t, got["gen_ai.request.functions.0.parameters"],
		`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)
}

func captureWarnLogs(t *testing.T) *[]string {
	t.Helper()
	var logs []string
	original := log.Default
	log.Default = captureLogger{warnf: func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}}
	t.Cleanup(func() {
		log.Default = original
	})
	return &logs
}

type captureLogger struct {
	warnf func(format string, args ...any)
}

func (l captureLogger) Debug(args ...any)                 {}
func (l captureLogger) Debugf(format string, args ...any) {}
func (l captureLogger) Info(args ...any)                  {}
func (l captureLogger) Infof(format string, args ...any)  {}
func (l captureLogger) Warn(args ...any)                  {}
func (l captureLogger) Warnf(format string, args ...any) {
	if l.warnf != nil {
		l.warnf(format, args...)
	}
}
func (l captureLogger) Error(args ...any)                 {}
func (l captureLogger) Errorf(format string, args ...any) {}
func (l captureLogger) Fatal(args ...any)                 {}
func (l captureLogger) Fatalf(format string, args ...any) {}

func assertLogContains(t *testing.T, logs []string, want string) {
	t.Helper()
	for _, msg := range logs {
		if strings.Contains(msg, want) {
			return
		}
	}
	t.Fatalf("missing log containing %q in %#v", want, logs)
}

func keyValueMap(attributes []*commonpb.KeyValue) map[string]*commonpb.AnyValue {
	result := make(map[string]*commonpb.AnyValue, len(attributes))
	for _, attribute := range attributes {
		result[attribute.Key] = attribute.Value
	}
	return result
}

func assertStringValue(t *testing.T, value *commonpb.AnyValue, want string) {
	t.Helper()
	if value == nil {
		t.Fatalf("missing value, want %q", want)
	}
	if got := value.GetStringValue(); got != want {
		t.Fatalf("string value = %q, want %q", got, want)
	}
}

func assertIntValue(t *testing.T, value *commonpb.AnyValue, want int64) {
	t.Helper()
	if value == nil {
		t.Fatalf("missing int value, want %d", want)
	}
	if got := value.GetIntValue(); got != want {
		t.Fatalf("int value = %d, want %d", got, want)
	}
}

func assertDoubleValue(t *testing.T, value *commonpb.AnyValue, want float64) {
	t.Helper()
	if value == nil {
		t.Fatalf("missing double value, want %v", want)
	}
	if got := value.GetDoubleValue(); got != want {
		t.Fatalf("double value = %v, want %v", got, want)
	}
}
