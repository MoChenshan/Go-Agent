package wecom

import (
	"context"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
)

const (
	controlCardTaskPrefix = "control"

	sessionCardViewHome      = "home"
	sessionCardViewPersona   = "persona"
	sessionCardViewStatus    = "status"
	sessionCardViewSessions  = "sessions"
	sessionCardViewWorkspace = "workspace"

	controlCardEventHome                      = "control_home"
	controlCardEventHelp                      = "control_help"
	controlCardEventPersona                   = "control_persona"
	controlCardEventStatus                    = "control_status"
	controlCardEventSessions                  = "control_sessions"
	controlCardEventCron                      = "control_cron"
	controlCardEventWorkspace                 = "control_workspace"
	controlCardEventRuntime                   = "control_runtime"
	controlCardEventCancel                    = "control_cancel"
	controlCardEventSessionSwitch             = "control_session_switch"
	controlCardEventSessionNew                = "control_session_new"
	controlCardEventSessionRecall             = "control_session_recall"
	controlCardEventCronDetails               = "control_cron_details"
	controlCardEventCronStop                  = "control_cron_stop"
	controlCardEventCronResume                = "control_cron_resume"
	controlCardEventCronRemove                = "control_cron_remove"
	controlCardEventCronClear                 = "control_cron_clear"
	controlCardEventWorkspaceClear            = "control_workspace_clear"
	controlCardEventRuntimeRestart            = "control_runtime_restart"
	controlCardEventRuntimeForceRestartPrompt = "control_runtime" +
		"_force_restart_prompt"
	controlCardEventRuntimeForceRestart = "control_runtime" +
		"_force_restart"
	controlCardEventRuntimeUpgrade            = "control_runtime_upgrade"
	controlCardEventRuntimeForceUpgradePrompt = "control_runtime" +
		"_force_upgrade_prompt"
	controlCardEventRuntimeForceUpgrade = "control_runtime" +
		"_force_upgrade"
	controlCardEventHelpPagePrefix     = "control_help_page_"
	controlCardEventHelpPagePrevPrefix = "control_help_page_prev_"
	controlCardEventHelpPageNextPrefix = "control_help_page_next_"

	controlCardSessionQuestionKey = "control_session"
	controlCardCronQuestionKey    = "control_cron"

	controlCardTitleHome      = "✨ 助手面板"
	controlCardTitleHelp      = "📚 使用帮助"
	controlCardTitleStatus    = "📍 当前状态"
	controlCardTitleSessions  = "🧵 会话面板"
	controlCardTitleCron      = "⏰ 定时任务"
	controlCardTitleWorkspace = "💻 工作区"
	controlCardTitleRuntime   = "🛠 运行时控制"

	controlCardButtonHome       = "🏠 主页"
	controlCardButtonHelp       = "📚 帮助"
	controlCardButtonPersona    = "🎭 人格"
	controlCardButtonStatus     = "📍 状态"
	controlCardButtonSessions   = "🧵 会话"
	controlCardButtonCron       = "⏰ 定时"
	controlCardButtonWorkspace  = "💻 工作区"
	controlCardButtonCancel     = "⛔ 取消"
	controlCardButtonSwitch     = "🔁 切换"
	controlCardButtonRecall     = "↩ 回前一会"
	controlCardButtonNew        = "🆕 新会话"
	controlCardButtonCronDetail = "ℹ️ 详情"
	controlCardButtonCronStop   = "⏸ 停止"
	controlCardButtonCronResume = "▶️ 恢复"
	controlCardButtonCronRemove = "🗑 删除"
	controlCardButtonCronClear  = "🧹 清空"
	controlCardButtonWsClear    = "🧹 清除"
	controlCardButtonRuntime    = "🛠 运行时"
	controlCardButtonRestart    = "♻ 无损重启"
	controlCardButtonUpgrade    = "⬆ 无损升级"
	controlCardButtonForceRst   = "⛔ 强制重启"
	controlCardButtonForceUpg   = "⚡ 强制升级"
	controlCardButtonConfirm    = "✅ 确认执行"
	controlCardButtonBack       = "↩ 返回"
	controlCardButtonPrevPage   = "⬅ 上页"
	controlCardButtonNextPage   = "➡ 下页"

	controlCardDescMaxRunes = 360
	controlCardListMaxItems = templateCardButtonOptionLimit
	controlCardNameRunes    = 12
	controlCardLineMaxRunes = 28

	controlHelpPageDefault       = 0
	controlHelpPageCommands      = 1
	controlHelpPageSessions      = 2
	controlHelpPageCount         = 3
	controlHelpPageIndicatorFmt  = "📄 第 %d/%d 页"
	controlHelpPageLabelCommon   = "常用入口"
	controlHelpPageLabelCommands = "任务与工作区"
	controlHelpPageLabelSessions = "会话与控制"
)

type activeSessionCard struct {
	TaskID    string
	View      string
	Variant   string
	StateHash string
}

type sessionCardTracker struct {
	mu     sync.RWMutex
	active map[string]activeSessionCard
}

func newSessionCardTracker() *sessionCardTracker {
	return &sessionCardTracker{
		active: make(map[string]activeSessionCard),
	}
}

