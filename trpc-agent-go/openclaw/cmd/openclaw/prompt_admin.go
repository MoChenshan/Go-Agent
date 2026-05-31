package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

const (
	runtimePromptBundleAgentInstruction = "agent_instruction"
	runtimePromptBundleAgentSystem      = "agent_system"
	runtimePromptBundleWeComPrefix      = "wecom_request_"

	promptAdminDefaultPerm = 0o600

	promptBundleModeNone         = "none"
	promptBundleModeFiles        = "files"
	promptBundleModeDir          = "dir"
	promptBundleModeDefaultFiles = "default_files"
	promptBundleModeDefaultDir   = "default_dir"

	personaStoreAgentLabel  = "Agent Personas"
	personaStoreSharedTitle = "Shared Persona Store"
)

type runtimeAdminProvider struct {
	sourceConfigPath string
	stateDir         string
	appName          string
	args             []string
	sessionSvc       session.Service

	reloader *runtimePromptReloader

	runtimeChannels []occhannel.Channel
	wecomTargets    []runtimeWeComPromptTarget
}

type promptBundleState struct {
	Key             string              `json:"key"`
	Title           string              `json:"title"`
	Summary         string              `json:"summary,omitempty"`
	SourceSummary   string              `json:"source_summary,omitempty"`
	Configured      string              `json:"configured,omitempty"`
	ConfiguredLabel string              `json:"configured_label,omitempty"`
	InlineValue     string              `json:"inline_value,omitempty"`
	InlineEnabled   bool                `json:"inline_enabled"`
	Effective       string              `json:"effective,omitempty"`
	EffectiveLabel  string              `json:"effective_label,omitempty"`
	SourceMode      string              `json:"source_mode,omitempty"`
	CreateDir       string              `json:"create_dir,omitempty"`
	Sources         []promptSourceState `json:"sources,omitempty"`
}

type promptSourceState struct {
	Path      string `json:"path"`
	Label     string `json:"label"`
	Content   string `json:"content,omitempty"`
	Deletable bool   `json:"deletable"`
}

type personaStoreState struct {
	Dir         string                  `json:"dir,omitempty"`
	Title       string                  `json:"title,omitempty"`
	UsageLabels []string                `json:"usage_labels,omitempty"`
	Definitions []personaapi.Definition `json:"definitions,omitempty"`
}

type runtimePromptAdminState struct {
	ConfigPath             string `json:"config_path,omitempty"`
	AssistantName          runtimeAssistantNameState
	WeComUserLabelMode     string                  `json:"wecom_user_label_mode,omitempty"`
	WeComUserLookupCommand string                  `json:"wecom_user_lookup_command,omitempty"`
	DefaultPersonaID       string                  `json:"default_persona_id,omitempty"`
	DefaultPersonaOptions  []personaapi.Definition `json:"default_persona_options,omitempty"`
	Bundles                []promptBundleState     `json:"bundles,omitempty"`
	PersonaStores          []personaStoreState     `json:"persona_stores,omitempty"`
}

type promptBundleConfigBinding struct {
	node     *yaml.Node
	filesKey string
	dirKey   string
}

var (
	_ admin.PromptsProvider       = (*runtimeAdminProvider)(nil)
	_ admin.IdentityProvider      = (*runtimeAdminProvider)(nil)
	_ admin.PersonasProvider      = (*runtimeAdminProvider)(nil)
	_ admin.RuntimeConfigProvider = (*runtimeAdminProvider)(nil)
)

func newRuntimeAdminProvider(
	sourceConfigPath string,
	stateDir string,
	appName string,
	args []string,
	sessionSvc session.Service,
	controller agentPromptController,
	runtimeChannels []occhannel.Channel,
	wecomTargets []runtimeWeComPromptTarget,
) (*runtimeAdminProvider, func()) {
	provider := &runtimeAdminProvider{
		sourceConfigPath: strings.TrimSpace(sourceConfigPath),
		stateDir:         strings.TrimSpace(stateDir),
		appName:          strings.TrimSpace(appName),
		args:             append([]string(nil), args...),
		sessionSvc:       sessionSvc,
		runtimeChannels: append(
			[]occhannel.Channel(nil),
			runtimeChannels...,
		),
		wecomTargets: append(
			[]runtimeWeComPromptTarget(nil),
			wecomTargets...,
		),
	}
	provider.reloader = newRuntimePromptReloader(
		provider.sourceConfigPath,
		provider.stateDir,
		provider.args,
		controller,
		provider.wecomTargets,
	)
	closeFn := func() {}
	if provider.reloader != nil {
		closeFn = provider.reloader.Start()
	}
	return provider, closeFn
}

func (p *runtimeAdminProvider) PromptsStatus() (
	admin.PromptsStatus,
	error,
) {
	state, err := p.loadState()
	if err != nil {
		return admin.PromptsStatus{}, err
	}
	return buildRuntimePromptsStatus(state), nil
}

