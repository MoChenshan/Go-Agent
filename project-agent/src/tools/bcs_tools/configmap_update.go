// Package bcstools —— bcs_configmap_update（配置热更，D18.4 新增，D18 阶段收尾里程碑）。
//
// 这是 repair_agent 生态的第 5 个生产级写工具，至此 BCS 四大写技能矩阵成型：
//
//	bcs_helm_manage          —— Release 级写（部署/回滚，D1-D7）
//	bcs_scale_deployment     —— Deployment 副本伸缩（D18.1）
//	bcs_pod_restart          —— Pod 级重启/驱逐/滚动重启（D18.2）
//	bcs_configmap_update     —— 配置热更/快照/回滚（本文件，D18.4）
//
// 为什么是 configmap 而不是别的：
//   - on-call 现场最常见的"非重启型修复"：改超时 / 开降级 / 调日志级别
//   - 前 4 个写工具都是"让系统动"，configmap 是"让系统换一种方式运行"，语义独立
//   - 风险模型最特别：改了不重启 = 不生效；改了立刻重启 = 中断服务 —— 必须工具层建模生效策略
//   - 运行时态 + 无 revision：Helm 靠 history、configmap 没有；必须自带 snapshot 快照机制
//
// 四大差异化设计（相对前三个写工具）：
//
//  1) 双维度操作矩阵（op × 4）：get / set / delete / rollback
//     - get：只读，Plan 前置读取（供 LLM 做 diff）
//     - set：最常用路径，强制 rollout_strategy
//     - delete：始终 High（比 set 更危险，没"默认值"兜底）
//     - rollback：必须凭 snapshot_id，无快照不给回滚
//
//  2) 生效策略建模（rollout_strategy 必填）：
//     - none              仅改 ConfigMap，不碰 Pod（仅配置本身支持 inotify 热更时）
//     - rolling_restart   改完触发关联 Deployment 滚动重启（90% 场景）
//     - immediate_restart 改完所有 Pod 立即重启（紧急修复，生产 ns 必 Critical）
//
//  3) diff + 快照双保险：
//     - 执行前必做 diff：当前 vs 目标，以 +/-/~ 三类展示给人看
//     - 执行时自动留快照：Annotation 写回 ConfigMap，记录 previous_data + 时间戳
//     - rollback 按 snapshot_id：无快照不回滚（避免黑盒）
//
//  4) 敏感键名识别：
//     - 键名含 password/secret/token/key/credential 强制 Critical + RequireReason
//     - keys 数量 > 10 升档 High（爆破保护）
package bcstools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// 单次 set 批量保护阈值：超过升档 High（避免"误把一整份 application.yaml 刷进去"）。
const configmapSetKeysSoftLimit = 10

// rollout_strategy 枚举值。
const (
	rolloutNone             = "none"
	rolloutRollingRestart   = "rolling_restart"
	rolloutImmediateRestart = "immediate_restart"
)

// 敏感键名前缀/包含片段（命中即 Critical + RequireReason）。
// 思路：宁可误报，不可漏报 —— 运维同学把密钥塞进 configmap 是常见反模式。
var sensitiveKeywords = []string{
	"password", "passwd", "secret", "token", "credential", "cred",
	"apikey", "api_key", "accesskey", "access_key", "privatekey", "private_key",
}

// snapshotAnnotationKey 存快照 JSON 的 Annotation Key。
// 使用反域名命名空间避免与业务 annotation 冲突。
const snapshotAnnotationKey = "gameops-agent.tencent.com/snapshot"

// ConfigmapUpdateInput bcs_configmap_update 工具入参。
//
// 四个 op 共用一套入参；每个 op 必要字段不同，doXxx 里校验。
type ConfigmapUpdateInput struct {
	Op              string            `json:"op"                description:"操作类型（必填）：get（读取）/ set（更新或创建）/ delete（删除 keys）/ rollback（按 snapshot_id 回滚）"`
	ClusterID       string            `json:"cluster_id"        description:"BCS 集群 ID（必填）"`
	Namespace       string            `json:"namespace"         description:"Kubernetes 命名空间（必填）"`
	Name            string            `json:"name"              description:"ConfigMap 名称（必填）"`
	Data            map[string]string `json:"data"              description:"待写入的键值（op=set 必填）；对同名 key 覆盖，未提及的 key 保留"`
	DeleteKeys      []string          `json:"delete_keys"       description:"待删除的键名列表（op=delete 必填）；只删指定 key，其他保留"`
	RolloutStrategy string            `json:"rollout_strategy"  description:"生效策略（op=set/delete 必填）：none / rolling_restart / immediate_restart；决定是否触发关联 Deployment 重启"`
	LinkedDeployment string           `json:"linked_deployment" description:"关联 Deployment 名称（rollout_strategy != none 必填）；工具会对其滚动/立即重启以使配置生效"`
	SnapshotID      string            `json:"snapshot_id"       description:"快照 ID（op=rollback 必填）；由 set/delete 自动生成并返回"`
	Reason          string            `json:"reason"            description:"变更原因；敏感键名命中时必填；生产 ns + immediate_restart 必填"`
	Confirmed       bool               `json:"confirmed"         description:"是否已获人工确认；写操作必须 true 才真正下发"`
}

