// runner_test.go —— AsyncRunner 核心路径测试。
//
// 覆盖 5 大类场景（共 ~16 用例）：
//
//   S = Submit & 幂等
//     - 普通 submit 立刻返回 id
//     - 相同 idempotency_key 复用已有 job（不起新 worker）
//     - 空 tool_name 拒绝
//     - 超过 MaxQueuedJobs 返回 ErrTooManyJobs
//     - timeout 自动裁剪到 MaxTimeout
//
//   R = 运行与状态机
//     - 成功：pending→running→succeeded，Result 被保存
//     - 失败：executor 返回 err → failed，Err 字段被保存
//     - panic：executor panic → failed（含 ErrJobPanicked 语义）
//
//   T = 超时与取消
//     - watchdog 触发 → timed_out
//     - Cancel 活动中的 Job → cancelled
//     - Cancel 已终态 → ErrJobAlreadyTerminal
//
//   W = Wait
//     - Wait 成功等到终态
//     - Wait 对已终态 Job 立即返回
//     - Wait 在 ctx 过期时返回 ctx.Err()
//     - Wait 后 Cancel：Wait 能及时唤醒
//
//   L = 生命周期
//     - Shutdown 能在 workers 未完时超时返回
//     - Janitor 清理过期终态 Job
package async

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mkRunner 便捷构造带小默认值的 Runner（便于测试快跑）。
func mkRunner(t *testing.T, exec Executor) *Runner {
	t.Helper()
	r := New(Config{
		MaxConcurrentJobs: 4,
		MaxQueuedJobs:     8,
		DefaultTimeout:    2 * time.Second,
		MaxTimeout:        10 * time.Second,
		JanitorInterval:   500 * time.Millisecond,
		JanitorRetention:  50 * time.Millisecond,
	}, NewMemStore(), exec)
	t.Cleanup(func() {
		_ = r.Shutdown(context.Background())
	})
	return r
}

// waitForStatus 轮询至指定状态，超时 fail。
func waitForStatus(t *testing.T, r *Runner, id string, want JobStatus, timeout time.Duration) *Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		j, err := r.Store().Get(context.Background(), id)
		if err == nil && j.Status == want {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := r.Store().Get(context.Background(), id)
	if j != nil {
		t.Fatalf("等待状态 %q 超时；当前 %q，err=%s", want, j.Status, j.Err)
	} else {
		t.Fatalf("等待状态 %q 超时；job 不存在", want)
	}
	return nil
}

// ---- S：Submit & 幂等 --------------------------------------------------------

func TestSubmit_BasicSuccess(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return "ok:" + name, nil
	}))
	id, err := r.Submit(context.Background(), "foo", map[string]any{"x": 1}, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("id 不应为空")
	}
	j := waitForStatus(t, r, id, StatusSucceeded, time.Second)
	if j.Result != "ok:foo" {
		t.Errorf("result 错 %v", j.Result)
	}
	if j.StartedAt == nil || j.FinishedAt == nil {
		t.Error("终态下 StartedAt/FinishedAt 应非空")
	}
}

func TestSubmit_IdempotencyKey(t *testing.T) {
	var calls int
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		calls++
		return nil, nil
	}))
	id1, _ := r.Submit(context.Background(), "foo", nil, 0, "dedup-key")
	id2, _ := r.Submit(context.Background(), "foo", nil, 0, "dedup-key")
	if id1 != id2 {
		t.Fatalf("相同 idempotency_key 应返回同一 id；id1=%s id2=%s", id1, id2)
	}
	waitForStatus(t, r, id1, StatusSucceeded, time.Second)
	if calls != 1 {
		t.Errorf("executor 应仅被调 1 次，实际 %d", calls)
	}
}

func TestSubmit_EmptyToolName(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, nil
	}))
	if _, err := r.Submit(context.Background(), "", nil, 0, ""); err == nil {
		t.Fatal("空 tool_name 应返回 err")
	}
}

