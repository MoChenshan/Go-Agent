// helm_test.go —— bcs_helm_manage 工具单元测试（D19.7 重点覆盖 ReadyWaiter 接入）。
//
// 覆盖点（按重要性）：
//  1) HITL 未 confirmed 写操作必返 Plan（回归已有契约）
//  2) WaitForReady=false → 写路径成功但不挂 wait_for_ready 字段（schema 紧凑）
//  3) WaitForReady=true + WaitDeployment="" → 显式 skipped（给 LLM "你忘了传名字" 反馈）
//  4) WaitForReady=true + WaitDeployment=X → rollback 路径真正调 Waiter，Mode="helm_rollback"
//  5) install 路径同样对称触发（Mode="helm_install"）
//  6) uninstall 即使打开 WaitForReady 也必不 wait（语义相反）
//  7) list/history 纯读即使打开 WaitForReady 也必不 wait
//
// 测试策略：
//   - 复用同包的 recordingWaiter（scale_test.go 定义）作为可观测 Waiter
//   - 复用 bcsapi Mock 模式：所有请求命中 ErrMockMode，走 mockHelm 分支
//   - HITL_DISABLE=1 绕过人工确认拦截，便于直接断言执行路径
package bcstools

import (
	"context"
	"encoding/json"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ---- 测试辅助 ---------------------------------------------------------------

func newMockHelmClient() *bcsapi.Client {
	return bcsapi.NewClient(bcsapi.WithMockMode(true))
}

// callHelm 把 HelmInput marshal 成 JSON 后通过 CallableTool.Call 调用。
func callHelm(t *testing.T, tl tool.Tool, in HelmInput) (*Result, error) {
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

func mustCallHelm(t *testing.T, tl tool.Tool, in HelmInput) *Result {
	t.Helper()
	r, err := callHelm(t, tl, in)
	if err != nil {
		t.Fatalf("callHelm unexpected error: %v", err)
	}
	return r
}

// -----------------------------------------------------------------------------
// 1) HITL 契约回归：未 confirmed 的写操作必返 Plan
// -----------------------------------------------------------------------------

// TestHelm_Rollback_NotConfirmed_ReturnsPlan
// 这是 D6 就建立的既有契约——D19.7 新增字段不得破坏它。
func TestHelm_Rollback_NotConfirmed_ReturnsPlan(t *testing.T) {
	// 不设置 HITL_DISABLE，保留默认拦截语义
	rw := &recordingWaiter{ready: true}
	tl := newHelmToolWithWaiter(newMockHelmClient(), rw)

	r := mustCallHelm(t, tl, HelmInput{
		Action:      "rollback",
		ClusterID:   "BCS-K8S-00001",
		Namespace:   "letsgo",
		ReleaseName: "game-core",
		Revision:    3,
		Confirmed:   false, // 关键：未确认
	})
	if r.OK {
		t.Fatalf("未 confirmed 时 OK 应为 false（返回 Plan），msg=%s", r.Message)
	}
	if rw.called {
		t.Fatal("未 confirmed 时绝不应触达 Waiter（请求连 API 都不该发）")
	}
}

// -----------------------------------------------------------------------------
// 2) WaitForReady=false：写成功但不挂 wait_for_ready 字段
// -----------------------------------------------------------------------------

// TestHelm_Rollback_WaitFalse_NoWaitField
// 默认不开等待时，响应 schema 应保持"纯净"——不挂多余字段，也不调 Waiter。
// 这保证 D19.7 对老行为零回归。
func TestHelm_Rollback_WaitFalse_NoWaitField(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: true}
	tl := newHelmToolWithWaiter(newMockHelmClient(), rw)

	r := mustCallHelm(t, tl, HelmInput{
		Action:      "rollback",
		ClusterID:   "BCS-K8S-00001",
		Namespace:   "letsgo",
		ReleaseName: "game-core",
		Revision:    3,
		Confirmed:   true,
		// WaitForReady 默认 false
	})
	if !r.OK {
		t.Fatalf("rollback 本身应成功，msg=%s", r.Message)
	}
	if rw.called {
		t.Fatal("WaitForReady=false 时 Waiter 绝不应被调用")
	}
	data, _ := r.Data.(map[string]any)
	if _, exists := data["wait_for_ready"]; exists {
		t.Errorf("WaitForReady=false 时不应出现 wait_for_ready 字段（保持 schema 紧凑），Data=%v", data)
	}
}

// -----------------------------------------------------------------------------
// 3) WaitForReady=true + WaitDeployment="" → skipped
// -----------------------------------------------------------------------------

// TestHelm_Rollback_WaitTrueButNoDeployment_Skipped
// 这是 D19.7 的独特语义：helm release 可能关联多个工作负载，ReadyWaiter 只支持单个，
// 因此 WaitForReady=true 必须配 WaitDeployment；缺失时用 skipped + reason 明确告知，
// 给 LLM 一个可机读的反馈信号（"你忘了先 action=history 拿 deployment 名"）。
// 比静默不 wait 强得多。
func TestHelm_Rollback_WaitTrueButNoDeployment_Skipped(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: true}
	tl := newHelmToolWithWaiter(newMockHelmClient(), rw)

	r := mustCallHelm(t, tl, HelmInput{
		Action:       "rollback",
		ClusterID:    "BCS-K8S-00001",
		Namespace:    "letsgo",
		ReleaseName:  "game-core",
		Revision:     3,
		Confirmed:    true,
		WaitForReady: true,
		// WaitDeployment 刻意缺省
	})
	if !r.OK {
		t.Fatalf("rollback 本身应成功，msg=%s", r.Message)
	}
	if rw.called {
		t.Fatal("缺 WaitDeployment 时 Waiter 绝不应被调用")
	}
	data, _ := r.Data.(map[string]any)
	wait, ok := data["wait_for_ready"].(map[string]any)
	if !ok {
		t.Fatalf("应带 wait_for_ready 字段，Data=%v", data)
	}
	if wait["status"] != "skipped" {
		t.Errorf("status 应为 skipped，实际=%v", wait["status"])
	}
	if wait["reason"] != "wait_deployment_required" {
		t.Errorf("reason 应为 wait_deployment_required，实际=%v", wait["reason"])
	}
	if wait["attempted"] != false {
		t.Errorf("attempted 应为 false，实际=%v", wait["attempted"])
	}
	// hint 字段应存在，便于 LLM 阅读
	if _, hasHint := wait["hint"]; !hasHint {
		t.Error("应带 hint 字段告知"+"LLM 如何补齐")
	}
}

