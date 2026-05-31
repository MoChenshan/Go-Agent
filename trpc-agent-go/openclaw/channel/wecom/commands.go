package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/croncmd"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
)

const (
	commandPrefix = "/"
)

const (
	helpArgAll           = "all"
	helpArgText          = "text"
	helpArgCommands      = "commands"
	personaExampleName   = "爱心"
	personaExamplePrompt = "热心一点，先给结论。"
	defaultChatPersonaID = personaapi.DefaultID

	cancelKeyword    = "/cancel"
	cronKeyword      = "/cron"
	helpKeyword      = "/help"
	nameKeyword      = "/name"
	newKeyword       = "/new"
	personaKeyword   = "/persona"
	recallKeyword    = "/recall"
	runtimeKeyword   = "/runtime"
	subagentsKeyword = "/subagents"
	sessionKeyword   = "/session"
	sessionsKeyword  = "/sessions"
	statusKeyword    = "/status"
	switchKeyword    = "/switch"
	welcomeKeyword   = "/welcome"
	workspaceKeyword = "/workspace"

	nameScopeGlobal = "global"
)

const (
	helpIntroLineOne = "👋 嗨，直接发问题、图片、截图或文件给我就行。"
	helpIntroLineTwo = "⚡ 想更快操作，也可以用下面这些命令："

	helpSectionCommon    = "✨ 常用命令："
	helpSectionCron      = "⏰ 定时任务："
	helpSectionRuntime   = "🛠 运行时控制："
	helpSectionSessions  = "🧵 会话管理："
	helpSectionPersona   = "🎭 人格设置："
	helpSectionWorkspace = "💻 代码工作区："
	helpSectionTips      = "💡 小提示："

	helpDescHelp        = "打开帮助卡片；/help all 看全文；/help runtime 看详解"
	helpDescName        = "查看当前名字；global 管默认名字"
	helpDescWelcome     = "重新打开欢迎卡片和快捷入口"
	helpDescStatus      = "查看当前阶段、排队情况和最近输出"
	helpDescCron        = "list/status/stop/resume/remove/clear"
	helpDescRuntime     = "查看运行时卡片、状态或调试 bundle"
	helpDescRuntimeMore = "status/restart/upgrade/versions/" +
		"changelog/bundle/full"
	helpDescNew         = "开始新会话，清空当前上下文"
	helpDescCancel      = "取消当前正在执行的请求"
	helpDescSubagents   = "list/get/cancel 当前会话的后台子任务"
	helpDescSession     = "查看当前会话、人格和分会话设置"
	helpDescSessions    = "[数量] 查看最近会话列表"
	helpDescSwitch      = "<序号> 切换到某个历史会话"
	helpDescRecall      = "切回 /new 前的上一会话"
	helpDescPersona     = "打开人格卡片或查看当前人格"
	helpDescPersonaDo   = "<名称或设定> 切换或直接新增人格"
	helpDescPersonaUse  = "use <名称> 只切换已存在人格"
	helpDescPersonaSave = "save <名称> <设定> 指定名称新增" +
		"或更新人格"
	helpDescPersonaShow   = "show <名称> 查看人格内容"
	helpDescPersonaDelete = "delete <名称> 删除自定义人格"
	helpDescWorkspace     = "[目录|off] 查看或切换代码工作区"
)

const (
	personaActionList   = "list"
	personaActionUse    = "use"
	personaActionShow   = "show"
	personaActionSave   = "save"
	personaActionDelete = "delete"
)

const (
	runStateQueued    = "queued"
	runStateRunning   = "running"
	runStateCompleted = "completed"
	runStateCanceled  = "canceled"
	runStateFailed    = "failed"

	runStatusSourceActive = "active"
	runStatusSourceQueued = "queued"
	runStatusSourceLast   = "last"

	statusPreviewMaxRunes = 160

	statusLineIdle      = "空闲"
	statusLineQueued    = "排队中"
	statusLineRunning   = "处理中"
	statusLineCompleted = "最近一次已完成"
	statusLineCanceled  = "最近一次已取消"
	statusLineFailed    = "最近一次失败"

	statusLabelState     = "当前状态："
	statusLabelStep      = "当前步骤："
	statusLabelElapsed   = "已用时："
	statusLabelQueued    = "排队请求："
	statusLabelOutput    = "最近输出："
	statusLabelLast      = "最近一次："
	statusLabelSession   = "当前会话："
	statusLabelAssistant = "当前名字："
	statusLabelPersona   = "当前人格："
	statusLabelTimeout   = "自动分会话："
	statusLabelHistory   = "最近会话："
	statusLabelWorkspace = "代码工作区："
	displayLabelModel    = "当前模型："
	displayLabelVersion  = "当前版本："

	statusHintRecall = "可发送 " + recallKeyword +
		" 切回 /new 前的上一会话。"
	statusHintCancel   = "可发送 " + cancelKeyword + " 取消当前请求。"
	statusHintSessions = "可发送 " + sessionsKeyword +
		" 查看最近会话，或用 " + switchKeyword +
		" <序号> 切换。"
	statusHintSubagents = "可发送 " + subagentsKeyword +
		" 查看当前会话的后台子任务。"
	statusHintPersona = "可发送 " + personaKeyword +
		" 打开人格卡片，查看、切换或新增人格。"
	statusHintAssistant = "可发送 " + nameKeyword +
		" 查看名字规则；" + nameKeyword +
		" <称呼|off> 只改当前聊天名字；" +
		nameKeyword + " global <称呼|off> 修改默认名字。"
	statusHintWorkspace = "可发送 " + workspaceKeyword +
		" <目录|off> 设置代码工作区。"
	statusHintCron = "可发送 " + cronKeyword +
		" list 查看当前聊天的定时任务。"
	statusHintRuntime = "可发送 " + runtimeKeyword +
		" 查看运行时状态、升级和重启入口。"

	nameCommandUsage = "用法：" + nameKeyword +
		" [称呼|off] 或 " + nameKeyword +
		" global [称呼|off]"
	nameStatusChatLabel   = "当前聊天名字："
	nameStatusGlobalLabel = "默认名字："
	nameStatusUnset       = "（未设置）"
	nameStatusRulesTitle  = "规则："
	nameStatusRuleOrder   = "- 当前聊天名字优先，默认名字兜底。"
	nameStatusRulePersist = "- 当前聊天名字会跨 /new 保留，直到发送 " +
		nameKeyword + " off。"
	nameStatusRuleGlobal = "- " + nameKeyword +
		" global <称呼|off> 用来设置或清除默认名字。"
	nameStatusExamplesTitle    = "例子："
	nameStatusGlobalExampleOne = "- 其他用户新开一个私聊，" +
		"如果那边还没单独改名，就会看到默认名字。"
	nameStatusGlobalExampleTwo = "- 其他群、其他私聊如果已经有自己" +
		"的当前聊天名字，就会继续优先用自己的名字。"
	helpCommandUsage = "用法：" + helpKeyword +
		" [" + helpArgAll + "|" + helpArgText + "|" +
		helpArgCommands + "|<主题>]"
	subagentsCommandUsage = "用法：" + subagentsKeyword +
		" [list|get <id>|cancel <id>|help]"
	welcomeCommandUsage  = "用法：" + welcomeKeyword
	sessionCommandUsage  = "用法：" + sessionKeyword
	sessionsCommandUsage = "用法：" + sessionsKeyword +
		" [数量]"
	switchCommandUsage = "用法：" + switchKeyword +
		" <序号>"
	personaCommandUsage = "用法：" + personaKeyword
	personaListUsage    = "用法：" + personaKeyword +
		" " + personaActionList
	personaUseUsage = "用法：" + personaKeyword +
		" <人格名称或设定> 或 " + personaKeyword +
		" " + personaActionUse + " <人格名称>"
	personaShowUsage = "用法：" + personaKeyword +
		" " + personaActionShow + " <人格名称>"
	personaSaveUsage = "用法：" + personaKeyword +
		" " + personaActionSave + " <人格名称> <设定>"
	personaDeleteUsage = "用法：" + personaKeyword +
		" " + personaActionDelete + " <人格名称>"
	workspaceCommandUsage = "用法：" + workspaceKeyword +
		" [目录|off]"
	cronCommandUsage = "用法：" + cronKeyword +
		" help|list|status <序号或ID>|stop <序号或ID>|" +
		"resume <序号或ID>|remove <序号或ID>|clear"
	runtimeCommandUsage = "用法：" + runtimeKeyword +
		" [help|status|restart [force]|" +
		"upgrade [force|版本|preview]|" +
		"versions|changelog [版本]|bundle [full [总上限]]]"
	personaDirectCreateHelpLine = "直接新增人格：发送 " +
		personaKeyword + " " + personaExamplePrompt
	personaSaveHelpLine = "指定名称新增人格：发送 " +
		personaKeyword + " " + personaActionSave +
		" " + personaExampleName + " " +
		personaExamplePrompt
	personaListHelpLine = "查看完整人格列表：发送 " +
		personaKeyword + " " + personaActionList + "。"
)

const (
	sessionTrackerStateDirEnvName = "TRPC_CLAW_STATE_DIR"

	sessionTrackerStoreVersion  = 8
	sessionTrackerStoreV1       = 1
	sessionTrackerStoreV2       = 2
	sessionTrackerStoreV3       = 3
	sessionTrackerStoreV4       = 4
	sessionTrackerStoreV5       = 5
	sessionTrackerStoreV6       = 6
	sessionTrackerStoreV7       = 7
	sessionTrackerStoreDirName  = "wecom"
	sessionTrackerStoreFileName = "session_tracker.json"
	personaStoreDirName         = "personas"

	sessionTrackerStoreDirPerm  = 0o700
	sessionTrackerStoreFilePerm = 0o600

	sessionHistoryMaxEntries = 12
	sessionListDefaultLimit  = 5
	sessionListMaxLimit      = 10

	assistantAliasMaxRunes     = 32
	knownUserIDMaxEntries      = 24
	directMessageSessionPrefix = pluginType + ":dm:"

	cronStatusTimeLayout = "2006-01-02 15:04:05 MST"
	cronTextPreviewRunes = 120

	cronRunCountLabel = "执行次数："
	cronRunCountList  = "次数="
	cronMaxRunsHint   = "提示：已达到最大执行次数，" +
		"恢复不会继续运行。请重新创建任务，或调整 " +
		"max_runs。"
	cronMaxRunsBlockedFormat = "该任务已达到最大执行次数" +
		"（%s），恢复不会继续运行。请重新创建任务，" +
		"或调整 max_runs。"
)

type parsedCommand struct {
	keyword string
	args    []string
	rawArgs string
}

type helpCommandEntry struct {
	command string
	desc    string
}

type helpSection struct {
	title   string
	entries []helpCommandEntry
}

type scheduledJobManager interface {
	ListScheduledJobs(
		ctx context.Context,
		channel string,
		userID string,
		target string,
	) ([]gwclient.ScheduledJobSummary, error)
	ClearScheduledJobs(
		ctx context.Context,
		channel string,
		userID string,
		target string,
	) (int, error)
	SetScheduledJobEnabled(
		ctx context.Context,
		channel string,
		userID string,
		target string,
		jobID string,
		enabled bool,
	) (gwclient.ScheduledJobSummary, error)
	RemoveScheduledJob(
		ctx context.Context,
		channel string,
		userID string,
		target string,
		jobID string,
	) (bool, error)
}

// parseCommand extracts a /command from the beginning of the text.
func parseCommand(text string) string {
	return parseCommandInput(text).keyword
}

func parseCommandInput(text string) parsedCommand {
	trimmed := normalizeAddressedCommandText(text)
	if !strings.HasPrefix(trimmed, commandPrefix) {
		return parsedCommand{}
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return parsedCommand{}
	}
	keyword, ok := normalizeCommandKeyword(fields[0])
	if !ok {
		return parsedCommand{}
	}
	return parsedCommand{
		keyword: keyword,
		args:    fields[1:],
		rawArgs: commandRawArgs(trimmed),
	}
}

func normalizeAddressedCommandText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if candidate, ok := trimLeadingAddressPrefix(trimmed); ok {
		return candidate
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	index := 0
	for index < len(fields) && isLeadingMentionToken(fields[index]) {
		index++
	}
	if index == 0 {
		return trimmed
	}
	return strings.Join(fields[index:], " ")
}

func trimLeadingAddressPrefix(text string) (string, bool) {
	if !strings.HasPrefix(text, commandPrefix) &&
		!strings.HasPrefix(text, mentionPrefix) {
		return "", false
	}
	if strings.HasPrefix(text, commandPrefix) {
		return text, true
	}

	index := strings.Index(text, commandPrefix)
	for index >= 0 {
		candidate := strings.TrimSpace(text[index:])
		if candidate == "" {
			return "", false
		}
		fields := strings.Fields(candidate)
		if len(fields) == 0 {
			return "", false
		}
		if _, ok := normalizeCommandKeyword(fields[0]); ok &&
			hasLeadingAddressPrefix(text[:index]) {
			return candidate, true
		}

		next := strings.Index(text[index+1:], commandPrefix)
		if next < 0 {
			break
		}
		index += next + 1
	}
	return "", false
}

func hasLeadingAddressPrefix(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	if strings.ContainsAny(prefix, "\r\n") {
		return false
	}
	return strings.HasPrefix(prefix, mentionPrefix)
}

