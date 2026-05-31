// runner.go —— 异步任务执行引擎（AsyncRunner）。
//
// 职责：
//   1) Submit：分配 JobID → 入 Store(pending) → 起 worker goroutine → 立刻返回
//   2) Worker：running → 调 underlying tool → succeeded / failed / timed_out
//   3) Watchdog：TimeoutAt 到期强制 cancel
//   4) Cancel：尽力取消（context.Cancel + 状态转 cancelled）
//   5) Wait：半阻塞等待（chan 通知 + 超时回落）
//
// 生命周期保证：
//   - 每个 Job 对应独立 context.WithCancel，保证 cancel 语义清晰
//   - Job 终态时 Runner 负责调用 cancelFn 一次，避免 goroutine 和 context 泄漏
//   - Runner.Shutdown 能按 timeout 优雅等待所有 worker 完成（超时后强 cancel 余下）
//
// 并发上限：
//   - MaxConcurrentJobs：同时运行的 Job 数（semaphore 实现）
//   - MaxQueuedJobs：Store 中 pending+running 总数上限（防止提交速度 > 消费速度）
//   - 超出即 ErrTooManyJobs（拒绝 submit，不入队阻塞）
package async

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Executor 是 AsyncRunner 对底层工具调用的抽象。
//
// 为什么单独抽象：Runner 不直接依赖 trpc 的 tool.Tool，而是靠 Executor 把
// "按名字执行一次工具 → 返回 any + error"这层委托出去；这样做有三个好处：
//
//   1. 测试友好：单测可以用 funcExecutor 直接塞任意闭包，不引 trpc 运行时
//   2. 解耦 tool 框架升级：trpc-agent-go 升级 tool 契约，只改 Executor 实现，
//      Runner 核心逻辑不变
//   3. 可替换：未来想把 Executor 放到远端（RPC 执行），接口都不用改
type Executor interface {
	// Execute 按工具名 + 入参执行，返回结果 any 与 error。
	// 实现必须尊重 ctx：ctx 取消时应及时返回。
	Execute(ctx context.Context, toolName string, args map[string]any) (any, error)
}

// ExecutorFunc 便捷适配器：把普通函数当 Executor 用。
type ExecutorFunc func(ctx context.Context, toolName string, args map[string]any) (any, error)

// Execute 实现 Executor 接口。
func (f ExecutorFunc) Execute(ctx context.Context, toolName string, args map[string]any) (any, error) {
	return f(ctx, toolName, args)
}

// Config Runner 运行时配置。所有字段都有"零值即合理"的默认值，便于最小化装配。
type Config struct {
	// MaxConcurrentJobs 同时运行的最大 Job 数；默认 16。
	MaxConcurrentJobs int
	// MaxQueuedJobs Store 中非终态 Job 数上限；默认 256。
	MaxQueuedJobs int
	// DefaultTimeout 未指定 timeout 时的默认值；默认 300s（5 分钟）。
	DefaultTimeout time.Duration
	// MaxTimeout 单个 Job 允许的最长 timeout；默认 1800s（30 分钟）。
	MaxTimeout time.Duration
	// JanitorInterval 后台清理 goroutine 运行间隔；默认 1 分钟。
	// 清理职责：把超过 JanitorRetention 的终态 Job 从 Store 删除，避免 MemStore OOM。
	JanitorInterval time.Duration
	// JanitorRetention 终态 Job 的保留时长；默认 10 分钟。
	JanitorRetention time.Duration
	// Clock 时钟抽象（测试可注入假时钟，本轮不强需要；nil 时用 time.Now）。
	Clock func() time.Time
	// Logger 日志函数；nil 时走 no-op。
	Logger func(format string, args ...any)
	// Metrics 可选的观测性钩子；nil 时走 no-op。
	//
	// 设计决策：为什么用接口而不直接 import observability？
	//   - async 包目前零项目内依赖，是个干净的工具库包
	//   - 测试可注 fake hook 验证调用时机与参数
	//   - 未来换掉 observability 实现零代价
	// 由 app 层在装配时注入 observability.AsyncMetricsAdapter 即可。
	Metrics MetricsHook
}

