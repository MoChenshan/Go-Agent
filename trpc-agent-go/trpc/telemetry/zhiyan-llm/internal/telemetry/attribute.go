package telemetry

// telemetry service constants.
const (
	ServiceName      = "telemetry"
	ServiceVersion   = "v0.1.0"
	ServiceNamespace = "trpc-go-agent"
	InstrumentName   = "trpc.agent.go"

	SpanNamePrefixExecuteTool = "execute_tool"
	SpanNamePrefixAgentRun    = "agent_run"
	SpanNameInvocation        = "invocation"

	OperationExecuteTool = "execute_tool"
	OperationChat        = "chat"
	OperationRunRunner   = "run_runner" // attribute of SpanNameInvocation
	OperationInvokeAgent = "invoke_agent"
)

const (
	// ProtocolGRPC uses gRPC protocol for OTLP exporter.
	ProtocolGRPC string = "grpc"
	// ProtocolHTTP uses HTTP protocol for OTLP exporter.
	ProtocolHTTP string = "http"
)

// telemetry attributes constants.
var (
	ResourceServiceNamespace = "trpc-go-agent"
	ResourceServiceName      = "telemetry"
	ResourceServiceVersion   = "v0.1.0"

	KeyEventID      = "trpc.go.agent.event_id"
	KeySessionID    = "trpc.go.agent.session_id"
	KeyInvocationID = "trpc.go.agent.invocation_id"
	KeyLLMRequest   = "trpc.go.agent.llm_request"
	KeyLLMResponse  = "trpc.go.agent.llm_response"

	// Runner-related attributes
	KeyRunnerName      = "trpc.go.agent.runner.name"
	KeyRunnerUserID    = "trpc.go.agent.runner.user_id"
	KeyRunnerSessionID = "trpc.go.agent.runner.session_id"
	KeyRunnerInput     = "trpc.go.agent.runner.input"
	KeyRunnerOutput    = "trpc.go.agent.runner.output"

	// Tool-related attributes
	KeyToolCallArgs = "trpc.go.agent.tool_call_args"
	KeyToolResponse = "trpc.go.agent.tool_response"
	KeyToolID       = "trpc.go.agent.tool_id"

	// GenAI operation attributes
	KeyGenAIOperationName     = "gen_ai.operation.name"
	KeyGenAISystem            = "gen_ai.system"
	KeyGenAIToolName          = "gen_ai.tool.name"
	KeyGenAIToolDesc          = "gen_ai.tool.description"
	KeyGenAIRequestModel      = "gen_ai.request.model"
	KeyGenAIInputMessages     = "gen_ai.input.messages"
	KeyGenAIOutputMessages    = "gen_ai.output.messages"
	KeyGenAIAgentName         = "gen_ai.agent.name"
	KeyGenAIConversationID    = "gen_ai.conversation.id"
	KeyGenAIResponseModel     = "gen_ai.response.model"
	KeyGenAIUsageOutputTokens = "gen_ai.usage.output_tokens"
	KeyGenAIResponseID        = "gen_ai.response.id"
	KeyGenAIUsageInputTokens  = "gen_ai.usage.input_tokens"

	// System value
	SystemTRPCGoAgent = "trpc.go.agent"
)
