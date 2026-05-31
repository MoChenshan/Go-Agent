// Package magiconcall 包含魔方平台统一agent的实现
// 整合了问题排查和配置查询两种能力
package magiconcall

import (
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=api.go --destination=mock.go --package=magiconcall

// Dep magiconcall 依赖的外部接口
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
