package main

import (
	"fmt"
	"os"
	"strings"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	weixinchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/weixin"
	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
)

const (
	channelConfigFieldEnabled         = "enabled"
	channelConfigFieldEnabledIfEnvAll = "enabled_if_env_all"
	channelTypeWeixin                 = "weixin"
	channelTypeWeCom                  = wecomchannel.PluginType

	channelConfigPagePath = "/config"

	channelConfigInputText   = "text"
	channelConfigInputSelect = "select"

	channelConfigApplyRestart = "restart"

	channelConfigSourceExplicit  = "explicit"
	channelConfigSourceInherited = "inherited"

	channelGlobalStateDirSourceConfig  = "config"
	channelGlobalStateDirSourceFlag    = "flag"
	channelGlobalStateDirSourceDefault = "default"

	channelEnabledDefault = true

	channelTypeSummary = "Switch the channel transport. Saving a new " +
		"type rewrites this channel config to the editable fields " +
		"supported by the selected transport and removes incompatible " +
		"transport-specific settings."
	channelNameSummary = "Optional instance name used to match the " +
		"running channel in shared admin views."

	channelHiddenSecretValue = "[saved]"
	channelSecretPlaceholder = "Enter a new value to replace the " +
		"saved secret"
	channelSecretConfiguredLabel = "Saved secret values are hidden in " +
		"admin."
	channelSecretRuntimeLabel = "Live secret values are hidden. " +
		"Restart after replacing them."
	channelUnknownSummary = "Basic channel identity is editable " +
		"here. Transport-specific config for this channel type is not " +
		"yet exposed in shared admin."
	channelConfigRequiresValueMsg = "value is required; use reset to " +
		"clear the saved setting"
	channelTypeIncompleteDisableSummary = "If the selected transport " +
		"does not yet have the required config to start cleanly, " +
		"admin saves this channel as disabled so the next restart " +
		"does not fail."

	weixinDefaultBaseURLConfigValue         = "https://ilinkai.weixin.qq.com"
	weixinDefaultPollTimeoutConfigValue     = "35s"
	weixinDefaultErrorBackoffConfigValue    = "30s"
	weixinDefaultEnableTypingConfigValue    = "true"
	weixinDefaultRuntimeCommandsConfigValue = "true"

	wecomDefaultBotModeConfigValue        = "notification"
	wecomDefaultConnectionModeConfigValue = "webhook"
	wecomDefaultCallbackPathConfigValue   = "/wecom/callback"
	wecomDefaultChatPolicyConfigValue     = "open"
	wecomDefaultRuntimeAdminConfigValue   = "inherit"
	wecomDefaultUserLabelModeConfigValue  = "alias_or_name"
	wecomDefaultEnableStreamConfigValue   = "false"
	wecomAIBotModeConfigValue             = "ai"
	wecomWebSocketModeConfigValue         = "websocket"
	wecomStreamSnapshotModeFull           = "full"
	wecomStreamSnapshotModeContentOnly    = "content_only"
	wecomStreamSnapshotModeFinalOnly      = "final_only"
	wecomDefaultStreamSnapshotConfigValue = wecomStreamSnapshotModeFull

	channelFieldBotMode              = "bot_mode"
	channelFieldConnectionMode       = "connection_mode"
	channelFieldToken                = "token"
	channelFieldEncodingAESKey       = "encoding_aes_key"
	channelFieldWebhookURL           = "webhook_url"
	channelFieldAIBotID              = "aibotid"
	channelFieldSecret               = "secret"
	channelFieldWebSocketURL         = "ws_url"
	channelFieldBaseURL              = "base_url"
	channelFieldPollTimeout          = "poll_timeout"
	channelFieldErrorBackoff         = "error_backoff"
	channelFieldEnableTyping         = "enable_typing"
	channelFieldEnableRuntimeCommand = "enable_runtime_commands"
	channelFieldStateDir             = "state_dir"
	channelFieldCallbackPath         = "callback_path"
	channelFieldChatPolicy           = "chat_policy"
	channelFieldRuntimeAdminPolicy   = "runtime_admin_policy"
	channelFieldUserLabelMode        = "user_label_mode"
	channelFieldEnableStream         = "enable_stream"
	channelFieldStreamSnapshotMode   = "stream_snapshot_mode"

	channelEnabledSummary = "Disable this channel in config while " +
		"keeping its block on disk. Disabled channels are skipped on " +
		"the next runtime start regardless of env gating."
	channelEnabledIfEnvAllSummary = "Optional comma-separated env " +
		"names. The channel only enters the next runtime when every " +
		"listed env var is non-empty. Missing env vars do not rewrite " +
		"`enabled`."
	channelEnabledIfEnvAllSourceLabel = "Unset means this channel has " +
		"no extra env gate."
	channelEnabledIfEnvAllUnsetValue    = "Not configured"
	channelEnabledIfEnvAllReadyValue    = "Satisfied"
	channelEnabledIfEnvAllMissingPref   = "Missing: "
	channelConfiguredDefaultPrefix      = "Unset uses the default value: "
	channelConfiguredStateDirFromConfig = "Unset inherits the global " +
		"state_dir value."
	channelConfiguredStateDirFromFlag = "Unset currently inherits the " +
		"startup --state-dir override."
	channelConfiguredStateDirDefault = "Unset inherits the runtime " +
		"state dir default."
	channelRuntimeLiveValueLabel = "Current runtime value from the " +
		"running channel."
	channelRuntimePendingValueLabel = "This runtime does not " +
		"currently expose a live value. Restart after saving to " +
		"apply it."
	channelRuntimeDisabledValueLabel = "Disabled channels do not " +
		"expose live runtime values."
	channelRuntimeWaitingEnvLabel = "This channel is configured, but " +
		"the next restart is waiting for env vars: "
	channelRuntimeDisabledNextLabel = "This channel is currently live, " +
		"but config disables it for the next restart."
	channelRuntimeWaitingEnvNextLabel = "This channel is currently " +
		"live, but the next restart is waiting for env vars: "
	channelRuntimeDefaultValueSuffix = "This field is unset in " +
		"config, so the runtime currently uses the default value."
	channelRuntimeStateDirFromConfig = "This field is unset in " +
		"config, so it currently inherits the global state_dir value."
	channelRuntimeStateDirFromFlag = "This field is unset in config, " +
		"so it currently inherits the startup --state-dir override."
	channelRuntimeStateDirDefault = "This field is unset in config, " +
		"so it currently inherits the runtime state dir default."
)

type configuredChannelEntry struct {
	Key                     string
	SectionKey              string
	Index                   int
	Type                    string
	Name                    string
	Enabled                 bool
	ChannelNode             *yaml.Node
	ConfigNode              *yaml.Node
	EnabledExplicit         bool
	EnabledIfEnvAll         []string
	EnabledIfEnvAllExplicit bool
	EffectiveEnabled        bool
	MissingEnabledEnv       []string
}

type runtimeChannelTarget struct {
	Type    string
	Name    string
	Weixin  *weixinchannel.AdminTarget
	WeCom   *wecomchannel.AdminTarget
	Channel string
}

type channelRuntimeContext struct {
	GlobalStateDirSource string
}

type wecomAdminTargetProvider interface {
	WeComAdminTarget() wecomchannel.AdminTarget
}

