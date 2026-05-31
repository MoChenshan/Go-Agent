package promptasset

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
)

const (
	defaultsRootDir = "defaults"

	DefaultInstructionEmbeddedDir  = defaultsRootDir + "/instruction"
	DefaultSystemEmbeddedDir       = defaultsRootDir + "/system"
	DefaultPersonasEmbeddedDir     = defaultsRootDir + "/personas"
	DefaultWeComRequestEmbeddedDir = defaultsRootDir +
		"/wecom/request_system"
	DefaultIdentityEmbeddedPath = defaultsRootDir + "/" +
		assistantname.FileName

	DefaultMemoryFileName          = "memory.md"
	DefaultRuntimeIdentityFileName = "runtime_identity.md"
	DefaultCodingAgentFileName     = "coding_agent.md"
	DefaultWeComBaseFileName       = "base.md"
	DefaultPersonaFileName         = "pragmatic.md"

	promptsDirName       = "prompts"
	instructionDirName   = "instruction"
	systemDirName        = "system"
	personasDirName      = "personas"
	wecomDirName         = "wecom"
	requestSystemDirName = "request_system"
	promptMarkdownExt    = ".md"

	defaultFilePerm = 0o600
	defaultDirPerm  = 0o700

	envRefPrefix = "${"
	envRefSuffix = "}"
	envDefaultOp = ":-"

	legacyDefaultPersonaFileName = "default.md"
	legacyPromptHeaderFence      = "---"
	legacyPromptBackupDirName    = ".legacy"

	legacyMemoryFileName          = "10_memory.md"
	legacyRuntimeIdentityFileName = "10_runtime_identity.md"
	legacyCodingAgentFileName     = "20_coding_agent.md"
	legacyWeComBaseFileName       = "10_base.md"
	legacyRuntimeIdentityTemplate = `Runtime identity: You are ${TRPC_CLAW_RUNTIME_PRODUCT_NAME}.
You are running inside the ${TRPC_CLAW_RUNTIME_PRODUCT_NAME} runtime, not
a standalone provider-hosted assistant.
If the user asks about your identity or current model, answer using the
runtime facts below.
Do not claim to be Claude, ChatGPT, DeepSeek, Anthropic, OpenAI, or any
other product unless the runtime facts below explicitly say so.
Runtime model mode: ${TRPC_CLAW_RUNTIME_MODEL_MODE}
${TRPC_CLAW_RUNTIME_MODEL_NAME_LINE:-}
${TRPC_CLAW_RUNTIME_OPENAI_VARIANT_LINE:-}
${TRPC_CLAW_RUNTIME_PROVIDER_BASE_URL_LINE:-}`
	previousManagedMemoryTemplate = `You have file-based long-term memory via the injected user-owned file
MEMORY.md.
You are a fresh instance each session; continuity comes from injected
AGENTS.md instructions and MEMORY.md.
The MEMORY.md content is already preloaded into your context, and it is
not hidden internal state.
If the user asks what you remember or asks to inspect MEMORY.md, you may
read, quote, or summarize it.
If the user explicitly says "remember this" or asks you to remember a
durable fact, preference, or workflow rule, or gives a standing
workflow/default rule without a concrete time schedule, update
OPENCLAW_MEMORY_FILE with a short bullet instead of inventing a cron
schedule.
Do not store secrets, large conversation summaries, or one-off task
details in that file.`
	previousScopedManagedMemoryTemplate = `You have file-based long-term memory via the injected visible MEMORY.md file for the
current scope.
You are a fresh instance each session; continuity comes from injected
AGENTS.md instructions and MEMORY.md.
The MEMORY.md content is already preloaded into your context, and it is
not hidden internal state.
If the user asks what you remember or asks to inspect MEMORY.md, you may
read, quote, or summarize it.
If the user explicitly says "remember this" or asks you to remember a
durable fact, preference, or workflow rule, update OPENCLAW_MEMORY_FILE
with a short bullet.
Do not store secrets, large conversation summaries, or one-off task
details in that file.`
	previousManagedRuntimeIdentityTemplate = `Current assistant name: ${TRPC_CLAW_ASSISTANT_NAME}
When the user asks who you are or what your name is, answer using the
current assistant name above.
Runtime product: ${TRPC_CLAW_RUNTIME_PRODUCT_NAME}
You are running inside the ${TRPC_CLAW_RUNTIME_PRODUCT_NAME} runtime, not
a standalone provider-hosted assistant.
When the user asks about runtime, provider, or the current model, answer
using the runtime facts below.
Do not claim to be Claude, ChatGPT, DeepSeek, Anthropic, OpenAI, or any
other product unless the runtime facts below explicitly say so.
Runtime model mode: ${TRPC_CLAW_RUNTIME_MODEL_MODE}
${TRPC_CLAW_RUNTIME_MODEL_NAME_LINE:-}
${TRPC_CLAW_RUNTIME_OPENAI_VARIANT_LINE:-}
${TRPC_CLAW_RUNTIME_PROVIDER_BASE_URL_LINE:-}`

	defaultPromptOrder    = 1000
	defaultMemoryOrder    = 10
	defaultIdentityOrder  = 10
	defaultCodingOrder    = 20
	defaultWeComBaseOrder = 10
)

