// Package integration —— D22.1 BCS 全链路端到端集成测试。
//
// # 这次做了啥
//
// D17-D22 累计 5 次"新增工具而不改动 Agent"的扩能力操作：
//
//	场次       | 新工具                 | 架构改动
//	-----------+------------------------+-----------------------
//	D20.2      | bcs_hpa_patch          | RepairAgent 0 行
//	D19.6/7    | WithWaiter 族工厂      | 已有工具源码 0 行
//	D21        | bcs_pod_logs_tail      | DiagnosisAgent 0 行
//	D21.1      | bcs_pod_describe       | DiagnosisAgent 0 行
//	D22        | bcs_secret_update      | RepairAgent 0 行
//
// 5 次都只做了单测覆盖，但跨工具协同、跨 Agent 分组可见性、跨阶段 HITL 协议
// 这些"组装正确性"**一次都没端到端跑过**。本文件补齐。
//
// # 场景矩阵（4 个）
//
//	Scenario A: 诊断链四砖协同（D27 升级：新增 node_describe 第四砖）
//	  resource_query (兜底列) → pod_describe (容器外事件) → pod_logs_tail (容器内日志)
//	    → node_describe (节点层)
//	  【验证 D21 + D21.1 + D24 的协同，DiagnosisAgent 零改动扩能力】
//
//	Scenario B: 修复侧写操作 HITL 两段式协同
//	  scale_deployment + hpa_patch 同组 + secret_update 新工具
//	  每个都走"未 confirmed → Plan → confirmed → 执行"两段式
//	  【验证 D20.2 + D22 的 HITL 协议稳定性】
//
//	Scenario C: 配置侧双路径闭环
//	  configmap_update (敏感键触发拦截) → 切换 secret_update → 轮转 → 重启
//	  【验证 D18.4 + D22 的"敏感键兜底闭环"端到端真实可走】
//
//	Scenario D: Target 分组过滤（诊断链/修复链严格隔离）
//	  DiagnosisAgent (bcs-read) 看到 5 个读工具，看不到任何写工具
//	  RepairAgent (bcs-write) 看到 6 个写工具，看不到任何诊断工具
//	  【验证 5 次零改动扩能力真的没打破 target 边界】
//
//	Scenario E (D27 新增): network_update 多 op 贯穿（D25 真实调用验证）
//	  get Service → set_selector（HITL 两段）→ get Ingress → set_tls（Critical+reason）
//	  → 并发守护 expected_resource_version 冲突验证
//	  → R3 主键保护（patch_spec 含 spec.name）验证
//	  【验证 D25 的 6 op / 三层防护从单测层面晋升到 E2E 层都能稳定触发】
//
//	Scenario F (D27 新增): 节点诊断 → 网络修复贯穿（D24+D25+D26 联合回归）
//	  node_describe 检出某节点 DiskPressure → 用户决定流量切到其他入口
//	  → network_update op=set_backend 改 Ingress 后端（HITL 两段）
//	  【D26 prompt 重构后第一个"跨工具链"回归：验证 LLM 视野里新工具的真实可用性】
//
// # 为什么聚焦"bcs_tools"而非跨包
//
// 既有 repair_flow_test.go 已经覆盖了 gongfeng/devops/tapd/bk 这几条链，
// 唯独缺 BCS —— 这是 D17-D22 重构的主战场，也是本项目最核心的修复通路。
// 本文件补足这块最大的盲区。
package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	bcstools "git.woa.com/trpc-go/gameops-agent/src/tools/bcs_tools"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"

	"git.woa.com/trpc-go/gameops-agent/src/tools"
)

// -----------------------------------------------------------------------------
// 共用辅助（区别于 repair_flow_test.go 的 call，这里需要对 bcs Result
// 的 Data 是嵌套结构体做更宽容的断言）
// -----------------------------------------------------------------------------

// bcsCall 调用 bcs 工具，返回结果 JSON map（把 Result 结构体拍平为 map）。
// 注意 *bcstools.Result 本身就有 json tag，序列化后字段名为 ok/mock/message/data。
func bcsCall(t *testing.T, ct tool.CallableTool, argsJSON string) map[string]any {
	t.Helper()
	raw, err := ct.Call(context.Background(), []byte(argsJSON))
	if err != nil {
		t.Fatalf("bcs call err: %v", err)
	}
	bs, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal bcs result: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(bs, &m); err != nil {
		t.Fatalf("unmarshal bcs result: %v; raw=%s", err, bs)
	}
	return m
}

// newBCSTargeted 构造 Mock BCS client 并返回全部 targeted 工具列表。
func newBCSTargeted(t *testing.T) []tools.TargetedTool {
	t.Helper()
	t.Setenv("BCS_API_MOCK", "1")
	// bcsapi.NewClient 无 gateway URL 时自动降级为 Mock
	client := bcsapi.NewClient()
	if !client.IsMock() {
		t.Fatalf("期望 bcsapi client 处于 Mock 模式，实际非 Mock")
	}
	return bcstools.NewAllTargeted(client)
}

