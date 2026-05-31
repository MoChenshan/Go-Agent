package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

type fakeScheduledJobManager struct {
	jobs          []gwclient.ScheduledJobSummary
	clearCnt      int
	clearTarget   string
	updateJobID   string
	updateEnabled bool
	updateTarget  string
	removeJobID   string
	removeOK      bool
	removeTarget  string
}

func (f *fakeScheduledJobManager) SendMessage(
	_ context.Context,
	_ gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	return gwclient.MessageResponse{}, nil
}

func (f *fakeScheduledJobManager) Cancel(
	context.Context,
	string,
) (bool, error) {
	return false, nil
}

func (f *fakeScheduledJobManager) ListScheduledJobs(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) ([]gwclient.ScheduledJobSummary, error) {
	out := make([]gwclient.ScheduledJobSummary, len(f.jobs))
	copy(out, f.jobs)
	return out, nil
}

func (f *fakeScheduledJobManager) ClearScheduledJobs(
	_ context.Context,
	_ string,
	_ string,
	target string,
) (int, error) {
	f.clearTarget = target
	return f.clearCnt, nil
}

func (f *fakeScheduledJobManager) SetScheduledJobEnabled(
	_ context.Context,
	_ string,
	_ string,
	target string,
	jobID string,
	enabled bool,
) (gwclient.ScheduledJobSummary, error) {
	f.updateJobID = jobID
	f.updateEnabled = enabled
	f.updateTarget = target
	for i := range f.jobs {
		if f.jobs[i].ID != jobID {
			continue
		}
		f.jobs[i].Enabled = enabled
		return f.jobs[i], nil
	}
	return gwclient.ScheduledJobSummary{}, nil
}

func (f *fakeScheduledJobManager) RemoveScheduledJob(
	_ context.Context,
	_ string,
	_ string,
	target string,
	jobID string,
) (bool, error) {
	f.removeJobID = jobID
	f.removeTarget = target
	return f.removeOK, nil
}

type failingTemplateCardSender struct {
	mockSender
	sendErr error
}

func (m *failingTemplateCardSender) SendTemplateCard(
	_ context.Context,
	chatID string,
	card *templateCard,
) error {
	m.lastChatID = chatID
	m.lastTemplateCard = card
	return m.sendErr
}

func TestParseCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"/cancel", "/cancel"},
		{"/cron", "/cron"},
		{"/help", "/help"},
		{"/name", "/name"},
		{"/status", "/status"},
		{"/session", "/session"},
		{"/sessions", "/sessions"},
		{"/switch", "/switch"},
		{"/welcome", "/welcome"},
		{"/persona", "/persona"},
		{"/runtime", "/runtime"},
		{"/subagents", "/subagents"},
		{"/workspace", "/workspace"},
		{"/Cancel", "/cancel"},
		{"/CRON", "/cron"},
		{"/HELP", "/help"},
		{"/NAME", "/name"},
		{"/STATUS", "/status"},
		{"/cancel extra args", "/cancel"},
		{"/name 彪子", "/name"},
		{"/sessions 3", "/sessions"},
		{"/switch 2", "/switch"},
		{"/WELCOME", "/welcome"},
		{"/persona concise", "/persona"},
		{"/runtime status", "/runtime"},
		{"/unknown", ""},
		{"hello", ""},
		{"", ""},
		{"  /help  ", "/help"},
		{"@X /help", "/help"},
		{"@机器人   /sessions 3", "/sessions"},
		{"@My Bot /help", "/help"},
		{"@My Bot：/runtime help", "/runtime"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := parseCommand(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseCommandInput(t *testing.T) {
	t.Parallel()

	cmd := parseCommandInput("/persona concise")
	require.Equal(t, personaKeyword, cmd.keyword)
	require.Equal(t, []string{"concise"}, cmd.args)

	cmd = parseCommandInput(
		"/persona save product_partner warm and direct",
	)
	require.Equal(t, personaKeyword, cmd.keyword)
	require.Equal(
		t,
		[]string{"save", "product_partner", "warm", "and", "direct"},
		cmd.args,
	)
	require.Equal(
		t,
		"save product_partner warm and direct",
		cmd.rawArgs,
	)

	cmd = parseCommandInput("@X /cron stop 2")
	require.Equal(t, cronKeyword, cmd.keyword)
	require.Equal(t, []string{"stop", "2"}, cmd.args)
	require.Equal(t, "stop 2", cmd.rawArgs)

	cmd = parseCommandInput("@X /name 彪子")
	require.Equal(t, nameKeyword, cmd.keyword)
	require.Equal(t, []string{"彪子"}, cmd.args)
	require.Equal(t, "彪子", cmd.rawArgs)

	cmd = parseCommandInput("@X /welcome")
	require.Equal(t, welcomeKeyword, cmd.keyword)
	require.Empty(t, cmd.args)
	require.Empty(t, cmd.rawArgs)

	cmd = parseCommandInput("@My Bot /help")
	require.Equal(t, helpKeyword, cmd.keyword)
	require.Empty(t, cmd.args)
	require.Empty(t, cmd.rawArgs)

	cmd = parseCommandInput("@My Bot：/runtime help")
	require.Equal(t, runtimeKeyword, cmd.keyword)
	require.Equal(t, []string{"help"}, cmd.args)
	require.Equal(t, "help", cmd.rawArgs)

	cmd = parseCommandInput("/subagents get run-1")
	require.Equal(t, subagentsKeyword, cmd.keyword)
	require.Equal(t, []string{"get", "run-1"}, cmd.args)
	require.Equal(t, "get run-1", cmd.rawArgs)
}

func TestParseNameCommandScope(t *testing.T) {
	t.Parallel()

	scope, value := parseNameCommandScope("彪子")
	require.Empty(t, scope)
	require.Equal(t, "彪子", value)

	scope, value = parseNameCommandScope("global 彪子")
	require.Equal(t, nameScopeGlobal, scope)
	require.Equal(t, "彪子", value)

	scope, value = parseNameCommandScope("GLOBAL off")
	require.Equal(t, nameScopeGlobal, scope)
	require.Equal(t, "off", value)
}

func TestParsePersonaSaveInput(t *testing.T) {
	t.Parallel()

	name, prompt, err := parsePersonaSaveInput(
		"save 爱心 你是一个有爱心的人",
	)
	require.NoError(t, err)
	require.Equal(t, "爱心", name)
	require.Equal(t, "你是一个有爱心的人", prompt)
}

func TestHandlePersonaResolveCommandCreatesOnMiss(
	t *testing.T,
) {
	t.Parallel()

	baseSessionID := "wecom:dm:user1"
	sender := &mockSender{}
	ch := &Channel{
		sessionTracker: newSessionTracker(),
		personas:       personaapi.NewRegistry(t.TempDir()),
	}

	err := ch.handlePersonaResolveCommand(
		context.Background(),
		"chat1",
		baseSessionID,
		[]string{"一个非常热心肠的人"},
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, "已保存并启用人格")

	info := ch.sessionTracker.getOrCreateSession(baseSessionID, 0)
	require.NotEqual(t, personaapi.DefaultID, info.personaID)
	require.True(t, info.personaPinned)

	def, ok, getErr := ch.lookupPersona(info.personaID)
	require.NoError(t, getErr)
	require.True(t, ok)
	require.Equal(t, "一个非常热心肠的人", def.ID)
	require.Equal(t, "一个非常热心肠的人", def.Name)
	require.Equal(t, "一个非常热心肠的人", def.Prompt)
}

func TestHandleNameCommandPersistsAlias(t *testing.T) {
	t.Parallel()

	baseSessionID := "wecom:dm:user1"
	sender := &mockSender{}
	ch := &Channel{
		sessionTracker: newSessionTracker(),
	}

	err := ch.handleNameCommand(
		context.Background(),
		"chat1",
		baseSessionID,
		"彪子",
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, "彪子")

	info := ch.sessionTracker.getOrCreateSession(baseSessionID, 0)
	require.Equal(t, "彪子", info.assistantAlias)

	err = ch.handleNameCommand(
		context.Background(),
		"chat1",
		baseSessionID,
		"off",
		sender,
	)
	require.NoError(t, err)

	info = ch.sessionTracker.getOrCreateSession(baseSessionID, 0)
	require.Empty(t, info.assistantAlias)
}

func TestHandleNameCommandUpdatesGlobalName(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	identityFile := promptasset.DefaultPaths(stateDir).IdentityFile
	baseSessionID := "wecom:dm:user1"
	sender := &mockSender{}
	ch := &Channel{
		cfg: channelCfg{
			BotName: "LegacyBot",
		},
		sessionTracker:        newSessionTracker(),
		assistantIdentityFile: identityFile,
	}

	err := ch.handleNameCommand(
		context.Background(),
		"chat1",
		baseSessionID,
		"global 阿爪",
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, "已更新默认名字：阿爪")

	name, err := assistantname.ReadFile(identityFile)
	require.NoError(t, err)
	require.Equal(t, "阿爪", name)

	err = ch.handleNameCommand(
		context.Background(),
		"chat1",
		baseSessionID,
		"global off",
		sender,
	)
	require.NoError(t, err)
	require.Contains(
		t,
		sender.lastText,
		"已清除默认名字，当前回退为：LegacyBot",
	)

	name, err = assistantname.ReadFile(identityFile)
	require.NoError(t, err)
	require.Empty(t, name)
}

func TestHandleNameCommandShowsChatAndGlobalState(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	identityFile := promptasset.DefaultPaths(stateDir).IdentityFile
	require.NoError(
		t,
		assistantname.WriteFile(identityFile, "winechord"),
	)

	baseSessionID := "wecom:dm:user1"
	sender := &mockSender{}
	ch := &Channel{
		sessionTracker:        newSessionTracker(),
		assistantIdentityFile: identityFile,
	}
	ch.sessionTracker.setAssistantAlias(baseSessionID, "林妹妹")

	err := ch.handleNameCommand(
		context.Background(),
		"chat1",
		baseSessionID,
		"",
		sender,
	)
	require.NoError(t, err)
	require.Contains(
		t,
		sender.lastText,
		"当前名字：林妹妹（当前聊天名字）",
	)
	require.Contains(
		t,
		sender.lastText,
		"当前聊天名字：林妹妹",
	)
	require.Contains(
		t,
		sender.lastText,
		"默认名字：winechord",
	)
	require.Contains(
		t,
		sender.lastText,
		"规则：",
	)
	require.Contains(
		t,
		sender.lastText,
		"当前聊天名字优先，默认名字兜底",
	)
	require.Contains(
		t,
		sender.lastText,
		"例子：",
	)
	require.Contains(
		t,
		sender.lastText,
		"其他用户新开一个私聊",
	)
	require.Contains(
		t,
		sender.lastText,
		"其他群、其他私聊如果已经有自己的当前聊天名字",
	)
	require.Contains(
		t,
		sender.lastText,
		"/name global [称呼|off]",
	)
}

func TestSetAssistantNameToolUpdatesSessionAndGlobal(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	toolImpl := setAssistantNameTool{stateDir: stateDir}
	invocation := agent.NewInvocation(
		agent.WithInvocationSession(
			session.NewSession(
				"openclaw",
				"user1",
				"wecom:dm:user1",
			),
		),
	)
	ctx := agent.NewInvocationContext(
		context.Background(),
		invocation,
	)

	out, err := toolImpl.Call(
		ctx,
		[]byte(`{"scope":"session","value":"阿爪"}`),
	)
	require.NoError(t, err)
	sessionResult, ok := out.(setAssistantNameResult)
	require.True(t, ok)
	require.Equal(t, assistantNameScopeSession, sessionResult.Scope)
	require.Equal(t, "阿爪", sessionResult.Configured)
	require.Equal(t, "阿爪", sessionResult.EffectiveName)
	require.False(t, sessionResult.Cleared)

	tracker := sharedSessionTrackerWithPath(
		sessionTrackerStorePath(stateDir),
	)
	info := tracker.getSession("wecom:dm:user1")
	require.NotNil(t, info)
	require.Equal(t, "阿爪", info.assistantAlias)

	out, err = toolImpl.Call(
		context.Background(),
		[]byte(`{"scope":"global","value":"全局名字"}`),
	)
	require.NoError(t, err)
	globalResult, ok := out.(setAssistantNameResult)
	require.True(t, ok)
	require.Equal(t, assistantNameScopeGlobal, globalResult.Scope)
	require.Equal(t, "全局名字", globalResult.Configured)
	require.Equal(t, "全局名字", globalResult.EffectiveName)
	require.False(t, globalResult.Cleared)

	name, err := assistantname.ReadFile(
		promptasset.DefaultPaths(stateDir).IdentityFile,
	)
	require.NoError(t, err)
	require.Equal(t, "全局名字", name)
}

func TestSetAssistantNameToolCanonicalizesThreadSessionID(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	toolImpl := setAssistantNameTool{stateDir: stateDir}
	invocation := agent.NewInvocation(
		agent.WithInvocationSession(
			session.NewSession(
				"openclaw",
				"user1",
				"wecom:thread:wecom:chat:group1",
			),
		),
	)
	ctx := agent.NewInvocationContext(
		context.Background(),
		invocation,
	)

	out, err := toolImpl.Call(
		ctx,
		[]byte(`{"scope":"session","value":"奥特曼"}`),
	)
	require.NoError(t, err)

	result, ok := out.(setAssistantNameResult)
	require.True(t, ok)
	require.Equal(t, "奥特曼", result.EffectiveName)

	tracker := sharedSessionTrackerWithPath(
		sessionTrackerStorePath(stateDir),
	)
	info := tracker.getSession("wecom:chat:group1")
	require.NotNil(t, info)
	require.Equal(t, "奥特曼", info.assistantAlias)

	_, exists := tracker.sessions["wecom:thread:wecom:chat:group1"]
	require.False(t, exists)
}

func TestSessionTrackerReadsLegacyThreadAssistantAlias(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	storePath := sessionTrackerStorePath(stateDir)
	require.NoError(
		t,
		os.MkdirAll(
			filepath.Dir(storePath),
			sessionTrackerStoreDirPerm,
		),
	)
	raw := []byte(`{
  "version": 8,
  "sessions": {
    "wecom:chat:group1": {
      "session_id": "wecom:chat:group1:123",
      "last_activity_unix": 123
    },
    "wecom:thread:wecom:chat:group1": {
      "session_id": "wecom:thread:wecom:chat:group1",
      "assistant_alias": "奥特曼",
      "last_activity_unix": 124
    }
  }
}
`)
	require.NoError(
		t,
		os.WriteFile(
			storePath,
			raw,
			sessionTrackerStoreFilePerm,
		),
	)

	tracker := newSessionTrackerWithPath(storePath)
	info := tracker.getOrCreateSession("wecom:chat:group1", 0)
	require.NotNil(t, info)
	require.Equal(t, "奥特曼", info.assistantAlias)

	_, exists := tracker.sessions["wecom:thread:wecom:chat:group1"]
	require.False(t, exists)
}

func TestHandlePersonaUseCommandKeepsStrictLookup(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		sessionTracker: newSessionTracker(),
		personas:       personaapi.NewRegistry(t.TempDir()),
	}

	err := ch.handlePersonaUseCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		[]string{"一个非常热心肠的人"},
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, "未知人格")
}

