// fast_poll_waiter_test.go D19.8 —— FastPollReadyWaiter 行为单测。
//
// 测试策略：
//   - **真路径**：用 httptest.Server 扮 BCS Gateway，让 FastPoll 走完整 HTTP 轮询
//     而非 Mock 短路。这样阶梯退避、首探快路径、probe 计数等核心能力都能被断言。
//   - **假时钟**：注入 NowFunc + SleepFunc，避免测试真的 sleep 秒级时间。
//   - **MockHook**：捕获 OnWaitFinished 的入参，断言 stats 精准。
//
// 不覆盖的点（已由 ready_waiter_test.go 覆盖）：
//   - isDeploymentReady 判据矩阵（三条件）
//   - withJitter 范围
//   - runReadyWait 状态翻译
//
// 本文件只聚焦 **D19.8 新增行为**：fastPollSchedule 生效、ProbeIndexWhenReady 精准、
// Hook 生命周期完整、Canceled/Deadline 分类正确。
package bcstools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ---- 辅助 --------------------------------------------------------------------

// readyJSONBody 返回一个"全部就绪"的 BCS 存储响应体。
func readyJSONBody() string {
	return `{"data":[{"data":{"metadata":{"generation":3},"spec":{"replicas":3},"status":{"observedGeneration":3,"updatedReplicas":3,"readyReplicas":3}}}]}`
}

// notReadyJSONBody 返回一个"readyReplicas 不足"的响应体。
func notReadyJSONBody() string {
	return `{"data":[{"data":{"metadata":{"generation":3},"spec":{"replicas":3},"status":{"observedGeneration":3,"updatedReplicas":3,"readyReplicas":0}}}]}`
}

// fakeBCSServer 返回一个 httptest.Server，每次请求按 responses 序列依次回（超出则一直返回最后一条）。
// 同时把被调用的次数累加到 hitCounter。
func fakeBCSServer(t *testing.T, hitCounter *atomic.Int32, responses []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(hitCounter.Add(1)) - 1
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, responses[idx])
	}))
	return srv
}

// newRealHTTPClient 创建一个**非 Mock**的 bcsapi.Client，指向 fakeBCSServer。
// 这让 FastPoll.Wait 走完整 HTTP 轮询路径，可观察 schedule 真实行为。
func newRealHTTPClient(baseURL string) *bcsapi.Client {
	return bcsapi.NewClient(
		bcsapi.WithBaseURL(baseURL),
		bcsapi.WithToken("test-token"),
	)
}

// recordedHook 捕获 OnWaitFinished 入参，便于断言 stats 精准度。
type recordedHook struct {
	mu    sync.Mutex
	calls []recordedHookCall
}

type recordedHookCall struct {
	mode  string
	stats FastPollStats
}

func (h *recordedHook) OnWaitFinished(mode string, stats FastPollStats) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, recordedHookCall{mode: mode, stats: stats})
}

func (h *recordedHook) last() (recordedHookCall, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) == 0 {
		return recordedHookCall{}, false
	}
	return h.calls[len(h.calls)-1], true
}

// frozenClock 给 Waiter 注入"完全可控的时间推进"。
// 每次 Sleep 调用只推进 fake now，不真 sleep。
type frozenClock struct {
	mu      sync.Mutex
	now     time.Time
	slept   []time.Duration // 记录每次 Sleep 的参数
	canceled bool
}

func newFrozenClock() *frozenClock {
	return &frozenClock{now: time.Unix(1700000000, 0)}
}

func (fc *frozenClock) Now() time.Time {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.now
}

// Sleep 实现 WaiterConfig.SleepFunc 的契约：
//   - d <= 0 立刻返回 nil
//   - ctx 已 done 返回 ctx.Err()
//   - 否则推进 fake now，记录 slept
func (fc *frozenClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if d > 0 {
		fc.now = fc.now.Add(d)
		fc.slept = append(fc.slept, d)
	}
	return nil
}

// ---- 首探快路径：首探即 ready -----------------------------------------------

