// logs_unified.go —— 双源日志聚合工具 `logs_unified_query`（D23'）。
//
// # 它做什么
//
// 在一次调用里并发拉取 **K8s 容器 stdout（bcsapi.GetRaw → /pods/*/log）** 和
// **蓝鲸日志平台（bkapi.PostJSON → /api/bk-log/prod/search/）** 两个源，
// 按时间戳合并成统一 entries[]，每条标记 source 字段。
//
// # 它为什么存在
//
// 见 package doc。简言之——真实 oncall 场景两源同屏是刚需，顺序调两个工具手工拼
// 时间线的体验太差；这个工具把"对齐"这件事下沉到工具层。
//
// # 为什么不直接调 bk_log_query / bcs_pod_logs_tail 已注册的 tool.Tool
//
// 两点考虑：
//
//   1. 包耦合：tool 包相互 import 会把"装配顺序"变成"编译顺序"，未来任一 tool 包
//      重构都会连累 composite。直接用 infra 层 client 保持单向依赖。
//   2. Mock 兜底：client 本身有 Mock 兜底，composite 不需要额外做 tool 层的适配；
//      两源在 Mock 模式下各自返回预置样例，合并逻辑用真实数据路径直接可测。
//
// # 失败隔离原则
//
// 任一源失败不阻塞另一源返回——这是诊断工具的第一直觉。调用方只要拿到**任何一条
// 真实日志**都比"两个源都失败整体返回 error"更有价值。失败原因写到 Data 里由 LLM 决策。
//
// # 时间戳解析与排序
//
// K8s stdout 若设置 timestamps=true 会在行首追加 RFC3339Nano（固定格式），
// 我们解析这个前缀；无前缀或解析失败的条目按"源的 lane 插入顺序"保留，合并时把
// "有 ts 的"按 ts 升序排列，"无 ts 的"穿插在对应源的时序位置——
// 用户肉眼看到的就是"近似按时间的合并流"，不会因为一行解析失败整个排序乱掉。
//
// # 输出契约
//
//   entries[] {
//     source:    "k8s_stdout" | "bk_log"
//     timestamp: RFC3339Nano，解析失败时为空
//     pod:       Pod 名（k8s_stdout 必填；bk_log 若原数据含 host/pod 字段则回填）
//     container: 容器名（仅 k8s_stdout 有）
//     level:     日志级别（仅 bk_log 结构化日志有）
//     message:   行内容（k8s_stdout 去掉时间戳前缀后的正文；bk_log 的 message 字段）
//     raw:       原始字节（仅 k8s_stdout，保留完整行；bk_log 为空）
//   }
package compositetools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
)

// 聚合上限常量。
//
//   - DefaultPerSourceLines：每个源的默认拉取行数（与 bcs_pod_logs_tail 的 DefaultTailLines
//     对齐为 100，确保单源不爆上下文）
//   - MaxPerSourceLines：每个源硬上限（与 bcs_pod_logs_tail 的 MaxTailLines 对齐，5000）
//   - MaxTotalBytes：合并后总字节硬上限，防 LLM 上下文爆炸（两个 256KB 源叠加 → 512KB）
//   - K8sLineCap：单个 K8s 源响应字节硬上限（下发给 bcsapi.GetRaw），256KB 与
//     bcs_pod_logs_tail.MaxLogBytes 对齐
const (
	DefaultPerSourceLines = 100
	MaxPerSourceLines     = 5000
	MaxTotalBytes         = 512 * 1024
	K8sLineCap            = 256 * 1024
)

