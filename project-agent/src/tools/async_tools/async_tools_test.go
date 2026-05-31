// async_tools_test.go —— 4 件套工具的端到端契约测试。
//
// 测试策略：
//   - 构造真实 Runner（Mem + funcExecutor），代表 async 框架
//   - 注册若干假"底层工具"（name + 闭包执行），表征被 async 化的业务工具
//   - 通过 Declaration().Callable 直接调 MCP 工具，验证 JSON 进 / Result 出
//
// 不测内部细节（那些在 async/*_test.go 里已充分覆盖）。
package asynctools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/async"
)

// mkTestEnv 构造一个典型的 async 测试环境：Runner + 注册表 + 2 个假工具。
//
// 假工具约定：
//   - "fake_quick"：立即 return {"ok": true}
//   - "fake_slow"：等 ctx 或 200ms 后 return
//   - "fake_fail"：return error "intended"
func mkTestEnv(t *testing.T) (*async.Runner, *async.ToolRegistry) {
	t.Helper()
	registry := async.NewToolRegistry()
	// 注册三个虚拟底层工具；async tools 会通过 registry + executor 间接调到它们
	// 工具内容在 executor 里模拟，不真的用 tool.Tool
	runner := async.New(async.Config{
		MaxConcurrentJobs: 4,
		MaxQueuedJobs:     8,
		DefaultTimeout:    time.Second,
		MaxTimeout:        5 * time.Second,
		JanitorInterval:   time.Hour, // 测试内不做清理
		JanitorRetention:  time.Hour,
	}, async.NewMemStore(), async.ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		switch name {
		case "fake_quick":
			return map[string]any{"ok": true, "echo": args}, nil
		case "fake_slow":
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
				return map[string]any{"ok": true, "slow": true}, nil
			}
		case "fake_fail":
			return nil, fakeErr("intended")
		default:
			return nil, fakeErr("unknown tool")
		}
	}))
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })

	// 注册三个名字；value 随意（job_submit 只查 Lookup 是否存在）
	registry.Register("fake_quick", "stub")
	registry.Register("fake_slow", "stub")
	registry.Register("fake_fail", "stub")
	return runner, registry
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

// callTool 便捷：拿 JSON payload 调一次 MCP 工具的 Callable，解析出 *Result。
//
// trpc-agent-go 的 FunctionTool.Call 直接返回 fn 的原值（这里是 *Result），
// 但既有测试断言假设 r.Data 是 map[string]any（即"通过 JSON 走过一遭后的形态"），
// 所以这里**统一序列化再反序列化**——这样不管框架返回 *Result/Result/[]byte 都
// 能拿到 Data 是 map 的稳定形态，与 LLM 真实拿到的 JSON 报文语义一致。
func callTool(t *testing.T, tl tool.Tool, payload string) *Result {
	t.Helper()
	callable, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("tool %s 不是 CallableTool", tl.Declaration().Name)
	}
	raw, err := callable.Call(context.Background(), []byte(payload))
	if err != nil {
		t.Fatalf("Call 失败：%v", err)
	}
	var b []byte
	switch v := raw.(type) {
	case []byte:
		b = v
	default:
		b, err = json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal 返回值 %T 失败：%v", v, err)
		}
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("解析 Result 失败：%v；raw=%s", err, string(b))
	}
	return &r
}

// ---- job_submit 单元 ----------------------------------------------------------

func TestJobSubmit_Basic(t *testing.T) {
	runner, registry := mkTestEnv(t)
	submit := newJobSubmitTool(runner, registry)

	r := callTool(t, submit, `{"tool_name":"fake_quick","args":{"k":"v"}}`)
	if !r.OK {
		t.Fatalf("submit 应 OK，msg=%s", r.Message)
	}
	// Data 里应有 job_id
	data, _ := r.Data.(map[string]any)
	if data == nil || data["job_id"] == "" {
		t.Fatalf("job_id 缺失：%+v", r.Data)
	}
}

func TestJobSubmit_UnknownTool(t *testing.T) {
	runner, registry := mkTestEnv(t)
	submit := newJobSubmitTool(runner, registry)
	callable := submit.(tool.CallableTool)
	_, err := callable.Call(context.Background(), []byte(`{"tool_name":"not_registered"}`))
	if err == nil {
		t.Error("未注册工具应返 err")
	}
}

func TestJobSubmit_Idempotency(t *testing.T) {
	runner, registry := mkTestEnv(t)
	submit := newJobSubmitTool(runner, registry)

	r1 := callTool(t, submit, `{"tool_name":"fake_quick","idempotency_key":"abc"}`)
	r2 := callTool(t, submit, `{"tool_name":"fake_quick","idempotency_key":"abc"}`)
	d1 := r1.Data.(map[string]any)
	d2 := r2.Data.(map[string]any)
	if d1["job_id"] != d2["job_id"] {
		t.Errorf("相同 idempotency_key 应返回同一 job_id；%s vs %s", d1["job_id"], d2["job_id"])
	}
}

// ---- job_status 单元 ----------------------------------------------------------