// -----------------------------------------------------------------------------
// Scenario A: 诊断链四砖协同（D21 + D21.1 + D24 零改动扩能力验证）
// -----------------------------------------------------------------------------

// TestBCSIntegration_DiagnosisQuartet 诊断链四砖协同：
//
//	resource_query (兜底列 Pod/Deployment)
//	  ↓
//	pod_describe 拿到 Pod 详情 + Events（容器外故障）
//	  ↓ 同一个 Pod，用 pod_logs_tail 拿到容器内日志
//	  ↓ 组合出"容器内 + 容器外"完整视图
//	  ↓ 若 Events 提示节点压力（Evicted/FailedScheduling），再跳 node_describe
//	node_describe 拿到所在节点的 Conditions/Capacity/Taints/Issues（节点层故障）
//
// 价值：验证 D21 / D21.1 / D24 新增工具与已有 bcs-read 工具协同时，
// 字段结构、Mock 行为、调用契约都正常。**呼应 D26 prompt 里的"三级诊断链"
// （resource_query → pod_describe → node_describe）与 D24 节点层章节**。
func TestBCSIntegration_DiagnosisQuartet(t *testing.T) {
	all := newBCSTargeted(t)

	// ---- Step 1: pod_describe 拉 Pod 详情 ----
	describe := findTool(t, all, "bcs_pod_describe")
	r := bcsCall(t, describe, `{
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"pod":        "game-core-7d9c88fcb7-abcde"
	}`)
	mustOK(t, r, "bcs_pod_describe")
	mustMock(t, r, "bcs_pod_describe")

	data := r["data"].(map[string]any)
	// D21.1 的顶层字段：pod_count / reports ([]PodDescribeReport) / warnings_total
	if _, has := data["reports"]; !has {
		t.Fatalf("pod_describe 缺 reports 字段：%+v", data)
	}
	if _, has := data["pod_count"]; !has {
		t.Fatalf("pod_describe 缺 pod_count 字段：%+v", data)
	}
	// 深入 reports[0] 验证 PodReport 结构（summary/events/containers）
	reports, _ := data["reports"].([]any)
	if len(reports) == 0 {
		t.Fatalf("reports 应至少含一个 Pod 条目：%+v", data)
	}
	firstReport, _ := reports[0].(map[string]any)
	if firstReport == nil {
		t.Fatalf("reports[0] 不是 map：%+v", reports[0])
	}
	for _, key := range []string{"summary", "events", "containers"} {
		if _, has := firstReport[key]; !has {
			t.Fatalf("PodReport 缺 %q 字段：%+v", key, firstReport)
		}
	}

	// ---- Step 2: pod_logs_tail 拉同 Pod 日志 ----
	logsTail := findTool(t, all, "bcs_pod_logs_tail")
	r = bcsCall(t, logsTail, `{
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"pod":        "game-core-7d9c88fcb7-abcde",
		"tail_lines": 100
	}`)
	mustOK(t, r, "bcs_pod_logs_tail")
	mustMock(t, r, "bcs_pod_logs_tail")
	data = r["data"].(map[string]any)
	// D21 的顶层字段：pod_count / entries ([]LogEntry) / total_bytes / total_lines
	for _, key := range []string{"entries", "pod_count", "total_bytes", "total_lines"} {
		if _, has := data[key]; !has {
			t.Fatalf("pod_logs_tail 缺 %q 字段：%+v", key, data)
		}
	}
	entries, _ := data["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("entries 应非空（Mock 至少一条）：%+v", data)
	}

	// ---- Step 3: resource_query 兜底（bcs-read 已有工具仍正常） ----
	resQuery := findTool(t, all, "bcs_resource_query")
	r = bcsCall(t, resQuery, `{
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"resource":   "deployment"
	}`)
	mustOK(t, r, "bcs_resource_query")
	mustMock(t, r, "bcs_resource_query")

	// ---- Step 4 (D27 新增): node_describe 第四砖——节点层诊断 ----
	//
	// 典型场景：pod_describe 看到 Events 里 "FailedScheduling" 或 "Evicted: DiskPressure"，
	// 此时应进一步调用 node_describe 查节点层 Conditions/Taints/Issues，
	// 否则无法区分"Pod 自己出问题"还是"整个节点有问题"。
	nodeDescribe := findTool(t, all, "bcs_node_describe")

	// 4a) 单节点模式（Ready 节点，无 issues）
	r = bcsCall(t, nodeDescribe, `{
		"cluster_id": "BCS-K8S-00001",
		"node":       "node-mock-01"
	}`)
	mustOK(t, r, "bcs_node_describe single-ready")
	mustMock(t, r, "bcs_node_describe single-ready")
	data = r["data"].(map[string]any)
	// D24 的顶层字段：reports / node_count / issues_total
	for _, key := range []string{"reports", "node_count", "issues_total"} {
		if _, has := data[key]; !has {
			t.Fatalf("node_describe 缺 %q 字段：%+v", key, data)
		}
	}
	nodeReports, _ := data["reports"].([]any)
	if len(nodeReports) != 1 {
		t.Fatalf("单节点模式应返 1 个 report，实际 %d", len(nodeReports))
	}
	// report 五段式：summary/conditions/capacity/taints/issues
	nodeRpt, _ := nodeReports[0].(map[string]any)
	for _, section := range []string{"summary", "conditions", "capacity", "taints", "issues"} {
		if _, has := nodeRpt[section]; !has {
			t.Fatalf("NodeReport 缺 %q 段落：%+v", section, nodeRpt)
		}
	}

	// 4b) 批量 nodes[] 模式 + only_issues 过滤（呼应 D26 prompt 里的"批量 >3 调 only_issues 节省 token"建议）
	r = bcsCall(t, nodeDescribe, `{
		"cluster_id":  "BCS-K8S-00001",
		"nodes":       ["node-mock-01", "node-mock-02", "node-mock-03"],
		"only_issues": true
	}`)
	mustOK(t, r, "bcs_node_describe batch+only_issues")
	mustMock(t, r, "bcs_node_describe batch+only_issues")
	data = r["data"].(map[string]any)
	// only_issues 应把 mock-01（Ready）过滤掉，保留 mock-02 + mock-03
	nodeReports, _ = data["reports"].([]any)
	if len(nodeReports) != 2 {
		t.Fatalf("only_issues 应保留 2 个异常节点，实际 %d：%+v", len(nodeReports), data)
	}
	// filtered_out 应为 1
	if fo, _ := data["filtered_out"].(float64); int(fo) != 1 {
		t.Errorf("filtered_out 应为 1（mock-01 被过滤），实际 %v", data["filtered_out"])
	}
	// 至少一个节点的 issues 非空（即 node_describe 确实在"发现问题"）
	foundIssueBearingNode := false
	for _, rep := range nodeReports {
		rm, _ := rep.(map[string]any)
		if issues, _ := rm["issues"].([]any); len(issues) > 0 {
			foundIssueBearingNode = true
			break
		}
	}
	if !foundIssueBearingNode {
		t.Fatalf("batch+only_issues 应至少有一个节点含 issues：%+v", data)
	}
}

