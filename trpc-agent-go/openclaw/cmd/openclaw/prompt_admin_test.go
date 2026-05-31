package main

import (
	"os"
	"path/filepath"
	"testing"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type promptAdminTestConfig struct {
	Agent struct {
		Instruction      string   `yaml:"instruction,omitempty"`
		InstructionFiles []string `yaml:"instruction_files,omitempty"`
		Persona          string   `yaml:"persona,omitempty"`
	} `yaml:"agent,omitempty"`
}

const promptAdminTestSessionTrackerRelPath = "wecom/session_tracker.json"

func TestRuntimeAdminProviderPromptsStatus(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	instructionPath := filepath.Join(
		configDir,
		"prompts",
		"instruction.md",
	)
	writePromptAdminTestFile(t, instructionPath, "from file")

	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  instruction: \"inline\"\n"+
			"  instruction_files:\n"+
			"    - \"./prompts/instruction.md\"\n",
	)

	controller := &stubAgentPromptController{}
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		controller,
		nil,
	)

	status, err := provider.PromptsStatus()
	require.NoError(t, err)
	require.Len(t, status.Bundles, 2)
	require.Len(t, status.Sections, 1)
	require.Equal(t, "Core Prompt", status.Sections[0].Title)
	require.Len(t, status.Previews, 1)
	require.Equal(t, "Agent Prompt", status.Previews[0].Title)
	require.Equal(
		t,
		"inline\n\nfrom file",
		status.Bundles[0].ConfiguredValue,
	)
	require.True(t, status.Bundles[0].InlineEditable)
	require.Equal(t, "inline", status.Bundles[0].InlineValue)
	require.Equal(
		t,
		"Configured Instruction Text",
		status.Bundles[0].ConfiguredLabel,
	)
	require.Equal(
		t,
		"Live Instruction Text",
		status.Bundles[0].EffectiveLabel,
	)
	require.Equal(t, "Config text plus 1 file", status.Bundles[0].SourceSummary)
	require.Len(t, status.Bundles[0].Files, 1)
	require.Equal(t, "Instruction", status.Bundles[0].Title)
}

func TestRuntimeAdminProviderWeComPromptUsesTemplateStructure(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"channels:\n"+
			"  - type: \"wecom\"\n"+
			"    config:\n"+
			"      bot_mode: \"ai\"\n"+
			"      connection_mode: \"websocket\"\n"+
			"      aibotid: \"bot\"\n"+
			"      secret: \"secret\"\n",
	)

	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		nil,
		[]runtimeWeComPromptTarget{{}},
	)

	status, err := provider.PromptsStatus()
	require.NoError(t, err)
	require.Len(t, status.Previews, 1)
	require.Len(t, status.Bundles, 3)

	wecomBundle := status.Bundles[2]
	require.Equal(t, "WeCom Turn Template 1", wecomBundle.Title)
	require.Equal(t, "Live Template Structure", wecomBundle.EffectiveLabel)
	require.Contains(t, wecomBundle.Summary, "exact text varies")
	require.Contains(
		t,
		wecomBundle.EffectiveValue,
		"[Turn context notes]",
	)
	require.Contains(
		t,
		wecomBundle.EffectiveValue,
		"[Runtime rules]",
	)
	require.NotContains(t, wecomBundle.EffectiveValue, "${TRPC_CLAW")
}

func TestRuntimeAdminProviderSavePromptInlineUpdatesConfigAndReloads(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  instruction: \"old instruction\"\n",
	)

	controller := &stubAgentPromptController{}
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		controller,
		nil,
	)

	require.NoError(t, provider.SavePromptInline(
		runtimePromptBundleAgentInstruction,
		"updated instruction",
	))

	cfg := readPromptAdminTestConfig(t, configPath)
	require.Equal(t, "updated instruction", cfg.Agent.Instruction)
	require.Equal(t, "updated instruction", controller.instruction)
}

func TestRuntimeAdminProviderIdentityStatusAndSave(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"channels:\n"+
			"  - type: \"wecom\"\n"+
			"    config:\n"+
			"      bot_name: \"LegacyBot\"\n",
	)

	controller := &stubAgentPromptController{}
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		controller,
		nil,
	)

	status, err := provider.IdentityStatus()
	require.NoError(t, err)
	require.Empty(t, status.ConfiguredName)
	require.Equal(t, "LegacyBot", status.EffectiveName)
	require.Equal(t, assistantNameFallbackBotName, status.FallbackSource)
	require.Equal(t, runtimeProductName, status.RuntimeProduct)
	require.Equal(
		t,
		promptasset.DefaultPaths(stateDir).IdentityFile,
		status.SourcePath,
	)

	require.NoError(t, provider.SaveAssistantName("阿爪"))

	status, err = provider.IdentityStatus()
	require.NoError(t, err)
	require.Equal(t, "阿爪", status.ConfiguredName)
	require.Equal(t, "阿爪", status.EffectiveName)
	require.Empty(t, status.FallbackSource)

	name, err := assistantname.ReadFile(
		promptasset.DefaultPaths(stateDir).IdentityFile,
	)
	require.NoError(t, err)
	require.Equal(t, "阿爪", name)
	require.Contains(
		t,
		controller.systemPrompt,
		"Current assistant name: 阿爪",
	)
}

