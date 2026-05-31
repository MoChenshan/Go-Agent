package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	weixinchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/weixin"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

type stubWeComAdminChannel struct {
	target wecomchannel.AdminTarget
}

func (s stubWeComAdminChannel) ID() string {
	return channelTypeWeCom
}

func (s stubWeComAdminChannel) Run(
	ctx context.Context,
) error {
	return ctx.Err()
}

func (s stubWeComAdminChannel) WeComAdminTarget() wecomchannel.AdminTarget {
	return s.target
}

func requireRuntimeConfigField(
	t *testing.T,
	section admin.RuntimeConfigSection,
	key string,
) admin.RuntimeConfigField {
	t.Helper()

	for _, field := range section.Fields {
		if field.Key == key {
			return field
		}
	}
	t.Fatalf("missing runtime config field %q", key)
	return admin.RuntimeConfigField{}
}

func TestApplyChannelEnabledDefaultsSkipsDisabledChannels(
	t *testing.T,
) {
	t.Parallel()

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(
		"channels:\n"+
			"  - type: wecom\n"+
			"    enabled: false\n"+
			"  - type: weixin\n",
	), &root))

	changed, err := applyChannelEnabledDefaults(&root)
	require.NoError(t, err)
	require.True(t, changed)

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)
	require.Len(t, channelsNode.Content, 1)
	require.Equal(
		t,
		channelTypeWeixin,
		mappingStringValue(channelsNode.Content[0], channelTypeKey),
	)
}

func TestApplyChannelEnabledDefaultsDropsEnabledField(
	t *testing.T,
) {
	t.Parallel()

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(
		"channels:\n"+
			"  - type: weixin\n"+
			"    enabled: true\n",
	), &root))

	changed, err := applyChannelEnabledDefaults(&root)
	require.NoError(t, err)
	require.True(t, changed)

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)
	require.Len(t, channelsNode.Content, 1)
	require.Nil(t, firstMappingValue(
		channelsNode.Content[0],
		channelConfigFieldEnabled,
	))
}

func TestApplyChannelEnabledDefaultsSkipsEnvGatedChannels(
	t *testing.T,
) {
	t.Parallel()

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(
		"channels:\n"+
			"  - type: wecom\n"+
			"    enabled: true\n"+
			"    enabled_if_env_all:\n"+
			"      - WECOM_STREAM_BOT_ID\n"+
			"      - WECOM_STREAM_SECRET\n"+
			"    config:\n"+
			"      aibotid: ${WECOM_STREAM_BOT_ID}\n"+
			"      secret: ${WECOM_STREAM_SECRET}\n"+
			"  - type: weixin\n"+
			"    enabled: true\n",
	), &root))

	changed, err := applyChannelEnabledDefaultsWithLookup(
		&root,
		envLookupFromMap(nil),
	)
	require.NoError(t, err)
	require.True(t, changed)

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)
	require.Len(t, channelsNode.Content, 1)
	require.Equal(
		t,
		channelTypeWeixin,
		mappingStringValue(channelsNode.Content[0], channelTypeKey),
	)
	require.Nil(t, firstMappingValue(
		channelsNode.Content[0],
		channelConfigFieldEnabled,
	))
}

func TestRuntimeAdminProviderRuntimeConfigStatusChannels(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"memory:\n"+
				"  backend: inmemory\n"+
				"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    enabled: false\n"+
				"    config:\n"+
				"      chat_policy: allowlist\n"+
				"  - type: weixin\n"+
				"    name: direct\n"+
				"    config:\n"+
				"      base_url: https://configured.example.com\n"+
				"      enable_typing: false\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         stateDir,
		args: []string{
			"--state-dir",
			stateDir,
		},
		runtimeChannels: []occhannel.Channel{
			stubWXAdminChannel{
				target: weixinchannel.AdminTarget{
					Name:                  "direct",
					StateDir:              filepath.Join(stateDir, "weixin"),
					DefaultBaseURL:        "https://live.example.com",
					EnableTyping:          true,
					EnableRuntimeCommands: true,
				},
			},
		},
	}

	status, err := provider.RuntimeConfigStatus()
	require.NoError(t, err)
	require.Equal(t, configPath, status.ConfigPath)
	require.Len(t, status.Sections, 3)

	require.Equal(
		t,
		"Channel 1 · WeCom · corp",
		status.Sections[1].Title,
	)
	require.Equal(
		t,
		"Channel 2 · Weixin · direct",
		status.Sections[2].Title,
	)

	typeField := requireRuntimeConfigField(
		t,
		status.Sections[1],
		channelConfigFieldKey(0, channelTypeKey),
	)
	require.Equal(t, channelTypeWeCom, typeField.EditorValue)

	enabledField := requireRuntimeConfigField(
		t,
		status.Sections[1],
		channelConfigFieldKey(0, channelConfigFieldEnabled),
	)
	require.Equal(t, "false", enabledField.EditorValue)
	require.Equal(t, "false", enabledField.RuntimeValue)

	baseURLField := requireRuntimeConfigField(
		t,
		status.Sections[2],
		channelConfigFieldKey(1, channelFieldBaseURL),
	)
	require.Equal(
		t,
		"https://configured.example.com",
		baseURLField.ConfiguredValue,
	)
	require.Equal(
		t,
		"https://live.example.com",
		baseURLField.RuntimeValue,
	)

	stateDirField := requireRuntimeConfigField(
		t,
		status.Sections[2],
		channelConfigFieldKey(1, channelFieldStateDir),
	)
	require.Equal(t, "", stateDirField.ConfiguredValue)
	require.Equal(
		t,
		channelConfiguredStateDirFromFlag,
		stateDirField.ConfiguredSourceLabel,
	)
	require.Contains(
		t,
		stateDirField.RuntimeSourceLabel,
		channelRuntimeStateDirFromFlag,
	)

	runtimeCommandsField := requireRuntimeConfigField(
		t,
		status.Sections[2],
		channelConfigFieldKey(1, channelFieldEnableRuntimeCommand),
	)
	require.Equal(t, "", runtimeCommandsField.ConfiguredValue)
	require.Equal(
		t,
		channelConfiguredDefaultPrefix+
			weixinDefaultRuntimeCommandsConfigValue+
			".",
		runtimeCommandsField.ConfiguredSourceLabel,
	)
	require.Contains(
		t,
		runtimeCommandsField.RuntimeSourceLabel,
		channelRuntimeDefaultValueSuffix,
	)
}

