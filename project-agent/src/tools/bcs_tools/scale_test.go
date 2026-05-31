// scale_test.go —— bcs_scale_deployment 工具单元测试。
//
// 覆盖点（按重要性）：
//  1) Severity 动态分级：缩容到 0 / 大比例 / 生产 ns / 大规模部署
//  2) HITL 两段式：未 confirmed 必返 Plan；confirmed 后真实走通
//  3) Guard R1：生产 ns 缩容到 0 必须带 reason
//  4) Guard R2：|Δ| > 硬上限硬拒
//  5) 并发守护：expected_current（Mock 下跳过）
//  6) scale_relative 的 Δ 计算 & 负数保护
//  7) get 动作走纯读路径
//  8) 入参校验（空字段 / 非法 replicas / 未知 action / delta=0）
//  9) Mock 模式审计事件入账（from/to 字段齐全）
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

// ---- 测试辅助 ---------------------------------------------------------------

// newMockScaleClient 快速构造一个 mock 模式的 bcsapi Client。
func newMockScaleClient() *bcsapi.Client {
	return bcsapi.NewClient(bcsapi.WithMockMode(true))
}

// callScale 把 ScaleInput marshal 成 JSON，通过 CallableTool 调用并断言返回 *Result。
// 若 Call 本身报错（例如必填字段校验失败），err 会原样返回，result 为 nil。
func callScale(t *testing.T, tl tool.Tool, in ScaleInput) (*Result, error) {
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

// mustCallScale 封装 callScale，校验不返回 err，用于期望成功路径的用例。
func mustCallScale(t *testing.T, tl tool.Tool, in ScaleInput) *Result {
	t.Helper()
	r, err := callScale(t, tl, in)
	if err != nil {
		t.Fatalf("callScale unexpected error: %v", err)
	}
	return r
}

// -----------------------------------------------------------------------------
// 1) Severity 分级（纯函数单测）
// -----------------------------------------------------------------------------

func TestClassifySeverity_ScaleDownToZeroProdIsCritical(t *testing.T) {
	sev, needReason := classifySeverity(3, 0, "prod-letsgo")
	if sev != hitl.SeverityCritical {
		t.Errorf("生产 ns 缩容到 0 应为 Critical，实际=%v", sev)
	}
	if !needReason {
		t.Error("生产 ns 缩容到 0 应 require_reason=true")
	}
}

func TestClassifySeverity_ScaleDownToZeroNonProdIsHigh(t *testing.T) {
	sev, needReason := classifySeverity(3, 0, "dev-letsgo")
	if sev != hitl.SeverityHigh {
		t.Errorf("非生产 ns 缩容到 0 应为 High，实际=%v", sev)
	}
	if needReason {
		t.Error("非生产 ns 不应强制 require_reason")
	}
}

func TestClassifySeverity_BigRatioIsHigh(t *testing.T) {
	// 2 → 5，Δ=3，ratio=1.5 ≥ 1.0 → High
	sev, _ := classifySeverity(2, 5, "letsgo")
	if sev != hitl.SeverityHigh {
		t.Errorf("翻倍扩容应为 High，实际=%v", sev)
	}
}

func TestClassifySeverity_SmallChangeIsMedium(t *testing.T) {
	// 10 → 12，ratio=0.2 < 0.5 → Medium
	sev, _ := classifySeverity(10, 12, "letsgo")
	if sev != hitl.SeverityMedium {
		t.Errorf("小幅度变化应为 Medium，实际=%v", sev)
	}
}

func TestClassifySeverity_LargeDeploymentBumpsToHigh(t *testing.T) {
	// 100 → 110，ratio=0.1（本为 Medium），但 to>100 升档为 High
	sev, _ := classifySeverity(100, 110, "letsgo")
	if sev != hitl.SeverityHigh {
		t.Errorf("大规模部署（to>100）应升档为 High，实际=%v", sev)
	}
}

// -----------------------------------------------------------------------------
// 2) HITL 两段式
// -----------------------------------------------------------------------------

func TestScale_UnconfirmedReturnsPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newScaleTool(newMockScaleClient())

	result := mustCallScale(t, tl, ScaleInput{
		Action:     "scale",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
		Replicas:   5,
		Confirmed:  false,
	})
	if result.OK {
		t.Fatal("未 confirmed 时 OK 必须为 false（awaiting_confirmation）")
	}
	// Data 为 hitl.PendingResult（value 类型）
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Status != "awaiting_confirmation" {
		t.Errorf("Status 应为 awaiting_confirmation，实际=%q", pending.Status)
	}
	if pending.Plan.Action != "bcs.deployment.scale" {
		t.Errorf("Plan.Action 错误：%q", pending.Plan.Action)
	}
	if _, has := pending.Plan.Params["from"]; !has {
		t.Error("Plan.Params 必须包含 from 字段（审计法律字段）")
	}
	if _, has := pending.Plan.Params["to"]; !has {
		t.Error("Plan.Params 必须包含 to 字段（审计法律字段）")
	}
}

