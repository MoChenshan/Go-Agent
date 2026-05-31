package wecome2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	metricllm "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/criterion/llm"
)

func TestWeComOnlineLikeContainerPDF(t *testing.T) {
	env := requireWeComE2EEnv(t)
	h := newWeComE2EHarness(t, env)
	t.Run("attachment_extract", func(t *testing.T) {
		runPDFAttachmentExtract(t, h)
	})
	t.Run("summary_quality", func(t *testing.T) {
		runPDFSummaryQuality(t, env, h)
	})
	t.Run("split_round_trip", func(t *testing.T) {
		runPDFSplitRoundTrip(t, h)
	})
}

func runPDFAttachmentExtract(t *testing.T, h *wecomE2EHarness) {
	t.Helper()
	fileURL := h.media.registerAsset(t, "alpha42.pdf", "application/pdf", mustCreateTextPDF(t, "Attachment token sheet", "Unique token: ALPHA42"))
	start := h.ws.frameCount()
	reqID := "req-content-pdf-extract"
	h.ws.sendMessageCallback(t, reqID, wecomchannel.WebhookMessage{
		MsgID:   "msg-content-pdf-extract",
		ChatID:  "chat-content-pdf-extract",
		From:    wecomchannel.FromInfo{UserID: "user-content"},
		MsgType: wecomchannel.MsgTypeMixed,
		MixedMessage: wecomchannel.MixedMessageContent{
			MsgItem: []wecomchannel.MixedMsgItem{
				{
					MsgType: wecomchannel.MsgTypeText,
					Text: wecomchannel.TextContent{
						Content: "读附件，回那串大写字母数字。",
					},
				},
				{
					MsgType: wecomchannel.MsgTypeFile,
					File: wecomchannel.FileContent{
						URL: fileURL,
					},
				},
			},
		},
	})
	reply := h.ws.waitForFinalReplyText(t, start, reqID, wecomE2EReplyTimeout)
	require.GreaterOrEqual(t, h.media.hitCount(fileURL), 1, reply, h.debugSnapshot())
	require.Contains(t, reply, "ALPHA42", reply, h.debugSnapshot())
}

func runPDFSummaryQuality(t *testing.T, env wecomE2EEnv, h *wecomE2EHarness) {
	t.Helper()
	pdfURL := h.media.registerAsset(t, "project-lighthouse-q2.pdf", "application/pdf", mustCreateTextPDF(t, "Project Lighthouse Q2 Review", "Revenue grew by 18 percent year over year.", "Customer churn dropped to 3 percent after onboarding changes.", "Singapore expansion is scheduled for next quarter."))
	start := h.ws.frameCount()
	reqID := "req-content-pdf-summary"
	userPrompt := "看附件，总结 3 点。第一行 PDF_SUMMARY，后面 1. 2. 3."
	h.ws.sendMessageCallback(t, reqID, wecomchannel.WebhookMessage{
		MsgID:   "msg-content-pdf-summary",
		ChatID:  "chat-content-pdf-summary",
		From:    wecomchannel.FromInfo{UserID: "user-content-pdf-summary"},
		MsgType: wecomchannel.MsgTypeMixed,
		MixedMessage: wecomchannel.MixedMessageContent{
			MsgItem: []wecomchannel.MixedMsgItem{
				{
					MsgType: wecomchannel.MsgTypeText,
					Text: wecomchannel.TextContent{
						Content: userPrompt,
					},
				},
				{
					MsgType: wecomchannel.MsgTypeFile,
					File: wecomchannel.FileContent{
						URL: pdfURL,
					},
				},
			},
		},
	})
	reply := h.ws.waitForFinalReplyText(t, start, reqID, wecomE2EReplyTimeout)
	require.GreaterOrEqual(t, h.media.hitCount(pdfURL), 1, reply, h.debugSnapshot())
	require.Contains(t, reply, "PDF_SUMMARY", reply, h.debugSnapshot())
	require.Contains(t, reply, "1.", reply, h.debugSnapshot())
	require.Contains(t, reply, "2.", reply, h.debugSnapshot())
	require.Contains(t, reply, "3.", reply, h.debugSnapshot())
	runTraceCriticEvaluation(t, env, traceCriticEvalInput{
		evalSetID:         "wecom-pdf-summary-eval",
		evalCaseID:        "pdf-summary-basic",
		userID:            "user-content-pdf-summary",
		userPrompt:        userPrompt,
		actualResponse:    reply,
		referenceResponse: "PDF_SUMMARY\n1. 营收同比增长18%。\n2. 客户流失率降至3%。\n3. 团队将在下季度拓展新加坡市场。",
		threshold:         1.0,
		rubrics: []*metricllm.Rubric{
			{
				ID:          "1",
				Description: "The summary states that revenue increased by 18 percent.",
				Type:        "FINAL_RESPONSE_QUALITY",
				Content: &metricllm.RubricContent{
					Text: "The summary must explicitly mention that revenue grew by 18 percent year over year.",
				},
			},
			{
				ID:          "2",
				Description: "The summary states that churn dropped to 3 percent.",
				Type:        "FINAL_RESPONSE_QUALITY",
				Content: &metricllm.RubricContent{
					Text: "The summary must explicitly mention that customer churn dropped to 3 percent.",
				},
			},
			{
				ID:          "3",
				Description: "The summary states that Singapore expansion happens next quarter.",
				Type:        "FINAL_RESPONSE_QUALITY",
				Content: &metricllm.RubricContent{
					Text: "The summary must explicitly mention that the team will expand into Singapore next quarter.",
				},
			},
		},
	})
}