func isLeadingMentionToken(token string) bool {
	trimmed := strings.TrimSpace(token)
	return strings.HasPrefix(trimmed, mentionPrefix) &&
		len([]rune(trimmed)) > 1
}

func commandRawArgs(trimmed string) string {
	index := strings.IndexAny(trimmed, " \t\r\n")
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[index:])
}

func normalizeCommandKeyword(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case cancelKeyword:
		return cancelKeyword, true
	case cronKeyword:
		return cronKeyword, true
	case helpKeyword:
		return helpKeyword, true
	case nameKeyword:
		return nameKeyword, true
	case newKeyword:
		return newKeyword, true
	case personaKeyword:
		return personaKeyword, true
	case recallKeyword:
		return recallKeyword, true
	case runtimeKeyword:
		return runtimeKeyword, true
	case subagentsKeyword:
		return subagentsKeyword, true
	case sessionKeyword:
		return sessionKeyword, true
	case sessionsKeyword:
		return sessionsKeyword, true
	case statusKeyword:
		return statusKeyword, true
	case switchKeyword:
		return switchKeyword, true
	case welcomeKeyword:
		return welcomeKeyword, true
	case workspaceKeyword:
		return workspaceKeyword, true
	default:
		return "", false
	}
}

func buildDefaultHelpMessage() string {
	lines := []string{
		helpIntroLineOne,
		helpIntroLineTwo,
	}
	for _, section := range defaultHelpSections() {
		lines = append(lines, "")
		lines = append(lines, formatHelpSection(section)...)
	}
	lines = append(lines, "")
	lines = append(lines, helpSectionTips)
	for _, tip := range defaultHelpTips() {
		lines = append(lines, "- "+tip)
	}
	return strings.Join(lines, "\n")
}

func defaultHelpSections() []helpSection {
	return []helpSection{
		{
			title: helpSectionCommon,
			entries: []helpCommandEntry{
				{command: helpKeyword, desc: helpDescHelp},
				{command: welcomeKeyword, desc: helpDescWelcome},
				{command: nameKeyword, desc: helpDescName},
				{command: statusKeyword, desc: helpDescStatus},
				{command: newKeyword, desc: helpDescNew},
				{command: cancelKeyword, desc: helpDescCancel},
			},
		},
		{
			title: helpSectionCron,
			entries: []helpCommandEntry{
				{command: cronKeyword, desc: helpDescCron},
			},
		},
		{
			title: helpSectionRuntime,
			entries: []helpCommandEntry{
				{
					command: runtimeKeyword,
					desc:    helpDescRuntime,
				},
				{
					command: runtimeKeyword,
					desc:    helpDescRuntimeMore,
				},
			},
		},
		{
			title: helpSectionSessions,
			entries: []helpCommandEntry{
				{command: subagentsKeyword, desc: helpDescSubagents},
				{command: sessionKeyword, desc: helpDescSession},
				{command: sessionsKeyword, desc: helpDescSessions},
				{command: switchKeyword, desc: helpDescSwitch},
				{command: recallKeyword, desc: helpDescRecall},
			},
		},
		{
			title: helpSectionPersona,
			entries: []helpCommandEntry{
				{command: personaKeyword, desc: helpDescPersona},
				{
					command: personaKeyword,
					desc:    helpDescPersonaDo,
				},
				{
					command: personaKeyword,
					desc:    helpDescPersonaUse,
				},
				{
					command: personaKeyword,
					desc:    helpDescPersonaSave,
				},
				{
					command: personaKeyword,
					desc:    helpDescPersonaShow,
				},
				{
					command: personaKeyword,
					desc:    helpDescPersonaDelete,
				},
			},
		},
		{
			title: helpSectionWorkspace,
			entries: []helpCommandEntry{
				{
					command: workspaceKeyword,
					desc:    helpDescWorkspace,
				},
			},
		},
	}
}

func defaultHelpTips() []string {
	return []string{
		"想看全文命令清单时，发送 " +
			helpKeyword + " " + helpArgAll,
		"想看某个命令的完整说明时，发送 " +
			helpKeyword + " runtime 或 " +
			runtimeKeyword + " " + helpAliasArg,
		"长任务处理中可随时发送 " + statusKeyword,
		"想重新打开欢迎卡片时，发送 " +
			welcomeKeyword,
		"想给我换个名字时，发送 " +
			nameKeyword + " 小助手",
		"查看或停止定时任务时，发送 " +
			cronKeyword + " list",
		"想看运行时升级或重启入口时，发送 " +
			runtimeKeyword,
		"想找回旧上下文时，先发 " + sessionsKeyword,
		"想切换回复风格时，发送 " +
			personaKeyword + " 打开人格卡片",
		"想直接新增人格时，发送 " +
			personaKeyword + " " + personaExamplePrompt,
		"想指定名称时，发送 " +
			personaKeyword + " " + personaActionSave +
			" " + personaExampleName + " " +
			personaExamplePrompt,
		"做代码任务前，可先发 " +
			workspaceKeyword + " /path/to/repo",
	}
}

func formatHelpSection(section helpSection) []string {
	lines := []string{section.title}
	for _, entry := range section.entries {
		lines = append(lines, formatHelpCommandEntry(entry))
	}
	return lines
}

func formatHelpCommandEntry(entry helpCommandEntry) string {
	command := strings.TrimSpace(entry.command)
	desc := strings.TrimSpace(entry.desc)
	switch {
	case command == "":
		return desc
	case desc == "":
		return command
	default:
		return command + " " + desc
	}
}

func isFullHelpRequest(args []string) bool {
	for _, arg := range args {
		if isFullHelpToken(arg) {
			return true
		}
	}
	return false
}

func (c *Channel) handleHelpCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	args []string,
	sender messageSender,
) error {
	if topic, ok := parseExplicitHelpTopic(args); ok {
		_ = sender.SendText(
			ctx,
			chatID,
			formatCommandHelpTopic(topic),
		)
		return nil
	}
	if isFullHelpRequest(args) {
		_ = sender.SendText(ctx, chatID, c.helpMessage)
		return nil
	}
	if len(args) > 0 {
		_ = sender.SendText(
			ctx,
			chatID,
			formatUnknownHelpTopic(args[0]),
		)
		return nil
	}
	if cardSender, ok := sender.(templateCardSender); ok &&
		cardSender != nil {
		_ = cardSender.SendTemplateCard(
			ctx,
			chatID,
			buildControlHelpCard(
				c.assistantDisplayNameForSession(baseSessionID),
				newInteractiveCardTaskID(
					controlCardTaskPrefix,
					baseSessionID,
				),
				controlHelpPageDefault,
			),
		)
		return nil
	}
	_ = sender.SendText(
		ctx,
		chatID,
		c.helpMessage+"\n\n"+helpCommandUsage,
	)
	return nil
}

func (c *Channel) handleWelcomeCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	sender messageSender,
) error {
	if c.sendHomeControlCard(
		ctx,
		chatID,
		baseSessionID,
		sender,
	) {
		return nil
	}

	_ = sender.SendText(
		ctx,
		chatID,
		buildEnterChatWelcomeMessage(
			c.assistantDisplayNameForSession(baseSessionID),
			c.runtimeModelDisplayName(),
		)+"\n"+welcomeCommandUsage,
	)
	return nil
}

func (c *Channel) handleCronCommand(
	ctx context.Context,
	chatID string,
	userID string,
	fromID string,
	cmd parsedCommand,
	sender messageSender,
) error {
	manager, ok := c.gw.(scheduledJobManager)
	if !ok {
		_ = sender.SendText(
			ctx,
			chatID,
			"当前环境不支持定时任务管理。",
		)
		return nil
	}

	parsed, err := croncmd.Parse(cmd.rawArgs)
	if err != nil {
		_ = sender.SendText(ctx, chatID, cronCommandUsage)
		return nil
	}

	target, ok := buildDefaultDeliveryTarget(chatID, fromID)
	if !ok {
		_ = sender.SendText(
			ctx,
			chatID,
			"当前聊天没有可用的回投目标，"+
				"无法管理定时任务。",
		)
		return nil
	}

	switch parsed.Action {
	case croncmd.ActionHelp:
		if cardSender, ok := sender.(templateCardSender); ok &&
			cardSender != nil {
			_ = cardSender.SendTemplateCard(
				ctx,
				chatID,
				buildControlCronCard(
					c.assistantDisplayNameForSession(userID),
					nil,
					newInteractiveCardTaskID(
						controlCardTaskPrefix,
						userID,
					),
					"",
					"",
				),
			)
			return nil
		}
		_ = sender.SendText(ctx, chatID, cronCommandUsage)
		return nil
	case croncmd.ActionClear:
		return c.handleCronClearCommand(
			ctx,
			chatID,
			userID,
			target,
			manager,
			sender,
		)
	}

	jobs, err := manager.ListScheduledJobs(
		ctx,
		target.Channel,
		userID,
		target.Target,
	)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"读取定时任务失败："+err.Error(),
		)
		return nil
	}

	switch parsed.Action {
	case croncmd.ActionList:
		if cardSender, ok := sender.(templateCardSender); ok &&
			cardSender != nil {
			_ = cardSender.SendTemplateCard(
				ctx,
				chatID,
				buildControlCronCard(
					c.assistantDisplayNameForSession(userID),
					jobs,
					newInteractiveCardTaskID(
						controlCardTaskPrefix,
						userID,
					),
					"",
					"",
				),
			)
			return nil
		}
		_ = sender.SendText(
			ctx,
			chatID,
			formatCronJobList(jobs),
		)
		return nil
	case croncmd.ActionStatus:
		return c.handleCronStatusCommand(
			ctx,
			chatID,
			jobs,
			parsed.Selector,
			sender,
		)
	case croncmd.ActionStop, croncmd.ActionResume:
		return c.handleCronSetEnabledCommand(
			ctx,
			chatID,
			userID,
			target,
			manager,
			jobs,
			parsed.Action,
			parsed.Selector,
			sender,
		)
	case croncmd.ActionRemove:
		return c.handleCronRemoveCommand(
			ctx,
			chatID,
			userID,
			target,
			manager,
			jobs,
			parsed.Selector,
			sender,
		)
	default:
		_ = sender.SendText(ctx, chatID, cronCommandUsage)
		return nil
	}
}

func (c *Channel) handleCronClearCommand(
	ctx context.Context,
	chatID string,
	userID string,
	target delivery.Target,
	manager scheduledJobManager,
	sender messageSender,
) error {
	removed, err := manager.ClearScheduledJobs(
		ctx,
		target.Channel,
		userID,
		target.Target,
	)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"清空定时任务失败："+err.Error(),
		)
		return nil
	}
	if removed == 0 {
		_ = sender.SendText(
			ctx,
			chatID,
			"当前聊天没有可清空的定时任务。",
		)
		return nil
	}
	_ = sender.SendText(
		ctx,
		chatID,
		"✅ 已清空当前聊天的 "+
			strconv.Itoa(removed)+" 个定时任务。",
	)
	return nil
}

func (c *Channel) handleCronStatusCommand(
	ctx context.Context,
	chatID string,
	jobs []gwclient.ScheduledJobSummary,
	selector string,
	sender messageSender,
) error {
	job, err := croncmd.ResolveSelector(jobs, selector)
	if err != nil {
		_ = sender.SendText(ctx, chatID, cronCommandUsage)
		return nil
	}
	_ = sender.SendText(ctx, chatID, formatCronJobDetails(job))
	return nil
}

func (c *Channel) handleCronSetEnabledCommand(
	ctx context.Context,
	chatID string,
	userID string,
	target delivery.Target,
	manager scheduledJobManager,
	jobs []gwclient.ScheduledJobSummary,
	action string,
	selector string,
	sender messageSender,
) error {
	job, err := croncmd.ResolveSelector(jobs, selector)
	if err != nil {
		_ = sender.SendText(ctx, chatID, cronCommandUsage)
		return nil
	}

	enabled := action == croncmd.ActionResume
	if enabled && cronJobReachedMaxRuns(job) {
		_ = sender.SendText(
			ctx,
			chatID,
			cronResumeBlockedMessage(job),
		)
		return nil
	}
	jobTarget := resolveCronJobDeliveryTarget(target, job)
	updated, err := manager.SetScheduledJobEnabled(
		ctx,
		jobTarget.Channel,
		userID,
		jobTarget.Target,
		job.ID,
		enabled,
	)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"更新定时任务失败："+err.Error(),
		)
		return nil
	}

	text := "✅ 已停止定时任务：" + cronJobDisplayName(updated)
	if enabled {
		text = "✅ 已恢复定时任务：" +
			cronJobDisplayName(updated)
	}
	_ = sender.SendText(ctx, chatID, text)
	return nil
}

func (c *Channel) handleCronRemoveCommand(
	ctx context.Context,
	chatID string,
	userID string,
	target delivery.Target,
	manager scheduledJobManager,
	jobs []gwclient.ScheduledJobSummary,
	selector string,
	sender messageSender,
) error {
	job, err := croncmd.ResolveSelector(jobs, selector)
	if err != nil {
		_ = sender.SendText(ctx, chatID, cronCommandUsage)
		return nil
	}

	jobTarget := resolveCronJobDeliveryTarget(target, job)
	removed, err := manager.RemoveScheduledJob(
		ctx,
		jobTarget.Channel,
		userID,
		jobTarget.Target,
		job.ID,
	)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"删除定时任务失败："+err.Error(),
		)
		return nil
	}
	if !removed {
		_ = sender.SendText(ctx, chatID, cronCommandUsage)
		return nil
	}

	_ = sender.SendText(
		ctx,
		chatID,
		"✅ 已删除定时任务："+cronJobDisplayName(job),
	)
	return nil
}