func buildAdminPromptBundleState(
	bundle promptBundleState,
) admin.PromptBundleState {
	files := make([]admin.PromptFileState, 0, len(bundle.Sources))
	supportsDelete := false
	for _, source := range bundle.Sources {
		files = append(files, admin.PromptFileState{
			Path:      source.Path,
			Label:     source.Label,
			Content:   source.Content,
			Deletable: source.Deletable,
		})
		supportsDelete = supportsDelete || source.Deletable
	}
	createEnabled := strings.TrimSpace(bundle.CreateDir) != ""
	return admin.PromptBundleState{
		Key:                bundle.Key,
		Title:              bundle.Title,
		Summary:            bundle.Summary,
		SourceSummary:      bundle.SourceSummary,
		ConfiguredLabel:    bundle.ConfiguredLabel,
		ConfiguredValue:    bundle.Configured,
		EffectiveLabel:     bundle.EffectiveLabel,
		EffectiveValue:     bundle.Effective,
		InlineValue:        bundle.InlineValue,
		InlineEditable:     bundle.InlineEnabled,
		RuntimeEditable:    false,
		RuntimeOverride:    false,
		CreateEnabled:      createEnabled,
		CreateDir:          strings.TrimSpace(bundle.CreateDir),
		Files:              files,
		SupportsFileEdits:  len(files) > 0,
		SupportsFileCreate: createEnabled,
		SupportsFileDelete: supportsDelete,
	}
}

func buildRuntimePromptsStatus(
	state runtimePromptAdminState,
) admin.PromptsStatus {
	out := admin.PromptsStatus{
		Enabled:  true,
		Bundles:  make([]admin.PromptBundleState, 0, len(state.Bundles)),
		Sections: make([]admin.PromptSectionState, 0, 2),
		Previews: make([]admin.PromptPreviewState, 0, len(state.Bundles)),
	}

	core := make([]admin.PromptBundleState, 0, 2)
	channel := make([]admin.PromptBundleState, 0, len(state.Bundles))
	for _, bundle := range state.Bundles {
		adminBundle := buildAdminPromptBundleState(bundle)
		out.Bundles = append(out.Bundles, adminBundle)
		switch {
		case strings.HasPrefix(bundle.Key, runtimePromptBundleWeComPrefix):
			channel = append(channel, adminBundle)
		default:
			core = append(core, adminBundle)
		}
	}

	if len(core) > 0 {
		out.Sections = append(out.Sections, admin.PromptSectionState{
			Key:     "core",
			Title:   "Core Prompt",
			Summary: "These blocks shape the assistant across every turn.",
			Bundles: core,
		})
		if content := buildAgentPromptPreview(state.Bundles); content != "" {
			out.Previews = append(
				[]admin.PromptPreviewState{{
					Key:   "agent",
					Title: "Agent Prompt",
					Summary: "The resolved editable prompt surface after" +
						" defaults, personas, and runtime" +
						" substitutions.",
					Content: content,
				}},
				out.Previews...,
			)
		}
	}
	if len(channel) > 0 {
		out.Sections = append(out.Sections, admin.PromptSectionState{
			Key:   "channels",
			Title: "Channel Prompts",
			Summary: "These blocks add channel-specific request guidance" +
				" before a turn reaches the agent.",
			Bundles: channel,
		})
	}
	return out
}

func (p *runtimeAdminProvider) PersonasStatus() (
	admin.PersonasStatus,
	error,
) {
	state, err := p.loadState()
	if err != nil {
		return admin.PersonasStatus{}, err
	}
	out := admin.PersonasStatus{
		Enabled:          true,
		DefaultPersonaID: state.DefaultPersonaID,
		DefaultOptions: make(
			[]admin.PersonaOption,
			0,
			len(state.DefaultPersonaOptions),
		),
		Stores: make([]admin.PersonaStoreView, 0, len(state.PersonaStores)),
	}
	for _, def := range state.DefaultPersonaOptions {
		out.DefaultOptions = append(out.DefaultOptions, admin.PersonaOption{
			ID:      def.ID,
			Name:    def.Name,
			Summary: def.Summary,
		})
	}
	for _, store := range state.PersonaStores {
		personas := make([]admin.PersonaView, 0, len(store.Definitions))
		for _, def := range store.Definitions {
			personas = append(personas, admin.PersonaView{
				ID:        def.ID,
				Name:      def.Name,
				Summary:   def.Summary,
				Prompt:    def.Prompt,
				BuiltIn:   def.BuiltIn,
				Editable:  true,
				Deletable: personaOverrideExists(store.Dir, def),
			})
		}
		out.Stores = append(out.Stores, admin.PersonaStoreView{
			Key:           store.Dir,
			Title:         store.Title,
			Path:          store.Dir,
			UsageLabels:   append([]string(nil), store.UsageLabels...),
			CreateEnabled: strings.TrimSpace(store.Dir) != "",
			Personas:      personas,
		})
	}
	return out, nil
}

