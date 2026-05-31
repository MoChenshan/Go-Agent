// pod_describe_test.go —— bcs_pod_describe 单元测试（D21.1）。
//
// # 覆盖矩阵
//
//   A) 输入校验
//     1. 缺 cluster_id/namespace → 报错
//     2. 无 pod 也无 pods → 报错
//
//   B) Mock 路径
//     3. 单 pod 默认入参 → 返完整结构化报告（Summary/Containers/Conditions/Events）
//     4. 多 pod 批量 → pod_count 正确
//     5. Pods 与 Pod 同填 → Pods 优先
//
//   C) resolveWithEvents 启发式
//     6. 显式 true 强制开启（即使 10 个 pod）
//     7. 单 pod 隐式默认 → 开启
//     8. 批量 >3 隐式 → 关闭
//     9. 批量 =3 边界 → 开启
//
//   D) humanAge 格式化
//     10. 空串 → ""
//     11. 解析失败 → 原样返回
//     12. <分钟 → "Xs"
//     13. <小时 → "XmYs"
//     14. <天 → "XhYm"
//     15. >=天 → "XdYh"
//
//   E) parseContainerState 三态
//     16. waiting 态映射
//     17. running 态映射
//     18. terminated 态 + ExitCode
//     19. 空 state → unknown
//
//   F) extractEvents 聚合
//     20. items 缺失 → 空切片
//     21. involvedObject 不匹配被过滤
//     22. 按 lastTime 倒序
//     23. 超 MaxEventsPerPod 被截断
//
//   G) fillPodFromRaw 集成
//     24. 完整 Pod JSON 正确映射 Summary/Containers/Conditions
//     25. data 包一层兼容
package bcstools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ---- 辅助 ----------------------------------------------------------------

