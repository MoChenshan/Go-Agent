// Package bcstools —— bcs_hpa_patch（HPA 写操作，D20.2 新增，闭合 D20 HPA 能力闭环）。
//
// # 为什么需要这个工具
//
// D20/D20.1 已经让 scale_deployment 和 pod_restart.rollout_restart 在执行前能
// 感知到 HPA 的存在，并把冲突信息原样摆到 Plan 上。但**用户看到冲突之后想做的
// 下一个动作 —— "那帮我改一下 HPA max 到 20" —— 现在还做不了**。
//
// 用户的解决路径被切成两半：
//
//	1) 在对话里触发 scale → 看到"HPA max=10 挡着"的 warn →
//	2) 回到终端敲 kubectl 改 HPA →
//	3) 回对话里再触发一次 scale
//
// 这个"从对话里脱离出去又回来"的动作模式正是 oncall 效率的杀手。D20.2 补齐写路径后，
// 链路变成：scale 看到 HPA 冲突 → 对话里直接让 agent 调 `bcs_hpa_patch` →
// 再 scale。整个过程不离开对话。
//
// # 为什么放在 HPA 能力闭环的最后一块
//
// 这是"感知 → 展示 → 决策 → 写入"四步链的最后一步。前三步已经在 D20/D20.1 建好：
//
//	D20    感知 + scale 冲突展示
//	D20.1  感知 + rollout 冲突展示 + 抽象复用毕业考
//	D20.2  写入 HPA 自身（本文件）
//
// 做完这一步之后，HPA 能力在本项目内就是**一块完整的积木**，接下来几个月不用再回来动。
//
// # 四大风险模型与对应防护
//
// HPA 写比 scale 写**风险更高**，因为 HPA 是"副本数的法官"：scale 改错可以被 HPA
// 纠正回来，HPA 改错则直接决定后续所有扩缩容行为的上下限。因此防护层级也更严：
//
//  1) **min=0 灾难**：minReplicas 设为 0 允许 HPA 缩到无实例，生产流量直接失去承载方。
//     → R2：minReplicas 必须 >= 1（R1 是必要字段检查）
//
//  2) **max 失控**：maxReplicas 设得过高会在突发负载下把集群资源吃光，打爆其他 Pod。
//     → R3：maxReplicas >= minReplicas（逻辑约束）
//     → R4：maxReplicas > HPAMaxCeiling(默认 100) 强制 Critical + RequireReason
//
//  3) **幅度突变**：从 max=5 改到 max=50（10 倍放大）虽然不触发 R4 绝对天花板，
//     但在业务不知情时会让 HPA 在下一次负载波动里突然扩到上限，引发二次事故。
//     → R5：max 增长 > 3x 或降幅 > 50% 升档 Critical
//
//  4) **并发覆盖**：两个 oncall 同时改同一个 HPA 会出现"后写覆盖前写"的幽灵问题。
//     → R6：expected_current_max 若填写，必须与实际现值一致（类似 scale 的
//           expected_current，防 TOCTOU）
//
// 此外：
//   - prod ns（含 prod/production）Severity 直接 Critical 起步（比 scale 的 High 起步更严）
//   - op=disable 冻结 HPA 相当于"把方向盘拆了"，始终 Critical + RequireReason
//
// # 操作矩阵
//
//	op=get         —— 只读 HPA 当前配置，不走 HITL
//	op=set_min     —— 只改 minReplicas
//	op=set_max     —— 只改 maxReplicas
//	op=set_range   —— 同时改 min 和 max
//	op=disable     —— 把 maxReplicas=minReplicas，冻结 HPA 弹性（高危）
//
// 不提供 op=delete（完整删除 HPA）：这等价于 disable 的更危险版本，目前没有真实
// on-call 场景需要，未来真要加也应该单独立技能。
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

