package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tlog "git.code.oa.com/trpc-go/trpc-go/log"
	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"gopkg.in/yaml.v3"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const promptReloadInterval = time.Second

type agentPromptController interface {
	SetPrompts(instruction string, systemPrompt string)
}

type requestPromptTemplateSetter interface {
	SetRequestSystemPromptTemplate(template string)
}

type runtimeWeComPromptChannel interface {
	requestPromptTemplateSetter
	PersonaDir() string
}

type runtimeWeComPromptTarget struct {
	Label        string
	PersonaDir   string
	PromptSetter requestPromptTemplateSetter
}

type runtimePromptState struct {
	Instruction    string
	SystemPrompt   string
	WeComTemplates []string
}

type runtimePromptReloader struct {
	sourceConfigPath string
	stateDir         string
	args             []string

	controller  agentPromptController
	wecomTarget []runtimeWeComPromptTarget

	mu            sync.Mutex
	lastState     runtimePromptState
	lastErrorText string
	stopCh        chan struct{}
	doneCh        chan struct{}
}

func collectRuntimeWeComPromptTargets(
	channels []occhannel.Channel,
) []runtimeWeComPromptTarget {
	targets := make([]runtimeWeComPromptTarget, 0, len(channels))
	for _, ch := range channels {
		wecom, ok := ch.(runtimeWeComPromptChannel)
		if !ok || wecom == nil {
			continue
		}
		targets = append(targets, runtimeWeComPromptTarget{
			PersonaDir:   wecom.PersonaDir(),
			PromptSetter: wecom,
		})
	}
	return targets
}

func newRuntimePromptReloader(
	sourceConfigPath string,
	stateDir string,
	args []string,
	controller agentPromptController,
	wecomTargets []runtimeWeComPromptTarget,
) *runtimePromptReloader {
	sourceConfigPath = strings.TrimSpace(sourceConfigPath)
	if sourceConfigPath == "" &&
		controller == nil &&
		len(wecomTargets) == 0 {
		return nil
	}
	return &runtimePromptReloader{
		sourceConfigPath: sourceConfigPath,
		stateDir:         strings.TrimSpace(stateDir),
		args:             append([]string(nil), args...),
		controller:       controller,
		wecomTarget: append(
			[]runtimeWeComPromptTarget(nil),
			wecomTargets...,
		),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		lastState:     runtimePromptState{},
		lastErrorText: "",
	}
}

func (r *runtimePromptReloader) Start() func() {
	if r == nil {
		return func() {}
	}
	go r.loop()
	return func() {
		close(r.stopCh)
		<-r.doneCh
	}
}

func (r *runtimePromptReloader) Reload() error {
	if r == nil {
		return nil
	}
	state, err := loadRuntimePromptState(
		r.sourceConfigPath,
		r.stateDir,
		r.args,
		len(r.wecomTarget),
	)
	if err != nil {
		r.logReloadError(err)
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErrorText = ""
	if promptStatesEqual(r.lastState, state) {
		return nil
	}
	r.applyState(state)
	r.lastState = cloneRuntimePromptState(state)
	return nil
}

func (r *runtimePromptReloader) loop() {
	defer close(r.doneCh)

	if err := r.Reload(); err != nil {
		tlog.Warnf("runtime prompt initial reload failed: %v", err)
	}

	ticker := time.NewTicker(promptReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			if err := r.Reload(); err != nil {
				continue
			}
		}
	}
}

func (r *runtimePromptReloader) applyState(
	state runtimePromptState,
) {
	if r.controller != nil {
		r.controller.SetPrompts(
			state.Instruction,
			state.SystemPrompt,
		)
	}
	for i := range r.wecomTarget {
		if r.wecomTarget[i].PromptSetter == nil {
			continue
		}
		template := ""
		if i < len(state.WeComTemplates) {
			template = state.WeComTemplates[i]
		}
		r.wecomTarget[i].PromptSetter.SetRequestSystemPromptTemplate(
			template,
		)
	}
}

func (r *runtimePromptReloader) logReloadError(err error) {
	if err == nil {
		return
	}
	text := strings.TrimSpace(err.Error())
	r.mu.Lock()
	defer r.mu.Unlock()
	if text == "" || text == r.lastErrorText {
		return
	}
	r.lastErrorText = text
	tlog.Warnf("runtime prompt reload failed: %s", text)
}

func loadRuntimePromptState(
	sourceConfigPath string,
	stateDir string,
	args []string,
	wecomCount int,
) (runtimePromptState, error) {
	sourceConfigPath = strings.TrimSpace(sourceConfigPath)
	if sourceConfigPath == "" {
		return runtimePromptState{}, nil
	}
	_, prepared, err := loadPromptConfigRoots(
		sourceConfigPath,
		args,
		stateDir,
	)
	if err != nil {
		return runtimePromptState{}, err
	}
	instruction, systemPrompt := extractPreparedAgentPrompts(prepared)
	wecomTemplates, err := extractWeComPromptTemplates(
		prepared,
		sourceConfigPath,
		stateDir,
		wecomCount,
	)
	if err != nil {
		return runtimePromptState{}, err
	}
	return runtimePromptState{
		Instruction:    instruction,
		SystemPrompt:   systemPrompt,
		WeComTemplates: wecomTemplates,
	}, nil
}