func applyChannelEnabledDefaults(
	root *yaml.Node,
) (bool, error) {
	return applyChannelEnabledDefaultsWithLookup(root, os.LookupEnv)
}

func resolveConfiguredChannelEnabled(
	channelNode *yaml.Node,
) (bool, bool, error) {
	if channelNode == nil || channelNode.Kind != yaml.MappingNode {
		return channelEnabledDefault, false, nil
	}
	value, ok, err := firstMappingBoolValue(
		channelNode,
		channelConfigFieldEnabled,
	)
	if err != nil {
		return false, false, err
	}
	if !ok {
		return channelEnabledDefault, false, nil
	}
	return value, true, nil
}

func collectConfiguredChannelEntries(
	root *yaml.Node,
	lookup envLookupFunc,
) ([]configuredChannelEntry, error) {
	doc := documentNode(root)
	if doc == nil {
		return nil, nil
	}
	channelsNode := mappingValue(doc, channelsKey)
	if channelsNode == nil {
		return nil, nil
	}
	if channelsNode.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("config: channels must be a sequence")
	}

	entries := make([]configuredChannelEntry, 0, len(channelsNode.Content))
	for index, channelNode := range channelsNode.Content {
		if channelNode == nil || channelNode.Kind != yaml.MappingNode {
			continue
		}
		state, err := resolveConfiguredChannelEnabledState(
			channelNode,
			lookup,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"config: channels[%d]: %w",
				index,
				err,
			)
		}
		entries = append(entries, configuredChannelEntry{
			Key:        channelConfigSectionKey(index),
			SectionKey: channelConfigSectionKey(index),
			Index:      index,
			Type: strings.ToLower(strings.TrimSpace(
				mappingStringValue(channelNode, channelTypeKey),
			)),
			Name: strings.TrimSpace(
				mappingStringValue(channelNode, toolNameKey),
			),
			Enabled:                 state.Enabled,
			ChannelNode:             channelNode,
			EnabledExplicit:         state.EnabledExplicit,
			EnabledIfEnvAll:         state.EnabledIfEnvAll,
			EnabledIfEnvAllExplicit: state.EnabledIfEnvAllExplicit,
			EffectiveEnabled:        state.EffectiveEnabled,
			MissingEnabledEnv:       state.MissingEnv,
			ConfigNode: ensureMappingValue(
				channelNode,
				channelConfigKey,
			),
		})
	}
	return entries, nil
}

func channelConfigSectionKey(index int) string {
	return "channel-" + fmt.Sprintf("%d", index+1)
}

func channelConfigFieldKey(index int, field string) string {
	return "channels." + fmt.Sprintf("%d", index) + "." +
		strings.TrimSpace(field)
}