// handleCancelCommand cancels the inflight request for a session.
func (c *Channel) handleCancelCommand(
	ctx context.Context,
	chatID string,
	sessionID string,
	sender messageSender,
) error {
	requestID := c.inflight.Get(sessionID)
	if strings.TrimSpace(requestID) == "" {
		_ = sender.SendText(ctx, chatID, c.cancelNoopMessage)
		return nil
	}

	canceled, err := c.gw.Cancel(ctx, requestID)
	if err != nil {
		log.WarnfContext(ctx, "wecom: cancel: %v", err)
		_ = sender.SendText(ctx, chatID, c.cancelFailedMessage)
		return nil
	}
	if !canceled {
		_ = sender.SendText(ctx, chatID, c.cancelNoopMessage)
		return nil
	}

	c.runStatus.cancel(sessionID, requestID)
	_ = sender.SendText(ctx, chatID, c.cancelOKMessage)
	return nil
}

func (c *Channel) handleStatusCommand(
	ctx context.Context,
	chatID string,
	sessionInfo *sessionInfo,
	sender messageSender,
) error {
	if sessionInfo != nil {
		if cardSender, ok := sender.(templateCardSender); ok &&
			cardSender != nil {
			_ = cardSender.SendTemplateCard(
				ctx,
				chatID,
				buildControlStatusCard(
					c.assistantDisplayNameForInfo(sessionInfo),
					c.statusMessageText(
						sessionInfo,
						c.runStatus.snapshot(
							sessionInfo.sessionID,
						),
						c.effectiveWorkspacePath(sessionInfo),
					),
					newInteractiveCardTaskID(
						controlCardTaskPrefix,
						sessionInfo.baseSessionID,
					),
				),
			)
			return nil
		}
	}

	if sessionInfo == nil {
		_ = sender.SendText(
			ctx,
			chatID,
			statusLabelState+statusLineIdle,
		)
		return nil
	}

	snapshot := c.runStatus.snapshot(sessionInfo.sessionID)
	_ = sender.SendText(
		ctx,
		chatID,
		c.statusMessageText(
			sessionInfo,
			snapshot,
			c.effectiveWorkspacePath(sessionInfo),
		),
	)
	return nil
}

func (c *Channel) handleRecallCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	sender messageSender,
) error {
	sessionInfo, ok := c.sessionTracker.recallPreviousSession(
		baseSessionID,
	)
	if !ok {
		_ = sender.SendText(ctx, chatID, defaultRecallNoopMessage)
		return nil
	}

	message := defaultRecallMessage
	snapshot := c.runStatus.snapshot(sessionInfo.sessionID)
	if !snapshot.empty() {
		message += "\n" + c.statusMessageText(
			sessionInfo,
			snapshot,
			c.effectiveWorkspacePath(sessionInfo),
		)
	}
	_ = sender.SendText(ctx, chatID, message)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

func (c *Channel) handleSessionCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	sender messageSender,
) error {
	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	_ = sender.SendText(
		ctx,
		chatID,
		c.formatSessionOverview(
			sessionInfo,
			c.sessionTimeout,
			c.effectiveWorkspacePath(sessionInfo),
		),
	)
	return nil
}

func (c *Channel) handleSessionsCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	args []string,
	sender messageSender,
) error {
	if len(args) == 0 {
		if cardSender, ok := sender.(templateCardSender); ok &&
			cardSender != nil {
			sessionInfo := c.sessionTracker.getOrCreateSession(
				baseSessionID,
				0,
			)
			card := buildControlSessionsCard(
				c.assistantDisplayNameForSession(
					baseSessionID,
				),
				c.assistantNameStateForInfo(sessionInfo),
				sessionInfo,
				c.sessionTimeout,
				c.effectiveWorkspacePath(sessionInfo),
				newInteractiveCardTaskID(
					controlCardTaskPrefix,
					baseSessionID,
				),
				"",
			)
			if err := cardSender.SendTemplateCard(
				ctx,
				chatID,
				card,
			); err != nil {
				log.WarnfContext(
					ctx,
					"wecom: send sessions control card failed: %v",
					err,
				)
				return nil
			}
			c.rememberSessionCard(
				baseSessionID,
				sessionCardViewSessions,
				card.TaskID,
				sessionInfo,
			)
			return nil
		}
	}

	limit, usage := parseSessionListLimit(args)
	if usage != "" {
		_ = sender.SendText(ctx, chatID, usage)
		return nil
	}

	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	_ = sender.SendText(
		ctx,
		chatID,
		formatSessionList(sessionInfo, limit),
	)
	return nil
}

func (c *Channel) handleSwitchCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	args []string,
	sender messageSender,
) error {
	index, usage := parseSwitchIndex(args)
	if usage != "" {
		_ = sender.SendText(ctx, chatID, usage)
		return nil
	}

	current := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	if index > len(current.history) {
		_ = sender.SendText(
			ctx,
			chatID,
			"没有找到对应会话。\n"+sessionsCommandUsage,
		)
		return nil
	}

	target := current.history[index-1]
	if target.SessionID == current.sessionID {
		_ = sender.SendText(
			ctx,
			chatID,
			"当前已经在这个会话中。\n"+
				c.formatSessionOverview(
					current,
					c.sessionTimeout,
					c.effectiveWorkspacePath(current),
				),
		)
		return nil
	}

	sessionInfo, ok := c.sessionTracker.switchSession(
		baseSessionID,
		target.SessionID,
	)
	if !ok {
		_ = sender.SendText(
			ctx,
			chatID,
			"切换会话失败，请先发送 "+
				sessionsKeyword+" 查看列表。",
		)
		return nil
	}

	message := "✅ 已切换到第 " + strconv.Itoa(index) +
		" 个会话。\n" +
		c.formatSessionOverview(
			sessionInfo,
			c.sessionTimeout,
			c.effectiveWorkspacePath(sessionInfo),
		)
	_ = sender.SendText(ctx, chatID, message)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

func (c *Channel) handlePersonaCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	cmd parsedCommand,
	sender messageSender,
) error {
	if len(cmd.args) == 0 {
		sessionInfo := c.sessionTracker.getOrCreateSession(
			baseSessionID,
			0,
		)
		if err := c.sendPersonaCard(
			ctx,
			chatID,
			baseSessionID,
			sessionInfo,
			personaCardViewDefault,
			sender,
		); err == nil {
			return nil
		}
		_ = sender.SendText(
			ctx,
			chatID,
			c.formatPersonaStatus(sessionInfo),
		)
		return nil
	}

	action := strings.ToLower(strings.TrimSpace(cmd.args[0]))
	switch action {
	case personaActionList:
		return c.handlePersonaListCommand(
			ctx,
			chatID,
			baseSessionID,
			sender,
		)
	case personaActionShow:
		return c.handlePersonaShowCommand(
			ctx,
			chatID,
			baseSessionID,
			cmd.args[1:],
			sender,
		)
	case personaActionUse:
		return c.handlePersonaUseCommand(
			ctx,
			chatID,
			baseSessionID,
			cmd.args[1:],
			sender,
		)
	case personaActionSave:
		return c.handlePersonaSaveCommand(
			ctx,
			chatID,
			baseSessionID,
			cmd.rawArgs,
			sender,
		)
	case personaActionDelete:
		return c.handlePersonaDeleteCommand(
			ctx,
			chatID,
			baseSessionID,
			cmd.args[1:],
			sender,
		)
	default:
		return c.handlePersonaResolveCommand(
			ctx,
			chatID,
			baseSessionID,
			cmd.args,
			sender,
		)
	}
}

func (c *Channel) handlePersonaListCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	sender messageSender,
) error {
	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	defs, err := c.listPersonas()
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"读取人格列表失败："+err.Error(),
		)
		return nil
	}
	_ = sender.SendText(
		ctx,
		chatID,
		c.formatPersonaList(sessionInfo, defs),
	)
	return nil
}

func (c *Channel) handlePersonaShowCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	args []string,
	sender messageSender,
) error {
	target := strings.TrimSpace(strings.Join(args, " "))
	if target == "" {
		_ = sender.SendText(ctx, chatID, personaShowUsage)
		return nil
	}
	def, ok, err := c.lookupPersona(target)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"读取人格失败："+err.Error(),
		)
		return nil
	}
	if !ok {
		_ = sender.SendText(
			ctx,
			chatID,
			"找不到人格："+target+"\n"+
				personaListHelpLine,
		)
		return nil
	}
	current := c.sessionTracker.getOrCreateSession(baseSessionID, 0)
	_ = sender.SendText(
		ctx,
		chatID,
		c.formatPersonaDetails(current, def),
	)
	return nil
}

func (c *Channel) handlePersonaUseCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	args []string,
	sender messageSender,
) error {
	target := strings.TrimSpace(strings.Join(args, " "))
	if target == "" {
		_ = sender.SendText(ctx, chatID, personaUseUsage)
		return nil
	}
	def, ok, err := c.lookupPersona(target)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"切换人格失败："+err.Error(),
		)
		return nil
	}
	if !ok {
		_ = sender.SendText(
			ctx,
			chatID,
			"未知人格："+target+"\n"+
				personaListHelpLine+"\n"+
				personaDirectCreateHelpLine,
		)
		return nil
	}
	sessionInfo := c.sessionTracker.setPersona(baseSessionID, def.ID)
	_ = sender.SendText(
		ctx,
		chatID,
		c.formatPersonaChanged(sessionInfo, def),
	)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

func (c *Channel) handlePersonaResolveCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	args []string,
	sender messageSender,
) error {
	target := strings.TrimSpace(strings.Join(args, " "))
	if target == "" {
		_ = sender.SendText(ctx, chatID, personaUseUsage)
		return nil
	}
	def, ok, err := c.lookupPersona(target)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"切换人格失败："+err.Error(),
		)
		return nil
	}
	if ok {
		sessionInfo := c.sessionTracker.setPersona(
			baseSessionID,
			def.ID,
		)
		_ = sender.SendText(
			ctx,
			chatID,
			c.formatPersonaChanged(sessionInfo, def),
		)
		c.syncActiveSessionCard(
			ctx,
			baseSessionID,
			sessionInfo,
			sender,
		)
		return nil
	}
	return c.handlePersonaCreateCommand(
		ctx,
		chatID,
		baseSessionID,
		target,
		sender,
	)
}

func (c *Channel) handlePersonaSaveCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	rawArgs string,
	sender messageSender,
) error {
	name, prompt, err := parsePersonaSaveInput(rawArgs)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			err.Error()+"\n"+personaSaveUsage,
		)
		return nil
	}
	def, err := c.savePersona(name, prompt)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"保存人格失败："+err.Error(),
		)
		return nil
	}
	sessionInfo := c.sessionTracker.setPersona(baseSessionID, def.ID)
	_ = sender.SendText(
		ctx,
		chatID,
		c.formatPersonaSaved(sessionInfo, def),
	)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

func (c *Channel) handlePersonaDeleteCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	args []string,
	sender messageSender,
) error {
	target := strings.TrimSpace(strings.Join(args, " "))
	if target == "" {
		_ = sender.SendText(ctx, chatID, personaDeleteUsage)
		return nil
	}
	def, ok, err := c.lookupPersona(target)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"删除人格失败："+err.Error(),
		)
		return nil
	}
	if !ok {
		_ = sender.SendText(
			ctx,
			chatID,
			"删除人格失败：找不到 "+target,
		)
		return nil
	}
	if def.BuiltIn {
		_ = sender.SendText(
			ctx,
			chatID,
			"删除人格失败：内置人格不能删除。",
		)
		return nil
	}
	if err := c.deletePersona(def.ID); err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"删除人格失败："+err.Error(),
		)
		return nil
	}
	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	if sessionInfo != nil &&
		sessionInfo.personaPinned &&
		sessionInfo.effectivePersonaID() == def.ID {
		sessionInfo = c.sessionTracker.clearPersona(baseSessionID)
	}
	_ = sender.SendText(
		ctx,
		chatID,
		c.formatPersonaDeleted(sessionInfo, def.Name),
	)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

func (c *Channel) handlePersonaCreateCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	prompt string,
	sender messageSender,
) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		_ = sender.SendText(
			ctx,
			chatID,
			personaDirectCreateHelpLine,
		)
		return nil
	}
	def, err := c.createPersona(prompt)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"新增人格失败："+err.Error(),
		)
		return nil
	}
	sessionInfo := c.sessionTracker.setPersona(baseSessionID, def.ID)
	_ = sender.SendText(
		ctx,
		chatID,
		c.formatPersonaSaved(sessionInfo, def),
	)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

func parsePersonaSaveInput(raw string) (string, string, error) {
	fields, remainder := splitLeadingFields(raw, 2)
	if len(fields) < 2 {
		return "", "", fmt.Errorf("缺少人格名称或设定")
	}
	name, err := personaapi.ValidateName(fields[1])
	if err != nil {
		return "", "", err
	}
	prompt := strings.TrimSpace(remainder)
	if prompt == "" {
		return "", "", fmt.Errorf("人格设定不能为空")
	}
	return name, prompt, nil
}

