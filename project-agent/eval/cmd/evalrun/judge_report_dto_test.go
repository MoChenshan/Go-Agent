//go:build eval

// judge_report_dto_test.go D30.1 单测。
//
// 覆盖矩阵：
//   1. WriteJudgeReportJSON 空路径 → no-op 无错误
//   2. 两 Judge 都 nil → 仍写一份空 judges 的 JSON（让 CI 能区分"没开启"和"文件丢失"）
//   3. 仅 LLM 启用 → JSON 里只含 "llm" 键
//   4. 仅 Tool 启用 → JSON 里只含 "tool" 键
//   5. 两者都启用 → JSON 含 "llm" 和 "tool" 两键
//   6. 父目录不存在会自动创建
//   7. DimAvg 顺序稳定（dim_avg_order 字母序）
//   8. schema_version 字段等于常量
//   9. case 明细 scores 字段保留 Reason
//  10. Total=0 时 PassRate=0（不崩 NaN / 除零）
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"git.woa.com/trpc-go/gameops-agent/eval"
)

// --- 辅助构造器 ---

func newMockBatch(total, passed int, dimAvg map[string]float64,
	reports []*eval.JudgeReport) *eval.BatchJudgeSummary {
	return &eval.BatchJudgeSummary{
		Total: total, Passed: passed,
		DimAvg: dimAvg, Reports: reports,
	}
}

func sampleLLMBatch() *eval.BatchJudgeSummary {
	return newMockBatch(2, 1,
		map[string]float64{"RootCauseAccuracy": 0.85, "EvidenceSufficiency": 0.60},
		[]*eval.JudgeReport{
			{
				CaseID:   "case_a",
				AvgScore: 0.85,
				AllPass:  true,
				Scores: []eval.JudgeScore{
					{Dimension: "RootCauseAccuracy", Score: 0.9, Pass: true, Reason: "命中"},
					{Dimension: "EvidenceSufficiency", Score: 0.8, Pass: true, Reason: "充分"},
				},
			},
			{
				CaseID:   "case_b",
				AvgScore: 0.60,
				AllPass:  false,
				Scores: []eval.JudgeScore{
					{Dimension: "RootCauseAccuracy", Score: 0.8, Pass: true, Reason: ""},
					{Dimension: "EvidenceSufficiency", Score: 0.4, Pass: false, Reason: "证据不足"},
				},
			},
		},
	)
}

func sampleToolBatch() *eval.BatchJudgeSummary {
	return newMockBatch(2, 1,
		map[string]float64{eval.DimensionToolSelectionAccuracy: 0.50},
		[]*eval.JudgeReport{
			{
				CaseID:   "case_a",
				AvgScore: 1.0,
				AllPass:  true,
				Scores: []eval.JudgeScore{
					{Dimension: eval.DimensionToolSelectionAccuracy, Score: 1.0, Pass: true},
				},
			},
			{
				CaseID:  "case_b",
				AvgScore: 0.0,
				AllPass:  false,
				Scores: []eval.JudgeScore{
					{Dimension: eval.DimensionToolSelectionAccuracy, Score: 0.0, Pass: false},
				},
			},
		},
	)
}

func readDTO(t *testing.T, path string) judgeReportDTO {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}
	var dto judgeReportDTO
	if err := json.Unmarshal(b, &dto); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	return dto
}

// 1) 空路径 → no-op
func TestWriteJudgeReportJSON_EmptyPathNoop(t *testing.T) {
	err := WriteJudgeReportJSON("set1",
		sampleLLMBatch(), "note-llm",
		sampleToolBatch(), "note-tool",
		"")
	if err != nil {
		t.Errorf("空路径应 no-op，实际错误：%v", err)
	}
}

// 2) 两 Judge 都 nil → 写空 judges 的 JSON
func TestWriteJudgeReportJSON_BothNilStillWritesEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	if err := WriteJudgeReportJSON("set1", nil, "", nil, "", path); err != nil {
		t.Fatalf("write: %v", err)
	}
	dto := readDTO(t, path)
	if dto.SchemaVersion != JudgeReportSchemaVersion {
		t.Errorf("schema_version 应为 %q，实际 %q", JudgeReportSchemaVersion, dto.SchemaVersion)
	}
	if len(dto.Judges) != 0 {
		t.Errorf("两 Judge 都 nil 时 judges 应为空 map，实际 %+v", dto.Judges)
	}
	if dto.EvalSetID != "set1" {
		t.Errorf("eval_set_id 应为 set1，实际 %q", dto.EvalSetID)
	}
}

// 3) 仅 LLM 启用
func TestWriteJudgeReportJSON_OnlyLLM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := WriteJudgeReportJSON("s", sampleLLMBatch(), "model=x",
		nil, "", path); err != nil {
		t.Fatalf("write: %v", err)
	}
	dto := readDTO(t, path)
	if _, ok := dto.Judges["llm"]; !ok {
		t.Error("应含 llm 键")
	}
	if _, ok := dto.Judges["tool"]; ok {
		t.Error("tool 未启用，不应出现键")
	}
	llm := dto.Judges["llm"]
	if llm.Note != "model=x" {
		t.Errorf("note 错：%q", llm.Note)
	}
	if llm.Total != 2 || llm.Passed != 1 {
		t.Errorf("total/passed 错：%d/%d", llm.Total, llm.Passed)
	}
	if llm.PassRate != 0.5 {
		t.Errorf("pass_rate 错：%f", llm.PassRate)
	}
}

