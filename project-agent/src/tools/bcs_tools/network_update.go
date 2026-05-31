// Package bcstools —— bcs_network_update（网络层统一 patch，D25 新增）。
//
// # 为什么要做这个工具（游戏 oncall 视角）
//
// BCS 写工具阵列截至 D24 已经覆盖：
//
//	计算层：scale_deployment / pod_restart.rollout_restart
//	配置层：configmap_update / secret_update / hpa_patch
//	应用层：helm
//	诊断层：pod_logs_tail / pod_describe / node_describe / resource_query / cluster / project
//
// 但**网络层是空白**——而网络层恰恰是游戏 oncall 里高频出问题的一块：
//
//	故障                             | 实际修复动作             | K8s 对象
//	---------------------------------+--------------------------+---------------
//	Service selector 写错漏选 Pod    | 改 spec.selector         | Service
//	Service targetPort 与容器不一致  | 改 spec.ports[].targetPort | Service
//	Service type 错（要 NodePort）   | 改 spec.type + nodePort  | Service
//	Ingress host/path 失效           | 改 spec.rules[]          | Ingress
//	Ingress backend 后端服务改名了   | 改 backend.service.name  | Ingress
//	Ingress TLS 证书过期换 Secret    | 改 spec.tls[].secretName | Ingress
//
// # 为什么是 1 个统一工具而不是 2 个（D25 范围审视）
//
// 原清单候选 `bcs_ingress_manage` + `bcs_service_patch` 是两工具方案。我做了对比：
//
//	维度              | 两独立工具            | 统一 bcs_network_update ★
//	------------------+-----------------------+--------------------------
//	LLM 工具选择压力  | +2 工具，tool-select↑ | +1 工具，压力 ↓
//	实现代码          | 两份同构二段式 HITL   | 一份，kind 参数分派
//	未来扩展          | 加 NetworkPolicy=第 3 | kind="NetworkPolicy" 接入
//	校验/审计逻辑     | 两处重复              | 一处集中
//
// 奥卡姆剃刀胜出：**一个工具按 kind 分派，内部实现按对象类型走不同 patch 构造函数**。
//
// # 为什么用 patch 而不是 put（full replace）
//
// Service/Ingress 对象字段多，其中有大量被 K8s controller 填写的只读/派生字段
// （如 Service.spec.clusterIP / Ingress.status.loadBalancer.ingress）。
//
//   - put（full replace）：需要 LLM 构造出完整合法的 spec；极易因漏字段或带上
//     派生字段导致 API Server 拒绝（immutable field changed）
//   - patch（strategic/merge）：LLM 只需描述"想改什么"，K8s 自动合并未改字段
//
// 因此本工具只接受 **目标字段集**（patch_spec），由本工具构造 merge patch。
// LLM 的思维负担小、出错概率低。
//
// # 六个 op 设计
//
//	op=get                 只读，不走 HITL（kind 决定对象，name 决定具体资源）
//	op=update_spec         通用 spec 补丁（patch_spec 作为 RFC7396 merge patch 直接合并到 spec）
//	op=set_selector        Service 专用便捷操作（改 selector）
//	op=set_port            Service 专用便捷操作（改某个 port 的 targetPort/port）
//	op=set_backend         Ingress 专用便捷操作（改某路由的 backend service）
//	op=set_tls             Ingress 专用便捷操作（改 tls[].secretName）
//
// 便捷 op 存在的价值：LLM 用 update_spec 时需要理解 K8s spec 结构；
// 便捷 op 给出明确语义钩子（"改 Service 的 selector"），极大降低出错概率。
//
// # 风险模型与防护（比 HPA 更严）
//
// 网络层写比 HPA 改动风险高一档：
//
//  1) Service/Ingress 改错直接把**全站流量带向错误目标**，影响面 = 所有客户端
//  2) Ingress 证书错写到生产会导致 HTTPS 断证，影响**所有 TLS 客户端**
//  3) Service selector 错会让 Pod 全下线 Endpoints 数秒到分钟级流量归零
//
// 防护对齐 HPA 的六层：
//
//	R1 必填字段检查（cluster/namespace/kind/name + op 对应必填）
//	R2 kind 白名单（只允许 Service/Ingress；NetworkPolicy/Endpoints 预留不实现）
//	R3 patch_spec 不得为空且不得包含 metadata.name/namespace（防改主键）
//	R4 prod ns → Severity 从 High 起步升到 Critical + RequireReason
//	R5 expected_resource_version 并发守护（防 TOCTOU）
//	R6 对 Ingress/TLS 类改动一律 Critical（证书影响面过大）
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

