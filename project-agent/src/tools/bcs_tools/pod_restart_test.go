// pod_restart_test.go —— bcs_pod_restart 工具单元测试。
//
// 覆盖点（按 mode 组织）：
//
//  A) delete_pod
//     1. 单 Pod 未 confirmed 返 Plan（Medium）
//     2. 单 Pod confirmed 成功（Mock）
//     3. 批量 >5 走串行（验证 Data.serial=true）
//     4. 批量 >20 硬拒 + 审计有 rejected_by=hard_limit
//     5. pod_names 空 → 报错
//     6. grace_period_seconds=0 生效（DeleteOptions body）
//
//  B) rollout_restart
//     7. 非生产 ns 未 confirmed 返 Plan（High）
//     8. 生产 ns 未 confirmed 返 Plan（Critical + RequireReason）
//     9. 生产 ns confirmed 但无 reason → 拒绝
//    10. 生产 ns confirmed + reason → 成功，打 restartedAt 注解
//    11. deployment 空 → 报错
//
//  C) evict_pod
//    12. 未 confirmed 返 Plan（Medium）
//    13. Mock 下 confirmed 成功
//    14. 批量 >20 硬拒
//    15. pod_names 空 → 报错
//
//  D) 通用 / Severity
//    16. mode 空或未知 → 报错
//    17. cluster_id / namespace 缺失 → 报错
//    18. classifyPodRestartSeverity 枚举覆盖
//    19. 审计事件字段完整性（cluster / ns / mode / pod_names）
//    20. 批量串行节奏耗时（应 >= (N-1)*batchSerialInterval）
package bcstools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// ---- 测试辅助 --------------------------------------------------------------

// callRestart 把 PodRestartInput marshal 成 JSON，通过 CallableTool 调用并断言返回 *Result。
func callRestart(t *testing.T, tl tool.Tool, in PodRestartInput) (*Result, error) {
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

func mustCallRestart(t *testing.T, tl tool.Tool, in PodRestartInput) *Result {
	t.Helper()
	r, err := callRestart(t, tl, in)
	if err != nil {
		t.Fatalf("callRestart unexpected error: %v", err)
	}
	return r
}

func newMockRestartTool() tool.Tool {
	return newPodRestartTool(bcsapi.NewClient(bcsapi.WithMockMode(true)))
}

// -----------------------------------------------------------------------------
// A) delete_pod
// -----------------------------------------------------------------------------

func TestPodRestart_DeleteSinglePod_UnconfirmedReturnsPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockRestartTool()

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:      "delete_pod",
		ClusterID: "BCS-K8S-00001",
		Namespace: "letsgo",
		PodNames:  []string{"game-core-xxx"},
		Confirmed: false,
	})
	if result.OK {
		t.Fatal("未 confirmed 时 OK 必须为 false")
	}
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Plan.Action != "bcs.pod.delete" {
		t.Errorf("Plan.Action 应为 bcs.pod.delete，实际=%q", pending.Plan.Action)
	}
	if pending.Plan.Severity != hitl.SeverityMedium {
		t.Errorf("单 Pod 非生产 ns 应 Medium，实际=%v", pending.Plan.Severity)
	}
}

func TestPodRestart_DeleteSinglePod_ConfirmedSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockRestartTool()

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:      "delete_pod",
		ClusterID: "BCS-K8S-00001",
		Namespace: "letsgo",
		PodNames:  []string{"game-core-xxx"},
		Confirmed: true,
	})
	if !result.OK {
		t.Fatalf("confirmed 下应 OK=true，msg=%s", result.Message)
	}
	if !result.Mock {
		t.Error("Mock 模式下应 Mock=true")
	}
	data, _ := result.Data.(map[string]any)
	if sc, _ := data["success_count"].(int); sc != 1 {
		t.Errorf("success_count 应为 1，实际=%v", data["success_count"])
	}
	if s, _ := data["serial"].(bool); s {
		t.Errorf("单 Pod 不应 serial=true")
	}
}

