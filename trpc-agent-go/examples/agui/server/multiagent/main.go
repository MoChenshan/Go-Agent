package main

import (
	"flag"
	"log"
	"net/http"

	"git.code.oa.com/trpc-go/trpc-go"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui"
)

const (
	serviceName  = "trpc.test.multiagent.agui"
	aguiPath     = "/agui"
	calcBasePath = "/calc/"
	timeBasePath = "/time/"
)

var (
	modelName = flag.String("model", "deepseek-chat", "Model to use")
	isStream  = flag.Bool("stream", true, "Whether to stream the response")
)

func main() {
	flag.Parse()

	calculatorAgent := newCalculatorAgent()
	calculatorRunner := runner.NewRunner(calculatorAgent.Info().Name, calculatorAgent)
	calculatorServer, err := agui.New(
		calculatorRunner,
		agui.WithBasePath(calcBasePath),
		agui.WithPath(aguiPath),
	)
	if err != nil {
		log.Fatalf("failed to create calculator AG-UI server: %v", err)
	}

	timeAgent := newTimeAgent()
	timeRunner := runner.NewRunner(timeAgent.Info().Name, timeAgent)
	timeServer, err := agui.New(
		timeRunner,
		agui.WithBasePath(timeBasePath),
		agui.WithPath(aguiPath),
	)
	if err != nil {
		log.Fatalf("failed to create time AG-UI server: %v", err)
	}

	trpcServer := trpc.NewServer()
	mux := http.NewServeMux()

	if err := tagui.RegisterAGUIServerToMux(trpcServer, mux, serviceName, calculatorServer); err != nil {
		log.Fatalf("failed to register calculator AG-UI server: %v", err)
	}
	if err := tagui.AddAGUIServerToMux(mux, timeServer); err != nil {
		log.Fatalf("failed to add time AG-UI server: %v", err)
	}

	log.Printf("AG-UI: calculator agent is serving on http://127.0.0.1:8080%s", calculatorServer.Path())
	log.Printf("AG-UI: time agent is serving on http://127.0.0.1:8080%s", timeServer.Path())

	if err := trpcServer.Serve(); err != nil {
		log.Fatalf("server stopped with error: %v", err)
	}
}