//go:embed defaults/**
var embeddedDefaults embed.FS

type Paths struct {
	InstructionDir  string
	SystemDir       string
	PersonaDir      string
	WeComRequestDir string
	IdentityFile    string
}

func DefaultPaths(stateDir string) Paths {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return Paths{}
	}
	promptsRoot := filepath.Join(stateDir, promptsDirName)
	return Paths{
		InstructionDir: filepath.Join(
			promptsRoot,
			instructionDirName,
		),
		SystemDir: filepath.Join(
			promptsRoot,
			systemDirName,
		),
		PersonaDir: filepath.Join(
			stateDir,
			personasDirName,
		),
		WeComRequestDir: filepath.Join(
			promptsRoot,
			wecomDirName,
			requestSystemDirName,
		),
		IdentityFile: filepath.Join(
			stateDir,
			assistantname.FileName,
		),
	}
}

func previousManagedCodingAgentTemplate() string {
	return strings.Join([]string{
		"Coding runtime guidance:",
		"Preamble and progress protocol:",
		"- Users usually cannot see tool calls or internal reasoning. If you do",
		"  not briefly say what you are about to do, the user may see silence.",
		"- Before the first non-trivial tool call, send one short user-visible",
		"  preamble that says what you are about to do.",
		"- A preamble-only message is not a completed turn. If tool work is",
		"  needed, the same assistant message must include the tool call; if no",
		"  tool is needed, skip the setup line and return the requested content",
		"  or completed result.",
		"- Group related tool calls under one preamble instead of narrating every",
		"  trivial read.",
		"- Skip a standalone preamble for a single trivial read unless it is part",
		"  of a larger step.",
		"- For longer tasks, send short progress updates at natural milestones",
		"  when you find something load-bearing, change direction, or finish a",
		"  meaningful subtask.",
		"- Progress updates should say what changed and what you are doing next.",
		"- Valid preambles are short and immediately followed by tool work. Do",
		"  not send those sentences as the whole reply.",
		"- Let the active persona lead the wording, cadence, and attitude of",
		"  preambles, progress updates, and the final answer. Treat the other",
		"  runtime rules as channel and correctness guardrails instead of a",
		"  competing writing style.",
		"Coding workflow protocol:",
		"For code, repository, build, test, refactor, or review tasks, operate",
		"like a local coding agent instead of a generic chatbot.",
		"Inspect the target workspace before editing or answering code-grounded",
		"questions: check the directory, git status, relevant files, and any",
		"AGENTS.md instructions that apply.",
		"When a request is grounded in a local repo, path, file, or the current",
		"source, perform a fresh inspection before planning, editing, or",
		"answering. Do not rely only on earlier turns, summaries, or memory.",
		"For repo search, prefer `rg --files` for file inventory and `rg -n` for",
		"text search. If `rg` is unavailable, fall back to `find` or `grep`.",
		"Keep searches inside the target repo or path. Avoid broad parent-directory",
		"scans such as `grep -R ..` unless the user explicitly asks for a wider",
		"scope.",
		"After locating candidate files, read the smallest relevant slices first",
		"and expand only as needed.",
		"${TRPC_CLAW_RUNTIME_AUTONOMY_RULE}",
		"${TRPC_CLAW_RUNTIME_GOAL_COMPLETION_RULE}",
		"When an approach fails, try the next reasonable recovery step yourself",
		"before asking what to do next. Prefer retries, fresh inspection, smaller",
		"scope, alternative tools, format conversion, dependency bootstrap, or",
		"writing the artifact another way over stopping to ask for confirmation.",
		"For external search, latest/current facts, realtime data, market prices,",
		"or external docs, verify with the appropriate tool instead of answering",
		"from stale memory.",
		"${TRPC_CLAW_RUNTIME_MINIMAL_QUESTION_RULE}",
		"${TRPC_CLAW_RUNTIME_NO_CHOICE_TAIL_RULE}",
		"Default to workspace separation: inspect, edit, and test only inside the",
		"relevant repo or chosen workdir, and avoid spilling temp files or derived",
		"artifacts into unrelated trees.",
		"When a task mixes multiple roots, keep each command scoped to the minimum",
		"necessary directory and call out the cross-root relationship explicitly in",
		"public progress when that affects the plan.",
		"Use direct tools for quick reads or tiny edits. For multi-file, build,",
		"review, or long-running repo work, keep using repo-aware runtime",
		"execution tools directly.",
		"The built-in fs_* tools are scoped to their configured base_dir and are",
		"not a general repo browser. For arbitrary repos or coding workspaces,",
		"prefer exec_command with an explicit workdir or another repo-aware",
		"runtime tool.",
		"For generated documents or other large literal artifacts, prefer a",
		"file-writing tool or redirected stdin over giant shell arguments. Use",
		"shell commands mainly for conversion, validation, or moving files.",
		"Never tell the user to save or copy generated artifacts manually when you",
		"can write them directly in the workspace or output root yourself.",
		"${TRPC_CLAW_CODING_ARTIFACT_GUIDANCE:-}",
		"${TRPC_CLAW_CODING_WORKDIR_LINE:-}",
		"${TRPC_CLAW_CODING_OUTPUT_ROOT_LINE:-}",
		"${TRPC_CLAW_CODING_TEMP_ROOT_LINE:-}",
		"When the user asks whether an env var exists, whether it came from a",
		"trusted env file, or whether a tool/runtime dependency is already wired,",
		"prefer envprobe or local inspection over guessing.",
		"When a task maps cleanly to existing runtime helpers or installers, use",
		"them instead of reimplementing bootstrap logic ad hoc.",
		"When several install paths are possible, choose the managed runtime path",
		"that best matches the current environment instead of presenting an options",
		"menu first.",
		"For Chinese or other CJK-heavy documents, preserve legible fonts and text",
		"rendering through conversion and verification instead of assuming default",
		"Latin-only settings are acceptable.",
		"For Chinese or other CJK-heavy outputs, verify the final artifact content",
		"visually or structurally when the toolchain could silently drop glyphs.",
		"If a PDF, OCR, or document-conversion task is self-contained, complete the",
		"conversion and write the resulting artifact instead of only describing the",
		"steps.",
	}, "\n")
}

