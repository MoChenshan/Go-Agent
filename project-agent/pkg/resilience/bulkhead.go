package resilience

import (
	"context"
	"errors"
	"time"
)

// ErrBulkheadFull 隔板已满时返回。
var ErrBulkheadFull = errors.New("resilience: bulkhead is full")

// Bulkhead 信号量隔板，限制对单个依赖的并发上限。
//
// 与限流（每秒 N 次）不同，隔板控制的是"同时在飞"数。当一个依赖卡顿时，
// 隔板会让请求快速失败而不是无限堆积，从而保护进程整体可用性。
type Bulkhead struct {
	name    string
	sem     chan struct{}
	timeout time.Duration
}

// BulkheadConfig 配置。
type BulkheadConfig struct {
	Name string
	// MaxConcurrent 最大并发数。<=0 视为 1。
	MaxConcurrent int
	// AcquireTimeout 等待槽位的最大时间。0 表示立即失败（fail-fast）。
	AcquireTimeout time.Duration
}

// NewBulkhead 构造一个新的隔板。
func NewBulkhead(cfg BulkheadConfig) *Bulkhead {
	n := cfg.MaxConcurrent
	if n <= 0 {
		n = 1
	}
	return &Bulkhead{
		name:    cfg.Name,
		sem:     make(chan struct{}, n),
		timeout: cfg.AcquireTimeout,
	}
}

// Do 在隔板保护下执行 fn。
func (b *Bulkhead) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := b.acquire(ctx); err != nil {
		return err
	}
	defer b.release()
	return fn(ctx)
}

// InFlight 返回当前在飞数（仅观测用）。
func (b *Bulkhead) InFlight() int {
	return len(b.sem)
}

// Capacity 返回最大并发数。
func (b *Bulkhead) Capacity() int {
	return cap(b.sem)
}

func (b *Bulkhead) acquire(ctx context.Context) error {
	// fail-fast 路径
	if b.timeout <= 0 {
		select {
		case b.sem <- struct{}{}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			return ErrBulkheadFull
		}
	}

	t := time.NewTimer(b.timeout)
	defer t.Stop()
	select {
	case b.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return ErrBulkheadFull
	}
}

func (b *Bulkhead) release() {
	select {
	case <-b.sem:
	default:
		// 不应到达此分支；防御性
	}
}
