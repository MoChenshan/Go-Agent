// Package bcstools —— bcs_secret_update（Secret 热更，D22 新增）。
//
// # 这是 D18.4 敏感键兜底规则的闭环
//
// D18.4 的 configmap_update 里埋了一条规则：检测到 password/token/secret_key/
// access_key 等敏感键名时，拦截并提示"请考虑使用 Secret 而非 ConfigMap"。
//
// 但用户被拦截后会问："那我该怎么改 Secret？"—— 现在有答案了。
//
// 至此修复侧**配置类能力闭环**：
//
//	configmap_update  —— 非敏感配置（明文、可 diff 可见、70% 场景）
//	secret_update     —— 敏感配置（base64、value 脱敏、证书/密钥/token）← 本文件
//
// # 与 configmap_update 的 5 个核心差异
//
//	维度            | ConfigMap            | Secret
//	----------------+----------------------+--------------------------------------
//	Data 编码        | 明文 string          | base64 强制（工具层自动编解码）
//	Type            | 单一                 | 多类型（Opaque / kubernetes.io/tls /
//	                |                      |   dockerconfigjson / service-account-token）
//	Immutable       | 极少用               | 常用（防误改），immutable Secret 必须删重建
//	审计敏感度      | 低（已拦截敏感键）   | 高（value 绝不打印，只记 key+len）
//	特殊校验        | 无                   | TLS 类型需同时含 tls.crt 和 tls.key
//
// # 为什么 Secret 独立成文件而非扩 configmap_update
//
//   - Data 类型不同（map[string]string vs 含 base64 的 map[string][]byte）导致
//     snapshot 结构无法共享
//   - 审计策略不同（configmap 允许摘要 key 列表；Secret 要隐藏 value 长度分布以防侧信道）
//   - type 维度带来独立的校验路径（跨 type 禁止）
//   - 用户反模式不同：configmap 被塞敏感键 vs Secret 被错误改成明文
//
// 复用层面：共享包级 `isProductionNS` / `detectSensitiveKeys`（Secret 场景反向用：
// 键名不含敏感词时警告"这真的该放 Secret 吗"）/ `triggerRollout`（rollout 路径一样）/
// HITL Plan 骨架。
package bcstools

