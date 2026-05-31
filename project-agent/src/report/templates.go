// Package report 的 Markdown 渲染器。
//
// 模板分 6 段：
//  1. 标题 + 基本信息（CaseID / 定级 / 生成时间）
//  2. 背景（Background）
//  3. 诊断结论（Diagnosis）
//  4. 修复动作（Actions 表格）
//  5. 时间轴（Timeline 列表）
//  6. 结论 + 关联资源（Outcome + References）
//
// 空段落会被跳过；保证即使只有 CaseID 也能渲染出合法 Markdown。
package report

import (
	"fmt"
	"strings"
)

// RenderMarkdown 输出人类可读的 Markdown 报告。
func RenderMarkdown(r Report) string {
	var sb strings.Builder

	title := r.Title
	if title == "" {
		title = r.CaseID
	}
	fmt.Fprintf(&sb, "# 修复报告 — %s\n\n", sanitizeInline(title))

	// —— 基本信息 ——
	sb.WriteString("| 字段 | 值 |\n")
	sb.WriteString("|------|------|\n")
	fmt.Fprintf(&sb, "| Case ID | `%s` |\n", sanitizeInline(r.CaseID))
	if r.Severity != "" {
		fmt.Fprintf(&sb, "| 定级 | **%s** |\n", strings.ToUpper(string(r.Severity)))
	}
	if r.GeneratedAt != "" {
		fmt.Fprintf(&sb, "| 生成时间 | %s |\n", sanitizeInline(r.GeneratedAt))
	}
	fmt.Fprintf(&sb, "| Schema 版本 | %s |\n", sanitizeInline(r.Version))
	sb.WriteString("\n")

	// —— 背景 ——
	if r.Background != "" {
		sb.WriteString("## 一、背景\n\n")
		sb.WriteString(sanitizeBlock(r.Background))
		sb.WriteString("\n\n")
	}

	// —— 诊断 ——
	if r.Diagnosis != "" {
		sb.WriteString("## 二、诊断结论\n\n")
		sb.WriteString(sanitizeBlock(r.Diagnosis))
		sb.WriteString("\n\n")
	}

	// —— 修复动作表 ——
	if len(r.Actions) > 0 {
		sb.WriteString("## 三、修复动作\n\n")
		sb.WriteString("| # | 时间 | Action | Target | Result | Operator | 备注 |\n")
		sb.WriteString("|---|------|--------|--------|--------|----------|------|\n")
		for i, a := range r.Actions {
			note := a.Description
			if a.ErrorMsg != "" {
				note = fmt.Sprintf("%s（err=%s）", note, a.ErrorMsg)
			}
			if a.Mock {
				note = strings.TrimSpace(note + " [MOCK]")
			}
			fmt.Fprintf(&sb, "| %d | %s | `%s` | %s | %s | %s | %s |\n",
				i+1,
				sanitizeInline(a.TS),
				sanitizeInline(a.Action),
				sanitizeInline(a.Target),
				renderResult(a.Result),
				sanitizeInline(a.Operator),
				sanitizeInline(note),
			)
		}
		sb.WriteString("\n")
	}

	// —— 时间轴 ——
	if len(r.Timeline) > 0 {
		sb.WriteString("## 四、时间轴\n\n")
		for _, t := range r.Timeline {
			sev := ""
			if t.Severity != "" {
				sev = fmt.Sprintf(" _[%s]_", strings.ToUpper(string(t.Severity)))
			}
			actor := t.Actor
			if actor == "" {
				actor = "system"
			}
			fmt.Fprintf(&sb, "- **%s** `%s`%s — %s\n",
				sanitizeInline(t.TS),
				sanitizeInline(actor),
				sev,
				sanitizeInline(t.Message),
			)
		}
		sb.WriteString("\n")
	}

	// —— 结论 ——
	if r.Outcome != "" {
		sb.WriteString("## 五、结论\n\n")
		sb.WriteString(sanitizeBlock(r.Outcome))
		sb.WriteString("\n\n")
	}

	// —— 关联资源 ——
	if len(r.References) > 0 {
		sb.WriteString("## 六、关联资源\n\n")
		for _, ref := range r.References {
			title := ref.Title
			if title == "" {
				title = ref.URL
			}
			if ref.URL == "" {
				fmt.Fprintf(&sb, "- [%s] %s\n", sanitizeInline(ref.Kind), sanitizeInline(title))
				continue
			}
			fmt.Fprintf(&sb, "- [%s] [%s](%s)\n",
				sanitizeInline(ref.Kind),
				sanitizeInline(title),
				sanitizeInline(ref.URL),
			)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderResult 把 success/failure/skipped 翻译为可视化标记。
func renderResult(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "success", "ok":
		return "✅ success"
	case "failure", "failed", "error":
		return "❌ failure"
	case "skipped", "skip":
		return "⚠ skipped"
	case "":
		return "—"
	default:
		return sanitizeInline(result)
	}
}

// sanitizeInline 去除可能破坏 Markdown 行内渲染的字符（| 竖线 / 换行）。
func sanitizeInline(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}

// sanitizeBlock 保留换行，仅统一 CRLF → LF。
func sanitizeBlock(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimRight(s, "\n")
}
