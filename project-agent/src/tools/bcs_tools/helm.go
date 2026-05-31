// BCS Helm 部署/回滚（bcs-helm，写操作，需 HITL）。
//
// 对接 BCS Helm Manager：POST /bcsapi/v4/helmmanager/v1/releases/...
//
// D6 起接入统一 `hitl` 框架：所有写操作（rollback/install/uninstall）走两段式
// 确认流程（plan → confirm → execute），未 confirmed 时返回结构化 Plan，
// 由 LLM 原样展示给用户，得到『确认』后才真正下发请求。
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
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// HelmInput bcs_helm_manage 工具入参。
type HelmInput struct {
	Action         string `json:"action"         description:"操作类型（必填）：list / history / rollback / install / uninstall"`
	ClusterID      string `json:"cluster_id"     description:"集群 ID（必填），如 BCS-K8S-00001"`
	Namespace      string `json:"namespace"      description:"命名空间（必填，除 list 外）"`
	ReleaseName    string `json:"release_name"   description:"Release 名称（必填，除 list 外）"`
	Revision       int    `json:"revision"       description:"回滚目标版本号（action=rollback 必填）"`
	Chart          string `json:"chart"          description:"Helm Chart（action=install 必填），如 'bkrepo/game-core:1.2.3'"`
	Values         string `json:"values"         description:"values.yaml 内容（install 可选）"`
	Confirmed      bool   `json:"confirmed"      description:"是否已获人工确认。rollback/uninstall/install 必须 true 才真正执行"`
	// D19.7 新增：等 Release 下某个具体 Deployment 收敛。
	// 【为什么需要 wait_deployment】：helm release 可能关联多个 Deployment/StatefulSet/DaemonSet，
	// ReadyWaiter 只支持单 Deployment；与其在工具里做 release→workloads 自动发现（需要 BCS 侧新接口，
	// 且会让抽象边界变模糊），不如让 LLM 先 action=history 看 chart 里的 Deployment 名再传入。
	// 这是 ReadyWaiter "毕业考"的诚实选择——抽象边界保持不变，由 Agent 的规划能力补足上下文。
	WaitForReady   bool   `json:"wait_for_ready"   description:"是否同步等 Release 下某个 Deployment 收敛到就绪（默认 false，仅 rollback/install 生效）"`
	WaitDeployment string `json:"wait_deployment"  description:"wait_for_ready=true 时必填的目标 Deployment 名；一个 release 若含多个工作负载，请先 action=history 确认目标名"`
}

// newHelmTool 构造 bcs_helm_manage 工具。
//
// D19.7：内部构造默认 ReadyWaiter（复用 D19.5 抽象）。测试若需注入 fake Waiter
// 请走 newHelmToolWithWaiter。双构造器模式与 pod_restart / scale_deployment 保持一致。
func newHelmTool(client *bcsapi.Client) tool.Tool {
	return newHelmToolWithWaiter(client, NewBCSReadyWaiter(client, WaiterConfig{}))
}

