// pod_restart_hpa_test.go D20.1 —— rollout_restart 与 HPA 感知的端到端测试。
//
// # 为什么与 pod_restart_test.go 分离
//
//   pod_restart_test.go 的 newMockRestartTool 走 Mock 模式，HPA 检测永远 Found=false。
//   D20.1 的核心验证是"检测到真实 HPA 时 warn/ignore 双档策略表现"，Mock 不覆盖。
//
// 本文件走 httptest.Server 真路径（与 scale_hpa_test.go 同款设计，但路径不同）：
//   - rollout_restart 是 PATCH /bcsapi/v4/storage/.../deployments/{name}
//   - HPA 检测是 GET /bcsapi/v4/storage/.../horizontalpodautoscaler
//   - 与 scale 差异：rollout 不读 deployment spec（不需要 currentReplicas）
//
// # 测试矩阵
//
//   A. 无 HPA（warn 默认）                 —— 行为等价于 D19，Plan 里无 hpa 段
//   B. 有 HPA + warn（未 confirmed）       —— Plan 含 HPA 提示；Severity 升到 High+
//   C. 有 HPA + warn（confirmed）          —— 真实 PATCH 成功，1 次写
//   D. 有 HPA + ignore（confirmed）        —— 真实 PATCH 成功；审计含 hpa_ignored=true
//   E. 有 HPA + 非法 policy                —— 回退为 warn（而非报错）
package bcstools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// ---- Fake BCS Server for rollout_restart -----------------------------------

// fakeBCSRouterForRollout 按 HTTP 路径分流：HPA 列表 + deployment PATCH。
//
// 与 fakeBCSRouter（scale 专用）的差异：
//   - 不需要 deployment GET（rollout 不读 current replicas）
//   - 捕获的写是 PATCH（而非 PUT /scale）
func fakeBCSRouterForRollout(
	t *testing.T,
	hpaResp string,
	patchHits *atomic.Int32,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/horizontalpodautoscaler"):
			_, _ = fmt.Fprint(w, hpaResp)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/deployments/"):
			if patchHits != nil {
				patchHits.Add(1)
			}
			_, _ = fmt.Fprint(w, `{}`)
		default:
			_, _ = fmt.Fprint(w, `{"data":[]}`)
		}
	}))
}

// newRealPodRestartTool 指向 fake BCS Server 的真实 pod_restart 工具（非 Mock）。
// 使用 Noop Waiter 避免 wait_for_ready 副作用（D19.5 已覆盖其行为）。
func newRealPodRestartTool(baseURL string) tool.Tool {
	client := bcsapi.NewClient(
		bcsapi.WithBaseURL(baseURL),
		bcsapi.WithToken("test-token"),
	)
	return newPodRestartToolWithWaiter(client, NewNoopReadyWaiter())
}

// ---- A. 无 HPA：warn 默认不打扰 --------------------------------------------

func TestRolloutHPA_NoHPA_WarnDefaultNoHPAInPlan(t *testing.T) {
	var patchHits atomic.Int32
	srv := fakeBCSRouterForRollout(t, emptyHPAListResp(), &patchHits)
	defer srv.Close()
	tl := newRealPodRestartTool(srv.URL)

	// 未 confirmed：返 Plan，应无 hpa 段
	r, err := callRestart(t, tl, PodRestartInput{
		Mode: "rollout_restart", ClusterID: "c", Namespace: "ns", Deployment: "d",
	})
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if r.OK {
		t.Fatal("未 confirmed 应 OK=false（pending Plan）")
	}
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 PendingResult，实际 %T", r.Data)
	}
	if _, has := pending.Plan.Params["hpa"]; has {
		t.Error("无 HPA 时 Plan.Params 不应出现 hpa 字段")
	}
	// Severity 应为 Medium（Prod ns 会升 High；ns=ns 非 prod 保持 Medium）
	if pending.Plan.Severity != hitl.SeverityMedium {
		t.Errorf("非 prod ns + 无 HPA，Severity 应为 Medium，实际 %q", pending.Plan.Severity)
	}
	if patchHits.Load() != 0 {
		t.Errorf("未 confirmed 不应触发 PATCH，实际 %d 次", patchHits.Load())
	}
}

// ---- B. 有 HPA + warn（未 confirmed）：Plan 含警告 + Severity 升档 ----------

func TestRolloutHPA_HasHPA_WarnElevatesSeverityAndAnnotatesPlan(t *testing.T) {
	var patchHits atomic.Int32
	srv := fakeBCSRouterForRollout(t, oneHPAListResp("hpa-core", "d", 3, 10), &patchHits)
	defer srv.Close()
	tl := newRealPodRestartTool(srv.URL)

	// 未 confirmed + HPAPolicy 留空（默认 warn）
	r, err := callRestart(t, tl, PodRestartInput{
		Mode: "rollout_restart", ClusterID: "c", Namespace: "ns", Deployment: "d",
	})
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if r.OK {
		t.Fatal("未 confirmed 应返回 pending Plan")
	}
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 PendingResult，实际 %T", r.Data)
	}

	// Severity：非 prod ns Medium + HPA warn → 升到 High
	if pending.Plan.Severity != hitl.SeverityHigh {
		t.Errorf("warn + HPA 应升 Severity 到 High，实际 %q", pending.Plan.Severity)
	}
	// SideEffect 前缀应含 HPA 警告
	if !strings.Contains(pending.Plan.SideEffect, "HPA") {
		t.Errorf("SideEffect 应含 HPA 警告，实际 %q", pending.Plan.SideEffect)
	}
	// Params.hpa 应有内容，Params.hpa_policy == warn
	hpaMap, _ := pending.Plan.Params["hpa"].(map[string]any)
	if hpaMap == nil || hpaMap["name"] != "hpa-core" {
		t.Errorf("Plan.Params.hpa 应含 name=hpa-core，实际 %+v", hpaMap)
	}
	if pending.Plan.Params["hpa_policy"] != "warn" {
		t.Errorf("Plan.Params.hpa_policy 应为 warn，实际 %v", pending.Plan.Params["hpa_policy"])
	}
	if patchHits.Load() != 0 {
		t.Errorf("未 confirmed 不应触发 PATCH，实际 %d", patchHits.Load())
	}
}