// HPA 写风险阈值常量。
//
// 这些值的选取思路：
//   - HPAMaxCeiling=100 是"单 Deployment 合理上限"的经验值；超过说明要么业务分拆
//     不合理，要么是配置误填（比如漏了一位小数点）
//   - GrowthRatioTrigger=3 来自于业务观测：HPA max 的调整一般 2-3 倍为一次"合理扩容"；
//     超过 3 倍基本只发生在重算容量 / 业务形态变化的场景，这类事件值得 Critical
//   - ShrinkRatioTrigger=0.5 对称逻辑：max 砍半以上通常是下线准备或重大重构，也值得
//     拦一下让人想清楚
const (
	// HPAMaxCeiling 为 maxReplicas 的绝对天花板（超过升 Critical + RequireReason）。
	HPAMaxCeiling = 100
	// GrowthRatioTrigger 为 max 放大倍数触发升档的阈值（新值 / 旧值 >= 此值）。
	GrowthRatioTrigger = 3.0
	// ShrinkRatioTrigger 为 max 缩小倍数触发升档的阈值（新值 / 旧值 <= 此值）。
	ShrinkRatioTrigger = 0.5
)

// HPAPatchInput 为 bcs_hpa_patch 工具入参。
//
// 五个 op 共用一套入参；每个 op 必要字段在 doXxx 里校验。
type HPAPatchInput struct {
	Op                 string `json:"op"                   description:"操作类型（必填）：get（只读）/ set_min（改 minReplicas）/ set_max（改 maxReplicas）/ set_range（同时改 min+max）/ disable（max=min 冻结弹性，高危）"`
	ClusterID          string `json:"cluster_id"           description:"BCS 集群 ID（必填）"`
	Namespace          string `json:"namespace"            description:"Kubernetes 命名空间（必填）"`
	Name               string `json:"name"                 description:"HPA 资源名（必填）；用 bcs_resource_query 可先列出 ns 内所有 HPA"`
	MinReplicas        int    `json:"min_replicas"         description:"目标 minReplicas（op=set_min/set_range 必填；必须 >= 1）"`
	MaxReplicas        int    `json:"max_replicas"         description:"目标 maxReplicas（op=set_max/set_range 必填；必须 >= min_replicas，且超过 HPAMaxCeiling=100 需 reason）"`
	ExpectedCurrentMax int    `json:"expected_current_max" description:"（可选）期望的现值 maxReplicas；若填写则必须与实际一致，否则拒绝（防并发覆盖，类似 scale 的 expected_current）"`
	Reason             string `json:"reason"               description:"变更原因；Critical 场景（max>100 / 幅度突变 / disable / prod ns）必填"`
	Confirmed          bool   `json:"confirmed"            description:"是否已获人工确认；写操作必须 true 才真正下发"`
}

// newHPAPatchTool 构造 bcs_hpa_patch 工具。
func newHPAPatchTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in HPAPatchInput) (*Result, error) {
		op := strings.ToLower(strings.TrimSpace(in.Op))
		if op == "" {
			return nil, fmt.Errorf("op 为必填项（get / set_min / set_max / set_range / disable）")
		}
		if in.ClusterID == "" || in.Namespace == "" || in.Name == "" {
			return nil, fmt.Errorf("cluster_id / namespace / name 均为必填")
		}
		switch op {
		case "get":
			return doHPAGet(ctx, client, in)
		case "set_min", "set_max", "set_range", "disable":
			return doHPAWrite(ctx, client, in, op)
		default:
			return nil, fmt.Errorf("不支持的 op: %q（可选：get / set_min / set_max / set_range / disable）", op)
		}
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_hpa_patch"),
		function.WithDescription(
			"BCS HPA 写操作工具，五种操作：get(只读) / set_min(改下限) / set_max(改上限) / set_range(同时改) / disable(冻结弹性)。"+
				"⚠ 必备防护：min 不得为 0；max >= min；max > 100 或幅度突变（增 3x 或降 50%）升 Critical + RequireReason。"+
				"⚠ 支持 expected_current_max 并发守护；prod ns 自动升 Critical。"+
				"⚠ 典型链路：先用 op=get 查当前值；再 set_min/set_max/set_range 调整。修改后推荐用 bcs_scale_deployment 验证目标落地。",
		),
	)
}

// =============================================================================
// op=get：只读，不走 HITL
// =============================================================================

