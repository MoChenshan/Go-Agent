// Package wuji 包含wuji配置，实现 domain/model.WujiAPI 接口
package wuji

import (
	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

// LocalToolConfig 本地工具配置（与 domain/model.LocalToolConfig 相同，供无极SDK注册用）
type LocalToolConfig = domainmodel.LocalToolConfig

// AgentConfig agent配置（与 domain/model.AgentConfig 相同，供无极SDK注册用）
type AgentConfig = domainmodel.AgentConfig

// MCPTool MCP工具配置（供无极SDK注册用；字段与无极表字段对应）
type MCPTool struct {
	ID            int    `json:"id"`
	Valid         int    `json:"valid"`
	TransportType string `json:"transport_type"`
	Headers       string `json:"headers"`
	URL           string `json:"url"`
	Comment       string `json:"comment"`
	Target        string `json:"target"`
	Type          string `json:"type"`
	Modifier      string `json:"modifier"`
	AllowedTools  string `json:"allowed_tools"`
	McpServerName string `json:"MCP_server_name"`
}