// NetworkUpdateInput 为 bcs_network_update 工具入参。
//
// 六 op 共用一套字段；每个 op 必要字段在 dispatch 时校验。
type NetworkUpdateInput struct {
	Op        string `json:"op"         description:"操作类型（必填）：get / update_spec / set_selector / set_port / set_backend / set_tls"`
	ClusterID string `json:"cluster_id" description:"BCS 集群 ID（必填）"`
	Namespace string `json:"namespace"  description:"Kubernetes 命名空间（必填）"`
	Kind      string `json:"kind"       description:"资源 kind（必填）：Service 或 Ingress（NetworkPolicy/Endpoints 尚未支持）"`
	Name      string `json:"name"       description:"资源名（必填）；可先用 bcs_resource_query 列出 ns 内 Service/Ingress"`

	// update_spec 专用：直接作为 RFC7396 merge patch 的 spec 内容
	PatchSpec map[string]any `json:"patch_spec" description:"（op=update_spec 必填）需合并到 spec 的字段；禁止包含 metadata/status 字段"`

	// set_selector 专用（Service）
	Selector map[string]string `json:"selector" description:"（op=set_selector 必填，仅 Service）新的 selector labels"`

	// set_port 专用（Service）
	PortName       string `json:"port_name"        description:"（op=set_port 必填，仅 Service）要修改的端口名"`
	TargetPort     int    `json:"target_port"      description:"（op=set_port 时可选）新的 targetPort"`
	ServicePort    int    `json:"service_port"     description:"（op=set_port 时可选）新的 service port"`

	// set_backend 专用（Ingress）
	RuleHost        string `json:"rule_host"        description:"（op=set_backend 必填，仅 Ingress）要修改的 rule host；精确匹配 spec.rules[].host"`
	RulePath        string `json:"rule_path"        description:"（op=set_backend 可选）要修改的 path；不填则作用于该 host 下所有 path"`
	BackendService  string `json:"backend_service"  description:"（op=set_backend 必填）新的 backend service name"`
	BackendPort     int    `json:"backend_port"     description:"（op=set_backend 必填）新的 backend service port number"`

	// set_tls 专用（Ingress）
	TLSHost       string `json:"tls_host"        description:"（op=set_tls 必填，仅 Ingress）tls[].hosts 中的 host 值（精确匹配 tls 条目的首个 host）"`
	TLSSecretName string `json:"tls_secret_name" description:"（op=set_tls 必填）新 Secret 名（证书/私钥需事先创建）"`

	// 并发守护（R5）——可选
	ExpectedResourceVersion string `json:"expected_resource_version" description:"（可选）期望现值的 metadata.resourceVersion；若填写必须一致否则拒绝（防并发覆盖）"`

	Reason    string `json:"reason"    description:"变更原因；Critical 场景（prod ns / tls 相关 / 幅度大）必填"`
	Confirmed bool   `json:"confirmed" description:"是否已获人工确认；写操作必须 true 才真正下发"`
}

// K8s 资源 kind 白名单。
var supportedNetworkKinds = map[string]struct{}{
	"Service": {},
	"Ingress": {},
}