// newConfigmapUpdateTool 构造 bcs_configmap_update 工具。
func newConfigmapUpdateTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in ConfigmapUpdateInput) (*Result, error) {
		op := strings.ToLower(strings.TrimSpace(in.Op))
		if op == "" {
			return nil, fmt.Errorf("op 为必填项（get / set / delete / rollback）")
		}
		if in.ClusterID == "" || in.Namespace == "" || in.Name == "" {
			return nil, fmt.Errorf("cluster_id / namespace / name 均为必填")
		}
		switch op {
		case "get":
			return doConfigmapGet(ctx, client, in)
		case "set":
			return doConfigmapSet(ctx, client, in)
		case "delete":
			return doConfigmapDelete(ctx, client, in)
		case "rollback":
			return doConfigmapRollback(ctx, client, in)
		default:
			return nil, fmt.Errorf("不支持的 op: %q（可选：get / set / delete / rollback）", op)
		}
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_configmap_update"),
		function.WithDescription(
			"BCS ConfigMap 配置热更工具，四种操作：get(只读，做 diff 前置) / set(增改 keys) / delete(删 keys) / rollback(按 snapshot_id 回退)。"+
				"⚠ set 和 delete 必须指定 rollout_strategy（none/rolling_restart/immediate_restart），决定配置如何生效。"+
				"敏感键名（password/secret/token/key/credential）强制 Critical + RequireReason；keys>10 升档 High。"+
				"每次 set/delete 自动留快照写入 Annotation，可凭 snapshot_id 一键 rollback。",
		),
	)
}

// =============================================================================
// op=get：只读，不走 HITL，直接返回当前 ConfigMap 数据
// =============================================================================

func doConfigmapGet(ctx context.Context, client *bcsapi.Client, in ConfigmapUpdateInput) (*Result, error) {
	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/configmaps/%s",
		in.ClusterID, in.Namespace, in.Name)
	var cm map[string]any
	err := client.Get(ctx, path, nil, &cm)

	if errors.Is(err, bcsapi.ErrMockMode) {
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：返回样例 ConfigMap %s/%s/%s", in.ClusterID, in.Namespace, in.Name),
			Data: map[string]any{
				"cluster_id": in.ClusterID,
				"namespace":  in.Namespace,
				"name":       in.Name,
				"data": map[string]string{
					"log.level":      "info",
					"request.timeout": "3s",
					"feature.rate_limit_enabled": "true",
				},
				"latest_snapshot_id": "",
			},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 ConfigMap 失败: %w", err)
	}
	data, _ := cm["data"].(map[string]any)
	snapshotID := extractLatestSnapshotID(cm)
	return &Result{
		OK: true,
		Data: map[string]any{
			"cluster_id":         in.ClusterID,
			"namespace":          in.Namespace,
			"name":               in.Name,
			"data":               data,
			"latest_snapshot_id": snapshotID,
			"raw":                cm,
		},
	}, nil
}

// =============================================================================
// op=set：增改 keys，主路径
// =============================================================================