func TestRuntimeAdminProviderChatsStatus(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	commandPath := filepath.Join(configDir, "lookup")
	writePromptAdminTestFile(
		t,
		commandPath,
		"#!/bin/sh\n"+
			"cat <<'EOF'\n"+
			"{\"staffAccountName\":\"alice\","+
			"\"staffDisplayName\":\"Alice Chen\"}\n"+
			"EOF\n",
	)
	require.NoError(t, os.Chmod(commandPath, 0o755))
	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  persona: \"pragmatic\"\n"+
			"channels:\n"+
			"  - type: \"wecom\"\n"+
			"    config:\n"+
			"      user_label_mode: \"name_or_alias\"\n"+
			"      user_identity_lookup_command: \""+
			commandPath+"\"\n",
	)
	require.NoError(
		t,
		assistantname.WriteFile(
			promptasset.DefaultPaths(stateDir).IdentityFile,
			"winechord",
		),
	)
	writePromptAdminTestFile(
		t,
		filepath.Join(
			stateDir,
			promptAdminTestSessionTrackerRelPath,
		),
		`{
  "version": 8,
  "sessions": {
    "wecom:dm:T00010001": {
      "session_id": "wecom:dm:T00010001:100",
      "assistant_alias": "林妹妹",
      "persona_id": "concise",
      "persona_pinned": true,
      "workspace_path": "/tmp/work",
      "known_user_ids": ["T00010001"],
      "last_activity_unix": 100,
      "history": [
        {
          "session_id": "wecom:dm:T00010001:100",
          "last_activity_unix": 100
        }
      ]
    },
    "wecom:chat:group1": {
      "session_id": "wecom:chat:group1",
      "last_activity_unix": 10
    }
  }
}
`,
	)

	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		nil,
		nil,
	)

	status, err := provider.ChatsStatus()
	require.NoError(t, err)
	conciseDef, ok := personaapi.LookupBuiltin(personaapi.ConciseID)
	require.True(t, ok)
	require.Equal(t, "winechord", status.GlobalAssistantName)
	require.Equal(
		t,
		chatNameSourceIdentity,
		status.GlobalAssistantSource,
	)
	require.Len(t, status.Chats, 2)
	require.Equal(
		t,
		"林妹妹",
		status.Chats[0].EffectiveAssistant,
	)
	require.Equal(
		t,
		chatNameSourceOverride,
		status.Chats[0].NameSource,
	)
	require.True(t, status.Chats[0].OverridesGlobal)
	require.Equal(t, "/tmp/work", status.Chats[0].WorkspacePath)
	require.Equal(
		t,
		[]string{"T00010001"},
		status.Chats[0].KnownUserIDs,
	)
	require.Len(t, status.Chats[0].KnownUsers, 1)
	require.Equal(
		t,
		"T00010001",
		status.Chats[0].KnownUsers[0].UserID,
	)
	require.Equal(
		t,
		"Alice Chen",
		status.Chats[0].KnownUsers[0].Label,
	)
	require.Equal(
		t,
		"DM · Alice Chen (T00010001)",
		status.Chats[0].DisplayLabel,
	)
	require.Equal(
		t,
		conciseDef.Name,
		status.Chats[0].PersonaLabel,
	)
	require.Equal(
		t,
		"winechord",
		status.Chats[1].EffectiveAssistant,
	)
	require.Equal(
		t,
		chatNameSourceIdentity,
		status.Chats[1].NameSource,
	)
	require.False(t, status.Chats[1].OverridesGlobal)
}

func TestRuntimeAdminProviderHidesLegacyInlineSystemPrompt(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  system_prompt: \"You are tRPC-Claw....\"\n",
	)

	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		nil,
		nil,
	)

	status, err := provider.PromptsStatus()
	require.NoError(t, err)
	require.Len(t, status.Bundles, 2)

	systemBundle := status.Bundles[1]
	require.Equal(t, "System Prompt", systemBundle.Title)
	require.Empty(t, systemBundle.ConfiguredValue)
	require.Empty(t, systemBundle.InlineValue)
	require.Equal(t, "Built-in files", systemBundle.SourceSummary)
	require.Equal(
		t,
		"Configured System Text",
		systemBundle.ConfiguredLabel,
	)
	require.Equal(t, "Live System Text", systemBundle.EffectiveLabel)
	require.Contains(
		t,
		systemBundle.EffectiveValue,
		"Current assistant name:",
	)
	require.Contains(
		t,
		systemBundle.EffectiveValue,
		"Runtime product: trpc-claw",
	)
	require.NotContains(
		t,
		systemBundle.EffectiveValue,
		"You are tRPC-Claw....",
	)
	require.NotContains(
		t,
		systemBundle.EffectiveValue,
		"Runtime identity: You are",
	)
}

