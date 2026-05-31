package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"strings"
	"sync"

	"git.code.oa.com/trpc-go/trpc-go"
	tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui" // 导入内网 agui
	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/adapter"
	aguirunner "trpc.group/trpc-go/trpc-agent-go/server/agui/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/translator"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	// 1. Import zhiyan-llm plugin for telemetry (plugin init registers telemetry.zhiyan-llm).
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm"
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
	callbacks := translator.NewCallbacks().RegisterAfterTranslate(zhiyanllmCallback())
	aguiServer, err := agui.New(runner,
		agui.WithPath("/agui"),
		agui.WithAGUIRunnerOptions(
			aguirunner.WithTranslateCallbacks(callbacks),
			aguirunner.WithRunOptionResolver(runOptionResolver),
		),
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

// runOptionResolver resolves the run options for the agent.
func runOptionResolver(ctx context.Context, input *adapter.RunAgentInput) ([]agent.RunOption, error) {
	content, ok := input.Messages[len(input.Messages)-1].ContentString()
	if !ok {
		return nil, fmt.Errorf("last message content is not a string")
	}
	return []agent.RunOption{
		agent.WithSpanAttributes(
			attribute.String("agentName", agentName),
			attribute.String("modelName", *modelName),
			attribute.String("user-message", content),
		),
	}, nil
}

// zhiyanllmCallback is a callback that sends the output to ZhiYanLLM.
func zhiyanllmCallback() translator.AfterTranslateCallback {
	// Store the output for each trace ID.
	zhiyanllmOutputs := sync.Map{}
	// Get the output for a given trace ID, default to empty string.
	getOutputBuilder := func(traceID string) *strings.Builder {
		data, ok := zhiyanllmOutputs.Load(traceID)
		if !ok {
			return &strings.Builder{}
		}
		output, ok := data.(*strings.Builder)
		if !ok {
			return &strings.Builder{}
		}
		return output
	}
	// Return the callback that sends the output to ZhiYanLLM.
	return func(ctx context.Context, event aguievents.Event) (aguievents.Event, error) {
		span := trace.SpanFromContext(ctx)
		traceID := span.SpanContext().TraceID().String()
		switch e := event.(type) {
		// Reset the output.
		case *aguievents.RunStartedEvent:
			zhiyanllmOutputs.Store(traceID, &strings.Builder{})
		// Report the output.
		case *aguievents.RunFinishedEvent:
			outputBuilder := getOutputBuilder(traceID)
			span.SetAttributes(attribute.String("output", outputBuilder.String()))
			span.SetAttributes(attribute.String("error", "<nil>"))
			zhiyanllmOutputs.Delete(traceID)
		// Report the error.
		case *aguievents.RunErrorEvent:
			code := "<nil>"
			if e.Code != nil {
				code = *e.Code
			}
			err := fmt.Errorf(
				"code: %s, message: %s",
				code,
				e.Message,
			)
			span.SetAttributes(attribute.String("error", err.Error()))
			log.Fatalf("Agent run failed: %v", err)
		// Aggregate the output.
		case *aguievents.TextMessageContentEvent:
			outputBuilder := getOutputBuilder(traceID)
			outputBuilder.WriteString(e.Delta)
			zhiyanllmOutputs.Store(traceID, outputBuilder)
		}
		return nil, nil
	}
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