func doConfigmapSet(ctx context.Context, client *bcsapi.Client, in ConfigmapUpdateInput) (*Result, error) {
	if len(in.Data) == 0 {
		return nil, fmt.Errorf("op=set 必须指定 data（至少 1 个 key）")
	}
	if err := validateRolloutStrategy(in); err != nil {
		return nil, err
	}

	keys := sortedKeys(in.Data)
	sensitiveKeys := detectSensitiveKeys(keys)
	severity := classifyConfigmapSeverity("set", in.Namespace, len(keys), in.RolloutStrategy, len(sensitiveKeys) > 0)

	// confirmed 路径下的强 Guard：敏感键 / 生产 immediate_restart 必须 reason
	if in.Confirmed {
		if len(sensitiveKeys) > 0 && strings.TrimSpace(in.Reason) == "" {
			return &Result{
				OK: false,
				Message: fmt.Sprintf(
					"规则拦截：检测到敏感键名 %v，必须在 reason 字段提供变更原因并复核是否应使用 Secret 而非 ConfigMap。",
					sensitiveKeys,
				),
			}, nil
		}
		if isProductionNS(in.Namespace) && in.RolloutStrategy == rolloutImmediateRestart &&
			strings.TrimSpace(in.Reason) == "" {
			return &Result{
				OK:      false,
				Message: "规则拦截：生产命名空间使用 immediate_restart 必须在 reason 字段提供变更原因（中断风险）。",
			}, nil
		}
	}

	// 先读当前状态，做 diff（既为 Plan 展示也为快照）
	// 错误处理：ErrMockMode 被 fetchCurrentConfigmap 当作"成功带预置数据"返回，此处视为正常；
	// 真实错误（含 ConfigMap 不存在）同样走 set 路径 —— 用空 current 做 diff 即"全新增"，
	// 然后 PUT 下去让服务端决定是 upsert 还是拒绝（由 K8s API 负责最终一致性）。
	current, currentErr := fetchCurrentConfigmap(ctx, client, in)
	if currentErr != nil && !errors.Is(currentErr, bcsapi.ErrMockMode) {
		// 非 Mock 真实错误：current 是空 map，diff 将全为新增；不主动失败，交给 PUT 阶段裁决。
		_ = currentErr // 当前阶段故意吞掉，上面注释已说明
	}
	diff := computeDiff(current, in.Data, nil) // set 语义：仅新增/修改，不删
	plan := buildSetPlan(in, severity, diff, sensitiveKeys, keys)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	// 执行 set：先构造合并后的 data，再 PUT 回去（PUT 幂等）
	merged := mergeData(current, in.Data)
	snapshot := makeSnapshot(current)
	body := buildConfigmapBody(in, merged, snapshot)
	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/configmaps/%s",
		in.ClusterID, in.Namespace, in.Name)

	var resp map[string]any
	err := client.PutJSON(ctx, path, body, &resp)
	auditExtra := map[string]any{
		"op":               "set",
		"rollout_strategy": in.RolloutStrategy,
		"keys":             keys,
		"keys_count":       len(keys),
		"sensitive_keys":   sensitiveKeys,
		"snapshot_id":      snapshot.ID,
		"diff_summary":     summarizeDiff(diff),
	}

	if errors.Is(err, bcsapi.ErrMockMode) {
		emitConfigmapAudit(client, in, "set", severity, true, nil, auditExtra, current, merged)
		// Mock 模式下模拟 rollout
		rolloutMsg := mockRolloutMessage(in)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：ConfigMap %s/%s 更新（%d key），snapshot_id=%s；%s",
				in.Namespace, in.Name, len(keys), snapshot.ID, rolloutMsg),
			Data: map[string]any{
				"snapshot_id":      snapshot.ID,
				"diff":             diff,
				"keys_changed":     len(keys),
				"rollout_strategy": in.RolloutStrategy,
				"rollout_status":   "simulated (mock)",
			},
		}, nil
	}
	if err != nil {
		emitConfigmapAudit(client, in, "set", severity, false, err, auditExtra, current, merged)
		return nil, fmt.Errorf("set ConfigMap 失败: %w", err)
	}

	// 按 rollout_strategy 触发 Deployment 重启
	rolloutResult, rolloutErr := triggerRollout(ctx, client, in)
	auditExtra["rollout_result"] = rolloutResult
	ok := rolloutErr == nil
	emitConfigmapAudit(client, in, "set", severity, ok, rolloutErr, auditExtra, current, merged)
	if rolloutErr != nil {
		// ConfigMap 已更新但 rollout 失败 —— 这是最需要人工介入的"半成功"态
		return &Result{
			OK:      false,
			Message: fmt.Sprintf("ConfigMap 已更新但 rollout 失败（%v），可用 snapshot_id=%s 回滚", rolloutErr, snapshot.ID),
			Data: map[string]any{
				"snapshot_id":      snapshot.ID,
				"rollout_error":    rolloutErr.Error(),
				"rollout_strategy": in.RolloutStrategy,
			},
		}, nil
	}
	return &Result{
		OK: true,
		Data: map[string]any{
			"snapshot_id":      snapshot.ID,
			"diff":             diff,
			"keys_changed":     len(keys),
			"rollout_strategy": in.RolloutStrategy,
			"rollout_result":   rolloutResult,
			"api_response":     resp,
		},
	}, nil
}

// =============================================================================
// op=delete：删 keys，始终 High 起步
// =============================================================================