// ---- C. 有 HPA + warn + confirmed：放行真实 PATCH --------------------------

func TestRolloutHPA_HasHPA_WarnConfirmedPatches(t *testing.T) {
	var patchHits atomic.Int32
	srv := fakeBCSRouterForRollout(t, oneHPAListResp("hpa-core", "d", 3, 10), &patchHits)
	defer srv.Close()
	tl := newRealPodRestartTool(srv.URL)

	r, err := callRestart(t, tl, PodRestartInput{
		Mode: "rollout_restart", ClusterID: "c", Namespace: "ns", Deployment: "d",
		HPAPolicy: "warn", Confirmed: true,
	})
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if !r.OK {
		t.Fatalf("warn + confirmed 应放行，实际 msg=%s", r.Message)
	}
	if patchHits.Load() != 1 {
		t.Errorf("应真实 PATCH 1 次，实际 %d 次", patchHits.Load())
	}
}

// ---- D. 有 HPA + ignore + confirmed：通过且 Plan 里体现 ignore ---------------

func TestRolloutHPA_HasHPA_IgnorePolicyPassesAndMarksPlan(t *testing.T) {
	var patchHits atomic.Int32
	srv := fakeBCSRouterForRollout(t, oneHPAListResp("hpa-core", "d", 3, 10), &patchHits)
	defer srv.Close()
	tl := newRealPodRestartTool(srv.URL)

	// 先用未 confirmed 验证 Plan 的 SideEffect 里出现 "ignore" 字样
	r, err := callRestart(t, tl, PodRestartInput{
		Mode: "rollout_restart", ClusterID: "c", Namespace: "ns", Deployment: "d",
		HPAPolicy: "ignore",
	})
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 PendingResult，实际 %T", r.Data)
	}
	if !strings.Contains(pending.Plan.SideEffect, "ignore") {
		t.Errorf("ignore 模式 Plan.SideEffect 应明示 ignore，实际 %q", pending.Plan.SideEffect)
	}
	if pending.Plan.Params["hpa_policy"] != "ignore" {
		t.Errorf("Plan.Params.hpa_policy 应为 ignore，实际 %v", pending.Plan.Params["hpa_policy"])
	}
	// ignore 不升 Severity（与 warn 的差异）：非 prod ns 保持 Medium
	if pending.Plan.Severity != hitl.SeverityMedium {
		t.Errorf("ignore 不应升 Severity，实际 %q", pending.Plan.Severity)
	}

	// 再 confirmed + ignore：放行真实 PATCH
	r2, err := callRestart(t, tl, PodRestartInput{
		Mode: "rollout_restart", ClusterID: "c", Namespace: "ns", Deployment: "d",
		HPAPolicy: "ignore", Confirmed: true,
	})
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if !r2.OK {
		t.Fatalf("ignore + confirmed 应放行，实际 msg=%s", r2.Message)
	}
	if patchHits.Load() != 1 {
		t.Errorf("应真实 PATCH 1 次，实际 %d 次", patchHits.Load())
	}
}

// ---- E. 非法 policy 回退为 warn（不报错） ----------------------------------

func TestRolloutHPA_InvalidPolicyFallsBackToWarn(t *testing.T) {
	var patchHits atomic.Int32
	srv := fakeBCSRouterForRollout(t, oneHPAListResp("hpa-core", "d", 3, 10), &patchHits)
	defer srv.Close()
	tl := newRealPodRestartTool(srv.URL)

	// "block" 在 rollout_restart 里不合法，应被归一化为 warn
	r, err := callRestart(t, tl, PodRestartInput{
		Mode: "rollout_restart", ClusterID: "c", Namespace: "ns", Deployment: "d",
		HPAPolicy: "block",
	})
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 PendingResult，实际 %T", r.Data)
	}
	// 被当作 warn：Severity 升 High，Params.hpa_policy=warn
	if pending.Plan.Severity != hitl.SeverityHigh {
		t.Errorf("非法 policy 应回退为 warn 升 Severity 到 High，实际 %q", pending.Plan.Severity)
	}
	if pending.Plan.Params["hpa_policy"] != "warn" {
		t.Errorf("非法 policy 应归一化为 warn，实际 %v", pending.Plan.Params["hpa_policy"])
	}
	if patchHits.Load() != 0 {
		t.Errorf("未 confirmed 不应下发，实际 %d", patchHits.Load())
	}
}
