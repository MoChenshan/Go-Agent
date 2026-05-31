//go:build agui

// Package agui 的真实实现：需 `-tags agui` 才会编译。
//
// 功能：
//  1. 基于 trpc-agent-go/server/agui 包装入口 Agent + Session；
//  2. 内部构造 runner（注入 Session），与 SSE 通过 session.Service 共享记忆；
//  3. Handler()/Mount() 暴露 HTTP 端点，供浏览器直接访问 /agui 对话。
//
// 参考：
//   - trpc-agent-go/examples/agui/server/default/main.go
//   - oncall_agent/services/agui/agui.go
package agui

import (
	"fmt"
	"net/http"

	"trpc.group/trpc-go/trpc-agent-go/runner"
	aguiserver "trpc.group/trpc-go/trpc-agent-go/server/agui"
)

// Server AG-UI 服务（真实构建）。
type Server struct {
	cfg Config
	srv *aguiserver.Server
}

// New 构造 AG-UI 服务（真实）。
//
// 若 cfg.Agent 为 nil 返回错误；Session 允许为 nil（此时 runner 使用默认 session）。
func New(cfg Config) (*Server, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("agui: agent cannot be nil")
	}
	applyDefaults(&cfg)

	// 构造 runner（注入 Session 保证跨通道记忆）。
	var r runner.Runner
	if cfg.Session != nil {
		r = runner.NewRunner(cfg.AppName, cfg.Agent, runner.WithSessionService(cfg.Session))
	} else {
		r = runner.NewRunner(cfg.AppName, cfg.Agent)
	}

	opts := []aguiserver.Option{
		aguiserver.WithPath(cfg.Path),
		aguiserver.WithAppName(cfg.AppName),
	}
	if cfg.Session != nil {
		opts = append(opts, aguiserver.WithSessionService(cfg.Session))
	}

	srv, err := aguiserver.New(r, opts...)
	if err != nil {
		return nil, fmt.Errorf("agui: build server: %w", err)
	}
	return &Server{cfg: cfg, srv: srv}, nil
}

// Handler 返回可挂到 http.ServeMux 的 HTTP Handler。
func (s *Server) Handler() http.Handler {
	if s == nil || s.srv == nil {
		return nil
	}
	return s.srv.Handler()
}

// Path 返回 AG-UI 服务挂载路径。
func (s *Server) Path() string {
	if s == nil {
		return ""
	}
	return s.cfg.Path
}

// Config 返回当前服务配置。
func (s *Server) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.cfg
}

// Enabled 真实构建下恒为 true。
func (s *Server) Enabled() bool { return true }

// Mount 把本 AG-UI 服务挂到指定 mux 的 `Path` 上（同时挂 "Path" 与 "Path/"）。
func (s *Server) Mount(mux *http.ServeMux) error {
	if s == nil || s.srv == nil {
		return fmt.Errorf("agui: server not initialized")
	}
	if mux == nil {
		return fmt.Errorf("agui: mux cannot be nil")
	}
	h := s.srv.Handler()
	if h == nil {
		return fmt.Errorf("agui: handler is nil")
	}
	mux.Handle(s.cfg.Path, h)
	mux.Handle(s.cfg.Path+"/", h)
	return nil
}