func TestHandleHelpCommandSendsFullHelpText(t *testing.T) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		cfg:            channelCfg{BotName: "Streambot2"},
		sessionTracker: newSessionTracker(),
		helpMessage:    defaultHelpMessage,
	}

	err := ch.handleHelpCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		nil,
		sender,
	)
	require.NoError(t, err)
	require.NotNil(t, sender.lastTemplateCard)
	require.Contains(
		t,
		sender.lastTemplateCard.MainTitle.Title,
		controlCardTitleHelp,
	)
	require.Contains(
		t,
		sender.lastTemplateCard.SubTitleText,
		runtimeKeyword,
	)
	require.Contains(
		t,
		sender.lastTemplateCard.SubTitleText,
		"第 1/3 页",
	)
	require.Len(t, sender.lastTemplateCard.ButtonList, 6)
	require.Equal(
		t,
		controlCardEventRuntime,
		sender.lastTemplateCard.ButtonList[3].Key,
	)
	require.Equal(
		t,
		controlHelpNextEvent(
			controlHelpNextPage(controlHelpPageDefault),
		),
		sender.lastTemplateCard.ButtonList[5].Key,
	)
}

func TestControlHelpPageNavigationWraps(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		controlHelpPageSessions,
		controlHelpPrevPage(controlHelpPageDefault),
	)
	require.Equal(
		t,
		controlHelpPageCommands,
		controlHelpNextPage(controlHelpPageDefault),
	)
	require.Equal(
		t,
		controlHelpPageDefault,
		controlHelpPrevPage(controlHelpPageCommands),
	)
	require.Equal(
		t,
		controlHelpPageSessions,
		controlHelpNextPage(controlHelpPageCommands),
	)
	require.Equal(
		t,
		controlHelpPageCommands,
		controlHelpPrevPage(controlHelpPageSessions),
	)
	require.Equal(
		t,
		controlHelpPageDefault,
		controlHelpNextPage(controlHelpPageSessions),
	)
	page, ok := parseControlHelpPageEvent(
		controlHelpPrevEvent(
			controlHelpPrevPage(controlHelpPageSessions),
		),
	)
	require.True(t, ok)
	require.Equal(t, controlHelpPageCommands, page)
	page, ok = parseControlHelpPageEvent(
		controlHelpNextEvent(
			controlHelpNextPage(controlHelpPageCommands),
		),
	)
	require.True(t, ok)
	require.Equal(t, controlHelpPageSessions, page)
	require.NotEqual(
		t,
		controlHelpPrevEvent(controlHelpPageSessions),
		controlHelpNextEvent(controlHelpPageSessions),
	)
}

