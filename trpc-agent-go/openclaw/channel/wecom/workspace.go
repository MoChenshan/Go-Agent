package wecom

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/workspacecfg"
)

const (
	agentsDocFileName = "AGENTS.md"
	gitDirName        = ".git"

	workspaceDisplayUnset = "未设置"

	workspaceNoteHeaderDefault  = "Runtime default coding workspace:"
	workspaceNoteHeaderOverride = "Runtime chat coding workspace override:"
	workspaceReplyLabelPath     = "工作区: "
	workspaceReplyLabelGitRoot  = "Git 根: "

	workspaceArtifactDirPerm = 0o755
)

type workspaceFacts struct {
	Path       string
	GitRoot    string
	AgentsPath string
}

func resolveChannelCodingDefaults(
	configuredWorkdir string,
	configuredScratchRoot string,
	stateDir string,
) (workspacecfg.Defaults, error) {
	defaults := workspacecfg.Defaults{
		DefaultWorkdir: workspacecfg.ImplicitDefaultWorkdir(),
		ScratchRoot:    workspacecfg.DefaultScratchRoot(stateDir),
	}

	workdir, err := workspacecfg.NormalizeDir(
		configuredWorkdir,
		true,
	)
	if err != nil {
		return workspacecfg.Defaults{}, err
	}
	if workdir != "" {
		defaults.DefaultWorkdir = workdir
	}

	scratchRoot, err := workspacecfg.NormalizeDir(
		configuredScratchRoot,
		false,
	)
	if err != nil {
		return workspacecfg.Defaults{}, err
	}
	if scratchRoot != "" {
		defaults.ScratchRoot = scratchRoot
	}
	defaults.ScratchRoot, err = workspacecfg.EnsureScratchRoot(
		defaults.ScratchRoot,
	)
	if err != nil {
		return workspacecfg.Defaults{}, fmt.Errorf(
			"prepare scratch root: %w",
			err,
		)
	}
	return defaults, nil
}

func normalizeWorkspacePath(
	raw string,
	requireExisting bool,
) (string, error) {
	path, err := workspacecfg.NormalizeDir(raw, requireExisting)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	return filepath.Clean(path), nil
}

func findWorkspaceGitRoot(path string) string {
	current := filepath.Clean(strings.TrimSpace(path))
	for current != "" {
		if _, err := os.Stat(filepath.Join(current, gitDirName)); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

func findNearestWorkspaceInstruction(path string) string {
	current := filepath.Clean(strings.TrimSpace(path))
	stop := findWorkspaceGitRoot(current)
	if stop == "" {
		stop = current
	}
	for current != "" {
		candidate := filepath.Join(current, agentsDocFileName)
		if info, err := os.Stat(candidate); err == nil &&
			info != nil && !info.IsDir() {
			return candidate
		}
		if current == stop {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return workspacecfg.ExistingUserAgentsPath()
}

func describeWorkspace(path string) workspaceFacts {
	path = strings.TrimSpace(path)
	if path == "" {
		return workspaceFacts{}
	}
	return workspaceFacts{
		Path:       path,
		GitRoot:    findWorkspaceGitRoot(path),
		AgentsPath: findNearestWorkspaceInstruction(path),
	}
}

func formatWorkspaceDisplay(
	custom string,
	fallback string,
) string {
	custom = strings.TrimSpace(custom)
	if custom != "" {
		return custom
	}
	fallback = strings.TrimSpace(fallback)
	if fallback != "" {
		return fallback
	}
	return workspaceDisplayUnset
}

func buildCodingWorkspaceNote(
	custom string,
	fallback string,
	scratchRoot string,
) string {
	path := strings.TrimSpace(custom)
	header := workspaceNoteHeaderOverride
	if path == "" {
		path = strings.TrimSpace(fallback)
		header = workspaceNoteHeaderDefault
	}
	if path == "" {
		return ""
	}

	facts := describeWorkspace(path)
	lines := []string{
		header,
		"Use " + facts.Path + " as the default workdir for " +
			"repo inspection, editing, builds, and " +
			"verification unless the user explicitly " +
			"picks another repo.",
		"Treat current repo, current workspace, or this " +
			"repo as this path unless the user explicitly " +
			"names another repo.",
		"Keep direct uploads and generated artifacts out " +
			"of this repo workspace unless the user " +
			"explicitly targets repo files here.",
	}
	if facts.GitRoot != "" && facts.GitRoot != facts.Path {
		lines = append(
			lines,
			"Git repository root: "+facts.GitRoot,
		)
	}
	if facts.AgentsPath != "" {
		lines = append(
			lines,
			"Effective AGENTS.md: "+facts.AgentsPath,
		)
	}
	scratchRoot = strings.TrimSpace(scratchRoot)
	if scratchRoot != "" {
		lines = append(
			lines,
			"Scratch repo root for standalone tasks: "+
				scratchRoot,
		)
	}
	return strings.Join(lines, "\n")
}

func ensureChannelArtifactOutputRoot(
	scratchRoot string,
) (string, error) {
	scratchRoot = strings.TrimSpace(scratchRoot)
	if scratchRoot == "" {
		return "", nil
	}
	outputRoot := filepath.Join(
		scratchRoot,
		replyDeliveryOutputDirName,
	)
	if err := os.MkdirAll(
		outputRoot,
		workspaceArtifactDirPerm,
	); err != nil {
		return "", fmt.Errorf(
			"create artifact output root %q: %w",
			outputRoot,
			err,
		)
	}
	return outputRoot, nil
}

func ensureChannelTempRoot(stateDir string) (string, error) {
	return workspacecfg.EnsureTempRoot(
		workspacecfg.DefaultTempRoot(stateDir),
	)
}

func channelManagedUploadsRoot(stateDir string) string {
	return workspacecfg.DefaultUploadsRoot(stateDir)
}

func buildReplyWorkspacePrefix(
	custom string,
	fallback string,
) string {
	path := strings.TrimSpace(custom)
	if path == "" {
		path = strings.TrimSpace(fallback)
	}
	if path == "" {
		return ""
	}

	facts := describeWorkspace(path)
	lines := []string{
		workspaceReplyLabelPath + facts.Path,
	}
	if facts.GitRoot != "" && facts.GitRoot != facts.Path {
		lines = append(
			lines,
			workspaceReplyLabelGitRoot+facts.GitRoot,
		)
	}
	return strings.Join(lines, "\n")
}

func (c *Channel) effectiveWorkspacePath(
	info *sessionInfo,
) string {
	if info != nil && strings.TrimSpace(info.workspacePath) != "" {
		return strings.TrimSpace(info.workspacePath)
	}
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.defaultCodingWorkspace)
}
