// hpa_detect.go D20 —— HPA（HorizontalPodAutoscaler）冲突检测。
//
// # 为什么单独成文件
//
// HPA 冲突是**真生产事故源**：手动 scale 后 HPA 会在几秒到几分钟内按自己的策略
// 把副本数拉回去——运维以为自己扩容成功了，监控里其实是短暂 spike 后迅速回落，
// 故障没解决、告警还在响、下一次事故升级。
//
// D20 把 HPA 感知能力抽到本文件（而非直接塞进 scale.go）有三条理由：
//
//  1. **可复用**：未来 rollout_restart / helm upgrade 触及副本数的场景都需要 HPA 感知
//  2. **可测试**：HPA 检测逻辑纯数据加工，可以用纯 JSON fixture 单测，不依赖 scale 写路径
//  3. **可独立演进**：HPA 查询格式未来若变（BCS API v5 / K8s v2 autoscaling），
//     修改面局限于本文件，scale.go 写路径语义不变
//
// # 核心数据结构
//
// HPAInfo 是"被 scale 目标的 HPA 情况快照"。字段设计遵循两条原则：
//
//   - **零假设**：HPA 可能不存在（Found=false），上层逻辑必须安全处理
//   - **决策最小必要集**：只暴露做决策真正需要的字段，不做通用 K8s 对象镜像
//
// # BCS API 约定
//
// BCS bcs-storage 对 K8s dynamic 资源的统一查询接口：
//
//	GET /bcsapi/v4/storage/k8s/dynamic/clusters/{cluster}/horizontalpodautoscaler
//	    ?namespace={ns}&scaleTargetRef.name={deploy}
//
// 返回结构与其他 dynamic 资源一致：{"data":[{"data":{<raw HPA object>},...}]}
package bcstools