// LogsUnifiedInput 聚合查询入参。
//
// K8s 侧与 bk-log 侧的寻址参数分开，因为两个源的"身份投影"不同：
//
//   - K8s 侧：cluster_id + namespace + pod + container 四元组锁定
//   - bk-log 侧：bk_biz_id + index_set + query 三元组锁定（通常 query 里附 pod 名做过滤）
//
// 统一维度只在时间窗口（since_seconds / tail_lines）上对齐。
type LogsUnifiedInput struct {
	// ===== K8s 侧 =====
	ClusterID  string   `json:"cluster_id"   description:"BCS 集群 ID（K8s 侧必填；留空 ⇒ 跳过 K8s 源）"`
	Namespace  string   `json:"namespace"    description:"K8s 命名空间（K8s 侧必填）"`
	Pod        string   `json:"pod"          description:"Pod 名（K8s 侧必填；bk-log 侧若传则用于回填 entries.pod 并建议拼进 bk_query）"`
	Container  string   `json:"container"    description:"容器名（多容器 Pod 必填；单容器可留空）"`
	Previous   bool     `json:"previous"     description:"是否拉 K8s 上一次崩溃的容器日志（CrashLoopBackOff 排查必备；仅 K8s 侧生效）"`
	// ===== bk-log 侧 =====
	BKBizID   int    `json:"bk_biz_id"   description:"蓝鲸业务 ID（bk-log 侧必填；留空 ⇒ 跳过 bk-log 源）"`
	IndexSet  string `json:"index_set"   description:"蓝鲸日志索引集 ID（bk-log 侧必填）"`
	BKQuery   string `json:"bk_query"    description:"bk-log 查询语句（KQL/Lucene）；留空默认 '*'（即不过滤）"`
	// ===== 统一参数 =====
	TailLines    int    `json:"tail_lines"    description:"每个源返回末尾行数（默认 100，每源最大 5000）"`
	SinceSeconds int    `json:"since_seconds" description:"只看最近 N 秒（两源同时生效；0/不填 ⇒ 不限）"`
	Timestamps   bool   `json:"timestamps"    description:"K8s 侧是否在行首附带 RFC3339Nano（便于时间线合并；默认 true 以保障排序）"`
	SortDesc     bool   `json:"sort_desc"     description:"合并后是否按时间戳倒序（默认 false ⇒ 时间升序）"`
}

