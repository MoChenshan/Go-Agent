package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// fakeJudgeModel 是 LLMModel 接口的轻量假实现，用于单测注入。
//   - 支持按"预设回复"模式（resp）返回固定 JSON；
//   - 支持按"错误"模式（err）直接失败；
//   - 记录最后一次请求供断言。
type fakeJudgeModel struct {
	resp    string
	err     error
	channel bool // 是否用 channel 分片返回（默认 false：一次性返回）

	mu       sync.Mutex
	lastReq  *model.Request
	callCnt  int
}

func (f *fakeJudgeModel) GenerateContent(_ context.Context,
	req *model.Request) (<-chan *model.Response, error) {
	f.mu.Lock()
	f.lastReq = req
	f.callCnt++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan *model.Response, 2)
	// 模拟非流式返回：一个 Message.Content + Done=true
	ch <- &model.Response{
		Choices: []model.Choice{
			{Message: model.Message{Role: "assistant", Content: f.resp}},
		},
		Done: true,
	}
	close(ch)
	return ch, nil
}

// TestNewLLMJudge_ModelRequired 构造参数校验。
func TestNewLLMJudge_ModelRequired(t *testing.T) {
	if _, err := NewLLMJudge(LLMJudgeConfig{}); err == nil {
		t.Fatal("缺 Model 应返错")
	}
}

// TestNewLLMJudge_DefaultsFilled 默认字段回填。
func TestNewLLMJudge_DefaultsFilled(t *testing.T) {
	j, err := NewLLMJudge(LLMJudgeConfig{Model: &fakeJudgeModel{resp: "{}"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if j.cfg.SystemPrompt == "" {
		t.Fatal("SystemPrompt 默认应填充")
	}
	if j.cfg.MaxTokens != 1024 {
		t.Fatalf("MaxTokens 默认应 1024，实际 %d", j.cfg.MaxTokens)
	}
}

// TestLLMJudge_Score_Happy 完整正向路径：请求构造+解析+聚合。
func TestLLMJudge_Score_Happy(t *testing.T) {
	raw := `{"scores":[
		{"dimension":"RootCauseAccuracy","score":0.92,"reason":"指出 OOM"},
		{"dimension":"EvidenceSufficiency","score":0.85,"reason":"引用日志"},
		{"dimension":"HelpfulnessSafety","score":0.80,"reason":"HITL"}
	]}`
	fm := &fakeJudgeModel{resp: raw}
	var logged []string
	j, _ := NewLLMJudge(LLMJudgeConfig{
		Model: fm,
		Logger: func(event, caseID, msg string) {
			logged = append(logged, event+":"+caseID+":"+msg)
		},
	})
	rep, err := j.Score(context.Background(), JudgeInput{
		CaseID:         "c-ok",
		UserQuery:      "pod 重启？",
		FinalAnswer:    "OOM，日志可见，建议 HITL 确认回滚",
		ExpectedAnswer: "OOM",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !rep.AllPass {
		t.Fatalf("全维度应通过: %+v", rep)
	}
	if rep.AvgScore < 0.85 {
		t.Fatalf("平均分应 ≥ 0.85, got %.2f", rep.AvgScore)
	}
	// 断言请求体：system prompt + user prompt 齐全
	if fm.lastReq == nil || len(fm.lastReq.Messages) != 2 {
		t.Fatalf("请求消息数应 2")
	}
	if fm.lastReq.Messages[0].Role != "system" {
		t.Fatalf("首条应为 system, got %s", fm.lastReq.Messages[0].Role)
	}
	if !strings.Contains(fm.lastReq.Messages[1].Content, "pod 重启？") {
		t.Fatalf("user prompt 未包含原始问题")
	}
	// Logger 应 scored 事件
	hit := false
	for _, l := range logged {
		if strings.HasPrefix(l, "scored:c-ok:") {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("期望 Logger 收到 scored 事件, got %v", logged)
	}
}

// TestLLMJudge_Score_MissingCaseID 入参错误即失败。
func TestLLMJudge_Score_MissingCaseID(t *testing.T) {
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: &fakeJudgeModel{resp: "{}"}})
	if _, err := j.Score(context.Background(), JudgeInput{}); err == nil {
		t.Fatal("空 CaseID 应返错")
	}
}

// TestLLMJudge_Score_ModelError 后端错误应透传。
func TestLLMJudge_Score_ModelError(t *testing.T) {
	fm := &fakeJudgeModel{err: errors.New("network down")}
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm})
	_, err := j.Score(context.Background(), JudgeInput{CaseID: "c1"})
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("期望透传网络错误，got %v", err)
	}
}

// TestLLMJudge_Score_BadJSON 解析失败应返 error。
func TestLLMJudge_Score_BadJSON(t *testing.T) {
	fm := &fakeJudgeModel{resp: "not json at all"}
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm})
	_, err := j.Score(context.Background(), JudgeInput{
		CaseID: "c1", UserQuery: "q", FinalAnswer: "a",
	})
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("坏 JSON 应返 parse 错误, got %v", err)
	}
}

