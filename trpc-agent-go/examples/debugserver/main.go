// Package main provides a standalone CLI demo showcasing how to wire the
// trpc-agent-go orchestration layer with an LLM agent that exposes two
// simple tools: a calculator and a time query. It starts an HTTP server
// compatible with ADK Web UI for manual testing.
package main

import (
	"fmt"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	// Import the trpc-go package to create a new tRPC server.
	"git.code.oa.com/trpc-go/trpc-go"
	// Import the trpc-go/http package to register the server handler for HTTP.
	thttp "git.code.oa.com/trpc-go/trpc-go/http"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug"
)

func main() {
	// --- Model and tools setup ---
	modelName := "deepseek-chat"
	modelInstance := openai.New(modelName)

	calculatorTool := function.NewFunctionTool(
		calculate,
		function.WithName("calculator"),
		function.WithDescription("Perform basic mathematical calculations (add, subtract, multiply, divide)"),
	)
	timeTool := function.NewFunctionTool(
		getCurrentTime,
		function.WithName("current_time"),
		function.WithDescription("Get the current time and date for a specific timezone"),
	)

	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(2000),
		Temperature: floatPtr(0.7),
		Stream:      true,
	}

	agentName := "assistant"
	llmAgent := llmagent.New(
		agentName,
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("A helpful AI assistant with calculator and time tools"),
		llmagent.WithInstruction("Use tools when appropriate for calculations or time queries. Be helpful and conversational."),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithChannelBufferSize(100),
		llmagent.WithTools([]tool.Tool{calculatorTool, timeTool}),
	)

	agents := map[string]agent.Agent{
		agentName: llmAgent,
	}

	s := trpc.NewServer()
	server := debug.New(agents)
	// Register the entire mux handler directly without additional wrapping.
	thttp.RegisterNoProtocolServiceMux(s.Service("trpc.test.debug.stdhttp"), server.Handler())

	log.Infof("Debug server listening on %s (apps: %v)", getIPPort(), agents)
	if err := s.Serve(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getIPPort() string {
	config := trpc.GlobalConfig()
	if config.Server.Service == nil {
		return ""
	}
	service := config.Server.Service[0]
	if service.Port == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", service.IP, service.Port)
}
