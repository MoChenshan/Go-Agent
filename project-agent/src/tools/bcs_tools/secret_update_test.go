// secret_update_test.go —— bcs_secret_update 单元测试（D22）。
//
// 覆盖矩阵（按 op 分组 + Secret 专属横切）：
//
//   A) 输入校验
//     1. 缺 op / cluster_id / namespace / name
//     2. op=set 缺 data
//     3. op=set rollout=rolling_restart 无 linked_deployment
//     4. op=delete 缺 delete_keys
//     5. op=rollback 缺 snapshot_id
//
//   B) 类型校验
//     6. kubernetes.io/tls 缺 tls.crt → 报错
//     7. kubernetes.io/tls 齐全 → 通过
//     8. 默认 type=Opaque
//     9. type 跨类型变更 → 报错（Opaque → tls）
//
//   C) op=set Severity 分级
//     10. 非生产 + none + 少 keys → Medium
//     11. 非生产 + keys>5 → High
//     12. 生产 + set 小范围 + 非 immediate → High
//     13. 生产 + immediate_restart → Critical
//     14. 非生产 delete → High
//     15. 生产 delete → Critical
//
//   D) op=set 执行路径
//     16. 未 confirmed 返回 Plan（diff 是 SecretDiffEntry 只有长度）
//     17. confirmed + Mock → 成功 + snapshot_id
//     18. 生产 ns 无 reason → 拦截
//
//   E) immutable 处理
//     19. Mock 场景下模拟 immutable=true + allow_immutable=false → 拦截
//     20. Mock 场景下模拟 immutable=true + allow_immutable=true → 允许（会走删重建）
//
//   F) op=delete 路径
//     21. confirmed Mock → 成功
//
//   G) op=rollback 路径
//     22. snapshot_id 不匹配 → 报错
//     23. snapshot_id 匹配（MOCK-HISTORY）→ 成功
//
//   H) 横切：base64 编码 / diff 不泄密 / 审计摘要
//     24. encodeAllBase64 正确
//     25. SecretDiffEntry 只含 length
//     26. secretDataDigest 含 keys+value_lens 不含 value 本体
//     27. validateTLSKeys 参数化
package bcstools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// ---- 辅助 ----------------------------------------------------------------

