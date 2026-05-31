
package gongfengtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/gongfengapi"
)

// newMockClient 返回一个 Token 为空（Mock 模式）的客户端。
func newMockClient() *gongfengapi.Client {
	// 直接清掉可能已存在的环境变量再构造
	return &gongfengapi.Client{Mock: true}
}

// callTool 用反射式断言走标准 CallableTool 路径。
func callTool(t *testing.T, tl tool.Tool, argsJSON string) *Result {
	t.Helper()
	t.Setenv("HITL_DISABLE", "") // 确保 HITL 启用
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

func TestMRCreate_TwoPhaseConfirm(t *testing.T) {
	tl := newMRCreateTool(newMockClient())

	// 第一阶段：未 confirmed，必须只返回 Plan，不调用 API
	r := callTool(t, tl, `{"project_id":"video/game-core","source_branch":"fix/oom","target_branch":"master","title":"fix: OOM in cache"}`)
	if r.OK {
		t.Fatalf("第一阶段 OK 必须为 false（awaiting_confirmation）")
	}
	// 把 Data 序列化回去确认结构
	raw, _ := json.Marshal(r.Data)
	if !strings.Contains(string(raw), "awaiting_confirmation") {
		t.Fatalf("第一阶段 Data 应包含 awaiting_confirmation 状态，实际：%s", raw)
	}
	if !strings.Contains(string(raw), "gongfeng.mr.create") {
		t.Fatalf("第一阶段 Data 应包含 action=gongfeng.mr.create")
	}

	// 第二阶段：confirmed=true，应真正走到 Mock 执行分支
	r = callTool(t, tl, `{"project_id":"video/game-core","source_branch":"fix/oom","target_branch":"master","title":"fix: OOM in cache","confirmed":true}`)
	if !r.OK {
		t.Fatalf("第二阶段应成功执行，实际 message=%s", r.Message)
	}
	if !r.Mock {
		t.Fatalf("Token 未配置时应走 Mock 模式")
	}
	raw, _ = json.Marshal(r.Data)
	if !strings.Contains(string(raw), "mr_iid") {
		t.Fatalf("第二阶段 Data 必须包含 mr_iid。实际：%s", raw)
	}
}

func TestMRMerge_RequiresReason(t *testing.T) {
	tl := newMRMergeTool(newMockClient())

	// 缺 reason，入参层面就报错
	ct := tl.(tool.CallableTool)
	_, err := ct.Call(context.Background(), []byte(`{"project_id":"x","mr_iid":42}`))
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("缺少 reason 应报错，实际 err=%v", err)
	}

	// 有 reason 但未 confirmed
	r := callTool(t, tl, `{"project_id":"x","mr_iid":42,"reason":"fix p0"}`)
	if r.OK {
		t.Fatalf("未 confirmed 必须拦截")
	}

	// 带 confirmed=true
	r = callTool(t, tl, `{"project_id":"x","mr_iid":42,"reason":"fix p0","confirmed":true}`)
	if !r.OK {
		t.Fatalf("confirmed=true 应执行（mock）")
	}
}

func TestHITL_Bypass_WithEnv(t *testing.T) {
	// HITL_DISABLE=1 时即便未 confirmed 也应直接执行（仅测试环境使用）
	t.Setenv("HITL_DISABLE", "1")
	tl := newMRCreateTool(newMockClient())
	ct := tl.(tool.CallableTool)
	raw, err := ct.Call(context.Background(), []byte(`{"project_id":"x","source_branch":"a","target_branch":"b","title":"t"}`))
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	r := raw.(*Result)
	if !r.OK {
		t.Fatalf("HITL_DISABLE=1 时应直接执行，message=%s", r.Message)
	}
}