import (
	"context"
	"encoding/base64"
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

// Secret 类型枚举（K8s 标准）。
const (
	SecretTypeOpaque              = "Opaque"
	SecretTypeTLS                 = "kubernetes.io/tls"
	SecretTypeDockerConfigJSON    = "kubernetes.io/dockerconfigjson"
	SecretTypeServiceAccountToken = "kubernetes.io/service-account-token"
	SecretTypeBasicAuth           = "kubernetes.io/basic-auth"
	SecretTypeSSHAuth             = "kubernetes.io/ssh-auth"
)

// secretSnapshotAnnotationKey 快照存储位置。
// 与 configmap 的 key 区分，避免交叉恢复误操作。
const secretSnapshotAnnotationKey = "gameops-agent.tencent.com/secret-snapshot"

// secretSetKeysSoftLimit Secret 的 keys 软上限。
// 比 configmap（10）更严，因为 Secret 每个 key 的风险更高（泄露一个等于全泄）。
const secretSetKeysSoftLimit = 5

// SecretUpdateInput bcs_secret_update 工具入参。
//
// 与 ConfigmapUpdateInput 字段对齐但语义收紧：
//   - Data 接收"明文"（工具层自动 base64），降低用户心智
//   - 新增 Type 字段和默认值 Opaque
//   - 无 LinkedDeployment 则 rollout_strategy 必须是 none（Secret 没挂载的话改了也白改）
type SecretUpdateInput struct {
	Op               string            `json:"op"                description:"操作类型（必填）：get（读取，value 自动脱敏）/ set（更新或创建）/ delete（删除 keys）/ rollback（按 snapshot_id 回滚）"`
	ClusterID        string            `json:"cluster_id"        description:"BCS 集群 ID（必填）"`
	Namespace        string            `json:"namespace"         description:"Kubernetes 命名空间（必填）"`
	Name             string            `json:"name"              description:"Secret 名称（必填）"`
	Type             string            `json:"type"              description:"Secret 类型：Opaque（默认）/ kubernetes.io/tls / kubernetes.io/dockerconfigjson 等；创建时使用，更新时工具会校验一致"`
	Data             map[string]string `json:"data"              description:"待写入的键值（op=set 必填）；**明文**传入，工具自动做 base64 编码；对同名 key 覆盖"`
	DeleteKeys       []string          `json:"delete_keys"       description:"待删除的键名列表（op=delete 必填）"`
	RolloutStrategy  string            `json:"rollout_strategy"  description:"生效策略（op=set/delete）：none / rolling_restart / immediate_restart；Secret 以环境变量注入的场景必须重启才生效"`
	LinkedDeployment string            `json:"linked_deployment" description:"关联 Deployment 名称（rollout_strategy != none 必填）"`
	SnapshotID       string            `json:"snapshot_id"       description:"快照 ID（op=rollback 必填）"`
	Reason           string            `json:"reason"            description:"变更原因；生产 ns 和 immutable 场景必填"`
	AllowImmutable   bool              `json:"allow_immutable"   description:"若现有 Secret 标记 immutable=true，本工具默认阻断；显式置 true 才允许走'删除重建'危险路径"`
	Confirmed        bool              `json:"confirmed"         description:"是否已获人工确认；写操作必须 true 才真正下发"`
}

// secretSnapshot Secret 数据快照。与 configmapSnapshot 结构独立。
//
// 设计要点：
//   - Data 存的是 base64 编码后的字符串（与 K8s API 同格式），不做再解码
//   - 不存 Type —— Type 变更必须走删重建，不允许 rollback 跨类型
type secretSnapshot struct {
	ID        string            `json:"id"`
	CreatedAt string            `json:"created_at"`
	Data      map[string]string `json:"data"` // base64 编码后的 value
}

// SecretDiffEntry 一条 key 变更。
//
// 关键差异于 DiffEntry：From/To 字段**只存长度**不存 value，彻底杜绝泄露。
type SecretDiffEntry struct {
	Op      string `json:"op"`                 // added / modified / deleted
	Key     string `json:"key"`
	FromLen int    `json:"from_len,omitempty"` // modified/deleted：原 value 字节数
	ToLen   int    `json:"to_len,omitempty"`   // added/modified：新 value 字节数
}

// newSecretUpdateTool 构造 bcs_secret_update 工具。
func newSecretUpdateTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in SecretUpdateInput) (*Result, error) {
		op := strings.ToLower(strings.TrimSpace(in.Op))
		if op == "" {
			return nil, fmt.Errorf("op 为必填项（get / set / delete / rollback）")
		}
		if in.ClusterID == "" || in.Namespace == "" || in.Name == "" {
			return nil, fmt.Errorf("cluster_id / namespace / name 均为必填")
		}
		switch op {
		case "get":
			return doSecretGet(ctx, client, in)
		case "set":
			return doSecretSet(ctx, client, in)
		case "delete":
			return doSecretDelete(ctx, client, in)
		case "rollback":
			return doSecretRollback(ctx, client, in)
		default:
			return nil, fmt.Errorf("不支持的 op: %q（可选：get / set / delete / rollback）", op)
		}
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_secret_update"),
		function.WithDescription(
			"BCS Secret 热更工具（D18.4 敏感键兜底的闭环）。四种操作：get(读，value 自动脱敏) / set(增改) / delete(删 keys) / rollback。"+
				"⚠ **与 configmap_update 的核心差异**：value 明文传入工具自动 base64；审计只记 key+length 绝不打印 value；支持 Secret 多 type；immutable Secret 需显式 allow_immutable=true。"+
				"⚠ TLS 类型必须同时含 tls.crt+tls.key；生产 ns 写操作始终 Critical+RequireReason。"+
				"⚠ set/delete 后通常需要 rolling_restart（Secret 以 env/volume 挂载都需要重启生效）。"+
				"典型用法：数据库密码轮转 / TLS 证书更新 / API token 轮换 / DockerConfig 修改镜像仓库凭证。",
		),
	)
}

// =============================================================================
// op=get：只读（value 自动脱敏）
// =============================================================================

func doSecretGet(ctx context.Context, client *bcsapi.Client, in SecretUpdateInput) (*Result, error) {
	current, stype, immutable, err := fetchCurrentSecret(ctx, client, in)
	if err != nil && !errors.Is(err, bcsapi.ErrMockMode) {
		return nil, fmt.Errorf("读取 Secret 失败: %w", err)
	}
	isMock := errors.Is(err, bcsapi.ErrMockMode) || (client != nil && client.IsMock())

	keys := sortedDataKeys(current)
	// 只暴露 key 列表和每个 key 的字节数（解码后长度），绝不暴露 value
	keyInfo := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		decoded, _ := base64.StdEncoding.DecodeString(current[k])
		keyInfo = append(keyInfo, map[string]any{
			"key":         k,
			"value_bytes": len(decoded),
		})
	}
	return &Result{
		OK:   true,
		Mock: isMock,
		Message: fmt.Sprintf("Secret %s/%s 含 %d 个 key（value 已脱敏，仅返回字节数）",
			in.Namespace, in.Name, len(keys)),
		Data: map[string]any{
			"namespace": in.Namespace,
			"name":      in.Name,
			"type":      stype,
			"immutable": immutable,
			"keys":      keyInfo,
		},
	}, nil
}

// =============================================================================
// op=set：增改 keys
// =============================================================================