// -----------------------------------------------------------------------------
// 4) rollback + WaitDeployment → Waiter 被调，Mode="helm_rollback"
// -----------------------------------------------------------------------------

// TestHelm_Rollback_WithWaitDeployment_InvokesWaiter
// D19.7 最核心的断言：Waiter 被正确传入 spec，Mode 命名符合"helm_<action>"约定，
// 该约定是为了让 observability 面板能按 action 分桶告警（rollback 比 install 更敏感）。
func TestHelm_Rollback_WithWaitDeployment_InvokesWaiter(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: true}
	tl := newHelmToolWithWaiter(newMockHelmClient(), rw)

	r := mustCallHelm(t, tl, HelmInput{
		Action:         "rollback",
		ClusterID:      "BCS-K8S-00001",
		Namespace:      "letsgo",
		ReleaseName:    "game-core",
		Revision:       3,
		Confirmed:      true,
		WaitForReady:   true,
		WaitDeployment: "game-core-api", // Release 下的某个具体 Deployment
	})
	if !r.OK {
		t.Fatalf("应成功，msg=%s", r.Message)
	}
	if !rw.called {
		t.Fatal("Waiter 必须被调用")
	}
	if rw.spec.Mode != "helm_rollback" {
		t.Errorf("Mode 应为 helm_rollback（便于指标分桶），实际=%q", rw.spec.Mode)
	}
	if rw.spec.ClusterID != "BCS-K8S-00001" {
		t.Errorf("ClusterID 传递错误：%q", rw.spec.ClusterID)
	}
	if rw.spec.Namespace != "letsgo" {
		t.Errorf("Namespace 传递错误：%q", rw.spec.Namespace)
	}
	if rw.spec.Deployment != "game-core-api" {
		t.Errorf("Deployment 应使用 WaitDeployment 的值，实际=%q", rw.spec.Deployment)
	}
	data, _ := r.Data.(map[string]any)
	wait, _ := data["wait_for_ready"].(map[string]any)
	if wait["status"] != "ready" {
		t.Errorf("wait_for_ready.status 应为 ready，实际=%v", wait["status"])
	}
}

