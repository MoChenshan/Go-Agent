// Package a2a 提供 A2A（Agent-to-Agent）协议服务封装，允许外部 Agent
// 通过 A2A v0.2 协议调用本服务的入口 Agent（Coordinator）。
//
// 采用 build tag 条件编译：
//   - 默认构建：返回空壳 Server（外网 CI / 离线测试友好）
//   - `-tags a2a`：启用真实 A2A server，注入 session，暴露 HTTP handler
//
// 使用方式：
//
//	srv, err := a2a.New(a2a.Config{
//	    ServiceName: "trpc.gameops.agent.A2A",
//	    Host:        "http://localhost:8080",
//	    Agent:       entrance,
//	    Session:     sess,
//	    Streaming:   true,
//	})
//	// 挂载到 HTTP mux：
//	// srv.Mount(mux, "/a2a/")
//
// 参考：
//   - trpc-agent-go/examples/a2amultipath/server/main.go
package a2a

import (
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// Config A2A 服务配置。
type Config struct {
	// ServiceName 服务名标识。
	//   - 默认 "trpc.gameops.agent.A2A"
	ServiceName string
	// Host A2A 服务对外暴露的 URL（用于 agent card），如 "http://localhost:8080"。
	Host string
	// Agent 入口 Agent（通常为 Coordinator）。必填。
	Agent agent.Agent
	// Session 会话服务；为 nil 时降级为不带 session 的 A2A 暴露（单轮）。
	Session session.Service
	// Streaming 是否启用流式；默认 true（让远端 Agent 能订阅事件流）。
	Streaming bool
}

// DefaultServiceName A2A 默认服务名，对应 trpc_go.yaml 中的 service.name。
const DefaultServiceName = "trpc.gameops.agent.A2A"