// newHelmToolWithWaiter 可注入 Waiter 的构造器，仅供内部测试调用。
func newHelmToolWithWaiter(client *bcsapi.Client, waiter ReadyWaiter) tool.Tool {
	if waiter == nil {
		waiter = NewNoopReadyWaiter()
	}
	fn := func(ctx context.Context, in HelmInput) (*Result, error) {
		action := strings.ToLower(strings.TrimSpace(in.Action))
		if action == "" || in.ClusterID == "" {
			return nil, fmt.Errorf("action 和 cluster_id 为必填项")
		}
		needRelease := action != "list"
		if needRelease && (in.ReleaseName == "" || in.Namespace == "") {
			return nil, fmt.Errorf("action=%s 时 namespace 与 release_name 为必填项", action)
		}

		// HITL 安全门：写操作走统一 hitl 框架，未 confirmed 时返回 Plan。
		writeOps := map[string]bool{"rollback": true, "install": true, "uninstall": true}
		var writePlan *hitl.Plan
		if writeOps[action] {
			p := buildHelmPlan(in, action)
			writePlan = &p
			if pending, need := hitl.Require(in.Confirmed, p); need {
				return &Result{
					OK:      false,
					Message: pending.Message,
					Data:    pending,
				}, nil
			}
		}

		// 分支到不同 API endpoint
		var (
			path    string
			reqBody map[string]any
		)
		switch action {
		case "list":
			path = fmt.Sprintf("/bcsapi/v4/helmmanager/v1/releases/list?cluster_id=%s", in.ClusterID)
		case "history":
			path = fmt.Sprintf("/bcsapi/v4/helmmanager/v1/clusters/%s/namespaces/%s/releases/%s/history",
				in.ClusterID, in.Namespace, in.ReleaseName)
		case "rollback":
			if in.Revision <= 0 {
				return nil, fmt.Errorf("rollback 必须指定 revision")
			}
			path = fmt.Sprintf("/bcsapi/v4/helmmanager/v1/clusters/%s/namespaces/%s/releases/%s/rollback",
				in.ClusterID, in.Namespace, in.ReleaseName)
			reqBody = map[string]any{"revision": in.Revision}
		case "install":
			path = fmt.Sprintf("/bcsapi/v4/helmmanager/v1/clusters/%s/namespaces/%s/releases/%s",
				in.ClusterID, in.Namespace, in.ReleaseName)
			reqBody = map[string]any{"chart": in.Chart, "values": in.Values}
		case "uninstall":
			path = fmt.Sprintf("/bcsapi/v4/helmmanager/v1/clusters/%s/namespaces/%s/releases/%s/uninstall",
				in.ClusterID, in.Namespace, in.ReleaseName)
		default:
			return nil, fmt.Errorf("不支持的 action: %s", action)
		}

		var respData map[string]any
		var err error
		if reqBody != nil {
			err = client.PostJSON(ctx, path, reqBody, &respData)
		} else {
			err = client.Get(ctx, path, nil, &respData)
		}

		// 统一结果装配 + 审计（仅写操作）
		emitAudit := func(result *Result, apiErr error) {
			if writePlan == nil {
				return
			}
			audit.Emit(audit.Event{
				Agent:    "repair_agent",
				Action:   writePlan.Action,
				Severity: string(writePlan.Severity),
				Target:   writePlan.Target,
				Params:   writePlan.Params,
				Success:  result != nil && result.OK,
				Err:      apiErr,
				Mock:     client.IsMock() || (result != nil && result.Mock),
			})
		}

		if errors.Is(err, bcsapi.ErrMockMode) {
			r := mockHelm(in, action)
			// D19.7：Mock 模式也走 Waiter（Mock Waiter 会在 ~50ms 内返回 ready），
			// 这样 Mock 和真实路径的响应 schema 完全一致——LLM 不会因为模式不同走两套逻辑。
			attachHelmWaitInfo(ctx, waiter, r, in, action)
			emitAudit(r, nil)
			return r, nil
		}
		if err != nil {
			emitAudit(&Result{OK: false}, err)
			return nil, fmt.Errorf("BCS Helm %s 失败: %w", action, err)
		}
		r := &Result{OK: true, Data: respData}
		// D19.7：写路径成功后接入 ReadyWaiter
		attachHelmWaitInfo(ctx, waiter, r, in, action)
		emitAudit(r, nil)
		return r, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_helm_manage"),
		function.WithDescription(
			"BCS Helm Release 管理：list(列出) / history(历史版本) / rollback(回滚) / install(部署) / uninstall(卸载)。"+
				"⚠ 写操作必须先让用户确认，然后以 confirmed=true 重新调用；"+
				"建议修复流程：先查 history → 用户确认目标 revision → rollback 且 confirmed=true。"+
				"D19.7：rollback/install 可传 wait_for_ready=true + wait_deployment=<名字> 同步等工作负载收敛；"+
				"一个 release 可能关联多个 Deployment，本工具只等单个，需先 action=history 确认目标名。"+
				"uninstall 下该参数会被忽略（语义相反）。长耗时建议配合 async_tools.job_submit。"),
	)
}

