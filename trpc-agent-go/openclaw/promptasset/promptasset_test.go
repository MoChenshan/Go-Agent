package promptasset

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePathsUsesConfigBaseDir(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(t.TempDir(), "cfg")
	err := os.MkdirAll(baseDir, 0o755)
	require.NoError(t, err)

	files, dir, err := ResolvePaths(
		baseDir,
		[]string{"./instruction/01.md"},
		"./system",
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{
			filepath.Join(baseDir, "instruction", "01.md"),
		},
		files,
	)
	require.Equal(
		t,
		filepath.Join(baseDir, "system"),
		dir,
	)
}

func TestReadDiskBundleOrdersDirectoryEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := os.WriteFile(
		filepath.Join(dir, DefaultCodingAgentFileName),
		[]byte("second"),
		0o600,
	)
	require.NoError(t, err)
	err = os.WriteFile(
		filepath.Join(dir, DefaultRuntimeIdentityFileName),
		[]byte("first"),
		0o600,
	)
	require.NoError(t, err)

	text, err := ReadDiskBundle(nil, dir)
	require.NoError(t, err)
	require.Equal(t, "first\n\nsecond", text)
}

func TestRenderSupportsDefaults(t *testing.T) {
	t.Parallel()

	text, err := Render(
		"hello ${NAME} ${MISSING:-fallback}",
		map[string]string{
			"NAME": "world",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "hello world fallback", text)
}

func TestReadEmbeddedBundleAndRender(t *testing.T) {
	t.Parallel()

	raw, err := ReadEmbeddedFiles(DefaultSystemEmbeddedDir)
	require.NoError(t, err)
	identityRaw, ok := raw[DefaultRuntimeIdentityFileName]
	require.True(t, ok)
	require.Contains(t, identityRaw, "Current assistant name:")
	codingRaw, ok := raw[DefaultCodingAgentFileName]
	require.True(t, ok)
	require.Contains(
		t,
		codingRaw,
		"A preamble-only message is not a completed turn",
	)

	rendered, err := Render(
		identityRaw,
		map[string]string{
			"TRPC_CLAW_ASSISTANT_NAME":                 "Claw",
			"TRPC_CLAW_RUNTIME_PRODUCT_NAME":           "trpc-claw",
			"TRPC_CLAW_RUNTIME_MODEL_MODE":             "openai",
			"TRPC_CLAW_RUNTIME_MODEL_NAME_LINE":        "Runtime model name: gpt-test",
			"TRPC_CLAW_RUNTIME_OPENAI_VARIANT_LINE":    "",
			"TRPC_CLAW_RUNTIME_PROVIDER_BASE_URL_LINE": "",
		},
	)
	require.NoError(t, err)
	require.Contains(t, rendered, "Current assistant name: Claw")
	require.Contains(t, rendered, "Runtime product: trpc-claw")
	require.Contains(t, rendered, "Runtime model name: gpt-test")
}

func TestDefaultPersonasIncludeTaskCompletionGuardrail(t *testing.T) {
	t.Parallel()

	const taskCompletionGuardrail = "answer only with what " +
		"you will do next"

	raw, err := ReadEmbeddedFiles(DefaultPersonasEmbeddedDir)
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	for name, text := range raw {
		normalized := strings.Join(strings.Fields(text), " ")
		require.Contains(t, normalized, taskCompletionGuardrail, name)
	}
}

func TestEnsureDefaultFilesSeedsRoots(t *testing.T) {
	t.Parallel()

	paths, err := EnsureDefaultFiles(t.TempDir())
	require.NoError(t, err)
	require.FileExists(
		t,
		filepath.Join(paths.InstructionDir, DefaultMemoryFileName),
	)
	require.FileExists(
		t,
		filepath.Join(paths.SystemDir, DefaultRuntimeIdentityFileName),
	)
	require.FileExists(
		t,
		filepath.Join(paths.PersonaDir, "friendly.md"),
	)
	require.FileExists(
		t,
		filepath.Join(paths.PersonaDir, DefaultPersonaFileName),
	)
	require.NoFileExists(
		t,
		filepath.Join(paths.PersonaDir, legacyDefaultPersonaFileName),
	)
	require.FileExists(
		t,
		filepath.Join(paths.WeComRequestDir, DefaultWeComBaseFileName),
	)
	require.FileExists(t, paths.IdentityFile)
}

func TestEnsureDefaultFilesMigratesLegacyDefaultPersona(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	personaDir := filepath.Join(stateDir, personasDirName)
	err := os.MkdirAll(personaDir, 0o755)
	require.NoError(t, err)

	legacyPath := filepath.Join(
		personaDir,
		legacyDefaultPersonaFileName,
	)
	err = os.WriteFile(
		legacyPath,
		[]byte(
			"---\n"+
				"name: 系统默认\n"+
				"summary: 旧默认\n"+
				"---\n\n"+
				"Keep answers direct.\n",
		),
		0o600,
	)
	require.NoError(t, err)

	paths, err := EnsureDefaultFiles(stateDir)
	require.NoError(t, err)
	require.NoFileExists(t, legacyPath)

	defaultPath := filepath.Join(
		paths.PersonaDir,
		DefaultPersonaFileName,
	)
	require.FileExists(t, defaultPath)
	data, err := os.ReadFile(defaultPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "Keep answers direct.")
}

func TestEnsureDefaultFilesMigratesLegacyPromptFiles(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	paths := DefaultPaths(stateDir)
	require.NoError(t, os.MkdirAll(paths.InstructionDir, 0o755))
	require.NoError(t, os.MkdirAll(paths.SystemDir, 0o755))
	require.NoError(t, os.MkdirAll(paths.WeComRequestDir, 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(paths.InstructionDir, legacyMemoryFileName),
		[]byte("legacy memory"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.SystemDir, legacyRuntimeIdentityFileName),
		[]byte("legacy identity"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.SystemDir, legacyCodingAgentFileName),
		[]byte("legacy coding"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.WeComRequestDir, legacyWeComBaseFileName),
		[]byte("legacy wecom"),
		0o600,
	))

	paths, err := EnsureDefaultFiles(stateDir)
	require.NoError(t, err)
	require.NoFileExists(
		t,
		filepath.Join(paths.InstructionDir, legacyMemoryFileName),
	)
	require.NoFileExists(
		t,
		filepath.Join(paths.SystemDir, legacyRuntimeIdentityFileName),
	)
	require.NoFileExists(
		t,
		filepath.Join(paths.SystemDir, legacyCodingAgentFileName),
	)
	require.NoFileExists(
		t,
		filepath.Join(paths.WeComRequestDir, legacyWeComBaseFileName),
	)

	data, err := os.ReadFile(
		filepath.Join(paths.InstructionDir, DefaultMemoryFileName),
	)
	require.NoError(t, err)
	require.Equal(t, "legacy memory", string(data))
	data, err = os.ReadFile(
		filepath.Join(paths.SystemDir, DefaultRuntimeIdentityFileName),
	)
	require.NoError(t, err)
	require.Equal(t, "legacy identity", string(data))
	data, err = os.ReadFile(
		filepath.Join(paths.SystemDir, DefaultCodingAgentFileName),
	)
	require.NoError(t, err)
	require.Equal(t, "legacy coding", string(data))
	data, err = os.ReadFile(
		filepath.Join(paths.WeComRequestDir, DefaultWeComBaseFileName),
	)
	require.NoError(t, err)
	require.Equal(t, "legacy wecom", string(data))
}

func TestEnsureDefaultFilesRefreshesLegacyRuntimeIdentityTemplate(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	paths := DefaultPaths(stateDir)
	require.NoError(t, os.MkdirAll(paths.SystemDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.SystemDir, DefaultRuntimeIdentityFileName),
		[]byte(legacyRuntimeIdentityTemplate+"\n"),
		0o600,
	))

	paths, err := EnsureDefaultFiles(stateDir)
	require.NoError(t, err)

	data, err := os.ReadFile(
		filepath.Join(paths.SystemDir, DefaultRuntimeIdentityFileName),
	)
	require.NoError(t, err)
	require.Contains(t, string(data), "Current assistant name:")
	require.Contains(t, string(data), "Runtime product:")
	require.NotContains(
		t,
		string(data),
		"Runtime identity: You are",
	)
}

