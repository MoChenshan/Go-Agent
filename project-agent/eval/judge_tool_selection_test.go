// judge_tool_selection_test.go —— D29 ToolSelectionAccuracy 维度单测。
//
// # 覆盖矩阵
//
//   A) ScoreToolSelection 算法
//     1. golden 与 actual 完全一致 → 1.0
//     2. actual 为空 + golden 非空 → 0.0
//     3. golden 为空 → 1.0（该 case 不关心工具选择）
//     4. 大小写 / 空格差异 → 归一化后仍算命中
//     5. 工具选对但顺序反了 → set=1.0, order<1.0 → 总分在 (0.6, 1.0) 之间
//     6. 工具集合部分命中 → 按比例扣分
//     7. actual 多调了无害工具 → set=1.0, order=1.0 → 1.0（容忍交叉验证）
//
//   B) ExtractGoldenToolNames
//     8. 多轮对话里的工具按顺序拼接
//     9. 空工具名被跳过
//
//   C) Scenario F 集成校验
//    10. evalset.json 里的 case_node_to_network_handoff 能正确抽出 4 个期望工具
//    11. 3 个 BCS 写工具按 prompt 决策树顺序（describe → get → set_backend）
//
//   D) 维度注册
//    12. DefaultJudgeDimensionsV2 比 V1 多一个 ToolSelectionAccuracy
//    13. 维度 Threshold 合法（0 < t ≤ 1）
package eval

import (
	"path/filepath"
	"testing"
)

// ---- A) ScoreToolSelection 算法 -------------------------------------------------

func TestScoreToolSelection_ExactMatch(t *testing.T) {
	golden := []string{"bcs_node_describe", "bcs_network_update"}
	actual := []string{"bcs_node_describe", "bcs_network_update"}
	got := ScoreToolSelection(golden, actual)
	if got != 1.0 {
		t.Errorf("完全一致应为 1.0，实际 %f", got)
	}
}

func TestScoreToolSelection_EmptyActual(t *testing.T) {
	golden := []string{"bcs_node_describe"}
	actual := []string{}
	got := ScoreToolSelection(golden, actual)
	if got != 0.0 {
		t.Errorf("actual 为空应为 0，实际 %f", got)
	}
}

func TestScoreToolSelection_EmptyGolden(t *testing.T) {
	got := ScoreToolSelection(nil, []string{"anything"})
	if got != 1.0 {
		t.Errorf("golden 为空应为 1.0（不关心工具），实际 %f", got)
	}
}

func TestScoreToolSelection_CaseInsensitive(t *testing.T) {
	golden := []string{"bcs_node_describe"}
	actual := []string{"BCS_NODE_DESCRIBE"}
	got := ScoreToolSelection(golden, actual)
	if got != 1.0 {
		t.Errorf("大小写差异应归一化后命中，实际 %f", got)
	}
}

func TestScoreToolSelection_OrderReversed(t *testing.T) {
	// golden: [node_describe, network_update]
	// actual: [network_update, node_describe]
	// set 命中率 = 2/2 = 1.0
	// LCS = 1 (只能选一个保持相对顺序)，order = 1/2 = 0.5
	// 总分 = 0.6*1.0 + 0.4*0.5 = 0.80
	golden := []string{"bcs_node_describe", "bcs_network_update"}
	actual := []string{"bcs_network_update", "bcs_node_describe"}
	got := ScoreToolSelection(golden, actual)
	if got < 0.75 || got > 0.85 {
		t.Errorf("顺序反了应在 0.8 附近，实际 %f", got)
	}
	if got == 1.0 {
		t.Error("顺序反了不应为 1.0")
	}
}

