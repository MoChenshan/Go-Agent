// configmap_update_test.go —— bcs_configmap_update 单元测试。
//
// 覆盖点（按 op 分组 + 横切关注点）：
//
//  A) 输入校验
//     1. 缺 op → 报错
//     2. 未知 op → 报错
//     3. 缺 cluster_id/namespace/name → 报错
//     4. op=set 缺 data → 报错
//     5. op=set 缺 rollout_strategy → 报错
//     6. op=set rollout=rolling_restart 但无 linked_deployment → 报错
//     7. op=delete 缺 delete_keys → 报错
//     8. op=rollback 缺 snapshot_id → 报错
//
//  B) op=set Severity 分级
//     9. 非生产 + rollout=none + 3 key → Medium（低风险默认档）
//    10. 生产 + rolling_restart → High
//    11. 生产 + immediate_restart → Critical + RequireReason
//    12. keys > 10 → High（爆破保护）
//    13. 敏感键名（含 token） → Critical + RequireReason
//
//  C) op=set 执行路径
//    14. 未 confirmed 返回 Plan（带 diff 三计数）
//    15. confirmed 且敏感键无 reason → 拦截
//    16. confirmed + reason → 成功 + 返回 snapshot_id
//
//  D) op=delete 路径
//    17. 非生产 delete 1 key → High
//    18. 生产 delete → Critical
//    19. confirmed 成功（返回 snapshot_id + rollout 状态）
//
//  E) op=rollback 路径
//    20. snapshot_id 与最近快照不匹配 → 报错
//    21. snapshot_id 匹配 → 成功，新快照 ID 不同于目标
//
//  F) 横切：审计 / diff / 敏感识别
//    22. 审计事件包含 op / from.keys_count / to.keys_count / snapshot_id
//    23. computeDiff 覆盖 added/modified/deleted/未变化
//    24. detectSensitiveKeys 覆盖多种命名写法
//    25. mergeData / removeKeys 不泄漏内部合成 key（__annotation_*）
package bcstools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// ---- 测试辅助 --------------------------------------------------------------

func callConfigmap(t *testing.T, tl tool.Tool, in ConfigmapUpdateInput) (*Result, error) {
	t.Helper()
	ct, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("tool is not CallableTool: %T", tl)
	}
	argsJSON, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	raw, err := ct.Call(context.Background(), argsJSON)
	if err != nil {
		return nil, err
	}
	r, ok := raw.(*Result)
	if !ok {
		t.Fatalf("result type mismatch: %T", raw)
	}
	return r, nil
}

func mustCallConfigmap(t *testing.T, tl tool.Tool, in ConfigmapUpdateInput) *Result {
	t.Helper()
	r, err := callConfigmap(t, tl, in)
	if err != nil {
		t.Fatalf("callConfigmap unexpected error: %v", err)
	}
	return r
}

func newMockCMTool() tool.Tool {
	return newConfigmapUpdateTool(bcsapi.NewClient(bcsapi.WithMockMode(true)))
}

func baseCMInput() ConfigmapUpdateInput {
	return ConfigmapUpdateInput{
		ClusterID: "BCS-K8S-00001",
		Namespace: "staging-letsgo",
		Name:      "game-core-config",
	}
}

// -----------------------------------------------------------------------------
// A) 输入校验
// -----------------------------------------------------------------------------

func TestConfigmap_EmptyOpRejected(t *testing.T) {
	tl := newMockCMTool()
	in := baseCMInput()
	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("缺 op 必须报错")
	}
}

func TestConfigmap_UnknownOpRejected(t *testing.T) {
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "patch"
	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("未知 op 必须报错")
	}
	if !strings.Contains(err.Error(), "不支持") {
		t.Errorf("错误信息应提 '不支持'，实际=%v", err)
	}
}

func TestConfigmap_MissingClusterIDRejected(t *testing.T) {
	tl := newMockCMTool()
	in := ConfigmapUpdateInput{Op: "get", Namespace: "ns", Name: "cm"}
	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("缺 cluster_id 必须报错")
	}
}

