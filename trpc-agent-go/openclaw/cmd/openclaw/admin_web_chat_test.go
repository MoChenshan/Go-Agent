package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwproto"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

type fakeAdminWebChatGateway struct {
	sendReq      gwclient.MessageRequest
	streamReq    gwclient.MessageRequest
	cancelReqID  string
	streamEvents []gwclient.StreamEvent
}

func (g *fakeAdminWebChatGateway) SendMessage(
	_ context.Context,
	req gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	g.sendReq = req
	return gwclient.MessageResponse{
		SessionID: req.SessionID,
		RequestID: req.RequestID,
		Reply:     "final answer",
	}, nil
}

func (g *fakeAdminWebChatGateway) StreamMessage(
	_ context.Context,
	req gwclient.MessageRequest,
) (<-chan gwclient.StreamEvent, error) {
	g.streamReq = req
	out := make(chan gwclient.StreamEvent, len(g.streamEvents))
	for _, evt := range g.streamEvents {
		out <- evt
	}
	close(out)
	return out, nil
}

func (g *fakeAdminWebChatGateway) Cancel(
	_ context.Context,
	requestID string,
) (bool, error) {
	g.cancelReqID = requestID
	return true, nil
}

func TestAdminWebChatSendUsesGatewayRequest(t *testing.T) {
	t.Parallel()

	gateway := &fakeAdminWebChatGateway{}
	service := newAdminWebChatService("openclaw", gateway, nil)
	body := bytes.NewBufferString(
		`{"user_id":"u1","session_id":"s1",` +
			`"request_id":"r1","message":"hello"}`,
	)
	req := httptest.NewRequest(
		http.MethodPost,
		adminWebChatSendPath,
		body,
	)
	rsp := httptest.NewRecorder()

	wrapAdminWebChatHandler(nil, service).ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Equal(t, adminWebChatChannel, gateway.sendReq.Channel)
	require.Equal(t, "u1", gateway.sendReq.UserID)
	require.Equal(t, "s1", gateway.sendReq.SessionID)
	require.Equal(t, "r1", gateway.sendReq.RequestID)
	require.Empty(t, gateway.sendReq.Thread)
	require.Equal(t, "hello", gateway.sendReq.Text)

	var decoded adminWebChatSendResponse
	require.NoError(t, json.Unmarshal(rsp.Body.Bytes(), &decoded))
	require.Equal(t, "s1", decoded.SessionID)
	require.Equal(t, "r1", decoded.RequestID)
	require.Equal(t, adminWebChatRoleAssistant, decoded.Message.Role)
	require.Equal(t, "final answer", decoded.Message.Content[0].Text)
}

func TestAdminWebChatStreamRelaysProgressAndDeltas(t *testing.T) {
	t.Parallel()

	gateway := &fakeAdminWebChatGateway{
		streamEvents: []gwclient.StreamEvent{
			{
				Type:    gwproto.StreamEventTypeRunProgress,
				Stage:   gwproto.StreamProgressStageRunningTool,
				Summary: "Running tool",
			},
			{
				Type:  gwproto.StreamEventTypeMessageDelta,
				Delta: "hel",
			},
			{
				Type:  gwproto.StreamEventTypeMessageCompleted,
				Reply: "hello",
			},
		},
	}
	service := newAdminWebChatService("openclaw", gateway, nil)
	body := bytes.NewBufferString(
		`{"user_id":"u1","session_id":"s1",` +
			`"request_id":"r1","message":"hello"}`,
	)
	req := httptest.NewRequest(
		http.MethodPost,
		adminWebChatStreamPath,
		body,
	)
	rsp := httptest.NewRecorder()

	wrapAdminWebChatHandler(nil, service).ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Equal(t, "s1", gateway.streamReq.SessionID)
	require.Empty(t, gateway.streamReq.Thread)
	require.Contains(t, rsp.Body.String(), "run.progress")
	require.Contains(t, rsp.Body.String(), "message.delta")
	require.Contains(t, rsp.Body.String(), "Running tool")
	require.Contains(t, rsp.Body.String(), "hello")
}