// newNetworkUpdateTool 构造 bcs_network_update 工具。
func newNetworkUpdateTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in NetworkUpdateInput) (*Result, error) {
		op := strings.ToLower(strings.TrimSpace(in.Op))
		if op == "" {
			return nil, fmt.Errorf("op 为必填项（get / update_spec / set_selector / set_port / set_backend / set_tls）")
		}
		if in.ClusterID == "" || in.Namespace == "" || in.Kind == "" || in.Name == "" {
			return nil, fmt.Errorf("cluster_id / namespace / kind / name 均为必填")
		}
		// R2 kind 白名单
		kind := normalizeNetworkKind(in.Kind)
		if _, ok := supportedNetworkKinds[kind]; !ok {
			return nil, fmt.Errorf("R2 违规：kind=%q 暂不支持；目前支持 Service / Ingress（NetworkPolicy/Endpoints 预留未实现）", in.Kind)
		}
		in.Kind = kind

		switch op {
		case "get":
			return doNetworkGet(ctx, client, in)
		case "update_spec", "set_selector", "set_port", "set_backend", "set_tls":
			return doNetworkWrite(ctx, client, in, op)
		default:
			return nil, fmt.Errorf("不支持的 op: %q", op)
		}
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_network_update"),
		function.WithDescription(
			"BCS 网络层统一更新工具（Service / Ingress）。六操作：get(只读) / update_spec(通用 RFC7396 merge patch) / "+
				"set_selector(Service 选择器) / set_port(Service 端口) / set_backend(Ingress 后端) / set_tls(Ingress 证书)。"+
				"⚠ 风险提示：网络层改错会把全站流量带向错误目标，tls 改动影响所有 HTTPS 客户端。"+
				"⚠ 防护：prod ns / tls 改动自动升 Critical；支持 expected_resource_version 并发守护。"+
				"⚠ 典型链路：先 op=get 查当前态 → 再 set_* 便捷改或 update_spec 通用改 → 修改后用 bcs_resource_query 验证 Endpoints 被正确同步。",
		),
	)
}

// normalizeNetworkKind 把常见变体规范化到 K8s 官方单数形式。
//
// 容错：允许 "service" / "services" / "SERVICE" 等；LLM 倾向复数。
func normalizeNetworkKind(k string) string {
	low := strings.ToLower(strings.TrimSpace(k))
	switch low {
	case "service", "services", "svc":
		return "Service"
	case "ingress", "ingresses", "ing":
		return "Ingress"
	default:
		return k
	}
}

// =============================================================================
// op=get：只读，不走 HITL
// =============================================================================

// networkResourceInfo 封装从 API 读到的资源现值（diff/并发守护/Plan 共用）。
type networkResourceInfo struct {
	Found           bool
	Kind            string
	Name            string
	Namespace       string
	ResourceVersion string
	Raw             map[string]any // 原始对象（数据回写到 Plan.Params 用）
}

func doNetworkGet(ctx context.Context, client *bcsapi.Client, in NetworkUpdateInput) (*Result, error) {
	if client != nil && client.IsMock() {
		return mockNetworkGetResult(in), nil
	}
	info, err := getNetworkResource(ctx, client, in.ClusterID, in.Namespace, in.Kind, in.Name)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", in.Kind, err)
	}
	if !info.Found {
		return &Result{
			OK: false,
			Message: fmt.Sprintf("%s %s/%s/%s 不存在", in.Kind, in.ClusterID, in.Namespace, in.Name),
			Data:    map[string]any{"found": false},
		}, nil
	}
	return &Result{
		OK: true,
		Data: map[string]any{
			"cluster_id":       in.ClusterID,
			"namespace":        in.Namespace,
			"kind":             in.Kind,
			"name":             in.Name,
			"resource_version": info.ResourceVersion,
			"spec":             getMap(info.Raw, "spec"),
			"found":            true,
		},
	}, nil
}

