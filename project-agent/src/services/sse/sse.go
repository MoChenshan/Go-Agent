// Package sse 提供面向前端的 SSE（Server-Sent Events）流式服务。
//
// 本实现参考 oncall_agent/services/sse 的成熟方案，核心职责：
//   - 接收 POST /v1/agent 请求（JSON body）
//   - 通过 Runner 执行 Agent，订阅 event channel
//   - 将每个事件转为 SSE 消息（丰富事件类型）流式推送
//   - D7 新增事件类型：agent_transfer / confirmation_required / tool_call / error / final
//   - Debug 模式下附加工具参数、Token 使用量等调试信息
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"

	"git.woa.com/trpc-go/gameops-agent/src/observability"
)

// transferToolName trpc-agent-go 内置的 Transfer 工具名（稳定契约）。
// 直接硬编码避免引入 tool/transfer 包带来的 module 顶级依赖。
const transferToolName = "transfer_to_agent"

// API SSE 服务接口。
type API interface {
	// HandleSSE 处理 HTTP 请求，按 SSE 协议流式返回 Agent 响应。
	HandleSSE(w http.ResponseWriter, r *http.Request)
}

// Service SSE 服务实现。
type Service struct {
	appName       string
	debug         bool
	entranceAgent agent.Agent
	agentRunner   runner.Runner
}

// New 构造 SSE 服务。
//
// 参数：
//   - appName：应用名（将传递给 Runner）
//   - entrance：入口 Agent（通常是 Coordinator）
//   - sess：Session 服务（nil 时 Runner 使用默认 inmemory session）
//   - debug：是否开启调试输出
func New(appName string, entrance agent.Agent, sess session.Service, debug bool) *Service {
	var r runner.Runner
	if sess != nil {
		r = runner.NewRunner(appName, entrance, runner.WithSessionService(sess))
	} else {
		r = runner.NewRunner(appName, entrance)
	}
	return &Service{
		appName:       appName,
		debug:         debug,
		entranceAgent: entrance,
		agentRunner:   r,
	}
}

// HandleSSE 处理 SSE 请求。
func (s *Service) HandleSSE(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. 解析请求
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content must not be empty", http.StatusBadRequest)
		return
	}

	// 2. 设置 SSE Header
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 3. 执行 Agent
	eventChan, err := s.agentRunner.Run(
		ctx, req.GetUserID(), req.GetSessionID(),
		model.NewUserMessage(req.Content),
	)
	if err != nil {
		writeSSE(w, flusher, Response{
			EventName: "error",
			Data:      Data{Response: fmt.Sprintf("❌ Runner error: %v", err), Finished: true},
		})
		return
	}

	// 4. 流式转发事件
	s.forward(ctx, w, flusher, eventChan)
}

// forward 将 Runner 事件流转为 SSE 消息。
//
// 事件分流优先级：
//  1. error    → 发 event=error
//  2. transfer → 发 event=agent_transfer（Coordinator ↔ 子 Agent 分发）
//  3. tool_call→ 分两种：transfer_to_agent 跳过（上一步已处理），其他发 event=tool_call
//  4. tool_response → 如果是 HITL PendingResult→发 event=confirmation_required；否则隐藏
//  5. delta    → 发 event=delta（流式文本）
//  6. done     → 发 event=final
func (s *Service) forward(_ context.Context, w http.ResponseWriter, flusher http.Flusher, ch <-chan *event.Event) {
	for ev := range ch {
		if ev == nil {
			continue
		}
		// 1. 错误事件
		if ev.Error != nil {
			writeSSE(w, flusher, Response{
				EventName: "error",
				Data:      Data{Response: fmt.Sprintf("\n❌ Error: %s\n", ev.Error.Message), Author: ev.Author},
			})
			continue
		}
		// 2. Transfer 事件（Coordinator → 子 Agent 或子 Agent 间 Transfer）
		if s.handleTransferEvent(w, flusher, ev) {
			continue
		}
		// 3. 工具调用事件
		if s.handleToolCalls(w, flusher, ev) {
			continue
		}
		// 4. 工具响应事件：HITL PendingResult 要单独上报，其他隐藏
		if isToolResponseEvent(ev) {
			s.handleToolResponse(w, flusher, ev)
			continue
		}
		// 5. 流式内容增量
		s.handleDelta(w, flusher, ev)
		// 6. 最终事件
		if s.handleFinal(w, flusher, ev) {
			return
		}
	}
}