func previousAutonomousCodingAgentTemplate() string {
	return strings.Join([]string{
		"Coding runtime guidance:",
		"Preamble and progress protocol:",
		"- Users usually cannot see tool calls or internal reasoning. If you do " +
			"not briefly say what you are about to do, the user may see silence.",
		"- Before the first non-trivial tool call, send one short user-visible " +
			"preamble that says what you are about to do.",
		"- That brief preamble is part of acting immediately, not a pause to " +
			"ask what to do next.",
		"- Do not turn a preamble into a confirmation request, options menu, " +
			"or summary of what you could do. Say the immediate next step " +
			"and then do it.",
		"- A preamble-only message is not a completed turn. If tool work is " +
			"needed, the same assistant message must include the tool call; " +
			"if no tool is needed, skip the setup line and return the " +
			"requested content or completed result.",
		"- Group related tool calls under one preamble instead of narrating " +
			"every trivial read.",
		"- Skip a standalone preamble for a single trivial read unless it is " +
			"part of a larger step.",
		"- For longer tasks, send short progress updates at natural milestones " +
			"when you find something load-bearing, change direction, or " +
			"finish a meaningful subtask.",
		"- When a long-running command, deployment, upload, build, or " +
			"interactive session emits a meaningful new stage, treat that " +
			"change as a user-visible progress milestone.",
		"- Do not narrate every empty poll, unchanged wait, or repeated " +
			"status check when nothing changed.",
		"- If a long-running task stays quiet for a while, send one brief " +
			"waiting update before you keep polling or waiting.",
		"- Progress updates should say what changed and what you are doing next.",
		"- Valid preambles are short and immediately followed by tool work. " +
			"Do not send those sentences as the whole reply.",
		"- Let the active persona lead the wording, cadence, and attitude of " +
			"preambles, progress updates, and the final answer. Treat the " +
			"other runtime rules as channel and correctness guardrails " +
			"instead of a competing writing style.",
		"Coding workflow protocol:",
		"For code, repository, build, test, refactor, or review tasks, " +
			"operate like a local coding agent instead of a generic chatbot.",
		"Inspect the target workspace before editing or answering " +
			"code-grounded questions: check the directory, git status, " +
			"relevant files, and any AGENTS.md instructions that apply.",
		"When a request is grounded in a local repo, path, file, or the " +
			"current source, perform a fresh inspection before planning, " +
			"editing, or answering. Do not rely only on earlier turns, " +
			"summaries, or memory.",
		"For repo search, prefer `rg --files` for file inventory and `rg -n` " +
			"for text search. If `rg` is unavailable, fall back to `find` " +
			"or `grep`.",
		"Keep searches inside the target repo or path. Avoid broad " +
			"parent-directory scans such as `grep -R ..` unless the user " +
			"explicitly asks for a wider scope.",
		"After locating candidate files, read the smallest relevant slices " +
			"first and expand only as needed.",
		"${TRPC_CLAW_RUNTIME_AUTONOMY_RULE}",
		"${TRPC_CLAW_RUNTIME_GOAL_COMPLETION_RULE}",
		"When an approach fails, try the next reasonable recovery step " +
			"yourself before asking what to do next. Prefer retries, fresh " +
			"inspection, smaller scope, alternative tools, format " +
			"conversion, dependency bootstrap, or writing the artifact " +
			"another way over stopping to ask for confirmation.",
		"When tool output already gives you one reasonable canonical " +
			"identifier, corrected parameter, or target resource, treat it " +
			"as the working value and continue in the same turn instead of " +
			"stopping to ask the user to confirm it.",
		"For external search, latest/current facts, realtime data, market " +
			"prices, or external docs, verify with the appropriate tool " +
			"instead of answering from stale memory.",
		"${TRPC_CLAW_RUNTIME_MINIMAL_QUESTION_RULE}",
		"${TRPC_CLAW_RUNTIME_NO_CHOICE_TAIL_RULE}",
		"Default to workspace separation: inspect, edit, and test only " +
			"inside the relevant repo or chosen workdir, and avoid spilling " +
			"temp files or derived artifacts into unrelated trees.",
		"When a task mixes multiple roots, keep each command scoped to the " +
			"minimum necessary directory and call out the cross-root " +
			"relationship explicitly in public progress when that affects " +
			"the plan.",
		"Use direct tools for quick reads or tiny edits. For multi-file, " +
			"build, review, or long-running repo work, keep using repo-aware " +
			"runtime execution tools directly.",
		"The built-in fs_* tools are scoped to their configured base_dir " +
			"and are not a general repo browser. For arbitrary repos or " +
			"coding workspaces, prefer exec_command with an explicit workdir " +
			"or another repo-aware runtime tool.",
		"For generated documents or other large literal artifacts, prefer a " +
			"file-writing tool or redirected stdin over giant shell " +
			"arguments. Use shell commands mainly for conversion, validation, " +
			"or moving files.",
		"Never tell the user to save or copy generated artifacts manually " +
			"when you can write them directly in the workspace or output " +
			"root yourself.",
		"${TRPC_CLAW_CODING_ARTIFACT_GUIDANCE:-}",
		"${TRPC_CLAW_CODING_WORKDIR_LINE:-}",
		"${TRPC_CLAW_CODING_OUTPUT_ROOT_LINE:-}",
		"${TRPC_CLAW_CODING_TEMP_ROOT_LINE:-}",
		"When the user asks whether an env var exists, whether it came " +
			"from a trusted env file, or whether a tool/runtime dependency " +
			"is already wired, prefer envprobe or local inspection over " +
			"guessing.",
		"When a task maps cleanly to existing runtime helpers or installers, " +
			"use them instead of reimplementing bootstrap logic ad hoc.",
		"When several install paths are possible, choose the managed runtime " +
			"path that best matches the current environment instead of " +
			"presenting an options menu first.",
		"For Chinese or other CJK-heavy documents, preserve legible fonts " +
			"and text rendering through conversion and verification instead " +
			"of assuming default Latin-only settings are acceptable.",
		"For Chinese or other CJK-heavy outputs, verify the final artifact " +
			"content visually or structurally when the toolchain could " +
			"silently drop glyphs.",
		"If a PDF, OCR, or document-conversion task is self-contained, " +
			"complete the conversion and write the resulting artifact " +
			"instead of only describing the steps.",
	}, "\n")
}

