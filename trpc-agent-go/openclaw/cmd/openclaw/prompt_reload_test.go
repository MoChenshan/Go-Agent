package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"github.com/stretchr/testify/require"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	testInstructionPrompt = "Instruction from file."
	testSystemPrompt      = "System prompt from file."
	testWeComPrompt       = "WeCom request prompt from file."

	testInstructionPromptUpdated = "Updated instruction."
	testSystemPromptUpdated      = "Updated system prompt."
	testWeComPromptUpdated       = "Updated WeCom prompt."
)

type stubAgentPromptController struct {
	instruction  string
	systemPrompt string
}

func (s *stubAgentPromptController) SetPrompts(
	instruction string,
	systemPrompt string,
) {
	s.instruction = instruction
	s.systemPrompt = systemPrompt
}

type stubRequestPromptSetter struct {
	template string
}

func (s *stubRequestPromptSetter) SetRequestSystemPromptTemplate(
	template string,
) {
	s.template = template
}

type stubRuntimeWeComChannel struct {
	personaDir string
	template   string
}

func (c *stubRuntimeWeComChannel) ID() string {
	return "wecom"
}

func (c *stubRuntimeWeComChannel) Run(ctx context.Context) error {
	return ctx.Err()
}

func (c *stubRuntimeWeComChannel) PersonaDir() string {
	return c.personaDir
}

func (c *stubRuntimeWeComChannel) SetRequestSystemPromptTemplate(
	template string,
) {
	c.template = template
}

type stubRuntimeChannel struct{}

func (stubRuntimeChannel) ID() string {
	return "stub"
}

func (stubRuntimeChannel) Run(ctx context.Context) error {
	return ctx.Err()
}

func TestCollectRuntimeWeComPromptTargets(t *testing.T) {
	t.Parallel()

	wecom := &stubRuntimeWeComChannel{
		personaDir: t.TempDir(),
	}
	targets := collectRuntimeWeComPromptTargets(
		[]occhannel.Channel{
			stubRuntimeChannel{},
			wecom,
		},
	)
	require.Len(t, targets, 1)
	require.Equal(t, wecom.personaDir, targets[0].PersonaDir)
	require.NotNil(t, targets[0].PromptSetter)
}

func TestRuntimePromptReloaderReloadsAgentAndWeComPrompts(
	t *testing.T,
) {
	t.Parallel()

	defaultPersona, ok := personaapi.LookupBuiltin(personaapi.DefaultID)
	require.True(t, ok)
	expectedSystemPrompt := testSystemPrompt + "\n\n" +
		defaultPersona.Prompt
	expectedSystemPromptUpdated := testSystemPromptUpdated + "\n\n" +
		defaultPersona.Prompt

	stateDir := t.TempDir()
	configDir := t.TempDir()
	instructionPath := filepath.Join(
		configDir,
		"prompts",
		"instruction.md",
	)
	systemPath := filepath.Join(
		configDir,
		"prompts",
		"system.md",
	)
	wecomPath := filepath.Join(
		configDir,
		"prompts",
		"wecom.md",
	)
	writePromptTestFile(t, instructionPath, testInstructionPrompt)
	writePromptTestFile(t, systemPath, testSystemPrompt)
	writePromptTestFile(t, wecomPath, testWeComPrompt)

	configPath := filepath.Join(configDir, "openclaw.yaml")
	writePromptTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n"+
			"agent:\n"+
			"  instruction_files:\n"+
			"    - \"./prompts/instruction.md\"\n"+
			"  system_prompt_files:\n"+
			"    - \"./prompts/system.md\"\n"+
			"channels:\n"+
			"  - type: \"wecom\"\n"+
			"    config:\n"+
			"      request_system_prompt_files:\n"+
			"        - \"./prompts/wecom.md\"\n",
	)

	controller := &stubAgentPromptController{}
	setter := &stubRequestPromptSetter{}
	reloader := newRuntimePromptReloader(
		configPath,
		stateDir,
		nil,
		controller,
		[]runtimeWeComPromptTarget{{
			PromptSetter: setter,
		}},
	)
	require.NotNil(t, reloader)

	require.NoError(t, reloader.Reload())
	require.Equal(t, testInstructionPrompt, controller.instruction)
	require.Equal(t, expectedSystemPrompt, controller.systemPrompt)
	require.Equal(t, testWeComPrompt, setter.template)

	writePromptTestFile(
		t,
		instructionPath,
		testInstructionPromptUpdated,
	)
	writePromptTestFile(t, systemPath, testSystemPromptUpdated)
	writePromptTestFile(t, wecomPath, testWeComPromptUpdated)

	require.NoError(t, reloader.Reload())
	require.Equal(
		t,
		testInstructionPromptUpdated,
		controller.instruction,
	)
	require.Equal(
		t,
		expectedSystemPromptUpdated,
		controller.systemPrompt,
	)
	require.Equal(t, testWeComPromptUpdated, setter.template)
}

func TestRuntimePromptReloaderReloadsIdentityFileIntoSystemPrompt(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	writePromptTestFile(
		t,
		configPath,
		"memory:\n"+
			"  backend: \"inmemory\"\n",
	)
	require.NoError(t, assistantname.WriteFile(
		promptasset.DefaultPaths(stateDir).IdentityFile,
		"阿爪",
	))

	controller := &stubAgentPromptController{}
	reloader := newRuntimePromptReloader(
		configPath,
		stateDir,
		nil,
		controller,
		nil,
	)
	require.NotNil(t, reloader)

	require.NoError(t, reloader.Reload())
	require.Contains(
		t,
		controller.systemPrompt,
		"Current assistant name: 阿爪",
	)

	require.NoError(t, assistantname.WriteFile(
		promptasset.DefaultPaths(stateDir).IdentityFile,
		"阿瓜",
	))
	require.NoError(t, reloader.Reload())
	require.Contains(
		t,
		controller.systemPrompt,
		"Current assistant name: 阿瓜",
	)
}

func writePromptTestFile(t *testing.T, path string, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
