// metrics_middleware_test.go —— D28 WithMetrics 装饰器单元测试。
//
// # 覆盖矩阵
//
//   A) 基础透传
//     1. Declaration() 透传内层工具的 Name/Description
//     2. 成功调用 → status=ok，耗时 > 0
//
//   B) HITL 漏斗识别
//     3. 返回含 "awaiting_confirmation" 的结果 → isPendingResult=true
//     4. 返回普通 Result → isPendingResult=false
//     5. confirmed=true 在 argsJSON 里 → extractConfirmed=true
//     6. confirmed=false / 缺失 → extractConfirmed=false
//
//   C) 拒绝原因提取
//     7. error 含 "R3" → r3_primary_key
//     8. error 含 "R5" → r5_rv_conflict
//     9. error 含 "reason" + "critical" → critical_noreason
//    10. error 含 "hard limit" → hard_limit_exceeded
//    11. error 含 "HPA" + "block" → hpa_conflict_block
//    12. error 含 "immutable" → immutable_resource
//    13. error 含 "TLS" + "cert" → tls_cert_mismatch
//    14. 其他 error → unknown
//
//   D) 入参异常提取
//    15. error 含 "missing" + "required" → missing_required
//    16. error 含 "must not be empty" → empty_required
//    17. error 含 "invalid" + "type" → wrong_type
//    18. error 含 "unknown field" → unknown_field
//    19. 规则拒绝类 error（R3/R5）→ 不打 anomaly（返回空字符串）
//
//   E) 装配层验证
//    20. wrapMetrics 后工具数量不变
//    21. wrapMetrics 后每个工具的 Declaration().Name 不变
//    22. wrapMetrics 后工具仍实现 tool.CallableTool
package bcstools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ---- 测试辅助 ---------------------------------------------------------------

// fakeCallableTool 是一个可配置返回值的 CallableTool stub。
type fakeCallableTool struct {
	name   string
	result any
	err    error
}

func (f *fakeCallableTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: f.name, Description: "fake tool for testing"}
}

func (f *fakeCallableTool) Call(_ context.Context, _ []byte) (any, error) {
	return f.result, f.err
}

// pendingJSON 构造一个含 "awaiting_confirmation" 的 JSON 字符串（模拟 HITL Plan 响应）。
func pendingJSON() any {
	return map[string]any{
		"ok":     false,
		"status": "awaiting_confirmation",
		"plan":   map[string]any{"action": "bcs.scale.deployment"},
	}
}

// ---- A) 基础透传 -------------------------------------------------------------

func TestMetricsMiddleware_DeclarationPassthrough(t *testing.T) {
	inner := &fakeCallableTool{name: "bcs_test_tool"}
	wrapped := WithMetrics(inner, "bcs_test_tool")
	if wrapped.Declaration().Name != "bcs_test_tool" {
		t.Errorf("Declaration().Name 应透传，实际 %q", wrapped.Declaration().Name)
	}
	if wrapped.Declaration().Description != "fake tool for testing" {
		t.Errorf("Declaration().Description 应透传，实际 %q", wrapped.Declaration().Description)
	}
}

func TestMetricsMiddleware_SuccessCallPassthrough(t *testing.T) {
	expected := map[string]any{"ok": true, "data": "hello"}
	inner := &fakeCallableTool{name: "bcs_test_tool", result: expected}
	wrapped := WithMetrics(inner, "bcs_test_tool")

	result, err := wrapped.Call(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("成功调用不应返回 error：%v", err)
	}
	got, _ := result.(map[string]any)
	if got["ok"] != true {
		t.Errorf("结果应透传 ok=true，实际 %+v", got)
	}
}

// ---- B) HITL 漏斗识别 --------------------------------------------------------

func TestIsPendingResult_WithPendingJSON(t *testing.T) {
	if !isPendingResult(pendingJSON()) {
		t.Error("含 awaiting_confirmation 的结果应被识别为 pending")
	}
}

func TestIsPendingResult_WithNormalResult(t *testing.T) {
	normal := &Result{OK: true, Data: "some data"}
	if isPendingResult(normal) {
		t.Error("普通 Result 不应被识别为 pending")
	}
}

func TestIsPendingResult_WithNil(t *testing.T) {
	if isPendingResult(nil) {
		t.Error("nil 不应被识别为 pending")
	}
}

func TestExtractConfirmed_True(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"confirmed": true, "op": "set_selector"})
	if !extractConfirmed(args) {
		t.Error("confirmed=true 应被识别")
	}
}

func TestExtractConfirmed_False(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"confirmed": false})
	if extractConfirmed(args) {
		t.Error("confirmed=false 不应被识别为 true")
	}
}

func TestExtractConfirmed_Missing(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"op": "get"})
	if extractConfirmed(args) {
		t.Error("缺少 confirmed 字段不应被识别为 true")
	}
}

func TestExtractConfirmed_EmptyJSON(t *testing.T) {
	if extractConfirmed(nil) {
		t.Error("nil argsJSON 不应被识别为 confirmed=true")
	}
}

// ---- C) 拒绝原因提取 ---------------------------------------------------------

