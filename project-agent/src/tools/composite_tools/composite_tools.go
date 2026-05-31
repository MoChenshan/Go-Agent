// Package compositetools 实现"跨数据源聚合"类 FunctionTool（D23'，双源日志聚合）。
//
// # 为什么要有这个包
//
// 截至 D22.1，agent 已经拥有两个日志源：
//
//	bk_log_query       → 蓝鲸日志平台（应用日志，预先采集/结构化，跨 Pod 聚合）
//	bcs_pod_logs_tail  → K8s logs API（容器 stdout/stderr 原始流，实时但分散）
//
// 真实 on-call 场景几乎每次都需要**同屏看两者**：
//
//   - 应用侧看到 ERROR → 去 K8s 那边拉当时窗口的 stdout 对齐（看是否有 panic）
//   - K8s 侧看到 CrashLoopBackOff → 去 bk-log 搜该 pod 的上游调用链（看谁在踩坑）
//
// 之前 agent 只能**顺序**调两次工具、手工拼时间线；用户体验等于"你自己对齐时间戳"。
// 本包的 `logs_unified_query` 工具把这一步合并成一次调用：
//
//   - 并发两路 fetch，任一失败不阻塞另一路
//   - 按时间戳合并排序，统一 entries[] 输出
//   - 每条带 source 字段标识来源（k8s_stdout / bk_log），LLM 可按源分桶分析
//
// # 为什么另起 composite_tools 包而不是塞进 bk_tools 或 bcs_tools
//
// 一个聚合工具跨越了 bk 和 bcs 两个 infrastructure 域，放在任一原包都要 import 对方，
// 违反"工具包按 target 分组、按 infra 后端分包"的组织原则。独立包还能让未来其他
// 跨源聚合能力（如 alarm+trace 关联、metric+log 关联）自然归档。
//
// # 零耦合约束
//
// 本包**只 import infrastructure 层**（bkapi / bcsapi），不 import bk_tools / bcs_tools。
// 原因：tool 包之间互相 import 会形成能力装配层的隐式依赖，未来任一 tool 包重构都会
// 牵动 composite。保持"composite 只认 client，不认工具"。
package compositetools

import (
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools"
)

// Target 常量定义。
//
// 聚合工具归属诊断链（只读），与 bcs_pod_logs_tail / bk_log_query 保持同档。
// DiagnosisAgent 默认订阅 bcs-read / bk-monitor，这里选 bcs-read 是因为：
//   - 语义上"跨 Pod 聚合日志"仍以 Pod 为轴，归到 bcs-read 更符合人直觉
//   - 避免因同名分组在不同 agent 下可见性差异导致的调试困惑
const TargetRead = "bcs-read"

// Result 本包工具统一返回结构，与 bk_tools/bcs_tools 的 Result 对齐但独立声明
// （为了不 import 其他 tool 包）。
type Result struct {
	OK      bool   `json:"ok"`
	Mock    bool   `json:"mock,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// NewAllTargeted 返回本包全部 TargetedTool。
//
// bkClient / bcsClient 为 nil 时自动调用各自的 NewClient()，便于单测与装配复用。
func NewAllTargeted(bkClient *bkapi.Client, bcsClient *bcsapi.Client) []tools.TargetedTool {
	if bkClient == nil {
		bkClient = bkapi.NewClient()
	}
	if bcsClient == nil {
		bcsClient = bcsapi.NewClient()
	}
	return []tools.TargetedTool{
		{Target: TargetRead, Tool: newLogsUnifiedTool(bkClient, bcsClient)},
	}
}

// NewReadOnly 便捷入口：仅返回 tool.Tool 列表（用于白名单注册场景）。
func NewReadOnly(bkClient *bkapi.Client, bcsClient *bcsapi.Client) []tool.Tool {
	if bkClient == nil {
		bkClient = bkapi.NewClient()
	}
	if bcsClient == nil {
		bcsClient = bcsapi.NewClient()
	}
	return []tool.Tool{
		newLogsUnifiedTool(bkClient, bcsClient),
	}
}
