package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	"github.com/stretchr/testify/require"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	testWeComIdentityCacheDir  = "wecom"
	testWeComIdentityCacheFile = "user_identity_cache.json"
	testWeComIdentityVersion   = 1
)

type stubMemoryUserLabelChannel struct {
	target wecomchannel.AdminTarget
}

func (s stubMemoryUserLabelChannel) ID() string {
	return "wecom"
}

func (s stubMemoryUserLabelChannel) Run(ctx context.Context) error {
	return ctx.Err()
}

func (s stubMemoryUserLabelChannel) WeComAdminTarget() wecomchannel.AdminTarget {
	return s.target
}

func TestMemoryAdminKnownWeComUserID(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"T00320026A",
		memoryAdminKnownWeComUserID("wecom:dm:T00320026A"),
	)
	require.Equal(
		t,
		"T00320026A",
		memoryAdminKnownWeComUserID(
			"wecom:chat:group1:user:T00320026A",
		),
	)
	require.Equal(
		t,
		"T00320026A",
		memoryAdminKnownWeComUserID("T00320026A"),
	)
	require.Empty(
		t,
		memoryAdminKnownWeComUserID("wecom:chat:group1"),
	)
}

func TestFormatMemoryKnownUserLabel(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"RTX wineguo (Guo Qizhou)",
		formatMemoryKnownUserLabel(
			wecomchannel.KnownUserIdentity{
				AccountName: "wineguo",
				DisplayName: "Guo Qizhou",
			},
		),
	)
	require.Equal(
		t,
		"RTX wineguo",
		formatMemoryKnownUserLabel(
			wecomchannel.KnownUserIdentity{AccountName: "wineguo"},
		),
	)
	require.Equal(
		t,
		"Guo Qizhou",
		formatMemoryKnownUserLabel(
			wecomchannel.KnownUserIdentity{DisplayName: "Guo Qizhou"},
		),
	)
}

func TestRuntimeMemoryUserLabelResolver_ResolvesWeComCache(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	writeMemoryAdminWeComIdentityCache(
		t,
		stateDir,
		map[string]map[string]string{
			"T00320026A": {
				"UserID":      "T00320026A",
				"AccountName": "wineguo",
				"DisplayName": "Guo Qizhou",
			},
		},
	)

	channels := []occhannel.Channel{
		stubMemoryUserLabelChannel{
			target: wecomchannel.AdminTarget{
				Name:     "wecom",
				StateDir: stateDir,
			},
		},
	}
	resolver := newRuntimeMemoryUserLabelResolver(channels)
	require.NotNil(t, resolver)
	require.Equal(
		t,
		"RTX wineguo (Guo Qizhou)",
		resolver.ResolveMemoryUserLabel(
			"openclaw",
			"wecom:dm:T00320026A",
		),
	)
	require.Equal(
		t,
		"RTX wineguo (Guo Qizhou)",
		resolver.ResolveMemoryUserLabel(
			"openclaw",
			"wecom:chat:group1:user:T00320026A",
		),
	)
	require.Empty(
		t,
		resolver.ResolveMemoryUserLabel(
			"openclaw",
			"wecom:chat:group1",
		),
	)
}

func writeMemoryAdminWeComIdentityCache(
	t *testing.T,
	stateDir string,
	users map[string]map[string]string,
) {
	t.Helper()

	cacheDir := filepath.Join(stateDir, testWeComIdentityCacheDir)
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))

	payload := map[string]any{
		"Version": testWeComIdentityVersion,
		"Users":   users,
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(
				cacheDir,
				testWeComIdentityCacheFile,
			),
			data,
			0o600,
		),
	)
}