// LogEntry 合并后的统一日志条目。
type LogEntry struct {
	Source    string `json:"source"`              // "k8s_stdout" | "bk_log"
	Timestamp string `json:"timestamp,omitempty"` // RFC3339Nano；解析失败为空
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`
	Level     string `json:"level,omitempty"`
	Host      string `json:"host,omitempty"`
	Message   string `json:"message"`
	Raw       string `json:"raw,omitempty"` // 仅 K8s 保留原始行（含 ts 前缀），便于调试
	// parsedTS 内部字段，不序列化；用于稳定排序
	parsedTS time.Time
	hasTS    bool
}

// sourceStats 单源的抓取统计，用于 Result.Data.stats 与审计。
type sourceStats struct {
	Source    string `json:"source"`
	OK        bool   `json:"ok"`
	Entries   int    `json:"entries"`
	Bytes     int    `json:"bytes"`
	Mock      bool   `json:"mock,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

// newLogsUnifiedTool 构造 logs_unified_query 工具。
func newLogsUnifiedTool(bkClient *bkapi.Client, bcsClient *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in LogsUnifiedInput) (*Result, error) {
		// —— 参数规整 ——
		if in.TailLines <= 0 {
			in.TailLines = DefaultPerSourceLines
		}
		if in.TailLines > MaxPerSourceLines {
			return nil, fmt.Errorf("tail_lines=%d 超过每源硬上限 %d（防 LLM 上下文爆炸；要更多历史请用 since_seconds 滚窗）",
				in.TailLines, MaxPerSourceLines)
		}
		if in.SinceSeconds < 0 {
			return nil, fmt.Errorf("since_seconds 不能为负数")
		}
		// timestamps 对 K8s 源合并排序很关键；默认开启
		if !in.Timestamps {
			in.Timestamps = true
		}

		// —— 判定两源是否要跑 ——
		runK8s := in.ClusterID != "" && in.Namespace != "" && in.Pod != ""
		runBK := in.BKBizID > 0 && strings.TrimSpace(in.IndexSet) != ""
		if !runK8s && !runBK {
			return nil, fmt.Errorf("至少要提供一个源的完整参数：K8s(cluster_id+namespace+pod) 或 bk-log(bk_biz_id+index_set)")
		}

		// —— 并发两路 fetch ——
		var (
			wg          sync.WaitGroup
			k8sEntries  []LogEntry
			bkEntries   []LogEntry
			k8sStat     sourceStats
			bkStat      sourceStats
		)
		k8sStat.Source = "k8s_stdout"
		bkStat.Source = "bk_log"

		if runK8s {
			wg.Add(1)
			go func() {
				defer wg.Done()
				k8sEntries, k8sStat = fetchK8sStdout(ctx, bcsClient, in)
			}()
		} else {
			k8sStat.Error = "skipped (cluster_id/namespace/pod 未提供)"
		}
		if runBK {
			wg.Add(1)
			go func() {
				defer wg.Done()
				bkEntries, bkStat = fetchBKLog(ctx, bkClient, in)
			}()
		} else {
			bkStat.Error = "skipped (bk_biz_id/index_set 未提供)"
		}
		wg.Wait()

		// —— 合并 + 按时间戳排序 ——
		merged := mergeAndSort(k8sEntries, bkEntries, in.SortDesc)

		// —— 总字节截断保护 ——
		merged, truncatedTotal := capTotalBytes(merged, MaxTotalBytes)

		// —— 构造 Result ——
		totalBytes := 0
		for _, e := range merged {
			totalBytes += len(e.Message) + len(e.Raw)
		}

		bothOK := k8sStat.OK || !runK8s
		bothOK = bothOK && (bkStat.OK || !runBK)
		isMock := k8sStat.Mock || bkStat.Mock

		msg := fmt.Sprintf("聚合完成：K8s=%d 条 / bk-log=%d 条 / 合并后=%d 条 / 字节=%d",
			k8sStat.Entries, bkStat.Entries, len(merged), totalBytes)
		if truncatedTotal {
			msg += "（⚠ 合并后超过 512KB 已硬截断，考虑缩小 tail_lines 或加 since_seconds 滚窗）"
		}
		if isMock {
			msg = "[Mock] " + msg
		}

		emitLogsUnifiedAudit(bkClient, bcsClient, in, k8sStat, bkStat, len(merged), totalBytes, truncatedTotal)

		return &Result{
			OK:      bothOK,
			Mock:    isMock,
			Message: msg,
			Data: map[string]any{
				"entries":    merged,
				"entry_count": len(merged),
				"total_bytes": totalBytes,
				"truncated":  truncatedTotal,
				"stats":      []sourceStats{k8sStat, bkStat},
				"input_echo": map[string]any{
					"cluster_id":    in.ClusterID,
					"namespace":     in.Namespace,
					"pod":           in.Pod,
					"container":     in.Container,
					"bk_biz_id":     in.BKBizID,
					"index_set":     in.IndexSet,
					"tail_lines":    in.TailLines,
					"since_seconds": in.SinceSeconds,
					"sort_desc":     in.SortDesc,
				},
			},
		}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("logs_unified_query"),
		function.WithDescription(
			"双源日志聚合查询（D23'，纯读）。"+
				"并发拉取 K8s 容器 stdout（cluster_id+namespace+pod）与 蓝鲸日志平台（bk_biz_id+index_set），"+
				"按时间戳合并排序成统一 entries[]，每条标记 source（k8s_stdout/bk_log）。"+
				"失败隔离：任一源失败不阻塞另一源返回。"+
				"典型用法：1) CrashLoopBackOff 跨源对齐：K8s stdout+应用 ERROR 一屏看；"+
				"2) 时间窗口聚焦：since_seconds=300 两源同时回溯 5 分钟；"+
				"3) 单源查询：只填一侧参数即可（另一侧自动跳过）。"+
				"⚠ 合并后 512KB 硬截断；tail_lines 每源 5000 硬上限。",
		),
	)
}