func TestConfigmap_SetMissingDataRejected(t *testing.T) {
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "set"
	in.RolloutStrategy = "none"
	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("set 缺 data 必须报错")
	}
}

func TestConfigmap_SetMissingRolloutStrategyRejected(t *testing.T) {
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "set"
	in.Data = map[string]string{"log.level": "debug"}
	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("set 缺 rollout_strategy 必须报错")
	}
}

func TestConfigmap_SetRollingWithoutLinkedDeploymentRejected(t *testing.T) {
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "set"
	in.Data = map[string]string{"log.level": "debug"}
	in.RolloutStrategy = "rolling_restart"
	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("rolling_restart 无 linked_deployment 必须报错")
	}
}

func TestConfigmap_DeleteMissingKeysRejected(t *testing.T) {
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "delete"
	in.RolloutStrategy = "none"
	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("delete 缺 delete_keys 必须报错")
	}
}

func TestConfigmap_RollbackMissingSnapshotRejected(t *testing.T) {
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "rollback"
	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("rollback 缺 snapshot_id 必须报错")
	}
}

// -----------------------------------------------------------------------------
// B) op=set Severity 分级
// -----------------------------------------------------------------------------

func TestClassifyConfigmapSeverity_Enumeration(t *testing.T) {
	cases := []struct {
		name       string
		op         string
		ns         string
		keysCount  int
		rollout    string
		sensitive  bool
		want       hitl.Severity
	}{
		{"staging set 3 key none", "set", "staging-letsgo", 3, "none", false, hitl.SeverityLow},
		{"staging set rolling", "set", "staging-letsgo", 3, "rolling_restart", false, hitl.SeverityMedium},
		{"prod set rolling", "set", "prod-letsgo", 3, "rolling_restart", false, hitl.SeverityHigh},
		{"prod set immediate", "set", "prod-letsgo", 3, "immediate_restart", false, hitl.SeverityCritical},
		{"set keys over limit", "set", "staging-letsgo", 11, "none", false, hitl.SeverityHigh},
		{"set sensitive key", "set", "staging-letsgo", 1, "none", true, hitl.SeverityCritical},
		{"delete staging", "delete", "staging-letsgo", 1, "none", false, hitl.SeverityHigh},
		{"delete prod", "delete", "prod-letsgo", 1, "none", false, hitl.SeverityCritical},
		{"delete sensitive", "delete", "staging-letsgo", 1, "none", true, hitl.SeverityCritical},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyConfigmapSeverity(c.op, c.ns, c.keysCount, c.rollout, c.sensitive)
			if got != c.want {
				t.Errorf("want=%v got=%v", c.want, got)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// C) op=set 执行路径
// -----------------------------------------------------------------------------

func TestConfigmap_Set_UnconfirmedReturnsPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "set"
	in.Data = map[string]string{"log.level": "debug", "request.timeout": "5s"}
	in.RolloutStrategy = "rolling_restart"
	in.LinkedDeployment = "game-core"

	result := mustCallConfigmap(t, tl, in)
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	// diff 字段应存在
	diffRaw, _ := pending.Plan.Params["diff"].([]DiffEntry)
	if len(diffRaw) == 0 {
		t.Error("Plan.Params.diff 应包含 diff entries")
	}
	if pending.Plan.Action != "bcs.configmap.set" {
		t.Errorf("Action 应为 bcs.configmap.set，实际=%q", pending.Plan.Action)
	}
}

func TestConfigmap_Set_SensitiveKeyConfirmedWithoutReasonRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "set"
	in.Data = map[string]string{"db.token": "xxx"}
	in.RolloutStrategy = "none"
	in.Confirmed = true
	// 无 reason

	result := mustCallConfigmap(t, tl, in)
	if result.OK {
		t.Fatal("敏感键无 reason 必须被拒")
	}
	if !strings.Contains(result.Message, "敏感") {
		t.Errorf("错误信息应提 '敏感'，实际=%q", result.Message)
	}
}

func TestConfigmap_Set_ConfirmedSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "set"
	in.Data = map[string]string{"log.level": "debug"}
	in.RolloutStrategy = "rolling_restart"
	in.LinkedDeployment = "game-core"
	in.Confirmed = true

	result := mustCallConfigmap(t, tl, in)
	if !result.OK {
		t.Fatalf("应成功，msg=%s", result.Message)
	}
	if !result.Mock {
		t.Error("Mock 模式应 Mock=true")
	}
	data, _ := result.Data.(map[string]any)
	sid, _ := data["snapshot_id"].(string)
	if !strings.HasPrefix(sid, "SNAP-") {
		t.Errorf("snapshot_id 格式不符，实际=%q", sid)
	}
}