func TestEnsureDefaultFilesRefreshesManagedMemoryTemplate(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	paths := DefaultPaths(stateDir)
	require.NoError(t, os.MkdirAll(paths.InstructionDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.InstructionDir, DefaultMemoryFileName),
		[]byte(previousManagedMemoryTemplate+"\n"),
		0o600,
	))

	paths, err := EnsureDefaultFiles(stateDir)
	require.NoError(t, err)

	data, err := os.ReadFile(
		filepath.Join(paths.InstructionDir, DefaultMemoryFileName),
	)
	require.NoError(t, err)
	want, err := fs.ReadFile(
		embeddedDefaults,
		filepath.ToSlash(filepath.Join(
			DefaultInstructionEmbeddedDir,
			DefaultMemoryFileName,
		)),
	)
	require.NoError(t, err)
	require.Equal(t, string(want), string(data))
}

func TestEnsureDefaultFilesRefreshesScopedManagedMemoryTemplate(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	paths := DefaultPaths(stateDir)
	require.NoError(t, os.MkdirAll(paths.InstructionDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.InstructionDir, DefaultMemoryFileName),
		[]byte(previousScopedManagedMemoryTemplate+"\n"),
		0o600,
	))

	paths, err := EnsureDefaultFiles(stateDir)
	require.NoError(t, err)

	data, err := os.ReadFile(
		filepath.Join(paths.InstructionDir, DefaultMemoryFileName),
	)
	require.NoError(t, err)
	want, err := fs.ReadFile(
		embeddedDefaults,
		filepath.ToSlash(filepath.Join(
			DefaultInstructionEmbeddedDir,
			DefaultMemoryFileName,
		)),
	)
	require.NoError(t, err)
	require.Equal(t, string(want), string(data))
}

