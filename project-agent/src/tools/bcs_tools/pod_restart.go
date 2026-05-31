// Package bcstools —— bcs_pod_restart（Pod 级重启，D18.2 新增）。
//
// 本工具是 BCS 写操作生态的第三个成员，专注"故障自愈"场景：
//
//	bcs_helm_manage       —— Release 级写（rollback/install/uninstall）
//	bcs_scale_deployment  —— Deployment 副本伸缩（D18.1）
//	bcs_pod_restart       —— Pod 级重启 / 驱逐 / 滚动重启（本文件）
//
// K8s 原生没有 "restart" 动词。我们把运维意图下"让它重启"统一抽象为三种语义：
//
//  1) delete_pod    —— 删除具体 Pod，由 ReplicaSet 自动拉起。粒度最细，最常用。
//  2) rollout_restart —— 给 Deployment.spec.template 打一个时间戳注解，触发滚动重启整组。
//  3) evict_pod     —— 走 Eviction API，受 PodDisruptionBudget 保护。节点维护场景。
//
// 比 scale 更"生产级"的 3 个设计：
//
//  A) 批量保护（soft + hard limit）
//     批量 delete_pod 最容易踩坑 —— LLM 一次性传入 50 个 pod 名就是分钟级雪崩。
//     soft limit（5）：超过就自动拆分为串行逐个处理（留存 2s 间隔），避免风暴；
//     hard limit（20）：直接拒绝，提示走 kubectl。
//
//  B) PDB 预检（仅 evict_pod）
//     Eviction API 在 PDB 耗尽时会 429，我们提前 GET PDB 状态，allowedDisruptions=0
//     直接拒绝；避免无意义试错 + 让 Plan 文案里能明确告诉用户"PDB 不允许"。
//
//  C) Ready 等待钩子（wait_for_ready）
//     部分场景希望"重启直到可用再返回"。本工具提供该参数，Mock 下返回模拟等待；
//     真实模式通过轮询 ReplicaSet/Deployment status 实现（首期占位，避免阻塞 agent）。
package bcstools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/observability"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// 批量限制常量。hard limit 是架构级硬闸门（HITL_DISABLE 也无法豁免），
// soft limit 是"串行化"分界线。
const (
	// batchSoftLimit 超过此数就启用串行节奏（2s/个），避免一次性大批量同时删引发雪崩。
	batchSoftLimit = 5
	// batchHardLimit 单次请求允许的最大 pod 数；超过直接拒绝。
	batchHardLimit = 20
	// batchSerialInterval 串行模式下相邻两次 delete 之间的间隔。
	batchSerialInterval = 2 * time.Second
)

// PodRestartInput bcs_pod_restart 工具入参。
type PodRestartInput struct {
	Mode       string   `json:"mode"         description:"重启语义（必填）：delete_pod(删单/多 Pod，由 RS 拉起) / rollout_restart(滚动重启整个 Deployment) / evict_pod(Eviction API 优雅驱逐，受 PDB 保护)"`
	ClusterID  string   `json:"cluster_id"   description:"集群 ID（必填），如 BCS-K8S-00001"`
	Namespace  string   `json:"namespace"    description:"命名空间（必填）"`
	PodNames   []string `json:"pod_names"    description:"Pod 名称列表（mode=delete_pod/evict_pod 必填），单次最多 20 个；>5 自动转串行节奏"`
	Deployment string   `json:"deployment"   description:"Deployment 名称（mode=rollout_restart 必填）"`
	GracePeriodSeconds *int `json:"grace_period_seconds" description:"优雅终止宽限（秒），可选。0 表示强杀（慎用）；默认由 K8s 按 Pod 配置处理"`
	WaitForReady bool   `json:"wait_for_ready" description:"是否等待新 Pod Ready 后再返回，默认 false。为 true 时真实模式将轮询 ReplicaSet/Deployment 状态（首期 Mock 返回模拟等待）"`
	Reason     string   `json:"reason"       description:"变更原因（生产 ns 滚动重启必填；合规留痕用）"`
	Confirmed  bool     `json:"confirmed"    description:"是否已获人工确认；写操作必须 true 才真正下发"`

	// HPAPolicy D20.1 新增：**仅对 mode=rollout_restart 生效**的 HPA 感知策略。
	//
	// 与 scale 的三档策略不同，rollout_restart 不改副本数，HPA 冲突属于**副作用型**：
	// 滚动期间 maxSurge 导致短时 Pod 数翻倍，HPA 可能误读为"负载骤升"触发扩容；
	// 之后滚动完成回落时 HPA 又会反向缩容，形成**昨太阳今太阳暗权**的查杂波。
	// 因此本工具只提供两档（而非三档）：
	//   ""/"warn" → 默认：Plan 里提示 HPA 存在及副作用风险，Severity 升 High
	//   "ignore" → 用户明知且接受 （如：正是为了触发一次 HPA 重新汇算），审计记 hpa_ignored=true
	// 不提供 block/force：因为 rollout 本身不“违反”HPA 区间，硬拒绝会误杀大量合理重启场景。
	HPAPolicy string `json:"hpa_policy"    description:"（仅 rollout_restart）HPA 感知策略：warn(默认，Plan 中提示并升 Severity) / ignore(明知有 HPA 仍执行，审计留痕)。其他 mode 下字段忽略。"`
}

