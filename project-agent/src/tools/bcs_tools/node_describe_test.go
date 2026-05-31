// Package bcstools — bcs_node_describe 单元测试（D24）。
//
// 设计要点：
//   - 全部 Mock 模式离线跑，CI 零依赖
//   - 覆盖 8 个关键路径：
//     1. 单节点 describe 正常 Ready 节点
//     2. 单节点 describe DiskPressure 节点（issues 检出）
//     3. 单节点 describe NotReady+Unschedulable 节点（多 issues）
//     4. 批量 nodes[] 正常返回
//     5. scan_all 正常返回全部三节点
//     6. only_issues 过滤：只返回 2 个异常节点
//     7. cluster_id 缺失 → 报错
//     8. 三种入参形态都缺 → 报错
//     9. nodes[] 超硬上限 → 报错
package bcstools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// callNodeDescribe 辅助：构造 Mock client + 调用工具 + 解析 Result。
func callNodeDescribe(t *testing.T, payload string) map[string]any {
	t.Helper()
	t.Setenv("BCS_API_MOCK", "1")
	client := bcsapi.NewClient()
	if !client.IsMock() {
		t.Fatalf("Mock 环境变量未生效")
	}
	tl := newNodeDescribeTool(client)
	ct, _ := tl.(tool.CallableTool)
	if ct == nil {
		t.Fatalf("newNodeDescribeTool 应返回 CallableTool")
	}
	raw, err := ct.Call(context.Background(), []byte(payload))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	bs, _ := json.Marshal(raw)
	var r map[string]any
	if err := json.Unmarshal(bs, &r); err != nil {
		t.Fatalf("Result 反序列化失败: %v", err)
	}
	return r
}

// --- 1. 单节点 Ready ---

func TestNodeDescribe_SingleReadyNode(t *testing.T) {
	r := callNodeDescribe(t, `{"cluster_id":"BCS-K8S-00001","node":"node-mock-01"}`)
	if ok, _ := r["ok"].(bool); !ok {
		t.Fatalf("应 ok=true：%+v", r)
	}
	if mock, _ := r["mock"].(bool); !mock {
		t.Errorf("Mock 模式应 mock=true")
	}
	data := r["data"].(map[string]any)
	reports := data["reports"].([]any)
	if len(reports) != 1 {
		t.Fatalf("应返 1 个 report")
	}
	rpt := reports[0].(map[string]any)
	if rpt["name"] != "node-mock-01" {
		t.Errorf("name 字段错：%v", rpt["name"])
	}
	if issues, _ := rpt["issues"].([]any); len(issues) != 0 {
		t.Errorf("Ready 节点不应有 issues：%+v", issues)
	}
}

// --- 2. DiskPressure ---

func TestNodeDescribe_DiskPressureIssueDetected(t *testing.T) {
	r := callNodeDescribe(t, `{"cluster_id":"BCS-K8S-00001","node":"node-mock-02"}`)
	data := r["data"].(map[string]any)
	reports := data["reports"].([]any)
	rpt := reports[0].(map[string]any)
	issues, _ := rpt["issues"].([]any)
	if len(issues) != 1 {
		t.Fatalf("DiskPressure 节点应有 1 条 issue，实际 %d：%+v", len(issues), issues)
	}
	if !strings.Contains(issues[0].(string), "DiskPressure") {
		t.Errorf("issue 应提及 DiskPressure，实际 %q", issues[0])
	}
}

// --- 3. NotReady + Unschedulable ---

func TestNodeDescribe_NotReadyNodeMultipleIssues(t *testing.T) {
	r := callNodeDescribe(t, `{"cluster_id":"BCS-K8S-00001","node":"node-mock-03"}`)
	data := r["data"].(map[string]any)
	reports := data["reports"].([]any)
	rpt := reports[0].(map[string]any)
	issues, _ := rpt["issues"].([]any)
	if len(issues) < 2 {
		t.Fatalf("NotReady+Unschedulable 节点应 >=2 条 issues，实际 %d", len(issues))
	}
	joined := ""
	for _, s := range issues {
		joined += s.(string)
	}
	if !strings.Contains(joined, "Ready") || !strings.Contains(joined, "unschedulable") {
		t.Errorf("issues 应同时提及 Ready 和 unschedulable：%+v", issues)
	}
	summary := rpt["summary"].(map[string]any)
	if schedulable, _ := summary["schedulable"].(bool); schedulable {
		t.Errorf("node-mock-03 应为 schedulable=false")
	}
}

// --- 4. 批量 nodes[] ---

func TestNodeDescribe_BatchNodes(t *testing.T) {
	r := callNodeDescribe(t, `{
		"cluster_id":"BCS-K8S-00001",
		"nodes":["node-mock-01","node-mock-02","node-mock-03"]
	}`)
	data := r["data"].(map[string]any)
	reports := data["reports"].([]any)
	if len(reports) != 3 {
		t.Fatalf("批量应返 3 个 report，实际 %d", len(reports))
	}
	if cnt, _ := data["node_count"].(float64); int(cnt) != 3 {
		t.Errorf("node_count 应为 3")
	}
	if total, _ := data["issues_total"].(float64); int(total) != 3 {
		// mock-02 贡献 1 条，mock-03 贡献 2 条
		t.Errorf("issues_total 应为 3，实际 %v", data["issues_total"])
	}
}

