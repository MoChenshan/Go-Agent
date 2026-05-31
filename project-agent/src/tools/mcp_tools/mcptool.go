// Package mcptools 提供 MCP 工具的统一管理。
//
// 设计思路（借鉴 oncall_agent/domain/tools/mcptool）：
//   - 每个 MCP Server 关联一个 target（目标服务域），如 "bk-monitor"/"bcs"/"gongfeng"/"*"。
//   - Agent 运行时按 target 动态加载相关工具，避免 40+ 工具全部挂给单个 Agent
//     导致工具选择准确率下降。
//   - target="*" 代表通用工具（如 RAG 知识库），对所有 Agent 可见。
//
// 关键接口：
//   - GetMCPListByTarget(target)：返回该 target 下的 MCP 服务名列表
//   - GetMCPToolsByName(name)：按服务名返回对应的 ToolSet
//   - GetAllToolSets()：返回所有 ToolSet（供通用场景使用）
package mcptools

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ServerConfig 单个 MCP Server 的配置项。
type ServerConfig struct {
	// Name 内部名称，唯一标识一个 MCP Server。
	Name string `yaml:"name"`
	// Target 目标服务域，例如 "bk-monitor" / "bcs" / "gongfeng" / "devops" / "tapd" / "*"。
	Target string `yaml:"target"`
	// URL MCP 端点地址。
	URL string `yaml:"url"`
	// Transport 传输协议，"streamable"（2025-03 默认）或 "sse"。
	Transport string `yaml:"transport"`
	// TimeoutSec 连接超时（秒）。
	TimeoutSec int `yaml:"timeout"`
	// AuthHeader 认证 Header 名，如 "X-Bkapi-Authorization"。
	AuthHeader string `yaml:"auth_header"`
	// AuthValue 认证值，支持 ${ENV_VAR} 占位符。
	AuthValue string `yaml:"auth_value"`
	// AllowedTools 可选的工具白名单；为空表示全部可用。
	AllowedTools []string `yaml:"allowed_tools"`
	// Enabled 是否启用，默认 true。
	Enabled *bool `yaml:"enabled"`
}

// IsEnabled 返回当前 ServerConfig 是否启用（默认 true）。
func (c *ServerConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// API 定义了 MCP 工具管理的外部接口。
//
//go:generate mockgen --source=mcptool.go --destination=mock.go --package=mcptools
type API interface {
	// GetMCPListByTarget 通过目标服务名称获取与该服务相关的 MCP 服务列表。
	GetMCPListByTarget(target string) []string

	// GetMCPToolsByName 通过 MCP 服务名称获取该 MCP 服务下的工具集合。
	// 若不存在则返回 nil。
	GetMCPToolsByName(mcpName string) tool.ToolSet

	// GetAllToolSets 返回所有已加载的 ToolSet。
	GetAllToolSets() []tool.ToolSet
}

// New 初始化 MCP 工具管理实例。
// configs 来自 config loader 解析的 mcp_servers.yaml。
func New(ctx context.Context, configs []ServerConfig) (API, error) {
	return newImpl(ctx, configs)
}
