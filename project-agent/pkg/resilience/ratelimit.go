package resilience

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrRateLimited 触发限流时返回。
var ErrRateLimited = errors.New("resilience: rate limited")

// RateLimiter 单 key 令牌桶。线程安全。
//
// 设计要点：
//   - 容量 capacity，恢复速率 rate 个/秒
//   - 不依赖时间轮 / 第三方库，每次 take 时按时间差补桶
//   - 支持 Wait（阻塞等待） / Allow（立即判定）
type RateLimiter struct {
	mu       sync.Mutex
	capacity float64
	rate     float64 // tokens per second
	tokens   float64
	last     time.Time
	now      func() time.Time
}

// RateLimitConfig 配置。
type RateLimitConfig struct {
	// Capacity 桶容量（最大爆发）。<=0 用 1。
	Capacity float64
	// RatePerSecond 每秒恢复 token 数。<=0 用 1。
	RatePerSecond float64
	// Now 时间源（测试用）。
	Now func() time.Time
}

// NewRateLimiter 构造单 key 限流器。
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	cap := cfg.Capacity
	if cap <= 0 {
		cap = 1
	}
	rate := cfg.RatePerSecond
	if rate <= 0 {
		rate = 1
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{
		capacity: cap,
		rate:     rate,
		tokens:   cap,
		last:     now(),
		now:      now,
	}
}

// Allow 立即判定能否放行 1 个 token。
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refill()
	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}

// Wait 阻塞等待获取 1 个 token，直到 ctx 取消。
func (r *RateLimiter) Wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		r.refill()
		if r.tokens >= 1 {
			r.tokens--
			r.mu.Unlock()
			return nil
		}
		// 计算还需多久补到 1 个 token
		need := (1 - r.tokens) / r.rate
		r.mu.Unlock()

		t := time.NewTimer(time.Duration(need * float64(time.Second)))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Do 用令牌桶包装一次调用：fail-fast（不等待）。
func (r *RateLimiter) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if !r.Allow() {
		return ErrRateLimited
	}
	return fn(ctx)
}

func (r *RateLimiter) refill() {
	now := r.now()
	delta := now.Sub(r.last).Seconds()
	if delta <= 0 {
		return
	}
	r.tokens += delta * r.rate
	if r.tokens > r.capacity {
		r.tokens = r.capacity
	}
	r.last = now
}

// =================== 多 key 限流器 ===================

// KeyedRateLimiter 按 key 维度独立限流（如按 user_id / cluster / tool_name）。
//
// 注意：内部使用 sync.Map，长期不活跃的 key 不会自动回收；
// 业务方应在合适的时机调用 Forget 或重启进程。
type KeyedRateLimiter struct {
	cfg     RateLimitConfig
	limiter sync.Map // map[string]*RateLimiter
}

// NewKeyedRateLimiter 构造多 key 限流器。
func NewKeyedRateLimiter(cfg RateLimitConfig) *KeyedRateLimiter {
	return &KeyedRateLimiter{cfg: cfg}
}

// Allow 对指定 key 立即判定。
func (k *KeyedRateLimiter) Allow(key string) bool {
	return k.get(key).Allow()
}

// Do 对指定 key fail-fast 包装。
func (k *KeyedRateLimiter) Do(ctx context.Context, key string, fn func(ctx context.Context) error) error {
	return k.get(key).Do(ctx, fn)
}

// Forget 移除某 key 的限流器（释放内存）。
func (k *KeyedRateLimiter) Forget(key string) {
	k.limiter.Delete(key)
}

func (k *KeyedRateLimiter) get(key string) *RateLimiter {
	if v, ok := k.limiter.Load(key); ok {
		return v.(*RateLimiter)
	}
	rl := NewRateLimiter(k.cfg)
	actual, _ := k.limiter.LoadOrStore(key, rl)
	return actual.(*RateLimiter)
}
