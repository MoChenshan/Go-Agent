//go:build darwin || linux

package wecom

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebSocketInstanceLockPath(t *testing.T) {
	t.Parallel()

	path := websocketInstanceLockPath(
		"/tmp/openclaw/wecom/session_tracker.json",
		"bot/id:1",
	)
	require.Equal(
		t,
		"/tmp/openclaw/wecom/websocket_bot_id_1.lock",
		path,
	)
}

func TestAcquireProcessLockExclusive(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "wecom.lock")

	first, err := acquireProcessLock(path)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := acquireProcessLock(path)
	require.Error(t, err)
	require.Nil(t, second)

	require.NoError(t, first.Close())

	third, err := acquireProcessLock(path)
	require.NoError(t, err)
	require.NotNil(t, third)
	require.NoError(t, third.Close())
}
