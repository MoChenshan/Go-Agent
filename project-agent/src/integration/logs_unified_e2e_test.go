// Package integration —— D23' 双源日志聚合工具 `logs_unified_query` 端到端集成测试。
//
// # 这次做了啥
//
// D23' 新增的 compositetools 包是跨 bk / bcs 两个 infra 域的首个工具包，
// 它和 bk_tools / bcs_tools 平级，通过独立的 `NewAllTargeted(bkClient, bcsClient)`
// 入口挂到 bcs-read target 下。单元测试已覆盖过核心合并/排序/截断/失败隔离逻辑，
// 但"装配进系统之后还能被 DiagnosisAgent 正确选到吗、target 分组会不会泄漏到
// repair 链、真实 CallableTool 路径下的 Mock 契约稳不稳"——这些"组装正确性"
// 属于 E2E 层的责任，单测看不见。本文件补齐。
//
// # 场景矩阵（3 个）
//
//	Scenario A: 双源聚合 + 时间戳合并（最常见用法）
//	  同时填 K8s 侧（cluster_id+namespace+pod）与 bk-log 侧（bk_biz_id+index_set）
//	  → entries[] 应包含两个 source 的条目，按时间升序；stats[] 两个源都 ok=true
//
//	Scenario B: 单源退化
//	  B1 只填 K8s 侧 → bk-log 源自动跳过，stats[].bk_log.error 含 "skipped"
//	  B2 只填 bk-log 侧 → K8s 源自动跳过，stats[].k8s_stdout.error 含 "skipped"
//	  B3 两源全空 → 工具直接报错（防误用硬上限）
//
//	Scenario C: Target 隔离 + 装配完整性
//	  - 通过 compositetools.NewAllTargeted 装配进 bcs-read target，
//	    DiagnosisAgent (订阅 bcs-read) 应该能看到 logs_unified_query
//	  - RepairAgent (订阅 bcs-write) 绝对看不到它（纯读工具不应串到写链）
//	  - 与 bcs_tools 合并装配后，两组工具互相不冲突（Name 唯一性）
//
// # 为什么不跑真实 LLM 链
//
// 与 bcs_full_flow_test.go 一脉相承：E2E 层只验证"工具装配 + 调用契约"的
// 组装正确性，LLM 决策正确性属于 prompt 调优范畴，不在 CI 覆盖目标里。
package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	bcstools "git.woa.com/trpc-go/gameops-agent/src/tools/bcs_tools"
	compositetools "git.woa.com/trpc-go/gameops-agent/src/tools/composite_tools"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"

	"git.woa.com/trpc-go/gameops-agent/src/tools"
)

// newCompositeTargeted 构造 Mock bk+bcs client 并返回 compositetools 全部 targeted 工具。
//
// 与 newBCSTargeted 同构：Mock 模式优先保证 CI 可离线运行。
func newCompositeTargeted(t *testing.T) []tools.TargetedTool {
	t.Helper()
	t.Setenv("BCS_API_MOCK", "1")
	t.Setenv("BK_API_MOCK", "1")
	bkClient := bkapi.NewClient()
	bcsClient := bcsapi.NewClient()
	if !bkClient.IsMock() {
		t.Fatalf("期望 bkapi client 处于 Mock 模式")
	}
	if !bcsClient.IsMock() {
		t.Fatalf("期望 bcsapi client 处于 Mock 模式")
	}
	return compositetools.NewAllTargeted(bkClient, bcsClient)
}

// -----------------------------------------------------------------------------
// Scenario A: 双源聚合 + 时间戳合并
// -----------------------------------------------------------------------------

