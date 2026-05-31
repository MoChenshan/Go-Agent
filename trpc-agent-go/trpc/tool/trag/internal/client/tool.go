package client

import (
	"context"
	"encoding/json"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TragTool implements the tool.CallableTool interface
type TragTool struct {
	client *Client
	def    *FunctionDefinition
}

// Declaration returns the tool declaration
func (t *TragTool) Declaration() *tool.Declaration {
	inputSchema := t.convertParameterTypeToSchema()

	return &tool.Declaration{
		Name:        t.def.FunctionName,
		Description: t.def.Description,
		InputSchema: inputSchema,
	}
}

// convertParameterTypeToSchema converts the function parameter type to tool.Schema
func (t *TragTool) convertParameterTypeToSchema() *tool.Schema {
	if len(t.def.ParameterType) == 0 {
		return &tool.Schema{
			Type:       "object",
			Properties: map[string]*tool.Schema{},
		}
	}

	// Convert map[string]interface{} to tool.Schema
	return convertMapToSchema(t.def.ParameterType)
}

// convertMapToSchema recursively converts a map to tool.Schema
func convertMapToSchema(m map[string]any) *tool.Schema {
	schema := &tool.Schema{}

	if typeVal, ok := m["type"].(string); ok {
		schema.Type = typeVal
	}

	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}

	if required, ok := m["required"].([]any); ok {
		reqStrings := make([]string, 0, len(required))
		for _, r := range required {
			if s, ok := r.(string); ok {
				reqStrings = append(reqStrings, s)
			}
		}
		schema.Required = reqStrings
	}

	if props, ok := m["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]*tool.Schema)
		for key, val := range props {
			if propMap, ok := val.(map[string]any); ok {
				schema.Properties[key] = convertMapToSchema(propMap)
			}
		}
	}

	if items, ok := m["items"].(map[string]any); ok {
		schema.Items = convertMapToSchema(items)
	}

	if defaultVal, ok := m["default"]; ok {
		schema.Default = defaultVal
	}

	if enumVal, ok := m["enum"].([]any); ok {
		schema.Enum = enumVal
	}

	if ref, ok := m["$ref"].(string); ok {
		schema.Ref = ref
	}

	return schema
}

// Call executes the tool with the provided arguments
func (t *TragTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	// Parse input as JSON parameters
	var params map[string]any
	if len(jsonArgs) > 0 {
		if err := json.Unmarshal(jsonArgs, &params); err != nil {
			return nil, fmt.Errorf("invalid input parameters: %w", err)
		}
	}

	// Submit function request
	submitResp, err := t.client.SubmitFunctionRequest(ctx, t.def.FullName, params)
	if err != nil {
		return nil, fmt.Errorf("failed to submit function request: %w", err)
	}

	if !submitResp.IsSuccess() {
		return nil, fmt.Errorf("submit failed: code=%d, msg=%s", submitResp.Code, submitResp.Message)
	}

	// Extract task ID
	var submitData map[string]any
	if err := json.Unmarshal(submitResp.Data, &submitData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal submit response: %w", err)
	}

	taskID, ok := submitData["taskId"].(string)
	if !ok {
		return nil, fmt.Errorf("task ID not found in response")
	}

	// Wait for result with configured timeout
	resultResp, err := t.client.RetrieveFunctionResult(ctx, taskID, t.client.functionExecTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve function result: %w", err)
	}

	if !resultResp.IsSuccess() {
		return nil, fmt.Errorf("execution failed: code=%d, msg=%s", resultResp.Code, resultResp.Message)
	}

	// Parse and return result data
	var result TragResponse
	if err := json.Unmarshal(resultResp.Data, &result); err != nil {
		// If unmarshal fails, return raw string
		return string(resultResp.Data), nil
	}

	return result, nil
}
