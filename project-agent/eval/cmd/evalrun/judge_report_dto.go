//go:build eval

// judge_report_dto.go D30.1 — Judge 批次结果 JSON 落盘。
//
// # 定位
//
// `printJudgeSummary` 把汇总写到 stdout，人类阅读友好但机器解析脆弱。
// 本文件提供 `WriteJudgeReportJSON`，把 LLMJudge 与 ToolSelectionJudge 的
// 批次结果以**稳定 schema**落盘为 JSON，供：
//
//   - CI 机器人（MR 评论区贴分数表）
//   - 趋势分析（历次 commit 的 DimAvg 折线图）
//   - 离线审计（哪些 case 长期低分）
//
// # 为什么单独建 DTO（而不是让 BatchJudgeSummary 直接 MarshalJSON）
//
//   1. 底层类型 `eval.BatchJudgeSummary` / `JudgeReport` / `JudgeScore` 字段
//      没有 json tag，默认会序列化成 PascalCase（`"Dimension"`、`"Score"`）；
//      给核心类型加 tag 会牵动 LLMJudge / MockJudge / ToolSelectionJudge 等
//      5+ 处测试里对字段名的隐式假设。
//   2. 外部协议需要 `schema_version` / `judge` 分段等 meta，这些是**落盘侧**
//      关心的字段，不是 Judge 领域模型的一部分。
//   3. 未来协议演进（如扩展 Trace ID、timestamp、commit SHA）只需改 DTO，
//      不触碰 Judge 算法核心。
//
// 结论：核心稳、外部协议另立，和 D28 ToolCallEvent 同一风格。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"git.woa.com/trpc-go/gameops-agent/eval"
)

// JudgeReportSchemaVersion 本 DTO 协议版本。
//
// 破坏性字段调整（删字段/改语义）必须 bump；新增字段（保持 omitempty）不需要。
// CI 机器人应当主动校验该字段以决定能否按预期 key 解析。
const JudgeReportSchemaVersion = "v1"

// judgeReportDTO 整份落盘文件的顶层结构。
type judgeReportDTO struct {
	// SchemaVersion 协议版本，见 JudgeReportSchemaVersion。
	SchemaVersion string `json:"schema_version"`
	// GeneratedAt ISO8601 本地时区。用于 CI 追踪产物新鲜度。
	GeneratedAt string `json:"generated_at"`
	// EvalSetID 评测集 ID，冗余方便下游在不同集合间切换时判别。
	EvalSetID string `json:"eval_set_id"`
	// Judges 各 Judge 的批次结果。
	// - "llm"  → LLMJudge（RootCause / Evidence / Helpfulness）
	// - "tool" → ToolSelectionJudge（D30 新增）
	// 未启用的 Judge 不会出现在 map 里（方便下游用 `if _, ok := ...` 判断）。
	Judges map[string]judgeBatchDTO `json:"judges"`
}

// judgeBatchDTO 单个 Judge 的批次汇总 + 明细。
type judgeBatchDTO struct {
	// Note 对应 printJudgeSummary 的 note 参数（如 "model=xxx, prompt=yyy"）。
	Note string `json:"note,omitempty"`
	// Total 总 case 数。
	Total int `json:"total"`
	// Passed AllPass=true 的 case 数。
	Passed int `json:"passed"`
	// PassRate = Passed / Total；Total=0 时为 0。冗余字段，省去下游算一遍。
	PassRate float64 `json:"pass_rate"`
	// DimAvg 按维度名排序的均分（map 本身无序，这里排序后序列化为对象——
	// 但 JSON 对象本就无序约束，所以我们额外给 DimAvgOrder 暴露排序切片）。
	DimAvg map[string]float64 `json:"dim_avg"`
	// DimAvgOrder 维度名字母序升序（稳定顺序，让 diff / PR 评论更稳）。
	DimAvgOrder []string `json:"dim_avg_order"`
	// Cases 单 case 明细。已按 CaseID 排序（RunBatch 保证）。
	Cases []caseReportDTO `json:"cases"`
}

// caseReportDTO 单 case 的明细打分。
type caseReportDTO struct {
	// CaseID 来自 evalset.json 的 eval_id。
	CaseID string `json:"case_id"`
	// AvgScore 该 case 在所有维度上的均分。
	AvgScore float64 `json:"avg_score"`
	// AllPass 所有维度是否都过阈值。
	AllPass bool `json:"all_pass"`
	// Scores 每个维度的打分。
	Scores []scoreDTO `json:"scores"`
}