// -----------------------------------------------------------------------------
// Scenario B: 修复侧写操作 HITL 两段式协同（D20.2 + D22 验证）
// -----------------------------------------------------------------------------

// TestBCSIntegration_RepairHITLTwoPhase 验证三个写工具 HITL 协议一致性：
//
//	scale_deployment (D18.1 老工具) → hpa_patch (D20.2 新) → secret_update (D22 新)
//
// 三次都走"Stage 1: 未 confirmed → Plan；Stage 2: confirmed → 执行"的两段式，
// 证明 HITL 协议在新旧工具间完全一致，扩能力不会破坏协议。
func TestBCSIntegration_RepairHITLTwoPhase(t *testing.T) {
	all := newBCSTargeted(t)

	// ---- (1) scale_deployment：老工具仍需两段式 ----
	scale := findTool(t, all, "bcs_scale_deployment")
	argsScaleStage1 := `{
		"action":     "scale",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"deployment": "game-core",
		"replicas":   5
	}`
	r := bcsCall(t, scale, argsScaleStage1)
	mustPending(t, r, "scale 阶段1")

	argsScaleStage2 := `{
		"action":     "scale",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"deployment": "game-core",
		"replicas":   5,
		"confirmed":  true
	}`
	r = bcsCall(t, scale, argsScaleStage2)
	mustOK(t, r, "scale 阶段2")
	mustMock(t, r, "scale 阶段2")

	// ---- (2) hpa_patch (D20.2 新)：相同两段式 ----
	hpaPatch := findTool(t, all, "bcs_hpa_patch")
	// get 操作是只读，不需要 HITL
	r = bcsCall(t, hpaPatch, `{
		"op":         "get",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"name":       "game-core-hpa"
	}`)
	mustOK(t, r, "hpa_patch get")
	mustMock(t, r, "hpa_patch get")

	// set_range 写操作走两段式
	argsHPAStage1 := `{
		"op":           "set_range",
		"cluster_id":   "BCS-K8S-00001",
		"namespace":    "staging-letsgo",
		"name":         "game-core-hpa",
		"min_replicas": 2,
		"max_replicas": 10
	}`
	r = bcsCall(t, hpaPatch, argsHPAStage1)
	mustPending(t, r, "hpa_patch set_range 阶段1")

	argsHPAStage2 := `{
		"op":           "set_range",
		"cluster_id":   "BCS-K8S-00001",
		"namespace":    "staging-letsgo",
		"name":         "game-core-hpa",
		"min_replicas": 2,
		"max_replicas": 10,
		"confirmed":    true
	}`
	r = bcsCall(t, hpaPatch, argsHPAStage2)
	mustOK(t, r, "hpa_patch set_range 阶段2")
	mustMock(t, r, "hpa_patch set_range 阶段2")

	// ---- (3) secret_update (D22 新)：相同两段式 ----
	secretUpdate := findTool(t, all, "bcs_secret_update")
	// get 操作只读
	r = bcsCall(t, secretUpdate, `{
		"op":         "get",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"name":       "game-core-secret"
	}`)
	mustOK(t, r, "secret_update get")
	mustMock(t, r, "secret_update get")
	data := r["data"].(map[string]any)
	// 关键：get 返回的 keys 中 value 已脱敏（只含 key 和 value_bytes）
	keysArr, _ := data["keys"].([]any)
	if len(keysArr) == 0 {
		t.Fatalf("secret_update get 应返 keys 数组：%+v", data)
	}
	// 确认没有任何 value 明文泄露到响应里
	rawJSON, _ := json.Marshal(r)
	if strings.Contains(string(rawJSON), "old-passwd") ||
		strings.Contains(string(rawJSON), "old-token-xxx") {
		t.Fatalf("Secret value 本体泄露到响应 JSON：%s", rawJSON)
	}

	// set 写操作走两段式
	argsSecretStage1 := `{
		"op":                "set",
		"cluster_id":        "BCS-K8S-00001",
		"namespace":         "staging-letsgo",
		"name":              "game-core-secret",
		"data":              {"db.password": "rotated-new"},
		"rollout_strategy":  "rolling_restart",
		"linked_deployment": "game-core"
	}`
	r = bcsCall(t, secretUpdate, argsSecretStage1)
	mustPending(t, r, "secret_update set 阶段1")
	// 关键：Plan 里也不能泄露 value
	planJSON, _ := json.Marshal(r)
	if strings.Contains(string(planJSON), "rotated-new") {
		t.Fatalf("Plan 阶段泄露 Secret value 明文：%s", planJSON)
	}

	argsSecretStage2 := `{
		"op":                "set",
		"cluster_id":        "BCS-K8S-00001",
		"namespace":         "staging-letsgo",
		"name":              "game-core-secret",
		"data":              {"db.password": "rotated-new"},
		"rollout_strategy":  "rolling_restart",
		"linked_deployment": "game-core",
		"confirmed":         true
	}`
	r = bcsCall(t, secretUpdate, argsSecretStage2)
	mustOK(t, r, "secret_update set 阶段2")
	mustMock(t, r, "secret_update set 阶段2")
	data = r["data"].(map[string]any)
	if _, has := data["snapshot_id"]; !has {
		t.Fatalf("secret_update set 成功响应应含 snapshot_id：%+v", data)
	}
}

