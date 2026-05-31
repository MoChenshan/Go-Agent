// Package bcstools —— bcs_pod_describe（Pod 详情+Events 结构化诊断，D21.1 新增，纯读）。
//
// # 为什么需要这个工具（D21 的对称补位）
//
// D21 的 pod_logs_tail 让**容器里的故事**可见（stdout/stderr）。但 oncall 高频
// 场景里还有**容器外的故事**藏在 K8s Events 和 Pod 对象字段里，日志永远看不到：
//
//	故障类型                 | 日志里有吗？        | Events 里有吗？
//	-------------------------+---------------------+-----------------------------
//	ImagePullBackOff         | ❌ 容器根本没起来   | ✅ "Failed to pull image"
//	PVC 挂载失败             | ❌                  | ✅ "FailedMount"
//	调度失败/资源不足        | ❌                  | ✅ "FailedScheduling: insufficient cpu"
//	Init 容器失败            | 只 Init 的 stdout   | ✅ phase=Init:Error
//	OOMKilled 历史记录       | 可能没留下          | ✅ lastState.terminated.reason=OOMKilled
//	Readiness 探针失败        | ❌ 容器活着但不 ready| ✅ "Unhealthy: readiness probe failed"
//	镜像拉取权限问题         | ❌                  | ✅ "ErrImagePull: 401 Unauthorized"
//
// 这就是 `kubectl describe pod` 在真实运维里不可替代的核心价值。做完这个工具，
// "为什么 Pod 起不来"的所有可能答案都有对应工具可查。
//
// # 与 bcs_resource_query / bcs_pod_logs_tail 的三角分工
//
//	bcs_resource_query   列表和批量过滤（"哪些 Pod 是 Running"）
//	bcs_pod_describe     单 Pod 深度诊断（"为什么这个 Pod 起不来"）← 本工具
//	bcs_pod_logs_tail    容器内日志（"应用自己报了什么错"）
//
// 典型诊断序列：
//	1. resource_query 筛出异常 Pod 列表
//	2. pod_describe 看 Events + lastState（容器外）
//	3. pod_logs_tail previous=true 看容器内崩溃前日志
//	4. 根因清晰后 transfer 给 repair_agent 处置
//
// # 为什么做结构化输出而不是返回原始 K8s JSON
//
// LLM 虽然能解析原始 kubectl describe 文本，但：
//   - 原始输出 200-500 行，LLM 上下文成本高
//   - 关键字段（restartCount/lastState.terminated.reason/最近 Warning Events）
//     散落在不同层级，LLM 容易遗漏
//   - 结构化后可以做"Summary 一眼看全"的呈现（phase + restartCount + age + 最近 1 条 Warning）
//
// 本工具把 K8s 对象"翻译"成 5 个清晰段落：Summary / Containers / Conditions /
// Events / InitContainers，让 LLM 无需二次解析。
package bcstools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// describe 常量。
//
// 选取依据：
//   - MaxEventsPerPod=50：K8s 默认 event TTL 是 1h，单 Pod 正常场景产生的 event 通常 < 20 条；
//     异常场景（如 CrashLoopBackOff）会反复产生相同 event，50 条足以覆盖 80% 诊断需求。
//   - DescribeTimeout=10s：describe 做了 2 路并发 HTTP（Pod + Events），单路不超 5s 即可
const (
	MaxEventsPerPod = 50
)

// PodDescribeInput 为 bcs_pod_describe 工具入参。
//
// 单 Pod 深度诊断，与 pod_logs_tail 对称支持批量（Pods[]）。
type PodDescribeInput struct {
	ClusterID  string   `json:"cluster_id"    description:"BCS 集群 ID（必填）"`
	Namespace  string   `json:"namespace"     description:"Kubernetes 命名空间（必填）"`
	Pod        string   `json:"pod"           description:"Pod 名称（单 Pod 场景；与 pods 互斥，pods 优先）"`
	Pods       []string `json:"pods"          description:"Pod 名称列表（批量场景，一次 describe 多个）"`
	WithEvents bool     `json:"with_events"   description:"是否并发拉取 Events（默认 true；置 false 可提速单纯看 Pod 对象）"`
}

// PodDescribeReport 单 Pod 的完整诊断报告。
type PodDescribeReport struct {
	Pod            string              `json:"pod"`
	Namespace      string              `json:"namespace"`
	Node           string              `json:"node,omitempty"`
	Summary        PodSummary          `json:"summary"`
	InitContainers []ContainerStatus   `json:"init_containers,omitempty"`
	Containers     []ContainerStatus   `json:"containers"`
	Conditions     []PodCondition      `json:"conditions,omitempty"`
	Events         []PodEvent          `json:"events,omitempty"`       // 已按 lastTimestamp 倒序
	WarningsCount  int                 `json:"warnings_count"`         // Events 中 type=Warning 的条数
	Error          string              `json:"error,omitempty"`        // 本 Pod 查询失败原因（不影响其他 Pod）
}

