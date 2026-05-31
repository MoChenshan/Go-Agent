//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package main provides a tRPC-Go client example for directly calling LLMModel.
// It demonstrates how to use trpc-agent-go with tRPC ecosystem to make direct
// LLM model calls using the model interface.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/log"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/model/hunyuan"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	toolNameCalc = "calculator"
	toolDescCalc = "Perform basic arithmetic on two numbers. " +
		"Supported operations: add, subtract, multiply, divide."

	opAdd      = "add"
	opSubtract = "subtract"
	opMultiply = "multiply"
	opDivide   = "divide"
)

var flagModel = flag.String(
	"model", "deepseek-chat", "Name of the model to use",
)

func main() {
	flag.Parse()

	// Load and setup tRPC client configuration (no server needed).
	if err := trpc.LoadGlobalConfig("trpc_go.yaml"); err != nil {
		log.Errorf("Failed to load trpc config: %v", err)
		return
	}
	if err := trpc.SetupClients(&trpc.GlobalConfig().Client); err != nil {
		log.Errorf("Failed to setup trpc clients: %v", err)
		return
	}

	// Create LLM model instance.
	llmModel := newModel()

	log.Infof("🚀 tRPC LLM Client starting...")
	log.Infof("🤖 Model: %s", llmModel.Info().Name)
	log.Infof("🔄 Streaming mode: enabled")
	log.Infof("💬 Enter your message (or 'quit' to exit):")

	// Create scanner for user input.
	scanner := bufio.NewScanner(os.Stdin)

	// Interactive loop for user input.
	for {
		fmt.Print("\n> ")

		// Read user input.
		if !scanner.Scan() {
			break
		}
		message := strings.TrimSpace(scanner.Text())

		// Check for quit command.
		if strings.ToLower(message) == "quit" {
			log.Info("👋 Goodbye!")
			break
		}

		if message == "" {
			continue
		}

		// Call LLM model directly (timeout controlled by trpc_go.yaml).
		if err := callLLM(context.Background(), llmModel, message); err != nil {
			log.Errorf("❌ LLM call failed: %v", err)
		}
	}
}

func newModel() model.Model {
	// newModel demonstrates different ways to create LLM model instances.
	//
	// This example shows three approaches:
	//
	// 1. Internal Taiji platform (recommended for DeepSeek models):
	//    - Automatic tRPC HTTP client configuration
	//    - Polaris service discovery, monitoring, interceptors
	//    - Platform-specific features like openai_infer, tool_choice, thinking
	_ = taiji.NewOpenAI("DeepSeek-V3_1-Online-32k",
		taiji.WithHTTPClientName("trpc.test.llm.openai"),
		taiji.WithOpenAIInfer(true), // Required for most models
		taiji.WithToolChoice(),      // Enable tool calling
		taiji.WithThinking(true),    // Enable thinking for DeepSeek V3.1/V3.2
	)
	// taijiModel is ready to use with full tRPC ecosystem support

	// 2. Internal Hunyuan platform (recommended for Hunyuan models):
	//    - Same tRPC HTTP client benefits
	//    - Platform-specific thinking mode support
	_ = hunyuan.NewOpenAI("hunyuan-a13b",
		hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
		hunyuan.WithThinking(), // Enable thinking for supported models
	)
	// hunyuanModel is ready to use with full tRPC ecosystem support

	// 3. Using external GitHub open-source OpenAI package directly:
	//   - Manual configuration required
	//   - No tRPC ecosystem integration
	//   - Suitable for external services
	return openai.New(*flagModel,
		openai.WithHTTPClientOptions(
			openai.WithHTTPClientName("trpc.test.llm.openai"),
		),
	)
}

// calcInput is the input schema for the calculator tool.
type calcInput struct {
	Operation string  `json:"operation" jsonschema:"description=Arithmetic operation to perform,enum=add,enum=subtract,enum=multiply,enum=divide"`
	A         float64 `json:"a" description:"First operand."`
	B         float64 `json:"b" description:"Second operand."`
}

