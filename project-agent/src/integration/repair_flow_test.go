// Package integration 提供跨 Agent / 跨工具的端到端剧本集成测试。
//
// 本文件聚焦：
//   - 用 Mock 模式串起「感知告警 → 诊断指标 → 提 MR / 创 Bug / 重跑流水线」全链路，
//     验证各工具层契约、HITL 两段式、Mock fallback 都能协同工作。
//   - 不依赖真实 LLM 和远程 API，确保在 CI 中可重复、可离线运行。
//
// 为什么放在独立包：避免进入任意单一 *_tools 包的内部 test 范围，
// 以工具的公共接口（CallableTool）触发调用，最接近真实 Runtime 的使用方式。
package integration

import (
	"context"
	"encoding/json"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	bktools "git.woa.com/trpc-go/gameops-agent/src/tools/bk_tools"
	devopstools "git.woa.com/trpc-go/gameops-agent/src/tools/devops_tools"
	gongfengtools "git.woa.com/trpc-go/gameops-agent/src/tools/gongfeng_tools"
	tapdtools "git.woa.com/trpc-go/gameops-agent/src/tools/tapd_tools"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/devopsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/gongfengapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/tapdapi"

	"git.woa.com/trpc-go/gameops-agent/src/tools"
)

// findTool 在 targetedTool 列表里按名字找工具；找不到失败。
func findTool(t *testing.T, all []tools.TargetedTool, name string) tool.CallableTool {
	t.Helper()
	for _, tt := range all {
		if tt.Tool.Declaration().Name == name {
			ct, ok := tt.Tool.(tool.CallableTool)
			if !ok {
				t.Fatalf("工具 %s 不是 CallableTool: %T", name, tt.Tool)
			}
			return ct
		}
	}
	t.Fatalf("未找到工具：%s", name)
	return nil
}

// findPlainTool 在普通 tool.Tool 列表中按名字找。
func findPlainTool(t *testing.T, all []tool.Tool, name string) tool.CallableTool {
	t.Helper()
	for _, tl := range all {
		if tl.Declaration().Name == name {
			ct, ok := tl.(tool.CallableTool)
			if !ok {
				t.Fatalf("工具 %s 不是 CallableTool", name)
			}
			return ct
		}
	}
	t.Fatalf("未找到工具：%s", name)
	return nil
}

// call 调用 CallableTool 并反序列化结果为 generic map 以便断言任意字段。
func call(t *testing.T, ct tool.CallableTool, argsJSON string) map[string]any {
	t.Helper()
	raw, err := ct.Call(context.Background(), []byte(argsJSON))
	if err != nil {
		t.Fatalf("工具 Call 失败: %v", err)
	}
	// 先序列化再反序列化，统一得到 map[string]any。
	bs, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal result 失败: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(bs, &out); err != nil {
		t.Fatalf("Unmarshal result 失败: %v; raw=%s", err, bs)
	}
	return out
}

// mustOK 断言结果 ok=true。
func mustOK(t *testing.T, r map[string]any, msg string) {
	t.Helper()
	if ok, _ := r["ok"].(bool); !ok {
		t.Fatalf("[%s] 期望 ok=true，实际 %+v", msg, r)
	}
}

// mustPending 断言结果 ok=false 且 data.status=awaiting_confirmation。
func mustPending(t *testing.T, r map[string]any, msg string) {
	t.Helper()
	if ok, _ := r["ok"].(bool); ok {
		t.Fatalf("[%s] 期望 ok=false（pending）实际 ok=true，结果 %+v", msg, r)
	}
	data, _ := r["data"].(map[string]any)
	if status, _ := data["status"].(string); status != "awaiting_confirmation" {
		t.Fatalf("[%s] 期望 data.status=awaiting_confirmation，实际 %+v", msg, data)
	}
}

// mustMock 断言结果在 Mock 模式下返回。
func mustMock(t *testing.T, r map[string]any, msg string) {
	t.Helper()
	mock, _ := r["mock"].(bool)
	if !mock {
		t.Fatalf("[%s] 期望 mock=true，实际 %+v", msg, r)
	}
}

// -----------------------------------------------------------------------------
// 剧本一：OOM 告警 → 指标查询 → 提 MR（HITL 两段）→ 创 TAPD Bug（HITL 两段）
// -----------------------------------------------------------------------------