func splitLeadingFields(
	raw string,
	count int,
) ([]string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || count <= 0 {
		return nil, ""
	}
	fields := make([]string, 0, count)
	index := 0
	for len(fields) < count {
		for index < len(raw) && isCommandSpace(rune(raw[index])) {
			index++
		}
		if index >= len(raw) {
			return fields, ""
		}
		start := index
		for index < len(raw) && !isCommandSpace(rune(raw[index])) {
			index++
		}
		fields = append(fields, raw[start:index])
	}
	return fields, strings.TrimSpace(raw[index:])
}

func isCommandSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func (c *Channel) listPersonas() ([]personaapi.Definition, error) {
	if c == nil || c.personas == nil {
		return personaapi.Builtins(), nil
	}
	return c.personas.List()
}

func (c *Channel) lookupPersona(
	raw string,
) (personaapi.Definition, bool, error) {
	if c == nil || c.personas == nil {
		def, ok := personaapi.LookupBuiltin(raw)
		return def, ok, nil
	}
	return c.personas.Get(raw)
}

func (c *Channel) savePersona(
	name string,
	prompt string,
) (personaapi.Definition, error) {
	if c == nil || c.personas == nil {
		return personaapi.Definition{}, fmt.Errorf(
			"人格存储未启用",
		)
	}
	return c.personas.Save(name, prompt)
}

func (c *Channel) createPersona(
	prompt string,
) (personaapi.Definition, error) {
	if c == nil || c.personas == nil {
		return personaapi.Definition{}, fmt.Errorf(
			"人格存储未启用",
		)
	}
	return c.personas.Create(prompt)
}

func (c *Channel) deletePersona(id string) error {
	if c == nil || c.personas == nil {
		return fmt.Errorf("人格存储未启用")
	}
	return c.personas.Delete(id)
}

func (c *Channel) sendPersonaCard(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	info *sessionInfo,
	view string,
	sender messageSender,
) error {
	cardSender, ok := sender.(interactiveTemplateCardSender)
	if !ok || cardSender == nil {
		return fmt.Errorf("interactive card sender unavailable")
	}
	defs, err := c.listPersonas()
	if err != nil {
		return err
	}
	card := buildPersonaSettingsCard(
		c.assistantDisplayNameForSession(
			baseSessionID,
		),
		c.activePersonaDisplay(info),
		info,
		defs,
		newInteractiveCardTaskID(
			personaCardTaskPrefix,
			baseSessionID,
		),
		view,
		"",
		c.personaStorageEnabled(),
	)
	if err := cardSender.SendTemplateCard(
		ctx,
		chatID,
		card,
	); err != nil {
		return err
	}
	c.rememberSessionCardWithVariant(
		baseSessionID,
		sessionCardViewPersona,
		view,
		card.TaskID,
		info,
	)
	return nil
}

func (c *Channel) handleWorkspaceCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	args []string,
	sender messageSender,
) error {
	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	defaultWorkspace := c.effectiveWorkspacePath(nil)
	if len(args) == 0 {
		if cardSender, ok := sender.(templateCardSender); ok &&
			cardSender != nil {
			card := buildControlWorkspaceCard(
				c.assistantDisplayNameForSession(
					baseSessionID,
				),
				sessionInfo.workspacePath,
				defaultWorkspace,
				newInteractiveCardTaskID(
					controlCardTaskPrefix,
					baseSessionID,
				),
				"",
			)
			if err := cardSender.SendTemplateCard(
				ctx,
				chatID,
				card,
			); err != nil {
				log.WarnfContext(
					ctx,
					"wecom: send workspace control card failed: %v",
					err,
				)
				return nil
			}
			c.rememberSessionCard(
				baseSessionID,
				sessionCardViewWorkspace,
				card.TaskID,
				sessionInfo,
			)
			return nil
		}
		_ = sender.SendText(
			ctx,
			chatID,
			formatWorkspaceStatus(
				sessionInfo.workspacePath,
				defaultWorkspace,
			),
		)
		return nil
	}

	action := strings.TrimSpace(args[0])
	if isWorkspaceResetToken(action) {
		sessionInfo = c.sessionTracker.setWorkspace(
			baseSessionID,
			"",
		)
		_ = sender.SendText(
			ctx,
			chatID,
			formatWorkspaceChanged(
				sessionInfo.workspacePath,
				defaultWorkspace,
			),
		)
		c.syncActiveSessionCard(
			ctx,
			baseSessionID,
			sessionInfo,
			sender,
		)
		return nil
	}

	workspacePath, err := normalizeWorkspacePath(
		strings.Join(args, " "),
		true,
	)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			"代码工作区无效："+err.Error()+"\n"+
				workspaceCommandUsage,
		)
		return nil
	}

	sessionInfo = c.sessionTracker.setWorkspace(
		baseSessionID,
		workspacePath,
	)
	_ = sender.SendText(
		ctx,
		chatID,
		formatWorkspaceChanged(
			sessionInfo.workspacePath,
			defaultWorkspace,
		),
	)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

func (c *Channel) handleNameCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	rawArgs string,
	sender messageSender,
) error {
	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	rawArgs = strings.TrimSpace(rawArgs)
	if rawArgs == "" {
		nameState := c.assistantNameStateForInfo(sessionInfo)
		_ = sender.SendText(
			ctx,
			chatID,
			strings.Join([]string{
				statusLabelAssistant +
					formatAssistantNameSummary(nameState),
				nameStatusChatLabel + displayConfiguredName(
					nameState.ChatOverride,
				),
				nameStatusGlobalLabel + displayConfiguredName(
					nameState.GlobalName,
				),
				"",
				nameStatusRulesTitle,
				nameStatusRuleOrder,
				nameStatusRulePersist,
				nameStatusRuleGlobal,
				"",
				nameStatusExamplesTitle,
				nameStatusGlobalExampleOne,
				nameStatusGlobalExampleTwo,
				"",
				nameCommandUsage,
			}, "\n"),
		)
		return nil
	}

	scope, value := parseNameCommandScope(rawArgs)
	if assistantname.IsResetToken(value) {
		if scope == nameScopeGlobal {
			if err := c.saveGlobalAssistantName(""); err != nil {
				return err
			}
			_ = sender.SendText(
				ctx,
				chatID,
				strings.Join([]string{
					"✅ 已清除默认名字，当前回退为：" +
						c.assistantDisplayName(),
					"其他私聊或群聊如果没有自己的当前聊天名字，" +
						"也会一起回退到这个默认名字。",
				}, "\n"),
			)
			return nil
		}
		sessionInfo = c.sessionTracker.setAssistantAlias(
			baseSessionID,
			"",
		)
		_ = sender.SendText(
			ctx,
			chatID,
			"✅ 已恢复当前聊天的默认称呼："+
				c.assistantDisplayNameForInfo(sessionInfo),
		)
		c.syncActiveSessionCard(
			ctx,
			baseSessionID,
			sessionInfo,
			sender,
		)
		return nil
	}

	name := normalizeAssistantAlias(value)
	if name == "" {
		_ = sender.SendText(
			ctx,
			chatID,
			nameCommandUsage,
		)
		return nil
	}

	if scope == nameScopeGlobal {
		if err := c.saveGlobalAssistantName(name); err != nil {
			return err
		}
		_ = sender.SendText(
			ctx,
			chatID,
			strings.Join([]string{
				"✅ 已更新默认名字：" +
					c.assistantDisplayName(),
				"它会影响其他用户的新私聊、其他新群聊，" +
					"以及任何还没有单独改名的现有聊天。",
				"如果某个聊天已经设置了自己的当前聊天名字，" +
					"那边不会被这次修改改掉。",
			}, "\n"),
		)
		c.syncActiveSessionCard(
			ctx,
			baseSessionID,
			c.sessionTracker.getOrCreateSession(
				baseSessionID,
				0,
			),
			sender,
		)
		return nil
	}

	sessionInfo = c.sessionTracker.setAssistantAlias(
		baseSessionID,
		name,
	)
	_ = sender.SendText(
		ctx,
		chatID,
		"✅ 已记住当前聊天里的称呼："+
			c.assistantDisplayNameForInfo(sessionInfo),
	)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

func parseNameCommandScope(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", ""
	}
	if !strings.EqualFold(fields[0], nameScopeGlobal) {
		return "", raw
	}
	value := strings.TrimSpace(strings.TrimPrefix(raw, fields[0]))
	return nameScopeGlobal, value
}

func displayConfiguredName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nameStatusUnset
	}
	return name
}

func formatAssistantNameSummary(
	state assistantNameState,
) string {
	name := displayConfiguredName(state.EffectiveName)
	source := strings.TrimSpace(state.SourceLabel)
	if source == "" {
		return name
	}
	return name + "（" + source + "）"
}

func formatCronJobList(
	jobs []gwclient.ScheduledJobSummary,
) string {
	if len(jobs) == 0 {
		return "当前聊天还没有定时任务。\n" + cronCommandUsage
	}

	lines := []string{"当前聊天的定时任务："}
	for i, job := range jobs {
		lines = append(
			lines,
			formatCronJobLine(i+1, job),
		)
	}
	lines = append(lines, cronCommandUsage)
	return strings.Join(lines, "\n")
}

func formatCronJobLine(
	index int,
	job gwclient.ScheduledJobSummary,
) string {
	parts := []string{
		strconv.Itoa(index) + ". " + cronJobDisplayName(job),
	}
	if schedule := strings.TrimSpace(job.Schedule); schedule != "" {
		parts = append(parts, schedule)
	}
	if runCount := formatCronRunCount(job); runCount != "" {
		parts = append(parts, cronRunCountList+runCount)
	}
	parts = append(
		parts,
		"启用="+formatCronEnabled(job.Enabled),
	)
	if status := strings.TrimSpace(job.LastStatus); status != "" {
		parts = append(parts, "状态="+status)
	}
	if text := strings.TrimSpace(job.LastError); text != "" {
		parts = append(parts, "错误="+trimCronText(text))
	}
	if job.NextRunAt != nil && !job.NextRunAt.IsZero() {
		parts = append(
			parts,
			"下次="+
				job.NextRunAt.Local().Format(
					cronStatusTimeLayout,
				),
		)
	}
	parts = append(
		parts,
		"ID="+croncmd.ShortID(job.ID),
	)
	return strings.Join(parts, " · ")
}

func formatCronJobDetails(
	job gwclient.ScheduledJobSummary,
) string {
	lines := []string{
		"定时任务详情：",
		"名称：" + cronJobDisplayName(job),
		"ID：" + strings.TrimSpace(job.ID),
		"计划：" + valueOrFallback(job.Schedule, "-"),
		"启用：" + formatCronEnabled(job.Enabled),
		"最近状态：" + valueOrFallback(job.LastStatus, "-"),
	}
	if runCount := formatCronRunCount(job); runCount != "" {
		lines = append(lines, cronRunCountLabel+runCount)
	}
	if job.NextRunAt != nil && !job.NextRunAt.IsZero() {
		lines = append(
			lines,
			"下次执行："+job.NextRunAt.Local().Format(
				cronStatusTimeLayout,
			),
		)
	}
	if text := strings.TrimSpace(job.LastError); text != "" {
		lines = append(lines, "最近错误："+text)
	}
	if cronJobReachedMaxRuns(job) {
		lines = append(lines, cronMaxRunsHint)
	}
	return strings.Join(lines, "\n")
}