// calcOutput is the output schema for the calculator tool.
type calcOutput struct {
	Result float64 `json:"result" description:"Calculation result."`
}

// calculate performs basic arithmetic on two numbers.
func calculate(_ context.Context, in calcInput) (calcOutput, error) {
	var result float64
	switch in.Operation {
	case opAdd:
		result = in.A + in.B
	case opSubtract:
		result = in.A - in.B
	case opMultiply:
		result = in.A * in.B
	case opDivide:
		if in.B == 0 {
			return calcOutput{}, fmt.Errorf("division by zero")
		}
		result = in.A / in.B
	default:
		return calcOutput{}, fmt.Errorf(
			"unknown operation: %s", in.Operation,
		)
	}
	return calcOutput{Result: result}, nil
}

// buildTools creates a calculator tool for the model request.
func buildTools() map[string]tool.Tool {
	calc := function.NewFunctionTool(
		calculate,
		function.WithName(toolNameCalc),
		function.WithDescription(toolDescCalc),
	)
	return map[string]tool.Tool{
		toolNameCalc: calc,
	}
}

const (
	// maxToolRounds limits the tool-call loop to prevent infinite cycles.
	maxToolRounds = 10
)

// callLLM makes a direct call to the LLM model with
// tool-call loop.
func callLLM(ctx context.Context, llmModel model.Model, message string) error {
	// Parse tools from an OpenAI-format JSON array instead
	// of building them programmatically. This demonstrates
	// how to consume tools defined in an upstream request.
	declTools, err := buildToolsFromJSON(sampleToolsJSON)
	if err != nil {
		return fmt.Errorf("build tools from JSON: %w", err)
	}

	// Local callable tools for execution. When the model
	// returns a tool_call, we look up the callable version
	// here. The declTools above are declaration-only and
	// sent to the model for schema awareness.
	callableTools := buildTools()

	temperature := 0.7
	maxTokens := 2000
	config := model.GenerationConfig{
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
		Stream:      true,
	}

	// Build initial messages.
	messages := []model.Message{
		model.NewSystemMessage("You are a helpful assistant."),
		model.NewUserMessage(message),
	}

	log.Infof("🤖 Calling LLM model: %s", llmModel.Info().Name)
	log.Infof("📝 Message: %s", message)

	// Tool-call loop: keep calling the model until it produces
	// a final text response (no more tool calls).
	for range maxToolRounds {
		req := &model.Request{
			Messages:         messages,
			Tools:            declTools,
			GenerationConfig: config,
		}

		respChan, err := llmModel.GenerateContent(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to call LLM model: %w", err)
		}

		// Collect the full assistant response from stream.
		assistantMsg, err := collectStreamResponse(respChan)
		if err != nil {
			return err
		}

		// Append assistant message to history.
		messages = append(messages, assistantMsg)

		// If no tool calls, we are done.
		if len(assistantMsg.ToolCalls) == 0 {
			break
		}

		// Execute each tool call using callable tools.
		for _, tc := range assistantMsg.ToolCalls {
			result, execErr := executeTool(
				ctx, callableTools, tc,
			)
			// Build tool result message.
			toolMsg := model.Message{
				Role:    model.RoleTool,
				Content: result,
				ToolID:  tc.ID,
			}
			if execErr != nil {
				log.Errorf(
					"❌ Tool %s error: %v",
					tc.Function.Name, execErr,
				)
			}
			messages = append(messages, toolMsg)
		}
		fmt.Println()
	}
	return nil
}