func TestScoreToolSelection_PartialSetMatch(t *testing.T) {
	// golden: [A, B, C]
	// actual: [A]
	// set 命中率 = 1/3
	// LCS = 1, order = 1/3
	// 总分 = 0.6*0.333 + 0.4*0.333 ≈ 0.33
	golden := []string{"tool_a", "tool_b", "tool_c"}
	actual := []string{"tool_a"}
	got := ScoreToolSelection(golden, actual)
	if got < 0.25 || got > 0.40 {
		t.Errorf("只命中 1/3 应在 0.33 附近，实际 %f", got)
	}
}

func TestScoreToolSelection_ExtraCrossCheck(t *testing.T) {
	// golden: [node_describe, network_update]
	// actual: [node_describe, pod_describe, network_update]  ← 多了交叉验证
	// set 命中率 = 2/2 = 1.0
	// LCS = 2, order = 2/2 = 1.0
	// 总分 = 1.0（完全容忍无害的多调用）
	golden := []string{"bcs_node_describe", "bcs_network_update"}
	actual := []string{"bcs_node_describe", "bcs_pod_describe", "bcs_network_update"}
	got := ScoreToolSelection(golden, actual)
	if got != 1.0 {
		t.Errorf("多调无害工具但核心顺序对，应为 1.0，实际 %f", got)
	}
}

// ---- B) ExtractGoldenToolNames --------------------------------------------------

func TestExtractGoldenToolNames_MultiInvocation(t *testing.T) {
	c := EvalCase{
		EvalID: "test_case",
		Conversation: []Invocation{
			{Tools: []ToolUse{{Name: "tool_a"}, {Name: "tool_b"}}},
			{Tools: []ToolUse{{Name: "tool_c"}}},
		},
	}
	got := ExtractGoldenToolNames(c)
	want := []string{"tool_a", "tool_b", "tool_c"}
	if len(got) != len(want) {
		t.Fatalf("期望 3 个工具，实际 %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("位置 %d：期望 %q，实际 %q", i, want[i], got[i])
		}
	}
}

func TestExtractGoldenToolNames_EmptyNameSkipped(t *testing.T) {
	c := EvalCase{
		Conversation: []Invocation{
			{Tools: []ToolUse{{Name: "tool_a"}, {Name: ""}, {Name: "tool_b"}}},
		},
	}
	got := ExtractGoldenToolNames(c)
	if len(got) != 2 {
		t.Errorf("空工具名应跳过，期望 2 个，实际 %d（%v）", len(got), got)
	}
}

// ---- C) Scenario F 集成校验 ----------------------------------------------------

