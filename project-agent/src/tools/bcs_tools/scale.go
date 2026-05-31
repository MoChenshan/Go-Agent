// Package bcstools —— bcs_scale_deployment（Deployment 副本数伸缩）。
//
// 本工具是 D18.1 新增，定位为 BCS 写操作生态的第二个成员：
//
//   bcs_helm_manage     —— 粗粒度 Release 级写（rollback/install/uninstall）
//   bcs_scale_deployment —— 细粒度 Deployment 级写（最常用的扩缩容）← 本文件
//
// 与 helm 工具相比，本工具引入了 3 个"更生产级"的设计：
//
//  1) Severity 动态分级
//     HITL 告警等级不是写死的，而是根据 |Δ|/current 和绝对副本数动态计算，
//     避免"改 1 个副本和改 500 个副本走一样的告警等级"的麻痹效应。
//
//  2) 并发竞态守护
//     支持可选的 expected_current：若实际当前副本数与期望不符，拒绝执行。
//     功能类似于 HTTP ETag / K8s resourceVersion，防止 LLM 基于旧读数覆盖他人改动。
//
//  3) 内置 Guard 兜底规则（R1 / R2）
//     即使 HITL 被软关闭（HITL_DISABLE=1），以下两条硬规则仍会拦截：
//       R1：生产 namespace（prefix=prod-）缩容到 0 必须 require_reason=true
//       R2：单次 |Δ| > scaleHardLimit 直接拒绝（防 LLM 把 3 理解成 3000）
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
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// scaleHardLimit 单次 scale 允许的绝对变化量上限。
// 超过此阈值一律拒绝 —— 任何"单次调整 500 副本以上"的操作都应该走 GitOps/人工 kubectl，
// 不应该由 Agent 一键下发。这是最后一道闸门，HITL 无法豁免。
const scaleHardLimit = 500

// prodNamespacePrefixes 生产命名空间前缀黑名单，用于 R1 规则。
// 命中任一前缀且缩容到 0 时，强制 require_reason=true（便于合规追溯）。
var prodNamespacePrefixes = []string{"prod-", "prod_", "production-", "release-"}

// ScaleInput bcs_scale_deployment 工具入参。
type ScaleInput struct {
	Action          string `json:"action"            description:"操作类型（必填）：get(查询当前副本) / scale(设置为绝对值) / scale_relative(相对变化 +N 或 -N)"`
	ClusterID       string `json:"cluster_id"        description:"集群 ID（必填），如 BCS-K8S-00001"`
	Namespace       string `json:"namespace"         description:"命名空间（必填）"`
	Deployment      string `json:"deployment"        description:"Deployment 名称（必填）"`
	Replicas        int    `json:"replicas"          description:"目标副本数（action=scale 必填，0~10000）"`
	Delta           int    `json:"delta"             description:"相对变化量（action=scale_relative 必填，支持正负整数）"`
	ExpectedCurrent *int   `json:"expected_current"  description:"期望的当前副本数（可选）。若实际值与此不符则拒绝执行，用于避免读-改-写竞态，类似 ETag。"`
	Reason          string `json:"reason"            description:"变更原因（生产环境缩容到 0 等高危场景必填，留痕审计用）"`
	Confirmed       bool   `json:"confirmed"         description:"是否已获人工确认。scale / scale_relative 必须 true 才真正下发"`
	// WaitForReady D19.6 新增：打开后会轮询 Deployment 直到 readyReplicas 收敛到新 spec.replicas
	// 【符合直觉语义】：扩容的人想知道新副本是否用上了，缩容的人想知道 Pod 是否跳下去了
	// 【辜射】：与 pod_restart.wait_for_ready 完全同名，便于 LLM 可迁移认知
	WaitForReady bool `json:"wait_for_ready"    description:"是否同步等待 Deployment 收敛到新副本数（默认 false）。长耗时场景请配合 async_tools.job_submit 使用避免阻塞对话"`

	// HPAPolicy D20 新增：scale 与 HPA 冲突的处理策略。
	//   ""/"warn" → 默认：不拒绝，但在 Plan 里显式告警、Severity 升到 High
	//   "block"   → 目标副本数不在 HPA [min,max] 区间时硬拒绝（HITL 不可豁免）
	//   "force"   → 明知故犯跳过检查，Severity=Critical 且必须带 reason，审计标注 hpa_bypass
	// 向后兼容：D19 调用方不填此字段即走 warn，行为与加入本字段前唯一差别是 Plan 里多一行 HPA 提示。
	HPAPolicy string `json:"hpa_policy"        description:"scale 与 HPA 冲突的处理策略（可选）：warn(默认, 告警但不拦截) / block(区间外硬拒绝) / force(明知故犯, 审计留痕)。未设置按 warn 处理。"`
}