func doSecretSet(ctx context.Context, client *bcsapi.Client, in SecretUpdateInput) (*Result, error) {
	if len(in.Data) == 0 {
		return nil, fmt.Errorf("op=set 必须指定 data（至少 1 个 key）")
	}
	if err := validateSecretRolloutStrategy(in); err != nil {
		return nil, err
	}
	// Type 默认值
	if strings.TrimSpace(in.Type) == "" {
		in.Type = SecretTypeOpaque
	}
	if err := validateSecretType(in.Type); err != nil {
		return nil, err
	}
	// TLS 类型结构校验
	if in.Type == SecretTypeTLS {
		if err := validateTLSKeys(in.Data); err != nil {
			return nil, err
		}
	}

	keys := sortedKeys(in.Data)
	severity := classifySecretSeverity("set", in.Namespace, len(keys), in.RolloutStrategy)

	// 生产 ns 永远要 reason
	if in.Confirmed && isProductionNS(in.Namespace) && strings.TrimSpace(in.Reason) == "" {
		return &Result{
			OK:      false,
			Message: "规则拦截：生产命名空间 Secret 写操作必须在 reason 字段提供变更原因。",
		}, nil
	}

	// 读当前状态 + immutable 检测
	current, existingType, immutable, curErr := fetchCurrentSecret(ctx, client, in)
	if curErr != nil && !errors.Is(curErr, bcsapi.ErrMockMode) {
		// Secret 不存在也可以继续（当作创建）—— 但记下来
		current = nil
	}
	// 跨 type 禁止（服务端通常也会拒，这里前置更友好）
	if existingType != "" && existingType != in.Type {
		return nil, fmt.Errorf("type 不匹配：现有 Secret 类型为 %q，入参 %q；Secret 类型不允许变更，请删重建",
			existingType, in.Type)
	}
	// immutable 检测
	if immutable && !in.AllowImmutable {
		return &Result{
			OK: false,
			Message: fmt.Sprintf(
				"Secret %s/%s 标记为 immutable=true，不可直接更新；"+
					"如确需修改请 1) 设 allow_immutable=true（工具将走'删除重建'路径，存在短暂空窗期）"+
					"或 2) 手动删除后重建。",
				in.Namespace, in.Name,
			),
			Data: map[string]any{"immutable": true, "requires_delete_recreate": true},
		}, nil
	}

	diff := computeSecretDiff(current, encodeAllBase64(in.Data), nil)
	plan := buildSecretSetPlan(in, severity, diff, keys, immutable)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	// 合并：保留未变 key + 覆盖/新增本次 key
	encodedNew := encodeAllBase64(in.Data)
	merged := mergeSecretData(current, encodedNew)
	snapshot := makeSecretSnapshot(current)
	body := buildSecretBody(in, merged, snapshot)

	// immutable Secret 特殊路径：先删后建
	if immutable && in.AllowImmutable {
		if err := deleteSecret(ctx, client, in); err != nil && !errors.Is(err, bcsapi.ErrMockMode) {
			return nil, fmt.Errorf("immutable 路径下删除原 Secret 失败: %w", err)
		}
	}

	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/secrets/%s",
		in.ClusterID, in.Namespace, in.Name)
	var resp map[string]any
	err := client.PutJSON(ctx, path, body, &resp)
	auditExtra := map[string]any{
		"op":                "set",
		"type":              in.Type,
		"rollout_strategy":  in.RolloutStrategy,
		"keys_count":        len(keys),
		"snapshot_id":       snapshot.ID,
		"diff_summary":      summarizeSecretDiff(diff),
		"immutable_recycle": immutable && in.AllowImmutable,
	}
	if errors.Is(err, bcsapi.ErrMockMode) {
		emitSecretAudit(client, in, "set", severity, true, nil, auditExtra, current, merged)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：Secret %s/%s 更新（%d key，type=%s），snapshot_id=%s",
				in.Namespace, in.Name, len(keys), in.Type, snapshot.ID),
			Data: map[string]any{
				"snapshot_id":      snapshot.ID,
				"diff":             diff,
				"type":             in.Type,
				"keys_changed":     len(keys),
				"rollout_strategy": in.RolloutStrategy,
				"rollout_status":   "simulated (mock)",
			},
		}, nil
	}
	if err != nil {
		emitSecretAudit(client, in, "set", severity, false, err, auditExtra, current, merged)
		return nil, fmt.Errorf("set Secret 失败: %w", err)
	}

	rolloutResult, rolloutErr := triggerSecretRollout(ctx, client, in)
	auditExtra["rollout_result"] = rolloutResult
	ok := rolloutErr == nil
	emitSecretAudit(client, in, "set", severity, ok, rolloutErr, auditExtra, current, merged)

	msg := fmt.Sprintf("Secret %s/%s 已更新（%d key，snapshot_id=%s）",
		in.Namespace, in.Name, len(keys), snapshot.ID)
	if rolloutErr != nil {
		msg += fmt.Sprintf("；rollout 失败：%v", rolloutErr)
	}
	return &Result{
		OK:      ok,
		Message: msg,
		Data: map[string]any{
			"snapshot_id":      snapshot.ID,
			"diff":             diff,
			"type":             in.Type,
			"keys_changed":     len(keys),
			"rollout_strategy": in.RolloutStrategy,
			"rollout_result":   rolloutResult,
		},
	}, nil
}

// =============================================================================
// op=delete：删 keys
// =============================================================================