func TestJobStatus_Succeeded(t *testing.T) {
	runner, registry := mkTestEnv(t)
	submit := newJobSubmitTool(runner, registry)
	status := newJobStatusTool(runner)

	r := callTool(t, submit, `{"tool_name":"fake_quick"}`)
	jobID := r.Data.(map[string]any)["job_id"].(string)

	// 轮询等成功
	var st *Result
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st = callTool(t, status, `{"job_id":"`+jobID+`","include_result":true}`)
		data := st.Data.(map[string]any)
		if data["is_terminal"] == true {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	data := st.Data.(map[string]any)
	if data["status"] != "succeeded" {
		t.Errorf("status=%v，期望 succeeded", data["status"])
	}
	if data["result"] == nil {
		t.Error("include_result=true 应带结果")
	}
}

func TestJobStatus_NotFound(t *testing.T) {
	runner, _ := mkTestEnv(t)
	status := newJobStatusTool(runner)
	r := callTool(t, status, `{"job_id":"not_exist"}`)
	if r.OK {
		t.Error("不存在应 OK=false")
	}
	if !strings.Contains(r.Message, "不存在") {
		t.Errorf("msg 应含'不存在'，实际=%s", r.Message)
	}
}

// ---- job_cancel 单元 ----------------------------------------------------------

func TestJobCancel_ActiveJob(t *testing.T) {
	runner, registry := mkTestEnv(t)
	submit := newJobSubmitTool(runner, registry)
	cancel := newJobCancelTool(runner)

	r := callTool(t, submit, `{"tool_name":"fake_slow","timeout_seconds":5}`)
	jobID := r.Data.(map[string]any)["job_id"].(string)

	// 让它开始（fake_slow 会阻塞 200ms）
	time.Sleep(20 * time.Millisecond)
	rc := callTool(t, cancel, `{"job_id":"`+jobID+`","reason":"test"}`)
	if !rc.OK {
		t.Errorf("cancel 应 OK，msg=%s", rc.Message)
	}
}

func TestJobCancel_NotFound(t *testing.T) {
	runner, _ := mkTestEnv(t)
	cancel := newJobCancelTool(runner)
	r := callTool(t, cancel, `{"job_id":"not_exist"}`)
	if r.OK {
		t.Error("不存在应 OK=false")
	}
}

// ---- job_wait 单元 ------------------------------------------------------------

func TestJobWait_SucceededWithinBudget(t *testing.T) {
	runner, registry := mkTestEnv(t)
	submit := newJobSubmitTool(runner, registry)
	wait := newJobWaitTool(runner)

	r := callTool(t, submit, `{"tool_name":"fake_slow","timeout_seconds":5}`)
	jobID := r.Data.(map[string]any)["job_id"].(string)

	rw := callTool(t, wait, `{"job_id":"`+jobID+`","max_wait_seconds":2}`)
	if !rw.OK {
		t.Fatalf("wait 失败：%s", rw.Message)
	}
	data := rw.Data.(map[string]any)
	if data["is_terminal"] != true {
		t.Errorf("200ms 的 slow job 应在 2s 内进入终态；实际=%+v", data)
	}
}

func TestJobWait_TimeoutNotTerminal(t *testing.T) {
	// 这个测试要验证"wait 超时但 job 未完成"，需要一个不会在短时间内完成的 job。
	// 构造独立环境：executor 是 forever-block
	registry := async.NewToolRegistry()
	registry.Register("forever", "stub")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	runner := async.New(async.Config{
		MaxConcurrentJobs: 2,
		MaxQueuedJobs:     4,
		DefaultTimeout:    10 * time.Second,
		MaxTimeout:        30 * time.Second,
		JanitorInterval:   time.Hour,
	}, async.NewMemStore(), async.ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
		select {
		case <-block:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	submit := newJobSubmitTool(runner, registry)
	wait := newJobWaitTool(runner)

	r := callTool(t, submit, `{"tool_name":"forever","timeout_seconds":20}`)
	jobID := r.Data.(map[string]any)["job_id"].(string)

	// 最短等 1s，但通过传 max_wait_seconds=1
	start := time.Now()
	rw := callTool(t, wait, `{"job_id":"`+jobID+`","max_wait_seconds":1}`)
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("wait 应在 max_wait 后立即返回，实际 %s", elapsed)
	}
	if !rw.OK {
		t.Fatalf("wait 本身应 OK=true（未完成不算错误）")
	}
	data := rw.Data.(map[string]any)
	if data["is_terminal"] != false {
		t.Errorf("job 尚在运行，is_terminal 应为 false；实际=%+v", data)
	}
}

// ---- 装配入口 ----------------------------------------------------------------

func TestNewAllTargeted(t *testing.T) {
	runner, registry := mkTestEnv(t)
	tts := NewAllTargeted(runner, registry)
	if len(tts) != 4 {
		t.Fatalf("期望 4 个工具，实际 %d", len(tts))
	}
	names := map[string]bool{}
	for _, tt := range tts {
		if tt.Target != "*" {
			t.Errorf("target 应全为 *，实际 %s", tt.Target)
		}
		names[tt.Tool.Declaration().Name] = true
	}
	want := []string{"job_submit", "job_status", "job_cancel", "job_wait"}
	for _, w := range want {
		if !names[w] {
			t.Errorf("缺少工具 %s", w)
		}
	}
}

func TestNewAllTargeted_NilSafe(t *testing.T) {
	if got := NewAllTargeted(nil, nil); got != nil {
		t.Errorf("nil runner 应返回 nil，实际 %v", got)
	}
}

func TestRegisterToolsForAsync(t *testing.T) {
	registry := async.NewToolRegistry()
	tl := function.NewFunctionTool(
		func(ctx context.Context, _ struct{}) (string, error) { return "ok", nil },
		function.WithName("my_async_tool"),
		function.WithDescription("test"),
	)
	RegisterToolsForAsync(registry, []tool.Tool{tl, nil}) // 含一个 nil，不应崩
	if _, ok := registry.Lookup("my_async_tool"); !ok {
		t.Error("my_async_tool 应已注册")
	}
}