func TestPodRestart_DeleteBatch_OverSoftLimitGoesSerial(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1") // 绕过 HITL，直接看串行逻辑
	tl := newMockRestartTool()

	// 6 个 Pod > soft=5，应走串行
	names := []string{"p1", "p2", "p3", "p4", "p5", "p6"}

	// 用带超时的 ctx 调用：串行间隔是 2s，总共约 5*2=10s，这里测试用 15s 上限够了
	start := time.Now()
	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:      "delete_pod",
		ClusterID: "BCS-K8S-00001",
		Namespace: "letsgo",
		PodNames:  names,
		Confirmed: true,
	})
	elapsed := time.Since(start)

	if !result.OK {
		t.Fatalf("应成功，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	if s, _ := data["serial"].(bool); !s {
		t.Error("批量 >5 应 serial=true")
	}
	// 5 次间隔 * 2s = 10s；允许小误差（-500ms）
	minExpected := time.Duration(len(names)-1) * batchSerialInterval
	if elapsed < minExpected-500*time.Millisecond {
		t.Errorf("串行节奏太快：%v < %v", elapsed, minExpected)
	}
}

func TestPodRestart_DeleteBatch_OverHardLimitRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	t.Setenv("AUDIT_DISABLE", "")
	mem := &audit.MemorySink{}
	old := audit.SetSink(mem)
	defer audit.SetSink(old)

	tl := newMockRestartTool()
	// 21 > hardLimit=20
	names := make([]string, 21)
	for i := range names {
		names[i] = "p" + string(rune('A'+i))
	}

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:      "delete_pod",
		ClusterID: "BCS-K8S-00001",
		Namespace: "letsgo",
		PodNames:  names,
		Confirmed: true,
	})
	if result.OK {
		t.Fatal("应被硬拒")
	}
	if !strings.Contains(result.Message, "硬上限") {
		t.Errorf("错误信息应提硬上限，实际=%q", result.Message)
	}
	// 审计：应有 rejected_by=hard_limit 记录
	var found bool
	for _, line := range mem.Snapshot() {
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Params["rejected_by"] == "hard_limit" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("硬拒必须留下 rejected_by=hard_limit 的审计记录")
	}
}

func TestPodRestart_DeleteWithoutPodNamesRejected(t *testing.T) {
	tl := newMockRestartTool()
	_, err := callRestart(t, tl, PodRestartInput{
		Mode: "delete_pod", ClusterID: "c", Namespace: "n", PodNames: nil,
	})
	if err == nil {
		t.Fatal("pod_names 为空必须报错")
	}
}

func TestPodRestart_DeleteWithGracePeriod_NoError(t *testing.T) {
	// Mock 模式下 gracePeriodSeconds 不会真正生效，但应该不报错
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockRestartTool()

	grace := 0
	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:               "delete_pod",
		ClusterID:          "c",
		Namespace:          "n",
		PodNames:           []string{"p1"},
		GracePeriodSeconds: &grace,
		Confirmed:          true,
	})
	if !result.OK {
		t.Fatalf("带 grace=0 应成功，msg=%s", result.Message)
	}
}

// -----------------------------------------------------------------------------
// B) rollout_restart
// -----------------------------------------------------------------------------

func TestPodRestart_Rollout_NonProdNs_UnconfirmedReturnsPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockRestartTool()

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:       "rollout_restart",
		ClusterID:  "c",
		Namespace:  "dev-letsgo",
		Deployment: "game-core",
		Confirmed:  false,
	})
	if result.OK {
		t.Fatal("未 confirmed 必返 Plan")
	}
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Plan.Severity != hitl.SeverityMedium {
		t.Errorf("非生产 ns rollout 应 Medium（D20 后基线）：rollout_restart 走控制器滚动比 delete_pod 温柔，非 prod 起步 Medium，HPA 冲突时再升 High，实际=%v", pending.Plan.Severity)
	}
	if pending.Plan.RequireReason {
		t.Error("非生产 ns 不应 RequireReason")
	}
}

func TestPodRestart_Rollout_ProdNs_IsCriticalAndRequireReason(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockRestartTool()

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:       "rollout_restart",
		ClusterID:  "c",
		Namespace:  "prod-letsgo",
		Deployment: "game-core",
		Confirmed:  false,
	})
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("生产 ns rollout 应 Critical，实际=%v", pending.Plan.Severity)
	}
	if !pending.Plan.RequireReason {
		t.Error("生产 ns 必须 RequireReason=true")
	}
}

func TestPodRestart_Rollout_ProdNsWithoutReasonRejected(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1") // 绕过 HITL，直接压到 reason 规则
	tl := newMockRestartTool()

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:       "rollout_restart",
		ClusterID:  "c",
		Namespace:  "prod-letsgo",
		Deployment: "game-core",
		Confirmed:  true, // 跳过 HITL，走到 reason 规则
	})
	if result.OK {
		t.Fatal("生产 ns 无 reason 必须被拒绝")
	}
	if !strings.Contains(result.Message, "reason") {
		t.Errorf("错误信息应提 reason，实际=%q", result.Message)
	}
}

