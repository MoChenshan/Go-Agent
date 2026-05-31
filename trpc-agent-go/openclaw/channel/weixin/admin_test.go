package weixin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestListAdminAccountStatesAndResumeAccount(t *testing.T) {
	t.Parallel()

	stateDir := resolveStateDir(t.TempDir(), "")
	require.NoError(t, SaveAccount(stateDir, Account{
		AccountID: testAccountID,
		Token:     testToken,
		BaseURL:   defaultBaseURL,
		UserID:    testPeerID,
	}))

	state, err := loadChannelState(stateDir)
	require.NoError(t, err)
	require.NoError(
		t,
		state.setContextToken(testAccountID, testPeerID, "ctx-1"),
	)
	require.NoError(
		t,
		state.markPaused(
			testAccountID,
			time.Now().Add(time.Minute),
			"session expired",
		),
	)

	accounts, err := ListAdminAccountStates(stateDir)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, testAccountID, accounts[0].Account.AccountID)
	require.Equal(t, testPeerID, accounts[0].Account.UserID)
	require.Equal(t, 1, accounts[0].ContextPeerCount)
	require.NotNil(t, accounts[0].Status.PausedUntil)
	require.Equal(t, "session expired", accounts[0].Status.LastError)

	require.NoError(t, ResumeAccount(stateDir, testAccountID))

	resumed, err := ListAdminAccountStates(stateDir)
	require.NoError(t, err)
	require.Len(t, resumed, 1)
	require.Nil(t, resumed[0].Status.PausedUntil)
	require.Equal(t, "session expired", resumed[0].Status.LastError)
}