// newScaleTool 构造 bcs_scale_deployment 工具。
//
// D19.6：内部构造默认 ReadyWaiter（复用 D19.5 抽象）。测试若需注入 fake Waiter
// 请走 newScaleToolWithWaiter。双构造器模式与 pod_restart 保持一致。
func newScaleTool(client *bcsapi.Client) tool.Tool {
	return newScaleToolWithWaiter(client, NewBCSReadyWaiter(client, WaiterConfig{}))
}

// newScaleToolWithWaiter 可注入 Waiter 的工具构造器，仅供内部测试调用。
func newScaleToolWithWaiter(client *bcsapi.Client, waiter ReadyWaiter) tool.Tool {
	if waiter == nil {
		waiter = NewNoopReadyWaiter()
	}
	fn := func(ctx context.Context, in ScaleInput) (*Result, error) {
		action := strings.ToLower(strings.TrimSpace(in.Action))
		if action == "" || in.ClusterID == "" || in.Namespace == "" || in.Deployment == "" {
			return nil, fmt.Errorf("action / cluster_id / namespace / deployment 均为必填项")
		}

		// ---- 1) 纯读路径：get ----
		if action == "get" {
			return doGetReplicas(ctx, client, in)
		}

		// ---- 2) 写路径：先查当前副本，用于计算 Δ / Severity / 竞态守护 ----
		currentReplicas, curErr := fetchCurrentReplicas(ctx, client, in)
		// 仅真实模式下失败才中断；mock 模式下 curErr 会是 ErrMockMode，留给下游继续。
		if curErr != nil && !errors.Is(curErr, bcsapi.ErrMockMode) {
			return nil, fmt.Errorf("查询当前副本数失败: %w", curErr)
		}

		// 计算目标副本数
		var targetReplicas int
		switch action {
		case "scale":
			if in.Replicas < 0 || in.Replicas > 10000 {
				return nil, fmt.Errorf("replicas 必须在 [0, 10000] 范围内")
			}
			targetReplicas = in.Replicas
		case "scale_relative":
			if in.Delta == 0 {
				return nil, fmt.Errorf("scale_relative 必须指定非零 delta")
			}
			targetReplicas = currentReplicas + in.Delta
			if targetReplicas < 0 {
				return nil, fmt.Errorf("相对伸缩后副本数为负（%d + %d = %d），已拒绝", currentReplicas, in.Delta, targetReplicas)
			}
		default:
			return nil, fmt.Errorf("不支持的 action: %s（可选：get / scale / scale_relative）", action)
		}

		delta := targetReplicas - currentReplicas

		// ---- 3) 并发守护：expected_current 校验 ----
		if in.ExpectedCurrent != nil && !errors.Is(curErr, bcsapi.ErrMockMode) {
			if *in.ExpectedCurrent != currentReplicas {
				return &Result{
					OK: false,
					Message: fmt.Sprintf(
						"并发守护拦截：expected_current=%d 与实际当前副本数 %d 不一致，可能有他人刚修改过。请重新查询后再发起 scale。",
						*in.ExpectedCurrent, currentReplicas,
					),
					Data: map[string]any{
						"expected_current": *in.ExpectedCurrent,
						"actual_current":   currentReplicas,
					},
				}, nil
			}
		}

		// ---- 3.5) D20：HPA 冲突感知 ---------------------------------------------
		//
		// 重要设计决策：HPA 检测放在 expected_current 之后、Guard R2 之前。
		//   - 放在 R2 之前：因为 PolicyBlock 的拒绝位阶要与 R2 等同（硬拒绝，HITL 不可豁免）
		//   - 放在 expected_current 之后：因为 expected_current 失败说明集群状态已变，
		//     此时任何进一步诊断都无意义，应最早返回
		//
		// 查询失败不阻断：HPA 感知是"增量加固"，可用性要求应低于 scale 主路径。
		hpaPolicy := NormalizeHPAPolicy(in.HPAPolicy)
		hpaInfo, _ := DetectHPAForDeployment(ctx, client, in.ClusterID, in.Namespace, in.Deployment)

		// PolicyBlock：检测到 HPA 且目标不在 [min,max] 区间 → 硬拒绝
		if hpaInfo.Found && !hpaInfo.InRange(targetReplicas) && hpaPolicy == PolicyBlock {
			audit.Emit(audit.Event{
				Agent:    "repair_agent",
				Action:   "bcs.deployment.scale",
				Severity: string(hitl.SeverityCritical),
				Target:   scaleTarget(in),
				Params: map[string]any{
					"from": currentReplicas, "to": targetReplicas, "delta": delta,
					"rejected_by": "hpa_conflict",
					"hpa_name":    hpaInfo.Name,
					"hpa_min":     hpaInfo.MinReplicas,
					"hpa_max":     hpaInfo.MaxReplicas,
					"hpa_policy":  string(hpaPolicy),
				},
				Success: false,
				Err:     fmt.Errorf("hpa conflict: target=%d out of [%d,%d] managed by HPA %q", targetReplicas, hpaInfo.MinReplicas, hpaInfo.MaxReplicas, hpaInfo.Name),
				Mock:    client.IsMock(),
			})
			return &Result{
				OK: false,
				Message: fmt.Sprintf(
					"HPA 冲突拦截：Deployment 被 HPA %q 托管，目标副本数 %d 不在允许区间 [%d, %d]，HPA 会在数秒内回滚。"+
						"请先调整 HPA 的 min/max，或切换 hpa_policy=force 明示明知故犯（需 reason 留痕）。",
					hpaInfo.Name, targetReplicas, hpaInfo.MinReplicas, hpaInfo.MaxReplicas,
				),
				Data: map[string]any{
					"from": currentReplicas, "to": targetReplicas, "delta": delta,
					"hpa": map[string]any{
						"name": hpaInfo.Name, "min": hpaInfo.MinReplicas, "max": hpaInfo.MaxReplicas,
					},
				},
			}, nil
		}

		// ---- 4) Guard 兜底规则 R2：硬上限（HITL 无法豁免）----
		if abs(delta) > scaleHardLimit {
			audit.Emit(audit.Event{
				Agent:    "repair_agent",
				Action:   "bcs.deployment.scale",
				Severity: string(hitl.SeverityCritical),
				Target:   scaleTarget(in),
				Params: map[string]any{
					"from": currentReplicas, "to": targetReplicas, "delta": delta,
					"rejected_by": "hard_limit",
				},
				Success: false,
				Err:     fmt.Errorf("hard limit exceeded: |Δ|=%d > %d", abs(delta), scaleHardLimit),
				Mock:    client.IsMock(),
			})
			return &Result{
				OK: false,
				Message: fmt.Sprintf(
					"单次副本数变化 |Δ|=%d 超过硬上限 %d，已拒绝。请拆分为多次小步调整，或走人工 kubectl / GitOps 流程。",
					abs(delta), scaleHardLimit,
				),
				Data: map[string]any{
					"from": currentReplicas, "to": targetReplicas, "delta": delta,
					"hard_limit": scaleHardLimit,
				},
			}, nil
		}

		// ---- 5) 动态 Severity 分级 + Guard R1 ----
		severity, requireReason := classifySeverity(currentReplicas, targetReplicas, in.Namespace)

		// D20：HPA 感知会对 Severity 做"升档"和"是否强制 reason"的增量决定：
		//   - Warn 且冲突存在 → 升到 High（若原本 Medium），让用户在 Plan 里看到红色告警
		//   - Force 明知故犯 → 强制 Critical + requireReason，审计留痕（防"我忘了告诉你"）
		if hpaInfo.Found && !hpaInfo.InRange(targetReplicas) {
			switch hpaPolicy {
			case PolicyWarn:
				if severity == hitl.SeverityMedium {
					severity = hitl.SeverityHigh
				}
			case PolicyForce:
				severity = hitl.SeverityCritical
				requireReason = true
			}
		}

		// R1：生产 ns 缩容到 0 必须给 reason
		if requireReason && strings.TrimSpace(in.Reason) == "" && in.Confirmed {
			return &Result{
				OK: false,
				Message: fmt.Sprintf(
					"规则拦截：生产命名空间 %q 下将副本数降为 0，必须在 reason 字段提供变更原因，便于合规留痕。",
					in.Namespace,
				),
			}, nil
		}

		// ---- 6) HITL 两段式确认 ----
		plan := buildScalePlan(in, currentReplicas, targetReplicas, delta, severity, requireReason, hpaInfo, hpaPolicy)
		if pending, need := hitl.Require(in.Confirmed, plan); need {
			return &Result{
				OK:      false,
				Message: pending.Message,
				Data:    pending,
			}, nil
		}

		// ---- 7) 真实下发 ----
		path := fmt.Sprintf(
			"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/namespaces/%s/deployments/%s/scale",
			in.ClusterID, in.Namespace, in.Deployment,
		)
		reqBody := map[string]any{"replicas": targetReplicas}

		var respData map[string]any
		err := client.PutJSON(ctx, path, reqBody, &respData)

		// 审计：写操作一律入账，from → to 是法律意义字段
		emitScaleAudit := func(ok bool, apiErr error, mock bool) {
			params := map[string]any{
				"cluster_id": in.ClusterID,
				"namespace":  in.Namespace,
				"deployment": in.Deployment,
				"from":       currentReplicas,
				"to":         targetReplicas,
				"delta":      delta,
				"mode":       action,
			}
			if in.ExpectedCurrent != nil {
				params["expected_current"] = *in.ExpectedCurrent
			}
			if in.Reason != "" {
				params["reason"] = in.Reason
			}
			// D20：审计必须记录 HPA 情况，事后追查"为啥 scale 完秒回滚"的关键证据
			params["hpa_policy"] = string(hpaPolicy)
			params["hpa_found"] = hpaInfo.Found
			if hpaInfo.Found {
				params["hpa_name"] = hpaInfo.Name
				params["hpa_min"] = hpaInfo.MinReplicas
				params["hpa_max"] = hpaInfo.MaxReplicas
				params["hpa_in_range"] = hpaInfo.InRange(targetReplicas)
				if !hpaInfo.InRange(targetReplicas) && hpaPolicy == PolicyForce {
					params["hpa_bypass"] = true
				}
			}
			audit.Emit(audit.Event{
				Agent:    "repair_agent",
				Action:   "bcs.deployment.scale",
				Severity: string(severity),
				Target:   scaleTarget(in),
				Params:   params,
				Success:  ok,
				Err:      apiErr,
				Mock:     mock,
			})
		}

		if errors.Is(err, bcsapi.ErrMockMode) {
			r := mockScale(in, currentReplicas, targetReplicas)
			// Mock 模式下也走一下 Waiter（Mock Waiter 会 50ms 短路），保持行为对称
			//
			// 不论 WaitForReady 与否都要往 Data 里挂 wait_for_ready 字段——schema 稳定性
			// 是 LLM 解析的硬约束，与 scale 真实路径保持完全一致（attempted:false 表示"工具
			// 看到了开关，但未尝试"，给 LLM 明确反馈而不是字段消失）。
			waitInfo := map[string]any{"attempted": false}
			if in.WaitForReady {
				waitInfo = runReadyWait(ctx, waiter, ReadySpec{
					Mode:       "scale_deployment",
					ClusterID:  in.ClusterID,
					Namespace:  in.Namespace,
					Deployment: in.Deployment,
				})
			}
			if data, ok := r.Data.(map[string]any); ok {
				data["wait_for_ready"] = waitInfo
			}
			emitScaleAudit(true, nil, true)
			return r, nil
		}
		if err != nil {
			emitScaleAudit(false, err, false)
			return nil, fmt.Errorf("BCS scale 失败: %w", err)
		}

		// D19.6：wait_for_ready 复用 D19.5 ReadyWaiter。
		//
		// 语义匹配性：scale 后“稳定完成”的官方定义就是 observedGen/updated/ready
		// 三条件同时满足 new spec.replicas——与 pod_restart 的 Ready 定义统一。
		// 这证明了 D19.5 抛开具体 mode 的 isDeploymentReady 判据是可复用的，无需新写判据。
		waitInfo := map[string]any{"attempted": false}
		if in.WaitForReady {
			waitInfo = runReadyWait(ctx, waiter, ReadySpec{
				Mode:       "scale_deployment",
				ClusterID:  in.ClusterID,
				Namespace:  in.Namespace,
				Deployment: in.Deployment,
			})
		}
		r := &Result{
			OK: true,
			Data: map[string]any{
				"cluster_id":     in.ClusterID,
				"namespace":      in.Namespace,
				"deployment":     in.Deployment,
				"from":           currentReplicas,
				"to":             targetReplicas,
				"delta":          delta,
				"api_response":   respData,
				"wait_for_ready": waitInfo,
			},
		}
		emitScaleAudit(true, nil, false)
		return r, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_scale_deployment"),
		function.WithDescription(
				"调整 BCS Deployment 的副本数：get(查询当前副本) / scale(设为绝对值) / scale_relative(相对增减)。"+
				"⚠ 写操作必须先不带 confirmed 发起，工具会返回 Plan（含动态 Severity 分级 / from→to / 影响范围 / HPA 冲突信息），"+
				"用户确认后再 confirmed=true 重发。生产 ns 缩容到 0 必须填 reason；单次 |Δ|>500 直接拒绝。"+
				"建议与 expected_current 搭配使用以避免读-改-写竞态。"+
				"wait_for_ready=true 时会同步等 Deployment 收敛到新副本数（典型 30s~3min），长耗时场景建议配合 async_tools.job_submit 异步执行。"+
				"D20 HPA 感知：自动检测关联 HPA，hpa_policy=warn(默认, 告警但不拦截) / block(区间外硬拒绝) / force(明知故犯, 需 reason)。",
		),
	)
}