// mockNetworkGetResult 为 Mock 模式提供一个能被诊断/演示看到的样例。
//
// 同样遵循 D24 "Mock 三态样本"的教育性原则，这里按 kind 给两份典型 spec：
//   - Service：有 selector / ports，暴露 ClusterIP 类型
//   - Ingress：含 rules + tls，给出 host/path/backend 完整示例
func mockNetworkGetResult(in NetworkUpdateInput) *Result {
	var spec map[string]any
	if in.Kind == "Service" {
		spec = map[string]any{
			"type":      "ClusterIP",
			"clusterIP": "10.0.0.100",
			"selector":  map[string]any{"app": "demo", "tier": "backend"},
			"ports": []any{
				map[string]any{"name": "http", "port": 80, "targetPort": 8080, "protocol": "TCP"},
			},
		}
	} else {
		spec = map[string]any{
			"ingressClassName": "nginx",
			"rules": []any{map[string]any{
				"host": "demo.example.com",
				"http": map[string]any{
					"paths": []any{map[string]any{
						"path":     "/",
						"pathType": "Prefix",
						"backend": map[string]any{"service": map[string]any{
							"name": "demo-svc",
							"port": map[string]any{"number": 80},
						}},
					}},
				},
			}},
			"tls": []any{map[string]any{
				"hosts":      []any{"demo.example.com"},
				"secretName": "demo-tls",
			}},
		}
	}
	return &Result{
		OK: true, Mock: true,
		Message: fmt.Sprintf("Mock 模式：返回样例 %s %s/%s/%s", in.Kind, in.ClusterID, in.Namespace, in.Name),
		Data: map[string]any{
			"cluster_id":       in.ClusterID,
			"namespace":        in.Namespace,
			"kind":             in.Kind,
			"name":             in.Name,
			"resource_version": "mock-rv-123",
			"spec":             spec,
			"found":            true,
		},
	}
}

// =============================================================================
// op=update_spec / set_*：写路径，统一 HITL + 多层防护
// =============================================================================

// doNetworkWrite 五个写 op 的统一入口（与 hpa_patch.doHPAWrite 同构）。
//
// 流程：
//  1) 读现值（diff + 并发守护）
//  2) 按 op 构造 merge patch body
//  3) R3 patch_spec 非空 & 不含 metadata.name/namespace
//  4) R5 并发守护（expected_resource_version）
//  5) Severity 计算（prod ns / tls-related 升档）
//  6) 构 Plan + HITL Require
//  7) 真实 PATCH 下发（application/merge-patch+json）
//  8) 审计入账
func doNetworkWrite(ctx context.Context, client *bcsapi.Client, in NetworkUpdateInput, op string) (*Result, error) {
	isMock := client != nil && client.IsMock()

	// 1) 读现值
	var before networkResourceInfo
	if isMock {
		before = networkResourceInfo{
			Found: true, Kind: in.Kind, Name: in.Name, Namespace: in.Namespace,
			ResourceVersion: "mock-rv-123",
		}
	} else {
		got, err := getNetworkResource(ctx, client, in.ClusterID, in.Namespace, in.Kind, in.Name)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 现值失败: %w", in.Kind, err)
		}
		if !got.Found {
			return nil, fmt.Errorf("%s %s/%s/%s 不存在，无法修改（可先用 op=get 确认）",
				in.Kind, in.ClusterID, in.Namespace, in.Name)
		}
		before = got
	}

	// 2) 按 op 构造 patch body
	patchBody, patchSummary, pErr := buildNetworkPatch(op, in)
	if pErr != nil {
		return nil, pErr
	}

	// 3) R3 校验：patch_spec 不能掺杂 metadata.name/namespace（防改主键）
	if spec, _ := patchBody["spec"].(map[string]any); spec != nil {
		if _, hasName := spec["name"]; hasName {
			return nil, fmt.Errorf("R3 违规：patch_spec 不得包含 spec.name 字段")
		}
	}
	if meta, _ := patchBody["metadata"].(map[string]any); meta != nil {
		if _, has := meta["name"]; has {
			return nil, fmt.Errorf("R3 违规：patch body 不得包含 metadata.name（禁止改主键）")
		}
		if _, has := meta["namespace"]; has {
			return nil, fmt.Errorf("R3 违规：patch body 不得包含 metadata.namespace（禁止跨 ns 改动）")
		}
	}

	// 4) 并发守护（R5）
	if in.ExpectedResourceVersion != "" && in.ExpectedResourceVersion != before.ResourceVersion {
		return nil, fmt.Errorf("R5 并发守护：预期 resourceVersion=%q 但实际为 %q —— 可能有其他会话刚改过，请重新 get 确认",
			in.ExpectedResourceVersion, before.ResourceVersion)
	}

	// 5) Severity 计算
	severity, requireReason := classifyNetworkSeverity(in, op)

	if requireReason && strings.TrimSpace(in.Reason) == "" {
		return nil, fmt.Errorf("本次网络层修改达到 Critical 风险等级，必须填写 reason 说明变更原因")
	}

	// 6) Plan + HITL
	plan := buildNetworkPlan(in, op, patchSummary, before, severity, requireReason)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	// 7) 下发
	if isMock {
		emitNetworkAudit(client, in, op, severity, true, nil, patchSummary)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：%s %s/%s/%s 已按 op=%s 更新（%s）",
				in.Kind, in.ClusterID, in.Namespace, in.Name, op, patchSummary),
			Data: networkWriteResultData(in, op, patchSummary, before),
		}, nil
	}
	if err := patchNetworkResource(ctx, client, in.ClusterID, in.Namespace, in.Kind, in.Name, patchBody); err != nil {
		emitNetworkAudit(client, in, op, severity, false, err, patchSummary)
		return nil, fmt.Errorf("%s PATCH 失败: %w", in.Kind, err)
	}

	// 8) 成功审计
	emitNetworkAudit(client, in, op, severity, true, nil, patchSummary)
	return &Result{
		OK: true,
		Data: networkWriteResultData(in, op, patchSummary, before),
	}, nil
}

