// Package session 封装 trpc-agent-go 框架的 session.Service，
// 提供 GameOps Agent 的会话与记忆能力。
//
// 设计目标：
//  1. 多轮对话记忆：HITL 场景下"先展示 Plan → 用户确认 → 执行"需要跨回合保持上下文。
//  2. 长会话自动总结：超过阈值时触发 LLM 总结，避免上下文窗口被塞爆。
//  3. 零凭据降级：未配置 LLM 时返回纯内存 session（仍保留多轮记忆，只是不会自动总结）。
//
// 参考实现：oncall_agent/wire.go 的 provideSessionService，
//         trpc-agent-go/examples/summary/main.go。
package session

import (
	"os"
	"strconv"
	"time"

	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
)

// Config 会话服务配置。所有字段都有合理默认值，可被环境变量覆盖。
type Config struct {
	// EventThreshold 事件数量触发阈值；超过则异步调用 LLM 生成摘要。
	//  - 默认 20；env: SESSION_EVENT_THRESHOLD
	EventThreshold int
	// TokenThreshold 估算 token 触发阈值；0 表示禁用此维度。
	//  - 默认 6000；env: SESSION_TOKEN_THRESHOLD
	TokenThreshold int
	// TimeThreshold 距离上一次事件的时间触发阈值；0 表示禁用。
	//  - 默认 10min；env: SESSION_TIME_THRESHOLD_MIN（单位：分钟）
	TimeThreshold time.Duration
	// MaxSummaryWords 摘要最大字数限制；0 表示不限。默认 500。
	MaxSummaryWords int
	// EventLimit session 保留的事件上限（超过则淘汰旧事件，摘要会被当作新事件注入）。
	// 默认 = EventThreshold * 2
	EventLimit int
	// AsyncWorkers 异步摘要 worker 数量。默认 2。
	AsyncWorkers int
	// QueueSize 摘要任务队列大小。默认 100。
	QueueSize int
	// JobTimeout 单次摘要任务超时。默认 60s。
	JobTimeout time.Duration
}

// DefaultConfig 返回默认配置（可被环境变量覆盖）。
func DefaultConfig() Config {
	c := Config{
		EventThreshold:  envInt("SESSION_EVENT_THRESHOLD", 20),
		TokenThreshold:  envInt("SESSION_TOKEN_THRESHOLD", 6000),
		TimeThreshold:   time.Duration(envInt("SESSION_TIME_THRESHOLD_MIN", 10)) * time.Minute,
		MaxSummaryWords: 500,
		AsyncWorkers:    2,
		QueueSize:       100,
		JobTimeout:      60 * time.Second,
	}
	if c.EventLimit == 0 {
		c.EventLimit = c.EventThreshold * 2
	}
	return c
}

// New 根据配置构造 session.Service：
//   - model != nil：启用 LLM summarizer，支持自动总结
//   - model == nil：返回纯内存 session（仍保留多轮事件，只是不会生成摘要）
//
// 永远不返回 error —— 降级优于失败，让服务能够在凭据不完整时仍然可启动。
func New(cfg Config, model *openaimodel.Model) session.Service {
	if cfg.EventThreshold <= 0 {
		cfg = DefaultConfig()
	}
	if model == nil {
		// 降级路径：不带 summarizer 的纯内存 session
		return inmemory.NewSessionService(
			inmemory.WithSessionEventLimit(cfg.EventLimit),
		)
	}

	// 带 summarizer 的完整版
	sum := summary.NewSummarizer(model,
		summary.WithMaxSummaryWords(cfg.MaxSummaryWords),
		summary.WithChecksAny(
			summary.CheckEventThreshold(cfg.EventThreshold),
			summary.CheckTokenThreshold(cfg.TokenThreshold),
			summary.CheckTimeThreshold(cfg.TimeThreshold),
		),
	)
	return inmemory.NewSessionService(
		inmemory.WithSummarizer(sum),
		inmemory.WithSessionEventLimit(cfg.EventLimit),
		inmemory.WithAsyncSummaryNum(cfg.AsyncWorkers),
		inmemory.WithSummaryQueueSize(cfg.QueueSize),
		inmemory.WithSummaryJobTimeout(cfg.JobTimeout),
	)
}

// envInt 读取 env 整数，缺失/无效时返回默认值。
func envInt(key string, def int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}