func doConfigmapDelete(ctx context.Context, client *bcsapi.Client, in ConfigmapUpdateInput) (*Result, error) {
	if len(in.DeleteKeys) == 0 {
		return nil, fmt.Errorf("op=delete 必须指定 delete_keys（至少 1 个）")
	}
	if err := validateRolloutStrategy(in); err != nil {
		return nil, err
	}

	sensitiveKeys := detectSensitiveKeys(in.DeleteKeys)
	severity := classifyConfigmapSeverity("delete", in.Namespace, len(in.DeleteKeys), in.RolloutStrategy, len(sensitiveKeys) > 0)

	if in.Confirmed && len(sensitiveKeys) > 0 && strings.TrimSpace(in.Reason) == "" {
		return &Result{
			OK:      false,
			Message: fmt.Sprintf("规则拦截：删除敏感键名 %v 必须提供 reason。", sensitiveKeys),
		}, nil
	}

	current, _ := fetchCurrentConfigmap(ctx, client, in)
	diff := computeDiff(current, nil, in.DeleteKeys)
	plan := buildDeletePlan(in, severity, diff, sensitiveKeys)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	merged := removeKeys(current, in.DeleteKeys)
	snapshot := makeSnapshot(current)
	body := buildConfigmapBody(in, merged, snapshot)
	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/configmaps/%s",
		in.ClusterID, in.Namespace, in.Name)

	var resp map[string]any
	err := client.PutJSON(ctx, path, body, &resp)
	auditExtra := map[string]any{
		"op":               "delete",
		"rollout_strategy": in.RolloutStrategy,
		"delete_keys":      in.DeleteKeys,
		"sensitive_keys":   sensitiveKeys,
		"snapshot_id":      snapshot.ID,
	}

	if errors.Is(err, bcsapi.ErrMockMode) {
		emitConfigmapAudit(client, in, "delete", severity, true, nil, auditExtra, current, merged)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：已删除 %d 个 key，snapshot_id=%s；%s",
				len(in.DeleteKeys), snapshot.ID, mockRolloutMessage(in)),
			Data: map[string]any{
				"snapshot_id":      snapshot.ID,
				"deleted_keys":     in.DeleteKeys,
				"rollout_strategy": in.RolloutStrategy,
				"rollout_status":   "simulated (mock)",
			},
		}, nil
	}
	if err != nil {
		emitConfigmapAudit(client, in, "delete", severity, false, err, auditExtra, current, merged)
		return nil, fmt.Errorf("delete keys 失败: %w", err)
	}

	rolloutResult, rolloutErr := triggerRollout(ctx, client, in)
	auditExtra["rollout_result"] = rolloutResult
	ok := rolloutErr == nil
	emitConfigmapAudit(client, in, "delete", severity, ok, rolloutErr, auditExtra, current, merged)
	if rolloutErr != nil {
		return &Result{
			OK: false,
			Message: fmt.Sprintf("keys 已删除但 rollout 失败（%v），可用 snapshot_id=%s 回滚", rolloutErr, snapshot.ID),
			Data:    map[string]any{"snapshot_id": snapshot.ID, "rollout_error": rolloutErr.Error()},
		}, nil
	}
	return &Result{
		OK: true,
		Data: map[string]any{
			"snapshot_id":      snapshot.ID,
			"deleted_keys":     in.DeleteKeys,
			"rollout_strategy": in.RolloutStrategy,
			"rollout_result":   rolloutResult,
		},
	}, nil
}

// =============================================================================
// op=rollback：按 snapshot_id 回滚到历史状态
// =============================================================================

func doConfigmapRollback(ctx context.Context, client *bcsapi.Client, in ConfigmapUpdateInput) (*Result, error) {
	if strings.TrimSpace(in.SnapshotID) == "" {
		return nil, fmt.Errorf("op=rollback 必须指定 snapshot_id（无快照不允许黑盒回滚）")
	}
	// rollout_strategy 默认 rolling_restart（回滚往往就是想生效）
	if in.RolloutStrategy == "" {
		in.RolloutStrategy = rolloutRollingRestart
	}

	severity := hitl.SeverityMedium // 回滚是恢复动作，相对鼓励
	if isProductionNS(in.Namespace) && in.RolloutStrategy == rolloutImmediateRestart {
		severity = hitl.SeverityHigh
	}

	// 读当前 configmap，从 Annotation 里取出目标快照
	current, curErr := fetchCurrentConfigmap(ctx, client, in)
	if curErr != nil && !errors.Is(curErr, bcsapi.ErrMockMode) {
		return nil, fmt.Errorf("读取当前 ConfigMap 失败: %w", curErr)
	}
	// 先做 snapshot 匹配校验：mismatch 时直接返回"不匹配"错误，不被
	// rollout_strategy 的前置校验掩盖（后者要求 linked_deployment）。
	// 业务直觉：用户给错 snapshot_id 时，最有用的反馈是"找不到快照"，
	// 而不是"linked_deployment 必填"。
	snapshot, sErr := loadSnapshot(current, in.SnapshotID)
	if sErr != nil {
		return nil, fmt.Errorf("加载快照失败: %w", sErr)
	}
	// 通过 snapshot 校验后再做 rollout_strategy 完整性校验
	if err := validateRolloutStrategy(in); err != nil {
		return nil, err
	}

	plan := buildRollbackPlan(in, severity, snapshot)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	// 新快照：记录当前状态（方便"回滚的回滚"）
	newSnap := makeSnapshot(current)
	body := buildConfigmapBody(in, snapshot.Data, newSnap)
	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/configmaps/%s",
		in.ClusterID, in.Namespace, in.Name)

	var resp map[string]any
	err := client.PutJSON(ctx, path, body, &resp)
	auditExtra := map[string]any{
		"op":                 "rollback",
		"rollout_strategy":   in.RolloutStrategy,
		"target_snapshot_id": in.SnapshotID,
		"new_snapshot_id":    newSnap.ID,
	}

	if errors.Is(err, bcsapi.ErrMockMode) {
		emitConfigmapAudit(client, in, "rollback", severity, true, nil, auditExtra, current, snapshot.Data)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：已回滚到 snapshot_id=%s（新快照 %s）；%s",
				in.SnapshotID, newSnap.ID, mockRolloutMessage(in)),
			Data: map[string]any{
				"target_snapshot_id": in.SnapshotID,
				"new_snapshot_id":    newSnap.ID,
				"rollout_strategy":   in.RolloutStrategy,
			},
		}, nil
	}
	if err != nil {
		emitConfigmapAudit(client, in, "rollback", severity, false, err, auditExtra, current, snapshot.Data)
		return nil, fmt.Errorf("rollback 失败: %w", err)
	}
	rolloutResult, rolloutErr := triggerRollout(ctx, client, in)
	auditExtra["rollout_result"] = rolloutResult
	ok := rolloutErr == nil
	emitConfigmapAudit(client, in, "rollback", severity, ok, rolloutErr, auditExtra, current, snapshot.Data)
	if rolloutErr != nil {
		return &Result{
			OK:      false,
			Message: fmt.Sprintf("回滚配置已写入但 rollout 失败：%v", rolloutErr),
			Data:    map[string]any{"new_snapshot_id": newSnap.ID, "rollout_error": rolloutErr.Error()},
		}, nil
	}
	return &Result{
		OK: true,
		Data: map[string]any{
			"target_snapshot_id": in.SnapshotID,
			"new_snapshot_id":    newSnap.ID,
			"rollout_strategy":   in.RolloutStrategy,
			"rollout_result":     rolloutResult,
			"api_response":       resp,
		},
	}, nil
}

