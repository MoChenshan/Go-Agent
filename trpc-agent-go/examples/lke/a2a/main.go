package main

import (
	"flag"
	"fmt"

	"git.code.oa.com/trpc-go/trpc-go"
	a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	a2aserver "trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/server/a2a"
)

const serviceName = "trpc.app.agent.lke"

var (
	botAppKey = flag.String("bot_app_key", "lke-a2a-demo", "LKE bot app key")
	endpoint  = flag.String("endpoint", "", "LKE endpoint override (optional)")
	mock      = flag.Bool("mock", true, "enable LKE SDK mock mode")
	streaming = flag.Bool("streaming", true, "enable streaming responses")
	debugLog  = flag.Bool("debug_log", false, "enable debug logging")
	agentName = flag.String("agent_name", "ExampleAgent", "LKE agent name (configured on LKE side)")
	agentID   = flag.String("agent_id", "lke-agent", "agent id shown in A2A Agent Card")
)

func main() {
	flag.Parse()

	server := trpc.NewServer()
	host := a2atrpc.GetServiceHost(serviceName)
	if host == "" {
		log.Fatalf("service %s not found in trpc_go.yaml", serviceName)
	}

	a2aServer, err := buildA2AServer(host)
	if err != nil {
		log.Fatalf("failed to build A2A server: %v", err)
	}
	if err := a2atrpc.RegisterA2AServer(server, serviceName, a2aServer); err != nil {
		log.Fatalf("failed to register A2A server: %v", err)
	}

	log.Infof("[service] %s running at http://%s", serviceName, host)
	if err := server.Serve(); err != nil {
		log.Fatalf("service start failed: %v", err)
	}
}

func buildA2AServer(host string) (*a2aserver.A2AServer, error) {
	setup, err := newLKEClientSetup(*endpoint, *agentName)
	if err != nil {
		return nil, err
	}

	agent := newLKEAgent(*botAppKey, *mock, *agentID, setup)

	server, err := a2a.New(
		a2a.WithHost(host),
		a2a.WithDebugLogging(*debugLog),
		a2a.WithAgent(agent, *streaming),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create A2A server: %w", err)
	}
	return server, nil
}
