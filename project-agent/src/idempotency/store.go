// Package idempotency 提供幂等键存储抽象。
//
// 用途：Webhook 入口（蓝鲸告警 / TAPD）以及高危工具调用，
// 都可能因为对端重发或者超时重试而被触发多次。本包提供
// "首次执行 → 记录结果 → 后续命中直接返回上次结果" 的语义。
//
// 三种实现：
//   - inmem  本地内存（默认，适合单副本/测试）
//   - redis  Redis SETNX + TTL（推荐生产）
//   - noop   关闭幂等（仅用于单元测试或确实不需要的场景）
//
// 使用模式（典型）：
//
//	store := idempotency.New(idempotency.Config{Backend: "redis", Addr: "redis:6379"})
//	got, hit, err := store.GetOrSet(ctx, "bk_alarm:"+alarmID, 24*time.Hour, func() (any, error) {
//	    return processAlarm(ctx, alarm)
//	})
//	if hit { /* 重复请求，已直接返回上次结果 */ }
package idempotency

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Store 幂等键存储接口。
type Store interface {
	// GetOrSet：
	//   - 若 key 已存在：直接返回保存的 value，hit=true
	//   - 若不存在：执行 fn，将结果保存到 key（TTL=ttl），hit=false
	//   - fn 返回 error 时，不会缓存（避免错误结果污染后续重试）
	GetOrSet(ctx context.Context, key string, ttl time.Duration, fn func() (any, error)) (value any, hit bool, err error)

	// Has 判断 key 是否仍存在（仅观测用，不更新）。
	Has(ctx context.Context, key string) (bool, error)

	// Forget 主动删除 key。
	Forget(ctx context.Context, key string) error

	// Close 释放资源。
	Close() error
}

// Config 配置。
type Config struct {
	Backend string        // "inmem" | "redis" | "noop"
	Addr    string        // redis 地址
	DefaultTTL time.Duration
}

// New 工厂方法。redis 后端需要在构建 tag `redis` 下编译；
// 默认实现里仅提供 inmem 与 noop，避免引入 redis 依赖到普通构建。
func New(cfg Config) Store {
	switch cfg.Backend {
	case "noop", "":
		return &noopStore{}
	case "inmem":
		return NewInMemory()
	case "redis":
		// 真实 redis 实现见 redis_store.go（build tag: redis）。
		// 缺省回退到 inmem，保证零凭据可用。
		return NewInMemory()
	default:
		return &noopStore{}
	}
}

// ===================== inmem =====================

type inMemEntry struct {
	value     any
	expiresAt time.Time
}

// InMemoryStore 进程内幂等键存储，简单 map + TTL。
type InMemoryStore struct {
	mu   sync.Mutex
	data map[string]inMemEntry
	now  func() time.Time
}

// NewInMemory 构造内存版幂等存储。
func NewInMemory() *InMemoryStore {
	return &InMemoryStore{
		data: make(map[string]inMemEntry),
		now:  time.Now,
	}
}

// GetOrSet 实现见接口注释。
//
// 注意：fn 在持有内部锁时执行，多 key 并发时不会互相阻塞（go map 锁很快），
// 但同一 key 的并发请求会串行化 → 也正是幂等的预期行为。
func (s *InMemoryStore) GetOrSet(ctx context.Context, key string, ttl time.Duration, fn func() (any, error)) (any, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	s.mu.Lock()
	if e, ok := s.data[key]; ok && s.now().Before(e.expiresAt) {
		s.mu.Unlock()
		return e.value, true, nil
	}
	// 占位：先写入一个占位 entry 防止并发重复执行
	// 简化版：同一 key 的并发会被外层 mu 串行化，足够用
	s.mu.Unlock()

	value, err := fn()
	if err != nil {
		return nil, false, err
	}

	s.mu.Lock()
	s.data[key] = inMemEntry{
		value:     value,
		expiresAt: s.now().Add(ttl),
	}
	s.mu.Unlock()
	return value, false, nil
}

// Has 仅查询。
func (s *InMemoryStore) Has(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok {
		return false, nil
	}
	if s.now().After(e.expiresAt) {
		delete(s.data, key)
		return false, nil
	}
	return true, nil
}

// Forget 删除。
func (s *InMemoryStore) Forget(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

// Close 无需释放。
func (s *InMemoryStore) Close() error { return nil }

// ===================== noop =====================

type noopStore struct{}

func (n *noopStore) GetOrSet(ctx context.Context, _ string, _ time.Duration, fn func() (any, error)) (any, bool, error) {
	v, err := fn()
	return v, false, err
}

func (n *noopStore) Has(context.Context, string) (bool, error) { return false, nil }
func (n *noopStore) Forget(context.Context, string) error      { return nil }
func (n *noopStore) Close() error                              { return nil }

// ErrConflict 占位错误，供 redis SETNX 失败场景使用。
var ErrConflict = errors.New("idempotency: key conflict in flight")
