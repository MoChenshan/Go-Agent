// hpa_detect_test.go D20 —— HPA 检测与策略的纯数据加工单测。
//
// 本文件不涉及 httptest.Server，专注于验证：
//   - pickHPAForDeployment 解析 BCS storage 各种边界 JSON
//   - HPAInfo.InRange 的零值行为
//   - NormalizeHPAPolicy 的输入规整
//
// 与 scale 写路径的 E2E 测试分离：让 HPA 能力成为"纯函数可测单元"。
package bcstools

import (
	"context"
	"encoding/json"
	"testing"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ---- pickHPAForDeployment --------------------------------------------------

// hpaBody 返回 BCS storage 格式的 HPA 列表响应（items 可变）。
func hpaBody(items ...map[string]any) map[string]any {
	data := make([]any, 0, len(items))
	for _, it := range items {
		data = append(data, map[string]any{"data": it})
	}
	return map[string]any{"data": data}
}

// makeHPA 按 K8s v2 HPA 的关键字段生成 raw 对象。
func makeHPA(name, targetKind, targetName string, min, max, desired int) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"name": name},
		"spec": map[string]any{
			"minReplicas":    float64(min),
			"maxReplicas":    float64(max),
			"scaleTargetRef": map[string]any{"kind": targetKind, "name": targetName},
		},
		"status": map[string]any{
			"desiredReplicas": float64(desired),
		},
	}
}

func TestPickHPAForDeployment_NoData(t *testing.T) {
	// 空 data 数组应返回 Found=false
	info := pickHPAForDeployment(hpaBody(), "any-deploy")
	if info.Found {
		t.Fatalf("空数据应返回 Found=false，实际 %+v", info)
	}
}

func TestPickHPAForDeployment_NoMatch(t *testing.T) {
	// 同 ns 有 HPA 但 target 不匹配
	body := hpaBody(
		makeHPA("hpa-a", "Deployment", "other-deploy", 2, 10, 3),
		makeHPA("hpa-b", "Deployment", "yet-another", 1, 5, 2),
	)
	info := pickHPAForDeployment(body, "my-deploy")
	if info.Found {
		t.Fatalf("无匹配应返回 Found=false，实际 %+v", info)
	}
}

func TestPickHPAForDeployment_Match(t *testing.T) {
	// 精确匹配 Deployment
	body := hpaBody(
		makeHPA("hpa-a", "Deployment", "other", 2, 10, 3),
		makeHPA("hpa-target", "Deployment", "my-deploy", 3, 20, 5),
	)
	info := pickHPAForDeployment(body, "my-deploy")
	if !info.Found {
		t.Fatal("应匹配到 hpa-target")
	}
	if info.Name != "hpa-target" {
		t.Errorf("Name 应为 hpa-target，实际 %q", info.Name)
	}
	if info.MinReplicas != 3 || info.MaxReplicas != 20 {
		t.Errorf("[min,max] 应为 [3,20]，实际 [%d,%d]", info.MinReplicas, info.MaxReplicas)
	}
	if info.CurrentSpec != 5 {
		t.Errorf("CurrentSpec 应为 5，实际 %d", info.CurrentSpec)
	}
	if info.Raw == nil {
		t.Error("Raw 应保留原始对象供审计")
	}
}

func TestPickHPAForDeployment_MinReplicasDefault(t *testing.T) {
	// HPA 允许省略 minReplicas：应默认 1
	raw := map[string]any{
		"metadata": map[string]any{"name": "no-min"},
		"spec": map[string]any{
			"maxReplicas":    float64(10),
			"scaleTargetRef": map[string]any{"kind": "Deployment", "name": "d"},
		},
	}
	info := pickHPAForDeployment(hpaBody(raw), "d")
	if info.MinReplicas != 1 {
		t.Errorf("缺 minReplicas 应默认 1，实际 %d", info.MinReplicas)
	}
}

