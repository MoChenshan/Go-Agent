// Package bcstools —— bcs_pod_logs_tail（Pod 日志拉取，D21 新增，纯读）。
//
// # 为什么需要这个工具
//
// D20.2 闭合 HPA 写路径之后出现了一个对称失衡：现在 agent 能**改**的能力，
// 已经远远强过能**看**的能力。on-call 真实工作流永远是"先看日志找到原因，
// 再决定怎么改"；如果只能改不能看，用户还得回终端敲 `kubectl logs -f`。
//
// 这跟 D20 之前"感知了但不能改"是对称的问题，必须现在补齐。
//
// # 诊断 vs 修复：两条链的对称性
//
//	诊断链（本工具要补齐）：     pod_logs_tail → pod_describe(未来)
//	修复链（D1-D20 已补齐）：     scale / pod_restart / configmap_update /
//	                             hpa_patch / helm_manage / alarm_silence
//
// D21 补 pod_logs_tail —— 诊断链里最高频的只读技能（运维 95% 场景的第一步
// 就是 "kubectl logs -f xxx"），做完这个诊断链就有了最核心的砖。
//
// # 与 bcs_resource_query 的分工
//
//	bcs_resource_query   关心"Pod 对象是什么样"：status.phase / conditions / events
//	bcs_pod_logs_tail    关心"Pod 内部发生了什么"：stdout/stderr 原始日志
//
// 两者一起用才能拼出完整诊断：resource_query 看到 Pod CrashLoopBackOff →
// pod_logs_tail 用 `previous=true` 拉上次崩溃的日志 → 找到真实原因。
//
// # 核心能力矩阵
//
//   - **多 Pod 批量**：pods[] 一次拉多个，按 name 聚合返回（诊断场景常见"3 个副本都看看"）
//   - **多 container 支持**：containers[] 指定容器；为空且 pod 只有一个容器则自动选择
//   - **tail_lines**：默认 100，最大 5000（LLM 上下文硬上限考量）
//   - **since_seconds**：只拉最近 N 秒（和 tail_lines 叠加生效）
//   - **previous**：拉上一次崩溃的容器日志（CrashLoopBackOff 场景必备）
//   - **截断保护**：单 pod 响应 > MaxLogBytes 硬截断 + 标记，防上下文爆炸
//
// # 为什么不做 follow/stream
//
// agent 场景是"拉过来分析"，不是"盯着终端看"。follow 模式会让工具调用永远不返回，
// 违反 MCP 调用模型。真要实时监听应该走 D19.2 的 async-tool 模式（未来可加）。
package bcstools

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// 日志拉取阈值常量。
//
// 这些值的选取思路：
//   - DefaultTailLines=100 覆盖 80% 诊断场景（故障多数在末尾 50-100 行能看清）
//   - MaxTailLines=5000 是 LLM 上下文硬上限考量：100 字/行 × 5000 = 500KB，接近 Claude
//     200k token 的 1/10；超过这个量 LLM 就看不完整了，拉下来也无意义
//   - MaxLogBytes=262144 (256KB) 是单 pod 响应的字节上限：5000 行 × 平均 50 字符 ≈ 250KB，
//     略留余量；触发截断时在响应里标记 Truncated=true 让上层知情
//   - DefaultSinceSeconds=0 表示"不限时间"（与 K8s 默认一致）
const (
	// DefaultTailLines 为默认返回末尾行数。
	DefaultTailLines = 100
	// MaxTailLines 为单次拉取行数硬上限（超过硬拒，防 LLM 上下文爆炸）。
	MaxTailLines = 5000
	// MaxLogBytes 为单 Pod 单容器日志响应字节硬上限（超过在 bcsapi 层截断）。
	MaxLogBytes = 256 * 1024
)

// PodLogsTailInput 为 bcs_pod_logs_tail 工具入参。
//
// 多 Pod 批量场景：填 Pods[]；单 Pod 直接用 Pod 字段（两者互斥，Pods 优先）。
type PodLogsTailInput struct {
	ClusterID    string   `json:"cluster_id"    description:"BCS 集群 ID（必填）"`
	Namespace    string   `json:"namespace"     description:"Kubernetes 命名空间（必填）"`
	Pod          string   `json:"pod"           description:"Pod 名称（单 Pod 场景；与 pods 互斥，pods 优先）"`
	Pods         []string `json:"pods"          description:"Pod 名称列表（批量场景，一次拉多个；按 name 聚合返回）"`
	Containers   []string `json:"containers"    description:"（可选）容器名列表；为空且 Pod 只有一个容器时自动选择；多容器 Pod 若不填会返 Pod 里所有容器的日志"`
	TailLines    int      `json:"tail_lines"    description:"返回末尾行数（默认 100，最大 5000）"`
	SinceSeconds int      `json:"since_seconds" description:"只拉最近 N 秒内的日志（与 tail_lines 叠加生效；0/不填 = 不限）"`
	Previous     bool     `json:"previous"      description:"是否拉上一次崩溃的容器日志（CrashLoopBackOff 排查必备）"`
	Timestamps   bool     `json:"timestamps"    description:"是否在每行前附带 RFC3339Nano 时间戳（便于时间线对齐）"`
}

