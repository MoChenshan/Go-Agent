// Package cdkagent 包含cdkey oncall agent实现
package cdkagent

import (
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=api.go --destination=mock.go --package=cdkagent

// Dep cdkagent 依赖的外部接口
type Dep struct {
	// ModelInstance LLM模型实例
	ModelInstance *openai.Model
	// WujiCli 无极配置客户端
	WujiCli domainmodel.WujiAPI
	// LocalTools 本地工具列表（顺序和内容由 main.go 决定）
	LocalTools []tool.Tool
	// MCPTool MCP工具管理接口
	MCPTool MCPToolAPI
	// GenConfig 大模型生成配置
	GenConfig domainmodel.GenConfig
}

// MCPToolAPI MCP工具管理接口
type MCPToolAPI interface {
	GetMCPListByTarget(target string) []string
	GetMCPToolsByName(mcpName string) tool.ToolSet
	GetAllToolSets() []tool.ToolSet
}
