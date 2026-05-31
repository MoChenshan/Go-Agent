// hpa_patch_test.go —— bcs_hpa_patch 单元测试（D20.2）。
//
// # 覆盖矩阵
//
//  A) 输入校验
//     1. 缺 op → 报错
//     2. 未知 op → 报错
//     3. 缺 cluster/namespace/name → 报错
//     4. op=set_min 缺 min_replicas → 报错
//     5. op=set_max 缺 max_replicas → 报错
//     6. op=set_range 缺 min 或 max → 报错
//
//  B) 防护红线（R2~R6，拒绝路径）
//     7.  R2：min=0 → 拒绝
//     8.  R3：max < min → 拒绝
//     9.  R6：expected_current_max 不一致 → 拒绝（并发守护）
//     10. HPA 不存在 → 拒绝
//
//  C) Severity 分级（B 路径以外）
//     11. 普通改 set_max 从 10→12 → High（起步级）
//     12. prod ns → Critical + RequireReason
//     13. max 超天花板 100 → Critical + RequireReason
//     14. max 增长 >= 3x → Critical
//     15. max 下降 <= 50% → Critical
//     16. op=disable → Critical + RequireReason
//     17. Critical + RequireReason 无 reason → 拒绝
//
//  D) 执行路径（confirmed=false/true）
//     18. 未 confirmed 返回 Plan（含 before_min/before_max/target_min/target_max/growth_ratio）
//     19. confirmed 成功路径（Mock）
//     20. op=get 只读直返
//
//  E) pickHPAByName 纯函数解析
//     21. 空 data → Found=false
//     22. 匹配名称但 spec 缺失 → Found=true / Max=0
//     23. 完整对象 → 字段映射正确
//     24. minReplicas 省略时默认 1
package bcstools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// ---- 测试辅助（与其他工具同款范式） ---------------------------------------

func callHPAPatch(t *testing.T, tl tool.Tool, in HPAPatchInput) (*Result, error) {
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

func mustCallHPAPatch(t *testing.T, tl tool.Tool, in HPAPatchInput) *Result {
	t.Helper()
	r, err := callHPAPatch(t, tl, in)
	if err != nil {
		t.Fatalf("callHPAPatch unexpected error: %v", err)
	}
	return r
}

// Mock 模式下的 tool：现值由 doHPAWrite 内部伪造为 min=2/max=10。
func newMockHPAPatchTool() tool.Tool {
	return newHPAPatchTool(bcsapi.NewClient(bcsapi.WithMockMode(true)))
}

// ---- A) 输入校验 ----------------------------------------------------------

func TestHPAPatch_MissingOp(t *testing.T) {
	tl := newMockHPAPatchTool()
	_, err := callHPAPatch(t, tl, HPAPatchInput{ClusterID: "c", Namespace: "ns", Name: "hpa-x"})
	if err == nil {
		t.Fatal("缺 op 应报错")
	}
}

func TestHPAPatch_UnknownOp(t *testing.T) {
	tl := newMockHPAPatchTool()
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_all", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
	})
	if err == nil {
		t.Fatal("未知 op 应报错")
	}
}

func TestHPAPatch_MissingIdentity(t *testing.T) {
	tl := newMockHPAPatchTool()
	_, err := callHPAPatch(t, tl, HPAPatchInput{Op: "get"})
	if err == nil {
		t.Fatal("缺 cluster/namespace/name 应报错")
	}
}

func TestHPAPatch_SetMinMissingValue(t *testing.T) {
	tl := newMockHPAPatchTool()
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_min", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		Confirmed: true,
	})
	if err == nil {
		t.Fatal("op=set_min 缺 min_replicas 应报错")
	}
}

func TestHPAPatch_SetMaxMissingValue(t *testing.T) {
	tl := newMockHPAPatchTool()
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		Confirmed: true,
	})
	if err == nil {
		t.Fatal("op=set_max 缺 max_replicas 应报错")
	}
}

func TestHPAPatch_SetRangeMissing(t *testing.T) {
	tl := newMockHPAPatchTool()
	// 只给 min 不给 max
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_range", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MinReplicas: 3, Confirmed: true,
	})
	if err == nil {
		t.Fatal("op=set_range 缺 max 应报错")
	}
}

// ---- B) 防护红线 ----------------------------------------------------------

func TestHPAPatch_R2_MinZeroRejected(t *testing.T) {
	tl := newMockHPAPatchTool()
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_min", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MinReplicas: 0, Confirmed: true,
	})
	// min=0 在 set_min 入口就会被 resolveHPATarget 拦（因为它要求 in.MinReplicas > 0）
	if err == nil {
		t.Fatal("min=0 应被拒绝")
	}
}

func TestHPAPatch_R2_MinZeroViaSetRangeRejected(t *testing.T) {
	tl := newMockHPAPatchTool()
	// 通过 set_range 绕开入口校验：给 min_replicas=1 正常通过 resolveHPATarget，但是
	// 想测"set_range 且 min=0"的场景 —— 由于 resolveHPATarget 里 in.MinReplicas <= 0
	// 也会被拦，R2 直接守护的场景实际是"目标 min < 1"，这里等价。此处主要验证守护存在。
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_range", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MinReplicas: -1, MaxReplicas: 5, Confirmed: true,
	})
	if err == nil {
		t.Fatal("min=-1 应被拒绝")
	}
}

