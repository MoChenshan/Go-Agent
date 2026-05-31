//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package main

import (
	"context"
	"flag"
	"fmt"
	"math"

	"git.code.oa.com/trpc-go/trpc-go"
	tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"          // 1.1. 导入内网 agui
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo" // 1.2. 匿名导入开启 galileo 上报
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/adapter"
	aguirunner "trpc.group/trpc-go/trpc-agent-go/server/agui/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	agentName = "agui-agent"
)

var (
	modelName = flag.String("model", "deepseek-chat", "Model to use")
	isStream  = flag.Bool("stream", true, "Whether to stream the response")
)

func main() {
	flag.Parse()
	// 2. 构建 agent 和 runner
	agent := newAgent()
	runner := runner.NewRunner(agent.Info().Name, agent)
	// 3. 创建 trpc 服务
	server := trpc.NewServer()
	// 4. 创建 AG-UI server
	aguiServer, err := agui.New(runner,
		agui.WithPath("/agui"),
		agui.WithAGUIRunnerOptions(aguirunner.WithUserIDResolver(userIDResolver)),
	)
	if err != nil {
		log.Fatalf("failed to create AG-UI server: %v", err)
	}
	// 5. 将 AG-UI server 注册到 trpc service
	if err := tagui.RegisterAGUIServer(server, "trpc.test.helloworld.agui", aguiServer); err != nil {
		log.Fatalf("failed to register AG-UI server: %v", err)
	}
	// 6. 启动 trpc 服务
	if err := server.Serve(); err != nil {
		log.Fatalf("server stopped with error: %v", err)
	}
}

// userIDResolver resolves the user ID from the AG-UI input.
func userIDResolver(ctx context.Context, input *adapter.RunAgentInput) (string, error) {
	return "user", nil
}

// newAgent creates a new agent.
func newAgent() agent.Agent {
	modelInstance := openai.New(*modelName)
	generationConfig := model.GenerationConfig{
		MaxTokens:   intPtr(512),
		Temperature: floatPtr(0.7),
		Stream:      *isStream,
	}
	calculatorTool := function.NewFunctionTool(
		calculator,
		function.WithName("calculator"),
		function.WithDescription("A calculator tool, you can use it to calculate the result of the operation. "+
			"a is the first number, b is the second number, "+
			"the operation can be add, subtract, multiply, divide, power."),
	)
	return llmagent.New(
		agentName,
		llmagent.WithTools([]tool.Tool{calculatorTool}),
		llmagent.WithModel(modelInstance),
		llmagent.WithGenerationConfig(generationConfig),
		llmagent.WithInstruction("You are a helpful assistant."),
	)
}

func calculator(ctx context.Context, args calculatorArgs) (calculatorResult, error) {
	var result float64
	switch args.Operation {
	case "add", "+":
		result = args.A + args.B
	case "subtract", "-":
		result = args.A - args.B
	case "multiply", "*":
		result = args.A * args.B
	case "divide", "/":
		result = args.A / args.B
	case "power", "^":
		result = math.Pow(args.A, args.B)
	default:
		return calculatorResult{Result: 0}, fmt.Errorf("invalid operation: %s", args.Operation)
	}
	return calculatorResult{Result: result}, nil
}

type calculatorArgs struct {
	Operation string  `json:"operation" description:"add, subtract, multiply, divide, power"`
	A         float64 `json:"a" description:"First number"`
	B         float64 `json:"b" description:"Second number"`
}

type calculatorResult struct {
	Result float64 `json:"result"`
}

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}