func editableChannelTypes() []string {
	candidates := []string{
		channelTypeWeCom,
		channelTypeWeixin,
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := registry.LookupChannel(candidate); !ok {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func editableChannelTypeOptions() []admin.RuntimeConfigOption {
	types := editableChannelTypes()
	options := make([]admin.RuntimeConfigOption, 0, len(types))
	for _, typeName := range types {
		options = append(options, admin.RuntimeConfigOption{
			Value: typeName,
			Label: channelTypeDisplayLabel(typeName),
		})
	}
	return options
}

func normalizeEditableChannelType(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	for _, candidate := range editableChannelTypes() {
		if value == candidate {
			return candidate, nil
		}
	}
	if value == "" {
		return "", fmt.Errorf("channel type is required")
	}
	return "", fmt.Errorf("unsupported channel type %q", raw)
}

func (p *runtimeAdminProvider) RuntimeConfigStatus() (
	admin.RuntimeConfigStatus,
	error,
) {
	if p == nil {
		return admin.RuntimeConfigStatus{}, nil
	}
	envSection, err := p.buildRuntimeEnvConfigSection()
	if err != nil {
		return admin.RuntimeConfigStatus{}, err
	}
	rawRoot, _, err := loadPromptConfigRoots(
		p.sourceConfigPath,
		p.args,
		p.stateDir,
	)
	if err != nil {
		return admin.RuntimeConfigStatus{}, err
	}

	entries, err := collectConfiguredChannelEntries(
		rawRoot,
		runtimeConfigEnvLookup(p.stateDir),
	)
	if err != nil {
		return admin.RuntimeConfigStatus{}, err
	}
	ctx, err := resolveChannelRuntimeContext(rawRoot, p.args)
	if err != nil {
		return admin.RuntimeConfigStatus{}, err
	}

	targets := p.channelTargets()
	matches := matchConfiguredRuntimeChannels(entries, targets)

	status := admin.RuntimeConfigStatus{
		Enabled:    true,
		ConfigPath: strings.TrimSpace(p.sourceConfigPath),
		Sections: make(
			[]admin.RuntimeConfigSection,
			0,
			len(entries)+1,
		),
	}
	status.Sections = append(status.Sections, envSection)

	for _, entry := range entries {
		section, ok := p.buildChannelConfigSection(
			entry,
			matches[entry.Key],
			ctx,
		)
		if !ok {
			continue
		}
		status.Sections = append(status.Sections, section)
	}
	return status, nil
}

func (p *runtimeAdminProvider) SaveRuntimeConfigValue(
	key string,
	value string,
) error {
	return p.updateRuntimeConfigValue(
		key,
		value,
		false,
	)
}

func (p *runtimeAdminProvider) ResetRuntimeConfigValue(
	key string,
) error {
	return p.updateRuntimeConfigValue(
		key,
		"",
		true,
	)
}

func (p *runtimeAdminProvider) updateRuntimeConfigValue(
	key string,
	value string,
	reset bool,
) error {
	if p == nil {
		return fmt.Errorf("runtime config provider is not available")
	}
	if handled, err := p.updateRuntimeEnvConfigValue(
		key,
		value,
		reset,
	); handled {
		return err
	}

	channelIndex, fieldKey, err := parseChannelConfigFieldKey(key)
	if err != nil {
		return err
	}

	rawRoot, _, err := loadPromptConfigRoots(
		p.sourceConfigPath,
		p.args,
		p.stateDir,
	)
	if err != nil {
		return err
	}

	channelNode, configNode, err := channelConfigNodesAt(
		rawRoot,
		channelIndex,
	)
	if err != nil {
		return err
	}

	if reset {
		switch fieldKey {
		case channelTypeKey:
			return fmt.Errorf("channel type cannot be reset")
		case channelConfigFieldEnabled:
			deleteMappingKey(channelNode, channelConfigFieldEnabled)
		case channelConfigFieldEnabledIfEnvAll:
			deleteMappingKey(
				channelNode,
				channelConfigFieldEnabledIfEnvAll,
			)
		case toolNameKey:
			deleteMappingKey(channelNode, toolNameKey)
		default:
			deleteMappingKey(configNode, fieldKey)
		}
	} else {
		if err := setChannelConfigFieldValue(
			channelNode,
			configNode,
			fieldKey,
			value,
		); err != nil {
			return err
		}
	}
	if err := keepConfiguredChannelRestartSafe(
		channelNode,
		runtimeConfigEnvLookup(p.stateDir),
	); err != nil {
		return err
	}

	return writeYAMLConfigNode(p.sourceConfigPath, rawRoot)
}

func channelConfigNodesAt(
	root *yaml.Node,
	index int,
) (*yaml.Node, *yaml.Node, error) {
	doc := documentNode(root)
	if doc == nil {
		return nil, nil, fmt.Errorf("config: missing document root")
	}
	channelsNode := mappingValue(doc, channelsKey)
	if channelsNode == nil {
		return nil, nil, fmt.Errorf("config: channels is not configured")
	}
	if channelsNode.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("config: channels must be a sequence")
	}
	if index < 0 || index >= len(channelsNode.Content) {
		return nil, nil, fmt.Errorf("config: unknown channel index %d", index)
	}
	channelNode := channelsNode.Content[index]
	if channelNode == nil || channelNode.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("config: channel %d is invalid", index)
	}
	configNode := ensureMappingValue(channelNode, channelConfigKey)
	if configNode == nil {
		return nil, nil, fmt.Errorf("config: channel %d has no config", index)
	}
	return channelNode, configNode, nil
}

func parseChannelConfigFieldKey(key string) (int, string, error) {
	parts := strings.Split(strings.TrimSpace(key), ".")
	if len(parts) != 3 {
		return 0, "", fmt.Errorf("unknown runtime config field %q", key)
	}
	if parts[0] != channelsKey {
		return 0, "", fmt.Errorf("unknown runtime config field %q", key)
	}
	index, err := parsePositiveInt(parts[1])
	if err != nil {
		return 0, "", fmt.Errorf("invalid channel index: %w", err)
	}
	return index, strings.TrimSpace(parts[2]), nil
}

func setChannelConfigFieldValue(
	channelNode *yaml.Node,
	configNode *yaml.Node,
	fieldKey string,
	value string,
) error {
	value = strings.TrimSpace(value)
	switch strings.TrimSpace(fieldKey) {
	case channelTypeKey:
		return switchConfiguredChannelType(channelNode, value)
	case toolNameKey:
		if value == "" {
			deleteMappingKey(channelNode, toolNameKey)
			return nil
		}
		setMappingString(channelNode, toolNameKey, value)
		return nil
	case channelConfigFieldEnabled:
		parsed, err := parseChannelConfigBool(value)
		if err != nil {
			return err
		}
		setMappingBool(
			channelNode,
			channelConfigFieldEnabled,
			parsed,
		)
		return nil
	case channelConfigFieldEnabledIfEnvAll:
		envNames, err := parseChannelEnabledIfEnvAll(value)
		if err != nil {
			return err
		}
		if len(envNames) == 0 {
			deleteMappingKey(
				channelNode,
				channelConfigFieldEnabledIfEnvAll,
			)
			return nil
		}
		setMappingSequence(
			channelNode,
			channelConfigFieldEnabledIfEnvAll,
			envNames,
		)
		return nil
	case channelFieldToken,
		channelFieldEncodingAESKey,
		channelFieldWebhookURL,
		channelFieldSecret:
		if value == "" {
			return fmt.Errorf(
				"%s: %s",
				fieldKey,
				channelConfigRequiresValueMsg,
			)
		}
		setMappingString(configNode, fieldKey, value)
		return nil
	case channelFieldEnableTyping,
		channelFieldEnableRuntimeCommand,
		channelFieldEnableStream:
		parsed, err := parseChannelConfigBool(value)
		if err != nil {
			return err
		}
		setMappingBool(configNode, fieldKey, parsed)
		return nil
	case channelFieldBotMode,
		channelFieldConnectionMode,
		channelFieldBaseURL,
		channelFieldPollTimeout,
		channelFieldErrorBackoff,
		channelFieldStateDir,
		channelFieldAIBotID,
		channelFieldWebSocketURL,
		channelFieldCallbackPath,
		channelFieldChatPolicy,
		channelFieldRuntimeAdminPolicy,
		channelFieldUserLabelMode:
		setMappingString(configNode, fieldKey, value)
		return nil
	case channelFieldStreamSnapshotMode:
		parsed, err := parseWeComStreamSnapshotMode(value)
		if err != nil {
			return err
		}
		setMappingString(configNode, fieldKey, parsed)
		return nil
	default:
		return fmt.Errorf("unknown runtime config field %q", fieldKey)
	}
}

func keepConfiguredChannelRestartSafe(
	channelNode *yaml.Node,
	lookup envLookupFunc,
) error {
	state, err := resolveConfiguredChannelEnabledState(
		channelNode,
		lookup,
	)
	if err != nil {
		return err
	}
	if !state.Enabled {
		return nil
	}
	if !state.EffectiveEnabled {
		return nil
	}
	if configuredChannelCanStart(channelNode) {
		return nil
	}
	setMappingBool(
		channelNode,
		channelConfigFieldEnabled,
		false,
	)
	return nil
}

func configuredChannelCanStart(channelNode *yaml.Node) bool {
	if channelNode == nil || channelNode.Kind != yaml.MappingNode {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(
		mappingStringValue(channelNode, channelTypeKey),
	)) {
	case channelTypeWeCom:
		return configuredWeComChannelCanStart(
			mappingValue(channelNode, channelConfigKey),
		)
	default:
		return true
	}
}

func configuredWeComChannelCanStart(configNode *yaml.Node) bool {
	botMode := strings.TrimSpace(
		mappingStringValue(configNode, channelFieldBotMode),
	)
	if botMode == "" {
		return false
	}
	connectionMode := normalizeWeComConnectionMode(
		mappingStringValue(configNode, channelFieldConnectionMode),
	)
	if connectionMode == "" {
		return false
	}
	switch botMode {
	case wecomDefaultBotModeConfigValue:
		if connectionMode == wecomWebSocketModeConfigValue {
			return false
		}
		return hasConfiguredString(
			configNode,
			channelFieldToken,
		) &&
			hasConfiguredString(
				configNode,
				channelFieldEncodingAESKey,
			) &&
			hasConfiguredString(
				configNode,
				channelFieldWebhookURL,
			)
	case wecomAIBotModeConfigValue:
		if connectionMode == wecomWebSocketModeConfigValue {
			return hasConfiguredString(
				configNode,
				channelFieldAIBotID,
			) &&
				hasConfiguredString(
					configNode,
					channelFieldSecret,
				)
		}
		return hasConfiguredString(
			configNode,
			channelFieldToken,
		) &&
			hasConfiguredString(
				configNode,
				channelFieldEncodingAESKey,
			)
	default:
		return false
	}
}

func normalizeWeComConnectionMode(raw string) string {
	value := strings.TrimSpace(raw)
	switch value {
	case "":
		return wecomDefaultConnectionModeConfigValue
	case wecomDefaultConnectionModeConfigValue,
		wecomWebSocketModeConfigValue:
		return value
	default:
		return ""
	}
}

func hasConfiguredString(
	root *yaml.Node,
	key string,
) bool {
	return strings.TrimSpace(
		mappingStringValue(root, key),
	) != ""
}

func switchConfiguredChannelType(
	channelNode *yaml.Node,
	rawType string,
) error {
	typeName, err := normalizeEditableChannelType(rawType)
	if err != nil {
		return err
	}
	currentType := strings.ToLower(strings.TrimSpace(
		mappingStringValue(channelNode, channelTypeKey),
	))
	if currentType == typeName {
		setMappingString(channelNode, channelTypeKey, typeName)
		return nil
	}

	configNode := mappingValue(channelNode, channelConfigKey)
	setMappingString(channelNode, channelTypeKey, typeName)
	setMappingNode(
		channelNode,
		channelConfigKey,
		buildChannelConfigNodeForType(typeName, configNode),
	)
	return nil
}

func buildChannelConfigNodeForType(
	typeName string,
	source *yaml.Node,
) *yaml.Node {
	configNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	for _, key := range editableChannelConfigKeys(typeName) {
		copyMappingValue(configNode, source, key)
	}
	return configNode
}

func editableChannelConfigKeys(typeName string) []string {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case channelTypeWeixin:
		return []string{
			channelFieldStateDir,
			channelFieldBaseURL,
			channelFieldPollTimeout,
			channelFieldErrorBackoff,
			channelFieldEnableTyping,
			channelFieldEnableRuntimeCommand,
		}
	case channelTypeWeCom:
		return []string{
			channelFieldBotMode,
			channelFieldConnectionMode,
			channelFieldToken,
			channelFieldEncodingAESKey,
			channelFieldWebhookURL,
			channelFieldAIBotID,
			channelFieldSecret,
			channelFieldWebSocketURL,
			channelFieldCallbackPath,
			channelFieldChatPolicy,
			channelFieldRuntimeAdminPolicy,
			channelFieldUserLabelMode,
			channelFieldEnableStream,
			channelFieldStreamSnapshotMode,
		}
	default:
		return nil
	}
}

func parseChannelConfigBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("expected true or false")
	}
}

func parseWeComStreamSnapshotMode(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case wecomStreamSnapshotModeFull:
		return wecomStreamSnapshotModeFull, nil
	case wecomStreamSnapshotModeContentOnly:
		return wecomStreamSnapshotModeContentOnly, nil
	case wecomStreamSnapshotModeFinalOnly:
		return wecomStreamSnapshotModeFinalOnly, nil
	default:
		return "", fmt.Errorf("unsupported stream_snapshot_mode %q", raw)
	}
}

func parsePositiveInt(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("empty value")
	}
	var out int
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid integer %q", raw)
		}
		out = out*10 + int(r-'0')
	}
	return out, nil
}

func (p *runtimeAdminProvider) channelTargets() []runtimeChannelTarget {
	if p == nil {
		return nil
	}

	targets := make([]runtimeChannelTarget, 0, len(p.runtimeChannels))
	for _, ch := range p.runtimeChannels {
		if ch == nil {
			continue
		}
		target := runtimeChannelTarget{
			Type:    strings.TrimSpace(ch.ID()),
			Channel: strings.TrimSpace(ch.ID()),
		}
		if provider, ok := ch.(weixinAdminTargetProvider); ok &&
			provider != nil {
			weixinTarget := provider.WeixinAdminTarget()
			target.Type = channelTypeWeixin
			target.Name = strings.TrimSpace(weixinTarget.Name)
			target.Weixin = &weixinTarget
		}
		if provider, ok := ch.(wecomAdminTargetProvider); ok &&
			provider != nil {
			wecomTarget := provider.WeComAdminTarget()
			target.Type = channelTypeWeCom
			target.Name = strings.TrimSpace(wecomTarget.Name)
			target.WeCom = &wecomTarget
		}
		targets = append(targets, target)
	}
	return targets
}

func matchConfiguredRuntimeChannels(
	entries []configuredChannelEntry,
	targets []runtimeChannelTarget,
) map[string]*runtimeChannelTarget {
	matches := make(map[string]*runtimeChannelTarget, len(entries))
	used := make(map[int]struct{}, len(targets))

	for i := range entries {
		entry := entries[i]
		index := findRuntimeChannelTarget(
			entry,
			targets,
			used,
			true,
		)
		if index < 0 {
			index = findRuntimeChannelTarget(
				entry,
				targets,
				used,
				false,
			)
		}
		if index < 0 {
			continue
		}
		used[index] = struct{}{}
		target := targets[index]
		matches[entry.Key] = &target
	}
	return matches
}

func findRuntimeChannelTarget(
	entry configuredChannelEntry,
	targets []runtimeChannelTarget,
	used map[int]struct{},
	requireName bool,
) int {
	for index := range targets {
		if _, ok := used[index]; ok {
			continue
		}
		target := targets[index]
		if target.Type != entry.Type {
			continue
		}
		if requireName && entry.Name != "" &&
			target.Name != entry.Name {
			continue
		}
		return index
	}
	return -1
}

func (p *runtimeAdminProvider) buildChannelConfigSection(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	ctx channelRuntimeContext,
) (admin.RuntimeConfigSection, bool) {
	baseFields := p.buildBaseChannelFields(entry, target, ctx)
	switch entry.Type {
	case channelTypeWeixin:
		return p.buildWeixinConfigSection(
			entry,
			target,
			baseFields,
			ctx,
		), true
	case channelTypeWeCom:
		return p.buildWeComConfigSection(
			entry,
			target,
			baseFields,
			ctx,
		), true
	default:
		return admin.RuntimeConfigSection{
			Key: entry.SectionKey,
			Title: channelSectionTitle(
				entry.Index,
				channelTypeDisplayLabel(entry.Type),
				entry.Name,
			),
			Summary: channelUnknownSummary,
			Fields:  baseFields,
		}, true
	}
}

func (p *runtimeAdminProvider) buildBaseChannelFields(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	ctx channelRuntimeContext,
) []admin.RuntimeConfigField {
	return []admin.RuntimeConfigField{
		p.buildChannelTypeField(entry, target, ctx),
		p.buildChannelNameField(entry, target, ctx),
		p.buildEnabledField(entry, target),
		p.buildEnabledIfEnvAllField(entry, target),
	}
}

func (p *runtimeAdminProvider) buildWeixinConfigSection(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	fields []admin.RuntimeConfigField,
	ctx channelRuntimeContext,
) admin.RuntimeConfigSection {
	title := channelSectionTitle(
		entry.Index,
		channelTypeDisplayLabel(entry.Type),
		entry.Name,
	)
	summary := "Weixin runtime login and account management live on " +
		"the Channels page."
	fields = append(fields,
		p.buildWeixinTextField(
			entry,
			target,
			channelFieldStateDir,
			"State Dir",
			"Override the Weixin account state root for this channel.",
			weixinDefaultStateDir(p.stateDir),
			ctx,
		),
		p.buildWeixinTextField(
			entry,
			target,
			channelFieldBaseURL,
			"Base URL",
			"Login and poll API base URL.",
			weixinDefaultBaseURLConfigValue,
			ctx,
		),
		p.buildWeixinTextField(
			entry,
			target,
			channelFieldPollTimeout,
			"Poll Timeout",
			"Long-poll timeout for update fetches.",
			weixinDefaultPollTimeoutConfigValue,
			ctx,
		),
		p.buildWeixinTextField(
			entry,
			target,
			channelFieldErrorBackoff,
			"Error Backoff",
			"Retry backoff after Weixin API failures.",
			weixinDefaultErrorBackoffConfigValue,
			ctx,
		),
		p.buildWeixinBoolField(
			entry,
			target,
			channelFieldEnableTyping,
			"Enable Typing",
			"Send typing indicators while a reply is running.",
			weixinDefaultEnableTypingConfigValue,
			ctx,
		),
		p.buildWeixinBoolField(
			entry,
			target,
			channelFieldEnableRuntimeCommand,
			"Enable Runtime Commands",
			"Allow /runtime status, versions, and changelog.",
			weixinDefaultRuntimeCommandsConfigValue,
			ctx,
		),
	)
	return admin.RuntimeConfigSection{
		Key:     entry.SectionKey,
		Title:   title,
		Summary: summary,
		Fields:  fields,
	}
}