func TestRuntimeAdminProviderRuntimeConfigStatusWeComStreamFields(
	t *testing.T,
) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: ai\n"+
				"      connection_mode: websocket\n"+
				"      aibotid: bot-corp\n"+
				"      secret: sec-corp\n"+
				"      enable_stream: true\n"+
				"      stream_snapshot_mode: "+
				wecomStreamSnapshotModeFinalOnly+"\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
		runtimeChannels: []occhannel.Channel{
			stubWeComAdminChannel{
				target: wecomchannel.AdminTarget{
					Name:               "corp",
					BotMode:            wecomAIBotModeConfigValue,
					ConnectionMode:     wecomWebSocketModeConfigValue,
					AIBotID:            "bot-corp",
					EnableStream:       true,
					StreamSnapshotMode: wecomStreamSnapshotModeFinalOnly,
					CallbackPath:       "/wecom/callback",
					ChatPolicy:         "open",
					RuntimeAdminPolicy: "inherit",
					UserLabelMode:      "alias_or_name",
				},
			},
		},
	}

	status, err := provider.RuntimeConfigStatus()
	require.NoError(t, err)
	require.Len(t, status.Sections, 2)

	enableField := requireRuntimeConfigField(
		t,
		status.Sections[1],
		channelConfigFieldKey(0, channelFieldEnableStream),
	)
	require.Equal(t, "true", enableField.EditorValue)
	require.Equal(t, "true", enableField.ConfiguredValue)
	require.Equal(t, "true", enableField.RuntimeValue)
	require.False(t, enableField.PendingRestart)
	require.True(t, enableField.Resettable)

	snapshotField := requireRuntimeConfigField(
		t,
		status.Sections[1],
		channelConfigFieldKey(0, channelFieldStreamSnapshotMode),
	)
	require.Equal(
		t,
		wecomStreamSnapshotModeFinalOnly,
		snapshotField.EditorValue,
	)
	require.Equal(
		t,
		wecomStreamSnapshotModeFinalOnly,
		snapshotField.ConfiguredValue,
	)
	require.Equal(
		t,
		wecomStreamSnapshotModeFinalOnly,
		snapshotField.RuntimeValue,
	)
	require.False(t, snapshotField.PendingRestart)
	require.True(t, snapshotField.Resettable)
	require.Len(t, snapshotField.Options, 3)
}

func TestRuntimeAdminProviderRuntimeConfigStatusWaitingForEnv(
	t *testing.T,
) {
	t.Parallel()

	const (
		testGateBotID  = "TRPC_CLAW_TEST_STATUS_GATE_BOT_ID"
		testGateSecret = "TRPC_CLAW_TEST_STATUS_GATE_SECRET"
	)

	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    enabled: true\n"+
				"    enabled_if_env_all:\n"+
				"      - "+testGateBotID+"\n"+
				"      - "+testGateSecret+"\n"+
				"    config:\n"+
				"      bot_mode: ai\n"+
				"      connection_mode: websocket\n"+
				"      aibotid: ${"+testGateBotID+"}\n"+
				"      secret: ${"+testGateSecret+"}\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         stateDir,
	}

	status, err := provider.RuntimeConfigStatus()
	require.NoError(t, err)
	require.Len(t, status.Sections, 2)

	enabledField := requireRuntimeConfigField(
		t,
		status.Sections[1],
		channelConfigFieldKey(0, channelConfigFieldEnabled),
	)
	require.Equal(t, "true", enabledField.EditorValue)
	require.Equal(t, "false", enabledField.RuntimeValue)
	require.False(t, enabledField.PendingRestart)
	require.Contains(
		t,
		enabledField.RuntimeSourceLabel,
		"waiting for env vars",
	)

	gateField := requireRuntimeConfigField(
		t,
		status.Sections[1],
		channelConfigFieldKey(0, channelConfigFieldEnabledIfEnvAll),
	)
	require.Equal(
		t,
		testGateBotID+", "+testGateSecret,
		gateField.EditorValue,
	)
	require.Equal(
		t,
		"Missing: "+testGateBotID+", "+testGateSecret,
		gateField.RuntimeValue,
	)
	require.Contains(
		t,
		gateField.RuntimeSourceLabel,
		"waiting for env vars",
	)
}