// 4) 仅 Tool 启用
func TestWriteJudgeReportJSON_OnlyTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := WriteJudgeReportJSON("s", nil, "",
		sampleToolBatch(), "algo", path); err != nil {
		t.Fatalf("write: %v", err)
	}
	dto := readDTO(t, path)
	if _, ok := dto.Judges["llm"]; ok {
		t.Error("llm 未启用，不应出现键")
	}
	tool, ok := dto.Judges["tool"]
	if !ok {
		t.Fatal("应含 tool 键")
	}
	if _, hasDim := tool.DimAvg[eval.DimensionToolSelectionAccuracy]; !hasDim {
		t.Error("dim_avg 应含 ToolSelectionAccuracy")
	}
}

// 5) 两者都启用
func TestWriteJudgeReportJSON_Both(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	err := WriteJudgeReportJSON("s",
		sampleLLMBatch(), "llm-note",
		sampleToolBatch(), "tool-note",
		path)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	dto := readDTO(t, path)
	if len(dto.Judges) != 2 {
		t.Errorf("应有 2 个 judge 键，实际 %d", len(dto.Judges))
	}
	if dto.Judges["llm"].Note != "llm-note" {
		t.Errorf("llm note 错：%q", dto.Judges["llm"].Note)
	}
	if dto.Judges["tool"].Note != "tool-note" {
		t.Errorf("tool note 错：%q", dto.Judges["tool"].Note)
	}
}

// 6) 父目录不存在自动创建
func TestWriteJudgeReportJSON_AutoMkdirParent(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "a", "b", "c", "r.json")
	err := WriteJudgeReportJSON("s", sampleLLMBatch(), "n", nil, "", deep)
	if err != nil {
		t.Fatalf("应自动建父目录，实际 %v", err)
	}
	if _, err := os.Stat(deep); err != nil {
		t.Errorf("文件未生成：%v", err)
	}
}

// 7) dim_avg_order 字母序稳定
func TestWriteJudgeReportJSON_DimOrderSorted(t *testing.T) {
	// 故意让 DimAvg 的 map 插入顺序乱：z、a、m
	batch := newMockBatch(1, 1,
		map[string]float64{"z": 0.9, "a": 0.8, "m": 0.7},
		nil,
	)
	path := filepath.Join(t.TempDir(), "r.json")
	if err := WriteJudgeReportJSON("s", batch, "n", nil, "", path); err != nil {
		t.Fatalf("write: %v", err)
	}
	dto := readDTO(t, path)
	order := dto.Judges["llm"].DimAvgOrder
	want := []string{"a", "m", "z"}
	if len(order) != len(want) {
		t.Fatalf("dim_avg_order 长度错：%v", order)
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("dim_avg_order[%d]=%q want %q (全序：%v)", i, order[i], w, order)
		}
	}
}

// 8) schema_version 恒等于常量
func TestWriteJudgeReportJSON_SchemaVersionPinned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	_ = WriteJudgeReportJSON("s", sampleLLMBatch(), "n", nil, "", path)
	dto := readDTO(t, path)
	if dto.SchemaVersion != JudgeReportSchemaVersion {
		t.Errorf("schema_version 漂移：%q（常量 %q）", dto.SchemaVersion, JudgeReportSchemaVersion)
	}
	if dto.SchemaVersion != "v1" {
		t.Errorf("v1 协议预期；实际 %q（若你改了 JudgeReportSchemaVersion 请同步更新 CI 消费方）",
			dto.SchemaVersion)
	}
}

// 9) case 明细保留 reason
func TestWriteJudgeReportJSON_CaseReasonPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	_ = WriteJudgeReportJSON("s", sampleLLMBatch(), "n", nil, "", path)
	dto := readDTO(t, path)
	cases := dto.Judges["llm"].Cases
	// RunBatch 按 CaseID 排序，但这里我们是手工 Reports，不经过 RunBatch，
	// 所以顺序就是插入序 (case_a, case_b)。
	if len(cases) != 2 {
		t.Fatalf("cases 数错：%d", len(cases))
	}
	// case_a 第一维 reason="命中"
	got := cases[0].Scores[0].Reason
	if got != "命中" {
		t.Errorf("case_a 第一维 reason 丢失：%q", got)
	}
	// case_b 第一维 Reason=""（omitempty，反序列化后是空字符串，可接受）
	emptyReason := cases[1].Scores[0].Reason
	if emptyReason != "" {
		t.Errorf("case_b 空 reason 被篡改为 %q", emptyReason)
	}
}

// 10) Total=0 时 PassRate=0
func TestWriteJudgeReportJSON_ZeroTotalNoNaN(t *testing.T) {
	batch := newMockBatch(0, 0, map[string]float64{}, nil)
	path := filepath.Join(t.TempDir(), "r.json")
	if err := WriteJudgeReportJSON("s", batch, "n", nil, "", path); err != nil {
		t.Fatalf("write: %v", err)
	}
	dto := readDTO(t, path)
	if dto.Judges["llm"].PassRate != 0 {
		t.Errorf("Total=0 时 PassRate 应为 0，实际 %f", dto.Judges["llm"].PassRate)
	}
}
