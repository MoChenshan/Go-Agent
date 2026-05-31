// judge_tool_selection_client.go —— D30 纯算法 JudgeClient。
//
// # 定位
//
// LLMJudge 适合评"答案质量"（RootCause / Evidence / Helpfulness），
// 但 ToolSelectionAccuracy 本身就是确定性算法分（ScoreToolSelection），
// 没有任何 LLM 价值——让 LLM 评它既浪费 token 又不稳定。
//
// 于是把它封装成独立 JudgeClient：
//
//	var j JudgeClient = eval.NewToolSelectionJudge()
//	report, _ := j.Score(ctx, JudgeInput{
//	    CaseID: "case_x",
//	    ExpectedToolCalls: []string{"bcs_node_describe", "bcs_network_update"},
//	    ActualToolCalls:   []string{"bcs_node_describe", "bcs_network_update"},
//	})
//
// 优点：
//   - 零网络 IO、零 token 花费；
//   - 和 LLMJudge 都走 JudgeClient 接口，可在 RunBatch 里复用同一套聚合逻辑；
//   - CI 环境无 API Key 也能跑；
//   - 和 LLMJudge 正交——SRE 可以两个一起开，互不影响。
package eval

import (
	"context"
	"fmt"
)

// ToolSelectionJudge 仅打 DimensionToolSelectionAccuracy 这一维度的 JudgeClient。
//
// 其他维度名被忽略（返回空 Scores 切片里不包含）。
// 这样"误配上 V1 三维度"不会触发 LLM 调用，而只是得到一个空 report（AllPass=true）。
// 这是刻意为之的"安静退化"：让 CLI 层面可以把一组 case 统一喂给两个 Judge，
// 由本 Judge 自行过滤它关心的维度。
type ToolSelectionJudge struct{}

// NewToolSelectionJudge 构造。
func NewToolSelectionJudge() *ToolSelectionJudge {
	return &ToolSelectionJudge{}
}

// Score 实现 JudgeClient。
//
// 维度过滤：只看 DimensionToolSelectionAccuracy，其余维度直接跳过。
// 若 in.Dimensions 为空，默认评该维度一次（使用默认阈值）。
func (ToolSelectionJudge) Score(_ context.Context, in JudgeInput) (*JudgeReport, error) {
	if in.CaseID == "" {
		return nil, fmt.Errorf("tool_selection_judge: CaseID required")
	}
	// 选出需要评的维度（默认只评 ToolSelectionAccuracy 自身）。
	var target []JudgeDimension
	if len(in.Dimensions) == 0 {
		target = []JudgeDimension{ToolSelectionAccuracyDimension()}
	} else {
		for _, d := range in.Dimensions {
			if d.Name == DimensionToolSelectionAccuracy {
				target = append(target, d)
			}
		}
	}

	rep := &JudgeReport{CaseID: in.CaseID, AllPass: true}
	if len(target) == 0 {
		// 没有要评的 Tool 维度 → 空报告，AllPass 按惯例为 true。
		// （上层如果同时跑了 LLMJudge，这张报告合并时不会造成负面影响）
		return rep, nil
	}

	var total float64
	for _, d := range target {
		score := ScoreToolSelection(in.ExpectedToolCalls, in.ActualToolCalls)
		s := JudgeScore{
			Dimension: d.Name,
			Score:     score,
			Pass:      score >= d.Threshold,
			Reason: fmt.Sprintf("expected=%d actual=%d score=%.2f threshold=%.2f",
				len(in.ExpectedToolCalls), len(in.ActualToolCalls), score, d.Threshold),
		}
		rep.Scores = append(rep.Scores, s)
		if !s.Pass {
			rep.AllPass = false
		}
		total += s.Score
	}
	rep.AvgScore = total / float64(len(target))
	return rep, nil
}
