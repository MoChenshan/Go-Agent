// Package mcptool 提供与MCP工具相关的接口和实现
package mcptool

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=api.go --destination=mock.go --package=mcptool

// RainbowConfigAPI 七彩石配置接口（由 infrastructure/config/rainbow 实现）
type RainbowConfigAPI interface {
	// ToMap 将配置转换为map，用于渲染模板
	ToMap() map[string]interface{}
}

// API 定义了MCP工具的接口
type API interface {
	// GetMCPListByTarget 通过目标服务名称获取与该服务相关的MCP服务列表
	GetMCPListByTarget(target string) []string

	// GetMCPToolsByName 通过MCP服务名称获取该MCP服务下的工具列表
	GetMCPToolsByName(mcpName string) tool.ToolSet

	// GetAllToolSets 获取所有的ToolSet列表
	GetAllToolSets() []tool.ToolSet
}

// NewMCPToolImpl 新建MCP工具实现
func NewMCPToolImpl(ctx context.Context, wujiCli domainmodel.WujiAPI, rainbowCfg RainbowConfigAPI) (API, error) {
	records := wujiCli.GetAllMCPTool()
	return newMCPToolImpl(ctx, records, rainbowCfg)
}
