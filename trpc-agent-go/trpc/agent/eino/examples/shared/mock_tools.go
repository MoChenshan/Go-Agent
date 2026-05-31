package shared

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// CalculatorRequest represents the input for calculator.
type CalculatorRequest struct {
	Operation string  `json:"operation" jsonschema:"description=The operation to perform (add subtract multiply divide)"`
	A         float64 `json:"a" jsonschema:"description=First number"`
	B         float64 `json:"b" jsonschema:"description=Second number"`
}

// CalculatorResult represents the output from calculator.
type CalculatorResult struct {
	Result float64 `json:"result" jsonschema:"description=The calculation result"`
}

// NewCalculatorTool creates a simple mock tool for calculations.
func NewCalculatorTool() tool.Tool {
	return function.NewFunctionTool(calculateNumbers,
		function.WithName("calculator"),
		function.WithDescription("Performs basic arithmetic operations"),
	)
}

func calculateNumbers(ctx context.Context, req CalculatorRequest) (CalculatorResult, error) {
	switch req.Operation {
	case "add":
		return CalculatorResult{Result: req.A + req.B}, nil
	case "subtract":
		return CalculatorResult{Result: req.A - req.B}, nil
	case "multiply":
		return CalculatorResult{Result: req.A * req.B}, nil
	case "divide":
		if req.B == 0 {
			return CalculatorResult{}, fmt.Errorf("division by zero")
		}
		return CalculatorResult{Result: req.A / req.B}, nil
	default:
		return CalculatorResult{}, fmt.Errorf("unsupported operation: %s", req.Operation)
	}
}

// WeatherRequest represents input for weather tool.
type WeatherRequest struct {
	City string `json:"city" jsonschema:"description=The city to get weather for"`
}

// WeatherResult represents output from weather tool.
type WeatherResult struct {
	Weather string `json:"weather" jsonschema:"description=Weather information"`
}

// NewWeatherTool creates a simple mock weather tool.
func NewWeatherTool() tool.Tool {
	return function.NewFunctionTool(getWeather,
		function.WithName("weather"),
		function.WithDescription("Gets weather information for a city"),
	)
}

func getWeather(ctx context.Context, req WeatherRequest) (WeatherResult, error) {
	weather := fmt.Sprintf("Weather in %s: Sunny, 25°C", req.City)
	return WeatherResult{Weather: weather}, nil
}

// EinoCalculatorTool is an Eino tool for demonstration.
type EinoCalculatorTool struct{}

// NewEinoCalculatorTool creates a new EinoCalculatorTool instance.
func NewEinoCalculatorTool() *EinoCalculatorTool {
	return &EinoCalculatorTool{}
}

// Info returns the tool information for EinoCalculatorTool.
func (t *EinoCalculatorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "eino_calculator",
		Desc: "Eino calculator tool for basic arithmetic",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"operation": {
				Type:     schema.String,
				Desc:     "The operation to perform (add, subtract, multiply, divide)",
				Required: true,
			},
			"a": {
				Type:     schema.Number,
				Desc:     "First number",
				Required: true,
			},
			"b": {
				Type:     schema.Number,
				Desc:     "Second number",
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun executes the calculator operation with given parameters.
func (t *EinoCalculatorTool) InvokableRun(ctx context.Context, params map[string]any, _ ...einotool.Option) (string, error) {
	operation, _ := params["operation"].(string)
	a, _ := params["a"].(float64)
	b, _ := params["b"].(float64)

	var result float64

	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return "", fmt.Errorf("division by zero")
		}
		result = a / b
	default:
		return "", fmt.Errorf("unsupported operation: %s", operation)
	}

	return fmt.Sprintf("Result: %.2f (operation: %s, inputs: a=%.2f, b=%.2f)", result, operation, a, b), nil
}

// EinoWeatherTool is an Eino weather tool.
type EinoWeatherTool struct{}

// NewEinoWeatherTool creates a new EinoWeatherTool instance.
func NewEinoWeatherTool() *EinoWeatherTool {
	return &EinoWeatherTool{}
}

// Info returns the tool information for EinoWeatherTool.
func (t *EinoWeatherTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "eino_weather",
		Desc: "Gets weather information using Eino tool interface",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"city": {
				Type:     schema.String,
				Desc:     "The city to get weather for",
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun executes the weather query with given parameters.
func (t *EinoWeatherTool) InvokableRun(ctx context.Context, params map[string]any, _ ...einotool.Option) (string, error) {
	city, _ := params["city"].(string)
	weather := fmt.Sprintf("Weather in %s: Sunny, 25°C (from Eino tool)", city)
	return weather, nil
}