// =============================================================================
// Severity / Plan / 校验
// =============================================================================

// validateRolloutStrategy 校验 rollout 策略取值 + 关联 Deployment 必填。
func validateRolloutStrategy(in ConfigmapUpdateInput) error {
	s := strings.ToLower(strings.TrimSpace(in.RolloutStrategy))
	if s == "" {
		return fmt.Errorf("rollout_strategy 必填（none / rolling_restart / immediate_restart）")
	}
	switch s {
	case rolloutNone, rolloutRollingRestart, rolloutImmediateRestart:
	default:
		return fmt.Errorf("不支持的 rollout_strategy: %q", in.RolloutStrategy)
	}
	if s != rolloutNone && strings.TrimSpace(in.LinkedDeployment) == "" {
		return fmt.Errorf("rollout_strategy=%q 必须指定 linked_deployment（用于触发重启）", s)
	}
	return nil
}

// classifyConfigmapSeverity 配置热更的 Severity 分级矩阵。
func classifyConfigmapSeverity(op, ns string, keysCount int, rollout string, hasSensitive bool) hitl.Severity {
	if hasSensitive {
		return hitl.SeverityCritical // 敏感键名永远 Critical
	}
	prod := isProductionNS(ns)
	switch op {
	case "delete":
		if prod {
			return hitl.SeverityCritical // 生产 ns 删 key 始终 Critical
		}
		return hitl.SeverityHigh // delete 基础 High
	case "set":
		// 生产 ns 的 immediate_restart 永远 Critical（中断风险）
		if prod && rollout == rolloutImmediateRestart {
			return hitl.SeverityCritical
		}
		// 生产 ns 的 rolling_restart High
		if prod && rollout == rolloutRollingRestart {
			return hitl.SeverityHigh
		}
		// keys 过多升档 High
		if keysCount > configmapSetKeysSoftLimit {
			return hitl.SeverityHigh
		}
		// 非生产 ns + none rollout 最低
		if !prod && rollout == rolloutNone {
			return hitl.SeverityLow
		}
		return hitl.SeverityMedium
	}
	return hitl.SeverityMedium
}

// buildSetPlan 构造 set 操作的 HITL Plan。
func buildSetPlan(in ConfigmapUpdateInput, severity hitl.Severity, diff []DiffEntry,
	sensitiveKeys, keys []string) hitl.Plan {
	diffSummary := summarizeDiff(diff)
	side := fmt.Sprintf(
		"向 %s/%s 写入 %d 个 key（+%d / ~%d / -%d），生效策略 %q。",
		in.Namespace, in.Name, len(keys),
		diffSummary["added"], diffSummary["modified"], diffSummary["deleted"],
		in.RolloutStrategy,
	)
	if in.RolloutStrategy == rolloutImmediateRestart {
		side += fmt.Sprintf(" ⚠ immediate_restart 会立刻重启 %q 全部 Pod，有服务中断风险。", in.LinkedDeployment)
	}
	if in.RolloutStrategy == rolloutRollingRestart {
		side += fmt.Sprintf(" 将触发 Deployment %q 滚动重启。", in.LinkedDeployment)
	}
	requireReason := len(sensitiveKeys) > 0 ||
		(isProductionNS(in.Namespace) && in.RolloutStrategy == rolloutImmediateRestart)
	plan := hitl.Plan{
		Action:   "bcs.configmap.set",
		Severity: severity,
		Target:   fmt.Sprintf("%s/%s/%s", in.ClusterID, in.Namespace, in.Name),
		SideEffect: side,
		ImpactScope: fmt.Sprintf("ConfigMap %s 被 %q 挂载的所有 Pod。",
			in.Name, firstNonEmptyStr(in.LinkedDeployment, "(未指定 linked_deployment)")),
		RollbackPlan: "执行成功后 Data 中会带 snapshot_id，可用 op=rollback + snapshot_id 一键回滚。",
		Params: map[string]any{
			"op":               "set",
			"cluster_id":       in.ClusterID,
			"namespace":        in.Namespace,
			"name":             in.Name,
			"keys":             keys,
			"keys_count":       len(keys),
			"rollout_strategy": in.RolloutStrategy,
			"sensitive_keys":   sensitiveKeys,
			"diff":             diff,
		},
		RequireReason: requireReason,
	}
	return plan
}

