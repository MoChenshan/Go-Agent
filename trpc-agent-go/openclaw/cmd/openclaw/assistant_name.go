package main

import (
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"gopkg.in/yaml.v3"
)

const (
	wecomBotNameKey = "bot_name"

	assistantNameFallbackBotName = "wecom bot_name"
	assistantNameFallbackRuntime = "runtime product"
)

type runtimeAssistantNameState struct {
	ConfiguredName string
	EffectiveName  string
	RuntimeProduct string
	SourcePath     string
	FallbackSource string
}

func loadRuntimeAssistantNameState(
	root *yaml.Node,
	stateDir string,
) (runtimeAssistantNameState, error) {
	paths := promptasset.DefaultPaths(stateDir)
	configured, err := assistantname.ReadFile(paths.IdentityFile)
	if err != nil {
		return runtimeAssistantNameState{}, err
	}

	state := runtimeAssistantNameState{
		ConfiguredName: configured,
		EffectiveName:  configured,
		RuntimeProduct: runtimeProductName,
		SourcePath:     strings.TrimSpace(paths.IdentityFile),
	}
	if state.EffectiveName != "" {
		return state, nil
	}

	if fallback := firstConfiguredWeComBotName(root); fallback != "" {
		state.EffectiveName = fallback
		state.FallbackSource = assistantNameFallbackBotName
		return state, nil
	}

	state.EffectiveName = runtimeProductName
	state.FallbackSource = assistantNameFallbackRuntime
	return state, nil
}

func firstConfiguredWeComBotName(root *yaml.Node) string {
	for _, configNode := range collectWeComConfigNodes(root) {
		name := assistantname.Normalize(
			mappingStringValue(configNode, wecomBotNameKey),
		)
		if name != "" {
			return name
		}
	}
	return ""
}
