package workspacecfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeDirExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := NormalizeDir("~/repo", false)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "repo"), path)
}

func TestImplicitDefaultWorkdirUsesCurrentWorkingDirectory(t *testing.T) {
	workdir := t.TempDir()

	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(wd))
	})

	require.Equal(t, workdir, ImplicitDefaultWorkdir())
}

func TestDefaultUserAgentsPathUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.Equal(
		t,
		filepath.Join(home, ".trpc-agent-go", "AGENTS.md"),
		DefaultUserAgentsPath(),
	)
}

func TestExistingUserAgentsPathSkipsMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.Empty(t, ExistingUserAgentsPath())

	agentsPath := filepath.Join(home, ".trpc-agent-go", "AGENTS.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(agentsPath), 0o755))
	require.NoError(t, os.WriteFile(agentsPath, []byte("rules"), 0o600))
	require.Equal(t, agentsPath, ExistingUserAgentsPath())
}

func TestDefaultScratchRootUsesStateDir(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join(t.TempDir(), "state")
	require.Equal(
		t,
		filepath.Join(stateDir, "workspaces", "scratch"),
		DefaultScratchRoot(stateDir),
	)
}

func TestDefaultScratchRootUsesTempDirWithoutStateDir(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		filepath.Join(
			os.TempDir(),
			"trpc-claw-workspaces",
			"scratch",
		),
		DefaultScratchRoot(""),
	)
}

func TestDefaultTempRootUsesStateDir(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join(t.TempDir(), "state")
	require.Equal(
		t,
		filepath.Join(stateDir, "runtime", "tmp"),
		DefaultTempRoot(stateDir),
	)
}

func TestDefaultTempRootUsesTempDirWithoutStateDir(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		filepath.Join(
			os.TempDir(),
			"trpc-claw-runtime",
			"tmp",
		),
		DefaultTempRoot(""),
	)
}

func TestDefaultUploadsRootUsesStateDir(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join(t.TempDir(), "state")
	require.Equal(
		t,
		filepath.Join(stateDir, "uploads"),
		DefaultUploadsRoot(stateDir),
	)
}

func TestDefaultUploadsRootSkipsEmptyStateDir(t *testing.T) {
	t.Parallel()

	require.Empty(t, DefaultUploadsRoot(""))
}

func TestEnsureScratchRootCreatesDirectory(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "scratch")

	got, err := EnsureScratchRoot(root)
	require.NoError(t, err)
	require.Equal(t, root, got)

	info, err := os.Stat(root)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestEnsureTempRootCreatesDirectory(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "runtime", "tmp")

	got, err := EnsureTempRoot(root)
	require.NoError(t, err)
	require.Equal(t, root, got)

	info, err := os.Stat(root)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}