func (p *runtimeAdminProvider) buildWeComConfigSection(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	fields []admin.RuntimeConfigField,
	ctx channelRuntimeContext,
) admin.RuntimeConfigSection {
	title := channelSectionTitle(
		entry.Index,
		channelTypeDisplayLabel(entry.Type),
		entry.Name,
	)
	summary := "WeCom transport operations continue to use the shared " +
		"Chats, Prompts, and Channels pages. Configure the core " +
		"transport fields here before restarting into a different " +
		"WeCom mode. Incomplete WeCom config stays disabled so " +
		"admin changes do not save a restart-breaking channel."
	fields = append(fields,
		p.buildWeComSelectField(
			entry,
			target,
			channelFieldBotMode,
			"Bot Mode",
			"Choose between notification bot and AI bot behavior.",
			wecomDefaultBotModeConfigValue,
			ctx,
			[]admin.RuntimeConfigOption{
				{
					Value: wecomDefaultBotModeConfigValue,
					Label: "Notification",
				},
				{
					Value: wecomAIBotModeConfigValue,
					Label: "AI",
				},
			},
		),
		p.buildWeComSelectField(
			entry,
			target,
			channelFieldConnectionMode,
			"Connection Mode",
			"Use webhook callbacks or the WeCom websocket transport.",
			wecomDefaultConnectionModeConfigValue,
			ctx,
			[]admin.RuntimeConfigOption{
				{
					Value: wecomDefaultConnectionModeConfigValue,
					Label: "Webhook",
				},
				{
					Value: wecomWebSocketModeConfigValue,
					Label: "WebSocket",
				},
			},
		),
		p.buildWeComSecretField(
			entry,
			channelFieldToken,
			"Token",
			"Required for webhook delivery in both notification and "+
				"AI bot modes.",
		),
		p.buildWeComSecretField(
			entry,
			channelFieldEncodingAESKey,
			"Encoding AES Key",
			"Required for webhook delivery in both notification and "+
				"AI bot modes.",
		),
		p.buildWeComSecretField(
			entry,
			channelFieldWebhookURL,
			"Webhook URL",
			"Required when bot mode is notification.",
		),
		p.buildWeComTextField(
			entry,
			target,
			channelFieldAIBotID,
			"AI Bot ID",
			"Required for websocket mode and optional for webhook "+
				"AI bot mode.",
			"",
			ctx,
		),
		p.buildWeComSecretField(
			entry,
			channelFieldSecret,
			"WebSocket Secret",
			"Required for WeCom websocket mode.",
		),
		p.buildWeComTextField(
			entry,
			target,
			channelFieldWebSocketURL,
			"WebSocket URL",
			"Optional custom websocket endpoint for WeCom AI bot "+
				"mode.",
			"",
			ctx,
		),
		p.buildWeComTextField(
			entry,
			target,
			channelFieldCallbackPath,
			"Callback Path",
			"Webhook callback path when this channel uses HTTP mode.",
			wecomDefaultCallbackPathConfigValue,
			ctx,
		),
		p.buildWeComBoolField(
			entry,
			target,
			channelFieldEnableStream,
			"Enable Stream",
			"Use WeCom stream frames while a reply is running. "+
				"Disable to send one markdown reply after completion.",
			wecomDefaultEnableStreamConfigValue,
			ctx,
		),
		p.buildWeComSelectField(
			entry,
			target,
			channelFieldStreamSnapshotMode,
			"Stream Snapshot Mode",
			"Choose which stream snapshots are visible to users. "+
				"Final Only sends only the completed answer.",
			wecomDefaultStreamSnapshotConfigValue,
			ctx,
			[]admin.RuntimeConfigOption{
				{
					Value: wecomStreamSnapshotModeFull,
					Label: "Full",
				},
				{
					Value: wecomStreamSnapshotModeContentOnly,
					Label: "Content Only",
				},
				{
					Value: wecomStreamSnapshotModeFinalOnly,
					Label: "Final Only",
				},
			},
		),
		p.buildWeComSelectField(
			entry,
			target,
			channelFieldChatPolicy,
			"Chat Policy",
			"Control which users may talk to this runtime.",
			wecomDefaultChatPolicyConfigValue,
			ctx,
			[]admin.RuntimeConfigOption{
				{Value: "open", Label: "Open"},
				{Value: "allowlist", Label: "Allowlist"},
				{Value: "disabled", Label: "Disabled"},
			},
		),
		p.buildWeComSelectField(
			entry,
			target,
			channelFieldRuntimeAdminPolicy,
			"Runtime Admin Policy",
			"Limit slash-command runtime controls to an allowlist.",
			wecomDefaultRuntimeAdminConfigValue,
			ctx,
			[]admin.RuntimeConfigOption{
				{Value: "inherit", Label: "Inherit"},
				{Value: "allowlist", Label: "Allowlist"},
			},
		),
		p.buildWeComSelectField(
			entry,
			target,
			channelFieldUserLabelMode,
			"User Label Mode",
			"Choose how WeCom users are labeled in shared admin views.",
			wecomDefaultUserLabelModeConfigValue,
			ctx,
			[]admin.RuntimeConfigOption{
				{Value: "alias_or_name", Label: "Alias Or Name"},
				{Value: "name_or_alias", Label: "Name Or Alias"},
				{Value: "alias", Label: "Alias"},
				{Value: "name", Label: "Name"},
				{Value: "id", Label: "ID"},
			},
		),
	)
	return admin.RuntimeConfigSection{
		Key:     entry.SectionKey,
		Title:   title,
		Summary: summary,
		Fields:  fields,
	}
}

func channelSectionTitle(
	index int,
	typeLabel string,
	name string,
) string {
	title := fmt.Sprintf("Channel %d · %s", index+1, typeLabel)
	name = strings.TrimSpace(name)
	if name == "" {
		return title
	}
	return title + " · " + name
}

func channelTypeDisplayLabel(typeName string) string {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case channelTypeWeCom:
		return "WeCom"
	case channelTypeWeixin:
		return "Weixin"
	default:
		typeName = strings.TrimSpace(typeName)
		if typeName == "" {
			return "Unknown"
		}
		return strings.ToUpper(typeName[:1]) + typeName[1:]
	}
}

func (p *runtimeAdminProvider) buildChannelTypeField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	ctx channelRuntimeContext,
) admin.RuntimeConfigField {
	configured := strings.ToLower(strings.TrimSpace(entry.Type))
	explicit := fieldExplicitOnChannel(entry, channelTypeKey)
	runtimeValue := configured
	if target != nil {
		runtimeValue = strings.ToLower(strings.TrimSpace(target.Type))
	}
	pendingRestart := entry.EffectiveEnabled
	if target != nil {
		pendingRestart = configured != runtimeValue ||
			!entry.EffectiveEnabled
	}
	return admin.RuntimeConfigField{
		Key:   channelConfigFieldKey(entry.Index, channelTypeKey),
		Title: "Type",
		Summary: channelTypeSummary + " " +
			channelTypeIncompleteDisableSummary,
		InputType:   channelConfigInputSelect,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: configured,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			channelTypeKey,
			configured,
		),
		ConfiguredSource: channelConfigSourceValue(explicit),
		RuntimeValue:     runtimeValue,
		RuntimeSourceLabel: channelRuntimeSourceLabel(
			entry,
			target,
			channelTypeKey,
			"",
			ctx,
		),
		PendingRestart: pendingRestart,
		Options:        editableChannelTypeOptions(),
	}
}