// scoreDTO 单维度打分。
type scoreDTO struct {
	Dimension string  `json:"dimension"`
	Score     float64 `json:"score"`
	Pass      bool    `json:"pass"`
	Reason    string  `json:"reason,omitempty"`
}

// judgeEntry 把一个 Judge 批次转为 DTO 形态；summary=nil 时返回 (zero, false)。
func judgeEntry(summary *eval.BatchJudgeSummary, note string) (judgeBatchDTO, bool) {
	if summary == nil {
		return judgeBatchDTO{}, false
	}
	entry := judgeBatchDTO{
		Note:   note,
		Total:  summary.Total,
		Passed: summary.Passed,
	}
	if summary.Total > 0 {
		entry.PassRate = float64(summary.Passed) / float64(summary.Total)
	}
	// DimAvg 做浅拷贝（外部可能继续读 summary；避免共享 map 被改）。
	entry.DimAvg = make(map[string]float64, len(summary.DimAvg))
	for k, v := range summary.DimAvg {
		entry.DimAvg[k] = v
		entry.DimAvgOrder = append(entry.DimAvgOrder, k)
	}
	sort.Strings(entry.DimAvgOrder)

	entry.Cases = make([]caseReportDTO, 0, len(summary.Reports))
	for _, rep := range summary.Reports {
		if rep == nil {
			continue
		}
		c := caseReportDTO{
			CaseID:   rep.CaseID,
			AvgScore: rep.AvgScore,
			AllPass:  rep.AllPass,
		}
		c.Scores = make([]scoreDTO, 0, len(rep.Scores))
		for _, s := range rep.Scores {
			c.Scores = append(c.Scores, scoreDTO{
				Dimension: s.Dimension,
				Score:     s.Score,
				Pass:      s.Pass,
				Reason:    s.Reason,
			})
		}
		entry.Cases = append(entry.Cases, c)
	}
	return entry, true
}

// WriteJudgeReportJSON 把两个 Judge 的结果落盘到 outPath。
//
//   - outPath 为空 → no-op，返回 nil（让调用方可以无脑调用）。
//   - 父目录不存在会自动 MkdirAll（方便首次跑 CI 不用额外 mkdir）。
//   - 两个 summary 都为 nil 时仍会落一份 schema_version + 空 judges 的 JSON，
//     好处是下游 CI 能识别"跑了但没 Judge 开"的情况，而不是"文件不存在"。
//   - 序列化失败会原样返回错误；写盘成功返回 nil。
//
// 参数：
//
//	evalSetID    评测集 ID，写入顶层 eval_set_id。
//	llmSummary   LLMJudge 批次结果；nil 表示未启用。
//	llmNote      printJudgeSummary 里的 note 字符串（model / prompt 信息）。
//	toolSummary  ToolSelectionJudge 批次结果；nil 表示未启用。
//	toolNote     同上（ToolSelectionJudge 的 note 固定给 fixed 字符串）。
//	outPath      落盘路径（建议 .json 后缀）。
func WriteJudgeReportJSON(
	evalSetID string,
	llmSummary *eval.BatchJudgeSummary, llmNote string,
	toolSummary *eval.BatchJudgeSummary, toolNote string,
	outPath string,
) error {
	if outPath == "" {
		return nil
	}
	dto := judgeReportDTO{
		SchemaVersion: JudgeReportSchemaVersion,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		EvalSetID:     evalSetID,
		Judges:        map[string]judgeBatchDTO{},
	}
	if e, ok := judgeEntry(llmSummary, llmNote); ok {
		dto.Judges["llm"] = e
	}
	if e, ok := judgeEntry(toolSummary, toolNote); ok {
		dto.Judges["tool"] = e
	}

	// 先序列化，再写盘——避免"目录建了但 JSON 写到一半失败留下半成品"。
	buf, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		return fmt.Errorf("judge-json: marshal: %w", err)
	}
	// 父目录容错：不存在就建；已存在就跳过。
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("judge-json: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(outPath, buf, 0o644); err != nil {
		return fmt.Errorf("judge-json: write %s: %w", outPath, err)
	}
	return nil
}