func (t *sessionCardTracker) activeCard(
	baseSessionID string,
) (activeSessionCard, bool) {
	if t == nil {
		return activeSessionCard{}, false
	}
	baseSessionID = strings.TrimSpace(baseSessionID)
	if baseSessionID == "" {
		return activeSessionCard{}, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	card, ok := t.active[baseSessionID]
	if !ok {
		return activeSessionCard{}, false
	}
	return card, true
}

func (t *sessionCardTracker) remember(
	baseSessionID string,
	card activeSessionCard,
) {
	if t == nil {
		return
	}
	baseSessionID = strings.TrimSpace(baseSessionID)
	card.TaskID = strings.TrimSpace(card.TaskID)
	card.View = strings.TrimSpace(card.View)
	card.Variant = strings.TrimSpace(card.Variant)
	card.StateHash = strings.TrimSpace(card.StateHash)
	if baseSessionID == "" ||
		card.TaskID == "" ||
		card.View == "" ||
		card.StateHash == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.active[baseSessionID] = card
}

func isControlCardEventKey(eventKey string) bool {
	if _, ok := parseControlHelpPageEvent(eventKey); ok {
		return true
	}
	switch strings.TrimSpace(eventKey) {
	case controlCardEventHome,
		controlCardEventHelp,
		controlCardEventPersona,
		controlCardEventStatus,
		controlCardEventSessions,
		controlCardEventCron,
		controlCardEventWorkspace,
		controlCardEventRuntime,
		controlCardEventCancel,
		controlCardEventSessionSwitch,
		controlCardEventSessionNew,
		controlCardEventSessionRecall,
		controlCardEventCronDetails,
		controlCardEventCronStop,
		controlCardEventCronResume,
		controlCardEventCronRemove,
		controlCardEventCronClear,
		controlCardEventWorkspaceClear,
		controlCardEventRuntimeRestart,
		controlCardEventRuntimeForceRestartPrompt,
		controlCardEventRuntimeForceRestart,
		controlCardEventRuntimeUpgrade,
		controlCardEventRuntimeForceUpgradePrompt,
		controlCardEventRuntimeForceUpgrade:
		return true
	default:
		return false
	}
}

func parseControlHelpPageEvent(eventKey string) (int, bool) {
	eventKey = strings.TrimSpace(eventKey)
	for _, prefix := range []string{
		controlCardEventHelpPagePrevPrefix,
		controlCardEventHelpPageNextPrefix,
		controlCardEventHelpPagePrefix,
	} {
		if !strings.HasPrefix(eventKey, prefix) {
			continue
		}

		page, err := strconv.Atoi(
			strings.TrimPrefix(eventKey, prefix),
		)
		if err != nil {
			return controlHelpPageDefault, true
		}
		return normalizeControlHelpPage(page), true
	}
	return 0, false
}

func statefulControlCardView(eventKey string) string {
	switch strings.TrimSpace(eventKey) {
	case controlCardEventHome:
		return sessionCardViewHome
	case controlCardEventPersona:
		return sessionCardViewPersona
	case controlCardEventStatus, controlCardEventCancel:
		return sessionCardViewStatus
	case controlCardEventSessions,
		controlCardEventSessionSwitch,
		controlCardEventSessionNew,
		controlCardEventSessionRecall:
		return sessionCardViewSessions
	case controlCardEventWorkspace,
		controlCardEventWorkspaceClear:
		return sessionCardViewWorkspace
	default:
		return ""
	}
}

func normalizePersonaSessionCardVariant(
	variant string,
) string {
	switch strings.TrimSpace(variant) {
	case personaCardViewSaveHelp:
		return personaCardViewSaveHelp
	default:
		return personaCardViewDefault
	}
}

func (c *Channel) resolveSessionCardInfo(
	baseSessionID string,
	info *sessionInfo,
) *sessionInfo {
	if info != nil {
		return info
	}
	if c == nil || c.sessionTracker == nil {
		return nil
	}
	return c.sessionTracker.getSession(baseSessionID)
}

func (c *Channel) sessionCardStateHash(
	baseSessionID string,
	info *sessionInfo,
) string {
	signature := c.sessionCardStateSignature(
		baseSessionID,
		info,
	)
	if signature == "" {
		return ""
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(signature))
	return strconv.FormatUint(hasher.Sum64(), 16)
}

func (c *Channel) sessionCardStateSignature(
	baseSessionID string,
	info *sessionInfo,
) string {
	info = c.resolveSessionCardInfo(baseSessionID, info)
	workspacePath := ""
	sessionID := ""
	recallSessionID := ""
	if info != nil {
		workspacePath = strings.TrimSpace(info.workspacePath)
		sessionID = strings.TrimSpace(info.sessionID)
		recallSessionID = strings.TrimSpace(
			info.recallSessionID,
		)
	}
	return strings.Join(
		[]string{
			strings.TrimSpace(baseSessionID),
			c.assistantDisplayNameForSession(baseSessionID),
			formatAssistantNameSummary(
				c.assistantNameStateForInfo(info),
			),
			c.activePersonaDisplay(info),
			workspacePath,
			c.effectiveWorkspacePath(info),
			sessionID,
			recallSessionID,
			sessionCardHistorySignature(info),
			c.runtimeModelDisplayName(),
		},
		"\n",
	)
}

func sessionCardHistorySignature(info *sessionInfo) string {
	if info == nil || len(info.history) == 0 {
		return ""
	}
	parts := make([]string, 0, len(info.history))
	for _, entry := range info.history {
		sessionID := strings.TrimSpace(entry.SessionID)
		if sessionID == "" {
			continue
		}
		parts = append(parts, sessionID)
	}
	return strings.Join(parts, "\n")
}

func (c *Channel) rememberSessionCard(
	baseSessionID string,
	view string,
	taskID string,
	info *sessionInfo,
) {
	c.rememberSessionCardWithVariant(
		baseSessionID,
		view,
		"",
		taskID,
		info,
	)
}

func (c *Channel) rememberSessionCardWithVariant(
	baseSessionID string,
	view string,
	variant string,
	taskID string,
	info *sessionInfo,
) {
	if c == nil || c.sessionCards == nil {
		return
	}
	view = strings.TrimSpace(view)
	if view == "" {
		return
	}
	c.sessionCards.remember(
		baseSessionID,
		activeSessionCard{
			TaskID: taskID,
			View:   view,
			Variant: strings.TrimSpace(
				variant,
			),
			StateHash: c.sessionCardStateHash(
				baseSessionID,
				info,
			),
		},
	)
}

func (c *Channel) buildSessionCardForView(
	baseSessionID string,
	view string,
	variant string,
	taskID string,
	info *sessionInfo,
) (*templateCard, error) {
	info = c.resolveSessionCardInfo(baseSessionID, info)
	if info == nil {
		info = &sessionInfo{baseSessionID: baseSessionID}
	}
	assistantName := c.assistantDisplayNameForSession(
		baseSessionID,
	)
	switch strings.TrimSpace(view) {
	case sessionCardViewHome:
		return buildControlHomeCard(
			assistantName,
			c.activePersonaDisplay(info),
			c.effectiveWorkspacePath(info),
			c.runtimeModelDisplayName(),
			taskID,
		), nil
	case sessionCardViewPersona:
		defs, err := c.listPersonas()
		if err != nil {
			return nil, err
		}
		variant = normalizePersonaSessionCardVariant(
			variant,
		)
		return buildPersonaSettingsCard(
			assistantName,
			c.activePersonaDisplay(info),
			info,
			defs,
			taskID,
			variant,
			"",
			c.personaStorageEnabled(),
		), nil
	case sessionCardViewStatus:
		return buildControlStatusCard(
			assistantName,
			c.statusMessageText(
				info,
				c.runStatus.snapshot(info.sessionID),
				c.effectiveWorkspacePath(info),
			),
			taskID,
		), nil
	case sessionCardViewSessions:
		return buildControlSessionsCard(
			assistantName,
			c.assistantNameStateForInfo(info),
			info,
			c.sessionTimeout,
			c.effectiveWorkspacePath(info),
			taskID,
			"",
		), nil
	case sessionCardViewWorkspace:
		return buildControlWorkspaceCard(
			assistantName,
			info.workspacePath,
			c.effectiveWorkspacePath(info),
			taskID,
			"",
		), nil
	default:
		return nil, nil
	}
}

func (c *Channel) syncActiveSessionCard(
	ctx context.Context,
	baseSessionID string,
	info *sessionInfo,
	sender messageSender,
) {
	if c == nil || c.sessionCards == nil {
		return
	}
	active, ok := c.sessionCards.activeCard(baseSessionID)
	if !ok {
		return
	}
	currentHash := c.sessionCardStateHash(baseSessionID, info)
	if currentHash == "" ||
		currentHash == strings.TrimSpace(active.StateHash) {
		return
	}
	updater, ok := sender.(templateCardUpdater)
	if !ok || updater == nil {
		return
	}
	card, err := c.buildSessionCardForView(
		baseSessionID,
		active.View,
		active.Variant,
		active.TaskID,
		info,
	)
	if err != nil {
		log.WarnfContext(
			ctx,
			"wecom: sync active session card failed: %v",
			err,
		)
		return
	}
	if card == nil {
		return
	}
	if err := updater.UpdateTemplateCard(ctx, card); err != nil {
		log.WarnfContext(
			ctx,
			"wecom: update active session card failed: %v",
			err,
		)
		return
	}
	c.rememberSessionCard(
		baseSessionID,
		active.View,
		active.TaskID,
		info,
	)
}

func normalizeControlHelpPage(page int) int {
	if page < 0 || page >= controlHelpPageCount {
		return controlHelpPageDefault
	}
	return page
}

func controlHelpPageEvent(page int) string {
	return controlCardEventHelpPagePrefix +
		strconv.Itoa(normalizeControlHelpPage(page))
}

func controlHelpPrevEvent(page int) string {
	return controlCardEventHelpPagePrevPrefix +
		strconv.Itoa(normalizeControlHelpPage(page))
}

func controlHelpNextEvent(page int) string {
	return controlCardEventHelpPageNextPrefix +
		strconv.Itoa(normalizeControlHelpPage(page))
}

func controlHelpPrevPage(page int) int {
	page = normalizeControlHelpPage(page)
	switch {
	case controlHelpPageCount <= 1:
		return controlHelpPageDefault
	case page == controlHelpPageDefault:
		return controlHelpPageCount - 1
	default:
		return page - 1
	}
}

func controlHelpNextPage(page int) int {
	page = normalizeControlHelpPage(page)
	switch {
	case controlHelpPageCount <= 1:
		return controlHelpPageDefault
	case page >= controlHelpPageCount-1:
		return controlHelpPageDefault
	default:
		return page + 1
	}
}

func buildControlCard(
	title string,
	desc string,
	body string,
	taskID string,
	selection *templateCardSelection,
	buttons []templateCardButton,
) *templateCard {
	return &templateCard{
		CardType:   templateCardTypeButtonInteraction,
		ActionMenu: buildControlCardActionMenu(),
		MainTitle: &templateCardMainTitle{
			Title: strings.TrimSpace(title),
			Desc:  trimControlCardText(desc),
		},
		SubTitleText:    trimControlCardText(body),
		ButtonSelection: selection,
		ButtonList:      buttons,
		TaskID:          strings.TrimSpace(taskID),
	}
}

func buildControlCardActionMenu() *templateCardActionMenu {
	return &templateCardActionMenu{
		Desc: "更多入口",
		ActionList: []templateCardActionMenuItem{
			{
				Text: controlCardButtonRuntime,
				Key:  controlCardEventRuntime,
			},
		},
	}
}

func trimControlCardText(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= controlCardDescMaxRunes {
		return string(runes)
	}
	return string(runes[:controlCardDescMaxRunes]) + "…"
}

func trimControlCardName(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= controlCardNameRunes {
		return string(runes)
	}
	return string(runes[:controlCardNameRunes]) + "…"
}

func controlButton(
	text string,
	key string,
) templateCardButton {
	return templateCardButton{
		Text:  strings.TrimSpace(text),
		Style: templateCardButtonStyleDefault,
		Key:   strings.TrimSpace(key),
	}
}

func controlButtons(buttons ...templateCardButton) []templateCardButton {
	return buttons
}

func buildControlHomeCard(
	assistantName string,
	personaName string,
	workspace string,
	modelName string,
	taskID string,
) *templateCard {
	desc := resolveAssistantDisplayName(assistantName) +
		" · " + controlCardTitleHome
	lines := []string{
		"💬 常用操作直接点按钮。",
		"🎭 人格：" + compactControlCardText(personaName),
		"💻 工作区：" + compactControlWorkspaceDisplay(workspace),
	}
	if modelLine := formatModelDisplayLine(modelName); modelLine != "" {
		lines = append(
			lines,
			"🤖 模型："+compactControlCardText(modelName),
		)
	}
	lines = append(
		lines,
		"📜 全命令：/help all",
	)
	return buildControlCard(
		desc,
		"欢迎回来，先点需要的面板就行。",
		strings.Join(lines, "\n"),
		taskID,
		nil,
		controlButtons(
			controlButton(
				controlCardButtonPersona,
				controlCardEventPersona,
			),
			controlButton(
				controlCardButtonStatus,
				controlCardEventStatus,
			),
			controlButton(
				controlCardButtonSessions,
				controlCardEventSessions,
			),
			controlButton(
				controlCardButtonCron,
				controlCardEventCron,
			),
			controlButton(
				controlCardButtonWorkspace,
				controlCardEventWorkspace,
			),
			controlButton(
				controlCardButtonHelp,
				controlCardEventHelp,
			),
		),
	)
}

func buildControlHelpCard(
	assistantName string,
	taskID string,
	page int,
) *templateCard {
	lines, buttons := buildControlHelpPage(page)
	return buildControlCard(
		resolveAssistantDisplayName(assistantName)+" · "+
			controlCardTitleHelp,
		"卡片负责常用入口，全文命令走文本。",
		strings.Join(lines, "\n"),
		taskID,
		nil,
		buttons,
	)
}

func buildControlHelpPage(
	page int,
) ([]string, []templateCardButton) {
	page = normalizeControlHelpPage(page)

	lines := []string{
		"🎯 先点卡片，复杂输入再用 slash。",
	}
	buttons := []templateCardButton{
		controlButton(
			controlCardButtonHome,
			controlCardEventHome,
		),
	}

	switch page {
	case controlHelpPageDefault:
		lines = append(
			lines,
			"📍 /status 看当前进度和最近输出。",
			"🎭 /persona 切换或新增人格。",
			"🛠 /runtime 打开升级和重启面板。",
			"📘 详解：/help runtime 或 /runtime help",
			"📜 全命令：/help all",
			controlHelpPageIndicator(
				page,
				controlHelpPageLabelCommon,
			),
		)
		buttons = append(
			buttons,
			controlButton(
				controlCardButtonPersona,
				controlCardEventPersona,
			),
			controlButton(
				controlCardButtonStatus,
				controlCardEventStatus,
			),
			controlButton(
				controlCardButtonRuntime,
				controlCardEventRuntime,
			),
		)
	case controlHelpPageCommands:
		lines = append(
			lines,
			"🧵 /sessions 看最近会话，/switch <序号> 切换。",
			"⏰ /cron list 查看并管理任务。",
			"💻 /workspace /path/to/repo 设代码工作区。",
			controlHelpPageIndicator(
				page,
				controlHelpPageLabelCommands,
			),
		)
		buttons = append(
			buttons,
			controlButton(
				controlCardButtonSessions,
				controlCardEventSessions,
			),
			controlButton(
				controlCardButtonCron,
				controlCardEventCron,
			),
			controlButton(
				controlCardButtonWorkspace,
				controlCardEventWorkspace,
			),
		)
	case controlHelpPageSessions:
		lines = append(
			lines,
			"🆕 /new 开始新会话，清掉当前上下文。",
			"↩ /recall 切回 /new 前的上一会话。",
			"⛔ /cancel 中断当前正在执行的请求。",
			"📜 全命令：/help all",
			controlHelpPageIndicator(
				page,
				controlHelpPageLabelSessions,
			),
		)
		buttons = append(
			buttons,
			controlButton(
				controlCardButtonNew,
				controlCardEventSessionNew,
			),
			controlButton(
				controlCardButtonRecall,
				controlCardEventSessionRecall,
			),
			controlButton(
				controlCardButtonCancel,
				controlCardEventCancel,
			),
		)
	}

	buttons = append(
		buttons,
		controlButton(
			controlCardButtonPrevPage,
			controlHelpPrevEvent(controlHelpPrevPage(page)),
		),
		controlButton(
			controlCardButtonNextPage,
			controlHelpNextEvent(controlHelpNextPage(page)),
		),
	)
	return lines, buttons
}

func controlHelpPageIndicator(
	page int,
	label string,
) string {
	line := fmt.Sprintf(
		controlHelpPageIndicatorFmt,
		normalizeControlHelpPage(page)+1,
		controlHelpPageCount,
	)
	label = strings.TrimSpace(label)
	if label == "" {
		return line
	}
	return line + " · " + label
}

func buildControlStatusCard(
	assistantName string,
	body string,
	taskID string,
) *templateCard {
	return buildControlCard(
		resolveAssistantDisplayName(assistantName)+" · "+
			controlCardTitleStatus,
		"这里会显示当前进度、排队和最近输出。",
		body,
		taskID,
		nil,
		controlButtons(
			controlButton(
				controlCardButtonCancel,
				controlCardEventCancel,
			),
			controlButton(
				controlCardButtonHome,
				controlCardEventHome,
			),
			controlButton(
				controlCardButtonPersona,
				controlCardEventPersona,
			),
			controlButton(
				controlCardButtonSessions,
				controlCardEventSessions,
			),
			controlButton(
				controlCardButtonCron,
				controlCardEventCron,
			),
			controlButton(
				controlCardButtonWorkspace,
				controlCardEventWorkspace,
			),
		),
	)
}

func buildControlSessionsCard(
	assistantName string,
	nameState assistantNameState,
	info *sessionInfo,
	timeout time.Duration,
	workspace string,
	taskID string,
	note string,
) *templateCard {
	bodyLines := []string{}
	note = strings.TrimSpace(note)
	if note != "" {
		bodyLines = append(bodyLines, note)
	}
	bodyLines = append(
		bodyLines,
		formatSessionList(info, controlCardListMaxItems),
	)
	return buildControlCard(
		resolveAssistantDisplayName(assistantName)+" · "+
			controlCardTitleSessions,
		cleanControlCardDesc(
			formatSessionOverviewForCard(
				nameState,
				info,
				timeout,
				workspace,
			),
		),
		strings.Join(bodyLines, "\n\n"),
		taskID,
		buildControlSessionSelection(info),
		controlButtons(
			controlButton(
				controlCardButtonSwitch,
				controlCardEventSessionSwitch,
			),
			controlButton(
				controlCardButtonRecall,
				controlCardEventSessionRecall,
			),
			controlButton(
				controlCardButtonNew,
				controlCardEventSessionNew,
			),
			controlButton(
				controlCardButtonHome,
				controlCardEventHome,
			),
			controlButton(
				controlCardButtonStatus,
				controlCardEventStatus,
			),
			controlButton(
				controlCardButtonPersona,
				controlCardEventPersona,
			),
		),
	)
}

func buildControlSessionSelection(
	info *sessionInfo,
) *templateCardSelection {
	if info == nil || len(info.history) == 0 {
		return nil
	}
	limit := controlCardListMaxItems
	if len(info.history) < limit {
		limit = len(info.history)
	}
	options := make([]templateCardOption, 0, limit)
	for i := 0; i < limit; i++ {
		entry := info.history[i]
		text := strconv.Itoa(i+1) + ". "
		if entry.SessionID == info.sessionID {
			text += "当前"
		} else {
			text += trimControlCardName(
				sessionDisplayLabel(
					info.baseSessionID,
					entry.SessionID,
				),
			)
		}
		options = append(options, templateCardOption{
			ID:        strings.TrimSpace(entry.SessionID),
			Text:      text,
			IsChecked: entry.SessionID == info.sessionID,
		})
	}
	return &templateCardSelection{
		QuestionKey: controlCardSessionQuestionKey,
		Title:       "最近会话",
		SelectedID:  strings.TrimSpace(info.sessionID),
		OptionList:  options,
	}
}

func buildControlCronCard(
	assistantName string,
	jobs []gwclient.ScheduledJobSummary,
	taskID string,
	note string,
	selectedID string,
) *templateCard {
	desc := "点按钮管理当前聊天的定时任务。"
	body := buildControlCronBody(jobs, note, selectedID)
	selection := buildControlCronSelection(jobs, selectedID)
	buttons := buildControlCronButtons(jobs)
	return buildControlCard(
		resolveAssistantDisplayName(assistantName)+" · "+
			controlCardTitleCron,
		desc,
		body,
		taskID,
		selection,
		buttons,
	)
}

func buildControlCronBody(
	jobs []gwclient.ScheduledJobSummary,
	note string,
	selectedID string,
) string {
	lines := []string{}
	if strings.TrimSpace(note) != "" {
		lines = append(lines, strings.TrimSpace(note))
	}
	if len(jobs) == 0 {
		lines = append(
			lines,
			"当前聊天还没有定时任务。",
			"💡 直接说：每 10 分钟提醒我喝水。",
		)
		return strings.Join(lines, "\n")
	}
	if job, ok := findControlCronJob(jobs, selectedID); ok {
		lines = append(lines, formatCronJobDetails(job))
		return strings.Join(lines, "\n")
	}
	lines = append(lines, formatCronJobList(jobs))
	return strings.Join(lines, "\n")
}

func buildControlCronSelection(
	jobs []gwclient.ScheduledJobSummary,
	selectedID string,
) *templateCardSelection {
	if len(jobs) == 0 {
		return nil
	}
	limit := controlCardListMaxItems
	if len(jobs) < limit {
		limit = len(jobs)
	}
	options := make([]templateCardOption, 0, limit)
	selected := strings.TrimSpace(selectedID)
	if selected == "" {
		selected = strings.TrimSpace(jobs[0].ID)
	}
	for i := 0; i < limit; i++ {
		job := jobs[i]
		text := strconv.Itoa(i+1) + ". " +
			trimControlCardName(cronJobDisplayName(job))
		options = append(options, templateCardOption{
			ID:        strings.TrimSpace(job.ID),
			Text:      text,
			IsChecked: strings.TrimSpace(job.ID) == selected,
		})
	}
	return &templateCardSelection{
		QuestionKey: controlCardCronQuestionKey,
		Title:       "当前任务",
		SelectedID:  selected,
		OptionList:  options,
	}
}

func buildControlCronButtons(
	jobs []gwclient.ScheduledJobSummary,
) []templateCardButton {
	if len(jobs) == 0 {
		return controlHomeNavButtons()
	}
	return controlButtons(
		controlButton(
			controlCardButtonCronDetail,
			controlCardEventCronDetails,
		),
		controlButton(
			controlCardButtonCronStop,
			controlCardEventCronStop,
		),
		controlButton(
			controlCardButtonCronResume,
			controlCardEventCronResume,
		),
		controlButton(
			controlCardButtonCronRemove,
			controlCardEventCronRemove,
		),
		controlButton(
			controlCardButtonCronClear,
			controlCardEventCronClear,
		),
		controlButton(
			controlCardButtonHome,
			controlCardEventHome,
		),
	)
}

func findControlCronJob(
	jobs []gwclient.ScheduledJobSummary,
	selectedID string,
) (gwclient.ScheduledJobSummary, bool) {
	selectedID = strings.TrimSpace(selectedID)
	if selectedID == "" {
		return gwclient.ScheduledJobSummary{}, false
	}
	for _, job := range jobs {
		if strings.TrimSpace(job.ID) == selectedID {
			return job, true
		}
	}
	return gwclient.ScheduledJobSummary{}, false
}

func buildControlWorkspaceCard(
	assistantName string,
	custom string,
	fallback string,
	taskID string,
	note string,
) *templateCard {
	lines := []string{}
	if strings.TrimSpace(note) != "" {
		lines = append(lines, strings.TrimSpace(note))
	}
	lines = append(
		lines,
		formatWorkspaceStatus(custom, fallback),
		"💡 想设置任意目录时，直接发 /workspace /path/to/repo。",
	)
	return buildControlCard(
		resolveAssistantDisplayName(assistantName)+" · "+
			controlCardTitleWorkspace,
		"当前聊天的代码工作区设置。",
		strings.Join(lines, "\n"),
		taskID,
		nil,
		controlButtons(
			controlButton(
				controlCardButtonWsClear,
				controlCardEventWorkspaceClear,
			),
			controlButton(
				controlCardButtonHome,
				controlCardEventHome,
			),
			controlButton(
				controlCardButtonStatus,
				controlCardEventStatus,
			),
			controlButton(
				controlCardButtonSessions,
				controlCardEventSessions,
			),
			controlButton(
				controlCardButtonCron,
				controlCardEventCron,
			),
			controlButton(
				controlCardButtonPersona,
				controlCardEventPersona,
			),
		),
	)
}

func buildControlRuntimeCard(
	assistantName string,
	statusText string,
	taskID string,
	note string,
) *templateCard {
	lines := []string{}
	note = strings.TrimSpace(note)
	if note != "" {
		lines = append(lines, note)
	}
	lines = append(
		lines,
		strings.TrimSpace(statusText),
		runtimeHintHelp,
	)
	return buildControlCard(
		resolveAssistantDisplayName(assistantName)+" · "+
			controlCardTitleRuntime,
		"点按钮执行升级或重启；强制动作会二次确认。",
		strings.Join(lines, "\n\n"),
		taskID,
		nil,
		controlButtons(
			controlButton(
				controlCardButtonUpgrade,
				controlCardEventRuntimeUpgrade,
			),
			controlButton(
				controlCardButtonForceUpg,
				controlCardEventRuntimeForceUpgradePrompt,
			),
			controlButton(
				controlCardButtonRestart,
				controlCardEventRuntimeRestart,
			),
			controlButton(
				controlCardButtonForceRst,
				controlCardEventRuntimeForceRestartPrompt,
			),
			controlButton(
				controlCardButtonHome,
				controlCardEventHome,
			),
			controlButton(
				controlCardButtonStatus,
				controlCardEventStatus,
			),
		),
	)
}

func buildControlRuntimeConfirmCard(
	assistantName string,
	statusText string,
	taskID string,
	title string,
	confirmEvent string,
) *templateCard {
	lines := []string{
		title,
		"⚠️ 强制动作会中断当前所有运行中的任务。",
		strings.TrimSpace(statusText),
	}
	return buildControlCard(
		resolveAssistantDisplayName(assistantName)+" · "+
			controlCardTitleRuntime,
		"请再次确认后执行。",
		strings.Join(lines, "\n\n"),
		taskID,
		nil,
		controlButtons(
			controlButton(
				controlCardButtonConfirm,
				confirmEvent,
			),
			controlButton(
				controlCardButtonBack,
				controlCardEventRuntime,
			),
			controlButton(
				controlCardButtonHome,
				controlCardEventHome,
			),
			controlButton(
				controlCardButtonStatus,
				controlCardEventStatus,
			),
		),
	)
}

func controlHomeNavButtons() []templateCardButton {
	return controlButtons(
		controlButton(
			controlCardButtonHome,
			controlCardEventHome,
		),
		controlButton(
			controlCardButtonPersona,
			controlCardEventPersona,
		),
		controlButton(
			controlCardButtonStatus,
			controlCardEventStatus,
		),
		controlButton(
			controlCardButtonSessions,
			controlCardEventSessions,
		),
		controlButton(
			controlCardButtonCron,
			controlCardEventCron,
		),
		controlButton(
			controlCardButtonWorkspace,
			controlCardEventWorkspace,
		),
	)
}

func (c *Channel) sendRuntimeControlCard(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	sender messageSender,
	note string,
) bool {
	cardSender, ok := sender.(templateCardSender)
	if !ok || cardSender == nil {
		return false
	}
	status := c.runtimeLifecycleStatus()
	card := buildControlRuntimeCard(
		c.assistantDisplayNameForSession(baseSessionID),
		c.formatRuntimeLifecycleStatus(ctx, status),
		newInteractiveCardTaskID(
			controlCardTaskPrefix,
			baseSessionID,
		),
		note,
	)
	if err := cardSender.SendTemplateCard(ctx, chatID, card); err != nil {
		log.WarnfContext(
			ctx,
			"wecom: send runtime control card failed: %v",
			err,
		)
		return false
	}
	return true
}

func cleanControlCardDesc(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n\n", "\n")
	return text
}

func defaultChatPersonaDisplay() string {
	def, ok := personaapi.LookupBuiltin(defaultChatPersonaID)
	if !ok {
		return defaultChatPersonaID
	}
	if strings.TrimSpace(def.Name) == "" {
		return def.ID
	}
	return def.Name
}

func formatSessionOverviewForCard(
	nameState assistantNameState,
	info *sessionInfo,
	timeout time.Duration,
	workspace string,
) string {
	if info == nil {
		return statusLabelSession + "默认会话\n" +
			statusLabelAssistant +
			compactControlCardText(
				formatAssistantNameSummary(nameState),
			) + "\n" +
			statusLabelPersona + defaultChatPersonaDisplay() + "\n" +
			statusLabelWorkspace + workspaceDisplayUnset + "\n" +
			statusLabelTimeout + formatTimeoutSetting(timeout)
	}
	return strings.Join([]string{
		statusLabelSession + sessionDisplayLabel(
			info.baseSessionID,
			info.sessionID,
		),
		statusLabelAssistant + compactControlCardText(
			formatAssistantNameSummary(nameState),
		),
		statusLabelPersona + controlCardPersonaDisplay(
			info.effectivePersonaID(),
		),
		statusLabelWorkspace + compactControlWorkspaceDisplay(
			formatWorkspaceDisplay(
				info.workspacePath,
				workspace,
			),
		),
		statusLabelHistory + strconv.Itoa(len(info.history)),
	}, "\n")
}

func controlCardPersonaDisplay(personaID string) string {
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		return defaultChatPersonaDisplay()
	}
	def, ok := personaapi.LookupBuiltin(personaID)
	if !ok {
		return compactControlCardText(personaID)
	}
	if strings.TrimSpace(def.Name) == "" {
		return compactControlCardText(def.ID)
	}
	return compactControlCardText(def.Name)
}