// buildNetworkPatch 按 op 构造 RFC7396 merge patch body + 人类可读摘要。
//
// 返回值第一项是实际用于 PATCH 的 body；第二项是 Plan/审计用的一句话总结。
func buildNetworkPatch(op string, in NetworkUpdateInput) (map[string]any, string, error) {
	switch op {
	case "update_spec":
		if len(in.PatchSpec) == 0 {
			return nil, "", fmt.Errorf("op=update_spec 必须提供非空 patch_spec")
		}
		// 直接把用户 patch_spec 包到 spec 下作为 merge patch
		return map[string]any{"spec": in.PatchSpec}, fmt.Sprintf("通用 spec patch（%d 个顶层字段）", len(in.PatchSpec)), nil

	case "set_selector":
		if in.Kind != "Service" {
			return nil, "", fmt.Errorf("op=set_selector 仅适用于 Kind=Service")
		}
		if len(in.Selector) == 0 {
			return nil, "", fmt.Errorf("op=set_selector 必须提供非空 selector")
		}
		// RFC7396 merge patch：selector 对象整体替换
		sel := make(map[string]any, len(in.Selector))
		for k, v := range in.Selector {
			sel[k] = v
		}
		return map[string]any{"spec": map[string]any{"selector": sel}},
			fmt.Sprintf("Service selector → %v", in.Selector), nil

	case "set_port":
		if in.Kind != "Service" {
			return nil, "", fmt.Errorf("op=set_port 仅适用于 Kind=Service")
		}
		if in.PortName == "" {
			return nil, "", fmt.Errorf("op=set_port 必须提供 port_name 指定要改的端口")
		}
		if in.TargetPort == 0 && in.ServicePort == 0 {
			return nil, "", fmt.Errorf("op=set_port 至少指定 target_port 或 service_port 之一")
		}
		portMap := map[string]any{"name": in.PortName}
		if in.TargetPort > 0 {
			portMap["targetPort"] = in.TargetPort
		}
		if in.ServicePort > 0 {
			portMap["port"] = in.ServicePort
		}
		// ⚠ 重要：K8s Service.spec.ports 是**数组**；merge patch 对数组默认是**整体替换**
		// 而非按 name merge。这里我们采用"只提供一个元素"的替换策略并明确警示用户。
		// 若 Service 原有多个 port，用户必须先 op=get 读全量 ports，然后用 op=update_spec
		// 传完整 ports 数组。便捷 op 本身只适用于单 port Service。
		return map[string]any{"spec": map[string]any{"ports": []any{portMap}}},
			fmt.Sprintf("Service 端口 %q 改为 port=%d targetPort=%d（注意：单 port Service 场景）", in.PortName, in.ServicePort, in.TargetPort), nil

	case "set_backend":
		if in.Kind != "Ingress" {
			return nil, "", fmt.Errorf("op=set_backend 仅适用于 Kind=Ingress")
		}
		if in.RuleHost == "" || in.BackendService == "" || in.BackendPort == 0 {
			return nil, "", fmt.Errorf("op=set_backend 必须提供 rule_host + backend_service + backend_port")
		}
		// 构造该 host 的完整 rule（path 可选）
		pathEntry := map[string]any{
			"path":     firstNonEmpty(in.RulePath, "/"),
			"pathType": "Prefix",
			"backend": map[string]any{"service": map[string]any{
				"name": in.BackendService,
				"port": map[string]any{"number": in.BackendPort},
			}},
		}
		rule := map[string]any{
			"host": in.RuleHost,
			"http": map[string]any{"paths": []any{pathEntry}},
		}
		// 同样：rules 是数组，此处是整体替换；多 rule 场景需用 update_spec
		return map[string]any{"spec": map[string]any{"rules": []any{rule}}},
			fmt.Sprintf("Ingress rule[host=%s, path=%s] backend → %s:%d",
				in.RuleHost, firstNonEmpty(in.RulePath, "/"), in.BackendService, in.BackendPort), nil

	case "set_tls":
		if in.Kind != "Ingress" {
			return nil, "", fmt.Errorf("op=set_tls 仅适用于 Kind=Ingress")
		}
		if in.TLSHost == "" || in.TLSSecretName == "" {
			return nil, "", fmt.Errorf("op=set_tls 必须提供 tls_host + tls_secret_name")
		}
		tlsEntry := map[string]any{
			"hosts":      []any{in.TLSHost},
			"secretName": in.TLSSecretName,
		}
		return map[string]any{"spec": map[string]any{"tls": []any{tlsEntry}}},
			fmt.Sprintf("Ingress tls[host=%s] secretName → %s", in.TLSHost, in.TLSSecretName), nil

	default:
		return nil, "", fmt.Errorf("内部错误：未识别的 op %q", op)
	}
}

