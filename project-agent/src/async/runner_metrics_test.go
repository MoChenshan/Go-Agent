// runner_metrics_test.go D19.4 — MetricsHook 行为单测。
//
// 这些测试不做 Otel 集成，只验证 Runner 是否在**正确时机**以**正确参数**调用
// MetricsHook。真实 Otel 打点由 observability 包的适配器测试负责。
package async

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// recordingHook 记录所有调用，供断言使用。
type recordingHook struct {
	mu        sync.Mutex
	submits   []submitCall
	finishes  []finishCall
}

type submitCall struct{ tool, outcome string }
type finishCall struct {
	tool, status string
	total        time.Duration
}

func (r *recordingHook) OnSubmit(tool, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.submits = append(r.submits, submitCall{tool, outcome})
}

func (r *recordingHook) OnFinish(tool, status string, total time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishes = append(r.finishes, finishCall{tool, status, total})
}

func (r *recordingHook) submitSnapshot() []submitCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]submitCall, len(r.submits))
	copy(out, r.submits)
	return out
}

func (r *recordingHook) finishSnapshot() []finishCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]finishCall, len(r.finishes))
	copy(out, r.finishes)
	return out
}

// TestMetricsHook_AcceptedAndSucceeded 场景 A：正常流程。
//
// 期望：OnSubmit(tool, accepted) 一次 + OnFinish(tool, succeeded, >0) 一次。
func TestMetricsHook_AcceptedAndSucceeded(t *testing.T) {
	hook := &recordingHook{}
	exec := ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		time.Sleep(30 * time.Millisecond) // 保证 duration > 0
		return "ok", nil
	})
	r := New(Config{
		MaxConcurrentJobs: 2,
		MaxQueuedJobs:     8,
		DefaultTimeout:    time.Second,
		JanitorInterval:   time.Hour,
		Metrics:           hook,
	}, NewMemStore(), exec)
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	id, err := r.Submit(context.Background(), "fake_tool", nil, 0, "")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	wctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := r.Wait(wctx, id); err != nil {
		t.Fatal(err)
	}

	// Submit 断言
	subs := hook.submitSnapshot()
	if len(subs) != 1 || subs[0].tool != "fake_tool" || subs[0].outcome != "accepted" {
		t.Errorf("submits 不符：%+v", subs)
	}
	// Finish 断言
	fins := hook.finishSnapshot()
	if len(fins) != 1 {
		t.Fatalf("finishes 期望 1 条，实际 %d", len(fins))
	}
	if fins[0].tool != "fake_tool" || fins[0].status != "succeeded" {
		t.Errorf("finish 字段不符：%+v", fins[0])
	}
	if fins[0].total < 30*time.Millisecond {
		t.Errorf("total duration 应 >= 30ms，实际 %s", fins[0].total)
	}
}

// TestMetricsHook_DedupHit 场景 B：幂等命中。
//
// 期望：第二次 Submit 应观测到 dedup_hit，且不再触发 Finished。
func TestMetricsHook_DedupHit(t *testing.T) {
	hook := &recordingHook{}
	block := make(chan struct{})
	exec := ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-block
		return "ok", nil
	})
	r := New(Config{
		MaxConcurrentJobs: 2,
		MaxQueuedJobs:     8,
		DefaultTimeout:    time.Second,
		JanitorInterval:   time.Hour,
		Metrics:           hook,
	}, NewMemStore(), exec)
	t.Cleanup(func() {
		close(block)
		_ = r.Shutdown(context.Background())
	})

	id1, err := r.Submit(context.Background(), "dedup_tool", nil, 0, "KEY-42")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := r.Submit(context.Background(), "dedup_tool", nil, 0, "KEY-42")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("幂等键命中应复用 ID，但得到不同 ID: %s vs %s", id1, id2)
	}
	subs := hook.submitSnapshot()
	if len(subs) != 2 {
		t.Fatalf("submits 期望 2 条，实际 %d", len(subs))
	}
	if subs[0].outcome != "accepted" {
		t.Errorf("第 1 次应 accepted，实际 %s", subs[0].outcome)
	}
	if subs[1].outcome != "dedup_hit" {
		t.Errorf("第 2 次应 dedup_hit，实际 %s", subs[1].outcome)
	}
}

// TestMetricsHook_Rejected 场景 C：队列满被拒绝。
func TestMetricsHook_Rejected(t *testing.T) {
	hook := &recordingHook{}
	block := make(chan struct{})
	exec := ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-block
		return "done", nil
	})
	r := New(Config{
		MaxConcurrentJobs: 1,
		MaxQueuedJobs:     1, // 极小容量
		DefaultTimeout:    5 * time.Second,
		JanitorInterval:   time.Hour,
		Metrics:           hook,
	}, NewMemStore(), exec)
	t.Cleanup(func() {
		close(block)
		_ = r.Shutdown(context.Background())
	})

	if _, err := r.Submit(context.Background(), "foo", nil, 0, ""); err != nil {
		t.Fatal(err)
	}
	// 第二次：必然拒绝
	if _, err := r.Submit(context.Background(), "foo", nil, 0, ""); !errors.Is(err, ErrTooManyJobs) {
		t.Fatalf("期望 ErrTooManyJobs，实际 %v", err)
	}

	subs := hook.submitSnapshot()
	if len(subs) != 2 {
		t.Fatalf("submits 期望 2 条，实际 %d", len(subs))
	}
	if subs[0].outcome != "accepted" || subs[1].outcome != "rejected" {
		t.Errorf("outcome 序列不符：%+v", subs)
	}
}

// TestMetricsHook_TimedOut 场景 D：超时状态区分于 cancelled。
func TestMetricsHook_TimedOut(t *testing.T) {
	hook := &recordingHook{}
	exec := ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	r := New(Config{
		MaxConcurrentJobs: 1,
		MaxQueuedJobs:     4,
		DefaultTimeout:    time.Second,
		MaxTimeout:        time.Second,
		JanitorInterval:   time.Hour,
		Metrics:           hook,
	}, NewMemStore(), exec)
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	id, err := r.Submit(context.Background(), "hang", nil, 80*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := r.Wait(wctx, id); err != nil {
		t.Fatal(err)
	}

	fins := hook.finishSnapshot()
	if len(fins) != 1 {
		t.Fatalf("finishes 期望 1 条，实际 %d", len(fins))
	}
	if fins[0].status != "timed_out" {
		t.Errorf("status 应为 timed_out，实际 %s（区分于 cancelled 是监控面板告警规则的关键）", fins[0].status)
	}
}

// TestMetricsHook_NoHookNoPanics 场景 E：Config.Metrics=nil 时仍可工作。
//
// 保障：默认 noopMetrics 自动注入，调用方不配 Metrics 时不应 panic。
func TestMetricsHook_NoHookNoPanics(t *testing.T) {
	exec := ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return "ok", nil
	})
	r := New(Config{
		MaxConcurrentJobs: 1,
		MaxQueuedJobs:     4,
		DefaultTimeout:    time.Second,
		JanitorInterval:   time.Hour,
		// 故意不设 Metrics
	}, NewMemStore(), exec)
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	id, err := r.Submit(context.Background(), "t", nil, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := r.Wait(wctx, id); err != nil {
		t.Fatal(err)
	}
	// 能走到这里不 panic 即过
}
