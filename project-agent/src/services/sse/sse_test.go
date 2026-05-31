
package sse

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// writerFlusher 把 httptest.ResponseRecorder 包装成同时实现 Flusher 的对象。
type writerFlusher struct{ *httptest.ResponseRecorder }

func (w *writerFlusher) Flush() {}

func newTestWriter() *writerFlusher {
	return &writerFlusher{httptest.NewRecorder()}
}

// parseSSE 解析一个 httptest.ResponseRecorder 里的 SSE 输出，
// 返回每条事件的 name / data(JSON) 映射列表。
func parseSSE(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, chunk := range strings.Split(body, "\n\n") {
		if chunk = strings.TrimSpace(chunk); chunk == "" {
			continue
		}
		lines := strings.SplitN(chunk, "\n", 2)
		if len(lines) < 2 {
			continue
		}
		name := strings.TrimPrefix(lines[0], "event:")
		dataJSON := strings.TrimPrefix(lines[1], "data:")
		var data map[string]any
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			t.Fatalf("unmarshal data err: %v, raw=%s", err, dataJSON)
		}
		out = append(out, map[string]any{"event": name, "data": data})
	}
	return out
}

// TestResponseString_DefaultEventDelta 验证默认 event 名为 delta。
func TestResponseString_DefaultEventDelta(t *testing.T) {
	r := Response{Data: Data{Response: "hello"}}
	s := r.String()
	if !strings.HasPrefix(s, "event:delta\n") {
		t.Fatalf("默认 event 应为 delta, got: %s", s)
	}
}

// TestResponseString_CustomEventName 自定义 event 名。
func TestResponseString_CustomEventName(t *testing.T) {
	r := Response{EventName: "confirmation_required", Data: Data{Response: "x"}}
	s := r.String()
	if !strings.HasPrefix(s, "event:confirmation_required\n") {
		t.Fatalf("event 名未生效: %s", s)
	}
	// data.event_type 也应被自动填充
	if !strings.Contains(s, `"event_type":"confirmation_required"`) {
		t.Fatalf("data.event_type 未注入: %s", s)
	}
}

// TestParseTransferArgs_Valid 验证 transfer_to_agent 参数解析。
func TestParseTransferArgs_Valid(t *testing.T) {
	to, reason := parseTransferArgs([]byte(`{"agent_name":"diagnosis_agent","message":"OOM 诊断"}`))
	if to != "diagnosis_agent" || reason != "OOM 诊断" {
		t.Fatalf("transfer 参数解析错误 to=%s reason=%s", to, reason)
	}
}

// TestParseTransferArgs_Invalid 非法 JSON 返回空字符串不 panic。
func TestParseTransferArgs_Invalid(t *testing.T) {
	to, reason := parseTransferArgs([]byte(`not json`))
	if to != "" || reason != "" {
		t.Fatalf("非法 JSON 应返回空: to=%s reason=%s", to, reason)
	}
}

// TestHandleTransferEvent_ToolCallForm 识别 ToolCalls 形式的 Transfer。
func TestHandleTransferEvent_ToolCallForm(t *testing.T) {
	svc := &Service{}
	w := newTestWriter()

	ev := &event.Event{
		Author: "coordinator",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{
					ToolCalls: []model.ToolCall{
						{Function: model.FunctionDefinitionParam{
							Name:      "transfer_to_agent",
							Arguments: []byte(`{"agent_name":"repair_agent","message":"执行回滚"}`),
						}},
					},
				}},
			},
		},
	}
	got := svc.handleTransferEvent(w, w, ev)
	if !got {
		t.Fatalf("handleTransferEvent 未识别 transfer ToolCall")
	}
	events := parseSSE(t, w.Body.String())
	if len(events) != 1 || events[0]["event"] != "agent_transfer" {
		t.Fatalf("期望一条 agent_transfer 事件，实际：%+v", events)
	}
	data := events[0]["data"].(map[string]any)
	tr := data["transfer"].(map[string]any)
	if tr["to"] != "repair_agent" || tr["reason"] != "执行回滚" {
		t.Fatalf("Transfer 详情不对：%+v", tr)
	}
}

