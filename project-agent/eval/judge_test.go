package eval

import (
	"context"
	"testing"
)

// TestDefaultJudgeDimensions_Shape 默认维度配置健全性。
func TestDefaultJudgeDimensions_Shape(t *testing.T) {
	dims := DefaultJudgeDimensions()
	if len(dims) != 3 {
		t.Fatalf("期望 3 个维度，实际 %d", len(dims))
	}
	seen := map[string]bool{}
	for _, d := range dims {
		if d.Name == "" {
			t.Fatalf("维度名不能为空")
		}
		if d.Threshold < 0 || d.Threshold > 1 {
			t.Fatalf("维度 %s 阈值越界: %v", d.Name, d.Threshold)
		}
		if seen[d.Name] {
			t.Fatalf("维度 %s 重复", d.Name)
		}
		seen[d.Name] = true
	}
	for _, must := range []string{"RootCauseAccuracy",
		"EvidenceSufficiency", "HelpfulnessSafety"} {
		if !seen[must] {
			t.Fatalf("缺失维度 %s", must)
		}
	}
}

// TestMockJudge_ScoresHighWithKeywords 命中多维关键词时应整体通过。
func TestMockJudge_ScoresHighWithKeywords(t *testing.T) {
	judge := NewMockJudge(0.5)
	in := JudgeInput{
		CaseID:    "case-1",
		UserQuery: "game-core pod 不断重启，怎么办？",
		FinalAnswer: "经查日志，pod 因 OOM 被 kill；指标显示 memory 使用率 98%。" +
			"建议回滚到上个版本，写操作需 HITL 人工确认。",
		ExpectedAnswer: "OOM 导致 pod 重启，需回滚",
	}
	rep, err := judge.Score(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !rep.AllPass {
		t.Fatalf("关键词齐全应整体通过，实际 report=%+v", rep)
	}
	if rep.AvgScore <= 0.7 {
		t.Fatalf("平均分应 > 0.7，实际 %.2f", rep.AvgScore)
	}
}

// TestMockJudge_ScoresLowOnEmptyAnswer 空答案应低分不通过。
func TestMockJudge_ScoresLowOnEmptyAnswer(t *testing.T) {
	judge := NewMockJudge(0.1)
	in := JudgeInput{
		CaseID:         "case-2",
		UserQuery:      "怎么修？",
		FinalAnswer:    "",
		ExpectedAnswer: "建议回滚坏版本",
	}
	rep, err := judge.Score(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rep.AllPass {
		t.Fatalf("空答案不应全通过")
	}
	for _, s := range rep.Scores {
		if s.Score > 0.5 {
			t.Fatalf("空答案维度 %s 分数不应 > 0.5，实际 %.2f",
				s.Dimension, s.Score)
		}
	}
}

// TestMockJudge_MissingCaseID 空 CaseID 应返错。
func TestMockJudge_MissingCaseID(t *testing.T) {
	judge := NewMockJudge(0)
	if _, err := judge.Score(context.Background(), JudgeInput{}); err == nil {
		t.Fatalf("空 CaseID 应返回错误")
	}
}

// TestMockJudge_CustomDimension 允许自定义维度（覆盖默认）。
func TestMockJudge_CustomDimension(t *testing.T) {
	judge := NewMockJudge(0.2)
	rep, err := judge.Score(context.Background(), JudgeInput{
		CaseID:      "case-3",
		FinalAnswer: "hello world",
		Dimensions: []JudgeDimension{
			{Name: "CustomX", Threshold: 0.5, Criterion: "随便打"},
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rep.Scores) != 1 || rep.Scores[0].Dimension != "CustomX" {
		t.Fatalf("自定义维度未生效：%+v", rep)
	}
}

// TestRunBatch_Aggregates 批量打分摘要字段应完整。
func TestRunBatch_Aggregates(t *testing.T) {
	judge := NewMockJudge(0.5)
	inputs := []JudgeInput{
		{CaseID: "c2", UserQuery: "q2",
			FinalAnswer:    "OOM 问题，日志看 memory 使用率高，需 HITL 确认回滚",
			ExpectedAnswer: "OOM 根因"},
		{CaseID: "c1", UserQuery: "q1",
			FinalAnswer:    "",
			ExpectedAnswer: "正确答案"},
	}
	sum, err := RunBatch(context.Background(), judge, inputs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sum.Total != 2 {
		t.Fatalf("Total 应为 2")
	}
	if sum.Passed != 1 {
		t.Fatalf("仅 c2 应整体通过，实际 Passed=%d", sum.Passed)
	}
	// 排序断言
	if sum.Reports[0].CaseID != "c1" || sum.Reports[1].CaseID != "c2" {
		t.Fatalf("Reports 应按 CaseID 升序，实际 %s/%s",
			sum.Reports[0].CaseID, sum.Reports[1].CaseID)
	}
	for _, dim := range []string{"RootCauseAccuracy",
		"EvidenceSufficiency", "HelpfulnessSafety"} {
		if _, ok := sum.DimAvg[dim]; !ok {
			t.Fatalf("DimAvg 缺失维度 %s", dim)
		}
	}
}

// TestRunBatch_NilClient 空 client 应返错。
func TestRunBatch_NilClient(t *testing.T) {
	if _, err := RunBatch(context.Background(), nil, nil); err == nil {
		t.Fatalf("空 client 应返错")
	}
}

// TestKeywordOverlap_Basic 关键词重合度计算正确。
func TestKeywordOverlap_Basic(t *testing.T) {
	// expected "OOM 导致 pod 重启"，answer 完整命中
	v := keywordOverlap("OOM 导致 pod 重启，建议回滚", "OOM 导致 pod 重启")
	if v < 0.9 {
		t.Fatalf("完全命中应 ≥ 0.9，实际 %.2f", v)
	}
	// 部分命中
	v = keywordOverlap("cpu 飙高", "OOM 导致 pod 重启")
	if v > 0.5 {
		t.Fatalf("不相关文本应 ≤ 0.5，实际 %.2f", v)
	}
	// expected 为空
	if got := keywordOverlap("any", ""); got != 0 {
		t.Fatalf("expected 为空应返 0，实际 %.2f", got)
	}
}