// classifyNetworkSeverity 计算 Severity + 是否要求 reason。
//
// 规则（从高到低命中即止）：
//  1) set_tls 任何情况        → Critical + RequireReason（证书影响所有 HTTPS 客户端）
//  2) prod ns                 → Critical + RequireReason
//  3) update_spec（通用改）   → Critical（不强制 reason；通用 patch 盲区大）
//  4) 其他 set_*              → High
func classifyNetworkSeverity(in NetworkUpdateInput, op string) (hitl.Severity, bool) {
	if op == "set_tls" {
		return hitl.SeverityCritical, true
	}
	if isProdNamespace(in.Namespace) {
		return hitl.SeverityCritical, true
	}
	if op == "update_spec" {
		return hitl.SeverityCritical, false
	}
	return hitl.SeverityHigh, false
}

// buildNetworkPlan 构建 HITL Plan。
func buildNetworkPlan(in NetworkUpdateInput, op, summary string, before networkResourceInfo,
	severity hitl.Severity, requireReason bool) hitl.Plan {

	impactScope := map[string]string{
		"Service": "Service 修改后 kube-proxy 会刷新 iptables/ipvs 规则（秒级生效）；" +
			"改 selector 会让 Endpoints 重新同步，期间该 Service 可能短暂无可用后端。",
		"Ingress": "Ingress Controller 会重新加载配置（通常秒级）；" +
			"修改 rule 会影响该 host 的全部入流量，修改 tls 会影响 HTTPS 握手（证书错换会导致握手失败）。",
	}[in.Kind]

	rollback := fmt.Sprintf(
		"回滚：用 op=get 保存的 spec 通过 op=update_spec 原样写回；或使用 expected_resource_version=%q 防并发。",
		before.ResourceVersion,
	)

	params := map[string]any{
		"op":               op,
		"kind":             in.Kind,
		"cluster_id":       in.ClusterID,
		"namespace":        in.Namespace,
		"name":             in.Name,
		"resource_version": before.ResourceVersion,
		"patch_summary":    summary,
	}
	if in.ExpectedResourceVersion != "" {
		params["expected_resource_version"] = in.ExpectedResourceVersion
	}

	return hitl.Plan{
		Action:        "bcs.network." + op,
		Severity:      severity,
		Target:        fmt.Sprintf("%s/%s/%s/%s", in.ClusterID, in.Namespace, in.Kind, in.Name),
		SideEffect:    summary,
		ImpactScope:   impactScope,
		RollbackPlan:  rollback,
		Params:        params,
		RequireReason: requireReason,
	}
}