// --- 5. scan_all ---

func TestNodeDescribe_ScanAll(t *testing.T) {
	r := callNodeDescribe(t, `{"cluster_id":"BCS-K8S-00001","scan_all":true}`)
	data := r["data"].(map[string]any)
	reports := data["reports"].([]any)
	if len(reports) != 3 {
		t.Fatalf("scan_all Mock 模式应返 3 个节点，实际 %d", len(reports))
	}
}

// --- 6. only_issues 过滤 ---

func TestNodeDescribe_OnlyIssuesFilter(t *testing.T) {
	r := callNodeDescribe(t, `{
		"cluster_id":"BCS-K8S-00001",
		"scan_all":true,
		"only_issues":true
	}`)
	data := r["data"].(map[string]any)
	reports := data["reports"].([]any)
	// mock-01 正常无 issues 应被过滤；mock-02/03 有 issues 应保留
	if len(reports) != 2 {
		t.Fatalf("only_issues 应保留 2 个异常节点（mock-02 DiskPressure + mock-03 NotReady），实际 %d", len(reports))
	}
	if fo, _ := data["filtered_out"].(float64); int(fo) != 1 {
		t.Errorf("filtered_out 应为 1（mock-01 被过滤），实际 %v", data["filtered_out"])
	}
	// issues_total 是未过滤前的聚合，仍为 3
	if total, _ := data["issues_total"].(float64); int(total) != 3 {
		t.Errorf("issues_total 不受 only_issues 影响，应为 3，实际 %v", data["issues_total"])
	}
}

// --- 7. cluster_id 缺失 ---

func TestNodeDescribe_MissingClusterID(t *testing.T) {
	t.Setenv("BCS_API_MOCK", "1")
	client := bcsapi.NewClient()
	tl := newNodeDescribeTool(client)
	ct, _ := tl.(tool.CallableTool)

	_, err := ct.Call(context.Background(), []byte(`{"node":"node-mock-01"}`))
	if err == nil {
		t.Fatal("cluster_id 缺失应报错")
	}
	if !strings.Contains(err.Error(), "cluster_id") {
		t.Errorf("错误应提及 cluster_id，实际 %q", err.Error())
	}
}

// --- 8. 三种入参形态都缺 ---

func TestNodeDescribe_NoTargetProvided(t *testing.T) {
	t.Setenv("BCS_API_MOCK", "1")
	client := bcsapi.NewClient()
	tl := newNodeDescribeTool(client)
	ct, _ := tl.(tool.CallableTool)

	_, err := ct.Call(context.Background(), []byte(`{"cluster_id":"BCS-K8S-00001"}`))
	if err == nil {
		t.Fatal("三种入参都缺应报错")
	}
	if !strings.Contains(err.Error(), "node") && !strings.Contains(err.Error(), "scan_all") {
		t.Errorf("错误应提及 node/nodes/scan_all，实际 %q", err.Error())
	}
}

// --- 9. nodes[] 超硬上限 ---

func TestNodeDescribe_NodesExceedLimit(t *testing.T) {
	t.Setenv("BCS_API_MOCK", "1")
	client := bcsapi.NewClient()
	tl := newNodeDescribeTool(client)
	ct, _ := tl.(tool.CallableTool)

	// 构造 21 个节点名超过 MaxNodesExplicit=20
	nodes := make([]string, 21)
	for i := 0; i < 21; i++ {
		nodes[i] = "node-mock-" + string(rune('a'+i))
	}
	payload, _ := json.Marshal(map[string]any{
		"cluster_id": "BCS-K8S-00001",
		"nodes":      nodes,
	})
	_, err := ct.Call(context.Background(), payload)
	if err == nil {
		t.Fatal("超过硬上限 20 应报错")
	}
	if !strings.Contains(err.Error(), "20") {
		t.Errorf("错误应提及硬上限 20，实际 %q", err.Error())
	}
}

// --- 10. extractNodeRoles 小单元 ---

func TestExtractNodeRoles(t *testing.T) {
	labels := map[string]any{
		"node-role.kubernetes.io/master":  "",
		"node-role.kubernetes.io/ingress": "",
		"kubernetes.io/hostname":          "node-01", // 不应被识别
		"node-role.kubernetes.io/":        "",        // 空 role 过滤掉
	}
	roles := extractNodeRoles(labels)
	if len(roles) != 2 {
		t.Fatalf("应提取 2 个 role（master/ingress），实际 %d: %v", len(roles), roles)
	}
	joined := strings.Join(roles, ",")
	if !strings.Contains(joined, "master") || !strings.Contains(joined, "ingress") {
		t.Errorf("应含 master 和 ingress，实际 %v", roles)
	}
}

// --- 11. Declaration 完整性 ---

func TestNodeDescribe_Declaration(t *testing.T) {
	client := bcsapi.NewClient()
	tl := newNodeDescribeTool(client)
	decl := tl.Declaration()
	if decl.Name != "bcs_node_describe" {
		t.Errorf("Name 应为 bcs_node_describe，实际 %q", decl.Name)
	}
	if !strings.Contains(decl.Description, "Conditions") {
		t.Errorf("Description 应包含 Conditions 说明")
	}
	if decl.InputSchema == nil {
		t.Errorf("InputSchema 不应为 nil")
	}
}
