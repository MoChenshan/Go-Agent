package wecome2e_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
)

func TestWeComOnlineLikeContainerSmoke(t *testing.T) {
	env := requireWeComE2EEnv(t)
	h := newWeComE2EHarness(t, env)
	t.Run("pure_text_round_trip", func(t *testing.T) {
		runPureTextRoundTrip(t, h)
	})
	t.Run("session_round_trip", func(t *testing.T) {
		runSessionRoundTrip(t, h)
	})
	t.Run("file_attachment_round_trip", func(t *testing.T) {
		runFileAttachmentRoundTrip(t, h)
	})
}

func runPureTextRoundTrip(t *testing.T, h *wecomE2EHarness) {
	t.Helper()
	start := h.ws.frameCount()
	reqID := "req-smoke-pure-text"
	h.ws.sendMessageCallback(t, reqID, wecomchannel.WebhookMessage{
		MsgID:   "msg-smoke-pure-text",
		ChatID:  "chat-smoke-pure-text",
		From:    wecomchannel.FromInfo{UserID: "user-smoke"},
		MsgType: wecomchannel.MsgTypeText,
		Text: wecomchannel.TextContent{
			Content: "回 TEXT_E2E_OK。",
		},
	})
	reply := h.ws.waitForReplyTextContains(t, start, reqID, "TEXT_E2E_OK", wecomE2EReplyTimeout)
	require.Contains(t, reply, "TEXT_E2E_OK")
}

func runSessionRoundTrip(t *testing.T, h *wecomE2EHarness) {
	t.Helper()
	chatID := "chat-smoke-session"
	userID := "user-smoke-session"
	start := h.ws.frameCount()
	reqID1 := "req-smoke-session-1"
	h.ws.sendMessageCallback(t, reqID1, wecomchannel.WebhookMessage{
		MsgID:   "msg-smoke-session-1",
		ChatID:  chatID,
		From:    wecomchannel.FromInfo{UserID: userID},
		MsgType: wecomchannel.MsgTypeText,
		Text: wecomchannel.TextContent{
			Content: "记住：我在新加坡。回 SESSION_OK。",
		},
	})
	reply1 := h.ws.waitForReplyTextContains(t, start, reqID1, "SESSION_OK", wecomE2EReplyTimeout)
	require.Contains(t, reply1, "SESSION_OK")
	start = h.ws.frameCount()
	reqID2 := "req-smoke-session-2"
	h.ws.sendMessageCallback(t, reqID2, wecomchannel.WebhookMessage{
		MsgID:   "msg-smoke-session-2",
		ChatID:  chatID,
		From:    wecomchannel.FromInfo{UserID: userID},
		MsgType: wecomchannel.MsgTypeText,
		Text: wecomchannel.TextContent{
			Content: "我在哪？只回城市名。",
		},
	})
	reply2 := h.ws.waitForReplyTextContains(t, start, reqID2, "新加坡", wecomE2EReplyTimeout)
	require.Contains(t, reply2, "新加坡")
}

func runFileAttachmentRoundTrip(t *testing.T, h *wecomE2EHarness) {
	t.Helper()
	attachmentPath := filepath.Join(h.workspaceDir, "wecom-smoke-output.txt")
	start := h.ws.frameCount()
	uploadStart := h.ws.uploadCount()
	reqID := "req-smoke-attachment"
	h.ws.sendMessageCallback(t, reqID, wecomchannel.WebhookMessage{
		MsgID:   "msg-smoke-attachment",
		ChatID:  "chat-smoke-attachment",
		From:    wecomchannel.FromInfo{UserID: "user-smoke"},
		MsgType: wecomchannel.MsgTypeText,
		Text: wecomchannel.TextContent{
			Content: "在 " + attachmentPath + " 写 WECOM_E2E_ATTACHMENT_OK，发回这个文件。回 ATTACHMENT_SENT。",
		},
	})
	reply := h.ws.waitForReplyTextContains(t, start, reqID, "ATTACHMENT_SENT", wecomE2EReplyTimeout)
	require.Contains(t, reply, "ATTACHMENT_SENT")
	fileFrame := h.ws.waitForFrame(t, start, wecomE2EReplyTimeout, func(frame capturedWSFrame) bool {
		if strings.TrimSpace(frame.Headers.ReqID) != reqID {
			return false
		}
		msgType, mediaID, ok := frame.replyMedia()
		return ok && msgType == wecomchannel.MsgTypeFile && mediaID != ""
	})
	msgType, mediaID, ok := fileFrame.replyMedia()
	require.True(t, ok)
	require.Equal(t, wecomchannel.MsgTypeFile, msgType)
	uploads := h.ws.waitForUploads(t, uploadStart, 1, wecomE2EReplyTimeout)
	require.Len(t, uploads, 1)
	require.Equal(t, mediaID, uploads[0].MediaID)
	require.Equal(t, "wecom-smoke-output.txt", uploads[0].Filename)
	require.Equal(t, "WECOM_E2E_ATTACHMENT_OK", strings.TrimSpace(string(uploads[0].Data)))
}