// TestLogsUnifiedE2E_BothSourcesMerged 双源聚合的核心路径：
//
//	同时提供 K8s 四元组（cluster_id+namespace+pod+container）
//	与 bk-log 三元组（bk_biz_id+index_set）
//	⇒ entries[] 应合并两源、按时间升序、每条 source 字段清晰可辨
//	⇒ stats[] 两个源各自 ok=true
//
// 这是 D23' 要解决的"最高频 oncall 场景"的端到端证伪。
func TestLogsUnifiedE2E_BothSourcesMerged(t *testing.T) {
	all := newCompositeTargeted(t)
	unified := findTool(t, all, "logs_unified_query")

	r := bcsCall(t, unified, `{
		"cluster_id":  "BCS-K8S-00001",
		"namespace":   "staging-letsgo",
		"pod":         "game-core-7d9c88fcb7-abcde",
		"container":   "app",
		"bk_biz_id":   100200,
		"index_set":   "2001",
		"bk_query":    "*",
		"tail_lines":  50,
		"since_seconds": 300
	}`)
	mustOK(t, r, "logs_unified_query dual-source")
	mustMock(t, r, "logs_unified_query dual-source")

	data := r["data"].(map[string]any)

	// ---- 顶层字段齐全 ----
	for _, key := range []string{"entries", "entry_count", "total_bytes", "truncated", "stats", "input_echo"} {
		if _, has := data[key]; !has {
			t.Fatalf("logs_unified_query 响应缺字段 %q：%+v", key, data)
		}
	}

	// ---- entries 应同时包含两个 source ----
	entries, _ := data["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("entries 应非空（两源 Mock 各出 2 条）：%+v", data)
	}
	haveK8s, haveBK := false, false
	var lastParsedTS string
	for _, e := range entries {
		m, _ := e.(map[string]any)
		src, _ := m["source"].(string)
		switch src {
		case "k8s_stdout":
			haveK8s = true
		case "bk_log":
			haveBK = true
		default:
			t.Fatalf("entry 的 source 字段非法：%q, entry=%+v", src, m)
		}
		// 时间升序：相邻 ts 单调不减（解析失败的条目 ts 为空，放尾部所以此处不会出现）
		if ts, ok := m["timestamp"].(string); ok && ts != "" {
			if lastParsedTS != "" && ts < lastParsedTS {
				t.Errorf("entries 应按 timestamp 升序，发现逆序：prev=%q cur=%q", lastParsedTS, ts)
			}
			lastParsedTS = ts
		}
	}
	if !haveK8s {
		t.Errorf("entries 应含至少一条 source=k8s_stdout：%+v", entries)
	}
	if !haveBK {
		t.Errorf("entries 应含至少一条 source=bk_log：%+v", entries)
	}

	// ---- stats 两源都 OK ----
	stats, _ := data["stats"].([]any)
	if len(stats) != 2 {
		t.Fatalf("stats 应固定两项（k8s_stdout + bk_log）：%+v", stats)
	}
	for _, s := range stats {
		m, _ := s.(map[string]any)
		ok, _ := m["ok"].(bool)
		src, _ := m["source"].(string)
		if !ok {
			t.Errorf("双源模式下 stats[%s].ok 应为 true：%+v", src, m)
		}
		// Mock 模式下两源都应标 mock
		if mock, _ := m["mock"].(bool); !mock {
			t.Errorf("Mock 模式下 stats[%s].mock 应为 true：%+v", src, m)
		}
	}

	// ---- entry_count 与 entries 长度一致 ----
	if ec, ok := data["entry_count"].(float64); !ok || int(ec) != len(entries) {
		t.Errorf("entry_count 应等于 len(entries)=%d，实际 %v", len(entries), data["entry_count"])
	}
}