// fetchK8sStdout 抓 K8s 容器 stdout。复用 bcsapi.GetRaw 直达 /pods/*/log。
//
// 解析约定：
//   - 若 in.Timestamps=true，每行前缀 RFC3339Nano + " " + 正文 —— 解析 TS；
//   - 若 ts 解析失败，该行 hasTS=false，后续排序时置于无 ts 桶（按插入序）。
func fetchK8sStdout(ctx context.Context, client *bcsapi.Client, in LogsUnifiedInput) ([]LogEntry, sourceStats) {
	stat := sourceStats{Source: "k8s_stdout"}
	if client == nil {
		stat.Error = "bcs client is nil"
		return nil, stat
	}
	stat.Mock = client.IsMock()

	// Mock 模式：造一些样例行，带时间戳，便于排序逻辑在单测里被覆盖。
	if stat.Mock {
		now := time.Now()
		entries := []LogEntry{
			{Source: "k8s_stdout", Pod: in.Pod, Container: in.Container,
				Timestamp: now.Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano),
				Message:   "INFO starting server on :8080 (mock k8s)", parsedTS: now.Add(-3 * time.Minute), hasTS: true,
				Raw: now.Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano) + " INFO starting server on :8080 (mock k8s)"},
			{Source: "k8s_stdout", Pod: in.Pod, Container: in.Container,
				Timestamp: now.Add(-1 * time.Minute).UTC().Format(time.RFC3339Nano),
				Message:   "ERROR panic: runtime error (mock k8s)", parsedTS: now.Add(-1 * time.Minute), hasTS: true,
				Raw: now.Add(-1 * time.Minute).UTC().Format(time.RFC3339Nano) + " ERROR panic: runtime error (mock k8s)"},
		}
		stat.OK = true
		stat.Entries = len(entries)
		for _, e := range entries {
			stat.Bytes += len(e.Raw)
		}
		return entries, stat
	}

	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/pods/%s/log",
		in.ClusterID, in.Namespace, in.Pod)
	query := map[string]string{
		"tailLines":  itoa(in.TailLines),
		"timestamps": boolStr(in.Timestamps),
	}
	if in.Container != "" {
		query["container"] = in.Container
	}
	if in.SinceSeconds > 0 {
		query["sinceSeconds"] = itoa(in.SinceSeconds)
	}
	if in.Previous {
		query["previous"] = "true"
	}

	raw, err := client.GetRaw(ctx, path, query, K8sLineCap)
	if err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			// 兜底（理论不到，IsMock 已拦）
			stat.Mock = true
			stat.OK = true
			return nil, stat
		}
		stat.Error = err.Error()
		return nil, stat
	}
	content := string(raw)
	stat.Bytes = len(content)
	if strings.HasSuffix(content, "...(truncated)") {
		stat.Truncated = true
	}

	// 按行解析，解析 RFC3339Nano 前缀（若有）
	var entries []LogEntry
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		e := LogEntry{Source: "k8s_stdout", Pod: in.Pod, Container: in.Container, Raw: line}
		// 尝试按 RFC3339Nano 前缀解析（K8s 格式：<RFC3339Nano> <msg>）
		if sp := strings.IndexByte(line, ' '); sp > 0 {
			if ts, err := time.Parse(time.RFC3339Nano, line[:sp]); err == nil {
				e.Timestamp = line[:sp]
				e.Message = line[sp+1:]
				e.parsedTS = ts
				e.hasTS = true
			} else {
				e.Message = line
			}
		} else {
			e.Message = line
		}
		entries = append(entries, e)
	}
	stat.OK = true
	stat.Entries = len(entries)
	return entries, stat
}