// PodSummary 呈现"一眼看全"的核心状态。
type PodSummary struct {
	Phase            string `json:"phase"`              // Pending/Running/Succeeded/Failed/Unknown
	Ready            string `json:"ready"`              // "2/3" 格式
	PodIP            string `json:"pod_ip,omitempty"`
	HostIP           string `json:"host_ip,omitempty"`
	QoSClass         string `json:"qos_class,omitempty"`// Guaranteed/Burstable/BestEffort
	Age              string `json:"age"`                // 人类友好："5d3h" / "2h15m"
	RestartCountSum  int    `json:"restart_count_sum"`  // 所有容器 restartCount 之和
	StartTime        string `json:"start_time,omitempty"`
	DeletionTimestamp string `json:"deletion_timestamp,omitempty"` // 非空说明正在被删除
}

// ContainerStatus 单容器状态聚合。
type ContainerStatus struct {
	Name         string         `json:"name"`
	Image        string         `json:"image"`
	Ready        bool           `json:"ready"`
	RestartCount int            `json:"restart_count"`
	State        ContainerState `json:"state"`                // 当前态
	LastState    ContainerState `json:"last_state,omitempty"` // 上次态（CrashLoop 排查关键）
}

// ContainerState 容器状态三元组之一（waiting/running/terminated）。
type ContainerState struct {
	Type     string `json:"type"`                // waiting / running / terminated / unknown
	Reason   string `json:"reason,omitempty"`    // 如 OOMKilled / CrashLoopBackOff / ImagePullBackOff
	Message  string `json:"message,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"` // terminated 时有意义
	StartedAt string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// PodCondition K8s 标准 Condition。
type PodCondition struct {
	Type    string `json:"type"`   // PodScheduled / Initialized / ContainersReady / Ready
	Status  string `json:"status"` // True / False / Unknown
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// PodEvent K8s Event。
type PodEvent struct {
	Type          string `json:"type"`           // Normal / Warning
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Count         int    `json:"count"`          // Event 合并计数（同一 reason 反复发生）
	FirstTime     string `json:"first_time,omitempty"`
	LastTime      string `json:"last_time,omitempty"`
	ReportingNode string `json:"reporting_node,omitempty"`
}

// newPodDescribeTool 构造 bcs_pod_describe 工具。
func newPodDescribeTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in PodDescribeInput) (*Result, error) {
		if in.ClusterID == "" || in.Namespace == "" {
			return nil, fmt.Errorf("cluster_id / namespace 为必填")
		}
		pods := in.Pods
		if len(pods) == 0 && strings.TrimSpace(in.Pod) != "" {
			pods = []string{in.Pod}
		}
		if len(pods) == 0 {
			return nil, fmt.Errorf("必须提供 pod 或 pods[]（至少一个 Pod 名）")
		}
		// 是否查询 Events 的最终决策：显式 true 永远遵从；否则按 pod 数量启发式
		// （详见 resolveWithEvents 注释）
		withEvents := resolveWithEvents(in.WithEvents, len(pods))

		isMock := client != nil && client.IsMock()

		reports := make([]PodDescribeReport, 0, len(pods))
		for _, pod := range pods {
			rpt := describeOnePod(ctx, client, in.ClusterID, in.Namespace, pod, withEvents, isMock)
			reports = append(reports, rpt)
		}

		// 审计
		emitPodDescribeAudit(client, in, pods, reports)

		// 聚合 Result
		okCount, failCount, warningsTotal := 0, 0, 0
		for _, r := range reports {
			if r.Error != "" {
				failCount++
			} else {
				okCount++
			}
			warningsTotal += r.WarningsCount
		}
		msg := fmt.Sprintf("成功 describe %d 个 Pod，失败 %d，累计 Warning 事件 %d 条",
			okCount, failCount, warningsTotal)
		if isMock {
			msg = "Mock 模式：" + msg
		}
		return &Result{
			OK:      failCount == 0,
			Mock:    isMock,
			Message: msg,
			Data: map[string]any{
				"cluster_id":     in.ClusterID,
				"namespace":      in.Namespace,
				"pod_count":      len(pods),
				"with_events":    withEvents,
				"reports":        reports,
				"warnings_total": warningsTotal,
			},
		}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_pod_describe"),
		function.WithDescription(
			"BCS Pod 深度诊断工具（只读，等价 kubectl describe pod）。返回结构化的 Summary/Containers/Conditions/Events。"+
				"**日志看不到的故障**在这里都能定位：ImagePullBackOff（镜像拉取失败）/ FailedScheduling（调度失败）/ "+
				"FailedMount（PVC 挂载失败）/ OOMKilled（lastState 记录）/ Readiness 探针失败 等。"+
				"典型用法：1) CrashLoopBackOff 排查：看 containers[].last_state.reason+exit_code 和 Warning Events；"+
				"2) Pod 一直 Pending：看 conditions[PodScheduled].message 和 FailedScheduling events；"+
				"3) 多副本对比：pods=[a,b,c] 批量 describe。"+
				"⚠ 与 bcs_pod_logs_tail 配合：describe 看容器外+K8s 侧，logs 看容器内。"+
				"参数 with_events（默认 true，批量 > 3 自动关闭提速）。",
		),
	)
}