// MetricsHook 是 Runner 向外曝光的观测回调接口，nil 无需特殊处理。
//
// 调用位置：
//   - OnSubmit  ：Submit() 的每条出口路径（accepted / rejected / dedup_hit）
//   - OnFinish  ：finish() 的终态跳转时（包含 total duration）
type MetricsHook interface {
	// OnSubmit 不应阻塞；跟错错误、低延迟。
	OnSubmit(tool, outcome string)
	// OnFinish 不应阻塞。total 是从 Submit 到 finish 的总耗时。
	OnFinish(tool, status string, total time.Duration)
}

// noopMetrics 是 MetricsHook 内部默认实现，避免每个调用点 nil 判断。
type noopMetrics struct{}

func (noopMetrics) OnSubmit(string, string)                      {}
func (noopMetrics) OnFinish(string, string, time.Duration)        {}

// applyDefaults 把零值字段补成默认。
func (c *Config) applyDefaults() {
	if c.MaxConcurrentJobs <= 0 {
		c.MaxConcurrentJobs = 16
	}
	if c.MaxQueuedJobs <= 0 {
		c.MaxQueuedJobs = 256
	}
	if c.DefaultTimeout <= 0 {
		c.DefaultTimeout = 5 * time.Minute
	}
	if c.MaxTimeout <= 0 {
		c.MaxTimeout = 30 * time.Minute
	}
	if c.JanitorInterval <= 0 {
		c.JanitorInterval = time.Minute
	}
	if c.JanitorRetention <= 0 {
		c.JanitorRetention = 10 * time.Minute
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	if c.Logger == nil {
		c.Logger = func(string, ...any) {}
	}
	if c.Metrics == nil {
		c.Metrics = noopMetrics{}
	}
}

// Runner 异步任务执行引擎。构造：New(Config, Store, Executor)。
type Runner struct {
	cfg      Config
	store    JobStore
	executor Executor

	// sem 并发闸门：buffered channel 做计数信号量，cap = MaxConcurrentJobs。
	// Submit 后立刻尝试 sem <- struct{}{}（非阻塞），拿不到就表示已达并发上限。
	// 但设计上我们不在 Submit 阻塞 —— 超过 MaxConcurrentJobs 的 Job 应该以
	// pending 状态排进 Store，由 worker 排队拾取；这就需要独立 dispatcher。
	// 本版本简化：Submit 本身就起 worker goroutine，通过 sem 控制实际并发数；
	// pending → running 的跃迁在 worker 内完成。Store 上限靠独立计数（queuedCount）。
	sem chan struct{}

	// queuedCount：当前"非终态"Job 计数（pending + running）。用于 MaxQueuedJobs 限流。
	// 用 atomic 而非锁，因为访问点很多且是热点路径（每次 Submit 都要比较）。
	queuedCount atomic.Int64

	// subs 订阅表：Wait() 为每个 JobID 注册一个 chan，终态时 notify 后删除。
	// 一个 Job 可能被多个 Wait 并发等（虽然少见），用 []chan 支持多订阅。
	subsMu sync.Mutex
	subs   map[string][]chan struct{}

	// 关闭/等待
	ctx        context.Context
	cancel     context.CancelFunc
	workersWG  sync.WaitGroup
	shutdownCh chan struct{}
	shutdonce  sync.Once
}

// New 构造 Runner。store/executor 不能为 nil。
//
// 调用方典型装配：
//
//	store := async.NewMemStore()
//	exec := myExecutorImpl
//	runner := async.New(async.Config{MaxConcurrentJobs: 32}, store, exec)
//	defer runner.Shutdown(context.Background())
func New(cfg Config, store JobStore, executor Executor) *Runner {
	cfg.applyDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		cfg:        cfg,
		store:      store,
		executor:   executor,
		sem:        make(chan struct{}, cfg.MaxConcurrentJobs),
		subs:       make(map[string][]chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
		shutdownCh: make(chan struct{}),
	}
	// 启动 janitor
	r.workersWG.Add(1)
	go r.janitorLoop()
	return r
}

// Submit 提交异步任务。
//
// 语义：
//   - 立即返回（非阻塞），返回新 JobID
//   - idempotencyKey 非空 + MemStore 命中同 key 的 Job → 返回已有 JobID，不再起新 worker
//   - 超出 MaxQueuedJobs → ErrTooManyJobs
//   - timeout > MaxTimeout → 自动裁剪到 MaxTimeout，并 log warn
func (r *Runner) Submit(ctx context.Context, toolName string, args map[string]any, timeout time.Duration, idempotencyKey string) (string, error) {
	if toolName == "" {
		return "", errors.New("async: tool_name is empty")
	}

	// 幂等：若 MemStore 命中，直接复用
	if idempotencyKey != "" {
		if mem, ok := r.store.(*MemStore); ok {
			if existed := mem.findByIdempotencyKey(idempotencyKey); existed != nil {
				r.cfg.Metrics.OnSubmit(toolName, "dedup_hit")
				return existed.ID, nil
			}
		}
	}

	// 限流
	if r.queuedCount.Load() >= int64(r.cfg.MaxQueuedJobs) {
		r.cfg.Metrics.OnSubmit(toolName, "rejected")
		return "", ErrTooManyJobs
	}

	// 规范化 timeout
	if timeout <= 0 {
		timeout = r.cfg.DefaultTimeout
	}
	if timeout > r.cfg.MaxTimeout {
		r.cfg.Logger("[async] timeout %s exceeds max %s, truncating", timeout, r.cfg.MaxTimeout)
		timeout = r.cfg.MaxTimeout
	}

	now := r.cfg.Clock()
	jobCtx, jobCancel := context.WithCancel(r.ctx)

	id := newJobID()
	job := &Job{
		ID:             id,
		ToolName:       toolName,
		Args:           args,
		IdempotencyKey: idempotencyKey,
		Status:         StatusPending,
		SubmittedAt:    now,
		TimeoutAt:      now.Add(timeout),
		cancelFn:       jobCancel,
	}
	if err := r.store.Put(ctx, job); err != nil {
		jobCancel()
		return "", fmt.Errorf("store put: %w", err)
	}
	r.queuedCount.Add(1)
	r.workersWG.Add(1)
	r.cfg.Metrics.OnSubmit(toolName, "accepted")
	go r.work(jobCtx, jobCancel, id, toolName, args, timeout)
	return id, nil
}

// work 单个 Job 的 worker goroutine。
//
// 流程：
//  1. 申请 sem（受 MaxConcurrentJobs 限制）
//  2. pending → running，记录 StartedAt
//  3. 启动 watchdog 定时器（到期调 jobCancel 并标记 timedOut）
//  4. 调 executor.Execute；panic 恢复为 failed
//  5. 根据结果转终态，notify 订阅者，释放资源
//
// 注意顺序：无论成功/失败/超时/取消，cancelFn 和 queuedCount.Add(-1) 都只执行一次。
func (r *Runner) work(
	jobCtx context.Context,
	jobCancel context.CancelFunc,
	id, toolName string,
	args map[string]any,
	timeout time.Duration,
) {
	defer r.workersWG.Done()

	// queuedCount 的扣减由 finish() 负责（仅在首次转终态时扪 1 次），这里不再 defer。
	// 原因：Cancel() 会同步转终态，如果这里也 defer 会造成 queuedCount
	// 被双重扣减，或者依赖 worker 退出才扣减导致 store 状态同步但
	// queuedCount 沪后，MaxQueuedJobs 限流出现误拒。

	// 1. 申请并发额度；若 Runner 已 shutdown 或 Job 在排队期间被 Cancel，立即退出。
	//
	// 关键：必须同时监听 jobCtx.Done()，否则一个 pending 排队中的 Job 被 Cancel
	// 后，worker 会一直阻塞在等 sem 上，导致 queuedCount 无法扣减、MaxQueuedJobs
	// 配额永久占用——这正是 TestIntegration_AsyncQueueLimitRejects 关心的语义。
	//
	// 优先级语义：先非阻塞尝试拿 sem（没排队就直接进 executor），拿不到才 select 三路。
	// 这避免了 r.ctx/jobCtx 与 sem 同时 ready 时 select 随机选择导致的副作用——
	// 例如 TestShutdown_Timeout 场景里 r.cancel() 在 worker 进 select 前发生，
	// 若随机走 ctx.Done() 分支，worker 还没真正进入 executor 就退出了，
	// Shutdown 看 workersWG=0 立即返回 nil（与"超时返 DeadlineExceeded"契约违背）。
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	default:
		select {
		case r.sem <- struct{}{}:
			defer func() { <-r.sem }()
		case <-r.ctx.Done():
			r.finish(id, StatusCancelled, nil, errors.New("runner shutdown before job started"))
			return
		case <-jobCtx.Done():
			// Job 在排队期间被外部 Cancel；finish 内部会判 IsTerminal 防重复写。
			r.finish(id, StatusCancelled, nil, errors.New("cancelled before scheduled"))
			return
		}
	}

	// 2. pending → running
	_ = r.store.Update(context.Background(), id, func(j *Job) error {
		if j.Status != StatusPending {
			return nil // 可能已被 cancel
		}
		now := r.cfg.Clock()
		j.Status = StatusRunning
		j.StartedAt = &now
		return nil
	})

	// 3. watchdog：到期取消 jobCtx 并标记 timedOut；正常完成也会 Stop
	var timedOut atomic.Bool
	watchdog := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		jobCancel()
	})
	defer watchdog.Stop()

	// 4. 执行
	var result any
	var err error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("%w: %v", ErrJobPanicked, rec)
			}
		}()
		result, err = r.executor.Execute(jobCtx, toolName, args)
	}()

	// 5. 终态转换（优先判定 timedOut，因为 cancel 也是由 watchdog 触发的）
	switch {
	case timedOut.Load():
		r.finish(id, StatusTimedOut, nil, fmt.Errorf("job exceeded timeout %s", timeout))
	case jobCtx.Err() != nil:
		// ctx 被外部 Cancel 取消（比如 job_cancel 工具），无论 executor 返回什么都算 cancelled
		msg := "cancelled"
		if err != nil {
			msg = err.Error()
		}
		r.finish(id, StatusCancelled, nil, errors.New(msg))
	case err != nil:
		r.finish(id, StatusFailed, nil, err)
	default:
		r.finish(id, StatusSucceeded, result, nil)
	}
}