func compactControlCardText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= controlCardLineMaxRunes {
		return text
	}
	return string(runes[:controlCardLineMaxRunes-1]) + "…"
}

func compactControlWorkspaceDisplay(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == workspaceDisplayUnset {
		return workspaceDisplayUnset
	}
	base := strings.TrimSpace(filepath.Base(path))
	if base != "" && base != "." && base != string(filepath.Separator) {
		short := "…/" + base
		return compactControlCardText(short)
	}
	return compactControlCardText(path)
}

func (c *Channel) sendHomeControlCard(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	sender messageSender,
) bool {
	cardSender, ok := sender.(templateCardSender)
	if !ok || cardSender == nil {
		return false
	}
	info := c.sessionTracker.getOrCreateSession(baseSessionID, 0)
	card := buildControlHomeCard(
		c.assistantDisplayNameForSession(baseSessionID),
		c.activePersonaDisplay(info),
		c.effectiveWorkspacePath(info),
		c.runtimeModelDisplayName(),
		newInteractiveCardTaskID(
			controlCardTaskPrefix,
			baseSessionID,
		),
	)
	if err := cardSender.SendTemplateCard(ctx, chatID, card); err != nil {
		log.WarnfContext(
			ctx,
			"wecom: send home control card failed: %v",
			err,
		)
		return false
	}
	c.rememberSessionCard(
		baseSessionID,
		sessionCardViewHome,
		card.TaskID,
		info,
	)
	return true
}

