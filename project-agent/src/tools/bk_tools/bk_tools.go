
// Package bktools 实现 6 个蓝鲸监控相关的 FunctionTool。
//
// 设计思路：
//   - 每个工具对应蓝鲸监控开放 API 的一个能力域（指标/日志/告警/事件/Trace/元数据）。
//   - 通过 infrastructure/bkapi.Client 统一发起请求（内置 Mock 兜底）。
//   - 未配置凭据或 BK_API_MOCK=1 时，工具自动返回预置样例数据，供本地/离线开发使用。
//   - 入参使用强类型 struct，出参统一为 Result 以便 LLM 解析。
//
// 工具清单：
//
//	读（target=bk-monitor，DiagnosisAgent 诊断用）：
//	  - bk_metrics_query   指标查询
//	  - bk_log_query       日志查询
//	  - bk_alarm_query     告警查询
//	  - bk_event_query     事件查询
//	  - bk_tracing_query   APM Trace 查询
//	  - bk_metadata_query  元数据（拓扑/主机/模块）查询
//
//	写（target=bk-write，RepairAgent 修复用，HITL 两段式确认）：
//	  - bk_alarm_silence   告警静默/抑制/撤销（D18.3 新增）
//
// TODO(D3+): 拿到蓝鲸真实凭据后，核对各工具的 path/payload schema，删除 mock 样例。
package bktools

import (
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools"
)

// Target 分组名常量。与 mcp_servers.yaml / 各 Agent.FocusedTargets 对齐。
const (
	// TargetRead 读（诊断用），保持向后兼容的"bk-monitor"。
	TargetRead = "bk-monitor"
	// TargetWrite 写（修复用，需 HITL），D18.3 新增。
	TargetWrite = "bk-write"

	// Target 兼容旧用法的别名，等价于 TargetRead。
	Target = TargetRead
)

// Result 是所有蓝鲸工具统一的返回结构。
//
// Mock 字段在 Mock 模式下为 true，帮助 LLM 感知数据非生产真实。
type Result struct {
	OK      bool   `json:"ok"`
	Mock    bool   `json:"mock,omitempty"`
	Message string `json:"message,omitempty"`
	// Data 工具自定义数据，具体结构见每个工具的文档。
	Data any `json:"data,omitempty"`
}

// NewAll 返回所有蓝鲸 FunctionTool 的 tool.Tool 列表。
//
// 从 D18.3 起同时包含读工具（6 个）和写工具（1 个 silence）。
// 老用法（仅读）可使用 NewReadOnly；仅写可使用 NewWriteOnly。
//
// client 为 nil 时内部自动调用 bkapi.NewClient()（读取环境变量）。
func NewAll(client *bkapi.Client) []tool.Tool {
	if client == nil {
		client = bkapi.NewClient()
	}
	out := make([]tool.Tool, 0, 7)
	out = append(out, NewReadOnly(client)...)
	out = append(out, NewWriteOnly(client)...)
	return out
}

// NewReadOnly 仅返回诊断用（只读）蓝鲸工具。
func NewReadOnly(client *bkapi.Client) []tool.Tool {
	if client == nil {
		client = bkapi.NewClient()
	}
	return []tool.Tool{
		newMetricsTool(client),
		newLogTool(client),
		newAlarmTool(client),
		newEventTool(client),
		newTracingTool(client),
		newMetadataTool(client),
	}
}

// NewWriteOnly 仅返回修复用（写）蓝鲸工具，需 HITL 两段式确认。
func NewWriteOnly(client *bkapi.Client) []tool.Tool {
	if client == nil {
		client = bkapi.NewClient()
	}
	return []tool.Tool{
		newAlarmSilenceTool(client),
	}
}

// NewAllTargeted 返回按 target 分组的 TargetedTool 列表。
//
//	读工具 → target=bk-monitor（供 DiagnosisAgent 订阅）
//	写工具 → target=bk-write   （供 RepairAgent 订阅，HITL）
func NewAllTargeted(client *bkapi.Client) []tools.TargetedTool {
	if client == nil {
		client = bkapi.NewClient()
	}
	out := make([]tools.TargetedTool, 0, 7)
	for _, t := range NewReadOnly(client) {
		out = append(out, tools.TargetedTool{Target: TargetRead, Tool: t})
	}
	for _, t := range NewWriteOnly(client) {
		out = append(out, tools.TargetedTool{Target: TargetWrite, Tool: t})
	}
	return out
}