func TestRuntimeAdminProviderRuntimeConfigStatusEnvSection(
	t *testing.T,
) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"memory:\n"+
				"  backend: inmemory\n",
		),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, runtimeEnvFileName),
		[]byte(
			runtimeExtraPathEnvName+"=/saved/bin:/opt/tools\n",
		),
		0o600,
	))

	t.Setenv(runtimePathEnvName, "/base/bin:/usr/bin")
	t.Setenv(
		runtimeExtraPathEnvName,
		"/runtime/bin:/runtime/tools",
	)

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         stateDir,
	}

	status, err := provider.RuntimeConfigStatus()
	require.NoError(t, err)
	require.NotEmpty(t, status.Sections)
	require.Equal(
		t,
		runtimeEnvSectionTitle,
		status.Sections[0].Title,
	)

	extraField := requireRuntimeConfigField(
		t,
		status.Sections[0],
		runtimeEnvFieldKey(runtimeEnvFieldExtraPathDirs),
	)
	require.Equal(t, "/saved/bin:/opt/tools", extraField.ConfiguredValue)
	require.Equal(
		t,
		"/runtime/bin:/runtime/tools",
		extraField.RuntimeValue,
	)
	require.True(t, extraField.PendingRestart)

	pathField := requireRuntimeConfigField(
		t,
		status.Sections[0],
		runtimeEnvFieldKey(runtimeEnvFieldEffectivePath),
	)
	require.Equal(t, "/base/bin:/usr/bin", pathField.RuntimeValue)

	searchField := requireRuntimeConfigField(
		t,
		status.Sections[0],
		runtimeEnvFieldKey(runtimeEnvFieldSearchedDirs),
	)
	require.Equal(
		t,
		"/base/bin, /usr/bin",
		searchField.RuntimeValue,
	)
}

func TestRuntimeAdminProviderSaveAndResetChannelConfigValue(
	t *testing.T,
) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"channels:\n"+
				"  - type: weixin\n"+
				"    config:\n"+
				"      base_url: https://before.example.com\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
	}

	require.NoError(t, provider.SaveRuntimeConfigValue(
		channelConfigFieldKey(0, channelConfigFieldEnabled),
		"false",
	))
	require.NoError(t, provider.SaveRuntimeConfigValue(
		channelConfigFieldKey(0, channelFieldBaseURL),
		"https://after.example.com",
	))

	var root yaml.Node
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)
	channelNode := channelsNode.Content[0]
	configNode := mappingValue(channelNode, channelConfigKey)
	require.Equal(
		t,
		"https://after.example.com",
		mappingStringValue(configNode, channelFieldBaseURL),
	)
	enabled, ok, err := firstMappingBoolValue(
		channelNode,
		channelConfigFieldEnabled,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, enabled)

	require.NoError(t, provider.ResetRuntimeConfigValue(
		channelConfigFieldKey(0, channelFieldBaseURL),
	))
	data, err = os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))
	channelNode = mappingValue(documentNode(&root), channelsKey).Content[0]
	configNode = mappingValue(channelNode, channelConfigKey)
	require.Nil(
		t,
		firstMappingValue(configNode, channelFieldBaseURL),
	)
}