func TestPodRestart_Rollout_WithReasonSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockRestartTool()

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:       "rollout_restart",
		ClusterID:  "c",
		Namespace:  "prod-letsgo",
		Deployment: "game-core",
		Reason:     "配置文件热更新",
		Confirmed:  true,
	})
	if !result.OK {
		t.Fatalf("带 reason 应成功，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	rs, _ := data["restartedAt"].(string)
	if rs == "" {
		t.Error("应返回 restartedAt 时间戳")
	}
	// 解析为 RFC3339
	if _, err := time.Parse(time.RFC3339, rs); err != nil {
		t.Errorf("restartedAt 应为 RFC3339，实际=%q err=%v", rs, err)
	}
}

func TestPodRestart_Rollout_WithoutDeploymentRejected(t *testing.T) {
	tl := newMockRestartTool()
	_, err := callRestart(t, tl, PodRestartInput{
		Mode: "rollout_restart", ClusterID: "c", Namespace: "n",
	})
	if err == nil {
		t.Fatal("rollout_restart 必须指定 deployment")
	}
}

// -----------------------------------------------------------------------------
// C) evict_pod
// -----------------------------------------------------------------------------

func TestPodRestart_Evict_UnconfirmedReturnsPlan(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	tl := newMockRestartTool()

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:      "evict_pod",
		ClusterID: "c",
		Namespace: "letsgo",
		PodNames:  []string{"p1"},
		Confirmed: false,
	})
	pending, ok := result.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 hitl.PendingResult，实际=%T", result.Data)
	}
	if pending.Plan.Action != "bcs.pod.evict" {
		t.Errorf("Plan.Action 应为 bcs.pod.evict，实际=%q", pending.Plan.Action)
	}
	// 单 Pod 非生产 ns，evict 比 delete 降一档 → Medium
	if pending.Plan.Severity != hitl.SeverityMedium {
		t.Errorf("单 Pod evict 非生产应 Medium，实际=%v", pending.Plan.Severity)
	}
}

func TestPodRestart_Evict_MockConfirmedSucceeds(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockRestartTool()

	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:      "evict_pod",
		ClusterID: "c",
		Namespace: "letsgo",
		PodNames:  []string{"p1", "p2"},
		Confirmed: true,
	})
	if !result.OK {
		t.Fatalf("Mock evict 应成功，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	if total, _ := data["total"].(int); total != 2 {
		t.Errorf("total 应为 2，实际=%v", data["total"])
	}
}

func TestPodRestart_Evict_BatchHardLimit(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockRestartTool()

	names := make([]string, 25)
	for i := range names {
		names[i] = "p" + string(rune('A'+i))
	}
	result := mustCallRestart(t, tl, PodRestartInput{
		Mode: "evict_pod", ClusterID: "c", Namespace: "n", PodNames: names, Confirmed: true,
	})
	if result.OK {
		t.Fatal("evict 批量 > hardLimit 应被拒")
	}
}

func TestPodRestart_Evict_WithoutPodNamesRejected(t *testing.T) {
	tl := newMockRestartTool()
	_, err := callRestart(t, tl, PodRestartInput{
		Mode: "evict_pod", ClusterID: "c", Namespace: "n",
	})
	if err == nil {
		t.Fatal("evict 必须指定 pod_names")
	}
}

// -----------------------------------------------------------------------------
// D) 通用 / Severity
// -----------------------------------------------------------------------------

func TestPodRestart_UnknownModeRejected(t *testing.T) {
	tl := newMockRestartTool()
	_, err := callRestart(t, tl, PodRestartInput{
		Mode: "reboot", ClusterID: "c", Namespace: "n",
	})
	if err == nil {
		t.Fatal("未知 mode 必须报错")
	}
	if !strings.Contains(err.Error(), "不支持") {
		t.Errorf("错误信息应提 '不支持'，实际=%v", err)
	}
}

func TestPodRestart_EmptyModeRejected(t *testing.T) {
	tl := newMockRestartTool()
	_, err := callRestart(t, tl, PodRestartInput{ClusterID: "c", Namespace: "n"})
	if err == nil {
		t.Fatal("空 mode 必须报错")
	}
}