// newPodRestartTool 构造 bcs_pod_restart 工具。
//
// D19.5：内部构造默认的 ReadyWaiter（用同一个 bcsapi.Client），
// 保持架构上的向后兼容性——newAll 调用方不需要改变。
// 测试要注入自定义 Waiter 请直接走 newPodRestartToolWithWaiter。
func newPodRestartTool(client *bcsapi.Client) tool.Tool {
	return newPodRestartToolWithWaiter(client, NewBCSReadyWaiter(client, WaiterConfig{}))
}

// newPodRestartToolWithWaiter 可注入 Waiter 的构造器，仅供单测/未来自定义度使用。
//
// 为什么双构造器：避免对业务装配入口的改动。外部只知道 newPodRestartTool(client)，
// 内部测试则可构造 Noop/fake Waiter 来覆盖超时与带意限路径。
func newPodRestartToolWithWaiter(client *bcsapi.Client, waiter ReadyWaiter) tool.Tool {
	if waiter == nil {
		waiter = NewNoopReadyWaiter()
	}
	fn := func(ctx context.Context, in PodRestartInput) (*Result, error) {
		mode := strings.ToLower(strings.TrimSpace(in.Mode))
		if mode == "" || in.ClusterID == "" || in.Namespace == "" {
			return nil, fmt.Errorf("mode / cluster_id / namespace 均为必填项")
		}
		switch mode {
		case "delete_pod":
			return doDeletePods(ctx, client, waiter, in)
		case "rollout_restart":
			return doRolloutRestart(ctx, client, waiter, in)
		case "evict_pod":
			return doEvictPods(ctx, client, waiter, in)
		default:
			return nil, fmt.Errorf("不支持的 mode: %s（可选：delete_pod / rollout_restart / evict_pod）", mode)
		}
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_pod_restart"),
		function.WithDescription(
			"按 Pod 粒度触发重启/驱逐的生产级写工具，三种语义：delete_pod（最常用，删单/多 Pod）/ "+
				"rollout_restart（滚动重启整个 Deployment，风险更高）/ evict_pod（走 Eviction API，PDB 感知）。"+
				"⚠ 写操作必须先不带 confirmed 发起获取 Plan，用户确认后再 confirmed=true 重发。"+
				"批量 delete 超过 5 个自动串行化，超过 20 个硬拒；生产 ns rollout_restart 必填 reason；evict 会预检 PDB。",
		),
	)
}

// =============================================================================
// delete_pod：最常用路径
// =============================================================================