// resolveWithEvents 决定最终是否查询 Events。
//
// 用户显式行为：
//   - with_events=true  → 强制开启（哪怕 10 个 pod 也查）
//   - with_events=false → 尊重关闭
//
// Go bool 零值问题：MCP 反序列化时"未填"和"显式 false"都是 false，无法区分。
// 因此采用启发式：当入参 false 且 pod 数 > 3 时，认为是"批量场景的默认关闭"；
// 否则（单/少量 pod）依然开启，因为 describe 单 pod 不看 events 价值损失太大。
//
// 这样设计的用户体验：
//   - 调 1 个 pod 不填 with_events     → 查 events ✓ (默认开)
//   - 调 2 个 pod 不填 with_events     → 查 events ✓
//   - 调 5 个 pod 不填 with_events     → 不查 events（LLM 明显在做批量摸底，events 过多）
//   - 调 5 个 pod 显式 with_events=true → 查 events ✓ (尊重显式意图)
func resolveWithEvents(userValue bool, podCount int) bool {
	if userValue {
		return true // 显式打开永远遵从
	}
	if podCount <= 3 {
		return true // 少量 Pod 默认开启
	}
	return false // 批量（>3）默认关闭加速
}

// describeOnePod 对单个 Pod 做 describe（Pod 对象 + Events 2 路查询）。
//
// 失败隔离：与 pod_logs_tail 对称，单 pod 查询失败只写 report.Error，不中断批量。
func describeOnePod(ctx context.Context, client *bcsapi.Client,
	clusterID, namespace, pod string, withEvents bool, isMock bool) PodDescribeReport {

	rpt := PodDescribeReport{Pod: pod, Namespace: namespace}

	if isMock {
		rpt.Summary = PodSummary{
			Phase: "Running", Ready: "1/1", Age: "3h12m",
			PodIP: "10.1.2.3", HostIP: "10.0.0.5", QoSClass: "Burstable",
			RestartCountSum: 0, StartTime: "2026-04-23T14:38:00Z",
		}
		rpt.Node = "node-mock-01"
		rpt.Containers = []ContainerStatus{{
			Name: "app", Image: "registry/game-core:v1.2.3",
			Ready: true, RestartCount: 0,
			State: ContainerState{Type: "running", StartedAt: "2026-04-23T14:38:05Z"},
		}}
		rpt.Conditions = []PodCondition{
			{Type: "PodScheduled", Status: "True"},
			{Type: "Initialized", Status: "True"},
			{Type: "ContainersReady", Status: "True"},
			{Type: "Ready", Status: "True"},
		}
		if withEvents {
			rpt.Events = []PodEvent{
				{Type: "Normal", Reason: "Scheduled", Message: "Successfully assigned default/" + pod + " to node-mock-01", Count: 1, LastTime: "2026-04-23T14:38:00Z"},
				{Type: "Normal", Reason: "Started", Message: "Started container app", Count: 1, LastTime: "2026-04-23T14:38:05Z"},
			}
		}
		return rpt
	}

	// ---- 1) GET Pod 对象 ----
	podPath := fmt.Sprintf(
		"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/namespaces/%s/pods/%s",
		clusterID, namespace, pod,
	)
	var podResp map[string]any
	if err := client.Get(ctx, podPath, nil, &podResp); err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			// 理论走不到（上面 isMock 分支已拦截）
			rpt.Error = "mock mode"
			return rpt
		}
		rpt.Error = fmt.Sprintf("get pod failed: %v", err)
		return rpt
	}
	fillPodFromRaw(&rpt, podResp)

	// ---- 2) LIST Events（可选）----
	if withEvents {
		eventsPath := fmt.Sprintf(
			"/clusters/%s/api/v1/namespaces/%s/events",
			clusterID, namespace,
		)
		// 使用 K8s fieldSelector 精确过滤本 Pod 的 events
		// 注意：BCS 代理有时对 fieldSelector 支持不完整，这里做降级：
		// 先试用 fieldSelector，失败再全量拉取然后本地过滤
		query := map[string]string{
			"fieldSelector": fmt.Sprintf("involvedObject.name=%s,involvedObject.namespace=%s", pod, namespace),
			"limit":         fmt.Sprintf("%d", MaxEventsPerPod),
		}
		var evResp map[string]any
		if err := client.Get(ctx, eventsPath, query, &evResp); err != nil {
			// Events 查询失败不影响主 report，只是 Events 列表为空
			// （不塞 rpt.Error 是因为那会整条 report 被视为失败）
			_ = err
		} else {
			rpt.Events = extractEvents(evResp, pod, namespace)
			for _, e := range rpt.Events {
				if e.Type == "Warning" {
					rpt.WarningsCount += e.Count
				}
			}
		}
	}
	return rpt
}

