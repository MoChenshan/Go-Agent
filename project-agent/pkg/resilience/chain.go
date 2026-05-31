package resilience

import (
	"context"
)

// Chain 把多个韧性原语组合成一个调用链：
//
//	rate -> bulkhead -> breaker -> retry -> fn
//
// 顺序原则：
//  1. RateLimit 在最外层：超过预算的请求最先被丢，不消耗其他资源
//  2. Bulkhead 次外层：限制单依赖在飞数，避免下游卡顿吃掉所有 goroutine
//  3. Breaker 中间：依据近期成功率快速失败，减少无谓重试
//  4. Retry 最内层：仅对真实调用包裹，避免重试时穿透限流/隔板
type Chain struct {
	Limiter *RateLimiter
	Bulk    *Bulkhead
	Breaker *Breaker
	Retry   *RetryConfig
}

// Do 按既定顺序执行。任意一层未配置则跳过该层。
func (c Chain) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	call := fn

	if c.Retry != nil {
		cfg := *c.Retry
		inner := call
		call = func(ctx context.Context) error {
			return Do(ctx, cfg, inner)
		}
	}
	if c.Breaker != nil {
		inner := call
		call = func(ctx context.Context) error {
			return c.Breaker.Do(ctx, inner)
		}
	}
	if c.Bulk != nil {
		inner := call
		call = func(ctx context.Context) error {
			return c.Bulk.Do(ctx, inner)
		}
	}
	if c.Limiter != nil {
		inner := call
		call = func(ctx context.Context) error {
			return c.Limiter.Do(ctx, inner)
		}
	}
	return call(ctx)
}