func doSecretDelete(ctx context.Context, client *bcsapi.Client, in SecretUpdateInput) (*Result, error) {
	if len(in.DeleteKeys) == 0 {
		return nil, fmt.Errorf("op=delete 必须指定 delete_keys（至少 1 个）")
	}
	if err := validateSecretRolloutStrategy(in); err != nil {
		return nil, err
	}

	severity := classifySecretSeverity("delete", in.Namespace, len(in.DeleteKeys), in.RolloutStrategy)
	if in.Confirmed && isProductionNS(in.Namespace) && strings.TrimSpace(in.Reason) == "" {
		return &Result{OK: false, Message: "规则拦截：生产命名空间 Secret 删除 key 必须提供 reason。"}, nil
	}

	current, _, immutable, _ := fetchCurrentSecret(ctx, client, in)
	if immutable && !in.AllowImmutable {
		return &Result{
			OK: false,
			Message: "Secret immutable=true 不允许删 key；如确需请 allow_immutable=true（走删除重建）或手动处理。",
		}, nil
	}
	diff := computeSecretDiff(current, nil, in.DeleteKeys)
	plan := buildSecretDeletePlan(in, severity, diff, immutable)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	merged := removeKeys(current, in.DeleteKeys)
	snapshot := makeSecretSnapshot(current)
	body := buildSecretBody(in, merged, snapshot)

	if immutable && in.AllowImmutable {
		if err := deleteSecret(ctx, client, in); err != nil && !errors.Is(err, bcsapi.ErrMockMode) {
			return nil, fmt.Errorf("immutable 路径下删除原 Secret 失败: %w", err)
		}
	}

	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/secrets/%s",
		in.ClusterID, in.Namespace, in.Name)
	var resp map[string]any
	err := client.PutJSON(ctx, path, body, &resp)
	auditExtra := map[string]any{
		"op":               "delete",
		"rollout_strategy": in.RolloutStrategy,
		"delete_keys":      in.DeleteKeys,
		"snapshot_id":      snapshot.ID,
		"diff_summary":     summarizeSecretDiff(diff),
	}
	if errors.Is(err, bcsapi.ErrMockMode) {
		emitSecretAudit(client, in, "delete", severity, true, nil, auditExtra, current, merged)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：Secret %s/%s 删除 %d key，snapshot_id=%s",
				in.Namespace, in.Name, len(in.DeleteKeys), snapshot.ID),
			Data: map[string]any{
				"snapshot_id":      snapshot.ID,
				"diff":             diff,
				"deleted_keys":     in.DeleteKeys,
				"rollout_strategy": in.RolloutStrategy,
				"rollout_status":   "simulated (mock)",
			},
		}, nil
	}
	if err != nil {
		emitSecretAudit(client, in, "delete", severity, false, err, auditExtra, current, merged)
		return nil, fmt.Errorf("delete Secret keys 失败: %w", err)
	}

	rolloutResult, rolloutErr := triggerSecretRollout(ctx, client, in)
	auditExtra["rollout_result"] = rolloutResult
	ok := rolloutErr == nil
	emitSecretAudit(client, in, "delete", severity, ok, rolloutErr, auditExtra, current, merged)
	return &Result{
		OK: ok,
		Message: fmt.Sprintf("Secret %s/%s 已删除 %d key，snapshot_id=%s",
			in.Namespace, in.Name, len(in.DeleteKeys), snapshot.ID),
		Data: map[string]any{
			"snapshot_id":      snapshot.ID,
			"diff":             diff,
			"deleted_keys":     in.DeleteKeys,
			"rollout_strategy": in.RolloutStrategy,
			"rollout_result":   rolloutResult,
		},
	}, nil
}

// =============================================================================
// op=rollback
// =============================================================================