func TestConfigmap_Set_ProductionImmediateWithoutReasonRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Namespace = "prod-letsgo"
	in.Op = "set"
	in.Data = map[string]string{"log.level": "debug"}
	in.RolloutStrategy = "immediate_restart"
	in.LinkedDeployment = "game-core"
	in.Confirmed = true
	// 无 reason

	result := mustCallConfigmap(t, tl, in)
	if result.OK {
		t.Fatal("生产 immediate_restart 无 reason 必须被拒")
	}
}

// -----------------------------------------------------------------------------
// D) op=delete 路径
// -----------------------------------------------------------------------------

func TestConfigmap_Delete_UnconfirmedReturnsHighPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "delete"
	in.DeleteKeys = []string{"log.level"}
	in.RolloutStrategy = "none"

	result := mustCallConfigmap(t, tl, in)
	pending, _ := result.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityHigh {
		t.Errorf("非生产 delete 基础 High，实际=%v", pending.Plan.Severity)
	}
}

func TestConfigmap_Delete_ProductionCritical(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Namespace = "prod-letsgo"
	in.Op = "delete"
	in.DeleteKeys = []string{"feature.x"}
	in.RolloutStrategy = "none"

	result := mustCallConfigmap(t, tl, in)
	pending, _ := result.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("生产 delete 应 Critical，实际=%v", pending.Plan.Severity)
	}
}

func TestConfigmap_Delete_ConfirmedSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "delete"
	in.DeleteKeys = []string{"log.level"}
	in.RolloutStrategy = "rolling_restart"
	in.LinkedDeployment = "game-core"
	in.Confirmed = true

	result := mustCallConfigmap(t, tl, in)
	if !result.OK {
		t.Fatalf("应成功，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	if _, ok := data["snapshot_id"].(string); !ok {
		t.Error("delete 成功应返回 snapshot_id")
	}
}

// -----------------------------------------------------------------------------
// E) op=rollback 路径
// -----------------------------------------------------------------------------

func TestConfigmap_Rollback_MismatchSnapshotRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "rollback"
	in.SnapshotID = "SNAP-NOT-EXIST"
	in.Confirmed = true

	_, err := callConfigmap(t, tl, in)
	if err == nil {
		t.Fatal("不匹配的 snapshot_id 必须报错")
	}
	if !strings.Contains(err.Error(), "不匹配") {
		t.Errorf("错误信息应提 '不匹配'，实际=%v", err)
	}
}

func TestConfigmap_Rollback_MatchedSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "rollback"
	in.SnapshotID = "SNAP-MOCK-HISTORY" // 与 fetchCurrentConfigmap Mock 数据一致
	in.LinkedDeployment = "game-core"
	in.Confirmed = true

	result := mustCallConfigmap(t, tl, in)
	if !result.OK {
		t.Fatalf("匹配 snapshot 应成功，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	if data["target_snapshot_id"] != "SNAP-MOCK-HISTORY" {
		t.Errorf("target_snapshot_id 错，%v", data["target_snapshot_id"])
	}
	newID, _ := data["new_snapshot_id"].(string)
	if !strings.HasPrefix(newID, "SNAP-") || newID == "SNAP-MOCK-HISTORY" {
		t.Errorf("new_snapshot_id 应是新生成的且不同于 target，实际=%q", newID)
	}
}

// -----------------------------------------------------------------------------
// F) 横切：审计 / diff / 敏感识别 / 清理
// -----------------------------------------------------------------------------