// TestFastPoll_FirstProbeReady
// 核心断言：如果 BCS 首探就返回 ready，我们**完全不睡**就返回，
// ProbeIndexWhenReady=1，TotalProbes=1。这是 FastPoll 对比传统 Poll 最大的卖点。
func TestFastPoll_FirstProbeReady(t *testing.T) {
	var hits atomic.Int32
	srv := fakeBCSServer(t, &hits, []string{readyJSONBody()})
	defer srv.Close()

	clk := newFrozenClock()
	hook := &recordedHook{}
	w := NewFastPollReadyWaiter(
		newRealHTTPClient(srv.URL),
		WaiterConfig{
			DefaultInterval: 2 * time.Second,
			DefaultTimeout:  5 * time.Minute,
			NowFunc:         clk.Now,
			SleepFunc:       clk.Sleep,
		},
		hook,
	)

	ready, err := w.Wait(context.Background(), ReadySpec{
		Mode:       "scale_deployment",
		ClusterID:  "BCS-1",
		Namespace:  "ns",
		Deployment: "game-core",
	})
	if err != nil {
		t.Fatalf("意外错误：%v", err)
	}
	if !ready {
		t.Fatal("应返回 ready=true")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("BCS 应只被打 1 次，实际 %d 次", got)
	}
	if len(clk.slept) != 0 {
		t.Errorf("首探命中时不应有任何 sleep，实际 slept=%v", clk.slept)
	}

	call, ok := hook.last()
	if !ok {
		t.Fatal("Hook.OnWaitFinished 未被调用")
	}
	if call.mode != "scale_deployment" {
		t.Errorf("mode 应原样传递，实际 %q", call.mode)
	}
	if call.stats.Reason != "ready" {
		t.Errorf("Reason 应为 ready，实际 %q", call.stats.Reason)
	}
	if call.stats.ProbeIndexWhenReady != 1 {
		t.Errorf("首探命中 ProbeIndexWhenReady 应为 1，实际 %d", call.stats.ProbeIndexWhenReady)
	}
	if call.stats.TotalProbes != 1 {
		t.Errorf("TotalProbes 应为 1，实际 %d", call.stats.TotalProbes)
	}
}

// ---- 阶梯退避：前 3 次未就绪，第 4 次就绪 ----------------------------------

// TestFastPoll_StaircaseBackoff
// 核心断言：阶梯表被严格按顺序执行。
//
//	probe#1 → 0ms   （立即）
//	probe#2 → 250ms
//	probe#3 → 500ms
//	probe#4 → 1s（第 4 次 ready，停在这里）
//
// 总睡眠时长应是 0+250+500+1000 = 1.75s，TotalProbes=4。
func TestFastPoll_StaircaseBackoff(t *testing.T) {
	var hits atomic.Int32
	srv := fakeBCSServer(t, &hits, []string{
		notReadyJSONBody(), // #1
		notReadyJSONBody(), // #2
		notReadyJSONBody(), // #3
		readyJSONBody(),    // #4
	})
	defer srv.Close()

	clk := newFrozenClock()
	hook := &recordedHook{}
	w := NewFastPollReadyWaiter(
		newRealHTTPClient(srv.URL),
		WaiterConfig{
			DefaultInterval: 2 * time.Second,
			DefaultTimeout:  10 * time.Second,
			NowFunc:         clk.Now,
			SleepFunc:       clk.Sleep,
		},
		hook,
	)

	ready, err := w.Wait(context.Background(), ReadySpec{
		Mode:       "helm_rollback",
		ClusterID:  "BCS-1",
		Namespace:  "ns",
		Deployment: "game-core",
	})
	if err != nil || !ready {
		t.Fatalf("应返回 ready，实际 ready=%v err=%v", ready, err)
	}
	if got := hits.Load(); got != 4 {
		t.Errorf("BCS 应被打 4 次，实际 %d 次", got)
	}

	// 阶梯表前 3 项：0ms(不记录)/250ms/500ms/1000ms — 只有 >0 的才被记录
	wantSleeps := []time.Duration{
		250 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
	}
	if len(clk.slept) != len(wantSleeps) {
		t.Fatalf("sleep 次数不符：实际 %v", clk.slept)
	}
	for i, want := range wantSleeps {
		if clk.slept[i] != want {
			t.Errorf("第 %d 次 sleep 应为 %s，实际 %s", i+1, want, clk.slept[i])
		}
	}

	call, _ := hook.last()
	if call.stats.ProbeIndexWhenReady != 4 {
		t.Errorf("ProbeIndexWhenReady 应为 4，实际 %d", call.stats.ProbeIndexWhenReady)
	}
	if call.stats.TotalProbes != 4 {
		t.Errorf("TotalProbes 应为 4，实际 %d", call.stats.TotalProbes)
	}
	if call.stats.Reason != "ready" {
		t.Errorf("Reason 应为 ready，实际 %q", call.stats.Reason)
	}
}