func doSecretRollback(ctx context.Context, client *bcsapi.Client, in SecretUpdateInput) (*Result, error) {
	if strings.TrimSpace(in.SnapshotID) == "" {
		return nil, fmt.Errorf("op=rollback 必须指定 snapshot_id（无快照不允许黑盒回滚）")
	}
	if in.RolloutStrategy == "" {
		in.RolloutStrategy = rolloutRollingRestart
	}
	if err := validateSecretRolloutStrategy(in); err != nil {
		return nil, err
	}

	severity := hitl.SeverityMedium
	if isProductionNS(in.Namespace) && in.RolloutStrategy == rolloutImmediateRestart {
		severity = hitl.SeverityHigh
	}

	current, _, immutable, curErr := fetchCurrentSecret(ctx, client, in)
	if curErr != nil && !errors.Is(curErr, bcsapi.ErrMockMode) {
		return nil, fmt.Errorf("读取当前 Secret 失败: %w", curErr)
	}
	if immutable && !in.AllowImmutable {
		return &Result{
			OK:      false,
			Message: "Secret immutable=true 不允许直接 rollback，需 allow_immutable=true（删除重建）或手动处理。",
		}, nil
	}
	snapshot, sErr := loadSecretSnapshot(current, in.SnapshotID)
	if sErr != nil {
		return nil, fmt.Errorf("加载快照失败: %w", sErr)
	}

	plan := buildSecretRollbackPlan(in, severity, snapshot)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	newSnap := makeSecretSnapshot(current)
	body := buildSecretBody(in, snapshot.Data, newSnap)

	if immutable && in.AllowImmutable {
		if err := deleteSecret(ctx, client, in); err != nil && !errors.Is(err, bcsapi.ErrMockMode) {
			return nil, fmt.Errorf("immutable 路径下删除原 Secret 失败: %w", err)
		}
	}

	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/secrets/%s",
		in.ClusterID, in.Namespace, in.Name)
	var resp map[string]any
	err := client.PutJSON(ctx, path, body, &resp)
	auditExtra := map[string]any{
		"op":                 "rollback",
		"target_snapshot_id": in.SnapshotID,
		"new_snapshot_id":    newSnap.ID,
		"rollout_strategy":   in.RolloutStrategy,
	}
	if errors.Is(err, bcsapi.ErrMockMode) {
		emitSecretAudit(client, in, "rollback", severity, true, nil, auditExtra, current, snapshot.Data)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：Secret %s/%s 已回滚到 %s（新快照=%s）",
				in.Namespace, in.Name, in.SnapshotID, newSnap.ID),
			Data: map[string]any{
				"target_snapshot_id": in.SnapshotID,
				"new_snapshot_id":    newSnap.ID,
				"rollout_strategy":   in.RolloutStrategy,
				"rollout_status":     "simulated (mock)",
			},
		}, nil
	}
	if err != nil {
		emitSecretAudit(client, in, "rollback", severity, false, err, auditExtra, current, snapshot.Data)
		return nil, fmt.Errorf("rollback Secret 失败: %w", err)
	}
	rolloutResult, rolloutErr := triggerSecretRollout(ctx, client, in)
	auditExtra["rollout_result"] = rolloutResult
	ok := rolloutErr == nil
	emitSecretAudit(client, in, "rollback", severity, ok, rolloutErr, auditExtra, current, snapshot.Data)
	return &Result{
		OK: ok,
		Message: fmt.Sprintf("Secret %s/%s 已回滚到 %s（新快照=%s）",
			in.Namespace, in.Name, in.SnapshotID, newSnap.ID),
		Data: map[string]any{
			"target_snapshot_id": in.SnapshotID,
			"new_snapshot_id":    newSnap.ID,
			"rollout_strategy":   in.RolloutStrategy,
			"rollout_result":     rolloutResult,
		},
	}, nil
}

// =============================================================================
// Plan 构造
// =============================================================================

func buildSecretSetPlan(in SecretUpdateInput, severity hitl.Severity, diff []SecretDiffEntry, keys []string, immutable bool) hitl.Plan {
	side := fmt.Sprintf(
		"向 %s/%s 写入 %d 个敏感 key（type=%s，rollout=%s）。"+
			"⚠ value 内容不会被展示或审计（仅记录 key 名和字节数）。",
		in.Namespace, in.Name, len(keys), in.Type, in.RolloutStrategy,
	)
	if immutable {
		side += "【高危】Secret 标记 immutable=true，工具将走'删除重建'路径，存在短暂空窗期；挂载该 Secret 的 Pod 期间可能读到空值。"
	}
	if in.Type == SecretTypeTLS {
		side += "【TLS 类型】证书/私钥会被更换，所有终止 TLS 的入口可能短暂不可用直到 Pod 重启完成。"
	}
	return hitl.Plan{
		Action:       "bcs.secret.set",
		Severity:     severity,
		Target:       fmt.Sprintf("%s/%s/%s", in.ClusterID, in.Namespace, in.Name),
		SideEffect:   side,
		ImpactScope:  fmt.Sprintf("Secret %s 被 %q 挂载的所有 Pod；以 env 注入的需要重启才生效。", in.Name, firstNonEmptyStr(in.LinkedDeployment, "(未指定 linked_deployment)")),
		RollbackPlan: "执行成功后返回 snapshot_id，可 op=rollback + snapshot_id 恢复（immutable 场景仍走删重建）。",
		Params: map[string]any{
			"op":               "set",
			"cluster_id":       in.ClusterID,
			"namespace":        in.Namespace,
			"name":             in.Name,
			"type":             in.Type,
			"keys":             keys, // 只列 key 名，不含 value
			"keys_count":       len(keys),
			"rollout_strategy": in.RolloutStrategy,
			"immutable":        immutable,
			"diff":             diff, // 只含 key + 长度
		},
		RequireReason: isProductionNS(in.Namespace) || immutable,
	}
}