func TestBuildControlHelpCardPagedButtons(t *testing.T) {
	t.Parallel()

	card := buildControlHelpCard(
		"TestBot",
		"task-1",
		controlHelpPageCommands,
	)
	require.Contains(
		t,
		card.SubTitleText,
		sessionsKeyword,
	)
	require.Contains(
		t,
		card.SubTitleText,
		cronKeyword+" list",
	)
	require.Contains(t, card.SubTitleText, "第 2/3 页")
	require.Equal(t, controlCardEventHome, card.ButtonList[0].Key)
	require.Equal(t, controlCardEventSessions, card.ButtonList[1].Key)
	require.Equal(t, controlCardEventCron, card.ButtonList[2].Key)
	require.Equal(t, controlCardEventWorkspace, card.ButtonList[3].Key)
	require.Equal(
		t,
		controlHelpPrevEvent(
			controlHelpPrevPage(controlHelpPageCommands),
		),
		card.ButtonList[4].Key,
	)
	require.Equal(
		t,
		controlHelpNextEvent(
			controlHelpNextPage(controlHelpPageCommands),
		),
		card.ButtonList[5].Key,
	)
}

func TestHandleControlHelpPageEvent(t *testing.T) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		sender:         sender,
		sessionTracker: newSessionTracker(),
	}

	err := ch.handleControlTemplateCardEvent(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			From: FromInfo{
				UserID: "user1",
			},
		},
		&TemplateCardEvent{
			EventKey: controlHelpPageEvent(controlHelpPageSessions),
			TaskID:   "help-task-1",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, sender.lastUpdatedCard)
	require.Contains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		newKeyword,
	)
	require.Contains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		"第 3/3 页",
	)
	require.Equal(
		t,
		controlCardEventSessionNew,
		sender.lastUpdatedCard.ButtonList[1].Key,
	)
	require.Equal(
		t,
		controlCardEventSessionRecall,
		sender.lastUpdatedCard.ButtonList[2].Key,
	)
}

func TestHandleHelpCommandAllSendsFullText(t *testing.T) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		cfg:            channelCfg{BotName: "Streambot2"},
		sessionTracker: newSessionTracker(),
		helpMessage:    defaultHelpMessage,
	}

	err := ch.handleHelpCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		[]string{helpArgAll},
		sender,
	)
	require.NoError(t, err)
	require.Nil(t, sender.lastTemplateCard)
	require.Contains(t, sender.lastText, helpSectionCommon)
	require.Contains(t, sender.lastText, cronKeyword+" list")
}

func TestHandleHelpCommandTopicSendsDetailedText(t *testing.T) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		cfg:            channelCfg{BotName: "Streambot2"},
		sessionTracker: newSessionTracker(),
		helpMessage:    defaultHelpMessage,
	}

	err := ch.handleHelpCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		[]string{"runtime"},
		sender,
	)
	require.NoError(t, err)
	require.Nil(t, sender.lastTemplateCard)
	require.Contains(t, sender.lastText, runtimeKeyword+" 运行时控制")
	require.Contains(
		t,
		sender.lastText,
		runtimeKeyword+" "+runtimeActionUpgrade,
	)
	require.Contains(
		t,
		sender.lastText,
		runtimectl.DefaultMinTargetVersion,
	)
	require.Contains(
		t,
		sender.lastText,
		runtimeKeyword+" "+runtimeActionBundle,
	)
	require.Contains(
		t,
		sender.lastText,
		runtimeKeyword+" "+runtimeActionBundle+
			" "+runtimeActionFull,
	)
	require.Contains(t, sender.lastText, "优先先发这个")
}

func TestHandleHelpCommandUnknownTopic(t *testing.T) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		cfg:            channelCfg{BotName: "Streambot2"},
		sessionTracker: newSessionTracker(),
		helpMessage:    defaultHelpMessage,
	}

	err := ch.handleHelpCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		[]string{"unknown"},
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, helpTopicUnknownPrefix)
	require.Contains(t, sender.lastText, "runtime")
}

func TestParseCommandHelpAlias(t *testing.T) {
	t.Parallel()

	topic, ok := parseCommandHelpAlias(
		parseCommandInput("/runtime help"),
	)
	require.True(t, ok)
	require.Equal(t, runtimeKeyword, topic.canonical)

	_, ok = parseCommandHelpAlias(
		parseCommandInput("/persona help extra"),
	)
	require.False(t, ok)
}

func TestHandleRuntimeCommandStatusSendsTemplateCard(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		sender:         sender,
		sessionTracker: newSessionTracker(),
		runtimeLifecycle: runtimectl.NewManager(
			runtimectl.Options{
				CurrentVersion: "v0.0.48",
			},
		),
	}

	err := ch.handleRuntimeCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		"user1",
		"",
		parseCommandInput("/runtime status"),
		sender,
	)
	require.NoError(t, err)
	require.NotNil(t, sender.lastTemplateCard)
	require.Contains(
		t,
		sender.lastTemplateCard.MainTitle.Title,
		controlCardTitleRuntime,
	)
	require.Contains(
		t,
		sender.lastTemplateCard.SubTitleText,
		"v0.0.48",
	)
}

func TestHandleRuntimeCommandUpgradeLatest(
	t *testing.T,
) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/latest/VERSION":
				_, _ = w.Write([]byte("v0.0.48"))
			case "/releases/v0.0.48/CHANGELOG.md":
				_, _ = w.Write([]byte(
					"## v0.0.48 (2026-03-30)\n- runtime card\n",
				))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	sender := &mockSender{}
	ch := &Channel{
		sender:         sender,
		sessionTracker: newSessionTracker(),
		runtimeLifecycle: runtimectl.NewManager(
			runtimectl.Options{
				CurrentVersion: "v0.0.47",
				ReleaseBaseURL: server.URL,
				HTTPClient:     server.Client(),
			},
		),
		runtimeAdminPolicy: runtimeAdminPolicyAllowlist,
		runtimeAdminUsers: buildAllowSet([]string{
			"user1",
		}),
	}

	err := ch.handleRuntimeCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		"user1",
		"",
		parseCommandInput("/runtime upgrade"),
		sender,
	)
	require.NoError(t, err)
	require.NotNil(t, sender.lastTemplateCard)
	require.Contains(
		t,
		sender.lastTemplateCard.SubTitleText,
		"v0.0.48",
	)
	require.Contains(
		t,
		sender.lastTemplateCard.SubTitleText,
		"runtime card",
	)
}

func TestHandleRuntimeCommandUpgradePreview(
	t *testing.T,
) {
	t.Parallel()

	const previewVersion = "v0.0.91-preview.1"

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/preview/VERSION":
				_, _ = w.Write([]byte(previewVersion))
			case "/releases/" + previewVersion + "/CHANGELOG.md":
				_, _ = w.Write([]byte(
					"## " + previewVersion + " (2026-04-30)\n" +
						"- preview runtime\n",
				))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	sender := &mockSender{}
	manager := runtimectl.NewManager(
		runtimectl.Options{
			CurrentVersion: "v0.0.90",
			ReleaseBaseURL: server.URL,
			HTTPClient:     server.Client(),
		},
	)
	ch := &Channel{
		sender:             sender,
		sessionTracker:     newSessionTracker(),
		runtimeLifecycle:   manager,
		runtimeAdminPolicy: runtimeAdminPolicyAllowlist,
		runtimeAdminUsers: buildAllowSet([]string{
			"user1",
		}),
	}

	err := ch.handleRuntimeCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		"user1",
		"",
		parseCommandInput("/runtime upgrade preview force"),
		sender,
	)
	require.NoError(t, err)
	require.NotNil(t, sender.lastTemplateCard)
	status := manager.Status()
	require.NotNil(t, status.Pending)
	require.Equal(t, runtimectl.ModeForce, status.Pending.Mode)
	require.Equal(t, previewVersion, status.Pending.TargetVersion)
	require.Equal(
		t,
		releaseinfo.ChannelPreview,
		status.Pending.TargetChannel,
	)
	require.Contains(
		t,
		sender.lastTemplateCard.SubTitleText,
		previewVersion,
	)
	require.Contains(
		t,
		sender.lastTemplateCard.SubTitleText,
		"preview runtime",
	)
}

func TestFormatRuntimeVersionsIncludesNotes(t *testing.T) {
	t.Parallel()

	text := formatRuntimeVersions(releaseinfo.Index{
		LatestVersion: "v0.0.52",
		Versions: []releaseinfo.Entry{
			{
				Version: "v0.0.52",
				Notes: []string{
					"one",
					"two",
				},
			},
			{
				Version: "v0.0.51",
			},
		},
		MinSupportedTarget: runtimectl.DefaultMinTargetVersion,
	})

	require.Contains(t, text, "- v0.0.52 (latest)")
	require.Contains(t, text, "  - one")
	require.Contains(t, text, "  - two")
	require.Contains(
		t,
		text,
		"指定版本最小要求："+runtimectl.DefaultMinTargetVersion,
	)
}