func TestExtractRejectReason(t *testing.T) {
	cases := []struct {
		errMsg string
		want   string
	}{
		{"R3: patch_spec 含主键 spec.name 被拒", "r3_primary_key"},
		{"R5: expected_resource_version 不匹配", "r5_rv_conflict"},
		{"Critical 操作必须提供 reason", "critical_noreason"},
		{"超出 hard limit 限制", "hard_limit_exceeded"},
		{"HPA block 策略拒绝", "hpa_conflict_block"},
		{"immutable Secret 不可修改", "immutable_resource"},
		{"TLS cert 与 key 不匹配", "tls_cert_mismatch"},
		{"BCS API 返回 500", "unknown"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		got := extractRejectReason(tc.errMsg)
		if got != tc.want {
			t.Errorf("extractRejectReason(%q) = %q，期望 %q", tc.errMsg, got, tc.want)
		}
	}
}

// ---- D) 入参异常提取 ---------------------------------------------------------

func TestExtractInputAnomaly(t *testing.T) {
	cases := []struct {
		errMsg string
		want   string
	}{
		{"missing required field: cluster_id", "missing_required"},
		{"cluster_id must not be empty", "empty_required"},
		{"invalid type: expected int, got string", "wrong_type"},
		{"unknown field: foo_bar", "unknown_field"},
		// 规则拒绝类不应打 anomaly
		{"R3: 主键保护", ""},
		{"R5: resourceVersion 冲突", ""},
		{"BCS API 500", ""},
	}
	for _, tc := range cases {
		got := extractInputAnomaly(tc.errMsg)
		if got != tc.want {
			t.Errorf("extractInputAnomaly(%q) = %q，期望 %q", tc.errMsg, got, tc.want)
		}
	}
}

// ---- E) 装配层验证 -----------------------------------------------------------

func TestWrapMetrics_ToolCountUnchanged(t *testing.T) {
	client := bcsapi.NewClient(bcsapi.WithMockMode(true))
	before := NewAllTargeted(client)
	// wrapMetrics 已在 NewAllTargeted 内部调用，这里验证工具数量不变
	if len(before) != 13 {
		t.Errorf("NewAllTargeted 应返回 13 个工具，实际 %d", len(before))
	}
}

func TestWrapMetrics_NamesPreserved(t *testing.T) {
	client := bcsapi.NewClient(bcsapi.WithMockMode(true))
	all := NewAllTargeted(client)
	expectedNames := map[string]bool{
		"bcs_project_query":   true,
		"bcs_cluster_query":   true,
		"bcs_resource_query":  true,
		"bcs_pod_logs_tail":   true,
		"bcs_pod_describe":    true,
		"bcs_node_describe":   true,
		"bcs_helm_manage":     true,
		"bcs_scale_deployment": true,
		"bcs_pod_restart":     true,
		"bcs_configmap_update": true,
		"bcs_secret_update":   true,
		"bcs_hpa_patch":       true,
		"bcs_network_update":  true,
	}
	for _, tt := range all {
		ct, ok := tt.Tool.(tool.CallableTool)
		if !ok {
			t.Errorf("工具 %T 应实现 CallableTool", tt.Tool)
			continue
		}
		name := ct.Declaration().Name
		if !expectedNames[name] {
			t.Errorf("意外的工具名 %q", name)
		}
		delete(expectedNames, name)
	}
	for name := range expectedNames {
		t.Errorf("工具 %q 在 NewAllTargeted 结果中缺失", name)
	}
}

func TestWrapMetrics_AllImplementCallableTool(t *testing.T) {
	client := bcsapi.NewClient(bcsapi.WithMockMode(true))
	all := NewAllTargeted(client)
	for _, tt := range all {
		if _, ok := tt.Tool.(tool.CallableTool); !ok {
			t.Errorf("工具 %T 经 wrapMetrics 后应仍实现 tool.CallableTool", tt.Tool)
		}
	}
}

// TestMetricsMiddleware_ErrorCallEmitsReject 验证 error 路径下中间件不 panic
// （指标实际是否写入需要 OTel ManualReader，这里只验证不崩溃）。
func TestMetricsMiddleware_ErrorCallEmitsReject(t *testing.T) {
	inner := &fakeCallableTool{
		name: "bcs_test_tool",
		err:  errors.New("R5: expected_resource_version 不匹配"),
	}
	wrapped := WithMetrics(inner, "bcs_test_tool")
	_, err := wrapped.Call(context.Background(), []byte(`{"confirmed":true}`))
	if err == nil {
		t.Fatal("应透传 inner 的 error")
	}
	if err.Error() != "R5: expected_resource_version 不匹配" {
		t.Errorf("error 应透传，实际 %q", err.Error())
	}
}

// TestMetricsMiddleware_PendingCallEmitsHITLPlan 验证 HITL Plan 路径下中间件不 panic。
func TestMetricsMiddleware_PendingCallEmitsHITLPlan(t *testing.T) {
	inner := &fakeCallableTool{
		name:   "bcs_scale_deployment",
		result: pendingJSON(),
	}
	wrapped := WithMetrics(inner, "bcs_scale_deployment")
	result, err := wrapped.Call(context.Background(), []byte(`{"op":"scale_absolute","replicas":3}`))
	if err != nil {
		t.Fatalf("HITL Plan 路径不应返回 error：%v", err)
	}
	// 结果应透传（仍含 awaiting_confirmation）
	bs, _ := json.Marshal(result)
	if !isPendingResult(result) {
		t.Errorf("HITL Plan 结果应透传，实际 %s", bs)
	}
}