func TestPickHPAForDeployment_WrongKindIgnored(t *testing.T) {
	// scaleTargetRef.kind=StatefulSet 应被跳过
	body := hpaBody(
		makeHPA("ss-hpa", "StatefulSet", "my-deploy", 1, 5, 2),
		makeHPA("dp-hpa", "Deployment", "my-deploy", 2, 10, 3),
	)
	info := pickHPAForDeployment(body, "my-deploy")
	if info.Name != "dp-hpa" {
		t.Errorf("应选中 Deployment kind 的 HPA，实际 %q", info.Name)
	}
}

// 真实 BCS 返回会是字符串 JSON，确保反序列化 → 解析链路可跑通
func TestPickHPAForDeployment_FromJSONString(t *testing.T) {
	raw := `{"data":[{"data":{"metadata":{"name":"h1"},"spec":{"minReplicas":2,"maxReplicas":15,"scaleTargetRef":{"kind":"Deployment","name":"game-core"}},"status":{"desiredReplicas":5}}}]}`
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	info := pickHPAForDeployment(m, "game-core")
	if !info.Found || info.MaxReplicas != 15 {
		t.Errorf("端到端解析失败：%+v", info)
	}
}

// ---- HPAInfo.InRange -------------------------------------------------------

func TestHPAInfo_InRange(t *testing.T) {
	cases := []struct {
		name   string
		info   HPAInfo
		target int
		want   bool
	}{
		{"未找到时一律 true", HPAInfo{Found: false}, 9999, true},
		{"在区间内下界", HPAInfo{Found: true, MinReplicas: 2, MaxReplicas: 10}, 2, true},
		{"在区间内上界", HPAInfo{Found: true, MinReplicas: 2, MaxReplicas: 10}, 10, true},
		{"超上界", HPAInfo{Found: true, MinReplicas: 2, MaxReplicas: 10}, 11, false},
		{"低于下界", HPAInfo{Found: true, MinReplicas: 2, MaxReplicas: 10}, 1, false},
		{"区间中点", HPAInfo{Found: true, MinReplicas: 1, MaxReplicas: 100}, 50, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.info.InRange(c.target); got != c.want {
				t.Errorf("InRange(%d) = %v, want %v", c.target, got, c.want)
			}
		})
	}
}

// ---- NormalizeHPAPolicy ----------------------------------------------------

func TestNormalizeHPAPolicy(t *testing.T) {
	cases := map[string]HPAConflictPolicy{
		"":         PolicyWarn,  // 向后兼容：未填字段 = warn
		"warn":     PolicyWarn,
		"  WARN  ": PolicyWarn,  // 大小写/空格容忍
		"Block":    PolicyBlock,
		"force":    PolicyForce,
		"invalid":  PolicyWarn,  // 非法值安全回退
		"stop":     PolicyWarn,
	}
	for in, want := range cases {
		if got := NormalizeHPAPolicy(in); got != want {
			t.Errorf("NormalizeHPAPolicy(%q)=%q want %q", in, got, want)
		}
	}
}

// ---- DetectHPAForDeployment Mock/nil 安全 ----------------------------------

func TestDetectHPA_MockClientReturnsEmpty(t *testing.T) {
	// Mock 模式必须安全返回 Found=false（理由见 hpa_detect.go 顶部注释）
	client := bcsapi.NewClient(bcsapi.WithMockMode(true))
	info, err := DetectHPAForDeployment(context.Background(), client, "c", "ns", "d")
	if err != nil {
		t.Fatalf("Mock 不应返回 err：%v", err)
	}
	if info.Found {
		t.Errorf("Mock 必须返回 Found=false，实际 %+v", info)
	}
}

func TestDetectHPA_NilClientSafe(t *testing.T) {
	// 防御性：client=nil 不应 panic
	info, err := DetectHPAForDeployment(context.Background(), nil, "c", "ns", "d")
	if err != nil {
		t.Fatalf("nil client 不应返回 err：%v", err)
	}
	if info.Found {
		t.Errorf("nil client 应返回 Found=false")
	}
}
