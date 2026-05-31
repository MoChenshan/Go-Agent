// alarm_silence_test.go —— bk_alarm_silence 工具单元测试。
//
// 覆盖点（按 scope 组织，与 pod_restart_test 同构）：
//
//  A) by_strategy
//     1. 缺 bk_biz_id → 报错
//     2. 缺 strategy_ids → 报错
//     3. duration=0 → 拒绝
//     4. duration > 24h → 硬拒 + 审计 rejected_by=duration_hard_limit
//     5. 未 confirmed 短时长返 Plan（Low）
//     6. 未 confirmed 长时长返 Plan（Medium）
//     7. confirmed 成功（Mock 返回 silence_id）
//
//  B) by_target
//     8. 缺 targets → 报错
//     9. 单目标未 confirmed 返 Plan（Medium/High 依时长）
//    10. 批量 ≥5 升档 High
//    11. 批量 > 50 硬拒
//    12. confirmed 成功
//
//  C) by_dimension
//    13. 缺 dimensions → 报错
//    14. 未 confirmed 返 Plan（Critical + RequireReason）
//    15. confirmed 无 reason → 拒绝
//    16. confirmed + reason → 成功
//
//  D) unsilence
//    17. 缺 silence_id → 报错
//    18. 未 confirmed 返 Plan（Low）
//    19. confirmed 成功
//
//  E) 通用
//    20. 未知 scope → 报错
//    21. Severity 枚举
//    22. 审计事件字段完整性（from=nil / to=silence_id）
package bktools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// ---- 测试辅助 --------------------------------------------------------------

func callSilence(t *testing.T, tl tool.Tool, in AlarmSilenceInput) (*Result, error) {
	t.Helper()
	ct, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("tool is not CallableTool: %T", tl)
	}
	argsJSON, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	raw, err := ct.Call(context.Background(), argsJSON)
	if err != nil {
		return nil, err
	}
	r, ok := raw.(*Result)
	if !ok {
		t.Fatalf("result type mismatch: %T", raw)
	}
	return r, nil
}

func mustCallSilence(t *testing.T, tl tool.Tool, in AlarmSilenceInput) *Result {
	t.Helper()
	r, err := callSilence(t, tl, in)
	if err != nil {
		t.Fatalf("callSilence unexpected error: %v", err)
	}
	return r
}

func newMockSilenceTool() tool.Tool {
	return newAlarmSilenceTool(bkapi.NewClient(bkapi.WithMockMode(true)))
}

// -----------------------------------------------------------------------------
// A) by_strategy
// -----------------------------------------------------------------------------

func TestSilence_ByStrategy_MissingBizRejected(t *testing.T) {
	tl := newMockSilenceTool()
	_, err := callSilence(t, tl, AlarmSilenceInput{
		Scope: "by_strategy", StrategyIDs: []int{1}, DurationSeconds: 1800,
	})
	if err == nil {
		t.Fatal("缺 bk_biz_id 必须报错")
	}
}

func TestSilence_ByStrategy_MissingIDsRejected(t *testing.T) {
	tl := newMockSilenceTool()
	_, err := callSilence(t, tl, AlarmSilenceInput{
		Scope: "by_strategy", BKBizID: 100, DurationSeconds: 1800,
	})
	if err == nil {
		t.Fatal("缺 strategy_ids 必须报错")
	}
}

func TestSilence_ByStrategy_ZeroDurationRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_strategy", BKBizID: 100, StrategyIDs: []int{1}, DurationSeconds: 0, Confirmed: true,
	})
	if result.OK {
		t.Fatal("duration=0 必须被拒")
	}
	if !strings.Contains(result.Message, "duration_seconds") {
		t.Errorf("错误信息应提 duration_seconds，实际=%q", result.Message)
	}
}

