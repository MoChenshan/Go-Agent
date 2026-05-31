// Package agents 的中文化 ReAct Planner 实现。
//
// 本文件从 "trpc.group/trpc-go/trpc-agent-go/planner/react" 复制并做中文化改造：
// 把英文标签 ***Planning / ***Reasoning / ***Action / ***Final Answer 替换为中文，
// 以便生成的推理过程直接面向中文运维工程师，避免模型再做一次翻译。
//
// 参考：oncall_agent/domain/model/planner.go
package agents

import (
	"context"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/planner"
)

// 中文化 ReAct 输出标签
const (
	PlanningTag    = "\n***规划***\n"
	ReplanningTag  = "\n***重新规划***\n"
	ReasoningTag   = "\n***推理***\n"
	ActionTag      = "\n***行动***\n"
	FinalAnswerTag = "\n***最终答案***\n"
)

// 确保 Planner 实现了 planner.Planner 接口
var _ planner.Planner = (*ReactPlanner)(nil)

// ReactPlanner 自定义中文化 ReAct 规划器。
type ReactPlanner struct{}

// NewReactPlanner 构造一个中文化 ReAct Planner 实例。
func NewReactPlanner() *ReactPlanner {
	return &ReactPlanner{}
}

// BuildPlanningInstruction 生成 System Prompt 中追加的规划指令。
func (p *ReactPlanner) BuildPlanningInstruction(
	_ context.Context,
	_ *agent.Invocation,
	_ *model.Request,
) string {
	return p.buildPlannerInstruction()
}

// ProcessPlanningResponse 对 LLM 响应做后处理，过滤掉无名的工具调用。
func (p *ReactPlanner) ProcessPlanningResponse(
	_ context.Context,
	_ *agent.Invocation,
	response *model.Response,
) *model.Response {
	if response == nil || len(response.Choices) == 0 {
		return nil
	}

	processed := *response
	processed.Choices = make([]model.Choice, len(response.Choices))
	for i, choice := range response.Choices {
		pc := choice
		if len(choice.Message.ToolCalls) > 0 {
			var filtered []model.ToolCall
			for _, tc := range choice.Message.ToolCalls {
				if tc.Function.Name != "" {
					filtered = append(filtered, tc)
				}
			}
			pc.Message.ToolCalls = filtered
		}
		processed.Choices[i] = pc
	}
	return &processed
}

// buildPlannerInstruction 拼接完整的规划指令。
func (p *ReactPlanner) buildPlannerInstruction() string {
	return strings.Join([]string{
		p.buildHighLevelPreamble(),
		p.buildPlanningPreamble(),
		p.buildActionPreamble(),
		p.buildReasoningPreamble(),
		p.buildFinalAnswerPreamble(),
		p.buildToolCodePreamble(),
		p.buildUserInputPreamble(),
	}, "\n\n")
}

func (p *ReactPlanner) buildHighLevelPreamble() string {
	return strings.Join([]string{
		"回答问题时，优先使用提供的工具收集信息，而不是仅依赖模型记忆。",
		"",
		"请按以下流程回答：(1) 先用自然语言制定一份计划；(2) 然后使用工具执行计划，" +
			"在工具调用之间进行推理，总结当前状态并明确下一步；(3) 最后给出一个最终答案。",
		"",
		"请按以下格式组织输出：(1) 规划部分放在 " + PlanningTag +
			" 下；(2) 工具调用放在 " + ActionTag + " 下，推理放在 " + ReasoningTag +
			" 下；(3) 最终答案放在 " + FinalAnswerTag + " 下。",
	}, "\n")
}

func (p *ReactPlanner) buildPlanningPreamble() string {
	return strings.Join([]string{
		"规划要求：",
		"- 计划要能覆盖用户问题的所有方面，仅涉及可用的工具。",
		"- 计划以编号列表呈现，每一步使用一个或多个工具。",
		"- 通过阅读计划，应能直观看出每一步要触发哪些工具或采取什么行动。",
		"- 如果初始计划执行失败，应从过往结果中学习并修订计划，修订后的计划放在 " +
			ReplanningTag + " 下，然后按新计划继续执行。",
	}, "\n")
}

func (p *ReactPlanner) buildActionPreamble() string {
	return strings.Join([]string{
		"行动要求：",
		"- 用第一人称明确声明下一步行动（例如「我将调用 xxx 工具查询 yyy」）。",
		"- 使用必要的工具执行该行动，并对结果做一句话总结。",
	}, "\n")
}

func (p *ReactPlanner) buildReasoningPreamble() string {
	return strings.Join([]string{
		"推理要求：",
		"- 基于用户问题与工具返回结果，总结当前进展。",
		"- 结合工具输出与原计划，给出下一步指令，使轨迹不断逼近最终答案。",
	}, "\n")
}

func (p *ReactPlanner) buildFinalAnswerPreamble() string {
	return strings.Join([]string{
		"最终答案要求：",
		"- 答案应精确，并遵循用户问题中的格式要求。",
		"- 若问题无法用现有工具与信息回答，应向用户说明原因并请求更多信息。",
	}, "\n")
}

func (p *ReactPlanner) buildToolCodePreamble() string {
	return strings.Join([]string{
		"工具调用要求：",
		"",
		"**自定义工具：** 上下文中已描述可用工具，可直接使用。",
		"- 工具调用必须是合法的自包含片段，不允许 import 或引用未提供的库。",
		"- 不允许使用上下文 API 中未显式定义的参数或字段。",
		"- 工具调用应简洁、高效，直接服务于用户问题与推理步骤。",
		"- 使用工具时，须使用完整的工具名（如 function_name）。",
		"- 若上下文未提供库，切勿自行编写除工具调用以外的代码。",
	}, "\n")
}

func (p *ReactPlanner) buildUserInputPreamble() string {
	return strings.Join([]string{
		"非常重要：除上述要求外，还必须遵守：",
		"",
		"- 若需要更多信息才能回答，应主动向用户澄清。",
		"- 优先使用上下文中已有的信息，避免重复调用工具。",
	}, "\n")
}