// PodLogEntry 单个（pod, container）维度的日志返回。
type PodLogEntry struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Lines     int    `json:"lines"`     // 实际返回行数（按 '\n' 计）
	Bytes     int    `json:"bytes"`     // 实际返回字节
	Truncated bool   `json:"truncated"` // 是否被 MaxLogBytes 截断
	Content   string `json:"content"`   // 日志文本
	Error     string `json:"error,omitempty"` // 本 entry 拉取失败原因（不影响其他 entry）
}

// newPodLogsTailTool 构造 bcs_pod_logs_tail 工具。
func newPodLogsTailTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in PodLogsTailInput) (*Result, error) {
		// 必要字段校验
		if in.ClusterID == "" || in.Namespace == "" {
			return nil, fmt.Errorf("cluster_id / namespace 为必填")
		}
		// 汇总 pod 列表：Pods[] 优先，fallback 到 Pod 单值
		pods := in.Pods
		if len(pods) == 0 && strings.TrimSpace(in.Pod) != "" {
			pods = []string{in.Pod}
		}
		if len(pods) == 0 {
			return nil, fmt.Errorf("必须提供 pod 或 pods[]（至少一个 Pod 名）")
		}
		// 行数规整
		tail := in.TailLines
		if tail <= 0 {
			tail = DefaultTailLines
		}
		if tail > MaxTailLines {
			return nil, fmt.Errorf("tail_lines=%d 超过硬上限 %d（防 LLM 上下文爆炸；需要更多历史日志请结合 since_seconds 多次拉取）", tail, MaxTailLines)
		}
		if in.SinceSeconds < 0 {
			return nil, fmt.Errorf("since_seconds 不能为负数")
		}

		// Mock 模式：每个 (pod, container) 返回一段样例日志
		isMock := client != nil && client.IsMock()

		var entries []PodLogEntry
		for _, pod := range pods {
			containers := in.Containers
			if len(containers) == 0 {
				// 空容器列表：让 K8s 默认返回（单容器 Pod 场景 K8s 会自动选；
				// 多容器 Pod 不指定 container 时 K8s 会返 400 "a container name must be specified"，
				// 我们在 fetch 里翻译成友好错误）
				containers = []string{""}
			}
			for _, container := range containers {
				entry := fetchOnePodLog(ctx, client, in, pod, container, tail, isMock)
				entries = append(entries, entry)
			}
		}

		// 审计（只读也审计，便于后续追查"谁拉过什么 pod 日志"）
		emitPodLogsTailAudit(client, in, pods, entries)

		// 聚合 Result
		okCount, failCount, totalBytes := 0, 0, 0
		for _, e := range entries {
			if e.Error != "" {
				failCount++
			} else {
				okCount++
			}
			totalBytes += e.Bytes
		}
		msg := fmt.Sprintf("成功拉取 %d 个日志段，失败 %d 段，共 %d 字节", okCount, failCount, totalBytes)
		if isMock {
			msg = "Mock 模式：" + msg
		}
		return &Result{
			OK:      failCount == 0,
			Mock:    isMock,
			Message: msg,
			Data: map[string]any{
				"cluster_id":  in.ClusterID,
				"namespace":   in.Namespace,
				"pod_count":   len(pods),
				"entries":     entries,
				"total_bytes": totalBytes,
				"total_lines": sumLines(entries),
			},
		}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_pod_logs_tail"),
		function.WithDescription(
			"BCS Pod 日志拉取工具（只读，不走 HITL）。"+
				"支持单 Pod / 多 Pod 批量 / 多 container / previous（上次崩溃日志）/ timestamps / tail_lines / since_seconds。"+
				"典型用法：1) CrashLoopBackOff 排查：previous=true 看上次崩溃；"+
				"2) 多副本对比：pods=[a,b,c] 一次拉所有副本；"+
				"3) 时间窗口聚焦：since_seconds=300 只看最近 5 分钟。"+
				"⚠ 不支持 follow/stream（LLM 场景不适用，要实时监听请走 async-tool 模式）。"+
				"⚠ 单段日志超 256KB 会被截断并标记 truncated=true；tail_lines 超过 5000 会硬拒（防上下文爆炸）。",
		),
	)
}

