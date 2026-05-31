package workspacecfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	workspacesDirName         = "workspaces"
	scratchDirName            = "scratch"
	runtimeDirName            = "runtime"
	tempDirName               = "tmp"
	uploadsDirName            = "uploads"
	tempWorkspacesRootDirName = "trpc-claw-workspaces"
	tempRuntimeRootDirName    = "trpc-claw-runtime"
	configRootDirName         = ".trpc-agent-go"
	agentsDocFileName         = "AGENTS.md"
	defaultDirPerm            = 0o755
)

// Defaults are the resolved runtime workspace defaults.
type Defaults struct {
	DefaultWorkdir string
	ScratchRoot    string
}

// NormalizeDir expands common path syntax and normalizes the result.
func NormalizeDir(
	raw string,
	requireExisting bool,
) (string, error) {
	path := strings.TrimSpace(os.ExpandEnv(raw))
	if path == "" {
		return "", nil
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		switch {
		case path == "~":
			path = home
		case strings.HasPrefix(path, "~/"):
			path = filepath.Join(
				home,
				strings.TrimPrefix(path, "~/"),
			)
		}
	}

	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			return "", err
		}
	}
	path = filepath.Clean(path)
	if !requireExisting {
		return path, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return path, nil
}

// ImplicitDefaultWorkdir resolves the runtime's implicit default workspace.
func ImplicitDefaultWorkdir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	path, err := NormalizeDir(cwd, true)
	if err != nil {
		return ""
	}
	return path
}

// DefaultUserAgentsPath resolves the user-level AGENTS.md path.
func DefaultUserAgentsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, configRootDirName, agentsDocFileName)
}

// ExistingUserAgentsPath resolves the user-level AGENTS.md when present.
func ExistingUserAgentsPath() string {
	path := DefaultUserAgentsPath()
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info == nil || info.IsDir() {
		return ""
	}
	return path
}

// DefaultScratchRoot resolves the runtime's default scratch root.
func DefaultScratchRoot(stateDir string) string {
	if path := stateSubdir(
		stateDir,
		workspacesDirName,
		scratchDirName,
	); path != "" {
		return path
	}
	return filepath.Join(
		os.TempDir(),
		tempWorkspacesRootDirName,
		scratchDirName,
	)
}

// DefaultTempRoot resolves the runtime's default temp root.
func DefaultTempRoot(stateDir string) string {
	if path := stateSubdir(
		stateDir,
		runtimeDirName,
		tempDirName,
	); path != "" {
		return path
	}
	return filepath.Join(
		os.TempDir(),
		tempRuntimeRootDirName,
		tempDirName,
	)
}

// DefaultUploadsRoot resolves the runtime-managed uploads root.
func DefaultUploadsRoot(stateDir string) string {
	return stateSubdir(stateDir, uploadsDirName)
}

// EnsureScratchRoot normalizes the scratch root and creates it when
// needed so runtime guidance never points at a missing directory.
func EnsureScratchRoot(raw string) (string, error) {
	return ensureDir(raw, "scratch root")
}

// EnsureTempRoot normalizes the runtime temp root and creates it.
func EnsureTempRoot(raw string) (string, error) {
	return ensureDir(raw, "runtime temp root")
}

func ensureDir(raw string, label string) (string, error) {
	path, err := NormalizeDir(raw, false)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := os.MkdirAll(path, defaultDirPerm); err != nil {
		return "", fmt.Errorf(
			"create %s %q: %w",
			label,
			path,
			err,
		)
	}
	return path, nil
}

func stateSubdir(stateDir string, parts ...string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	path, err := NormalizeDir(stateDir, false)
	if err != nil || path == "" {
		return ""
	}
	joined := make([]string, 0, len(parts)+1)
	joined = append(joined, path)
	joined = append(joined, parts...)
	return filepath.Join(joined...)
}
