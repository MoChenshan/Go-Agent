package wecom

import (
	"context"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	"github.com/stretchr/testify/require"
)

func TestHandleRuntimeCommandStagesCompletionNotice(
	t *testing.T,
) {
	t.Parallel()

	sender := newAIBotWebSocketSender(
		newAckWSWriter(),
		"req-runtime-1",
	)
	channel := mustCreateWebSocketChannel(t, stubGateway{})
	channel.runtimeLifecycle = runtimectl.NewManager(
		runtimectl.Options{
			CurrentVersion: "v0.0.48",
		},
	)
	channel.runtimeAdminPolicy = runtimeAdminPolicyAllowlist
	channel.runtimeAdminUsers = buildAllowSet([]string{
		"user1",
	})

	err := channel.handleRuntimeCommand(
		context.Background(),
		"chat1",
		"wecom:chat:chat1",
		"user1",
		"https://example.com/runtime/reply",
		parseCommandInput("/runtime restart"),
		sender,
	)
	require.NoError(t, err)

	notices, err := channel.runtimeCompletionNotifier.loadPending()
	require.NoError(t, err)
	require.Len(t, notices, 1)
	require.Equal(
		t,
		runtimectl.ActionRestart,
		notices[0].notice.Action,
	)
	require.Equal(
		t,
		runtimectl.ModeGraceful,
		notices[0].notice.Mode,
	)
	require.Equal(t, "group:chat1", notices[0].notice.PushTarget)
	require.Equal(
		t,
		"https://example.com/runtime/reply",
		notices[0].notice.ResponseURL,
	)
	require.Equal(t, "req-runtime-1", notices[0].notice.ReplyReqID)
	require.Equal(t, "user1", notices[0].notice.Actor)
	require.Equal(t, "slash", notices[0].notice.Source)
}