func TestScale_ConfirmedRunsThroughInMock(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newScaleTool(newMockScaleClient())

	result := mustCallScale(t, tl, ScaleInput{
		Action:     "scale",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
		Replicas:   5, // mock current=3 → Δ=2
		Confirmed:  true,
	})
	if !result.OK {
		t.Fatalf("confirmed 下应 OK=true，实际 msg=%s", result.Message)
	}
	if !result.Mock {
		t.Error("mock 模式下应标记 Mock=true")
	}
}

// -----------------------------------------------------------------------------
// 3) Guard R1：生产 ns 缩容到 0 必须带 reason
// -----------------------------------------------------------------------------

func TestScale_ProdNsToZeroWithoutReasonIsRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1") // 绕过 HITL，直接压到 R1
	tl := newScaleTool(newMockScaleClient())

	result := mustCallScale(t, tl, ScaleInput{
		Action:     "scale",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "prod-letsgo",
		Deployment: "game-core",
		Replicas:   0,
		Confirmed:  true,
	})
	if result.OK {
		t.Fatal("生产 ns 缩容到 0 且未给 reason 必须被 R1 拒绝")
	}
	if !strings.Contains(result.Message, "reason") {
		t.Errorf("错误信息应提示需要 reason，实际=%q", result.Message)
	}
}

func TestScale_ProdNsToZeroWithReasonPasses(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newScaleTool(newMockScaleClient())

	result := mustCallScale(t, tl, ScaleInput{
		Action:     "scale",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "prod-letsgo",
		Deployment: "game-core",
		Replicas:   0,
		Reason:     "活动结束下线",
		Confirmed:  true,
	})
	if !result.OK {
		t.Fatalf("带 reason 后应放行，实际 msg=%s", result.Message)
	}
}

// -----------------------------------------------------------------------------
// 4) Guard R2：|Δ| > 500 硬拒
// -----------------------------------------------------------------------------

func TestScale_DeltaExceedsHardLimitRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1") // 即使跳过 HITL，R2 也必须拦截
	tl := newScaleTool(newMockScaleClient())

	result := mustCallScale(t, tl, ScaleInput{
		Action:     "scale",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
		Replicas:   600, // mock current=3 → Δ=597 > 500
		Confirmed:  true,
	})
	if result.OK {
		t.Fatal("|Δ|>500 必须被 R2 硬拒")
	}
	if !strings.Contains(result.Message, "硬上限") {
		t.Errorf("错误信息应提示硬上限，实际=%q", result.Message)
	}
}

// -----------------------------------------------------------------------------
// 5) 并发守护：expected_current
// -----------------------------------------------------------------------------

// Mock 模式下 fetchCurrentReplicas 返回 ErrMockMode，守护逻辑会跳过 —— 这是预期行为，
// 因为 Mock 模式不可能存在真正的并发改动。这里验证"不匹配也能放行"即可。
func TestScale_ExpectedCurrentIsSkippedInMock(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newScaleTool(newMockScaleClient())

	exp := 99 // 故意错的期望值
	result := mustCallScale(t, tl, ScaleInput{
		Action:          "scale",
		ClusterID:       "BCS-K8S-00001",
		Namespace:       "letsgo",
		Deployment:      "game-core",
		Replicas:        5,
		ExpectedCurrent: &exp,
		Confirmed:       true,
	})
	if !result.OK {
		t.Fatalf("Mock 下 expected_current 不校验，应放行；实际 msg=%s", result.Message)
	}
}

// -----------------------------------------------------------------------------
// 6) scale_relative
// -----------------------------------------------------------------------------

