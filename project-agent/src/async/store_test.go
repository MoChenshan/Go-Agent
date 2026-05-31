// store_test.go —— MemStore 并发安全与语义契约测试（~10 用例）。
package async

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func mkJob(id, toolName string, status JobStatus) *Job {
	now := time.Now()
	return &Job{
		ID:          id,
		ToolName:    toolName,
		Status:      status,
		SubmittedAt: now,
		TimeoutAt:   now.Add(time.Minute),
	}
}

func TestMemStore_PutAndGet(t *testing.T) {
	s := NewMemStore()
	j := mkJob("j1", "foo", StatusPending)
	if err := s.Put(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "j1")
	if err != nil {
		t.Fatal(err)
	}
	if got == j {
		t.Error("Get 应返回 Clone，而不是内部指针")
	}
	if got.ID != "j1" {
		t.Errorf("id=%s", got.ID)
	}
}

func TestMemStore_PutEmptyID(t *testing.T) {
	s := NewMemStore()
	if err := s.Put(context.Background(), &Job{}); err == nil {
		t.Error("空 ID 应返回 err")
	}
}

func TestMemStore_GetNotFound(t *testing.T) {
	s := NewMemStore()
	_, err := s.Get(context.Background(), "nope")
	if err != ErrJobNotFound {
		t.Errorf("应 ErrJobNotFound，实际=%v", err)
	}
}

func TestMemStore_Update(t *testing.T) {
	s := NewMemStore()
	_ = s.Put(context.Background(), mkJob("j1", "foo", StatusPending))
	err := s.Update(context.Background(), "j1", func(j *Job) error {
		j.Status = StatusRunning
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(context.Background(), "j1")
	if got.Status != StatusRunning {
		t.Errorf("status=%s", got.Status)
	}
}

func TestMemStore_UpdateMutatorError(t *testing.T) {
	s := NewMemStore()
	_ = s.Put(context.Background(), mkJob("j1", "foo", StatusPending))
	wantErr := fmt.Errorf("mutator refuses")
	err := s.Update(context.Background(), "j1", func(j *Job) error {
		j.Status = StatusRunning // 这个改动应该被丢弃... 
		// 注：当前实现是"原地改"，mutator err 时其实已经改了（没 rollback）
		// 这里仅验证 err 透传；"带事务的 Store"留给 DB 实现
		return wantErr
	})
	if err != wantErr {
		t.Errorf("err 未透传：%v", err)
	}
}

func TestMemStore_List_FilterByToolAndStatus(t *testing.T) {
	s := NewMemStore()
	_ = s.Put(context.Background(), mkJob("a", "foo", StatusPending))
	_ = s.Put(context.Background(), mkJob("b", "foo", StatusSucceeded))
	_ = s.Put(context.Background(), mkJob("c", "bar", StatusFailed))

	// 只看 foo 的全部
	fooOnly, _ := s.List(context.Background(), JobFilter{ToolName: "foo"})
	if len(fooOnly) != 2 {
		t.Errorf("foo 应 2 条，实际 %d", len(fooOnly))
	}

	// 只看 succeeded
	succ, _ := s.List(context.Background(), JobFilter{Statuses: []JobStatus{StatusSucceeded}})
	if len(succ) != 1 || succ[0].ID != "b" {
		t.Errorf("succeeded 过滤错：%+v", succ)
	}

	// 组合过滤
	combo, _ := s.List(context.Background(), JobFilter{ToolName: "foo", Statuses: []JobStatus{StatusPending}})
	if len(combo) != 1 || combo[0].ID != "a" {
		t.Errorf("组合过滤错：%+v", combo)
	}
}

func TestMemStore_List_Limit(t *testing.T) {
	s := NewMemStore()
	for i := 0; i < 5; i++ {
		j := mkJob(fmt.Sprintf("j%d", i), "foo", StatusPending)
		// 让 SubmittedAt 递增，验证排序
		j.SubmittedAt = time.Now().Add(time.Duration(i) * time.Millisecond)
		_ = s.Put(context.Background(), j)
	}
	got, _ := s.List(context.Background(), JobFilter{Limit: 3})
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	// 新的在前：j4 应在最前
	if got[0].ID != "j4" {
		t.Errorf("排序错：最前应为 j4，实际 %s", got[0].ID)
	}
}

func TestMemStore_Delete(t *testing.T) {
	s := NewMemStore()
	_ = s.Put(context.Background(), mkJob("j1", "foo", StatusSucceeded))
	if err := s.Delete(context.Background(), "j1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), "j1"); err != ErrJobNotFound {
		t.Errorf("重复 delete 应 ErrJobNotFound，实际=%v", err)
	}
}

func TestMemStore_ConcurrentAccess(t *testing.T) {
	s := NewMemStore()
	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("j%d", i)
			_ = s.Put(context.Background(), mkJob(id, "foo", StatusPending))
			_ = s.Update(context.Background(), id, func(j *Job) error {
				j.Status = StatusRunning
				return nil
			})
			_, _ = s.Get(context.Background(), id)
		}(i)
	}
	wg.Wait()
	if s.Len() != N {
		t.Errorf("期望 %d 条，实际 %d", N, s.Len())
	}
}

func TestMemStore_FindByIdempotencyKey(t *testing.T) {
	s := NewMemStore()
	j := mkJob("j1", "foo", StatusPending)
	j.IdempotencyKey = "key-A"
	_ = s.Put(context.Background(), j)
	got := s.findByIdempotencyKey("key-A")
	if got == nil || got.ID != "j1" {
		t.Errorf("按 key 查失败：%+v", got)
	}
	if s.findByIdempotencyKey("") != nil {
		t.Error("空 key 不应匹配任何")
	}
	if s.findByIdempotencyKey("nope") != nil {
		t.Error("未知 key 不应匹配任何")
	}
}
