// Package telemetry provides telemetry and observability functionality for the trpc-agent-go framework.
// It includes tracing, metrics, and monitoring capabilities for agent operations.
package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
)

// telemetry service constants.
var (
	OperationExecuteTool = "execute_tool"
	OperationCallLLM     = "call_llm"
	OperationChat        = "chat"
	OperationRunRunner   = "run_runner" // attribute of SpanNameInvocation
)

// Telemetry attribute keys aliases from semconv package.
var (
	ResourceServiceNamespace = semconvtrace.ResourceServiceNamespace
	ResourceServiceName      = semconvtrace.ResourceServiceName
	ResourceServiceVersion   = semconvtrace.ResourceServiceVersion

	KeyEventID      = semconvtrace.KeyEventID
	KeyInvocationID = semconvtrace.KeyInvocationID
	KeyLLMRequest   = semconvtrace.KeyLLMRequest
	KeyLLMResponse  = semconvtrace.KeyLLMResponse

	KeyRunnerName      = semconvtrace.KeyRunnerName
	KeyRunnerUserID    = semconvtrace.KeyRunnerUserID
	KeyRunnerSessionID = semconvtrace.KeyRunnerSessionID
	KeyRunnerInput     = semconvtrace.KeyRunnerInput
	KeyRunnerOutput    = semconvtrace.KeyRunnerOutput

	// Internal keys not in semconv (used by galileo converter and other internal components)
	KeySessionID    = "trpc.go.agent.session_id"
	KeyToolCallArgs = "trpc.go.agent.tool_call_args"
	KeyToolResponse = "trpc.go.agent.tool_response"

	KeyTRPCAgentGoAppName                = semconvtrace.KeyTRPCAgentGoAppName
	KeyTRPCAgentGoUserID                 = semconvtrace.KeyTRPCAgentGoUserID
	KeyTRPCAgentGoClientTimeToFirstToken = semconvtrace.KeyTRPCAgentGoClientTimeToFirstToken

	KeyGenAIOperationName = semconvtrace.KeyGenAIOperationName
	KeyGenAISystem        = semconvtrace.KeyGenAISystem

	KeyGenAIRequestModel            = semconvtrace.KeyGenAIRequestModel
	KeyGenAIRequestIsStream         = semconvtrace.KeyGenAIRequestIsStream
	KeyGenAIRequestChoiceCount      = semconvtrace.KeyGenAIRequestChoiceCount
	KeyGenAIInputMessages           = semconvtrace.KeyGenAIInputMessages
	KeyGenAIOutputMessages          = semconvtrace.KeyGenAIOutputMessages
	KeyGenAIAgentName               = semconvtrace.KeyGenAIAgentName
	KeyGenAIConversationID          = semconvtrace.KeyGenAIConversationID
	KeyGenAIUsageOutputTokens       = semconvtrace.KeyGenAIUsageOutputTokens
	KeyGenAIUsageInputTokens        = semconvtrace.KeyGenAIUsageInputTokens
	KeyGenAIProviderName            = semconvtrace.KeyGenAIProviderName
	KeyGenAIAgentDescription        = semconvtrace.KeyGenAIAgentDescription
	KeyGenAIResponseFinishReasons   = semconvtrace.KeyGenAIResponseFinishReasons
	KeyGenAIResponseID              = semconvtrace.KeyGenAIResponseID
	KeyGenAIResponseModel           = semconvtrace.KeyGenAIResponseModel
	KeyGenAIRequestStopSequences    = semconvtrace.KeyGenAIRequestStopSequences
	KeyGenAIRequestFrequencyPenalty = semconvtrace.KeyGenAIRequestFrequencyPenalty
	KeyGenAIRequestMaxTokens        = semconvtrace.KeyGenAIRequestMaxTokens
	KeyGenAIRequestPresencePenalty  = semconvtrace.KeyGenAIRequestPresencePenalty
	KeyGenAIRequestTemperature      = semconvtrace.KeyGenAIRequestTemperature
	KeyGenAIRequestTopP             = semconvtrace.KeyGenAIRequestTopP
	KeyGenAISystemInstructions      = semconvtrace.KeyGenAISystemInstructions
	KeyGenAITokenType               = semconvtrace.KeyGenAITokenType

	KeyGenAIToolName          = semconvtrace.KeyGenAIToolName
	KeyGenAIToolDescription   = semconvtrace.KeyGenAIToolDescription
	KeyGenAIToolDesc          = semconvtrace.KeyGenAIToolDescription // Alias for backward compatibility
	KeyGenAIToolCallID        = semconvtrace.KeyGenAIToolCallID
	KeyGenAIToolCallArguments = semconvtrace.KeyGenAIToolCallArguments
	KeyGenAIToolCallResult    = semconvtrace.KeyGenAIToolCallResult

	KeyGenAIRequestEncodingFormats = semconvtrace.KeyGenAIRequestEncodingFormats

	KeyErrorType          = semconvtrace.KeyErrorType
	KeyErrorMessage       = semconvtrace.KeyErrorMessage
	ValueDefaultErrorType = semconvtrace.ValueDefaultErrorType

	SystemTRPCGoAgent = semconvtrace.SystemTRPCGoAgent

	// Taiji-specific attributes (not in semconv)
	KeyTaijiApplicationID = "taiji.application_id"
	KeyTaijiURL           = "taiji.url"
	ToolNameMergedTools   = "(merged tools)"
)

// BuildInvocationAttributes extracts common attributes from the invocation
func BuildInvocationAttributes(invocation *agent.Invocation) []attribute.KeyValue {
	if invocation == nil {
		return nil
	}

	attrs := []attribute.KeyValue{
		attribute.String(KeyInvocationID, invocation.InvocationID),
	}

	if invocation.Session != nil {
		attrs = append(attrs,
			attribute.String(KeyGenAIConversationID, invocation.Session.ID),
			attribute.String(KeyRunnerUserID, invocation.Session.UserID),
		)
	}

	return attrs
}
