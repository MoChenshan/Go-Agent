// job_test.go —— 基础类型层测试：状态机 / Clone / ToolRegistry。
package async

import (
	"testing"
	"time"
)

func TestJobStatus_IsTerminal(t *testing.T) {
	terminal := []JobStatus{StatusSucceeded, StatusFailed, StatusCancelled, StatusTimedOut}
	nonTerminal := []JobStatus{StatusPending, StatusRunning, "weird", ""}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q 应为终态", s)
		}
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%q 不应为终态", s)
		}
	}
}

func TestJob_Clone(t *testing.T) {
	now := time.Now()
	j := &Job{
		ID:          "j1",
		ToolName:    "foo",
		Args:        map[string]any{"x": 1},
		Status:      StatusRunning,
		Progress:    &Progress{UpdatedAt: now, Fields: map[string]any{"p": 0.5}},
		SubmittedAt: now,
		cancelFn:    func() { t.Fatal("不应被调用") },
	}
	cp := j.Clone()
	if cp == j {
		t.Error("Clone 应返回新指针")
	}
	if cp.cancelFn != nil {
		t.Error("Clone 必须剥离 cancelFn，避免外部意外 cancel")
	}
	if cp.Progress == j.Progress {
		t.Error("Progress 应深拷贝")
	}
	if cp.Progress.Fields["p"] != 0.5 {
		t.Error("Progress.Fields 应保留")
	}
	// nil 安全
	var nilJob *Job
	if nilJob.Clone() != nil {
		t.Error("nil.Clone 应返回 nil")
	}
}

func TestToolRegistry_Basic(t *testing.T) {
	r := NewToolRegistry()

	// 空 / nil 的忽略
	r.Register("", "x")
	r.Register("y", nil)
	if len(r.Names()) != 0 {
		t.Errorf("空/nil 不应注册，实际 names=%v", r.Names())
	}

	r.Register("foo", "TOOL_FOO")
	r.Register("bar", "TOOL_BAR")
	if got, ok := r.Lookup("foo"); !ok || got != "TOOL_FOO" {
		t.Errorf("Lookup foo 错：%v %v", got, ok)
	}
	if _, ok := r.Lookup("baz"); ok {
		t.Error("Lookup baz 不应存在")
	}

	// 覆盖注册
	r.Register("foo", "TOOL_FOO_V2")
	if got, _ := r.Lookup("foo"); got != "TOOL_FOO_V2" {
		t.Errorf("覆盖注册失败：%v", got)
	}

	names := r.Names()
	if len(names) != 2 {
		t.Errorf("names 长度应 2，实际 %v", names)
	}
}
