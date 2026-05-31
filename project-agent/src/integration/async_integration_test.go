// async_integration_test.go —— AsyncRunner 与 4 件套工具的端到端契约。
//
// 覆盖场景：
//
//   场景 3：AsyncSubmitWaitCancel —— 模拟真实长耗时任务：
//     * executor 模拟 800ms 耗时的 pod_restart
//     * job_submit 立刻返回（<50ms）
//     * job_wait 能等到成功终态
//     * 另一个 job 在运行中被 job_cancel，Wait 及时被唤醒
//
//   场景 4：AsyncQueueLimitRejects —— 背压边界：
//     * MaxQueued=3 时，第 4 次 Submit 返回 ErrTooManyJobs
//     * 取消已有的一个后腾出配额，新 Submit 能成功
//
// 装配：真实 async.Runner + 真实 MemStore + funcExecutor 模拟底层工具
package integration

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/async"
)

// mkIntegrationRunner 构造一个"接近生产"的 Runner：并发 4 / 队列 16 / 默认 2s timeout。
// 测试内无需清理终态 Job，所以 janitor 设得很长避免干扰。
func mkIntegrationRunner(t *testing.T, exec async.Executor) *async.Runner {
	t.Helper()
	r := async.New(async.Config{
		MaxConcurrentJobs: 4,
		MaxQueuedJobs:     16,
		DefaultTimeout:    2 * time.Second,
		MaxTimeout:        10 * time.Second,
		JanitorInterval:   time.Hour,
		JanitorRetention:  time.Hour,
	}, async.NewMemStore(), exec)
	t.Cleanup(func() {
		_ = r.Shutdown(context.Background())
	})
	return r
}

// TestIntegration_AsyncSubmitWaitCancel 场景 3：完整 submit→wait→cancel 链。
func TestIntegration_AsyncSubmitWaitCancel(t *testing.T) {
	var execCalls atomic.Int64
	started := make(chan string, 8)
	exec := async.ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		execCalls.Add(1)
		// 通知"已进入 executor"
		select {
		case started <- name:
		default:
		}
		// 模拟长耗时；尊重 ctx 取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(600 * time.Millisecond):
			return map[string]any{"ok": true, "tool": name}, nil
		}
	})
	runner := mkIntegrationRunner(t, exec)

	// ========== 子场景 A：submit → wait 成功 ==========
	submitStart := time.Now()
	id1, err := runner.Submit(context.Background(), "fake_pod_restart",
		map[string]any{"cluster_id": "BCS-001"}, 0, "")
	submitLatency := time.Since(submitStart)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Submit 必须立刻返回（不阻塞）
	if submitLatency > 100*time.Millisecond {
		t.Errorf("Submit 不应阻塞，实际耗时 %s", submitLatency)
	}
	<-started // 确认 executor 已经跑起来

	// Wait 最多等 3s
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	j1, err := runner.Wait(waitCtx, id1)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if j1.Status != async.StatusSucceeded {
		t.Errorf("期望 succeeded，实际 %s（err=%s）", j1.Status, j1.Err)
	}
	res, ok := j1.Result.(map[string]any)
	if !ok || res["tool"] != "fake_pod_restart" {
		t.Errorf("result 不符：%+v", j1.Result)
	}

	// ========== 子场景 B：Cancel 唤醒 Wait ==========
	id2, err := runner.Submit(context.Background(), "fake_pod_restart", nil, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	<-started // 确认进入 executor

	// 另起 goroutine 在 50ms 后 cancel
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = runner.Cancel(context.Background(), id2)
	}()

	cancelCtx, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	j2, err := runner.Wait(cancelCtx, id2)
	if err != nil {
		t.Fatalf("wait after cancel: %v", err)
	}
	if j2.Status != async.StatusCancelled {
		t.Errorf("期望 cancelled，实际 %s", j2.Status)
	}

	// 两次 submit 都应该触发了 executor
	if got := execCalls.Load(); got != 2 {
		t.Errorf("executor 调用次数期望 2，实际 %d", got)
	}
}