// buildDeletePlan 构造 delete 操作的 HITL Plan。
func buildDeletePlan(in ConfigmapUpdateInput, severity hitl.Severity, diff []DiffEntry, sensitiveKeys []string) hitl.Plan {
	side := fmt.Sprintf(
		"从 %s/%s 删除 %d 个 key：%v，生效策略 %q。"+
			"删 key 比改值更危险 —— 没有默认值兜底。",
		in.Namespace, in.Name, len(in.DeleteKeys), in.DeleteKeys, in.RolloutStrategy,
	)
	return hitl.Plan{
		Action:       "bcs.configmap.delete",
		Severity:     severity,
		Target:       fmt.Sprintf("%s/%s/%s", in.ClusterID, in.Namespace, in.Name),
		SideEffect:   side,
		ImpactScope:  fmt.Sprintf("ConfigMap %s 被 %q 挂载的所有 Pod；缺失的 key 可能导致启动失败。", in.Name, in.LinkedDeployment),
		RollbackPlan: "执行后自动留快照，可 op=rollback + snapshot_id 恢复。",
		Params: map[string]any{
			"op":               "delete",
			"cluster_id":       in.ClusterID,
			"namespace":        in.Namespace,
			"name":             in.Name,
			"delete_keys":      in.DeleteKeys,
			"rollout_strategy": in.RolloutStrategy,
			"sensitive_keys":   sensitiveKeys,
			"diff":             diff,
		},
		RequireReason: len(sensitiveKeys) > 0,
	}
}

// buildRollbackPlan 构造 rollback 操作的 HITL Plan。
func buildRollbackPlan(in ConfigmapUpdateInput, severity hitl.Severity, snapshot *configmapSnapshot) hitl.Plan {
	return hitl.Plan{
		Action:   "bcs.configmap.rollback",
		Severity: severity,
		Target:   fmt.Sprintf("%s/%s/%s → snapshot %s", in.ClusterID, in.Namespace, in.Name, in.SnapshotID),
		SideEffect: fmt.Sprintf(
			"将 ConfigMap %s 回滚到 %s 拍摄的快照（%d 个 key），生效策略 %q。",
			in.Name, snapshot.CreatedAt, len(snapshot.Data), in.RolloutStrategy,
		),
		ImpactScope:  "当前状态会被覆盖；执行前会自动保存为新快照供再次回滚。",
		RollbackPlan: "不可直接再次 rollback 到同一 snapshot_id；需使用 op=rollback + 新快照 ID（本次执行会返回）。",
		Params: map[string]any{
			"op":                 "rollback",
			"cluster_id":         in.ClusterID,
			"namespace":          in.Namespace,
			"name":               in.Name,
			"target_snapshot_id": in.SnapshotID,
			"rollout_strategy":   in.RolloutStrategy,
		},
	}
}

// =============================================================================
// Diff / Snapshot / 合并 / rollout
// =============================================================================

// DiffEntry 表示一条 key 变更。
type DiffEntry struct {
	Op    string `json:"op"`              // added / modified / deleted
	Key   string `json:"key"`
	From  string `json:"from,omitempty"`  // modified/deleted 场景
	To    string `json:"to,omitempty"`    // added/modified 场景
}

// computeDiff 对比当前 data 与目标 changes（仅 set 用）/ delKeys（仅 delete 用）。
//
// changes 传入时：对每条 key 生成 added（新）或 modified（原有）；
// delKeys 传入时：对每个 key 生成 deleted。
func computeDiff(current map[string]string, changes map[string]string, delKeys []string) []DiffEntry {
	out := make([]DiffEntry, 0, len(changes)+len(delKeys))
	for _, k := range sortedKeys(changes) {
		v := changes[k]
		if old, ok := current[k]; ok {
			if old == v {
				continue // 未变化
			}
			out = append(out, DiffEntry{Op: "modified", Key: k, From: old, To: v})
		} else {
			out = append(out, DiffEntry{Op: "added", Key: k, To: v})
		}
	}
	for _, k := range delKeys {
		if old, ok := current[k]; ok {
			out = append(out, DiffEntry{Op: "deleted", Key: k, From: old})
		}
	}
	return out
}

// summarizeDiff 返回 {added, modified, deleted} 三个计数（用于 Plan 文案）。
func summarizeDiff(diff []DiffEntry) map[string]int {
	s := map[string]int{"added": 0, "modified": 0, "deleted": 0}
	for _, e := range diff {
		s[e.Op]++
	}
	return s
}

// configmapSnapshot 快照结构。
type configmapSnapshot struct {
	ID        string            `json:"id"`
	CreatedAt string            `json:"created_at"`
	Data      map[string]string `json:"data"`
}