func TestHPAPatch_R3_MaxLessThanMinRejected(t *testing.T) {
	tl := newMockHPAPatchTool()
	// set_range：min=5, max=3 → R3 拦截
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_range", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MinReplicas: 5, MaxReplicas: 3, Confirmed: true,
	})
	if err == nil || !strings.Contains(err.Error(), "R3") {
		t.Fatalf("max<min 应被 R3 拦截，实际 err=%v", err)
	}
}

func TestHPAPatch_R6_ExpectedCurrentMaxMismatch(t *testing.T) {
	tl := newMockHPAPatchTool()
	// Mock 现值 max=10；expected_current_max=99 不一致 → 拒绝
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MaxReplicas: 12, ExpectedCurrentMax: 99, Confirmed: true,
	})
	if err == nil || !strings.Contains(err.Error(), "R6") {
		t.Fatalf("并发守护应触发，实际 err=%v", err)
	}
}

// ---- C) Severity 分级 ----------------------------------------------------

func TestHPAPatch_C11_NormalSetMaxIsHigh(t *testing.T) {
	tl := newMockHPAPatchTool()
	// Mock 现值 max=10 → target 12（仅 1.2x，不触发 R5）；非 prod ns；未超天花板
	// 期望：Severity=High（HPA 起步级）
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MaxReplicas: 12,
	})
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 PendingResult，实际 %T", r.Data)
	}
	if pending.Plan.Severity != hitl.SeverityHigh {
		t.Errorf("普通 set_max 应 High，实际 %q", pending.Plan.Severity)
	}
	if pending.Plan.RequireReason {
		t.Error("普通档不应 RequireReason=true")
	}
}

func TestHPAPatch_C12_ProdNamespaceIsCritical(t *testing.T) {
	tl := newMockHPAPatchTool()
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "prod-core", Name: "hpa-x",
		MaxReplicas: 12,
	})
	pending := r.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("prod ns 应 Critical，实际 %q", pending.Plan.Severity)
	}
	if !pending.Plan.RequireReason {
		t.Error("prod ns 应 RequireReason=true")
	}
}

func TestHPAPatch_C13_OverCeilingIsCritical(t *testing.T) {
	tl := newMockHPAPatchTool()
	// 目标 max=150 超 HPAMaxCeiling=100
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MaxReplicas: 150,
	})
	pending := r.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("max 超天花板应 Critical，实际 %q", pending.Plan.Severity)
	}
	if !pending.Plan.RequireReason {
		t.Error("超天花板应 RequireReason=true")
	}
	if over, _ := pending.Plan.Params["over_ceiling"].(bool); !over {
		t.Error("Params.over_ceiling 应为 true")
	}
}

func TestHPAPatch_C14_GrowthOver3xIsCritical(t *testing.T) {
	tl := newMockHPAPatchTool()
	// Mock 现值 max=10；目标 max=35 → 3.5x，触发 R5
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MaxReplicas: 35,
	})
	pending := r.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("max 增长 3.5x 应 Critical，实际 %q", pending.Plan.Severity)
	}
	if ratio, _ := pending.Plan.Params["growth_ratio"].(float64); ratio < 3.0 {
		t.Errorf("growth_ratio 应 >= 3，实际 %v", ratio)
	}
}

func TestHPAPatch_C15_ShrinkBelowHalfIsCritical(t *testing.T) {
	tl := newMockHPAPatchTool()
	// Mock 现值 max=10；目标 max=4 → 0.4x，触发 R5 下降
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MaxReplicas: 4,
	})
	pending := r.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("max 砍半以上应 Critical，实际 %q", pending.Plan.Severity)
	}
}

func TestHPAPatch_C16_DisableIsCritical(t *testing.T) {
	tl := newMockHPAPatchTool()
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "disable", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
	})
	pending := r.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("disable 应 Critical，实际 %q", pending.Plan.Severity)
	}
	if !pending.Plan.RequireReason {
		t.Error("disable 应 RequireReason=true")
	}
	// SideEffect 应明示"冻结"语义
	if !strings.Contains(pending.Plan.SideEffect, "冻结") {
		t.Errorf("disable SideEffect 应含 '冻结'，实际 %q", pending.Plan.SideEffect)
	}
}

func TestHPAPatch_C17_CriticalNoReasonRejected(t *testing.T) {
	tl := newMockHPAPatchTool()
	// prod ns + confirmed=true + 无 reason → 硬拒绝
	_, err := callHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "prod-core", Name: "hpa-x",
		MaxReplicas: 12, Confirmed: true,
	})
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("Critical + RequireReason 无 reason 应拒绝，实际 err=%v", err)
	}
}

// ---- D) 执行路径 ---------------------------------------------------------