func TestSubmit_TooManyJobs(t *testing.T) {
	// 块 executor：保持 worker 不退出，占满队列
	block := make(chan struct{})
	r := New(Config{
		MaxConcurrentJobs: 2,
		MaxQueuedJobs:     3, // 很小，方便触发
		DefaultTimeout:    5 * time.Second,
	}, NewMemStore(), ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-block
		return nil, nil
	}))
	t.Cleanup(func() {
		close(block)
		_ = r.Shutdown(context.Background())
	})
	for i := 0; i < 3; i++ {
		if _, err := r.Submit(context.Background(), "foo", nil, 0, ""); err != nil {
			t.Fatalf("第 %d 次 submit 不应失败：%v", i, err)
		}
	}
	if _, err := r.Submit(context.Background(), "foo", nil, 0, ""); !errors.Is(err, ErrTooManyJobs) {
		t.Fatalf("第 4 次 submit 应 ErrTooManyJobs，实际=%v", err)
	}
}

func TestSubmit_TimeoutTruncated(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, nil
	}))
	// MaxTimeout = 10s；给 1 小时应被裁到 10s
	id, _ := r.Submit(context.Background(), "foo", nil, time.Hour, "")
	j, _ := r.Store().Get(context.Background(), id)
	if diff := j.TimeoutAt.Sub(j.SubmittedAt); diff > 11*time.Second {
		t.Errorf("timeout 应被裁到 ~10s，实际 %s", diff)
	}
}

// ---- R：运行与状态机 ---------------------------------------------------------

func TestRun_Failure(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, errors.New("boom")
	}))
	id, _ := r.Submit(context.Background(), "foo", nil, 0, "")
	j := waitForStatus(t, r, id, StatusFailed, time.Second)
	if j.Err != "boom" {
		t.Errorf("err 字段=%s", j.Err)
	}
}

func TestRun_Panic(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		panic("kaboom")
	}))
	id, _ := r.Submit(context.Background(), "foo", nil, 0, "")
	j := waitForStatus(t, r, id, StatusFailed, time.Second)
	if j.Err == "" || !errors.Is(errors.New(j.Err), ErrJobPanicked) && !contains(j.Err, "panicked") {
		t.Errorf("panic 应带 panicked 标记，实际=%s", j.Err)
	}
}

// contains 辅助子串判定（避免引 strings 包做额外动作，其实不必避；用 strings 没问题）
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---- T：超时与取消 -----------------------------------------------------------

func TestRun_Timeout(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	// 给一个超短 timeout
	id, _ := r.Submit(context.Background(), "foo", nil, 100*time.Millisecond, "")
	j := waitForStatus(t, r, id, StatusTimedOut, time.Second)
	if j.Err == "" {
		t.Error("timed_out 应带 err 描述")
	}
}

func TestRun_Cancel(t *testing.T) {
	started := make(chan struct{})
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	id, _ := r.Submit(context.Background(), "foo", nil, 5*time.Second, "")
	<-started // 等 worker 进入 running
	if err := r.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, r, id, StatusCancelled, time.Second)
}

func TestCancel_AlreadyTerminal(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, nil
	}))
	id, _ := r.Submit(context.Background(), "foo", nil, 0, "")
	waitForStatus(t, r, id, StatusSucceeded, time.Second)
	err := r.Cancel(context.Background(), id)
	if !errors.Is(err, ErrJobAlreadyTerminal) {
		t.Errorf("对终态 cancel 应返 ErrJobAlreadyTerminal，实际=%v", err)
	}
}

func TestCancel_NotFound(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, nil
	}))
	err := r.Cancel(context.Background(), "job_doesnotexist")
	if !errors.Is(err, ErrJobNotFound) {
		t.Errorf("cancel 不存在的 job 应返 ErrJobNotFound，实际=%v", err)
	}
}

// ---- W：Wait ----------------------------------------------------------------

func TestWait_Succeeded(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		time.Sleep(50 * time.Millisecond)
		return "done", nil
	}))
	id, _ := r.Submit(context.Background(), "foo", nil, 0, "")
	j, err := r.Wait(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != StatusSucceeded || j.Result != "done" {
		t.Errorf("Wait 结果不符：%+v", j)
	}
}