// TestLogsUnifiedE2E_SortDesc 验证 sort_desc=true 改变合并序。
func TestLogsUnifiedE2E_SortDesc(t *testing.T) {
	all := newCompositeTargeted(t)
	unified := findTool(t, all, "logs_unified_query")

	r := bcsCall(t, unified, `{
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"pod":        "game-core-7d9c88fcb7-abcde",
		"bk_biz_id":  100200,
		"index_set":  "2001",
		"tail_lines": 50,
		"sort_desc":  true
	}`)
	mustOK(t, r, "logs_unified_query desc")
	data := r["data"].(map[string]any)
	entries, _ := data["entries"].([]any)
	if len(entries) < 2 {
		t.Fatalf("需要至少两条 entries 才能验证排序方向")
	}
	// 相邻 ts 单调不增
	var last string
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if ts, ok := m["timestamp"].(string); ok && ts != "" {
			if last != "" && ts > last {
				t.Errorf("sort_desc=true 时 entries 应按 timestamp 倒序，逆序位置：prev=%q cur=%q", last, ts)
			}
			last = ts
		}
	}
}

// -----------------------------------------------------------------------------
// Scenario B: 单源退化与入参校验
// -----------------------------------------------------------------------------

// TestLogsUnifiedE2E_K8sOnlyDegradation 只填 K8s 侧参数：bk-log 应自动跳过。
func TestLogsUnifiedE2E_K8sOnlyDegradation(t *testing.T) {
	all := newCompositeTargeted(t)
	unified := findTool(t, all, "logs_unified_query")

	r := bcsCall(t, unified, `{
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"pod":        "game-core-7d9c88fcb7-abcde",
		"tail_lines": 20
	}`)
	mustOK(t, r, "logs_unified_query k8s-only")

	data := r["data"].(map[string]any)
	stats, _ := data["stats"].([]any)

	var k8sStat, bkStat map[string]any
	for _, s := range stats {
		m, _ := s.(map[string]any)
		switch m["source"] {
		case "k8s_stdout":
			k8sStat = m
		case "bk_log":
			bkStat = m
		}
	}
	if k8sStat == nil || bkStat == nil {
		t.Fatalf("stats 应固定包含两项：%+v", stats)
	}
	if ok, _ := k8sStat["ok"].(bool); !ok {
		t.Errorf("只填 K8s 侧时 k8s_stdout.ok 应为 true：%+v", k8sStat)
	}
	// bk_log 被跳过：ok=false 且 error 含 "skipped"
	if ok, _ := bkStat["ok"].(bool); ok {
		t.Errorf("只填 K8s 侧时 bk_log.ok 应为 false（被跳过）：%+v", bkStat)
	}
	if errMsg, _ := bkStat["error"].(string); !strings.Contains(errMsg, "skipped") {
		t.Errorf("bk_log 被跳过时 error 应含 'skipped'，实际 %q", errMsg)
	}

	// entries 里不应出现 source=bk_log
	entries, _ := data["entries"].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m["source"] == "bk_log" {
			t.Errorf("只填 K8s 侧时 entries 不应含 bk_log 条目：%+v", m)
		}
	}
}

// TestLogsUnifiedE2E_BKOnlyDegradation 只填 bk-log 侧：K8s 应自动跳过。
func TestLogsUnifiedE2E_BKOnlyDegradation(t *testing.T) {
	all := newCompositeTargeted(t)
	unified := findTool(t, all, "logs_unified_query")

	r := bcsCall(t, unified, `{
		"bk_biz_id":  100200,
		"index_set":  "2001",
		"bk_query":   "level:ERROR",
		"tail_lines": 20
	}`)
	mustOK(t, r, "logs_unified_query bk-only")

	data := r["data"].(map[string]any)
	stats, _ := data["stats"].([]any)
	for _, s := range stats {
		m, _ := s.(map[string]any)
		switch m["source"] {
		case "k8s_stdout":
			if ok, _ := m["ok"].(bool); ok {
				t.Errorf("只填 bk-log 侧时 k8s_stdout.ok 应为 false（被跳过）：%+v", m)
			}
			if errMsg, _ := m["error"].(string); !strings.Contains(errMsg, "skipped") {
				t.Errorf("k8s_stdout 被跳过时 error 应含 'skipped'，实际 %q", errMsg)
			}
		case "bk_log":
			if ok, _ := m["ok"].(bool); !ok {
				t.Errorf("只填 bk-log 侧时 bk_log.ok 应为 true：%+v", m)
			}
		}
	}

	entries, _ := data["entries"].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m["source"] == "k8s_stdout" {
			t.Errorf("只填 bk-log 侧时 entries 不应含 k8s_stdout 条目：%+v", m)
		}
	}
}

