// Package magiconcall 包含魔方平台统一agent的实现
// 整合了问题排查和配置查询两种能力
package magiconcall

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"

	_ "embed"
)

const (
	// agentName agent名称
	agentName = "magic_agent"
)

var (
	agentDesc = "魔方营销平台统一助手,负责问题排查、配置查询及日常答疑"

	// mcpNameList 需要加载的MCP工具列表
	// 合并了 oncall agent 和 config agent 的MCP需求
	mcpNameList = []string{
		"magic_iwiki",
		"vip_technology_center_iwiki",
	}

	//go:embed system_prompt.md
	systemPromptTemplate string
)

// New 初始化统一魔方agent
func New(dep Dep) (agent.Agent, error) {
	// 打印工具的执行结果
	toolCallBack := tool.NewCallbacks().
		RegisterAfterTool(func(ctx context.Context, toolName string, _ *tool.Declaration,
			jsonArgs []byte, result any, runErr error) (any, error) {
			log.InfoContextf(ctx, "[magic agent] tool %s, args %s, result %v, err %v",
				toolName, string(jsonArgs), utils.MustToJSON(result), runErr)
			return result, runErr
		})

	// 合并MCP工具集
	var toolSets = make([]tool.ToolSet, 0)
	if dep.MCPTool != nil {
		// 加载所有目标为 "*" 的MCP工具（oncall agent原有）
		for _, name := range dep.MCPTool.GetMCPListByTarget("*") {
			toolSets = append(toolSets, dep.MCPTool.GetMCPToolsByName(name))
		}
		// 加载配置生成相关的MCP工具（config agent原有）
		for _, name := range mcpNameList {
			ts := dep.MCPTool.GetMCPToolsByName(name)
			// 避免重复添加
			if ts != nil {
				toolSets = append(toolSets, ts)
			}
		}
	}

	localTools := dep.LocalTools

	// 填充系统上下文信息
	modelCallbacks := model.NewCallbacks().
		RegisterBeforeModel(domainmodel.FillSystemContextInfo)

	// 若无极配置了system prompt和desc等参数，则使用无极配置
	var inputSchema map[string]any
	localDesc := agentDesc
	localSystemPrompt := systemPromptTemplate
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
				inputSchema = utils.MustJSONToMap(agentConfig.InputSchema)
			}
		}
	}

	return llmagent.New(
		agentName,
		llmagent.WithModel(dep.ModelInstance),
		llmagent.WithModelCallbacks(modelCallbacks),
		llmagent.WithTools(localTools),
		llmagent.WithToolSets(toolSets),
		llmagent.WithToolCallbacks(toolCallBack),
		llmagent.WithPlanner(domainmodel.NewReactPlanner()),
		llmagent.WithGenerationConfig(domainmodel.BuildGenConfig(dep.GenConfig)),
		llmagent.WithInstruction(localSystemPrompt),
		llmagent.WithDescription(localDesc),
		llmagent.WithInputSchema(inputSchema),
		llmagent.WithEnableParallelTools(true),
		llmagent.WithAddSessionSummary(true),
	), nil
}
