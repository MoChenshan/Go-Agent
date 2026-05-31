// Package async 提供"长耗时工具"的异步执行框架。
//
// # 为什么需要独立的 async 包
//
// LLM 工具调用的底层契约是**同步请求-响应**：LLM 发一条 tool_call，等一个 tool_result，
// 这期间整个对话线程都是阻塞的。对于"秒级返回"的查询类工具这很自然；但对于这几类场景
// 就会非常拧巴：
//
//   1. pod_restart wait_for_ready 要轮询 60~120s —— LLM 对话卡两分钟
//   2. helm upgrade --wait 可能 5 分钟 —— 极端阻塞
//   3. scale 后等副本 ready —— 15~60s 看规模
//   4. 真实 CI/CD 触发后轮询完成 —— 更长
//
// 这些场景如果放进 tool_call 同步等，有三重代价：
//
//   A) 用户体验灾难：LLM"像死机了"，用户不知道还要多久
//   B) 吞吐瓶颈：单个 Agent 会话同一时刻只能 1 个慢任务
//   C) 超时误报：LLM 框架的 tool_timeout（常 60s）比真实任务短，频繁误判失败
//
// # 本包采用的模型：Job + Poll + Callback
//
// 提交式异步（行业标准模型，K8s Job / CI pipeline / Celery 全一致）：
//
//   submit(tool, args)   ──>   立即返回 JobID
//   get_status(JobID)    ──>   pending / running / succeeded / failed / cancelled / timed_out
//   wait(JobID, max)     ──>   半阻塞等待（带超时上限，常用于"顺手等一会儿"）
//   cancel(JobID)        ──>   尽力取消（context.Cancel）
//
// 不做 async/await —— Go 的 goroutine 已经是异步原语，LLM 框架的契约依然是同步
// 请求-响应，硬塞 async 会破坏工具调用协议、制造混乱。Job 模型与 LLM 交互友好：
// LLM 只要记住 JobID，每个 tool_call 都是秒级返回。
//
// # 状态机
//
//   pending ──(start)──> running ──(done)──> succeeded
//               │                └──(err)──> failed
//               │                └─(panic)─> failed
//               │                └─(deadl)─> timed_out
//               └─(cancel)─────────────────> cancelled
//
// 约束：
//   - 每个状态都是"终态"一次性转换；succeeded/failed/cancelled/timed_out 不可再流转
//   - timed_out 属于 failed 的子类，单列是为了可观测性（告警规则 / 面板区分）
package async

import (
	"errors"
	"sync"
	"time"
)

// JobStatus 异步任务状态，参见包注释中的状态机图。
type JobStatus string

const (
	// StatusPending 已提交、尚未被 worker goroutine 拾起。
	StatusPending JobStatus = "pending"
	// StatusRunning worker 已开始执行 underlying tool。
	StatusRunning JobStatus = "running"
	// StatusSucceeded tool 返回 nil error 且 Result 已存储。
	StatusSucceeded JobStatus = "succeeded"
	// StatusFailed tool 返回 error（含 panic 恢复）。
	StatusFailed JobStatus = "failed"
	// StatusCancelled 被显式 cancel；区别于 timed_out，记录"谁"取消便于追责。
	StatusCancelled JobStatus = "cancelled"
	// StatusTimedOut 到 TimeoutAt 仍未完成，watchdog 强制终止。
	StatusTimedOut JobStatus = "timed_out"
)

// IsTerminal 判断该状态是否为终态（不可再流转）。
func (s JobStatus) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled, StatusTimedOut:
		return true
	}
	return false
}

// Progress 可选的进度快照；工具如果想上报进度，通过 ProgressReporter 写入。
//
// 用 map 而不是固定字段是刻意选择：
//   - 不同工具进度语义差异大（"50 个 pod 已处理 20 个" vs "镜像上传 70%"），
//     上强结构反而别扭
//   - LLM 读到这些字段会自己择取重点回复给用户，不必我们硬做 UI
//
// 约束：序列化后应在 1KB 以内；过大 Payload 不要塞进来（用 Result）。
type Progress struct {
	UpdatedAt time.Time      `json:"updated_at"`
	Fields    map[string]any `json:"fields"`
}