func TestHandleRuntimeCommandVersionsSplitsLongReply(
	t *testing.T,
) {
	t.Parallel()

	longNote := strings.Repeat("版", maxReplyRunes/2)
	index := releaseinfo.Index{
		LatestVersion: "v0.0.72",
		Versions: []releaseinfo.Entry{
			{
				Version: "v0.0.72",
				Notes: []string{
					longNote,
				},
			},
			{
				Version: "v0.0.71",
				Notes: []string{
					longNote,
				},
			},
			{
				Version: "v0.0.70",
				Notes: []string{
					longNote,
				},
			},
		},
		MinSupportedTarget: runtimectl.DefaultMinTargetVersion,
	}
	indexJSON, err := json.Marshal(index)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/latest/releases.json" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(indexJSON)
		},
	))
	defer server.Close()

	sender := &mockSender{keepPrefix: true}
	ch := &Channel{
		sender:         sender,
		sessionTracker: newSessionTracker(),
		runtimeLifecycle: runtimectl.NewManager(
			runtimectl.Options{
				CurrentVersion: "v0.0.71",
				ReleaseBaseURL: server.URL,
				HTTPClient:     server.Client(),
			},
		),
	}

	err = ch.handleRuntimeCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		"user1",
		"",
		parseCommandInput("/runtime versions"),
		sender,
	)
	require.NoError(t, err)

	require.Greater(t, len(sender.textCalls), 1)

	reconstructed := sender.textCalls[0]
	for _, part := range sender.textCalls[1:] {
		reconstructed += strings.TrimPrefix(
			part,
			continuedReplyPrefix,
		)
	}
	require.Equal(t, formatRuntimeVersions(index), reconstructed)
}

func TestHandleRuntimeCommandChangelogLatest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/latest/VERSION":
				_, _ = w.Write([]byte("v0.0.52"))
			case "/releases/v0.0.52/CHANGELOG.md":
				_, _ = w.Write([]byte(
					"## v0.0.52 (2026-03-30)\n- one\n- two\n",
				))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	sender := &mockSender{}
	ch := &Channel{
		sender:         sender,
		sessionTracker: newSessionTracker(),
		runtimeLifecycle: runtimectl.NewManager(
			runtimectl.Options{
				CurrentVersion: "v0.0.51",
				ReleaseBaseURL: server.URL,
				HTTPClient:     server.Client(),
			},
		),
	}

	err := ch.handleRuntimeCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		"user1",
		"",
		parseCommandInput("/runtime changelog"),
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, runtimeLatestLabel)
	require.Contains(t, sender.lastText, "- one")
	require.Contains(t, sender.lastText, "- two")
}

func TestBuildControlHomeCardIncludesRuntimeActionMenu(
	t *testing.T,
) {
	t.Parallel()

	card := buildControlHomeCard(
		"TestBot",
		"default",
		"/repo",
		"gpt-5.2",
		"task-1",
	)
	require.NotNil(t, card.ActionMenu)
	require.NotEmpty(t, card.ActionMenu.ActionList)
	require.Equal(
		t,
		controlCardEventRuntime,
		card.ActionMenu.ActionList[0].Key,
	)
}

func TestHandleControlRuntimeForcePrompt(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		sender:         sender,
		sessionTracker: newSessionTracker(),
		runtimeLifecycle: runtimectl.NewManager(
			runtimectl.Options{
				CurrentVersion: "v0.0.48",
			},
		),
	}

	err := ch.handleControlTemplateCardEvent(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			From: FromInfo{
				UserID: "user1",
			},
		},
		&TemplateCardEvent{
			EventKey: controlCardEventRuntimeForceRestartPrompt,
			TaskID:   "runtime-task-1",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, sender.lastUpdatedCard)
	require.Contains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		"确认强制重启",
	)
}

func TestBuildDefaultHelpMessageIncludesCoreCommands(t *testing.T) {
	t.Parallel()

	help := buildDefaultHelpMessage()

	require.Contains(t, help, helpKeyword+" "+helpDescHelp)
	require.Contains(t, help, welcomeKeyword+" "+helpDescWelcome)
	require.Contains(t, help, statusKeyword+" "+helpDescStatus)
	require.Contains(t, help, cronKeyword+" "+helpDescCron)
	require.Contains(t, help, runtimeKeyword+" "+helpDescRuntime)
	require.Contains(t, help, subagentsKeyword+" "+helpDescSubagents)
	require.Contains(t, help, newKeyword+" "+helpDescNew)
	require.Contains(t, help, cancelKeyword+" "+helpDescCancel)
	require.Contains(
		t,
		help,
		workspaceKeyword+" "+helpDescWorkspace,
	)
	require.Contains(
		t,
		help,
		personaKeyword+" "+helpDescPersonaSave,
	)
}

func TestHandleWelcomeCommandSendsTemplateCard(t *testing.T) {
	t.Parallel()

	sender := &mockSender{}
	ch := &Channel{
		cfg: channelCfg{
			BotName: "TestBot",
		},
		sessionTracker:   newSessionTracker(),
		runtimeModelName: "gpt-5.2",
	}

	err := ch.handleWelcomeCommand(
		context.Background(),
		"chat1",
		"wecom:dm:user1",
		sender,
	)
	require.NoError(t, err)
	require.NotNil(t, sender.lastTemplateCard)
	require.Equal(
		t,
		"TestBot · "+controlCardTitleHome,
		sender.lastTemplateCard.MainTitle.Title,
	)
}

func TestSyncActiveSessionCardRefreshesStatefulViews(
	t *testing.T,
) {
	t.Parallel()

	type syncTestCase struct {
		name   string
		view   string
		mutate func(
			t *testing.T,
			ch *Channel,
			baseID string,
		) *sessionInfo
		assert func(
			t *testing.T,
			card *templateCard,
			info *sessionInfo,
		)
	}

	tests := []syncTestCase{
		{
			name: "home",
			view: sessionCardViewHome,
			mutate: func(
				_ *testing.T,
				ch *Channel,
				baseID string,
			) *sessionInfo {
				return ch.sessionTracker.setPersona(
					baseID,
					personaapi.SnarkyID,
				)
			},
			assert: func(
				t *testing.T,
				card *templateCard,
				_ *sessionInfo,
			) {
				require.Equal(
					t,
					"TestBot · "+controlCardTitleHome,
					card.MainTitle.Title,
				)
				require.Contains(
					t,
					card.SubTitleText,
					"🎭 人格：毒舌",
				)
			},
		},
		{
			name: "persona",
			view: sessionCardViewPersona,
			mutate: func(
				_ *testing.T,
				ch *Channel,
				baseID string,
			) *sessionInfo {
				return ch.sessionTracker.setPersona(
					baseID,
					personaapi.SnarkyID,
				)
			},
			assert: func(
				t *testing.T,
				card *templateCard,
				_ *sessionInfo,
			) {
				require.Contains(
					t,
					card.MainTitle.Desc,
					"当前人格：毒舌",
				)
			},
		},
		{
			name: "status",
			view: sessionCardViewStatus,
			mutate: func(
				t *testing.T,
				ch *Channel,
				baseID string,
			) *sessionInfo {
				workspace := filepath.Join(
					t.TempDir(),
					"repo",
				)
				return ch.sessionTracker.setWorkspace(
					baseID,
					workspace,
				)
			},
			assert: func(
				t *testing.T,
				card *templateCard,
				info *sessionInfo,
			) {
				require.Equal(
					t,
					"TestBot · "+controlCardTitleStatus,
					card.MainTitle.Title,
				)
				require.Contains(
					t,
					card.SubTitleText,
					info.workspacePath,
				)
			},
		},
		{
			name: "sessions",
			view: sessionCardViewSessions,
			mutate: func(
				_ *testing.T,
				ch *Channel,
				baseID string,
			) *sessionInfo {
				return ch.sessionTracker.startNewSession(
					baseID,
				)
			},
			assert: func(
				t *testing.T,
				card *templateCard,
				info *sessionInfo,
			) {
				require.NotNil(t, card.ButtonSelection)
				require.Equal(
					t,
					info.sessionID,
					card.ButtonSelection.SelectedID,
				)
			},
		},
		{
			name: "workspace",
			view: sessionCardViewWorkspace,
			mutate: func(
				t *testing.T,
				ch *Channel,
				baseID string,
			) *sessionInfo {
				workspace := filepath.Join(
					t.TempDir(),
					"repo",
				)
				return ch.sessionTracker.setWorkspace(
					baseID,
					workspace,
				)
			},
			assert: func(
				t *testing.T,
				card *templateCard,
				info *sessionInfo,
			) {
				require.Equal(
					t,
					"TestBot · "+controlCardTitleWorkspace,
					card.MainTitle.Title,
				)
				require.Contains(
					t,
					card.SubTitleText,
					info.workspacePath,
				)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sender := &mockSender{}
			ch := mustCreateChannel(t)
			baseID := "wecom:dm:user1"

			ch.rememberSessionCard(
				baseID,
				tt.view,
				"task-1",
				ch.sessionTracker.getOrCreateSession(
					baseID,
					0,
				),
			)

			info := tt.mutate(t, ch, baseID)
			ch.syncActiveSessionCard(
				context.Background(),
				baseID,
				info,
				sender,
			)

			require.NotNil(t, sender.lastUpdatedCard)
			require.Equal(
				t,
				"task-1",
				sender.lastUpdatedCard.TaskID,
			)
			tt.assert(t, sender.lastUpdatedCard, info)
		})
	}
}

