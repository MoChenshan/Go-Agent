// Package bcstools 实现 13 个 BCS（蓝鲸容器服务）相关的 FunctionTool。
//
// 设计划分（按读/写职责）：
//   - bcs-read  → DiagnosisAgent（诊断只读）：
//       - bcs_project_query   项目元数据
//       - bcs_cluster_query   集群列表/详情
//       - bcs_resource_query  K8s 资源查询
//       - bcs_pod_logs_tail   Pod 日志拉取（D21，纯读，诊断链核心砖）
//       - bcs_pod_describe    Pod 深度诊断（D21.1，Events+结构化 Summary，容器外故障定位）
//       - bcs_node_describe   Node 深度诊断（D24，Conditions/Capacity/Taints/Issues，节点级故障定位）
//
//   - bcs-write → RepairAgent（修复写操作，全部 HITL 两段式确认）：
//       - bcs_helm_manage       Helm Release 部署/回滚（D1-D7）
//       - bcs_scale_deployment  Deployment 副本伸缩（D18.1）
//       - bcs_pod_restart       Pod 级重启/滚动/驱逐（D18.2）
//       - bcs_configmap_update  ConfigMap 热更/快照/回滚（D18.4，D18 收尾）
//       - bcs_secret_update     Secret 热更（D22，D18.4 敏感键兜底闭环，base64 自动编码+脱敏审计）
//       - bcs_hpa_patch         HPA 写操作（改 min/max/冻结）（D20.2）
//       - bcs_network_update    网络层统一更新（Service/Ingress 的 patch，D25）
//
// 所有工具通过 infrastructure/bcsapi.Client 发起请求，未配置 BCS_GATEWAY_URL / BCS_TOKEN
// 时自动走 Mock 模式，返回预置样例便于本地开发。
package bcstools

import (
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools"
)

// Target 常量定义。
//
// TargetRead  只读类 BCS 工具分组名（诊断使用）。
// TargetWrite 写操作类 BCS 工具分组名（修复使用，需 HITL）。
const (
	TargetRead  = "bcs-read"
	TargetWrite = "bcs-write"
)