func TestWait_AlreadyTerminal(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return "quick", nil
	}))
	id, _ := r.Submit(context.Background(), "foo", nil, 0, "")
	waitForStatus(t, r, id, StatusSucceeded, time.Second)
	// 再 Wait 应立刻返回
	start := time.Now()
	j, err := r.Wait(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("已终态 Wait 应立刻返回，实际耗时 %s", elapsed)
	}
	if j.Status != StatusSucceeded {
		t.Errorf("status=%s", j.Status)
	}
}

func TestWait_CtxTimeout(t *testing.T) {
	block := make(chan struct{})
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-block
		return nil, nil
	}))
	t.Cleanup(func() { close(block) })
	id, _ := r.Submit(context.Background(), "foo", nil, 10*time.Second, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := r.Wait(ctx, id); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("应 ctx DeadlineExceeded，实际=%v", err)
	}
}

func TestWait_WakenByCancel(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	id, _ := r.Submit(context.Background(), "foo", nil, 10*time.Second, "")
	waitErrCh := make(chan error, 1)
	go func() {
		_, err := r.Wait(context.Background(), id)
		waitErrCh <- err
	}()
	time.Sleep(20 * time.Millisecond) // 让 Wait 进入 select
	_ = r.Cancel(context.Background(), id)
	select {
	case err := <-waitErrCh:
		if err != nil {
			t.Errorf("Wait 因 Cancel 唤醒后应成功取到终态：%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait 未被 Cancel 唤醒")
	}
}

// ---- L：生命周期 -------------------------------------------------------------

func TestShutdown_Timeout(t *testing.T) {
	block := make(chan struct{})
	r := New(Config{
		MaxConcurrentJobs: 2,
		MaxQueuedJobs:     4,
		DefaultTimeout:    5 * time.Second,
	}, NewMemStore(), ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-block
		return nil, nil
	}))
	t.Cleanup(func() { close(block) })
	_, _ = r.Submit(context.Background(), "foo", nil, 0, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown 应在 ctx 到期时返 DeadlineExceeded，实际=%v", err)
	}
}

func TestShutdown_Idempotent(t *testing.T) {
	r := mkRunner(t, ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, nil
	}))
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 第二次应 no-op 无 err
	if err := r.Shutdown(context.Background()); err != nil {
		t.Errorf("重复 Shutdown 应幂等，实际=%v", err)
	}
}

func TestJanitor_CleansOldTerminalJobs(t *testing.T) {
	r := New(Config{
		MaxConcurrentJobs: 2,
		MaxQueuedJobs:     4,
		DefaultTimeout:    time.Second,
		JanitorInterval:   30 * time.Millisecond,
		JanitorRetention:  30 * time.Millisecond,
	}, NewMemStore(), ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, nil
	}))
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })
	id, _ := r.Submit(context.Background(), "foo", nil, 0, "")
	waitForStatus(t, r, id, StatusSucceeded, time.Second)
	// 等 janitor 扫过
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if r.Store().Len() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("janitor 未清理过期终态 Job；store len=%d", r.Store().Len())
}

// ---- 进度上报 ---------------------------------------------------------------

func TestReportProgress(t *testing.T) {
	start := make(chan struct{})
	stop := make(chan struct{})
	var runner *Runner
	runner = New(Config{
		MaxConcurrentJobs: 2,
		MaxQueuedJobs:     4,
		DefaultTimeout:    5 * time.Second,
	}, NewMemStore(), ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		close(start)
		<-stop
		return nil, nil
	}))
	t.Cleanup(func() { close(stop); _ = runner.Shutdown(context.Background()) })
	id, _ := runner.Submit(context.Background(), "foo", nil, 0, "")
	<-start
	// 上报进度
	if err := runner.ReportProgress(id, map[string]any{"done": 3, "total": 10}); err != nil {
		t.Fatal(err)
	}
	j, _ := runner.Store().Get(context.Background(), id)
	if j.Progress == nil || j.Progress.Fields["done"] != 3 {
		t.Errorf("progress 上报失败：%+v", j.Progress)
	}
}