func (p *runtimeAdminProvider) IdentityStatus() (
	admin.IdentityStatus,
	error,
) {
	state, err := p.loadState()
	if err != nil {
		return admin.IdentityStatus{}, err
	}
	return admin.IdentityStatus{
		Enabled:        true,
		ConfiguredName: state.AssistantName.ConfiguredName,
		EffectiveName:  state.AssistantName.EffectiveName,
		RuntimeProduct: state.AssistantName.RuntimeProduct,
		SourcePath:     state.AssistantName.SourcePath,
		FallbackSource: state.AssistantName.FallbackSource,
	}, nil
}

func (p *runtimeAdminProvider) SaveAssistantName(name string) error {
	path := promptasset.DefaultPaths(p.stateDir).IdentityFile
	if err := assistantname.WriteFile(path, name); err != nil {
		return err
	}
	return p.reload()
}

func personaOverrideExists(
	dir string,
	def personaapi.Definition,
) bool {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(def.ID) == "" {
		return false
	}
	if !def.BuiltIn {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, def.ID+".md"))
	return err == nil
}

func (p *runtimeAdminProvider) SavePromptInline(
	bundleKey string,
	content string,
) error {
	rawRoot, _, err := loadPromptConfigRoots(
		p.sourceConfigPath,
		p.args,
		p.stateDir,
	)
	if err != nil {
		return err
	}
	doc := documentNode(rawRoot)
	agentNode := ensureMappingValue(doc, agentKey)
	if agentNode == nil {
		return fmt.Errorf("config: missing agent")
	}
	switch strings.TrimSpace(bundleKey) {
	case runtimePromptBundleAgentInstruction:
		updateMappingString(agentNode, instructionKey, content)
	case runtimePromptBundleAgentSystem:
		updateMappingString(agentNode, systemPromptKey, content)
	default:
		return fmt.Errorf("inline prompt is not editable")
	}
	if err := writeYAMLConfigNode(
		p.sourceConfigPath,
		rawRoot,
	); err != nil {
		return err
	}
	return p.reload()
}

func (p *runtimeAdminProvider) SavePromptRuntime(
	bundleKey string,
	content string,
) error {
	return fmt.Errorf(
		"runtime prompt overrides are not supported for %s",
		strings.TrimSpace(bundleKey),
	)
}

func (p *runtimeAdminProvider) SavePromptFile(
	bundleKey string,
	path string,
	content string,
) error {
	state, err := p.loadState()
	if err != nil {
		return err
	}
	bundle, ok := findPromptBundle(state.Bundles, bundleKey)
	if !ok {
		return fmt.Errorf("unknown prompt bundle")
	}
	source, ok := findPromptSource(bundle.Sources, path)
	if !ok {
		return fmt.Errorf("unknown prompt source")
	}
	if err := writePromptTextFile(source.Path, content); err != nil {
		return err
	}
	return p.reload()
}

func (p *runtimeAdminProvider) CreatePromptFile(
	bundleKey string,
	fileName string,
	content string,
) error {
	state, err := p.loadState()
	if err != nil {
		return err
	}
	bundle, ok := findPromptBundle(state.Bundles, bundleKey)
	if !ok {
		return fmt.Errorf("unknown prompt bundle")
	}
	if strings.TrimSpace(bundle.CreateDir) == "" {
		return fmt.Errorf("prompt bundle does not support file creation")
	}
	fileName, err = normalizePromptAdminFileName(fileName)
	if err != nil {
		return err
	}
	path := filepath.Join(bundle.CreateDir, fileName)
	rawRoot, _, err := loadPromptConfigRoots(
		p.sourceConfigPath,
		p.args,
		p.stateDir,
	)
	if err != nil {
		return err
	}
	if err := updatePromptBundleSourceConfig(
		rawRoot,
		p.sourceConfigPath,
		bundle,
		path,
		true,
	); err != nil {
		return err
	}
	if err := writePromptTextFile(path, content); err != nil {
		return err
	}
	if err := writeYAMLConfigNode(
		p.sourceConfigPath,
		rawRoot,
	); err != nil {
		return err
	}
	return p.reload()
}

func (p *runtimeAdminProvider) DeletePromptFile(
	bundleKey string,
	path string,
) error {
	state, err := p.loadState()
	if err != nil {
		return err
	}
	bundle, ok := findPromptBundle(state.Bundles, bundleKey)
	if !ok {
		return fmt.Errorf("unknown prompt bundle")
	}
	source, ok := findPromptSource(bundle.Sources, path)
	if !ok || !source.Deletable {
		return fmt.Errorf("prompt source is not deletable")
	}
	rawRoot, _, err := loadPromptConfigRoots(
		p.sourceConfigPath,
		p.args,
		p.stateDir,
	)
	if err != nil {
		return err
	}
	if err := updatePromptBundleSourceConfig(
		rawRoot,
		p.sourceConfigPath,
		bundle,
		source.Path,
		false,
	); err != nil {
		return err
	}
	if err := os.Remove(source.Path); err != nil {
		return err
	}
	if err := writeYAMLConfigNode(
		p.sourceConfigPath,
		rawRoot,
	); err != nil {
		return err
	}
	return p.reload()
}

