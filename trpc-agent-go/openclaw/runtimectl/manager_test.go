package runtimectl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"github.com/stretchr/testify/require"
)

func TestAdmitRequestBlockedDuringDrain(t *testing.T) {
	t.Parallel()

	manager := NewManager(Options{
		CurrentVersion: "v0.0.48",
	})

	_, err := manager.RequestAction(
		context.Background(),
		ActionRequest{
			Kind: ActionRestart,
			Mode: ModeGraceful,
		},
	)
	require.NoError(t, err)

	handle, admitErr := manager.AdmitRequest(
		context.Background(),
		"chat-1",
	)
	require.Nil(t, handle)
	require.Error(t, admitErr)
	var admission *AdmissionError
	require.ErrorAs(t, admitErr, &admission)
	require.Equal(t, StateReady, admission.Status.State)
}

func TestForceActionCancelsAcceptedRequests(t *testing.T) {
	t.Parallel()

	manager := NewManager(Options{
		CurrentVersion: "v0.0.48",
	})

	handle, err := manager.AdmitRequest(
		context.Background(),
		"chat-1",
	)
	require.NoError(t, err)
	require.NotNil(t, handle)

	var aborted atomic.Int32
	handle.SetAbort("req-1", func(context.Context) {
		aborted.Add(1)
	})

	_, err = manager.RequestAction(
		context.Background(),
		ActionRequest{
			Kind: ActionRestart,
			Mode: ModeForce,
		},
	)
	require.NoError(t, err)
	require.Equal(t, int32(1), aborted.Load())
	require.ErrorIs(t, handle.Context().Err(), context.Canceled)
	require.NotEmpty(t, UserMessageFromContext(handle.Context()))
}

func TestRequestDoneTransitionsReady(t *testing.T) {
	t.Parallel()

	readyCh := make(chan Intent, 1)
	manager := NewManager(Options{
		CurrentVersion: "v0.0.48",
		OnReadyToExit: func(intent Intent) {
			readyCh <- intent
		},
	})

	handle, err := manager.AdmitRequest(
		context.Background(),
		"chat-1",
	)
	require.NoError(t, err)
	handle.MarkRunning()

	result, err := manager.RequestAction(
		context.Background(),
		ActionRequest{
			Kind: ActionRestart,
			Mode: ModeGraceful,
		},
	)
	require.NoError(t, err)
	require.True(t, result.Started)
	require.Equal(t, StateDraining, result.Status.State)

	handle.Done()

	select {
	case intent := <-readyCh:
		require.Equal(t, ActionRestart, intent.Action)
		require.Equal(t, ModeGraceful, intent.Mode)
	case <-time.After(2 * time.Second):
		t.Fatal("expected ready-to-exit callback")
	}
}

func TestPersistWritesIntentAndStatus(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	manager := NewManager(Options{
		CurrentVersion: "v0.0.48",
		StateDir:       stateDir,
	})

	_, err := manager.RequestAction(
		context.Background(),
		ActionRequest{
			Kind: ActionRestart,
			Mode: ModeGraceful,
		},
	)
	require.NoError(t, err)

	lifecycleDir := filepath.Join(
		stateDir,
		runtimeDirName,
		lifecycleDirName,
	)
	_, err = os.Stat(filepath.Join(lifecycleDir, statusFileName))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(lifecycleDir, intentJSONName))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(lifecycleDir, intentEnvFileName))
	require.NoError(t, err)
}

func TestUpgradeRejectsVersionsBelowMinimum(t *testing.T) {
	t.Parallel()

	manager := NewManager(Options{
		CurrentVersion: "v0.0.48",
	})

	result, err := manager.RequestAction(
		context.Background(),
		ActionRequest{
			Kind:          ActionUpgrade,
			Mode:          ModeGraceful,
			TargetVersion: "v0.0.47",
		},
	)
	require.Error(t, err)
	require.False(t, result.Started)
	require.Contains(
		t,
		err.Error(),
		"below minimum "+DefaultMinTargetVersion,
	)
	require.True(
		t,
		strings.Contains(err.Error(), "v0.0.47"),
	)
}

func TestUpgradePreviewChannelResolvesIntentVersion(t *testing.T) {
	t.Parallel()

	const previewVersion = "v0.0.91-preview.1"

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/preview/VERSION":
				_, _ = w.Write([]byte(previewVersion))
			case "/releases/" + previewVersion + "/CHANGELOG.md":
				_, _ = w.Write([]byte(
					"## " + previewVersion + " (2026-04-30)\n" +
						"- preview channel\n",
				))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	stateDir := t.TempDir()
	manager := NewManager(Options{
		CurrentVersion: "v0.0.90",
		StateDir:       stateDir,
		ReleaseBaseURL: server.URL,
		HTTPClient:     server.Client(),
	})

	result, err := manager.RequestAction(
		context.Background(),
		ActionRequest{
			Kind:          ActionUpgrade,
			Mode:          ModeGraceful,
			TargetChannel: releaseinfo.ChannelPreview,
		},
	)
	require.NoError(t, err)
	require.True(t, result.Started)
	require.NotNil(t, result.Status.Pending)
	require.Equal(t, previewVersion, result.Status.Pending.TargetVersion)
	require.Equal(
		t,
		releaseinfo.ChannelPreview,
		result.Status.Pending.TargetChannel,
	)

	intentEnv := filepath.Join(
		stateDir,
		runtimeDirName,
		lifecycleDirName,
		intentEnvFileName,
	)
	data, err := os.ReadFile(intentEnv)
	require.NoError(t, err)
	text := string(data)
	require.Contains(
		t,
		text,
		"TRPC_CLAW_LIFECYCLE_TARGET_VERSION='"+previewVersion+"'",
	)
	require.Contains(
		t,
		text,
		"TRPC_CLAW_LIFECYCLE_TARGET_CHANNEL='preview'",
	)
}