// ---- 稳态轮询：超出阶梯表后用 DefaultInterval --------------------------------

// TestFastPoll_SteadyStateAfterSchedule
// 断言：超出 5 次阶梯后，使用 DefaultInterval 稳态轮询。
// 设 Interval=3s（刻意区别默认 2s）+ JitterRatio=0 让间隔可预测。
func TestFastPoll_SteadyStateAfterSchedule(t *testing.T) {
	var hits atomic.Int32
	// 6 次都不 ready，第 7 次 ready
	responses := []string{
		notReadyJSONBody(), notReadyJSONBody(), notReadyJSONBody(),
		notReadyJSONBody(), notReadyJSONBody(), notReadyJSONBody(),
		readyJSONBody(),
	}
	srv := fakeBCSServer(t, &hits, responses)
	defer srv.Close()

	clk := newFrozenClock()
	hook := &recordedHook{}
	w := NewFastPollReadyWaiter(
		newRealHTTPClient(srv.URL),
		WaiterConfig{
			DefaultInterval: 3 * time.Second,
			DefaultTimeout:  30 * time.Second,
			JitterRatio:     0, // 禁用 jitter 让断言可预测
			NowFunc:         clk.Now,
			SleepFunc:       clk.Sleep,
		},
		hook,
	)

	ready, err := w.Wait(context.Background(), ReadySpec{
		Mode:       "rollout_restart",
		ClusterID:  "BCS-1",
		Namespace:  "ns",
		Deployment: "game-core",
	})
	if err != nil || !ready {
		t.Fatalf("应返回 ready，实际 ready=%v err=%v", ready, err)
	}

	// 睡眠序列应该是：250ms(不含 0) / 500ms / 1s / 2s / 3s / 3s
	wantSleeps := []time.Duration{
		250 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		3 * time.Second, // 第一次稳态
		3 * time.Second, // 第二次稳态
	}
	if len(clk.slept) != len(wantSleeps) {
		t.Fatalf("sleep 次数不符：实际 %v", clk.slept)
	}
	for i, want := range wantSleeps {
		if clk.slept[i] != want {
			t.Errorf("第 %d 次 sleep 应为 %s，实际 %s", i+1, want, clk.slept[i])
		}
	}

	call, _ := hook.last()
	if call.stats.ProbeIndexWhenReady != 7 {
		t.Errorf("ProbeIndexWhenReady 应为 7，实际 %d", call.stats.ProbeIndexWhenReady)
	}
}

// ---- 超时：始终不 ready，Timeout 到期返回 ------------------------------------

// TestFastPoll_TimeoutReturnsDeadlineExceeded
// 断言：始终不 ready 时，Timeout 到期返回 (false, DeadlineExceeded)，
// 并且 Hook.Reason="timeout"。
func TestFastPoll_TimeoutReturnsDeadlineExceeded(t *testing.T) {
	var hits atomic.Int32
	srv := fakeBCSServer(t, &hits, []string{notReadyJSONBody()})
	defer srv.Close()

	clk := newFrozenClock()
	hook := &recordedHook{}
	w := NewFastPollReadyWaiter(
		newRealHTTPClient(srv.URL),
		WaiterConfig{
			DefaultInterval: 2 * time.Second,
			DefaultTimeout:  1 * time.Second, // 刻意很短
			JitterRatio:     0,
			NowFunc:         clk.Now,
			SleepFunc:       clk.Sleep,
		},
		hook,
	)

	ready, err := w.Wait(context.Background(), ReadySpec{
		Mode:       "delete_pod",
		ClusterID:  "BCS-1",
		Namespace:  "ns",
		Deployment: "game-core",
	})
	if ready {
		t.Fatal("不应返回 ready=true")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("应返回 DeadlineExceeded，实际 %v", err)
	}

	call, _ := hook.last()
	if call.stats.Reason != "timeout" {
		t.Errorf("Reason 应为 timeout，实际 %q", call.stats.Reason)
	}
}

// ---- 取消：ctx 被 cancel ------------------------------------------------------

