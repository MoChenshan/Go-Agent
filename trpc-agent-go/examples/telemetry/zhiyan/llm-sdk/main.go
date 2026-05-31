// Package main demonstrates tracing telemetry usage with OpenTelemetry.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/examples/telemetry/agent"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	zhiyanllm "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm"
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	_, err := zhiyanllm.Start(context.Background())
	if err != nil {
		log.Fatal(err)
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
