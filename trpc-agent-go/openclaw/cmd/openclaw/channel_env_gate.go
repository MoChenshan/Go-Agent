package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type envLookupFunc func(string) (string, bool)

type configuredChannelEnabledState struct {
	Enabled                 bool
	EnabledExplicit         bool
	EnabledIfEnvAll         []string
	EnabledIfEnvAllExplicit bool
	EffectiveEnabled        bool
	MissingEnv              []string
}

func runtimeConfigEnvLookup(stateDir string) envLookupFunc {
	return envLookupFromMap(runtimeConfigEnvSnapshot(stateDir))
}

func runtimeConfigEnvSnapshot(stateDir string) map[string]string {
	values := currentEnvSnapshot()
	defaults, err := readRuntimeEnvAssignmentsForStateDir(stateDir)
	if err != nil {
		return values
	}
	applyEnvDefaults(values, defaults)
	return values
}

func currentEnvSnapshot() map[string]string {
	values := make(map[string]string)
	for _, raw := range os.Environ() {
		name, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		values[name] = value
	}
	return values
}

func applyEnvDefaults(
	current map[string]string,
	defaults map[string]string,
) {
	for name, value := range defaults {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		if strings.TrimSpace(current[name]) != "" {
			continue
		}
		current[name] = value
	}
}

func envLookupFromMap(values map[string]string) envLookupFunc {
	return func(name string) (string, bool) {
		if values == nil {
			return "", false
		}
		value, ok := values[name]
		return value, ok
	}
}

func applyChannelEnabledDefaultsWithLookup(
	root *yaml.Node,
	lookup envLookupFunc,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	channelsNode := mappingValue(doc, channelsKey)
	if channelsNode == nil {
		return false, nil
	}
	if channelsNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("config: channels must be a sequence")
	}

	filtered := make([]*yaml.Node, 0, len(channelsNode.Content))
	changed := false
	for index, channelNode := range channelsNode.Content {
		if channelNode == nil ||
			channelNode.Kind != yaml.MappingNode {
			filtered = append(filtered, channelNode)
			continue
		}
		state, err := resolveConfiguredChannelEnabledState(
			channelNode,
			lookup,
		)
		if err != nil {
			return false, fmt.Errorf(
				"config: channels[%d]: %w",
				index,
				err,
			)
		}
		if !state.EffectiveEnabled {
			changed = true
			continue
		}
		changed = deletePreparedChannelGateKeys(channelNode) || changed
		filtered = append(filtered, channelNode)
	}
	if changed {
		channelsNode.Content = filtered
	}
	return changed, nil
}

func deletePreparedChannelGateKeys(channelNode *yaml.Node) bool {
	if channelNode == nil || channelNode.Kind != yaml.MappingNode {
		return false
	}
	changed := false
	if firstMappingValue(channelNode, channelConfigFieldEnabled) != nil {
		deleteMappingKey(channelNode, channelConfigFieldEnabled)
		changed = true
	}
	if firstMappingValue(
		channelNode,
		channelConfigFieldEnabledIfEnvAll,
	) != nil {
		deleteMappingKey(
			channelNode,
			channelConfigFieldEnabledIfEnvAll,
		)
		changed = true
	}
	return changed
}

func resolveConfiguredChannelEnabledState(
	channelNode *yaml.Node,
	lookup envLookupFunc,
) (configuredChannelEnabledState, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}

	enabled, explicit, err := resolveConfiguredChannelEnabled(
		channelNode,
	)
	if err != nil {
		return configuredChannelEnabledState{}, err
	}
	envAll, envExplicit, err := resolveConfiguredChannelEnabledIfEnvAll(
		channelNode,
	)
	if err != nil {
		return configuredChannelEnabledState{}, err
	}
	missing := missingEnvNames(envAll, lookup)
	return configuredChannelEnabledState{
		Enabled:                 enabled,
		EnabledExplicit:         explicit,
		EnabledIfEnvAll:         envAll,
		EnabledIfEnvAllExplicit: envExplicit,
		EffectiveEnabled:        enabled && len(missing) == 0,
		MissingEnv:              missing,
	}, nil
}

func resolveConfiguredChannelEnabledIfEnvAll(
	channelNode *yaml.Node,
) ([]string, bool, error) {
	if channelNode == nil || channelNode.Kind != yaml.MappingNode {
		return nil, false, nil
	}
	valueNode := firstMappingValue(
		channelNode,
		channelConfigFieldEnabledIfEnvAll,
	)
	if valueNode == nil {
		return nil, false, nil
	}
	if valueNode.Kind != yaml.SequenceNode {
		return nil, true, fmt.Errorf(
			"%s must be a sequence",
			channelConfigFieldEnabledIfEnvAll,
		)
	}

	values := make([]string, 0, len(valueNode.Content))
	seen := make(map[string]struct{}, len(valueNode.Content))
	for _, child := range valueNode.Content {
		if child == nil {
			continue
		}
		name := strings.TrimSpace(child.Value)
		if name == "" {
			continue
		}
		if !isValidChannelEnvName(name) {
			return nil, true, fmt.Errorf(
				"%s has invalid env name %q",
				channelConfigFieldEnabledIfEnvAll,
				name,
			)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		values = append(values, name)
	}
	return values, true, nil
}

func missingEnvNames(
	names []string,
	lookup envLookupFunc,
) []string {
	if len(names) == 0 {
		return nil
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}

	missing := make([]string, 0, len(names))
	for _, name := range names {
		value, ok := lookup(name)
		if ok && strings.TrimSpace(value) != "" {
			continue
		}
		missing = append(missing, name)
	}
	return missing
}

func parseChannelEnabledIfEnvAll(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '\n', '\r':
			return true
		default:
			return false
		}
	})
	values := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		if !isValidChannelEnvName(name) {
			return nil, fmt.Errorf("invalid env name %q", name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		values = append(values, name)
	}
	return values, nil
}

func isValidChannelEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !isChannelEnvFirstRune(r) {
				return false
			}
			continue
		}
		if !isChannelEnvRune(r) {
			return false
		}
	}
	return true
}

func isChannelEnvFirstRune(r rune) bool {
	return r == '_' ||
		r >= 'A' && r <= 'Z' ||
		r >= 'a' && r <= 'z'
}

func isChannelEnvRune(r rune) bool {
	return isChannelEnvFirstRune(r) || r >= '0' && r <= '9'
}