// TestLogsUnifiedE2E_BothEmpty 两源参数全空：工具应显式报错，防误用。
func TestLogsUnifiedE2E_BothEmpty(t *testing.T) {
	all := newCompositeTargeted(t)
	unified := findTool(t, all, "logs_unified_query")

	// 这里不能走 bcsCall，因为工具会直接 return error 而不是 Result
	raw, err := unified.Call(t.Context(), []byte(`{"tail_lines":10}`))
	if err == nil {
		t.Fatalf("两源全空应报错；实际成功，raw=%+v", raw)
	}
	if !strings.Contains(err.Error(), "至少要提供一个源") {
		t.Errorf("错误消息应指明'至少要提供一个源'，实际 %q", err.Error())
	}
}

// TestLogsUnifiedE2E_TailLinesExceedCap 超过每源硬上限应报错。
func TestLogsUnifiedE2E_TailLinesExceedCap(t *testing.T) {
	all := newCompositeTargeted(t)
	unified := findTool(t, all, "logs_unified_query")

	_, err := unified.Call(t.Context(), []byte(`{
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "staging-letsgo",
		"pod":        "game-core",
		"tail_lines": 99999
	}`))
	if err == nil {
		t.Fatalf("tail_lines=99999 应超过硬上限报错")
	}
	if !strings.Contains(err.Error(), "硬上限") && !strings.Contains(err.Error(), "tail_lines") {
		t.Errorf("错误消息应指明 tail_lines 硬上限，实际 %q", err.Error())
	}
}

// -----------------------------------------------------------------------------
// Scenario C: Target 隔离 + 装配完整性
// -----------------------------------------------------------------------------