func EnsureDefaultFiles(stateDir string) (Paths, error) {
	paths := DefaultPaths(stateDir)
	if strings.TrimSpace(stateDir) == "" {
		return paths, nil
	}
	if err := migrateLegacyDefaultPersona(paths.PersonaDir); err != nil {
		return Paths{}, err
	}
	if err := migrateLegacyPromptFiles(paths); err != nil {
		return Paths{}, err
	}
	if err := syncManagedPromptDefaults(paths); err != nil {
		return Paths{}, err
	}
	if err := copyEmbeddedFile(
		DefaultIdentityEmbeddedPath,
		paths.IdentityFile,
	); err != nil {
		return Paths{}, err
	}
	for _, item := range []struct {
		embeddedDir string
		targetDir   string
	}{
		{
			embeddedDir: DefaultInstructionEmbeddedDir,
			targetDir:   paths.InstructionDir,
		},
		{
			embeddedDir: DefaultSystemEmbeddedDir,
			targetDir:   paths.SystemDir,
		},
		{
			embeddedDir: DefaultPersonasEmbeddedDir,
			targetDir:   paths.PersonaDir,
		},
		{
			embeddedDir: DefaultWeComRequestEmbeddedDir,
			targetDir:   paths.WeComRequestDir,
		},
	} {
		if err := copyEmbeddedDir(item.embeddedDir, item.targetDir); err != nil {
			return Paths{}, err
		}
	}
	return paths, nil
}

