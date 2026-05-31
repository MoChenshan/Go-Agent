// Package agui 提供 AG-UI Web 前端服务，给运维人员零前端开发的 Web 调试入口。
//
// 设计目标：
//  1. 浏览器直接访问 `/agui` 与 Agent 对话（SSE 流式），无需额外前端代码；
//  2. 共享 session.Service 与 SSE 服务保持会话一致（同 user+session 跨通道续写）；
//  3. 暴露 `Handler() http.Handler` 和 `Mount(mux)`，与现有 `/v1/agent`、`/healthz` 同进程共存。
//
// 采用 build tag 条件编译：
//   - 默认构建：stub 模式（不引入 server/agui 依赖，保证外网 CI 和离线环境可编译）；
//   - `-tags agui`：启用真实 `aguiserver.New` 链路，浏览器即可访问 Web 前端。
//
// 使用方式：
//
//	srv, err := agui.New(agui.Config{
//	    Agent:   entrance,
//	    Session: sess,
//	})
//	// stub 构建：Mount 返回明确错误，不会破坏 HTTP 启动；
//	// 真实构建（`go build -tags agui ./...`）：Mount 把 /agui 挂到 mux 上。
//
// 参考：
//   - trpc-agent-go/examples/agui/server/default/main.go
//   - oncall_agent/services/agui/agui.go
package agui

import (
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// Config AGUI 服务配置。
type Config struct {
	// Path HTTP 挂载路径，默认 "/agui"。
	Path string
	// AppName 应用名（会写入 AG-UI 协议事件），默认 "gameops-agent"。
	AppName string
	// Agent 入口 Agent（通常是 Coordinator）。必填。
	Agent agent.Agent
	// Session 会话服务；允许为 nil（单次会话不跨回合时可用）。
	Session session.Service
}

// DefaultPath AG-UI 默认 HTTP 挂载路径。
const DefaultPath = "/agui"

// DefaultAppName AG-UI 默认应用名。
const DefaultAppName = "gameops-agent"

// applyDefaults 给 Config 补默认值（Path / AppName）。
func applyDefaults(cfg *Config) {
	if cfg.Path == "" {
		cfg.Path = DefaultPath
	}
	if cfg.AppName == "" {
		cfg.AppName = DefaultAppName
	}
}