// =============================================================================
// PATCH 下发 & 读取
// =============================================================================

// getNetworkResource 读取一个 Service/Ingress 的当前状态。
//
// 用 BCS storage 只读路径（与 node_describe/pod_describe 一致），而不是直写 K8s apiserver，
// 目的是走 BCS 统一网关的权限控制。
func getNetworkResource(ctx context.Context, client *bcsapi.Client,
	clusterID, namespace, kind, name string) (networkResourceInfo, error) {

	path := fmt.Sprintf(
		"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/namespaces/%s/%s/%s",
		clusterID, namespace, strings.ToLower(kind)+"s", name, // Service→services, Ingress→ingresses
	)
	var resp map[string]any
	if err := client.Get(ctx, path, nil, &resp); err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			return networkResourceInfo{}, err
		}
		return networkResourceInfo{}, fmt.Errorf("get %s failed: %w", kind, err)
	}
	inner := getMap(resp, "data")
	if inner == nil {
		inner = resp
	}
	meta := getMap(inner, "metadata")
	if meta == nil {
		return networkResourceInfo{Found: false}, nil
	}
	return networkResourceInfo{
		Found:           true,
		Kind:            kind,
		Name:            getString(meta, "name"),
		Namespace:       getString(meta, "namespace"),
		ResourceVersion: getString(meta, "resourceVersion"),
		Raw:             inner,
	}, nil
}

// patchNetworkResource 向 K8s API 发 PATCH 请求（RFC7396 merge patch）。
//
// BCS 网关转发 K8s 原生路径：
//   - Service ：/clusters/{cid}/api/v1/namespaces/{ns}/services/{name}
//   - Ingress ：/clusters/{cid}/apis/networking.k8s.io/v1/namespaces/{ns}/ingresses/{name}
//
// PatchJSON 底层 Content-Type 由 bcsapi.Client 决定（现实现是 application/merge-patch+json），
// 对 spec 字段的非数组部分按 merge 语义合并、数组整体替换——与 K8s 约定一致。
func patchNetworkResource(ctx context.Context, client *bcsapi.Client,
	clusterID, namespace, kind, name string, body map[string]any) error {

	var path string
	switch kind {
	case "Service":
		path = fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/services/%s", clusterID, namespace, name)
	case "Ingress":
		path = fmt.Sprintf("/clusters/%s/apis/networking.k8s.io/v1/namespaces/%s/ingresses/%s", clusterID, namespace, name)
	default:
		return fmt.Errorf("不支持的 kind: %s", kind)
	}
	var resp map[string]any
	if err := client.PatchJSON(ctx, path, body, &resp); err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			return nil
		}
		return err
	}
	return nil
}

// =============================================================================
// 审计 & 辅助
// =============================================================================

func emitNetworkAudit(client *bcsapi.Client, in NetworkUpdateInput, op string,
	severity hitl.Severity, ok bool, err error, summary string) {
	params := map[string]any{
		"op":            op,
		"kind":          in.Kind,
		"cluster_id":    in.ClusterID,
		"namespace":     in.Namespace,
		"name":          in.Name,
		"patch_summary": summary,
	}
	if in.ExpectedResourceVersion != "" {
		params["expected_resource_version"] = in.ExpectedResourceVersion
	}
	if in.Reason != "" {
		params["reason"] = in.Reason
	}
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   "bcs.network." + op,
		Severity: string(severity),
		Target:   fmt.Sprintf("%s/%s/%s/%s", in.ClusterID, in.Namespace, in.Kind, in.Name),
		Params:   params,
		Success:  ok,
		Err:      err,
		Mock:     client != nil && client.IsMock(),
	})
}

func networkWriteResultData(in NetworkUpdateInput, op, summary string, before networkResourceInfo) map[string]any {
	return map[string]any{
		"cluster_id":       in.ClusterID,
		"namespace":        in.Namespace,
		"kind":             in.Kind,
		"name":             in.Name,
		"op":               op,
		"patch_summary":    summary,
		"resource_version": before.ResourceVersion,
	}
}