func (c *Channel) updateControlCard(
	ctx context.Context,
	updater templateCardUpdater,
	card *templateCard,
) error {
	if updater == nil || card == nil {
		return nil
	}
	return updater.UpdateTemplateCard(ctx, card)
}

func (c *Channel) controlCardFallbackText(
	baseSessionID string,
	view string,
) string {
	info := c.sessionTracker.getOrCreateSession(baseSessionID, 0)
	switch view {
	case controlCardEventStatus:
		return c.statusMessageText(
			info,
			c.runStatus.snapshot(info.sessionID),
			c.effectiveWorkspacePath(info),
		)
	case controlCardEventSessions:
		return formatSessionList(info, controlCardListMaxItems)
	case controlCardEventWorkspace:
		return formatWorkspaceStatus(
			info.workspacePath,
			c.effectiveWorkspacePath(info),
		)
	case controlCardEventRuntime:
		if c.runtimeLifecycle == nil {
			return "当前环境未启用运行时控制。"
		}
		return c.formatRuntimeLifecycleStatus(
			context.Background(),
			c.runtimeLifecycleStatus(),
		)
	case controlCardEventHelp:
		return c.helpMessage
	default:
		if _, ok := parseControlHelpPageEvent(view); ok {
			return c.helpMessage
		}
		return buildEnterChatWelcomeMessage(
			c.assistantDisplayNameForSession(baseSessionID),
			c.runtimeModelDisplayName(),
		)
	}
}

