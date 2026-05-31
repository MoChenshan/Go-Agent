package wecome2e_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
)

type cronJobStore struct {
	Jobs []cronJobSnapshot `json:"jobs"`
}

type cronJobSnapshot struct {
	Message string        `json:"message"`
	Policy  cronJobPolicy `json:"policy"`
	Stats   cronJobStats  `json:"stats"`
	Status  string        `json:"last_status"`
	NextRun *time.Time    `json:"next_run_at"`
}

type cronJobPolicy struct {
	MaxRuns int `json:"max_runs"`
}

type cronJobStats struct {
	RunCount             int `json:"run_count"`
	SuccessCount         int `json:"success_count"`
	DeliveryFailureCount int `json:"delivery_failure_count"`
}

func TestWeComOnlineLikeContainerCron(t *testing.T) {
	env := requireWeComE2EEnv(t)
	h := newWeComE2EHarness(t, env)
	runCronThreeRuns(t, h)
}

func runCronThreeRuns(t *testing.T, h *wecomE2EHarness) {
	t.Helper()
	start := h.ws.frameCount()
	reqID := "req-automation-cron-create"
	h.ws.sendMessageCallback(t, reqID, wecomchannel.WebhookMessage{
		MsgID:   "msg-automation-cron-create",
		ChatID:  "chat-automation-cron",
		From:    wecomchannel.FromInfo{UserID: "user-automation-cron"},
		MsgType: wecomchannel.MsgTypeText,
		Text: wecomchannel.TextContent{
			Content: "建个提醒：从 2 秒后开始，每 2 秒在这发一次 CRON_E2E_OK，共 3 次。建好回 CRON_CREATED。",
		},
	})
	reply := h.ws.waitForReplyTextContains(t, start, reqID, "CRON_CREATED", wecomE2EReplyTimeout)
	require.Contains(t, reply, "CRON_CREATED")
	jobsPath := filepath.Join(h.stateDir, "cron", "jobs.json")
	h.waitForFileContains(t, jobsPath, "CRON_E2E_OK", wecomE2EReplyTimeout)
	matchPush := func(frame capturedWSFrame) bool {
		if frame.Command != wsCommandSend {
			return false
		}
		text, ok, final := frame.replyText()
		return ok && final && strings.Contains(text, "CRON_E2E_OK")
	}
	pushes := waitForDistinctMatchingFrames(t, h.ws, start, 3, wecomE2ECronTimeout, matchPush)
	require.Len(t, pushes, 3)
	for _, push := range pushes {
		text, ok, final := push.replyText()
		require.True(t, ok)
		require.True(t, final)
		require.Contains(t, text, "CRON_E2E_OK")
	}
	job := waitForCronJobState(t, h, jobsPath, "CRON_E2E_OK", wecomE2ECronQuietTime)
	require.Equal(t, 3, job.Policy.MaxRuns)
	require.Equal(t, 3, job.Stats.RunCount)
	require.Equal(t, 3, job.Stats.SuccessCount)
	require.Zero(t, job.Stats.DeliveryFailureCount)
}

func waitForDistinctMatchingFrames(
	t *testing.T,
	ws *fakeWeComWebSocketServer,
	start int,
	expected int,
	timeout time.Duration,
	match func(capturedWSFrame) bool,
) []capturedWSFrame {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ws.requireHealthy(t)
		frames := distinctMatchingFrames(ws.matchingFrames(start, match))
		if len(frames) >= expected {
			return frames
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting %d distinct websocket frames after index %d", expected, start)
		}
		time.Sleep(wecomE2EPollInterval)
	}
}

func waitForCronJobState(
	t *testing.T,
	h *wecomE2EHarness,
	path string,
	message string,
	timeout time.Duration,
) cronJobSnapshot {
	t.Helper()
	var matched cronJobSnapshot
	require.Eventually(t, func() bool {
		raw := h.readFile(t, path)
		var store cronJobStore
		if err := json.Unmarshal([]byte(raw), &store); err != nil {
			return false
		}
		var found *cronJobSnapshot
		for index := range store.Jobs {
			job := &store.Jobs[index]
			if !strings.Contains(job.Message, message) {
				continue
			}
			if found != nil {
				return false
			}
			found = job
		}
		if found == nil {
			return false
		}
		matched = *found
		return matched.Policy.MaxRuns == 3 &&
			matched.Stats.RunCount == 3 &&
			matched.Stats.SuccessCount == 3
	}, timeout, wecomE2EPollInterval, h.debugSnapshot())
	return matched
}

func distinctMatchingFrames(frames []capturedWSFrame) []capturedWSFrame {
	const duplicateWindow = 1500 * time.Millisecond
	lastSeen := make(map[string]time.Time, len(frames))
	out := make([]capturedWSFrame, 0, len(frames))
	for _, frame := range frames {
		key := frame.Command + "|" + string(frame.Body)
		if strings.TrimSpace(string(frame.Body)) == "" {
			key = fmt.Sprintf("%s|%d", frame.Command, frame.Received.UnixNano())
		}
		if last, exists := lastSeen[key]; exists && frame.Received.Sub(last) <= duplicateWindow {
			continue
		}
		lastSeen[key] = frame.Received
		out = append(out, frame)
	}
	return out
}
