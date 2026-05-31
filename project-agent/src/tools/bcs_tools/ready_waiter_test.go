// ready_waiter_test.go D19.5 —— ReadyWaiter 行为单测。
//
// 测试策略：
//   - isDeploymentReady 的判据矩阵用纯 map 输入，不起任何 goroutine
//   - Waiter 行为（interval/timeout/jitter）注入 fake Clock + SleepFunc
//   - 不起真 bcsapi，而是 Mock 模式短路路径 + 判据函数单独验证
//
// 这里不做"真的 HTTP 轮询"端到端——那是集成测试的范畴（超出本轮）。
package bcstools

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// ---- isDeploymentReady 判据矩阵 -----------------------------------------------

func TestIsDeploymentReady_AllConditionsMet(t *testing.T) {
	obj := map[string]any{
		"data": []any{
			map[string]any{
				"data": map[string]any{
					"metadata": map[string]any{"generation": 3.0},
					"spec":     map[string]any{"replicas": 3.0},
					"status": map[string]any{
						"observedGeneration": 3.0,
						"updatedReplicas":    3.0,
						"readyReplicas":      3.0,
					},
				},
			},
		},
	}
	if !isDeploymentReady(obj) {
		t.Fatal("三条件全部满足应返回 true")
	}
}

func TestIsDeploymentReady_ObservedGenerationStale(t *testing.T) {
	// Deployment spec 刚改过（generation=4），但 controller 还没追上（observedGen=3）
	obj := map[string]any{
		"metadata": map[string]any{"generation": 4.0},
		"spec":     map[string]any{"replicas": 3.0},
		"status": map[string]any{
			"observedGeneration": 3.0, // 落后
			"updatedReplicas":    3.0,
			"readyReplicas":      3.0,
		},
	}
	if isDeploymentReady(obj) {
		t.Fatal("observedGeneration 落后时应返回 false")
	}
}

func TestIsDeploymentReady_UpdatedLagging(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{"generation": 3.0},
		"spec":     map[string]any{"replicas": 3.0},
		"status": map[string]any{
			"observedGeneration": 3.0,
			"updatedReplicas":    2.0, // 还有 1 个旧 Pod
			"readyReplicas":      3.0,
		},
	}
	if isDeploymentReady(obj) {
		t.Fatal("updatedReplicas < desired 时应返回 false")
	}
}

func TestIsDeploymentReady_ReadyLagging(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{"generation": 3.0},
		"spec":     map[string]any{"replicas": 3.0},
		"status": map[string]any{
			"observedGeneration": 3.0,
			"updatedReplicas":    3.0,
			"readyReplicas":      1.0, // readiness probe 还没通过
		},
	}
	if isDeploymentReady(obj) {
		t.Fatal("readyReplicas < desired 时应返回 false")
	}
}

func TestIsDeploymentReady_NilResponse(t *testing.T) {
	if isDeploymentReady(nil) {
		t.Fatal("nil 响应应视为未就绪")
	}
	if isDeploymentReady(map[string]any{}) {
		t.Fatal("空 map 应视为未就绪")
	}
	if isDeploymentReady(map[string]any{"data": []any{}}) {
		t.Fatal("空 data 数组应视为未就绪")
	}
}

// ---- Waiter 行为 --------------------------------------------------------------

// TestNoopReadyWaiter_ImmediateReturn Noop 必须瞬时返回 ready。
func TestNoopReadyWaiter_ImmediateReturn(t *testing.T) {
	w := NewNoopReadyWaiter()
	ready, err := w.Wait(context.Background(), ReadySpec{Mode: "delete_pod"})
	if err != nil {
		t.Fatalf("noop 不应出错：%v", err)
	}
	if !ready {
		t.Fatal("noop 必须返回 ready=true")
	}
}

// TestRunReadyWait_Ready 正向：Waiter 返 true 时 info.status=ready。
func TestRunReadyWait_Ready(t *testing.T) {
	info := runReadyWait(context.Background(), NewNoopReadyWaiter(), ReadySpec{
		Mode:       "rollout_restart",
		Deployment: "game-core",
	})
	if info["status"] != "ready" {
		t.Errorf("status 期望 ready，实际 %v", info["status"])
	}
	if info["attempted"] != true {
		t.Error("attempted 应为 true")
	}
	if info["ready"] != true {
		t.Error("ready 字段应为 true")
	}
	if info["deployment"] != "game-core" {
		t.Error("deployment 字段应回传")
	}
}

// fakeWaiter 可控返回值。
type fakeWaiter struct {
	ready bool
	err   error
	calls atomic.Int32
}