func (c *Channel) controlDeliveryTarget(
	chatID string,
	fromID string,
) (delivery.Target, bool) {
	return buildDefaultDeliveryTarget(chatID, fromID)
}

func (c *Channel) handleControlTemplateCardEvent(
	ctx context.Context,
	msg WebhookMessage,
	event *TemplateCardEvent,
) error {
	fromID := messageTransportUserID(msg)
	if !c.isUserAllowed(fromID) {
		return nil
	}

	baseSessionID := buildScopedSessionID(
		msg.ChatID,
		fromID,
		c.groupSessionMode,
	)
	sender := c.senderForMsg(msg)
	updater, ok := sender.(templateCardUpdater)
	if !ok || updater == nil {
		if sender != nil {
			_ = sender.SendText(
				ctx,
				msg.ChatID,
				c.controlCardFallbackText(
					baseSessionID,
					strings.TrimSpace(event.EventKey),
				),
			)
		}
		return nil
	}

	card, err := c.buildControlEventCard(
		ctx,
		msg.ChatID,
		baseSessionID,
		fromID,
		msg.ResponseURL,
		msg.CallbackReqID,
		event,
	)
	if err != nil {
		return err
	}
	if err := c.updateControlCard(ctx, updater, card); err != nil {
		return err
	}
	if view := statefulControlCardView(event.EventKey); view != "" {
		c.rememberSessionCard(
			baseSessionID,
			view,
			card.TaskID,
			c.sessionTracker.getSession(baseSessionID),
		)
	}
	return nil
}