// -----------------------------------------------------------------------------
// Scenario C: 配置侧双路径闭环（D18.4 + D22 验证）
// -----------------------------------------------------------------------------

// TestBCSIntegration_ConfigTwinPath 验证配置侧完整的"二元分流"闭环：
//
//	用户想改密码 → 误用 configmap_update
//	  ↓ configmap_update 敏感键识别 → 拦截并提示用 Secret（D18.4 埋的兜底）
//	用户切换到 secret_update
//	  ↓ secret_update 完成密码轮转（D22 闭环）
//
// 这是"配置侧修复能力毕业"的真实端到端验证。
func TestBCSIntegration_ConfigTwinPath(t *testing.T) {
	all := newBCSTargeted(t)

	// ---- Phase 1: 用户误用 configmap_update 写密码 ----
	//
	// 注意 D18.4 的兜底拦截**只在 confirmed=true 路径下触发**：
	// 这样设计是为了不打扰"还在规划阶段"的用户，只在真要执行时才硬刹车。
	// 拦截条件：敏感键 + confirmed=true + 无 reason → 返回"应使用 Secret"提示。
	// 前置：必须先通过 rollout_strategy / linked_deployment 的必填校验。
	configmapUpdate := findTool(t, all, "bcs_configmap_update")
	argsBadConfigmap := `{
		"op":                "set",
		"cluster_id":        "BCS-K8S-00001",
		"namespace":         "staging-letsgo",
		"name":              "game-core-config",
		"data":              {"db.password": "should-not-be-here"},
		"rollout_strategy":  "rolling_restart",
		"linked_deployment": "game-core",
		"confirmed":         true
	}`
	rPhase1 := bcsCall(t, configmapUpdate, argsBadConfigmap)

	// D18.4 的兜底规则：敏感键 + 无 reason 应触发拦截（ok=false 且 message 提示用 Secret）
	if ok, _ := rPhase1["ok"].(bool); ok {
		t.Fatalf("configmap 写密码应被拦截（D18.4 敏感键规则），实际通过了：%+v", rPhase1)
	}
	msg, _ := rPhase1["message"].(string)
	if !strings.Contains(msg, "Secret") && !strings.Contains(msg, "secret") {
		t.Fatalf("configmap 敏感键拦截消息应提示改用 Secret，实际 %q", msg)
	}

	// ---- Phase 2: 用户切换到 secret_update 正确改 ----
	secretUpdate := findTool(t, all, "bcs_secret_update")
	argsSecretStage1 := `{
		"op":                "set",
		"cluster_id":        "BCS-K8S-00001",
		"namespace":         "staging-letsgo",
		"name":              "game-core-secret",
		"data":              {"db.password": "rotated-correctly"},
		"rollout_strategy":  "rolling_restart",
		"linked_deployment": "game-core"
	}`
	rPhase2Stage1 := bcsCall(t, secretUpdate, argsSecretStage1)
	mustPending(t, rPhase2Stage1, "phase2: secret_update stage1")

	argsSecretStage2 := `{
		"op":                "set",
		"cluster_id":        "BCS-K8S-00001",
		"namespace":         "staging-letsgo",
		"name":              "game-core-secret",
		"data":              {"db.password": "rotated-correctly"},
		"rollout_strategy":  "rolling_restart",
		"linked_deployment": "game-core",
		"confirmed":         true,
		"reason":             "integration test: twin-path closure"
	}`
	rPhase2Stage2 := bcsCall(t, secretUpdate, argsSecretStage2)
	mustOK(t, rPhase2Stage2, "phase2: secret_update stage2")
	mustMock(t, rPhase2Stage2, "phase2: secret_update stage2")

	// ---- Phase 3: 全链路不泄密验证 ----
	//
	// 完整扫描三个响应：Phase 1 拦截响应 + Phase 2 Stage1 Plan + Phase 2 Stage2 执行结果。
	// 任何一处泄露 Secret value 明文都视为严重安全回归。
	for idx, r := range []map[string]any{rPhase1, rPhase2Stage1, rPhase2Stage2} {
		responseJSON, _ := json.Marshal(r)
		if strings.Contains(string(responseJSON), "rotated-correctly") {
			t.Fatalf("Phase %d 响应泄露了 Secret value 明文：%s", idx+1, responseJSON)
		}
	}
}