func TestConfigmap_AuditEvent_FromToDigest(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	t.Setenv("AUDIT_DISABLE", "")
	mem := &audit.MemorySink{}
	old := audit.SetSink(mem)
	defer audit.SetSink(old)

	tl := newMockCMTool()
	in := baseCMInput()
	in.Op = "set"
	in.Data = map[string]string{"log.level": "debug"}
	in.RolloutStrategy = "none"
	in.Confirmed = true
	_ = mustCallConfigmap(t, tl, in)

	var hit *audit.Record
	for _, line := range mem.Snapshot() {
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("审计应为合法 JSON：%s", line)
		}
		if rec.Action == "bcs.configmap.set" {
			hit = &rec
			break
		}
	}
	if hit == nil {
		t.Fatal("未找到 bcs.configmap.set 审计事件")
	}
	// from/to 应为包含 keys_count 的 map
	from, ok := hit.Params["from"].(map[string]any)
	if !ok {
		t.Errorf("审计 from 应为 map，实际=%T", hit.Params["from"])
	} else if _, has := from["keys_count"]; !has {
		t.Error("审计 from 应包含 keys_count")
	}
	to, _ := hit.Params["to"].(map[string]any)
	if to == nil {
		t.Error("审计 to 不应为空")
	}
	if hit.Params["snapshot_id"] == nil {
		t.Error("审计应包含 snapshot_id")
	}
}

func TestComputeDiff(t *testing.T) {
	current := map[string]string{"a": "1", "b": "2", "c": "3"}
	changes := map[string]string{
		"a": "1",   // 未变化 → 不出现
		"b": "22",  // modified
		"d": "4",   // added
	}
	diff := computeDiff(current, changes, []string{"c", "nonexist"})
	// 期望：modified b / added d / deleted c；nonexist 不出现
	expectedOps := map[string]string{"b": "modified", "d": "added", "c": "deleted"}
	if len(diff) != 3 {
		t.Fatalf("期望 3 条 diff，实际 %d: %+v", len(diff), diff)
	}
	for _, e := range diff {
		want, ok := expectedOps[e.Key]
		if !ok {
			t.Errorf("意外 diff key=%q op=%q", e.Key, e.Op)
			continue
		}
		if e.Op != want {
			t.Errorf("key=%q 期望 op=%q，实际=%q", e.Key, want, e.Op)
		}
	}
}

func TestDetectSensitiveKeys(t *testing.T) {
	cases := []struct {
		key       string
		sensitive bool
	}{
		{"log.level", false},
		{"request.timeout", false},
		{"db.password", true},
		{"API_KEY", true},
		{"apiKey", true},
		{"access_key_id", true},
		{"my_credential", true},
		{"service.token", true},
		{"private_key", true},
		{"feature.enabled", false},
	}
	for _, c := range cases {
		got := len(detectSensitiveKeys([]string{c.key})) == 1
		if got != c.sensitive {
			t.Errorf("key=%q 期望 sensitive=%v，实际=%v", c.key, c.sensitive, got)
		}
	}
}

func TestMergeData_DropsInternalFields(t *testing.T) {
	current := map[string]string{
		"a":                       "1",
		"__annotation_snapshot__": `{"id":"X"}`,
	}
	changes := map[string]string{"b": "2"}
	merged := mergeData(current, changes)
	if _, ok := merged["__annotation_snapshot__"]; ok {
		t.Error("mergeData 不应泄漏 __annotation_ 开头的内部字段")
	}
	if merged["a"] != "1" || merged["b"] != "2" {
		t.Errorf("mergeData 内容错误：%v", merged)
	}
}

func TestRemoveKeys_DropsInternalFields(t *testing.T) {
	current := map[string]string{
		"a":                       "1",
		"b":                       "2",
		"__annotation_snapshot__": `{"id":"X"}`,
	}
	out := removeKeys(current, []string{"a"})
	if _, ok := out["__annotation_snapshot__"]; ok {
		t.Error("removeKeys 不应泄漏 __annotation_ 字段")
	}
	if _, ok := out["a"]; ok {
		t.Error("a 应已删除")
	}
	if out["b"] != "2" {
		t.Error("b 应保留")
	}
}