func TestScenarioF_CaseExists(t *testing.T) {
	set, err := LoadEvalSet(filepath.Join(testDataDir, DefaultEvalSetID, DefaultEvalSetID+".evalset.json"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	var found *EvalCase
	for i := range set.EvalCases {
		if set.EvalCases[i].EvalID == "case_node_to_network_handoff" {
			found = &set.EvalCases[i]
			break
		}
	}
	if found == nil {
		t.Fatal("case_node_to_network_handoff 缺失（D29 剧本未接入）")
	}
}

func TestScenarioF_ToolSequenceMatchesDecisionTree(t *testing.T) {
	set, err := LoadEvalSet(filepath.Join(testDataDir, DefaultEvalSetID, DefaultEvalSetID+".evalset.json"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	var found *EvalCase
	for i := range set.EvalCases {
		if set.EvalCases[i].EvalID == "case_node_to_network_handoff" {
			found = &set.EvalCases[i]
			break
		}
	}
	if found == nil {
		t.Skip("case 缺失，已由 TestScenarioF_CaseExists 报告")
	}
	names := ExtractGoldenToolNames(*found)
	// D26 prompt 决策树期望：节点诊断 → 读网络现状 → 改网络配置
	want := []string{"bcs_node_describe", "bcs_network_update", "bcs_network_update"}
	if len(names) != len(want) {
		t.Fatalf("Scenario F 应含 3 个工具调用（decide→get→set），实际 %d（%v）", len(names), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("第 %d 步应为 %q，实际 %q", i+1, want[i], names[i])
		}
	}
}

func TestScenarioF_CriticalNoReasonCaseExists(t *testing.T) {
	set, err := LoadEvalSet(filepath.Join(testDataDir, DefaultEvalSetID, DefaultEvalSetID+".evalset.json"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	var found *EvalCase
	for i := range set.EvalCases {
		if set.EvalCases[i].EvalID == "case_scale_critical_noreason_blocked" {
			found = &set.EvalCases[i]
			break
		}
	}
	if found == nil {
		t.Fatal("case_scale_critical_noreason_blocked 缺失")
	}
	// Critical 拦截剧本的期望行为：LLM 应只调用只读查询（resource_query），
	// 不应直接走 scale_deployment（因为用户没给 reason，prompt 应教会 LLM 主动拦截）
	names := ExtractGoldenToolNames(*found)
	for _, n := range names {
		if n == "bcs_scale_deployment" {
			t.Errorf("Critical 无 reason 剧本不应包含 bcs_scale_deployment（应被 LLM 侧拦截），实际工具链 %v", names)
		}
	}
}

// ---- D) 维度注册 -----------------------------------------------------------------

func TestDefaultJudgeDimensionsV2_AddsToolSelection(t *testing.T) {
	v1 := DefaultJudgeDimensions()
	v2 := DefaultJudgeDimensionsV2()
	if len(v2) != len(v1)+1 {
		t.Fatalf("V2 应比 V1 多 1 个维度，V1=%d V2=%d", len(v1), len(v2))
	}
	last := v2[len(v2)-1]
	if last.Name != DimensionToolSelectionAccuracy {
		t.Errorf("V2 最后一维应为 %q，实际 %q", DimensionToolSelectionAccuracy, last.Name)
	}
}

func TestToolSelectionAccuracyDimension_ThresholdValid(t *testing.T) {
	d := ToolSelectionAccuracyDimension()
	if d.Threshold <= 0 || d.Threshold > 1 {
		t.Errorf("Threshold 应在 (0, 1]，实际 %f", d.Threshold)
	}
	if d.Name != DimensionToolSelectionAccuracy {
		t.Errorf("Name 不匹配，实际 %q", d.Name)
	}
	if d.Criterion == "" {
		t.Error("Criterion 不应为空")
	}
}

// TestScoreToolSelection_ScenarioFGolden 端到端：用真实 Scenario F 的 golden 轨迹
// 模拟"LLM 选对工具并按正确顺序调用"，应得 1.0。
func TestScoreToolSelection_ScenarioFGolden(t *testing.T) {
	set, err := LoadEvalSet(filepath.Join(testDataDir, DefaultEvalSetID, DefaultEvalSetID+".evalset.json"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	var found *EvalCase
	for i := range set.EvalCases {
		if set.EvalCases[i].EvalID == "case_node_to_network_handoff" {
			found = &set.EvalCases[i]
			break
		}
	}
	if found == nil {
		t.Skip("case 缺失")
	}
	golden := ExtractGoldenToolNames(*found)

	// 模拟 LLM 按 prompt 决策树完美执行
	actualGood := []string{"bcs_node_describe", "bcs_network_update", "bcs_network_update"}
	if s := ScoreToolSelection(golden, actualGood); s != 1.0 {
		t.Errorf("完美执行应为 1.0，实际 %f", s)
	}

	// 模拟 LLM 违反决策树：不看节点直接改网络
	actualBad := []string{"bcs_network_update", "bcs_network_update"}
	if s := ScoreToolSelection(golden, actualBad); s >= 0.80 {
		t.Errorf("跳过 node_describe 应低于阈值 0.80，实际 %f", s)
	}

	// 模拟 LLM 选错工具：用 pod_restart 代替 network_update（错误修复方向）
	actualWrong := []string{"bcs_node_describe", "bcs_pod_restart"}
	if s := ScoreToolSelection(golden, actualWrong); s >= 0.80 {
		t.Errorf("用 pod_restart 代替 network_update 应低于阈值 0.80，实际 %f", s)
	}
}