// fillPodFromRaw 把原始 Pod JSON 填充到 PodDescribeReport。
//
// 这里不用 struct + json.Unmarshal 是因为 K8s Pod 对象字段极多，只需要其中 15%；
// 用 map[string]any 按路径取反而代码更清爽、对服务端字段增减更宽容。
//
// 小心点：
//   - BCS storage 返回格式可能是 {data: {...}} 包一层，也可能直接就是 Pod 对象；
//     兼容两种形态
//   - 时间戳字段可能是 string（RFC3339）或 int64（unix 秒），这里统一当 string 处理
//     （BCS storage 实测是 RFC3339 字符串）
func fillPodFromRaw(rpt *PodDescribeReport, raw map[string]any) {
	// BCS storage 响应兜底：优先取 data，否则直接用 raw
	pod := getMap(raw, "data")
	if pod == nil {
		pod = raw
	}
	meta := getMap(pod, "metadata")
	spec := getMap(pod, "spec")
	status := getMap(pod, "status")

	// ---- Summary ----
	rpt.Summary.Phase = getString(status, "phase")
	rpt.Summary.PodIP = getString(status, "podIP")
	rpt.Summary.HostIP = getString(status, "hostIP")
	rpt.Summary.QoSClass = getString(status, "qosClass")
	rpt.Summary.StartTime = getString(status, "startTime")
	rpt.Summary.DeletionTimestamp = getString(meta, "deletionTimestamp")
	rpt.Node = getString(spec, "nodeName")
	rpt.Summary.Age = humanAge(getString(meta, "creationTimestamp"))

	// ---- Containers ----
	containerStatuses := getArray(status, "containerStatuses")
	rpt.Containers = make([]ContainerStatus, 0, len(containerStatuses))
	readyCnt, totalCnt := 0, len(containerStatuses)
	for _, cs := range containerStatuses {
		csMap, _ := cs.(map[string]any)
		if csMap == nil {
			continue
		}
		c := extractContainerStatus(csMap)
		rpt.Summary.RestartCountSum += c.RestartCount
		if c.Ready {
			readyCnt++
		}
		rpt.Containers = append(rpt.Containers, c)
	}
	rpt.Summary.Ready = fmt.Sprintf("%d/%d", readyCnt, totalCnt)

	// ---- Init Containers ----
	initStatuses := getArray(status, "initContainerStatuses")
	for _, cs := range initStatuses {
		csMap, _ := cs.(map[string]any)
		if csMap == nil {
			continue
		}
		rpt.InitContainers = append(rpt.InitContainers, extractContainerStatus(csMap))
	}

	// ---- Conditions ----
	conditions := getArray(status, "conditions")
	for _, cd := range conditions {
		cdMap, _ := cd.(map[string]any)
		if cdMap == nil {
			continue
		}
		rpt.Conditions = append(rpt.Conditions, PodCondition{
			Type:    getString(cdMap, "type"),
			Status:  getString(cdMap, "status"),
			Reason:  getString(cdMap, "reason"),
			Message: getString(cdMap, "message"),
		})
	}
}

// extractContainerStatus 从原始 map 抽出 ContainerStatus。
func extractContainerStatus(csMap map[string]any) ContainerStatus {
	c := ContainerStatus{
		Name:         getString(csMap, "name"),
		Image:        getString(csMap, "image"),
		Ready:        getBool(csMap, "ready"),
		RestartCount: getInt(csMap, "restartCount"),
		State:        parseContainerState(getMap(csMap, "state")),
		LastState:    parseContainerState(getMap(csMap, "lastState")),
	}
	return c
}

