package weixin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSaveAccountRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := resolveStateDir(t.TempDir(), "")
	require.NoError(t, SaveAccount(stateDir, Account{
		AccountID: testAccountID,
		Token:     testToken,
		BaseURL:   defaultBaseURL,
		UserID:    "owner@im.wechat",
	}))

	accounts, err := ListAccounts(stateDir)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, testAccountID, accounts[0].AccountID)
	require.Equal(t, testToken, accounts[0].Token)
	require.Equal(t, defaultBaseURL, accounts[0].BaseURL)
	require.Equal(t, "owner@im.wechat", accounts[0].UserID)
}

func TestContextTokenAndPauseStateRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := resolveStateDir(t.TempDir(), "")
	require.NoError(t, SaveAccount(stateDir, Account{
		AccountID: testAccountID,
		Token:     testToken,
	}))

	state, err := loadChannelState(stateDir)
	require.NoError(t, err)
	require.NoError(
		t,
		state.setContextToken(testAccountID, testPeerID, "ctx-1"),
	)
	require.Equal(
		t,
		"ctx-1",
		state.contextToken(testAccountID, testPeerID),
	)

	now := time.Now()
	until := now.Add(2 * time.Second)
	require.NoError(
		t,
		state.markPaused(testAccountID, until, "expired"),
	)

	reloaded, err := loadChannelState(stateDir)
	require.NoError(t, err)
	require.Equal(
		t,
		"ctx-1",
		reloaded.contextToken(testAccountID, testPeerID),
	)

	status := reloaded.statusSnapshot(testAccountID)
	require.NotNil(t, status.PausedUntil)
	require.Equal(t, "expired", status.LastError)
	require.WithinDuration(t, until, *status.PausedUntil, time.Second)

	remaining, err := reloaded.pauseRemaining(
		testAccountID,
		until.Add(time.Second),
	)
	require.NoError(t, err)
	require.Zero(t, remaining)
	require.Nil(
		t,
		reloaded.statusSnapshot(testAccountID).PausedUntil,
	)
}