func doDeletePods(ctx context.Context, client *bcsapi.Client, waiter ReadyWaiter, in PodRestartInput) (*Result, error) {
	if len(in.PodNames) == 0 {
		return nil, fmt.Errorf("delete_pod 必须指定 pod_names（至少 1 个）")
	}
	// R1：hard limit 硬拒
	if len(in.PodNames) > batchHardLimit {
		rejectAudit(client, in, "delete_pod", "hard_limit",
			fmt.Errorf("batch size %d > hard limit %d", len(in.PodNames), batchHardLimit))
		return &Result{
			OK: false,
			Message: fmt.Sprintf(
				"批量 delete_pod 数量 %d 超过硬上限 %d，已拒绝。请拆分多次调用，或通过 kubectl / GitOps 批处理。",
				len(in.PodNames), batchHardLimit,
			),
			Data: map[string]any{
				"batch_size": len(in.PodNames),
				"hard_limit": batchHardLimit,
			},
		}, nil
	}

	// 动态 Severity：单 Pod=Medium；批量=High；批量+生产 ns=Critical
	severity := classifyPodRestartSeverity("delete_pod", len(in.PodNames), in.Namespace)

	// HITL 两段式
	plan := buildDeletePodPlan(in, severity)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	// 串行 or 并发？超过 soft limit 串行化
	serial := len(in.PodNames) > batchSoftLimit
	results := make([]map[string]any, 0, len(in.PodNames))
	var lastErr error
	successCount := 0

	for i, podName := range in.PodNames {
		itemResult, err := deleteSinglePod(ctx, client, in, podName)
		if err != nil {
			lastErr = err
			results = append(results, map[string]any{
				"pod": podName, "ok": false, "error": err.Error(),
			})
			continue
		}
		successCount++
		results = append(results, map[string]any{
			"pod": podName, "ok": true, "mock": itemResult.mock,
		})
		// 串行节奏：除了最后一个，每次间隔 batchSerialInterval
		if serial && i < len(in.PodNames)-1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(batchSerialInterval):
			}
		}
	}

	// 等待 ready（D19.5 真实化）
	//
	// 语义话额外说明：
	//   - 对 delete_pod 而言，被删的 Pod 本身不会恢复，我们盯的是"RS 重新拉起同等副本数"
	//   - in.Deployment 可选；缺失时 Waiter 会报 deploy 必填错误，此时由上层倒第二标签以
	//     "wait_skipped: no deployment" 方式记录，不让 LLM 拿到误导性信息
	//   - Mock 模式由 Waiter 内部短路，保证单测快速通过
	waitInfo := map[string]any{"attempted": false}
	if in.WaitForReady && successCount > 0 {
		waitInfo = runReadyWait(ctx, waiter, ReadySpec{
			Mode:       "delete_pod",
			ClusterID:  in.ClusterID,
			Namespace:  in.Namespace,
			Deployment: in.Deployment,
			PodNames:   in.PodNames,
		})
	}

	// 审计
	ok := lastErr == nil || successCount > 0 // 至少 1 个成功也算部分成功
	emitPodRestartAudit(client, in, "delete_pod", severity, ok, lastErr,
		map[string]any{
			"batch_size":    len(in.PodNames),
			"success_count": successCount,
			"mode":          "delete_pod",
			"serial":        serial,
			"wait_for_ready": waitInfo,
		})

	msg := fmt.Sprintf("delete_pod 完成：%d/%d 成功", successCount, len(in.PodNames))
	if lastErr != nil {
		msg += fmt.Sprintf("（最后一个错误：%v）", lastErr)
	}
	return &Result{
		OK:      successCount > 0,
		Mock:    client.IsMock(),
		Message: msg,
		Data: map[string]any{
			"results":        results,
			"success_count":  successCount,
			"total":          len(in.PodNames),
			"serial":         serial,
			"wait_for_ready": waitInfo,
		},
	}, nil
}

// deleteSinglePod 删除单个 Pod。返回 mock=true 表示走 Mock 分支。
func deleteSinglePod(ctx context.Context, client *bcsapi.Client, in PodRestartInput, podName string) (struct{ mock bool }, error) {
	if client.IsMock() {
		return struct{ mock bool }{true}, nil
	}
	path := fmt.Sprintf(
		"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/namespaces/%s/pods/%s",
		in.ClusterID, in.Namespace, podName,
	)
	var body any
	if in.GracePeriodSeconds != nil {
		body = map[string]any{
			"kind":               "DeleteOptions",
			"apiVersion":         "v1",
			"gracePeriodSeconds": *in.GracePeriodSeconds,
		}
	}
	if err := client.DeleteJSON(ctx, path, body, nil); err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			return struct{ mock bool }{true}, nil
		}
		return struct{ mock bool }{false}, err
	}
	return struct{ mock bool }{false}, nil
}

// =============================================================================
// rollout_restart：风险最高，严格审批
// =============================================================================