func TestRuntimeAdminProviderSaveAndResetWeComStreamConfig(
	t *testing.T,
) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    config:\n"+
				"      bot_mode: ai\n"+
				"      connection_mode: websocket\n"+
				"      aibotid: bot-corp\n"+
				"      secret: sec-corp\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
	}

	require.NoError(t, provider.SaveRuntimeConfigValue(
		channelConfigFieldKey(0, channelFieldEnableStream),
		"true",
	))
	require.NoError(t, provider.SaveRuntimeConfigValue(
		channelConfigFieldKey(0, channelFieldStreamSnapshotMode),
		wecomStreamSnapshotModeFinalOnly,
	))

	var root yaml.Node
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))

	channelNode := mappingValue(documentNode(&root), channelsKey).Content[0]
	configNode := mappingValue(channelNode, channelConfigKey)
	enabled, ok, err := firstMappingBoolValue(
		configNode,
		channelFieldEnableStream,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, enabled)
	require.Equal(
		t,
		wecomStreamSnapshotModeFinalOnly,
		mappingStringValue(configNode, channelFieldStreamSnapshotMode),
	)

	require.NoError(t, provider.ResetRuntimeConfigValue(
		channelConfigFieldKey(0, channelFieldEnableStream),
	))
	require.NoError(t, provider.ResetRuntimeConfigValue(
		channelConfigFieldKey(0, channelFieldStreamSnapshotMode),
	))

	data, err = os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))
	channelNode = mappingValue(documentNode(&root), channelsKey).Content[0]
	configNode = mappingValue(channelNode, channelConfigKey)
	require.Nil(t, firstMappingValue(configNode, channelFieldEnableStream))
	require.Nil(
		t,
		firstMappingValue(configNode, channelFieldStreamSnapshotMode),
	)
}

func TestRuntimeAdminProviderSaveAndResetEnabledIfEnvAll(
	t *testing.T,
) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte("channels:\n  - type: wecom\n    config: {}\n"),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
	}
	require.NoError(t, provider.SaveRuntimeConfigValue(
		channelConfigFieldKey(0, channelConfigFieldEnabledIfEnvAll),
		"WECOM_STREAM_BOT_ID, WECOM_STREAM_SECRET,\n"+
			"WECOM_STREAM_BOT_ID",
	))

	var root yaml.Node
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))

	channelNode := mappingValue(documentNode(&root), channelsKey).Content[0]
	require.Equal(
		t,
		[]string{
			"WECOM_STREAM_BOT_ID",
			"WECOM_STREAM_SECRET",
		},
		yamlSequenceValues(firstMappingValue(
			channelNode,
			channelConfigFieldEnabledIfEnvAll,
		)),
	)

	require.NoError(t, provider.ResetRuntimeConfigValue(
		channelConfigFieldKey(0, channelConfigFieldEnabledIfEnvAll),
	))

	data, err = os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))
	channelNode = mappingValue(documentNode(&root), channelsKey).Content[0]
	require.Nil(t, firstMappingValue(
		channelNode,
		channelConfigFieldEnabledIfEnvAll,
	))
}

func TestRuntimeAdminProviderSaveAndResetExtraPathDirs(
	t *testing.T,
) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	homeDir := t.TempDir()
	workDir := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWD))
	})
	t.Setenv("HOME", homeDir)
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte("memory:\n  backend: inmemory\n"),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         stateDir,
	}

	require.NoError(t, provider.SaveRuntimeConfigValue(
		runtimeEnvFieldKey(runtimeEnvFieldExtraPathDirs),
		" ~/bin :\n tools/bin ",
	))

	data, err := os.ReadFile(
		filepath.Join(stateDir, runtimeEnvFileName),
	)
	require.NoError(t, err)
	expected := strings.Join(
		[]string{
			filepath.Join(homeDir, defaultUserBinDir),
			filepath.Join(workDir, "tools", defaultUserBinDir),
		},
		string(os.PathListSeparator),
	)
	require.Contains(
		t,
		string(data),
		runtimeExtraPathEnvName+"="+expected,
	)

	require.NoError(t, provider.ResetRuntimeConfigValue(
		runtimeEnvFieldKey(runtimeEnvFieldExtraPathDirs),
	))

	data, err = os.ReadFile(filepath.Join(stateDir, runtimeEnvFileName))
	require.NoError(t, err)
	require.NotContains(t, string(data), runtimeExtraPathEnvName+"=")
}

func TestRuntimeAdminProviderRejectsReadOnlyRuntimeEnvFieldSave(
	t *testing.T,
) {
	provider := &runtimeAdminProvider{
		sourceConfigPath: filepath.Join(t.TempDir(), "openclaw.yaml"),
		stateDir:         t.TempDir(),
	}

	err := provider.SaveRuntimeConfigValue(
		runtimeEnvFieldKey(runtimeEnvFieldEffectivePath),
		"/tmp/bin",
	)
	require.EqualError(t, err, runtimeEnvReadOnlySaveErr)
}

func TestRuntimeAdminProviderStateDirLabelUsesGlobalConfig(
	t *testing.T,
) {
	t.Parallel()

	globalStateDir := filepath.Join(t.TempDir(), "state-root")
	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"state_dir: "+globalStateDir+"\n"+
				"channels:\n"+
				"  - type: weixin\n"+
				"    name: direct\n"+
				"    config:\n"+
				"      base_url: https://configured.example.com\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
		runtimeChannels: []occhannel.Channel{
			stubWXAdminChannel{
				target: weixinchannel.AdminTarget{
					Name:                  "direct",
					StateDir:              filepath.Join(globalStateDir, "weixin"),
					DefaultBaseURL:        "https://live.example.com",
					EnableTyping:          true,
					EnableRuntimeCommands: true,
				},
			},
		},
	}

	status, err := provider.RuntimeConfigStatus()
	require.NoError(t, err)
	require.Len(t, status.Sections, 2)

	stateDirField := requireRuntimeConfigField(
		t,
		status.Sections[1],
		channelConfigFieldKey(0, channelFieldStateDir),
	)
	require.Equal(
		t,
		channelConfiguredStateDirFromConfig,
		stateDirField.ConfiguredSourceLabel,
	)
	require.Contains(
		t,
		stateDirField.RuntimeSourceLabel,
		channelRuntimeStateDirFromConfig,
	)
}