func (p *runtimeAdminProvider) buildChannelNameField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	ctx channelRuntimeContext,
) admin.RuntimeConfigField {
	configured := strings.TrimSpace(entry.Name)
	explicit := fieldExplicitOnChannel(entry, toolNameKey)
	runtimeValue := ""
	if target != nil {
		runtimeValue = strings.TrimSpace(target.Name)
	}
	return admin.RuntimeConfigField{
		Key:         channelConfigFieldKey(entry.Index, toolNameKey),
		Title:       "Name",
		Summary:     channelNameSummary,
		InputType:   channelConfigInputText,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: configured,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			toolNameKey,
			configured,
		),
		ConfiguredSource: channelConfigSourceValue(explicit),
		RuntimeValue:     runtimeValue,
		RuntimeSourceLabel: channelRuntimeSourceLabel(
			entry,
			target,
			toolNameKey,
			"",
			ctx,
		),
		PendingRestart: configuredFieldPending(
			explicit,
			configured,
			runtimeValue,
			target != nil,
		),
		Resettable: explicit,
	}
}

func (p *runtimeAdminProvider) buildEnabledField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
) admin.RuntimeConfigField {
	configuredValue := boolConfigValue(entry.Enabled)
	configuredSource := channelConfigSourceValue(
		entry.EnabledExplicit,
	)
	runtimeActive := target != nil

	runtimeValue := boolConfigValue(runtimeActive)
	runtimeLabel := channelEnabledRuntimeLabel(entry, target)

	return admin.RuntimeConfigField{
		Key:         channelConfigFieldKey(entry.Index, channelConfigFieldEnabled),
		Title:       "Enabled",
		Summary:     channelEnabledSummary,
		InputType:   channelConfigInputSelect,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: configuredValue,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			channelConfigFieldEnabled,
			configuredValue,
		),
		ConfiguredSource:      configuredSource,
		ConfiguredSourceLabel: "Unset defaults to enabled.",
		RuntimeValue:          runtimeValue,
		RuntimeSourceLabel:    runtimeLabel,
		PendingRestart:        entry.EffectiveEnabled != runtimeActive,
		Resettable: fieldExplicitOnChannel(
			entry,
			channelConfigFieldEnabled,
		),
		Options: boolOptions(),
	}
}

func (p *runtimeAdminProvider) buildEnabledIfEnvAllField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
) admin.RuntimeConfigField {
	configuredValue := strings.Join(entry.EnabledIfEnvAll, ", ")
	runtimeValue := channelEnabledIfEnvAllRuntimeValue(entry)

	return admin.RuntimeConfigField{
		Key: channelConfigFieldKey(
			entry.Index,
			channelConfigFieldEnabledIfEnvAll,
		),
		Title:       "Enabled If Env All",
		Summary:     channelEnabledIfEnvAllSummary,
		InputType:   channelConfigInputText,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: configuredValue,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			channelConfigFieldEnabledIfEnvAll,
			configuredValue,
		),
		ConfiguredSource: channelConfigSourceValue(
			entry.EnabledIfEnvAllExplicit,
		),
		ConfiguredSourceLabel: channelEnabledIfEnvAllConfiguredLabel(
			entry,
		),
		RuntimeValue:       runtimeValue,
		RuntimeSourceLabel: channelEnabledIfEnvAllRuntimeLabel(entry, target),
		PendingRestart:     entry.EffectiveEnabled != (target != nil),
		Resettable: fieldExplicitOnChannel(
			entry,
			channelConfigFieldEnabledIfEnvAll,
		),
	}
}

func resolveConfiguredChannelEnabledFromEntry(
	entry configuredChannelEntry,
) (bool, bool, error) {
	channelNode := entryChannelNode(entry)
	return resolveConfiguredChannelEnabled(channelNode)
}

func entryChannelNode(entry configuredChannelEntry) *yaml.Node {
	return entry.ChannelNode
}

func fieldExplicitOnChannel(
	entry configuredChannelEntry,
	key string,
) bool {
	channelNode := entryChannelNode(entry)
	return firstMappingValue(channelNode, key) != nil
}

func boolOptions() []admin.RuntimeConfigOption {
	return []admin.RuntimeConfigOption{
		{Value: "true", Label: "Enabled"},
		{Value: "false", Label: "Disabled"},
	}
}

func boolConfigValue(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func configuredValueIfExplicit(
	entry configuredChannelEntry,
	fieldKey string,
	value string,
) string {
	switch fieldKey {
	case channelConfigFieldEnabled,
		channelConfigFieldEnabledIfEnvAll,
		channelTypeKey,
		toolNameKey:
		if !fieldExplicitOnChannel(entry, fieldKey) {
			return ""
		}
		return value
	}
	if !fieldExplicitOnConfig(entry, fieldKey) {
		return ""
	}
	return value
}

func fieldExplicitOnConfig(
	entry configuredChannelEntry,
	key string,
) bool {
	return firstMappingValue(entry.ConfigNode, key) != nil
}

func channelConfigSourceValue(explicit bool) string {
	if explicit {
		return channelConfigSourceExplicit
	}
	return channelConfigSourceInherited
}

func resolveChannelRuntimeContext(
	root *yaml.Node,
	args []string,
) (channelRuntimeContext, error) {
	if strings.TrimSpace(configuredStateDirValue(root)) != "" {
		return channelRuntimeContext{
			GlobalStateDirSource: channelGlobalStateDirSourceConfig,
		}, nil
	}
	_, ok, err := flagValueFromArgs(args, flagStateDir)
	if err != nil {
		return channelRuntimeContext{}, err
	}
	if ok {
		return channelRuntimeContext{
			GlobalStateDirSource: channelGlobalStateDirSourceFlag,
		}, nil
	}
	return channelRuntimeContext{
		GlobalStateDirSource: channelGlobalStateDirSourceDefault,
	}, nil
}

func channelFieldExplicit(
	entry configuredChannelEntry,
	fieldKey string,
) bool {
	switch strings.TrimSpace(fieldKey) {
	case channelConfigFieldEnabled,
		channelConfigFieldEnabledIfEnvAll,
		channelTypeKey,
		toolNameKey:
		return fieldExplicitOnChannel(entry, fieldKey)
	default:
		return fieldExplicitOnConfig(entry, fieldKey)
	}
}

func channelConfiguredSourceLabel(
	entry configuredChannelEntry,
	fieldKey string,
	defaultValue string,
	ctx channelRuntimeContext,
) string {
	if channelFieldExplicit(entry, fieldKey) {
		return ""
	}
	if strings.TrimSpace(fieldKey) == channelFieldStateDir {
		switch ctx.GlobalStateDirSource {
		case channelGlobalStateDirSourceConfig:
			return channelConfiguredStateDirFromConfig
		case channelGlobalStateDirSourceFlag:
			return channelConfiguredStateDirFromFlag
		default:
			return channelConfiguredStateDirDefault
		}
	}
	defaultValue = strings.TrimSpace(defaultValue)
	if defaultValue == "" {
		return ""
	}
	return channelConfiguredDefaultPrefix + defaultValue + "."
}

func channelRuntimeSourceLabel(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	fieldKey string,
	defaultValue string,
	ctx channelRuntimeContext,
) string {
	base := runtimeValueLabel(entry, target)
	if target == nil || channelFieldExplicit(entry, fieldKey) {
		return base
	}
	if strings.TrimSpace(fieldKey) == channelFieldStateDir {
		switch ctx.GlobalStateDirSource {
		case channelGlobalStateDirSourceConfig:
			return base + " " + channelRuntimeStateDirFromConfig
		case channelGlobalStateDirSourceFlag:
			return base + " " + channelRuntimeStateDirFromFlag
		default:
			return base + " " + channelRuntimeStateDirDefault
		}
	}
	if strings.TrimSpace(defaultValue) == "" {
		return base
	}
	return base + " " + channelRuntimeDefaultValueSuffix
}

func (p *runtimeAdminProvider) buildWeixinTextField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	fieldKey string,
	title string,
	summary string,
	defaultValue string,
	ctx channelRuntimeContext,
) admin.RuntimeConfigField {
	configured := strings.TrimSpace(
		mappingStringValue(entry.ConfigNode, fieldKey),
	)
	explicit := fieldExplicitOnConfig(entry, fieldKey)
	editorValue := configured
	if editorValue == "" {
		editorValue = defaultValue
	}
	runtimeValue := strings.TrimSpace(
		weixinRuntimeFieldValue(target, fieldKey),
	)
	return admin.RuntimeConfigField{
		Key:         channelConfigFieldKey(entry.Index, fieldKey),
		Title:       title,
		Summary:     summary,
		InputType:   channelConfigInputText,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: editorValue,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			fieldKey,
			configured,
		),
		ConfiguredSource: channelConfigSourceValue(explicit),
		ConfiguredSourceLabel: channelConfiguredSourceLabel(
			entry,
			fieldKey,
			defaultValue,
			ctx,
		),
		RuntimeValue: runtimeValue,
		RuntimeSourceLabel: channelRuntimeSourceLabel(
			entry,
			target,
			fieldKey,
			defaultValue,
			ctx,
		),
		PendingRestart: configuredFieldPending(
			explicit,
			editorValue,
			runtimeValue,
			target != nil,
		),
		Resettable: explicit,
	}
}