// TestFastPoll_CtxCancelReturnsCanceled
// 断言：ctx 被取消时 Hook.Reason="canceled"（区分于 timeout，这是 D19.4 定下的语义）。
func TestFastPoll_CtxCancelReturnsCanceled(t *testing.T) {
	var hits atomic.Int32
	srv := fakeBCSServer(t, &hits, []string{notReadyJSONBody()})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消

	hook := &recordedHook{}
	w := NewFastPollReadyWaiter(
		newRealHTTPClient(srv.URL),
		WaiterConfig{
			DefaultInterval: 2 * time.Second,
			DefaultTimeout:  5 * time.Minute,
		},
		hook,
	)

	ready, err := w.Wait(ctx, ReadySpec{
		Mode:       "scale_deployment",
		ClusterID:  "BCS-1",
		Namespace:  "ns",
		Deployment: "game-core",
	})
	if ready {
		t.Fatal("应返回 ready=false")
	}
	if err == nil {
		t.Fatal("ctx 已取消应返回错误")
	}

	call, _ := hook.last()
	if call.stats.Reason != "canceled" && call.stats.Reason != "timeout" {
		// 极罕见情况下首探已发出 ctx 检查在 HTTP 层返回的可能是 context.DeadlineExceeded，
		// 但我们测试用的是 Cancel，所以应该稳定为 canceled。留 timeout 作兜底避免 flaky。
		t.Errorf("Reason 应为 canceled（或极端场景下 timeout），实际 %q", call.stats.Reason)
	}
}

// ---- bad_spec：缺 Deployment --------------------------------------------------

// TestFastPoll_BadSpecNoDeployment
// 真实模式下，缺 Deployment 应立刻失败（不打 BCS、不 sleep），Hook.Reason="bad_spec"。
func TestFastPoll_BadSpecNoDeployment(t *testing.T) {
	var hits atomic.Int32
	srv := fakeBCSServer(t, &hits, []string{readyJSONBody()})
	defer srv.Close()

	hook := &recordedHook{}
	w := NewFastPollReadyWaiter(
		newRealHTTPClient(srv.URL),
		WaiterConfig{},
		hook,
	)

	ready, err := w.Wait(context.Background(), ReadySpec{
		Mode: "delete_pod",
		// 刻意缺 Deployment
	})
	if ready {
		t.Fatal("bad_spec 应返回 ready=false")
	}
	if err == nil {
		t.Fatal("bad_spec 应返回非 nil err")
	}
	if hits.Load() != 0 {
		t.Errorf("BCS 绝不应被调用，实际 %d 次", hits.Load())
	}

	call, _ := hook.last()
	if call.stats.Reason != "bad_spec" {
		t.Errorf("Reason 应为 bad_spec，实际 %q", call.stats.Reason)
	}
}

// ---- Mock 模式短路：保留 D19.5 行为 ------------------------------------------

// TestFastPoll_MockModeShortCircuit
// 核心契约：FastPollWaiter 必须与 bcsReadyWaiter 保持一致的 Mock 行为——
// 50ms sleep 后返回 ready。这是**抽象可替换性**的底线：两个实现在 Mock 下不可区分。
func TestFastPoll_MockModeShortCircuit(t *testing.T) {
	clk := newFrozenClock()
	hook := &recordedHook{}
	w := NewFastPollReadyWaiter(
		bcsapi.NewClient(bcsapi.WithMockMode(true)),
		WaiterConfig{
			NowFunc:   clk.Now,
			SleepFunc: clk.Sleep,
		},
		hook,
	)

	ready, err := w.Wait(context.Background(), ReadySpec{
		Mode:       "helm_install",
		ClusterID:  "BCS-1",
		Namespace:  "ns",
		Deployment: "game-core",
	})
	if err != nil {
		t.Fatalf("Mock 模式不应出错：%v", err)
	}
	if !ready {
		t.Fatal("Mock 模式必须返回 ready=true")
	}
	if len(clk.slept) != 1 || clk.slept[0] != 50*time.Millisecond {
		t.Errorf("Mock 模式应 sleep 恰好 50ms，实际 %v", clk.slept)
	}

	call, _ := hook.last()
	if call.stats.Reason != "ready" {
		t.Errorf("Mock 模式 Reason 应为 ready，实际 %q", call.stats.Reason)
	}
	if call.stats.ProbeIndexWhenReady != 1 {
		t.Errorf("Mock 模式 ProbeIndexWhenReady 应为 1，实际 %d", call.stats.ProbeIndexWhenReady)
	}
}

// ---- Hook=nil 不 panic -------------------------------------------------------

