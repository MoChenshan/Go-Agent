package wecome2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	metricllm "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/criterion/llm"
)

func TestWeComOnlineLikeContainerImage(t *testing.T) {
	env := requireWeComE2EEnv(t)
	h := newWeComE2EHarness(t, env)
	t.Run("understanding", func(t *testing.T) {
		runImageUnderstanding(t, env, h)
	})
	t.Run("generation", func(t *testing.T) {
		runImageGeneration(t, h)
	})
}

func runImageUnderstanding(t *testing.T, env wecomE2EEnv, h *wecomE2EHarness) {
	t.Helper()
	fixtureBytes := mustReadFixtureBytes(t, "admin-ui.png")
	userPrompt := "看图，提取页面标题、模型名、通道名、健康检查路径。保留原文，简短作答。"
	imageURL := h.media.registerAsset(
		t,
		"admin-ui.png",
		"image/png",
		fixtureBytes,
	)
	start := h.ws.frameCount()
	reqID := "req-image-understanding"
	h.ws.sendMessageCallback(t, reqID, wecomchannel.WebhookMessage{
		MsgID:   "msg-image-understanding",
		ChatID:  "chat-image-understanding",
		From:    wecomchannel.FromInfo{UserID: "user-image"},
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
					MsgType: wecomchannel.MsgTypeImage,
					Image: wecomchannel.ImageContent{
						URL: imageURL,
					},
				},
			},
		},
	})
	reply := h.ws.waitForFinalReplyText(t, start, reqID, wecomE2EReplyTimeout)
	require.GreaterOrEqual(t, h.media.hitCount(imageURL), 1, reply, h.debugSnapshot())
	require.NotEmpty(t, strings.TrimSpace(reply), h.debugSnapshot())
	runTraceCriticEvaluation(t, env, traceCriticEvalInput{
		evalSetID:         "wecom-image-understanding-eval",
		evalCaseID:        "image-admin-ui-understanding",
		userID:            "user-image",
		userPrompt:        userPrompt,
		actualResponse:    reply,
		referenceResponse: "页面标题是 OpenClaw Admin。模型名是 gpt-5.2。通道名是 telegram。健康检查路径是 /healthz。",
		threshold:         1.0,
		rubrics: []*metricllm.Rubric{
			{
				ID:          "1",
				Description: "The answer identifies the page title.",
				Type:        "FINAL_RESPONSE_QUALITY",
				Content: &metricllm.RubricContent{
					Text: "The answer must explicitly identify the page title as OpenClaw Admin.",
				},
			},
			{
				ID:          "2",
				Description: "The answer identifies the model name.",
				Type:        "FINAL_RESPONSE_QUALITY",
				Content: &metricllm.RubricContent{
					Text: "The answer must explicitly identify the model name as gpt-5.2.",
				},
			},
			{
				ID:          "3",
				Description: "The answer identifies the channel name.",
				Type:        "FINAL_RESPONSE_QUALITY",
				Content: &metricllm.RubricContent{
					Text: "The answer must explicitly identify the channel name as telegram.",
				},
			},
			{
				ID:          "4",
				Description: "The answer identifies the health check path.",
				Type:        "FINAL_RESPONSE_QUALITY",
				Content: &metricllm.RubricContent{
					Text: "The answer must explicitly identify the health check path as /healthz.",
				},
			},
		},
	})
}

func runImageGeneration(t *testing.T, h *wecomE2EHarness) {
	t.Helper()
	imagePath := filepath.Join(h.workspaceDir, "wecom-generated-red.png")
	start := h.ws.frameCount()
	uploadStart := h.ws.uploadCount()
	reqID := "req-image-generation"
	h.ws.sendMessageCallback(t, reqID, wecomchannel.WebhookMessage{
		MsgID:   "msg-image-generation",
		ChatID:  "chat-image-generation",
		From:    wecomchannel.FromInfo{UserID: "user-image"},
		MsgType: wecomchannel.MsgTypeText,
		Text: wecomchannel.TextContent{
			Content: "在 " + imagePath + " 生成一张 128x128 的纯红 PNG，并作为图片发回。回 IMAGE_SENT。",
		},
	})
	reply := h.ws.waitForReplyTextContains(t, start, reqID, "IMAGE_SENT", wecomE2EReplyTimeout)
	require.Contains(t, reply, "IMAGE_SENT")
	imageFrame := h.ws.waitForFrame(t, start, wecomE2EReplyTimeout, func(frame capturedWSFrame) bool {
		if strings.TrimSpace(frame.Headers.ReqID) != reqID {
			return false
		}
		msgType, mediaID, ok := frame.replyMedia()
		return ok && msgType == wecomchannel.MsgTypeImage && mediaID != ""
	})
	msgType, mediaID, ok := imageFrame.replyMedia()
	require.True(t, ok)
	require.Equal(t, wecomchannel.MsgTypeImage, msgType)
	uploads := h.ws.waitForUploads(t, uploadStart, 1, wecomE2EReplyTimeout)
	require.Len(t, uploads, 1)
	require.Equal(t, mediaID, uploads[0].MediaID)
	img := decodeImageData(t, uploads[0].Data)
	require.Equal(t, 128, img.Bounds().Dx())
	require.Equal(t, 128, img.Bounds().Dy())
	r, g, b, _ := img.At(64, 64).RGBA()
	require.Greater(t, r, uint32(g))
	require.Greater(t, r, uint32(b))
}

func mustReadFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