func TestEnsureDefaultFilesRefreshesManagedCodingAgentTemplate(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
	}{
		{
			name:     "legacy",
			template: previousManagedCodingAgentTemplate(),
		},
		{
			name:     "autonomous",
			template: previousAutonomousCodingAgentTemplate(),
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stateDir := t.TempDir()
			paths := DefaultPaths(stateDir)
			require.NoError(t, os.MkdirAll(paths.SystemDir, 0o755))
			require.NoError(t, os.WriteFile(
				filepath.Join(paths.SystemDir, DefaultCodingAgentFileName),
				[]byte(tc.template+"\n"),
				0o600,
			))

			paths, err := EnsureDefaultFiles(stateDir)
			require.NoError(t, err)

			data, err := os.ReadFile(
				filepath.Join(paths.SystemDir, DefaultCodingAgentFileName),
			)
			require.NoError(t, err)
			want, err := fs.ReadFile(
				embeddedDefaults,
				filepath.ToSlash(filepath.Join(
					DefaultSystemEmbeddedDir,
					DefaultCodingAgentFileName,
				)),
			)
			require.NoError(t, err)
			require.Equal(t, string(want), string(data))
		})
	}
}

func TestSortPathsKeepsPromptOrderStable(t *testing.T) {
	t.Parallel()

	paths := []string{
		filepath.Join("/tmp", DefaultCodingAgentFileName),
		filepath.Join("/tmp", "notes.md"),
		filepath.Join("/tmp", DefaultRuntimeIdentityFileName),
		filepath.Join("/tmp", legacyMemoryFileName),
	}

	SortPaths(paths)
	require.Equal(
		t,
		[]string{
			filepath.Join("/tmp", legacyMemoryFileName),
			filepath.Join("/tmp", DefaultRuntimeIdentityFileName),
			filepath.Join("/tmp", DefaultCodingAgentFileName),
			filepath.Join("/tmp", "notes.md"),
		},
		paths,
	)
}