func TestRuntimeAdminProviderUsesSourceConfigPath(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  instruction: \"from source\"\n",
	)

	runtimeConfigPath := filepath.Join(
		t.TempDir(),
		"trpc-claw-config-1.yaml",
	)
	controller := &stubAgentPromptController{}
	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         stateDir,
		args: []string{
			"-config",
			runtimeConfigPath,
		},
		reloader: newRuntimePromptReloader(
			configPath,
			stateDir,
			[]string{
				"-config",
				runtimeConfigPath,
			},
			controller,
			nil,
		),
	}

	status, err := provider.PromptsStatus()
	require.NoError(t, err)
	require.Len(t, status.Bundles, 2)
	require.Equal(
		t,
		"from source",
		status.Bundles[0].ConfiguredValue,
	)
	require.NoError(t, provider.SavePromptInline(
		runtimePromptBundleAgentInstruction,
		"updated from source",
	))

	cfg := readPromptAdminTestConfig(t, configPath)
	require.Equal(t, "updated from source", cfg.Agent.Instruction)
	require.Equal(t, "updated from source", controller.instruction)
	_, err = os.Stat(runtimeConfigPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRuntimeAdminProviderSavePromptFileUpdatesSourceAndReloads(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	instructionPath := filepath.Join(
		configDir,
		"prompts",
		"instruction.md",
	)
	writePromptAdminTestFile(t, instructionPath, "before")

	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  instruction_files:\n"+
			"    - \"./prompts/instruction.md\"\n",
	)

	controller := &stubAgentPromptController{}
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		controller,
		nil,
	)

	require.NoError(t, provider.SavePromptFile(
		runtimePromptBundleAgentInstruction,
		instructionPath,
		"after",
	))

	data, err := os.ReadFile(instructionPath)
	require.NoError(t, err)
	require.Equal(t, "after\n", string(data))
	require.Equal(t, "after", controller.instruction)
}

func TestRuntimeAdminProviderSetDefaultPersonaReloads(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	systemPath := filepath.Join(configDir, "prompts", "system.md")
	writePromptAdminTestFile(t, systemPath, "Base system prompt.")

	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  persona: \"default\"\n"+
			"  persona_dir: \"./personas\"\n"+
			"  system_prompt_files:\n"+
			"    - \"./prompts/system.md\"\n",
	)

	controller := &stubAgentPromptController{}
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		controller,
		nil,
	)

	require.NoError(t, provider.SetDefaultPersona(
		personaapi.FriendlyID,
	))

	cfg := readPromptAdminTestConfig(t, configPath)
	require.Equal(t, personaapi.FriendlyID, cfg.Agent.Persona)
	friendly, ok := personaapi.LookupBuiltin(personaapi.FriendlyID)
	require.True(t, ok)
	require.Contains(t, controller.systemPrompt, "Base system prompt.")
	require.Contains(t, controller.systemPrompt, friendly.Prompt)
}

func TestRuntimeAdminProviderPersonasStatusDefaultsToPragmatic(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  persona_dir: \"./personas\"\n",
	)

	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		nil,
		nil,
	)

	status, err := provider.PersonasStatus()
	require.NoError(t, err)
	require.Equal(t, personaapi.PragmaticID, status.DefaultPersonaID)

	legacyPath := filepath.Join(configDir, "personas", "default.md")
	writePromptAdminTestFile(
		t,
		legacyPath,
		"---\nname: 旧默认\nsummary: 旧\n---\n\nBe brief.\n",
	)
	status, err = provider.PersonasStatus()
	require.NoError(t, err)
	require.Equal(t, personaapi.PragmaticID, status.DefaultPersonaID)
	for _, store := range status.Stores {
		for _, def := range store.Personas {
			require.NotEqual(t, "default", def.ID)
		}
	}
}