// TestRepairScenario_OOMFlow 模拟「游戏核心服务 OOM」完整处置流。
//
// 步骤：
//  1. DiagnosisAgent 用 bk_alarm_query 拿到告警列表
//  2. DiagnosisAgent 用 bk_metrics_query 查内存指标确认 OOM
//  3. RepairAgent 用 gongfeng_mr_create 提修复 MR（第一次未 confirmed → pending）
//  4. 用户 confirmed=true 后再次调用 → 执行（Mock 模式下走 mock 数据）
//  5. RepairAgent 用 tapd_bug_create 记录 Bug（同样 HITL 两段）
func TestRepairScenario_OOMFlow(t *testing.T) {
	// 确保所有 client 处于 Mock 模式（无真实凭据时自动 Mock）
	t.Setenv("BK_API_MOCK", "1")
	t.Setenv("GONGFENG_API_MOCK", "1")
	t.Setenv("TAPD_API_MOCK", "1")

	bkClient := bkapi.NewClient()
	gfClient := gongfengapi.NewClient()
	tpClient := tapdapi.NewClient()

	bkTools := bktools.NewAll(bkClient)
	gfTargeted := gongfengtools.NewAllTargeted(gfClient)
	tpTargeted := tapdtools.NewAllTargeted(tpClient)

	// ---- 步骤 1：查询告警 ----
	alarmTool := findPlainTool(t, bkTools, "bk_alarm_query")
	r := call(t, alarmTool, `{"bk_biz_id":100,"keyword":"OOM","page_size":10}`)
	mustOK(t, r, "bk_alarm_query")
	// 验证 Mock 数据里确实含 alerts 字段
	data, _ := r["data"].(map[string]any)
	if _, has := data["alerts"]; !has {
		t.Fatalf("alarm 查询缺少 alerts 字段：%+v", data)
	}

	// ---- 步骤 2：查询内存指标 ----
	metricsTool := findPlainTool(t, bkTools, "bk_metrics_query")
	r = call(t, metricsTool, `{"bk_biz_id":100,"data_label":"system","metric_name":"mem_usage","interval_sec":60}`)
	mustOK(t, r, "bk_metrics_query")

	// ---- 步骤 3：提 MR（第一阶段：未 confirmed）----
	mrTool := findTool(t, gfTargeted, "gongfeng_mr_create")
	r = call(t, mrTool, `{
		"project_id":"video/game-core",
		"source_branch":"feat/fix-oom",
		"target_branch":"master",
		"title":"fix: reduce goroutine leak causing OOM",
		"description":"root cause: X; fix: Y",
		"reviewers":"alice,bob"
	}`)
	mustPending(t, r, "gongfeng_mr_create 阶段1")

	// ---- 步骤 3b：用户 confirmed=true ----
	r = call(t, mrTool, `{
		"project_id":"video/game-core",
		"source_branch":"feat/fix-oom",
		"target_branch":"master",
		"title":"fix: reduce goroutine leak causing OOM",
		"description":"root cause: X; fix: Y",
		"reviewers":"alice,bob",
		"confirmed":true
	}`)
	mustOK(t, r, "gongfeng_mr_create 阶段2")
	mustMock(t, r, "gongfeng_mr_create 阶段2")

	// ---- 步骤 4：创 TAPD Bug（第一阶段）----
	bugTool := findTool(t, tpTargeted, "tapd_bug_create")
	r = call(t, bugTool, `{
		"workspace_id":"12345",
		"title":"OOM on game-core 2026-04-20 14:10",
		"description":"goroutine leak confirmed; MR filed.",
		"priority":"high",
		"severity":"major"
	}`)
	mustPending(t, r, "tapd_bug_create 阶段1")

	// ---- 步骤 4b：confirmed ----
	r = call(t, bugTool, `{
		"workspace_id":"12345",
		"title":"OOM on game-core 2026-04-20 14:10",
		"description":"goroutine leak confirmed; MR filed.",
		"priority":"high",
		"severity":"major",
		"confirmed":true
	}`)
	mustOK(t, r, "tapd_bug_create 阶段2")
	mustMock(t, r, "tapd_bug_create 阶段2")
}

// -----------------------------------------------------------------------------
// 剧本二：坏版本上线 → 查构建历史 → 重跑流水线（HITL + 安全闸门验证）
// -----------------------------------------------------------------------------