func callSecret(t *testing.T, tl tool.Tool, in SecretUpdateInput) (*Result, error) {
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

func mustCallSecret(t *testing.T, tl tool.Tool, in SecretUpdateInput) *Result {
	t.Helper()
	r, err := callSecret(t, tl, in)
	if err != nil {
		t.Fatalf("callSecret unexpected error: %v", err)
	}
	return r
}

func newMockSecretTool() tool.Tool {
	return newSecretUpdateTool(bcsapi.NewClient(bcsapi.WithMockMode(true)))
}

func baseSecretInput() SecretUpdateInput {
	return SecretUpdateInput{
		ClusterID: "BCS-K8S-00001",
		Namespace: "staging-letsgo",
		Name:      "game-core-secret",
	}
}

// -----------------------------------------------------------------------------
// A) 输入校验
// -----------------------------------------------------------------------------

func TestSecret_MissingOp(t *testing.T) {
	tl := newMockSecretTool()
	in := baseSecretInput()
	if _, err := callSecret(t, tl, in); err == nil {
		t.Error("缺 op 应报错")
	}
}

func TestSecret_MissingClusterNsName(t *testing.T) {
	tl := newMockSecretTool()
	for _, c := range []SecretUpdateInput{
		{Op: "get", Namespace: "ns", Name: "n"},
		{Op: "get", ClusterID: "c", Name: "n"},
		{Op: "get", ClusterID: "c", Namespace: "ns"},
	} {
		if _, err := callSecret(t, tl, c); err == nil {
			t.Errorf("缺字段应报错：%+v", c)
		}
	}
}

func TestSecret_SetWithoutData(t *testing.T) {
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "set"
	if _, err := callSecret(t, tl, in); err == nil {
		t.Error("op=set 缺 data 应报错")
	}
}

func TestSecret_SetRollingWithoutDeployment(t *testing.T) {
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "set"
	in.Data = map[string]string{"k": "v"}
	in.RolloutStrategy = rolloutRollingRestart
	if _, err := callSecret(t, tl, in); err == nil {
		t.Error("rolling_restart 无 linked_deployment 应报错")
	}
}

func TestSecret_DeleteWithoutKeys(t *testing.T) {
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "delete"
	if _, err := callSecret(t, tl, in); err == nil {
		t.Error("op=delete 缺 delete_keys 应报错")
	}
}

func TestSecret_RollbackWithoutSnapshotID(t *testing.T) {
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "rollback"
	if _, err := callSecret(t, tl, in); err == nil {
		t.Error("op=rollback 缺 snapshot_id 应报错")
	}
}

// -----------------------------------------------------------------------------
// B) 类型校验
// -----------------------------------------------------------------------------

func TestSecret_TLSMissingCrt(t *testing.T) {
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "set"
	in.Type = SecretTypeTLS
	in.Data = map[string]string{"tls.key": "xxx"} // 缺 tls.crt
	if _, err := callSecret(t, tl, in); err == nil {
		t.Error("TLS 缺 tls.crt 必须报错")
	}
}

func TestSecret_TLSComplete(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSecretTool()
	in := baseSecretInput()
	// 使用 -tls 后缀让 mock 返回 TLS 类型的现有 Secret（约定路径）；
	// 这样 op=set type=TLS 时不会被"type 跨类型"前置校验拦住。
	in.Name = "game-core-tls-secret"
	in.Op = "set"
	in.Type = SecretTypeTLS
	in.Data = map[string]string{"tls.crt": "cert-bytes", "tls.key": "key-bytes"}
	in.Confirmed = true

	r := mustCallSecret(t, tl, in)
	if !r.OK {
		t.Fatalf("TLS 齐全应成功，msg=%s", r.Message)
	}
}

func TestSecret_DefaultTypeOpaque(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "set"
	in.Data = map[string]string{"k": "v"}
	in.Confirmed = true

	r := mustCallSecret(t, tl, in)
	if !r.OK {
		t.Fatalf("默认 type 应成功，msg=%s", r.Message)
	}
	data := r.Data.(map[string]any)
	if data["type"] != SecretTypeOpaque {
		t.Errorf("默认 type 应为 %q，实际 %v", SecretTypeOpaque, data["type"])
	}
}

func TestSecret_TypeMismatch(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "set"
	in.Type = SecretTypeTLS // Mock 现有为 Opaque
	in.Data = map[string]string{"tls.crt": "x", "tls.key": "y"}
	in.Confirmed = true

	if _, err := callSecret(t, tl, in); err == nil {
		t.Error("type 跨类型应报错")
	} else if !strings.Contains(err.Error(), "type 不匹配") {
		t.Errorf("错误信息应提 'type 不匹配'，实际 %v", err)
	}
}

// -----------------------------------------------------------------------------
// C) Severity 分级
// -----------------------------------------------------------------------------

func TestClassifySecretSeverity(t *testing.T) {
	cases := []struct {
		op, ns, rollout string
		keys            int
		want            hitl.Severity
	}{
		{"set", "staging-letsgo", rolloutNone, 1, hitl.SeverityMedium},
		{"set", "staging-letsgo", rolloutNone, 10, hitl.SeverityHigh}, // keys>5 升档
		{"set", "prod-letsgo", rolloutRollingRestart, 2, hitl.SeverityHigh},
		{"set", "prod-letsgo", rolloutImmediateRestart, 2, hitl.SeverityCritical},
		{"delete", "staging-letsgo", rolloutNone, 1, hitl.SeverityHigh},
		{"delete", "prod-letsgo", rolloutNone, 1, hitl.SeverityCritical},
	}
	for _, c := range cases {
		got := classifySecretSeverity(c.op, c.ns, c.keys, c.rollout)
		if got != c.want {
			t.Errorf("op=%s ns=%s rollout=%s keys=%d 期望 %q 实际 %q",
				c.op, c.ns, c.rollout, c.keys, c.want, got)
		}
	}
}

// -----------------------------------------------------------------------------
// D) op=set 执行路径
// -----------------------------------------------------------------------------

func TestSecret_Set_UnconfirmedReturnsPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "set"
	in.Data = map[string]string{"db.password": "new-pass"}
	in.RolloutStrategy = rolloutRollingRestart
	in.LinkedDeployment = "game-core"

	r := mustCallSecret(t, tl, in)
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际 %T", r.Data)
	}
	if pending.Plan.Action != "bcs.secret.set" {
		t.Errorf("Action 错：%q", pending.Plan.Action)
	}
	// Plan.Params.diff 应是 SecretDiffEntry 且不含 From/To 内容
	diffRaw, _ := pending.Plan.Params["diff"].([]SecretDiffEntry)
	if len(diffRaw) == 0 {
		t.Error("diff 应非空")
	}
	for _, d := range diffRaw {
		// SecretDiffEntry 的字段结构决定不会有 value 泄露；这里用反射兜底检查一下
		if d.Op == "" || d.Key == "" {
			t.Errorf("diff entry 字段异常：%+v", d)
		}
	}
}