// -----------------------------------------------------------------------------
// Scenario D: Target 分组过滤（5 次零改动扩能力的边界完整性）
// -----------------------------------------------------------------------------

// TestBCSIntegration_TargetIsolation 验证 target 分组的严格隔离：
//
//	- DiagnosisAgent 订阅 bcs-read，应看到 5 个工具（D21+D21.1 纳入）
//	- RepairAgent 订阅 bcs-write，应看到 6 个工具（D22 纳入）
//	- 诊断 Agent 不应看到任何写工具；修复 Agent 不应看到任何诊断工具
//
// 这是"5 次零改动扩能力"的最终验证：
// 如果某一次扩能力把工具错接到另一个 target，这里就会失败。
func TestBCSIntegration_TargetIsolation(t *testing.T) {
	all := newBCSTargeted(t)

	// ---- DiagnosisAgent 视角（bcs-read）----
	readScope := tools.FilterByTargets(all, []string{"bcs-read"})
	readNames := map[string]bool{}
	for _, tl := range readScope {
		readNames[tl.Declaration().Name] = true
	}

	// D21 / D21.1 / D24 新工具必须出现在 bcs-read
	expectedReadTools := []string{
		"bcs_project_query",
		"bcs_cluster_query",
		"bcs_resource_query",
		"bcs_pod_logs_tail",  // D21
		"bcs_pod_describe",   // D21.1
		"bcs_node_describe",  // D24
	}
	for _, name := range expectedReadTools {
		if !readNames[name] {
			t.Errorf("DiagnosisAgent (bcs-read) 应包含 %q 但未找到；实际可见：%v", name, keysOf(readNames))
		}
	}
	// bcs-read 不应含任何写工具
	forbiddenInRead := []string{
		"bcs_helm_manage", "bcs_scale_deployment", "bcs_pod_restart",
		"bcs_configmap_update", "bcs_secret_update", "bcs_hpa_patch",
	}
	for _, name := range forbiddenInRead {
		if readNames[name] {
			t.Errorf("DiagnosisAgent (bcs-read) 不应看到写工具 %q，当前泄露", name)
		}
	}

	// ---- RepairAgent 视角（bcs-write）----
	writeScope := tools.FilterByTargets(all, []string{"bcs-write"})
	writeNames := map[string]bool{}
	for _, tl := range writeScope {
		writeNames[tl.Declaration().Name] = true
	}
	expectedWriteTools := []string{
		"bcs_helm_manage",
		"bcs_scale_deployment",
		"bcs_pod_restart",
		"bcs_configmap_update",
		"bcs_secret_update",   // D22
		"bcs_hpa_patch",       // D20.2
		"bcs_network_update",  // D25
	}
	for _, name := range expectedWriteTools {
		if !writeNames[name] {
			t.Errorf("RepairAgent (bcs-write) 应包含 %q 但未找到；实际可见：%v", name, keysOf(writeNames))
		}
	}
	// bcs-write 不应含任何诊断工具
	forbiddenInWrite := []string{
		"bcs_project_query", "bcs_cluster_query", "bcs_resource_query",
		"bcs_pod_logs_tail", "bcs_pod_describe", "bcs_node_describe",
	}
	for _, name := range forbiddenInWrite {
		if writeNames[name] {
			t.Errorf("RepairAgent (bcs-write) 不应看到诊断工具 %q，当前泄露", name)
		}
	}

	// ---- 全景断言：bcs_tools 总数 = 13（D25 后，新增 bcs_network_update）----
	if len(all) != 13 {
		names := []string{}
		for _, tl := range all {
			names = append(names, tl.Tool.Declaration().Name)
		}
		t.Errorf("bcs_tools 总数应为 13（D25 后），实际 %d：%v", len(all), names)
	}
}

