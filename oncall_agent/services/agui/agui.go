// Package agui 包含agui相关代码
package agui

import (
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// New 新建agui服务
func New(agent agent.Agent, sessionService session.Service) (*agui.Server, error) {
	runner := runner.NewRunner(agent.Info().Name, agent, runner.WithSessionService(sessionService))
	aguiServer, err := agui.New(runner, agui.WithPath("/agui"))
	return aguiServer, err
}