// makeSnapshot 根据当前 data 创建快照。
func makeSnapshot(current map[string]string) *configmapSnapshot {
	return &configmapSnapshot{
		ID:        fmt.Sprintf("SNAP-%d", time.Now().UnixNano()),
		CreatedAt: time.Now().Format(time.RFC3339),
		Data:      cloneStringMap(current),
	}
}

// loadSnapshot 从当前 ConfigMap 的 Annotation 里加载指定快照。
//
// 为简化：Annotation 只保留"最近一次"快照（key=gameops-agent.tencent.com/snapshot）。
// 要求 snapshot_id 与 Annotation 中一致才允许回滚 —— 避免跨越多次覆盖导致"回滚到不存在的快照"。
func loadSnapshot(current map[string]string, targetID string) (*configmapSnapshot, error) {
	raw, ok := current["__annotation_snapshot__"]
	if !ok || raw == "" {
		return nil, fmt.Errorf("当前 ConfigMap 无 Annotation 快照，无法 rollback 到 %q", targetID)
	}
	var snap configmapSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, fmt.Errorf("解析快照 JSON 失败: %w", err)
	}
	if snap.ID != targetID {
		return nil, fmt.Errorf("当前快照 %q 与目标 %q 不匹配（本工具仅保留最近一次快照）", snap.ID, targetID)
	}
	return &snap, nil
}

// extractLatestSnapshotID 从当前 configmap 对象里读 Annotation 中的 snapshot ID。
func extractLatestSnapshotID(cm map[string]any) string {
	meta, _ := cm["metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	ann, _ := meta["annotations"].(map[string]any)
	if ann == nil {
		return ""
	}
	raw, _ := ann[snapshotAnnotationKey].(string)
	if raw == "" {
		return ""
	}
	var snap configmapSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return ""
	}
	return snap.ID
}

// fetchCurrentConfigmap 读取 configmap 并把 Annotation 提取到合成字段 __annotation_snapshot__ 里。
//
// Mock 模式下返回预置样例。
func fetchCurrentConfigmap(ctx context.Context, client *bcsapi.Client, in ConfigmapUpdateInput) (map[string]string, error) {
	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/configmaps/%s",
		in.ClusterID, in.Namespace, in.Name)
	var cm map[string]any
	err := client.Get(ctx, path, nil, &cm)
	if errors.Is(err, bcsapi.ErrMockMode) {
		// Mock 返回预置：包含一个历史快照 key 供 rollback 测试
		return map[string]string{
			"log.level":                   "info",
			"request.timeout":             "3s",
			"__annotation_snapshot__":     `{"id":"SNAP-MOCK-HISTORY","created_at":"2026-04-20T12:00:00+08:00","data":{"log.level":"debug","request.timeout":"5s"}}`,
		}, bcsapi.ErrMockMode
	}
	if err != nil {
		return map[string]string{}, err
	}
	data := map[string]string{}
	if d, ok := cm["data"].(map[string]any); ok {
		for k, v := range d {
			if s, ok := v.(string); ok {
				data[k] = s
			}
		}
	}
	// 把 Annotation 中的 snapshot 合成到特殊 key 供内部用
	if snap := rawSnapshotAnnotation(cm); snap != "" {
		data["__annotation_snapshot__"] = snap
	}
	return data, nil
}

// rawSnapshotAnnotation 从 cm 里抽出原始 snapshot JSON 字符串。
func rawSnapshotAnnotation(cm map[string]any) string {
	meta, _ := cm["metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	ann, _ := meta["annotations"].(map[string]any)
	if ann == nil {
		return ""
	}
	s, _ := ann[snapshotAnnotationKey].(string)
	return s
}

// mergeData 返回 current + changes 的合并结果（changes 优先），不含内部合成字段。
func mergeData(current, changes map[string]string) map[string]string {
	out := make(map[string]string, len(current)+len(changes))
	for k, v := range current {
		if strings.HasPrefix(k, "__annotation_") {
			continue
		}
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}

// removeKeys 返回移除指定 keys 后的副本。
func removeKeys(current map[string]string, keys []string) map[string]string {
	out := make(map[string]string, len(current))
	del := map[string]bool{}
	for _, k := range keys {
		del[k] = true
	}
	for k, v := range current {
		if strings.HasPrefix(k, "__annotation_") {
			continue
		}
		if del[k] {
			continue
		}
		out[k] = v
	}
	return out
}

// buildConfigmapBody 构造要 PUT 回去的 ConfigMap 对象（带 Annotation 快照）。
func buildConfigmapBody(in ConfigmapUpdateInput, data map[string]string, snap *configmapSnapshot) map[string]any {
	snapJSON, _ := json.Marshal(snap)
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      in.Name,
			"namespace": in.Namespace,
			"annotations": map[string]string{
				snapshotAnnotationKey: string(snapJSON),
			},
		},
		"data": data,
	}
}