func TestSyncActiveSessionCardKeepsPersonaCardVariant(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockSender{}
	ch := mustCreateChannel(t)
	baseID := "wecom:dm:user1"
	info := ch.sessionTracker.getOrCreateSession(baseID, 0)

	err := ch.sendPersonaCard(
		context.Background(),
		"chat1",
		baseID,
		info,
		personaCardViewSaveHelp,
		sender,
	)
	require.NoError(t, err)

	info = ch.sessionTracker.setPersona(
		baseID,
		personaapi.SnarkyID,
	)
	ch.syncActiveSessionCard(
		context.Background(),
		baseID,
		info,
		sender,
	)

	require.NotNil(t, sender.lastUpdatedCard)
	require.Contains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		personaKeyword+" "+personaExamplePrompt,
	)
	require.NotContains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		personaCardQuickHint,
	)
}

func TestCardSendFailureDoesNotRememberActiveCard(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(
			t *testing.T,
			ch *Channel,
			sender messageSender,
		) error
	}{
		{
			name: "sessions",
			run: func(
				_ *testing.T,
				ch *Channel,
				sender messageSender,
			) error {
				return ch.handleSessionsCommand(
					context.Background(),
					"chat1",
					"wecom:dm:user1",
					nil,
					sender,
				)
			},
		},
		{
			name: "workspace",
			run: func(
				_ *testing.T,
				ch *Channel,
				sender messageSender,
			) error {
				return ch.handleWorkspaceCommand(
					context.Background(),
					"chat1",
					"wecom:dm:user1",
					nil,
					sender,
				)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sender := &failingTemplateCardSender{
				sendErr: errors.New("send failed"),
			}
			ch := mustCreateChannel(t)

			err := tt.run(t, ch, sender)
			require.NoError(t, err)

			_, ok := ch.sessionCards.activeCard(
				"wecom:dm:user1",
			)
			require.False(t, ok)
		})
	}
}

func TestHandleMessagePersonaCommandRefreshesActiveHomeCard(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockSender{}
	ch := mustCreateChannel(t)
	ch.sender = sender

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: welcomeKeyword,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, sender.lastTemplateCard)

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: personaKeyword +
				" " + personaapi.SnarkyID,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, sender.lastUpdatedCard)
	require.Equal(
		t,
		sender.lastTemplateCard.TaskID,
		sender.lastUpdatedCard.TaskID,
	)
	require.Contains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		"🎭 人格：毒舌",
	)
}

func TestHandleMessagePersonaCommandRefreshesActivePersonaCard(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockSender{}
	ch := mustCreateChannel(t)
	ch.sender = sender

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: personaKeyword,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, sender.lastTemplateCard)

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: personaKeyword +
				" " + personaapi.SnarkyID,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, sender.lastUpdatedCard)
	require.Equal(
		t,
		sender.lastTemplateCard.TaskID,
		sender.lastUpdatedCard.TaskID,
	)
	require.Contains(
		t,
		sender.lastUpdatedCard.MainTitle.Desc,
		"当前人格：毒舌",
	)
}

func TestHandleCancelNoInflight(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		gw:                stubGateway{},
		sender:            ms,
		inflight:          newInflightRequests(),
		cancelNoopMessage: defaultCancelNoopMessage,
	}

	err := ch.handleCancelCommand(context.Background(), "chat1", "session1", ms)
	require.NoError(t, err)
	require.Equal(t, defaultCancelNoopMessage, ms.lastText)
}

func TestHandleCancelSuccess(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	gw := &cancelGateway{cancelResult: true}
	ch := &Channel{
		gw:              gw,
		sender:          ms,
		inflight:        newInflightRequests(),
		cancelOKMessage: defaultCancelOKMessage,
	}
	ch.inflight.Set("session1", "req1")

	err := ch.handleCancelCommand(context.Background(), "chat1", "session1", ms)
	require.NoError(t, err)
	require.Equal(t, defaultCancelOKMessage, ms.lastText)
	require.Equal(t, "req1", gw.lastRequestID)
}

func TestHandleCancelNotCanceled(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	gw := &cancelGateway{cancelResult: false}
	ch := &Channel{
		gw:                gw,
		sender:            ms,
		inflight:          newInflightRequests(),
		cancelNoopMessage: defaultCancelNoopMessage,
	}
	ch.inflight.Set("session1", "req1")

	err := ch.handleCancelCommand(context.Background(), "chat1", "session1", ms)
	require.NoError(t, err)
	require.Equal(t, defaultCancelNoopMessage, ms.lastText)
}

func TestHandleCancelError(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	gw := &cancelGateway{cancelErr: fmt.Errorf("cancel failed")}
	ch := &Channel{
		gw:                  gw,
		sender:              ms,
		inflight:            newInflightRequests(),
		cancelFailedMessage: defaultCancelFailedMessage,
	}
	ch.inflight.Set("session1", "req1")

	err := ch.handleCancelCommand(context.Background(), "chat1", "session1", ms)
	require.NoError(t, err)
	require.Equal(t, defaultCancelFailedMessage, ms.lastText)
}

func TestHandleCronCommandList(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 25, 20, 30, 0, 0, time.UTC)
	manager := &fakeScheduledJobManager{
		jobs: []gwclient.ScheduledJobSummary{
			{
				ID:         "job-1",
				Name:       "memory report",
				Enabled:    true,
				Schedule:   "every 10s",
				NextRunAt:  &now,
				LastStatus: "running",
			},
		},
	}
	ms := &mockSender{}
	ch := &Channel{gw: manager}

	err := ch.handleCronCommand(
		context.Background(),
		"chat1",
		"wecom:chat:chat1",
		"user1",
		parseCommandInput("/cron list"),
		ms,
	)
	require.NoError(t, err)
	require.NotNil(t, ms.lastTemplateCard)
	require.Contains(
		t,
		ms.lastTemplateCard.MainTitle.Title,
		controlCardTitleCron,
	)
	require.Contains(t, ms.lastTemplateCard.SubTitleText, "memory report")
}

func TestHandleCronCommandStop(t *testing.T) {
	t.Parallel()

	manager := &fakeScheduledJobManager{
		jobs: []gwclient.ScheduledJobSummary{
			{
				ID:             "job-1",
				Name:           "memory report",
				Enabled:        true,
				DeliveryTarget: "group:chat1?mentions=user2",
			},
		},
	}
	ms := &mockSender{}
	ch := &Channel{gw: manager}

	err := ch.handleCronCommand(
		context.Background(),
		"chat1",
		"wecom:chat:chat1",
		"user1",
		parseCommandInput("/cron stop 1"),
		ms,
	)
	require.NoError(t, err)
	require.Equal(t, "job-1", manager.updateJobID)
	require.False(t, manager.updateEnabled)
	require.Equal(
		t,
		"group:chat1?mentions=user2",
		manager.updateTarget,
	)
	require.Contains(t, ms.lastText, "已停止定时任务")
}

func TestHandleCronCommandRemove(t *testing.T) {
	t.Parallel()

	manager := &fakeScheduledJobManager{
		removeOK: true,
		jobs: []gwclient.ScheduledJobSummary{
			{
				ID:             "job-1",
				Name:           "memory report",
				DeliveryTarget: "group:chat1?mentions=user2",
			},
		},
	}
	ms := &mockSender{}
	ch := &Channel{gw: manager}

	err := ch.handleCronCommand(
		context.Background(),
		"chat1",
		"wecom:chat:chat1",
		"user1",
		parseCommandInput("/cron remove 1"),
		ms,
	)
	require.NoError(t, err)
	require.Equal(t, "job-1", manager.removeJobID)
	require.Equal(
		t,
		"group:chat1?mentions=user2",
		manager.removeTarget,
	)
	require.Contains(t, ms.lastText, "已删除定时任务")
}

func TestHandleCronCommandResumeBlockedAtMaxRuns(
	t *testing.T,
) {
	t.Parallel()

	manager := &fakeScheduledJobManager{
		jobs: []gwclient.ScheduledJobSummary{
			{
				ID:       "job-1",
				Name:     "memory report",
				Enabled:  false,
				RunCount: 5,
				MaxRuns:  5,
			},
		},
	}
	ms := &mockSender{}
	ch := &Channel{gw: manager}

	err := ch.handleCronCommand(
		context.Background(),
		"chat1",
		"wecom:chat:chat1",
		"user1",
		parseCommandInput("/cron resume 1"),
		ms,
	)
	require.NoError(t, err)
	require.Empty(t, manager.updateJobID)
	require.Contains(
		t,
		ms.lastText,
		"该任务已达到最大执行次数（5/5）",
	)
}