func callPodDescribe(t *testing.T, tl tool.Tool, in PodDescribeInput) (*Result, error) {
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

func mustCallPodDescribe(t *testing.T, tl tool.Tool, in PodDescribeInput) *Result {
	t.Helper()
	r, err := callPodDescribe(t, tl, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return r
}

func newMockPodDescribeTool() tool.Tool {
	return newPodDescribeTool(bcsapi.NewClient(bcsapi.WithMockMode(true)))
}

// ---- A) 输入校验 ---------------------------------------------------------

func TestPodDescribe_MissingClusterOrNamespace(t *testing.T) {
	tl := newMockPodDescribeTool()
	if _, err := callPodDescribe(t, tl, PodDescribeInput{Namespace: "ns", Pod: "p"}); err == nil {
		t.Error("缺 cluster_id 应报错")
	}
	if _, err := callPodDescribe(t, tl, PodDescribeInput{ClusterID: "c", Pod: "p"}); err == nil {
		t.Error("缺 namespace 应报错")
	}
}

func TestPodDescribe_NoPodProvided(t *testing.T) {
	tl := newMockPodDescribeTool()
	_, err := callPodDescribe(t, tl, PodDescribeInput{ClusterID: "c", Namespace: "ns"})
	if err == nil {
		t.Fatal("既无 pod 又无 pods 应报错")
	}
}

// ---- B) Mock 路径 --------------------------------------------------------

func TestPodDescribe_SinglePodMockFull(t *testing.T) {
	tl := newMockPodDescribeTool()
	r := mustCallPodDescribe(t, tl, PodDescribeInput{
		ClusterID: "c", Namespace: "ns", Pod: "p-1",
	})
	if !r.OK {
		t.Fatalf("Mock 单 pod 应 OK，msg=%s", r.Message)
	}
	data := r.Data.(map[string]any)
	reports, _ := data["reports"].([]PodDescribeReport)
	if len(reports) != 1 {
		t.Fatalf("应返 1 个 report，实际 %d", len(reports))
	}
	rpt := reports[0]
	if rpt.Summary.Phase == "" {
		t.Error("Mock Summary.Phase 不应为空")
	}
	if len(rpt.Containers) == 0 {
		t.Error("Mock 应至少有 1 个 container")
	}
	if len(rpt.Conditions) == 0 {
		t.Error("Mock 应有 Conditions")
	}
	if len(rpt.Events) == 0 {
		t.Error("Mock withEvents 默认 true，应有 Events")
	}
}

func TestPodDescribe_MultiPodBatch(t *testing.T) {
	tl := newMockPodDescribeTool()
	r := mustCallPodDescribe(t, tl, PodDescribeInput{
		ClusterID: "c", Namespace: "ns",
		Pods: []string{"p-1", "p-2", "p-3"},
	})
	data := r.Data.(map[string]any)
	if data["pod_count"] != 3 {
		t.Errorf("pod_count 应为 3，实际 %v", data["pod_count"])
	}
}

func TestPodDescribe_PodsPreferredOverPod(t *testing.T) {
	tl := newMockPodDescribeTool()
	r := mustCallPodDescribe(t, tl, PodDescribeInput{
		ClusterID: "c", Namespace: "ns",
		Pod:  "should-be-ignored",
		Pods: []string{"p-a"},
	})
	data := r.Data.(map[string]any)
	reports, _ := data["reports"].([]PodDescribeReport)
	if len(reports) != 1 {
		t.Fatalf("Pods 应优先，实际 report 数=%d", len(reports))
	}
	if reports[0].Pod == "should-be-ignored" {
		t.Error("Pods 应覆盖 Pod 字段")
	}
}

// ---- C) resolveWithEvents 启发式 ----------------------------------------

func TestResolveWithEvents_ExplicitTrueAlwaysOn(t *testing.T) {
	if !resolveWithEvents(true, 100) {
		t.Error("显式 true 即使 100 个 pod 也应返 true")
	}
}

func TestResolveWithEvents_SinglePodDefault(t *testing.T) {
	if !resolveWithEvents(false, 1) {
		t.Error("单 pod 隐式应开启")
	}
}

func TestResolveWithEvents_BatchGreaterThan3Off(t *testing.T) {
	if resolveWithEvents(false, 5) {
		t.Error("批量>3 隐式应关闭")
	}
}

func TestResolveWithEvents_BoundaryEqual3(t *testing.T) {
	if !resolveWithEvents(false, 3) {
		t.Error("pod=3 边界应开启（<=3 规则）")
	}
}

// ---- D) humanAge 格式化 -------------------------------------------------

func TestHumanAge_Empty(t *testing.T) {
	if got := humanAge(""); got != "" {
		t.Errorf("空串应返 ''，实际 %q", got)
	}
}

func TestHumanAge_ParseFailure(t *testing.T) {
	input := "not-a-timestamp"
	if got := humanAge(input); got != input {
		t.Errorf("解析失败应原样返，实际 %q", got)
	}
}

func TestHumanAge_Seconds(t *testing.T) {
	ts := time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)
	got := humanAge(ts)
	if !strings.HasSuffix(got, "s") || strings.Contains(got, "m") {
		t.Errorf("30 秒前应只有秒位，实际 %q", got)
	}
}

func TestHumanAge_Minutes(t *testing.T) {
	ts := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	got := humanAge(ts)
	if !strings.Contains(got, "m") || strings.Contains(got, "h") {
		t.Errorf("5 分钟前应含 m 不含 h，实际 %q", got)
	}
}

func TestHumanAge_Hours(t *testing.T) {
	ts := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	got := humanAge(ts)
	if !strings.Contains(got, "h") || strings.Contains(got, "d") {
		t.Errorf("3 小时前应含 h 不含 d，实际 %q", got)
	}
}

func TestHumanAge_Days(t *testing.T) {
	ts := time.Now().Add(-72 * time.Hour).UTC().Format(time.RFC3339)
	got := humanAge(ts)
	if !strings.Contains(got, "d") {
		t.Errorf("72 小时前应含 d，实际 %q", got)
	}
}

// ---- E) parseContainerState 三态 ----------------------------------------

func TestParseContainerState_Waiting(t *testing.T) {
	st := parseContainerState(map[string]any{
		"waiting": map[string]any{
			"reason":  "ImagePullBackOff",
			"message": "Back-off pulling image xxx",
		},
	})
	if st.Type != "waiting" {
		t.Errorf("Type 应为 waiting，实际 %q", st.Type)
	}
	if st.Reason != "ImagePullBackOff" {
		t.Errorf("Reason 应为 ImagePullBackOff，实际 %q", st.Reason)
	}
}

func TestParseContainerState_Running(t *testing.T) {
	st := parseContainerState(map[string]any{
		"running": map[string]any{"startedAt": "2026-04-23T10:00:00Z"},
	})
	if st.Type != "running" || st.StartedAt == "" {
		t.Errorf("running 映射失败，实际 %+v", st)
	}
}

func TestParseContainerState_Terminated(t *testing.T) {
	st := parseContainerState(map[string]any{
		"terminated": map[string]any{
			"reason":     "OOMKilled",
			"exitCode":   float64(137), // JSON 数字会被反序列化为 float64
			"finishedAt": "2026-04-23T10:00:00Z",
		},
	})
	if st.Type != "terminated" || st.ExitCode != 137 || st.Reason != "OOMKilled" {
		t.Errorf("terminated 映射失败，实际 %+v", st)
	}
}

func TestParseContainerState_EmptyUnknown(t *testing.T) {
	st := parseContainerState(nil)
	if st.Type != "unknown" {
		t.Errorf("空 state 应为 unknown，实际 %q", st.Type)
	}
	st2 := parseContainerState(map[string]any{})
	if st2.Type != "unknown" {
		t.Errorf("空 map 应为 unknown，实际 %q", st2.Type)
	}
}

// ---- F) extractEvents 聚合 ----------------------------------------------

func TestExtractEvents_NoItems(t *testing.T) {
	ev := extractEvents(map[string]any{}, "p", "ns")
	if len(ev) != 0 {
		t.Errorf("items 缺失应返空切片，实际 %d", len(ev))
	}
}

func TestExtractEvents_MismatchFiltered(t *testing.T) {
	raw := map[string]any{
		"items": []any{
			map[string]any{
				"type":           "Normal",
				"reason":         "Scheduled",
				"message":        "ok",
				"involvedObject": map[string]any{"name": "other-pod", "namespace": "ns"},
			},
			map[string]any{
				"type":           "Warning",
				"reason":         "BackOff",
				"message":        "crash",
				"involvedObject": map[string]any{"name": "my-pod", "namespace": "ns"},
			},
		},
	}
	ev := extractEvents(raw, "my-pod", "ns")
	if len(ev) != 1 || ev[0].Reason != "BackOff" {
		t.Errorf("过滤失败，实际 %+v", ev)
	}
}

func TestExtractEvents_SortedByLastTimeDesc(t *testing.T) {
	raw := map[string]any{
		"items": []any{
			map[string]any{
				"type":           "Normal",
				"reason":         "A",
				"lastTimestamp":  "2026-04-23T10:00:00Z",
				"involvedObject": map[string]any{"name": "p", "namespace": "ns"},
			},
			map[string]any{
				"type":           "Warning",
				"reason":         "B",
				"lastTimestamp":  "2026-04-23T12:00:00Z",
				"involvedObject": map[string]any{"name": "p", "namespace": "ns"},
			},
			map[string]any{
				"type":           "Normal",
				"reason":         "C",
				"lastTimestamp":  "2026-04-23T11:00:00Z",
				"involvedObject": map[string]any{"name": "p", "namespace": "ns"},
			},
		},
	}
	ev := extractEvents(raw, "p", "ns")
	if len(ev) != 3 {
		t.Fatalf("应有 3 条，实际 %d", len(ev))
	}
	// 最新在前：B > C > A
	if ev[0].Reason != "B" || ev[1].Reason != "C" || ev[2].Reason != "A" {
		t.Errorf("排序错误，实际 %q %q %q", ev[0].Reason, ev[1].Reason, ev[2].Reason)
	}
}

func TestExtractEvents_Truncated(t *testing.T) {
	// 生成 MaxEventsPerPod+10 条
	items := make([]any, 0, MaxEventsPerPod+10)
	for i := 0; i < MaxEventsPerPod+10; i++ {
		items = append(items, map[string]any{
			"type":           "Normal",
			"reason":         fmt.Sprintf("R%d", i),
			"lastTimestamp":  fmt.Sprintf("2026-04-23T10:%02d:00Z", i%60),
			"involvedObject": map[string]any{"name": "p", "namespace": "ns"},
		})
	}
	ev := extractEvents(map[string]any{"items": items}, "p", "ns")
	if len(ev) != MaxEventsPerPod {
		t.Errorf("应被截断到 %d，实际 %d", MaxEventsPerPod, len(ev))
	}
}

// ---- G) fillPodFromRaw 集成 --------------------------------------------

func TestFillPodFromRaw_Full(t *testing.T) {
	// 构造一个典型的 K8s Pod 对象
	raw := map[string]any{
		"metadata": map[string]any{
			"creationTimestamp": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		},
		"spec": map[string]any{
			"nodeName": "node-42",
		},
		"status": map[string]any{
			"phase":     "Running",
			"podIP":     "10.1.2.3",
			"hostIP":    "10.0.0.5",
			"qosClass":  "Burstable",
			"startTime": "2026-04-23T14:00:00Z",
			"containerStatuses": []any{
				map[string]any{
					"name":         "app",
					"image":        "registry/x:v1",
					"ready":        true,
					"restartCount": float64(3),
					"state":        map[string]any{"running": map[string]any{"startedAt": "x"}},
					"lastState":    map[string]any{"terminated": map[string]any{"reason": "OOMKilled", "exitCode": float64(137)}},
				},
				map[string]any{
					"name":         "sidecar",
					"image":        "registry/proxy:v1",
					"ready":        false,
					"restartCount": float64(1),
					"state":        map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
				},
			},
			"conditions": []any{
				map[string]any{"type": "PodScheduled", "status": "True"},
				map[string]any{"type": "Ready", "status": "False", "reason": "ContainersNotReady"},
			},
		},
	}
	var rpt PodDescribeReport
	fillPodFromRaw(&rpt, raw)
	if rpt.Node != "node-42" {
		t.Errorf("Node 应为 node-42，实际 %q", rpt.Node)
	}
	if rpt.Summary.Phase != "Running" {
		t.Errorf("Phase 应为 Running，实际 %q", rpt.Summary.Phase)
	}
	if rpt.Summary.Ready != "1/2" {
		t.Errorf("Ready 应为 1/2，实际 %q", rpt.Summary.Ready)
	}
	if rpt.Summary.RestartCountSum != 4 {
		t.Errorf("RestartCountSum 应为 4（3+1），实际 %d", rpt.Summary.RestartCountSum)
	}
	if len(rpt.Containers) != 2 {
		t.Fatalf("应有 2 container，实际 %d", len(rpt.Containers))
	}
	// 验证 OOMKilled 从 lastState 被捕获
	appC := rpt.Containers[0]
	if appC.LastState.Reason != "OOMKilled" || appC.LastState.ExitCode != 137 {
		t.Errorf("lastState 捕获 OOMKilled 失败，实际 %+v", appC.LastState)
	}
	if len(rpt.Conditions) != 2 {
		t.Errorf("应有 2 conditions，实际 %d", len(rpt.Conditions))
	}
	if !strings.Contains(rpt.Summary.Age, "h") {
		t.Errorf("Age 应含 h（2h 前创建），实际 %q", rpt.Summary.Age)
	}
}

func TestFillPodFromRaw_DataWrapped(t *testing.T) {
	// BCS storage 常见的 {data: {...}} 包一层形态
	inner := map[string]any{
		"metadata": map[string]any{},
		"spec":     map[string]any{"nodeName": "node-xyz"},
		"status":   map[string]any{"phase": "Pending"},
	}
	raw := map[string]any{"data": inner}
	var rpt PodDescribeReport
	fillPodFromRaw(&rpt, raw)
	if rpt.Node != "node-xyz" || rpt.Summary.Phase != "Pending" {
		t.Errorf("data 包一层兼容失败，实际 Node=%q Phase=%q", rpt.Node, rpt.Summary.Phase)
	}
}