// doGetReplicas 处理 action=get 路径。
func doGetReplicas(ctx context.Context, client *bcsapi.Client, in ScaleInput) (*Result, error) {
	current, err := fetchCurrentReplicas(ctx, client, in)
	if errors.Is(err, bcsapi.ErrMockMode) {
		return &Result{
			OK: true, Mock: true, Message: "Mock 模式：返回模拟副本数",
			Data: map[string]any{
				"cluster_id": in.ClusterID, "namespace": in.Namespace, "deployment": in.Deployment,
				"replicas": 3, "readyReplicas": 3,
			},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &Result{
		OK: true,
		Data: map[string]any{
			"cluster_id": in.ClusterID, "namespace": in.Namespace, "deployment": in.Deployment,
			"replicas": current,
		},
	}, nil
}

// fetchCurrentReplicas 查询 Deployment 当前副本数（spec.replicas）。
//
// 真实模式下使用 bcs-storage GET 接口读取 deployment 的 spec.replicas；
// Mock 模式下返回 ErrMockMode 由上游分支处理，默认视为 3 副本。
func fetchCurrentReplicas(ctx context.Context, client *bcsapi.Client, in ScaleInput) (int, error) {
	if client.IsMock() {
		return 3, bcsapi.ErrMockMode
	}
	path := fmt.Sprintf(
		"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/deployment",
		in.ClusterID,
	)
	query := map[string]string{
		"namespace": in.Namespace,
		"name":      in.Deployment,
	}
	var resp map[string]any
	if err := client.Get(ctx, path, query, &resp); err != nil {
		return 0, err
	}
	// BCS storage 返回结构一般是 {"data":[{"data":{"spec":{"replicas":3}, ...}}]}
	return extractReplicas(resp), nil
}

// extractReplicas 宽松地从 BCS storage 响应里挖 spec.replicas；
// 解析失败统一返回 0 —— 这会让后续 Δ 计算偏大，反而会触发更严格的 HITL 等级，保守安全。
func extractReplicas(resp map[string]any) int {
	arr, ok := resp["data"].([]any)
	if !ok || len(arr) == 0 {
		return 0
	}
	first, _ := arr[0].(map[string]any)
	data, _ := first["data"].(map[string]any)
	spec, _ := data["spec"].(map[string]any)
	// JSON number 反序列化为 float64
	if r, ok := spec["replicas"].(float64); ok {
		return int(r)
	}
	if r, ok := spec["replicas"].(int); ok {
		return r
	}
	return 0
}

// classifySeverity 根据 from/to/namespace 计算 HITL 等级与是否强制要求 reason。
//
// 决策矩阵（见文件头注释）：
//
//	缩容 to=0 && 生产 ns    → Critical & requireReason
//	缩容 to=0 && 非生产 ns  → High
//	|Δ|/max(from,1) >= 100% → High
//	|Δ|/max(from,1) >= 50%  → High
//	目标副本数 > 100         → 升一档（至少 High）
//	其他                     → Medium
func classifySeverity(from, to int, namespace string) (hitl.Severity, bool) {
	delta := to - from
	absDelta := abs(delta)
	base := 1
	if from > 0 {
		base = from
	}
	ratio := float64(absDelta) / float64(base) // 相对变化比例

	// 缩容到 0：特殊对待
	if to == 0 && from > 0 {
		if isProdNamespace(namespace) {
			return hitl.SeverityCritical, true
		}
		return hitl.SeverityHigh, false
	}

	severity := hitl.SeverityMedium
	switch {
	case ratio >= 1.0: // 翻倍或以上
		severity = hitl.SeverityHigh
	case ratio >= 0.5: // 超过一半
		severity = hitl.SeverityHigh
	}
	// 大规模部署（目标 >100）额外加一档
	if to > 100 && severity == hitl.SeverityMedium {
		severity = hitl.SeverityHigh
	}
	return severity, false
}

// isProdNamespace 判断 namespace 是否命中生产前缀黑名单。
func isProdNamespace(ns string) bool {
	lowered := strings.ToLower(ns)
	for _, p := range prodNamespacePrefixes {
		if strings.HasPrefix(lowered, p) {
			return true
		}
	}
	return false
}

// buildScalePlan 构造 HITL Plan 文本，把 from→to / Δ / 比例 / HPA 冲突都摆到面上。
//
// D20 新签名：接收 HPAInfo 和 policy，让用户在 Plan 里直接看到
//   - 是否被 HPA 托管
//   - HPA 的 [min, max] 允许区间
//   - 本次操作是否冲突，以及所选 policy 的后果
func buildScalePlan(in ScaleInput, from, to, delta int, severity hitl.Severity, requireReason bool, hpaInfo HPAInfo, hpaPolicy HPAConflictPolicy) hitl.Plan {
	target := scaleTarget(in)
	direction := "扩容"
	if delta < 0 {
		direction = "缩容"
	} else if delta == 0 {
		direction = "无变化（noop）"
	}
	params := map[string]any{
		"cluster_id": in.ClusterID,
		"namespace":  in.Namespace,
		"deployment": in.Deployment,
		"from":       from,
		"to":         to,
		"delta":      delta,
		"hpa_policy": string(hpaPolicy),
	}
	if in.ExpectedCurrent != nil {
		params["expected_current"] = *in.ExpectedCurrent
	}
	if in.WaitForReady {
		params["wait_for_ready"] = true
	}
	if hpaInfo.Found {
		params["hpa"] = map[string]any{
			"name":     hpaInfo.Name,
			"min":      hpaInfo.MinReplicas,
			"max":      hpaInfo.MaxReplicas,
			"in_range": hpaInfo.InRange(to),
		}
	}

	sideEffect := fmt.Sprintf(
		"%s：%d → %d（Δ=%+d）。K8s 将新建或删除对应数量的 Pod，可能触发滚动重启。",
		direction, from, to, delta,
	)
	// D20：HPA 冲突时把告警叠加到 SideEffect 最前面——Plan 渲染器通常按此字段显眼呈现
	if hpaInfo.Found && !hpaInfo.InRange(to) {
		var policyHint string
		switch hpaPolicy {
		case PolicyForce:
			policyHint = "【已选 force：明知故犯将执行，审计记录 hpa_bypass=true】"
		case PolicyWarn:
			policyHint = "【已选 warn：将执行，但 HPA 预计数秒内回滚到区间内】"
		default:
			policyHint = "【block 已拦截】"
		}
		sideEffect = fmt.Sprintf(
			"⚠ HPA 冲突：Deployment 被 HPA %q 托管，目标 %d 不在 [%d,%d] 区间。%s\n%s",
			hpaInfo.Name, to, hpaInfo.MinReplicas, hpaInfo.MaxReplicas, policyHint, sideEffect,
		)
	}

	impactScope := fmt.Sprintf(
		"命名空间 %s 下 Deployment %q 的流量承接能力将立即变化。",
		in.Namespace, in.Deployment,
	)
	if hpaInfo.Found {
		impactScope += fmt.Sprintf(" 关联 HPA=%q（min=%d, max=%d）；若目标不在区间内，HPA 会在数秒~数分钟内按自身策略回滚本次变更。",
			hpaInfo.Name, hpaInfo.MinReplicas, hpaInfo.MaxReplicas)
	}

	rollback := fmt.Sprintf(
		"如需回退，可再次调用 bcs_scale_deployment 并将 replicas 设为 %d（即原值）。",
		from,
	)
	if hpaInfo.Found && !hpaInfo.InRange(to) {
		rollback += " 若希望变更长期生效，必须同步调整 HPA 的 min/max 配置，否则 HPA 会自动覆盖。"
	}

	return hitl.Plan{
		Action:        "bcs.deployment.scale",
		Severity:      severity,
		Target:        target,
		SideEffect:    sideEffect,
		ImpactScope:   impactScope,
		RollbackPlan:  rollback,
		Params:        params,
		RequireReason: requireReason,
	}
}

// scaleTarget 组装 "<cluster>/<ns>/<deploy>" 格式的审计 target。
func scaleTarget(in ScaleInput) string {
	return fmt.Sprintf("%s / %s / %s", in.ClusterID, in.Namespace, in.Deployment)
}

// mockScale 返回 Mock 模式下的写结果。
func mockScale(in ScaleInput, from, to int) *Result {
	return &Result{
		OK:      true,
		Mock:    true,
		Message: fmt.Sprintf("Mock 模式：副本数从 %d 调整至 %d（未真实下发）", from, to),
		Data: map[string]any{
			"cluster_id": in.ClusterID,
			"namespace":  in.Namespace,
			"deployment": in.Deployment,
			"from":       from,
			"to":         to,
			"delta":      to - from,
			"status":     "SCALED (mock)",
		},
	}
}

// abs 返回 int 的绝对值。
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