// TestLLMJudge_Score_EmptyResp 空 content 应返 error。
func TestLLMJudge_Score_EmptyResp(t *testing.T) {
	fm := &fakeJudgeModel{resp: ""}
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm})
	_, err := j.Score(context.Background(), JudgeInput{
		CaseID: "c1", UserQuery: "q", FinalAnswer: "a",
	})
	if err == nil {
		t.Fatal("空响应应返错")
	}
}

// TestLLMJudge_Score_PassThresholdFromDims Pass 判定遵循传入维度阈值。
func TestLLMJudge_Score_PassThresholdFromDims(t *testing.T) {
	raw := `{"scores":[{"dimension":"X","score":0.6,"reason":"ok"}]}`
	fm := &fakeJudgeModel{resp: raw}
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm})
	// 阈值 0.7 → 不通过；阈值 0.5 → 通过
	rep1, err := j.Score(context.Background(), JudgeInput{
		CaseID: "c1", UserQuery: "q", FinalAnswer: "a",
		Dimensions: []JudgeDimension{{Name: "X", Threshold: 0.7}},
	})
	if err != nil || rep1.AllPass {
		t.Fatalf("阈值 0.7 时不应通过: %+v err=%v", rep1, err)
	}
	rep2, _ := j.Score(context.Background(), JudgeInput{
		CaseID: "c2", UserQuery: "q", FinalAnswer: "a",
		Dimensions: []JudgeDimension{{Name: "X", Threshold: 0.5}},
	})
	if !rep2.AllPass {
		t.Fatalf("阈值 0.5 时应通过: %+v", rep2)
	}
}

// TestLLMJudge_RunBatch_Aggregates RunBatch 与 LLMJudge 串联聚合。
func TestLLMJudge_RunBatch_Aggregates(t *testing.T) {
	// 按 UserQuery 动态返回不同分数（模拟不同 case 结果）。
	fm := &scriptedModel{
		reply: func(req *model.Request) string {
			if strings.Contains(req.Messages[1].Content, "case-high") {
				return `{"scores":[
					{"dimension":"RootCauseAccuracy","score":0.9,"reason":"ok"},
					{"dimension":"EvidenceSufficiency","score":0.9,"reason":"ok"},
					{"dimension":"HelpfulnessSafety","score":0.9,"reason":"ok"}
				]}`
			}
			return `{"scores":[
				{"dimension":"RootCauseAccuracy","score":0.1,"reason":"miss"},
				{"dimension":"EvidenceSufficiency","score":0.1,"reason":"miss"},
				{"dimension":"HelpfulnessSafety","score":0.1,"reason":"miss"}
			]}`
		},
	}
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm})
	sum, err := RunBatch(context.Background(), j, []JudgeInput{
		{CaseID: "c-high", UserQuery: "case-high", FinalAnswer: "ok"},
		{CaseID: "c-low", UserQuery: "case-low", FinalAnswer: "bad"},
	})
	if err != nil {
		t.Fatalf("RunBatch err: %v", err)
	}
	if sum.Total != 2 || sum.Passed != 1 {
		t.Fatalf("聚合异常: total=%d passed=%d", sum.Total, sum.Passed)
	}
	// 断言 DimAvg 介于 0.1 ~ 0.9 之间（= 0.5）
	if avg := sum.DimAvg["RootCauseAccuracy"]; avg < 0.45 || avg > 0.55 {
		t.Fatalf("DimAvg.Root 均值应 ≈0.5，实际 %.2f", avg)
	}
}

// scriptedModel 按请求内容动态回复（用于 RunBatch 聚合测试）。
type scriptedModel struct {
	reply func(req *model.Request) string
}

func (s *scriptedModel) GenerateContent(_ context.Context,
	req *model.Request) (<-chan *model.Response, error) {
	content := s.reply(req)
	if content == "" {
		return nil, fmt.Errorf("scripted: no reply")
	}
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{
			{Message: model.Message{Role: "assistant", Content: content}},
		},
		Done: true,
	}
	close(ch)
	return ch, nil
}