func doRolloutRestart(ctx context.Context, client *bcsapi.Client, waiter ReadyWaiter, in PodRestartInput) (*Result, error) {
	if in.Deployment == "" {
		return nil, fmt.Errorf("rollout_restart 必须指定 deployment")
	}

	severity := classifyPodRestartSeverity("rollout_restart", 0, in.Namespace)

	// D20.1：HPA 感知——在 R2 reason 检查之前、severity 分级之后。
	//
	// 位置设计：HPA 冲突信息要能**升 severity 档位**从而影响 R2 reason 激活条件和 Plan 呈现，
	// 而不是仅在 Plan 里"附加一行文字"。这跟 D20 scale 的设计保持一致。
	//
	// 查询失败/Mock/nil 均返回 Found=false，主路径零影响。
	hpaPolicy := normalizeRolloutHPAPolicy(in.HPAPolicy)
	hpaInfo, _ := DetectHPAForDeployment(ctx, client, in.ClusterID, in.Namespace, in.Deployment)
	if hpaInfo.Found && hpaPolicy == PolicyWarn && severity == hitl.SeverityMedium {
		// warn 档加上有 HPA → 升为 High，与 D20 scale 一致
		severity = hitl.SeverityHigh
	}

	// R2：生产 ns 滚动重启必须带 reason
	needReason := isProdNamespace(in.Namespace)
	if needReason && in.Confirmed && strings.TrimSpace(in.Reason) == "" {
		return &Result{
			OK: false,
			Message: fmt.Sprintf(
				"规则拦截：生产命名空间 %q 下的 rollout_restart 影响整个 Deployment %q，必须在 reason 字段提供变更原因。",
				in.Namespace, in.Deployment,
			),
		}, nil
	}

	plan := buildRolloutRestartPlan(in, severity, needReason, hpaInfo, hpaPolicy)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	// 审计 params 的 HPA 字段构造器：三处 emit 复用同一份结构，避免漂移。
	enrichHPA := func(base map[string]any) map[string]any {
		if hpaInfo.Found {
			base["hpa_found"] = true
			base["hpa_name"] = hpaInfo.Name
			base["hpa_min"] = hpaInfo.MinReplicas
			base["hpa_max"] = hpaInfo.MaxReplicas
			base["hpa_policy"] = string(hpaPolicy)
			if hpaPolicy == PolicyIgnore {
				base["hpa_ignored"] = true
			}
		} else {
			base["hpa_found"] = false
		}
		return base
	}

	// 构造 Strategic Merge Patch：给 spec.template 打时间戳注解
	restartedAt := time.Now().UTC().Format(time.RFC3339)
	patchBody := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"kubectl.kubernetes.io/restartedAt": restartedAt,
						"gameops-agent/restarted-by":        "repair_agent",
						"gameops-agent/restart-reason":      firstNonEmpty(in.Reason, "n/a"),
					},
				},
			},
		},
	}

	path := fmt.Sprintf(
		"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/namespaces/%s/deployments/%s",
		in.ClusterID, in.Namespace, in.Deployment,
	)
	var respData map[string]any
	err := client.PatchJSON(ctx, path, patchBody, &respData)

	if errors.Is(err, bcsapi.ErrMockMode) {
		waitInfo := map[string]any{"attempted": false}
		if in.WaitForReady {
			waitInfo = runReadyWait(ctx, waiter, ReadySpec{
				Mode:       "rollout_restart",
				ClusterID:  in.ClusterID,
				Namespace:  in.Namespace,
				Deployment: in.Deployment,
			})
		}
		emitPodRestartAudit(client, in, "rollout_restart", severity, true, nil,
			enrichHPA(map[string]any{"mode": "rollout_restart", "restartedAt": restartedAt, "wait_for_ready": waitInfo}))
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：Deployment %q 已打 restartedAt=%s 注解（未真实下发）", in.Deployment, restartedAt),
			Data: map[string]any{
				"deployment":     in.Deployment,
				"restartedAt":    restartedAt,
				"status":         "PATCHED (mock)",
				"wait_for_ready": waitInfo,
			},
		}, nil
	}
	if err != nil {
		emitPodRestartAudit(client, in, "rollout_restart", severity, false, err,
			enrichHPA(map[string]any{"mode": "rollout_restart"}))
		return nil, fmt.Errorf("rollout_restart 失败: %w", err)
	}
	// 真实下发成功：若 wait_for_ready 为 true 则轮询直到满足 rollout 完成三条件（D19.5）
	waitInfo := map[string]any{"attempted": false}
	if in.WaitForReady {
		waitInfo = runReadyWait(ctx, waiter, ReadySpec{
			Mode:       "rollout_restart",
			ClusterID:  in.ClusterID,
			Namespace:  in.Namespace,
			Deployment: in.Deployment,
		})
	}
	emitPodRestartAudit(client, in, "rollout_restart", severity, true, nil,
		enrichHPA(map[string]any{"mode": "rollout_restart", "restartedAt": restartedAt, "wait_for_ready": waitInfo}))
	return &Result{
		OK: true,
		Data: map[string]any{
			"deployment":     in.Deployment,
			"restartedAt":    restartedAt,
			"api_response":   respData,
			"wait_for_ready": waitInfo,
		},
	}, nil
}

// =============================================================================
// evict_pod：PDB 感知
// =============================================================================