func TestRuntimeAdminProviderSwitchChannelTypeRewritesConfig(
	t *testing.T,
) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: notification\n"+
				"      connection_mode: webhook\n"+
				"      token: tok-1\n"+
				"      encoding_aes_key: aes-1\n"+
				"      webhook_url: https://example.com/hook\n"+
				"      chat_policy: allowlist\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
	}
	require.NoError(t, provider.SaveRuntimeConfigValue(
		channelConfigFieldKey(0, channelTypeKey),
		channelTypeWeixin,
	))

	var root yaml.Node
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))

	channelNode := mappingValue(documentNode(&root), channelsKey).Content[0]
	require.Equal(
		t,
		channelTypeWeixin,
		mappingStringValue(channelNode, channelTypeKey),
	)

	configNode := mappingValue(channelNode, channelConfigKey)
	require.NotNil(t, configNode)
	require.Nil(t, firstMappingValue(configNode, channelFieldBotMode))
	require.Nil(t, firstMappingValue(configNode, channelFieldToken))
	require.Nil(
		t,
		firstMappingValue(configNode, channelFieldWebhookURL),
	)
	require.Len(t, configNode.Content, 0)
}

func TestRuntimeAdminProviderSwitchChannelTypeDisablesIncompleteWeCom(
	t *testing.T,
) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"channels:\n"+
				"  - type: weixin\n"+
				"    name: direct\n"+
				"    config:\n"+
				"      base_url: https://before.example.com\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
	}
	require.NoError(t, provider.SaveRuntimeConfigValue(
		channelConfigFieldKey(0, channelTypeKey),
		channelTypeWeCom,
	))

	var root yaml.Node
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))

	channelNode := mappingValue(documentNode(&root), channelsKey).Content[0]
	require.Equal(
		t,
		channelTypeWeCom,
		mappingStringValue(channelNode, channelTypeKey),
	)

	enabled, ok, err := firstMappingBoolValue(
		channelNode,
		channelConfigFieldEnabled,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, enabled)
}

func TestRuntimeAdminProviderResetRequiredWeComFieldDisablesChannel(
	t *testing.T,
) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    config:\n"+
				"      bot_mode: notification\n"+
				"      connection_mode: webhook\n"+
				"      token: tok-1\n"+
				"      encoding_aes_key: aes-1\n"+
				"      webhook_url: https://example.com/hook\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
	}
	require.NoError(t, provider.ResetRuntimeConfigValue(
		channelConfigFieldKey(0, channelFieldWebhookURL),
	))

	var root yaml.Node
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))

	channelNode := mappingValue(documentNode(&root), channelsKey).Content[0]
	configNode := mappingValue(channelNode, channelConfigKey)
	require.Nil(
		t,
		firstMappingValue(configNode, channelFieldWebhookURL),
	)

	enabled, ok, err := firstMappingBoolValue(
		channelNode,
		channelConfigFieldEnabled,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, enabled)
}

func TestRuntimeAdminProviderResetRequiredWeComFieldKeepsGatedChannel(
	t *testing.T,
) {
	t.Parallel()

	const (
		testGateBotID  = "TRPC_CLAW_TEST_RESET_GATE_BOT_ID"
		testGateSecret = "TRPC_CLAW_TEST_RESET_GATE_SECRET"
	)

	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    enabled: true\n"+
				"    enabled_if_env_all:\n"+
				"      - "+testGateBotID+"\n"+
				"      - "+testGateSecret+"\n"+
				"    config:\n"+
				"      bot_mode: notification\n"+
				"      connection_mode: webhook\n"+
				"      token: tok-1\n"+
				"      encoding_aes_key: aes-1\n"+
				"      webhook_url: https://example.com/hook\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
	}
	require.NoError(t, provider.ResetRuntimeConfigValue(
		channelConfigFieldKey(0, channelFieldWebhookURL),
	))

	var root yaml.Node
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &root))

	channelNode := mappingValue(documentNode(&root), channelsKey).Content[0]
	configNode := mappingValue(channelNode, channelConfigKey)
	require.Nil(
		t,
		firstMappingValue(configNode, channelFieldWebhookURL),
	)
	enabled, ok, err := firstMappingBoolValue(
		channelNode,
		channelConfigFieldEnabled,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, enabled)
}

