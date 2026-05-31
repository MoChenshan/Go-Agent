// Package bcstools —— bcs_node_describe（Node 深度诊断，D24 新增，纯读）。
//
// # 为什么需要这个工具（诊断链自然延伸）
//
// D21 / D21.1 把诊断链拉到 Pod 层：
//
//	D21   bcs_pod_logs_tail   容器内日志（"应用自己报了什么错"）
//	D21.1 bcs_pod_describe    Pod 对象 + Events（"为什么 Pod 起不来"）
//
// 但真实 oncall 里还有一大类故障根因**根本不在 Pod 层**，而在它运行的 Node 上：
//
//	故障类型                       | Pod 层能看到吗？       | Node 层能看到吗？
//	-------------------------------+------------------------+-----------------------------
//	节点磁盘满导致无法拉镜像       | Events:ErrImagePull     | conditions[DiskPressure]=True
//	节点内存压力触发 OOM 驱逐        | Events:Evicted          | conditions[MemoryPressure]=True
//	节点 NotReady 导致 Pod 调度失败 | FailedScheduling       | conditions[Ready]=False
//	Taint 未被容忍导致全部 Pending | "didn't tolerate taint" | spec.taints[]（根因）
//	网络插件异常导致 CNI 分配失败  | Pod 卡 Pending          | conditions[NetworkUnavailable]=True
//	节点 kubelet 版本过旧          | （无直接信号）          | status.nodeInfo.kubeletVersion
//	IP 耗尽 / Pod 上限打满         | FailedCreatePodSandBox  | status.allocatable.pods vs 已用
//
// 这就是 `kubectl describe node` 的价值——**Pod 层走到死胡同时，往上一层看全景**。
//
// # 与现有诊断工具的三角分工
//
//	bcs_pod_describe    Pod 视角（"这个 Pod 怎么了"）
//	bcs_node_describe   Node 视角（"这台机器怎么了"）← 本工具
//	bcs_pod_logs_tail   容器内视角（"程序自己在喊什么"）
//
// 典型上升排查序列：
//
//	1. resource_query 发现一批 Pod Pending
//	2. pod_describe 看 Events 得知 "FailedScheduling: 0/5 nodes available"
//	3. node_describe 批量扫所有节点的 conditions + allocatable + taints
//	4. 定位到具体异常 Node → 联动 SRE 处理（drain/reboot/alarm）
//
// # 为什么只做 describe、不做 drain（D24 范围收敛）
//
// Node 层写操作（drain/cordon/uncordon）是**运维级高风险维护动作**：
//   - Blast radius 是 Pod 级 delete 的 N 倍（整节点所有 Pod 被驱逐）
//   - LLM 自主判断"该 drain 哪个 node"需要的上下文（节点容量 + Pod 分布 + PDB 链路）
//     显著高于当前成熟度
//   - 频次远低于诊断场景（游戏 oncall 里 drain 一周不到 1 次）
//
// 因此 D24 刻意只做只读侧的 describe，Node 写操作单独立项。
// 本工具的价值闭环：**让诊断链覆盖 Node 层，把 Pod 层"卡住看不见"的故障显化**。
package bcstools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// Node describe 硬上限。
//
//   - MaxNodesScan=50：单集群正常规模 10-200 节点；50 是 LLM 单次能良好消化的上限。
//     超出时返回错误提示用户先 resource_query 缩小范围，或指定 nodes[]。
//   - MaxNodesExplicit=20：显式 nodes[] 的最大长度（对齐 pod_describe 的批量约束）。
const (
	MaxNodesScan     = 50
	MaxNodesExplicit = 20
)