// Result BCS 工具统一返回结构。
type Result struct {
	OK      bool   `json:"ok"`
	Mock    bool   `json:"mock,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// NewAllTargeted 返回 13 个 BCS FunctionTool，已按 target 分组。
//
// client 为 nil 时自动构造。
//
// 关于 ReadyWaiter：本函数内部三个写工具（helm/scale/pod_restart）都会走默认的
// `NewBCSReadyWaiter`。若装配层希望注入自选实现（如 D19.8 FastPollReadyWaiter 或 Noop），
// 请改用 NewAllTargetedWithWaiter。这是"抽象可替换性"的装配入口——
// 上层业务工具代码一行不改，底层 Waiter 实现通过此入口全量切换。
func NewAllTargeted(client *bcsapi.Client) []tools.TargetedTool {
	if client == nil {
		client = bcsapi.NewClient()
	}
	return wrapMetrics([]tools.TargetedTool{
		{Target: TargetRead, Tool: newProjectTool(client)},
		{Target: TargetRead, Tool: newClusterTool(client)},
		{Target: TargetRead, Tool: newResourceTool(client)},
		{Target: TargetRead, Tool: newPodLogsTailTool(client)}, // D21：Pod 日志拉取（诊断链）
		{Target: TargetRead, Tool: newPodDescribeTool(client)}, // D21.1：Pod 深度诊断（Events+结构化 Summary）
		{Target: TargetRead, Tool: newNodeDescribeTool(client)}, // D24：Node 深度诊断（Conditions/Capacity/Taints/Issues）
		{Target: TargetWrite, Tool: newHelmTool(client)},
		{Target: TargetWrite, Tool: newScaleTool(client)},
		{Target: TargetWrite, Tool: newPodRestartTool(client)},
		{Target: TargetWrite, Tool: newConfigmapUpdateTool(client)},
		{Target: TargetWrite, Tool: newSecretUpdateTool(client)}, // D22：Secret 热更（D18.4 敏感键兜底闭环）
		{Target: TargetWrite, Tool: newHPAPatchTool(client)},
		{Target: TargetWrite, Tool: newNetworkUpdateTool(client)}, // D25：网络层统一更新（Service/Ingress patch）
	})
}

// NewAllTargetedWithWaiter 与 NewAllTargeted 功能完全一致，但允许装配层注入一个
// 自选的 ReadyWaiter 实现（D19.8 新增）。
//
// 为什么需要这个入口：
//
//	D19.7 毕业考证明了 ReadyWaiter 接口在 3 个异形场景下都无需改动。
//	D19.8 要证明"接口毕业"的含金量——**底层实现真的可以被替换**。
//	本函数就是替换入口：装配层可以从 env 决定实例，或者测试时注入 Noop。
//
// 关键事实：
//
//   - 本函数是"抽象可替换性"在**装配层的唯一触点**
//   - 三个写工具（helm/scale/pod_restart）的源代码**不因此改动**
//   - 所有单测继续用 newXxxToolWithWaiter 注入 fakeWaiter，也不受影响
//
// waiter 为 nil 时退化成 NewAllTargeted 的行为（用默认 BCS 轮询实现）。
func NewAllTargetedWithWaiter(client *bcsapi.Client, waiter ReadyWaiter) []tools.TargetedTool {
	if client == nil {
		client = bcsapi.NewClient()
	}
	// waiter=nil 时退化成默认实现，保证向后兼容
	if waiter == nil {
		return NewAllTargeted(client)
	}
	return wrapMetrics([]tools.TargetedTool{
		{Target: TargetRead, Tool: newProjectTool(client)},
		{Target: TargetRead, Tool: newClusterTool(client)},
		{Target: TargetRead, Tool: newResourceTool(client)},
		{Target: TargetRead, Tool: newPodLogsTailTool(client)}, // D21：Pod 日志拉取（诊断链）
		{Target: TargetRead, Tool: newPodDescribeTool(client)}, // D21.1：Pod 深度诊断（Events+结构化 Summary）
		{Target: TargetRead, Tool: newNodeDescribeTool(client)}, // D24：Node 深度诊断（Conditions/Capacity/Taints/Issues）
		{Target: TargetWrite, Tool: newHelmToolWithWaiter(client, waiter)},
		{Target: TargetWrite, Tool: newScaleToolWithWaiter(client, waiter)},
		{Target: TargetWrite, Tool: newPodRestartToolWithWaiter(client, waiter)},
		{Target: TargetWrite, Tool: newConfigmapUpdateTool(client)}, // 该工具未接入 Waiter（配置变更不走 ready 等待）
		{Target: TargetWrite, Tool: newSecretUpdateTool(client)},    // D22：Secret 热更（同上，不接 Waiter）
		{Target: TargetWrite, Tool: newHPAPatchTool(client)},           // D20.2：HPA 写操作不需 ready 等待（生效是秒级的）
		{Target: TargetWrite, Tool: newNetworkUpdateTool(client)},      // D25：网络层统一更新（Service/Ingress patch，不接 Waiter）
	})
}

// NewReadOnly 仅返回诊断用（只读）BCS 工具的 tool.Tool 列表。
func NewReadOnly(client *bcsapi.Client) []tool.Tool {
	if client == nil {
		client = bcsapi.NewClient()
	}
	return []tool.Tool{
		newProjectTool(client),
		newClusterTool(client),
		newResourceTool(client),
		newPodLogsTailTool(client),
		newPodDescribeTool(client),
		newNodeDescribeTool(client),
	}
}

// wrapMetrics 对 TargetedTool 列表里的每个工具做 WithMetrics 包装（D28）。
//
// 工具名从 Declaration().Name 自动获取，无需手动传入。
// 若某个工具不实现 tool.CallableTool（理论上不应发生），则跳过包装。
func wrapMetrics(list []tools.TargetedTool) []tools.TargetedTool {
	for i, tt := range list {
		ct, ok := tt.Tool.(tool.CallableTool)
		if !ok {
			continue
		}
		name := ct.Declaration().Name
		list[i].Tool = WithMetrics(ct, name)
	}
	return list
}

// NewWriteOnly 仅返回修复用（写）BCS 工具的 tool.Tool 列表。
func NewWriteOnly(client *bcsapi.Client) []tool.Tool {
	if client == nil {
		client = bcsapi.NewClient()
	}
	return []tool.Tool{
		newHelmTool(client),
		newScaleTool(client),
		newPodRestartTool(client),
		newConfigmapUpdateTool(client),
		newSecretUpdateTool(client),
		newHPAPatchTool(client),
		newNetworkUpdateTool(client),
	}
}