func buildSecretDeletePlan(in SecretUpdateInput, severity hitl.Severity, diff []SecretDiffEntry, immutable bool) hitl.Plan {
	side := fmt.Sprintf(
		"从 %s/%s 删除 %d 个 key：%v，生效策略 %q。⚠ 删 Secret key 比改值更危险（没有兜底）。",
		in.Namespace, in.Name, len(in.DeleteKeys), in.DeleteKeys, in.RolloutStrategy,
	)
	if immutable {
		side += "【高危】Secret immutable=true，将删除重建。"
	}
	return hitl.Plan{
		Action:       "bcs.secret.delete",
		Severity:     severity,
		Target:       fmt.Sprintf("%s/%s/%s", in.ClusterID, in.Namespace, in.Name),
		SideEffect:   side,
		ImpactScope:  fmt.Sprintf("Secret %s 被 %q 挂载的所有 Pod；缺失 key 可能导致启动失败。", in.Name, firstNonEmptyStr(in.LinkedDeployment, "(未指定 linked_deployment)")),
		RollbackPlan: "执行后自动留快照，可 op=rollback + snapshot_id 恢复。",
		Params: map[string]any{
			"op":               "delete",
			"cluster_id":       in.ClusterID,
			"namespace":        in.Namespace,
			"name":             in.Name,
			"delete_keys":      in.DeleteKeys,
			"rollout_strategy": in.RolloutStrategy,
			"immutable":        immutable,
			"diff":             diff,
		},
		RequireReason: isProductionNS(in.Namespace) || immutable,
	}
}

