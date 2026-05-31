package converters

import (
	"fmt"

	"github.com/cloudwego/eino/schema"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ConvertToEinoMessage converts a trpc-agent-go message to eino schema message.
func ConvertToEinoMessage(msg model.Message) *schema.Message {
	einoMsg := &schema.Message{
		Content: msg.Content,
	}

	// Convert role
	switch msg.Role {
	case model.RoleUser:
		einoMsg.Role = schema.User
	case model.RoleAssistant:
		einoMsg.Role = schema.Assistant
	case model.RoleSystem:
		einoMsg.Role = schema.System
	case model.RoleTool:
		einoMsg.Role = schema.Tool
	default:
		einoMsg.Role = schema.User // default fallback
	}

	// Convert tool calls if present
	if len(msg.ToolCalls) > 0 {
		einoMsg.ToolCalls = make([]schema.ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			einoMsg.ToolCalls[i] = schema.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: schema.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: string(tc.Function.Arguments),
				},
			}
		}
	}

	return einoMsg
}

// ConvertFromEinoMessage converts an eino schema message to trpc-agent-go message.
func ConvertFromEinoMessage(einoMsg *schema.Message) model.Message {
	msg := model.Message{
		Content: einoMsg.Content,
	}

	// Convert role using unified conversion function
	msg.Role = ConvertEinoRoleToModelRole(einoMsg.Role)

	// Convert tool calls using unified conversion function
	msg.ToolCalls = ConvertFromEinoToolCalls(einoMsg.ToolCalls)

	return msg
}

// BuildEinoInput builds the input map required by eino Chain from trpc-agent-go invocation.
// This handles the common pattern where eino Chains expect map[string]any input.
func BuildEinoInput(msg model.Message) map[string]any {
	// Convert trpc-agent-go message to eino message for comprehensive input
	einoMsg := ConvertToEinoMessage(msg)

	return map[string]any{
		"query":   msg.Content,
		"message": einoMsg,
		"role":    msg.Role,
		// Add more fields as needed based on your Chain's input requirements
	}
}

// ConvertFromEinoToolCalls converts eino tool calls to trpc-agent-go tool calls.
func ConvertFromEinoToolCalls(einoToolCalls []schema.ToolCall) []model.ToolCall {
	if len(einoToolCalls) == 0 {
		return nil
	}

	toolCalls := make([]model.ToolCall, len(einoToolCalls))
	for i, tc := range einoToolCalls {
		toolCalls[i] = model.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: model.FunctionDefinitionParam{
				Name:      tc.Function.Name,
				Arguments: []byte(tc.Function.Arguments),
			},
		}
	}

	return toolCalls
}

// FormatStructuredData formats structured data for display.
func FormatStructuredData(data map[string]any) string {
	// Simple formatting for now - could be enhanced with JSON pretty printing
	result := "{"
	first := true
	for k, v := range data {
		if !first {
			result += ", "
		}
		result += k + ": " + SafeStringify(v)
		first = false
	}
	result += "}"
	return result
}

// SafeStringify safely converts any value to string.
func SafeStringify(v any) string {
	if v == nil {
		return ""
	}

	switch val := v.(type) {
	case string:
		return val
	case *schema.Message:
		return val.Content
	default:
		return fmt.Sprintf("%v", v)
	}
}

// CreateFinalMessage creates a final message for completion events.
func CreateFinalMessage(content string) model.Message {
	return model.Message{
		Role:    model.RoleAssistant,
		Content: content,
	}
}

// CreateAssistantEinoMessage creates an eino Assistant message with the given content.
// This eliminates the repeated pattern of creating assistant messages in stream processing.
func CreateAssistantEinoMessage(content string) *schema.Message {
	return &schema.Message{
		Role:    schema.Assistant,
		Content: content,
	}
}

// ConvertEinoRoleToModelRole converts an eino role directly to model.Role type.
// This avoids the string conversion overhead in streaming scenarios.
func ConvertEinoRoleToModelRole(role schema.RoleType) model.Role {
	switch role {
	case schema.User:
		return model.RoleUser
	case schema.Assistant:
		return model.RoleAssistant
	case schema.System:
		return model.RoleSystem
	case schema.Tool:
		return model.RoleTool
	default:
		return model.RoleUser // default fallback
	}
}

// ConvertChunkToEinoMessage converts various chunk types to eino Message.
// This centralizes the chunk conversion logic used in streaming scenarios.
func ConvertChunkToEinoMessage(chunk any) *schema.Message {
	switch v := chunk.(type) {
	case *schema.Message:
		// Direct message chunk
		return v
	case string:
		// String content chunk
		return CreateAssistantEinoMessage(v)
	case map[string]any:
		// Structured data chunk - extract content if possible
		if content, ok := v["content"].(string); ok {
			return CreateAssistantEinoMessage(content)
		}
		// Fallback: convert to string representation
		return CreateAssistantEinoMessage(FormatStructuredData(v))
	default:
		// Unknown type - convert to string
		return CreateAssistantEinoMessage(SafeStringify(v))
	}
}