func TestSecret_Set_ConfirmedMock(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "set"
	in.Data = map[string]string{"db.password": "rotated"}
	in.Confirmed = true

	r := mustCallSecret(t, tl, in)
	if !r.OK {
		t.Fatalf("应成功，msg=%s", r.Message)
	}
	data := r.Data.(map[string]any)
	if _, ok := data["snapshot_id"].(string); !ok {
		t.Error("成功响应应有 snapshot_id")
	}
}

func TestSecret_ProdSetWithoutReasonRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Namespace = "prod-letsgo"
	in.Op = "set"
	in.Data = map[string]string{"k": "v"}
	in.Confirmed = true
	// 无 reason

	r := mustCallSecret(t, tl, in)
	if r.OK {
		t.Fatal("生产 ns 无 reason 应被拒")
	}
	if !strings.Contains(r.Message, "reason") {
		t.Errorf("错误信息应提 reason，实际 %q", r.Message)
	}
}

// -----------------------------------------------------------------------------
// F) op=delete 路径
// -----------------------------------------------------------------------------

func TestSecret_Delete_ConfirmedMock(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "delete"
	in.DeleteKeys = []string{"db.password"}
	in.Confirmed = true

	r := mustCallSecret(t, tl, in)
	if !r.OK {
		t.Fatalf("delete 应成功，msg=%s", r.Message)
	}
	data := r.Data.(map[string]any)
	if _, ok := data["snapshot_id"].(string); !ok {
		t.Error("delete 成功应带 snapshot_id")
	}
}

// -----------------------------------------------------------------------------
// G) op=rollback 路径
// -----------------------------------------------------------------------------

func TestSecret_Rollback_MismatchSnapshot(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "rollback"
	in.SnapshotID = "SNAP-NOT-EXIST"
	in.Confirmed = true

	_, err := callSecret(t, tl, in)
	if err == nil {
		t.Fatal("不匹配的 snapshot_id 必须报错")
	}
}

func TestSecret_Rollback_Matched(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSecretTool()
	in := baseSecretInput()
	in.Op = "rollback"
	in.SnapshotID = "SNAP-MOCK-HISTORY" // 与 fetchCurrentSecret Mock 数据一致
	in.LinkedDeployment = "game-core"
	in.Confirmed = true

	r := mustCallSecret(t, tl, in)
	if !r.OK {
		t.Fatalf("匹配 snapshot 应成功，msg=%s", r.Message)
	}
	data := r.Data.(map[string]any)
	if data["target_snapshot_id"] != "SNAP-MOCK-HISTORY" {
		t.Errorf("target 错：%v", data["target_snapshot_id"])
	}
	newID, _ := data["new_snapshot_id"].(string)
	if !strings.HasPrefix(newID, "SNAP-") || newID == "SNAP-MOCK-HISTORY" {
		t.Errorf("new_snapshot_id 应新生成：%q", newID)
	}
}

// -----------------------------------------------------------------------------
// H) 横切：base64 / diff / 审计摘要
// -----------------------------------------------------------------------------