// TestLogsUnifiedE2E_TargetVisibility 装配完整性 & target 边界验证：
//
//	- compositetools.NewAllTargeted 只返回 1 个工具，且 target=bcs-read
//	- 与 bcs_tools 合并装配后，总数=14（bcs_tools 13 个 + compositetools 1 个），
//	  Name 无冲突
//	- DiagnosisAgent (bcs-read) 应看到 logs_unified_query
//	- RepairAgent (bcs-write) 应完全看不到（纯读工具不应泄漏到写链）
func TestLogsUnifiedE2E_TargetVisibility(t *testing.T) {
	t.Setenv("BCS_API_MOCK", "1")
	t.Setenv("BK_API_MOCK", "1")

	bkClient := bkapi.NewClient()
	bcsClient := bcsapi.NewClient()

	compositeAll := compositetools.NewAllTargeted(bkClient, bcsClient)
	bcsAll := bcstools.NewAllTargeted(bcsClient)

	// ---- compositetools 包的形状 ----
	if len(compositeAll) != 1 {
		names := make([]string, 0, len(compositeAll))
		for _, tt := range compositeAll {
			names = append(names, tt.Tool.Declaration().Name)
		}
		t.Fatalf("compositetools.NewAllTargeted 应返 1 个工具，实际 %d：%v", len(compositeAll), names)
	}
	first := compositeAll[0]
	if got := first.Tool.Declaration().Name; got != "logs_unified_query" {
		t.Fatalf("compositetools 第一个工具名应为 logs_unified_query，实际 %q", got)
	}
	if first.Target != "bcs-read" {
		t.Errorf("logs_unified_query 应归属 bcs-read target，实际 %q", first.Target)
	}

	// ---- 合并装配后 Name 不冲突 ----
	merged := append([]tools.TargetedTool{}, bcsAll...)
	merged = append(merged, compositeAll...)
	seen := map[string]bool{}
	for _, tt := range merged {
		name := tt.Tool.Declaration().Name
		if seen[name] {
			t.Errorf("工具 Name 冲突：%q 在合并装配中重复", name)
		}
		seen[name] = true
	}
	// 期望总数：bcs_tools 13 个（D25 新增 bcs_network_update）+ compositetools 1 个 = 14
	if len(merged) != 14 {
		t.Errorf("合并装配总数应为 14（bcs 13 + composite 1），实际 %d", len(merged))
	}

	// ---- DiagnosisAgent (bcs-read) 视角必须看到 logs_unified_query ----
	readScope := tools.FilterByTargets(merged, []string{"bcs-read"})
	found := false
	for _, tl := range readScope {
		if tl.Declaration().Name == "logs_unified_query" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, 0, len(readScope))
		for _, tl := range readScope {
			names = append(names, tl.Declaration().Name)
		}
		t.Errorf("DiagnosisAgent (bcs-read) 应看到 logs_unified_query，实际可见：%v", names)
	}

	// ---- RepairAgent (bcs-write) 绝对不应看到 logs_unified_query ----
	writeScope := tools.FilterByTargets(merged, []string{"bcs-write"})
	for _, tl := range writeScope {
		if tl.Declaration().Name == "logs_unified_query" {
			t.Errorf("RepairAgent (bcs-write) 不应看到纯读工具 logs_unified_query")
		}
	}

	// ---- Declaration 可序列化（Schema 完整） ----
	decl := first.Tool.Declaration()
	if decl.Description == "" {
		t.Errorf("logs_unified_query Declaration.Description 不应为空")
	}
	if decl.InputSchema == nil {
		t.Errorf("logs_unified_query Declaration.InputSchema 不应为 nil")
	}
	// 断言 CallableTool 转换成功（兜底）
	if _, ok := first.Tool.(tool.CallableTool); !ok {
		t.Errorf("logs_unified_query 应实现 tool.CallableTool")
	}
}

// TestLogsUnifiedE2E_NilClientsFallback 两个 client 都传 nil 时，
// 装配入口应自动 NewClient() 兜底（Mock 模式下），仍可完成调用。
//
// 这条用例保证 app.go 装配路径上偶发的"client 未就绪"不会 panic，
// 是"零崩溃装配"原则的小守门员。
func TestLogsUnifiedE2E_NilClientsFallback(t *testing.T) {
	t.Setenv("BCS_API_MOCK", "1")
	t.Setenv("BK_API_MOCK", "1")

	// 两个 nil 走 fallback
	all := compositetools.NewAllTargeted(nil, nil)
	if len(all) != 1 {
		t.Fatalf("nil client fallback 装配应仍返 1 个工具，实际 %d", len(all))
	}
	unified, _ := all[0].Tool.(tool.CallableTool)
	if unified == nil {
		t.Fatal("fallback 装配的工具应实现 CallableTool")
	}
	raw, err := unified.Call(t.Context(), []byte(`{
		"cluster_id": "BCS-K8S-00001",
		"namespace":  "ns",
		"pod":        "p",
		"bk_biz_id":  42,
		"index_set":  "1",
		"tail_lines": 10
	}`))
	if err != nil {
		t.Fatalf("fallback 路径调用失败：%v", err)
	}
	// 序列化到 map 确认 ok / mock
	bs, _ := json.Marshal(raw)
	var r map[string]any
	_ = json.Unmarshal(bs, &r)
	if ok, _ := r["ok"].(bool); !ok {
		t.Errorf("fallback 路径应返回 ok=true，实际 %+v", r)
	}
	if mock, _ := r["mock"].(bool); !mock {
		t.Errorf("fallback 路径应标 mock=true，实际 %+v", r)
	}
}
