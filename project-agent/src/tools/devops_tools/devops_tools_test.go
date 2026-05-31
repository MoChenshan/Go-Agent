package devopstools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/devopsapi"
)

func newMockClient() *devopsapi.Client {
	return &devopsapi.Client{Mock: true}
}

func callTool(t *testing.T, tl tool.Tool, argsJSON string) *Result {
	t.Helper()
	ct, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("tool is not CallableTool: %T", tl)
	}
	raw, err := ct.Call(context.Background(), []byte(argsJSON))
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	r, ok := raw.(*Result)
	if !ok {
		t.Fatalf("result type mismatch: %T", raw)
	}
	return r
}

// TestPipelineRerun_RequiresConfirm HITL 两段式 + Mock 兜底。
func TestPipelineRerun_RequiresConfirm(t *testing.T) {
	tl := newPipelineRerunTool(newMockClient())

	// 第一阶段：未 confirmed
	r := callTool(t, tl, `{"project_id":"p","pipeline_id":"pl","reason":"oom"}`)
	if r.OK {
		t.Fatalf("未 confirmed 时必须返回 pending")
	}
	raw, _ := json.Marshal(r.Data)
	if !strings.Contains(string(raw), "awaiting_confirmation") {
		t.Fatalf("第一阶段应返回 awaiting_confirmation，data=%s", raw)
	}

	// 第二阶段：confirmed=true
	r = callTool(t, tl, `{"project_id":"p","pipeline_id":"pl","reason":"oom","confirmed":true}`)
	if !r.OK {
		t.Fatalf("confirmed=true 应执行，message=%s", r.Message)
	}
	if !r.Mock {
		t.Fatalf("Token 未配置应走 Mock 模式")
	}
}

// TestPipelineRerun_RequiredFields 必填校验。
func TestPipelineRerun_RequiredFields(t *testing.T) {
	tl := newPipelineRerunTool(newMockClient())
	ct := tl.(tool.CallableTool)
	_, err := ct.Call(context.Background(), []byte(`{"project_id":"","pipeline_id":"","confirmed":true}`))
	if err == nil {
		t.Fatal("缺少必填字段应报错")
	}
}

// TestBuildCancel_RequireReason reason 必填。
func TestBuildCancel_RequireReason(t *testing.T) {
	tl := newBuildCancelTool(newMockClient())
	ct := tl.(tool.CallableTool)
	_, err := ct.Call(context.Background(),
		[]byte(`{"project_id":"p","pipeline_id":"pl","build_id":"b-1","confirmed":true}`))
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("缺 reason 应报错：%v", err)
	}
}

// TestBuildCancel_FullFlow HITL + Mock fallback。
func TestBuildCancel_FullFlow(t *testing.T) {
	tl := newBuildCancelTool(newMockClient())

	// 第一阶段
	r := callTool(t, tl, `{"project_id":"p","pipeline_id":"pl","build_id":"b-1","reason":"bad release"}`)
	if r.OK {
		t.Fatal("未 confirmed 时必须返回 pending")
	}

	// 第二阶段
	r = callTool(t, tl,
		`{"project_id":"p","pipeline_id":"pl","build_id":"b-1","reason":"bad release","confirmed":true}`)
	if !r.OK {
		t.Fatalf("confirmed=true 应执行，msg=%s", r.Message)
	}
	raw, _ := json.Marshal(r.Data)
	if !strings.Contains(string(raw), "CANCELLED") {
		t.Fatalf("Data 应含 CANCELLED，data=%s", raw)
	}
}