func TestProjectAdminWebChatMessagesKeepsToolCards(t *testing.T) {
	t.Parallel()

	now := time.Now()
	args := []byte(`{"cmd":"go test ./cmd/openclaw"}`)
	sess := session.NewSession(
		"openclaw",
		"u1",
		"s1",
		session.WithSessionEvents([]event.Event{
			{
				ID:        "e1",
				Author:    adminWebChatRoleUser,
				RequestID: "r1",
				Timestamp: now,
				Response: &model.Response{
					Choices: []model.Choice{{
						Message: model.NewUserMessage("run tests"),
					}},
				},
			},
			{
				ID:        "e2",
				Author:    adminWebChatRoleAssistant,
				RequestID: "r1",
				Timestamp: now,
				Response: &model.Response{
					Choices: []model.Choice{{
						Message: model.Message{
							Role: model.RoleAssistant,
							ToolCalls: []model.ToolCall{{
								ID: "call-1",
								Function: model.FunctionDefinitionParam{
									Name:      "exec_command",
									Arguments: args,
								},
							}},
						},
					}},
				},
			},
			{
				ID:        "e3",
				Author:    adminWebChatRoleTool,
				RequestID: "r1",
				Timestamp: now,
				Response: &model.Response{
					Choices: []model.Choice{{
						Message: model.NewToolMessage(
							"call-1",
							"exec_command",
							"ok",
						),
					}},
				},
			},
		}),
	)

	messages := projectAdminWebChatMessages(sess)

	require.Len(t, messages, 3)
	require.Equal(t, adminWebChatRoleUser, messages[0].Role)
	require.Equal(t, "run tests", messages[0].Content[0].Text)
	require.Equal(t, adminWebChatContentToolCall, messages[1].Content[0].Type)
	require.Equal(t, "call-1", messages[1].Content[0].ToolCallID)
	require.Equal(t, "exec_command", messages[1].Content[0].Name)
	require.Contains(t, messages[1].Content[0].Arguments, "go test")
	require.Equal(t, adminWebChatContentToolResult, messages[2].Content[0].Type)
	require.Equal(t, "ok", messages[2].Content[0].Output)
}

func TestAdminWebChatCancelUsesGateway(t *testing.T) {
	t.Parallel()

	gateway := &fakeAdminWebChatGateway{}
	service := newAdminWebChatService("openclaw", gateway, nil)
	req := httptest.NewRequest(
		http.MethodPost,
		adminWebChatCancelPath,
		strings.NewReader(`{"request_id":"r1"}`),
	)
	rsp := httptest.NewRecorder()

	wrapAdminWebChatHandler(nil, service).ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Equal(t, "r1", gateway.cancelReqID)
	require.Contains(t, rsp.Body.String(), `"canceled": true`)
}

func TestAdminWebChatPageSidebarCanScroll(t *testing.T) {
	t.Parallel()

	require.Contains(t, adminWebChatPageHTML, "overflow-y: auto")
	require.Contains(t, adminWebChatPageHTML, "overflow: visible")
	require.Contains(t, adminWebChatPageHTML, "overscroll-behavior: contain")
	require.Contains(t, adminWebChatPageHTML, "scrollbar-gutter: stable")
	require.Contains(t, adminWebChatPageHTML, "sidebar.scrollTop")
	require.Contains(t, adminWebChatPageHTML, "openclaw.admin.pendingScroll")
	require.Contains(t, adminWebChatPageHTML, "window.sessionStorage")
	require.Contains(t, adminWebChatPageHTML, "window.scrollBy")
	require.Contains(t, adminWebChatPageHTML, "targetURL.pathname")
	require.Contains(t, adminWebChatPageHTML, "value.targetPath")
	require.Contains(
		t,
		adminWebChatPageHTML,
		`throw new Error("Request ignored")`,
	)
	require.Contains(t, adminWebChatPageHTML, "throw new Error(evt.error)")
	require.NotContains(t, adminWebChatPageHTML, "window.scrollTo(0, pageTop)")
	require.NotContains(t, adminWebChatPageHTML, "pageTop:")
	require.NotContains(
		t,
		adminWebChatPageHTML,
		`window.addEventListener("pagehide"`,
	)
	require.NotContains(t, adminWebChatPageHTML, "scrollIntoView")
}
