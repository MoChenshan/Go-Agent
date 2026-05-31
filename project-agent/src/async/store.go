// store.go —— AsyncJob 存储抽象 + 内存实现。
//
// 为什么从 MemStore 开局：
//
//   1) Job 是"暂存状态"：典型寿命 10s~10min，覆盖范围是"LLM 多轮对话内"，
//      进程重启后这些 Job 哪怕保留状态，绑定的 context/goroutine 也已失效；
//      即便 FileStore 把状态写到磁盘，恢复后 cancel 权、Wait 唤醒都是断的。
//   2) 从 MemStore 平移到 FileStore/DB 只是一个 "Put/Get/List/Update" 的适配问题；
//      本轮把接口打对，持久化留到 D19.2.1 再做。
//
// 并发模型：
//   - RWMutex：查询远多于写入（多次 job_status 对应一次 Put + 几次 Update）
//   - Update 用 mutator 回调 pattern，保证"读-改-写"原子性，避免 ABA
//   - 禁止调用方持有内部指针：Get/List 一律 Clone 外发
package async

import (
	"context"
	"sort"
	"sync"
)

// JobFilter 用于 List 的筛选条件。零值视为"全部"。
type JobFilter struct {
	// ToolName 仅列出指定工具名的 Job（空串忽略）
	ToolName string
	// Statuses 仅列出处于这些状态的 Job（nil/空视为全部状态）
	Statuses []JobStatus
	// Limit 返回条数上限；0 = 不限（List 全部，不建议在大批量场景用）
	Limit int
}

// JobStore 异步任务存储接口。
//
// 所有实现都必须：
//   - 并发安全
//   - Get/List 返回 Clone 而非内部指针
//   - Update 原子执行 mutator
type JobStore interface {
	Put(ctx context.Context, j *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	List(ctx context.Context, f JobFilter) ([]*Job, error)
	// Update 以 mutator 方式更新：加锁 → 取当前 Job → 调用 mutator 修改 → 存回。
	// 如果 mutator 返回 error，整次更新回滚（不存回）。
	Update(ctx context.Context, id string, mutator func(*Job) error) error
	// Delete 移除一个 Job；不存在返回 ErrJobNotFound。
	Delete(ctx context.Context, id string) error
	// Len 当前存储的 Job 总数（含终态）。
	Len() int
}

// =============================================================================
// MemStore —— 默认内存实现
// =============================================================================

// MemStore sync.RWMutex 守护的 map 实现。
type MemStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

// NewMemStore 创建空 MemStore。
func NewMemStore() *MemStore {
	return &MemStore{jobs: make(map[string]*Job)}
}

// Put 存入新 Job（或覆盖同 ID）。JobID 必须非空。
func (s *MemStore) Put(_ context.Context, j *Job) error {
	if j == nil || j.ID == "" {
		return ErrJobNotFound // ID 缺失按"找不到"处理，避免额外错误类型
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
	return nil
}

// Get 返回 Job 的 Clone 副本；不存在返回 ErrJobNotFound。
func (s *MemStore) Get(_ context.Context, id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	return j.Clone(), nil
}

// List 按 filter 返回 Job Clone 列表，按 SubmittedAt 降序（新的在前）。
func (s *MemStore) List(_ context.Context, f JobFilter) ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statusSet := make(map[JobStatus]struct{}, len(f.Statuses))
	for _, st := range f.Statuses {
		statusSet[st] = struct{}{}
	}

	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		if f.ToolName != "" && j.ToolName != f.ToolName {
			continue
		}
		if len(statusSet) > 0 {
			if _, ok := statusSet[j.Status]; !ok {
				continue
			}
		}
		out = append(out, j.Clone())
	}
	// 新的在前，便于 LLM 看到最近提交
	sort.Slice(out, func(i, k int) bool {
		return out[i].SubmittedAt.After(out[k].SubmittedAt)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// Update 原子地对指定 Job 应用 mutator 函数。
//
// 语义：
//   - 取得写锁
//   - Get 最新 Job（直接拿内部指针，不 Clone —— mutator 在锁内读写安全）
//   - mutator 返回 err → 不提交，直接上抛
//   - mutator 返回 nil → Job 已原地改完，无需再 put（引用就是原指针）
func (s *MemStore) Update(_ context.Context, id string, mutator func(*Job) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	return mutator(j)
}

// Delete 移除 Job；不存在返回 ErrJobNotFound。
func (s *MemStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return ErrJobNotFound
	}
	delete(s.jobs, id)
	return nil
}

// Len 当前 Job 总数。
func (s *MemStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

// findByIdempotencyKey 辅助：按幂等 key 查已有 Job（仅 MemStore 内部使用）。
//
// Runner 在 Submit 时会调用此方法实现幂等：同一 key 未终态的 Job 直接复用。
// 接受重复 key 的旧 Job 是终态的场景 —— 让它复用还是重新起？当前策略是"复用，
// 让调用方通过 job_status 看到终态"；如果想强制重新执行，调用方应传新 key。
func (s *MemStore) findByIdempotencyKey(key string) *Job {
	if key == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, j := range s.jobs {
		if j.IdempotencyKey == key {
			return j.Clone()
		}
	}
	return nil
}