func doHPAGet(ctx context.Context, client *bcsapi.Client, in HPAPatchInput) (*Result, error) {
	info, err := GetHPAByName(ctx, client, in.ClusterID, in.Namespace, in.Name)
	if err != nil {
		return nil, fmt.Errorf("读取 HPA 失败: %w", err)
	}
	// Mock 模式：返回样例数据
	if client != nil && client.IsMock() {
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：返回样例 HPA %s/%s/%s", in.ClusterID, in.Namespace, in.Name),
			Data: map[string]any{
				"cluster_id":   in.ClusterID,
				"namespace":    in.Namespace,
				"name":         in.Name,
				"min_replicas": 2,
				"max_replicas": 10,
				"current_spec": 3,
				"found":        true,
			},
		}, nil
	}
	if !info.Found {
		return &Result{
			OK: false,
			Message: fmt.Sprintf("HPA %s/%s/%s 不存在", in.ClusterID, in.Namespace, in.Name),
			Data: map[string]any{"found": false},
		}, nil
	}
	return &Result{
		OK: true,
		Data: map[string]any{
			"cluster_id":   in.ClusterID,
			"namespace":    in.Namespace,
			"name":         in.Name,
			"min_replicas": info.MinReplicas,
			"max_replicas": info.MaxReplicas,
			"current_spec": info.CurrentSpec,
			"found":        true,
		},
	}, nil
}

// =============================================================================
// op=set_min/set_max/set_range/disable：写路径，统一走 HITL + 多层防护
// =============================================================================

// doHPAWrite 四个写 op 的统一入口。
//
// 流程固定：
//
//	1) 读取现值（为 diff / 幅度保护 / 并发守护做准备）
//	2) 计算目标值（根据 op 不同算法不同）
//	3) R1 必要字段检查（op 相应参数必填）
//	4) R2 min >= 1
//	5) R3 max >= min
//	6) R6 expected_current_max 并发守护
//	7) Severity 计算（R4 天花板 / R5 幅度 / prod ns / disable）
//	8) buildHPAPlan 构建 Plan + HITL Require
//	9) 真实 PATCH 下发
//	10) 审计入账（无论成败）
func doHPAWrite(ctx context.Context, client *bcsapi.Client, in HPAPatchInput, op string) (*Result, error) {
	// 1) 读现值
	before, err := GetHPAByName(ctx, client, in.ClusterID, in.Namespace, in.Name)
	isMock := client != nil && client.IsMock()
	if err != nil && !isMock {
		return nil, fmt.Errorf("读取 HPA 现值失败（写操作需先知道现值）: %w", err)
	}
	if isMock {
		// Mock 模式下伪造一个合理的现值，让防护逻辑能被测试覆盖到
		before = HPAInfo{Found: true, Name: in.Name, MinReplicas: 2, MaxReplicas: 10}
	}
	if !before.Found {
		return nil, fmt.Errorf("HPA %s/%s/%s 不存在，无法修改（可先用 op=get 确认）",
			in.ClusterID, in.Namespace, in.Name)
	}

	// 2) 计算目标 min/max
	targetMin, targetMax, rerr := resolveHPATarget(op, in, before)
	if rerr != nil {
		return nil, rerr
	}

	// 3)~5) 硬性约束检查（R2/R3 —— 拒绝而非降档）
	if targetMin < 1 {
		return nil, fmt.Errorf("R2 违规：min_replicas 不得小于 1（避免 HPA 缩到 0 副本导致无实例承载流量）")
	}
	if targetMax < targetMin {
		return nil, fmt.Errorf("R3 违规：max_replicas(%d) 不得小于 min_replicas(%d)", targetMax, targetMin)
	}

	// 6) 并发守护（R6）
	if in.ExpectedCurrentMax > 0 && in.ExpectedCurrentMax != before.MaxReplicas {
		return nil, fmt.Errorf("R6 并发守护：预期现 max=%d 但实际为 %d —— 可能有其他会话刚改过，请重新 get 确认",
			in.ExpectedCurrentMax, before.MaxReplicas)
	}

	// 7) Severity 计算
	severity, requireReason := classifyHPASeverity(in, before, targetMin, targetMax, op)

	// 8) Plan + HITL
	//
	// 注意：reason 强制校验**必须**放在 hitl.Require 之后，否则会破坏 plan→confirm 两段式语义：
	// plan 阶段调用方还没看见 Severity / RequireReason，强制要 reason 等于把"协商提示"
	// 变成"前置硬拒绝"，违反 D6 设计。RequireReason=true 的语义是"confirmed=true 且空 reason
	// 才拒绝"，让 LLM 从 Plan 中读到 RequireReason 后再带 reason 重发。
	plan := buildHPAPlan(in, op, before, targetMin, targetMax, severity, requireReason)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	// hitl 已放行（confirmed=true），此时 RequireReason 仍未补就硬拒绝
	if requireReason && strings.TrimSpace(in.Reason) == "" {
		return nil, fmt.Errorf("本次 HPA 修改达到 Critical 风险等级，必须填写 reason 说明变更原因")
	}

	// 9) 真实 PATCH 下发
	after := HPAInfo{
		Found: true, Name: before.Name,
		MinReplicas: targetMin, MaxReplicas: targetMax,
		CurrentSpec: before.CurrentSpec, Raw: before.Raw,
	}
	if isMock {
		emitHPAPatchAudit(client, in, op, severity, true, nil, before, after)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：HPA %s/%s/%s 已修改为 min=%d/max=%d",
				in.ClusterID, in.Namespace, in.Name, targetMin, targetMax),
			Data: hpaWriteResultData(in, before, after, op),
		}, nil
	}
	patchErr := patchHPAReplicas(ctx, client, in.ClusterID, in.Namespace, in.Name, targetMin, targetMax)
	if patchErr != nil {
		emitHPAPatchAudit(client, in, op, severity, false, patchErr, before, after)
		return nil, fmt.Errorf("HPA PATCH 失败: %w", patchErr)
	}

	// 10) 成功审计
	emitHPAPatchAudit(client, in, op, severity, true, nil, before, after)
	return &Result{
		OK: true,
		Data: hpaWriteResultData(in, before, after, op),
	}, nil
}