func TestBuildControlRuntimeActionCardStagesCompletionNotice(
	t *testing.T,
) {
	t.Parallel()

	channel := mustCreateWebSocketChannel(t, stubGateway{})
	channel.runtimeLifecycle = runtimectl.NewManager(
		runtimectl.Options{
			CurrentVersion: "v0.0.48",
		},
	)
	channel.runtimeAdminPolicy = runtimeAdminPolicyAllowlist
	channel.runtimeAdminUsers = buildAllowSet([]string{
		"user1",
	})

	card, err := channel.buildControlRuntimeActionCard(
		context.Background(),
		"chat1",
		"TestBot",
		"task-1",
		"user1",
		"https://example.com/runtime/card",
		"req-card-1",
		runtimectl.ActionRequest{
			Kind:          runtimectl.ActionUpgrade,
			Mode:          runtimectl.ModeForce,
			TargetVersion: "v0.0.48",
			Actor:         "user1",
			Source:        "card",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, card)

	notices, err := channel.runtimeCompletionNotifier.loadPending()
	require.NoError(t, err)
	require.Len(t, notices, 1)
	require.Equal(
		t,
		runtimectl.ActionUpgrade,
		notices[0].notice.Action,
	)
	require.Equal(
		t,
		runtimectl.ModeForce,
		notices[0].notice.Mode,
	)
	require.Equal(t, "group:chat1", notices[0].notice.PushTarget)
	require.Equal(
		t,
		"https://example.com/runtime/card",
		notices[0].notice.ResponseURL,
	)
	require.Equal(t, "req-card-1", notices[0].notice.ReplyReqID)
	require.Equal(t, "card", notices[0].notice.Source)
}

func TestFlushPendingRuntimeCompletionNoticesSendsResponseURL(
	t *testing.T,
) {
	t.Parallel()

	channel := mustCreateWebSocketChannel(t, stubGateway{})
	channel.runtimeLifecycle = runtimectl.NewManager(
		runtimectl.Options{
			CurrentVersion: "v0.0.48",
		},
	)

	result, err := channel.runtimeLifecycle.RequestAction(
		context.Background(),
		runtimectl.ActionRequest{
			Kind:   runtimectl.ActionRestart,
			Mode:   runtimectl.ModeGraceful,
			Actor:  "user1",
			Source: "slash",
		},
	)
	require.NoError(t, err)
	channel.stageRuntimeCompletionNotice(
		result,
		"chat1",
		"user1",
		"https://example.com/runtime/reply",
		"req-runtime-1",
	)

	calls := 0
	gotURL := ""
	gotMessage := ""
	channel.runtimeCompletionNotifier.sendResponse = func(
		_ context.Context,
		responseURL string,
		message string,
	) error {
		calls++
		gotURL = responseURL
		gotMessage = message
		return nil
	}

	channel.flushPendingRuntimeCompletionNotices(
		context.Background(),
	)

	require.Equal(t, 1, calls)
	require.Equal(
		t,
		"https://example.com/runtime/reply",
		gotURL,
	)
	require.Contains(t, gotMessage, "已完成无损重启")
	require.Contains(t, gotMessage, "当前版本：v0.0.48")

	notices, err := channel.runtimeCompletionNotifier.loadPending()
	require.NoError(t, err)
	require.Empty(t, notices)
}

func TestFlushPendingRuntimeCompletionNoticesDropsLegacyPushOnly(
	t *testing.T,
) {
	t.Parallel()

	channel := mustCreateWebSocketChannel(t, stubGateway{})
	require.NoError(
		t,
		writeRuntimeCompletionNotice(
			channel.runtimeCompletionNotifier.noticePath("legacy"),
			runtimeCompletionNotice{
				ActionID:    "legacy",
				Action:      runtimectl.ActionRestart,
				Mode:        runtimectl.ModeGraceful,
				PushTarget:  "single:user1",
				CreatedAt:   time.Now(),
				RequestedAt: time.Now(),
			},
		),
	)

	channel.flushPendingRuntimeCompletionNotices(
		context.Background(),
	)

	notices, err := channel.runtimeCompletionNotifier.loadPending()
	require.NoError(t, err)
	require.Empty(t, notices)
}

func TestFormatRuntimeCompletionMessageUpgradeMismatch(
	t *testing.T,
) {
	t.Parallel()

	message := formatRuntimeCompletionMessage(
		runtimeCompletionNotice{
			Action:         runtimectl.ActionUpgrade,
			Mode:           runtimectl.ModeGraceful,
			CurrentVersion: "v0.0.52",
			TargetVersion:  "v0.0.49",
		},
		"v0.0.48",
	)

	require.Contains(t, message, "升级前版本：v0.0.52")
	require.Contains(t, message, "当前版本：v0.0.48")
	require.Contains(t, message, "目标版本：v0.0.49")
	require.Contains(t, message, "请检查外层 start.sh。")
}

func TestFormatRuntimeCompletionMessageUpgradeIncludesSummary(
	t *testing.T,
) {
	t.Parallel()

	message := formatRuntimeCompletionMessage(
		runtimeCompletionNotice{
			Action:         runtimectl.ActionUpgrade,
			Mode:           runtimectl.ModeGraceful,
			CurrentVersion: "v0.0.52",
			TargetVersion:  "v0.0.53",
			Summary: []string{
				"one",
				"two",
			},
		},
		"v0.0.53",
	)

	require.Contains(t, message, "已完成无损升级")
	require.Contains(t, message, "已从 v0.0.52 升级到 v0.0.53")
	require.Contains(t, message, runtimeCompletionSummary)
	require.Contains(t, message, "- one")
	require.Contains(t, message, "- two")
}

func TestFormatRuntimeCompletionMessageUpgradeSameVersion(
	t *testing.T,
) {
	t.Parallel()

	message := formatRuntimeCompletionMessage(
		runtimeCompletionNotice{
			Action:         runtimectl.ActionUpgrade,
			Mode:           runtimectl.ModeGraceful,
			CurrentVersion: "v0.0.53",
			TargetVersion:  "v0.0.53",
		},
		"v0.0.53",
	)

	require.Contains(t, message, "已完成无损升级")
	require.Contains(t, message, "当前版本：v0.0.53")
	require.NotContains(t, message, "已从 v0.0.53 升级到 v0.0.53")
}
