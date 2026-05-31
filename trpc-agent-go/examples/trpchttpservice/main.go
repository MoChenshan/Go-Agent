//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package main provides a tRPC-Go HTTP service example for exposing Agent capabilities.
// It demonstrates how to integrate trpc-agent-go with tRPC ecosystem through simple HTTP endpoints.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/log"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// AgentRunRequest represents a simplified request aligned with runner.Run interface
type AgentRunRequest struct {
	Message   string `json:"message"`              // User input message (simplified from model.Message)
	UserID    string `json:"user_id"`              // Maps to runner.Run userID parameter
	SessionID string `json:"session_id,omitempty"` // Maps to runner.Run sessionID parameter (optional)
}

// AgentResponse represents the final response for non-streaming calls
type AgentResponse struct {
	Content   string `json:"content"`         // Final agent response content
	SessionID string `json:"session_id"`      // Session ID for continuation
	Done      bool   `json:"done"`            // Always true for non-streaming
	Usage     *Usage `json:"usage,omitempty"` // Token usage information
}

// StreamResponse represents individual streaming chunks
type StreamResponse struct {
	Type         string      `json:"type,omitempty"`          // Event type: "content", "tool_call", "tool_response"
	Content      string      `json:"content,omitempty"`       // Incremental content (streaming)
	ToolCallInfo *ToolCall   `json:"tool_call,omitempty"`     // Tool call information
	ToolResponse *ToolResult `json:"tool_response,omitempty"` // Tool execution result
	SessionID    string      `json:"session_id"`              // Session ID
	Done         bool        `json:"done"`                    // True for final chunk
	Usage        *Usage      `json:"usage,omitempty"`         // Token usage (final chunk only)
}

// ToolCall represents a tool call request
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolResult represents a tool execution result
type ToolResult struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// AgentService encapsulates the tRPC Agent HTTP service
type AgentService struct {
	runner runner.Runner
}

// NewAgentService creates a new AgentService instance
func NewAgentService() *AgentService {
	// Create agent with tools
	agent := createAgent()

	// Create runner with session service
	sessionService := inmemory.NewSessionService()
	agentRunner := runner.NewRunner("trpc-agent-service", agent,
		runner.WithSessionService(sessionService))

	return &AgentService{runner: agentRunner}
}

func main() {
	// Initialize tRPC server
	s := trpc.NewServer()

	// Create agent service
	service := NewAgentService()

	// Register HTTP endpoints using tRPC HTTP no-protocol service
	thttp.HandleFunc("/agent/run", service.handleRun)
	thttp.HandleFunc("/agent/stream", service.handleStream)

	// Register the NoProtocolService with the service name from trpc_go.yaml
	thttp.RegisterNoProtocolService(s.Service("trpc.app.server.agent"))

	log.Infof("🚀 tRPC Agent HTTP Service starting...")
	log.Infof("📝 Endpoints:")
	log.Infof("   POST /agent/run    - Non-streaming execution")
	log.Infof("   POST /agent/stream - Streaming execution")

	// Start serving
	if err := s.Serve(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// handleRun handles non-streaming agent execution
func (svc *AgentService) handleRun(w http.ResponseWriter, r *http.Request) error {
	// Parse request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return err
	}
	defer r.Body.Close()

	var req AgentRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return err
	}

	// Validate request
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return fmt.Errorf("message is required")
	}
	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return fmt.Errorf("user_id is required")
	}

	// Generate session ID if not provided
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	log.Infof("Processing non-streaming request: user=%s, session=%s, message=%s",
		req.UserID, req.SessionID, req.Message)

	// Convert to model.Message (aligning with runner.Run interface)
	message := model.NewUserMessage(req.Message)

	// Execute agent via runner.Run
	eventChan, err := svc.runner.Run(r.Context(), req.UserID, req.SessionID, message)
	if err != nil {
		log.Errorf("Agent execution failed: %v", err)
		http.Error(w, fmt.Sprintf("Agent execution failed: %v", err), http.StatusInternalServerError)
		return err
	}

	// Collect all events and build final response
	// For LLM streaming mode, accumulate Delta.Content (incremental chunks)
	var finalContent strings.Builder
	var usage *Usage

	for event := range eventChan {
		if event.Error != nil {
			log.Errorf("Agent error: %s", event.Error.Message)
			http.Error(w, fmt.Sprintf("Agent error: %s", event.Error.Message), http.StatusInternalServerError)
			return fmt.Errorf("agent error: %s", event.Error.Message)
		}

		// Extract incremental content from Delta (not Message to avoid duplication)
		content := extractContentFromEventNonStreaming(event)
		if content != "" {
			finalContent.WriteString(content)
		}

		// Extract usage information from final event
		if event.Usage != nil {
			usage = &Usage{
				PromptTokens:     event.Usage.PromptTokens,
				CompletionTokens: event.Usage.CompletionTokens,
				TotalTokens:      event.Usage.TotalTokens,
			}
		}
	}

	// Build response
	response := AgentResponse{
		Content:   finalContent.String(),
		SessionID: req.SessionID,
		Done:      true,
		Usage:     usage,
	}

	// Send JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Errorf("Failed to encode response: %v", err)
		return err
	}

	log.Infof("Completed non-streaming request: user=%s, session=%s, tokens=%v",
		req.UserID, req.SessionID, usage)
	return nil
}