func TestHandleControlCronEventUsesJobDeliveryTarget(
	t *testing.T,
) {
	t.Parallel()

	manager := &fakeScheduledJobManager{
		jobs: []gwclient.ScheduledJobSummary{
			{
				ID:             "job-1",
				Name:           "memory report",
				Enabled:        true,
				DeliveryTarget: "group:chat1?mentions=user2",
			},
		},
	}
	sender := &mockSender{}
	ch := &Channel{
		gw:             manager,
		sender:         sender,
		sessionTracker: newSessionTracker(),
	}

	err := ch.handleControlTemplateCardEvent(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			From: FromInfo{
				UserID: "user1",
			},
		},
		&TemplateCardEvent{
			EventKey: controlCardEventCronStop,
			TaskID:   "cron-task-1",
			SelectedItems: TemplateCardSelectedItems{
				SelectedItem: []TemplateCardSelectedItem{
					{
						QuestionKey: controlCardCronQuestionKey,
						OptionIDs: TemplateCardOptionIDs{
							OptionID: []string{"job-1"},
						},
					},
				},
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, "job-1", manager.updateJobID)
	require.Equal(
		t,
		"group:chat1?mentions=user2",
		manager.updateTarget,
	)
	require.NotNil(t, sender.lastUpdatedCard)
	require.Contains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		"✅ 已停止：memory report",
	)
}

func TestHandleControlCronEventResumeBlockedAtMaxRuns(
	t *testing.T,
) {
	t.Parallel()

	manager := &fakeScheduledJobManager{
		jobs: []gwclient.ScheduledJobSummary{
			{
				ID:       "job-1",
				Name:     "memory report",
				Enabled:  false,
				RunCount: 5,
				MaxRuns:  5,
			},
		},
	}
	sender := &mockSender{}
	ch := &Channel{
		gw:             manager,
		sender:         sender,
		sessionTracker: newSessionTracker(),
	}

	err := ch.handleControlTemplateCardEvent(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			From: FromInfo{
				UserID: "user1",
			},
		},
		&TemplateCardEvent{
			EventKey: controlCardEventCronResume,
			TaskID:   "cron-task-1",
			SelectedItems: TemplateCardSelectedItems{
				SelectedItem: []TemplateCardSelectedItem{
					{
						QuestionKey: controlCardCronQuestionKey,
						OptionIDs: TemplateCardOptionIDs{
							OptionID: []string{"job-1"},
						},
					},
				},
			},
		},
	)
	require.NoError(t, err)
	require.Empty(t, manager.updateJobID)
	require.NotNil(t, sender.lastUpdatedCard)
	require.Contains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		"该任务已达到最大执行次数（5/5）",
	)
}

// --- Nil safety tests ---

func TestInflightRequestsNilSafe(t *testing.T) {
	t.Parallel()

	var ir *inflightRequests
	require.Equal(t, "", ir.Get("s1"))
	ir.Set("s1", "r1") // should not panic
	ir.Clear("s1", "r1")
}

func TestLaneLockerNilSafe(t *testing.T) {
	t.Parallel()

	var l *laneLocker
	called := false
	l.withLock("key", func() { called = true })
	require.True(t, called)
}

// --- Concurrency helper tests ---

func TestInflightRequests(t *testing.T) {
	t.Parallel()

	ir := newInflightRequests()

	ir.Set("s1", "r1")
	require.Equal(t, "r1", ir.Get("s1"))

	ir.Clear("s1", "r1")
	require.Equal(t, "", ir.Get("s1"))

	// Clear with wrong request ID is no-op.
	ir.Set("s2", "r2")
	ir.Clear("s2", "wrong")
	require.Equal(t, "r2", ir.Get("s2"))
}

func TestRunStatusTrackerTracksQueuedAndLast(t *testing.T) {
	t.Parallel()

	tracker := newRunStatusTracker()
	tracker.start("session1", "req1", defaultProcessingMessage)
	tracker.progress(
		"session1",
		"req1",
		streamStageRunningTool,
		"正在运行 exec_command",
		3*time.Second,
	)
	tracker.queue("session1", "req2", defaultQueuedMessage)

	snapshot := tracker.snapshot("session1")
	require.NotNil(t, snapshot.active)
	require.Equal(t, runStateRunning, snapshot.active.state)
	require.Equal(
		t,
		"正在运行 exec_command",
		snapshot.active.summary,
	)
	require.Equal(t, 3*time.Second, snapshot.active.elapsed)
	require.NotNil(t, snapshot.queued)
	require.Equal(t, runStateQueued, snapshot.queued.state)

	tracker.finish(
		"session1",
		"req1",
		defaultCompletedStatusSummary,
		"hello world",
	)

	snapshot = tracker.snapshot("session1")
	require.Nil(t, snapshot.active)
	require.NotNil(t, snapshot.last)
	require.Equal(t, runStateCompleted, snapshot.last.state)
	require.Equal(
		t,
		defaultCompletedStatusSummary,
		snapshot.last.summary,
	)
	require.Equal(t, "hello world", snapshot.last.preview)
	require.NotNil(t, snapshot.queued)
}

func TestFormatStatusMessageIncludesHints(t *testing.T) {
	t.Parallel()

	message := formatStatusMessage(
		&sessionInfo{
			recallSessionID: "prev-session",
			workspacePath:   "/tmp/custom-repo",
		},
		sessionRunSnapshot{
			active: &requestRunStatus{
				state:   runStateRunning,
				summary: progressTextReadingDocument,
				preview: "partial output",
				contextUsage: &contextUsageStatus{
					usedTokens:    12345,
					contextWindow: 200000,
				},
				startedAt: time.Now().Add(-2 * time.Second),
			},
			queued: &requestRunStatus{
				state:   runStateQueued,
				summary: defaultQueuedMessage,
			},
		},
		"/tmp/default-repo",
		"gpt-5.2",
	)

	require.Contains(t, message, statusLabelState+statusLineRunning)
	require.Contains(
		t,
		message,
		statusLabelWorkspace+"/tmp/custom-repo",
	)
	require.Contains(
		t,
		message,
		displayLabelModel+"gpt-5.2",
	)
	require.Contains(
		t,
		message,
		statusLabelContext+"12.3K / 200K (6.2%)",
	)
	require.Contains(
		t,
		message,
		statusLabelStep+progressTextReadingDocument,
	)
	require.Contains(t, message, statusLabelElapsed)
	require.Contains(t, message, statusLabelQueued)
	require.Contains(t, message, statusLabelOutput)
	require.Contains(t, message, statusHintRecall)
	require.Contains(t, message, statusHintCancel)
	require.Contains(t, message, statusHintSubagents)
}

func TestChannelStatusMessageIncludesRuntimeVersion(
	t *testing.T,
) {
	t.Parallel()

	channel := &Channel{
		runtimeLifecycle: runtimectl.NewManager(
			runtimectl.Options{
				CurrentVersion: "v0.0.50",
			},
		),
		runtimeModelName: "gpt-5.2",
	}

	message := channel.statusMessageText(
		&sessionInfo{
			workspacePath: "/tmp/custom-repo",
		},
		sessionRunSnapshot{},
		"/tmp/default-repo",
	)

	require.Contains(
		t,
		message,
		displayLabelVersion+"v0.0.50",
	)
	require.Contains(
		t,
		message,
		displayLabelModel+"gpt-5.2",
	)
}

func TestRuntimeLifecycleStatusUsesResolvedActorLabel(
	t *testing.T,
) {
	t.Parallel()

	cache := newUserIdentityCache("", userIdentityCacheTTL)
	cache.put(userIdentity{
		UserID:      "T00010001",
		AccountName: "alice.dev",
	})

	channel := &Channel{
		identityResolver: &userIdentityResolver{
			cache: cache,
		},
		userLabelMode: userLabelModeAliasOrName,
	}

	message := channel.formatRuntimeLifecycleStatus(
		context.Background(),
		runtimectl.Status{
			CurrentVersion: "v0.0.50",
			Pending: &runtimectl.PendingAction{
				Kind:  runtimectl.ActionRestart,
				Mode:  runtimectl.ModeGraceful,
				Actor: "T00010001",
			},
		},
	)

	require.Contains(
		t,
		message,
		runtimeStatusLineActor+"alice.dev",
	)
}

func TestSessionTrackerRecallPreviousSession(t *testing.T) {
	t.Parallel()

	tracker := newSessionTracker()
	tracker.sessions["chat1"] = &sessionInfo{
		sessionID:       "current-session",
		baseSessionID:   "chat1",
		recallSessionID: "previous-session",
		lastActivity:    time.Now(),
	}

	info, ok := tracker.recallPreviousSession("chat1")
	require.True(t, ok)
	require.Equal(t, "previous-session", info.sessionID)
	require.Equal(t, "current-session", info.recallSessionID)

	current := tracker.getOrCreateSession("chat1", 0)
	require.Equal(t, "previous-session", current.sessionID)
	require.Equal(t, "current-session", current.recallSessionID)
}

