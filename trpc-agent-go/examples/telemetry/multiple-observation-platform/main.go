package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.woa.com/trpc-go/trpc-agent-go/examples/telemetry/agent"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"            // Galileo telemetry
	zhiyanllm "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm" // Zhiyan LLM telemetry

	"github.com/gorilla/mux"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/langfuse" // Langfuse telemetry
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

type cleanFunc func(context.Context) error

func startObservation(ctx context.Context) cleanFunc {
	if _, err := zhiyanllm.Start(ctx); err != nil {
		log.Fatalf("failed to start Zhiyan LLM telemetry: %v", err)
	}

	langfuseClean, err := langfuse.Start(ctx)
	if err != nil {
		log.Fatalf("failed to start Langfuse telemetry: %v", err)
	}

	return func(ctx context.Context) error {
		return langfuseClean(ctx)
	}
}

type chatAgent struct {
	agent     *agent.MultiToolChatAgent
	agentName string
	modelName string
}

func newChatAgent(agentName, modelName string) *chatAgent {
	return &chatAgent{
		agent:     agent.NewMultiToolChatAgent(agentName, modelName),
		agentName: agentName,
		modelName: modelName,
	}
}

func (a *chatAgent) handle(w http.ResponseWriter, r *http.Request) {
	// 取 url 中的参数
	userID := r.FormValue("user-id")
	sessionID := r.FormValue("session-id")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	commonAttrs := []attribute.KeyValue{
		attribute.String("agentName", a.agentName),
		attribute.String("modelName", a.modelName),
		attribute.String("langfuse.environment", "development"),
		attribute.String("langfuse.session.id", sessionID),
		attribute.String("langfuse.user.id", userID),
		attribute.String("langfuse.trace.input", string(body)),
	}

	ctx, span := atrace.Tracer.Start(
		r.Context(),
		a.agentName,
		trace.WithAttributes(commonAttrs...),
	)
	defer span.End()

	result, err := a.agent.ProcessMessage(ctx, string(body))
	if result != "" {
		span.SetAttributes(attribute.String("langfuse.trace.output", result))
	}

	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	span.SetAttributes(attribute.String("error", "<nil>"))
	w.Write([]byte(result))
}

func main() {
	ctx := context.Background()
	cleanup := startObservation(ctx)
	defer func() {
		if err := cleanup(ctx); err != nil {
			log.Printf("failed to clean up Langfuse telemetry: %v", err)
		}
	}()

	const agentName = "multi-tool-assistant"

	modelName := flag.String("model", os.Getenv("MODEL_NAME"), "Model name to use")

	flag.Parse()

	a := newChatAgent(agentName, *modelName)
	s := trpc.NewServer() // load galileo config from trpc_go.yaml

	router := mux.NewRouter()
	router.HandleFunc("/agent/run", a.handle).Methods(http.MethodPost)
	// 注册 RegisterNoProtocolServiceMux 时传的参数必须和配置中的 service name 一致：s.Service("trpc.app.server.stdhttp")
	thttp.RegisterNoProtocolServiceMux(s.Service("trpc.app.app.agent"), router)
	printGuideMessage(*modelName)

	go sendRequests(ctx)

	if err := s.Serve(); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}

func sendRequests(ctx context.Context) error {
	time.Sleep(2 * time.Second)

	const agentURL = "http://127.0.0.1:8080/agent/run"
	client := &http.Client{Timeout: 30 * time.Second}
	userMessages := []string{
		"Calculate 123 + 456 * 789",
		"What day of the week is today?",
		"'Hello World' to uppercase",
		"Create a test file in the current directory",
		"Find information about Tesla company",
	}
	userID := fmt.Sprintf("user-%d", 1)
	sessionID := fmt.Sprintf("session-%d", 1)

	for _, msg := range userMessages {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, agentURL, strings.NewReader(msg))
		if err != nil {
			return err
		}

		query := req.URL.Query()
		query.Set("user-id", userID)
		query.Set("session-id", sessionID)
		req.URL.RawQuery = query.Encode()
		req.Header.Set("Content-Type", "text/plain")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("failed to send request for message %q: %v", msg, err)
			continue
		}

		func() {
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("failed to read response for message %q: %v", msg, err)
				return
			}

			if resp.StatusCode >= http.StatusBadRequest {
				log.Printf("agent returned error for message %q: status=%s body=%s", msg, resp.Status, string(body))
				return
			}

			log.Print("-------------------------agent response---------------------------------\n")
			log.Printf("message: %q\nbody: %s\n", msg, string(body))
			log.Print("--------------------------------------------------------------------------------------\n")
		}()
	}
	return nil
}

func printGuideMessage(modelName string) {
	fmt.Printf("🚀 Multi-Tool Intelligent Assistant Demo\n")
	fmt.Printf("Model: %s\n", modelName)
	fmt.Printf("Available tools: calculator, time_tool, text_tool, file_tool, duckduckgo_search\n")
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
