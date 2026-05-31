//go:build a2a

// Package a2a 的真实实现：需 `-tags a2a` 才会编译。
//
// 功能：
//  1. 基于 trpc-agent-go/server/a2a 包装入口 Agent；
//  2. 通过标准 net/http 暴露 A2A 协议端点（无需内网 tRPC-Go 框架）；
//  3. 复用 session.Service 保证跨通道记忆（与 SSE/AG-UI 同一 session）。
//
// 参考：trpc-agent-go/examples/a2amultipath/server/main.go
package a2a

import (
	"context"
	"fmt"
	"net/http"

	a2aserverlib "trpc.group/trpc-go/trpc-a2a-go/server"
	a2a "trpc.group/trpc-go/trpc-agent-go/server/a2a"
)

// Server A2A 服务（真实构建）。
type Server struct {
	cfg  Config
	name string
	srv  *a2aserverlib.A2AServer
}

// New 构造 A2A 服务：使用公开版 trpc-agent-go/server/a2a 包装 Agent，
// 通过 Handler() 暴露标准 HTTP handler。
func New(cfg Config) (*Server, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("a2a: agent cannot be nil")
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = DefaultServiceName
	}

	// host 用于 agent card 中的 URL，默认使用 ServiceName 作为标识
	host := cfg.Host
	if host == "" {
		host = "http://localhost:8080"
	}

	opts := []a2a.Option{
		a2a.WithHost(host),
		a2a.WithAgent(cfg.Agent, cfg.Streaming),
	}
	if cfg.Session != nil {
		opts = append(opts, a2a.WithSessionService(cfg.Session))
	}
	srv, err := a2a.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("a2a: build server: %w", err)
	}
	return &Server{cfg: cfg, name: cfg.ServiceName, srv: srv}, nil
}

// Config 返回当前服务配置。
func (s *Server) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.cfg
}

// ServiceName 返回服务名。
func (s *Server) ServiceName() string {
	if s == nil {
		return ""
	}
	return s.name
}

// Enabled 真实构建下恒为 true。
func (s *Server) Enabled() bool { return true }

// Handler 返回 A2A 协议的 HTTP Handler，可直接挂到 http.ServeMux。
func (s *Server) Handler() http.Handler {
	if s == nil || s.srv == nil {
		return nil
	}
	return s.srv.Handler()
}

// Mount 把 A2A 服务挂到指定 mux 的路径上。
func (s *Server) Mount(mux *http.ServeMux, pattern string) error {
	if s == nil || s.srv == nil {
		return fmt.Errorf("a2a: server not initialized")
	}
	if mux == nil {
		return fmt.Errorf("a2a: mux cannot be nil")
	}
	h := s.srv.Handler()
	if h == nil {
		return fmt.Errorf("a2a: handler is nil")
	}
	mux.Handle(pattern, h)
	return nil
}

// Stop 优雅停止 A2A 服务。
func (s *Server) Stop(ctx context.Context) error {
	if s == nil || s.srv == nil {
		return nil
	}
	return s.srv.Stop(ctx)
}