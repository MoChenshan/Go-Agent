// logs_unified_test.go —— 覆盖 logs_unified_query 的关键行为：
//
//  1. 单源场景（只给 K8s / 只给 bk-log）
//  2. 双源并发聚合（Mock 模式下两个源都返回样例）
//  3. 时间戳排序（升序/倒序）
//  4. 失败隔离（一侧出错不阻塞另一侧）
//  5. 参数校验（tail_lines 上限 / since_seconds 负数 / 两源全空）
//  6. 总字节截断（构造超大条目触发 capTotalBytes）
//  7. parseBKHits 的路径容错
package compositetools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// —— 通用测试辅助：拿到工具并用 Mock client 执行一次调用，解出 Result ——

func callUnified(t *testing.T, in LogsUnifiedInput) *Result {
	t.Helper()
	bkClient := bkapi.NewClient()   // Mock：未设 BK_APIGW_BASE_URL 时默认 mock
	bcsClient := bcsapi.NewClient() // Mock：未设 BCS_GATEWAY_URL 时默认 mock

	tl := newLogsUnifiedTool(bkClient, bcsClient)
	callable, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("tool is not CallableTool")
	}
	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	raw, err := callable.Call(context.Background(), payload)
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}
	// FunctionTool.Call 返回 fn 原值（*Result）；统一走 marshal→unmarshal
	// 让 r.Data 退化为 map[string]any，与既有断言兼容。
	var b []byte
	switch v := raw.(type) {
	case []byte:
		b = v
	default:
		b, err = json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %T: %v", v, err)
		}
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal result: %v (raw=%s)", err, string(b))
	}
	return &r
}

// ========== 1. 单源：只给 K8s ==========

func TestLogsUnified_K8sOnly(t *testing.T) {
	r := callUnified(t, LogsUnifiedInput{
		ClusterID: "BCS-K8S-00001",
		Namespace: "ns-test",
		Pod:       "pod-x",
		Container: "app",
		TailLines: 50,
	})
	if !r.OK {
		t.Fatalf("expect ok, got %+v", r)
	}
	if !r.Mock {
		t.Fatalf("expect mock=true, got %+v", r)
	}
	data := r.Data.(map[string]any)
	stats := data["stats"].([]any)
	if len(stats) != 2 {
		t.Fatalf("expect 2 stats, got %d", len(stats))
	}
	// bk 源没跑：应体现在 stats 里
	bkStat := stats[1].(map[string]any)
	if bkStat["source"] != "bk_log" {
		t.Fatalf("stats[1] should be bk_log, got %v", bkStat["source"])
	}
	// merged_count 应该大于 0
	if int(data["entry_count"].(float64)) == 0 {
		t.Fatalf("expect entries>0, got 0")
	}
}

// ========== 2. 单源：只给 bk-log ==========

func TestLogsUnified_BKLogOnly(t *testing.T) {
	r := callUnified(t, LogsUnifiedInput{
		BKBizID:   100205,
		IndexSet:  "2_bklog.app_log",
		BKQuery:   "level:ERROR",
		TailLines: 20,
	})
	if !r.OK {
		t.Fatalf("expect ok, got %+v", r)
	}
	data := r.Data.(map[string]any)
	entries := data["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expect bk-log entries, got 0")
	}
	// 所有条目 source 应该是 bk_log
	for _, e := range entries {
		m := e.(map[string]any)
		if m["source"] != "bk_log" {
			t.Fatalf("expect all entries source=bk_log in bk-only scenario, got %v", m["source"])
		}
	}
}

// ========== 3. 双源聚合 + 时间戳升序 ==========

func TestLogsUnified_BothSources_SortAsc(t *testing.T) {
	r := callUnified(t, LogsUnifiedInput{
		ClusterID: "BCS-K8S-00001",
		Namespace: "ns",
		Pod:       "pod-y",
		Container: "app",
		BKBizID:   100205,
		IndexSet:  "2_bklog.x",
		TailLines: 100,
	})
	if !r.OK {
		t.Fatalf("expect ok, got %+v", r)
	}
	data := r.Data.(map[string]any)
	entries := data["entries"].([]any)
	if len(entries) < 3 {
		t.Fatalf("expect >=3 entries (2 k8s + 2 bk) from mock, got %d", len(entries))
	}
	// 检查时间戳升序：parsedTS 不可见，退而检查 Timestamp 字段
	var prev time.Time
	for i, e := range entries {
		m := e.(map[string]any)
		tsStr, _ := m["timestamp"].(string)
		if tsStr == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			continue
		}
		if i > 0 && !prev.IsZero() && ts.Before(prev) {
			t.Fatalf("entries not sorted asc at idx=%d: prev=%v ts=%v", i, prev, ts)
		}
		prev = ts
	}
}

