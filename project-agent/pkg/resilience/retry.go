package resilience

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Retryable 决定一个 error 是否值得重试。
// 业务方可注入自己的判定（例如：HTTP 5xx / RST / context.DeadlineExceeded
// 可重试，HTTP 4xx 不可重试）。
type Retryable func(err error) bool

// DefaultRetryable 是默认的"保守"判定：
//   - context.Canceled 不重试（上游主动取消）
//   - context.DeadlineExceeded 不重试（已超总预算）
//   - 其他 error 都重试
//
// 业务侧通常会覆盖此判定。
func DefaultRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// RetryConfig 重试参数。
type RetryConfig struct {
	// MaxAttempts 最大尝试次数（含首次），>=1。默认 3。
	MaxAttempts int
	// InitialInterval 初始等待时间。默认 100ms。
	InitialInterval time.Duration
	// MaxInterval 最大单次等待时间。默认 5s。
	MaxInterval time.Duration
	// Multiplier 指数退避倍数。默认 2.0。
	Multiplier float64
	// JitterFraction 抖动比例 [0,1]。默认 0.2 表示 ±20%。
	JitterFraction float64
	// Retryable 自定义错误判定。nil 表示用 DefaultRetryable。
	Retryable Retryable
	// OnRetry 每次重试前回调（attempt 从 1 开始；attempt=2 表示第一次重试）。
	OnRetry func(attempt int, err error, nextWait time.Duration)
}

// defaults 把 0 值替换为合理默认。
func (c *RetryConfig) defaults() {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.InitialInterval <= 0 {
		c.InitialInterval = 100 * time.Millisecond
	}
	if c.MaxInterval <= 0 {
		c.MaxInterval = 5 * time.Second
	}
	if c.Multiplier <= 1 {
		c.Multiplier = 2.0
	}
	if c.JitterFraction < 0 {
		c.JitterFraction = 0
	}
	if c.JitterFraction > 1 {
		c.JitterFraction = 1
	}
	if c.Retryable == nil {
		c.Retryable = DefaultRetryable
	}
}

// Do 按 cfg 重试 fn，直到成功、达到 MaxAttempts、被 ctx 取消或遇到不可重试错误。
//
// 返回值：
//   - 最后一次的 error；nil 表示某次尝试成功
//   - 即便是不可重试错误，也会原样返回，便于上游用 errors.Is/As 判断
func Do(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	cfg.defaults()

	var lastErr error
	wait := cfg.InitialInterval
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// 提前检查 ctx，避免做无意义的尝试
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		// 不可重试 → 直接返回
		if !cfg.Retryable(err) {
			return err
		}
		// 已是最后一次尝试 → 不再 sleep，直接返回
		if attempt == cfg.MaxAttempts {
			return err
		}

		next := withJitter(wait, cfg.JitterFraction)
		if cfg.OnRetry != nil {
			cfg.OnRetry(attempt+1, err, next)
		}

		// 等待 + 同步 ctx
		t := time.NewTimer(next)
		select {
		case <-ctx.Done():
			t.Stop()
			return errors.Join(lastErr, ctx.Err())
		case <-t.C:
		}

		// 指数退避
		wait = time.Duration(float64(wait) * cfg.Multiplier)
		if wait > cfg.MaxInterval {
			wait = cfg.MaxInterval
		}
	}
	return lastErr
}

// withJitter 给 base 加上 ±frac*base 的均匀抖动，避免雷鸣群效应。
func withJitter(base time.Duration, frac float64) time.Duration {
	if frac <= 0 {
		return base
	}
	delta := float64(base) * frac
	// rand.Float64() ∈ [0,1)，映射到 [-delta, +delta]
	jitter := (rand.Float64()*2 - 1) * delta // #nosec G404 — 抖动用，无安全要求
	d := time.Duration(float64(base) + jitter)
	if d < 0 {
		return 0
	}
	return d
}
