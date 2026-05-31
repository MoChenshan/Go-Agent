// Package sse 的请求/响应类型定义。
//
// 参考 oncall_agent/services/sse/types.go，简化掉企微特殊字段。
package sse

import (
	"encoding/json"
	"fmt"
	"time"
)

// DefaultUserID 匿名用户默认 ID。
const DefaultUserID = "default_user"

// Request SSE 请求参数。
type Request struct {
	User      string `json:"user"`       // 用户 ID
	Content   string `json:"content"`    // 用户消息文本
	SessionID string `json:"session_id"` // 会话 ID
}

// GetUserID 获取用户 ID，为空时返回默认值。
func (req Request) GetUserID() string {
	if req.User != "" {
		return req.User
	}
	return DefaultUserID
}

// GetSessionID 获取会话 ID，为空时基于 UserID + 日期构造。
func (req Request) GetSessionID() string {
	if req.SessionID != "" {
		return req.SessionID
	}
	return req.GetUserID() + "_" + time.Now().Format("20060102")
}

// Response SSE 响应体。
type Response struct {
	// EventName SSE 事件名（可选；为空时默认为 "delta"）。
	//
	// D7 起引入，用于区分：
	//   - "delta"                 普通内容增量
	//   - "agent_transfer"        Coordinator/子 Agent 间 Transfer
	//   - "tool_call"             工具开始调用（可视化）
	//   - "confirmation_required" HITL 等待人工确认（前端应弹确认 UI）
	//   - "final"                 本轮结束
	//   - "error"                 出错
	EventName string `json:"-"`
	Data      Data   `json:"data"`
}

// Data SSE 响应主体。
type Data struct {
	Response     string       `json:"response"`
	Finished     bool         `json:"finished"`
	GlobalOutput GlobalOutput `json:"global_output"`

	// 以下为 D7 新增的可选字段，便于前端结构化渲染，
	// 省略时不影响旧前端兼容（JSON 序列化会 omitempty）。
	EventType string          `json:"event_type,omitempty"` // 同 Response.EventName，冗余放入 data 便于前端解析
	Author    string          `json:"author,omitempty"`     // 当前说话的 Agent 名（coordinator / diagnosis_agent / ...）
	ToolCall  *ToolCallInfo   `json:"tool_call,omitempty"`  // 工具调用（event_type=tool_call 时）
	Transfer  *TransferInfo   `json:"transfer,omitempty"`   // Transfer 信息（event_type=agent_transfer 时）
	Confirm   *ConfirmPayload `json:"confirmation,omitempty"` // HITL 确认请求（event_type=confirmation_required 时）
}

// ToolCallInfo 工具调用元信息（用于前端可视化）。
type ToolCallInfo struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"` // Debug 模式填充
}

// TransferInfo Agent 切换元信息。
type TransferInfo struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// ConfirmPayload HITL 确认载荷，直接透传 hitl.PendingResult 关键字段。
// 前端收到后应该高亮渲染 human_prompt 并弹出「确认 / 取消」按钮。
type ConfirmPayload struct {
	Action      string         `json:"action"`
	Severity    string         `json:"severity"`
	Target      string         `json:"target"`
	SideEffect  string         `json:"side_effect,omitempty"`
	ImpactScope string         `json:"impact_scope,omitempty"`
	Rollback    string         `json:"rollback,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
	HumanPrompt string         `json:"human_prompt"`
}

// GlobalOutput 全局输出（便于前端统一渲染）。
type GlobalOutput struct {
	Context       string `json:"context"`
	AnswerSuccess int    `json:"answer_success"`
	Docs          []Doc  `json:"docs"`
}

// Doc 引用文档元信息。
type Doc struct {
	DocID   string  `json:"doc_id"`
	SpaceID string  `json:"space_id"`
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Score   float64 `json:"score"`
}

// String 序列化为 SSE 协议格式：`event:<name>\ndata:{...}\n\n`。
//
// EventName 为空时默认 "delta"，兼容既有前端。
func (r Response) String() string {
	if r.Data.GlobalOutput.Docs == nil {
		r.Data.GlobalOutput.Docs = []Doc{}
	}
	name := r.EventName
	if name == "" {
		name = "delta"
	}
	// data.event_type 与 SSE event 名保持一致，便于前端不解析 event name 直接从 payload 里读。
	if r.Data.EventType == "" {
		r.Data.EventType = name
	}
	payload, _ := json.Marshal(r.Data)
	return fmt.Sprintf("event:%s\ndata:%s\n\n", name, string(payload))
}
