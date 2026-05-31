// Package report 生成修复闭环（Repair Flow）的结构化报告。
//
// 设计目标（D15）：
//  1. 数据模型稳定：可序列化 JSON，便于下游（TAPD 回填 / 站内通告 / 存档）消费。
//  2. 多格式渲染：同一份 Report 能同时输出 Markdown（人看）与 JSON（机看）。
//  3. 零 LLM 依赖：Builder 仅做字段聚合 + 时间轴排序，不调用大模型总结，
//     避免 Webhook 场景下的延迟抖动；LLM 总结留给上层可选增强。
//  4. 聚合 audit.Record：修复路径上的所有写操作天然是高价值时间轴事件，
//     FromAuditRecords 直接把 audit jsonl 聚合为 Report.Timeline。
//
// 典型用法：
//
//	b := report.NewBuilder("case-20260421-oom-01")
//	b.SetTitle("game-core OOM 重启事件").
//	    SetBackground("凌晨 03:12—03:41 gamesvr Pod 连续重启 3 次").
//	    SetDiagnosis("Old Gen 内存持续攀升至 95%，Full GC 停顿超 30s").
//	    AddAction("bcs.helm.upgrade", "提升 -Xmx 至 12G", "success").
//	    SetOutcome("重启消除，RT 恢复基线").
//	    AppendTimelineFromAudit(memSink.Snapshot())
//	md, _ := b.Render(report.FormatMarkdown)
//	js, _ := b.Render(report.FormatJSON)
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
)

// Format 渲染格式枚举。
type Format string

// 支持的渲染格式。
const (
	FormatMarkdown Format = "markdown"
	FormatJSON     Format = "json"
)

// Severity 与 audit/hitl 保持一致，便于聚合显示。
type Severity string

// 支持的 Severity 等级。
const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Action 一条修复动作的结构化记录。
type Action struct {
	// Action 动作名，与 hitl.Plan.Action / audit.Record.Action 对齐。
	Action string `json:"action"`
	// Description 人类可读的动作说明（例如 "提升 -Xmx 至 12G"）。
	Description string `json:"description"`
	// Target 作用对象（可选，默认取自 audit.Target）。
	Target string `json:"target,omitempty"`
	// Severity 破坏等级。
	Severity Severity `json:"severity,omitempty"`
	// Result 执行结果：success / failure / skipped。
	Result string `json:"result"`
	// Operator 操作人（由 audit.User 透传）。
	Operator string `json:"operator,omitempty"`
	// TS 执行时间（RFC3339）。
	TS string `json:"ts,omitempty"`
	// ErrorMsg 失败时的错误说明。
	ErrorMsg string `json:"error,omitempty"`
	// Mock 是否走 Mock 通道（true 表示未真正打到线上 API）。
	Mock bool `json:"mock,omitempty"`
}

// TimelineItem 时间轴节点，统一承载告警 / 诊断推进 / 修复动作 / 人工决策等。
type TimelineItem struct {
	TS       string `json:"ts"`
	Actor    string `json:"actor,omitempty"` // coordinator / diagnosis_agent / repair_agent / user / system
	Kind     string `json:"kind"`            // alarm / diagnosis / action / gate / outcome / note
	Message  string `json:"message"`
	Severity Severity `json:"severity,omitempty"`
}