func syncManagedPromptDefaults(paths Paths) error {
	if err := syncManagedPromptFile(
		filepath.Join(paths.InstructionDir, DefaultMemoryFileName),
		filepath.ToSlash(filepath.Join(
			DefaultInstructionEmbeddedDir,
			DefaultMemoryFileName,
		)),
		previousManagedMemoryTemplate,
		previousScopedManagedMemoryTemplate,
	); err != nil {
		return err
	}
	if err := syncManagedPromptFile(
		filepath.Join(paths.SystemDir, DefaultRuntimeIdentityFileName),
		filepath.ToSlash(filepath.Join(
			DefaultSystemEmbeddedDir,
			DefaultRuntimeIdentityFileName,
		)),
		legacyRuntimeIdentityTemplate,
		previousManagedRuntimeIdentityTemplate,
	); err != nil {
		return err
	}
	if err := syncManagedPromptFile(
		filepath.Join(paths.SystemDir, DefaultCodingAgentFileName),
		filepath.ToSlash(filepath.Join(
			DefaultSystemEmbeddedDir,
			DefaultCodingAgentFileName,
		)),
		previousManagedCodingAgentTemplate(),
		previousAutonomousCodingAgentTemplate(),
	); err != nil {
		return err
	}
	return syncManagedPromptFile(
		filepath.Join(paths.WeComRequestDir, DefaultWeComBaseFileName),
		filepath.ToSlash(filepath.Join(
			DefaultWeComRequestEmbeddedDir,
			DefaultWeComBaseFileName,
		)),
	)
}

