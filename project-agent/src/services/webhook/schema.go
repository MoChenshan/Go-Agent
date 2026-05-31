// Package webhook 是 D15 的外部告警入口：
//
//	POST /webhook/bk_alarm  → 蓝鲸告警 Push
//	POST /webhook/tapd      → TAPD 新 Bug / 状态变更 Push
//
// 设计目标：
//  1. **异步化**：Webhook 必须在 1~3 秒内回 200，上游（蓝鲸/TAPD）才不会重试打爆。
//     因此内部把 Coordinator 调用放进后台 goroutine，HTTP 直接返回 {accepted:true, case_id}。
//  2. **签名校验**：默认开启 HMAC-SHA256（Header: `X-Signature: sha256=<hex>`）；
//     `WEBHOOK_VERIFY_SIG=0` 显式关闭（调试 / demo 用）。
//  3. **Schema 约束**：两条链路各定义一份 Go struct，多余字段按 json 忽略。
//  4. **结果沉淀**：后台任务完成后把报告交给 Report Store（内存实现），
//     /v1/report/{case_id} 可以直接拉回 Markdown/JSON 两种格式。
package webhook

// —— 蓝鲸监控告警 Webhook Payload —————————————————————————————————————————
// 参考蓝鲸监控自定义 Webhook 规范（简化版，保留报告聚合所需字段）。

// BKAlarmPayload 蓝鲸监控告警 Webhook 载荷。
type BKAlarmPayload struct {
	// AlarmID 告警唯一 ID（用于去重）。
	AlarmID string `json:"alarm_id"`
	// AlarmName 告警策略名。
	AlarmName string `json:"alarm_name"`
	// Severity critical / high / medium / low。
	Severity string `json:"severity"`
	// StartTime 告警首次触发时间（RFC3339 / 秒时间戳字符串均可）。
	StartTime string `json:"start_time"`
	// EndTime 告警结束时间，持续告警可为空。
	EndTime string `json:"end_time,omitempty"`
	// BizID 业务 ID（BK CMDB BizID）。
	BizID int64 `json:"biz_id,omitempty"`
	// Module / Service / Instance 粒度标签，用于定位服务。
	Module   string `json:"module,omitempty"`
	Service  string `json:"service,omitempty"`
	Instance string `json:"instance,omitempty"`
	// Description 告警文案（人看），供 Coordinator 首轮输入。
	Description string `json:"description"`
	// Metric 触发的指标名（memory.usage / cpu.load 等，可选）。
	Metric string `json:"metric,omitempty"`
	// CurrentValue 当前指标值，触发阈值（可选）。
	CurrentValue float64 `json:"current_value,omitempty"`
	Threshold    float64 `json:"threshold,omitempty"`
	// DashboardURL 告警对应大盘链接（作为 Report.Reference 附上）。
	DashboardURL string `json:"dashboard_url,omitempty"`
}

// Prompt 把蓝鲸告警翻译为用户消息，用于喂给 Coordinator。
func (p BKAlarmPayload) Prompt() string {
	if p.Description != "" {
		if p.Service != "" {
			return "[蓝鲸告警] " + p.Service + "：" + p.Description
		}
		return "[蓝鲸告警] " + p.Description
	}
	if p.AlarmName != "" {
		return "[蓝鲸告警] " + p.AlarmName
	}
	return "[蓝鲸告警] 未命名告警"
}

// CaseTitle 归纳为报告标题。
func (p BKAlarmPayload) CaseTitle() string {
	switch {
	case p.AlarmName != "" && p.Service != "":
		return p.AlarmName + " — " + p.Service
	case p.AlarmName != "":
		return p.AlarmName
	case p.Description != "":
		return p.Description
	}
	return "蓝鲸告警"
}

// —— TAPD Webhook Payload ————————————————————————————————————————————————
// 参考 TAPD Webhook 文档（https://www.tapd.cn/help/show#1120003271001000019），
// 仅保留我们需要的字段；未声明字段由 Go 的 json.Unmarshal 自动忽略。

// TAPDPayload TAPD Webhook 载荷。
type TAPDPayload struct {
	// Event 事件类型：bug_create / bug_update / story_update …
	Event string `json:"event"`
	// WorkspaceID 项目 ID。
	WorkspaceID string `json:"workspace_id"`
	// Bug 对应 Bug 结构（bug_create / bug_update 时非空）。
	Bug *TAPDBug `json:"bug,omitempty"`
}

// TAPDBug TAPD Bug 简化结构，字段对齐 tapdapi.Bug。
type TAPDBug struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Status      string `json:"status,omitempty"`
	Module      string `json:"module,omitempty"`
	Reporter    string `json:"reporter,omitempty"`
	URL         string `json:"url,omitempty"`
}

// Prompt 把 TAPD 事件翻译为用户消息。
func (p TAPDPayload) Prompt() string {
	if p.Bug == nil {
		return "[TAPD] " + p.Event
	}
	title := p.Bug.Title
	if title == "" {
		title = "BUG-" + p.Bug.ID
	}
	desc := p.Bug.Description
	if desc == "" {
		return "[TAPD Bug] " + title
	}
	return "[TAPD Bug] " + title + "：" + desc
}

// CaseTitle 归纳为报告标题。
func (p TAPDPayload) CaseTitle() string {
	if p.Bug != nil && p.Bug.Title != "" {
		return "TAPD/" + p.Bug.Title
	}
	return "TAPD 事件"
}
