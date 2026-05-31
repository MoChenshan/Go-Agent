// Package a2a 包含A2A服务的注册
package a2a

import (
	"trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/server/a2a"
	"trpc.group/trpc-go/trpc-agent-go/session"

	"git.woa.com/trpc-go/trpc-a2a-go/trpc"
)

// NewA2AServer 创建A2A服务
func NewA2AServer(a2aServiceName string, agent agent.Agent, sessionService session.Service) (*server.A2AServer, error) {
	host := trpc.GetServiceHost(a2aServiceName)

	// Create a2a server with the agent
	server, err := a2a.New(
		a2a.WithHost(host),
		a2a.WithAgent(agent, true),
		a2a.WithSessionService(sessionService),
	)
	if err != nil {
		log.Errorf("Failed to create a2a server: %v", err)
		return nil, err
	}
	log.Infof("Starting server on %s...", host)
	return server, nil
}