func syncManagedPromptFile(
	targetPath string,
	embeddedPath string,
	legacyContents ...string,
) error {
	targetPath = strings.TrimSpace(targetPath)
	embeddedPath = strings.TrimSpace(embeddedPath)
	if targetPath == "" || embeddedPath == "" {
		return nil
	}

	current, err := os.ReadFile(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !matchesManagedPromptDefault(string(current), legacyContents) {
		return nil
	}

	desired, err := fs.ReadFile(embeddedDefaults, embeddedPath)
	if err != nil {
		return err
	}
	if normalizePromptContents(string(current)) ==
		normalizePromptContents(string(desired)) {
		return nil
	}
	return os.WriteFile(targetPath, desired, defaultFilePerm)
}

func matchesManagedPromptDefault(
	current string,
	legacyContents []string,
) bool {
	current = normalizePromptContents(current)
	if current == "" {
		return false
	}
	for _, legacy := range legacyContents {
		if current == normalizePromptContents(legacy) {
			return true
		}
	}
	return false
}

func normalizePromptContents(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	return strings.TrimSpace(raw)
}

func migrateLegacyDefaultPersona(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}

	legacyPath := filepath.Join(dir, legacyDefaultPersonaFileName)
	data, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(legacyPersonaPromptBody(string(data))) == "" {
		if err := os.Remove(legacyPath); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}

	defaultPath := filepath.Join(dir, DefaultPersonaFileName)
	if _, err := os.Stat(defaultPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(legacyPath, defaultPath)
}

func legacyPersonaPromptBody(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return ""
	}
	if strings.TrimSpace(lines[0]) != legacyPromptHeaderFence {
		return strings.TrimSpace(raw)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != legacyPromptHeaderFence {
			continue
		}
		return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
	}
	return ""
}

func ResolvePaths(
	baseDir string,
	rawFiles []string,
	rawDir string,
) ([]string, string, error) {
	files := make([]string, 0, len(rawFiles))
	for _, raw := range rawFiles {
		path, err := resolvePath(baseDir, raw)
		if err != nil {
			return nil, "", err
		}
		if path == "" {
			continue
		}
		files = append(files, path)
	}
	dir, err := resolvePath(baseDir, rawDir)
	if err != nil {
		return nil, "", err
	}
	return files, dir, nil
}

func SortPaths(paths []string) {
	sort.Slice(paths, func(i int, j int) bool {
		leftOrder, leftKey := promptSortKey(
			filepath.Base(strings.TrimSpace(paths[i])),
		)
		rightOrder, rightKey := promptSortKey(
			filepath.Base(strings.TrimSpace(paths[j])),
		)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return leftKey < rightKey
	})
}

