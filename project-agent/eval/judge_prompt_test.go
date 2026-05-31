package eval

import (
	"strings"
	"testing"
)

// TestBuildJudgeUserPrompt_Sections 模板应包含维度/问题/答复三段。
func TestBuildJudgeUserPrompt_Sections(t *testing.T) {
	in := JudgeInput{
		CaseID:         "c1",
		UserQuery:      "pod 为什么不断重启？",
		FinalAnswer:    "OOM 导致 pod 被 kill",
		ExpectedAnswer: "内存溢出",
		Dimensions:     DefaultJudgeDimensions(),
	}
	p := BuildJudgeUserPrompt(in)
	for _, must := range []string{
		"维度列表", "用户问题", "参考答案", "待评答复",
		"pod 为什么不断重启？", "OOM 导致 pod 被 kill", "内存溢出",
		"RootCauseAccuracy", "EvidenceSufficiency", "HelpfulnessSafety",
	} {
		if !strings.Contains(p, must) {
			t.Fatalf("prompt 缺失片段 %q；prompt=%s", must, p)
		}
	}
}

// TestBuildJudgeUserPrompt_NoExpected 参考答案为空时应省略该段。
func TestBuildJudgeUserPrompt_NoExpected(t *testing.T) {
	p := BuildJudgeUserPrompt(JudgeInput{
		CaseID: "c1", UserQuery: "q", FinalAnswer: "a",
	})
	if strings.Contains(p, "参考答案") {
		t.Fatalf("空 expected 不应渲染参考答案段；prompt=%s", p)
	}
	if !strings.Contains(p, "用户问题") || !strings.Contains(p, "待评答复") {
		t.Fatalf("基本段缺失；prompt=%s", p)
	}
}

// TestBuildJudgeUserPrompt_EmptyDims 空维度应回退默认 3 维。
func TestBuildJudgeUserPrompt_EmptyDims(t *testing.T) {
	p := BuildJudgeUserPrompt(JudgeInput{CaseID: "x", UserQuery: "q", FinalAnswer: "a"})
	for _, name := range []string{"RootCauseAccuracy",
		"EvidenceSufficiency", "HelpfulnessSafety"} {
		if !strings.Contains(p, name) {
			t.Fatalf("空维度回退缺 %s", name)
		}
	}
}

// TestParseJudgeResponse_Clean 正常 JSON 路径。
func TestParseJudgeResponse_Clean(t *testing.T) {
	raw := `{"scores":[
		{"dimension":"RootCauseAccuracy","score":0.92,"reason":"指出 OOM"},
		{"dimension":"EvidenceSufficiency","score":0.88,"reason":"引用日志"},
		{"dimension":"HelpfulnessSafety","score":0.70,"reason":"未提 HITL"}
	]}`
	scores, err := ParseJudgeResponse(raw, DefaultJudgeDimensions())
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("期望 3 条分数，got %d", len(scores))
	}
	if !scores[0].Pass || !scores[1].Pass || scores[2].Pass {
		t.Fatalf("Pass 判定异常: %+v", scores)
	}
}

// TestParseJudgeResponse_WithMarkdownFence 允许 ```json``` 围栏。
func TestParseJudgeResponse_WithMarkdownFence(t *testing.T) {
	raw := "```json\n" + `{"scores":[{"dimension":"RootCauseAccuracy","score":0.9,"reason":"OK"}]}` + "\n```"
	scores, err := ParseJudgeResponse(raw, []JudgeDimension{
		{Name: "RootCauseAccuracy", Threshold: 0.8},
	})
	if err != nil {
		t.Fatalf("围栏应被剥离；err=%v", err)
	}
	if len(scores) != 1 || scores[0].Score != 0.9 {
		t.Fatalf("解析结果不符: %+v", scores)
	}
}

// TestParseJudgeResponse_WithPreamble 前后文噪音时应能裁切出 JSON。
func TestParseJudgeResponse_WithPreamble(t *testing.T) {
	raw := "我的分析如下：\n{\"scores\":[{\"dimension\":\"D1\",\"score\":0.5,\"reason\":\"OK\"}]}\n谢谢。"
	scores, err := ParseJudgeResponse(raw, []JudgeDimension{
		{Name: "D1", Threshold: 0.4},
	})
	if err != nil {
		t.Fatalf("应通过裁切恢复 JSON；err=%v", err)
	}
	if len(scores) != 1 || !scores[0].Pass {
		t.Fatalf("解析异常: %+v", scores)
	}
}

// TestParseJudgeResponse_ScoreClamped 越界分数应夹到 [0,1]。
func TestParseJudgeResponse_ScoreClamped(t *testing.T) {
	raw := `{"scores":[
		{"dimension":"D1","score":1.5,"reason":"high"},
		{"dimension":"D2","score":-0.3,"reason":"neg"}
	]}`
	scores, err := ParseJudgeResponse(raw, []JudgeDimension{
		{Name: "D1", Threshold: 0.5}, {Name: "D2", Threshold: 0.5},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if scores[0].Score != 1 {
		t.Fatalf("D1 应被夹到 1，实际 %.2f", scores[0].Score)
	}
	if scores[1].Score != 0 {
		t.Fatalf("D2 应被夹到 0，实际 %.2f", scores[1].Score)
	}
}

// TestParseJudgeResponse_MissingDimensionFillsZero 漏返维度应补零分。
func TestParseJudgeResponse_MissingDimensionFillsZero(t *testing.T) {
	raw := `{"scores":[{"dimension":"A","score":0.9,"reason":"ok"}]}`
	scores, err := ParseJudgeResponse(raw, []JudgeDimension{
		{Name: "A", Threshold: 0.5},
		{Name: "B", Threshold: 0.5}, // LLM 漏返
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(scores) != 2 {
		t.Fatalf("应按请求维度数对齐 2 条，实际 %d", len(scores))
	}
	if scores[1].Score != 0 || scores[1].Pass {
		t.Fatalf("漏返维度应 0 分 + 不通过；实际 %+v", scores[1])
	}
	if !strings.Contains(scores[1].Reason, "未返回") {
		t.Fatalf("漏返理由不符: %s", scores[1].Reason)
	}
}

// TestParseJudgeResponse_Empty 空/不可解析应返 error。
func TestParseJudgeResponse_Empty(t *testing.T) {
	if _, err := ParseJudgeResponse("", DefaultJudgeDimensions()); err == nil {
		t.Fatal("空串应返错")
	}
	if _, err := ParseJudgeResponse("totally not json",
		DefaultJudgeDimensions()); err == nil {
		t.Fatal("非法文本应返错")
	}
}