func (c *Channel) buildControlEventCard(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	fromID string,
	responseURL string,
	replyReqID string,
	event *TemplateCardEvent,
) (*templateCard, error) {
	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	taskID := c.controlTaskID(event, baseSessionID)
	assistantName := c.assistantDisplayNameForSession(
		baseSessionID,
	)
	eventKey := strings.TrimSpace(event.EventKey)
	if page, ok := parseControlHelpPageEvent(eventKey); ok {
		return buildControlHelpCard(
			assistantName,
			taskID,
			page,
		), nil
	}

	switch eventKey {
	case controlCardEventHome:
		return buildControlHomeCard(
			assistantName,
			c.activePersonaDisplay(sessionInfo),
			c.effectiveWorkspacePath(sessionInfo),
			c.runtimeModelDisplayName(),
			taskID,
		), nil
	case controlCardEventHelp:
		return buildControlHelpCard(
			assistantName,
			taskID,
			controlHelpPageDefault,
		), nil
	case controlCardEventPersona:
		defs, err := c.listPersonas()
		if err != nil {
			return nil, err
		}
		return buildPersonaSettingsCard(
			assistantName,
			c.activePersonaDisplay(sessionInfo),
			sessionInfo,
			defs,
			taskID,
			personaCardViewDefault,
			"",
			c.personaStorageEnabled(),
		), nil
	case controlCardEventStatus:
		return buildControlStatusCard(
			assistantName,
			c.statusMessageText(
				sessionInfo,
				c.runStatus.snapshot(sessionInfo.sessionID),
				c.effectiveWorkspacePath(sessionInfo),
			),
			taskID,
		), nil
	case controlCardEventCancel:
		note := c.cancelControlRun(ctx, sessionInfo)
		return buildControlStatusCard(
			assistantName,
			note+"\n\n"+c.statusMessageText(
				sessionInfo,
				c.runStatus.snapshot(sessionInfo.sessionID),
				c.effectiveWorkspacePath(sessionInfo),
			),
			taskID,
		), nil
	case controlCardEventSessions:
		return buildControlSessionsCard(
			assistantName,
			c.assistantNameStateForInfo(sessionInfo),
			sessionInfo,
			c.sessionTimeout,
			c.effectiveWorkspacePath(sessionInfo),
			taskID,
			"",
		), nil
	case controlCardEventSessionSwitch:
		selectedID := resolveControlSessionSelection(
			sessionInfo,
			event,
		)
		switched, ok := c.sessionTracker.switchSession(
			baseSessionID,
			selectedID,
		)
		note := "没有找到对应会话。"
		if ok {
			sessionInfo = switched
			note = "✅ 已切换到：" + sessionDisplayLabel(
				sessionInfo.baseSessionID,
				sessionInfo.sessionID,
			)
		}
		return buildControlSessionsCard(
			assistantName,
			c.assistantNameStateForInfo(sessionInfo),
			sessionInfo,
			c.sessionTimeout,
			c.effectiveWorkspacePath(sessionInfo),
			taskID,
			note,
		), nil
	case controlCardEventSessionNew:
		sessionInfo = c.sessionTracker.startNewSession(baseSessionID)
		return buildControlSessionsCard(
			assistantName,
			c.assistantNameStateForInfo(sessionInfo),
			sessionInfo,
			c.sessionTimeout,
			c.effectiveWorkspacePath(sessionInfo),
			taskID,
			"✅ 已开始一个新的分会话。",
		), nil
	case controlCardEventSessionRecall:
		recalled, ok := c.sessionTracker.recallPreviousSession(
			baseSessionID,
		)
		note := defaultRecallNoopMessage
		if ok {
			sessionInfo = recalled
			note = defaultRecallMessage
		}
		return buildControlSessionsCard(
			assistantName,
			c.assistantNameStateForInfo(sessionInfo),
			sessionInfo,
			c.sessionTimeout,
			c.effectiveWorkspacePath(sessionInfo),
			taskID,
			note,
		), nil
	case controlCardEventCron,
		controlCardEventCronDetails,
		controlCardEventCronStop,
		controlCardEventCronResume,
		controlCardEventCronRemove,
		controlCardEventCronClear:
		return c.buildControlCronEventCard(
			ctx,
			chatID,
			baseSessionID,
			fromID,
			assistantName,
			taskID,
			event,
		)
	case controlCardEventWorkspace:
		return buildControlWorkspaceCard(
			assistantName,
			sessionInfo.workspacePath,
			c.effectiveWorkspacePath(sessionInfo),
			taskID,
			"",
		), nil
	case controlCardEventRuntime:
		return buildControlRuntimeCard(
			assistantName,
			c.formatRuntimeLifecycleStatus(
				ctx,
				c.runtimeLifecycleStatus(),
			),
			taskID,
			"",
		), nil
	case controlCardEventRuntimeRestart:
		return c.buildControlRuntimeActionCard(
			ctx,
			chatID,
			assistantName,
			taskID,
			fromID,
			responseURL,
			replyReqID,
			runtimectl.ActionRequest{
				Kind:   runtimectl.ActionRestart,
				Mode:   runtimectl.ModeGraceful,
				Actor:  fromID,
				Source: "card",
			},
		)
	case controlCardEventRuntimeUpgrade:
		return c.buildControlRuntimeActionCard(
			ctx,
			chatID,
			assistantName,
			taskID,
			fromID,
			responseURL,
			replyReqID,
			runtimectl.ActionRequest{
				Kind:   runtimectl.ActionUpgrade,
				Mode:   runtimectl.ModeGraceful,
				Actor:  fromID,
				Source: "card",
			},
		)
	case controlCardEventRuntimeForceRestartPrompt:
		return buildControlRuntimeConfirmCard(
			assistantName,
			c.formatRuntimeLifecycleStatus(
				ctx,
				c.runtimeLifecycleStatus(),
			),
			taskID,
			"确认强制重启当前运行中的实例。",
			controlCardEventRuntimeForceRestart,
		), nil
	case controlCardEventRuntimeForceUpgradePrompt:
		return buildControlRuntimeConfirmCard(
			assistantName,
			c.formatRuntimeLifecycleStatus(
				ctx,
				c.runtimeLifecycleStatus(),
			),
			taskID,
			"确认强制升级到最新版本。",
			controlCardEventRuntimeForceUpgrade,
		), nil
	case controlCardEventRuntimeForceRestart:
		return c.buildControlRuntimeActionCard(
			ctx,
			chatID,
			assistantName,
			taskID,
			fromID,
			responseURL,
			replyReqID,
			runtimectl.ActionRequest{
				Kind:   runtimectl.ActionRestart,
				Mode:   runtimectl.ModeForce,
				Actor:  fromID,
				Source: "card",
			},
		)
	case controlCardEventRuntimeForceUpgrade:
		return c.buildControlRuntimeActionCard(
			ctx,
			chatID,
			assistantName,
			taskID,
			fromID,
			responseURL,
			replyReqID,
			runtimectl.ActionRequest{
				Kind:   runtimectl.ActionUpgrade,
				Mode:   runtimectl.ModeForce,
				Actor:  fromID,
				Source: "card",
			},
		)
	case controlCardEventWorkspaceClear:
		sessionInfo = c.sessionTracker.setWorkspace(
			baseSessionID,
			"",
		)
		return buildControlWorkspaceCard(
			assistantName,
			sessionInfo.workspacePath,
			c.effectiveWorkspacePath(sessionInfo),
			taskID,
			"✅ 已清除当前聊天的工作区覆盖。",
		), nil
	default:
		return buildControlHomeCard(
			assistantName,
			c.activePersonaDisplay(sessionInfo),
			c.effectiveWorkspacePath(sessionInfo),
			c.runtimeModelDisplayName(),
			taskID,
		), nil
	}
}