// handleStream handles streaming agent execution using SSE
func (svc *AgentService) handleStream(w http.ResponseWriter, r *http.Request) error {
	// Parse request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return err
	}
	defer r.Body.Close()

	var req AgentRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return err
	}

	// Validate request
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return fmt.Errorf("message is required")
	}
	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return fmt.Errorf("user_id is required")
	}

	// Generate session ID if not provided
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	log.Infof("Processing streaming request: user=%s, session=%s, message=%s",
		req.UserID, req.SessionID, req.Message)

	// Setup SSE headers
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return fmt.Errorf("streaming unsupported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Convert to model.Message (aligning with runner.Run interface)
	message := model.NewUserMessage(req.Message)

	// Execute agent via runner.Run
	eventChan, err := svc.runner.Run(r.Context(), req.UserID, req.SessionID, message)
	if err != nil {
		log.Errorf("Agent execution failed: %v", err)
		// Send error as SSE event
		errorResponse := StreamResponse{
			Content:   fmt.Sprintf("Error: %v", err),
			SessionID: req.SessionID,
			Done:      true,
		}
		data, _ := json.Marshal(errorResponse)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return err
	}

	// Stream events as they arrive
	for event := range eventChan {
		if event.Error != nil {
			log.Errorf("Agent error: %s", event.Error.Message)
			// Send error event
			errorResponse := StreamResponse{
				Type:      "error",
				Content:   fmt.Sprintf("Error: %s", event.Error.Message),
				SessionID: req.SessionID,
				Done:      true,
			}
			data, _ := json.Marshal(errorResponse)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return fmt.Errorf("agent error: %s", event.Error.Message)
		}

		// Handle tool calls
		if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
			for _, toolCall := range event.Choices[0].Message.ToolCalls {
				streamResp := StreamResponse{
					Type: "tool_call",
					ToolCallInfo: &ToolCall{
						ID:        toolCall.ID,
						Name:      toolCall.Function.Name,
						Arguments: string(toolCall.Function.Arguments),
					},
					SessionID: req.SessionID,
					Done:      false,
				}
				data, _ := json.Marshal(streamResp)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
			continue
		}

		// Handle tool responses
		if event.Response != nil && len(event.Response.Choices) > 0 {
			hasToolResponse := false
			for _, choice := range event.Response.Choices {
				if choice.Message.Role == model.RoleTool && choice.Message.ToolID != "" {
					streamResp := StreamResponse{
						Type: "tool_response",
						ToolResponse: &ToolResult{
							ID:      choice.Message.ToolID,
							Content: strings.TrimSpace(choice.Message.Content),
						},
						SessionID: req.SessionID,
						Done:      false,
					}
					data, _ := json.Marshal(streamResp)
					fmt.Fprintf(w, "data: %s\n\n", data)
					flusher.Flush()
					hasToolResponse = true
				}
			}
			if hasToolResponse {
				continue
			}
		}

		// Extract content from event
		content := extractContentFromEvent(event)
		if content == "" && event.Usage == nil {
			continue // Skip empty events
		}

		// Build streaming response
		streamResp := StreamResponse{
			Type:      "content",
			Content:   content,
			SessionID: req.SessionID,
			Done:      event.Done,
		}

		// Add usage information for final event
		if event.Usage != nil {
			streamResp.Usage = &Usage{
				PromptTokens:     event.Usage.PromptTokens,
				CompletionTokens: event.Usage.CompletionTokens,
				TotalTokens:      event.Usage.TotalTokens,
			}
		}

		// Send SSE event
		data, err := json.Marshal(streamResp)
		if err != nil {
			log.Errorf("Failed to marshal SSE event: %v", err)
			continue // Skip malformed events
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		// Exit after final event
		if event.IsRunnerCompletion() {
			log.Infof("Completed streaming request: user=%s, session=%s, usage=%v",
				req.UserID, req.SessionID, streamResp.Usage)
			break
		}
	}

	return nil
}

// extractContentFromEvent extracts text content for SSE streaming endpoint
// Uses Delta.Content (incremental chunks) for LLM streaming mode
func extractContentFromEvent(event *event.Event) string {
	if len(event.Choices) == 0 {
		return ""
	}

	choice := event.Choices[0]

	// For SSE streaming: always use Delta content (incremental chunks)
	if choice.Delta.Content != "" {
		return choice.Delta.Content
	}

	return ""
}

// extractContentFromEventNonStreaming extracts text content for non-streaming HTTP endpoint
// Uses Delta.Content (incremental chunks) when LLM is in streaming mode
func extractContentFromEventNonStreaming(event *event.Event) string {
	if len(event.Choices) == 0 {
		return ""
	}

	choice := event.Choices[0]

	// For non-streaming HTTP with LLM streaming mode:
	// Use Delta.Content (incremental) to avoid duplication
	// Message.Content is cumulative and will cause content duplication if accumulated
	if choice.Delta.Content != "" {
		return choice.Delta.Content
	}

	return ""
}

// createAgent creates a simple agent with calculator tool
func createAgent() *llmagent.LLMAgent {
	// Get model name from environment, default to deepseek-v3-0324
	modelName := getEnv("MODEL_NAME", "deepseek-v3-0324")

	// Create model (OpenAI client will auto-read OPENAI_API_KEY and OPENAI_BASE_URL)
	modelInstance := openai.New(modelName)

	// Create calculator tool
	calculatorTool := function.NewFunctionTool(
		calculate,
		function.WithName("calculator"),
		function.WithDescription("Perform basic mathematical calculations"),
	)

	// Create agent
	return llmagent.New(
		"trpc-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("A helpful assistant with calculator tool"),
		llmagent.WithInstruction("Be helpful and use the calculator tool when appropriate"),
		llmagent.WithGenerationConfig(model.GenerationConfig{
			MaxTokens:   intPtr(2000),
			Temperature: floatPtr(0.7),
			Stream:      true,
		}),
		llmagent.WithTools([]tool.Tool{calculatorTool}),
	)
}

// Tool implementations

type calculatorArgs struct {
	Operation string  `json:"operation" description:"add, subtract, multiply, divide"`
	A         float64 `json:"a" description:"First number"`
	B         float64 `json:"b" description:"Second number"`
}

type calculatorResult struct {
	Result float64 `json:"result"`
}

func calculate(ctx context.Context, args calculatorArgs) (calculatorResult, error) {
	var result float64
	switch strings.ToLower(args.Operation) {
	case "add", "+":
		result = args.A + args.B
	case "subtract", "-":
		result = args.A - args.B
	case "multiply", "*":
		result = args.A * args.B
	case "divide", "/":
		if args.B != 0 {
			result = args.A / args.B
		}
	}

	log.Infof("Calculator: %f %s %f = %f", args.A, args.Operation, args.B, result)
	return calculatorResult{Result: result}, nil
}

// Helper functions
func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}

func getEnv(key, defaultValue string) string {
	// Try to get from environment variables
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
