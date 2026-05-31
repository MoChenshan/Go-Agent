package tapdtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/tapdapi"
)

// newMockClient 返回一个处于 Mock 模式的 TAPD 客户端。
func newMockClient() *tapdapi.Client {
	return &tapdapi.Client{Mock: true, WorkspaceID: "12345"}
}

func callTool(t *testing.T, tl tool.Tool, argsJSON string) *Result {
	t.Helper()
	t.Setenv("HITL_DISABLE", "")
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

// TestBugQuery_MockFallback 验证 Mock 模式下仍能返回样例数据。
func TestBugQuery_MockFallback(t *testing.T) {
	tl := newBugQueryTool(newMockClient())
	r := callTool(t, tl, `{"keyword":"oom"}`)
	if !r.OK || !r.Mock {
		t.Fatalf("Mock 模式必须返回 OK + Mock 标记，实际 %+v", r)
	}
	raw, _ := json.Marshal(r.Data)
	if !strings.Contains(string(raw), "items") {
		t.Fatalf("Data 应包含 items 列表：%s", raw)
	}
}

// TestBugCreate_RequiresConfirm 验证 HITL 两段式。
func TestBugCreate_RequiresConfirm(t *testing.T) {
	tl := newBugCreateTool(newMockClient())

	// 第一阶段：未 confirmed
	r := callTool(t, tl, `{"title":"OOM bug"}`)
	if r.OK {
		t.Fatalf("未 confirmed 时必须拦截")
	}
	raw, _ := json.Marshal(r.Data)
	if !strings.Contains(string(raw), "awaiting_confirmation") {
		t.Fatalf("第一阶段 Data 应含 awaiting_confirmation：%s", raw)
	}

	// 第二阶段：confirmed=true
	r = callTool(t, tl, `{"title":"OOM bug","confirmed":true}`)
	if !r.OK {
		t.Fatalf("confirmed=true 应执行，message=%s", r.Message)
	}
	if !r.Mock {
		t.Fatalf("Token 未配置时应走 Mock 模式")
	}
}

// TestBugCreate_EmptyTitle 验证必填校验。
func TestBugCreate_EmptyTitle(t *testing.T) {
	tl := newBugCreateTool(newMockClient())
	ct := tl.(tool.CallableTool)
	_, err := ct.Call(context.Background(), []byte(`{"title":"","confirmed":true}`))
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("缺 title 应报错：%v", err)
	}
}