func (p *runtimeAdminProvider) buildWeixinBoolField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	fieldKey string,
	title string,
	summary string,
	defaultValue string,
	ctx channelRuntimeContext,
) admin.RuntimeConfigField {
	configured, explicit := configBoolValue(entry.ConfigNode, fieldKey)
	editorValue := configured
	if editorValue == "" {
		editorValue = defaultValue
	}
	runtimeValue := weixinRuntimeFieldValue(target, fieldKey)
	return admin.RuntimeConfigField{
		Key:         channelConfigFieldKey(entry.Index, fieldKey),
		Title:       title,
		Summary:     summary,
		InputType:   channelConfigInputSelect,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: editorValue,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			fieldKey,
			configured,
		),
		ConfiguredSource: channelConfigSourceValue(explicit),
		ConfiguredSourceLabel: channelConfiguredSourceLabel(
			entry,
			fieldKey,
			defaultValue,
			ctx,
		),
		RuntimeValue: runtimeValue,
		RuntimeSourceLabel: channelRuntimeSourceLabel(
			entry,
			target,
			fieldKey,
			defaultValue,
			ctx,
		),
		PendingRestart: configuredFieldPending(
			explicit,
			editorValue,
			runtimeValue,
			target != nil,
		),
		Resettable: explicit,
		Options:    boolOptions(),
	}
}

func (p *runtimeAdminProvider) buildWeComTextField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	fieldKey string,
	title string,
	summary string,
	defaultValue string,
	ctx channelRuntimeContext,
) admin.RuntimeConfigField {
	configured := strings.TrimSpace(
		mappingStringValue(entry.ConfigNode, fieldKey),
	)
	explicit := fieldExplicitOnConfig(entry, fieldKey)
	editorValue := configured
	if editorValue == "" {
		editorValue = defaultValue
	}
	runtimeValue := strings.TrimSpace(
		wecomRuntimeFieldValue(target, fieldKey),
	)
	return admin.RuntimeConfigField{
		Key:         channelConfigFieldKey(entry.Index, fieldKey),
		Title:       title,
		Summary:     summary,
		InputType:   channelConfigInputText,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: editorValue,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			fieldKey,
			configured,
		),
		ConfiguredSource: channelConfigSourceValue(explicit),
		ConfiguredSourceLabel: channelConfiguredSourceLabel(
			entry,
			fieldKey,
			defaultValue,
			ctx,
		),
		RuntimeValue: runtimeValue,
		RuntimeSourceLabel: channelRuntimeSourceLabel(
			entry,
			target,
			fieldKey,
			defaultValue,
			ctx,
		),
		PendingRestart: configuredFieldPending(
			explicit,
			editorValue,
			runtimeValue,
			target != nil,
		),
		Resettable: explicit,
	}
}

func (p *runtimeAdminProvider) buildWeComSecretField(
	entry configuredChannelEntry,
	fieldKey string,
	title string,
	summary string,
) admin.RuntimeConfigField {
	explicit := fieldExplicitOnConfig(entry, fieldKey)
	configuredValue := ""
	if explicit {
		configuredValue = channelHiddenSecretValue
	}
	return admin.RuntimeConfigField{
		Key:                   channelConfigFieldKey(entry.Index, fieldKey),
		Title:                 title,
		Summary:               summary,
		InputType:             channelConfigInputText,
		Placeholder:           channelSecretPlaceholder,
		ApplyMode:             channelConfigApplyRestart,
		EditorValue:           "",
		ConfiguredValue:       configuredValue,
		ConfiguredSource:      channelConfigSourceValue(explicit),
		ConfiguredSourceLabel: channelSecretConfiguredLabel,
		RuntimeSourceLabel:    channelSecretRuntimeLabel,
		Resettable:            explicit,
	}
}

func (p *runtimeAdminProvider) buildWeComBoolField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	fieldKey string,
	title string,
	summary string,
	defaultValue string,
	ctx channelRuntimeContext,
) admin.RuntimeConfigField {
	configured, explicit := configBoolValue(entry.ConfigNode, fieldKey)
	editorValue := configured
	if editorValue == "" {
		editorValue = defaultValue
	}
	runtimeValue := wecomRuntimeFieldValue(target, fieldKey)
	return admin.RuntimeConfigField{
		Key:         channelConfigFieldKey(entry.Index, fieldKey),
		Title:       title,
		Summary:     summary,
		InputType:   channelConfigInputSelect,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: editorValue,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			fieldKey,
			configured,
		),
		ConfiguredSource: channelConfigSourceValue(explicit),
		ConfiguredSourceLabel: channelConfiguredSourceLabel(
			entry,
			fieldKey,
			defaultValue,
			ctx,
		),
		RuntimeValue: runtimeValue,
		RuntimeSourceLabel: channelRuntimeSourceLabel(
			entry,
			target,
			fieldKey,
			defaultValue,
			ctx,
		),
		PendingRestart: configuredFieldPending(
			explicit,
			editorValue,
			runtimeValue,
			target != nil,
		),
		Resettable: explicit,
		Options:    boolOptions(),
	}
}