func runPDFSplitRoundTrip(t *testing.T, h *wecomE2EHarness) {
	t.Helper()
	sourceURL := h.media.registerAsset(
		t,
		"split-source.pdf",
		"application/pdf",
		mustCreateMultiPageTextPDF(
			t,
			[]string{"Split source page 1", "Unique token: SPLIT_ALPHA"},
			[]string{"Split source page 2", "Unique token: SPLIT_BRAVO"},
		),
	)
	start := h.ws.frameCount()
	uploadStart := h.ws.uploadCount()
	reqID := "req-content-pdf-split"
	h.ws.sendMessageCallback(t, reqID, wecomchannel.WebhookMessage{
		MsgID:   "msg-content-pdf-split",
		ChatID:  "chat-content-pdf-split",
		From:    wecomchannel.FromInfo{UserID: "user-content-pdf-split"},
		MsgType: wecomchannel.MsgTypeMixed,
		MixedMessage: wecomchannel.MixedMessageContent{
			MsgItem: []wecomchannel.MixedMsgItem{
				{
					MsgType: wecomchannel.MsgTypeText,
					Text: wecomchannel.TextContent{
						Content: "把附件按页拆成单页 PDF 发回来，不要压缩。拆好回 SPLIT_DONE。",
					},
				},
				{
					MsgType: wecomchannel.MsgTypeFile,
					File: wecomchannel.FileContent{
						URL: sourceURL,
					},
				},
			},
		},
	})
	reply := h.ws.waitForReplyTextContains(t, start, reqID, "SPLIT_DONE", wecomE2EReplyTimeout)
	require.GreaterOrEqual(t, h.media.hitCount(sourceURL), 1, reply, h.debugSnapshot())
	matchFileFrame := func(frame capturedWSFrame) bool {
		if strings.TrimSpace(frame.Headers.ReqID) != reqID {
			return false
		}
		msgType, mediaID, ok := frame.replyMedia()
		return ok && msgType == wecomchannel.MsgTypeFile && mediaID != ""
	}
	fileFrames := h.ws.waitForMatchingFrames(t, start, 2, wecomE2EReplyTimeout, matchFileFrame)
	require.Len(t, fileFrames, 2)
	h.ws.requireMatchingFrameCountStable(t, start, 2, 3*time.Second, matchFileFrame)
	uploads := h.ws.waitForUploads(t, uploadStart, 2, wecomE2EReplyTimeout)
	require.Len(t, uploads, 2)
	mediaByID := make(map[string]capturedUpload, len(uploads))
	for _, upload := range uploads {
		mediaByID[upload.MediaID] = upload
	}
	foundTokens := make([]string, 0, len(fileFrames))
	for _, frame := range fileFrames {
		_, mediaID, ok := frame.replyMedia()
		require.True(t, ok)
		upload, exists := mediaByID[mediaID]
		require.True(t, exists)
		require.True(t, strings.HasSuffix(strings.ToLower(upload.Filename), ".pdf"))
		pages := readPDFPagesText(t, upload.Data)
		require.Len(t, pages, 1)
		switch {
		case strings.Contains(pages[0], "SPLIT_ALPHA"):
			require.NotContains(t, pages[0], "SPLIT_BRAVO")
			foundTokens = append(foundTokens, "SPLIT_ALPHA")
		case strings.Contains(pages[0], "SPLIT_BRAVO"):
			require.NotContains(t, pages[0], "SPLIT_ALPHA")
			foundTokens = append(foundTokens, "SPLIT_BRAVO")
		default:
			t.Fatalf("split pdf does not contain expected token: %s", pages[0])
		}
	}
	require.ElementsMatch(t, []string{"SPLIT_ALPHA", "SPLIT_BRAVO"}, foundTokens)
}