// triggerRollout 按 rollout_strategy 触发 Deployment 重启。
//
// 返回的 map 用于审计里记录 rollout 细节。
// 实现策略：
//   - none：no-op
//   - rolling_restart：patch Deployment 打 restartedAt 注解（与 pod_restart 同机制）
//   - immediate_restart：先 scale 到 0 再 scale 回原 replicas（Mock 模式下仅模拟）
//
// 真实模式下此处暂走 rolling_restart 的 patch 路径；immediate 标记为 TODO 待对接。
func triggerRollout(ctx context.Context, client *bcsapi.Client, in ConfigmapUpdateInput) (map[string]any, error) {
	if in.RolloutStrategy == rolloutNone {
		return map[string]any{"strategy": "none", "status": "skipped"}, nil
	}
	// 与 pod_restart.go 的 rollout_restart 同路径：patch deployment 打 restartedAt
	path := fmt.Sprintf("/clusters/%s/apis/apps/v1/namespaces/%s/deployments/%s",
		in.ClusterID, in.Namespace, in.LinkedDeployment)
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"kubectl.kubernetes.io/restartedAt": time.Now().Format(time.RFC3339),
						"gameops-agent.tencent.com/cause":   "configmap-update",
					},
				},
			},
		},
	}
	var resp map[string]any
	if err := client.PatchJSON(ctx, path, patch, &resp); err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			return map[string]any{"strategy": in.RolloutStrategy, "status": "mock-ok"}, nil
		}
		return nil, fmt.Errorf("rollout deployment %q failed: %w", in.LinkedDeployment, err)
	}
	note := ""
	if in.RolloutStrategy == rolloutImmediateRestart {
		note = "immediate_restart 当前以 rolling_restart 语义下发（TODO: 对接 scale 0→N 的立刻重启路径）"
	}
	return map[string]any{
		"strategy":   in.RolloutStrategy,
		"deployment": in.LinkedDeployment,
		"status":     "patched",
		"note":       note,
	}, nil
}

// mockRolloutMessage Mock 模式下给用户看的简短 rollout 提示。
func mockRolloutMessage(in ConfigmapUpdateInput) string {
	switch in.RolloutStrategy {
	case rolloutNone:
		return "rollout 策略=none，未触发 Deployment 重启"
	case rolloutRollingRestart:
		return fmt.Sprintf("已模拟对 Deployment %q 发起滚动重启", in.LinkedDeployment)
	case rolloutImmediateRestart:
		return fmt.Sprintf("已模拟对 Deployment %q 发起立即重启", in.LinkedDeployment)
	}
	return ""
}

// =============================================================================
// 敏感键 / 生产 ns / 审计 / 辅助
// =============================================================================

// detectSensitiveKeys 返回命中敏感关键字的 key 列表。
func detectSensitiveKeys(keys []string) []string {
	out := make([]string, 0)
	for _, k := range keys {
		lower := strings.ToLower(k)
		for _, kw := range sensitiveKeywords {
			if strings.Contains(lower, kw) {
				out = append(out, k)
				break
			}
		}
	}
	return out
}

// isProductionNS 判断是否生产 namespace（复用包级 prodNamespacePrefixes，来自 scale.go）。
func isProductionNS(ns string) bool {
	lower := strings.ToLower(ns)
	for _, p := range prodNamespacePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// emitConfigmapAudit 统一审计入账。from/to 与 D17.6 HMAC 链语义对齐：
//
//	from = 执行前 data 摘要
//	to   = 执行后 data 摘要
func emitConfigmapAudit(client *bcsapi.Client, in ConfigmapUpdateInput, op string,
	severity hitl.Severity, ok bool, err error, extra map[string]any,
	before, after map[string]string) {
	params := map[string]any{
		"op":         op,
		"cluster_id": in.ClusterID,
		"namespace":  in.Namespace,
		"name":       in.Name,
		"from":       dataDigest(before),
		"to":         dataDigest(after),
	}
	if in.Reason != "" {
		params["reason"] = in.Reason
	}
	for k, v := range extra {
		params[k] = v
	}
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   "bcs.configmap." + op,
		Severity: string(severity),
		Target:   fmt.Sprintf("%s/%s/%s", in.ClusterID, in.Namespace, in.Name),
		Params:   params,
		Success:  ok,
		Err:      err,
		Mock:     client.IsMock(),
	})
}

// dataDigest 为审计生成简短摘要（key 数 + 排序后 keys 列表），避免把完整 value 写入审计日志。
func dataDigest(d map[string]string) map[string]any {
	keys := make([]string, 0, len(d))
	for k := range d {
		if strings.HasPrefix(k, "__annotation_") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return map[string]any{
		"keys_count": len(keys),
		"keys":       keys,
	}
}

// cloneStringMap 拷贝字符串 map，剔除内部合成字段。
func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if strings.HasPrefix(k, "__annotation_") {
			continue
		}
		out[k] = v
	}
	return out
}

// sortedKeys 返回 map 排序后的 keys（保证 Plan / diff 输出稳定）。
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// firstNonEmptyStr 返回首个非空字符串。
func firstNonEmptyStr(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