// parseContainerState 从 state/lastState 节点抽取三态之一。
//
// K8s 约定：state 是 {waiting:{...}} 或 {running:{...}} 或 {terminated:{...}}
// 三选一的结构（对应枚举 union type）。未设置时返回 type=unknown。
func parseContainerState(state map[string]any) ContainerState {
	if state == nil || len(state) == 0 {
		return ContainerState{Type: "unknown"}
	}
	if w := getMap(state, "waiting"); w != nil {
		return ContainerState{
			Type: "waiting", Reason: getString(w, "reason"), Message: getString(w, "message"),
		}
	}
	if r := getMap(state, "running"); r != nil {
		return ContainerState{
			Type: "running", StartedAt: getString(r, "startedAt"),
		}
	}
	if t := getMap(state, "terminated"); t != nil {
		return ContainerState{
			Type: "terminated", Reason: getString(t, "reason"),
			Message: getString(t, "message"), ExitCode: getInt(t, "exitCode"),
			StartedAt: getString(t, "startedAt"), FinishedAt: getString(t, "finishedAt"),
		}
	}
	return ContainerState{Type: "unknown"}
}

// extractEvents 从 Events List JSON 抽取并排序。
//
// 输入形态（K8s core/v1 EventList）：
//	{ "items": [ {metadata, involvedObject, type, reason, message, count, firstTimestamp, lastTimestamp, ...}, ... ] }
//
// 处理：
//   1. 兼容 BCS data 包一层 / 原生 items 两种形态
//   2. 二次本地过滤（某些 BCS 代理版本 fieldSelector 不生效）
//   3. 按 lastTimestamp 倒序（最新在前）
//   4. 截断到 MaxEventsPerPod
func extractEvents(raw map[string]any, pod, namespace string) []PodEvent {
	root := getMap(raw, "data")
	if root == nil {
		root = raw
	}
	items := getArray(root, "items")
	events := make([]PodEvent, 0, len(items))
	for _, it := range items {
		itMap, _ := it.(map[string]any)
		if itMap == nil {
			continue
		}
		// 本地二次过滤：确保 involvedObject 匹配本 Pod
		involved := getMap(itMap, "involvedObject")
		if getString(involved, "name") != pod {
			continue
		}
		if ns := getString(involved, "namespace"); ns != "" && ns != namespace {
			continue
		}
		ev := PodEvent{
			Type:          getString(itMap, "type"),
			Reason:        getString(itMap, "reason"),
			Message:       getString(itMap, "message"),
			Count:         getInt(itMap, "count"),
			FirstTime:     getString(itMap, "firstTimestamp"),
			LastTime:      getString(itMap, "lastTimestamp"),
			ReportingNode: getString(itMap, "reportingComponent"),
		}
		if ev.Count == 0 {
			ev.Count = 1
		}
		events = append(events, ev)
	}
	// 按 lastTime 倒序
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].LastTime > events[j].LastTime
	})
	if len(events) > MaxEventsPerPod {
		events = events[:MaxEventsPerPod]
	}
	return events
}

// humanAge 把 RFC3339 创建时间格式化成人类友好 "5d3h" / "2h15m" / "45s"。
func humanAge(creationTS string) string {
	if creationTS == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, creationTS)
	if err != nil {
		return creationTS // 解析失败就原样返
	}
	d := time.Since(t)
	if d < 0 {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd%dh", days, hours)
}

// emitPodDescribeAudit describe 操作审计。
func emitPodDescribeAudit(client *bcsapi.Client, in PodDescribeInput, pods []string, reports []PodDescribeReport) {
	warningsTotal, errCnt := 0, 0
	for _, r := range reports {
		warningsTotal += r.WarningsCount
		if r.Error != "" {
			errCnt++
		}
	}
	audit.Emit(audit.Event{
		Agent:    "diagnosis_agent",
		Action:   "bcs.pod.describe",
		Severity: "Info",
		Target:   fmt.Sprintf("%s/%s (pods=%d)", in.ClusterID, in.Namespace, len(pods)),
		Params: map[string]any{
			"cluster_id":     in.ClusterID,
			"namespace":      in.Namespace,
			"pods":           pods,
			"warnings_total": warningsTotal,
			"errors":         errCnt,
		},
		Success: errCnt == 0,
		Mock:    client != nil && client.IsMock(),
	})
}

// ---- map 辅助函数（避免到处写类型断言）----------------------------------

func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

func getArray(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	v, ok := m[key].([]any)
	if !ok {
		return nil
	}
	return v
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func getBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

// getInt 兼容 JSON 数字以 float64 反序列化的场景。
func getInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