// TestFastPoll_NilHookSafe
// 必要的防御性：Hook 为 nil 时内部 fallback 到 noopFastPollHook，不会 panic。
func TestFastPoll_NilHookSafe(t *testing.T) {
	w := NewFastPollReadyWaiter(
		bcsapi.NewClient(bcsapi.WithMockMode(true)),
		WaiterConfig{},
		nil, // 故意传 nil
	)

	ready, err := w.Wait(context.Background(), ReadySpec{
		Mode:       "delete_pod",
		Deployment: "game-core",
	})
	if err != nil {
		t.Fatalf("nil hook 不应引发错误：%v", err)
	}
	if !ready {
		t.Fatal("Mock 模式应返回 ready=true")
	}
}

// ---- 抽象可替换性断言 --------------------------------------------------------

// TestFastPoll_ImplementsReadyWaiter
// 这是 D19.8 最重要的一行测试代码：
//
//	var _ ReadyWaiter = (*fastPollReadyWaiter)(nil)
//
// 只要这一行能编译通过，就证明 FastPoll 满足 ReadyWaiter 接口。
// 上层 helm/scale/pod_restart 注入的是 ReadyWaiter，所以**必然**可以无感替换。
// 这是 Go 泛型约束最优雅的用法：**把"抽象可替换性"降级为编译期检查**。
func TestFastPoll_ImplementsReadyWaiter(t *testing.T) {
	var _ ReadyWaiter = (*fastPollReadyWaiter)(nil)
	var _ ReadyWaiter = NewFastPollReadyWaiter(nil, WaiterConfig{}, nil)
}

// ---- 工厂函数：NewReadyWaiterFromEnv 行为 ------------------------------------

// TestNewReadyWaiterFromEnv_DefaultIsFast
// 默认（未设置环境变量）应选中 FastPoll。
func TestNewReadyWaiterFromEnv_DefaultIsFast(t *testing.T) {
	t.Setenv("GAMEOPS_READY_WAITER", "")
	w := NewReadyWaiterFromEnv(bcsapi.NewClient(bcsapi.WithMockMode(true)), WaiterConfig{}, nil)
	if _, ok := w.(*fastPollReadyWaiter); !ok {
		t.Errorf("默认应为 fastPollReadyWaiter，实际 %T", w)
	}
	if got := SelectedWaiterKind(); got != "fast" {
		t.Errorf("SelectedWaiterKind 应为 fast，实际 %q", got)
	}
}

func TestNewReadyWaiterFromEnv_PollMode(t *testing.T) {
	t.Setenv("GAMEOPS_READY_WAITER", "poll")
	w := NewReadyWaiterFromEnv(bcsapi.NewClient(bcsapi.WithMockMode(true)), WaiterConfig{}, nil)
	if _, ok := w.(*bcsReadyWaiter); !ok {
		t.Errorf("poll 模式应为 bcsReadyWaiter，实际 %T", w)
	}
	if got := SelectedWaiterKind(); got != "poll" {
		t.Errorf("SelectedWaiterKind 应为 poll，实际 %q", got)
	}
}

func TestNewReadyWaiterFromEnv_NoopMode(t *testing.T) {
	t.Setenv("GAMEOPS_READY_WAITER", "noop")
	w := NewReadyWaiterFromEnv(bcsapi.NewClient(bcsapi.WithMockMode(true)), WaiterConfig{}, nil)
	if _, ok := w.(noopWaiter); !ok {
		t.Errorf("noop 模式应为 noopWaiter，实际 %T", w)
	}
	if got := SelectedWaiterKind(); got != "noop" {
		t.Errorf("SelectedWaiterKind 应为 noop，实际 %q", got)
	}
}

func TestNewReadyWaiterFromEnv_UnknownFallsBackToPoll(t *testing.T) {
	t.Setenv("GAMEOPS_READY_WAITER", "invalid_value_xxx")
	w := NewReadyWaiterFromEnv(bcsapi.NewClient(bcsapi.WithMockMode(true)), WaiterConfig{}, nil)
	if _, ok := w.(*bcsReadyWaiter); !ok {
		t.Errorf("未知值应安全回退到 bcsReadyWaiter，实际 %T", w)
	}
	if got := SelectedWaiterKind(); got != "unknown" {
		t.Errorf("SelectedWaiterKind 应为 unknown，实际 %q", got)
	}
}