// handleTransferEvent 处理 Agent 间 Transfer。
//
// trpc-agent-go 框架会通过两种方式暴露：
//
//	a) Event.Object == ObjectTypeTransfer（在自定义事件中可能会出现）
//	b) 普通 ToolCall 事件中 function.name == transfer_to_agent
//
// 我们同时兼容两种。
func (s *Service) handleTransferEvent(w http.ResponseWriter, flusher http.Flusher, ev *event.Event) bool {
	// case b：ToolCall 中包含 transfer_to_agent
	if len(ev.Choices) > 0 && len(ev.Choices[0].Message.ToolCalls) > 0 {
		for _, tc := range ev.Choices[0].Message.ToolCalls {
			if tc.Function.Name == transferToolName {
				to, reason := parseTransferArgs(tc.Function.Arguments)
				writeSSE(w, flusher, Response{
					EventName: "agent_transfer",
					Data: Data{
						Response: fmt.Sprintf("\n🔄 分发给 `%s`：%s\n", to, reason),
						Author:   ev.Author,
						Transfer: &TransferInfo{From: ev.Author, To: to, Reason: reason},
					},
				})
				return true
			}
		}
	}
	// case a：Object 显式标记为 Transfer
	if ev.Response != nil && ev.Object == model.ObjectTypeTransfer {
		content := ""
		if len(ev.Response.Choices) > 0 {
			content = ev.Response.Choices[0].Message.Content
		}
		writeSSE(w, flusher, Response{
			EventName: "agent_transfer",
			Data: Data{
				Response: fmt.Sprintf("\n🔄 %s\n", content),
				Author:   ev.Author,
				Transfer: &TransferInfo{From: ev.Author},
			},
		})
		return true
	}
	return false
}

// parseTransferArgs 解析 transfer_to_agent 的参数 JSON。
// 返回：target agent 名、message/reason。
func parseTransferArgs(argsJSON []byte) (string, string) {
	var m map[string]any
	if err := json.Unmarshal(argsJSON, &m); err != nil {
		return "", ""
	}
	to, _ := m["agent_name"].(string)
	reason, _ := m["message"].(string)
	return to, reason
}

// handleToolResponse 尝试从工具响应中发现 HITL PendingResult，
// 如果是则单独上报 event=confirmation_required，否则静默。
func (s *Service) handleToolResponse(w http.ResponseWriter, flusher http.Flusher, ev *event.Event) {
	if ev == nil || ev.Response == nil {
		return
	}
	for _, c := range ev.Response.Choices {
		if c.Message.Role != model.RoleTool {
			continue
		}
		payload := c.Message.Content
		if !strings.Contains(payload, "awaiting_confirmation") || !strings.Contains(payload, "human_prompt") {
			continue
		}
		// 尝试解析为 HITL PendingResult 结构
		var parsed struct {
			OK          bool   `json:"ok"`
			Status      string `json:"status"`
			HumanPrompt string `json:"human_prompt"`
			Plan        struct {
				Action       string         `json:"action"`
				Severity     string         `json:"severity"`
				Target       string         `json:"target"`
				SideEffect   string         `json:"side_effect"`
				ImpactScope  string         `json:"impact_scope"`
				RollbackPlan string         `json:"rollback_plan"`
				Params       map[string]any `json:"params"`
			} `json:"plan"`
		}
		if err := extractPendingResult(payload, &parsed); err != nil || parsed.Status != "awaiting_confirmation" {
			continue
		}
		writeSSE(w, flusher, Response{
			EventName: "confirmation_required",
			Data: Data{
				Response: parsed.HumanPrompt,
				Author:   ev.Author,
				Confirm: &ConfirmPayload{
					Action:      parsed.Plan.Action,
					Severity:    parsed.Plan.Severity,
					Target:      parsed.Plan.Target,
					SideEffect:  parsed.Plan.SideEffect,
					ImpactScope: parsed.Plan.ImpactScope,
					Rollback:    parsed.Plan.RollbackPlan,
					Params:      parsed.Plan.Params,
					HumanPrompt: parsed.HumanPrompt,
				},
			},
		})
	}
}

