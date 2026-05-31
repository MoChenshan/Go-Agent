package tool

import (
	jsonschema "github.com/eino-contrib/jsonschema"

	"github.com/cloudwego/eino/schema"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ConvertEinoParamsToTrpcSchema converts eino ParamsOneOf to trpc-agent-go schema.
func ConvertEinoParamsToTrpcSchema(paramsOneOf *schema.ParamsOneOf) *tool.Schema {
	if paramsOneOf == nil {
		return nil
	}

	// Try to convert to JSON schema first, but handle errors gracefully
	jsonSchema, err := paramsOneOf.ToJSONSchema()
	if err != nil || jsonSchema == nil {
		// Log the error for debugging purposes
		log.Warnf("Failed to convert Eino ParamsOneOf to JSON schema: %v, falling back to basic object schema", err)
		// Fallback: create a basic object schema
		return &tool.Schema{
			Type:        "object",
			Description: "Tool parameters",
		}
	}

	// Convert JSON schema to trpc-agent-go schema
	trpcSchema := &tool.Schema{
		Type:        "object", // Default to object for tool parameters
		Description: jsonSchema.Description,
		Required:    jsonSchema.Required,
	}

	// Convert properties if they exist
	convertProperties(jsonSchema, trpcSchema)

	return trpcSchema
}

// convertProperties converts JSON schema properties to trpc-agent-go schema properties.
func convertProperties(js *jsonschema.Schema, trpcSchema *tool.Schema) {
	if js.Properties != nil {
		trpcSchema.Properties = make(map[string]*tool.Schema)
		for pair := js.Properties.Oldest(); pair != nil; pair = pair.Next() {
			if pair.Value != nil {
				trpcSchema.Properties[pair.Key] = convertJSONSchemaToTrpcSchema(pair.Value)
			}
		}
	}
}

// convertJSONSchemaToTrpcSchema converts JSON schema to trpc-agent-go schema.
func convertJSONSchemaToTrpcSchema(js *jsonschema.Schema) *tool.Schema {
	if js == nil {
		return nil
	}

	schemaType := "string" // Default type
	if js.Type != "" {
		schemaType = js.Type
	}

	trpcSchema := &tool.Schema{
		Type:        schemaType,
		Description: js.Description,
	}

	// Convert properties if they exist
	convertProperties(js, trpcSchema)

	return trpcSchema
}
