//go:build eval

// evalrun_judge_test.go D17.5 — 纯函数级单测。
//
// 不起真实 LLM：
//   - buildJudge 的 PromptStore 分支用本地临时 YAML 文件；
//   - runJudge 走 MockJudge 而非 LLMJudge（验证装配流程本身）；
//   - collectJudgeInputs / printJudgeSummary 用手写 fake result 断言输出路径。
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.woa.com/trpc-go/gameops-agent/eval"

	"trpc.group/trpc-go/trpc-agent-go/evaluation"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalresult"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalset"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// newFakeResult 构造一个最小可打分的 *evaluation.EvaluationResult。
//
// 含两个 case：
//   - c1：ActualInvocation 和 ExpectedInvocation 俱全；
//   - c2：ActualInvocation.FinalResponse 缺失 → collectJudgeInputs 应跳过。
func newFakeResult() *evaluation.EvaluationResult {
	msg := func(s string) *model.Message {
		m := model.NewAssistantMessage(s)
		return &m
	}
	um := func(s string) *model.Message {
		m := model.NewUserMessage(s)
		return &m
	}
	return &evaluation.EvaluationResult{
		AppName:   "gameops-agent",
		EvalSetID: "gameops-core",
		EvalCases: []*evaluation.EvaluationCaseResult{
			{EvalCaseID: "c1"},
			{EvalCaseID: "c2"},
		},
		EvalResult: &evalresult.EvalSetResult{
			EvalCaseResults: []*evalresult.EvalCaseResult{
				{
					EvalID: "c1",
					EvalMetricResultPerInvocation: []*evalresult.EvalMetricResultPerInvocation{
						{
							ActualInvocation: &evalset.Invocation{
								UserContent:   um("OOM 了什么情况"),
								FinalResponse: msg("pay-service OOMKilled，建议走 HITL 扩 limit"),
							},
							ExpectedInvocation: &evalset.Invocation{
								UserContent:   um("OOM 了什么情况"),
								FinalResponse: msg("pay-service 内存超限，HITL 确认后扩到 2Gi"),
							},
						},
					},
				},
				{
					EvalID: "c2",
					EvalMetricResultPerInvocation: []*evalresult.EvalMetricResultPerInvocation{
						{
							ActualInvocation: &evalset.Invocation{
								UserContent: um("查一下 bug"),
								// FinalResponse 故意不填
							},
							ExpectedInvocation: &evalset.Invocation{
								FinalResponse: msg("有 3 条 bug"),
							},
						},
					},
				},
			},
		},
	}
}

// TestCollectJudgeInputs_SkipsMissingActualFinal c2 应被跳过，c1 应完整被抽出。
func TestCollectJudgeInputs_SkipsMissingActualFinal(t *testing.T) {
	inputs := collectJudgeInputs(newFakeResult())
	if len(inputs) != 1 {
		t.Fatalf("want 1 input, got %d", len(inputs))
	}
	got := inputs[0]
	if got.CaseID != "c1" {
		t.Errorf("case id=%q, want c1", got.CaseID)
	}
	if !strings.Contains(got.UserQuery, "OOM") {
		t.Errorf("UserQuery missing OOM: %q", got.UserQuery)
	}
	if !strings.Contains(got.FinalAnswer, "HITL") {
		t.Errorf("FinalAnswer missing HITL: %q", got.FinalAnswer)
	}
	if !strings.Contains(got.ExpectedAnswer, "扩到 2Gi") {
		t.Errorf("ExpectedAnswer missing '扩到 2Gi': %q", got.ExpectedAnswer)
	}
}

// TestCollectJudgeInputs_NilSafe 入参为 nil 时返回 nil，不 panic。
func TestCollectJudgeInputs_NilSafe(t *testing.T) {
	if got := collectJudgeInputs(nil); got != nil {
		t.Errorf("want nil, got %v", got)
	}
	empty := &evaluation.EvaluationResult{}
	if got := collectJudgeInputs(empty); got != nil {
		t.Errorf("want nil on EvalResult=nil, got %v", got)
	}
}