func TestSilence_ByStrategy_OverHardLimitRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	t.Setenv("AUDIT_DISABLE", "")
	mem := &audit.MemorySink{}
	old := audit.SetSink(mem)
	defer audit.SetSink(old)

	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_strategy", BKBizID: 100, StrategyIDs: []int{1},
		DurationSeconds: silenceHardMaxSeconds + 1,
		Confirmed:       true,
	})
	if result.OK {
		t.Fatal("超 24h 必须硬拒")
	}
	// 审计：必须留下 rejected_by=duration_hard_limit
	var found bool
	for _, line := range mem.Snapshot() {
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Params["rejected_by"] == "duration_hard_limit" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("硬拒必须留下 rejected_by=duration_hard_limit 审计")
	}
}

func TestSilence_ByStrategy_ShortDurationIsLow(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_strategy", BKBizID: 100, StrategyIDs: []int{1},
		DurationSeconds: 1800, // 30min <= 1h
		Confirmed:       false,
	})
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Plan.Severity != hitl.SeverityLow {
		t.Errorf("短时长 by_strategy 应 Low，实际=%v", pending.Plan.Severity)
	}
}

func TestSilence_ByStrategy_LongDurationIsMedium(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_strategy", BKBizID: 100, StrategyIDs: []int{1},
		DurationSeconds: 7200, // 2h > 1h
	})
	pending, _ := result.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityMedium {
		t.Errorf("长时长 by_strategy 应 Medium，实际=%v", pending.Plan.Severity)
	}
}

func TestSilence_ByStrategy_ConfirmedSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_strategy", BKBizID: 100, StrategyIDs: []int{1, 2},
		DurationSeconds: 1800, Confirmed: true,
	})
	if !result.OK {
		t.Fatalf("应成功，msg=%s", result.Message)
	}
	if !result.Mock {
		t.Error("Mock 模式应 Mock=true")
	}
	data, _ := result.Data.(map[string]any)
	id, _ := data["silence_id"].(string)
	if !strings.HasPrefix(id, "MOCK-SILENCE-BY_STRATEGY-100-") {
		t.Errorf("silence_id 前缀不符，实际=%q", id)
	}
}

// -----------------------------------------------------------------------------
// B) by_target
// -----------------------------------------------------------------------------

func TestSilence_ByTarget_MissingTargetsRejected(t *testing.T) {
	tl := newMockSilenceTool()
	_, err := callSilence(t, tl, AlarmSilenceInput{
		Scope: "by_target", BKBizID: 100, DurationSeconds: 1800,
	})
	if err == nil {
		t.Fatal("缺 targets 必须报错")
	}
}

func TestSilence_ByTarget_SingleTargetUnconfirmedReturnsPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_target", BKBizID: 100, Targets: []string{"ip=10.1.1.1"},
		DurationSeconds: 1800,
	})
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Plan.Severity != hitl.SeverityMedium {
		t.Errorf("单目标 by_target 短时长应 Medium，实际=%v", pending.Plan.Severity)
	}
}

func TestSilence_ByTarget_BatchOverSoftIsHigh(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockSilenceTool()
	// 5 个 targets 触发 soft limit，Severity=High
	targets := []string{"ip=10.1.1.1", "ip=10.1.1.2", "ip=10.1.1.3", "ip=10.1.1.4", "ip=10.1.1.5"}
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_target", BKBizID: 100, Targets: targets,
		DurationSeconds: 1800,
	})
	pending, _ := result.Data.(hitl.PendingResult)
	if pending.Plan.Severity != hitl.SeverityHigh {
		t.Errorf("批量≥5 应 High，实际=%v", pending.Plan.Severity)
	}
}

func TestSilence_ByTarget_OverHardLimitRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSilenceTool()
	targets := make([]string, silenceTargetsHardLimit+1)
	for i := range targets {
		targets[i] = "ip=10.1.1." + string(rune('A'+i))
	}
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_target", BKBizID: 100, Targets: targets,
		DurationSeconds: 1800, Confirmed: true,
	})
	if result.OK {
		t.Fatal("批量 > 硬上限必须被拒")
	}
}