func doEvictPods(ctx context.Context, client *bcsapi.Client, waiter ReadyWaiter, in PodRestartInput) (*Result, error) {
	if len(in.PodNames) == 0 {
		return nil, fmt.Errorf("evict_pod 必须指定 pod_names（至少 1 个）")
	}
	if len(in.PodNames) > batchHardLimit {
		return &Result{
			OK: false,
			Message: fmt.Sprintf("批量 evict 数量 %d 超过硬上限 %d", len(in.PodNames), batchHardLimit),
		}, nil
	}

	severity := classifyPodRestartSeverity("evict_pod", len(in.PodNames), in.Namespace)

	plan := buildEvictPlan(in, severity)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	// PDB 预检：真实模式下先查 PDB allowedDisruptions
	// Mock 下直接跳过（模拟通过）
	if !client.IsMock() {
		if allowed, pdbErr := checkPDBAllowed(ctx, client, in); pdbErr == nil && allowed == 0 {
			emitPodRestartAudit(client, in, "evict_pod", severity, false,
				fmt.Errorf("PDB disallows eviction"),
				map[string]any{"mode": "evict_pod", "rejected_by": "pdb_exhausted"})
			return &Result{
				OK: false,
				Message: fmt.Sprintf(
					"PDB 预检失败：命名空间 %q 下的 PodDisruptionBudget 当前 allowedDisruptions=0，无法驱逐任何 Pod。"+
						"请稍后重试，或调整 PDB minAvailable / maxUnavailable。",
					in.Namespace,
				),
				Data: map[string]any{"allowed_disruptions": 0},
			}, nil
		}
	}

	results := make([]map[string]any, 0, len(in.PodNames))
	successCount := 0
	var lastErr error
	for _, podName := range in.PodNames {
		err := evictSinglePod(ctx, client, in, podName)
		if err != nil {
			lastErr = err
			results = append(results, map[string]any{"pod": podName, "ok": false, "error": err.Error()})
			continue
		}
		successCount++
		results = append(results, map[string]any{"pod": podName, "ok": true})
	}

	emitPodRestartAudit(client, in, "evict_pod", severity, successCount > 0, lastErr,
		map[string]any{
			"mode": "evict_pod", "batch_size": len(in.PodNames), "success_count": successCount,
			"wait_for_ready": evictWaitInfo(ctx, waiter, in, successCount),
		})
	return &Result{
		OK:   successCount > 0,
		Mock: client.IsMock(),
		Message: fmt.Sprintf("evict_pod 完成：%d/%d 成功", successCount, len(in.PodNames)),
		Data: map[string]any{
			"results": results, "success_count": successCount, "total": len(in.PodNames),
		},
	}, nil
}

// evictSinglePod 通过 Eviction API 驱逐单个 Pod。
func evictSinglePod(ctx context.Context, client *bcsapi.Client, in PodRestartInput, podName string) error {
	if client.IsMock() {
		return nil
	}
	// POST /api/v1/namespaces/<ns>/pods/<name>/eviction
	path := fmt.Sprintf(
		"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/namespaces/%s/pods/%s/eviction",
		in.ClusterID, in.Namespace, podName,
	)
	body := map[string]any{
		"apiVersion": "policy/v1",
		"kind":       "Eviction",
		"metadata":   map[string]any{"name": podName, "namespace": in.Namespace},
	}
	if in.GracePeriodSeconds != nil {
		body["deleteOptions"] = map[string]any{"gracePeriodSeconds": *in.GracePeriodSeconds}
	}
	return client.PostJSON(ctx, path, body, nil)
}

// checkPDBAllowed 查询命名空间下 PDB 的 status.disruptionsAllowed。
// 粗粒度实现：取首个 PDB 的允许值。真实生产中可按 pod label selector 匹配更精准的 PDB。
// 返回 (-1, err) 表示查询失败，不应视为拒绝（保持兼容老集群无 PDB 的情况）。
func checkPDBAllowed(ctx context.Context, client *bcsapi.Client, in PodRestartInput) (int, error) {
	path := fmt.Sprintf(
		"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/poddisruptionbudget",
		in.ClusterID,
	)
	var resp map[string]any
	if err := client.Get(ctx, path, map[string]string{"namespace": in.Namespace}, &resp); err != nil {
		return -1, err
	}
	arr, ok := resp["data"].([]any)
	if !ok || len(arr) == 0 {
		return -1, nil // 没有 PDB 视为不限制
	}
	first, _ := arr[0].(map[string]any)
	data, _ := first["data"].(map[string]any)
	status, _ := data["status"].(map[string]any)
	if v, ok := status["disruptionsAllowed"].(float64); ok {
		return int(v), nil
	}
	return -1, nil
}