// TestCollectJudgeInputs_FallbackToExpectedUserContent
// Actual 没有 UserContent 时应回落到 Expected 的 UserContent。
func TestCollectJudgeInputs_FallbackToExpectedUserContent(t *testing.T) {
	um := model.NewUserMessage("expected user")
	am := model.NewAssistantMessage("some answer")
	res := &evaluation.EvaluationResult{
		EvalCases: []*evaluation.EvaluationCaseResult{{EvalCaseID: "x"}},
		EvalResult: &evalresult.EvalSetResult{
			EvalCaseResults: []*evalresult.EvalCaseResult{{
				EvalID: "x",
				EvalMetricResultPerInvocation: []*evalresult.EvalMetricResultPerInvocation{{
					ActualInvocation:   &evalset.Invocation{FinalResponse: &am},
					ExpectedInvocation: &evalset.Invocation{UserContent: &um},
				}},
			}},
		},
	}
	inputs := collectJudgeInputs(res)
	if len(inputs) != 1 || inputs[0].UserQuery != "expected user" {
		t.Fatalf("fallback failed: %+v", inputs)
	}
}

// TestRunJudge_NilShortCircuit judge=nil 时返回 (nil, nil)。
func TestRunJudge_NilShortCircuit(t *testing.T) {
	sum, err := runJudge(context.Background(), nil, []eval.JudgeInput{{CaseID: "x"}})
	if err != nil || sum != nil {
		t.Errorf("want (nil,nil), got (%v,%v)", sum, err)
	}
	// 非 nil runtime 但 Judge=nil 时同样 short-circuit。
	sum, err = runJudge(context.Background(), &judgeRuntime{}, []eval.JudgeInput{{CaseID: "x"}})
	if err != nil || sum != nil {
		t.Errorf("want (nil,nil), got (%v,%v)", sum, err)
	}
}