func TestScale_RelativeComputesDelta(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newScaleTool(newMockScaleClient())

	// mock current=3，delta=+2 → to=5
	result := mustCallScale(t, tl, ScaleInput{
		Action:     "scale_relative",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
		Delta:      2,
		Confirmed:  true,
	})
	if !result.OK {
		t.Fatalf("应成功，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	if data["to"] != 5 {
		t.Errorf("to 应为 5，实际=%v", data["to"])
	}
	if data["from"] != 3 {
		t.Errorf("from 应为 3（mock），实际=%v", data["from"])
	}
}

func TestScale_RelativeNegativeResultRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newScaleTool(newMockScaleClient())

	// mock current=3，delta=-10 → 负数，拒绝
	_, err := callScale(t, tl, ScaleInput{
		Action:     "scale_relative",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
		Delta:      -10,
		Confirmed:  true,
	})
	if err == nil {
		t.Fatal("相对缩容为负时必须报错")
	}
	if !strings.Contains(err.Error(), "为负") {
		t.Errorf("错误信息应提及负数，实际=%v", err)
	}
}

func TestScale_RelativeZeroDeltaRejected(t *testing.T) {
	tl := newScaleTool(newMockScaleClient())
	_, err := callScale(t, tl, ScaleInput{
		Action: "scale_relative", ClusterID: "c", Namespace: "n", Deployment: "d", Delta: 0, Confirmed: true,
	})
	if err == nil {
		t.Fatal("delta=0 必须被拒绝")
	}
}

// -----------------------------------------------------------------------------
// 7) get 走纯读路径
// -----------------------------------------------------------------------------

func TestScale_GetActionReturnsReplicas(t *testing.T) {
	tl := newScaleTool(newMockScaleClient())

	result := mustCallScale(t, tl, ScaleInput{
		Action:     "get",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
	})
	if !result.OK {
		t.Fatalf("get 应返回 OK，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	if data["replicas"] != 3 {
		t.Errorf("mock replicas 应为 3，实际=%v", data["replicas"])
	}
}

// -----------------------------------------------------------------------------
// 8) 入参校验
// -----------------------------------------------------------------------------

func TestScale_MissingRequiredFieldsRejected(t *testing.T) {
	tl := newScaleTool(newMockScaleClient())

	cases := []ScaleInput{
		{}, // 全空
		{Action: "scale"},
		{Action: "scale", ClusterID: "c"},
		{Action: "scale", ClusterID: "c", Namespace: "n"}, // 缺 deployment
	}
	for i, in := range cases {
		_, err := callScale(t, tl, in)
		if err == nil {
			t.Errorf("case %d 应报错，却通过；in=%+v", i, in)
		}
	}
}

func TestScale_InvalidReplicasRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newScaleTool(newMockScaleClient())

	// replicas < 0
	_, err := callScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "n", Deployment: "d", Replicas: -1, Confirmed: true,
	})
	if err == nil {
		t.Error("负数 replicas 应被拒绝")
	}
	// replicas > 10000
	_, err = callScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "n", Deployment: "d", Replicas: 99999, Confirmed: true,
	})
	if err == nil {
		t.Error("过大 replicas 应被拒绝")
	}
}

func TestScale_UnknownActionRejected(t *testing.T) {
	tl := newScaleTool(newMockScaleClient())
	_, err := callScale(t, tl, ScaleInput{
		Action: "delete", ClusterID: "c", Namespace: "n", Deployment: "d",
	})
	if err == nil {
		t.Fatal("未知 action 应被拒绝")
	}
}

// -----------------------------------------------------------------------------
// 9) 审计事件入账（通过 MemorySink 捕获 JSONL 解析回 Record）
// -----------------------------------------------------------------------------

