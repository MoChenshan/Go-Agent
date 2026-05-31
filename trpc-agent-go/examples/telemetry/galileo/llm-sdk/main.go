// Package main demonstrates tracing telemetry usage with OpenTelemetry.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"git.woa.com/galileo/eco/go/sdk/base/configs/ocp"
	traceconf "git.woa.com/galileo/eco/go/sdk/base/configs/traces"
	"git.woa.com/galileo/eco/go/sdk/base/lib/logs"
	basemode "git.woa.com/galileo/eco/go/sdk/base/model"
	"git.woa.com/galileo/eco/go/sdk/base/self"
	"git.woa.com/galileo/eco/go/sdk/base/semconv"
	modelv3 "git.woa.com/galileo/eco/go/sdk/base/v3/model"
	"git.woa.com/trpc-go/trpc-agent-go/examples/telemetry/agent"
	gmetrics "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/metrics"
	gtrace "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/trace"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func main() {
	if err := setupGalileo(); err != nil {
		log.Fatalf("galileo setup error")
	}

	const agentName = "multi-tool-assistant"
	// Parse command line arguments
	modelName := flag.String("model", "deepseek-chat", "Model name to use")
	flag.Parse()
	printGuideMessage(*modelName)
	a := agent.NewMultiToolChatAgent("multi-tool-assistant", *modelName)
	userMessage := []string{
		"Calculate 123 + 456 * 789",
		"What day of the week is today?",
		"'Hello World' to uppercase",
		"Create a test file in the current directory",
		"Find information about Tesla company",
	}

	for _, msg := range userMessage {
		func() {
			// Attributes represent additional key-value descriptors that can be bound to a metric observer or recorder.
			commonAttrs := []attribute.KeyValue{
				attribute.String("agentName", agentName),
				attribute.String("modelName", *modelName),
			}
			ctx, span := atrace.Tracer.Start(
				context.Background(),
				agentName,
				trace.WithAttributes(commonAttrs...),
			)
			defer span.End()
			span.SetAttributes(attribute.String("user-message", msg))
			result, err := a.ProcessMessage(ctx, msg)
			if err != nil {
				span.SetAttributes(attribute.String("error", err.Error()))
				log.Fatalf("Chat system failed to run: %v", err)
			}
			span.SetAttributes(attribute.String("output", result))
			span.SetAttributes(attribute.String("error", "<nil>"))
		}()
	}
}

func printGuideMessage(modelName string) {
	fmt.Printf("🚀 Multi-Tool Intelligent Assistant Demo\n")
	fmt.Printf("Model: %s\n", modelName)
	fmt.Printf("Available tools: calculator, time_tool, text_tool, file_tool, duckduckgo_search\n")
	// Print welcome message and examples
	fmt.Println("💡 Try asking these questions:")
	fmt.Println("   [Calculator] Calculate 123 + 456 * 789")
	fmt.Println("   [Calculator] Calculate the square root of pi")
	fmt.Println("   [Time] What time is it now?")
	fmt.Println("   [Time] What day of the week is today?")
	fmt.Println("   [Text] Convert 'Hello World' to uppercase")
	fmt.Println("   [Text] Count characters in 'Hello World'")
	fmt.Println("   [File] Read the README.md file")
	fmt.Println("   [File] Create a test file in the current directory")
	fmt.Println("   [Search] Search for information about Steve Jobs")
	fmt.Println("   [Search] Find information about Tesla company")
	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
}

func setupGalileo() error {
	// Resource description, see documentation: https://git.woa.com/galileo/semantic-conventions/blob/toraxie-omp-3.0/semconv/doc/v3.0.0/index.md
	resv3 := modelv3.NewResource(
		"PCG-123.knocknock_test.short_token_proxy", // required, 观测对象的唯一标识 ID
		basemode.Production,                        // required, 命名空间，区分正式环境和测试环境
		"formal",                                   // required, 用户环境
		"",                                         // 本机 IP 地址
		"",                                         // 容器名
		"",                                         // 服务 set
		"",                                         // 部署城市
		"",                                         //  服务版本
		"",                                         // 框架协议，如 trpc、http、grpc 等
	)
	cfg := basemode.OpenTelemetryPushConfig{
		Enable: true,
		Url:    "otlp.j.woa.com:80", // 伽利略 OpenTelemetry collector 地址。
	}
	local := func(to *ocp.GalileoConfig) error {
		to.Verbose = "error"
		// Modify the access address as needed, refer to: https://iwiki.woa.com/p/4010767585: https://iwiki.woa.com/p/4010767585
		// OCP management address: Mainland China intranet (default)
		to.OcpAddr = "http://gocp.woa.com/ocp/api/v1/get_config"
		// Data access point: Mainland China intranet (default)
		to.Config.AccessPoint = basemode.AccessPoint_ACCESS_POINT_CN_PRIVATE
		return nil
	}
	_ = ocp.RegisterResource(
		resv3, ocp.WithLocalDecoder(ocp.DecodeFunc(local)),
		ocp.WithDuration(time.Minute),
	)
	// Initialize self-monitoring reporting, settings required for non-default configurations such as overseas access.
	config := ocp.GetUpdater(resv3.Target).GetConfig().Config
	self.SetupObserver(resv3, logs.DefaultWrapper(), config.SelfMonitor, config.ConfigServer)

	tracesConfig := traceconf.NewConfig(
		resv3,
		traceconf.WithSchemaURL(semconv.SchemaURL),
	)
	if err := gtrace.Setup(tracesConfig); err != nil {
		return err
	}
	return gmetrics.Setup(*resv3, cfg)
}
