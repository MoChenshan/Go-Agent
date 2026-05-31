// Package model 包含跨领域共享的领域对象和原语
package model

// LocalToolConfig 本地工具配置
// 现网配置 https://wuji.woa.com/p/edit?appid=magic_online&schemaid=t_local_tool_config
type LocalToolConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AgentConfig 无极配置中的agent配置，所有agent共用此类型
// 现网配置 https://wuji.woa.com/p/edit?appid=magic_online&schemaid=t_agent_config
type AgentConfig struct {
	Name         string `json:"name"`
	IsValid      int    `json:"is_valid"`
	SystemPrompt string `json:"system_prompt"`
	McpTools     string `json:"mcp_tools"`
	LocalTools   string `json:"local_tools"`
	Desc         string `json:"desc"`
	InputSchema  string `json:"input_schema"`
	ID           int    `json:"id"`
}

// MCPTool MCP工具无极配置
// 现网配置 https://wuji.woa.com/p/edit?appid=magic_online&schemaid=t_mcp_tool
type MCPTool struct {
	McpServerName string `json:"mcp_server_name"`
	URL           string `json:"url"`
	TransportType string `json:"transport_type"`
	Headers       string `json:"headers"`
	AllowedTools  string `json:"allowed_tools"`
	Target        string `json:"target"`
	Valid         int    `json:"valid"`
}

// WujiAPI 无极配置接口，由 infrastructure/config/wuji 实现，供 domain 层注入
type WujiAPI interface {
	// GetAllMCPTool 获取所有MCP工具配置
	GetAllMCPTool() []*MCPTool
	// GetAgentConfig 获取agent配置
	GetAgentConfig(name string) *AgentConfig
	// GetLocalToolConfig 获取本地工具配置
	GetLocalToolConfig(name string) *LocalToolConfig
}