func TestRuntimeAdminProviderPersonasStatusSharedStoreLabels(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  persona_dir: \"./personas\"\n",
	)

	sharedDir := filepath.Join(configDir, "personas")
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		nil,
		[]runtimeWeComPromptTarget{{
			Label:      "WeCom Personas 1",
			PersonaDir: sharedDir,
		}},
	)

	status, err := provider.PersonasStatus()
	require.NoError(t, err)
	require.Len(t, status.Stores, 1)
	require.Equal(t, personaStoreSharedTitle, status.Stores[0].Title)
	require.Equal(
		t,
		[]string{
			personaStoreAgentLabel,
			"WeCom Personas 1",
		},
		status.Stores[0].UsageLabels,
	)
}

func TestRuntimeAdminProviderSavePersonaUpdatesSelectedPrompt(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	systemPath := filepath.Join(configDir, "prompts", "system.md")
	writePromptAdminTestFile(t, systemPath, "Base system prompt.")

	personaDir := filepath.Join(configDir, "personas")
	registry := personaapi.NewRegistry(personaDir)
	saved, err := registry.Save("Warm", "Original custom persona.")
	require.NoError(t, err)

	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  persona: \""+saved.ID+"\"\n"+
			"  persona_dir: \"./personas\"\n"+
			"  system_prompt_files:\n"+
			"    - \"./prompts/system.md\"\n",
	)

	controller := &stubAgentPromptController{}
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		controller,
		nil,
	)

	require.NoError(t, provider.SavePersona(
		personaDir,
		saved.ID,
		"Warm",
		"Updated custom persona.",
	))

	got, ok, err := registry.Get(saved.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Updated custom persona.", got.Prompt)
	require.Contains(t, controller.systemPrompt, got.Prompt)
}

func TestRuntimeAdminProviderDeletePersonaRemovesCustomPersona(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	personaDir := filepath.Join(configDir, "personas")
	registry := personaapi.NewRegistry(personaDir)
	saved, err := registry.Save("Warm", "Original custom persona.")
	require.NoError(t, err)

	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  persona_dir: \"./personas\"\n",
	)

	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		nil,
		nil,
	)

	require.NoError(t, provider.DeletePersona(personaDir, saved.ID))

	_, ok, err := registry.Get(saved.ID)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRuntimeAdminProviderCreateFileUpdatesExplicitFilesConfig(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	instructionPath := filepath.Join(configDir, "prompts", "10_base.md")
	writePromptAdminTestFile(t, instructionPath, "base")

	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  instruction_files:\n"+
			"    - \"./prompts/10_base.md\"\n",
	)

	controller := &stubAgentPromptController{}
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		controller,
		nil,
	)

	require.NoError(t, provider.CreatePromptFile(
		runtimePromptBundleAgentInstruction,
		"20_extra.md",
		"extra",
	))

	cfg := readPromptAdminTestConfig(t, configPath)
	require.Equal(
		t,
		[]string{"./prompts/10_base.md", "./prompts/20_extra.md"},
		cfg.Agent.InstructionFiles,
	)
	require.Equal(t, "base\n\nextra", controller.instruction)
}

func TestRuntimeAdminProviderDeleteFileUpdatesExplicitFilesConfig(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configDir := t.TempDir()
	basePath := filepath.Join(configDir, "prompts", "10_base.md")
	extraPath := filepath.Join(configDir, "prompts", "20_extra.md")
	writePromptAdminTestFile(t, basePath, "base")
	writePromptAdminTestFile(t, extraPath, "extra")

	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptAdminTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  instruction_files:\n"+
			"    - \"./prompts/10_base.md\"\n"+
			"    - \"./prompts/20_extra.md\"\n",
	)

	controller := &stubAgentPromptController{}
	provider := newTestRuntimeAdminProvider(
		configPath,
		stateDir,
		controller,
		nil,
	)

	require.NoError(t, provider.DeletePromptFile(
		runtimePromptBundleAgentInstruction,
		extraPath,
	))

	cfg := readPromptAdminTestConfig(t, configPath)
	require.Equal(
		t,
		[]string{"./prompts/10_base.md"},
		cfg.Agent.InstructionFiles,
	)
	require.NoFileExists(t, extraPath)
	require.Equal(t, "base", controller.instruction)
}

func newTestRuntimeAdminProvider(
	configPath string,
	stateDir string,
	controller agentPromptController,
	wecomTargets []runtimeWeComPromptTarget,
) *runtimeAdminProvider {
	return &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         stateDir,
		reloader: newRuntimePromptReloader(
			configPath,
			stateDir,
			nil,
			controller,
			wecomTargets,
		),
		wecomTargets: append(
			[]runtimeWeComPromptTarget(nil),
			wecomTargets...,
		),
	}
}

func readPromptAdminTestConfig(
	t *testing.T,
	path string,
) promptAdminTestConfig {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var cfg promptAdminTestConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	return cfg
}

func writePromptAdminTestFile(
	t *testing.T,
	path string,
	content string,
) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