func TestInjectChannelsAdminNavAddsLink(t *testing.T) {
	t.Parallel()

	base := http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(
			`<html><body><nav class="sidebar-nav"></nav></body></html>`,
		))
	})

	req := httptest.NewRequest(http.MethodGet, channelsOverviewPath, nil)
	rsp := httptest.NewRecorder()
	injectChannelsAdminNav(base).ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(t, rsp.Body.String(), `href="chat"`)
	require.Contains(t, rsp.Body.String(), `href="channels"`)
	require.NotContains(t, rsp.Body.String(), `href="/channels"`)
}

func TestInjectedBaseAdminPageKeepsSidebarScrollable(t *testing.T) {
	t.Parallel()

	base := admin.New(admin.Config{AppName: "openclaw"}).Handler()
	req := httptest.NewRequest(http.MethodGet, channelsOverviewPath, nil)
	rsp := httptest.NewRecorder()
	injectChannelsAdminNav(base).ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	body := rsp.Body.String()
	require.Contains(t, body, `href="chat"`)
	require.Contains(t, body, `href="channels"`)
	require.Contains(t, body, "overflow-y: auto;")
	require.Contains(t, body, "overscroll-behavior: contain;")
	require.Contains(t, body, "scrollbar-gutter: stable;")
	require.Contains(t, body, "sidebar.scrollTop")
	require.Contains(t, body, "openclaw.admin.pendingScroll")
	require.Contains(t, body, "window.sessionStorage")
	require.Contains(t, body, "window.scrollBy")
	require.Contains(t, body, "targetURL.pathname")
	require.Contains(t, body, "value.targetPath")
	require.NotContains(t, body, "window.scrollTo(0, pageTop)")
	require.NotContains(t, body, "pageTop:")
	require.NotContains(t, body, `window.addEventListener("pagehide"`)
	require.NotContains(t, body, "scrollIntoView")
}

func TestInjectChannelsAdminNavUsesNestedRelativeLink(
	t *testing.T,
) {
	t.Parallel()

	base := http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(
			`<html><body><nav class="sidebar-nav"></nav></body></html>`,
		))
	})

	req := httptest.NewRequest(http.MethodGet, "/chats/detail", nil)
	rsp := httptest.NewRecorder()
	injectChannelsAdminNav(base).ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(t, rsp.Body.String(), `href="../chat"`)
	require.Contains(t, rsp.Body.String(), `href="../channels"`)
}