func ReadDiskBundle(
	files []string,
	dir string,
) (string, error) {
	paths := make([]string, 0, len(files)+8)
	paths = append(paths, files...)

	if strings.TrimSpace(dir) != "" {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			names = append(names, entry.Name())
		}
		sortPromptNames(names)
		for _, name := range names {
			paths = append(paths, filepath.Join(dir, name))
		}
	}

	return readDiskFiles(paths)
}

func ReadEmbeddedBundle(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	entries, err := fs.ReadDir(embeddedDefaults, dir)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sortPromptNames(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(
			embeddedDefaults,
			filepath.ToSlash(filepath.Join(dir, name)),
		)
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n"), nil
}

func ReadEmbeddedFiles(dir string) (map[string]string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	entries, err := fs.ReadDir(embeddedDefaults, dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
		data, err := fs.ReadFile(embeddedDefaults, path)
		if err != nil {
			return nil, err
		}
		out[entry.Name()] = string(data)
	}
	return out, nil
}

func Render(raw string, vars map[string]string) (string, error) {
	if !strings.Contains(raw, envRefPrefix) {
		return strings.TrimSpace(raw), nil
	}

	var out strings.Builder
	remaining := raw
	for remaining != "" {
		start := strings.Index(remaining, envRefPrefix)
		if start < 0 {
			out.WriteString(remaining)
			break
		}
		out.WriteString(remaining[:start])
		remaining = remaining[start+len(envRefPrefix):]

		end := strings.Index(remaining, envRefSuffix)
		if end < 0 {
			return "", fmt.Errorf(
				"promptasset: unterminated variable in %q",
				raw,
			)
		}
		token := remaining[:end]
		value, err := resolveToken(token, vars)
		if err != nil {
			return "", err
		}
		out.WriteString(value)
		remaining = remaining[end+len(envRefSuffix):]
	}
	return strings.TrimSpace(out.String()), nil
}

func readDiskFiles(paths []string) (string, error) {
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n"), nil
}

func resolveToken(
	token string,
	vars map[string]string,
) (string, error) {
	name, defaultValue, hasDefault := strings.Cut(
		token,
		envDefaultOp,
	)
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf(
			"promptasset: empty variable name in %q",
			token,
		)
	}
	if vars != nil {
		if value, ok := vars[name]; ok {
			return value, nil
		}
	}
	if value, ok := os.LookupEnv(name); ok {
		return value, nil
	}
	if hasDefault {
		return defaultValue, nil
	}
	return "", fmt.Errorf(
		"promptasset: variable %s is not set",
		name,
	)
}

func resolvePath(
	baseDir string,
	raw string,
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

	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Clean(filepath.Join(baseDir, path)), nil
}

func copyEmbeddedDir(
	embeddedDir string,
	targetDir string,
) error {
	embeddedDir = strings.TrimSpace(embeddedDir)
	targetDir = strings.TrimSpace(targetDir)
	if embeddedDir == "" || targetDir == "" {
		return nil
	}

	entries, err := fs.ReadDir(embeddedDefaults, embeddedDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, defaultDirPerm); err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.ToSlash(
			filepath.Join(embeddedDir, entry.Name()),
		)
		targetPath := filepath.Join(targetDir, entry.Name())
		if entry.IsDir() {
			if err := copyEmbeddedDir(sourcePath, targetPath); err != nil {
				return err
			}
			continue
		}
		if info, err := os.Stat(targetPath); err == nil &&
			info != nil && !info.IsDir() {
			continue
		}
		data, err := fs.ReadFile(embeddedDefaults, sourcePath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(
			targetPath,
			data,
			defaultFilePerm,
		); err != nil {
			return err
		}
	}
	return nil
}