func (f *fakeWaiter) Wait(ctx context.Context, spec ReadySpec) (bool, error) {
	f.calls.Add(1)
	return f.ready, f.err
}

// TestRunReadyWait_TimeoutVsCancelled 关键语义区分：timeout vs cancelled。
//
// 这是 D19.4 明确过的取舍：两者绝不能混淆，否则告警 GameOpsAsyncTimeoutRatioHigh 失信。
func TestRunReadyWait_TimeoutVsCancelled(t *testing.T) {
	cases := []struct {
		name       string
		waiterErr  error
		wantStatus string
	}{
		{"deadline_exceeded", context.DeadlineExceeded, "timeout"},
		{"ctx_cancelled", context.Canceled, "cancelled"},
		{"wrapped_deadline", fmt.Errorf("outer: %w", context.DeadlineExceeded), "timeout"},
		{"unknown_error", errors.New("bcs api 500"), "error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fw := &fakeWaiter{ready: false, err: c.waiterErr}
			info := runReadyWait(context.Background(), fw, ReadySpec{Mode: "delete_pod"})
			if info["status"] != c.wantStatus {
				t.Errorf("status 期望 %s，实际 %v", c.wantStatus, info["status"])
			}
		})
	}
}

// TestRunReadyWait_NilWaiter nil Waiter 不 panic。
func TestRunReadyWait_NilWaiter(t *testing.T) {
	info := runReadyWait(context.Background(), nil, ReadySpec{Mode: "delete_pod"})
	if info["status"] != "skipped" {
		t.Errorf("nil waiter 应返回 skipped，实际 %v", info["status"])
	}
	if info["attempted"] != false {
		t.Error("nil waiter 下 attempted 应为 false")
	}
}

// TestEvictWaitInfo_SkippedWhenZeroSuccess successCount=0 时不应真正 wait。
func TestEvictWaitInfo_SkippedWhenZeroSuccess(t *testing.T) {
	fw := &fakeWaiter{ready: true, err: nil}
	info := evictWaitInfo(context.Background(), fw, PodRestartInput{
		WaitForReady: true,
		Deployment:   "dep",
	}, 0)
	if info["attempted"] != false {
		t.Error("0 成功时不应 wait")
	}
	if fw.calls.Load() != 0 {
		t.Error("Waiter 不应被调用")
	}
}

// TestEvictWaitInfo_SkippedWhenFlagOff WaitForReady=false 时不应 wait。
func TestEvictWaitInfo_SkippedWhenFlagOff(t *testing.T) {
	fw := &fakeWaiter{ready: true, err: nil}
	info := evictWaitInfo(context.Background(), fw, PodRestartInput{
		WaitForReady: false,
		Deployment:   "dep",
	}, 3)
	if info["attempted"] != false {
		t.Error("WaitForReady=false 时不应 wait")
	}
	if fw.calls.Load() != 0 {
		t.Error("Waiter 不应被调用")
	}
}

// TestEvictWaitInfo_HappyPath WaitForReady=true 且至少 1 个成功。
func TestEvictWaitInfo_HappyPath(t *testing.T) {
	fw := &fakeWaiter{ready: true, err: nil}
	info := evictWaitInfo(context.Background(), fw, PodRestartInput{
		WaitForReady: true,
		Deployment:   "dep",
	}, 2)
	if info["status"] != "ready" {
		t.Errorf("status 应为 ready，实际 %v", info["status"])
	}
	if fw.calls.Load() != 1 {
		t.Errorf("Waiter 应被调用 1 次，实际 %d", fw.calls.Load())
	}
}

// ---- withJitter --------------------------------------------------------------

func TestWithJitter_NoRatio(t *testing.T) {
	d := withJitter(time.Second, 0)
	if d != time.Second {
		t.Errorf("ratio=0 时应原样返回，得到 %s", d)
	}
}

func TestWithJitter_StaysInRange(t *testing.T) {
	// 跑 200 次，确保所有结果都在 [80%, 120%] 范围内
	base := 100 * time.Millisecond
	for i := 0; i < 200; i++ {
		d := withJitter(base, 0.2)
		low := 80 * time.Millisecond
		high := 120 * time.Millisecond
		if d < low || d > high {
			t.Errorf("jitter 超出范围：%s（允许 %s~%s）", d, low, high)
		}
	}
}

func TestWithJitter_NeverNegative(t *testing.T) {
	// 极端 ratio；验证不会返回负值
	for i := 0; i < 100; i++ {
		d := withJitter(time.Nanosecond, 0.5)
		if d < 0 {
			t.Errorf("jitter 返回负值：%s", d)
		}
	}
}