func TestSessionTrackerDefaultSessionPersistsAcrossReload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	tracker := newSessionTrackerWithPath(path)

	info := tracker.getOrCreateSession("wecom:dm:user1", time.Hour)
	require.Equal(t, "wecom:dm:user1", info.sessionID)
	require.Empty(t, info.recallSessionID)

	reloaded := newSessionTrackerWithPath(path)
	info = reloaded.getOrCreateSession("wecom:dm:user1", time.Hour)
	require.Equal(t, "wecom:dm:user1", info.sessionID)
	require.Empty(t, info.recallSessionID)
}

func TestSessionTrackerRotatedSessionPersistsAcrossReload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	tracker := newSessionTrackerWithPath(path)
	tracker.now = func() time.Time {
		return time.Unix(100, 0)
	}

	info := tracker.getOrCreateSession("wecom:dm:user1", time.Hour)
	require.Equal(t, "wecom:dm:user1", info.sessionID)

	tracker.now = func() time.Time {
		return time.Unix(101, 0)
	}
	info = tracker.startNewSession("wecom:dm:user1")
	require.NotEqual(t, "wecom:dm:user1", info.sessionID)
	require.Equal(t, "wecom:dm:user1", info.recallSessionID)

	reloaded := newSessionTrackerWithPath(path)
	reloaded.now = func() time.Time {
		return time.Unix(102, 0)
	}
	info = reloaded.getOrCreateSession("wecom:dm:user1", time.Hour)
	require.NotEqual(t, "wecom:dm:user1", info.sessionID)
	require.Equal(t, "wecom:dm:user1", info.recallSessionID)
}

func TestSessionTrackerAutoSplitDoesNotEnableRecall(t *testing.T) {
	t.Parallel()

	tracker := newSessionTracker()
	tracker.now = func() time.Time {
		return time.Unix(100, 0)
	}

	info := tracker.getOrCreateSession("wecom:dm:user1", time.Minute)
	require.Equal(t, "wecom:dm:user1", info.sessionID)

	tracker.now = func() time.Time {
		return time.Unix(200, 0)
	}

	info = tracker.getOrCreateSession("wecom:dm:user1", time.Minute)
	require.Equal(t, "wecom:dm:user1:200", info.sessionID)
	require.Empty(t, info.recallSessionID)

	_, ok := tracker.recallPreviousSession("wecom:dm:user1")
	require.False(t, ok)
}

func TestSessionTrackerVersionOneDropsLegacyRecallTarget(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	data := `{
  "version": 1,
  "sessions": {
    "wecom:dm:user1": {
      "session_id": "wecom:dm:user1:100",
      "previous_session_id": "wecom:dm:user1",
      "last_activity_unix": 100,
      "epoch": 100
    }
  }
}
`
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	tracker := newSessionTrackerWithPath(path)
	tracker.now = func() time.Time {
		return time.Unix(100, 0)
	}
	info := tracker.getOrCreateSession("wecom:dm:user1", time.Hour)
	require.Equal(t, "wecom:dm:user1:100", info.sessionID)
	require.Empty(t, info.recallSessionID)
}

func TestSessionTrackerSwitchSessionUpdatesHistory(t *testing.T) {
	t.Parallel()

	tracker := newSessionTracker()
	tracker.now = func() time.Time {
		return time.Unix(100, 0)
	}

	baseID := "wecom:dm:user1"
	info := tracker.getOrCreateSession(baseID, 0)
	require.Equal(t, baseID, info.sessionID)

	tracker.now = func() time.Time {
		return time.Unix(200, 0)
	}
	info = tracker.startNewSession(baseID)
	require.Len(t, info.history, 2)

	tracker.now = func() time.Time {
		return time.Unix(300, 0)
	}
	switched, ok := tracker.switchSession(baseID, baseID)
	require.True(t, ok)
	require.Equal(t, baseID, switched.sessionID)
	require.Len(t, switched.history, 2)
	require.Equal(t, baseID, switched.history[0].SessionID)
	require.Equal(t, time.Unix(300, 0), switched.history[0].LastActivity)
}

func TestSessionTrackerPersonaPersistsAcrossReload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	tracker := newSessionTrackerWithPath(path)
	tracker.now = func() time.Time {
		return time.Unix(100, 0)
	}

	baseID := "wecom:dm:user1"
	info := tracker.setPersona(baseID, personaapi.ConciseID)
	require.Equal(t, personaapi.ConciseID, info.personaID)
	require.True(t, info.personaPinned)

	reloaded := newSessionTrackerWithPath(path)
	info = reloaded.getOrCreateSession(baseID, 0)
	require.Equal(t, personaapi.ConciseID, info.personaID)
	require.True(t, info.personaPinned)
	require.NotEmpty(t, info.history)
}

func TestSessionTrackerInheritedPersonaPersistsWithoutPin(
	t *testing.T,
) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	tracker := newSessionTrackerWithPath(path)

	baseID := "wecom:dm:user1"
	info := tracker.getOrCreateSession(baseID, 0)
	require.Equal(t, personaapi.PragmaticID, info.personaID)
	require.False(t, info.personaPinned)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "\"persona_id\"")
	require.NotContains(t, string(raw), "\"persona_pinned\"")

	reloaded := newSessionTrackerWithPath(path)
	info = reloaded.getOrCreateSession(baseID, 0)
	require.Equal(t, personaapi.PragmaticID, info.personaID)
	require.False(t, info.personaPinned)
}

func TestSessionTrackerMigratesLegacyPersonaState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		personaID     string
		wantPersonaID string
		wantPinned    bool
	}{
		{
			name:          "snarky_default",
			personaID:     personaapi.SnarkyID,
			wantPersonaID: personaapi.PragmaticID,
			wantPinned:    false,
		},
		{
			name:          "pragmatic_default",
			personaID:     personaapi.PragmaticID,
			wantPersonaID: personaapi.PragmaticID,
			wantPinned:    false,
		},
		{
			name:          "custom_override",
			personaID:     "custom_toxic",
			wantPersonaID: "custom_toxic",
			wantPinned:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(
				t.TempDir(),
				"session-tracker.json",
			)
			baseID := "wecom:dm:user1"
			data := fmt.Sprintf(
				"{\n"+
					"  \"version\": %d,\n"+
					"  \"sessions\": {\n"+
					"    %q: {\n"+
					"      \"session_id\": %q,\n"+
					"      \"persona_id\": %q\n"+
					"    }\n"+
					"  }\n"+
					"}\n",
				sessionTrackerStoreV7,
				baseID,
				baseID,
				tt.personaID,
			)
			require.NoError(
				t,
				os.WriteFile(path, []byte(data), 0o600),
			)

			tracker := newSessionTrackerWithPath(path)
			info := tracker.getOrCreateSession(baseID, 0)
			require.Equal(t, tt.wantPersonaID, info.personaID)
			require.Equal(t, tt.wantPinned, info.personaPinned)
			require.Equal(
				t,
				tt.wantPersonaID,
				info.effectivePersonaID(),
			)
		})
	}
}

func TestSessionTrackerClearPersonaRestoresInheritedDefault(
	t *testing.T,
) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	tracker := newSessionTrackerWithPath(path)

	baseID := "wecom:dm:user1"
	info := tracker.setPersona(baseID, personaapi.ConciseID)
	require.Equal(t, personaapi.ConciseID, info.personaID)
	require.True(t, info.personaPinned)

	info = tracker.clearPersona(baseID)
	require.Equal(t, personaapi.PragmaticID, info.personaID)
	require.False(t, info.personaPinned)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "\"persona_id\"")
	require.NotContains(t, string(raw), "\"persona_pinned\"")

	reloaded := newSessionTrackerWithPath(path)
	info = reloaded.getOrCreateSession(baseID, 0)
	require.Equal(t, personaapi.PragmaticID, info.personaID)
	require.False(t, info.personaPinned)
}

func TestSessionTrackerAssistantAliasPersistsAcrossReload(
	t *testing.T,
) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	tracker := newSessionTrackerWithPath(path)
	tracker.now = func() time.Time {
		return time.Unix(100, 0)
	}

	baseID := "wecom:dm:user1"
	info := tracker.setAssistantAlias(baseID, "彪子")
	require.Equal(t, "彪子", info.assistantAlias)

	reloaded := newSessionTrackerWithPath(path)
	info = reloaded.getOrCreateSession(baseID, 0)
	require.Equal(t, "彪子", info.assistantAlias)
	require.NotEmpty(t, info.history)
}

func TestSessionTrackerKnownUserIDsPersistAcrossReload(
	t *testing.T,
) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	tracker := newSessionTrackerWithPath(path)

	baseID := "wecom:chat:chat1"
	info := tracker.recordKnownUsers(
		baseID,
		[]string{"T00010001", "T00010002", "T00010001"},
	)
	require.Equal(
		t,
		[]string{"T00010001", "T00010002"},
		info.knownUserIDs,
	)

	reloaded := newSessionTrackerWithPath(path)
	info = reloaded.getOrCreateSession(baseID, 0)
	require.Equal(
		t,
		[]string{"T00010001", "T00010002"},
		info.knownUserIDs,
	)
}

