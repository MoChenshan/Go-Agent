// Package mcptool 提供与MCP工具相关的接口和实现
package mcptool

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/log"
	middle_platform "git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

type mcpToolImpl struct {
	// 目标服务名映射到MCP服务名列表
	target2MCPListMap map[string][]string
	// MCP服务名映射到ToolSet列表
	mcpName2ToolSetMap map[string]*mcp.ToolSet
}

// newMCPToolImpl 初始化MCP工具实现
func newMCPToolImpl(ctx context.Context, records []*domainmodel.MCPTool, rainbowCfg RainbowConfigAPI) (API, error) {
	target2MCPListMap, mcpName2ToolSetMap, err := initMCPTools(ctx, records, rainbowCfg)
	if err != nil {
		log.ErrorContextf(ctx, "initMCPTools failed, err=%v", err)
		return nil, err
	}
	return &mcpToolImpl{
		target2MCPListMap:  target2MCPListMap,
		mcpName2ToolSetMap: mcpName2ToolSetMap,
	}, nil
}

// initMCPTools 初始化 wuji配置的mcp工具
func initMCPTools(ctx context.Context, records []*domainmodel.MCPTool, rainbowCfg RainbowConfigAPI) (map[string][]string, map[string]*mcp.ToolSet, error) {
	var (
		target2MCPListMap  = make(map[string][]string)
		mcpName2ToolSetMap = make(map[string]*mcp.ToolSet)
	)

	handler := make([]func() error, 0, len(records))
	var mu sync.Mutex
	for _, record := range records {
		if record.Valid == 0 {
			continue
		}
		handler = append(handler, func() error {
			var (
				mcpToolSet *mcp.ToolSet
				headerMap  map[string]string
			)
			if err := json.Unmarshal([]byte(record.Headers), &headerMap); err != nil {
				log.ErrorContextf(ctx, "Unmarshal failed, err=%v", err)
				return err
			}
			config := rainbowCfg.ToMap()
			for k, v := range headerMap {
				headerMap[k] = middle_platform.RenderString(v, config)
			}
			opts := []mcp.ToolSetOption{
				mcp.WithSessionReconnect(3),
				mcp.WithName(record.McpServerName),
			}
			if record.AllowedTools != "" {
				opts = append(opts,
					mcp.WithToolFilterFunc(tool.NewIncludeToolNamesFilter(
						strings.Split(record.AllowedTools, "\n")...,
					)),
				)
			}
			mcpToolSet = mcp.NewMCPToolSet(
				mcp.ConnectionConfig{
					Transport: record.TransportType,
					ServerURL: record.URL,
					Timeout:   10 * time.Second,
					Headers:   headerMap,
				},
				opts...,
			)

			mu.Lock()
			target2MCPListMap[record.Target] = append(target2MCPListMap[record.Target], record.McpServerName)
			mcpName2ToolSetMap[record.McpServerName] = mcpToolSet
			mu.Unlock()
			return nil
		})
	}
	if err := trpc.GoAndWait(handler...); err != nil {
		return nil, nil, err
	}
	return target2MCPListMap, mcpName2ToolSetMap, nil
}

// GetMCPListByTarget 通过目标服务名称获取与该服务相关的MCP服务列表
func (w *mcpToolImpl) GetMCPListByTarget(target string) []string {
	return w.target2MCPListMap[target]
}

// GetMCPToolsByName 通过MCP服务名称获取该MCP服务下的工具列表
func (w *mcpToolImpl) GetMCPToolsByName(mcpName string) tool.ToolSet {
	return w.mcpName2ToolSetMap[mcpName]
}

// GetAllToolSets 获取所有的ToolSet列表
func (w *mcpToolImpl) GetAllToolSets() []tool.ToolSet {
	toolSets := make([]tool.ToolSet, 0, len(w.mcpName2ToolSetMap))
	for _, toolSet := range w.mcpName2ToolSetMap {
		toolSets = append(toolSets, toolSet)
	}
	return toolSets
}