// finish 原子地把 Job 置为终态 + 调用 cancelFn + 通知订阅者 + 扣减 queuedCount。
//
// queuedCount 扣减只能发生一次（“首次转终态”那次），由 store.Update 的原子性保证。
func (r *Runner) finish(id string, status JobStatus, result any, finishErr error) {
	var (
		observedTool   string
		observedStatus string
		totalDuration  time.Duration
		shouldEmit     bool
	)
	_ = r.store.Update(context.Background(), id, func(j *Job) error {
		if j.Status.IsTerminal() {
			return nil // 已经是终态，避免覆盖（比如先 cancel 再 timeout 的竞速）
		}
		now := r.cfg.Clock()
		j.Status = status
		j.FinishedAt = &now
		j.Result = result
		if finishErr != nil {
			j.Err = finishErr.Error()
		}
		if j.cancelFn != nil {
			j.cancelFn()
			j.cancelFn = nil
		}
		// 采集备用于钩子发射；必须在持锁状态下读，避免读到不一致快照
		observedTool = j.ToolName
		observedStatus = string(status)
		totalDuration = now.Sub(j.SubmittedAt)
		shouldEmit = true
		return nil
	})
	if shouldEmit {
		// 首次转终态：同步扣减 queuedCount，立即释放 MaxQueuedJobs 额度。
		r.queuedCount.Add(-1)
		r.cfg.Metrics.OnFinish(observedTool, observedStatus, totalDuration)
	}
	r.notify(id)
}

