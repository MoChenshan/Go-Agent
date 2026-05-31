package taijia2a

import (
	"context"
	"fmt"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.code.oa.com/trpc-go/trpc-go/server"
	a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/taijia2a/config"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/taijia2a/internal"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/taijia2a/proxy"
	"github.com/go-chi/chi/v5"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	a2aserver "trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// RegisterTaijiA2AProxyServer registers the taiji A2A server.
func RegisterTaijiA2AProxyServer(s *server.Server, name string, basePath string) error {
	a2aServer, err := newA2AProxyServer(basePath)
	if err != nil {
		log.Errorf("Failed to create A2A server: %v", err)
		return err
	}
	if err = a2atrpc.RegisterA2AServer(s, name, a2aServer); err != nil {
		log.Errorf("RegisterA2AServer failed: %v", err)
		return err
	}
	return nil
}

func newA2AProxyServer(basePath string) (*a2aserver.A2AServer, error) {
	taskManager, err := taskmanager.NewMemoryTaskManager(newTaijiMessageProcessor())
	if err != nil {
		log.Errorf("failed to create task manager: %v", err)
		return nil, err
	}

	destBasePath := fmt.Sprintf("/%s{%s}/",
		internal.RepairBasePath(basePath), internal.CtxAgentNameValue)
	srv, err := a2aserver.NewA2AServer(a2aserver.AgentCard{}, taskManager,
		a2aserver.WithHTTPRouter(chi.NewMux()),
		a2aserver.WithMiddleWare(internal.NewMiddleWare()),
		a2aserver.WithAgentCardHandler(internal.NewAgentCardHTTPHandler()),
		a2aserver.WithCORSEnabled(true),
		a2aserver.WithBasePath(destBasePath),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create A2A server: %v", err)
	}
	return srv, nil
}

type taijiMessageProcessorImpl struct{}

// newTaijiMessageProcessor creates a new A2A server message processor
func newTaijiMessageProcessor() taskmanager.MessageProcessor {
	return &taijiMessageProcessorImpl{}
}

// ProcessMessage processes the message
func (p *taijiMessageProcessorImpl) ProcessMessage(
	ctx context.Context,
	message protocol.Message,
	options taskmanager.ProcessOptions,
	taskHandler taskmanager.TaskHandler,
) (*taskmanager.MessageProcessingResult, error) {
	subPath := internal.GetSubPath(ctx)
	if proxyCfg, ok := config.GetConfiger().GetProxyConfig(subPath); ok {
		return proxy.New(proxyCfg).ProcessMessage(ctx, message, options, taskHandler)
	}
	return nil, fmt.Errorf("proxy config not found: %s", subPath)
}