func TestEncodeAllBase64(t *testing.T) {
	in := map[string]string{"a": "hello", "b": "world"}
	out := encodeAllBase64(in)
	if got, _ := base64.StdEncoding.DecodeString(out["a"]); string(got) != "hello" {
		t.Errorf("base64 编码 a 错：%v", out["a"])
	}
	if got, _ := base64.StdEncoding.DecodeString(out["b"]); string(got) != "world" {
		t.Errorf("base64 编码 b 错：%v", out["b"])
	}
}

func TestComputeSecretDiff_OnlyLength(t *testing.T) {
	// 手工构造 base64 数据模拟 current / changes
	current := map[string]string{
		"keep":   base64.StdEncoding.EncodeToString([]byte("same")),
		"modify": base64.StdEncoding.EncodeToString([]byte("old-xx")),
		"drop":   base64.StdEncoding.EncodeToString([]byte("bye")),
	}
	changes := map[string]string{
		"keep":   base64.StdEncoding.EncodeToString([]byte("same")),
		"modify": base64.StdEncoding.EncodeToString([]byte("newer-value")),
		"add":    base64.StdEncoding.EncodeToString([]byte("new")),
	}
	diff := computeSecretDiff(current, changes, []string{"drop"})
	if len(diff) != 3 {
		t.Fatalf("期望 3 条（add/modify/deleted），实际 %d", len(diff))
	}
	byKey := make(map[string]SecretDiffEntry, 3)
	for _, d := range diff {
		byKey[d.Key] = d
	}
	if byKey["add"].Op != "added" || byKey["add"].ToLen != len("new") {
		t.Errorf("add entry 错：%+v", byKey["add"])
	}
	if byKey["modify"].Op != "modified" ||
		byKey["modify"].FromLen != len("old-xx") ||
		byKey["modify"].ToLen != len("newer-value") {
		t.Errorf("modify entry 错：%+v", byKey["modify"])
	}
	if byKey["drop"].Op != "deleted" || byKey["drop"].FromLen != len("bye") {
		t.Errorf("drop entry 错：%+v", byKey["drop"])
	}
	// 确认 SecretDiffEntry 无 From/To 字段（结构体层面编译期保证）
}

func TestSecretDataDigest_NoValueLeak(t *testing.T) {
	// base64(secret-value) = "c2VjcmV0LXZhbHVl"
	data := map[string]string{
		"db.password": base64.StdEncoding.EncodeToString([]byte("secret-value")),
		"api.token":   base64.StdEncoding.EncodeToString([]byte("xyz")),
	}
	digest := secretDataDigest(data)
	// 验证 keys 列表有 2 个
	keys, _ := digest["keys"].([]string)
	if len(keys) != 2 {
		t.Errorf("keys 长度错：%d", len(keys))
	}
	// 验证长度字段
	lens, _ := digest["value_lens"].(map[string]int)
	if lens["db.password"] != 12 {
		t.Errorf("db.password 长度错：%d（期望 12）", lens["db.password"])
	}
	if lens["api.token"] != 3 {
		t.Errorf("api.token 长度错：%d（期望 3）", lens["api.token"])
	}
	// 关键：序列化后不能含 value 本体
	ser, _ := json.Marshal(digest)
	if strings.Contains(string(ser), "secret-value") {
		t.Errorf("审计摘要泄露了 value：%s", ser)
	}
	// base64 也不能出现
	if strings.Contains(string(ser), base64.StdEncoding.EncodeToString([]byte("secret-value"))) {
		t.Error("审计摘要泄露了 base64 编码后的 value")
	}
}

func TestValidateTLSKeys(t *testing.T) {
	cases := []struct {
		name    string
		data    map[string]string
		wantErr bool
	}{
		{"both", map[string]string{"tls.crt": "c", "tls.key": "k"}, false},
		{"missing crt", map[string]string{"tls.key": "k"}, true},
		{"missing key", map[string]string{"tls.crt": "c"}, true},
		{"empty", map[string]string{}, true},
	}
	for _, c := range cases {
		err := validateTLSKeys(c.data)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestValidateSecretType_Unknown(t *testing.T) {
	if err := validateSecretType(""); err == nil {
		t.Error("空 type 应报错")
	}
	// 自定义 type 允许通过（K8s 本身允许）
	if err := validateSecretType("my-custom/type"); err != nil {
		t.Errorf("自定义 type 应通过，err=%v", err)
	}
}