func copyEmbeddedFile(
	embeddedPath string,
	targetPath string,
) error {
	embeddedPath = strings.TrimSpace(embeddedPath)
	targetPath = strings.TrimSpace(targetPath)
	if embeddedPath == "" || targetPath == "" {
		return nil
	}

	if _, err := os.Stat(targetPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	data, err := fs.ReadFile(embeddedDefaults, embeddedPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(
		filepath.Dir(targetPath),
		defaultDirPerm,
	); err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, defaultFilePerm)
}

func migrateLegacyPromptFiles(paths Paths) error {
	for _, item := range []struct {
		dir        string
		legacyName string
		targetName string
	}{
		{
			dir:        paths.InstructionDir,
			legacyName: legacyMemoryFileName,
			targetName: DefaultMemoryFileName,
		},
		{
			dir:        paths.SystemDir,
			legacyName: legacyRuntimeIdentityFileName,
			targetName: DefaultRuntimeIdentityFileName,
		},
		{
			dir:        paths.SystemDir,
			legacyName: legacyCodingAgentFileName,
			targetName: DefaultCodingAgentFileName,
		},
		{
			dir:        paths.WeComRequestDir,
			legacyName: legacyWeComBaseFileName,
			targetName: DefaultWeComBaseFileName,
		},
	} {
		if err := migrateLegacyPromptFile(
			item.dir,
			item.legacyName,
			item.targetName,
		); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyPromptFile(
	dir string,
	legacyName string,
	targetName string,
) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}

	legacyPath := filepath.Join(dir, legacyName)
	legacyData, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	targetPath := filepath.Join(dir, targetName)
	targetData, err := os.ReadFile(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return os.Rename(legacyPath, targetPath)
	}
	if err != nil {
		return err
	}

	if strings.TrimSpace(string(legacyData)) ==
		strings.TrimSpace(string(targetData)) {
		if err := os.Remove(legacyPath); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}

	backupDir := filepath.Join(dir, legacyPromptBackupDirName)
	if err := os.MkdirAll(backupDir, defaultDirPerm); err != nil {
		return err
	}
	backupPath := filepath.Join(backupDir, legacyName)
	return os.Rename(legacyPath, backupPath)
}

func sortPromptNames(names []string) {
	sort.Slice(names, func(i int, j int) bool {
		leftOrder, leftKey := promptSortKey(names[i])
		rightOrder, rightKey := promptSortKey(names[j])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return leftKey < rightKey
	})
}

func promptSortKey(name string) (int, string) {
	base := strings.TrimSpace(strings.ToLower(name))
	base = strings.TrimSuffix(base, strings.ToLower(filepath.Ext(base)))
	if order, suffix, ok := parsePromptNumericOrder(base); ok {
		return order, suffix
	}
	switch base {
	case strings.TrimSuffix(
		strings.ToLower(DefaultMemoryFileName),
		promptMarkdownExt,
	):
		return defaultMemoryOrder, base
	case strings.TrimSuffix(
		strings.ToLower(DefaultRuntimeIdentityFileName),
		promptMarkdownExt,
	):
		return defaultIdentityOrder, base
	case strings.TrimSuffix(
		strings.ToLower(DefaultCodingAgentFileName),
		promptMarkdownExt,
	):
		return defaultCodingOrder, base
	case strings.TrimSuffix(
		strings.ToLower(DefaultWeComBaseFileName),
		promptMarkdownExt,
	):
		return defaultWeComBaseOrder, base
	default:
		return defaultPromptOrder, base
	}
}

func parsePromptNumericOrder(raw string) (int, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", false
	}
	end := 0
	for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
		end++
	}
	if end == 0 || end == len(raw) {
		return 0, "", false
	}
	if raw[end] != '_' && raw[end] != '-' {
		return 0, "", false
	}
	order, err := strconv.Atoi(raw[:end])
	if err != nil {
		return 0, "", false
	}
	return order, strings.TrimSpace(raw[end+1:]), true
}
