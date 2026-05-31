package weixin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTextTargetRoundTrip(t *testing.T) {
	t.Parallel()

	target := buildTextTarget(testAccountID, testPeerID)
	parsed, err := parseTextTarget(target)
	require.NoError(t, err)
	require.Equal(t, testAccountID, parsed.AccountID)
	require.Equal(t, testPeerID, parsed.PeerID)
}

func TestSendTextUsesStoredContextTokenAndSplitsReply(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := resolveStateDir(tmpDir, "")
	backend := newFakeBackend(t, nil)
	server := httptest.NewServer(http.HandlerFunc(backend.handler))
	defer server.Close()

	require.NoError(t, SaveAccount(stateDir, Account{
		AccountID: testAccountID,
		Token:     testToken,
		BaseURL:   server.URL,
	}))

	channel := newTestChannel(
		t,
		tmpDir,
		server.URL,
		"",
		&stubGateway{},
	)
	require.NoError(
		t,
		channel.state.setContextToken(testAccountID, testPeerID, "ctx-send"),
	)

	longText := strings.Repeat("a", maxReplyRunes+10)
	err := channel.SendText(
		context.Background(),
		buildTextTarget(testAccountID, testPeerID),
		longText,
	)
	require.NoError(t, err)

	waitForCondition(t, func() bool {
		return backend.sendCount() == 2
	})
	require.Equal(t, 2, backend.sendCount())
	for _, part := range backend.sendBodies {
		require.Equal(t, "ctx-send", part.Message.ContextToken)
		require.Equal(t, testPeerID, part.Message.ToUserID)
	}
}
