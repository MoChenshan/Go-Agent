// Package diagnosis 实现故障诊断 Agent。
//
// DiagnosisAgent 是 LetsGo 游戏服务器故障诊断的核心执行者，
// 根据 target 动态加载蓝鲸监控（bk-monitor）+ BCS 容器平台（bcs）两大类 MCP 工具：
//
//   - 蓝鲸监控 × 6：bk-metrics / bk-log / bk-alarm / bk-event / bk-tracing / bk-metadata
//   - BCS 容器  × 4：bcs-project / bcs-cluster / bcs-resource / bcs-helm
//
// 使用 ReAct Planner 实现「规划 → 行动 → 推理 → 最终答案」的结构化推理链，
// 在诊断完成后可主动 Transfer 给 RepairAgent 进入修复闭环。
//
// TODO(D4): 接入 BCS MCP + 实现 createToolFilter 动态过滤工具。
package diagnosis

import (
	_ "embed"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/agents"
	mcptools "git.woa.com/trpc-go/gameops-agent/src/tools/mcp_tools"
)

// AgentName 诊断 Agent 名称。
const AgentName = "diagnosis_agent"

const agentDesc = "LetsGo 游戏服务器故障诊断专家，查询蓝鲸监控与 BCS 容器平台的指标/日志/告警/Pod 状态，定位根因。"

//go:embed system_prompt.md
var defaultSystemPrompt string

// FocusedTargets 诊断 Agent 关注的 target 列表。
//
// 包含：
//   - bk-monitor 蓝鲸监控（指标/日志/告警/事件/Trace/元数据）
//   - bcs-read   BCS 容器平台只读（项目/集群/资源）
//   - *          通用工具
var FocusedTargets = []string{"bk-monitor", "bcs-read", "tapd-read", "*"}

// Dep 诊断 Agent 的依赖。
type Dep struct {
	Model        *openaimodel.Model
	GenConfig    agents.GenConfig
	MCPTool      mcptools.API
	LocalTools   []tool.Tool
	SystemPrompt string
	// D14 起注入：Agent 级 tool.Callbacks（safety_guard + audit_hook）。
	//   - Diagnosis 目前以只读工具为主，safety_guard 实际不会命中，
	//     挂载只是"防御在前"：一旦将来诊断工具升级成写操作（如告警规则变更），
	//     审计与拦截自动覆盖，避免漏网。
	ToolCallbacks *tool.Callbacks
}

// New 构造 DiagnosisAgent。
func New(dep Dep) (agent.Agent, error) {
	prompt := defaultSystemPrompt
	if dep.SystemPrompt != "" {
		prompt = dep.SystemPrompt
	}

	toolSets := collectToolSets(dep.MCPTool)

	// D14：NewDefaultModelCallbacks 自动叠加 input_guard / output_guard。
	modelCallbacks := agents.NewDefaultModelCallbacks()

	opts := []llmagent.Option{
		llmagent.WithModel(dep.Model),
		llmagent.WithDescription(agentDesc),
		llmagent.WithInstruction(prompt),
		llmagent.WithGenerationConfig(agents.BuildGenConfig(dep.GenConfig)),
		llmagent.WithModelCallbacks(modelCallbacks),
		llmagent.WithTools(dep.LocalTools),
		llmagent.WithToolSets(toolSets),
		llmagent.WithPlanner(agents.NewReactPlanner()),
		llmagent.WithEnableParallelTools(true),
		llmagent.WithAddSessionSummary(true),
	}
	if dep.ToolCallbacks != nil {
		opts = append(opts, llmagent.WithToolCallbacks(dep.ToolCallbacks))
	}

	return llmagent.New(AgentName, opts...), nil
}

// collectToolSets 按 focusedTargets 从 MCPTool 加载 ToolSet。
func collectToolSets(mt mcptools.API) []tool.ToolSet {
	if mt == nil {
		return nil
	}
	var toolSets []tool.ToolSet
	seen := map[string]struct{}{}
	for _, target := range FocusedTargets {
		for _, name := range mt.GetMCPListByTarget(target) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			if ts := mt.GetMCPToolsByName(name); ts != nil {
				toolSets = append(toolSets, ts)
			}
		}
	}
	return toolSets
}
