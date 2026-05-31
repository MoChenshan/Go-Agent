// Package wecom 封装企微AI Bot WebSocket服务
package wecom

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	"git.code.oa.com/trpc-go/trpc-go/log"
	twecom "git.woa.com/trpc-go/trpc-agent-go/trpc/server/wecom"
)

// Config 企微WeCom服务配置
type Config struct {
	BotID         string
	Secret        string
	BotName       string
	WebSocketURL  string
	EnableStream  bool
	ShowToolCalls bool
}

// Server 封装企微AI Bot WebSocket服务
type Server struct {
	server *twecom.Server
	cfg    Config
}

// New 创建WeCom服务
// appName: 应用名称, 用于runner创建
// ag: agent实例
// cfg: WeCom配置
func New(appName string, ag agent.Agent, cfg Config) (*Server, error) {
	if cfg.BotID == "" || cfg.Secret == "" {
		return nil, fmt.Errorf("wecom bot_id and secret are required")
	}

	baseRunner := runner.NewRunner(appName, ag)

	srv, err := twecom.New(baseRunner, twecom.Config{
		BotID:        cfg.BotID,
		Secret:       cfg.Secret,
		BotName:      cfg.BotName,
		WebSocketURL: cfg.WebSocketURL,
		EnableStream: cfg.EnableStream,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create wecom server: %w", err)
	}

	return &Server{
		server: srv,
		cfg:    cfg,
	}, nil
}

// Run 启动WeCom服务, 阻塞直到context取消或发生错误
func (s *Server) Run(ctx context.Context) error {
	log.Infof("WeCom AI bot server started for bot %q", s.cfg.BotID)
	if err := s.server.Run(ctx); err != nil {
		return fmt.Errorf("wecom server stopped: %w", err)
	}
	return nil
}