func TestSilence_ByTarget_ConfirmedSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_target", BKBizID: 100, Targets: []string{"ip=10.1.1.1"},
		DurationSeconds: 1800, Confirmed: true,
	})
	if !result.OK {
		t.Fatalf("应成功，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	if _, ok := data["silence_id"].(string); !ok {
		t.Errorf("silence_id 应存在，Data=%v", data)
	}
}

// -----------------------------------------------------------------------------
// C) by_dimension
// -----------------------------------------------------------------------------

func TestSilence_ByDimension_MissingDimensionsRejected(t *testing.T) {
	tl := newMockSilenceTool()
	_, err := callSilence(t, tl, AlarmSilenceInput{
		Scope: "by_dimension", BKBizID: 100, DurationSeconds: 1800,
	})
	if err == nil {
		t.Fatal("缺 dimensions 必须报错")
	}
}

func TestSilence_ByDimension_UnconfirmedReturnsCriticalPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope:      "by_dimension",
		BKBizID:    100,
		Dimensions: map[string]string{"env": "gray", "service": "game-core"},
		DurationSeconds: 1800,
	})
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("by_dimension 必须 Critical，实际=%v", pending.Plan.Severity)
	}
	if !pending.Plan.RequireReason {
		t.Error("by_dimension 必须 RequireReason=true")
	}
}

func TestSilence_ByDimension_ConfirmedWithoutReasonRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope:      "by_dimension",
		BKBizID:    100,
		Dimensions: map[string]string{"env": "gray"},
		DurationSeconds: 1800,
		Confirmed:  true, // 无 reason
	})
	if result.OK {
		t.Fatal("by_dimension 无 reason 必须被拒")
	}
	if !strings.Contains(result.Message, "reason") {
		t.Errorf("错误信息应提 reason，实际=%q", result.Message)
	}
}

func TestSilence_ByDimension_WithReasonSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope:      "by_dimension",
		BKBizID:    100,
		Dimensions: map[string]string{"env": "gray"},
		DurationSeconds: 1800,
		Reason:     "灰度发布窗口",
		Confirmed:  true,
	})
	if !result.OK {
		t.Fatalf("带 reason 应成功，msg=%s", result.Message)
	}
}

// -----------------------------------------------------------------------------
// D) unsilence
// -----------------------------------------------------------------------------

func TestSilence_Unsilence_MissingIDRejected(t *testing.T) {
	tl := newMockSilenceTool()
	_, err := callSilence(t, tl, AlarmSilenceInput{Scope: "unsilence"})
	if err == nil {
		t.Fatal("缺 silence_id 必须报错")
	}
}

func TestSilence_Unsilence_UnconfirmedReturnsLowPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "unsilence", SilenceID: "S-001",
	})
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Plan.Severity != hitl.SeverityLow {
		t.Errorf("unsilence 应 Low 鼓励使用，实际=%v", pending.Plan.Severity)
	}
	if pending.Plan.Action != "bk.alarm.unsilence" {
		t.Errorf("Action 应为 bk.alarm.unsilence，实际=%q", pending.Plan.Action)
	}
}

func TestSilence_Unsilence_ConfirmedSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockSilenceTool()
	result := mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "unsilence", SilenceID: "S-001", Confirmed: true,
	})
	if !result.OK {
		t.Fatalf("应成功，msg=%s", result.Message)
	}
	if !result.Mock {
		t.Error("Mock 模式应 Mock=true")
	}
	data, _ := result.Data.(map[string]any)
	if data["silence_id"] != "S-001" {
		t.Errorf("silence_id 应为 S-001，实际=%v", data["silence_id"])
	}
}

// -----------------------------------------------------------------------------
// E) 通用 / Severity / 审计
// -----------------------------------------------------------------------------