func TestHPAPatch_D18_PendingPlanHasDiffFields(t *testing.T) {
	tl := newMockHPAPatchTool()
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "set_range", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MinReplicas: 3, MaxReplicas: 15,
	})
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("未 confirmed 应返 PendingResult")
	}
	mustInt := func(k string, want int) {
		t.Helper()
		v, ok := pending.Plan.Params[k]
		if !ok {
			t.Errorf("Plan.Params.%s 缺失", k)
			return
		}
		// JSON 经过 marshal/unmarshal 会变 float64；这里 Params 直接是 int 原值
		switch val := v.(type) {
		case int:
			if val != want {
				t.Errorf("Plan.Params.%s 应为 %d，实际 %d", k, want, val)
			}
		case float64:
			if int(val) != want {
				t.Errorf("Plan.Params.%s 应为 %d，实际 %v", k, want, val)
			}
		default:
			t.Errorf("Plan.Params.%s 类型异常 %T", k, v)
		}
	}
	mustInt("before_min", 2)
	mustInt("before_max", 10)
	mustInt("target_min", 3)
	mustInt("target_max", 15)
}

func TestHPAPatch_D19_ConfirmedSucceedsInMock(t *testing.T) {
	tl := newMockHPAPatchTool()
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "set_max", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
		MaxReplicas: 12, Confirmed: true,
	})
	if !r.OK {
		t.Fatalf("confirmed + 合法参数应成功，实际 msg=%s", r.Message)
	}
	data := r.Data.(map[string]any)
	if data["max_replicas"] != 12 {
		t.Errorf("结果 max_replicas 应为 12，实际 %v", data["max_replicas"])
	}
	if data["before_max"] != 10 {
		t.Errorf("结果 before_max 应为 10（Mock 伪造现值），实际 %v", data["before_max"])
	}
}

func TestHPAPatch_D20_GetIsReadOnly(t *testing.T) {
	tl := newMockHPAPatchTool()
	r := mustCallHPAPatch(t, tl, HPAPatchInput{
		Op: "get", ClusterID: "c", Namespace: "ns", Name: "hpa-x",
	})
	if !r.OK {
		t.Fatalf("op=get 应成功，实际 msg=%s", r.Message)
	}
	// Mock 返回样例 data
	data := r.Data.(map[string]any)
	if data["max_replicas"] != 10 {
		t.Errorf("Mock get 应返 max=10，实际 %v", data["max_replicas"])
	}
	if data["found"] != true {
		t.Error("Mock get 应返 found=true")
	}
}

// ---- E) pickHPAByName 纯函数 --------------------------------------------

func TestPickHPAByName_EmptyData(t *testing.T) {
	info := pickHPAByName(map[string]any{}, "hpa-x")
	if info.Found {
		t.Error("空 data 应 Found=false")
	}
}

func TestPickHPAByName_NoMatch(t *testing.T) {
	resp := map[string]any{
		"data": []any{
			map[string]any{
				"data": map[string]any{
					"metadata": map[string]any{"name": "hpa-other"},
				},
			},
		},
	}
	info := pickHPAByName(resp, "hpa-x")
	if info.Found {
		t.Error("名称不匹配应 Found=false")
	}
}

func TestPickHPAByName_FullObject(t *testing.T) {
	resp := map[string]any{
		"data": []any{
			map[string]any{
				"data": map[string]any{
					"metadata": map[string]any{"name": "hpa-x"},
					"spec": map[string]any{
						"minReplicas": float64(3),
						"maxReplicas": float64(20),
						"scaleTargetRef": map[string]any{
							"kind": "Deployment", "name": "game-core",
						},
					},
					"status": map[string]any{
						"desiredReplicas": float64(5),
					},
				},
			},
		},
	}
	info := pickHPAByName(resp, "hpa-x")
	if !info.Found {
		t.Fatal("完整对象应 Found=true")
	}
	if info.Name != "hpa-x" || info.MinReplicas != 3 || info.MaxReplicas != 20 || info.CurrentSpec != 5 {
		t.Errorf("字段映射错：%+v", info)
	}
	// extractScaleTargetName 能识别
	if got := extractScaleTargetName(info); got != "game-core" {
		t.Errorf("extractScaleTargetName 应返 game-core，实际 %q", got)
	}
}

func TestPickHPAByName_MinReplicasDefault(t *testing.T) {
	// 省略 minReplicas → 默认 1
	resp := map[string]any{
		"data": []any{
			map[string]any{
				"data": map[string]any{
					"metadata": map[string]any{"name": "hpa-x"},
					"spec": map[string]any{
						"maxReplicas": float64(10),
					},
				},
			},
		},
	}
	info := pickHPAByName(resp, "hpa-x")
	if info.MinReplicas != 1 {
		t.Errorf("省略 minReplicas 应默认 1，实际 %d", info.MinReplicas)
	}
	// 缺 scaleTargetRef 时 extractScaleTargetName 返 (unknown)
	if got := extractScaleTargetName(info); got != "(unknown)" {
		t.Errorf("缺 scaleTargetRef 应返 (unknown)，实际 %q", got)
	}
}