// NodeDescribeInput 为 bcs_node_describe 工具入参。
//
// 支持三种查询形态：
//  1. 指定单节点（node="node-01"）
//  2. 批量指定（nodes=["node-01","node-02"]）
//  3. 全量扫描（scan_all=true → LIST 所有节点后 describe）—— 小集群诊断利器
type NodeDescribeInput struct {
	ClusterID  string   `json:"cluster_id"  description:"BCS 集群 ID（必填）"`
	Node       string   `json:"node"        description:"节点名称（单节点；与 nodes 互斥，nodes 优先）"`
	Nodes      []string `json:"nodes"       description:"节点名称列表（批量场景）"`
	ScanAll    bool     `json:"scan_all"    description:"是否扫描集群全部节点（与 node/nodes 互斥；大集群慎用，上限 MaxNodesScan）"`
	OnlyIssues bool     `json:"only_issues" description:"是否仅返回检出 issues 的节点（过滤正常节点，缩小 LLM 上下文）"`
}

// NodeDescribeReport 单 Node 的完整诊断报告。
//
// JSON schema 稳定性说明：为便于 LLM 按固定 schema 解析，
// conditions/taints/issues 三个数组字段**不使用 omitempty**，
// 即使空也会以 [] 序列化。赋值点需保证不是 nil（看 normalizeNodeReport）。
type NodeDescribeReport struct {
	Name       string          `json:"name"`
	Summary    NodeSummary     `json:"summary"`
	Conditions []NodeCondition `json:"conditions"`
	Capacity   NodeCapacity    `json:"capacity"`
	Taints     []NodeTaint     `json:"taints"`
	Issues     []string        `json:"issues"`
	Error      string          `json:"error,omitempty"`
}

// NodeSummary "一眼看全"字段。
type NodeSummary struct {
	Roles            []string `json:"roles,omitempty"`
	Age              string   `json:"age"`
	Schedulable      bool     `json:"schedulable"`
	InternalIP       string   `json:"internal_ip,omitempty"`
	ExternalIP       string   `json:"external_ip,omitempty"`
	Hostname         string   `json:"hostname,omitempty"`
	KubeletVersion   string   `json:"kubelet_version,omitempty"`
	KernelVersion    string   `json:"kernel_version,omitempty"`
	OSImage          string   `json:"os_image,omitempty"`
	Arch             string   `json:"arch,omitempty"`
	ContainerRuntime string   `json:"container_runtime,omitempty"`
}