func (p *runtimeAdminProvider) SavePersona(
	storeKey string,
	personaID string,
	name string,
	prompt string,
) error {
	state, err := p.loadState()
	if err != nil {
		return err
	}
	if !hasPersonaStore(state.PersonaStores, storeKey) {
		return fmt.Errorf("unknown persona store")
	}
	registry := personaapi.NewRegistry(storeKey)
	if strings.TrimSpace(personaID) == "" {
		_, err = registry.Save(name, prompt)
	} else {
		_, err = registry.Upsert(personaID, name, prompt)
	}
	if err != nil {
		return err
	}
	return p.reload()
}

func (p *runtimeAdminProvider) DeletePersona(
	storeKey string,
	personaID string,
) error {
	state, err := p.loadState()
	if err != nil {
		return err
	}
	if !hasPersonaStore(state.PersonaStores, storeKey) {
		return fmt.Errorf("unknown persona store")
	}
	if err := personaapi.NewRegistry(storeKey).Delete(personaID); err != nil {
		return err
	}
	return p.reload()
}

func (p *runtimeAdminProvider) SetDefaultPersona(
	personaID string,
) error {
	personaID, enabled := normalizeConfiguredPersonaID(personaID)
	rawRoot, _, err := loadPromptConfigRoots(
		p.sourceConfigPath,
		p.args,
		p.stateDir,
	)
	if err != nil {
		return err
	}
	doc := documentNode(rawRoot)
	agentNode := ensureMappingValue(doc, agentKey)
	if agentNode == nil {
		return fmt.Errorf("config: missing agent")
	}
	if !enabled {
		deleteMappingKey(agentNode, personaKey)
	} else {
		setMappingString(agentNode, personaKey, personaID)
	}
	if err := writeYAMLConfigNode(
		p.sourceConfigPath,
		rawRoot,
	); err != nil {
		return err
	}
	return p.reload()
}

func (s *runtimeAdminProvider) reload() error {
	if s == nil || s.reloader == nil {
		return nil
	}
	return s.reloader.Reload()
}