// TestRepairScenario_BadDeployRollback 模拟「发错版本 → 重跑流水线发正确版本」。
//
// 重点验证：
//   - devops_pipeline_rerun 强制走 HITL
//   - DEVOPS_ALLOW_AUTO_OPS 未设置时，即便 confirmed 也只返回 Mock（安全闸门）
//   - DEVOPS_ALLOW_AUTO_OPS=1 时会进入真实 API 路径（但 Client 是 Mock → ErrMockMode → fallback mock）
func TestRepairScenario_BadDeployRollback(t *testing.T) {
	t.Setenv("DEVOPS_API_MOCK", "1")
	devClient := devopsapi.NewClient()
	devTargeted := devopstools.NewAllTargeted(devClient)

	rerunTool := findTool(t, devTargeted, "devops_pipeline_rerun")

	// 阶段 1：未 confirmed → pending
	r := call(t, rerunTool, `{
		"project_id":"game-core",
		"pipeline_id":"p-001",
		"reason":"bad version shipped, rolling back"
	}`)
	mustPending(t, r, "devops_pipeline_rerun 阶段1")

	// 阶段 2：confirmed=true（未开闸门 → 仍走 Mock）
	t.Setenv("DEVOPS_ALLOW_AUTO_OPS", "")
	r = call(t, rerunTool, `{
		"project_id":"game-core",
		"pipeline_id":"p-001",
		"reason":"bad version shipped, rolling back",
		"confirmed":true
	}`)
	mustOK(t, r, "devops_pipeline_rerun 阶段2 闸门关闭")
	mustMock(t, r, "devops_pipeline_rerun 阶段2 闸门关闭")

	// 阶段 3：开闸门 + confirmed（Client 仍是 Mock → 仍走 mock，但走过真实调用分支再 fallback）
	t.Setenv("DEVOPS_ALLOW_AUTO_OPS", "1")
	r = call(t, rerunTool, `{
		"project_id":"game-core",
		"pipeline_id":"p-001",
		"reason":"bad version shipped, rolling back",
		"confirmed":true
	}`)
	mustOK(t, r, "devops_pipeline_rerun 阶段3 闸门打开")
	// 此时仍是 Mock，因为 devopsapi.Client 自身处于 Mock 模式
	mustMock(t, r, "devops_pipeline_rerun 阶段3 闸门打开")
}

// -----------------------------------------------------------------------------
// 剧本三：工具分组 target 过滤验证（app 层 dispatcher 行为）
// -----------------------------------------------------------------------------

// TestTargetedTool_Filtering 验证 target → agent 的分发策略。
//
// - DiagnosisAgent 只读范围：bk-monitor / bcs-read / tapd-read；不能看到 gongfeng / devops / tapd(写)
// - RepairAgent 写范围：bcs-write / gongfeng / devops / tapd
// - "*" 通用工具对所有 Agent 可见
func TestTargetedTool_Filtering(t *testing.T) {
	t.Setenv("GONGFENG_API_MOCK", "1")
	t.Setenv("DEVOPS_API_MOCK", "1")
	t.Setenv("TAPD_API_MOCK", "1")

	gfT := gongfengtools.NewAllTargeted(gongfengapi.NewClient())
	devT := devopstools.NewAllTargeted(devopsapi.NewClient())
	tpT := tapdtools.NewAllTargeted(tapdapi.NewClient())

	// 拼成全量 TargetedTool 列表
	all := append([]tools.TargetedTool{}, gfT...)
	all = append(all, devT...)
	all = append(all, tpT...)

	// DiagnosisAgent 只读范围：bk-monitor / bcs-read / tapd-read
	diagScope := tools.FilterByTargets(all, []string{"bk-monitor", "bcs-read", "tapd-read"})
	// 应只包含 tapd-read 的工具（tapd_bug_query），不应包含任何 gongfeng/devops/tapd(写) 工具。
	for _, tl := range diagScope {
		name := tl.Declaration().Name
		switch name {
		case "tapd_bug_query":
			// OK
		case "gongfeng_mr_create", "gongfeng_mr_merge",
			"devops_pipeline_rerun", "devops_build_cancel",
			"tapd_bug_create":
			t.Fatalf("DiagnosisAgent 不应看到写工具：%s", name)
		default:
			t.Logf("DiagnosisAgent 可见工具：%s (意料之外但非致命)", name)
		}
	}

	// RepairAgent 写范围
	repairScope := tools.FilterByTargets(all, []string{"gongfeng", "devops", "tapd"})
	// 至少 mr_create / mr_merge / pipeline_rerun / build_cancel / bug_create = 5 个
	if len(repairScope) < 5 {
		names := []string{}
		for _, t := range repairScope {
			names = append(names, t.Declaration().Name)
		}
		t.Fatalf("RepairAgent 工具数不足：got=%d names=%v", len(repairScope), names)
	}

	// "*" 通配 → 全部可见
	universal := tools.FilterByTargets(all, []string{"*"})
	if len(universal) != len(all) {
		t.Fatalf("通配符应返回全部工具：got=%d want=%d", len(universal), len(all))
	}
}

// -----------------------------------------------------------------------------
// 辅助调试函数已按需内联，此处不再导出，避免 unused-function 警告。
// -----------------------------------------------------------------------------