// Job 一次异步任务的完整状态快照。
//
// 设计准则：
//   - 字段皆可 JSON 序列化，为后续 FileStore/DB 持久化留口（本轮 MemStore 不依赖）
//   - Result/Err 互斥：终态只会有一个非零
//   - cancelFn 不序列化：运行时字段，持久化恢复后 cancel 能力会丢失（符合预期）
type Job struct {
	ID             string         `json:"id"`
	ToolName       string         `json:"tool_name"`
	Args           map[string]any `json:"args"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`

	Status   JobStatus `json:"status"`
	Progress *Progress `json:"progress,omitempty"`

	// Result 成功时的返回值。存 any 是为了兼容各工具不同的返回类型；
	// 读取方（job_status 工具）会自行 json marshal 后回给 LLM。
	Result any    `json:"result,omitempty"`
	Err    string `json:"err,omitempty"`

	SubmittedAt time.Time  `json:"submitted_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	TimeoutAt   time.Time  `json:"timeout_at"`

	// cancelFn 运行时字段，指向本 Job 对应 context 的 CancelFunc。
	// - Submit 时由 Runner 注入；
	// - 终态时由 Runner 负责调用一次，避免泄漏；
	// - 不 JSON 序列化（持久化恢复的 Job 将失去 cancel 能力，这是可接受的）。
	cancelFn func() `json:"-"`
}

// Clone 返回 Job 的深拷贝（Args/Progress/Result 字段按"不在外部写"的假设浅拷贝）。
//
// 为什么需要 Clone：Store.Get 返回的 Job 可能被调用方读取、LLM 序列化、或穿越 goroutine；
// 直接返回内部指针会导致 Runner 内部状态被外部无意修改。统一用 Clone 守住不变量。
func (j *Job) Clone() *Job {
	if j == nil {
		return nil
	}
	cp := *j
	// cancelFn 一定不外泄（即使同一进程，也不应让外部代码持有取消权）
	cp.cancelFn = nil
	if j.Progress != nil {
		pp := *j.Progress
		cp.Progress = &pp
	}
	// Args/Result 按引用拷贝：上层约定"入参出参 immutable"，避免深拷贝 any 的开销
	return &cp
}

// =============================================================================
// 错误定义
// =============================================================================

// ErrJobNotFound 查询/取消一个不存在的 JobID。
var ErrJobNotFound = errors.New("async: job not found")

// ErrToolNotRegistered AsyncRunner 未注册该工具，无法 submit。
var ErrToolNotRegistered = errors.New("async: tool not registered")

// ErrTooManyJobs 超出 MaxConcurrentJobs + MaxQueuedJobs 限制。
var ErrTooManyJobs = errors.New("async: too many jobs in flight")

// ErrJobAlreadyTerminal 对已终态的 Job 执行 cancel 等操作。
var ErrJobAlreadyTerminal = errors.New("async: job already in terminal state")

// ErrJobPanicked worker 捕获到 panic 时附加的错误前缀。
var ErrJobPanicked = errors.New("async: job panicked")

// =============================================================================
// 工具注册表 —— AsyncRunner 内部使用的小型注册中心
// =============================================================================

// ToolRegistry 提供按名字查找 tool.Tool 的能力；被 Runner 持有。
//
// 独立成 struct 是为了：
//   - 可单独在测试里用 mock Tool 注入，不依赖 app 装配
//   - 加锁边界清晰：注册时加写锁，查询时加读锁，高频查询不互斥
//
// 注意：这里用的是 `any` 而非直接引 trpc 的 tool.Tool，是为了本包完全不依赖
// 具体 tool 接口细节（Runner 调用时再 type-assert），这样 async 包可以被测试
// 独立编译，不把 trpc 生态拖进来。
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]any // name -> tool.Tool（实际类型）
}

// NewToolRegistry 创建空注册表。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]any)}
}

// Register 以 name 注册工具；同名会覆盖（最后注册者胜）。
func (r *ToolRegistry) Register(name string, tool any) {
	if name == "" || tool == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = tool
}

// Lookup 按 name 查工具；未注册返回 nil,false。
func (r *ToolRegistry) Lookup(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names 列出已注册工具名（用于错误信息友好提示、诊断）。
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	return out
}