// TestHandleToolResponse_PendingConfirmation 验证 HITL PendingResult 被识别。
func TestHandleToolResponse_PendingConfirmation(t *testing.T) {
	svc := &Service{}
	w := newTestWriter()

	pending := `{
		"ok": false,
		"status": "awaiting_confirmation",
		"message": "需要人工确认",
		"human_prompt": "⚠ 即将执行 bcs.helm.rollback\n请回复确认",
		"plan": {
			"action": "bcs.helm.rollback",
			"severity": "high",
			"target": "BCS-K8S-001/letsgo/game-core",
			"side_effect": "release 回滚到 revision=4",
			"impact_scope": "命名空间下所有 Pod 滚动重启",
			"rollback_plan": "回滚到更早 revision",
			"params": {"revision": 4}
		}
	}`
	ev := &event.Event{
		Author: "repair_agent",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{
					Role:    model.RoleTool,
					ToolID:  "tc-001",
					Content: pending,
				}},
			},
		},
	}
	svc.handleToolResponse(w, w, ev)
	events := parseSSE(t, w.Body.String())
	if len(events) != 1 || events[0]["event"] != "confirmation_required" {
		t.Fatalf("期望一条 confirmation_required，实际：%+v", events)
	}
	data := events[0]["data"].(map[string]any)
	confirm := data["confirmation"].(map[string]any)
	if confirm["action"] != "bcs.helm.rollback" || confirm["severity"] != "high" {
		t.Fatalf("HITL 字段解析错误：%+v", confirm)
	}
	if !strings.Contains(confirm["human_prompt"].(string), "bcs.helm.rollback") {
		t.Fatalf("human_prompt 未透传：%s", confirm["human_prompt"])
	}
}

// TestHandleToolResponse_NonHITL 非 HITL 工具响应应被静默跳过。
func TestHandleToolResponse_NonHITL(t *testing.T) {
	svc := &Service{}
	w := newTestWriter()

	ev := &event.Event{
		Author: "diagnosis_agent",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{
					Role:    model.RoleTool,
					ToolID:  "tc-002",
					Content: `{"ok":true,"data":{"metric":"cpu","value":88}}`,
				}},
			},
		},
	}
	svc.handleToolResponse(w, w, ev)
	if w.Body.Len() != 0 {
		t.Fatalf("非 HITL 响应不应产生任何 SSE 事件，实际：%s", w.Body.String())
	}
}

// TestHandleToolCalls_SkipTransfer 普通 ToolCall 走 tool_call 事件，transfer_to_agent 被跳过。
func TestHandleToolCalls_SkipTransfer(t *testing.T) {
	svc := &Service{debug: true}
	w := newTestWriter()

	ev := &event.Event{
		Author: "diagnosis_agent",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{
					ToolCalls: []model.ToolCall{
						{Function: model.FunctionDefinitionParam{Name: "transfer_to_agent", Arguments: []byte(`{}`)}},
						{Function: model.FunctionDefinitionParam{Name: "bk_query_metrics", Arguments: []byte(`{"metric":"cpu"}`)}},
					},
				}},
			},
		},
	}
	if !svc.handleToolCalls(w, w, ev) {
		t.Fatalf("期望 handleToolCalls 返回 true")
	}
	events := parseSSE(t, w.Body.String())
	// 只应产出一个 tool_call（transfer_to_agent 被跳过）
	if len(events) != 1 || events[0]["event"] != "tool_call" {
		t.Fatalf("期望一条 tool_call，实际：%+v", events)
	}
	data := events[0]["data"].(map[string]any)
	tc := data["tool_call"].(map[string]any)
	if tc["name"] != "bk_query_metrics" {
		t.Fatalf("tool_call name 不对：%+v", tc)
	}
	if !strings.Contains(tc["args"].(string), "cpu") {
		t.Fatalf("debug=true 时 args 应被填充")
	}
}

// TestExtractPendingResult_Fallback 测试双层嵌套的 extract。
func TestExtractPendingResult_Fallback(t *testing.T) {
	// 包装在 {"data": {...}} 里的情况
	payload := `{"ok":false,"data":{"status":"awaiting_confirmation","human_prompt":"p","plan":{"action":"x","severity":"high","target":"t"}}}`
	var parsed struct {
		Status      string `json:"status"`
		HumanPrompt string `json:"human_prompt"`
		Plan        struct {
			Action   string `json:"action"`
			Severity string `json:"severity"`
			Target   string `json:"target"`
		} `json:"plan"`
	}
	if err := extractPendingResult(payload, &parsed); err != nil {
		t.Fatalf("extractPendingResult err: %v", err)
	}
	if parsed.Status != "awaiting_confirmation" || parsed.Plan.Action != "x" {
		t.Fatalf("extract 失败：%+v", parsed)
	}
}