func TestChannelsAdminPageRendersConfiguredAndRuntimeSections(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"memory:\n"+
				"  backend: inmemory\n"+
				"channels:\n"+
				"  - type: weixin\n"+
				"    name: direct\n"+
				"    config:\n"+
				"      base_url: https://configured.example.com\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: ai\n"+
				"      connection_mode: websocket\n",
		),
		0o600,
	))

	require.NoError(t, weixinchannel.SaveAccount(
		stateDir,
		weixinchannel.Account{
			AccountID: "acc-1",
			Token:     "tok-1",
			BaseURL:   "https://live.example.com",
			UserID:    "user-1",
		},
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
		runtimeChannels: []occhannel.Channel{
			stubWXAdminChannel{
				target: weixinchannel.AdminTarget{
					Name:                  "direct",
					StateDir:              stateDir,
					DefaultBaseURL:        "https://live.example.com",
					EnableTyping:          true,
					EnableRuntimeCommands: true,
				},
			},
			stubWeComAdminChannel{
				target: wecomchannel.AdminTarget{
					Name:               "corp",
					StateDir:           t.TempDir(),
					BotMode:            "ai",
					ConnectionMode:     "websocket",
					CallbackPath:       "/wecom/callback",
					ChatPolicy:         "open",
					RuntimeAdminPolicy: "inherit",
					UserLabelMode:      "alias_or_name",
				},
			},
		},
	}
	weixinSvc := newWeixinAdminServiceWithRunner(
		[]weixinchannel.AdminTarget{{
			Name:           "direct",
			StateDir:       stateDir,
			DefaultBaseURL: "https://live.example.com",
		}},
		nil,
	)
	wecomChannel := provider.runtimeChannels[1]
	wecomRuntime := wecomChannel.(stubWeComAdminChannel)
	require.NoError(t, os.WriteFile(
		filepath.Join(
			wecomRuntime.target.StateDir,
			runtimeEnvFileName,
		),
		[]byte(
			wecomActivateDefaultUserEnvName+
				"='"+testWeComCreatorUserID+"'\n",
		),
		0o600,
	))
	wecomActChannel := &stubActChannel{
		target: wecomchannel.AdminTarget{
			Name:               "corp",
			StateDir:           wecomRuntime.target.StateDir,
			BotMode:            "ai",
			ConnectionMode:     "websocket",
			CallbackPath:       "/wecom/callback",
			ChatPolicy:         "open",
			RuntimeAdminPolicy: "inherit",
			UserLabelMode:      "alias_or_name",
			AIBotID:            "bot-corp",
		},
		status: wecomchannel.AdminActivationStatus{
			Supported: true,
			Available: true,
		},
	}
	wecomActivateSvc := newWeComActivateAdminService(
		[]occhannel.Channel{wecomActChannel},
	)
	wecomDebugSendSvc := newWeComDebugSendAdminService(
		[]occhannel.Channel{wecomActChannel},
	)
	service := newChannelsAdminService(
		provider,
		weixinSvc,
		wecomActivateSvc,
		wecomDebugSendSvc,
	)

	req := httptest.NewRequest(http.MethodGet, channelsAdminPagePath, nil)
	rsp := httptest.NewRecorder()
	wrapChannelsAdminHandler(nil, service).ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(t, rsp.Body.String(), "Configured Channels")
	require.Contains(t, rsp.Body.String(), "overflow-y: auto")
	require.Contains(t, rsp.Body.String(), "overflow: visible")
	require.Contains(t, rsp.Body.String(), "overscroll-behavior: contain")
	require.Contains(t, rsp.Body.String(), "scrollbar-gutter: stable")
	require.Contains(t, rsp.Body.String(), "sidebar.scrollTop")
	require.Contains(t, rsp.Body.String(), "openclaw.admin.pendingScroll")
	require.Contains(t, rsp.Body.String(), "window.sessionStorage")
	require.Contains(t, rsp.Body.String(), "window.scrollBy")
	require.Contains(t, rsp.Body.String(), "targetURL.pathname")
	require.Contains(t, rsp.Body.String(), "value.targetPath")
	require.NotContains(t, rsp.Body.String(), "window.scrollTo(0, pageTop)")
	require.NotContains(t, rsp.Body.String(), "pageTop:")
	require.NotContains(
		t,
		rsp.Body.String(),
		`window.addEventListener("pagehide"`,
	)
	require.NotContains(t, rsp.Body.String(), "scrollIntoView")
	require.Contains(t, rsp.Body.String(), "Weixin Runtime")
	require.Contains(t, rsp.Body.String(), "WeCom Runtime")
	require.Contains(
		t,
		rsp.Body.String(),
		"config#config-section-channel-1",
	)
	require.Contains(
		t,
		rsp.Body.String(),
		"config#config-field-channels.0.type",
	)
	require.Contains(t, rsp.Body.String(), `href="channels"`)
	require.Contains(
		t,
		rsp.Body.String(),
		`action="api/weixin/login/start"`,
	)
	require.Contains(
		t,
		rsp.Body.String(),
		`action="api/channels/wecom/activate"`,
	)
	require.Contains(
		t,
		rsp.Body.String(),
		`action="api/channels/wecom/debug/send"`,
	)
	require.NotContains(t, rsp.Body.String(), `href="/channels"`)
	require.Contains(t, rsp.Body.String(), "Open Runtime Control")
	require.Contains(t, rsp.Body.String(), "Open Chats")
	require.Contains(t, rsp.Body.String(), "Send Activation")
	require.Contains(t, rsp.Body.String(), "Send Debug Message")
	require.Contains(t, rsp.Body.String(), `name="wecom_user_id"`)
	require.Contains(
		t,
		rsp.Body.String(),
		`value="`+testWeComCreatorUserID+`"`,
	)
}

func TestChannelsAdminPageHidesImplicitWeixinConfigLink(
	t *testing.T,
) {
	t.Parallel()

	weixinStateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(
			"memory:\n"+
				"  backend: inmemory\n"+
				"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: ai\n"+
				"      connection_mode: websocket\n",
		),
		0o600,
	))

	provider := &runtimeAdminProvider{
		sourceConfigPath: configPath,
		stateDir:         t.TempDir(),
		runtimeChannels: []occhannel.Channel{
			stubWXAdminChannel{
				target: weixinchannel.AdminTarget{
					Name:           implicitWeixinChannelName,
					StateDir:       weixinStateDir,
					DefaultBaseURL: weixinDefaultBaseURLConfigValue,
				},
			},
			stubWeComAdminChannel{
				target: wecomchannel.AdminTarget{
					Name:               "corp",
					StateDir:           t.TempDir(),
					BotMode:            "ai",
					ConnectionMode:     "websocket",
					CallbackPath:       "/wecom/callback",
					ChatPolicy:         "open",
					RuntimeAdminPolicy: "inherit",
					UserLabelMode:      "alias_or_name",
				},
			},
		},
	}
	weixinSvc := newWeixinAdminServiceWithRunner(
		[]weixinchannel.AdminTarget{{
			Name:           implicitWeixinChannelName,
			StateDir:       weixinStateDir,
			DefaultBaseURL: weixinDefaultBaseURLConfigValue,
		}},
		nil,
	)
	service := newChannelsAdminService(provider, weixinSvc, nil)

	req := httptest.NewRequest(http.MethodGet, channelsAdminPagePath, nil)
	rsp := httptest.NewRecorder()
	wrapChannelsAdminHandler(nil, service).ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(t, rsp.Body.String(), "Weixin Runtime")
	require.Contains(t, rsp.Body.String(), "Open QR Entry")
	require.Contains(t, rsp.Body.String(), weixinImplicitConfigHint)
	require.NotContains(
		t,
		rsp.Body.String(),
		`href="config#config-section-"`,
	)
}

