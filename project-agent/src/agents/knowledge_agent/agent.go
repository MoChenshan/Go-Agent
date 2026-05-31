// Package knowledge 实现知识问答 Agent。
//
// KnowledgeAgent 面向运维文档、FAQ、架构知识等场景，
// D3 起将接入 Agentic RAG（Self-RAG / CRAG / GraphRAG）+ BGE-M3 三合一混合检索 +
// BGE-Reranker-v2-M3 重排，覆盖通用检索 target="*" 的 MCP（如 iwiki）。
//
// 未来亦可替换底层模型为微调的 Qwen3-8B（KnowledgeAgent 专用模型），
// 详见「模型算法微调项目执行方案.md」。
//
// TODO(D3): 接入运维文档向量化 + 检索增强链路。
package knowledge

import (
	_ "embed"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/agents"
	mcptools "git.woa.com/trpc-go/gameops-agent/src/tools/mcp_tools"
)

// AgentName 知识问答 Agent 名称。
const AgentName = "knowledge_agent"

const agentDesc = "LetsGo 运维知识问答专家，基于运维文档、FAQ、架构文档、历史工单回答概念性与流程性问题。"

//go:embed system_prompt.md
var defaultSystemPrompt string

// Dep 知识 Agent 的依赖。
type Dep struct {
	Model        *openaimodel.Model
	GenConfig    agents.GenConfig
	MCPTool      mcptools.API
	LocalTools   []tool.Tool
	SystemPrompt string
}

// New 构造 KnowledgeAgent。
func New(dep Dep) (agent.Agent, error) {
	prompt := defaultSystemPrompt
	if dep.SystemPrompt != "" {
		prompt = dep.SystemPrompt
	}

	// 知识问答只加载通用工具（target="*"）。
	var toolSets []tool.ToolSet
	if dep.MCPTool != nil {
		for _, name := range dep.MCPTool.GetMCPListByTarget("*") {
			if ts := dep.MCPTool.GetMCPToolsByName(name); ts != nil {
				toolSets = append(toolSets, ts)
			}
		}
	}

	// D14：NewDefaultModelCallbacks 自动叠加 input_guard / output_guard（若 app 层已注册）。
	modelCallbacks := agents.NewDefaultModelCallbacks()

	return llmagent.New(
		AgentName,
		llmagent.WithModel(dep.Model),
		llmagent.WithDescription(agentDesc),
		llmagent.WithInstruction(prompt),
		llmagent.WithGenerationConfig(agents.BuildGenConfig(dep.GenConfig)),
		llmagent.WithModelCallbacks(modelCallbacks),
		llmagent.WithTools(dep.LocalTools),
		llmagent.WithToolSets(toolSets),
		llmagent.WithEnableParallelTools(true),
	), nil
}
