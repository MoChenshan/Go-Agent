// Package asynctools 把 src/async 的异步执行框架封装成 4 个 LLM 可直接调用的 MCP 工具。
//
// # 4 件套工具
//
//	job_submit   —— 异步投递任务，立即返回 JobID（不阻塞）
//	job_status   —— 查询 Job 状态/进度/结果
//	job_cancel   —— 取消活动中的 Job
//	job_wait     —— 半阻塞等待（带超时上限，用于"顺手等一会儿"场景）
//
// # 与存量工具的关系：非侵入式并列
//
// 不改动 bcs_pod_restart / helm_manage / scale 等存量工具；LLM 在耗时 > 10s 的场景
// 通过"先 job_submit，再 job_status 轮询"的双步调用达成"伪异步"效果。
// system_prompt.md 中会给 LLM 明确的使用指南。
//
// # target 约定
//
// 4 个工具都放 target="*"：控制流元工具，对所有 Agent 都有用，不应按场景筛选。
package asynctools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/async"
	"git.woa.com/trpc-go/gameops-agent/src/tools"
)

// Result 与其他 tools 包保持一致的返回结构。
//
// OK=false 表示工具本身出错；OK=true 表示工具成功、Data 里是结果（包括 Job 的终态可能是 failed）。
type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// waitUpperBound job_wait 允许的最长等待时间（避免 LLM 传太大让 tool_call 超时）。
const waitUpperBound = 25 * time.Second

// =============================================================================
// job_submit
// =============================================================================

// JobSubmitInput 异步提交任务的入参。
//
// 故意给 tool_name 做白名单校验：LLM 有时会胡乱填（比如"restart_pod"而不是"bcs_pod_restart"），
// 让错误在 submit 时就返回，避免起空跑的 worker。
type JobSubmitInput struct {
	ToolName       string         `json:"tool_name"        description:"要异步执行的工具名（必填），如 bcs_pod_restart / bcs_scale_deployment；必须是已注册的工具"`
	Args           map[string]any `json:"args"             description:"传给底层工具的入参（JSON 对象），按该工具自己的 schema 填"`
	TimeoutSeconds int            `json:"timeout_seconds"  description:"最长耗时秒数，默认 300，上限 1800；到期自动 timed_out"`
	IdempotencyKey string         `json:"idempotency_key"  description:"幂等键（可选）：相同 key 的重复 submit 会返回已有 job_id，不重复执行"`
}

// JobSubmitOutput 提交成功后的摘要；LLM 需要保留 job_id 用于后续查询。
type JobSubmitOutput struct {
	JobID      string    `json:"job_id"`
	ToolName   string    `json:"tool_name"`
	Status     string    `json:"status"`     // 通常 "pending"
	TimeoutAt  time.Time `json:"timeout_at"`
	SubmittedAt time.Time `json:"submitted_at"`
	Reused     bool      `json:"reused,omitempty"` // 为 true 表示是幂等复用，没有真正新起 job
}

// newJobSubmitTool 构造 job_submit 工具。
func newJobSubmitTool(runner *async.Runner, registry *async.ToolRegistry) tool.Tool {
	fn := func(ctx context.Context, in JobSubmitInput) (*Result, error) {
		if strings.TrimSpace(in.ToolName) == "" {
			return nil, fmt.Errorf("tool_name 为必填")
		}
		// 白名单校验
		if _, ok := registry.Lookup(in.ToolName); !ok {
			return nil, fmt.Errorf("tool %q 未注册为异步可执行工具；可用列表：%v", in.ToolName, registry.Names())
		}

		timeout := time.Duration(in.TimeoutSeconds) * time.Second
		id, err := runner.Submit(ctx, in.ToolName, in.Args, timeout, in.IdempotencyKey)
		if err != nil {
			if errors.Is(err, async.ErrTooManyJobs) {
				return &Result{OK: false, Message: "当前异步任务已达并发上限，请稍后再试或取消已有任务"}, nil
			}
			return nil, fmt.Errorf("submit: %w", err)
		}

		// 再查一次 store 拿到完整字段（包括 SubmittedAt/TimeoutAt）
		j, err := runner.Store().Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get submitted job: %w", err)
		}
		out := JobSubmitOutput{
			JobID:       j.ID,
			ToolName:    j.ToolName,
			Status:      string(j.Status),
			TimeoutAt:   j.TimeoutAt,
			SubmittedAt: j.SubmittedAt,
			Reused:      in.IdempotencyKey != "" && j.SubmittedAt.Before(time.Now().Add(-50*time.Millisecond)),
		}
		return &Result{
			OK:      true,
			Message: fmt.Sprintf("任务已提交，job_id=%s；请用 job_status / job_wait 跟进", j.ID),
			Data:    out,
		}, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("job_submit"),
		function.WithDescription("异步提交一个长耗时工具任务。立即返回 job_id（不等执行完成）。"+
			"适用场景：bcs_pod_restart(wait_for_ready=true) / bcs_scale_deployment 大规模扩缩 / "+
			"任何预期超过 10s 的写操作。提交后请用 job_status 查询进度、job_wait 短等、job_cancel 取消。"),
	)
}