func loadPromptConfigRoots(
	configPath string,
	args []string,
	stateDir string,
) (*yaml.Node, *yaml.Node, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"read config %q: %w",
			configPath,
			err,
		)
	}

	var rawRoot yaml.Node
	if err := yaml.Unmarshal(data, &rawRoot); err != nil {
		return nil, nil, fmt.Errorf(
			"parse config %q: %w",
			configPath,
			err,
		)
	}

	var preparedRoot yaml.Node
	if err := yaml.Unmarshal(data, &preparedRoot); err != nil {
		return nil, nil, fmt.Errorf(
			"parse config %q: %w",
			configPath,
			err,
		)
	}
	if _, _, _, err := prepareRuntimeConfigRoot(
		&preparedRoot,
		args,
		stateDir,
		configPath,
		runtimeConfigEnvLookup(stateDir),
	); err != nil {
		return nil, nil, err
	}
	return &rawRoot, &preparedRoot, nil
}

func extractPreparedAgentPrompts(root *yaml.Node) (string, string) {
	doc := documentNode(root)
	if doc == nil {
		return "", ""
	}
	agentNode := mappingValue(doc, agentKey)
	if agentNode == nil {
		return "", ""
	}
	return strings.TrimSpace(
			mappingStringValue(agentNode, instructionKey),
		), strings.TrimSpace(
			mappingStringValue(agentNode, systemPromptKey),
		)
}

func extractWeComPromptTemplates(
	root *yaml.Node,
	configPath string,
	stateDir string,
	wecomCount int,
) ([]string, error) {
	if wecomCount <= 0 {
		return nil, nil
	}
	nodes := collectWeComConfigNodes(root)
	templates := make([]string, 0, wecomCount)
	for i := 0; i < wecomCount; i++ {
		var configNode *yaml.Node
		if i < len(nodes) {
			configNode = nodes[i]
		}
		template, err := loadWeComPromptTemplateForNode(
			configNode,
			configPath,
			stateDir,
		)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, nil
}

func collectWeComConfigNodes(root *yaml.Node) []*yaml.Node {
	doc := documentNode(root)
	if doc == nil {
		return nil
	}
	channelsNode := mappingValue(doc, channelsKey)
	if channelsNode == nil || channelsNode.Kind != yaml.SequenceNode {
		return nil
	}
	nodes := make([]*yaml.Node, 0, len(channelsNode.Content))
	for _, entry := range channelsNode.Content {
		if entry == nil || entry.Kind != yaml.MappingNode {
			continue
		}
		if strings.TrimSpace(
			mappingStringValue(entry, channelTypeKey),
		) != wecomchannel.PluginType {
			continue
		}
		nodes = append(nodes, mappingValue(entry, channelConfigKey))
	}
	return nodes
}

func loadWeComPromptTemplateForNode(
	configNode *yaml.Node,
	configPath string,
	stateDir string,
) (string, error) {
	files := yamlSequenceValues(mappingValue(
		configNode,
		wecomchannel.RequestSystemPromptFilesConfigKey,
	))
	dir := mappingStringValue(
		configNode,
		wecomchannel.RequestSystemPromptDirConfigKey,
	)
	resolvedFiles, resolvedDir, err := promptasset.ResolvePaths(
		configBaseDir(configPath),
		files,
		dir,
	)
	if err != nil {
		return "", err
	}
	switch {
	case len(resolvedFiles) > 0 || resolvedDir != "":
		return promptasset.ReadDiskBundle(
			resolvedFiles,
			resolvedDir,
		)
	case strings.TrimSpace(stateDir) != "":
		paths, err := promptasset.EnsureDefaultFiles(stateDir)
		if err != nil {
			return "", err
		}
		return promptasset.ReadDiskBundle(nil, paths.WeComRequestDir)
	default:
		return promptasset.ReadEmbeddedBundle(
			promptasset.DefaultWeComRequestEmbeddedDir,
		)
	}
}

func promptStatesEqual(
	left runtimePromptState,
	right runtimePromptState,
) bool {
	if left.Instruction != right.Instruction ||
		left.SystemPrompt != right.SystemPrompt {
		return false
	}
	if len(left.WeComTemplates) != len(right.WeComTemplates) {
		return false
	}
	for i := range left.WeComTemplates {
		if left.WeComTemplates[i] != right.WeComTemplates[i] {
			return false
		}
	}
	return true
}

func cloneRuntimePromptState(
	state runtimePromptState,
) runtimePromptState {
	out := runtimePromptState{
		Instruction:  state.Instruction,
		SystemPrompt: state.SystemPrompt,
	}
	if len(state.WeComTemplates) > 0 {
		out.WeComTemplates = append(
			[]string(nil),
			state.WeComTemplates...,
		)
	}
	return out
}
