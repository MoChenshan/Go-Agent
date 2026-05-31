package wecom

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"github.com/stretchr/testify/require"
)

type mockLocalFileSender struct {
	mockSender
	lastFilePath string
	filePaths    []string
}

func (m *mockLocalFileSender) SendLocalFile(
	_ context.Context,
	chatID string,
	path string,
) error {
	m.lastChatID = chatID
	m.lastFilePath = path
	m.filePaths = append(m.filePaths, path)
	return nil
}

func TestParseRuntimeDebugBundleRequest(t *testing.T) {
	t.Parallel()

	req, err := parseRuntimeDebugBundleRequest(nil)
	require.NoError(t, err)
	require.False(t, req.Full)
	require.Zero(t, req.TotalLimitBytes)

	req, err = parseRuntimeDebugBundleRequest(
		[]string{runtimeActionFull},
	)
	require.NoError(t, err)
	require.True(t, req.Full)
	require.Equal(
		t,
		int64(runtimeBundleFullDefaultTotalBytes),
		req.TotalLimitBytes,
	)

	req, err = parseRuntimeDebugBundleRequest(
		[]string{runtimeActionFull, "80mb"},
	)
	require.NoError(t, err)
	require.Equal(t, int64(80*1024*1024), req.TotalLimitBytes)

	req, err = parseRuntimeDebugBundleRequest(
		[]string{runtimeActionFull, "64"},
	)
	require.NoError(t, err)
	require.Equal(t, int64(64*1024*1024), req.TotalLimitBytes)

	_, err = parseRuntimeDebugBundleRequest(
		[]string{runtimeActionFull, "bogus"},
	)
	require.Error(t, err)
}

func TestHandleRuntimeCommandBundleSendsArchive(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	paths, err := promptasset.EnsureDefaultFiles(stateDir)
	require.NoError(t, err)
	require.NoError(
		t,
		assistantname.WriteFile(paths.IdentityFile, "winechord"),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(stateDir, runtimeBundleConfigFileName),
			[]byte("agent:\n  persona: pragmatic\n"),
			0o600,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(stateDir, runtimeBundleSessionDBName),
			[]byte("sqlite"),
			0o600,
		),
	)
	require.NoError(
		t,
		os.MkdirAll(
			filepath.Join(stateDir, runtimeBundleDebugDirName),
			0o700,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(
				stateDir,
				runtimeBundleDebugDirName,
				"trace.json",
			),
			[]byte(`{"ok":true}`),
			0o600,
		),
	)

	tracker := newSessionTrackerWithPath(
		sessionTrackerStorePath(stateDir),
	)
	tracker.setAssistantAlias("wecom:dm:user1", "林妹妹")

	ch := &Channel{
		chatPolicy:            chatPolicyOpen,
		stateDir:              stateDir,
		runtimeTempRoot:       filepath.Join(stateDir, "runtime", "tmp"),
		assistantIdentityFile: paths.IdentityFile,
	}
	sender := &mockLocalFileSender{}

	err = ch.handleRuntimeCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		"user1",
		"",
		parsedCommand{
			keyword: runtimeKeyword,
			args:    []string{runtimeActionBundle},
		},
		sender,
	)
	require.NoError(t, err)
	require.NotEmpty(t, sender.lastFilePath)
	require.Contains(t, sender.lastText, "已打包并回传调试资料")

	reader, err := zip.OpenReader(sender.lastFilePath)
	require.NoError(t, err)
	defer reader.Close()

	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	require.Contains(t, names, runtimeBundleManifestName)
	require.Contains(t, names, runtimeBundleConfigFileName)
	require.Contains(t, names, assistantname.FileName)
	require.Contains(t, names, "prompts/instruction/memory.md")
	require.Contains(t, names, "prompts/system/runtime_identity.md")
	require.Contains(t, names, "wecom/session_tracker.json")
	require.Contains(t, names, "sessions.sqlite")
	require.Contains(t, names, "debug/trace.json")
}

func TestHandleRuntimeCommandBundleNeedsLocalFileSender(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{chatPolicy: chatPolicyOpen}
	sender := &mockSender{}

	err := ch.handleRuntimeCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		"user1",
		"",
		parsedCommand{
			keyword: runtimeKeyword,
			args:    []string{runtimeActionBundle},
		},
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, runtimeBundleSenderUnavailable)
}