// attachHelmWaitInfo 将 wait_for_ready 信息塞进 Result.Data（map 语义下原地写入）。
//
// # 为什么单拉一层
//
// helm 的 Data 可能是 map 或具体 struct（mockHelm 返回 map），真实路径 respData 也是 map；
// 统一一个辅助函数避免两条分支各写一遍，同时保留 Data 非 map 时的安全降级——虽然当前
// 实现不会出现非 map，但这是防御性写法，未来若有人改返回类型不会悄悄丢字段。
//
// # 三条规则
//
//  1. action!=rollback/install（即 list/history/uninstall）→ 永不 wait
//     - uninstall 的语义是"资源消失"，与"Deployment ready"语义相反，放进来只会污染指标
//     - list/history 是纯读，加等待毫无意义
//  2. WaitForReady=true 但 WaitDeployment="" → 不调 Waiter，但显式标 status=skipped
//     reason=wait_deployment_required，告知 LLM "你忘了传 deployment 名"，便于下一轮补齐
//  3. WaitForReady=true 且 WaitDeployment 非空 → 真正调用 Waiter，Mode="helm_"+action
//     Mode 带 action 后缀是为了让指标按 helm_rollback / helm_install 分桶——
//     rollback 比 install 更敏感（已有生产流量），面板上能快速隔离
func attachHelmWaitInfo(ctx context.Context, waiter ReadyWaiter, r *Result, in HelmInput, action string) {
	if r == nil {
		return
	}
	data, ok := r.Data.(map[string]any)
	if !ok {
		// 防御性降级：Data 不是 map 就不挂 wait_for_ready，避免破坏既有返回结构
		return
	}

	// 规则 1：非 rollback/install 永不 wait
	if action != "rollback" && action != "install" {
		// 注意：不写 wait_for_ready 字段——不写比写 attempted=false 更清晰，
		// schema 上只有"可能 wait 的 action"才会出现此字段
		return
	}

	// 规则 2：开关关闭，**完全不写** wait_for_ready 字段。
	//
	// 设计取舍：与 scale 工具的"写 attempted:false"不同——helm 的 release 可能关联多个
	// 工作负载，wait_for_ready 字段在没开启时本身就不是稳定语义（上面规则 1 也是不写），
	// 这里保持一致：helm 工具只在"实际尝试 wait"时才出现该字段，schema 更紧凑。
	if !in.WaitForReady {
		return
	}

	// 规则 2 另一半：开关开了但没给 deployment 名——明确上报 skipped，给 LLM 反馈信号
	if in.WaitDeployment == "" {
		data["wait_for_ready"] = map[string]any{
			"attempted": false,
			"status":    "skipped",
			"reason":    "wait_deployment_required",
			"hint":      "helm release 可能关联多个工作负载，请先 action=history 确认目标 Deployment 名再传 wait_deployment",
		}
		return
	}

	// 规则 3：真正调用 Waiter
	data["wait_for_ready"] = runReadyWait(ctx, waiter, ReadySpec{
		Mode:       "helm_" + action,
		ClusterID:  in.ClusterID,
		Namespace:  in.Namespace,
		Deployment: in.WaitDeployment,
	})
}