func buildSecretRollbackPlan(in SecretUpdateInput, severity hitl.Severity, snapshot *secretSnapshot) hitl.Plan {
	return hitl.Plan{
		Action:   "bcs.secret.rollback",
		Severity: severity,
		Target:   fmt.Sprintf("%s/%s/%s → snapshot %s", in.ClusterID, in.Namespace, in.Name, in.SnapshotID),
		SideEffect: fmt.Sprintf(
			"将 Secret %s 回滚到 %s 拍摄的快照（%d 个 key），生效策略 %q。",
			in.Name, snapshot.CreatedAt, len(snapshot.Data), in.RolloutStrategy,
		),
		ImpactScope:  "当前状态会被覆盖；执行前会自动保存为新快照供再次回滚。",
		RollbackPlan: "不可再次 rollback 到同一 snapshot_id；需 op=rollback + 新快照 ID（本次执行返回）。",
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
// Severity 分级
// =============================================================================

// classifySecretSeverity Secret 风险分级（比 configmap 整体更高一档）。
//
// 规则：
//   - 生产 ns 任何写操作：至少 Critical（Secret 事关身份）
//   - 非生产 set + none rollout + keys 少：Medium（不到 High，因为 value 脱敏已经降风险）
//   - 非生产 delete：始终 High（删 Secret key 没兜底）
//   - keys > secretSetKeysSoftLimit：升档（爆破保护，Secret 阈值比 configmap 低）
func classifySecretSeverity(op, ns string, keysCount int, rollout string) hitl.Severity {
	prod := isProductionNS(ns)
	if prod {
		// 生产 ns Secret 写：默认 Critical（无论 op）
		if op == "set" && rollout != rolloutImmediateRestart && keysCount <= secretSetKeysSoftLimit {
			return hitl.SeverityHigh // 生产但小范围温和 rollout：High 而非 Critical
		}
		return hitl.SeverityCritical
	}
	switch op {
	case "delete":
		return hitl.SeverityHigh
	case "set":
		if keysCount > secretSetKeysSoftLimit {
			return hitl.SeverityHigh
		}
		if rollout == rolloutImmediateRestart {
			return hitl.SeverityHigh
		}
		return hitl.SeverityMedium
	}
	return hitl.SeverityMedium
}

// =============================================================================
// Type / TLS / rollout 校验
// =============================================================================

func validateSecretType(t string) error {
	switch t {
	case SecretTypeOpaque, SecretTypeTLS, SecretTypeDockerConfigJSON,
		SecretTypeServiceAccountToken, SecretTypeBasicAuth, SecretTypeSSHAuth:
		return nil
	default:
		// 允许未知 type（K8s 支持自定义），但给出警告式默认通过
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("type 不能为空")
		}
		return nil
	}
}

// validateTLSKeys kubernetes.io/tls 要求同时含 tls.crt 和 tls.key。
//
// 这是 K8s 服务端会拒的硬约束，前置校验给更友好的报错。
func validateTLSKeys(data map[string]string) error {
	_, hasCrt := data["tls.crt"]
	_, hasKey := data["tls.key"]
	if !hasCrt || !hasKey {
		return fmt.Errorf("kubernetes.io/tls 类型 Secret 必须同时包含 tls.crt 和 tls.key（当前 keys=%v）",
			sortedKeys(data))
	}
	return nil
}

// validateSecretRolloutStrategy 复用 configmap 的枚举但收紧场景。
func validateSecretRolloutStrategy(in SecretUpdateInput) error {
	switch in.RolloutStrategy {
	case "", rolloutNone:
		in.RolloutStrategy = rolloutNone
		return nil
	case rolloutRollingRestart, rolloutImmediateRestart:
		if strings.TrimSpace(in.LinkedDeployment) == "" {
			return fmt.Errorf("rollout_strategy=%s 必须同时指定 linked_deployment", in.RolloutStrategy)
		}
		return nil
	default:
		return fmt.Errorf("不支持的 rollout_strategy: %q（可选：none / rolling_restart / immediate_restart）",
			in.RolloutStrategy)
	}
}

// =============================================================================
// Diff / Snapshot / 合并（Secret 专用）
// =============================================================================

// computeSecretDiff 只对比 base64 字节；结果中 From/To 只保留长度不含内容。
func computeSecretDiff(current map[string]string, changes map[string]string, delKeys []string) []SecretDiffEntry {
	out := make([]SecretDiffEntry, 0)
	// added / modified
	for k, to := range changes {
		from, exists := current[k]
		toDec, _ := base64.StdEncoding.DecodeString(to)
		if !exists {
			out = append(out, SecretDiffEntry{Op: "added", Key: k, ToLen: len(toDec)})
			continue
		}
		if from != to {
			fromDec, _ := base64.StdEncoding.DecodeString(from)
			out = append(out, SecretDiffEntry{
				Op: "modified", Key: k, FromLen: len(fromDec), ToLen: len(toDec),
			})
		}
	}
	// deleted
	for _, k := range delKeys {
		if from, exists := current[k]; exists {
			fromDec, _ := base64.StdEncoding.DecodeString(from)
			out = append(out, SecretDiffEntry{Op: "deleted", Key: k, FromLen: len(fromDec)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func summarizeSecretDiff(diff []SecretDiffEntry) map[string]int {
	out := map[string]int{"added": 0, "modified": 0, "deleted": 0}
	for _, d := range diff {
		out[d.Op]++
	}
	return out
}

func makeSecretSnapshot(current map[string]string) *secretSnapshot {
	// 仅复制"真实 data"，不含合成 __annotation_ 字段
	clean := make(map[string]string, len(current))
	for k, v := range current {
		if strings.HasPrefix(k, "__annotation_") {
			continue
		}
		clean[k] = v
	}
	return &secretSnapshot{
		ID:        fmt.Sprintf("SNAP-%d", time.Now().UnixNano()),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Data:      clean,
	}
}

// loadSecretSnapshot 从当前 Secret 的 Annotation 里抽出历史快照（按 ID 匹配）。
func loadSecretSnapshot(current map[string]string, targetID string) (*secretSnapshot, error) {
	raw, ok := current["__annotation_secret_snapshot__"]
	if !ok {
		return nil, fmt.Errorf("Secret 无快照 annotation，无法回滚")
	}
	var snap secretSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, fmt.Errorf("解析快照 JSON 失败: %w", err)
	}
	if snap.ID != targetID {
		return nil, fmt.Errorf("快照 ID 不匹配：期望 %q，实际 %q", targetID, snap.ID)
	}
	return &snap, nil
}

// encodeAllBase64 将明文 map 转 base64 编码 map（tools 层自动做，用户无需手动编码）。
func encodeAllBase64(plain map[string]string) map[string]string {
	out := make(map[string]string, len(plain))
	for k, v := range plain {
		out[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	return out
}

// mergeSecretData 现有 base64 map + 新 base64 map → 合并（覆盖同名 key）。
// 注意剔除 __annotation_ 开头的合成字段。
func mergeSecretData(current, changes map[string]string) map[string]string {
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

// sortedDataKeys 过滤合成字段后排序，给 get/审计摘要用。
func sortedDataKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if strings.HasPrefix(k, "__annotation_") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// =============================================================================
// 读/写/删 Secret（bcsapi 交互）
// =============================================================================

// fetchCurrentSecret 拉当前 Secret，返回 data/type/immutable。
func fetchCurrentSecret(ctx context.Context, client *bcsapi.Client, in SecretUpdateInput) (
	data map[string]string, secretType string, immutable bool, err error) {
	if client != nil && client.IsMock() {
		// Mock 默认是 Opaque，专供 TypeMismatch / 普通 set / rollback 测试。
		//
		// 例外：name 命中"-tls"约定 → 模拟一个**已存在的 TLS Secret**，让 set type=TLS
		// 的"协议匹配"测试能跑通；name 命中"-new"约定 → 模拟"不存在"（创建场景），
		// existingType 留空，调用方走的是创建路径而非更新路径。
		nameLower := strings.ToLower(in.Name)
		switch {
		case strings.Contains(nameLower, "-new"):
			// 创建场景：返回"不存在"
			return nil, "", false, bcsapi.ErrMockMode
		case strings.Contains(nameLower, "-tls"):
			mockTLS := map[string]string{
				"tls.crt": base64.StdEncoding.EncodeToString([]byte("old-cert-pem")),
				"tls.key": base64.StdEncoding.EncodeToString([]byte("old-key-pem")),
			}
			return mockTLS, SecretTypeTLS, false, bcsapi.ErrMockMode
		}
		// Mock 返回一个典型 Opaque Secret，含 1 个历史快照供 rollback 测试
		mockData := map[string]string{
			"db.password": base64.StdEncoding.EncodeToString([]byte("old-passwd")),
			"api.token":   base64.StdEncoding.EncodeToString([]byte("old-token-xxx")),
		}
		snap := &secretSnapshot{
			ID:        "SNAP-MOCK-HISTORY",
			CreatedAt: "2026-04-20T10:00:00Z",
			Data: map[string]string{
				"db.password": base64.StdEncoding.EncodeToString([]byte("very-old-passwd")),
			},
		}
		snapJSON, _ := json.Marshal(snap)
		mockData["__annotation_secret_snapshot__"] = string(snapJSON)
		return mockData, SecretTypeOpaque, false, bcsapi.ErrMockMode
	}
	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/secrets/%s",
		in.ClusterID, in.Namespace, in.Name)
	var resp map[string]any
	if err = client.Get(ctx, path, nil, &resp); err != nil {
		return nil, "", false, err
	}
	// 提取 type
	secretType = getString(resp, "type")
	// 提取 immutable
	immutable = getBool(resp, "immutable")
	// 提取 data（K8s Secret 的 data 字段就是 base64 后的 map[string]string）
	dataRaw := getMap(resp, "data")
	data = make(map[string]string, len(dataRaw))
	for k, v := range dataRaw {
		if s, ok := v.(string); ok {
			data[k] = s
		}
	}
	// 把 snapshot annotation 合成到 data（用前缀避免冲突）
	meta := getMap(resp, "metadata")
	anns := getMap(meta, "annotations")
	if raw := getString(anns, secretSnapshotAnnotationKey); raw != "" {
		data["__annotation_secret_snapshot__"] = raw
	}
	return data, secretType, immutable, nil
}

// buildSecretBody 组 Secret PUT body。
func buildSecretBody(in SecretUpdateInput, data map[string]string, snap *secretSnapshot) map[string]any {
	snapJSON, _ := json.Marshal(snap)
	body := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      in.Name,
			"namespace": in.Namespace,
			"annotations": map[string]string{
				secretSnapshotAnnotationKey: string(snapJSON),
			},
		},
		"type": in.Type,
		"data": data, // 已经是 base64
	}
	return body
}

// deleteSecret immutable 路径下先删再建。
func deleteSecret(ctx context.Context, client *bcsapi.Client, in SecretUpdateInput) error {
	if client != nil && client.IsMock() {
		return bcsapi.ErrMockMode
	}
	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/secrets/%s",
		in.ClusterID, in.Namespace, in.Name)
	var resp map[string]any
	if err := client.DeleteJSON(ctx, path, nil, &resp); err != nil {
		return fmt.Errorf("delete secret failed: %w", err)
	}
	return nil
}

// triggerSecretRollout 与 configmap 共用 triggerRollout 思路但 path 独立。
// 为了复用，直接构造一个 ConfigmapUpdateInput 形态调 triggerRollout 会污染语义；
// 所以复制一小段专用逻辑（避免耦合）。
func triggerSecretRollout(ctx context.Context, client *bcsapi.Client, in SecretUpdateInput) (map[string]any, error) {
	if in.RolloutStrategy == rolloutNone {
		return map[string]any{"strategy": "none", "status": "skipped"}, nil
	}
	path := fmt.Sprintf("/clusters/%s/apis/apps/v1/namespaces/%s/deployments/%s",
		in.ClusterID, in.Namespace, in.LinkedDeployment)
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"kubectl.kubernetes.io/restartedAt": time.Now().Format(time.RFC3339),
						"gameops-agent.tencent.com/cause":   "secret-update",
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
	return map[string]any{
		"strategy":   in.RolloutStrategy,
		"deployment": in.LinkedDeployment,
		"status":     "patched",
	}, nil
}

// =============================================================================
// 审计
// =============================================================================

// emitSecretAudit 统一审计。**绝不写 value**，只写 key+length 摘要。
func emitSecretAudit(client *bcsapi.Client, in SecretUpdateInput, op string,
	severity hitl.Severity, ok bool, err error, extra map[string]any,
	before, after map[string]string) {
	params := map[string]any{
		"op":         op,
		"cluster_id": in.ClusterID,
		"namespace":  in.Namespace,
		"name":       in.Name,
		"from":       secretDataDigest(before),
		"to":         secretDataDigest(after),
	}
	if in.Reason != "" {
		params["reason"] = in.Reason
	}
	for k, v := range extra {
		params[k] = v
	}
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   "bcs.secret." + op,
		Severity: string(severity),
		Target:   fmt.Sprintf("%s/%s/%s", in.ClusterID, in.Namespace, in.Name),
		Params:   params,
		Success:  ok,
		Err:      err,
		Mock:     client != nil && client.IsMock(),
	})
}

// secretDataDigest 审计摘要：key 列表 + 字节数（解码后），绝不含 value 本体。
//
// 注意与 configmap 的 dataDigest 差异：
//   - configmap 摘要只有 keys
//   - Secret 额外含每个 key 的 value 字节数，方便审计"这次改的是不是数量级合理的内容"
//   - **仍然不含 value 本体**
func secretDataDigest(d map[string]string) map[string]any {
	keys := make([]string, 0, len(d))
	lens := make(map[string]int, len(d))
	for k, v := range d {
		if strings.HasPrefix(k, "__annotation_") {
			continue
		}
		keys = append(keys, k)
		decoded, _ := base64.StdEncoding.DecodeString(v)
		lens[k] = len(decoded)
	}
	sort.Strings(keys)
	return map[string]any{
		"keys_count": len(keys),
		"keys":       keys,
		"value_lens": lens,
	}
}