func TestScale_AuditEventHasFromAndTo(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	t.Setenv("AUDIT_DISABLE", "") // 确保审计开启

	mem := &audit.MemorySink{}
	old := audit.SetSink(mem)
	defer audit.SetSink(old)

	tl := newScaleTool(newMockScaleClient())

	r := mustCallScale(t, tl, ScaleInput{
		Action:     "scale",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
		Replicas:   5,
		Confirmed:  true,
	})
	if !r.OK {
		t.Fatalf("call 应成功，msg=%s", r.Message)
	}

	lines := mem.Snapshot()
	if len(lines) == 0 {
		t.Fatal("审计必须至少记录 1 条事件")
	}

	var hit *audit.Record
	for _, line := range lines {
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("审计日志不是合法 JSON：%s; err=%v", line, err)
		}
		if rec.Action == "bcs.deployment.scale" {
			hit = &rec
			break
		}
	}
	if hit == nil {
		t.Fatal("未找到 bcs.deployment.scale 审计事件")
	}

	// 经 json 往返，数值会变成 float64
	if v, _ := hit.Params["from"].(float64); int(v) != 3 {
		t.Errorf("Params.from 应为 3，实际=%v", hit.Params["from"])
	}
	if v, _ := hit.Params["to"].(float64); int(v) != 5 {
		t.Errorf("Params.to 应为 5，实际=%v", hit.Params["to"])
	}
	if hit.Params["mode"] != "scale" {
		t.Errorf("Params.mode 应为 scale，实际=%v", hit.Params["mode"])
	}
	if hit.Result != "success" {
		t.Errorf("Result 应为 success，实际=%q", hit.Result)
	}
	if !hit.Mock {
		t.Error("Mock 模式下审计 Mock 字段应为 true")
	}
}

// 记录 Guard R2 拦截路径也应入账（便于事后"哪些被拒了"查询）。
func TestScale_HardLimitRejectionStillAudited(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	t.Setenv("AUDIT_DISABLE", "")

	mem := &audit.MemorySink{}
	old := audit.SetSink(mem)
	defer audit.SetSink(old)

	tl := newScaleTool(newMockScaleClient())

	_ = mustCallScale(t, tl, ScaleInput{
		Action:     "scale",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
		Replicas:   600,
		Confirmed:  true,
	})

	var foundRejected bool
	for _, line := range mem.Snapshot() {
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Action == "bcs.deployment.scale" && rec.Params["rejected_by"] == "hard_limit" {
			foundRejected = true
			if rec.Result != "failure" {
				t.Errorf("被 R2 拦截时 Result 应为 failure，实际=%q", rec.Result)
			}
		}
	}
	if !foundRejected {
		t.Fatal("R2 拦截必须留下 rejected_by=hard_limit 的审计记录")
	}
}

// -----------------------------------------------------------------------------
// 10) D19.6 wait_for_ready —— 复用 D19.5 ReadyWaiter
// -----------------------------------------------------------------------------

// recordingWaiter 记录被调用参数，用于断言 Waiter 被正确触发。
type recordingWaiter struct {
	called bool
	spec   ReadySpec
	ready  bool
	err    error
}

func (rw *recordingWaiter) Wait(_ context.Context, spec ReadySpec) (bool, error) {
	rw.called = true
	rw.spec = spec
	return rw.ready, rw.err
}

// TestScale_WaitForReadyFalse_DoesNotInvokeWaiter
// 验证默认关闭时 Waiter 绝不被调用 —— 不影响既有行为，零风险上线。
func TestScale_WaitForReadyFalse_DoesNotInvokeWaiter(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: true}
	tl := newScaleToolWithWaiter(newMockScaleClient(), rw)

	r := mustCallScale(t, tl, ScaleInput{
		Action:     "scale",
		ClusterID:  "BCS-K8S-00001",
		Namespace:  "letsgo",
		Deployment: "game-core",
		Replicas:   5,
		Confirmed:  true,
		// WaitForReady 默认 false
	})
	if !r.OK {
		t.Fatalf("应成功，msg=%s", r.Message)
	}
	if rw.called {
		t.Fatal("wait_for_ready=false 时 Waiter 绝不应被调用")
	}
	// 响应里仍应带 wait_for_ready 字段，值 attempted=false（便于统一 schema）
	data, _ := r.Data.(map[string]any)
	wait, ok := data["wait_for_ready"].(map[string]any)
	if !ok {
		t.Fatalf("响应应带 wait_for_ready 字段，实际 Data=%v", data)
	}
	if wait["attempted"] != false {
		t.Errorf("attempted 应为 false，实际=%v", wait["attempted"])
	}
}