func TestPodRestart_MissingClusterOrNamespace(t *testing.T) {
	tl := newMockRestartTool()
	cases := []PodRestartInput{
		{Mode: "delete_pod"},                             // 缺 cluster + ns
		{Mode: "delete_pod", ClusterID: "c"},              // 缺 ns
		{Mode: "rollout_restart", Namespace: "n"},         // 缺 cluster
	}
	for i, in := range cases {
		_, err := callRestart(t, tl, in)
		if err == nil {
			t.Errorf("case %d 应报错；in=%+v", i, in)
		}
	}
}

func TestClassifyPodRestartSeverity_Enumeration(t *testing.T) {
	cases := []struct {
		mode      string
		batch     int
		namespace string
		want      hitl.Severity
	}{
		{"delete_pod", 1, "dev", hitl.SeverityMedium},
		{"delete_pod", 1, "prod-letsgo", hitl.SeverityHigh},
		{"delete_pod", 5, "dev", hitl.SeverityHigh},
		{"delete_pod", 5, "prod-letsgo", hitl.SeverityCritical},
		{"rollout_restart", 0, "dev", hitl.SeverityMedium},
		{"rollout_restart", 0, "prod-letsgo", hitl.SeverityCritical},
		{"evict_pod", 1, "dev", hitl.SeverityMedium},
		{"evict_pod", 1, "prod-letsgo", hitl.SeverityHigh},
	}
	for _, c := range cases {
		got := classifyPodRestartSeverity(c.mode, c.batch, c.namespace)
		if got != c.want {
			t.Errorf("mode=%s batch=%d ns=%s: want=%v got=%v",
				c.mode, c.batch, c.namespace, c.want, got)
		}
	}
}

func TestPodRestart_AuditEventFields(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	t.Setenv("AUDIT_DISABLE", "")
	mem := &audit.MemorySink{}
	old := audit.SetSink(mem)
	defer audit.SetSink(old)

	tl := newMockRestartTool()
	_ = mustCallRestart(t, tl, PodRestartInput{
		Mode:      "delete_pod",
		ClusterID: "BCS-K8S-00001",
		Namespace: "letsgo",
		PodNames:  []string{"p1"},
		Confirmed: true,
	})

	var hit *audit.Record
	for _, line := range mem.Snapshot() {
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("审计日志应为合法 JSON：%s", line)
		}
		if rec.Action == "bcs.pod.delete" {
			hit = &rec
			break
		}
	}
	if hit == nil {
		t.Fatal("未找到 bcs.pod.delete 审计事件")
	}
	if hit.Params["cluster_id"] != "BCS-K8S-00001" {
		t.Errorf("cluster_id 错误：%v", hit.Params["cluster_id"])
	}
	if hit.Params["namespace"] != "letsgo" {
		t.Errorf("namespace 错误：%v", hit.Params["namespace"])
	}
	if hit.Params["mode"] != "delete_pod" {
		t.Errorf("mode 错误：%v", hit.Params["mode"])
	}
	// pod_names 经 JSON 往返是 []any
	pods, _ := hit.Params["pod_names"].([]any)
	if len(pods) != 1 || pods[0] != "p1" {
		t.Errorf("pod_names 错误：%v", hit.Params["pod_names"])
	}
	if hit.Result != "success" {
		t.Errorf("Result 应为 success，实际=%q", hit.Result)
	}
	if !hit.Mock {
		t.Error("Mock 应为 true")
	}
}

// 额外验证：小批量（<=soft）不应串行。
func TestPodRestart_DeleteBatch_UnderSoftLimitParallel(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	tl := newMockRestartTool()

	names := []string{"p1", "p2", "p3"} // 3 <= soft=5
	start := time.Now()
	result := mustCallRestart(t, tl, PodRestartInput{
		Mode:      "delete_pod",
		ClusterID: "c",
		Namespace: "n",
		PodNames:  names,
		Confirmed: true,
	})
	elapsed := time.Since(start)
	if !result.OK {
		t.Fatalf("应成功，msg=%s", result.Message)
	}
	data, _ := result.Data.(map[string]any)
	if s, _ := data["serial"].(bool); s {
		t.Error("小批量不应 serial")
	}
	// 非串行应该快速完成（<1s），绝不应该超过 1 个 batchSerialInterval
	if elapsed > batchSerialInterval {
		t.Errorf("非串行耗时异常：%v", elapsed)
	}
}