// keysOf 小工具：导出 map 的键，用于失败时的可读日志。
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// -----------------------------------------------------------------------------
// Scenario E (D27 新增): network_update 多 op 贯穿（D25 真实调用验证）
// -----------------------------------------------------------------------------

// TestBCSIntegration_NetworkUpdateMultiOp 对 bcs_network_update 的 6 op 做核心路径 E2E：
//
//	op=get Service → op=set_selector (HITL 两段) → op=get Ingress
//	  → op=set_tls (Critical + reason) → 并发守护冲突 → R3 主键保护
//
// 这里不重复 network_update_test.go 的单 op 穷举测试，只验证 3 件事：
//   1. 多 op 串起来跑不会互相干扰（Mock client 状态无持久化残留）
//   2. HITL 协议在 network_update 里与其他 bcs-write 工具**完全一致**
//      （未 confirmed → Plan；confirmed=true → 执行），这是 D25 对协议一致性的承诺
//   3. 三层防护（R2 kind 白名单 / R3 主键保护 / R5 resourceVersion 并发守护）
//      在 E2E 层仍能按预期触发 —— 即 Mock 路径里的防护与真实路径对等
//
// 这份测试也是"D26 决策树"在代码层的**投射**：prompt 里教 LLM 怎么用
// network_update 的 6 op，这里就真把 6 op 里的核心 4 op 跑通一遍。
func TestBCSIntegration_NetworkUpdateMultiOp(t *testing.T) {
	all := newBCSTargeted(t)
	netUpdate := findTool(t, all, "bcs_network_update")

	// ---- (1) op=get Service —— 只读不走 HITL ----
	r := bcsCall(t, netUpdate, `{
		"op":         "get",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "default",
		"kind":       "Service",
		"name":       "demo"
	}`)
	mustOK(t, r, "network_update get Service")
	mustMock(t, r, "network_update get Service")
	data := r["data"].(map[string]any)
	spec, _ := data["spec"].(map[string]any)
	if spec == nil {
		t.Fatalf("get Service 应返回 spec 字段：%+v", data)
	}
	if sel, _ := spec["selector"].(map[string]any); sel == nil {
		t.Errorf("get Service Mock 样例应含 spec.selector：%+v", spec)
	}

	// ---- (2) op=set_selector —— HITL 两段式（呼应 D26 "首要阅读" 决策树）----
	argsSelStage1 := `{
		"op":         "set_selector",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "default",
		"kind":       "Service",
		"name":       "demo",
		"selector":   {"app": "new-app", "tier": "frontend"}
	}`
	r = bcsCall(t, netUpdate, argsSelStage1)
	mustPending(t, r, "network_update set_selector 阶段1")

	argsSelStage2 := `{
		"op":         "set_selector",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "default",
		"kind":       "Service",
		"name":       "demo",
		"selector":   {"app": "new-app", "tier": "frontend"},
		"confirmed":  true
	}`
	r = bcsCall(t, netUpdate, argsSelStage2)
	mustOK(t, r, "network_update set_selector 阶段2")
	mustMock(t, r, "network_update set_selector 阶段2")
	data = r["data"].(map[string]any)
	if summary, _ := data["patch_summary"].(string); !strings.Contains(summary, "selector") {
		t.Errorf("set_selector 成功响应 patch_summary 应提及 selector，实际 %q", summary)
	}

	// ---- (3) op=get Ingress —— 验证第二种 kind 也正常 ----
	r = bcsCall(t, netUpdate, `{
		"op":         "get",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "default",
		"kind":       "Ingress",
		"name":       "demo-ing"
	}`)
	mustOK(t, r, "network_update get Ingress")
	mustMock(t, r, "network_update get Ingress")
	data = r["data"].(map[string]any)
	ingSpec, _ := data["spec"].(map[string]any)
	if _, hasRules := ingSpec["rules"]; !hasRules {
		t.Fatalf("get Ingress Mock 应含 spec.rules：%+v", ingSpec)
	}

	// ---- (4) op=set_tls —— Critical 触发点：不带 reason 必须被拒 ----
	//
	// 这是 D26 "统一生产红线"章节的第一个触发条件（TLS 改动必强制 reason）。
	// E2E 层在这里验证：工具拒绝路径里 err 非空，与单测结论一致。
	_, errNoReason := netUpdate.Call(context.Background(), []byte(`{
		"op":              "set_tls",
		"cluster_id":      "BCS-K8S-00001",
		"namespace":       "default",
		"kind":            "Ingress",
		"name":            "demo-ing",
		"tls_host":        "demo.example.com",
		"tls_secret_name": "new-cert",
		"confirmed":       true
	}`))
	if errNoReason == nil {
		t.Fatal("set_tls 未带 reason 应被 Critical 拦截（D26 统一生产红线）")
	}
	if !strings.Contains(errNoReason.Error(), "reason") {
		t.Errorf("set_tls 拒绝原因应提及 reason，实际 %q", errNoReason.Error())
	}

	// ---- (5) op=set_tls 带 reason —— HITL 两段式成功路径 ----
	argsTLSStage1 := `{
		"op":              "set_tls",
		"cluster_id":      "BCS-K8S-00001",
		"namespace":       "default",
		"kind":            "Ingress",
		"name":            "demo-ing",
		"tls_host":        "demo.example.com",
		"tls_secret_name": "new-cert-v2",
		"reason":          "integration-test: 证书轮转演练 2026Q2"
	}`
	r = bcsCall(t, netUpdate, argsTLSStage1)
	mustPending(t, r, "network_update set_tls 阶段1")

	argsTLSStage2 := `{
		"op":              "set_tls",
		"cluster_id":      "BCS-K8S-00001",
		"namespace":       "default",
		"kind":            "Ingress",
		"name":            "demo-ing",
		"tls_host":        "demo.example.com",
		"tls_secret_name": "new-cert-v2",
		"reason":          "integration-test: 证书轮转演练 2026Q2",
		"confirmed":       true
	}`
	r = bcsCall(t, netUpdate, argsTLSStage2)
	mustOK(t, r, "network_update set_tls 阶段2")
	mustMock(t, r, "network_update set_tls 阶段2")

	// ---- (6) R5 并发守护：expected_resource_version 不匹配应被拒 ----
	_, errRVConflict := netUpdate.Call(context.Background(), []byte(`{
		"op":                        "set_selector",
		"cluster_id":                "BCS-K8S-00001",
		"namespace":                 "default",
		"kind":                      "Service",
		"name":                      "demo",
		"selector":                  {"app": "x"},
		"expected_resource_version": "stale-rv-999",
		"confirmed":                 true
	}`))
	if errRVConflict == nil {
		t.Fatal("expected_resource_version 不匹配应被 R5 并发守护拦截")
	}
	if !strings.Contains(errRVConflict.Error(), "R5") {
		t.Errorf("R5 并发守护错误应提及 R5，实际 %q", errRVConflict.Error())
	}

	// ---- (7) R3 主键保护：patch_spec 含 spec.name 应被拒 ----
	_, errR3 := netUpdate.Call(context.Background(), []byte(`{
		"op":         "update_spec",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "default",
		"kind":       "Service",
		"name":       "demo",
		"patch_spec": {"name": "hijacked"},
		"confirmed":  true
	}`))
	if errR3 == nil {
		t.Fatal("patch_spec 含 spec.name 应被 R3 主键保护拒绝")
	}
	if !strings.Contains(errR3.Error(), "R3") {
		t.Errorf("R3 错误应提及 R3，实际 %q", errR3.Error())
	}
}

