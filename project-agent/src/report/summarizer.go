// Package report 的 Summarizer：把结构化 Report 翻译成"人话" Outcome 段落（D16）。
//
// 为什么要 Summarizer：
//   - D15 Webhook 落 Outcome 时用的是固定字符串（"Agent 已完成自动处置..."），
//     上游（TAPD 评论 / 站内通告）对这种模板化输出无感。
//   - 升级为"根据 Timeline + Actions 的语义概括"，才能直接作为 TAPD 回填文案。
//
// 设计决策（与 D14 JudgeClient 同构）：
//  1. **接口 + Mock + 真实实现分离**：`SummarizerClient` 接口立约，`MockSummarizer`
//     保证离线/CI 确定性；真实 LLM 实现（OpenAI / 混元）由后续在 `summarizer_openai.go`
//     用 build tag 接入。
//  2. **ctx 强制传递**：所有真实 Summarizer 都有网络调用，必须能被 Webhook 的
//     AsyncTimeout 取消；Mock 也遵守 ctx 语义以免单测漏掉 ctx 泄漏。
//  3. **Nil-safety**：调用方（Webhook）可以不配 Summarizer；`WithSummarizer(nil)`
//     是合法降级，FallbackOutcome 会用 D15 的模板字符串。
package report

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SummarizerClient 把 Report 总结为人话 Outcome 文本。
//
// 返回的字符串应满足：
//   - 单段 < 500 字，适合直接作为 TAPD 评论 / 站内通告头部；
//   - 开头一句下结论（成功 / 失败 / 部分成功），紧跟关键事实；
//   - 避免 markdown 重格式；下游渲染器负责包装。
type SummarizerClient interface {
	Summarize(ctx context.Context, r Report) (string, error)
}

// MockSummarizer 零依赖确定性实现；
// 用"分类 Actions → 统计动词频次 → 拼装模板"的轻量策略生成自然语言。
type MockSummarizer struct {
	// SuccessPrefix 成功路径的开头（默认 "本次事件已自动收敛。"）。
	SuccessPrefix string
	// FailurePrefix 失败路径的开头（默认 "本次事件未完全闭环。"）。
	FailurePrefix string
	// PartialPrefix 部分成功（有失败 Action 但非全失败）开头。
	PartialPrefix string
	// EmptyPrefix Actions 为空时的开头（默认 "Agent 已接收告警，未执行写操作。"）。
	EmptyPrefix string
}

// NewMockSummarizer 用默认前缀构造。
func NewMockSummarizer() *MockSummarizer {
	return &MockSummarizer{
		SuccessPrefix: "本次事件已自动收敛。",
		FailurePrefix: "本次事件未完全闭环。",
		PartialPrefix: "本次事件已部分处置。",
		EmptyPrefix:   "Agent 已接收告警，未执行写操作。",
	}
}

// Summarize 实现 SummarizerClient。Mock 路径不需要 ctx，但保留接口一致性。
func (m *MockSummarizer) Summarize(_ context.Context, r Report) (string, error) {
	if m == nil {
		return "", errors.New("nil summarizer")
	}
	totals := countActions(r.Actions)
	var sb strings.Builder
	sb.WriteString(m.pickPrefix(totals))

	// 1) 诊断结论（如有）
	if diag := strings.TrimSpace(r.Diagnosis); diag != "" {
		sb.WriteString("根因：")
		sb.WriteString(trimTo(diag, 120))
		if !strings.HasSuffix(diag, "。") && !strings.HasSuffix(diag, ".") {
			sb.WriteString("。")
		}
	}

	// 2) 修复动作概述
	if totals.total > 0 {
		sb.WriteString("共执行 ")
		fmt.Fprintf(&sb, "%d 个写操作", totals.total)
		// 历史 bug（D15）：原实现仅在 success>0 时才渲染括号里的明细，导致"全失败"
		// 场景摘要里缺少"失败 N"，TAPD 回填信息不完整、值班人无法一眼看出失败量。
		// 现改为：只要 success/failure/skipped 任一 > 0 就渲染，三者用中文顿号按固定
		// 顺序拼接，保持跨语言可读性与测试确定性。
		if segs := countSegments(totals); len(segs) > 0 {
			sb.WriteString("（")
			sb.WriteString(strings.Join(segs, "、"))
			sb.WriteString("）")
		}
		sb.WriteString("，")
		sb.WriteString(joinTopActions(r.Actions, 3))
		sb.WriteString("。")
	}

	// 3) 关联资源提醒（MR / TAPD / dashboard）
	if refs := formatRefs(r.References); refs != "" {
		sb.WriteString("相关资源：")
		sb.WriteString(refs)
		sb.WriteString("。")
	}

	// 4) 失败时点出主因
	if totals.failure > 0 {
		if firstErr := firstErrorMsg(r.Actions); firstErr != "" {
			sb.WriteString("首个失败原因：")
			sb.WriteString(trimTo(firstErr, 80))
			sb.WriteString("。")
		}
	}

	return strings.TrimSpace(sb.String()), nil
}