// TestScale_WaitForReadyTrue_InvokesWaiterWithCorrectSpec
// 这是 D19.6 最重要的断言：Waiter 确实被调用，且 ReadySpec 字段正确传递。
func TestScale_WaitForReadyTrue_InvokesWaiterWithCorrectSpec(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: true}
	tl := newScaleToolWithWaiter(newMockScaleClient(), rw)

	r := mustCallScale(t, tl, ScaleInput{
		Action:       "scale",
		ClusterID:    "BCS-K8S-00001",
		Namespace:    "letsgo",
		Deployment:   "game-core",
		Replicas:     5,
		Confirmed:    true,
		WaitForReady: true,
	})
	if !r.OK {
		t.Fatalf("应成功，msg=%s", r.Message)
	}
	if !rw.called {
		t.Fatal("wait_for_ready=true 时 Waiter 必须被调用")
	}
	// 核心：Waiter 收到的 Mode 是 scale_deployment（区分于 pod_restart 的几种 mode，
	// 便于 observability 指标按工具维度分桶）
	if rw.spec.Mode != "scale_deployment" {
		t.Errorf("ReadySpec.Mode 应为 scale_deployment，实际=%q", rw.spec.Mode)
	}
	if rw.spec.Deployment != "game-core" {
		t.Errorf("ReadySpec.Deployment 传递错误：%q", rw.spec.Deployment)
	}
	if rw.spec.ClusterID != "BCS-K8S-00001" {
		t.Errorf("ReadySpec.ClusterID 传递错误：%q", rw.spec.ClusterID)
	}
	// 响应里 status 应为 ready
	data, _ := r.Data.(map[string]any)
	wait, _ := data["wait_for_ready"].(map[string]any)
	if wait["status"] != "ready" {
		t.Errorf("wait_for_ready.status 应为 ready，实际=%v", wait["status"])
	}
}

// TestScale_WaitForReadyError_DoesNotFailMainAction
// 关键语义：Waiter 报错（超时/cancelled）不能把 scale 本身的成功状态翻转成失败。
// scale 已经下发了 —— 即使后续等待失败，这次调用整体仍是"执行成功，只是没等到稳定"。
// 这符合 D19.4 定下的 timeout ≠ failure 原则。
func TestScale_WaitForReadyError_DoesNotFailMainAction(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: false, err: context.DeadlineExceeded}
	tl := newScaleToolWithWaiter(newMockScaleClient(), rw)

	r := mustCallScale(t, tl, ScaleInput{
		Action:       "scale",
		ClusterID:    "BCS-K8S-00001",
		Namespace:    "letsgo",
		Deployment:   "game-core",
		Replicas:     5,
		Confirmed:    true,
		WaitForReady: true,
	})
	// scale 动作本身成功
	if !r.OK {
		t.Fatalf("scale 本身成功（只是 wait 超时），不应翻转 OK。msg=%s", r.Message)
	}
	// 但 wait_for_ready 应显式标 timeout
	data, _ := r.Data.(map[string]any)
	wait, _ := data["wait_for_ready"].(map[string]any)
	if wait["status"] != "timeout" {
		t.Errorf("wait_for_ready.status 应为 timeout，实际=%v", wait["status"])
	}
}

// TestScale_WaitForReadyRelative_AlsoTriggers
// scale_relative 路径也应走 wait_for_ready（对称性测试）。
func TestScale_WaitForReadyRelative_AlsoTriggers(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	rw := &recordingWaiter{ready: true}
	tl := newScaleToolWithWaiter(newMockScaleClient(), rw)

	r := mustCallScale(t, tl, ScaleInput{
		Action:       "scale_relative",
		ClusterID:    "BCS-K8S-00001",
		Namespace:    "letsgo",
		Deployment:   "game-core",
		Delta:        2,
		Confirmed:    true,
		WaitForReady: true,
	})
	if !r.OK {
		t.Fatalf("应成功，msg=%s", r.Message)
	}
	if !rw.called {
		t.Fatal("scale_relative 路径也必须触发 Waiter")
	}
}

// TestScale_GetAction_NeverWaits
// 纯读 action=get 即使传 wait_for_ready=true 也绝不应调用 Waiter
// ——等待一个查询操作毫无意义，且会污染 observability 指标。
func TestScale_GetAction_NeverWaits(t *testing.T) {
	rw := &recordingWaiter{ready: true}
	tl := newScaleToolWithWaiter(newMockScaleClient(), rw)

	_ = mustCallScale(t, tl, ScaleInput{
		Action:       "get",
		ClusterID:    "BCS-K8S-00001",
		Namespace:    "letsgo",
		Deployment:   "game-core",
		WaitForReady: true, // 故意打开以验证不生效
	})
	if rw.called {
		t.Fatal("action=get 下 Waiter 永远不应被调用（纯读操作不需等待）")
	}
}