func cronJobDisplayName(job gwclient.ScheduledJobSummary) string {
	name := strings.TrimSpace(job.Name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(job.ID)
}

func formatCronEnabled(enabled bool) string {
	if enabled {
		return "是"
	}
	return "否"
}

func trimCronText(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= cronTextPreviewRunes {
		return string(runes)
	}
	return string(runes[:cronTextPreviewRunes]) + "..."
}

func formatCronRunCount(
	job gwclient.ScheduledJobSummary,
) string {
	switch {
	case job.MaxRuns > 0:
		return strconv.Itoa(job.RunCount) + "/" +
			strconv.Itoa(job.MaxRuns)
	case job.RunCount > 0:
		return strconv.Itoa(job.RunCount)
	default:
		return ""
	}
}

func cronJobReachedMaxRuns(
	job gwclient.ScheduledJobSummary,
) bool {
	return job.MaxRuns > 0 && job.RunCount >= job.MaxRuns
}

func cronResumeBlockedMessage(
	job gwclient.ScheduledJobSummary,
) string {
	return fmt.Sprintf(
		cronMaxRunsBlockedFormat,
		valueOrFallback(formatCronRunCount(job), "-"),
	)
}

func resolveCronJobDeliveryTarget(
	fallback delivery.Target,
	job gwclient.ScheduledJobSummary,
) delivery.Target {
	channel := strings.TrimSpace(job.DeliveryChannel)
	target := strings.TrimSpace(job.DeliveryTarget)
	if target == "" {
		return fallback
	}
	if channel == "" {
		channel = strings.TrimSpace(fallback.Channel)
	}
	if channel == "" {
		return fallback
	}
	return delivery.Target{
		Channel: channel,
		Target:  target,
	}
}

func valueOrFallback(text string, fallback string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func parseSessionListLimit(args []string) (int, string) {
	if len(args) == 0 {
		return sessionListDefaultLimit, ""
	}
	if len(args) > 1 {
		return 0, sessionsCommandUsage
	}

	limit, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || limit <= 0 {
		return 0, sessionsCommandUsage
	}
	if limit > sessionListMaxLimit {
		limit = sessionListMaxLimit
	}
	return limit, ""
}

func parseSwitchIndex(args []string) (int, string) {
	if len(args) != 1 {
		return 0, switchCommandUsage
	}

	index, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || index <= 0 {
		return 0, switchCommandUsage
	}
	return index, ""
}

func isWorkspaceResetToken(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "off", "clear", "default", "reset":
		return true
	default:
		return false
	}
}

func normalizeAssistantAlias(raw string) string {
	return assistantname.Normalize(raw)
}

func (c *Channel) formatSessionOverview(
	info *sessionInfo,
	timeout time.Duration,
	workspace string,
) string {
	if info == nil {
		return statusLabelSession + "默认会话\n" +
			statusLabelAssistant +
			formatAssistantNameSummary(
				c.assistantNameStateForInfo(nil),
			) + "\n" +
			statusLabelPersona +
			c.personaDisplay(defaultChatPersonaID) + "\n" +
			statusLabelWorkspace + workspaceDisplayUnset + "\n" +
			statusLabelTimeout + formatTimeoutSetting(timeout)
	}

	lines := []string{
		statusLabelSession + sessionDisplayLabel(
			info.baseSessionID,
			info.sessionID,
		),
		statusLabelAssistant + formatAssistantNameSummary(
			c.assistantNameStateForInfo(info),
		),
		statusLabelPersona + c.activePersonaDisplay(info),
		statusLabelWorkspace + formatWorkspaceDisplay(
			info.workspacePath,
			workspace,
		),
		statusLabelTimeout + formatTimeoutSetting(timeout),
		statusLabelHistory + strconv.Itoa(len(info.history)),
		statusHintSessions,
		statusHintSubagents,
		statusHintCron,
		statusHintRuntime,
		statusHintAssistant,
		statusHintPersona,
		statusHintWorkspace,
	}
	return strings.Join(lines, "\n")
}

func formatSessionList(
	info *sessionInfo,
	limit int,
) string {
	if info == nil || len(info.history) == 0 {
		return "当前还没有历史会话。\n" + newKeyword +
			" 可立即开始一个新会话。"
	}

	if limit <= 0 {
		limit = sessionListDefaultLimit
	}
	if limit > len(info.history) {
		limit = len(info.history)
	}

	lines := []string{"最近会话（1=当前）："}
	for i := 0; i < limit; i++ {
		entry := info.history[i]
		line := strconv.Itoa(i+1) + ". "
		if entry.SessionID == info.sessionID {
			line += "当前 · "
		}
		line += sessionDisplayLabel(
			info.baseSessionID,
			entry.SessionID,
		)
		if relative := formatRelativeTime(entry.LastActivity); relative != "" {
			line += " · " + relative
		}
		lines = append(lines, line)
	}
	lines = append(lines, switchCommandUsage)
	return strings.Join(lines, "\n")
}

func (c *Channel) formatPersonaList(
	info *sessionInfo,
	defs []personaapi.Definition,
) string {
	lines := []string{
		statusLabelPersona + c.activePersonaDisplay(info),
		"可用人格：",
	}
	for _, def := range defs {
		summary := strings.TrimSpace(def.Summary)
		if summary == "" {
			if def.BuiltIn {
				summary = "内置"
			} else {
				summary = "自定义"
			}
		}
		lines = append(
			lines,
			"- "+def.Name+"（"+def.ID+"） · "+summary,
		)
	}
	lines = append(
		lines,
		personaUseUsage,
		"只切换已存在人格："+personaKeyword+
			" "+personaActionUse+" <名称>",
		personaDirectCreateHelpLine,
		personaShowUsage,
		personaSaveUsage,
		personaDeleteUsage,
	)
	return strings.Join(lines, "\n")
}

func (c *Channel) formatPersonaStatus(info *sessionInfo) string {
	lines := []string{
		statusLabelPersona + c.activePersonaDisplay(info),
		personaListHelpLine,
		personaDirectCreateHelpLine,
		personaSaveHelpLine,
	}
	if info != nil && strings.TrimSpace(info.personaID) != "" {
		lines = append(
			lines,
			"切换已有："+personaKeyword+" <名称>",
			"严格切换："+personaKeyword+" "+
				personaActionUse+" <名称>",
		)
	}
	return strings.Join(lines, "\n")
}

func (c *Channel) formatPersonaChanged(
	info *sessionInfo,
	def personaapi.Definition,
) string {
	if info == nil {
		return "✅ 已更新当前聊天的人格。"
	}
	return "✅ 当前聊天人格已切换为 " +
		c.personaDisplay(def.ID) + "。\n" +
		personaListHelpLine
}

func (c *Channel) formatPersonaSaved(
	info *sessionInfo,
	def personaapi.Definition,
) string {
	lines := []string{
		"✅ 已保存并启用人格：" + c.personaDisplay(def.ID),
		"名称：" + def.Name,
		"命令名：" + def.ID,
		"摘要：" + defaultString(def.Summary, "无"),
		personaShowUsage,
	}
	return strings.Join(lines, "\n")
}

func (c *Channel) formatPersonaDeleted(
	info *sessionInfo,
	id string,
) string {
	lines := []string{
		"✅ 已删除自定义人格：" + strings.TrimSpace(id),
		personaListHelpLine,
	}
	if info != nil {
		lines = append(
			lines,
			statusLabelPersona+c.activePersonaDisplay(info),
		)
	}
	return strings.Join(lines, "\n")
}

func (c *Channel) formatPersonaDetails(
	info *sessionInfo,
	def personaapi.Definition,
) string {
	source := "自定义"
	if def.BuiltIn {
		source = "内置"
	}
	lines := []string{
		"人格：" + def.Name,
		"命令名：" + def.ID,
		"来源：" + source,
		"摘要：" + defaultString(def.Summary, "无"),
		"",
		strings.TrimSpace(def.Prompt),
	}
	if info != nil {
		lines = append(
			lines,
			"",
			"当前聊天人格："+c.activePersonaDisplay(info),
		)
	}
	return strings.Join(lines, "\n")
}

func formatWorkspaceStatus(
	custom string,
	fallback string,
) string {
	return statusLabelWorkspace + formatWorkspaceDisplay(
		custom,
		fallback,
	) + "\n" + statusHintWorkspace
}

func formatWorkspaceChanged(
	custom string,
	fallback string,
) string {
	if strings.TrimSpace(custom) == "" {
		display := formatWorkspaceDisplay("", fallback)
		if display == workspaceDisplayUnset {
			return "✅ 已清除当前聊天的代码工作区。\n" +
				statusHintWorkspace
		}
		return "✅ 已恢复默认代码工作区：\n" +
			display + "\n" + statusHintWorkspace
	}
	return "✅ 当前聊天的代码工作区已设置为：\n" +
		formatWorkspaceDisplay(custom, fallback) + "\n" +
		statusHintWorkspace
}

func (c *Channel) personaDisplay(personaID string) string {
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		personaID = defaultChatPersonaID
	}
	if c != nil && c.personas != nil {
		def, ok, err := c.personas.Get(personaID)
		if err == nil && ok {
			if strings.TrimSpace(def.Name) == "" {
				return def.ID
			}
			return def.Name + "（" + def.ID + "）"
		}
	}
	return personaID
}

func (c *Channel) activePersonaDisplay(info *sessionInfo) string {
	if info == nil {
		return c.personaDisplay(defaultChatPersonaID)
	}
	return c.personaDisplay(info.effectivePersonaID())
}

func formatTimeoutSetting(timeout time.Duration) string {
	if timeout <= 0 {
		return "关闭"
	}
	return timeout.String()
}

func sessionDisplayLabel(
	baseSessionID string,
	sessionID string,
) string {
	baseSessionID = strings.TrimSpace(baseSessionID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || sessionID == baseSessionID {
		return "默认会话"
	}

	prefix := baseSessionID + ":"
	if strings.HasPrefix(sessionID, prefix) {
		epoch, err := strconv.ParseInt(
			strings.TrimPrefix(sessionID, prefix),
			10,
			64,
		)
		if err == nil && epoch > 0 {
			return "分会话@" +
				time.Unix(epoch, 0).Format("01-02 15:04")
		}
	}
	return sessionID
}

func formatRelativeTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}

	elapsed := time.Since(ts)
	switch {
	case elapsed < time.Minute:
		return "刚刚"
	case elapsed < time.Hour:
		return strconv.FormatInt(
			int64(elapsed/time.Minute),
			10,
		) + "m前"
	case elapsed < 24*time.Hour:
		return strconv.FormatInt(
			int64(elapsed/time.Hour),
			10,
		) + "h前"
	case elapsed < 7*24*time.Hour:
		return strconv.FormatInt(
			int64(elapsed/(24*time.Hour)),
			10,
		) + "d前"
	default:
		return ts.Format("01-02 15:04")
	}
}

// --- Concurrency helpers (same as Telegram, with nil safety) ---

type inflightRequests struct {
	mu sync.Mutex
	m  map[string]string
}

func newInflightRequests() *inflightRequests {
	return &inflightRequests{m: make(map[string]string)}
}

func (r *inflightRequests) Get(sessionID string) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.m[sessionID]
}

func (r *inflightRequests) Set(sessionID, requestID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[sessionID] = requestID
}

func (r *inflightRequests) Clear(sessionID, requestID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m[sessionID] == requestID {
		delete(r.m, sessionID)
	}
}

type laneLocker struct {
	mu    sync.Mutex
	lanes map[string]*laneEntry
}

type laneEntry struct {
	lock sync.Mutex
	refs int
}

func newLaneLocker() *laneLocker {
	return &laneLocker{lanes: make(map[string]*laneEntry)}
}

func (l *laneLocker) withLockErrNotify(
	key string,
	onWait func(),
	fn func() error,
) error {
	if fn == nil {
		return nil
	}
	var err error
	l.withLockNotify(key, onWait, func() { err = fn() })
	return err
}

func (l *laneLocker) withLock(key string, fn func()) {
	l.withLockNotify(key, nil, fn)
}

func (l *laneLocker) withLockNotify(
	key string,
	onWait func(),
	fn func(),
) {
	if l == nil {
		fn()
		return
	}
	entry, waited := l.acquire(key)
	if waited && onWait != nil {
		onWait()
	}
	entry.lock.Lock()
	defer func() {
		entry.lock.Unlock()
		l.release(key, entry)
	}()
	fn()
}

func (l *laneLocker) acquire(key string) (*laneEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.lanes[key]
	if ok {
		entry.refs++
		return entry, true
	}
	entry = &laneEntry{refs: 1}
	l.lanes[key] = entry
	return entry, false
}

func (l *laneLocker) release(key string, entry *laneEntry) {
	if l == nil || entry == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.lanes[key]
	if !ok || current != entry {
		return
	}
	entry.refs--
	if entry.refs <= 0 {
		delete(l.lanes, key)
	}
}

type requestRunStatus struct {
	requestID    string
	state        string
	stage        string
	summary      string
	preview      string
	contextUsage *contextUsageStatus
	startedAt    time.Time
	updatedAt    time.Time
	completedAt  time.Time
	elapsed      time.Duration
}

type sessionRunState struct {
	active *requestRunStatus
	queued *requestRunStatus
	last   *requestRunStatus
}

type sessionRunSnapshot struct {
	active *requestRunStatus
	queued *requestRunStatus
	last   *requestRunStatus
}

func (s sessionRunSnapshot) empty() bool {
	return s.active == nil && s.queued == nil && s.last == nil
}

type runStatusTracker struct {
	mu       sync.RWMutex
	sessions map[string]*sessionRunState
}

func newRunStatusTracker() *runStatusTracker {
	return &runStatusTracker{
		sessions: make(map[string]*sessionRunState),
	}
}