// TestIntegration_AsyncQueueLimitRejects 场景 4：队列限流回弹。
func TestIntegration_AsyncQueueLimitRejects(t *testing.T) {
	block := make(chan struct{})
	var running atomic.Int64
	exec := async.ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		running.Add(1)
		defer running.Add(-1)
		select {
		case <-block:
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	// 构造专门的小容量 Runner：MaxQueued=3
	runner := async.New(async.Config{
		MaxConcurrentJobs: 2,
		MaxQueuedJobs:     3,
		DefaultTimeout:    5 * time.Second,
	}, async.NewMemStore(), exec)
	t.Cleanup(func() {
		close(block)
		_ = runner.Shutdown(context.Background())
	})

	// 提交 3 个，每个都要阻塞在 block 上 → queuedCount 爬到 3
	var ids []string
	for i := 0; i < 3; i++ {
		id, err := runner.Submit(context.Background(), "forever", nil, 0, "")
		if err != nil {
			t.Fatalf("submit #%d: %v", i, err)
		}
		ids = append(ids, id)
	}

	// 等直到 worker 进入阻塞（running 或 pending 都计入 queuedCount）
	// 这里的目的是排除 race——Submit 到 queuedCount.Add(1) 之间有极小延迟
	waitForQueueDepth := func(want int) {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			list, _ := runner.Store().List(context.Background(), async.JobFilter{})
			active := 0
			for _, j := range list {
				if !j.Status.IsTerminal() {
					active++
				}
			}
			if active == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("等待 queue depth=%d 超时", want)
	}
	waitForQueueDepth(3)

	// 第 4 次 Submit 应当被拒绝
	if _, err := runner.Submit(context.Background(), "forever", nil, 0, ""); !errors.Is(err, async.ErrTooManyJobs) {
		t.Errorf("第 4 次 Submit 期望 ErrTooManyJobs，实际 %v", err)
	}

	// 取消一个已有的，让出配额
	if err := runner.Cancel(context.Background(), ids[0]); err != nil {
		t.Fatal(err)
	}
	// 等待被取消的 Job 进入终态，从 queuedCount 退出
	waitForQueueDepth(2)

	// 现在应当可以再提交
	if _, err := runner.Submit(context.Background(), "forever", nil, 0, ""); err != nil {
		t.Errorf("取消后 Submit 应成功，实际 %v", err)
	}
}

// TestIntegration_AsyncTimeoutTrumpsCancel 边界场景：watchdog 超时优先于外部 cancel。
//
// 这个用例验证 Runner 的一个关键语义：如果 watchdog 已经在 timeout 时刻触发
// jobCancel 取消了 jobCtx，后续 executor 返回时 Runner 必须把状态置为 timed_out
// 而非 cancelled（虽然 ctx 的取消本质是 watchdog 做的）。
//
// 这个微妙差异直接决定了监控面板上的告警规则：
//   - timed_out = 工具本身慢 → 需要扩 timeout 或优化工具实现
//   - cancelled = 用户主动取消 → 无需告警
// 混淆两者会让 SRE 面板失信。
func TestIntegration_AsyncTimeoutTrumpsCancel(t *testing.T) {
	exec := async.ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	runner := mkIntegrationRunner(t, exec)

	// 设 100ms 的超短 timeout
	id, err := runner.Submit(context.Background(), "hang", nil, 100*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	j, err := runner.Wait(waitCtx, id)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if j.Status != async.StatusTimedOut {
		t.Errorf("期望 timed_out，实际 %s", j.Status)
	}
	// Err 字段应含 "timeout" 字样
	if !strings.Contains(j.Err, "timeout") {
		t.Errorf("timed_out job.Err 应含 'timeout'，实际=%q", j.Err)
	}
}