func TestBuildWeComRuntimeViewsKeepsMatchedConfigSection(
	t *testing.T,
) {
	t.Parallel()

	targets := []runtimeChannelTarget{
		{
			Type: channelTypeWeCom,
			Name: "alpha",
			WeCom: &wecomchannel.AdminTarget{
				Name:               "alpha",
				StateDir:           t.TempDir(),
				BotMode:            wecomAIBotModeConfigValue,
				ConnectionMode:     wecomWebSocketModeConfigValue,
				CallbackPath:       wecomDefaultCallbackPathConfigValue,
				ChatPolicy:         wecomDefaultChatPolicyConfigValue,
				RuntimeAdminPolicy: wecomDefaultRuntimeAdminConfigValue,
				UserLabelMode:      wecomDefaultUserLabelModeConfigValue,
			},
		},
		{
			Type: channelTypeWeCom,
			Name: "beta",
			WeCom: &wecomchannel.AdminTarget{
				Name:               "beta",
				StateDir:           t.TempDir(),
				BotMode:            wecomDefaultBotModeConfigValue,
				ConnectionMode:     wecomDefaultConnectionModeConfigValue,
				CallbackPath:       wecomDefaultCallbackPathConfigValue,
				ChatPolicy:         wecomDefaultChatPolicyConfigValue,
				RuntimeAdminPolicy: wecomDefaultRuntimeAdminConfigValue,
				UserLabelMode:      wecomDefaultUserLabelModeConfigValue,
			},
		},
	}
	entries := []configuredChannelEntry{
		{
			Key:        channelConfigSectionKey(0),
			SectionKey: channelConfigSectionKey(0),
			Type:       channelTypeWeCom,
			Name:       "beta",
			Enabled:    true,
		},
		{
			Key:        channelConfigSectionKey(1),
			SectionKey: channelConfigSectionKey(1),
			Type:       channelTypeWeCom,
			Name:       "alpha",
			Enabled:    true,
		},
	}

	sections := configuredRuntimeSectionKeys(entries, targets)
	views := buildWeComRuntimeViews(
		targets,
		sections,
		map[string]string{},
		nil,
		nil,
	)
	require.Len(t, views, 2)
	require.Equal(t, channelConfigSectionKey(1), views[0].ConfigSectionKey)
	require.Equal(t, channelConfigSectionKey(0), views[1].ConfigSectionKey)
}

func TestBuildConfiguredChannelViewsStates(t *testing.T) {
	t.Parallel()

	entries := []configuredChannelEntry{
		{
			SectionKey:              channelConfigSectionKey(0),
			Index:                   0,
			Type:                    channelTypeWeCom,
			Name:                    "disabled",
			Enabled:                 false,
			EffectiveEnabled:        false,
			MissingEnabledEnv:       nil,
			ConfigNode:              &yaml.Node{Kind: yaml.MappingNode},
			ChannelNode:             &yaml.Node{Kind: yaml.MappingNode},
			EnabledIfEnvAll:         nil,
			EnabledIfEnvAllExplicit: false,
		},
		{
			SectionKey:        channelConfigSectionKey(1),
			Index:             1,
			Type:              channelTypeWeCom,
			Name:              "waiting",
			Enabled:           true,
			EffectiveEnabled:  false,
			MissingEnabledEnv: []string{"WECOM_STREAM_SECRET"},
			ConfigNode:        &yaml.Node{Kind: yaml.MappingNode},
			ChannelNode:       &yaml.Node{Kind: yaml.MappingNode},
		},
		{
			SectionKey:       channelConfigSectionKey(2),
			Index:            2,
			Type:             channelTypeWeCom,
			Name:             "restart",
			Enabled:          true,
			EffectiveEnabled: true,
			ConfigNode:       &yaml.Node{Kind: yaml.MappingNode},
			ChannelNode:      &yaml.Node{Kind: yaml.MappingNode},
		},
	}

	views := buildConfiguredChannelViews(
		entries,
		map[string]*runtimeChannelTarget{},
	)
	require.Len(t, views, 3)
	require.Equal(t, "Disabled", views[0].StateLabel)
	require.Equal(t, channelCardStateDisabled, views[0].StateClass)
	require.Equal(t, "Waiting For Env", views[1].StateLabel)
	require.Equal(
		t,
		channelCardStateWaitingForEnv,
		views[1].StateClass,
	)
	require.Equal(t, "Restart Required", views[2].StateLabel)
	require.Equal(
		t,
		channelCardStateRestartRequired,
		views[2].StateClass,
	)
}