func (s *runtimeAdminProvider) loadState() (
	runtimePromptAdminState,
	error,
) {
	rawRoot, preparedRoot, err := loadPromptConfigRoots(
		s.sourceConfigPath,
		s.args,
		s.stateDir,
	)
	if err != nil {
		return runtimePromptAdminState{}, err
	}
	state := runtimePromptAdminState{
		ConfigPath: strings.TrimSpace(s.sourceConfigPath),
	}
	rawDoc := documentNode(rawRoot)
	rawAgentNode := mappingValue(rawDoc, agentKey)
	preparedDoc := documentNode(preparedRoot)
	preparedAgentNode := mappingValue(preparedDoc, agentKey)
	state.AssistantName, err = loadRuntimeAssistantNameState(
		preparedRoot,
		s.stateDir,
	)
	if err != nil {
		return runtimePromptAdminState{}, err
	}

	defaultPaths := promptasset.Paths{}
	if strings.TrimSpace(s.stateDir) != "" {
		paths, err := promptasset.EnsureDefaultFiles(s.stateDir)
		if err != nil {
			return runtimePromptAdminState{}, err
		}
		defaultPaths = paths
	}

	instructionBundle, err := buildPromptBundleState(
		runtimePromptBundleAgentInstruction,
		"Instruction",
		configBaseDir(s.sourceConfigPath),
		mappingStringValue(rawAgentNode, instructionKey),
		yamlSequenceValues(mappingValue(rawAgentNode, instructionFilesKey)),
		mappingStringValue(rawAgentNode, instructionDirKey),
		defaultPaths.InstructionDir,
		[]string{promptasset.DefaultMemoryFileName},
		strings.TrimSpace(mappingStringValue(
			preparedAgentNode,
			instructionKey,
		)),
		true,
	)
	if err != nil {
		return runtimePromptAdminState{}, err
	}
	state.Bundles = append(state.Bundles, instructionBundle)

	systemBundle, err := buildPromptBundleState(
		runtimePromptBundleAgentSystem,
		"System Prompt",
		configBaseDir(s.sourceConfigPath),
		stripLegacyManagedSystemPrompt(
			mappingStringValue(rawAgentNode, systemPromptKey),
		),
		yamlSequenceValues(mappingValue(rawAgentNode, systemPromptFilesKey)),
		mappingStringValue(rawAgentNode, systemPromptDirKey),
		defaultPaths.SystemDir,
		defaultSystemPromptFiles(),
		stripLegacyManagedSystemPrompt(mappingStringValue(
			preparedAgentNode,
			systemPromptKey,
		)),
		true,
	)
	if err != nil {
		return runtimePromptAdminState{}, err
	}
	state.Bundles = append(state.Bundles, systemBundle)

	preparedWeComNodes := collectWeComConfigNodes(preparedRoot)
	state.WeComUserLabelMode = firstWeComConfigString(
		preparedWeComNodes,
		wecomUserLabelModeKey,
	)
	state.WeComUserLookupCommand = firstWeComConfigString(
		preparedWeComNodes,
		wecomUserLookupCommandConfigKey,
	)
	for i := range s.wecomTargets {
		var configNode *yaml.Node
		if i < len(preparedWeComNodes) {
			configNode = preparedWeComNodes[i]
		}
		bundle, err := buildPromptBundleState(
			runtimePromptBundleWeComPrefix+fmt.Sprintf("%d", i),
			firstNonEmptyString(
				s.wecomTargets[i].Label,
				fmt.Sprintf("WeCom Turn Template %d", i+1),
			),
			configBaseDir(s.sourceConfigPath),
			"",
			yamlSequenceValues(mappingValue(
				configNode,
				wecomchannel.RequestSystemPromptFilesConfigKey,
			)),
			mappingStringValue(
				configNode,
				wecomchannel.RequestSystemPromptDirConfigKey,
			),
			defaultPaths.WeComRequestDir,
			nil,
			loadWeComEffectiveTemplate(
				configNode,
				s.sourceConfigPath,
				s.stateDir,
			),
			false,
		)
		if err != nil {
			return runtimePromptAdminState{}, err
		}
		state.Bundles = append(state.Bundles, bundle)
	}

	agentPersonaDir, err := resolveAgentPersonaDir(
		rawAgentNode,
		s.stateDir,
		s.sourceConfigPath,
	)
	if err != nil {
		return runtimePromptAdminState{}, err
	}
	state.DefaultPersonaID, _ = normalizeConfiguredPersonaID(
		mappingStringValue(rawAgentNode, personaKey),
	)
	options, err := personaapi.NewRegistry(agentPersonaDir).List()
	if err != nil {
		return runtimePromptAdminState{}, err
	}
	state.DefaultPersonaOptions = options

	storeDefs, err := collectPersonaStoreStates(
		agentPersonaDir,
		s.wecomTargets,
	)
	if err != nil {
		return runtimePromptAdminState{}, err
	}
	state.PersonaStores = storeDefs
	return state, nil
}

func firstWeComConfigString(
	nodes []*yaml.Node,
	key string,
) string {
	for _, node := range nodes {
		value := strings.TrimSpace(
			mappingStringValue(node, key),
		)
		if value != "" {
			return value
		}
	}
	return ""
}

func buildPromptBundleState(
	key string,
	title string,
	baseDir string,
	inlineValue string,
	files []string,
	dir string,
	defaultDir string,
	defaultFiles []string,
	effective string,
	inlineEnabled bool,
) (promptBundleState, error) {
	sources, mode, createDir, err := resolvePromptBundleSources(
		baseDir,
		files,
		dir,
		defaultDir,
		defaultFiles,
	)
	if err != nil {
		return promptBundleState{}, err
	}
	configured, err := loadConfiguredPromptValue(
		baseDir,
		inlineValue,
		files,
		dir,
	)
	if err != nil {
		return promptBundleState{}, err
	}
	return promptBundleState{
		Key:             key,
		Title:           title,
		Summary:         promptBundleSummary(key),
		SourceSummary:   promptSourceSummary(mode, inlineValue, len(sources)),
		Configured:      configured,
		ConfiguredLabel: promptConfiguredLabel(key),
		InlineValue:     inlineValue,
		InlineEnabled:   inlineEnabled,
		Effective:       promptBundleEffectiveValue(key, effective),
		EffectiveLabel:  promptEffectiveLabel(key),
		SourceMode:      mode,
		CreateDir:       createDir,
		Sources:         sources,
	}, nil
}

