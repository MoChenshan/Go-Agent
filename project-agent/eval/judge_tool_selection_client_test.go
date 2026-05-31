// judge_tool_selection_client_test.go —— D30 ToolSelectionJudge 单测。
//
// # 覆盖矩阵
//
//   1. 正向：tool trace 完全匹配 → score=1.0, AllPass=true
//   2. 反向：actual 为空但 golden 非空 → score=0.0, AllPass=false
//   3. 顺序反转：score 降到 0.80 临界，维度阈值 0.80 → 命中 Pass 边界
//   4. 只选了 DimensionToolSelectionAccuracy 之外的维度 → 空报告 + AllPass=true
//   5. CaseID 为空 → 返回 error
//   6. Dimensions 为空 → 默认评 ToolSelectionAccuracy 一次
//   7. JudgeClient 接口契约兼容（能作为 JudgeClient 使用，进入 RunBatch）
package eval

import (
	"context"
	"strings"
	"testing"
)

// 1) 完美匹配
func TestToolSelectionJudge_PerfectMatch(t *testing.T) {
	j := NewToolSelectionJudge()
	in := JudgeInput{
		CaseID:            "case_ok",
		ExpectedToolCalls: []string{"bcs_node_describe", "bcs_network_update"},
		ActualToolCalls:   []string{"bcs_node_describe", "bcs_network_update"},
	}
	rep, err := j.Score(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !rep.AllPass {
		t.Errorf("完全匹配应 AllPass=true，实际 %+v", rep)
	}
	if rep.AvgScore != 1.0 {
		t.Errorf("完全匹配 AvgScore 应为 1.0，实际 %f", rep.AvgScore)
	}
	if len(rep.Scores) != 1 {
		t.Fatalf("应只有 1 维，实际 %d", len(rep.Scores))
	}
	if rep.Scores[0].Dimension != DimensionToolSelectionAccuracy {
		t.Errorf("维度名错，实际 %q", rep.Scores[0].Dimension)
	}
}

// 2) actual 为空
func TestToolSelectionJudge_EmptyActual(t *testing.T) {
	j := NewToolSelectionJudge()
	in := JudgeInput{
		CaseID:            "case_llm_gave_up",
		ExpectedToolCalls: []string{"bcs_node_describe"},
		ActualToolCalls:   nil,
	}
	rep, err := j.Score(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rep.AllPass {
		t.Error("actual 为空应 AllPass=false")
	}
	if rep.AvgScore != 0.0 {
		t.Errorf("actual 为空 AvgScore 应为 0，实际 %f", rep.AvgScore)
	}
}

// 3) 顺序反转到达阈值边界
func TestToolSelectionJudge_OrderReversedBoundary(t *testing.T) {
	j := NewToolSelectionJudge()
	// 算法：set=1.0, lcs=0.5 → 总分 0.60+0.20 = 0.80
	// 阈值 0.80 → Pass (0.80 >= 0.80)
	in := JudgeInput{
		CaseID:            "case_reversed",
		ExpectedToolCalls: []string{"a", "b"},
		ActualToolCalls:   []string{"b", "a"},
	}
	rep, _ := j.Score(context.Background(), in)
	if rep.AvgScore != 0.80 {
		t.Errorf("顺序反转应为 0.80，实际 %f", rep.AvgScore)
	}
	if !rep.AllPass {
		t.Error("0.80 正好踩阈值边界应 Pass")
	}
}

// 4) 只传其他维度 → 空报告但 AllPass=true（安静退化）
func TestToolSelectionJudge_FiltersOutOtherDimensions(t *testing.T) {
	j := NewToolSelectionJudge()
	in := JudgeInput{
		CaseID:            "case_only_rca",
		ExpectedToolCalls: []string{"whatever"},
		ActualToolCalls:   []string{"whatever"},
		Dimensions: []JudgeDimension{
			{Name: "RootCauseAccuracy", Threshold: 0.85},
		},
	}
	rep, err := j.Score(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !rep.AllPass {
		t.Error("空报告应 AllPass=true（无维度就没失败）")
	}
	if len(rep.Scores) != 0 {
		t.Errorf("应为空 Scores 切片，实际 %+v", rep.Scores)
	}
}

// 5) CaseID 为空
func TestToolSelectionJudge_CaseIDRequired(t *testing.T) {
	j := NewToolSelectionJudge()
	_, err := j.Score(context.Background(), JudgeInput{})
	if err == nil {
		t.Error("CaseID 为空应报错")
	}
	if !strings.Contains(err.Error(), "CaseID") {
		t.Errorf("错误消息应提及 CaseID，实际 %v", err)
	}
}

// 6) Dimensions 为空 → 默认评 Tool 维度一次
func TestToolSelectionJudge_DefaultDimensions(t *testing.T) {
	j := NewToolSelectionJudge()
	in := JudgeInput{
		CaseID:            "case_default",
		ExpectedToolCalls: []string{"x"},
		ActualToolCalls:   []string{"x"},
		// Dimensions 留空
	}
	rep, err := j.Score(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(rep.Scores) != 1 {
		t.Fatalf("默认应评 1 维，实际 %d", len(rep.Scores))
	}
	if rep.Scores[0].Dimension != DimensionToolSelectionAccuracy {
		t.Errorf("默认维度名错，实际 %q", rep.Scores[0].Dimension)
	}
}

// 7) 作为 JudgeClient 进入 RunBatch
func TestToolSelectionJudge_RunBatchCompatible(t *testing.T) {
	var client JudgeClient = NewToolSelectionJudge()
	inputs := []JudgeInput{
		{
			CaseID:            "c1",
			ExpectedToolCalls: []string{"A", "B"},
			ActualToolCalls:   []string{"A", "B"},
		},
		{
			CaseID:            "c2",
			ExpectedToolCalls: []string{"A", "B"},
			ActualToolCalls:   []string{"B"},
		},
	}
	sum, err := RunBatch(context.Background(), client, inputs)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if sum.Total != 2 {
		t.Errorf("total 应为 2，实际 %d", sum.Total)
	}
	if sum.Passed != 1 {
		t.Errorf("应只有 c1 通过（c2 缺一个工具），实际 Passed=%d", sum.Passed)
	}
	if _, ok := sum.DimAvg[DimensionToolSelectionAccuracy]; !ok {
		t.Error("DimAvg 应含 ToolSelectionAccuracy 键")
	}
}

// 8) MockJudge 也能正确评 Tool 维度（D30 同时加了 MockJudge 分支）
func TestMockJudge_ToolSelectionDimension(t *testing.T) {
	m := NewMockJudge(0.5)
	in := JudgeInput{
		CaseID:            "case_mock_tool",
		FinalAnswer:       "无所谓的答案",
		ExpectedAnswer:    "无所谓的参考",
		ExpectedToolCalls: []string{"tool_a", "tool_b"},
		ActualToolCalls:   []string{"tool_a", "tool_b"},
		Dimensions: []JudgeDimension{
			ToolSelectionAccuracyDimension(),
		},
	}
	rep, err := m.Score(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(rep.Scores) != 1 {
		t.Fatalf("应评 1 维，实际 %d", len(rep.Scores))
	}
	s := rep.Scores[0]
	if s.Dimension != DimensionToolSelectionAccuracy {
		t.Errorf("维度名错，实际 %q", s.Dimension)
	}
	if s.Score != 1.0 {
		t.Errorf("MockJudge 对完美匹配应返回 1.0（不应叠加 Floor），实际 %f", s.Score)
	}
	if !strings.Contains(s.Reason, "tool_trace") {
		t.Errorf("Reason 应含 tool_trace 标识，实际 %q", s.Reason)
	}
}

// 9) MockJudge 对 Tool 维度"actual 为空"的情况不应被 Floor 抬分
//    —— 这是 D30 有意设计的反 Floor 隔离。
func TestMockJudge_ToolSelectionNoFloorLeak(t *testing.T) {
	m := NewMockJudge(0.5) // 故意给高 Floor
	in := JudgeInput{
		CaseID:            "case_no_actual",
		ExpectedToolCalls: []string{"critical_tool"},
		ActualToolCalls:   nil,
		Dimensions: []JudgeDimension{
			ToolSelectionAccuracyDimension(),
		},
	}
	rep, _ := m.Score(context.Background(), in)
	s := rep.Scores[0]
	if s.Score != 0.0 {
		t.Errorf("actual 为空时不应被 Floor 抬到 0.5，应严格 0，实际 %f", s.Score)
	}
	if s.Pass {
		t.Error("0 不应 Pass（阈值 0.80）")
	}
}
