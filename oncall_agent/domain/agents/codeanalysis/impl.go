// Package codeanalysis 包含代码分析agent实现
// 整合了代码仓库解释和链路span分析两种能力
package codeanalysis

import (
	"context"
	"fmt"

	"github.com/tidwall/gjson"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	agenttool "trpc.group/trpc-go/trpc-agent-go/tool/agent"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"

	_ "embed"
)

var (
	agentName = "code_analysis_agent"
	// agentInputSchema 输入参数 — 合并了 span_analysis 和 repo_agent 两种模式
	agentInputSchema = map[string]interface{}{
		"type":        "object",
		"description": "输入的问题和上下文",
		"properties": map[string]interface{}{
			"target": map[string]interface{}{
				"type":        "string",
				"description": "需要查询的目标服务名，app.server。",
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "需要查询的问题或分析描述",
			},
			// Span 分析模式参数
			"span_log": map[string]interface{}{
				"type":        "string",
				"description": "span中打印的日志，通常包含目标服务名的输入和输出（Span分析模式使用）",
			},
			"trace_id": map[string]interface{}{
				"type":        "string",
				"description": "查询的链路TraceID（Span分析模式使用）",
			},
			"start_time": map[string]interface{}{
				"type":        "number",
				"description": "查询的起始时间，格式为单位为毫秒的时间戳（Span分析模式使用）",
			},
			"end_time": map[string]interface{}{
				"type":        "number",
				"description": "查询的结束时间，格式为单位为毫秒的时间戳（Span分析模式使用）",
			},
			"namespace": map[string]interface{}{
				"type":        "string",
				"description": "查询的命名空间，正式、预发布环境: Production, 测试环境: Development（Span分析模式使用）",
			},
			// 代码解释模式参数
			"module_info": map[string]interface{}{
				"type":        "object",
				"description": "需要查询的模块信息, 包括条件输出列表、业务接口列表和其余描述（代码解释模式使用）",
			},
		},
		"required": []string{"target", "query"},
	}
	mcpNameList = []string{
		"magic_iwiki",
		"vip_technology_center_iwiki",
	}

	agentDesc = "魔方平台代码分析 Agent，支持代码仓库解释和链路Span分析"
	//go:embed system_prompt.md
	systemPromptTemplate string
)

func createToolFilter(
	mcpToolAPI MCPToolAPI, localTools []tool.Tool,
) func(ctx context.Context, req *model.Request) (*model.Response, error) {
	return func(ctx context.Context, req *model.Request) (*model.Response, error) {
		var reqMsg string
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == model.RoleUser {
				reqMsg = req.Messages[i].Content
				break
			}
		}
		target := gjson.Get(reqMsg, "target").String()
		toolMap := make(map[string]tool.Tool)
		// 合并 target 特定 MCP、通配符 MCP、固定知识库 MCP
		toolSetList := append(mcpToolAPI.GetMCPListByTarget(target), mcpToolAPI.GetMCPListByTarget("*")...)
		toolSetList = append(toolSetList, mcpNameList...)

		for _, toolSetName := range toolSetList {
			toolSet := mcpToolAPI.GetMCPToolsByName(toolSetName)
			for _, t := range toolSet.Tools(ctx) {
				toolName := fmt.Sprintf("%s_%s", toolSet.Name(), t.Declaration().Name)
				if reqTool, ok := req.Tools[toolName]; ok {
					toolMap[toolName] = reqTool
				}
			}
		}
		for _, t := range localTools {
			toolMap[t.Declaration().Name] = t
		}
		req.Tools = toolMap
		log.DebugContextf(ctx, "[code analysis agent] toolMap: %+v", toolMap)
		return nil, nil
	}
}

// newCodeAnalysisAgent 初始化代码分析agent
func newCodeAnalysisAgent(dep Dep) (agent.Agent, error) {
	localTools := dep.LocalTools
	toolCallBack := tool.NewCallbacks().
		RegisterAfterTool(func(ctx context.Context, toolName string, _ *tool.Declaration,
			jsonArgs []byte, result any, runErr error) (any, error) {
			log.InfoContextf(ctx, "[code analysis agent]tool %s, args %s, result %s, err %v",
				toolName, string(jsonArgs), utils.MustToJSON(result), runErr)
			return result, runErr
		})

	modelCallbacks := model.NewCallbacks().
		RegisterBeforeModel(domainmodel.FillSystemContextInfo).
		RegisterBeforeModel(createToolFilter(dep.MCPTool, localTools))

	localDesc := agentDesc
	localSystemPrompt := systemPromptTemplate
	localInputSchema := agentInputSchema
	if dep.WujiCli != nil {
		agentConfig := dep.WujiCli.GetAgentConfig(agentName)
		if agentConfig != nil {
			if agentConfig.SystemPrompt != "" {
				localSystemPrompt = agentConfig.SystemPrompt
			}
			if agentConfig.Desc != "" {
				localDesc = agentConfig.Desc
			}
			if agentConfig.InputSchema != "" {
				localInputSchema = utils.MustJSONToMap(agentConfig.InputSchema)
			}
		}
	}

	return llmagent.New(
		agentName,
		llmagent.WithModel(dep.ModelInstance),
		llmagent.WithModelCallbacks(modelCallbacks),
		llmagent.WithTools(localTools),
		llmagent.WithToolSets(dep.MCPTool.GetAllToolSets()),
		llmagent.WithToolCallbacks(toolCallBack),
		llmagent.WithPlanner(domainmodel.NewReactPlanner()),
		llmagent.WithGenerationConfig(domainmodel.BuildGenConfig(dep.GenConfig)),
		llmagent.WithInstruction(localSystemPrompt),
		llmagent.WithDescription(localDesc),
		llmagent.WithInputSchema(localInputSchema),
		llmagent.WithEnableParallelTools(true),
		llmagent.WithAddSessionSummary(true),
	), nil
}

// NewCodeAnalysisAgentTool 获取代码分析agent工具给其余agent使用
func NewCodeAnalysisAgentTool(dep Dep) (tool.CallableTool, error) {
	codeAnalysisAgent, err := newCodeAnalysisAgent(dep)
	if err != nil {
		log.Errorf("failed to create code analysis agent: %v", err)
		return nil, err
	}
	agentTool := agenttool.NewTool(
		codeAnalysisAgent,
		agenttool.WithHistoryScope(agenttool.HistoryScopeIsolated),
		agenttool.WithStreamInner(true),
		agenttool.WithSkipSummarization(false),
	)
	return agentTool, nil
}
