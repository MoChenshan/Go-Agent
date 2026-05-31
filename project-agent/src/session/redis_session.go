//go:build redis
// +build redis

// Redis 后端真实实现。仅在 -tags=redis 构建时生效。
//
// 装配要点：
//   - 通过 SESSION_REDIS_ADDR 注入 Redis 地址（redis://host:port 或 host:port）
//   - 通过 SESSION_REDIS_PASSWORD 注入密码（可选）
//   - 通过 SESSION_REDIS_DB 注入 DB 编号（默认 0）
//   - 仍保留原 inmem 路径下的 summarizer 逻辑（Redis 仅替换底层 KV）
//
// 使用公开版 trpc.group/trpc-go/trpc-agent-go/session/redis 实现。
package session

import (
	"fmt"
	"os"
	"strconv"
	"time"

	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/session"
	redissess "trpc.group/trpc-go/trpc-agent-go/session/redis"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
)

func newRedisSession(cfg Config, model *openaimodel.Model) session.Service {
	addr := os.Getenv("SESSION_REDIS_ADDR")
	if addr == "" {
		// 必须显式提供地址；空地址等同未启用
		return nil
	}
	password := os.Getenv("SESSION_REDIS_PASSWORD")
	db := 0
	if v := os.Getenv("SESSION_REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			db = n
		}
	}

	// 构造 Redis URL：redis://:password@host:port/db
	redisURL := buildRedisURL(addr, password, db)

	opts := []redissess.ServiceOpt{
		redissess.WithRedisClientURL(redisURL),
		redissess.WithSessionEventLimit(cfg.EventLimit),
	}
	if model != nil {
		sum := summary.NewSummarizer(model,
			summary.WithMaxSummaryWords(cfg.MaxSummaryWords),
			summary.WithChecksAny(
				summary.CheckEventThreshold(cfg.EventThreshold),
				summary.CheckTokenThreshold(cfg.TokenThreshold),
				summary.CheckTimeThreshold(cfg.TimeThreshold),
			),
		)
		opts = append(opts,
			redissess.WithSummarizer(sum),
			redissess.WithAsyncSummaryNum(cfg.AsyncWorkers),
			redissess.WithSummaryQueueSize(cfg.QueueSize),
			redissess.WithSummaryJobTimeout(cfg.JobTimeout),
		)
	}
	svc, err := redissess.NewService(opts...)
	if err != nil {
		// 构造失败降级为 nil，上层会 fallback 到 inmemory
		fmt.Fprintf(os.Stderr, "redis session: init failed: %v\n", err)
		return nil
	}
	return svc
}

// buildRedisURL 根据 addr/password/db 构造 Redis URL。
func buildRedisURL(addr, password string, db int) string {
	// 如果已经是 redis:// 格式则直接返回
	if len(addr) > 8 && addr[:8] == "redis://" {
		return addr
	}
	if len(addr) > 9 && addr[:9] == "rediss://" {
		return addr
	}
	// 构造标准 redis URL
	var userInfo string
	if password != "" {
		userInfo = ":" + password + "@"
	}
	return fmt.Sprintf("redis://%s%s/%d", userInfo, addr, db)
}

// redisSessionTimeout 用于 summary job 的默认超时。
var _ = time.Second // 确保 time 包被使用
