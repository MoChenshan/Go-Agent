// Package fileanalyst 实现文件分析 Agent。
//
// FileAnalystAgent 专门处理用户上传的：
//   - 文本类：日志片段、配置文件、堆栈 dump
//   - 结构化数据：CSV / Excel（性能压测结果）
//   - 图片：监控面板截图（多模态）
//
// D6 起实现本地 file_analyze / image_analyze 两类 FunctionTool，
// D7 起接入 Skills 技能系统（log_pattern / csv_compare / perf_report）。
package fileanalyst

import (
	_ "embed"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/agents"
)

// AgentName 文件分析 Agent 名称。
const AgentName = "file_analyst_agent"

const agentDesc = "LetsGo 文件分析专家，解析用户上传的日志/CSV/Excel/监控面板截图，给出分析结论。"

//go:embed system_prompt.md
var defaultSystemPrompt string

// Dep 文件分析 Agent 的依赖。
type Dep struct {
	Model        *openaimodel.Model
	GenConfig    agents.GenConfig
	LocalTools   []tool.Tool // D6 起注入：file_analyze / image_analyze 等本地工具
	SystemPrompt string
	// D13 起注入：Skills 技能仓库 + 本地代码执行器；
	// 两者必须同时非 nil 才启用，缺失时回退到"仅 FunctionTool"模式。
	SkillRepo    *skill.FSRepository
	CodeExecutor codeexecutor.CodeExecutor
	// D13 起注入：Agent 级 tool.Callbacks（可挂 safety_guard / audit_hook）。
	ToolCallbacks *tool.Callbacks
}

// New 构造 FileAnalystAgent。
func New(dep Dep) (agent.Agent, error) {
	prompt := defaultSystemPrompt
	if dep.SystemPrompt != "" {
		prompt = dep.SystemPrompt
	}

	// D14：NewDefaultModelCallbacks 自动叠加 input_guard / output_guard。
	modelCallbacks := agents.NewDefaultModelCallbacks()

	opts := []llmagent.Option{
		llmagent.WithModel(dep.Model),
		llmagent.WithDescription(agentDesc),
		llmagent.WithInstruction(prompt),
		llmagent.WithGenerationConfig(agents.BuildGenConfig(dep.GenConfig)),
		llmagent.WithModelCallbacks(modelCallbacks),
		llmagent.WithTools(dep.LocalTools),
		llmagent.WithEnableParallelTools(true),
	}
	if dep.SkillRepo != nil && dep.CodeExecutor != nil {
		opts = append(opts,
			llmagent.WithSkills(dep.SkillRepo),
			llmagent.WithCodeExecutor(dep.CodeExecutor),
		)
	}
	if dep.ToolCallbacks != nil {
		opts = append(opts, llmagent.WithToolCallbacks(dep.ToolCallbacks))
	}

	return llmagent.New(AgentName, opts...), nil
}