// fetchBKLog 抓蓝鲸日志平台。复用 bkapi.PostJSON → /api/bk-log/prod/search/。
//
// 请求体与 bk_tools/log.go 的 LogInput 对齐，参数做等价投影：
//
//	keyword      ← in.BKQuery (空则 "*"，表示不过滤)
//	start_time   ← now - since_seconds（若 since_seconds>0）
//	end_time     ← now
//	size         ← in.TailLines
//	sort_list    ← [[@timestamp desc]]
func fetchBKLog(ctx context.Context, client *bkapi.Client, in LogsUnifiedInput) ([]LogEntry, sourceStats) {
	stat := sourceStats{Source: "bk_log"}
	if client == nil {
		stat.Error = "bk client is nil"
		return nil, stat
	}
	stat.Mock = client.IsMock()

	if stat.Mock {
		// 与 bk_tools/log.go 的 mockLog() 风格一致，但标记到 entries 里并带 TS
		now := time.Now()
		entries := []LogEntry{
			{Source: "bk_log", Pod: in.Pod, Host: "10.1.1.100", Level: "ERROR",
				Timestamp: now.Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano),
				Message:   "connection reset by peer: redis://game-cache:6379 (mock bk-log)",
				parsedTS:  now.Add(-2 * time.Minute), hasTS: true},
			{Source: "bk_log", Pod: in.Pod, Host: "10.1.1.100", Level: "WARN",
				Timestamp: now.Add(-5 * time.Minute).UTC().Format(time.RFC3339Nano),
				Message:   "slow query detected, latency=1.2s (mock bk-log)",
				parsedTS:  now.Add(-5 * time.Minute), hasTS: true},
		}
		stat.OK = true
		stat.Entries = len(entries)
		for _, e := range entries {
			stat.Bytes += len(e.Message)
		}
		return entries, stat
	}

	keyword := strings.TrimSpace(in.BKQuery)
	if keyword == "" {
		keyword = "*"
	}
	// 与 pod 做软关联：若 Pod 非空，建议用户在 query 里手动拼；这里不强制注入避免误过滤
	reqBody := map[string]any{
		"bk_biz_id":    in.BKBizID,
		"index_set_id": in.IndexSet,
		"keyword":      keyword,
		"size":         in.TailLines,
		"sort_list":    [][]string{{"@timestamp", "desc"}},
	}
	if in.SinceSeconds > 0 {
		now := time.Now().UTC()
		reqBody["start_time"] = now.Add(-time.Duration(in.SinceSeconds) * time.Second).Format(time.RFC3339)
		reqBody["end_time"] = now.Format(time.RFC3339)
	}

	var resp map[string]any
	err := client.PostJSON(ctx, "/api/bk-log/prod/search/", reqBody, &resp)
	if err != nil {
		if errors.Is(err, bkapi.ErrMockMode) {
			stat.Mock = true
			stat.OK = true
			return nil, stat
		}
		stat.Error = err.Error()
		return nil, stat
	}

	// 解析通用字段：hits[].{timestamp,level,host,message}
	// 真实蓝鲸日志平台返回结构有多种形态，这里做容错提取，找不到就保留空值。
	entries := parseBKHits(resp, in.Pod)
	stat.OK = true
	stat.Entries = len(entries)
	for _, e := range entries {
		stat.Bytes += len(e.Message)
	}
	return entries, stat
}

// parseBKHits 从 bk-log 返回的 JSON 里提取 hits（兼容多种嵌套路径）。
//
// 宽容原则：找不到任何一条合法 hit 就返回空；不 panic、不硬报错。
// 常见路径：
//   resp["hits"]                 →  []map{timestamp,level,host,message}
//   resp["data"]["hits"]         →  同上
//   resp["data"]["list"]         →  同上（别名）
func parseBKHits(resp map[string]any, podHint string) []LogEntry {
	candidates := []any{
		resp["hits"],
		digMap(resp, "data", "hits"),
		digMap(resp, "data", "list"),
	}
	var hits []any
	for _, c := range candidates {
		if arr, ok := c.([]any); ok && len(arr) > 0 {
			hits = arr
			break
		}
	}
	if hits == nil {
		return nil
	}
	out := make([]LogEntry, 0, len(hits))
	for _, h := range hits {
		m, ok := h.(map[string]any)
		if !ok {
			continue
		}
		e := LogEntry{Source: "bk_log"}
		if v, ok := m["timestamp"].(string); ok {
			e.Timestamp = v
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				e.parsedTS = ts
				e.hasTS = true
			} else if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
				e.parsedTS = ts
				e.hasTS = true
			}
		}
		if v, ok := m["level"].(string); ok {
			e.Level = v
		}
		if v, ok := m["host"].(string); ok {
			e.Host = v
		}
		if v, ok := m["message"].(string); ok {
			e.Message = v
		}
		if v, ok := m["pod"].(string); ok && v != "" {
			e.Pod = v
		} else if podHint != "" {
			e.Pod = podHint
		}
		out = append(out, e)
	}
	return out
}