func (c *Channel) buildControlRuntimeActionCard(
	ctx context.Context,
	chatID string,
	assistantName string,
	taskID string,
	fromID string,
	responseURL string,
	replyReqID string,
	req runtimectl.ActionRequest,
) (*templateCard, error) {
	if c.runtimeLifecycle == nil {
		return buildControlRuntimeCard(
			assistantName,
			formatRuntimeLifecycleStatus(runtimectl.Status{}),
			taskID,
			"当前环境未启用运行时控制。",
		), nil
	}
	if !c.isRuntimeAdmin(fromID) {
		return buildControlRuntimeCard(
			assistantName,
			c.formatRuntimeLifecycleStatus(
				ctx,
				c.runtimeLifecycleStatus(),
			),
			taskID,
			runtimePermissionDenied,
		), nil
	}
	result, err := c.runtimeLifecycle.RequestAction(ctx, req)
	if err != nil && !result.Started {
		return buildControlRuntimeCard(
			assistantName,
			c.formatRuntimeLifecycleStatus(ctx, result.Status),
			taskID,
			c.formatRuntimeLifecycleStatus(ctx, result.Status),
		), nil
	}
	c.stageRuntimeCompletionNotice(
		result,
		chatID,
		fromID,
		responseURL,
		replyReqID,
	)
	return buildControlRuntimeCard(
		assistantName,
		c.formatRuntimeLifecycleStatus(ctx, result.Status),
		taskID,
		c.formatRuntimeActionResult(ctx, result),
	), nil
}

