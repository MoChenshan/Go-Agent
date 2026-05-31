// Package mcptools MCP 工具管理的实现。
//
// 核心职责：
//  1. 解析 mcp_servers.yaml 中的每个 ServerConfig，建立：
//     - target2MCPListMap：target → MCP 服务名列表
//     - mcpName2ToolSetMap：MCP 服务名 → ToolSet 实例
//  2. 支持 AuthValue 中的 ${ENV_VAR} 占位符（从环境变量读取实际值）。
//  3. 支持 AllowedTools 白名单过滤。
//
// 参考：oncall_agent/domain/tools/mcptool/impl.go
package mcptools

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

// envVarPattern 匹配 ${VAR_NAME} 形式的占位符。
var envVarPattern = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)

type impl struct {
	target2MCPListMap  map[string][]string
	mcpName2ToolSetMap map[string]*mcp.ToolSet
	mu                 sync.RWMutex
}

// newImpl 基于 configs 初始化 MCP ToolSet 集合。
func newImpl(_ context.Context, configs []ServerConfig) (API, error) {
	i := &impl{
		target2MCPListMap:  make(map[string][]string),
		mcpName2ToolSetMap: make(map[string]*mcp.ToolSet),
	}

	for idx := range configs {
		cfg := &configs[idx]
		if !cfg.IsEnabled() {
			continue
		}
		if err := i.registerOne(cfg); err != nil {
			return nil, fmt.Errorf("register mcp %q failed: %w", cfg.Name, err)
		}
	}
	return i, nil
}

// registerOne 注册单个 MCP Server。
func (i *impl) registerOne(cfg *ServerConfig) error {
	if cfg.Name == "" || cfg.URL == "" {
		return fmt.Errorf("mcp server name/url must not be empty")
	}

	transport := cfg.Transport
	if transport == "" {
		transport = "streamable" // 2025-03 MCP 规范默认
	}

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	headers := map[string]string{}
	if cfg.AuthHeader != "" {
		headers[cfg.AuthHeader] = expandEnv(cfg.AuthValue)
	}

	opts := []mcp.ToolSetOption{
		mcp.WithSessionReconnect(3),
		mcp.WithName(cfg.Name),
	}
	if len(cfg.AllowedTools) > 0 {
		opts = append(opts,
			mcp.WithToolFilterFunc(tool.NewIncludeToolNamesFilter(cfg.AllowedTools...)),
		)
	}

	toolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport: transport,
			ServerURL: cfg.URL,
			Timeout:   timeout,
			Headers:   headers,
		},
		opts...,
	)

	i.mu.Lock()
	defer i.mu.Unlock()
	target := cfg.Target
	if target == "" {
		target = "*"
	}
	i.target2MCPListMap[target] = append(i.target2MCPListMap[target], cfg.Name)
	i.mcpName2ToolSetMap[cfg.Name] = toolSet
	return nil
}

// GetMCPListByTarget 实现 API。
func (i *impl) GetMCPListByTarget(target string) []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.target2MCPListMap[target]
}

// GetMCPToolsByName 实现 API。
func (i *impl) GetMCPToolsByName(mcpName string) tool.ToolSet {
	i.mu.RLock()
	defer i.mu.RUnlock()
	ts, ok := i.mcpName2ToolSetMap[mcpName]
	if !ok {
		return nil
	}
	return ts
}

// GetAllToolSets 实现 API。
func (i *impl) GetAllToolSets() []tool.ToolSet {
	i.mu.RLock()
	defer i.mu.RUnlock()
	toolSets := make([]tool.ToolSet, 0, len(i.mcpName2ToolSetMap))
	for _, ts := range i.mcpName2ToolSetMap {
		toolSets = append(toolSets, ts)
	}
	return toolSets
}

// expandEnv 展开字符串中的 ${ENV_VAR} 占位符。
func expandEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		return os.Getenv(name)
	})
}