// resolveHPATarget 按 op 计算目标 min/max。
//
// op=disable 的语义是"把 HPA 变成定数"：max 与 min 都锁到现 min。
// 这样 HPA 仍然存在（可以审计被冻结），但弹性被完全消除。
// 用户若想完全解除 HPA，应该直接删除，而不是用 disable。
func resolveHPATarget(op string, in HPAPatchInput, before HPAInfo) (int, int, error) {
	switch op {
	case "set_min":
		if in.MinReplicas <= 0 {
			return 0, 0, fmt.Errorf("op=set_min 必须指定 min_replicas（且 >= 1）")
		}
		return in.MinReplicas, before.MaxReplicas, nil
	case "set_max":
		if in.MaxReplicas <= 0 {
			return 0, 0, fmt.Errorf("op=set_max 必须指定 max_replicas（且 >= 1）")
		}
		return before.MinReplicas, in.MaxReplicas, nil
	case "set_range":
		if in.MinReplicas <= 0 || in.MaxReplicas <= 0 {
			return 0, 0, fmt.Errorf("op=set_range 必须同时指定 min_replicas 和 max_replicas")
		}
		return in.MinReplicas, in.MaxReplicas, nil
	case "disable":
		// 冻结：max = min = 现 min；若现 min 本就过小（< 1）则强制 1
		m := before.MinReplicas
		if m < 1 {
			m = 1
		}
		return m, m, nil
	default:
		return 0, 0, fmt.Errorf("内部错误：未识别的 op %q", op)
	}
}