import (
	"context"
	"fmt"
	"strings"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// HPAInfo 刻画"当前 Deployment 是否挂了 HPA，以及 HPA 的关键参数"。
//
// 决策方可直接读 Found / MinReplicas / MaxReplicas 三字段回答三个核心问题：
//
//	Found=false                                     → 无 HPA，随便 scale
//	Found=true && target ∈ [Min, Max]               → 有 HPA 但目标在区间内（短期可能生效）
//	Found=true && target ∉ [Min, Max]               → 有 HPA 且目标会被秒级回滚（高危）
//
// Raw 保留原始对象仅供审计留痕和面板诊断使用，决策代码**不应**直接读 Raw 的字段。
type HPAInfo struct {
	Found       bool           // 是否找到匹配本 Deployment 的 HPA
	Name        string         // HPA 资源名，用于 Plan 展示
	MinReplicas int            // minReplicas（默认 1，HPA 规范允许省略 min 只写 max）
	MaxReplicas int            // maxReplicas
	CurrentSpec int            // 当前 HPA 状态下的 desiredReplicas（若有）
	Raw         map[string]any // 原始对象，仅用于审计和面板诊断
}

// InRange 判断给定副本数是否落在 HPA 的 [Min, Max] 闭区间内。
//
// 约定：Found=false 时视为"没有约束"，一律返回 true——让上层决策不要写
// `if !info.Found || info.InRange(target)` 这种啰嗦分支。
func (h HPAInfo) InRange(replicas int) bool {
	if !h.Found {
		return true
	}
	return replicas >= h.MinReplicas && replicas <= h.MaxReplicas
}

// hpaDetectPath 返回 BCS storage 的 HPA 查询路径。
// 抽成函数便于测试注入（真实代码直接调 bcsapi.Client.Get）。
func hpaDetectPath(clusterID string) string {
	return fmt.Sprintf("/bcsapi/v4/storage/k8s/dynamic/clusters/%s/horizontalpodautoscaler", clusterID)
}

// DetectHPAForDeployment 查询指定 Deployment 是否被某个 HPA 托管。
//
// # 行为契约
//
//   - Mock 模式：**永远返回 Found=false 且不报错**。理由：Mock 不是真实环境，
//     告警必须基于真实集群数据。强行伪造 Mock HPA 会误导单测作者。
//   - 真实模式：按 BCS 约定查询，失败**不返回错误**而是返回 Found=false。
//     理由是 "HPA 检测失败不应阻断主流程"——scale 本身有 Guard R1/R2 兜底，
//     HPA 感知只是**增量加固**，它的可用性要求应低于 scale 主路径。
//   - 若查询到多个 HPA，只返回第一个匹配的（同一 Deployment 挂多个 HPA 本身就是
//     配置错误，这里保守选第一个，并在 Raw 里保留全量以备审计追溯）。
//
// # 匹配规则
//
// BCS bcs-storage 不支持 scaleTargetRef 的直接过滤，因此这里**拉 ns 下全部 HPA 再客户端过滤**：
// 过滤条件 = spec.scaleTargetRef.name == deployment && spec.scaleTargetRef.kind == Deployment。
//
// 性能考量：生产集群单 ns 的 HPA 数量通常 <20，一次查询 + 内存过滤开销可以接受；
// 若未来某命名空间 HPA 爆炸（>500），需要改用 label selector 或服务端过滤。
func DetectHPAForDeployment(ctx context.Context, client *bcsapi.Client, clusterID, namespace, deployment string) (HPAInfo, error) {
	if client == nil || client.IsMock() {
		// Mock 或未初始化：安全返回"无 HPA"，让上层走正常路径
		return HPAInfo{}, nil
	}

	var resp map[string]any
	query := map[string]string{"namespace": namespace}
	if err := client.Get(ctx, hpaDetectPath(clusterID), query, &resp); err != nil {
		// 查询失败不阻断：回退到"无感知"状态。
		// 上层若强依赖 HPA 感知，应走 hpa_policy=block 模式并显式检查 Found。
		return HPAInfo{}, nil
	}

	return pickHPAForDeployment(resp, deployment), nil
}

// pickHPAForDeployment 从 BCS storage 返回中挑出匹配 Deployment 的 HPA。
//
// 抽成独立函数便于单测（给一段 JSON 就能验证解析逻辑，不需要起 httptest）。
func pickHPAForDeployment(resp map[string]any, deployment string) HPAInfo {
	arr, ok := resp["data"].([]any)
	if !ok || len(arr) == 0 {
		return HPAInfo{}
	}
	for _, item := range arr {
		wrap, _ := item.(map[string]any)
		raw, _ := wrap["data"].(map[string]any)
		if raw == nil {
			continue
		}
		spec, _ := raw["spec"].(map[string]any)
		if spec == nil {
			continue
		}
		ref, _ := spec["scaleTargetRef"].(map[string]any)
		if ref == nil {
			continue
		}
		if refKind, _ := ref["kind"].(string); !strings.EqualFold(refKind, "Deployment") {
			continue
		}
		if refName, _ := ref["name"].(string); refName != deployment {
			continue
		}
		// 命中：提取关键字段
		info := HPAInfo{
			Found: true,
			Raw:   raw,
		}
		if meta, _ := raw["metadata"].(map[string]any); meta != nil {
			info.Name, _ = meta["name"].(string)
		}
		// maxReplicas 必填
		info.MaxReplicas = extractIntField(spec, "maxReplicas")
		// minReplicas 可选，默认 1
		minR := extractIntField(spec, "minReplicas")
		if minR <= 0 {
			minR = 1
		}
		info.MinReplicas = minR
		// status.desiredReplicas 可选
		if st, _ := raw["status"].(map[string]any); st != nil {
			info.CurrentSpec = extractIntField(st, "desiredReplicas")
		}
		return info
	}
	return HPAInfo{}
}

// extractIntField 从 map 里宽松提取 int（JSON 数字默认是 float64）。
// 失败一律返回 0——调用方自己决定如何处理 0。
func extractIntField(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case int64:
			return int(t)
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// D20：HPA 冲突策略
// ---------------------------------------------------------------------------

// HPAConflictPolicy 定义当检测到 HPA 时的处理策略。
//
// 三档语义：
//
//	PolicyBlock → 检测到 HPA 且目标副本数不在 [min,max] 区间时，**硬拒绝**。
//	              等同于 Guard R2 的强度，不允许 HITL 豁免。
//	              适用场景：严格生产环境 / 不允许手动干预 HPA 托管的服务。
//
//	PolicyWarn  → 检测到 HPA 一律不拒绝，但把冲突信息进 Plan（SideEffect 加红），
//	              并把 Severity 强制升到 High（若本来是 Medium）。
//	              适用场景：默认行为——既不误伤合规变更，也不让冲突悄悄通过。
//
//	PolicyForce → 用户明知故犯，跳过 HPA 检查。Severity 强制 Critical 且必须带 reason。
//	              审计里标注 hpa_bypass=true，方便事后追查"为啥 scale 完秒回滚"。
//	              适用场景：紧急止损必须立即改副本数、后续再改 HPA 配置。
//
//	PolicyIgnore→ **仅 rollout_restart 专用**（D20.1）——明知有 HPA 仍滚动重启。
//	              与 Force 的差别：rollout 不改副本数，谈不上"违反"HPA 区间，不需要 Critical；
//	              只需在审计打上 hpa_ignored=true 以便事后回溯。Scale 工具不消费此枚举。
type HPAConflictPolicy string

const (
	PolicyBlock  HPAConflictPolicy = "block"
	PolicyWarn   HPAConflictPolicy = "warn"
	PolicyForce  HPAConflictPolicy = "force"
	PolicyIgnore HPAConflictPolicy = "ignore" // D20.1 新增，rollout_restart 专用
)

// NormalizeHPAPolicy 把用户输入的 policy 字符串规整为枚举值。
//
// 未指定或非法值一律回退到 PolicyWarn：
//   - 默认 Warn 最不打扰（向后兼容：D19 调用方可以完全不改）
//   - 非法值不报错但打到审计里（见上层 emit），避免 LLM 写错字段就失败
func NormalizeHPAPolicy(s string) HPAConflictPolicy {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(PolicyBlock):
		return PolicyBlock
	case string(PolicyForce):
		return PolicyForce
	case "", string(PolicyWarn):
		return PolicyWarn
	default:
		// 非法值：宽松回退到 Warn，不阻断主流程
		return PolicyWarn
	}
}

// ---------------------------------------------------------------------------
// D20.2：按 HPA 名直取（给 bcs_hpa_patch 用）
// ---------------------------------------------------------------------------

// GetHPAByName 按 (cluster, namespace, name) 直接查 HPA，不经 deployment 反查。
//
// # 与 DetectHPAForDeployment 的区别
//
//   - Detect 是"给我一个 deployment，告诉我它被谁 HPA 托管"（反查）
//   - GetByName 是"给我一个 HPA 名，告诉我它的 min/max/targetRef 等配置"（直取）
//
// D20.2 的 bcs_hpa_patch 工具需要在改 HPA 前读取当前值（用于 diff + 并发守护 +
// 幅度保护），此时 LLM 给出的就是 HPA 名，不是 deployment 名。所以不能直接复用
// DetectHPAForDeployment（会拿不到目标）。
//
// # 行为契约（与 Detect 一致）
//
//   - Mock 模式：返回 Found=false 且不报错
//   - 真实查询失败：Found=false + 返回错误（与 Detect 不同：D20.2 的写路径需要知道
//     "究竟是真的无 HPA、还是查询出错"，不能静默吞掉。Detect 是增量加固可以静默，
//     Patch 是主路径必须显式）
//   - 查询成功但无匹配：Found=false，无错误
func GetHPAByName(ctx context.Context, client *bcsapi.Client, clusterID, namespace, name string) (HPAInfo, error) {
	if client == nil || client.IsMock() {
		return HPAInfo{}, nil
	}

	var resp map[string]any
	query := map[string]string{
		"namespace":    namespace,
		"resourceName": name, // BCS storage 约定的按名过滤字段
	}
	if err := client.Get(ctx, hpaDetectPath(clusterID), query, &resp); err != nil {
		// 与 Detect 策略不同：Patch 是主路径，必须把错误抛给调用方决策
		return HPAInfo{}, fmt.Errorf("查询 HPA %s/%s 失败: %w", namespace, name, err)
	}

	return pickHPAByName(resp, name), nil
}

// pickHPAByName 从 BCS storage 返回中挑出指定名称的 HPA。
//
// 独立抽出便于纯 JSON 单测。与 pickHPAForDeployment 的过滤条件不同：
// 这里按 metadata.name 匹配，不关心 scaleTargetRef（即便 HPA 配错也要能读出来做 diff）。
func pickHPAByName(resp map[string]any, name string) HPAInfo {
	arr, ok := resp["data"].([]any)
	if !ok || len(arr) == 0 {
		return HPAInfo{}
	}
	for _, item := range arr {
		wrap, _ := item.(map[string]any)
		raw, _ := wrap["data"].(map[string]any)
		if raw == nil {
			continue
		}
		meta, _ := raw["metadata"].(map[string]any)
		if meta == nil {
			continue
		}
		if metaName, _ := meta["name"].(string); metaName != name {
			continue
		}
		spec, _ := raw["spec"].(map[string]any)
		if spec == nil {
			// 命中但 spec 缺失：仍然标记 Found=true 方便上层报错
			return HPAInfo{Found: true, Name: name, Raw: raw}
		}
		info := HPAInfo{Found: true, Name: name, Raw: raw}
		info.MaxReplicas = extractIntField(spec, "maxReplicas")
		minR := extractIntField(spec, "minReplicas")
		if minR <= 0 {
			minR = 1
		}
		info.MinReplicas = minR
		if st, _ := raw["status"].(map[string]any); st != nil {
			info.CurrentSpec = extractIntField(st, "desiredReplicas")
		}
		return info
	}
	return HPAInfo{}
}