package main

import (
	"encoding/json"
	"fmt"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// openAIToolJSON represents the OpenAI-format tool definition
// in a JSON request body, e.g.:
//
//	{"type":"function","function":{"name":"...","description":"...","parameters":{...}}}
type openAIToolJSON struct {
	Type     string             `json:"type"`
	Function openAIFunctionJSON `json:"function"`
}

// openAIFunctionJSON is the "function" object inside an
// OpenAI-format tool definition.
type openAIFunctionJSON struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// declTool is a declaration-only tool.Tool implementation.
// It wraps a *tool.Declaration without any execution logic,
// suitable for tools whose execution is handled externally.
type declTool struct {
	decl *tool.Declaration
}

// Declaration returns the tool metadata.
func (d *declTool) Declaration() *tool.Declaration {
	return d.decl
}

// buildToolsFromJSON parses an OpenAI-format JSON tools array
// (e.g. from an upstream request body) and converts it into
// map[string]tool.Tool that can be assigned to
// model.Request.Tools.
//
// The input should be the raw JSON array value of the "tools"
// field, for example:
//
//	[{"type":"function","function":{"name":"calculator",...}}]
func buildToolsFromJSON(
	toolsJSON json.RawMessage,
) (map[string]tool.Tool, error) {
	var defs []openAIToolJSON
	if err := json.Unmarshal(toolsJSON, &defs); err != nil {
		return nil, fmt.Errorf("unmarshal tools JSON: %w", err)
	}
	result := make(map[string]tool.Tool, len(defs))
	for _, def := range defs {
		// Parse the "parameters" object directly into
		// *tool.Schema. The JSON tag names are compatible
		// (type, properties, required, items, enum, etc.).
		var schema tool.Schema
		if len(def.Function.Parameters) > 0 {
			if err := json.Unmarshal(
				def.Function.Parameters, &schema,
			); err != nil {
				return nil, fmt.Errorf(
					"unmarshal parameters for tool %q: %w",
					def.Function.Name, err,
				)
			}
		}
		result[def.Function.Name] = &declTool{
			decl: &tool.Declaration{
				Name:        def.Function.Name,
				Description: def.Function.Description,
				InputSchema: &schema,
			},
		}
	}
	return result, nil
}

// sampleToolsJSON is a sample OpenAI-format tools JSON array
// for demonstration. In practice this would come from an
// upstream HTTP request body or configuration.
//
//nolint:lll
var sampleToolsJSON = json.RawMessage(`[
  {
    "type": "function",
    "function": {
      "name": "calculator",
      "description": "Perform basic arithmetic on two numbers. Supported operations: add, subtract, multiply, divide.",
      "parameters": {
        "type": "object",
        "properties": {
          "operation": {
            "type": "string",
            "description": "Arithmetic operation to perform.",
            "enum": ["add", "subtract", "multiply", "divide"]
          },
          "a": {"type": "number", "description": "First operand."},
          "b": {"type": "number", "description": "Second operand."}
        },
        "required": ["operation", "a", "b"]
      }
    }
  }
]`)
