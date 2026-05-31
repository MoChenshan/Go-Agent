// Package wuji 实现无极配置中心客户端，提供 MCP 工具、Agent 配置和本地工具配置的获取能力。
package wuji

import (
	"fmt"

	"git.woa.com/galileo/eco/go/sdk/base/self/log"
	wuji "git.woa.com/open-wuji/sdk/go/wujiclient"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

type wujiImpl struct {
	mcpConfigCli       wuji.FilterInterface
	agentConfigCli     wuji.FilterInterface
	localToolConfigCli wuji.FilterInterface
}

// New 创建一个 wujiImpl 实例
func New(mcpConfigCli, agentConfigCli, localToolConfig wuji.FilterInterface) domainmodel.WujiAPI {
	return &wujiImpl{
		mcpConfigCli:       mcpConfigCli,
		agentConfigCli:     agentConfigCli,
		localToolConfigCli: localToolConfig,
	}
}

// GetAllMCPTool 获取所有mcp工具，将无极DB结构转换为 domain/model.MCPTool
func (w *wujiImpl) GetAllMCPTool() []*domainmodel.MCPTool {
	mcpTools, ok := w.mcpConfigCli.GetALL().([]*MCPTool)
	if !ok {
		log.Errorf("get all mcp tool failed")
		return nil
	}
	result := make([]*domainmodel.MCPTool, 0, len(mcpTools))
	for _, t := range mcpTools {
		if t == nil {
			continue
		}
		result = append(result, &domainmodel.MCPTool{
			McpServerName: t.McpServerName,
			URL:           t.URL,
			TransportType: t.TransportType,
			Headers:       t.Headers,
			AllowedTools:  t.AllowedTools,
			Target:        t.Target,
			Valid:         t.Valid,
		})
	}
	return result
}

// GetAgentConfig 获取agent配置
func (w *wujiImpl) GetAgentConfig(name string) *AgentConfig {
	agentConfig, ok := w.agentConfigCli.Get(fmt.Sprintf("name=%s", name)).(*AgentConfig)
	if !ok {
		log.Errorf("get agent config failed, name: %s", name)
		return nil
	}
	return agentConfig
}

// GetLocalToolConfig 获取本地工具配置
func (w *wujiImpl) GetLocalToolConfig(name string) *LocalToolConfig {
	localToolConfig, ok := w.localToolConfigCli.Get(fmt.Sprintf("name=%s", name)).(*LocalToolConfig)
	if !ok {
		log.Errorf("get local tool config failed, name: %s", name)
		return nil
	}
	return localToolConfig
}