// =============================================================================
// 辅助：分级 / Plan / 审计
// =============================================================================

// classifyPodRestartSeverity 按 mode / 批量 / 生产 ns 决定 Severity。
func classifyPodRestartSeverity(mode string, batchSize int, namespace string) hitl.Severity {
	prod := isProdNamespace(namespace)
	switch mode {
	case "delete_pod":
		if batchSize == 1 {
			if prod {
				return hitl.SeverityHigh
			}
			return hitl.SeverityMedium
		}
		if prod {
			return hitl.SeverityCritical
		}
		return hitl.SeverityHigh
	case "rollout_restart":
		// rollout_restart 走 Deployment 控制器滚动，配合 maxSurge/maxUnavailable 自带保护，
		// 比 delete_pod 更"温柔"。非 prod ns 起步 Medium；prod ns 仍 Critical。
		// 后续 HPA 冲突检测会按 hpa_policy=warn 升一档（Medium→High），见 doRolloutRestart。
		if prod {
			return hitl.SeverityCritical
		}
		return hitl.SeverityMedium
	case "evict_pod":
		// evict 有 PDB 保护，默认比 delete 降一档
		if prod {
			return hitl.SeverityHigh
		}
		return hitl.SeverityMedium
	}
	return hitl.SeverityMedium
}

// buildDeletePodPlan 构造 delete_pod 的 HITL Plan。
func buildDeletePodPlan(in PodRestartInput, severity hitl.Severity) hitl.Plan {
	target := fmt.Sprintf("%s / %s / %d Pod(s)", in.ClusterID, in.Namespace, len(in.PodNames))
	params := map[string]any{
		"cluster_id": in.ClusterID,
		"namespace":  in.Namespace,
		"pod_names":  in.PodNames,
		"mode":       "delete_pod",
	}
	if in.GracePeriodSeconds != nil {
		params["grace_period_seconds"] = *in.GracePeriodSeconds
	}
	sideEffect := fmt.Sprintf("删除 %d 个 Pod，由 ReplicaSet 立即拉起新实例；旧 Pod 会经过 terminationGracePeriod 后终止。", len(in.PodNames))
	if len(in.PodNames) > batchSoftLimit {
		sideEffect += fmt.Sprintf(" 本次批量 > %d，将按每 %s 一个的串行节奏下发。", batchSoftLimit, batchSerialInterval)
	}
	impactScope := fmt.Sprintf("命名空间 %q：受影响 Pod = %v。期间被删 Pod 短暂不可用；请确认业务副本数足以承接流量。", in.Namespace, in.PodNames)
	rollback := "delete_pod 不可撤销（但由 RS 立即拉起相同数量新实例，等价于重启）；若误删，可通过 `bcs_scale_deployment` 临时扩容保障容量。"
	return hitl.Plan{
		Action:       "bcs.pod.delete",
		Severity:     severity,
		Target:       target,
		SideEffect:   sideEffect,
		ImpactScope:  impactScope,
		RollbackPlan: rollback,
		Params:       params,
	}
}

