// Package bcstools — bcs_network_update 单元测试（D25）。
//
// 测试均在 Mock 模式跑离线，无外部依赖。
// 覆盖 14 个关键路径（kind 白名单 / 六 op / 三层防护 / Declaration）。
package bcstools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// callNetworkUpdate 辅助：Mock client + 调用工具 + 解析 Result。
func callNetworkUpdate(t *testing.T, payload string) (map[string]any, error) {
	t.Helper()
	t.Setenv("BCS_API_MOCK", "1")
	client := bcsapi.NewClient()
	if !client.IsMock() {
		t.Fatalf("Mock 环境变量未生效")
	}
	tl := newNetworkUpdateTool(client)
	ct, _ := tl.(tool.CallableTool)
	if ct == nil {
		t.Fatalf("newNetworkUpdateTool 应返回 CallableTool")
	}
	raw, err := ct.Call(context.Background(), []byte(payload))
	if err != nil {
		return nil, err
	}
	bs, _ := json.Marshal(raw)
	var r map[string]any
	if jErr := json.Unmarshal(bs, &r); jErr != nil {
		t.Fatalf("Result 反序列化失败: %v", jErr)
	}
	return r, nil
}

// --- 1. kind 白名单拒绝 ---

func TestNetworkUpdate_UnsupportedKind(t *testing.T) {
	_, err := callNetworkUpdate(t, `{
		"op":"get","cluster_id":"c","namespace":"ns","kind":"NetworkPolicy","name":"n"
	}`)
	if err == nil {
		t.Fatal("未支持的 kind 应报错")
	}
	if !strings.Contains(err.Error(), "R2") || !strings.Contains(err.Error(), "NetworkPolicy") {
		t.Errorf("错误应提及 R2 与 kind 名，实际 %q", err.Error())
	}
}

// --- 2. op=get Service Mock 样例 ---

func TestNetworkUpdate_GetServiceMock(t *testing.T) {
	r, err := callNetworkUpdate(t, `{
		"op":"get","cluster_id":"BCS-K8S-00001","namespace":"default","kind":"Service","name":"demo"
	}`)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if ok, _ := r["ok"].(bool); !ok {
		t.Fatalf("应 ok=true：%+v", r)
	}
	data := r["data"].(map[string]any)
	spec := data["spec"].(map[string]any)
	if spec["type"] != "ClusterIP" {
		t.Errorf("Service Mock 样例 spec.type 应为 ClusterIP，实际 %v", spec["type"])
	}
	if sel, _ := spec["selector"].(map[string]any); sel == nil || sel["app"] != "demo" {
		t.Errorf("Service Mock 样例应含 selector.app=demo，实际 %+v", spec["selector"])
	}
}

// --- 3. op=get Ingress Mock 样例 ---

func TestNetworkUpdate_GetIngressMock(t *testing.T) {
	r, err := callNetworkUpdate(t, `{
		"op":"get","cluster_id":"BCS-K8S-00001","namespace":"default","kind":"Ingress","name":"demo-ing"
	}`)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	data := r["data"].(map[string]any)
	spec := data["spec"].(map[string]any)
	if _, has := spec["rules"]; !has {
		t.Errorf("Ingress Mock 样例应含 rules 字段")
	}
	tls, _ := spec["tls"].([]any)
	if len(tls) == 0 {
		t.Errorf("Ingress Mock 样例应含 tls[] 条目")
	}
}

// --- 4. update_spec empty patch_spec ---

func TestNetworkUpdate_UpdateSpecEmpty(t *testing.T) {
	_, err := callNetworkUpdate(t, `{
		"op":"update_spec","cluster_id":"c","namespace":"default","kind":"Service","name":"demo",
		"patch_spec":{},"confirmed":true
	}`)
	if err == nil || !strings.Contains(err.Error(), "update_spec") {
		t.Fatalf("空 patch_spec 应报错，实际 %v", err)
	}
}