// Report 修复报告的完整数据模型。
//
// 所有字段均可为空：调用方按需 Set，Render 内部对缺失段落做优雅降级。
type Report struct {
	// CaseID 报告唯一 ID，建议 "case-YYYYMMDD-<symptom>-<seq>" 格式。
	CaseID string `json:"case_id"`
	// Title 报告标题（人看）。
	Title string `json:"title"`
	// Severity 报告整体定级。
	Severity Severity `json:"severity,omitempty"`
	// Background 背景 / 现象描述。
	Background string `json:"background,omitempty"`
	// Diagnosis 诊断结论（根因 + 关键证据）。
	Diagnosis string `json:"diagnosis,omitempty"`
	// Actions 修复动作列表（含成功失败，按 TS 升序）。
	Actions []Action `json:"actions,omitempty"`
	// Timeline 时间轴（告警 / 诊断 / 动作 / 门禁决策），按 TS 升序。
	Timeline []TimelineItem `json:"timeline,omitempty"`
	// Outcome 最终结论（问题是否消除、剩余风险）。
	Outcome string `json:"outcome,omitempty"`
	// References 关联 MR / 流水线 / TAPD 单号等外部资源链接。
	References []Reference `json:"references,omitempty"`
	// GeneratedAt 报告生成时间（RFC3339）。
	GeneratedAt string `json:"generated_at"`
	// Version 报告 schema 版本，便于未来兼容演进。
	Version string `json:"version"`
}

// Reference 外部资源引用。
type Reference struct {
	Kind  string `json:"kind"` // mr / pipeline / tapd / dashboard / knowledge
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
}

// SchemaVersion 当前 Report schema 版本号。
const SchemaVersion = "v1"

// Builder 用于增量拼装 Report，提供链式 API。
type Builder struct {
	r Report
}

// NewBuilder 创建一个 Builder。caseID 若为空，使用 "case-<unix>"。
func NewBuilder(caseID string) *Builder {
	if strings.TrimSpace(caseID) == "" {
		caseID = fmt.Sprintf("case-%d", time.Now().Unix())
	}
	return &Builder{r: Report{CaseID: caseID, Version: SchemaVersion}}
}

// SetTitle 设置标题，返回自身支持链式调用。
func (b *Builder) SetTitle(s string) *Builder { b.r.Title = s; return b }

// SetSeverity 设置整体定级。
func (b *Builder) SetSeverity(s Severity) *Builder { b.r.Severity = s; return b }

// SetBackground 设置背景。
func (b *Builder) SetBackground(s string) *Builder { b.r.Background = s; return b }

// SetDiagnosis 设置诊断结论。
func (b *Builder) SetDiagnosis(s string) *Builder { b.r.Diagnosis = s; return b }

// SetOutcome 设置最终结论。
func (b *Builder) SetOutcome(s string) *Builder { b.r.Outcome = s; return b }

// AddAction 追加一条修复动作；会同时入 Timeline。
func (b *Builder) AddAction(a Action) *Builder {
	if a.TS == "" {
		a.TS = time.Now().Format(time.RFC3339)
	}
	if a.Result == "" {
		a.Result = "success"
	}
	b.r.Actions = append(b.r.Actions, a)
	b.r.Timeline = append(b.r.Timeline, TimelineItem{
		TS:       a.TS,
		Actor:    "repair_agent",
		Kind:     "action",
		Severity: a.Severity,
		Message:  formatActionMessage(a),
	})
	return b
}

// AddTimeline 追加一条时间轴（非 action 类）。
func (b *Builder) AddTimeline(item TimelineItem) *Builder {
	if item.TS == "" {
		item.TS = time.Now().Format(time.RFC3339)
	}
	if item.Kind == "" {
		item.Kind = "note"
	}
	b.r.Timeline = append(b.r.Timeline, item)
	return b
}

// AddReference 追加一个外部资源引用。
func (b *Builder) AddReference(ref Reference) *Builder {
	b.r.References = append(b.r.References, ref)
	return b
}

// AppendTimelineFromAudit 从 audit jsonl 字节切片列表聚合时间轴。
//
// 每一行都是一条 audit.Record。解析失败的行会被静默跳过，不打断聚合；
// 这符合"尽力而为"的报告语义：报告用于复盘，不能因为个别脏数据阻塞。
func (b *Builder) AppendTimelineFromAudit(lines [][]byte) *Builder {
	for _, line := range lines {
		line = trimTrailingNewline(line)
		if len(line) == 0 {
			continue
		}
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		b.appendAuditRecord(rec)
	}
	return b
}