func TestSessionTrackerKnownUserIDsForSession(
	t *testing.T,
) {
	t.Parallel()

	tracker := newSessionTrackerWithPath("")
	groupBaseID := "wecom:chat:chat1"

	tracker.recordKnownUsers(groupBaseID, []string{"T00010001"})
	tracker.recordKnownUsers(
		"wecom:dm:T00010002",
		[]string{"T00010002"},
	)
	tracker.recordKnownUsers(
		"wecom:dm:T00010003",
		[]string{"T00010003", "T00010001"},
	)

	require.ElementsMatch(
		t,
		[]string{
			"T00010001",
			"T00010002",
			"T00010003",
		},
		tracker.knownUserIDsForSession(groupBaseID),
	)
}

func TestSessionTrackerDefaultsToPragmaticPersona(
	t *testing.T,
) {
	t.Parallel()

	tracker := newSessionTracker()
	info := tracker.getOrCreateSession("wecom:dm:user1", 0)
	require.Equal(t, personaapi.PragmaticID, info.personaID)
	require.False(t, info.personaPinned)
}

func TestBuildPersonaSettingsCardUsesCurrentSelection(t *testing.T) {
	t.Parallel()

	card := buildPersonaSettingsCard(
		"Streambot2",
		"教练（coach）",
		&sessionInfo{
			personaID:     personaapi.CoachID,
			personaPinned: true,
		},
		personaapi.Builtins(),
		"task-1",
		personaCardViewDefault,
		"",
		true,
	)

	require.Equal(
		t,
		templateCardTypeButtonInteraction,
		card.CardType,
	)
	require.Equal(t, "task-1", card.TaskID)
	require.NotNil(t, card.ButtonSelection)
	require.Equal(
		t,
		personaapi.CoachID,
		card.ButtonSelection.SelectedID,
	)
	require.Equal(
		t,
		personaCardSelectionTitle,
		card.ButtonSelection.Title,
	)
	require.Len(
		t,
		card.ButtonSelection.OptionList,
		len(personaCardDropdownPersonaIDs),
	)
	require.Equal(
		t,
		personaapi.CoachID,
		card.ButtonSelection.SelectedID,
	)
	for _, option := range card.ButtonSelection.OptionList {
		require.False(
			t,
			isPersonaCardQuickID(option.ID),
		)
	}
	require.NotContains(
		t,
		personaCardOptionIDs(card.ButtonSelection.OptionList),
		personaapi.ProfessionalID,
	)
	require.Contains(
		t,
		personaCardOptionIDs(card.ButtonSelection.OptionList),
		personaapi.PragmaticID,
	)
	require.NotContains(
		t,
		personaCardOptionIDs(card.ButtonSelection.OptionList),
		personaapi.LegacyDefaultID,
	)
	require.Len(t, card.ButtonList, 6)
	require.Equal(t, "毒舌", card.ButtonList[0].Text)
	require.Equal(t, "女友", card.ButtonList[1].Text)
	require.Equal(t, "男友", card.ButtonList[2].Text)
	require.Equal(
		t,
		personaCardApplyText,
		card.ButtonList[3].Text,
	)
	require.Equal(
		t,
		personaCardSaveHelpText,
		card.ButtonList[4].Text,
	)
	require.Equal(
		t,
		personaCardHomeText,
		card.ButtonList[5].Text,
	)
	require.Contains(
		t,
		card.SubTitleText,
		personaCardQuickHint,
	)
	require.Contains(
		t,
		card.SubTitleText,
		personaCardMoreHint,
	)
	require.Contains(
		t,
		card.SubTitleText,
		personaCardEffectHint,
	)
}

func TestBuildPersonaSettingsCardMarksCurrentQuickPersona(
	t *testing.T,
) {
	t.Parallel()

	card := buildPersonaSettingsCard(
		"Streambot2",
		"毒舌（snarky）",
		&sessionInfo{
			personaID:     personaapi.SnarkyID,
			personaPinned: true,
		},
		personaapi.Builtins(),
		"task-2",
		personaCardViewDefault,
		personaCardChangedNote,
		true,
	)

	require.Len(t, card.ButtonList, 6)
	require.Equal(
		t,
		"毒舌"+personaCardCurrentSuffix,
		card.ButtonList[0].Text,
	)
	require.NotNil(t, card.ButtonSelection)
	require.Empty(t, card.ButtonSelection.SelectedID)
	require.Contains(
		t,
		card.SubTitleText,
		personaCardChangedNote,
	)
}

func TestBuildPersonaSettingsCardKeepsSelectedCustomPersona(
	t *testing.T,
) {
	t.Parallel()

	defs := append(
		personaapi.Builtins(),
		personaapi.Definition{
			ID:      "custom_persona",
			Name:    "自定义人格",
			Summary: "测试",
			Prompt:  "test",
		},
	)
	card := buildPersonaSettingsCard(
		"Streambot2",
		"自定义人格",
		&sessionInfo{
			personaID:     "custom_persona",
			personaPinned: true,
		},
		defs,
		"task-3",
		personaCardViewDefault,
		"",
		true,
	)

	require.NotNil(t, card.ButtonSelection)
	require.Equal(
		t,
		"custom_persona",
		card.ButtonSelection.SelectedID,
	)
	require.Len(
		t,
		card.ButtonSelection.OptionList,
		len(personaCardDropdownPersonaIDs),
	)
	found := false
	for _, option := range card.ButtonSelection.OptionList {
		if option.ID == "custom_persona" {
			found = true
		}
	}
	require.True(t, found)
}

func personaCardOptionIDs(
	options []templateCardOption,
) []string {
	ids := make([]string, 0, len(options))
	for _, option := range options {
		ids = append(ids, option.ID)
	}
	return ids
}

func TestNormalizeTemplateCardClampsInteractiveLimits(
	t *testing.T,
) {
	t.Parallel()

	options := make([]templateCardOption, 0, 12)
	for i := 0; i < 12; i++ {
		options = append(options, templateCardOption{
			ID:   fmt.Sprintf("opt-%d", i),
			Text: fmt.Sprintf("Option %d", i),
		})
	}
	buttons := make([]templateCardButton, 0, 7)
	for i := 0; i < 7; i++ {
		buttons = append(buttons, templateCardButton{
			Text: fmt.Sprintf("Button %d", i),
			Key:  fmt.Sprintf("btn-%d", i),
		})
	}
	card := normalizeTemplateCard(&templateCard{
		CardType: templateCardTypeButtonInteraction,
		ButtonSelection: &templateCardSelection{
			QuestionKey: personaCardQuestionKey,
			SelectedID:  "opt-11",
			OptionList:  options,
		},
		ButtonList: buttons,
	})

	require.Len(t, card.ButtonList, templateCardButtonLimit)
	require.NotNil(t, card.ButtonSelection)
	require.Len(
		t,
		card.ButtonSelection.OptionList,
		templateCardButtonOptionLimit,
	)
	require.Equal(t, "opt-11", card.ButtonSelection.SelectedID)
	lastIndex := len(card.ButtonSelection.OptionList) - 1
	lastOption := card.ButtonSelection.OptionList[lastIndex]
	require.Equal(
		t,
		"opt-11",
		lastOption.ID,
	)
}

func TestSessionTrackerWorkspacePersistsAcrossReload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session-tracker.json")
	tracker := newSessionTrackerWithPath(path)
	tracker.now = func() time.Time {
		return time.Unix(100, 0)
	}

	baseID := "wecom:dm:user1"
	workspacePath := filepath.Join(t.TempDir(), "repo")
	info := tracker.setWorkspace(baseID, workspacePath)
	require.Equal(t, workspacePath, info.workspacePath)

	reloaded := newSessionTrackerWithPath(path)
	info = reloaded.getOrCreateSession(baseID, 0)
	require.Equal(t, workspacePath, info.workspacePath)
	require.NotEmpty(t, info.history)
}

func TestBuildReplyWorkspacePrefix(t *testing.T) {
	t.Parallel()

	repoDir := filepath.Join(t.TempDir(), "repo")
	workdir := filepath.Join(repoDir, "pkg")
	require.NoError(t, os.MkdirAll(workdir, 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755))

	prefix := buildReplyWorkspacePrefix(
		workdir,
		"",
	)
	require.Contains(t, prefix, workspaceReplyLabelPath+workdir)
	require.Contains(t, prefix, workspaceReplyLabelGitRoot+repoDir)

	require.Equal(
		t,
		workspaceReplyLabelPath+repoDir,
		buildReplyWorkspacePrefix("", repoDir),
	)
}

func TestFormatWorkspaceDisplayOmitsRuntimePrefix(
	t *testing.T,
) {
	t.Parallel()

	require.Equal(
		t,
		"/tmp/openclaw",
		formatWorkspaceDisplay("", "/tmp/openclaw"),
	)
	require.Equal(
		t,
		"/tmp/custom",
		formatWorkspaceDisplay("/tmp/custom", "/tmp/openclaw"),
	)
	require.Equal(
		t,
		workspaceDisplayUnset,
		formatWorkspaceDisplay("", ""),
	)
}
