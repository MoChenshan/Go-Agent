// Package coordinator 实现协调者 Agent。
//
// Coordinator 是整个 Multi-Agent 系统的入口，只做意图识别与路由，
// 自身不挂任何工具。通过 tRPC-Agent-Go 的原生 Transfer/Handoff 机制
// 将用户请求分发到对应的专家子 Agent：
//
//   - DiagnosisAgent   ：故障诊断（蓝鲸 + BCS MCP）
//   - KnowledgeAgent   ：运维知识问答（Agentic RAG）
//   - FileAnalystAgent ：文件/图片分析（Skills + 多模态）
//   - RepairAgent      ：自动修复闭环（工蜂 Git + 蓝盾 + TAPD）
//
// 兜底策略：Prompt 中显式规定「不确定时优先路由到 knowledge_agent」。
package coordinator

import (
	_ "embed"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"

	"git.woa.com/trpc-go/gameops-agent/src/agents"
)

// AgentName 协调者 Agent 名称（用于 Runner/SSE 显示）。
const AgentName = "coordinator"

// agentDesc 协调者 Agent 的简要描述。
const agentDesc = "GameOps 智能运维助手入口，负责理解用户意图并分发到故障诊断/知识问答/文件分析/自动修复四个子 Agent。"

//go:embed system_prompt.md
var defaultSystemPrompt string

// Dep 协调者 Agent 的依赖。
type Dep struct {
	// Model LLM 模型实例（必填）。
	Model *openaimodel.Model
	// GenConfig 生成参数。
	GenConfig agents.GenConfig
	// SubAgents 子 Agent 列表（Coordinator 通过 Transfer 将请求委派给它们）。
	SubAgents []agent.Agent
	// SystemPrompt 可选，覆盖默认 System Prompt（例如从配置中心热更新）。
	SystemPrompt string
}

// New 构造 Coordinator Agent。
func New(dep Dep) (agent.Agent, error) {
	prompt := defaultSystemPrompt
	if dep.SystemPrompt != "" {
		prompt = dep.SystemPrompt
	}

	// 注入当前时间上下文 + D14 全局 input_guard / output_guard。
	modelCallbacks := agents.NewDefaultModelCallbacks()

	return llmagent.New(
		AgentName,
		llmagent.WithModel(dep.Model),
		llmagent.WithDescription(agentDesc),
		llmagent.WithInstruction(prompt),
		llmagent.WithGenerationConfig(agents.BuildGenConfig(dep.GenConfig)),
		llmagent.WithModelCallbacks(modelCallbacks),
		llmagent.WithSubAgents(dep.SubAgents),
		llmagent.WithEnableParallelTools(true),
		// D7: Transfer 到子 Agent 后结束 Coordinator 这一轮的调用，
		// 避免 Coordinator 与子 Agent 在同一次 Run 中反复交接造成 Token 浪费和循环。
		// 对于"先诊断后修复"类场景，可由子 Agent 自身决定是否再 Transfer，
		// 或者让用户在下一轮会话中继续；任一策略都不会被本选项阻断。
		llmagent.WithEndInvocationAfterTransfer(true),
	), nil
}