// AppendAuditRecords 直接从 audit.Record 列表聚合（测试友好）。
func (b *Builder) AppendAuditRecords(recs []audit.Record) *Builder {
	for _, rec := range recs {
		b.appendAuditRecord(rec)
	}
	return b
}

// appendAuditRecord 将单条 audit.Record 翻译为 Action + TimelineItem。
func (b *Builder) appendAuditRecord(rec audit.Record) {
	action := Action{
		Action:      rec.Action,
		Description: formatAuditDescription(rec),
		Target:      rec.Target,
		Severity:    Severity(rec.Severity),
		Result:      rec.Result,
		Operator:    rec.User,
		TS:          rec.TS,
		ErrorMsg:    rec.ErrorMsg,
		Mock:        rec.Mock,
	}
	b.r.Actions = append(b.r.Actions, action)
	b.r.Timeline = append(b.r.Timeline, TimelineItem{
		TS:       rec.TS,
		Actor:    defaultStr(rec.Agent, "repair_agent"),
		Kind:     "action",
		Severity: Severity(rec.Severity),
		Message:  formatActionMessage(action),
	})
}

// Build 冻结 Builder，返回排序后的 Report 副本。
func (b *Builder) Build() Report {
	out := b.r
	sort.SliceStable(out.Actions, func(i, j int) bool {
		return out.Actions[i].TS < out.Actions[j].TS
	})
	sort.SliceStable(out.Timeline, func(i, j int) bool {
		return out.Timeline[i].TS < out.Timeline[j].TS
	})
	if out.GeneratedAt == "" {
		out.GeneratedAt = time.Now().Format(time.RFC3339)
	}
	if out.Version == "" {
		out.Version = SchemaVersion
	}
	return out
}

// Render 以指定格式输出报告字节串。
func (b *Builder) Render(format Format) ([]byte, error) {
	r := b.Build()
	switch format {
	case FormatMarkdown:
		return []byte(RenderMarkdown(r)), nil
	case FormatJSON:
		return json.MarshalIndent(r, "", "  ")
	default:
		return nil, fmt.Errorf("unsupported report format: %q", format)
	}
}

// Render 独立函数形式，便于外部直接对 Report 渲染（无 Builder 场景）。
func Render(r Report, format Format) ([]byte, error) {
	sort.SliceStable(r.Actions, func(i, j int) bool {
		return r.Actions[i].TS < r.Actions[j].TS
	})
	sort.SliceStable(r.Timeline, func(i, j int) bool {
		return r.Timeline[i].TS < r.Timeline[j].TS
	})
	if r.Version == "" {
		r.Version = SchemaVersion
	}
	switch format {
	case FormatMarkdown:
		return []byte(RenderMarkdown(r)), nil
	case FormatJSON:
		return json.MarshalIndent(r, "", "  ")
	default:
		return nil, fmt.Errorf("unsupported report format: %q", format)
	}
}

// ErrEmptyReport 当 Render 的 Report 完全为空时返回。
var ErrEmptyReport = errors.New("empty report")

// formatActionMessage 把 Action 翻译为单行时间轴消息。
func formatActionMessage(a Action) string {
	var sb strings.Builder
	sb.WriteString("[" + strings.ToUpper(a.Result) + "] ")
	sb.WriteString(a.Action)
	if a.Target != "" {
		sb.WriteString(" → " + a.Target)
	}
	if a.Description != "" {
		sb.WriteString("（" + a.Description + "）")
	}
	if a.ErrorMsg != "" {
		sb.WriteString("，err=" + a.ErrorMsg)
	}
	if a.Mock {
		sb.WriteString(" [MOCK]")
	}
	return sb.String()
}

// formatAuditDescription 给 Action.Description 提供兜底文本。
func formatAuditDescription(rec audit.Record) string {
	if rec.Reason != "" {
		return rec.Reason
	}
	if len(rec.Params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(rec.Params))
	for k := range rec.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s=%v", k, rec.Params[k])
	}
	return sb.String()
}

func defaultStr(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func trimTrailingNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