// buildRolloutRestartPlan 构造 rollout_restart 的 HITL Plan。
//
// D20.1——新签名接收 HPAInfo + policy。有 HPA 时：
//   - SideEffect 开头拼接 ⚠ HPA 可能误触发扩缩容的警示
//   - ImpactScope 补充"滚动期间 maxSurge 导致短时副本倍增、HPA 可能反向动作"
//   - RollbackPlan 补充"若 HPA 已自动扩容，回退前需检查 maxReplicas 是否被占满"
//   - Params.hpa 结构化携带 （供面板/审计检索）
func buildRolloutRestartPlan(in PodRestartInput, severity hitl.Severity, needReason bool, hpaInfo HPAInfo, hpaPolicy HPAConflictPolicy) hitl.Plan {
	target := fmt.Sprintf("%s / %s / %s", in.ClusterID, in.Namespace, in.Deployment)
	params := map[string]any{
		"cluster_id": in.ClusterID,
		"namespace":  in.Namespace,
		"deployment": in.Deployment,
		"mode":       "rollout_restart",
		"hpa_policy": string(hpaPolicy),
	}
	if hpaInfo.Found {
		params["hpa"] = map[string]any{
			"name": hpaInfo.Name,
			"min":  hpaInfo.MinReplicas,
			"max":  hpaInfo.MaxReplicas,
		}
	}

	sideEffect := fmt.Sprintf("给 Deployment %q 的 spec.template 打 restartedAt 注解，触发滚动重启所有 Pod，按 maxSurge/maxUnavailable 节奏进行。", in.Deployment)
	impactScope := fmt.Sprintf("命名空间 %q 下 Deployment %q 的全部 Pod 将被逐批重启，期间可能短暂容量波动。", in.Namespace, in.Deployment)
	rollback := "rollout restart 本身等价于一次滚动更新；如需回退，可用 `bcs_helm_manage action=rollback` 或 `kubectl rollout undo`。"

	if hpaInfo.Found {
		var hpaHint string
		if hpaPolicy == PolicyIgnore {
			hpaHint = "【已选 ignore：明知存在 HPA 仍执行，审计标记 hpa_ignored=true】"
		} else {
			hpaHint = "【已选 warn：将执行，滚动期间注意观察 HPA 行为】"
		}
		sideEffect = fmt.Sprintf("⚠ HPA 感知：Deployment 被 HPA %q 托管（[%d,%d]）。%s\n%s",
			hpaInfo.Name, hpaInfo.MinReplicas, hpaInfo.MaxReplicas, hpaHint, sideEffect)
		impactScope += fmt.Sprintf(" 滚动期间 maxSurge 导致短时副本翻倍，HPA（%q）可能误读为负载上涨触发扩容，滚动完成后又会反向缩容；建议为关键发布窗口临时暂停 HPA 或调高 maxReplicas。", hpaInfo.Name)
		rollback += " 若滚动期间 HPA 已扩容，回退前先确认当前副本数，必要时先 bcs_scale_deployment action=get 查一下再决定回退策略。"
	}

	return hitl.Plan{
		Action:        "bcs.deployment.rollout_restart",
		Severity:      severity,
		Target:        target,
		SideEffect:    sideEffect,
		ImpactScope:   impactScope,
		RollbackPlan:  rollback,
		Params:        params,
		RequireReason: needReason,
	}
}

// normalizeRolloutHPAPolicy 将 rollout_restart 输入的 policy 字符串规整为枚举。
//
// 与 NormalizeHPAPolicy 的差异：
//   - 仅支持 warn / ignore，其他值（包括 block/force）一律回退 warn
//   - 因为 rollout_restart 不改副本数，block/force 没有合理语义
//
// 定义在本文件而非 hpa_detect.go：将"mode 专属策略"的知识关在工具内部，
// 避免 hpa_detect.go 补充多种工具专属的枚举造成席位膀胀。
func normalizeRolloutHPAPolicy(s string) HPAConflictPolicy {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(PolicyIgnore):
		return PolicyIgnore
	default:
		// 空 / warn / 任何非法值 / block / force → 一律 warn
		return PolicyWarn
	}
}

// buildEvictPlan 构造 evict_pod 的 HITL Plan。
func buildEvictPlan(in PodRestartInput, severity hitl.Severity) hitl.Plan {
	target := fmt.Sprintf("%s / %s / %d Pod(s)", in.ClusterID, in.Namespace, len(in.PodNames))
	params := map[string]any{
		"cluster_id": in.ClusterID,
		"namespace":  in.Namespace,
		"pod_names":  in.PodNames,
		"mode":       "evict_pod",
	}
	if in.GracePeriodSeconds != nil {
		params["grace_period_seconds"] = *in.GracePeriodSeconds
	}
	return hitl.Plan{
		Action:       "bcs.pod.evict",
		Severity:     severity,
		Target:       target,
		SideEffect:   "通过 Eviction API 驱逐 Pod，受 PodDisruptionBudget 保护；如果 PDB 不允许则会被 K8s 拒绝（429）。",
		ImpactScope:  fmt.Sprintf("命名空间 %q：目标 Pod = %v；PDB 会决定是否真正执行。", in.Namespace, in.PodNames),
		RollbackPlan: "evict 不可撤销（与 delete_pod 类似，由 RS 拉起新实例）。",
		Params:       params,
	}
}

