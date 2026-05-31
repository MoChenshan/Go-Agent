// Package cdkagent 包含cdkey oncall agent实现
package cdkagent

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
	agentName = "cdkey_oncall_agent"
)

var (
	agentDesc = "腾讯视频cdkey oncall agent"

	//go:embed system_prompt.md
	systemPromptTemplate string
)

// New 初始化cdkey oncall agent
func New(dep Dep) (agent.Agent, error) {
	toolCallBack := tool.NewCallbacks().
		RegisterAfterTool(func(ctx context.Context, toolName string, _ *tool.Declaration,
			jsonArgs []byte, result any, runErr error) (any, error) {
			log.InfoContextf(ctx, "[cdkey agent]tool %s, args %s, result %v, err %v",
				toolName, string(jsonArgs), utils.MustToJSON(result), runErr)
			return result, runErr
		})

	var toolSets = make([]tool.ToolSet, 0)
	if dep.MCPTool != nil {
		toolSets = append(toolSets, dep.MCPTool.GetMCPToolsByName("magic_tools"))
	}

	localTools := dep.LocalTools

	modelCallbacks := model.NewCallbacks().
		RegisterBeforeModel(domainmodel.FillSystemContextInfo)

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
		llmagent.WithGenerationConfig(domainmodel.BuildGenConfig(dep.GenConfig)),
		llmagent.WithInstruction(localSystemPrompt),
		llmagent.WithDescription(localDesc),
		llmagent.WithInputSchema(inputSchema),
		llmagent.WithAddSessionSummary(true),
	), nil
}