// pickPrefix 根据成功/失败配比选前缀。
func (m *MockSummarizer) pickPrefix(c actionCounts) string {
	switch {
	case c.total == 0:
		return m.EmptyPrefix
	case c.failure == 0:
		return m.SuccessPrefix
	case c.success == 0:
		return m.FailurePrefix
	default:
		return m.PartialPrefix
	}
}

// actionCounts Actions 分类统计。
type actionCounts struct{ total, success, failure, skipped int }

func countActions(actions []Action) actionCounts {
	var c actionCounts
	for _, a := range actions {
		c.total++
		switch strings.ToLower(a.Result) {
		case "success":
			c.success++
		case "failure", "failed", "error":
			c.failure++
		case "skipped", "skip":
			c.skipped++
		}
	}
	return c
}

// countSegments 把 actionCounts 的非零分量按"成功 → 失败 → 跳过"固定顺序
// 翻译成可直接用顿号 join 的中文片段。
//
// 独立出来的两个理由：
//  1. 未来无论是 MockSummarizer 还是真实 LLM Summarizer，"确定性事实片段"
//     都可以共享这段；短语格式只需在一处维护。
//  2. 按固定顺序输出保证测试断言（如 Contains "失败 2"）稳定，不会因 map
//     遍历顺序不稳定而 flaky。
func countSegments(c actionCounts) []string {
	segs := make([]string, 0, 3)
	if c.success > 0 {
		segs = append(segs, fmt.Sprintf("成功 %d", c.success))
	}
	if c.failure > 0 {
		segs = append(segs, fmt.Sprintf("失败 %d", c.failure))
	}
	if c.skipped > 0 {
		segs = append(segs, fmt.Sprintf("跳过 %d", c.skipped))
	}
	return segs
}

// joinTopActions 拼接最常见的前 n 个 Action 名，便于摘要可读。
func joinTopActions(actions []Action, n int) string {
	if len(actions) == 0 {
		return ""
	}
	freq := map[string]int{}
	for _, a := range actions {
		key := a.Action
		if key == "" {
			continue
		}
		freq[key]++
	}
	if len(freq) == 0 {
		return ""
	}
	type kv struct {
		name  string
		count int
	}
	pairs := make([]kv, 0, len(freq))
	for k, v := range freq {
		pairs = append(pairs, kv{k, v})
	}
	// 按频次降序、名字升序（保证确定性）
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].name < pairs[j].name
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	items := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p.count > 1 {
			items = append(items, fmt.Sprintf("%s × %d", p.name, p.count))
		} else {
			items = append(items, p.name)
		}
	}
	return "主要动作：" + strings.Join(items, " / ")
}

// formatRefs 把 References 压成"kind:title"序列，便于摘要引用。
func formatRefs(refs []Reference) string {
	if len(refs) == 0 {
		return ""
	}
	items := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Title == "" && r.URL == "" {
			continue
		}
		name := r.Title
		if name == "" {
			name = r.URL
		}
		items = append(items, r.Kind+":"+name)
	}
	return strings.Join(items, "; ")
}

// firstErrorMsg 返回第一条 Action 的 ErrorMsg，空字符串表示无。
func firstErrorMsg(actions []Action) string {
	for _, a := range actions {
		if strings.TrimSpace(a.ErrorMsg) != "" {
			return a.ErrorMsg
		}
	}
	return ""
}

// trimTo 保证单段不超过 n 个 rune（按 rune 切），避免 ASCII 场景下 n=0 越界。
func trimTo(s string, n int) string {
	if n <= 0 {
		return s
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// SummarizeOrFallback 是 Webhook 场景下的安全调用入口：
//   - client 为 nil：直接返回 fallback
//   - client.Summarize 返回 error：打日志并返回 fallback
//
// 这样 Webhook 侧零心智负担，不需要自己做 nil 检查与 error 分支。
func SummarizeOrFallback(
	ctx context.Context,
	client SummarizerClient,
	r Report,
	fallback string,
	logger func(format string, args ...any),
) string {
	if client == nil {
		return fallback
	}
	out, err := client.Summarize(ctx, r)
	if err != nil {
		if logger != nil {
			logger("[summarizer] fallback due to error: %v", err)
		}
		return fallback
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return fallback
	}
	return out
}
