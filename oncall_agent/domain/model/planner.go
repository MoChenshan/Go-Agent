// Package model 包含跨领域共享的领域对象和原语
package model

import (
	"context"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/planner"
)

// 本包复制自"trpc.group/trpc-go/trpc-agent-go/planner/react"，由于需要自定义react tag，因此进行复制

// Tags used to structure the LLM response.
const (
	PlanningTag    = "\n***规划***\n"
	ReplanningTag  = "\n***重新规划***\n"
	ReasoningTag   = "\n***推理***\n"
	ActionTag      = "\n***行动***\n"
	FinalAnswerTag = "\n***最终答案***\n"
)

// Verify that Planner implements the planner.Planner interface.
var _ planner.Planner = (*Planner)(nil)

// Planner represents the React planner that uses explicit planning instructions.
type Planner struct{}

// NewReactPlanner creates a new React planner instance.
func NewReactPlanner() *Planner {
	return &Planner{}
}

// BuildPlanningInstruction builds the system instruction for the React planner.
func (p *Planner) BuildPlanningInstruction(
	_ context.Context,
	_ *agent.Invocation,
	_ *model.Request,
) string {
	return p.buildPlannerInstruction()
}

// ProcessPlanningResponse processes the LLM response by filtering and cleaning
// tool calls to ensure only valid function calls are preserved.
func (p *Planner) ProcessPlanningResponse(
	_ context.Context,
	_ *agent.Invocation,
	response *model.Response,
) *model.Response {
	if response == nil || len(response.Choices) == 0 {
		return nil
	}

	processedResponse := *response
	processedResponse.Choices = make([]model.Choice, len(response.Choices))

	for i, choice := range response.Choices {
		processedChoice := choice

		if len(choice.Message.ToolCalls) > 0 {
			var filteredToolCalls []model.ToolCall
			for _, toolCall := range choice.Message.ToolCalls {
				if toolCall.Function.Name != "" {
					filteredToolCalls = append(filteredToolCalls, toolCall)
				}
			}
			processedChoice.Message.ToolCalls = filteredToolCalls
		}
		processedResponse.Choices[i] = processedChoice
	}

	return &processedResponse
}

// buildPlannerInstruction builds the comprehensive planning instruction.
func (p *Planner) buildPlannerInstruction() string {
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

func (p *Planner) buildHighLevelPreamble() string {
	return strings.Join([]string{
		"When answering the question, try to leverage the available tools " +
			"to gather the information instead of your memorized knowledge.",
		"",
		"Follow this process when answering the question: (1) first come up " +
			"with a plan in natural language text format; (2) Then use tools to " +
			"execute the plan and provide reasoning between tool code snippets " +
			"to make a summary of current state and next step. Tool code " +
			"snippets and reasoning should be interleaved with each other. (3) " +
			"In the end, return one final answer.",
		"",
		"Follow this format when answering the question: (1) The planning " +
			"part should be under " + PlanningTag + ". (2) The tool code " +
			"snippets should be under " + ActionTag + ", and the reasoning " +
			"parts should be under " + ReasoningTag + ". (3) The final answer " +
			"part should be under " + FinalAnswerTag + ".",
	}, "\n")
}

func (p *Planner) buildPlanningPreamble() string {
	return strings.Join([]string{
		"Below are the requirements for the planning:",
		"The plan is made to answer the user query if following the plan. The plan " +
			"is coherent and covers all aspects of information from user query, and " +
			"only involves the tools that are accessible by the agent.",
		"The plan contains the decomposed steps as a numbered list where each step " +
			"should use one or multiple available tools.",
		"By reading the plan, you can intuitively know which tools to trigger or " +
			"what actions to take.",
		"If the initial plan cannot be successfully executed, you should learn from " +
			"previous execution results and revise your plan. The revised plan should " +
			"be under " + ReplanningTag + ". Then use tools to follow the new plan.",
	}, "\n")
}

func (p *Planner) buildActionPreamble() string {
	return strings.Join([]string{
		"Below are the requirements for the action:",
		"Explicitly state your next action in the first person ('I will...').",
		"Execute your action using necessary tools and provide a concise summary of the outcome.",
	}, "\n")
}

func (p *Planner) buildReasoningPreamble() string {
	return strings.Join([]string{
		"Below are the requirements for the reasoning:",
		"The reasoning makes a summary of the current trajectory based on the user " +
			"query and tool outputs.",
		"Based on the tool outputs and plan, the reasoning also comes up with " +
			"instructions to the next steps, making the trajectory closer to the " +
			"final answer.",
	}, "\n")
}

func (p *Planner) buildFinalAnswerPreamble() string {
	return strings.Join([]string{
		"Below are the requirements for the final answer:",
		"The final answer should be precise and follow query formatting " +
			"requirements.",
		"Some queries may not be answerable with the available tools and " +
			"information. In those cases, inform the user why you cannot process " +
			"their query and ask for more information.",
	}, "\n")
}

func (p *Planner) buildToolCodePreamble() string {
	return strings.Join([]string{
		"Below are the requirements for the tool code:",
		"",
		"**Custom Tools:** The available tools are described in the context and " +
			"can be directly used.",
		"- Code must be valid self-contained snippets with no imports and no " +
			"references to tools or libraries that are not in the context.",
		"- You cannot use any parameters or fields that are not explicitly defined " +
			"in the APIs in the context.",
		"- The code snippets should be readable, efficient, and directly relevant to " +
			"the user query and reasoning steps.",
		"- When using the tools, you should use the tool name together with the " +
			"function name.",
		"- If libraries are not provided in the context, NEVER write your own code " +
			"other than the function calls using the provided tools.",
	}, "\n")
}

func (p *Planner) buildUserInputPreamble() string {
	return strings.Join([]string{
		"VERY IMPORTANT instruction that you MUST follow in addition to the above " +
			"instructions:",
		"",
		"You should ask for clarification if you need more information to answer " +
			"the question.",
		"You should prefer using the information available in the context instead " +
			"of repeated tool use.",
	}, "\n")
}