// collectStreamResponse reads all streaming chunks and returns
// the assembled assistant message (with content and tool calls).
func collectStreamResponse(
	respChan <-chan *model.Response,
) (model.Message, error) {
	var (
		content   strings.Builder
		toolCalls []model.ToolCall
		tokens    int
		started   bool
	)

	for resp := range respChan {
		if resp.Error != nil {
			return model.Message{}, fmt.Errorf(
				"LLM error: %s", resp.Error.Message,
			)
		}
		if resp.Usage != nil {
			tokens = resp.Usage.TotalTokens
		}
		if len(resp.Choices) == 0 {
			continue
		}
		choice := resp.Choices[0]

		// Accumulate text delta.
		if choice.Delta.Content != "" {
			if !started {
				fmt.Print("\n🤖 ")
				started = true
			}
			fmt.Print(choice.Delta.Content)
			content.WriteString(choice.Delta.Content)
		}

		// Accumulate tool calls from delta.
		toolCalls = mergeToolCallDeltas(
			toolCalls, choice.Delta.ToolCalls,
		)

		// Also check the final message for tool calls
		// (non-streaming providers).
		if len(choice.Message.ToolCalls) > 0 &&
			len(toolCalls) == 0 {
			toolCalls = choice.Message.ToolCalls
		}
		if choice.Message.Content != "" &&
			content.Len() == 0 {
			content.WriteString(choice.Message.Content)
		}
	}

	if started {
		fmt.Println()
	}
	if tokens > 0 {
		fmt.Printf("📊 Tokens: %d\n", tokens)
	}

	// Print tool calls if any.
	if len(toolCalls) > 0 {
		fmt.Printf("🔧 Tool calls: %d\n", len(toolCalls))
		for _, tc := range toolCalls {
			fmt.Printf(
				"   • %s(%s)\n",
				tc.Function.Name,
				string(tc.Function.Arguments),
			)
		}
	}

	msg := model.Message{
		Role:      model.RoleAssistant,
		Content:   content.String(),
		ToolCalls: toolCalls,
	}
	return msg, nil
}

// mergeToolCallDeltas merges streaming tool_call deltas into
// the accumulated slice. Each delta chunk carries an index;
// new indices create new entries, existing indices append to
// the Arguments buffer.
func mergeToolCallDeltas(
	acc []model.ToolCall,
	deltas []model.ToolCall,
) []model.ToolCall {
	for _, d := range deltas {
		// Find or create slot by matching ID or by position.
		found := false
		for i := range acc {
			if acc[i].ID == d.ID && d.ID != "" {
				acc[i].Function.Arguments = append(
					acc[i].Function.Arguments,
					d.Function.Arguments...,
				)
				found = true
				break
			}
		}
		if !found {
			acc = append(acc, model.ToolCall{
				ID:   d.ID,
				Type: d.Type,
				Function: model.FunctionDefinitionParam{
					Name:      d.Function.Name,
					Arguments: d.Function.Arguments,
				},
			})
		}
	}
	return acc
}

// executeTool looks up and calls the named tool.
func executeTool(
	ctx context.Context,
	tools map[string]tool.Tool,
	tc model.ToolCall,
) (string, error) {
	t, ok := tools[tc.Function.Name]
	if !ok {
		return fmt.Sprintf(
			"error: unknown tool %q", tc.Function.Name,
		), fmt.Errorf("unknown tool: %s", tc.Function.Name)
	}
	callable, ok := t.(tool.CallableTool)
	if !ok {
		return fmt.Sprintf(
			"error: tool %q is not callable", tc.Function.Name,
		), fmt.Errorf("tool %s is not callable", tc.Function.Name)
	}

	fmt.Printf(
		"⚡ Executing %s ...\n", tc.Function.Name,
	)
	result, err := callable.Call(ctx, tc.Function.Arguments)
	if err != nil {
		errMsg := fmt.Sprintf("tool error: %v", err)
		return errMsg, err
	}

	// Serialize result to JSON string for the model.
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprint(result), nil
	}
	fmt.Printf("✅ Result: %s\n", string(resultJSON))
	return string(resultJSON), nil
}
