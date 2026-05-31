//go:build !a2a

// Package a2a 的 stub 实现：默认构建路径，不引入任何内网依赖。
//
// 调用方仍可正常 New(...)，但 Enabled() 返回 false，Handler() 返回 nil，
// 生产部署时使用 `go build -tags a2a ./...` 切换到真实实现。
package a2a

import (
	"context"
	"fmt"
	"net/http"
)

// Server A2A 服务占位（stub 构建）。
type Server struct {
	cfg Config
}

// New 构造 A2A 服务占位。
//
// stub 构建下仅做参数校验，真实的 A2A 协议端点需要 `-tags a2a`。
func New(cfg Config) (*Server, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("a2a: agent cannot be nil")
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = DefaultServiceName
	}
	return &Server{cfg: cfg}, nil
}

// Config 返回服务配置。
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
	return s.cfg.ServiceName
}

// Enabled 返回当前构建是否启用真实 A2A 链路；stub 恒为 false。
func (s *Server) Enabled() bool { return false }

// Handler stub 构建下返回 nil。
func (s *Server) Handler() http.Handler { return nil }

// Mount stub 构建下返回错误提示。
func (s *Server) Mount(mux *http.ServeMux, pattern string) error {
	return fmt.Errorf("a2a: real A2A link disabled in stub build; rebuild with `-tags a2a` to enable")
}

// Stop stub 构建下为空操作。
func (s *Server) Stop(ctx context.Context) error { return nil }