// fetchOnePodLog 拉取单个 (pod, container) 的日志。
//
// 设计决策：失败不中断批量 —— 让单个 Pod 不存在 / 权限不足 / 超时等场景**不连累**
// 其他 entry 返回结果。错误信息写到 entry.Error 里由上层决策。这符合诊断场景的直觉：
// "3 个副本有 1 个被删了，另外 2 个的日志我还是想看"。
func fetchOnePodLog(ctx context.Context, client *bcsapi.Client, in PodLogsTailInput,
	pod, container string, tail int, isMock bool) PodLogEntry {

	entry := PodLogEntry{Pod: pod, Container: container}

	if isMock {
		entry.Content = fmt.Sprintf("[mock] %s/%s/%s container=%s tail=%d\n"+
			"2026-04-23T17:14:10Z INFO starting server on :8080\n"+
			"2026-04-23T17:14:11Z INFO listening successfully\n"+
			"2026-04-23T17:14:12Z ERROR example error message for mock mode\n",
			in.ClusterID, in.Namespace, pod, container, tail)
		entry.Bytes = len(entry.Content)
		entry.Lines = strings.Count(entry.Content, "\n")
		return entry
	}

	path := fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/pods/%s/log",
		in.ClusterID, in.Namespace, pod)
	query := buildLogsQuery(container, tail, in.SinceSeconds, in.Previous, in.Timestamps)

	raw, err := client.GetRaw(ctx, path, query, MaxLogBytes)
	if err != nil {
		if errors.Is(err, bcsapi.ErrMockMode) {
			// 理论走不到（isMock 分支已拦截），兜底
			entry.Content = "(mock mode)"
			entry.Bytes = len(entry.Content)
			return entry
		}
		// 对 K8s 多容器未指定的情况做友好翻译
		if strings.Contains(err.Error(), "a container name must be specified") {
			entry.Error = "Pod 含多容器但未指定 container；请在 containers[] 里点名（可先用 bcs_resource_query 查看 Pod 的 containers）"
		} else {
			entry.Error = err.Error()
		}
		return entry
	}

	content := string(raw)
	entry.Content = content
	entry.Bytes = len(content)
	entry.Lines = strings.Count(content, "\n")
	// GetRaw 在超限时会在末尾追加 "\n...(truncated)"
	if strings.HasSuffix(content, "...(truncated)") {
		entry.Truncated = true
	}
	return entry
}

// buildLogsQuery 构造 K8s logs API 的 query 参数。
//
// K8s 官方约定：
//   - tailLines     int           返回最后 N 行
//   - sinceSeconds  int           只看最近 N 秒（与 sinceTime 二选一）
//   - previous      bool          上一次容器实例的日志
//   - timestamps    bool          行前加 RFC3339Nano 时间戳
//   - container     string        多容器 Pod 必须指定
func buildLogsQuery(container string, tail, sinceSec int, previous, timestamps bool) map[string]string {
	q := map[string]string{
		"tailLines": strconv.Itoa(tail),
	}
	if container != "" {
		q["container"] = container
	}
	if sinceSec > 0 {
		q["sinceSeconds"] = strconv.Itoa(sinceSec)
	}
	if previous {
		q["previous"] = "true"
	}
	if timestamps {
		q["timestamps"] = "true"
	}
	return q
}

// emitPodLogsTailAudit 审计入账。
//
// 只读也审计的理由：
//   - 合规性：某些场景下日志包含 PII / 业务敏感信息，需要审计谁查看过
//   - 故障复盘：回溯"故障当时谁先看了哪些 pod 的日志"有助于改进 runbook
//   - 容量监控：异常高频的日志拉取可能指示"agent 卡在某个诊断循环里"
func emitPodLogsTailAudit(client *bcsapi.Client, in PodLogsTailInput, pods []string, entries []PodLogEntry) {
	totalBytes, truncatedCnt, errCnt := 0, 0, 0
	for _, e := range entries {
		totalBytes += e.Bytes
		if e.Truncated {
			truncatedCnt++
		}
		if e.Error != "" {
			errCnt++
		}
	}
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   "bcs.pod.logs_tail",
		Severity: "Info", // 只读操作 —— 不占 High/Critical 档
		Target:   fmt.Sprintf("%s/%s (pods=%d)", in.ClusterID, in.Namespace, len(pods)),
		Params: map[string]any{
			"cluster_id":    in.ClusterID,
			"namespace":     in.Namespace,
			"pods":          pods,
			"containers":    in.Containers,
			"tail_lines":    in.TailLines,
			"since_seconds": in.SinceSeconds,
			"previous":      in.Previous,
			"timestamps":    in.Timestamps,
			"total_bytes":   totalBytes,
			"truncated":     truncatedCnt,
			"errors":        errCnt,
		},
		Success: errCnt == 0,
		Mock:    client != nil && client.IsMock(),
	})
}

// sumLines 聚合 entries 行数，用于 Result.Data.total_lines。
func sumLines(entries []PodLogEntry) int {
	s := 0
	for _, e := range entries {
		s += e.Lines
	}
	return s
}
