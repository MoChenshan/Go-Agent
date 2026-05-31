package callbacks

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/callbacks"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// toolCallbackAdapter adapts Eino callback handlers to tRPC tool callbacks.
type toolCallbackAdapter struct {
	einoHandler callbacks.Handler
	config      *CallbackConfig
}

// CallbackConfig provides configuration for callback adapters.
type CallbackConfig struct {
	NodeFilter map[string]bool
}

// beforeTool implements tRPC BeforeToolCallback by calling Eino OnStart.
func (adapter *toolCallbackAdapter) beforeTool(
	ctx context.Context,
	toolName string,
	toolDeclaration *tool.Declaration,
	jsonArgs *[]byte,
) (any, error) {
	// Check node filter
	if len(adapter.config.NodeFilter) > 0 && !adapter.config.NodeFilter[toolName] {
		return nil, nil
	}

	// Create Eino RunInfo
	runInfo := &callbacks.RunInfo{
		Name: toolName,
		Type: "tool",
	}

	// Create Eino CallbackInput from tRPC tool call data
	einoInput := createEinoToolInput(toolName, toolDeclaration, jsonArgs)

	// Call Eino OnStart
	newCtx := adapter.einoHandler.OnStart(ctx, runInfo, einoInput)

	// Extract any custom result from context (if the Eino handler set one)
	if customResult := extractCustomResult(newCtx); customResult != nil {
		return customResult, nil
	}

	return nil, nil
}

// afterTool implements tRPC AfterToolCallback by calling Eino OnEnd or OnError.
func (adapter *toolCallbackAdapter) afterTool(
	ctx context.Context,
	toolName string,
	toolDeclaration *tool.Declaration,
	jsonArgs []byte,
	result any,
	runErr error,
) (any, error) {

	// Check node filter
	if len(adapter.config.NodeFilter) > 0 && !adapter.config.NodeFilter[toolName] {
		return nil, nil
	}

	// Create Eino RunInfo
	runInfo := &callbacks.RunInfo{
		Name: toolName,
		Type: "tool",
	}

	if runErr != nil {
		// Call Eino OnError
		adapter.einoHandler.OnError(ctx, runInfo, runErr)
		// Return the original error to propagate it up
		return nil, runErr
	}

	// Create Eino CallbackOutput from tRPC tool result
	einoOutput := createEinoToolOutput(result)

	// Call Eino OnEnd
	adapter.einoHandler.OnEnd(ctx, runInfo, einoOutput)

	// Check if there's a custom result to return
	if customResult := extractCustomResult(ctx); customResult != nil {
		return customResult, nil
	}

	return nil, nil
}

// createEinoToolInput converts tRPC tool call data to Eino CallbackInput.
func createEinoToolInput(toolName string, toolDeclaration *tool.Declaration, jsonArgs *[]byte) callbacks.CallbackInput {
	// Create a structured input that mimics Eino's tool.CallbackInput
	input := map[string]any{
		"tool_name":        toolName,
		"tool_declaration": toolDeclaration,
		"arguments": func() string {
			if jsonArgs == nil {
				return ""
			}
			return string(*jsonArgs)
		}(), // Keep as string to match Eino's JSON handling
	}

	// Add more fields if available
	if toolDeclaration != nil {
		input["description"] = toolDeclaration.Description
		if toolDeclaration.InputSchema != nil {
			input["input_schema"] = toolDeclaration.InputSchema
		}
		if toolDeclaration.OutputSchema != nil {
			input["output_schema"] = toolDeclaration.OutputSchema
		}
	}

	return input
}

// createEinoToolOutput converts tRPC tool result to Eino CallbackOutput.
func createEinoToolOutput(result any) callbacks.CallbackOutput {
	// Create a structured output that mimics Eino's tool.CallbackOutput
	output := map[string]any{
		"result": result,
	}

	// Add additional metadata if needed
	if result != nil {
		output["result_type"] = fmt.Sprintf("%T", result)
	}

	return output
}

// extractCustomResult extracts custom results from context.
// This is a placeholder for future context-based result passing.
func extractCustomResult(ctx context.Context) any {
	// For now, we don't implement context-based result passing
	// This can be extended later if needed
	return nil
}

// NewToolCallbackAdapter creates a new tool callback adapter.
func NewToolCallbackAdapter(einoHandler callbacks.Handler, config *CallbackConfig) *tool.Callbacks {
	adapter := &toolCallbackAdapter{
		einoHandler: einoHandler,
		config:      config,
	}

	return tool.NewCallbacks().
		RegisterBeforeTool(adapter.beforeTool).
		RegisterAfterTool(adapter.afterTool)
}