func mockHelm(in HelmInput, action string) *Result {
	now := time.Now()
	var data any
	switch action {
	case "list":
		data = map[string]any{
			"releases": []map[string]any{
				{"name": "game-core", "namespace": "letsgo", "chart": "game-core-1.2.3", "status": "deployed", "revision": 5},
				{"name": "game-gateway", "namespace": "letsgo", "chart": "game-gateway-0.9.1", "status": "deployed", "revision": 2},
			},
		}
	case "history":
		data = map[string]any{
			"release": in.ReleaseName,
			"history": []map[string]any{
				{"revision": 5, "status": "deployed", "chart": "game-core-1.2.3", "updated": now.Add(-2 * time.Hour).Format(time.RFC3339)},
				{"revision": 4, "status": "superseded", "chart": "game-core-1.2.2", "updated": now.Add(-24 * time.Hour).Format(time.RFC3339)},
				{"revision": 3, "status": "superseded", "chart": "game-core-1.2.1", "updated": now.Add(-72 * time.Hour).Format(time.RFC3339)},
			},
		}
	case "rollback":
		data = map[string]any{
			"release":  in.ReleaseName,
			"revision": in.Revision,
			"status":   "ROLLED_BACK (mock)",
		}
	case "install":
		data = map[string]any{"release": in.ReleaseName, "chart": in.Chart, "status": "INSTALLED (mock)"}
	case "uninstall":
		data = map[string]any{"release": in.ReleaseName, "status": "UNINSTALLED (mock)"}
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式（action=" + action + "）",
		Data:    data,
	}
}

// buildHelmPlan 根据 action 构造 HITL Plan。
func buildHelmPlan(in HelmInput, action string) hitl.Plan {
	target := fmt.Sprintf("%s / %s / %s", in.ClusterID, in.Namespace, in.ReleaseName)
	params := map[string]any{
		"cluster_id":   in.ClusterID,
		"namespace":    in.Namespace,
		"release_name": in.ReleaseName,
	}
	switch action {
	case "rollback":
		params["revision"] = in.Revision
		if in.WaitForReady && in.WaitDeployment != "" {
			params["wait_for_ready"] = true
			params["wait_deployment"] = in.WaitDeployment
		}
		return hitl.Plan{
			Action:       "bcs.helm.rollback",
			Severity:     hitl.SeverityHigh,
			Target:       target,
			SideEffect:   fmt.Sprintf("release %q 将回滚到 revision=%d，滚动重启对应 Pod", in.ReleaseName, in.Revision),
			ImpactScope:  fmt.Sprintf("命名空间 %s 下所有关联 Deployment/StatefulSet 实例", in.Namespace),
			RollbackPlan: "若回滚后仍异常，可再次调用本工具指向更早的 revision",
			Params:       params,
		}
	case "install":
		params["chart"] = in.Chart
		if in.WaitForReady && in.WaitDeployment != "" {
			params["wait_for_ready"] = true
			params["wait_deployment"] = in.WaitDeployment
		}
		return hitl.Plan{
			Action:       "bcs.helm.install",
			Severity:     hitl.SeverityHigh,
			Target:       target,
			SideEffect:   fmt.Sprintf("部署 Chart %q 到 release %q", in.Chart, in.ReleaseName),
			ImpactScope:  fmt.Sprintf("命名空间 %s 将创建或覆盖相应 K8s 资源", in.Namespace),
			RollbackPlan: "若部署失败，可通过 action=rollback 回到上一个 deployed revision",
			Params:       params,
		}
	case "uninstall":
		return hitl.Plan{
			Action:        "bcs.helm.uninstall",
			Severity:      hitl.SeverityCritical,
			Target:        target,
			SideEffect:    fmt.Sprintf("release %q 将被完全卸载，所有关联资源会被删除", in.ReleaseName),
			ImpactScope:   fmt.Sprintf("命名空间 %s 下 Deployment / Service / ConfigMap 等全部清理", in.Namespace),
			RollbackPlan:  "无法自动回滚，请先 action=history 确认当前状态再决定是否继续",
			Params:        params,
			RequireReason: true,
		}
	}
	return hitl.Plan{Action: "bcs.helm." + action, Severity: hitl.SeverityHigh, Target: target, Params: params}
}