func loadConfiguredPromptValue(
	baseDir string,
	inlineValue string,
	files []string,
	dir string,
) (string, error) {
	parts := make([]string, 0, 2)
	if value := strings.TrimSpace(inlineValue); value != "" {
		parts = append(parts, value)
	}
	if len(files) == 0 && strings.TrimSpace(dir) == "" {
		return strings.Join(parts, "\n\n"), nil
	}
	resolvedFiles, resolvedDir, err := promptasset.ResolvePaths(
		baseDir,
		files,
		dir,
	)
	if err != nil {
		return "", err
	}
	text, err := promptasset.ReadDiskBundle(resolvedFiles, resolvedDir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n"), nil
}

func resolvePromptBundleSources(
	baseDir string,
	files []string,
	dir string,
	defaultDir string,
	defaultFiles []string,
) ([]promptSourceState, string, string, error) {
	resolvedFiles, resolvedDir, err := promptasset.ResolvePaths(
		baseDir,
		files,
		dir,
	)
	if err != nil {
		return nil, "", "", err
	}
	switch {
	case len(resolvedFiles) > 0:
		createDir := filepath.Dir(resolvedFiles[0])
		sources, err := readPromptSources(resolvedFiles, true)
		return sources, promptBundleModeFiles, createDir, err
	case resolvedDir != "":
		paths, err := promptDirFilePaths(resolvedDir)
		if err != nil {
			return nil, "", "", err
		}
		sources, err := readPromptSources(paths, true)
		return sources, promptBundleModeDir, resolvedDir, err
	case strings.TrimSpace(defaultDir) == "":
		return nil, promptBundleModeNone, "", nil
	case len(defaultFiles) > 0:
		paths := make([]string, 0, len(defaultFiles))
		for _, name := range defaultFiles {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			paths = append(paths, filepath.Join(defaultDir, name))
		}
		sources, err := readPromptSources(paths, true)
		return sources, promptBundleModeDefaultFiles, defaultDir, err
	default:
		paths, err := promptDirFilePaths(defaultDir)
		if err != nil {
			return nil, "", "", err
		}
		sources, err := readPromptSources(paths, true)
		return sources, promptBundleModeDefaultDir, defaultDir, err
	}
}

func readPromptSources(
	paths []string,
	deletable bool,
) ([]promptSourceState, error) {
	promptasset.SortPaths(paths)
	sources := make([]promptSourceState, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, promptSourceState{
			Path:      path,
			Label:     promptSourceLabel(path),
			Content:   string(content),
			Deletable: deletable,
		})
	}
	return sources, nil
}

func promptDirFilePaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	promptasset.SortPaths(paths)
	return paths, nil
}

func promptBundleSummary(key string) string {
	switch {
	case key == runtimePromptBundleAgentInstruction:
		return "Shapes how the assistant reasons and follows durable" +
			" workflow guidance across turns."
	case key == runtimePromptBundleAgentSystem:
		return "Defines runtime identity, coding guardrails, and the" +
			" selected persona."
	case strings.HasPrefix(key, runtimePromptBundleWeComPrefix):
		return "This template injects runtime channel notes before" +
			" each WeCom turn. The exact text varies with the" +
			" chat, persona, workspace, and current user input."
	default:
		return ""
	}
}

func promptConfiguredLabel(key string) string {
	switch {
	case key == runtimePromptBundleAgentInstruction:
		return "Configured Instruction Text"
	case key == runtimePromptBundleAgentSystem:
		return "Configured System Text"
	case strings.HasPrefix(key, runtimePromptBundleWeComPrefix):
		return "Configured Template Text"
	default:
		return "Configured Prompt"
	}
}

func promptEffectiveLabel(key string) string {
	switch {
	case key == runtimePromptBundleAgentInstruction:
		return "Live Instruction Text"
	case key == runtimePromptBundleAgentSystem:
		return "Live System Text"
	case strings.HasPrefix(key, runtimePromptBundleWeComPrefix):
		return "Live Template Structure"
	default:
		return "Live Prompt Text"
	}
}

func promptSourceSummary(
	mode string,
	inlineValue string,
	sourceCount int,
) string {
	hasInline := strings.TrimSpace(inlineValue) != ""
	switch mode {
	case promptBundleModeFiles:
		fileLabel := promptFileCountLabel(sourceCount)
		if hasInline {
			return "Config text plus " + fileLabel
		}
		return fileLabel
	case promptBundleModeDir:
		if hasInline {
			return "Config text plus a directory (" +
				promptFileCountLabel(sourceCount) + ")"
		}
		return "Directory (" +
			promptFileCountLabel(sourceCount) + ")"
	case promptBundleModeDefaultFiles, promptBundleModeDefaultDir:
		if hasInline {
			return "Config text plus built-in files"
		}
		return "Built-in files"
	case promptBundleModeNone:
		if hasInline {
			return "Config text only"
		}
		return "No configured sources"
	default:
		if hasInline {
			return "Config text"
		}
		return ""
	}
}

func promptFileCountLabel(count int) string {
	if count == 1 {
		return "1 file"
	}
	return strconv.Itoa(count) + " files"
}

func promptBundleEffectiveValue(
	key string,
	effective string,
) string {
	effective = strings.TrimSpace(effective)
	if effective == "" {
		return ""
	}
	if !strings.HasPrefix(key, runtimePromptBundleWeComPrefix) {
		return effective
	}
	return wecomchannel.RenderRequestSystemPromptStructure(
		effective,
	)
}

