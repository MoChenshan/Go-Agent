package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// State 熔断器状态。
type State int32

const (
	// StateClosed 关闭：正常放行，统计失败率。
	StateClosed State = iota
	// StateOpen 打开：拒绝所有请求，避免雪崩。
	StateOpen
	// StateHalfOpen 半开：允许少量探测请求，按结果决定回到 Closed 或退回 Open。
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen 熔断器打开时返回的标记错误。
var ErrCircuitOpen = errors.New("resilience: circuit breaker is open")

// BreakerConfig 熔断器配置。
type BreakerConfig struct {
	// Name 熔断器名称（用于 metrics/log）。
	Name string
	// Window 滑动窗口长度。在窗口内统计请求总数与失败数。默认 30s。
	Window time.Duration
	// MinRequests 触发熔断判定的最小请求数（避免少量请求误判）。默认 20。
	MinRequests uint64
	// FailureRate 触发熔断的失败率阈值 [0,1]。默认 0.5（50%）。
	FailureRate float64
	// ConsecutiveFailures 连续失败到此值立即熔断。0 表示禁用。默认 5。
	ConsecutiveFailures uint64
	// OpenTimeout Open 状态持续时间，到期后转 HalfOpen。默认 10s。
	OpenTimeout time.Duration
	// HalfOpenMaxCalls HalfOpen 状态允许的最大并发探测请求数。默认 1。
	HalfOpenMaxCalls uint64
	// IsFailure 自定义"失败"判定。nil 表示 err != nil 即失败。
	IsFailure func(err error) bool
	// OnStateChange 状态变化回调（用于 metrics）。
	OnStateChange func(name string, from, to State)
	// Now 时间源，便于测试。nil 用 time.Now。
	Now func() time.Time
}

func (c *BreakerConfig) defaults() {
	if c.Window <= 0 {
		c.Window = 30 * time.Second
	}
	if c.MinRequests == 0 {
		c.MinRequests = 20
	}
	if c.FailureRate <= 0 || c.FailureRate > 1 {
		c.FailureRate = 0.5
	}
	if c.ConsecutiveFailures == 0 {
		c.ConsecutiveFailures = 5
	}
	if c.OpenTimeout <= 0 {
		c.OpenTimeout = 10 * time.Second
	}
	if c.HalfOpenMaxCalls == 0 {
		c.HalfOpenMaxCalls = 1
	}
	if c.IsFailure == nil {
		c.IsFailure = func(err error) bool { return err != nil }
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// Breaker 三态熔断器。线程安全。
type Breaker struct {
	cfg BreakerConfig

	mu              sync.Mutex
	state           State
	openedAt        time.Time
	consecFailures  uint64
	halfOpenInFlight uint64

	// 滑动窗口（简易实现：每秒一个 bucket）
	bucketsMu sync.Mutex
	buckets   []bucket
	lastTick  int64
}

type bucket struct {
	tsSec uint64 // 桶的秒时间戳
	total atomic.Uint64
	fails atomic.Uint64
}

// NewBreaker 构造一个新的熔断器。
func NewBreaker(cfg BreakerConfig) *Breaker {
	cfg.defaults()
	bWindow := int(cfg.Window.Seconds())
	if bWindow < 1 {
		bWindow = 1
	}
	b := &Breaker{
		cfg:     cfg,
		state:   StateClosed,
		buckets: make([]bucket, bWindow),
	}
	return b
}

// State 返回当前状态（不会驱动状态机，仅观测用）。
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Do 在熔断保护下执行 fn。
//
// 返回 ErrCircuitOpen 表示被熔断器拒绝；其他 error 是 fn 的真实错误。
func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := b.allow(); err != nil {
		return err
	}

	err := fn(ctx)
	b.report(err)
	return err
}

// allow 决定是否放行；同时驱动 Open→HalfOpen 的转换。
func (b *Breaker) allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return nil
	case StateOpen:
		if b.cfg.Now().Sub(b.openedAt) >= b.cfg.OpenTimeout {
			b.transition(StateHalfOpen)
			b.halfOpenInFlight = 1
			return nil
		}
		return ErrCircuitOpen
	case StateHalfOpen:
		if b.halfOpenInFlight >= b.cfg.HalfOpenMaxCalls {
			return ErrCircuitOpen
		}
		b.halfOpenInFlight++
		return nil
	}
	return nil
}

// report 处理 fn 返回的结果，驱动状态机。
func (b *Breaker) report(err error) {
	failed := b.cfg.IsFailure(err)
	b.recordBucket(failed)

	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		if failed {
			b.consecFailures++
			if b.consecFailures >= b.cfg.ConsecutiveFailures {
				b.transition(StateOpen)
				b.openedAt = b.cfg.Now()
				return
			}
			// 窗口失败率判定
			total, fails := b.windowStats()
			if total >= b.cfg.MinRequests &&
				float64(fails)/float64(total) >= b.cfg.FailureRate {
				b.transition(StateOpen)
				b.openedAt = b.cfg.Now()
			}
		} else {
			b.consecFailures = 0
		}
	case StateHalfOpen:
		if b.halfOpenInFlight > 0 {
			b.halfOpenInFlight--
		}
		if failed {
			// 半开探测失败 → 退回 Open
			b.transition(StateOpen)
			b.openedAt = b.cfg.Now()
		} else {
			// 半开探测成功 → 直接闭合并清空窗口
			b.transition(StateClosed)
			b.consecFailures = 0
			b.resetBuckets()
		}
	}
}

// transition 状态切换，触发回调。调用方需持有 b.mu。
func (b *Breaker) transition(to State) {
	from := b.state
	if from == to {
		return
	}
	b.state = to
	if b.cfg.OnStateChange != nil {
		// 异步回调，避免阻塞调用方
		go b.cfg.OnStateChange(b.cfg.Name, from, to)
	}
}

// ============== 滑动窗口 ==============

func (b *Breaker) currentBucket() *bucket {
	tsSec := uint64(b.cfg.Now().Unix())
	idx := int(tsSec) % len(b.buckets)

	b.bucketsMu.Lock()
	defer b.bucketsMu.Unlock()
	bk := &b.buckets[idx]
	if bk.tsSec != tsSec {
		// 老桶被覆盖
		bk.tsSec = tsSec
		bk.total.Store(0)
		bk.fails.Store(0)
	}
	return bk
}

func (b *Breaker) recordBucket(failed bool) {
	bk := b.currentBucket()
	bk.total.Add(1)
	if failed {
		bk.fails.Add(1)
	}
}

func (b *Breaker) windowStats() (total, fails uint64) {
	tsSec := uint64(b.cfg.Now().Unix())
	cutoff := tsSec - uint64(b.cfg.Window.Seconds()) + 1

	b.bucketsMu.Lock()
	defer b.bucketsMu.Unlock()
	for i := range b.buckets {
		bk := &b.buckets[i]
		if bk.tsSec >= cutoff && bk.tsSec <= tsSec {
			total += bk.total.Load()
			fails += bk.fails.Load()
		}
	}
	return
}

func (b *Breaker) resetBuckets() {
	b.bucketsMu.Lock()
	defer b.bucketsMu.Unlock()
	for i := range b.buckets {
		b.buckets[i].tsSec = 0
		b.buckets[i].total.Store(0)
		b.buckets[i].fails.Store(0)
	}
}