// NodeCondition K8s NodeCondition。
type NodeCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"last_transition_time,omitempty"`
}

// NodeCapacity capacity 与 allocatable 的并列展示。
//
// 两者差值反映系统预留是否合理；Pod 调度基于 allocatable。
type NodeCapacity struct {
	CPU              string `json:"cpu,omitempty"`
	Memory           string `json:"memory,omitempty"`
	Pods             string `json:"pods,omitempty"`
	EphemeralStorage string `json:"ephemeral_storage,omitempty"`

	AllocCPU              string `json:"alloc_cpu,omitempty"`
	AllocMemory           string `json:"alloc_memory,omitempty"`
	AllocPods             string `json:"alloc_pods,omitempty"`
	AllocEphemeralStorage string `json:"alloc_ephemeral_storage,omitempty"`
}

// NodeTaint 节点污点。
type NodeTaint struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Effect string `json:"effect"`
}

// newNodeDescribeTool 构造 bcs_node_describe 工具。
func newNodeDescribeTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in NodeDescribeInput) (*Result, error) {
		if in.ClusterID == "" {
			return nil, fmt.Errorf("cluster_id 为必填")
		}
		nodes, err := resolveNodeTargets(ctx, client, in)
		if err != nil {
			return nil, err
		}
		if len(nodes) == 0 {
			return nil, fmt.Errorf("未解析出任何节点（请提供 node / nodes / scan_all）")
		}

		isMock := client != nil && client.IsMock()
		reports := make([]NodeDescribeReport, 0, len(nodes))
		for _, n := range nodes {
			reports = append(reports, describeOneNode(ctx, client, in.ClusterID, n, isMock))
		}

		filtered := reports
		if in.OnlyIssues {
			filtered = filterIssueReports(reports)
		}

		okCount, failCount, issuesTotal := 0, 0, 0
		for _, r := range reports {
			if r.Error != "" {
				failCount++
			} else {
				okCount++
			}
			issuesTotal += len(r.Issues)
		}
		msg := fmt.Sprintf("成功 describe %d 个 Node，失败 %d，检出 issues 累计 %d 条",
			okCount, failCount, issuesTotal)
		if isMock {
			msg = "Mock 模式：" + msg
		}

		emitNodeDescribeAudit(client, in, nodes, reports, issuesTotal)

		return &Result{
			OK:      failCount == 0,
			Mock:    isMock,
			Message: msg,
			Data: map[string]any{
				"cluster_id":   in.ClusterID,
				"node_count":   len(nodes),
				"reports":      filtered,
				"issues_total": issuesTotal,
				"only_issues":  in.OnlyIssues,
				"filtered_out": len(reports) - len(filtered),
			},
		}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_node_describe"),
		function.WithDescription(
			"BCS Node 深度诊断工具（只读，等价 kubectl describe node）。返回结构化 Summary/Conditions/"+
				"Capacity/Taints/Issues。**Pod 层看不到的节点级故障**在这里定位：DiskPressure / MemoryPressure / "+
				"NetworkUnavailable / NotReady / Taint 未被容忍 / allocatable 不足等。"+
				"典型用法：1) Pod Pending 排查：看 conditions 和 taints；2) 节点容量评估：scan_all+only_issues=true "+
				"快速筛出所有异常节点；3) 单机深度排查：node=<name> 拿完整报告。"+
				"与 bcs_pod_describe 配合：pod 视角走到死胡同时往上追一层。"+
				"参数：node / nodes[] / scan_all 三选一；批量上限 20，scan_all 上限 50。",
		),
	)
}

// resolveNodeTargets 解析三种入参形态为最终节点名列表。
//
// 优先级：nodes[] > node > scan_all
func resolveNodeTargets(ctx context.Context, client *bcsapi.Client, in NodeDescribeInput) ([]string, error) {
	if len(in.Nodes) > 0 {
		if len(in.Nodes) > MaxNodesExplicit {
			return nil, fmt.Errorf("nodes[] 批量数量 %d 超过硬上限 %d；请拆分多次或使用 scan_all",
				len(in.Nodes), MaxNodesExplicit)
		}
		return in.Nodes, nil
	}
	if strings.TrimSpace(in.Node) != "" {
		return []string{in.Node}, nil
	}
	if in.ScanAll {
		return listAllNodes(ctx, client, in.ClusterID)
	}
	return nil, fmt.Errorf("必须提供 node / nodes / scan_all 三者之一")
}

// listAllNodes 扫描集群全部节点名；超过 MaxNodesScan 报错。
func listAllNodes(ctx context.Context, client *bcsapi.Client, clusterID string) ([]string, error) {
	if client != nil && client.IsMock() {
		return []string{"node-mock-01", "node-mock-02", "node-mock-03"}, nil
	}
	path := fmt.Sprintf("/bcsapi/v4/storage/k8s/dynamic/clusters/%s/nodes", clusterID)
	var resp map[string]any
	if err := client.Get(ctx, path, nil, &resp); err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			return []string{"node-mock-01"}, nil
		}
		return nil, fmt.Errorf("list nodes failed: %w", err)
	}
	items := getArray(resp, "data")
	if len(items) == 0 {
		items = getArray(resp, "items")
	}
	names := make([]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m == nil {
			continue
		}
		inner := getMap(m, "data")
		if inner == nil {
			inner = m
		}
		meta := getMap(inner, "metadata")
		name := getString(meta, "name")
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) > MaxNodesScan {
		return nil, fmt.Errorf("scan_all 检出 %d 个节点，超过硬上限 %d；请用 nodes[] 指定范围或先 resource_query 过滤",
			len(names), MaxNodesScan)
	}
	return names, nil
}

// describeOneNode 对单个 Node 做 describe。
//
// 失败隔离：单节点查询失败只写 report.Error，不中断批量（与 pod_describe 对称）。
func describeOneNode(ctx context.Context, client *bcsapi.Client,
	clusterID, node string, isMock bool) NodeDescribeReport {

	rpt := NodeDescribeReport{Name: node}

	if isMock {
		return normalizeNodeReport(mockNodeReport(node))
	}

	path := fmt.Sprintf("/bcsapi/v4/storage/k8s/dynamic/clusters/%s/nodes/%s", clusterID, node)
	var resp map[string]any
	if err := client.Get(ctx, path, nil, &resp); err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			rpt.Error = "mock mode"
			return normalizeNodeReport(rpt)
		}
		rpt.Error = fmt.Sprintf("get node failed: %v", err)
		return normalizeNodeReport(rpt)
	}
	fillNodeFromRaw(&rpt, resp)
	rpt.Issues = detectNodeIssues(&rpt)
	return normalizeNodeReport(rpt)
}

// normalizeNodeReport 把三个数组字段从 nil 提升为空切片，保证输出 schema 稳定。
//
// 为什么必要：JSON marshal nil slice 会产出 null，而 LLM（包括下游集成测试）
// 期待的是 "taints": [] 等空数组。这里统一修正，避免上游遗漏。
func normalizeNodeReport(r NodeDescribeReport) NodeDescribeReport {
	if r.Conditions == nil {
		r.Conditions = []NodeCondition{}
	}
	if r.Taints == nil {
		r.Taints = []NodeTaint{}
	}
	if r.Issues == nil {
		r.Issues = []string{}
	}
	return r
}

// mockNodeReport 为三种 Mock 节点构造不同态样（Ready / DiskPressure / NotReady）。
//
// 目的：让 E2E 测试与 LLM 演示能**直接看到 issues 检出工作**。
func mockNodeReport(node string) NodeDescribeReport {
	switch node {
	case "node-mock-02":
		return NodeDescribeReport{
			Name: node,
			Summary: NodeSummary{
				Roles: []string{"worker"}, Age: "15d6h", Schedulable: true,
				InternalIP: "10.0.0.12", KubeletVersion: "v1.24.10", Arch: "amd64",
				OSImage: "CentOS Linux 7 (Core)", ContainerRuntime: "containerd://1.6.21",
			},
			Conditions: []NodeCondition{
				{Type: "Ready", Status: "True"},
				{Type: "MemoryPressure", Status: "False"},
				{Type: "DiskPressure", Status: "True", Reason: "KubeletHasDiskPressure", Message: "kubelet has disk pressure"},
				{Type: "PIDPressure", Status: "False"},
				{Type: "NetworkUnavailable", Status: "False"},
			},
			Capacity: NodeCapacity{
				CPU: "8", Memory: "32Gi", Pods: "110", EphemeralStorage: "100Gi",
				AllocCPU: "7800m", AllocMemory: "30Gi", AllocPods: "110", AllocEphemeralStorage: "95Gi",
			},
			Issues: []string{"condition[DiskPressure]=True：节点磁盘压力过大，可能导致镜像拉取失败或 Pod 被驱逐"},
		}
	case "node-mock-03":
		return NodeDescribeReport{
			Name: node,
			Summary: NodeSummary{
				Roles: []string{"worker"}, Age: "30d2h", Schedulable: false,
				InternalIP: "10.0.0.13", KubeletVersion: "v1.24.10", Arch: "amd64",
				OSImage: "CentOS Linux 7 (Core)", ContainerRuntime: "containerd://1.6.21",
			},
			Conditions: []NodeCondition{
				{Type: "Ready", Status: "False", Reason: "KubeletNotReady", Message: "PLEG is not healthy"},
			},
			Capacity: NodeCapacity{
				CPU: "8", Memory: "32Gi", Pods: "110",
				AllocCPU: "7800m", AllocMemory: "30Gi", AllocPods: "110",
			},
			Taints: []NodeTaint{
				{Key: "node.kubernetes.io/unschedulable", Effect: "NoSchedule"},
			},
			Issues: []string{
				"condition[Ready]=False：节点未就绪（KubeletNotReady）",
				"spec.unschedulable=true：节点已被禁止调度（可能正在维护）",
			},
		}
	default:
		return NodeDescribeReport{
			Name: node,
			Summary: NodeSummary{
				Roles: []string{"worker"}, Age: "10d3h", Schedulable: true,
				InternalIP: "10.0.0.11", KubeletVersion: "v1.24.10", Arch: "amd64",
				OSImage: "CentOS Linux 7 (Core)", ContainerRuntime: "containerd://1.6.21",
			},
			Conditions: []NodeCondition{
				{Type: "Ready", Status: "True"},
				{Type: "MemoryPressure", Status: "False"},
				{Type: "DiskPressure", Status: "False"},
				{Type: "PIDPressure", Status: "False"},
			},
			Capacity: NodeCapacity{
				CPU: "8", Memory: "32Gi", Pods: "110",
				AllocCPU: "7800m", AllocMemory: "30Gi", AllocPods: "110",
			},
		}
	}
}

// fillNodeFromRaw 把原始 K8s Node JSON 填充到 NodeDescribeReport。
//
// 与 pod_describe.fillPodFromRaw 同构：不做 struct Unmarshal，用 map 按路径取。
func fillNodeFromRaw(rpt *NodeDescribeReport, raw map[string]any) {
	node := getMap(raw, "data")
	if node == nil {
		node = raw
	}
	meta := getMap(node, "metadata")
	spec := getMap(node, "spec")
	status := getMap(node, "status")

	// ---- Summary ----
	rpt.Summary.Age = humanAge(getString(meta, "creationTimestamp"))
	rpt.Summary.Schedulable = !getBool(spec, "unschedulable")
	rpt.Summary.Roles = extractNodeRoles(getMap(meta, "labels"))

	addrs := getArray(status, "addresses")
	for _, a := range addrs {
		am, _ := a.(map[string]any)
		if am == nil {
			continue
		}
		switch getString(am, "type") {
		case "InternalIP":
			rpt.Summary.InternalIP = getString(am, "address")
		case "ExternalIP":
			rpt.Summary.ExternalIP = getString(am, "address")
		case "Hostname":
			rpt.Summary.Hostname = getString(am, "address")
		}
	}

	info := getMap(status, "nodeInfo")
	rpt.Summary.KubeletVersion = getString(info, "kubeletVersion")
	rpt.Summary.KernelVersion = getString(info, "kernelVersion")
	rpt.Summary.OSImage = getString(info, "osImage")
	rpt.Summary.Arch = getString(info, "architecture")
	rpt.Summary.ContainerRuntime = getString(info, "containerRuntimeVersion")

	// ---- Conditions ----
	conditions := getArray(status, "conditions")
	for _, c := range conditions {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		rpt.Conditions = append(rpt.Conditions, NodeCondition{
			Type:               getString(cm, "type"),
			Status:             getString(cm, "status"),
			Reason:             getString(cm, "reason"),
			Message:            getString(cm, "message"),
			LastTransitionTime: getString(cm, "lastTransitionTime"),
		})
	}

	// ---- Capacity & Allocatable ----
	capMap := getMap(status, "capacity")
	alloc := getMap(status, "allocatable")
	rpt.Capacity = NodeCapacity{
		CPU:                   getString(capMap, "cpu"),
		Memory:                getString(capMap, "memory"),
		Pods:                  getString(capMap, "pods"),
		EphemeralStorage:      getString(capMap, "ephemeral-storage"),
		AllocCPU:              getString(alloc, "cpu"),
		AllocMemory:           getString(alloc, "memory"),
		AllocPods:             getString(alloc, "pods"),
		AllocEphemeralStorage: getString(alloc, "ephemeral-storage"),
	}

	// ---- Taints ----
	taints := getArray(spec, "taints")
	for _, t := range taints {
		tm, _ := t.(map[string]any)
		if tm == nil {
			continue
		}
		rpt.Taints = append(rpt.Taints, NodeTaint{
			Key:    getString(tm, "key"),
			Value:  getString(tm, "value"),
			Effect: getString(tm, "effect"),
		})
	}
}

// extractNodeRoles 从 labels 中提取角色（K8s 约定 node-role.kubernetes.io/<role>）。
func extractNodeRoles(labels map[string]any) []string {
	if labels == nil {
		return nil
	}
	const prefix = "node-role.kubernetes.io/"
	var roles []string
	for k := range labels {
		if strings.HasPrefix(k, prefix) {
			role := strings.TrimPrefix(k, prefix)
			if role != "" {
				roles = append(roles, role)
			}
		}
	}
	return roles
}

// detectNodeIssues 自动检出节点异常清单（人类可读文案）。
//
// 检出规则：
//  1. Ready=False/Unknown          → 节点异常
//  2. 任一 Pressure condition=True → 资源压力
//  3. NetworkUnavailable=True      → CNI 异常
//  4. spec.unschedulable=true      → 禁止调度
//
// 原则：taint 是正常 K8s 语义，不升级为 issue（仅放进 Taints 段）。
func detectNodeIssues(rpt *NodeDescribeReport) []string {
	var issues []string
	for _, c := range rpt.Conditions {
		switch c.Type {
		case "Ready":
			if c.Status != "True" {
				issues = append(issues, fmt.Sprintf("condition[Ready]=%s：节点未就绪（%s）",
					c.Status, firstNonEmpty(c.Reason, c.Message, "unknown")))
			}
		case "MemoryPressure", "DiskPressure", "PIDPressure":
			if c.Status == "True" {
				hint := map[string]string{
					"MemoryPressure": "内存压力过大，可能触发 Pod 驱逐",
					"DiskPressure":   "节点磁盘压力过大，可能导致镜像拉取失败或 Pod 被驱逐",
					"PIDPressure":    "进程数压力过大，可能无法创建新 Pod",
				}[c.Type]
				issues = append(issues, fmt.Sprintf("condition[%s]=True：%s", c.Type, hint))
			}
		case "NetworkUnavailable":
			if c.Status == "True" {
				issues = append(issues, "condition[NetworkUnavailable]=True：节点网络插件异常，Pod 可能无法获得 IP")
			}
		}
	}
	if !rpt.Summary.Schedulable {
		issues = append(issues, "spec.unschedulable=true：节点已被禁止调度（可能正在维护）")
	}
	return issues
}

// filterIssueReports 按 OnlyIssues=true 过滤。
//
// 保留条件：Error!="" 或 len(Issues)>0；若过滤后空，保留首条作为"一切正常"证据。
func filterIssueReports(reports []NodeDescribeReport) []NodeDescribeReport {
	kept := make([]NodeDescribeReport, 0, len(reports))
	for _, r := range reports {
		if r.Error != "" || len(r.Issues) > 0 {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 && len(reports) > 0 {
		return reports[:1]
	}
	return kept
}

// emitNodeDescribeAudit node describe 审计入账。
func emitNodeDescribeAudit(client *bcsapi.Client, in NodeDescribeInput,
	nodes []string, reports []NodeDescribeReport, issuesTotal int) {
	errCnt := 0
	for _, r := range reports {
		if r.Error != "" {
			errCnt++
		}
	}
	audit.Emit(audit.Event{
		Agent:    "diagnosis_agent",
		Action:   "bcs.node.describe",
		Severity: "Info",
		Target:   fmt.Sprintf("%s (nodes=%d)", in.ClusterID, len(nodes)),
		Params: map[string]any{
			"cluster_id":   in.ClusterID,
			"nodes":        nodes,
			"scan_all":     in.ScanAll,
			"only_issues":  in.OnlyIssues,
			"issues_total": issuesTotal,
			"errors":       errCnt,
		},
		Success: errCnt == 0,
		Mock:    client != nil && client.IsMock(),
	})
}
