package persona

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupBuiltin(t *testing.T) {
	t.Parallel()

	def, ok := LookupBuiltin(DefaultID)
	require.True(t, ok)
	require.Equal(t, PragmaticID, def.ID)
	require.Equal(t, "务实", def.Name)
	require.NotEmpty(t, def.Prompt)

	def, ok = LookupBuiltin(LegacyDefaultID)
	require.True(t, ok)
	require.Equal(t, PragmaticID, def.ID)
	require.Equal(t, "务实", def.Name)
	require.NotEmpty(t, def.Prompt)

	def, ok = LookupBuiltin(ConciseID)
	require.True(t, ok)
	require.Equal(t, ConciseID, def.ID)
	require.Equal(t, "简洁", def.Name)
	require.NotEmpty(t, def.Prompt)

	def, ok = LookupBuiltin(FriendlyID)
	require.True(t, ok)
	require.Equal(t, FriendlyID, def.ID)
	require.Equal(t, "伙伴", def.Name)
	require.NotEmpty(t, def.Prompt)
	require.Contains(
		t,
		def.Prompt,
		"moving the task",
	)
	require.Contains(
		t,
		def.Prompt,
		"reasonable assumptions",
	)
	require.Contains(
		t,
		def.Prompt,
		"recover autonomously",
	)
	require.Contains(
		t,
		def.Prompt,
		"routine\nsetbacks",
	)
	require.Contains(
		t,
		def.Prompt,
		"keep clarification questions disabled by default",
	)
	require.Contains(
		t,
		def.Prompt,
		"infer the likely complete goal",
	)
	require.Contains(
		t,
		def.Prompt,
		"exact missing piece",
	)
	require.Contains(
		t,
		def.Prompt,
		"briefly as fact",
	)

	def, ok = LookupBuiltin(SnarkyID)
	require.True(t, ok)
	require.Equal(t, SnarkyID, def.ID)
	require.Equal(t, "毒舌", def.Name)
	require.NotEmpty(t, def.Prompt)

	def, ok = LookupBuiltin("女友")
	require.True(t, ok)
	require.Equal(t, GirlfriendID, def.ID)
	require.Equal(t, "女友", def.Name)
	require.NotEmpty(t, def.Prompt)

	def, ok = LookupBuiltin("男友")
	require.True(t, ok)
	require.Equal(t, BoyfriendID, def.ID)
	require.Equal(t, "男友", def.Name)
	require.NotEmpty(t, def.Prompt)
}

func TestBuiltinsIncludeUnifiedPresetSet(t *testing.T) {
	t.Parallel()

	ids := make([]string, 0, len(Builtins()))
	for _, def := range Builtins() {
		ids = append(ids, def.ID)
	}
	require.Equal(
		t,
		[]string{
			PragmaticID,
			SnarkyID,
			GirlfriendID,
			BoyfriendID,
			QuirkyID,
			CreativeID,
			NerdyID,
			FriendlyID,
			CoachID,
			CandidID,
			ConciseID,
			ProfessionalID,
		},
		ids,
	)
}

func TestValidateCustomID(t *testing.T) {
	t.Parallel()

	id, err := ValidateCustomID("product_partner")
	require.NoError(t, err)
	require.Equal(t, "product_partner", id)

	_, err = ValidateCustomID(DefaultID)
	require.Error(t, err)

	_, err = ValidateCustomID(LegacyDefaultID)
	require.Error(t, err)

	id, err = ValidateCustomID("产品合伙人")
	require.NoError(t, err)
	require.Equal(t, "产品合伙人", id)
}

func TestRegistrySaveGetListDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	registry := NewRegistry(dir)

	saved, err := registry.Save(
		"爱心",
		"You are a pragmatic product partner.\n"+
			"Lead with a conclusion.",
	)
	require.NoError(t, err)
	require.Equal(t, "爱心", saved.Name)
	require.Equal(t, "爱心", saved.ID)
	require.Equal(
		t,
		filepath.Join(dir, saved.ID+".md"),
		saved.Path,
	)

	got, ok, err := registry.Get("爱心")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, saved.ID, got.ID)
	require.Equal(t, saved.Name, got.Name)
	require.Equal(t, saved.Summary, got.Summary)
	require.Equal(t, saved.Prompt, got.Prompt)

	list, err := registry.List()
	require.NoError(t, err)
	require.Len(t, list, len(Builtins())+1)
	require.Equal(t, saved.ID, list[len(list)-1].ID)

	err = registry.Delete("爱心")
	require.NoError(t, err)

	_, ok, err = registry.Get("爱心")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRegistryCreateUsesGeneratedName(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())

	saved, err := registry.Create("你是一个有爱心的人。回答时温柔一点。")
	require.NoError(t, err)
	require.Equal(t, "有爱心的人", saved.Name)
	require.Equal(t, "有爱心的人", saved.ID)

	got, ok, err := registry.Get(saved.Name)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, saved.ID, got.ID)
}

func TestRegistryCreateAddsReadableSuffixOnCollision(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())

	first, err := registry.Create("你是一个有爱心的人。")
	require.NoError(t, err)
	require.Equal(t, "有爱心的人", first.ID)

	second, err := registry.Create("你是一个有爱心的人。")
	require.NoError(t, err)
	require.Equal(t, "有爱心的人 2", second.Name)
	require.Equal(t, "有爱心的人-2", second.ID)
}

func TestRegistryListSkipsNonPersonaFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := os.WriteFile(
		filepath.Join(dir, "README.txt"),
		[]byte("ignore"),
		0o600,
	)
	require.NoError(t, err)

	registry := NewRegistry(dir)
	list, err := registry.List()
	require.NoError(t, err)
	require.Len(t, list, len(Builtins()))
}

func TestRegistryUpsertOverridesBuiltinAndDeleteResets(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	builtin, ok := LookupBuiltin(FriendlyID)
	require.True(t, ok)

	updated, err := registry.Upsert(
		FriendlyID,
		"",
		"Override friendly prompt.",
	)
	require.NoError(t, err)
	require.True(t, updated.BuiltIn)
	require.Equal(t, builtin.Name, updated.Name)

	got, ok, err := registry.Get(FriendlyID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Override friendly prompt.", got.Prompt)

	require.NoError(t, registry.Delete(FriendlyID))

	reset, ok, err := registry.Get(FriendlyID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, builtin.Prompt, reset.Prompt)
}

func TestRegistryUpsertKeepsExistingCustomName(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	saved, err := registry.Save(
		"Warm",
		"Original prompt.",
	)
	require.NoError(t, err)

	updated, err := registry.Upsert(saved.ID, "", "Updated prompt.")
	require.NoError(t, err)
	require.Equal(t, saved.Name, updated.Name)

	got, ok, err := registry.Get(saved.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, saved.Name, got.Name)
	require.Equal(t, "Updated prompt.", got.Prompt)
}