// ========== 4. 时间戳倒序 ==========

func TestLogsUnified_BothSources_SortDesc(t *testing.T) {
	r := callUnified(t, LogsUnifiedInput{
		ClusterID: "c",
		Namespace: "ns",
		Pod:       "pod-z",
		BKBizID:   1,
		IndexSet:  "i",
		SortDesc:  true,
	})
	if !r.OK {
		t.Fatalf("expect ok, got %+v", r)
	}
	data := r.Data.(map[string]any)
	entries := data["entries"].([]any)
	var prev time.Time
	for i, e := range entries {
		m := e.(map[string]any)
		tsStr, _ := m["timestamp"].(string)
		if tsStr == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			continue
		}
		if i > 0 && !prev.IsZero() && ts.After(prev) {
			t.Fatalf("entries not sorted desc at idx=%d: prev=%v ts=%v", i, prev, ts)
		}
		prev = ts
	}
}

// ========== 5. 参数校验 ==========

func TestLogsUnified_InputValidation(t *testing.T) {
	tl := newLogsUnifiedTool(bkapi.NewClient(), bcsapi.NewClient())
	callable := tl.(tool.CallableTool)

	tests := []struct {
		name    string
		input   LogsUnifiedInput
		wantErr string
	}{
		{
			name:    "both sources empty",
			input:   LogsUnifiedInput{},
			wantErr: "至少要提供一个源",
		},
		{
			name: "tail_lines exceeds cap",
			input: LogsUnifiedInput{
				ClusterID: "c", Namespace: "ns", Pod: "p",
				TailLines: MaxPerSourceLines + 1,
			},
			wantErr: "超过每源硬上限",
		},
		{
			name: "since_seconds negative",
			input: LogsUnifiedInput{
				ClusterID: "c", Namespace: "ns", Pod: "p",
				SinceSeconds: -1,
			},
			wantErr: "不能为负数",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.input)
			_, err := callable.Call(context.Background(), payload)
			if err == nil {
				t.Fatalf("expect err containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expect err containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// ========== 6. 失败隔离（通过 nil client 模拟）==========

func TestLogsUnified_FailureIsolation(t *testing.T) {
	// 构造一个场景：bk 正常、bcs 传 nil 模拟失败
	tl := newLogsUnifiedTool(bkapi.NewClient(), nil)
	callable := tl.(tool.CallableTool)
	payload, _ := json.Marshal(LogsUnifiedInput{
		ClusterID: "c", Namespace: "ns", Pod: "p",
		BKBizID: 1, IndexSet: "i",
	})
	raw, err := callable.Call(context.Background(), payload)
	if err != nil {
		t.Fatalf("tool should not fail even if one source down, got err=%v", err)
	}
	// 与上面 helper 保持一致：static struct → marshal → unmarshal → map
	var r Result
	var b []byte
	switch v := raw.(type) {
	case []byte:
		b = v
	default:
		b, _ = json.Marshal(v)
	}
	_ = json.Unmarshal(b, &r)
	// 不应整体 OK（因为 k8s 侧失败）
	if r.OK {
		t.Fatalf("expect OK=false (one source failed), got OK=true")
	}
	data := r.Data.(map[string]any)
	stats := data["stats"].([]any)
	k8sStat := stats[0].(map[string]any)
	bkStat := stats[1].(map[string]any)
	if k8sStat["ok"].(bool) {
		t.Fatalf("expect k8s ok=false, got true")
	}
	if !bkStat["ok"].(bool) {
		t.Fatalf("expect bk ok=true (not affected), got false")
	}
	// 至少得有 bk 的 entries
	entries := data["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expect bk entries even when k8s down, got 0")
	}
}

// ========== 7. 合并 + 总字节截断（纯单元测 capTotalBytes） ==========

func TestCapTotalBytes(t *testing.T) {
	big := strings.Repeat("x", 100*1024) // 100KB 一条
	entries := []LogEntry{
		{Source: "a", Message: big},
		{Source: "b", Message: big},
		{Source: "c", Message: big},
		{Source: "d", Message: big},
		{Source: "e", Message: big},
		{Source: "f", Message: big}, // 600KB > 512KB 应被截断
	}
	out, trunc := capTotalBytes(entries, MaxTotalBytes)
	if !trunc {
		t.Fatalf("expect truncated=true, got false")
	}
	if len(out) >= len(entries) {
		t.Fatalf("expect output shorter than input, got %d>=%d", len(out), len(entries))
	}
	// 保留的字节总和 <= cap
	total := 0
	for _, e := range out {
		total += len(e.Message) + len(e.Raw)
	}
	if total > MaxTotalBytes {
		t.Fatalf("retained bytes %d exceed cap %d", total, MaxTotalBytes)
	}
}

// ========== 8. mergeAndSort 直接测（无 TS 条目追加在尾部） ==========

func TestMergeAndSort_NoTSEntriesPreserved(t *testing.T) {
	t0 := time.Now()
	k8s := []LogEntry{
		{Source: "k8s_stdout", Message: "k1", parsedTS: t0.Add(-2 * time.Minute), hasTS: true},
		{Source: "k8s_stdout", Message: "k2-no-ts"}, // 无 ts
	}
	bk := []LogEntry{
		{Source: "bk_log", Message: "b1", parsedTS: t0.Add(-1 * time.Minute), hasTS: true},
		{Source: "bk_log", Message: "b2-no-ts"}, // 无 ts
	}
	merged := mergeAndSort(k8s, bk, false)
	if len(merged) != 4 {
		t.Fatalf("expect 4 merged, got %d", len(merged))
	}
	// 前 2 条应该是 hasTS 的升序：k1(-2min), b1(-1min)
	if merged[0].Message != "k1" || merged[1].Message != "b1" {
		t.Fatalf("sort order wrong, got %s, %s", merged[0].Message, merged[1].Message)
	}
	// 后 2 条是无 ts 的，按原插入序：k2, b2
	if merged[2].Message != "k2-no-ts" || merged[3].Message != "b2-no-ts" {
		t.Fatalf("no-ts tail order wrong, got %s, %s", merged[2].Message, merged[3].Message)
	}
}

// ========== 9. parseBKHits 路径容错 ==========

func TestParseBKHits_MultiplePaths(t *testing.T) {
	// 顶层 hits
	r1 := parseBKHits(map[string]any{
		"hits": []any{
			map[string]any{"timestamp": "2026-04-23T12:00:00Z", "level": "ERROR", "message": "m1"},
		},
	}, "")
	if len(r1) != 1 || r1[0].Message != "m1" {
		t.Fatalf("top-level hits path failed: %+v", r1)
	}

	// data.hits
	r2 := parseBKHits(map[string]any{
		"data": map[string]any{
			"hits": []any{
				map[string]any{"message": "m2"},
			},
		},
	}, "")
	if len(r2) != 1 || r2[0].Message != "m2" {
		t.Fatalf("data.hits path failed: %+v", r2)
	}

	// data.list 别名
	r3 := parseBKHits(map[string]any{
		"data": map[string]any{
			"list": []any{
				map[string]any{"message": "m3", "host": "h3"},
			},
		},
	}, "")
	if len(r3) != 1 || r3[0].Host != "h3" {
		t.Fatalf("data.list path failed: %+v", r3)
	}

	// 全空
	r4 := parseBKHits(map[string]any{"foo": "bar"}, "")
	if len(r4) != 0 {
		t.Fatalf("expect 0 hits for unknown structure, got %d", len(r4))
	}

	// podHint 回填
	r5 := parseBKHits(map[string]any{
		"hits": []any{map[string]any{"message": "m5"}},
	}, "pod-hint")
	if r5[0].Pod != "pod-hint" {
		t.Fatalf("expect podHint fallback, got %q", r5[0].Pod)
	}
}

// ========== 10. NewAllTargeted 装配入口 ==========

func TestNewAllTargeted(t *testing.T) {
	targeted := NewAllTargeted(nil, nil)
	if len(targeted) != 1 {
		t.Fatalf("expect 1 tool, got %d", len(targeted))
	}
	if targeted[0].Target != TargetRead {
		t.Fatalf("expect target=%s, got %s", TargetRead, targeted[0].Target)
	}
}