// TestRunJudge_UsesMockJudgeEndToEnd runtime 内替换为 MockJudge 走 RunBatch。
func TestRunJudge_UsesMockJudgeEndToEnd(t *testing.T) {
	rt := &judgeRuntime{Judge: eval.NewMockJudge(0.5)}
	inputs := []eval.JudgeInput{
		{CaseID: "c1", UserQuery: "q", FinalAnswer: "OOM happened, HITL confirmed",
			ExpectedAnswer: "OOM happened, HITL confirmed"},
	}
	sum, err := runJudge(context.Background(), rt, inputs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sum == nil || sum.Total != 1 {
		t.Fatalf("sum=%+v", sum)
	}
	if len(sum.Reports) != 1 || sum.Reports[0].CaseID != "c1" {
		t.Errorf("reports=%+v", sum.Reports)
	}
}

// TestPrintJudgeSummary_Shape 输出应包含 total / pass_rate / 维度均分 / 逐 case 明细。
func TestPrintJudgeSummary_Shape(t *testing.T) {
	// 用 MockJudge 真实跑一批，再喂给 printJudgeSummary。
	rt := &judgeRuntime{Judge: eval.NewMockJudge(0.5)}
	inputs := []eval.JudgeInput{
		{CaseID: "case_a", UserQuery: "Q", FinalAnswer: "oom HITL log",
			ExpectedAnswer: "oom HITL log"},
		{CaseID: "case_b", UserQuery: "Q", FinalAnswer: "timeout metric",
			ExpectedAnswer: "timeout metric"},
	}
	sum, _ := runJudge(context.Background(), rt, inputs)

	// 捕获 stdout。
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printJudgeSummary(sum, "model=mock, prompt=<default>")
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	out := buf.String()

	for _, kw := range []string{
		"LLMJudge", "total=2", "case=case_a", "case=case_b",
		"RootCauseAccuracy", "avg=", "model=mock",
	} {
		if !strings.Contains(out, kw) {
			t.Errorf("summary missing %q:\n%s", kw, out)
		}
	}
}

// TestPrintJudgeSummary_EmptyTotal 空 summary 应提示"无可评估用例"而不 panic。
func TestPrintJudgeSummary_EmptyTotal(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printJudgeSummary(&eval.BatchJudgeSummary{DimAvg: map[string]float64{}}, "n=0")
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	if !strings.Contains(buf.String(), "无可评估用例") {
		t.Errorf("missing empty hint:\n%s", buf.String())
	}
}

// TestBuildJudge_DisabledReturnsEmpty Enabled=false 时返回空 runtime，不触发模型构造。
func TestBuildJudge_DisabledReturnsEmpty(t *testing.T) {
	rt, err := buildJudge(judgeOptions{Enabled: false})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rt == nil || rt.Judge != nil || rt.Cleanup != nil {
		t.Errorf("want zero runtime, got %+v", rt)
	}
}

// TestBuildJudge_PromptPathFailFast 指定了不存在的 prompt 路径时应报错。
// 验证"显式配置必须生效、不要静默回退"的 fail-fast 原则。
func TestBuildJudge_PromptPathFailFast(t *testing.T) {
	rt, err := buildJudge(judgeOptions{
		Enabled:    true,
		PromptPath: "/nope/does/not/exist.yaml",
	})
	if err == nil {
		if rt != nil && rt.Cleanup != nil {
			rt.Cleanup()
		}
		t.Fatal("want error on missing prompt path, got nil")
	}
	if !strings.Contains(err.Error(), "prompt load failed") {
		t.Errorf("err msg should mention prompt load failed, got: %v", err)
	}
}

// TestBuildJudge_ValidPromptPath 指向合法 YAML 时 PromptStore 成功加载。
func TestBuildJudge_ValidPromptPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	yaml := `version: v1
system_prompt: "严格评审"
dimensions:
  - name: Quality
    threshold: 0.8
    criterion: 答案质量
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write tmp yaml: %v", err)
	}
	rt, err := buildJudge(judgeOptions{
		Enabled:    true,
		PromptPath: path,
		ModelName:  "mock-model",
	})
	if err != nil {
		t.Fatalf("build judge: %v", err)
	}
	defer rt.Cleanup()
	if rt.Judge == nil {
		t.Fatal("judge should not be nil")
	}
	if !strings.Contains(rt.Note, "mock-model") || !strings.Contains(rt.Note, path) {
		t.Errorf("note should include model & prompt path, got %q", rt.Note)
	}
}

// -------------------- D30 --------------------

// TestCollectJudgeInputs_ExtractsToolTraces D30：确认 collectJudgeInputs 会把
// ActualInvocation.Tools / ExpectedInvocation.Tools 的工具名按顺序填到
// ActualToolCalls / ExpectedToolCalls 字段。
func TestCollectJudgeInputs_ExtractsToolTraces(t *testing.T) {
	am := model.NewAssistantMessage("some answer")
	um := model.NewUserMessage("some q")
	res := &evaluation.EvaluationResult{
		EvalCases: []*evaluation.EvaluationCaseResult{{EvalCaseID: "case_tool"}},
		EvalResult: &evalresult.EvalSetResult{
			EvalCaseResults: []*evalresult.EvalCaseResult{{
				EvalID: "case_tool",
				EvalMetricResultPerInvocation: []*evalresult.EvalMetricResultPerInvocation{{
					ActualInvocation: &evalset.Invocation{
						UserContent:   &um,
						FinalResponse: &am,
						Tools: []*evalset.Tool{
							{Name: "bcs_node_describe"},
							{Name: "bcs_network_update"},
						},
					},
					ExpectedInvocation: &evalset.Invocation{
						Tools: []*evalset.Tool{
							{Name: "bcs_node_describe"},
							{Name: "bcs_network_update"},
							{Name: "bcs_network_update"},
						},
					},
				}},
			}},
		},
	}
	inputs := collectJudgeInputs(res)
	if len(inputs) != 1 {
		t.Fatalf("want 1 input, got %d", len(inputs))
	}
	got := inputs[0]
	wantActual := []string{"bcs_node_describe", "bcs_network_update"}
	if len(got.ActualToolCalls) != len(wantActual) {
		t.Fatalf("ActualToolCalls len=%d, want %d (%v)",
			len(got.ActualToolCalls), len(wantActual), got.ActualToolCalls)
	}
	for i, n := range wantActual {
		if got.ActualToolCalls[i] != n {
			t.Errorf("ActualToolCalls[%d]=%q want %q", i, got.ActualToolCalls[i], n)
		}
	}
	wantExpected := []string{"bcs_node_describe", "bcs_network_update", "bcs_network_update"}
	if len(got.ExpectedToolCalls) != len(wantExpected) {
		t.Fatalf("ExpectedToolCalls len=%d, want %d (%v)",
			len(got.ExpectedToolCalls), len(wantExpected), got.ExpectedToolCalls)
	}
	for i, n := range wantExpected {
		if got.ExpectedToolCalls[i] != n {
			t.Errorf("ExpectedToolCalls[%d]=%q want %q", i, got.ExpectedToolCalls[i], n)
		}
	}
}

// TestCollectJudgeInputs_SkipsEmptyToolNames D30：空工具名应被跳过，nil Tool 指针应被跳过。
func TestCollectJudgeInputs_SkipsEmptyToolNames(t *testing.T) {
	am := model.NewAssistantMessage("answer")
	res := &evaluation.EvaluationResult{
		EvalCases: []*evaluation.EvaluationCaseResult{{EvalCaseID: "c"}},
		EvalResult: &evalresult.EvalSetResult{
			EvalCaseResults: []*evalresult.EvalCaseResult{{
				EvalID: "c",
				EvalMetricResultPerInvocation: []*evalresult.EvalMetricResultPerInvocation{{
					ActualInvocation: &evalset.Invocation{
						FinalResponse: &am,
						Tools: []*evalset.Tool{
							{Name: "real_tool"},
							{Name: ""},  // 空名：应跳过
							nil,         // nil 指针：应跳过
							{Name: "  "}, // 纯空白：应跳过
						},
					},
				}},
			}},
		},
	}
	inputs := collectJudgeInputs(res)
	if len(inputs) != 1 {
		t.Fatalf("want 1 input, got %d", len(inputs))
	}
	if len(inputs[0].ActualToolCalls) != 1 || inputs[0].ActualToolCalls[0] != "real_tool" {
		t.Errorf("空名/nil 应被过滤，只剩 real_tool，实际 %v", inputs[0].ActualToolCalls)
	}
}

// TestRunToolSelectionJudge_DisabledReturnsNil D30：未启用时 short-circuit。
func TestRunToolSelectionJudge_DisabledReturnsNil(t *testing.T) {
	sum, err := runToolSelectionJudge(context.Background(), false,
		[]eval.JudgeInput{{CaseID: "x"}})
	if err != nil || sum != nil {
		t.Errorf("disabled 应返回 (nil, nil)，实际 sum=%+v err=%v", sum, err)
	}
}

// TestRunToolSelectionJudge_EmptyInputsEmptySummary D30：启用但 inputs 为空
// 时返回空 summary（Total=0），不报错。
func TestRunToolSelectionJudge_EmptyInputsEmptySummary(t *testing.T) {
	sum, err := runToolSelectionJudge(context.Background(), true, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sum == nil || sum.Total != 0 {
		t.Errorf("空 inputs 应返回 Total=0 summary，实际 %+v", sum)
	}
}

// TestRunToolSelectionJudge_HappyPath D30：3 case 走一遍批次。
func TestRunToolSelectionJudge_HappyPath(t *testing.T) {
	inputs := []eval.JudgeInput{
		{
			CaseID:            "perfect",
			ExpectedToolCalls: []string{"a", "b"},
			ActualToolCalls:   []string{"a", "b"},
		},
		{
			CaseID:            "partial",
			ExpectedToolCalls: []string{"a", "b", "c"},
			ActualToolCalls:   []string{"a"}, // 只中 1/3
		},
		{
			CaseID:            "empty_actual",
			ExpectedToolCalls: []string{"a"},
			ActualToolCalls:   nil,
		},
	}
	sum, err := runToolSelectionJudge(context.Background(), true, inputs)
	if err != nil {
		t.Fatalf("batch err: %v", err)
	}
	if sum.Total != 3 {
		t.Errorf("Total 应为 3，实际 %d", sum.Total)
	}
	if sum.Passed != 1 {
		t.Errorf("仅 perfect 一条应 Pass，实际 %d", sum.Passed)
	}
	avg, ok := sum.DimAvg[eval.DimensionToolSelectionAccuracy]
	if !ok {
		t.Fatal("DimAvg 缺 ToolSelectionAccuracy 键")
	}
	// 均分：(1.0 + 0.333 + 0.0) / 3 ≈ 0.444
	if avg < 0.30 || avg > 0.60 {
		t.Errorf("平均分应在 0.30~0.60 区间（反映 1 通过 + 1 部分 + 1 失败），实际 %f", avg)
	}
}

// TestRunToolSelectionJudge_DoesNotMutateInputDimensions D30：不应污染调用方
// 传入的 inputs.Dimensions 字段（避免 LLMJudge 随后再看见 Tool 维度）。
func TestRunToolSelectionJudge_DoesNotMutateInputDimensions(t *testing.T) {
	origDims := []eval.JudgeDimension{
		{Name: "RootCauseAccuracy", Threshold: 0.85},
	}
	inputs := []eval.JudgeInput{
		{
			CaseID:            "c",
			ExpectedToolCalls: []string{"x"},
			ActualToolCalls:   []string{"x"},
			Dimensions:        origDims,
		},
	}
	_, err := runToolSelectionJudge(context.Background(), true, inputs)
	if err != nil {
		t.Fatalf("batch err: %v", err)
	}
	// 原数组长度应保持 1；不应被 runToolSelectionJudge 额外追加 Tool 维度。
	if len(inputs[0].Dimensions) != 1 {
		t.Errorf("Dimensions 被污染：%+v", inputs[0].Dimensions)
	}
	if inputs[0].Dimensions[0].Name != "RootCauseAccuracy" {
		t.Errorf("Dimensions 内容被污染：%+v", inputs[0].Dimensions)
	}
}
