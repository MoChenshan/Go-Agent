package callbacks

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// modelCallbackAdapter adapts Eino callback handlers to tRPC model callbacks.
type modelCallbackAdapter struct {
	einoHandler callbacks.Handler
	config      *CallbackConfig
}

// beforeModel implements tRPC BeforeModelCallback by calling Eino OnStart.
func (adapter *modelCallbackAdapter) beforeModel(
	ctx context.Context,
	req *model.Request,
) (*model.Response, error) {

	// Create Eino RunInfo
	runInfo := &callbacks.RunInfo{
		Name: "model_execution",
		Type: "model",
	}

	// Create Eino CallbackInput from tRPC model request
	einoInput := createEinoModelInput(req)

	// Call Eino OnStart
	newCtx := adapter.einoHandler.OnStart(ctx, runInfo, einoInput)

	// Extract any custom response from context (if the Eino handler set one)
	if customResp := extractCustomResponse(newCtx); customResp != nil {

		return customResp, nil
	}

	return nil, nil
}

// afterModel implements tRPC AfterModelCallback by calling Eino OnEnd or OnError.
func (adapter *modelCallbackAdapter) afterModel(
	ctx context.Context,
	req *model.Request,
	resp *model.Response,
	modelErr error,
) (*model.Response, error) {

	// Create Eino RunInfo
	runInfo := &callbacks.RunInfo{
		Name: "model_execution",
		Type: "model",
	}

	if modelErr != nil {
		// Call Eino OnError
		adapter.einoHandler.OnError(ctx, runInfo, modelErr)
		// Don't check for custom response when there's an error to avoid masking the error
		return nil, nil
	}

	// Create Eino CallbackOutput from tRPC model response
	einoOutput := createEinoModelOutput(req, resp)

	// Call Eino OnEnd
	adapter.einoHandler.OnEnd(ctx, runInfo, einoOutput)

	// Check if there's a custom response to return
	if customResp := extractCustomResponse(ctx); customResp != nil {
		return customResp, nil
	}

	return nil, nil
}

// createEinoModelInput converts tRPC model request to Eino CallbackInput.
func createEinoModelInput(req *model.Request) callbacks.CallbackInput {
	// Create a structured input that mimics Eino's model.CallbackInput
	input := map[string]any{
		"messages": convertTRPCMessagesToEino(req.Messages),
	}

	// Add generation config if available
	input["config"] = map[string]any{
		"max_tokens":  req.GenerationConfig.MaxTokens,
		"temperature": req.GenerationConfig.Temperature,
		"top_p":       req.GenerationConfig.TopP,
		"stream":      req.GenerationConfig.Stream,
	}

	// Add tools if available
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for toolName, toolObj := range req.Tools {
			declaration := toolObj.Declaration()
			toolMap := map[string]any{
				"name":        toolName,
				"description": declaration.Description,
			}
			if declaration.InputSchema != nil {
				toolMap["input_schema"] = declaration.InputSchema
			}
			if declaration.OutputSchema != nil {
				toolMap["output_schema"] = declaration.OutputSchema
			}
			tools = append(tools, toolMap)
		}
		input["tools"] = tools
	}

	return input
}

// createEinoModelOutput converts tRPC model response to Eino CallbackOutput.
func createEinoModelOutput(req *model.Request, resp *model.Response) callbacks.CallbackOutput {
	// Create a structured output that mimics Eino's model.CallbackOutput
	output := map[string]any{}

	// Add response message if available
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		output["message"] = map[string]any{
			"role":    choice.Message.Role,
			"content": choice.Message.Content,
		}

		// Add tool calls if present
		if len(choice.Message.ToolCalls) > 0 {
			output["tool_calls"] = convertToolCallsToMap(choice.Message.ToolCalls)
		}
	}

	// Add usage information if available
	if resp.Usage != nil {
		output["usage"] = map[string]any{
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		}
	}

	// Add metadata
	output["done"] = resp.Done
	if resp.Error != nil {
		output["error"] = map[string]any{
			"message": resp.Error.Message,
			"type":    resp.Error.Type,
		}
	}

	return output
}

// convertTRPCMessagesToEino converts tRPC messages to a format suitable for Eino.
func convertTRPCMessagesToEino(messages []model.Message) []map[string]any {
	einoMessages := make([]map[string]any, len(messages))
	for i, msg := range messages {
		einoMsg := map[string]any{
			"role":    msg.Role,
			"content": msg.Content,
		}

		// Add tool-related fields if present
		if msg.ToolID != "" {
			einoMsg["tool_id"] = msg.ToolID
		}
		if len(msg.ToolCalls) > 0 {
			einoMsg["tool_calls"] = convertToolCallsToMap(msg.ToolCalls)
		}

		einoMessages[i] = einoMsg
	}
	return einoMessages
}

// convertToolCallsToMap converts tRPC tool calls to map format for Eino.
func convertToolCallsToMap(toolCalls []model.ToolCall) []map[string]any {
	result := make([]map[string]any, len(toolCalls))
	for i, toolCall := range toolCalls {
		result[i] = map[string]any{
			"id":   toolCall.ID,
			"type": toolCall.Type,
			"function": map[string]any{
				"name":      toolCall.Function.Name,
				"arguments": string(toolCall.Function.Arguments),
			},
		}
	}
	return result
}

// extractCustomResponse extracts custom responses from context.
// This is a placeholder for future context-based response passing.
func extractCustomResponse(ctx context.Context) *model.Response {
	// For now, we don't implement context-based response passing
	// This can be extended later if needed
	return nil
}

// NewModelCallbackAdapter creates a new model callback adapter.
func NewModelCallbackAdapter(einoHandler callbacks.Handler, config *CallbackConfig) *model.Callbacks {
	adapter := &modelCallbackAdapter{
		einoHandler: einoHandler,
		config:      config,
	}

	return model.NewCallbacks().
		RegisterBeforeModel(adapter.beforeModel).
		RegisterAfterModel(adapter.afterModel)
}