// -----------------------------------------------------------------------------
// Scenario F (D27 新增): 节点诊断 → 网络修复贯穿剧本（D24+D25+D26 联合回归）
// -----------------------------------------------------------------------------

// TestBCSIntegration_NodeToNetworkHandoff 是 D27 最关键的贯穿剧本：
//
//	现实痛点：某集群节点批量出现 DiskPressure，短期内修不完磁盘，
//	运维决定让入口网关把流量暂时切到灾备集群的后端 Service。
//
//	诊断侧（DiagnosisAgent 视角）：
//	  1. node_describe only_issues=true 快速筛出有问题的节点
//	  2. 确认是 DiskPressure 而非其他问题 → 判定"节点层故障，不可单 Pod 修"
//
//	修复侧（RepairAgent 视角）：
//	  3. network_update op=get Ingress 拿到当前 backend + resourceVersion
//	  4. network_update op=set_backend 把 Ingress 指向新 Service（HITL 两段）
//
// 这个剧本的**独特价值**：
//   - D21/D21.1 时代只能诊断到 Pod 层，遇到节点层故障 agent 只能"说看不见"
//   - D24 补了节点视野，D25 补了网络层修复能力，D26 用 prompt 决策树串起来
//   - 但这三件事**从未在一个测试里跑过** —— Scenario F 就是这个缺失的最后一块
//
// 当这个测试通过，意味着 D24+D25+D26 的"诊断广度+修复能力+ LLM 选择正确"三者
// 在代码层已经机械化可验证 —— prompt 里的"推荐动作"能被真实执行。
func TestBCSIntegration_NodeToNetworkHandoff(t *testing.T) {
	all := newBCSTargeted(t)

	// ===== 诊断侧 =====

	// ---- Step 1: node_describe only_issues=true 筛问题节点 ----
	nodeDescribe := findTool(t, all, "bcs_node_describe")
	r := bcsCall(t, nodeDescribe, `{
		"cluster_id":  "BCS-K8S-00001",
		"nodes":       ["node-mock-01", "node-mock-02", "node-mock-03"],
		"only_issues": true
	}`)
	mustOK(t, r, "F-step1 node_describe only_issues")
	mustMock(t, r, "F-step1 node_describe only_issues")
	data := r["data"].(map[string]any)

	// 断言筛出了异常节点
	reports, _ := data["reports"].([]any)
	if len(reports) == 0 {
		t.Fatalf("F-step1 应筛出至少 1 个异常节点：%+v", data)
	}

	// 找到有 DiskPressure 相关 issue 的节点（呼应 D26 prompt 里的"节点层诊断样板"）
	foundDiskPressureNode := ""
	for _, rep := range reports {
		rm, _ := rep.(map[string]any)
		issuesArr, _ := rm["issues"].([]any)
		for _, issue := range issuesArr {
			if s, _ := issue.(string); strings.Contains(s, "DiskPressure") {
				foundDiskPressureNode, _ = rm["name"].(string)
				break
			}
		}
		if foundDiskPressureNode != "" {
			break
		}
	}
	if foundDiskPressureNode == "" {
		t.Fatalf("F-step1 应至少有一个节点含 DiskPressure issue（Mock 保证 node-mock-02 是）：%+v", reports)
	}
	t.Logf("F-step1 定位到 DiskPressure 节点：%s", foundDiskPressureNode)

	// ===== 修复侧 =====

	netUpdate := findTool(t, all, "bcs_network_update")

	// ---- Step 2: 修复前先"读一眼"当前 Ingress 配置（SRE 纪律：改前先看）----
	//
	// 这一步对应 D26 决策树里"想改网络层前先 op=get 拿现状再决定"的教导。
	// E2E 层验证 get 调用本身正常；并发守护的完整验证放在 Step 4 用"陈旧 RV"反例。
	r = bcsCall(t, netUpdate, `{
		"op":         "get",
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "default",
		"kind":       "Ingress",
		"name":       "demo-ing"
	}`)
	mustOK(t, r, "F-step2 get Ingress")
	mustMock(t, r, "F-step2 get Ingress")
	ingData := r["data"].(map[string]any)
	if _, hasSpec := ingData["spec"]; !hasSpec {
		t.Fatalf("F-step2 get Ingress 应返回 spec 字段：%+v", ingData)
	}

	// ---- Step 3: set_backend 切换 Ingress 后端（HITL 两段式）----
	argsBackendStage1 := `{
		"op":              "set_backend",
		"cluster_id":      "BCS-K8S-00001",
		"namespace":       "default",
		"kind":            "Ingress",
		"name":            "demo-ing",
		"rule_host":       "demo.example.com",
		"rule_path":       "/api",
		"backend_service": "dr-failover-svc",
		"backend_port":    8080
	}`
	r = bcsCall(t, netUpdate, argsBackendStage1)
	mustPending(t, r, "F-step3 set_backend 阶段1")
	// Plan 阶段应能让用户看到影响面（data 里要有 Plan 相关字段，status=awaiting_confirmation 已在 mustPending 验过）

	argsBackendStage2 := `{
		"op":              "set_backend",
		"cluster_id":      "BCS-K8S-00001",
		"namespace":       "default",
		"kind":            "Ingress",
		"name":            "demo-ing",
		"rule_host":       "demo.example.com",
		"rule_path":       "/api",
		"backend_service": "dr-failover-svc",
		"backend_port":    8080,
		"confirmed":       true
	}`
	r = bcsCall(t, netUpdate, argsBackendStage2)
	mustOK(t, r, "F-step3 set_backend 阶段2")
	mustMock(t, r, "F-step3 set_backend 阶段2")
	data = r["data"].(map[string]any)
	summary, _ := data["patch_summary"].(string)
	if !strings.Contains(summary, "dr-failover-svc") || !strings.Contains(summary, "8080") {
		t.Errorf("F-step3 成功响应 patch_summary 应含新 backend，实际 %q", summary)
	}

	// ---- Step 4: 并发守护冲突分支（防止未来 Mock 实现变动导致守护被绕过）----
	//
	// 无论 Mock resourceVersion 是什么，传一个肯定不匹配的值都应被拒。
	_, errStale := netUpdate.Call(context.Background(), []byte(`{
		"op":                        "set_backend",
		"cluster_id":                "BCS-K8S-00001",
		"namespace":                 "default",
		"kind":                      "Ingress",
		"name":                      "demo-ing",
		"rule_host":                 "demo.example.com",
		"rule_path":                 "/api",
		"backend_service":           "dr-failover-svc",
		"backend_port":              8080,
		"expected_resource_version": "absolutely-stale-rv-for-scenario-F",
		"confirmed":                 true
	}`))
	if errStale == nil {
		t.Fatal("F-step4 传入明显陈旧的 expected_resource_version 应被 R5 拦截")
	}
	if !strings.Contains(errStale.Error(), "R5") {
		t.Errorf("F-step4 拒绝原因应提及 R5，实际 %q", errStale.Error())
	}

	// 至此，D24（node_describe）→ D25（network_update）的完整故事线贯穿一次 E2E，
	// 且 D26 prompt 里"节点层诊断样板"+"网络层修复决策"的组合路径被真实触达。
}