// classifyHPASeverity 计算 Severity + 是否要求 reason。
//
// 规则（从高到低，命中即止）：
//
//	1) op=disable            → Critical + RequireReason（冻结弹性 = 拆方向盘）
//	2) prod ns               → Critical 起步；若未命中其他升档条件仍 RequireReason=true
//	3) targetMax > 天花板    → Critical + RequireReason
//	4) 幅度突变（R5）        → Critical（不强制 reason，让用户在 confirm 里说）
//	5) 其他                  → High（HPA 改动起步就比 scale 严一级）
func classifyHPASeverity(in HPAPatchInput, before HPAInfo, targetMin, targetMax int, op string) (hitl.Severity, bool) {
	nsLower := strings.ToLower(in.Namespace)
	isProd := strings.Contains(nsLower, "prod") || strings.Contains(nsLower, "production")

	// 1) disable：最高级
	if op == "disable" {
		return hitl.SeverityCritical, true
	}
	// 3) 天花板
	if targetMax > HPAMaxCeiling {
		return hitl.SeverityCritical, true
	}
	// 2) prod ns：起步 Critical
	if isProd {
		return hitl.SeverityCritical, true
	}
	// 4) 幅度突变
	if before.MaxReplicas > 0 {
		ratio := float64(targetMax) / float64(before.MaxReplicas)
		if ratio >= GrowthRatioTrigger || ratio <= ShrinkRatioTrigger {
			return hitl.SeverityCritical, false
		}
	}
	// 5) 默认 High
	return hitl.SeverityHigh, false
}

// buildHPAPlan 构建 HITL Plan。
//
// 与其他写工具的差异：
//   - Target 用 HPA 路径（不是 Deployment）
//   - ImpactScope 强调"后续所有扩缩容上下限均受此决定"—— HPA 是副本数法官
//   - RollbackPlan 包含原 min/max 数值，确保一眼可见如何复原
//   - Params 暴露幅度倍数（growth_ratio）和天花板触发标记（over_ceiling）便于审计
func buildHPAPlan(in HPAPatchInput, op string, before HPAInfo, targetMin, targetMax int,
	severity hitl.Severity, requireReason bool) hitl.Plan {

	// 核心语义：op 决定 side effect 描述
	var sideEffect string
	switch op {
	case "set_min":
		sideEffect = fmt.Sprintf("修改 HPA 下限 minReplicas: %d → %d", before.MinReplicas, targetMin)
	case "set_max":
		sideEffect = fmt.Sprintf("修改 HPA 上限 maxReplicas: %d → %d", before.MaxReplicas, targetMax)
	case "set_range":
		sideEffect = fmt.Sprintf("修改 HPA 区间 [%d,%d] → [%d,%d]",
			before.MinReplicas, before.MaxReplicas, targetMin, targetMax)
	case "disable":
		sideEffect = fmt.Sprintf("⚠ 冻结 HPA 弹性：[%d,%d] → [%d,%d]（max=min，HPA 事实上失效）",
			before.MinReplicas, before.MaxReplicas, targetMin, targetMax)
	}

	params := map[string]any{
		"op":              op,
		"before_min":      before.MinReplicas,
		"before_max":      before.MaxReplicas,
		"target_min":      targetMin,
		"target_max":      targetMax,
		"over_ceiling":    targetMax > HPAMaxCeiling,
		"ceiling":         HPAMaxCeiling,
	}
	if before.MaxReplicas > 0 {
		params["growth_ratio"] = float64(targetMax) / float64(before.MaxReplicas)
	}
	if in.ExpectedCurrentMax > 0 {
		params["expected_current_max"] = in.ExpectedCurrentMax
	}

	return hitl.Plan{
		Action:   "bcs.hpa." + op,
		Severity: severity,
		Target:   fmt.Sprintf("%s/%s/%s", in.ClusterID, in.Namespace, in.Name),
		SideEffect: sideEffect,
		ImpactScope: fmt.Sprintf(
			"HPA 是副本数法官，本次修改会影响后续所有扩缩容的上下限。"+
				"修改生效后 HPA 可能立即基于当前负载调整副本数到新区间内（秒级），"+
				"原托管的 Deployment=%s 将按新 [%d,%d] 被约束。",
			extractScaleTargetName(before), targetMin, targetMax,
		),
		RollbackPlan: fmt.Sprintf(
			"若需回滚，请用 op=set_range 将 min/max 改回 [%d,%d]；或使用 expected_current_max=%d 确保回滚前无其他变更。",
			before.MinReplicas, before.MaxReplicas, targetMax,
		),
		Params:        params,
		RequireReason: requireReason,
	}
}

