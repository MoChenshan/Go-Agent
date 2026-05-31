package report

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 1. 空 Report → EmptyPrefix
func TestMockSummarizer_Empty(t *testing.T) {
	m := NewMockSummarizer()
	out, err := m.Summarize(context.Background(), Report{CaseID: "c1"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.HasPrefix(out, "Agent 已接收告警") {
		t.Errorf("want empty prefix, got: %q", out)
	}
}

// 2. 全部成功 → SuccessPrefix + 动作总览
func TestMockSummarizer_AllSuccess(t *testing.T) {
	m := NewMockSummarizer()
	r := Report{
		CaseID:    "c1",
		Diagnosis: "Old Gen 95% Full GC 超 30s",
		Actions: []Action{
			{Action: "bcs_helm_upgrade", Result: "success"},
			{Action: "bcs_helm_upgrade", Result: "success"},
			{Action: "gongfeng_mr_create", Result: "success"},
		},
	}
	out, _ := m.Summarize(context.Background(), r)
	if !strings.HasPrefix(out, "本次事件已自动收敛") {
		t.Errorf("want success prefix, got: %q", out)
	}
	if !strings.Contains(out, "共执行 3 个写操作") {
		t.Errorf("want action count, got: %q", out)
	}
	if !strings.Contains(out, "成功 3") {
		t.Errorf("want success count, got: %q", out)
	}
	if !strings.Contains(out, "bcs_helm_upgrade × 2") {
		t.Errorf("want top action freq, got: %q", out)
	}
	if !strings.Contains(out, "Old Gen 95%") {
		t.Errorf("want diagnosis, got: %q", out)
	}
}

// 3. 全部失败 → FailurePrefix + first error
func TestMockSummarizer_AllFailure(t *testing.T) {
	m := NewMockSummarizer()
	r := Report{
		CaseID: "c1",
		Actions: []Action{
			{Action: "devops_pipeline_rerun", Result: "failure", ErrorMsg: "403 permission denied"},
			{Action: "devops_pipeline_rerun", Result: "failure", ErrorMsg: "timeout"},
		},
	}
	out, _ := m.Summarize(context.Background(), r)
	if !strings.HasPrefix(out, "本次事件未完全闭环") {
		t.Errorf("want failure prefix, got: %q", out)
	}
	if !strings.Contains(out, "失败 2") {
		t.Errorf("want failure count, got: %q", out)
	}
	if !strings.Contains(out, "403 permission denied") {
		t.Errorf("want first error, got: %q", out)
	}
}

// 4. 混合（部分成功）→ PartialPrefix
func TestMockSummarizer_Partial(t *testing.T) {
	m := NewMockSummarizer()
	r := Report{
		CaseID: "c1",
		Actions: []Action{
			{Action: "gongfeng_mr_create", Result: "success"},
			{Action: "devops_pipeline_rerun", Result: "failure", ErrorMsg: "token expired"},
		},
	}
	out, _ := m.Summarize(context.Background(), r)
	if !strings.HasPrefix(out, "本次事件已部分处置") {
		t.Errorf("want partial prefix, got: %q", out)
	}
	if !strings.Contains(out, "成功 1") || !strings.Contains(out, "失败 1") {
		t.Errorf("want mixed counts, got: %q", out)
	}
}

// 5. References 拼接
func TestMockSummarizer_References(t *testing.T) {
	m := NewMockSummarizer()
	r := Report{
		CaseID: "c1",
		Actions: []Action{
			{Action: "gongfeng_mr_create", Result: "success"},
		},
		References: []Reference{
			{Kind: "mr", Title: "MR-42", URL: "https://git"},
			{Kind: "tapd", Title: "BUG-7", URL: "https://tapd"},
			{Kind: "ignore-empty", Title: "", URL: ""}, // 空都要跳过
		},
	}
	out, _ := m.Summarize(context.Background(), r)
	if !strings.Contains(out, "mr:MR-42") || !strings.Contains(out, "tapd:BUG-7") {
		t.Errorf("refs missing, got: %q", out)
	}
	if strings.Contains(out, "ignore-empty:") {
		t.Errorf("empty ref should be skipped, got: %q", out)
	}
}

// 6. SummarizeOrFallback：nil client 走 fallback
func TestSummarizeOrFallback_NilClient(t *testing.T) {
	got := SummarizeOrFallback(context.Background(), nil, Report{}, "FB", nil)
	if got != "FB" {
		t.Errorf("want fallback, got: %q", got)
	}
}

// 7. SummarizeOrFallback：client error → fallback + logger 被调用
type errorSummarizer struct{}

func (errorSummarizer) Summarize(_ context.Context, _ Report) (string, error) {
	return "", errors.New("downstream oops")
}

func TestSummarizeOrFallback_ClientError(t *testing.T) {
	var calls int
	logger := func(string, ...any) { calls++ }
	got := SummarizeOrFallback(context.Background(), errorSummarizer{}, Report{}, "FB", logger)
	if got != "FB" {
		t.Errorf("want fallback on error, got: %q", got)
	}
	if calls != 1 {
		t.Errorf("logger should be called once, got %d", calls)
	}
}

// 8. SummarizeOrFallback：client 返回空串 → fallback
type emptySummarizer struct{}

func (emptySummarizer) Summarize(_ context.Context, _ Report) (string, error) {
	return "   ", nil
}

func TestSummarizeOrFallback_EmptyReturn(t *testing.T) {
	got := SummarizeOrFallback(context.Background(), emptySummarizer{}, Report{}, "FB", nil)
	if got != "FB" {
		t.Errorf("want fallback on blank, got: %q", got)
	}
}

// 9. nil MockSummarizer Summarize 报错（安全边界）
func TestMockSummarizer_NilReceiver(t *testing.T) {
	var m *MockSummarizer
	_, err := m.Summarize(context.Background(), Report{})
	if err == nil {
		t.Fatalf("nil receiver should error")
	}
}

// 10. trimTo 边界
func TestTrimTo(t *testing.T) {
	if got := trimTo("abcdef", 3); got != "abc…" {
		t.Errorf("trim 3: got %q", got)
	}
	if got := trimTo("ab", 5); got != "ab" {
		t.Errorf("trim 5: got %q", got)
	}
	if got := trimTo("abc", 0); got != "abc" {
		t.Errorf("trim 0: got %q", got)
	}
	// 中文 rune
	if got := trimTo("中国人民", 2); got != "中国…" {
		t.Errorf("trim cn: got %q", got)
	}
}