// --- 5. update_spec 含 metadata.name 被拒（R3）---

func TestNetworkUpdate_R3RejectMetadataName(t *testing.T) {
	// patch_spec 放进一个 name 字段，会被包进 spec.name —— R3 应拦截
	_, err := callNetworkUpdate(t, `{
		"op":"update_spec","cluster_id":"c","namespace":"default","kind":"Service","name":"demo",
		"patch_spec":{"name":"hijacked-name"},"confirmed":true
	}`)
	if err == nil {
		t.Fatal("patch_spec 含 spec.name 应被 R3 拒绝")
	}
	if !strings.Contains(err.Error(), "R3") {
		t.Errorf("错误应提及 R3，实际 %q", err.Error())
	}
}

// --- 6. set_selector 未 confirmed → HITL pending ---

func TestNetworkUpdate_SetSelectorPending(t *testing.T) {
	r, err := callNetworkUpdate(t, `{
		"op":"set_selector","cluster_id":"c","namespace":"default","kind":"Service","name":"demo",
		"selector":{"app":"new-app"}
	}`)
	if err != nil {
		t.Fatalf("未 confirmed 应返回 pending 而不是 err：%v", err)
	}
	if ok, _ := r["ok"].(bool); ok {
		t.Errorf("pending 状态应 ok=false，实际 %+v", r)
	}
	// Data 中应含 HITL 预案
	if data := r["data"]; data == nil {
		t.Error("pending 必须带 Plan data")
	}
}

// --- 7. set_selector confirmed → 成功 ---

func TestNetworkUpdate_SetSelectorConfirmed(t *testing.T) {
	r, err := callNetworkUpdate(t, `{
		"op":"set_selector","cluster_id":"c","namespace":"default","kind":"Service","name":"demo",
		"selector":{"app":"new-app"},"confirmed":true
	}`)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if ok, _ := r["ok"].(bool); !ok {
		t.Fatalf("confirmed 后应 ok=true：%+v", r)
	}
	if mock, _ := r["mock"].(bool); !mock {
		t.Errorf("Mock 模式应 mock=true")
	}
	data := r["data"].(map[string]any)
	if s, _ := data["patch_summary"].(string); !strings.Contains(s, "selector") {
		t.Errorf("patch_summary 应提及 selector，实际 %q", s)
	}
}

// --- 8. set_port 必填字段缺失 ---

func TestNetworkUpdate_SetPortMissingFields(t *testing.T) {
	_, err := callNetworkUpdate(t, `{
		"op":"set_port","cluster_id":"c","namespace":"default","kind":"Service","name":"demo",
		"port_name":"http","confirmed":true
	}`)
	if err == nil {
		t.Fatal("port_name 存在但 target_port/service_port 都缺应报错")
	}
	if !strings.Contains(err.Error(), "target_port") {
		t.Errorf("错误应提及 target_port，实际 %q", err.Error())
	}
}

// --- 9. set_backend Ingress 完整参数成功 ---

func TestNetworkUpdate_SetBackendSuccess(t *testing.T) {
	r, err := callNetworkUpdate(t, `{
		"op":"set_backend","cluster_id":"c","namespace":"default","kind":"Ingress","name":"demo-ing",
		"rule_host":"demo.example.com","rule_path":"/api",
		"backend_service":"new-svc","backend_port":8080,
		"confirmed":true
	}`)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if ok, _ := r["ok"].(bool); !ok {
		t.Fatalf("应成功：%+v", r)
	}
	data := r["data"].(map[string]any)
	summary, _ := data["patch_summary"].(string)
	if !strings.Contains(summary, "new-svc") || !strings.Contains(summary, "8080") {
		t.Errorf("summary 应含 new-svc:8080，实际 %q", summary)
	}
}

// --- 10. set_tls 必须提供 reason（Critical）---