// extractScaleTargetName 从 HPA Raw 里挖出 scaleTargetRef.name，用于 Plan 展示。
// 取不到时返回 "(unknown)"。
func extractScaleTargetName(info HPAInfo) string {
	if info.Raw == nil {
		return "(unknown)"
	}
	spec, _ := info.Raw["spec"].(map[string]any)
	if spec == nil {
		return "(unknown)"
	}
	ref, _ := spec["scaleTargetRef"].(map[string]any)
	if ref == nil {
		return "(unknown)"
	}
	if name, _ := ref["name"].(string); name != "" {
		return name
	}
	return "(unknown)"
}

// =============================================================================
// PATCH 下发
// =============================================================================

// patchHPAReplicas 向 BCS 转发的 K8s 原生 API 发 PATCH 请求。
//
// BCS 约定：/clusters/{cluster}/apis/autoscaling/v2/namespaces/{ns}/horizontalpodautoscalers/{name}
//
// 为什么用 autoscaling/v2 而不是 v1：
//   - v1 只支持 CPU 指标，绝大多数生产 HPA 都用 v2 的自定义指标（QPS / 延迟 等）
//   - v2 向下兼容 v1 对象结构，PATCH spec.minReplicas/maxReplicas 两个字段在两版本都可用
//
// PATCH body 使用 strategic merge patch 格式：{"spec":{"minReplicas":N,"maxReplicas":M}}。
// 未改的字段（scaleTargetRef/metrics/behavior）会被 K8s 自动保留。
func patchHPAReplicas(ctx context.Context, client *bcsapi.Client, clusterID, namespace, name string,
	minR, maxR int) error {
	path := fmt.Sprintf(
		"/clusters/%s/apis/autoscaling/v2/namespaces/%s/horizontalpodautoscalers/%s",
		clusterID, namespace, name,
	)
	body := map[string]any{
		"spec": map[string]any{
			"minReplicas": minR,
			"maxReplicas": maxR,
		},
	}
	var resp map[string]any
	if err := client.PatchJSON(ctx, path, body, &resp); err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			return nil // Mock 模式静默成功
		}
		return err
	}
	return nil
}

// =============================================================================
// 审计 & 辅助
// =============================================================================

// emitHPAPatchAudit 统一审计入账。
//
// Params 字段清单（事后追查必备）：
//   - op / cluster_id / namespace / name 基础信息
//   - before_min/before_max / target_min/target_max 完整变更前后态
//   - growth_ratio 幅度倍数（便于后期做"异常改动率"告警）
//   - over_ceiling 是否触发天花板防护
//   - reason（若填写）
func emitHPAPatchAudit(client *bcsapi.Client, in HPAPatchInput, op string,
	severity hitl.Severity, ok bool, err error, before, after HPAInfo) {
	params := map[string]any{
		"op":         op,
		"cluster_id": in.ClusterID,
		"namespace":  in.Namespace,
		"name":       in.Name,
		"before_min": before.MinReplicas,
		"before_max": before.MaxReplicas,
		"target_min": after.MinReplicas,
		"target_max": after.MaxReplicas,
	}
	if before.MaxReplicas > 0 {
		params["growth_ratio"] = float64(after.MaxReplicas) / float64(before.MaxReplicas)
	}
	if after.MaxReplicas > HPAMaxCeiling {
		params["over_ceiling"] = true
	}
	if in.ExpectedCurrentMax > 0 {
		params["expected_current_max"] = in.ExpectedCurrentMax
	}
	if in.Reason != "" {
		params["reason"] = in.Reason
	}
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   "bcs.hpa." + op,
		Severity: string(severity),
		Target:   fmt.Sprintf("%s/%s/%s", in.ClusterID, in.Namespace, in.Name),
		Params:   params,
		Success:  ok,
		Err:      err,
		Mock:     client != nil && client.IsMock(),
	})
}

// hpaWriteResultData 构建统一的 Result.Data 结构（Mock 和真实路径共用）。
func hpaWriteResultData(in HPAPatchInput, before, after HPAInfo, op string) map[string]any {
	return map[string]any{
		"cluster_id":   in.ClusterID,
		"namespace":    in.Namespace,
		"name":         in.Name,
		"op":           op,
		"before_min":   before.MinReplicas,
		"before_max":   before.MaxReplicas,
		"min_replicas": after.MinReplicas,
		"max_replicas": after.MaxReplicas,
	}
}