func TestSilence_UnknownScopeRejected(t *testing.T) {
	tl := newMockSilenceTool()
	_, err := callSilence(t, tl, AlarmSilenceInput{Scope: "mute", BKBizID: 100})
	if err == nil {
		t.Fatal("未知 scope 必须报错")
	}
	if !strings.Contains(err.Error(), "不支持") {
		t.Errorf("错误信息应提 '不支持'，实际=%v", err)
	}
}

func TestClassifySilenceSeverity_Enumeration(t *testing.T) {
	cases := []struct {
		scope    string
		duration int
		batch    int
		want     hitl.Severity
	}{
		{"by_strategy", 1800, 0, hitl.SeverityLow},     // 短 strategy → Low
		{"by_strategy", 7200, 0, hitl.SeverityMedium},  // 长 strategy → Medium
		{"by_target", 1800, 1, hitl.SeverityMedium},    // 单 target → Medium
		{"by_target", 7200, 1, hitl.SeverityHigh},      // 长 target → High
		{"by_target", 1800, 5, hitl.SeverityHigh},      // 批量 target → High
		{"by_target", 1800, 10, hitl.SeverityHigh},     // 大批量 target → High
		{"by_dimension", 1800, 0, hitl.SeverityCritical}, // dimension 永远 Critical
		{"by_dimension", 7200, 0, hitl.SeverityCritical},
	}
	for _, c := range cases {
		got := classifySilenceSeverity(c.scope, c.duration, c.batch, false)
		if got != c.want {
			t.Errorf("scope=%s dur=%d batch=%d: want=%v got=%v",
				c.scope, c.duration, c.batch, c.want, got)
		}
	}
}

func TestSilence_AuditEvent_FromToFields(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	t.Setenv("AUDIT_DISABLE", "")
	mem := &audit.MemorySink{}
	old := audit.SetSink(mem)
	defer audit.SetSink(old)

	tl := newMockSilenceTool()
	_ = mustCallSilence(t, tl, AlarmSilenceInput{
		Scope: "by_strategy", BKBizID: 100, StrategyIDs: []int{1},
		DurationSeconds: 1800, Confirmed: true,
	})

	var hit *audit.Record
	for _, line := range mem.Snapshot() {
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("审计应为合法 JSON：%s", line)
		}
		if rec.Action == "bk.alarm.silence" {
			hit = &rec
			break
		}
	}
	if hit == nil {
		t.Fatal("未找到 bk.alarm.silence 审计事件")
	}
	// from 应为 nil（创建前无 silence_id）
	if _, exists := hit.Params["from"]; !exists {
		t.Error("审计 Params 应包含 from 字段（即使为 nil）")
	}
	// to 应为 mock 生成的 silence_id 字符串
	to, _ := hit.Params["to"].(string)
	if !strings.HasPrefix(to, "MOCK-SILENCE-") {
		t.Errorf("审计 to 应为 mock silence_id，实际=%v", hit.Params["to"])
	}
	if hit.Params["scope"] != "by_strategy" {
		t.Errorf("scope 错误：%v", hit.Params["scope"])
	}
	if hit.Params["bk_biz_id"] != float64(100) { // JSON 往返后是 float64
		t.Errorf("bk_biz_id 错误：%v (%T)", hit.Params["bk_biz_id"], hit.Params["bk_biz_id"])
	}
	if hit.Result != "success" {
		t.Errorf("Result 应为 success，实际=%q", hit.Result)
	}
	if !hit.Mock {
		t.Error("Mock 应为 true")
	}
}

// 验证 formatDuration 的几个关键路径（虽然是小函数，但输出进入 Plan 文案）
func TestFormatDuration(t *testing.T) {
	cases := []struct {
		sec  int
		want string
	}{
		{1800, "30m"},
		{3600, "1h"},
		{7200, "2h"},
		{5400, "1h30m"},
		{60, "1m"},
	}
	for _, c := range cases {
		got := formatDuration(c.sec)
		if got != c.want {
			t.Errorf("formatDuration(%d): want=%q got=%q", c.sec, c.want, got)
		}
	}
}
