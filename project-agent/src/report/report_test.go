package report

import (
	"encoding/json"
	"strings"
	"testing"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
)

// 1. 空 Report 也能渲染成合法 Markdown / JSON
func TestRender_EmptyReport(t *testing.T) {
	b := NewBuilder("")
	md, err := b.Render(FormatMarkdown)
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	if !strings.Contains(string(md), "# 修复报告") {
		t.Errorf("markdown missing title header: %s", md)
	}
	js, err := b.Render(FormatJSON)
	if err != nil {
		t.Fatalf("render json: %v", err)
	}
	var rec Report
	if err := json.Unmarshal(js, &rec); err != nil {
		t.Fatalf("json should be unmarshalable: %v\n%s", err, js)
	}
	if rec.Version != SchemaVersion {
		t.Errorf("version want=%s got=%s", SchemaVersion, rec.Version)
	}
	if rec.CaseID == "" {
		t.Errorf("case_id should auto-generate when empty")
	}
}

// 2. 完整 Report 各字段正确写入
func TestBuilder_Fluent(t *testing.T) {
	b := NewBuilder("case-20260421-oom-01").
		SetTitle("game-core OOM 重启事件").
		SetSeverity(SeverityHigh).
		SetBackground("凌晨 03:12—03:41 Pod 连续重启 3 次").
		SetDiagnosis("Old Gen 内存攀升至 95%，Full GC > 30s").
		SetOutcome("重启消除，RT 恢复基线").
		AddAction(Action{
			Action:      "bcs.helm.upgrade",
			Description: "提升 -Xmx 至 12G",
			Target:      "BCS-K8S-001/ns-letsgo/game-core",
			Severity:    SeverityHigh,
			Result:      "success",
			Operator:    "alice",
			TS:          "2026-04-21T03:45:00+08:00",
		}).
		AddReference(Reference{Kind: "mr", Title: "gameops/fix-oom#42", URL: "https://git/.../42"})

	r := b.Build()
	if r.Severity != SeverityHigh {
		t.Errorf("severity mismatch: %s", r.Severity)
	}
	if len(r.Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(r.Actions))
	}
	if len(r.Timeline) != 1 {
		t.Fatalf("AddAction should also push timeline, got %d", len(r.Timeline))
	}
	if r.Timeline[0].Kind != "action" {
		t.Errorf("timeline kind want=action got=%s", r.Timeline[0].Kind)
	}
	if len(r.References) != 1 {
		t.Errorf("refs len: %d", len(r.References))
	}
}

// 3. Markdown 渲染包含所有 6 段关键标题
func TestRenderMarkdown_Sections(t *testing.T) {
	b := NewBuilder("case-x").
		SetTitle("T").
		SetBackground("B").
		SetDiagnosis("D").
		SetOutcome("O").
		AddAction(Action{Action: "a1", Description: "d1", Result: "success", TS: "2026-04-21T03:45:00+08:00"}).
		AddReference(Reference{Kind: "tapd", Title: "BUG-123"})

	md := string(mustRender(t, b, FormatMarkdown))
	for _, want := range []string{
		"# 修复报告 — T",
		"## 一、背景",
		"## 二、诊断结论",
		"## 三、修复动作",
		"## 四、时间轴",
		"## 五、结论",
		"## 六、关联资源",
		"✅ success",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing section %q:\n%s", want, md)
		}
	}
}

// 4. JSON 渲染可被再次 Unmarshal，且字段完整
func TestRenderJSON_Roundtrip(t *testing.T) {
	b := NewBuilder("case-x").
		SetSeverity(SeverityCritical).
		AddAction(Action{Action: "a1", Result: "failure", ErrorMsg: "boom", TS: "2026-04-21T03:45:00+08:00"})

	js := mustRender(t, b, FormatJSON)
	var r Report
	if err := json.Unmarshal(js, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Severity != SeverityCritical {
		t.Errorf("severity lost: %s", r.Severity)
	}
	if len(r.Actions) != 1 || r.Actions[0].ErrorMsg != "boom" {
		t.Errorf("action error_msg lost: %+v", r.Actions)
	}
}

// 5. 从 audit.Record 聚合，结果进入 Actions 且 Timeline 同步
func TestAppendAuditRecords(t *testing.T) {
	recs := []audit.Record{
		{
			TS: "2026-04-21T03:45:00+08:00", User: "alice", Agent: "repair_agent",
			Action: "bcs.helm.upgrade", Severity: "high",
			Target: "BCS-K8S-001/ns/game-core",
			Params: map[string]any{"chart": "game-core", "version": "v1.2.3"},
			Reason: "提升 -Xmx 至 12G", Result: "success",
		},
		{
			TS: "2026-04-21T03:50:00+08:00", User: "alice", Agent: "repair_agent",
			Action: "gongfeng.mr.merge", Severity: "high",
			Target: "gameops/fix-oom!42", Result: "failure",
			ErrorMsg: "CI red", Mock: true,
		},
	}
	b := NewBuilder("case-agg").AppendAuditRecords(recs)
	r := b.Build()
	if len(r.Actions) != 2 {
		t.Fatalf("want 2 actions, got %d", len(r.Actions))
	}
	if r.Actions[0].TS >= r.Actions[1].TS {
		t.Errorf("actions should be sorted ascending: %+v", r.Actions)
	}
	if len(r.Timeline) != 2 {
		t.Fatalf("want 2 timeline items, got %d", len(r.Timeline))
	}
	if !r.Actions[1].Mock {
		t.Errorf("mock flag lost")
	}
	if r.Actions[1].ErrorMsg != "CI red" {
		t.Errorf("error_msg lost: %s", r.Actions[1].ErrorMsg)
	}
}

// 6. 时间轴按 TS 升序（乱序插入后 Build 应稳定排序）
func TestTimeline_SortedAscending(t *testing.T) {
	b := NewBuilder("case-sort")
	b.AddTimeline(TimelineItem{TS: "2026-04-21T04:00:00+08:00", Message: "late"})
	b.AddTimeline(TimelineItem{TS: "2026-04-21T03:00:00+08:00", Message: "early"})
	r := b.Build()
	if r.Timeline[0].Message != "early" || r.Timeline[1].Message != "late" {
		t.Errorf("timeline not sorted ascending: %+v", r.Timeline)
	}
}

// 7. 从 audit jsonl 字节流聚合，解析失败行静默跳过
func TestAppendTimelineFromAudit_JSONLResilient(t *testing.T) {
	good := `{"ts":"2026-04-21T03:45:00+08:00","user":"alice","action":"bcs.helm.upgrade","severity":"high","result":"success"}`
	bad := `{not a valid json`
	lines := [][]byte{
		[]byte(good + "\n"),
		[]byte(bad),
		nil,
		[]byte(""),
	}
	b := NewBuilder("case-jsonl").AppendTimelineFromAudit(lines)
	r := b.Build()
	if len(r.Actions) != 1 {
		t.Fatalf("want 1 action (bad line dropped), got %d", len(r.Actions))
	}
	if r.Actions[0].Action != "bcs.helm.upgrade" {
		t.Errorf("action parsed incorrectly: %+v", r.Actions[0])
	}
}

// 8. 未知格式返回错误
func TestRender_UnsupportedFormat(t *testing.T) {
	b := NewBuilder("case-x")
	if _, err := b.Render("xml"); err == nil {
		t.Errorf("expected error for unsupported format")
	}
}

// ---- helpers ----

func mustRender(t *testing.T, b *Builder, f Format) []byte {
	t.Helper()
	out, err := b.Render(f)
	if err != nil {
		t.Fatalf("render %s: %v", f, err)
	}
	return out
}