// extractPendingResult 工具响应的 content 可能是：
//   - 纯 JSON（框架直接 marshal 了 Result）——平铺结构，含 status/human_prompt/plan
//   - 包裹在 {"data": {...}} 里（取决于 Tool.Call 的包装）——嵌套一层
//
// 策略：
//  1. 优先尝试 {"data": {...}} 嵌套格式（嵌套是更明确的信号，外层可能有 ok/message 等噪声字段会干扰平铺解析）
//  2. 嵌套无 data 字段或解析失败，再按平铺直接 Unmarshal
//  3. 两者都失败才返回 error
func extractPendingResult(payload string, v any) error {
	// 1. 优先嵌套格式
	var outer struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &outer); err == nil && len(outer.Data) > 0 {
		if err := json.Unmarshal(outer.Data, v); err == nil {
			return nil
		}
	}
	// 2. 平铺格式
	if err := json.Unmarshal([]byte(payload), v); err == nil {
		return nil
	}
	return fmt.Errorf("unrecognized tool response payload")
}

// handleToolCalls 若事件包含工具调用，则输出可视化提示。
// transfer_to_agent 由 handleTransferEvent 先消费，此函数不会再看到。
func (s *Service) handleToolCalls(w http.ResponseWriter, flusher http.Flusher, ev *event.Event) bool {
	if len(ev.Choices) == 0 || len(ev.Choices[0].Message.ToolCalls) == 0 {
		return false
	}
	for _, tc := range ev.Choices[0].Message.ToolCalls {
		// 防御式：若遇到 transfer，交给上游处理（一般不会进入这里）
		if tc.Function.Name == transferToolName {
			continue
		}
		msg := fmt.Sprintf("\n*开始执行工具: %s*\n", tc.Function.Name)
		info := &ToolCallInfo{Name: tc.Function.Name}
		if s.debug {
			info.Args = string(tc.Function.Arguments)
			msg = fmt.Sprintf("\n*开始执行工具: %s。请求参数: %s*\n",
				tc.Function.Name, string(tc.Function.Arguments))
		}
		writeSSE(w, flusher, Response{
			EventName: "tool_call",
			Data: Data{
				Response:     msg,
				Author:       ev.Author,
				ToolCall:     info,
				GlobalOutput: GlobalOutput{AnswerSuccess: 1},
			},
		})
	}
	return true
}

// handleDelta 输出流式内容增量。
func (s *Service) handleDelta(w http.ResponseWriter, flusher http.Flusher, ev *event.Event) {
	if len(ev.Choices) == 0 {
		return
	}
	delta := ev.Choices[0].Delta.Content
	if delta == "" {
		return
	}
	writeSSE(w, flusher, Response{Data: Data{Response: delta, Author: ev.Author}})
}

// handleFinal 处理最终事件，返回是否已终止。
func (s *Service) handleFinal(w http.ResponseWriter, flusher http.Flusher, ev *event.Event) bool {
	if !ev.Done || isToolEvent(ev) {
		return false
	}
	if s.entranceAgent != nil && ev.Author != s.entranceAgent.Info().Name {
		// 仅入口 Agent 的 Done 才视为整体结束（子 Agent Done 会继续）
		return false
	}
	writeSSE(w, flusher, Response{
		EventName: "final",
		Data: Data{
			Response:     "",
			Finished:     true,
			Author:       ev.Author,
			GlobalOutput: GlobalOutput{AnswerSuccess: 1},
		},
	})
	return true
}

// isToolEvent 判断事件是否为工具调用/响应。
func isToolEvent(ev *event.Event) bool {
	if ev == nil || ev.Response == nil {
		return false
	}
	if len(ev.Choices) > 0 && len(ev.Choices[0].Message.ToolCalls) > 0 {
		return true
	}
	if len(ev.Choices) > 0 && ev.Choices[0].Message.ToolID != "" {
		return true
	}
	for _, c := range ev.Response.Choices {
		if c.Message.Role == model.RoleTool {
			return true
		}
	}
	return false
}

// isToolResponseEvent 判断事件是否为工具响应（非最终响应）。
func isToolResponseEvent(ev *event.Event) bool {
	if ev == nil || ev.Response == nil {
		return false
	}
	for _, c := range ev.Response.Choices {
		if c.Message.Role == model.RoleTool && strings.TrimSpace(c.Message.ToolID) != "" {
			return true
		}
	}
	return false
}

// writeSSE 将 Response 编码为 SSE 一条消息写入 writer。
// 同时在 D16 阶段按 event 类型打点 Counter（Noop Meter 时零开销）。
func writeSSE(w http.ResponseWriter, flusher http.Flusher, r Response) {
	_, _ = w.Write([]byte(r.String()))
	flusher.Flush()
	observability.IncSSEEvent(context.Background(), r.EventName)
}
