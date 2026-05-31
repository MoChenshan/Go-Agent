package wecom

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeComAdminActivationStatus(t *testing.T) {
	t.Parallel()

	t.Run("requires ai mode", func(t *testing.T) {
		t.Parallel()

		channel := &Channel{
			botMode:        botModeNotification,
			connectionMode: connectionModeWebSocket,
		}

		require.Equal(
			t,
			AdminActivationStatus{
				Reason: adminActivationReasonAIModeRequired,
			},
			channel.WeComAdminActivationStatus(),
		)
	})

	t.Run("requires websocket mode", func(t *testing.T) {
		t.Parallel()

		channel := &Channel{
			botMode:        botModeAI,
			connectionMode: connectionModeWebhook,
		}

		require.Equal(
			t,
			AdminActivationStatus{
				Reason: adminActivationReasonWebSocketRequired,
			},
			channel.WeComAdminActivationStatus(),
		)
	})

	t.Run("reports not connected", func(t *testing.T) {
		t.Parallel()

		channel := &Channel{
			botMode:        botModeAI,
			connectionMode: connectionModeWebSocket,
		}

		require.Equal(
			t,
			AdminActivationStatus{
				Supported: true,
				Reason:    adminActivationReasonNotConnected,
			},
			channel.WeComAdminActivationStatus(),
		)
	})

	t.Run("reports available", func(t *testing.T) {
		t.Parallel()

		channel := &Channel{
			botMode:        botModeAI,
			connectionMode: connectionModeWebSocket,
		}
		writer := newAckWSWriter()
		channel.setWebSocketPushWriter(writer)
		defer channel.clearWebSocketPushWriter(writer)

		require.Equal(
			t,
			AdminActivationStatus{
				Supported: true,
				Available: true,
			},
			channel.WeComAdminActivationStatus(),
		)
	})
}

func TestWeComAdminAllowsUser(t *testing.T) {
	t.Parallel()

	channel := &Channel{
		chatPolicy: chatPolicyAllowlist,
		allowUsers: buildAllowSet([]string{"user1"}),
	}

	require.True(t, channel.WeComAdminAllowsUser("user1"))
	require.False(t, channel.WeComAdminAllowsUser("user2"))
}

func TestBuildAdminDirectMessageTarget(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"single:user1",
		BuildAdminDirectMessageTarget(" user1 "),
	)
}

func TestAdminActivationTargetIsAcceptedBySendText(t *testing.T) {
	t.Parallel()

	channel := mustCreateWebSocketChannel(t, stubGateway{})
	writer := newAckWSWriter()
	channel.setWebSocketPushWriter(writer)
	defer channel.clearWebSocketPushWriter(writer)

	err := channel.SendText(
		context.Background(),
		BuildAdminDirectMessageTarget("user1"),
		"hello",
	)
	require.NoError(t, err)
	require.Len(t, writer.frames, 1)
}
