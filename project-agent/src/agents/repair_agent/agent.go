// Package repair 实现自动修复 Agent。
//
// RepairAgent 是全链路闭环的执行者，接受来自 DiagnosisAgent 的诊断结论，
// 按严格编排（StateGraph，D13 起接入）完成：
//
//	诊断结论 → 获取源码 → LLM 生成修复 → 创建分支 → 提交代码 → 创建 MR
//	                                        ↓
//	                  触发蓝盾编译 → 轮询状态 → 更新 TAPD 单 → 推送通知
//
// **安全红线**：
//   - 绝不自动合并 MR
//   - 绝不自动关闭 TAPD 单
//   - safety_guard Plugin（D15）将拦截 force push / 删除分支 / 直推 master 等高危操作
//
// TODO(D11-D15): 接入 TAPD MCP + 工蜂 Git FunctionTool + 蓝盾 MCP + StateGraph 编排。
package repair

import (
	_ "embed"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/agents"
	mcptools "git.woa.com/trpc-go/gameops-agent/src/tools/mcp_tools"
)

// AgentName 修复 Agent 名称。
const AgentName = "repair_agent"

const agentDesc = "LetsGo 自动修复专家，根据诊断结论生成修复代码、创建工蜂 MR、触发蓝盾编译、更新 TAPD 单，全链路闭环（严禁自动合并）。"

//go:embed system_prompt.md
var defaultSystemPrompt string

// FocusedTargets 修复 Agent 关注的 target 列表。
//
// 包含：
//   - bcs-write   BCS Helm 写操作（回滚/部署，HITL 两段式确认）
//   - bk-write    蓝鲸监控写操作（告警静默/抑制，HITL 两段式确认，D18.3 新增）
//   - gongfeng    工蜂 Git（MR 创建/合并，HITL）
//   - devops      蓝盾 CI/CD（流水线重跑/取消，HITL）
//   - tapd        TAPD 缺陷登记（软写，HITL）
//   - tapd-read   TAPD 查询（查历史同类单）
//   - *           通用工具
var FocusedTargets = []string{"bcs-write", "bk-write", "gongfeng", "devops", "tapd", "tapd-read", "*"}

// Dep 修复 Agent 的依赖。
type Dep struct {
	Model        *openaimodel.Model
	GenConfig    agents.GenConfig
	MCPTool      mcptools.API
	LocalTools   []tool.Tool
	SystemPrompt string
	// D13 起注入：Agent 级 tool.Callbacks（safety_guard + audit_hook）。
	ToolCallbacks *tool.Callbacks
}

// New 构造 RepairAgent。
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
		llmagent.WithEnableParallelTools(false), // 修复流程有副作用，串行更安全
	}
	if dep.ToolCallbacks != nil {
		opts = append(opts, llmagent.WithToolCallbacks(dep.ToolCallbacks))
	}

	return llmagent.New(AgentName, opts...), nil
}

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
