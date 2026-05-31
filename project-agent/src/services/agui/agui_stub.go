//go:build !agui

// Package agui 的 stub 实现：默认构建路径，不引入 trpc-agent-go/server/agui 依赖。
//
// 调用方仍可正常 agui.New(...)，但 Handler()/Mount() 返回明确错误提示；
// 生产部署使用 `go build -tags agui ./...` 切换到真实实现。
package agui

import (
	"fmt"
	"net/http"
)

// Server AG-UI 服务占位（stub 构建）。
type Server struct {
	cfg Config
}

// New 构造 AG-UI 服务占位（stub）。
//
// 参数校验正常进行，但 Mount/Handler 在 stub 构建下不可用。
func New(cfg Config) (*Server, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("agui: agent cannot be nil")
	}
	applyDefaults(&cfg)
	return &Server{cfg: cfg}, nil
}

// Handler stub 构建下恒为 nil；调用方应先检查 Enabled()。
func (s *Server) Handler() http.Handler { return nil }

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

// Enabled 返回当前构建是否启用真实 AG-UI 链路；stub 恒为 false。
func (s *Server) Enabled() bool { return false }

// Mount stub 构建下返回明确错误，调用方可判断 Enabled() 再决定是否 Mount。
func (s *Server) Mount(mux *http.ServeMux) error {
	return fmt.Errorf("agui: real AG-UI link disabled in stub build; rebuild with `-tags agui` to enable")
}