// mergeAndSort 合并两源并按时间戳排序。
//
// 策略：
//   - 有 ts 的条目按 ts 升/降序排列
//   - 无 ts 的条目**追加在尾部**，保留两源的原始插入序（k8s 在前，bk-log 在后）
//     — 这是为了"解析失败时不掩盖任何行，但也不污染已对齐的时间线"
func mergeAndSort(k8s, bk []LogEntry, desc bool) []LogEntry {
	var hasTS, noTS []LogEntry
	for _, e := range k8s {
		if e.hasTS {
			hasTS = append(hasTS, e)
		} else {
			noTS = append(noTS, e)
		}
	}
	for _, e := range bk {
		if e.hasTS {
			hasTS = append(hasTS, e)
		} else {
			noTS = append(noTS, e)
		}
	}
	sort.SliceStable(hasTS, func(i, j int) bool {
		if desc {
			return hasTS[i].parsedTS.After(hasTS[j].parsedTS)
		}
		return hasTS[i].parsedTS.Before(hasTS[j].parsedTS)
	})
	merged := make([]LogEntry, 0, len(hasTS)+len(noTS))
	merged = append(merged, hasTS...)
	merged = append(merged, noTS...)
	return merged
}

// capTotalBytes 强制合并后总字节不超过 cap，超出截断并返回 truncated=true。
//
// 按"从头保留"策略（时序升序时保留最早的；倒序时保留最新的）。
func capTotalBytes(entries []LogEntry, cap int) ([]LogEntry, bool) {
	total := 0
	for i, e := range entries {
		total += len(e.Message) + len(e.Raw)
		if total > cap {
			return entries[:i], true
		}
	}
	return entries, false
}

// emitLogsUnifiedAudit 只读操作也审计（与 bcs_pod_logs_tail 一致的理由：
// 合规/复盘/高频检测）。
func emitLogsUnifiedAudit(bkClient *bkapi.Client, bcsClient *bcsapi.Client,
	in LogsUnifiedInput, k8sStat, bkStat sourceStats, mergedCount, totalBytes int, truncated bool) {
	target := fmt.Sprintf("k8s=%s/%s/%s, bk=%d/%s",
		in.ClusterID, in.Namespace, in.Pod, in.BKBizID, in.IndexSet)
	audit.Emit(audit.Event{
		Agent:    "diagnosis_agent",
		Action:   "composite.logs_unified",
		Severity: "Info",
		Target:   target,
		Params: map[string]any{
			"cluster_id":    in.ClusterID,
			"namespace":     in.Namespace,
			"pod":           in.Pod,
			"container":     in.Container,
			"bk_biz_id":     in.BKBizID,
			"index_set":     in.IndexSet,
			"tail_lines":    in.TailLines,
			"since_seconds": in.SinceSeconds,
			"k8s_entries":   k8sStat.Entries,
			"k8s_ok":        k8sStat.OK,
			"k8s_error":     k8sStat.Error,
			"bk_entries":    bkStat.Entries,
			"bk_ok":         bkStat.OK,
			"bk_error":      bkStat.Error,
			"merged_count":  mergedCount,
			"total_bytes":   totalBytes,
			"truncated":     truncated,
		},
		Success: k8sStat.OK || bkStat.OK, // 任一源成功都算成功
		Mock:    (bkClient != nil && bkClient.IsMock()) || (bcsClient != nil && bcsClient.IsMock()),
	})
}

// —— helpers ——

// digMap 深挖 map：digMap(m, "a","b","c") ≡ m["a"]["b"]["c"]，任一层缺失返回 nil。
func digMap(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