func (c *Channel) buildControlCronEventCard(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	fromID string,
	assistantName string,
	taskID string,
	event *TemplateCardEvent,
) (*templateCard, error) {
	manager, ok := c.gw.(scheduledJobManager)
	if !ok {
		return buildControlCronCard(
			assistantName,
			nil,
			taskID,
			"当前环境不支持定时任务管理。",
			"",
		), nil
	}
	target, ok := c.controlDeliveryTarget(chatID, fromID)
	if !ok {
		return buildControlCronCard(
			assistantName,
			nil,
			taskID,
			"当前聊天没有可用的回投目标。",
			"",
		), nil
	}
	jobs, err := manager.ListScheduledJobs(
		ctx,
		target.Channel,
		baseSessionID,
		target.Target,
	)
	if err != nil {
		return buildControlCronCard(
			assistantName,
			nil,
			taskID,
			"读取定时任务失败："+err.Error(),
			"",
		), nil
	}
	selectedID := resolveControlCronSelection(jobs, event)
	note := ""
	viewTarget := target

	switch strings.TrimSpace(event.EventKey) {
	case controlCardEventCronDetails, controlCardEventCron:
	case controlCardEventCronStop, controlCardEventCronResume:
		job, ok := findControlCronJob(jobs, selectedID)
		if !ok {
			note = "先选一个任务再操作。"
			break
		}
		enabled := strings.TrimSpace(event.EventKey) ==
			controlCardEventCronResume
		if enabled && cronJobReachedMaxRuns(job) {
			note = cronResumeBlockedMessage(job)
			break
		}
		viewTarget = resolveCronJobDeliveryTarget(
			target,
			job,
		)
		updated, setErr := manager.SetScheduledJobEnabled(
			ctx,
			viewTarget.Channel,
			baseSessionID,
			viewTarget.Target,
			job.ID,
			enabled,
		)
		if setErr != nil {
			note = "更新定时任务失败：" + setErr.Error()
			break
		}
		if enabled {
			note = "✅ 已恢复：" + cronJobDisplayName(updated)
		} else {
			note = "✅ 已停止：" + cronJobDisplayName(updated)
		}
	case controlCardEventCronRemove:
		job, ok := findControlCronJob(jobs, selectedID)
		if !ok {
			note = "先选一个任务再操作。"
			break
		}
		viewTarget = resolveCronJobDeliveryTarget(
			target,
			job,
		)
		removed, removeErr := manager.RemoveScheduledJob(
			ctx,
			viewTarget.Channel,
			baseSessionID,
			viewTarget.Target,
			job.ID,
		)
		if removeErr != nil {
			note = "删除定时任务失败：" + removeErr.Error()
			break
		}
		if removed {
			note = "✅ 已删除：" + cronJobDisplayName(job)
		}
	case controlCardEventCronClear:
		removed, clearErr := manager.ClearScheduledJobs(
			ctx,
			target.Channel,
			baseSessionID,
			target.Target,
		)
		if clearErr != nil {
			note = "清空定时任务失败：" + clearErr.Error()
			break
		}
		note = "✅ 已清空 " + strconv.Itoa(removed) + " 个任务。"
	}

	jobs, err = manager.ListScheduledJobs(
		ctx,
		viewTarget.Channel,
		baseSessionID,
		viewTarget.Target,
	)
	if err != nil {
		note = "读取定时任务失败：" + err.Error()
		jobs = nil
	}
	return buildControlCronCard(
		assistantName,
		jobs,
		taskID,
		note,
		selectedID,
	), nil
}

func resolveControlSessionSelection(
	info *sessionInfo,
	event *TemplateCardEvent,
) string {
	selectedID := selectedTemplateCardOption(
		event,
		controlCardSessionQuestionKey,
	)
	if strings.TrimSpace(selectedID) != "" {
		return strings.TrimSpace(selectedID)
	}
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.sessionID)
}

func resolveControlCronSelection(
	jobs []gwclient.ScheduledJobSummary,
	event *TemplateCardEvent,
) string {
	selectedID := selectedTemplateCardOption(
		event,
		controlCardCronQuestionKey,
	)
	if strings.TrimSpace(selectedID) != "" {
		return strings.TrimSpace(selectedID)
	}
	if len(jobs) == 0 {
		return ""
	}
	return strings.TrimSpace(jobs[0].ID)
}

func (c *Channel) cancelControlRun(
	ctx context.Context,
	info *sessionInfo,
) string {
	if info == nil {
		return c.cancelNoopMessage
	}
	requestID := c.inflight.Get(info.sessionID)
	if strings.TrimSpace(requestID) == "" {
		return c.cancelNoopMessage
	}
	canceled, err := c.gw.Cancel(ctx, requestID)
	if err != nil {
		return c.cancelFailedMessage + "\n" + err.Error()
	}
	if !canceled {
		return c.cancelNoopMessage
	}
	c.runStatus.cancel(info.sessionID, requestID)
	return c.cancelOKMessage
}

func (c *Channel) controlTaskID(
	event *TemplateCardEvent,
	baseSessionID string,
) string {
	if event != nil && strings.TrimSpace(event.TaskID) != "" {
		return strings.TrimSpace(event.TaskID)
	}
	return newInteractiveCardTaskID(
		controlCardTaskPrefix,
		baseSessionID,
	)
}