func (p *runtimeAdminProvider) buildWeComSelectField(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
	fieldKey string,
	title string,
	summary string,
	defaultValue string,
	ctx channelRuntimeContext,
	options []admin.RuntimeConfigOption,
) admin.RuntimeConfigField {
	configured := strings.TrimSpace(
		mappingStringValue(entry.ConfigNode, fieldKey),
	)
	explicit := fieldExplicitOnConfig(entry, fieldKey)
	editorValue := configured
	if editorValue == "" {
		editorValue = defaultValue
	}
	runtimeValue := strings.TrimSpace(
		wecomRuntimeFieldValue(target, fieldKey),
	)
	return admin.RuntimeConfigField{
		Key:         channelConfigFieldKey(entry.Index, fieldKey),
		Title:       title,
		Summary:     summary,
		InputType:   channelConfigInputSelect,
		ApplyMode:   channelConfigApplyRestart,
		EditorValue: editorValue,
		ConfiguredValue: configuredValueIfExplicit(
			entry,
			fieldKey,
			configured,
		),
		ConfiguredSource: channelConfigSourceValue(explicit),
		ConfiguredSourceLabel: channelConfiguredSourceLabel(
			entry,
			fieldKey,
			defaultValue,
			ctx,
		),
		RuntimeValue: runtimeValue,
		RuntimeSourceLabel: channelRuntimeSourceLabel(
			entry,
			target,
			fieldKey,
			defaultValue,
			ctx,
		),
		PendingRestart: configuredFieldPending(
			explicit,
			editorValue,
			runtimeValue,
			target != nil,
		),
		Resettable: explicit,
		Options:    options,
	}
}

func configBoolValue(
	node *yaml.Node,
	key string,
) (string, bool) {
	value, ok, err := firstMappingBoolValue(node, key)
	if err != nil || !ok {
		return "", false
	}
	return boolConfigValue(value), true
}

func configuredFieldPending(
	explicit bool,
	editorValue string,
	runtimeValue string,
	runtimeExists bool,
) bool {
	if !runtimeExists {
		return explicit
	}
	return strings.TrimSpace(editorValue) !=
		strings.TrimSpace(runtimeValue)
}

func channelEnabledRuntimeLabel(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
) string {
	if target != nil {
		if !entry.Enabled {
			return channelRuntimeDisabledNextLabel
		}
		if len(entry.MissingEnabledEnv) > 0 {
			return channelRuntimeWaitingEnvNextLabel +
				strings.Join(entry.MissingEnabledEnv, ", ")
		}
		return "Channel is currently loaded in the running runtime."
	}
	if !entry.Enabled {
		return "Channel is not currently loaded. Config disables it " +
			"for the next restart."
	}
	if len(entry.MissingEnabledEnv) > 0 {
		return channelRuntimeWaitingEnvLabel +
			strings.Join(entry.MissingEnabledEnv, ", ")
	}
	return "Channel is not currently loaded. Restart to apply the " +
		"saved config."
}

func channelEnabledIfEnvAllConfiguredLabel(
	entry configuredChannelEntry,
) string {
	if entry.EnabledIfEnvAllExplicit {
		return ""
	}
	return channelEnabledIfEnvAllSourceLabel
}

func channelEnabledIfEnvAllRuntimeValue(
	entry configuredChannelEntry,
) string {
	if len(entry.EnabledIfEnvAll) == 0 {
		return channelEnabledIfEnvAllUnsetValue
	}
	if len(entry.MissingEnabledEnv) == 0 {
		return channelEnabledIfEnvAllReadyValue
	}
	return channelEnabledIfEnvAllMissingPref +
		strings.Join(entry.MissingEnabledEnv, ", ")
}

func channelEnabledIfEnvAllRuntimeLabel(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
) string {
	if !entry.Enabled {
		if target != nil {
			return channelRuntimeDisabledNextLabel
		}
		return "This channel is disabled in config. Re-enable it " +
			"before env gating can load it."
	}
	if len(entry.EnabledIfEnvAll) == 0 {
		if target != nil {
			return "Current runtime is loaded and this channel has no " +
				"extra env gate."
		}
		return "This channel has no extra env gate."
	}
	if len(entry.MissingEnabledEnv) == 0 {
		if target != nil {
			return "Current runtime is loaded and the next restart " +
				"gate is satisfied."
		}
		return "Next restart gate is satisfied."
	}
	missing := strings.Join(entry.MissingEnabledEnv, ", ")
	if target != nil {
		return channelRuntimeWaitingEnvNextLabel + missing
	}
	return channelRuntimeWaitingEnvLabel + missing
}

func runtimeValueLabel(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
) string {
	if target != nil {
		return channelRuntimeLiveValueLabel
	}
	if !entry.Enabled {
		return channelRuntimeDisabledValueLabel
	}
	if entry.EffectiveEnabled {
		return channelRuntimePendingValueLabel
	}
	if len(entry.MissingEnabledEnv) > 0 {
		return channelRuntimeWaitingEnvLabel +
			strings.Join(entry.MissingEnabledEnv, ", ")
	}
	return channelRuntimePendingValueLabel
}

func weixinRuntimeFieldValue(
	target *runtimeChannelTarget,
	fieldKey string,
) string {
	if target == nil || target.Weixin == nil {
		return ""
	}
	switch strings.TrimSpace(fieldKey) {
	case channelFieldStateDir:
		return strings.TrimSpace(target.Weixin.StateDir)
	case channelFieldBaseURL:
		return strings.TrimSpace(target.Weixin.DefaultBaseURL)
	case channelFieldPollTimeout:
		return target.Weixin.PollTimeout.String()
	case channelFieldErrorBackoff:
		return target.Weixin.ErrorBackoff.String()
	case channelFieldEnableTyping:
		return boolConfigValue(target.Weixin.EnableTyping)
	case channelFieldEnableRuntimeCommand:
		return boolConfigValue(target.Weixin.EnableRuntimeCommands)
	default:
		return ""
	}
}

func wecomRuntimeFieldValue(
	target *runtimeChannelTarget,
	fieldKey string,
) string {
	if target == nil || target.WeCom == nil {
		return ""
	}
	switch strings.TrimSpace(fieldKey) {
	case channelFieldBotMode:
		return strings.TrimSpace(target.WeCom.BotMode)
	case channelFieldConnectionMode:
		return strings.TrimSpace(target.WeCom.ConnectionMode)
	case channelFieldAIBotID:
		return strings.TrimSpace(target.WeCom.AIBotID)
	case channelFieldWebSocketURL:
		return strings.TrimSpace(target.WeCom.WebSocketURL)
	case channelFieldCallbackPath:
		return strings.TrimSpace(target.WeCom.CallbackPath)
	case channelFieldEnableStream:
		return boolConfigValue(target.WeCom.EnableStream)
	case channelFieldStreamSnapshotMode:
		return strings.TrimSpace(target.WeCom.StreamSnapshotMode)
	case channelFieldChatPolicy:
		return strings.TrimSpace(target.WeCom.ChatPolicy)
	case channelFieldRuntimeAdminPolicy:
		return strings.TrimSpace(target.WeCom.RuntimeAdminPolicy)
	case channelFieldUserLabelMode:
		return strings.TrimSpace(target.WeCom.UserLabelMode)
	default:
		return ""
	}
}

func weixinDefaultStateDir(stateDir string) string {
	return weixinchannel.ResolveStateDir(stateDir, "")
}