// runReadyWait 真实调用 Waiter 并把结果打包成"可上报给 LLM 和审计"的 map。
//
// 设计要点：
//   - **永远返回 map（不为 nil）**：上层直接写进 Data 和审计 Extra，无需再判空
//   - **永远不 panic**：waiter 任何返回都翻译成结构化字段
//   - **duration 精度 ms**：够 SRE 判断，也不会因为纳秒让审计日志冗长
//   - **status 枚举**：ready/timeout/cancelled/error/skipped——面板可直接按此聚合
//
// 为什么把 ctx 错误单独分桶：
//   - context.DeadlineExceeded 语义上是"SRE 配的 timeout 到了"，应该告警（升级 timeout 或查 BCS）
//   - context.Canceled 语义上是"上游主动取消"（比如 HTTP 请求关闭），不应告警
//   混淆两者会让 GameOpsAsyncTimeoutRatioHigh 告警失信——这是 D19.4 明确过的取舍。
func runReadyWait(ctx context.Context, waiter ReadyWaiter, spec ReadySpec) map[string]any {
	if waiter == nil {
		return map[string]any{"attempted": false, "status": "skipped", "reason": "no_waiter"}
	}
	start := time.Now()
	ready, err := waiter.Wait(ctx, spec)
	elapsedMs := time.Since(start).Milliseconds()

	info := map[string]any{
		"attempted":  true,
		"ready":      ready,
		"elapsed_ms": elapsedMs,
		"mode":       spec.Mode,
	}
	if spec.Deployment != "" {
		info["deployment"] = spec.Deployment
	}

	switch {
	case ready && err == nil:
		info["status"] = "ready"
	case errors.Is(err, context.DeadlineExceeded):
		info["status"] = "timeout"
		info["reason"] = "timeout"
	case errors.Is(err, context.Canceled):
		info["status"] = "cancelled"
		info["reason"] = "context_cancelled"
	case err != nil:
		info["status"] = "error"
		info["reason"] = err.Error()
	default:
		// 非 ready 也非 err：理论上不会发生（Waiter 契约不允许）；兜底做 unknown
		info["status"] = "unknown"
	}

	// D19.5 可观测性打点
	// 用 context.Background() 是故意的：调用方的 ctx 可能已因 timeout/cancel 死掉，
	// 但指标仍应上报——这与 D19.4 AsyncMetricsAdapter 的选择一致。
	observability.IncPodReadyWait(context.Background(), spec.Mode, info["status"].(string))
	observability.ObservePodReadyWaitDuration(context.Background(), spec.Mode,
		info["status"].(string), float64(elapsedMs)/1000.0)

	return info
}

// evictWaitInfo evict_pod 专用：仅在 WaitForReady=true 且至少一个成功时才真正 wait。
// 单拉一层是因为审计调用位置在 evict 主函数尾部，这里保持调用点整洁。
func evictWaitInfo(ctx context.Context, waiter ReadyWaiter, in PodRestartInput, successCount int) map[string]any {
	if !in.WaitForReady || successCount == 0 {
		return map[string]any{"attempted": false}
	}
	return runReadyWait(ctx, waiter, ReadySpec{
		Mode:       "evict_pod",
		ClusterID:  in.ClusterID,
		Namespace:  in.Namespace,
		Deployment: in.Deployment,
		PodNames:   in.PodNames,
	})
}

// emitPodRestartAudit 统一审计入账。
func emitPodRestartAudit(client *bcsapi.Client, in PodRestartInput, mode string,
	severity hitl.Severity, ok bool, err error, extra map[string]any) {
	params := map[string]any{
		"cluster_id": in.ClusterID,
		"namespace":  in.Namespace,
		"mode":       mode,
	}
	if len(in.PodNames) > 0 {
		params["pod_names"] = in.PodNames
	}
	if in.Deployment != "" {
		params["deployment"] = in.Deployment
	}
	if in.Reason != "" {
		params["reason"] = in.Reason
	}
	for k, v := range extra {
		params[k] = v
	}
	action := "bcs.pod.restart"
	switch mode {
	case "delete_pod":
		action = "bcs.pod.delete"
	case "rollout_restart":
		action = "bcs.deployment.rollout_restart"
	case "evict_pod":
		action = "bcs.pod.evict"
	}
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   action,
		Severity: string(severity),
		Target:   fmt.Sprintf("%s / %s", in.ClusterID, in.Namespace),
		Params:   params,
		Success:  ok,
		Err:      err,
		Mock:     client.IsMock(),
	})
}

// rejectAudit 记录"在正式下发前就被 Guard 拒绝"的事件（便于事后统计哪些被硬拒了）。
func rejectAudit(client *bcsapi.Client, in PodRestartInput, mode, reason string, err error) {
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   "bcs.pod.restart",
		Severity: string(hitl.SeverityCritical),
		Target:   fmt.Sprintf("%s / %s", in.ClusterID, in.Namespace),
		Params: map[string]any{
			"mode":        mode,
			"rejected_by": reason,
			"pod_names":   in.PodNames,
			"deployment":  in.Deployment,
		},
		Success: false,
		Err:     err,
		Mock:    client.IsMock(),
	})
}

// firstNonEmpty 返回首个非空字符串。
func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