// ReportProgress 工具实现可选地调用本方法上报进度。
//
// 用法：工具侧拿到 ctx，在 ctx 中 Runner 会注入一个 ProgressReporter；
// 当前简化实现：工具通过 Runner.Progress(id, fields) 直接写入。
func (r *Runner) ReportProgress(id string, fields map[string]any) error {
	return r.store.Update(context.Background(), id, func(j *Job) error {
		if j.Status.IsTerminal() {
			return ErrJobAlreadyTerminal
		}
		j.Progress = &Progress{
			UpdatedAt: r.cfg.Clock(),
			Fields:    fields,
		}
		return nil
	})
}

// Cancel 尽力取消指定 Job。
//
// - Job 不存在 → ErrJobNotFound
// - Job 已终态 → ErrJobAlreadyTerminal（调用方可忽略）
// - 其他：状态转 cancelled + 调 cancelFn + 同步扣减 queuedCount + notify。
//
// 为什么同步扣减 queuedCount：
// MaxQueuedJobs 限流是“Submit 即时决定拒不拒”的，调用方 Cancel 后立即 Submit
// 应该能拿到额度。若依赖 worker 退出才扣减，会出现 store.Status==cancelled
// 但 queuedCount 未同步的 race 窗口。
func (r *Runner) Cancel(ctx context.Context, id string) error {
	var firstTransition bool
	err := r.store.Update(ctx, id, func(j *Job) error {
		if j.Status.IsTerminal() {
			return ErrJobAlreadyTerminal
		}
		now := r.cfg.Clock()
		j.Status = StatusCancelled
		j.FinishedAt = &now
		j.Err = "cancelled by user"
		if j.cancelFn != nil {
			j.cancelFn()
			j.cancelFn = nil
		}
		firstTransition = true
		return nil
	})
	if firstTransition {
		r.queuedCount.Add(-1)
		go r.notify(id)
	}
	return err
}

