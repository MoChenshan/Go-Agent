package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
)

// newAfter 构造 AfterToolArgs 辅助函数。
func newAfter(name, raw string, err error) *tool.AfterToolArgs {
	return &tool.AfterToolArgs{
		ToolName:  name,
		Arguments: []byte(raw),
		Error:     err,
	}
}

// withMemSink 替换 audit.Sink 为 MemorySink，返回 sink 和复位函数。
func withMemSink(t *testing.T) (*audit.MemorySink, func()) {
	t.Helper()
	mem := &audit.MemorySink{}
	old := audit.SetSink(mem)
	return mem, func() { audit.SetSink(old) }
}

// TestAuditHook_NonWriteToolNotEmitted 读操作（不在白名单）不落审计。
func TestAuditHook_NonWriteToolNotEmitted(t *testing.T) {
	mem, reset := withMemSink(t)
	defer reset()
	t.Setenv("AUDIT_DISABLE", "")

	h := NewAuditHook(AuditHookConfig{AgentName: "repair_agent"})
	_, _ = h.after(context.Background(),
		newAfter("bk_alarm_query", `{"cluster":"c1"}`, nil))

	if got := len(mem.Snapshot()); got != 0 {
		t.Fatalf("读操作不应写审计，实际写了 %d 条", got)
	}
}

// TestAuditHook_WriteToolEmitted 写操作（mr_merge）必须落审计，字段核心字段正确。
func TestAuditHook_WriteToolEmitted(t *testing.T) {
	mem, reset := withMemSink(t)
	defer reset()
	t.Setenv("AUDIT_DISABLE", "")

	h := NewAuditHook(AuditHookConfig{AgentName: "repair_agent"})
	raw := `{"project_id":"group/app","iid":42,"target_branch":"develop",
	        "token":"SHOULD-NOT-APPEAR"}`
	_, _ = h.after(context.Background(),
		newAfter("gongfeng_mr_merge", raw, nil))

	lines := mem.Snapshot()
	if len(lines) != 1 {
		t.Fatalf("期望写 1 条，实际 %d", len(lines))
	}
	var rec audit.Record
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatalf("audit JSON 反序列失败: %v", err)
	}
	if rec.Agent != "repair_agent" {
		t.Errorf("Agent want repair_agent, got %q", rec.Agent)
	}
	if rec.Action != "tool.gongfeng_mr_merge" {
		t.Errorf("Action want tool.gongfeng_mr_merge, got %q", rec.Action)
	}
	if rec.Severity != "critical" {
		t.Errorf("Severity want critical (mr_merge), got %q", rec.Severity)
	}
	if rec.Result != "success" {
		t.Errorf("Result want success, got %q", rec.Result)
	}
	if rec.Target != "group/app" {
		t.Errorf("Target want group/app, got %q", rec.Target)
	}
	// 敏感字段 token 必须被剔除
	if _, ok := rec.Params["token"]; ok {
		t.Errorf("token 字段不应出现在 Params 里")
	}
	if v, ok := rec.Params["project_id"]; !ok || v != "group/app" {
		t.Errorf("project_id 应被保留")
	}
}

// TestAuditHook_FailureEmitted 工具执行失败时，审计记录 Result=failure 且带 Err。
func TestAuditHook_FailureEmitted(t *testing.T) {
	mem, reset := withMemSink(t)
	defer reset()
	t.Setenv("AUDIT_DISABLE", "")

	h := NewAuditHook(AuditHookConfig{AgentName: "repair_agent"})
	_, _ = h.after(context.Background(),
		newAfter("bcs_helm_manage",
			`{"action":"rollback","release":"game","cluster":"bcs-1"}`,
			errors.New("timeout")))

	lines := mem.Snapshot()
	if len(lines) != 1 {
		t.Fatalf("期望写 1 条，实际 %d", len(lines))
	}
	var rec audit.Record
	_ = json.Unmarshal(lines[0], &rec)
	if rec.Result != "failure" {
		t.Errorf("Result want failure, got %q", rec.Result)
	}
	if !strings.Contains(rec.ErrorMsg, "timeout") {
		t.Errorf("ErrorMsg 应含 timeout, got %q", rec.ErrorMsg)
	}
	if rec.Severity != "high" {
		t.Errorf("Severity want high (helm_manage), got %q", rec.Severity)
	}
	if rec.Target != "bcs-1" {
		t.Errorf("Target 应取 cluster=bcs-1, got %q", rec.Target)
	}
}

// TestAuditHook_PrefixMatch 前缀匹配（devops_pipeline_rerun）生效。
func TestAuditHook_PrefixMatch(t *testing.T) {
	mem, reset := withMemSink(t)
	defer reset()
	t.Setenv("AUDIT_DISABLE", "")

	h := NewAuditHook(AuditHookConfig{AgentName: "repair_agent"})
	_, _ = h.after(context.Background(),
		newAfter("devops_pipeline_rerun",
			`{"pipeline_id":"P-1"}`, nil))

	if got := len(mem.Snapshot()); got != 1 {
		t.Fatalf("前缀匹配未命中，实际写 %d 条", got)
	}
}