func (t *runStatusTracker) queue(
	sessionID string,
	requestID string,
	summary string,
) {
	if t == nil || strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(requestID) == "" {
		return
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	session := t.ensureSessionLocked(sessionID)
	if session.active != nil &&
		session.active.requestID == requestID {
		return
	}
	status := session.ensureQueued(requestID, now)
	status.state = runStateQueued
	status.summary = normalizeStatusSummary(summary)
	status.updatedAt = now
}

func (t *runStatusTracker) start(
	sessionID string,
	requestID string,
	summary string,
) {
	if t == nil || strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(requestID) == "" {
		return
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	session := t.ensureSessionLocked(sessionID)
	status := session.ensureActive(requestID, now)
	status.state = runStateRunning
	status.summary = normalizeStatusSummary(summary)
	status.updatedAt = now
}

func (t *runStatusTracker) progress(
	sessionID string,
	requestID string,
	stage string,
	summary string,
	elapsed time.Duration,
) {
	if t == nil || strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(requestID) == "" {
		return
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	session := t.ensureSessionLocked(sessionID)
	status := session.ensureActive(requestID, now)
	status.state = runStateRunning
	status.stage = strings.TrimSpace(stage)
	if cleaned := normalizeStatusSummary(summary); cleaned != "" {
		status.summary = cleaned
	}
	if elapsed > 0 {
		status.elapsed = elapsed
	}
	status.updatedAt = now
}

func (t *runStatusTracker) preview(
	sessionID string,
	requestID string,
	preview string,
) {
	if t == nil || strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(requestID) == "" {
		return
	}

	preview = trimPreview(preview)
	if preview == "" {
		return
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	session := t.ensureSessionLocked(sessionID)
	status := session.ensureActive(requestID, now)
	status.preview = preview
	status.updatedAt = now
}

func (t *runStatusTracker) setUsage(
	sessionID string,
	requestID string,
	usage *gwclient.Usage,
	contextWindow int,
) {
	if t == nil || strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(requestID) == "" {
		return
	}
	contextUsage := buildContextUsageStatus(
		usage,
		contextWindow,
	)
	if contextUsage == nil {
		return
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	session := t.ensureSessionLocked(sessionID)
	status, _ := session.lookup(requestID)
	if status == nil {
		status = session.ensureActive(requestID, now)
	}
	status.contextUsage = contextUsage
	status.updatedAt = now
}

func (t *runStatusTracker) finish(
	sessionID string,
	requestID string,
	summary string,
	preview string,
) {
	t.complete(
		sessionID,
		requestID,
		runStateCompleted,
		summary,
		preview,
	)
}

func (t *runStatusTracker) fail(
	sessionID string,
	requestID string,
	summary string,
	preview string,
) {
	t.complete(
		sessionID,
		requestID,
		runStateFailed,
		summary,
		preview,
	)
}

func (t *runStatusTracker) cancel(
	sessionID string,
	requestID string,
) {
	t.complete(
		sessionID,
		requestID,
		runStateCanceled,
		defaultCancelOKMessage,
		"",
	)
}

func (t *runStatusTracker) complete(
	sessionID string,
	requestID string,
	state string,
	summary string,
	preview string,
) {
	if t == nil || strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(requestID) == "" {
		return
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	session := t.ensureSessionLocked(sessionID)
	status, source := session.lookup(requestID)
	if status == nil {
		status = &requestRunStatus{
			requestID: requestID,
			startedAt: now,
		}
	}
	status.state = state
	status.summary = normalizeStatusSummary(summary)
	if trimmed := trimPreview(preview); trimmed != "" {
		status.preview = trimmed
	}
	if status.startedAt.IsZero() {
		status.startedAt = now
	}
	status.updatedAt = now
	status.completedAt = now

	switch source {
	case runStatusSourceActive:
		session.active = nil
	case runStatusSourceQueued:
		session.queued = nil
	}
	session.last = cloneRunStatus(status)
}

func (t *runStatusTracker) snapshot(
	sessionID string,
) sessionRunSnapshot {
	if t == nil || strings.TrimSpace(sessionID) == "" {
		return sessionRunSnapshot{}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	session, ok := t.sessions[sessionID]
	if !ok || session == nil {
		return sessionRunSnapshot{}
	}
	return sessionRunSnapshot{
		active: cloneRunStatus(session.active),
		queued: cloneRunStatus(session.queued),
		last:   cloneRunStatus(session.last),
	}
}

func (t *runStatusTracker) ensureSessionLocked(
	sessionID string,
) *sessionRunState {
	session, ok := t.sessions[sessionID]
	if ok && session != nil {
		return session
	}
	session = &sessionRunState{}
	t.sessions[sessionID] = session
	return session
}

func (s *sessionRunState) ensureQueued(
	requestID string,
	now time.Time,
) *requestRunStatus {
	if s == nil {
		return nil
	}
	if s.queued != nil && s.queued.requestID == requestID {
		return s.queued
	}
	s.queued = &requestRunStatus{
		requestID: requestID,
		startedAt: now,
	}
	return s.queued
}

func (s *sessionRunState) ensureActive(
	requestID string,
	now time.Time,
) *requestRunStatus {
	if s == nil {
		return nil
	}
	if s.active != nil && s.active.requestID == requestID {
		return s.active
	}
	if s.queued != nil && s.queued.requestID == requestID {
		s.active = s.queued
		s.queued = nil
		return s.active
	}
	s.active = &requestRunStatus{
		requestID: requestID,
		startedAt: now,
	}
	return s.active
}

func (s *sessionRunState) lookup(
	requestID string,
) (*requestRunStatus, string) {
	if s == nil {
		return nil, ""
	}
	switch {
	case s.active != nil && s.active.requestID == requestID:
		return s.active, runStatusSourceActive
	case s.queued != nil && s.queued.requestID == requestID:
		return s.queued, runStatusSourceQueued
	case s.last != nil && s.last.requestID == requestID:
		return s.last, runStatusSourceLast
	default:
		return nil, ""
	}
}

func cloneRunStatus(
	status *requestRunStatus,
) *requestRunStatus {
	if status == nil {
		return nil
	}
	cloned := *status
	cloned.contextUsage = cloneContextUsageStatus(
		status.contextUsage,
	)
	return &cloned
}

func normalizeStatusSummary(summary string) string {
	return strings.TrimSpace(summary)
}

func trimPreview(preview string) string {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return ""
	}
	runes := []rune(preview)
	if len(runes) <= statusPreviewMaxRunes {
		return string(runes)
	}
	return "..." + string(
		runes[len(runes)-statusPreviewMaxRunes:],
	)
}

func (c *Channel) statusMessageText(
	sessionInfo *sessionInfo,
	snapshot sessionRunSnapshot,
	workspace string,
) string {
	modelName := ""
	runtimeVersion := ""
	if c != nil {
		modelName = c.runtimeModelDisplayName()
		runtimeVersion = c.currentRuntimeVersion()
	}
	return formatStatusMessageWithVersion(
		sessionInfo,
		snapshot,
		workspace,
		modelName,
		runtimeVersion,
	)
}

func formatStatusMessage(
	sessionInfo *sessionInfo,
	snapshot sessionRunSnapshot,
	workspace string,
	modelName string,
) string {
	return formatStatusMessageWithVersion(
		sessionInfo,
		snapshot,
		workspace,
		modelName,
		"",
	)
}

func formatStatusMessageWithVersion(
	sessionInfo *sessionInfo,
	snapshot sessionRunSnapshot,
	workspace string,
	modelName string,
	runtimeVersion string,
) string {
	lines := make([]string, 0, 9)

	if snapshot.active != nil {
		lines = append(
			lines,
			statusLabelState+statusStateText(snapshot.active.state),
		)
		appendStatusContextLines(
			&lines,
			sessionInfo,
			snapshot.active,
			workspace,
			modelName,
			runtimeVersion,
		)
		appendStatusDetails(&lines, snapshot.active)
		if snapshot.queued != nil {
			lines = append(
				lines,
				statusLabelQueued+queuedStatusText(snapshot.queued),
			)
		}
		appendStatusFollowupHints(&lines, sessionInfo, true)
		return strings.Join(lines, "\n")
	}

	if snapshot.queued != nil {
		lines = append(
			lines,
			statusLabelState+statusStateText(snapshot.queued.state),
		)
		appendStatusContextLines(
			&lines,
			sessionInfo,
			snapshot.queued,
			workspace,
			modelName,
			runtimeVersion,
		)
		appendStatusDetails(&lines, snapshot.queued)
		appendStatusFollowupHints(&lines, sessionInfo, false)
		return strings.Join(lines, "\n")
	}

	lines = append(lines, statusLabelState+statusLineIdle)
	appendStatusContextLines(
		&lines,
		sessionInfo,
		nil,
		workspace,
		modelName,
		runtimeVersion,
	)
	if snapshot.last == nil {
		appendStatusFollowupHints(&lines, sessionInfo, false)
		return strings.Join(lines, "\n")
	}

	lines = append(
		lines,
		statusLabelLast+statusStateText(snapshot.last.state),
	)
	appendStatusContextUsageLine(&lines, snapshot.last)
	appendStatusDetails(&lines, snapshot.last)
	appendStatusFollowupHints(&lines, sessionInfo, false)
	return strings.Join(lines, "\n")
}

func appendStatusFollowupHints(
	lines *[]string,
	sessionInfo *sessionInfo,
	includeCancel bool,
) {
	if lines == nil {
		return
	}
	if sessionInfo != nil &&
		sessionInfo.recallSessionID != "" {
		*lines = append(*lines, statusHintRecall)
	}
	if includeCancel {
		*lines = append(*lines, statusHintCancel)
	}
	*lines = append(*lines, statusHintSubagents)
}

func appendStatusContextLines(
	lines *[]string,
	sessionInfo *sessionInfo,
	status *requestRunStatus,
	workspace string,
	modelName string,
	runtimeVersion string,
) {
	appendStatusWorkspaceLine(lines, sessionInfo, workspace)
	appendStatusModelLine(lines, modelName)
	appendStatusRuntimeVersionLine(lines, runtimeVersion)
	appendStatusContextUsageLine(lines, status)
}

func appendStatusContextUsageLine(
	lines *[]string,
	status *requestRunStatus,
) {
	if lines == nil {
		return
	}
	if text := formatContextUsage(
		statusContextUsage(status),
	); text != "" {
		*lines = append(*lines, statusLabelContext+text)
	}
}

func appendStatusDetails(
	lines *[]string,
	status *requestRunStatus,
) {
	if lines == nil || status == nil {
		return
	}
	if status.summary != "" {
		*lines = append(*lines, statusLabelStep+status.summary)
	}
	if elapsed := statusElapsed(status); elapsed != "" {
		*lines = append(*lines, statusLabelElapsed+elapsed)
	}
	if status.preview != "" {
		*lines = append(*lines, statusLabelOutput)
		*lines = append(*lines, status.preview)
	}
}

func appendStatusWorkspaceLine(
	lines *[]string,
	sessionInfo *sessionInfo,
	workspace string,
) {
	if lines == nil {
		return
	}
	custom := ""
	if sessionInfo != nil {
		custom = sessionInfo.workspacePath
	}
	*lines = append(
		*lines,
		statusLabelWorkspace+formatWorkspaceDisplay(
			custom,
			workspace,
		),
	)
}

func appendStatusModelLine(
	lines *[]string,
	modelName string,
) {
	if lines == nil {
		return
	}
	if line := formatModelDisplayLine(modelName); line != "" {
		*lines = append(*lines, line)
	}
}

func appendStatusRuntimeVersionLine(
	lines *[]string,
	runtimeVersion string,
) {
	if lines == nil {
		return
	}
	runtimeVersion = strings.TrimSpace(runtimeVersion)
	if runtimeVersion == "" {
		return
	}
	*lines = append(*lines, displayLabelVersion+runtimeVersion)
}

func statusElapsed(status *requestRunStatus) string {
	if status == nil {
		return ""
	}
	if status.elapsed > 0 {
		return formatElapsedShort(status.elapsed)
	}
	if status.state == runStateRunning &&
		!status.startedAt.IsZero() {
		return formatElapsedShort(time.Since(status.startedAt))
	}
	return ""
}

func formatElapsedShort(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Second {
		return "<1s"
	}
	d = d.Truncate(time.Second)
	if d < time.Second {
		return "<1s"
	}
	return d.String()
}

func queuedStatusText(status *requestRunStatus) string {
	if status == nil || status.summary == "" {
		return defaultQueuedMessage
	}
	return status.summary
}

func statusStateText(state string) string {
	switch state {
	case runStateQueued:
		return statusLineQueued
	case runStateRunning:
		return statusLineRunning
	case runStateCompleted:
		return statusLineCompleted
	case runStateCanceled:
		return statusLineCanceled
	case runStateFailed:
		return statusLineFailed
	default:
		return statusLineIdle
	}
}

// --- Session tracking for timeout-based auto-splitting ---

type sessionHistoryEntry struct {
	SessionID    string
	LastActivity time.Time
}

// sessionInfo holds information about a session.
type sessionInfo struct {
	// sessionID is the current session identifier. The default root
	// session is the stable base session ID itself; rotated sessions
	// use a derived epoch suffix.
	sessionID string
	// baseSessionID is the chat/user identifier (without epoch).
	baseSessionID string
	// recallSessionID is the explicit recall target for /recall.
	recallSessionID string
	// lastActivity is the timestamp of the last message in this session.
	lastActivity time.Time
	// epoch is the session epoch (incremented on /new or timeout).
	epoch int64
	// personaID is the current effective chat persona.
	personaID string
	// personaPinned reports whether personaID is an explicit override.
	personaPinned bool
	// assistantAlias is the chat-local display name for the assistant.
	assistantAlias string
	// workspacePath is the user-selected coding workspace for this chat.
	workspacePath string
	// knownUserIDs keeps recent WeCom user IDs seen in this chat.
	knownUserIDs []string
	// history keeps recent sessions for this chat, newest first.
	history []sessionHistoryEntry
}

// sessionTracker manages session state for timeout-based splitting.
type sessionTracker struct {
	mu       sync.RWMutex
	sessions map[string]*sessionInfo // keyed by baseSessionID (chat/user ID)
	path     string
	now      func() time.Time
}

type sessionTrackerState struct {
	Version int `json:"version"`

	Sessions map[string]*sessionTrackerEntry `json:"sessions,omitempty"`
}

type sessionTrackerEntry struct {
	SessionID         string                       `json:"session_id,omitempty"`
	PreviousSessionID string                       `json:"previous_session_id,omitempty"`
	RecallSessionID   string                       `json:"recall_session_id,omitempty"`
	LastActivityUnix  int64                        `json:"last_activity_unix,omitempty"`
	Epoch             int64                        `json:"epoch,omitempty"`
	PersonaID         string                       `json:"persona_id,omitempty"`
	PersonaPinned     bool                         `json:"persona_pinned,omitempty"`
	AssistantAlias    string                       `json:"assistant_alias,omitempty"`
	WorkspacePath     string                       `json:"workspace_path,omitempty"`
	KnownUserIDs      []string                     `json:"known_user_ids,omitempty"`
	History           []sessionTrackerHistoryEntry `json:"history,omitempty"`
}

type sessionTrackerHistoryEntry struct {
	SessionID        string `json:"session_id,omitempty"`
	LastActivityUnix int64  `json:"last_activity_unix,omitempty"`
}

var sharedSessionTrackers sync.Map

func newSessionTracker() *sessionTracker {
	return newSessionTrackerWithPath(
		sessionTrackerStorePath(
			os.Getenv(sessionTrackerStateDirEnvName),
		),
	)
}

func sharedSessionTrackerWithPath(path string) *sessionTracker {
	path = strings.TrimSpace(path)
	if path == "" {
		return newSessionTrackerWithPath(path)
	}
	if loaded, ok := sharedSessionTrackers.Load(path); ok {
		tracker, _ := loaded.(*sessionTracker)
		if tracker != nil {
			return tracker
		}
	}

	tracker := newSessionTrackerWithPath(path)
	actual, _ := sharedSessionTrackers.LoadOrStore(path, tracker)
	shared, _ := actual.(*sessionTracker)
	if shared == nil {
		return tracker
	}
	return shared
}

func newSessionTrackerWithPath(path string) *sessionTracker {
	tracker := &sessionTracker{
		sessions: make(map[string]*sessionInfo),
		path:     strings.TrimSpace(path),
		now:      time.Now,
	}
	if err := tracker.load(); err != nil {
		log.Warnf(
			"wecom: load session tracker store failed: %v",
			err,
		)
	}
	return tracker
}

func sessionTrackerStorePath(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(
		stateDir,
		sessionTrackerStoreDirName,
		sessionTrackerStoreFileName,
	)
}

func legacyThreadSessionBaseID(baseID string) string {
	baseID = canonicalWeComSessionID(baseID)
	if baseID == "" {
		return ""
	}
	return wecomThreadSessionPrefix + baseID
}

func normalizeTrackedSessionInfo(
	info *sessionInfo,
	baseID string,
) {
	if info == nil {
		return
	}
	baseID = canonicalWeComSessionID(baseID)
	if baseID == "" {
		return
	}
	info.baseSessionID = baseID
	info.sessionID = defaultString(
		canonicalWeComSessionID(info.sessionID),
		baseID,
	)
	info.recallSessionID = canonicalWeComSessionID(
		info.recallSessionID,
	)
	info.history = normalizeTrackedSessionHistory(
		info.history,
		info.sessionID,
		info.lastActivity,
	)
}

func normalizeTrackedSessionHistory(
	history []sessionHistoryEntry,
	currentSessionID string,
	currentActivity time.Time,
) []sessionHistoryEntry {
	if len(history) == 0 {
		return sanitizeSessionHistory(
			nil,
			canonicalWeComSessionID(currentSessionID),
			currentActivity,
		)
	}
	normalized := make(
		[]sessionHistoryEntry,
		0,
		len(history),
	)
	for _, entry := range history {
		sessionID := canonicalWeComSessionID(entry.SessionID)
		if sessionID == "" {
			continue
		}
		normalized = append(
			normalized,
			sessionHistoryEntry{
				SessionID:    sessionID,
				LastActivity: entry.LastActivity,
			},
		)
	}
	return sanitizeSessionHistory(
		normalized,
		canonicalWeComSessionID(currentSessionID),
		currentActivity,
	)
}

func mergeLegacySessionInfo(
	current *sessionInfo,
	legacy *sessionInfo,
) {
	if current == nil || legacy == nil {
		return
	}
	if normalizeAssistantAlias(current.assistantAlias) == "" {
		current.assistantAlias = normalizeAssistantAlias(
			legacy.assistantAlias,
		)
	}
	if !current.personaPinned && legacy.personaPinned {
		current.personaID = legacy.personaID
		current.personaPinned = true
	}
	if strings.TrimSpace(current.workspacePath) == "" {
		current.workspacePath = strings.TrimSpace(
			legacy.workspacePath,
		)
	}
	if strings.TrimSpace(current.recallSessionID) == "" {
		current.recallSessionID = canonicalWeComSessionID(
			legacy.recallSessionID,
		)
	}
	if len(legacy.knownUserIDs) > 0 {
		current.knownUserIDs = sanitizeKnownUserIDs(
			append(
				current.knownUserIDs,
				legacy.knownUserIDs...,
			),
		)
	}
	if current.lastActivity.IsZero() &&
		!legacy.lastActivity.IsZero() {
		current.lastActivity = legacy.lastActivity
	}
	if current.epoch <= 0 && legacy.epoch > 0 {
		current.epoch = legacy.epoch
	}
	history := make(
		[]sessionHistoryEntry,
		0,
		len(current.history)+len(legacy.history),
	)
	history = append(history, current.history...)
	history = append(history, legacy.history...)
	current.history = normalizeTrackedSessionHistory(
		history,
		current.sessionID,
		current.lastActivity,
	)
}

func (t *sessionTracker) resolveSessionStateLocked(
	baseID string,
) (string, *sessionInfo, bool) {
	baseID = canonicalWeComSessionID(baseID)
	if baseID == "" {
		return "", nil, false
	}

	current := t.sessions[baseID]
	legacyID := legacyThreadSessionBaseID(baseID)
	legacy := t.sessions[legacyID]
	if legacy == nil || legacyID == baseID {
		return baseID, current, false
	}

	normalizeTrackedSessionInfo(legacy, baseID)
	if current == nil {
		t.sessions[baseID] = legacy
		delete(t.sessions, legacyID)
		return baseID, legacy, true
	}

	normalizeTrackedSessionInfo(current, baseID)
	mergeLegacySessionInfo(current, legacy)
	delete(t.sessions, legacyID)
	return baseID, current, true
}

func (t *sessionTracker) load() error {
	if t == nil || strings.TrimSpace(t.path) == "" {
		return nil
	}

	raw, err := os.ReadFile(t.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(
			"wecom: read session tracker store: %w",
			err,
		)
	}

	var state sessionTrackerState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf(
			"wecom: decode session tracker store: %w",
			err,
		)
	}
	if state.Version != sessionTrackerStoreVersion &&
		state.Version != sessionTrackerStoreV7 &&
		state.Version != sessionTrackerStoreV6 &&
		state.Version != sessionTrackerStoreV5 &&
		state.Version != sessionTrackerStoreV4 &&
		state.Version != sessionTrackerStoreV3 &&
		state.Version != sessionTrackerStoreV2 &&
		state.Version != sessionTrackerStoreV1 {
		return fmt.Errorf(
			"wecom: unexpected session tracker version: %d",
			state.Version,
		)
	}

	for baseID, entry := range state.Sessions {
		baseID = strings.TrimSpace(baseID)
		if baseID == "" || entry == nil {
			continue
		}
		recallSessionID := strings.TrimSpace(
			entry.RecallSessionID,
		)
		if state.Version == sessionTrackerStoreV1 {
			recallSessionID = ""
		}
		personaID, personaPinned := loadSessionPersonaState(
			state.Version,
			entry,
		)
		workspacePath := strings.TrimSpace(entry.WorkspacePath)
		info := newSessionInfo(baseID, time.Time{})
		info.sessionID = defaultString(entry.SessionID, baseID)
		info.recallSessionID = recallSessionID
		info.epoch = entry.Epoch
		info.personaID = personaID
		info.personaPinned = personaPinned
		info.assistantAlias = normalizeAssistantAlias(
			entry.AssistantAlias,
		)
		info.workspacePath = workspacePath
		info.knownUserIDs = sanitizeKnownUserIDs(
			entry.KnownUserIDs,
		)
		if entry.LastActivityUnix > 0 {
			info.lastActivity = time.Unix(
				entry.LastActivityUnix,
				0,
			)
		}
		info.history = loadSessionHistory(
			baseID,
			info.sessionID,
			recallSessionID,
			info.lastActivity,
			state.Version,
			entry.History,
		)
		t.sessions[baseID] = info
	}
	return nil
}

func (t *sessionTracker) persistLocked() error {
	if t == nil || strings.TrimSpace(t.path) == "" {
		return nil
	}

	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(
		dir,
		sessionTrackerStoreDirPerm,
	); err != nil {
		return fmt.Errorf(
			"wecom: create session tracker dir: %w",
			err,
		)
	}

	state := sessionTrackerState{
		Version:  sessionTrackerStoreVersion,
		Sessions: make(map[string]*sessionTrackerEntry),
	}
	for baseID, info := range t.sessions {
		if info == nil {
			continue
		}
		personaID, personaPinned := persistSessionPersonaState(
			info.personaID,
			info.personaPinned,
		)
		entry := &sessionTrackerEntry{
			SessionID: strings.TrimSpace(info.sessionID),
			RecallSessionID: strings.TrimSpace(
				info.recallSessionID,
			),
			Epoch:         info.epoch,
			PersonaID:     personaID,
			PersonaPinned: personaPinned,
			AssistantAlias: normalizeAssistantAlias(
				info.assistantAlias,
			),
			WorkspacePath: strings.TrimSpace(
				info.workspacePath,
			),
			KnownUserIDs: sanitizeKnownUserIDs(
				info.knownUserIDs,
			),
			History: dumpSessionHistory(info.history),
		}
		if !info.lastActivity.IsZero() {
			entry.LastActivityUnix =
				info.lastActivity.Unix()
		}
		state.Sessions[baseID] = entry
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf(
			"wecom: encode session tracker store: %w",
			err,
		)
	}
	data = append(data, '\n')

	tmp := fmt.Sprintf("%s.tmp", t.path)
	if err := os.WriteFile(
		tmp,
		data,
		sessionTrackerStoreFilePerm,
	); err != nil {
		return fmt.Errorf(
			"wecom: write session tracker store: %w",
			err,
		)
	}
	if err := os.Rename(tmp, t.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf(
			"wecom: rename session tracker store: %w",
			err,
		)
	}
	return nil
}

func (t *sessionTracker) persistLockedWarn() {
	if err := t.persistLocked(); err != nil {
		log.Warnf(
			"wecom: persist session tracker store failed: %v",
			err,
		)
	}
}

func (t *sessionTracker) currentTime() time.Time {
	if t == nil || t.now == nil {
		return time.Now()
	}
	return t.now()
}

func (t *sessionTracker) getSession(
	baseID string,
) *sessionInfo {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	_, info, migrated := t.resolveSessionStateLocked(baseID)
	if migrated {
		t.persistLockedWarn()
	}
	return cloneSessionInfo(info)
}

func (info *sessionInfo) effectivePersonaID() string {
	if info == nil {
		return defaultChatPersonaID
	}
	if !info.personaPinned {
		return defaultChatPersonaID
	}
	personaID := strings.TrimSpace(info.personaID)
	if personaID == "" {
		return defaultChatPersonaID
	}
	return personaID
}

func normalizeSessionPersonaState(
	personaID string,
	pinned bool,
) (string, bool) {
	personaID = strings.TrimSpace(personaID)
	if !pinned || personaID == "" {
		return defaultChatPersonaID, false
	}
	return personaID, true
}

func loadSessionPersonaState(
	version int,
	entry *sessionTrackerEntry,
) (string, bool) {
	if entry == nil {
		return normalizeSessionPersonaState("", false)
	}
	if version == sessionTrackerStoreVersion {
		return normalizeSessionPersonaState(
			entry.PersonaID,
			entry.PersonaPinned,
		)
	}
	personaID := strings.TrimSpace(entry.PersonaID)
	switch personaID {
	case "", personaapi.SnarkyID, defaultChatPersonaID:
		return normalizeSessionPersonaState("", false)
	default:
		return normalizeSessionPersonaState(personaID, true)
	}
}

func persistSessionPersonaState(
	personaID string,
	pinned bool,
) (string, bool) {
	personaID = strings.TrimSpace(personaID)
	if !pinned || personaID == "" {
		return "", false
	}
	return personaID, true
}

func loadSessionHistory(
	baseSessionID string,
	sessionID string,
	recallSessionID string,
	lastActivity time.Time,
	version int,
	raw []sessionTrackerHistoryEntry,
) []sessionHistoryEntry {
	history := make([]sessionHistoryEntry, 0, len(raw)+2)
	if version == sessionTrackerStoreVersion ||
		version == sessionTrackerStoreV7 ||
		version == sessionTrackerStoreV6 {
		for _, entry := range raw {
			sessionID := strings.TrimSpace(entry.SessionID)
			if sessionID == "" {
				continue
			}
			item := sessionHistoryEntry{SessionID: sessionID}
			if entry.LastActivityUnix > 0 {
				item.LastActivity = time.Unix(
					entry.LastActivityUnix,
					0,
				)
			}
			history = append(history, item)
		}
		return sanitizeSessionHistory(
			history,
			sessionID,
			lastActivity,
		)
	}

	currentSessionID := defaultString(sessionID, baseSessionID)
	history = append(
		history,
		sessionHistoryEntry{
			SessionID:    currentSessionID,
			LastActivity: lastActivity,
		},
	)
	if recallSessionID != "" && recallSessionID != currentSessionID {
		history = append(
			history,
			sessionHistoryEntry{
				SessionID:    recallSessionID,
				LastActivity: lastActivity,
			},
		)
	}
	if currentSessionID != baseSessionID {
		history = append(
			history,
			sessionHistoryEntry{
				SessionID:    baseSessionID,
				LastActivity: lastActivity,
			},
		)
	}
	return sanitizeSessionHistory(
		history,
		currentSessionID,
		lastActivity,
	)
}

func dumpSessionHistory(
	history []sessionHistoryEntry,
) []sessionTrackerHistoryEntry {
	if len(history) == 0 {
		return nil
	}

	dumped := make([]sessionTrackerHistoryEntry, 0, len(history))
	for _, entry := range history {
		sessionID := strings.TrimSpace(entry.SessionID)
		if sessionID == "" {
			continue
		}
		item := sessionTrackerHistoryEntry{SessionID: sessionID}
		if !entry.LastActivity.IsZero() {
			item.LastActivityUnix = entry.LastActivity.Unix()
		}
		dumped = append(dumped, item)
	}
	if len(dumped) == 0 {
		return nil
	}
	return dumped
}

func sanitizeSessionHistory(
	history []sessionHistoryEntry,
	currentSessionID string,
	currentActivity time.Time,
) []sessionHistoryEntry {
	currentSessionID = strings.TrimSpace(currentSessionID)
	if currentSessionID != "" && currentActivity.IsZero() {
		currentActivity = lookupSessionLastActivity(
			history,
			currentSessionID,
		)
	}

	sanitized := make([]sessionHistoryEntry, 0, len(history)+1)
	seen := make(map[string]struct{}, len(history)+1)
	appendEntry := func(entry sessionHistoryEntry) {
		sessionID := strings.TrimSpace(entry.SessionID)
		if sessionID == "" {
			return
		}
		if _, ok := seen[sessionID]; ok {
			return
		}
		sanitized = append(
			sanitized,
			sessionHistoryEntry{
				SessionID:    sessionID,
				LastActivity: entry.LastActivity,
			},
		)
		seen[sessionID] = struct{}{}
	}

	if currentSessionID != "" {
		appendEntry(sessionHistoryEntry{
			SessionID:    currentSessionID,
			LastActivity: currentActivity,
		})
	}

	for _, entry := range history {
		appendEntry(entry)
		if len(sanitized) >= sessionHistoryMaxEntries {
			break
		}
	}
	if len(sanitized) > sessionHistoryMaxEntries {
		sanitized = sanitized[:sessionHistoryMaxEntries]
	}
	return sanitized
}

func lookupSessionLastActivity(
	history []sessionHistoryEntry,
	sessionID string,
) time.Time {
	sessionID = strings.TrimSpace(sessionID)
	for _, entry := range history {
		if strings.TrimSpace(entry.SessionID) == sessionID {
			return entry.LastActivity
		}
	}
	return time.Time{}
}

func upsertSessionHistory(
	history []sessionHistoryEntry,
	sessionID string,
	lastActivity time.Time,
) []sessionHistoryEntry {
	if strings.TrimSpace(sessionID) == "" {
		return sanitizeSessionHistory(history, "", time.Time{})
	}
	merged := append(
		[]sessionHistoryEntry{{
			SessionID:    strings.TrimSpace(sessionID),
			LastActivity: lastActivity,
		}},
		history...,
	)
	return sanitizeSessionHistory(
		merged,
		sessionID,
		lastActivity,
	)
}

func cloneSessionInfo(info *sessionInfo) *sessionInfo {
	if info == nil {
		return nil
	}
	cloned := *info
	if len(info.history) > 0 {
		cloned.history = append(
			make([]sessionHistoryEntry, 0, len(info.history)),
			info.history...,
		)
	}
	if len(info.knownUserIDs) > 0 {
		cloned.knownUserIDs = append(
			make([]string, 0, len(info.knownUserIDs)),
			info.knownUserIDs...,
		)
	}
	return &cloned
}

func newSessionInfo(baseID string, now time.Time) *sessionInfo {
	baseID = strings.TrimSpace(baseID)
	return &sessionInfo{
		sessionID:       baseID,
		baseSessionID:   baseID,
		recallSessionID: "",
		lastActivity:    now,
		epoch:           0,
		personaID:       defaultChatPersonaID,
		personaPinned:   false,
		assistantAlias:  "",
		knownUserIDs:    nil,
		history: []sessionHistoryEntry{{
			SessionID:    baseID,
			LastActivity: now,
		}},
	}
}

func nextSessionEpoch(lastEpoch int64, now time.Time) int64 {
	epoch := now.Unix()
	if epoch <= lastEpoch {
		return lastEpoch + 1
	}
	return epoch
}

func derivedSessionID(baseID string, epoch int64) string {
	baseID = strings.TrimSpace(baseID)
	if baseID == "" || epoch <= 0 {
		return baseID
	}
	return fmt.Sprintf("%s:%d", baseID, epoch)
}

func sameUnixSecond(left, right time.Time) bool {
	if left.IsZero() || right.IsZero() {
		return false
	}
	return left.Unix() == right.Unix()
}

// getOrCreateSession returns the current session info, creating a new
// one if needed.
// If timeout is exceeded since lastActivity, a new session epoch is
// started.
// baseID is the chat/user identifier, for example
// "wecom:chat:xxx" or "wecom:dm:xxx".
func (t *sessionTracker) getOrCreateSession(
	baseID string,
	timeout time.Duration,
) *sessionInfo {
	t.mu.Lock()
	defer t.mu.Unlock()

	baseID, info, migrated := t.resolveSessionStateLocked(baseID)
	now := t.currentTime()

	if info == nil {
		info = newSessionInfo(baseID, now)
		t.sessions[baseID] = info
		t.persistLockedWarn()
		return cloneSessionInfo(info)
	}

	// Check if timeout exceeded (auto-split).
	if timeout > 0 &&
		!info.lastActivity.IsZero() &&
		now.Sub(info.lastActivity) > timeout {
		// Start a new session epoch.
		lastActivity := info.lastActivity
		epoch := nextSessionEpoch(info.epoch, now)
		info.history = upsertSessionHistory(
			info.history,
			info.sessionID,
			info.lastActivity,
		)
		info.sessionID = derivedSessionID(baseID, epoch)
		info.epoch = epoch
		info.lastActivity = now
		info.history = upsertSessionHistory(
			info.history,
			info.sessionID,
			now,
		)
		t.persistLockedWarn()
		log.Infof(
			"wecom: session auto-split for %s "+
				"(inactive for %v), new session: %s",
			baseID,
			now.Sub(lastActivity),
			info.sessionID,
		)
		return cloneSessionInfo(info)
	}

	// Update last activity.
	changed := migrated || !sameUnixSecond(info.lastActivity, now)
	info.lastActivity = now
	info.history = upsertSessionHistory(
		info.history,
		info.sessionID,
		now,
	)
	if changed {
		t.persistLockedWarn()
	}
	return cloneSessionInfo(info)
}

// startNewSession forces a new session epoch (for /new command).
// Returns the new session info.
func (t *sessionTracker) startNewSession(baseID string) *sessionInfo {
	t.mu.Lock()
	defer t.mu.Unlock()

	baseID, info, _ := t.resolveSessionStateLocked(baseID)
	now := t.currentTime()
	if info == nil {
		epoch := nextSessionEpoch(0, now)
		info = newSessionInfo(baseID, now)
		info.sessionID = derivedSessionID(baseID, epoch)
		info.recallSessionID = strings.TrimSpace(baseID)
		info.epoch = epoch
		info.history = upsertSessionHistory(
			info.history,
			info.sessionID,
			now,
		)
		t.sessions[baseID] = info
		t.persistLockedWarn()
		return cloneSessionInfo(info)
	}

	// 保存当前会话为上一个会话，开始新纪元。
	oldSessionID := info.sessionID
	epoch := nextSessionEpoch(info.epoch, now)
	info.recallSessionID = oldSessionID
	info.history = upsertSessionHistory(
		info.history,
		oldSessionID,
		info.lastActivity,
	)
	info.sessionID = derivedSessionID(baseID, epoch)
	info.epoch = epoch
	info.lastActivity = now
	info.history = upsertSessionHistory(
		info.history,
		info.sessionID,
		now,
	)

	t.persistLockedWarn()
	return cloneSessionInfo(info)
}

func (t *sessionTracker) recallPreviousSession(
	baseID string,
) (*sessionInfo, bool) {
	if t == nil {
		return nil, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	_, info, _ := t.resolveSessionStateLocked(baseID)
	if info == nil ||
		strings.TrimSpace(info.recallSessionID) == "" {
		return nil, false
	}

	info.sessionID, info.recallSessionID =
		info.recallSessionID, info.sessionID
	info.lastActivity = t.currentTime()
	info.history = upsertSessionHistory(
		info.history,
		info.sessionID,
		info.lastActivity,
	)
	t.persistLockedWarn()
	return cloneSessionInfo(info), true
}

func (t *sessionTracker) switchSession(
	baseID string,
	targetSessionID string,
) (*sessionInfo, bool) {
	if t == nil {
		return nil, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	_, info, _ := t.resolveSessionStateLocked(baseID)
	if info == nil {
		return nil, false
	}

	targetSessionID = strings.TrimSpace(targetSessionID)
	if targetSessionID == "" {
		return nil, false
	}

	found := false
	for _, entry := range info.history {
		if strings.TrimSpace(entry.SessionID) == targetSessionID {
			found = true
			break
		}
	}
	if !found {
		return nil, false
	}

	info.sessionID = targetSessionID
	info.lastActivity = t.currentTime()
	info.history = upsertSessionHistory(
		info.history,
		targetSessionID,
		info.lastActivity,
	)
	t.persistLockedWarn()
	return cloneSessionInfo(info), true
}

func (t *sessionTracker) setPersona(
	baseID string,
	personaID string,
) *sessionInfo {
	return t.updatePersona(baseID, personaID, true)
}

func (t *sessionTracker) clearPersona(baseID string) *sessionInfo {
	return t.updatePersona(baseID, "", false)
}

func (t *sessionTracker) updatePersona(
	baseID string,
	personaID string,
	pinned bool,
) *sessionInfo {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	baseID, info, _ := t.resolveSessionStateLocked(baseID)
	now := t.currentTime()
	if info == nil {
		info = newSessionInfo(baseID, now)
		t.sessions[baseID] = info
	}

	info.personaID, info.personaPinned =
		normalizeSessionPersonaState(
			personaID,
			pinned,
		)
	info.personaID = defaultString(
		info.personaID,
		defaultChatPersonaID,
	)
	info.history = upsertSessionHistory(
		info.history,
		info.sessionID,
		info.lastActivity,
	)
	t.persistLockedWarn()
	return cloneSessionInfo(info)
}

func (t *sessionTracker) setWorkspace(
	baseID string,
	workspacePath string,
) *sessionInfo {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	baseID, info, _ := t.resolveSessionStateLocked(baseID)
	now := t.currentTime()
	if info == nil {
		info = newSessionInfo(baseID, now)
		t.sessions[baseID] = info
	}

	info.workspacePath = strings.TrimSpace(workspacePath)
	info.history = upsertSessionHistory(
		info.history,
		info.sessionID,
		info.lastActivity,
	)
	t.persistLockedWarn()
	return cloneSessionInfo(info)
}

func (t *sessionTracker) setAssistantAlias(
	baseID string,
	alias string,
) *sessionInfo {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	baseID, info, _ := t.resolveSessionStateLocked(baseID)
	now := t.currentTime()
	if info == nil {
		info = newSessionInfo(baseID, now)
		t.sessions[baseID] = info
	}

	info.assistantAlias = normalizeAssistantAlias(alias)
	info.history = upsertSessionHistory(
		info.history,
		info.sessionID,
		info.lastActivity,
	)
	t.persistLockedWarn()
	return cloneSessionInfo(info)
}

func (t *sessionTracker) recordKnownUsers(
	baseID string,
	userIDs []string,
) *sessionInfo {
	if t == nil {
		return nil
	}
	userIDs = sanitizeKnownUserIDs(userIDs)
	if len(userIDs) == 0 {
		return t.getSession(baseID)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	baseID, info, migrated := t.resolveSessionStateLocked(baseID)
	now := t.currentTime()
	if info == nil {
		info = newSessionInfo(baseID, now)
		t.sessions[baseID] = info
	}
	merged := append(
		append(
			make([]string, 0, len(userIDs)+len(info.knownUserIDs)),
			userIDs...,
		),
		info.knownUserIDs...,
	)
	sanitized := sanitizeKnownUserIDs(merged)
	if sameStringSlice(info.knownUserIDs, sanitized) {
		if migrated {
			t.persistLockedWarn()
		}
		return cloneSessionInfo(info)
	}
	info.knownUserIDs = sanitized
	t.persistLockedWarn()
	return cloneSessionInfo(info)
}

func (t *sessionTracker) knownUserIDsForSession(
	baseID string,
) []string {
	if t == nil {
		return nil
	}
	baseID = canonicalWeComSessionID(baseID)

	t.mu.RLock()
	defer t.mu.RUnlock()

	collected := make([]string, 0, knownUserIDMaxEntries)
	if info := t.sessions[baseID]; info != nil {
		collected = append(collected, info.knownUserIDs...)
	}
	for sessionBaseID, info := range t.sessions {
		if strings.HasPrefix(
			sessionBaseID,
			directMessageSessionPrefix,
		) {
			userID := strings.TrimSpace(
				strings.TrimPrefix(
					sessionBaseID,
					directMessageSessionPrefix,
				),
			)
			if userID != "" {
				collected = append(collected, userID)
			}
		}
		if info != nil {
			collected = append(collected, info.knownUserIDs...)
		}
	}
	return sanitizeKnownUserIDs(collected)
}

func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