func TestNetworkUpdate_SetTLSRequireReason(t *testing.T) {
	_, err := callNetworkUpdate(t, `{
		"op":"set_tls","cluster_id":"c","namespace":"default","kind":"Ingress","name":"demo-ing",
		"tls_host":"demo.example.com","tls_secret_name":"new-cert",
		"confirmed":true
	}`)
	if err == nil {
		t.Fatal("set_tls 未提供 reason 应被 Critical 拦截")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("错误应提及 reason，实际 %q", err.Error())
	}
}

// --- 11. set_tls 提供 reason 成功 ---

func TestNetworkUpdate_SetTLSWithReasonSuccess(t *testing.T) {
	r, err := callNetworkUpdate(t, `{
		"op":"set_tls","cluster_id":"c","namespace":"default","kind":"Ingress","name":"demo-ing",
		"tls_host":"demo.example.com","tls_secret_name":"new-cert",
		"reason":"旧证书即将过期","confirmed":true
	}`)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if ok, _ := r["ok"].(bool); !ok {
		t.Fatalf("应成功：%+v", r)
	}
}

// --- 12. prod ns 自动升 Critical（未提供 reason 被拦）---
//
// 注意：isProdNamespace 采用前缀匹配（prod-/prod_/production-/release-），
// 因此用 "prod-backend" 作为样本，"production" 单词本身不会被识别。

func TestNetworkUpdate_ProdNSRequireReason(t *testing.T) {
	_, err := callNetworkUpdate(t, `{
		"op":"set_selector","cluster_id":"c","namespace":"prod-backend","kind":"Service","name":"demo",
		"selector":{"app":"x"},"confirmed":true
	}`)
	if err == nil {
		t.Fatal("prod ns 的 set_selector 应被升 Critical + reason 拦截")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("prod ns 错误应提及 reason，实际 %q", err.Error())
	}
}

// --- 13. expected_resource_version 并发守护 ---

func TestNetworkUpdate_ResourceVersionConflict(t *testing.T) {
	// Mock before.ResourceVersion 固定为 "mock-rv-123"
	_, err := callNetworkUpdate(t, `{
		"op":"set_selector","cluster_id":"c","namespace":"default","kind":"Service","name":"demo",
		"selector":{"app":"x"},
		"expected_resource_version":"stale-rv-999",
		"confirmed":true
	}`)
	if err == nil {
		t.Fatal("expected_resource_version 不匹配应报错")
	}
	if !strings.Contains(err.Error(), "R5") || !strings.Contains(err.Error(), "resourceVersion") {
		t.Errorf("错误应提及 R5 与 resourceVersion，实际 %q", err.Error())
	}
}

// --- 14. Declaration 完整性 ---

func TestNetworkUpdate_Declaration(t *testing.T) {
	client := bcsapi.NewClient()
	tl := newNetworkUpdateTool(client)
	decl := tl.Declaration()
	if decl.Name != "bcs_network_update" {
		t.Errorf("Name 应为 bcs_network_update，实际 %q", decl.Name)
	}
	for _, kw := range []string{"Service", "Ingress", "set_tls", "并发守护"} {
		if !strings.Contains(decl.Description, kw) {
			t.Errorf("Description 应包含 %q", kw)
		}
	}
	if decl.InputSchema == nil {
		t.Errorf("InputSchema 不应为 nil")
	}
}

// --- 15. normalizeNetworkKind 小单元 ---

func TestNormalizeNetworkKind(t *testing.T) {
	cases := map[string]string{
		"service":   "Service",
		"Services":  "Service",
		"SVC":       "Service",
		"ingress":   "Ingress",
		"INGRESSES": "Ingress",
		"ing":       "Ingress",
		"Pod":       "Pod", // 非网络 kind 保持原样（会被白名单拦）
	}
	for in, want := range cases {
		if got := normalizeNetworkKind(in); got != want {
			t.Errorf("normalizeNetworkKind(%q) = %q, want %q", in, got, want)
		}
	}
}