// =============================================================================
// job_status
// =============================================================================

// JobStatusInput 查询入参。
type JobStatusInput struct {
	JobID          string `json:"job_id"            description:"要查询的任务 ID（必填）"`
	IncludeResult  bool   `json:"include_result"    description:"是否附带 Result（成功结果可能体积大，默认 true；若只想看状态可传 false）"`
}

// JobStatusOutput 友好格式：把 Job 的关键字段拍平，避免 LLM 误解套娃结构。
type JobStatusOutput struct {
	JobID       string         `json:"job_id"`
	ToolName    string         `json:"tool_name"`
	Status      string         `json:"status"`
	IsTerminal  bool           `json:"is_terminal"`
	SubmittedAt time.Time      `json:"submitted_at"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	FinishedAt  *time.Time     `json:"finished_at,omitempty"`
	TimeoutAt   time.Time      `json:"timeout_at"`
	DurationSec float64        `json:"duration_sec,omitempty"`
	Progress    map[string]any `json:"progress,omitempty"`
	Err         string         `json:"err,omitempty"`
	// Result 可能很大，序列化前做一次 json marshal 验证（避免 LLM 那头解析失败）
	Result any `json:"result,omitempty"`
}

// newJobStatusTool 构造 job_status 工具。
func newJobStatusTool(runner *async.Runner) tool.Tool {
	fn := func(ctx context.Context, in JobStatusInput) (*Result, error) {
		if in.JobID == "" {
			return nil, fmt.Errorf("job_id 为必填")
		}
		j, err := runner.Store().Get(ctx, in.JobID)
		if err != nil {
			if errors.Is(err, async.ErrJobNotFound) {
				return &Result{OK: false, Message: fmt.Sprintf("job %q 不存在（可能已被清理）", in.JobID)}, nil
			}
			return nil, err
		}
		out := JobStatusOutput{
			JobID:       j.ID,
			ToolName:    j.ToolName,
			Status:      string(j.Status),
			IsTerminal:  j.Status.IsTerminal(),
			SubmittedAt: j.SubmittedAt,
			StartedAt:   j.StartedAt,
			FinishedAt:  j.FinishedAt,
			TimeoutAt:   j.TimeoutAt,
			Err:         j.Err,
		}
		if j.Progress != nil {
			out.Progress = j.Progress.Fields
		}
		// duration：从开始到结束/现在
		if j.StartedAt != nil {
			end := time.Now()
			if j.FinishedAt != nil {
				end = *j.FinishedAt
			}
			out.DurationSec = end.Sub(*j.StartedAt).Seconds()
		}
		// include_result：LLM 在轮询中一般不需要 Result，终态时再带
		include := in.IncludeResult
		// IncludeResult 零值时默认 true（LLM 经常不填，给友好默认）
		if include || (!include && j.Status.IsTerminal()) {
			// 验证一次 marshal 不爆；失败就给个 toString
			if _, err := json.Marshal(j.Result); err == nil {
				out.Result = j.Result
			} else {
				out.Result = fmt.Sprintf("<unserializable: %T>", j.Result)
			}
		}
		return &Result{OK: true, Data: out}, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("job_status"),
		function.WithDescription("查询一个异步任务的当前状态/进度/结果。is_terminal=true 表示已进入终态，不必再轮询。"+
			"建议轮询间隔：5~10s。terminal 状态：succeeded / failed / cancelled / timed_out。"),
	)
}

// =============================================================================
// job_cancel
// =============================================================================

// JobCancelInput 取消入参。
type JobCancelInput struct {
	JobID  string `json:"job_id"  description:"要取消的任务 ID（必填）"`
	Reason string `json:"reason"  description:"取消原因（可选，用于审计留痕）"`
}

// newJobCancelTool 构造 job_cancel 工具。
func newJobCancelTool(runner *async.Runner) tool.Tool {
	fn := func(ctx context.Context, in JobCancelInput) (*Result, error) {
		if in.JobID == "" {
			return nil, fmt.Errorf("job_id 为必填")
		}
		err := runner.Cancel(ctx, in.JobID)
		switch {
		case err == nil:
			return &Result{OK: true, Message: fmt.Sprintf("job %s 已标记取消（reason=%s）", in.JobID, in.Reason)}, nil
		case errors.Is(err, async.ErrJobNotFound):
			return &Result{OK: false, Message: fmt.Sprintf("job %s 不存在", in.JobID)}, nil
		case errors.Is(err, async.ErrJobAlreadyTerminal):
			return &Result{OK: false, Message: fmt.Sprintf("job %s 已进入终态，无法再取消", in.JobID)}, nil
		default:
			return nil, err
		}
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("job_cancel"),
		function.WithDescription("尽力取消活动中的异步任务。底层通过 context.Cancel 触达工具，工具必须尊重 ctx 才能及时退出。"),
	)
}

// =============================================================================
// job_wait
// =============================================================================

// JobWaitInput 半阻塞等待入参。
type JobWaitInput struct {
	JobID          string `json:"job_id"          description:"要等待的任务 ID（必填）"`
	MaxWaitSeconds int    `json:"max_wait_seconds" description:"最长等待秒数（默认 10，上限 25，避免 tool_call 超时）"`
}

// newJobWaitTool 构造 job_wait 工具。
func newJobWaitTool(runner *async.Runner) tool.Tool {
	fn := func(ctx context.Context, in JobWaitInput) (*Result, error) {
		if in.JobID == "" {
			return nil, fmt.Errorf("job_id 为必填")
		}
		wait := time.Duration(in.MaxWaitSeconds) * time.Second
		if wait <= 0 {
			wait = 10 * time.Second
		}
		if wait > waitUpperBound {
			wait = waitUpperBound
		}
		wctx, cancel := context.WithTimeout(ctx, wait)
		defer cancel()

		j, err := runner.Wait(wctx, in.JobID)
		if err != nil {
			// ctx 过期不是"失败"，是"还没好"
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				cur, gerr := runner.Store().Get(ctx, in.JobID)
				if gerr != nil {
					return nil, gerr
				}
				return &Result{
					OK:      true,
					Message: "在最长等待时间内未进入终态；请稍后再用 job_status 或 job_wait 跟进",
					Data: map[string]any{
						"job_id":      cur.ID,
						"status":      string(cur.Status),
						"is_terminal": false,
					},
				}, nil
			}
			if errors.Is(err, async.ErrJobNotFound) {
				return &Result{OK: false, Message: fmt.Sprintf("job %s 不存在", in.JobID)}, nil
			}
			return nil, err
		}
		// 进入终态
		return &Result{
			OK: true,
			Data: map[string]any{
				"job_id":      j.ID,
				"status":      string(j.Status),
				"is_terminal": true,
				"result":      j.Result,
				"err":         j.Err,
			},
		}, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("job_wait"),
		function.WithDescription("半阻塞等待一个异步任务进入终态。"+
			"最多等 max_wait_seconds 秒（上限 25s，避免 tool_call 超时），超时未完成则返回当前状态、不算错误。"+
			"典型用法：job_submit 拿到 id 后立即 job_wait 等 10s，如果还没好就回头用 job_status 继续轮询。"),
	)
}

// =============================================================================
// 装配入口
// =============================================================================

// NewAllTargeted 返回 async 4 件套的 TargetedTool 列表。
//
// target 统一为 "*"：所有 Agent 都可用（控制流元工具）。
// 调用方仍需自己把需要 async 化的底层工具注册到 registry 里（通过 RegisterToolsForAsync）。
func NewAllTargeted(runner *async.Runner, registry *async.ToolRegistry) []tools.TargetedTool {
	if runner == nil || registry == nil {
		return nil
	}
	return []tools.TargetedTool{
		{Target: "*", Tool: newJobSubmitTool(runner, registry)},
		{Target: "*", Tool: newJobStatusTool(runner)},
		{Target: "*", Tool: newJobCancelTool(runner)},
		{Target: "*", Tool: newJobWaitTool(runner)},
	}
}

// RegisterToolsForAsync 把底层工具按 Name 注册到 async registry 里，让 job_submit 能找到它们。
//
// 调用方传入 []tool.Tool 列表（通常是已筛选过的可异步化工具，例如 bcs-write 全集）；
// 工具名通过 tool.Declaration().Name 取。
//
// 安全策略：建议只注册"写操作且耗时 > 10s"的工具；读操作 LLM 直接同步调就行，
// 无意义放 async 只会让对话变复杂。
func RegisterToolsForAsync(registry *async.ToolRegistry, toolList []tool.Tool) {
	if registry == nil {
		return
	}
	for _, t := range toolList {
		if t == nil {
			continue
		}
		name := t.Declaration().Name
		registry.Register(name, t)
	}
}