// Wait 半阻塞等待 Job 达到终态或 ctx 过期。
//
// 返回：终态 Job 的 Clone 副本 + nil；若 ctx 先过期，返回 ctx.Err()。
// 已经是终态的 Job 立即返回。
func (r *Runner) Wait(ctx context.Context, id string) (*Job, error) {
	// 快速路径：直接查一下，已终态立即返
	if j, err := r.store.Get(ctx, id); err != nil {
		return nil, err
	} else if j.Status.IsTerminal() {
		return j, nil
	}

	// 注册订阅者
	ch := r.subscribe(id)
	defer r.unsubscribe(id, ch)

	// 订阅后再查一次，避免"订阅前终态就到了"的竞速
	if j, err := r.store.Get(ctx, id); err == nil && j.Status.IsTerminal() {
		return j, nil
	}

	select {
	case <-ch:
		return r.store.Get(ctx, id)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// subscribe 注册一个终态通知 chan 并返回之。
func (r *Runner) subscribe(id string) chan struct{} {
	ch := make(chan struct{}, 1)
	r.subsMu.Lock()
	r.subs[id] = append(r.subs[id], ch)
	r.subsMu.Unlock()
	return ch
}

// unsubscribe 移除订阅（Wait 退出时 defer 调用）。
func (r *Runner) unsubscribe(id string, ch chan struct{}) {
	r.subsMu.Lock()
	defer r.subsMu.Unlock()
	chs := r.subs[id]
	for i, c := range chs {
		if c == ch {
			r.subs[id] = append(chs[:i], chs[i+1:]...)
			break
		}
	}
	if len(r.subs[id]) == 0 {
		delete(r.subs, id)
	}
}

// notify 终态触达时给所有订阅者发信号（非阻塞）。
func (r *Runner) notify(id string) {
	r.subsMu.Lock()
	chs := r.subs[id]
	// 一次性"全 fire"，再清除
	delete(r.subs, id)
	r.subsMu.Unlock()
	for _, ch := range chs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// janitorLoop 定时清理过期的终态 Job，避免 MemStore 无限增长。
func (r *Runner) janitorLoop() {
	defer r.workersWG.Done()
	ticker := time.NewTicker(r.cfg.JanitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.janitorSweep()
		}
	}
}

// janitorSweep 清除 FinishedAt 早于 now - retention 的终态 Job。
func (r *Runner) janitorSweep() {
	now := r.cfg.Clock()
	cutoff := now.Add(-r.cfg.JanitorRetention)
	jobs, err := r.store.List(context.Background(), JobFilter{})
	if err != nil {
		return
	}
	for _, j := range jobs {
		if j.Status.IsTerminal() && j.FinishedAt != nil && j.FinishedAt.Before(cutoff) {
			_ = r.store.Delete(context.Background(), j.ID)
		}
	}
}

// Shutdown 优雅关闭 Runner。
//
// 步骤：
//  1. 关闭根 context，所有 worker/janitor 感知到 ctx.Done()
//  2. 在 ctx 限定的 timeout 内等待所有 worker 结束
//  3. 超时则返回 ctx.Err()（已取消的 worker 会被丢弃，不再追）
//
// 幂等：多次调用只触发一次真实关闭。
func (r *Runner) Shutdown(ctx context.Context) error {
	var firstErr error
	r.shutdonce.Do(func() {
		r.cancel()
		done := make(chan struct{})
		go func() {
			r.workersWG.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			firstErr = ctx.Err()
		}
		close(r.shutdownCh)
	})
	return firstErr
}

// Store 暴露内部 store 供调用方只读访问（用于 job_status 工具）。
func (r *Runner) Store() JobStore { return r.store }

// Config 暴露内部配置供外部检查（如 wait 上限等）。
func (r *Runner) Config() Config { return r.cfg }

// newJobID 生成形如 "job_<12hex>" 的唯一 ID（足够 LLM 可读又不冲突）。
func newJobID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "job_" + hex.EncodeToString(b[:])
}
