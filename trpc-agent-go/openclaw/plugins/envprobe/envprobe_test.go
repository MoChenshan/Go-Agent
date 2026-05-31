package envprobe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testEnvCurrent = "CLAW_ENVPROBE_TEST_CURRENT_TOKEN"
	testEnvFile    = "CLAW_ENVPROBE_TEST_FILE_TOKEN"
	testEnvShell   = "CLAW_ENVPROBE_TEST_SHELL_TOKEN"
	testEnvRuntime = "CLAW_ENVPROBE_TEST_RUNTIME_TOKEN"
	testEnvEmpty   = "CLAW_ENVPROBE_TEST_EMPTY_TOKEN"
	testEnvDynamic = "CLAW_ENVPROBE_TEST_DYNAMIC_TOKEN"
)

func TestResolverProbeUsesCurrentProcessEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(testEnvCurrent, "super-secret-value")

	result, err := newResolver(t.TempDir()).Probe(testEnvCurrent)
	require.NoError(t, err)
	require.Equal(t, testEnvCurrent, result.Name)
	require.True(t, result.PresentNow)
	require.True(t, result.NonEmptyNow)
	require.True(t, result.Sensitive)
	require.False(t, result.RequiresRestartForMainProcess)
	require.Empty(t, result.DeclaredSources)
	require.Contains(
		t,
		result.SafeMessage,
		"present in the current trpc-claw process environment",
	)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "super-secret-value")
}

func TestResolverProbeUsesPlatformEnvFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	envFile := filepath.Join(t.TempDir(), "platform.env")
	require.NoError(
		t,
		os.WriteFile(
			envFile,
			[]byte("export "+testEnvFile+"=platform-token\n"),
			0o600,
		),
	)
	t.Setenv(envPlatformFile, envFile)

	result, err := newResolver(t.TempDir()).Probe(testEnvFile)
	require.NoError(t, err)
	require.True(t, result.PresentNow)
	require.True(t, result.NonEmptyNow)
	require.True(t, result.ActivatedNow)
	require.Equal(t, envFile, result.ActivatedSource)
	require.Equal(t, []string{envFile}, result.DeclaredSources)
	require.Equal(
		t,
		[]string{envFile},
		result.NonEmptyDeclaredSources,
	)
	require.False(t, result.RequiresRestartForMainProcess)
	require.Contains(t, result.SafeMessage, "loaded into the current")
	require.Equal(t, "platform-token", os.Getenv(testEnvFile))
	require.NotContains(t, result.SafeMessage, "platform-token")
}

func TestResolverProbeUsesShellRCFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(home, shellFileBashRC),
			[]byte("export "+testEnvShell+"=bashrc-token\n"),
			0o600,
		),
	)

	result, err := newResolver(t.TempDir()).Probe(testEnvShell)
	require.NoError(t, err)
	require.True(t, result.PresentNow)
	require.True(t, result.NonEmptyNow)
	require.True(t, result.ActivatedNow)
	require.Equal(t, "~/.bashrc", result.ActivatedSource)
	require.Equal(t, []string{"~/.bashrc"}, result.DeclaredSources)
	require.Equal(
		t,
		[]string{"~/.bashrc"},
		result.NonEmptyDeclaredSources,
	)
	require.False(t, result.RequiresRestartForMainProcess)
	require.Contains(t, result.SafeMessage, "loaded into the current")
	require.Equal(t, "bashrc-token", os.Getenv(testEnvShell))
	require.NotContains(t, result.SafeMessage, "bashrc-token")
}

func TestResolverProbeUsesRuntimeEnvFile(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("HOME", home)
	runtimeDir := filepath.Join(stateDir, runtimeDirName)
	require.NoError(t, os.MkdirAll(runtimeDir, 0o755))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(runtimeDir, runtimeEnvFileName),
			[]byte(testEnvRuntime+"=runtime-token\n"),
			0o600,
		),
	)

	result, err := newResolver(stateDir).Probe(testEnvRuntime)
	require.NoError(t, err)
	require.True(t, result.PresentNow)
	require.True(t, result.NonEmptyNow)
	require.True(t, result.ActivatedNow)
	require.Len(t, result.DeclaredSources, 1)
	require.Contains(
		t,
		result.DeclaredSources[0],
		filepath.Join(runtimeDirName, runtimeEnvFileName),
	)
	require.False(t, result.RequiresRestartForMainProcess)
	require.Equal(t, "runtime-token", os.Getenv(testEnvRuntime))
	require.NotContains(t, result.SafeMessage, "runtime-token")
}

func TestResolverProbeHandlesEmptyDeclarations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(home, shellFileBashRC),
			[]byte("export "+testEnvEmpty+"=\n"),
			0o600,
		),
	)

	result, err := newResolver(t.TempDir()).Probe(testEnvEmpty)
	require.NoError(t, err)
	require.False(t, result.PresentNow)
	require.Equal(t, []string{"~/.bashrc"}, result.DeclaredSources)
	require.Empty(t, result.NonEmptyDeclaredSources)
	require.False(t, result.RequiresRestartForMainProcess)
	require.Contains(t, result.SafeMessage, "looks empty")
}

func TestResolverProbeLeavesDynamicShellDeclarationInactive(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(home, shellFileBashRC),
			[]byte("export "+testEnvDynamic+"=\"$HOME/token\"\n"),
			0o600,
		),
	)

	result, err := newResolver(t.TempDir()).Probe(testEnvDynamic)
	require.NoError(t, err)
	require.False(t, result.PresentNow)
	require.False(t, result.ActivatedNow)
	require.Equal(t, []string{"~/.bashrc"}, result.DeclaredSources)
	require.Equal(
		t,
		[]string{"~/.bashrc"},
		result.NonEmptyDeclaredSources,
	)
	require.True(t, result.RequiresRestartForMainProcess)
	require.Contains(t, result.SafeMessage, "does not auto-activate")
	require.Empty(t, os.Getenv(testEnvDynamic))
}

func TestResolverProbeRejectsInvalidEnvName(t *testing.T) {
	_, err := newResolver(t.TempDir()).Probe("../bad")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid env name")
}
