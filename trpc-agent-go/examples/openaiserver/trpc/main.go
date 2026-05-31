// Package main demonstrates how to use trpc-agent-go to create an OpenAI server.
package main

import (
	"flag"
	"log"

	"git.code.oa.com/trpc-go/trpc-go"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	topenai "git.woa.com/trpc-go/trpc-agent-go/trpc/server/openai" // 1. 导入内网 OpenAI 依赖
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	openaiserver "trpc.group/trpc-go/trpc-agent-go/server/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

var (
	modelName = flag.String("model", "deepseek-chat", "Model to use")
	isStream  = flag.Bool("stream", true, "Whether to stream the response")
)

func main() {
	flag.Parse()
	agent := newAgent(*modelName)

	// 2. 创建 OpenAI Server
	openAIServer, err := openaiserver.New(
		openaiserver.WithAgent(agent),
		openaiserver.WithBasePath("/v1"),
		openaiserver.WithModelName(*modelName),
	)
	if err != nil {
		log.Fatalf("failed to create OpenAI server: %v", err)
	}
	defer openAIServer.Close()

	// 3. 加载配置文件，创建 trpc 服务
	trpcServer := trpc.NewServer()

	// 4. 将 OpenAI Server 注册到 trpc service
	if err := topenai.RegisterOpenAIServer(trpcServer, "trpc.test.openai.server", openAIServer); err != nil {
		log.Fatalf("failed to register OpenAI server: %v", err)
	}

	// 5. 启动 trpc 服务
	if err := trpcServer.Serve(); err != nil {
		log.Fatalf("server stopped with error: %v", err)
	}
}

// newAgent creates and configures the LLM agent with tools.
func newAgent(modelName string) *llmagent.LLMAgent {
	// Create the OpenAI model instance for LLM interactions.
	modelInstance := openai.New(modelName)

	// Create calculator tool for mathematical operations.
	calculatorTool := function.NewFunctionTool(
		calculate,
		function.WithName("calculator"),
		function.WithDescription(
			"Perform basic mathematical calculations "+
				"(add, subtract, multiply, divide)",
		),
	)

	// Create time tool for timezone queries.
	timeTool := function.NewFunctionTool(
		getCurrentTime,
		function.WithName("current_time"),
		function.WithDescription(
			"Get the current time and date for a specific timezone",
		),
	)

	// Configure generation parameters for the LLM.
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(2000),
		Temperature: floatPtr(0.7),
		Stream:      *isStream,
	}

	// Create the LLM agent with tools and configuration.
	const agentName = "assistant"
	llmAgent := llmagent.New(
		agentName,
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription(
			"A helpful AI assistant with calculator and time tools",
		),
		llmagent.WithInstruction(
			"Use tools when appropriate for calculations or time queries. "+
				"Be helpful and conversational.",
		),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools([]tool.Tool{calculatorTool, timeTool}),
	)
	return llmAgent
}

// intPtr returns a pointer to the given int value.
func intPtr(i int) *int {
	return &i
}

// floatPtr returns a pointer to the given float64 value.
func floatPtr(f float64) *float64 {
	return &f
}