// -----------------------------------------------------------------------------
// 5) install 路径对称触发 Waiter，Mode="helm_install"
// -----------------------------------------------------------------------------

func TestHelm_Install_WithWaitDeployment_InvokesWaiterWithInstallMode(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: true}
	tl := newHelmToolWithWaiter(newMockHelmClient(), rw)

	r := mustCallHelm(t, tl, HelmInput{
		Action:         "install",
		ClusterID:      "BCS-K8S-00001",
		Namespace:      "letsgo",
		ReleaseName:    "game-core",
		Chart:          "bkrepo/game-core:1.2.3",
		Confirmed:      true,
		WaitForReady:   true,
		WaitDeployment: "game-core",
	})
	if !r.OK {
		t.Fatalf("应成功，msg=%s", r.Message)
	}
	if !rw.called {
		t.Fatal("install 路径 Waiter 必须被调用")
	}
	if rw.spec.Mode != "helm_install" {
		t.Errorf("Mode 应为 helm_install，实际=%q", rw.spec.Mode)
	}
}

// -----------------------------------------------------------------------------
// 6) uninstall：WaitForReady=true 也绝不 wait（语义相反）
// -----------------------------------------------------------------------------

// TestHelm_Uninstall_NeverWaitsEvenIfFlagOn
// uninstall 语义是"资源消失"，与"Deployment ready"语义相反。
// 即使用户 / LLM 传 WaitForReady=true + WaitDeployment=X，本工具也必须无视——
// 若真的 wait 会得到"永远 ready=false"的误告警，污染指标。
func TestHelm_Uninstall_NeverWaitsEvenIfFlagOn(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: true}
	tl := newHelmToolWithWaiter(newMockHelmClient(), rw)

	// uninstall 在 Plan 里 RequireReason=true，但 HITL_DISABLE=1 下仍绕过，
	// 因此这里只需触发执行路径验证 Waiter 不被调。
	r := mustCallHelm(t, tl, HelmInput{
		Action:         "uninstall",
		ClusterID:      "BCS-K8S-00001",
		Namespace:      "letsgo",
		ReleaseName:    "game-core",
		Confirmed:      true,
		WaitForReady:   true,     // 故意打开
		WaitDeployment: "game-core", // 故意给名字
	})
	if !r.OK {
		t.Fatalf("uninstall 应成功，msg=%s", r.Message)
	}
	if rw.called {
		t.Fatal("uninstall 语义与 ready 等待相反，Waiter 绝不应被调用")
	}
	data, _ := r.Data.(map[string]any)
	if _, exists := data["wait_for_ready"]; exists {
		t.Errorf("uninstall 响应不应出现 wait_for_ready 字段，Data=%v", data)
	}
}

// -----------------------------------------------------------------------------
// 7) list / history：纯读即使打开 WaitForReady 也不 wait
// -----------------------------------------------------------------------------

func TestHelm_List_NeverWaits(t *testing.T) {
	rw := &recordingWaiter{ready: true}
	tl := newHelmToolWithWaiter(newMockHelmClient(), rw)

	_ = mustCallHelm(t, tl, HelmInput{
		Action:         "list",
		ClusterID:      "BCS-K8S-00001",
		WaitForReady:   true, // 打开也无效
		WaitDeployment: "game-core",
	})
	if rw.called {
		t.Fatal("list 是纯读操作，Waiter 绝不应被调用")
	}
}

func TestHelm_History_NeverWaits(t *testing.T) {
	rw := &recordingWaiter{ready: true}
	tl := newHelmToolWithWaiter(newMockHelmClient(), rw)

	_ = mustCallHelm(t, tl, HelmInput{
		Action:         "history",
		ClusterID:      "BCS-K8S-00001",
		Namespace:      "letsgo",
		ReleaseName:    "game-core",
		WaitForReady:   true, // 打开也无效
		WaitDeployment: "game-core",
	})
	if rw.called {
		t.Fatal("history 是纯读操作，Waiter 绝不应被调用")
	}
}