func buildAgentPromptPreview(
	bundles []promptBundleState,
) string {
	parts := make([]string, 0, 2)
	if bundle, ok := findRuntimePromptBundle(
		bundles,
		runtimePromptBundleAgentInstruction,
	); ok && strings.TrimSpace(bundle.Effective) != "" {
		parts = append(parts, formatPromptPreviewBlock(
			bundle.Title,
			bundle.Effective,
		))
	}
	if bundle, ok := findRuntimePromptBundle(
		bundles,
		runtimePromptBundleAgentSystem,
	); ok && strings.TrimSpace(bundle.Effective) != "" {
		parts = append(parts, formatPromptPreviewBlock(
			bundle.Title,
			bundle.Effective,
		))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func findRuntimePromptBundle(
	bundles []promptBundleState,
	key string,
) (promptBundleState, bool) {
	for _, bundle := range bundles {
		if bundle.Key == key {
			return bundle, true
		}
	}
	return promptBundleState{}, false
}

func formatPromptPreviewBlock(
	title string,
	content string,
) string {
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if title == "" || content == "" {
		return content
	}
	return title + "\n" + strings.Repeat("=", len(title)) +
		"\n" + content
}

func promptSourceLabel(path string) string {
	base := filepath.Base(strings.TrimSpace(path))
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = stripPromptNumericPrefix(name)
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return base
	}
	for i := range parts {
		runes := []rune(parts[i])
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func stripPromptNumericPrefix(name string) string {
	name = strings.TrimSpace(name)
	end := 0
	for end < len(name) && name[end] >= '0' && name[end] <= '9' {
		end++
	}
	if end == 0 || end >= len(name) {
		return name
	}
	if name[end] != '_' && name[end] != '-' {
		return name
	}
	return strings.TrimSpace(name[end+1:])
}

func loadWeComEffectiveTemplate(
	configNode *yaml.Node,
	configPath string,
	stateDir string,
) string {
	template, err := loadWeComPromptTemplateForNode(
		configNode,
		configPath,
		stateDir,
	)
	if err != nil {
		return ""
	}
	return template
}

func collectPersonaStoreStates(
	agentPersonaDir string,
	targets []runtimeWeComPromptTarget,
) ([]personaStoreState, error) {
	labelsByDir := map[string][]string{}
	addLabel := func(dir string, label string) {
		dir = strings.TrimSpace(dir)
		label = strings.TrimSpace(label)
		if dir == "" || label == "" {
			return
		}
		if slicesContains(labelsByDir[dir], label) {
			return
		}
		labelsByDir[dir] = append(labelsByDir[dir], label)
	}
	addLabel(agentPersonaDir, personaStoreAgentLabel)
	for i := range targets {
		dir := strings.TrimSpace(targets[i].PersonaDir)
		label := firstNonEmptyString(
			targets[i].Label,
			fmt.Sprintf("WeCom Personas %d", i+1),
		)
		addLabel(dir, label)
	}

	dirs := make([]string, 0, len(labelsByDir))
	for dir := range labelsByDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	stores := make([]personaStoreState, 0, len(dirs))
	for _, dir := range dirs {
		defs, err := personaapi.NewRegistry(dir).List()
		if err != nil {
			return nil, err
		}
		usageLabels := append([]string(nil), labelsByDir[dir]...)
		stores = append(stores, personaStoreState{
			Dir:         dir,
			Title:       personaStoreTitle(usageLabels),
			UsageLabels: usageLabels,
			Definitions: defs,
		})
	}
	return stores, nil
}

func personaStoreTitle(labels []string) string {
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	default:
		return personaStoreSharedTitle
	}
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func findPromptBundle(
	bundles []promptBundleState,
	key string,
) (promptBundleState, bool) {
	for _, bundle := range bundles {
		if bundle.Key == key {
			return bundle, true
		}
	}
	return promptBundleState{}, false
}

func findPromptSource(
	sources []promptSourceState,
	path string,
) (promptSourceState, bool) {
	for _, source := range sources {
		if source.Path == path {
			return source, true
		}
	}
	return promptSourceState{}, false
}

func hasPersonaStore(
	stores []personaStoreState,
	dir string,
) bool {
	dir = strings.TrimSpace(dir)
	for _, store := range stores {
		if strings.TrimSpace(store.Dir) == dir {
			return true
		}
	}
	return false
}

func normalizePromptAdminFileName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("file name is required")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("file name must not include a path")
	}
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	return name, nil
}

func (s *runtimeAdminProvider) resolvePromptBundleConfigBinding(
	rawRoot *yaml.Node,
	bundleKey string,
) (promptBundleConfigBinding, error) {
	doc := documentNode(rawRoot)
	if doc == nil {
		return promptBundleConfigBinding{}, fmt.Errorf(
			"config document is empty",
		)
	}
	switch bundleKey {
	case runtimePromptBundleAgentInstruction:
		node := ensureMappingValue(doc, agentKey)
		if node == nil {
			return promptBundleConfigBinding{}, fmt.Errorf(
				"config: missing agent",
			)
		}
		return promptBundleConfigBinding{
			node:     node,
			filesKey: instructionFilesKey,
			dirKey:   instructionDirKey,
		}, nil
	case runtimePromptBundleAgentSystem:
		node := ensureMappingValue(doc, agentKey)
		if node == nil {
			return promptBundleConfigBinding{}, fmt.Errorf(
				"config: missing agent",
			)
		}
		return promptBundleConfigBinding{
			node:     node,
			filesKey: systemPromptFilesKey,
			dirKey:   systemPromptDirKey,
		}, nil
	default:
		if !strings.HasPrefix(
			bundleKey,
			runtimePromptBundleWeComPrefix,
		) {
			return promptBundleConfigBinding{}, fmt.Errorf(
				"unknown prompt bundle",
			)
		}
		index, err := strconv.Atoi(
			strings.TrimPrefix(
				bundleKey,
				runtimePromptBundleWeComPrefix,
			),
		)
		if err != nil {
			return promptBundleConfigBinding{}, fmt.Errorf(
				"invalid prompt bundle",
			)
		}
		nodes := collectWeComConfigNodes(rawRoot)
		if index < 0 || index >= len(nodes) || nodes[index] == nil {
			return promptBundleConfigBinding{}, fmt.Errorf(
				"unknown prompt bundle",
			)
		}
		return promptBundleConfigBinding{
			node: nodes[index],
			filesKey: wecomchannel.
				RequestSystemPromptFilesConfigKey,
			dirKey: wecomchannel.
				RequestSystemPromptDirConfigKey,
		}, nil
	}
}

func updatePromptBundleSourceConfig(
	rawRoot *yaml.Node,
	configPath string,
	bundle promptBundleState,
	path string,
	add bool,
) error {
	switch bundle.SourceMode {
	case promptBundleModeDir, promptBundleModeDefaultDir:
		return nil
	case promptBundleModeFiles, promptBundleModeDefaultFiles:
	default:
		return nil
	}

	svc := &runtimeAdminProvider{sourceConfigPath: configPath}
	binding, err := svc.resolvePromptBundleConfigBinding(
		rawRoot,
		bundle.Key,
	)
	if err != nil {
		return err
	}

	switch bundle.SourceMode {
	case promptBundleModeDefaultFiles:
		setMappingString(
			binding.node,
			binding.dirKey,
			promptConfigPathValue(configPath, bundle.CreateDir),
		)
		deleteMappingKey(binding.node, binding.filesKey)
		return nil
	case promptBundleModeFiles:
		values := make([]string, 0, len(bundle.Sources)+1)
		target := filepath.Clean(strings.TrimSpace(path))
		for _, source := range bundle.Sources {
			if !add &&
				filepath.Clean(source.Path) == target {
				continue
			}
			values = append(
				values,
				promptConfigPathValue(configPath, source.Path),
			)
		}
		if add {
			values = append(
				values,
				promptConfigPathValue(configPath, path),
			)
		}
		values = normalizeStringSequence(values)
		if len(values) == 0 {
			deleteMappingKey(binding.node, binding.filesKey)
			return nil
		}
		setMappingSequence(binding.node, binding.filesKey, values)
		return nil
	default:
		return nil
	}
}

func promptConfigPathValue(
	configPath string,
	targetPath string,
) string {
	targetPath = filepath.Clean(strings.TrimSpace(targetPath))
	if targetPath == "" {
		return ""
	}
	baseDir := configBaseDir(configPath)
	if strings.TrimSpace(baseDir) == "" {
		return targetPath
	}
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return targetPath
	}
	rel = filepath.ToSlash(rel)
	switch {
	case rel == ".":
		return "./"
	case strings.HasPrefix(rel, "../"):
		return targetPath
	case strings.HasPrefix(rel, "./"):
		return rel
	default:
		return "./" + rel
	}
}

func updateMappingString(
	root *yaml.Node,
	key string,
	value string,
) {
	value = strings.TrimSpace(value)
	if value == "" {
		deleteMappingKey(root, key)
		return
	}
	setMappingString(root, key, value)
}

func writePromptTextFile(path string, content string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("prompt path is empty")
	}
	mode := os.FileMode(promptAdminDefaultPerm)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return writePromptAdminFile(path, []byte(content), mode)
}

func writeYAMLConfigNode(path string, root *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		_ = enc.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	mode := os.FileMode(promptAdminDefaultPerm)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	return writePromptAdminFile(path, buf.Bytes(), mode)
}

func writePromptAdminFile(
	path string,
	data []byte,
	mode os.FileMode,
) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, ".prompt-admin-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = os.Remove(tempPath)
	}
	defer cleanup()

	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}
