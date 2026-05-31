// pod_logs_tail_test.go —— bcs_pod_logs_tail 单元测试（D21）。
//
// # 覆盖矩阵
//
//   A) 输入校验
//     1. 缺 cluster_id / namespace → 报错
//     2. 既无 pod 又无 pods → 报错
//     3. tail_lines 超过 MaxTailLines → 报错
//     4. since_seconds 为负 → 报错
//
//   B) Mock 路径
//     5. 单 pod 默认参数 → 返 1 个 entry，含样例内容
//     6. 多 pod + 多 container → 返 M*N 个 entry
//     7. Pods 与 Pod 同时填 → Pods 优先
//     8. tail_lines 参数在 Mock 日志里被 echo 出来（验证透传）
//
//   C) buildLogsQuery 纯函数
//     9. container 为空时不写入 query 字段
//     10. sinceSeconds=0 不写入
//     11. previous / timestamps bool 正确映射
//
//   D) 聚合字段
//     12. Result.Data.total_bytes / total_lines / pod_count 正确
package bcstools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ---- 辅助 ----------------------------------------------------------------

func callPodLogsTail(t *testing.T, tl tool.Tool, in PodLogsTailInput) (*Result, error) {
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

func mustCallPodLogsTail(t *testing.T, tl tool.Tool, in PodLogsTailInput) *Result {
	t.Helper()
	r, err := callPodLogsTail(t, tl, in)
	if err != nil {
		t.Fatalf("callPodLogsTail unexpected error: %v", err)
	}
	return r
}

func newMockPodLogsTailTool() tool.Tool {
	return newPodLogsTailTool(bcsapi.NewClient(bcsapi.WithMockMode(true)))
}

// ---- A) 输入校验 ---------------------------------------------------------

func TestPodLogsTail_MissingClusterOrNamespace(t *testing.T) {
	tl := newMockPodLogsTailTool()
	// 缺 cluster
	if _, err := callPodLogsTail(t, tl, PodLogsTailInput{Namespace: "ns", Pod: "p-1"}); err == nil {
		t.Error("缺 cluster_id 应报错")
	}
	// 缺 namespace
	if _, err := callPodLogsTail(t, tl, PodLogsTailInput{ClusterID: "c", Pod: "p-1"}); err == nil {
		t.Error("缺 namespace 应报错")
	}
}

func TestPodLogsTail_NoPodProvided(t *testing.T) {
	tl := newMockPodLogsTailTool()
	_, err := callPodLogsTail(t, tl, PodLogsTailInput{ClusterID: "c", Namespace: "ns"})
	if err == nil {
		t.Fatal("既无 pod 又无 pods 应报错")
	}
}

func TestPodLogsTail_TailLinesExceedsMax(t *testing.T) {
	tl := newMockPodLogsTailTool()
	_, err := callPodLogsTail(t, tl, PodLogsTailInput{
		ClusterID: "c", Namespace: "ns", Pod: "p-1",
		TailLines: MaxTailLines + 1,
	})
	if err == nil || !strings.Contains(err.Error(), "tail_lines") {
		t.Fatalf("tail_lines 超上限应报错，实际 err=%v", err)
	}
}

func TestPodLogsTail_NegativeSinceSeconds(t *testing.T) {
	tl := newMockPodLogsTailTool()
	_, err := callPodLogsTail(t, tl, PodLogsTailInput{
		ClusterID: "c", Namespace: "ns", Pod: "p-1",
		SinceSeconds: -1,
	})
	if err == nil {
		t.Fatal("负数 since_seconds 应报错")
	}
}

// ---- B) Mock 路径 --------------------------------------------------------

func TestPodLogsTail_SinglePodDefault(t *testing.T) {
	tl := newMockPodLogsTailTool()
	r := mustCallPodLogsTail(t, tl, PodLogsTailInput{
		ClusterID: "c", Namespace: "ns", Pod: "p-1",
	})
	if !r.OK {
		t.Fatalf("单 pod Mock 应成功，实际 msg=%s", r.Message)
	}
	data := r.Data.(map[string]any)
	if data["pod_count"] != 1 {
		t.Errorf("pod_count 应为 1，实际 %v", data["pod_count"])
	}
	entries, _ := data["entries"].([]PodLogEntry)
	if len(entries) != 1 {
		t.Fatalf("应返 1 个 entry，实际 %d", len(entries))
	}
	if entries[0].Pod != "p-1" {
		t.Errorf("entry.Pod 应为 p-1，实际 %q", entries[0].Pod)
	}
	if entries[0].Lines == 0 {
		t.Error("Mock 样例日志应有若干行")
	}
}

func TestPodLogsTail_MultiPodMultiContainer(t *testing.T) {
	tl := newMockPodLogsTailTool()
	r := mustCallPodLogsTail(t, tl, PodLogsTailInput{
		ClusterID:  "c",
		Namespace:  "ns",
		Pods:       []string{"p-1", "p-2"},
		Containers: []string{"app", "sidecar"},
	})
	data := r.Data.(map[string]any)
	entries, _ := data["entries"].([]PodLogEntry)
	if len(entries) != 4 { // 2 pod × 2 container
		t.Fatalf("应返 4 个 entry，实际 %d", len(entries))
	}
	// 验证组合完整
	combos := map[string]bool{}
	for _, e := range entries {
		combos[e.Pod+"/"+e.Container] = true
	}
	for _, want := range []string{"p-1/app", "p-1/sidecar", "p-2/app", "p-2/sidecar"} {
		if !combos[want] {
			t.Errorf("缺组合 %q", want)
		}
	}
}

func TestPodLogsTail_PodsPreferredOverPod(t *testing.T) {
	tl := newMockPodLogsTailTool()
	r := mustCallPodLogsTail(t, tl, PodLogsTailInput{
		ClusterID: "c", Namespace: "ns",
		Pod:  "should-be-ignored",
		Pods: []string{"p-a", "p-b"},
	})
	data := r.Data.(map[string]any)
	if data["pod_count"] != 2 {
		t.Errorf("Pods 应优先，pod_count 应为 2，实际 %v", data["pod_count"])
	}
	entries, _ := data["entries"].([]PodLogEntry)
	for _, e := range entries {
		if e.Pod == "should-be-ignored" {
			t.Errorf("不应含 pod=%q（Pods 应覆盖 Pod）", e.Pod)
		}
	}
}

func TestPodLogsTail_TailLinesEchoedInMock(t *testing.T) {
	tl := newMockPodLogsTailTool()
	r := mustCallPodLogsTail(t, tl, PodLogsTailInput{
		ClusterID: "c", Namespace: "ns", Pod: "p-1", TailLines: 77,
	})
	data := r.Data.(map[string]any)
	entries, _ := data["entries"].([]PodLogEntry)
	if !strings.Contains(entries[0].Content, "tail=77") {
		t.Errorf("Mock 日志应 echo tail_lines=77，实际内容=%q", entries[0].Content)
	}
}

// ---- C) buildLogsQuery 纯函数 -------------------------------------------

func TestBuildLogsQuery_EmptyContainer(t *testing.T) {
	q := buildLogsQuery("", 100, 0, false, false)
	if _, has := q["container"]; has {
		t.Error("container 为空不应写入 query")
	}
	if q["tailLines"] != "100" {
		t.Errorf("tailLines 应为 100，实际 %q", q["tailLines"])
	}
}

func TestBuildLogsQuery_ZeroSince(t *testing.T) {
	q := buildLogsQuery("app", 50, 0, false, false)
	if _, has := q["sinceSeconds"]; has {
		t.Error("sinceSeconds=0 不应写入 query")
	}
}

func TestBuildLogsQuery_BoolFlagsMapped(t *testing.T) {
	q := buildLogsQuery("app", 50, 300, true, true)
	if q["previous"] != "true" {
		t.Errorf("previous 应为 'true'，实际 %q", q["previous"])
	}
	if q["timestamps"] != "true" {
		t.Errorf("timestamps 应为 'true'，实际 %q", q["timestamps"])
	}
	if q["sinceSeconds"] != "300" {
		t.Errorf("sinceSeconds 应为 '300'，实际 %q", q["sinceSeconds"])
	}
	if q["container"] != "app" {
		t.Errorf("container 应为 'app'，实际 %q", q["container"])
	}
}

// ---- D) 聚合字段 --------------------------------------------------------

func TestPodLogsTail_AggregatedFields(t *testing.T) {
	tl := newMockPodLogsTailTool()
	r := mustCallPodLogsTail(t, tl, PodLogsTailInput{
		ClusterID: "c", Namespace: "ns",
		Pods: []string{"p-1", "p-2"},
	})
	data := r.Data.(map[string]any)
	totalBytes, _ := data["total_bytes"].(int)
	totalLines, _ := data["total_lines"].(int)
	entries, _ := data["entries"].([]PodLogEntry)

	sumB, sumL := 0, 0
	for _, e := range entries {
		sumB += e.Bytes
		sumL += e.Lines
	}
	if sumB != totalBytes {
		t.Errorf("total_bytes 聚合不一致：sum=%d vs reported=%d", sumB, totalBytes)
	}
	if sumL != totalLines {
		t.Errorf("total_lines 聚合不一致：sum=%d vs reported=%d", sumL, totalLines)
	}
}
