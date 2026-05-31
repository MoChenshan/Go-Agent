package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/conversation"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

const (
	chatNameSourceOverride     = "Current chat name"
	chatNameSourceSameAsGlobal = "Current chat name " +
		"(same as default name)"
	chatNameSourceIdentity = "Default name from IDENTITY.md"
	chatNameSourceBotName  = "Default name from legacy bot_name"
	chatNameSourceRuntime  = "Default name from runtime product"

	chatOverrideHelpText = "Each chat can keep its own current name. " +
		"When a chat sets its own name, that name wins there. " +
		"When a chat does not set one, it falls back to the default " +
		"name. A current-chat name survives /new in that same chat. " +
		"Use /name <称呼> for one chat, or /name global <称呼> " +
		"for the default name."

	wecomUserLabelModeKey           = "user_label_mode"
	wecomUserLookupCommandConfigKey = "user_identity_lookup_command"

	adminChatHistorySessionLimit  = 12
	adminChatHistoryVisibleCount  = 5
	adminChatHistoryPageTurnCount = 12
	adminChatTranscriptTextLimit  = 1500

	adminChatHistoryItemSession = "session"
	adminChatHistoryItemTurn    = "turn"

	adminChatHistoryLabelCurrent = "Current session"
	adminChatHistoryLabelRecall  = "Recall session"
	adminChatHistoryLabelRecent  = "Recent session"
)

var (
	_ admin.ChatsProvider       = (*runtimeAdminProvider)(nil)
	_ admin.ChatDetailProvider  = (*runtimeAdminProvider)(nil)
	_ admin.ChatHistoryProvider = (*runtimeAdminProvider)(nil)
)

func (p *runtimeAdminProvider) ChatsStatus() (
	admin.ChatsStatus,
	error,
) {
	state, err := p.loadState()
	if err != nil {
		return admin.ChatsStatus{}, err
	}
	tracked, err := wecomchannel.ListTrackedChats(p.stateDir)
	if err != nil {
		return admin.ChatsStatus{}, err
	}

	personaLabels := personaLabelIndex(state)
	globalName := strings.TrimSpace(state.AssistantName.EffectiveName)
	knownUserLabels := resolveTrackedChatKnownUserLabels(
		state,
		tracked,
		p.stateDir,
	)
	status := admin.ChatsStatus{
		Enabled:              true,
		GlobalAssistantName:  globalName,
		RuntimeAssistantName: state.AssistantName.RuntimeProduct,
		GlobalAssistantSource: globalAssistantSourceLabel(
			state.AssistantName,
		),
		ChatOverrideHelp: chatOverrideHelpText,
		Chats:            make([]admin.ChatView, 0, len(tracked)),
	}
	for _, chat := range tracked {
		status.Chats = append(
			status.Chats,
			buildAdminChatView(
				chat,
				globalName,
				status.GlobalAssistantSource,
				personaLabels,
				knownUserLabels,
			),
		)
	}
	return status, nil
}

func (p *runtimeAdminProvider) ChatDetail(
	baseSessionID string,
) (admin.ChatView, error) {
	baseSessionID = strings.TrimSpace(baseSessionID)
	if baseSessionID == "" {
		return admin.ChatView{}, fmt.Errorf("chat_id is required")
	}
	state, err := p.loadState()
	if err != nil {
		return admin.ChatView{}, err
	}
	tracked, err := wecomchannel.ListTrackedChats(p.stateDir)
	if err != nil {
		return admin.ChatView{}, err
	}
	chat, ok := findTrackedChatState(tracked, baseSessionID)
	if !ok {
		return admin.ChatView{}, fmt.Errorf("tracked chat not found")
	}

	personaLabels := personaLabelIndex(state)
	globalName := strings.TrimSpace(state.AssistantName.EffectiveName)
	knownUserLabels := resolveTrackedChatKnownUserLabels(
		state,
		[]wecomchannel.TrackedChatState{chat},
		p.stateDir,
	)
	detail := buildAdminChatView(
		chat,
		globalName,
		globalAssistantSourceLabel(state.AssistantName),
		personaLabels,
		knownUserLabels,
	)
	return detail, nil
}

func (p *runtimeAdminProvider) ChatHistory(
	baseSessionID string,
	cursor string,
) (admin.ChatHistoryPage, error) {
	baseSessionID = strings.TrimSpace(baseSessionID)
	if baseSessionID == "" {
		return admin.ChatHistoryPage{}, fmt.Errorf("chat_id is required")
	}
	state, err := p.loadState()
	if err != nil {
		return admin.ChatHistoryPage{}, err
	}
	tracked, err := wecomchannel.ListTrackedChats(p.stateDir)
	if err != nil {
		return admin.ChatHistoryPage{}, err
	}
	chat, ok := findTrackedChatState(tracked, baseSessionID)
	if !ok {
		return admin.ChatHistoryPage{}, fmt.Errorf("tracked chat not found")
	}
	knownUserLabels := resolveTrackedChatKnownUserLabels(
		state,
		[]wecomchannel.TrackedChatState{chat},
		p.stateDir,
	)
	history, bounded := buildAdminChatHistorySessions(
		p.appName,
		p.sessionSvc,
		chat,
		knownUserLabels,
	)
	return buildAdminChatHistoryPage(
		baseSessionID,
		history,
		bounded,
		cursor,
	)
}

func globalAssistantSourceLabel(
	state runtimeAssistantNameState,
) string {
	if strings.TrimSpace(state.ConfiguredName) != "" {
		return chatNameSourceIdentity
	}
	switch strings.TrimSpace(state.FallbackSource) {
	case assistantNameFallbackBotName:
		return chatNameSourceBotName
	case assistantNameFallbackRuntime:
		return chatNameSourceRuntime
	default:
		return chatNameSourceIdentity
	}
}

func buildAdminChatView(
	chat wecomchannel.TrackedChatState,
	globalName string,
	globalSource string,
	personaLabels map[string]string,
	knownUserLabels map[string]string,
) admin.ChatView {
	override := strings.TrimSpace(chat.AssistantAlias)
	nameSource := strings.TrimSpace(globalSource)
	if override != "" {
		nameSource = chatNameSourceOverride
		if override == globalName {
			nameSource = chatNameSourceSameAsGlobal
		}
	}

	history := make(
		[]admin.ChatSessionView,
		0,
		len(chat.History),
	)
	historyTotalCount := len(chat.History)
	historyTruncated := false
	if len(chat.History) > adminChatHistorySessionLimit {
		chat.History = chat.History[:adminChatHistorySessionLimit]
		historyTruncated = true
	}
	for i, entry := range chat.History {
		history = append(history, admin.ChatSessionView{
			SessionID:    strings.TrimSpace(entry.SessionID),
			LastActivity: entry.LastActivity,
			Visible:      i < adminChatHistoryVisibleCount,
		})
	}

	effectiveAssistant := globalName
	if override != "" {
		effectiveAssistant = override
	}
	personaID := strings.TrimSpace(chat.PersonaID)
	return admin.ChatView{
		BaseSessionID: strings.TrimSpace(chat.BaseSessionID),
		DisplayLabel: wecomchannel.FormatTrackedChatDisplayLabel(
			chat,
			knownUserLabels,
		),
		Kind:                  strings.TrimSpace(chat.Kind),
		KindLabel:             strings.TrimSpace(chat.KindLabel),
		CurrentSessionID:      strings.TrimSpace(chat.CurrentSessionID),
		RecallSessionID:       strings.TrimSpace(chat.RecallSessionID),
		LastActivity:          chat.LastActivity,
		Epoch:                 chat.Epoch,
		EffectiveAssistant:    effectiveAssistant,
		ChatAssistantOverride: override,
		NameSource:            nameSource,
		OverridesGlobal:       override != "" && override != globalName,
		PersonaID:             personaID,
		PersonaLabel:          personaLabelForID(personaID, personaLabels),
		PersonaPinned:         chat.PersonaPinned,
		WorkspacePath: strings.TrimSpace(
			chat.WorkspacePath,
		),
		KnownUserIDs: append(
			make([]string, 0, len(chat.KnownUserIDs)),
			chat.KnownUserIDs...,
		),
		KnownUsers: buildAdminKnownUsers(
			chat.KnownUserIDs,
			knownUserLabels,
		),
		HistoryTotalCount: historyTotalCount,
		HistoryTruncated:  historyTruncated,
		History:           history,
	}
}

func findTrackedChatState(
	tracked []wecomchannel.TrackedChatState,
	baseSessionID string,
) (wecomchannel.TrackedChatState, bool) {
	baseSessionID = strings.TrimSpace(baseSessionID)
	for _, chat := range tracked {
		if strings.TrimSpace(chat.BaseSessionID) != baseSessionID {
			continue
		}
		return chat, true
	}
	return wecomchannel.TrackedChatState{}, false
}

type adminChatHistorySession struct {
	SessionID    string
	SessionLabel string
	LastActivity time.Time
	Current      bool
	Recall       bool
	Turns        []admin.ChatTurnView
}

func buildAdminChatHistorySessions(
	appName string,
	sessionSvc session.Service,
	chat wecomchannel.TrackedChatState,
	labelOverrides map[string]string,
) ([]adminChatHistorySession, bool) {
	if strings.TrimSpace(appName) == "" || sessionSvc == nil {
		return nil, false
	}
	lines := transcriptSessionLines(chat)
	if len(lines) == 0 {
		return nil, false
	}
	bounded := false
	if len(lines) > adminChatHistorySessionLimit {
		lines = lines[:adminChatHistorySessionLimit]
		bounded = true
	}
	history := make(
		[]adminChatHistorySession,
		0,
		len(lines),
	)
	for i := len(lines) - 1; i >= 0; i-- {
		view, ok := buildAdminChatHistorySessionView(
			appName,
			sessionSvc,
			strings.TrimSpace(chat.BaseSessionID),
			chat,
			lines[i],
			labelOverrides,
		)
		if !ok {
			continue
		}
		history = append(history, view)
	}
	if len(history) == 0 {
		return nil, bounded
	}
	history = mergeAdminChatHistorySessions(history)
	return history, bounded
}

func mergeAdminChatHistorySessions(
	history []adminChatHistorySession,
) []adminChatHistorySession {
	if len(history) < 2 {
		return history
	}
	merged := make([]adminChatHistorySession, 0, len(history))
	for _, sessionView := range history {
		if len(merged) == 0 {
			merged = append(merged, sessionView)
			continue
		}
		last := &merged[len(merged)-1]
		if strings.TrimSpace(last.SessionID) !=
			strings.TrimSpace(sessionView.SessionID) {
			merged = append(merged, sessionView)
			continue
		}
		if sessionView.LastActivity.After(last.LastActivity) {
			last.LastActivity = sessionView.LastActivity
		}
		last.Current = last.Current || sessionView.Current
		last.Recall = last.Recall || sessionView.Recall
		last.SessionLabel = adminChatHistorySessionLabel(
			last.Current,
			last.Recall,
		)
		if len(last.Turns) == 0 && len(sessionView.Turns) != 0 {
			last.Turns = sessionView.Turns
		}
	}
	return merged
}

func transcriptSessionLines(
	chat wecomchannel.TrackedChatState,
) []wecomchannel.TrackedChatSessionLine {
	if len(chat.History) != 0 {
		return append(
			make([]wecomchannel.TrackedChatSessionLine, 0, len(chat.History)),
			chat.History...,
		)
	}
	sessionID := strings.TrimSpace(chat.CurrentSessionID)
	if sessionID == "" {
		return nil
	}
	return []wecomchannel.TrackedChatSessionLine{{
		SessionID:    sessionID,
		LastActivity: chat.LastActivity,
	}}
}

func buildAdminChatHistorySessionView(
	appName string,
	sessionSvc session.Service,
	baseSessionID string,
	chat wecomchannel.TrackedChatState,
	line wecomchannel.TrackedChatSessionLine,
	labelOverrides map[string]string,
) (adminChatHistorySession, bool) {
	sessionID := strings.TrimSpace(line.SessionID)
	if sessionID == "" {
		return adminChatHistorySession{}, false
	}
	sess, ok := lookupAdminChatTranscriptSession(
		appName,
		sessionSvc,
		baseSessionID,
		sessionID,
	)
	if !ok || sess == nil {
		return adminChatHistorySession{}, false
	}

	turns := conversation.BuildTurns(
		sess,
		conversation.TurnOptions{
			LabelOverrides: labelOverrides,
		},
	)
	mapped := make([]admin.ChatTurnView, 0, len(turns))
	for _, turn := range turns {
		text := trimAdminChatTranscriptText(turn.Text)
		quoteText := trimAdminChatTranscriptText(turn.QuoteText)
		if strings.TrimSpace(text) == "" &&
			strings.TrimSpace(quoteText) == "" {
			continue
		}
		mapped = append(mapped, admin.ChatTurnView{
			Role:      strings.TrimSpace(turn.Role),
			Speaker:   strings.TrimSpace(turn.Speaker),
			QuoteText: quoteText,
			Text:      text,
			Timestamp: turn.Timestamp,
		})
	}
	if len(mapped) == 0 {
		return adminChatHistorySession{}, false
	}
	current := sessionID == strings.TrimSpace(chat.CurrentSessionID)
	recall := sessionID == strings.TrimSpace(chat.RecallSessionID)
	return adminChatHistorySession{
		SessionID:    sessionID,
		SessionLabel: adminChatHistorySessionLabel(current, recall),
		LastActivity: line.LastActivity,
		Current:      current,
		Recall:       recall,
		Turns:        mapped,
	}, true
}

func adminChatHistorySessionLabel(
	current bool,
	recall bool,
) string {
	switch {
	case current:
		return adminChatHistoryLabelCurrent
	case recall:
		return adminChatHistoryLabelRecall
	default:
		return adminChatHistoryLabelRecent
	}
}

func buildAdminChatHistoryPage(
	baseSessionID string,
	history []adminChatHistorySession,
	bounded bool,
	cursor string,
) (admin.ChatHistoryPage, error) {
	page := admin.ChatHistoryPage{
		BaseSessionID:    strings.TrimSpace(baseSessionID),
		SessionLineCount: len(history),
		Bounded:          bounded,
	}
	if len(history) == 0 {
		return page, nil
	}
	totalTurns := 0
	for _, sessionView := range history {
		totalTurns += len(sessionView.Turns)
	}
	page.TurnCount = totalTurns
	if totalTurns == 0 {
		return page, nil
	}
	offset, err := parseAdminChatHistoryCursor(
		cursor,
		totalTurns,
	)
	if err != nil {
		return admin.ChatHistoryPage{}, err
	}
	start, end := adminChatHistoryWindow(
		totalTurns,
		offset,
	)
	page.ReturnedTurnCount = end - start
	if start > 0 {
		page.NextCursor = strconv.Itoa(offset + page.ReturnedTurnCount)
	}
	page.Items = buildAdminChatHistoryItems(
		history,
		start,
		end,
	)
	return page, nil
}

func parseAdminChatHistoryCursor(
	cursor string,
	totalTurns int,
) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid chat history cursor")
	}
	if offset < 0 || offset > totalTurns {
		return 0, fmt.Errorf("invalid chat history cursor")
	}
	return offset, nil
}

func adminChatHistoryWindow(
	totalTurns int,
	offset int,
) (int, int) {
	end := totalTurns - offset
	if end < 0 {
		end = 0
	}
	start := end - adminChatHistoryPageTurnCount
	if start < 0 {
		start = 0
	}
	return start, end
}

func buildAdminChatHistoryItems(
	history []adminChatHistorySession,
	start int,
	end int,
) []admin.ChatHistoryItem {
	if len(history) == 0 || end <= start {
		return nil
	}
	items := make([]admin.ChatHistoryItem, 0, end-start+len(history))
	globalTurnIndex := 0
	for _, sessionView := range history {
		if len(sessionView.Turns) == 0 {
			continue
		}
		addedSession := false
		for _, turn := range sessionView.Turns {
			if globalTurnIndex >= end {
				return items
			}
			if globalTurnIndex >= start {
				if !addedSession {
					items = append(items, admin.ChatHistoryItem{
						Kind:         adminChatHistorySessionKind(),
						SessionID:    sessionView.SessionID,
						SessionLabel: sessionView.SessionLabel,
						LastActivity: sessionView.LastActivity,
						Current:      sessionView.Current,
						Recall:       sessionView.Recall,
					})
					addedSession = true
				}
				items = append(items, admin.ChatHistoryItem{
					Kind:      adminChatHistoryTurnKind(),
					SessionID: sessionView.SessionID,
					Role:      strings.TrimSpace(turn.Role),
					Speaker:   strings.TrimSpace(turn.Speaker),
					QuoteText: strings.TrimSpace(turn.QuoteText),
					Text:      strings.TrimSpace(turn.Text),
					Timestamp: turn.Timestamp,
				})
			}
			globalTurnIndex++
		}
	}
	return items
}

func adminChatHistorySessionKind() string {
	return adminChatHistoryItemSession
}

func adminChatHistoryTurnKind() string {
	return adminChatHistoryItemTurn
}

func lookupAdminChatTranscriptSession(
	appName string,
	sessionSvc session.Service,
	baseSessionID string,
	sessionID string,
) (*session.Session, bool) {
	for _, candidate := range wecomchannel.TranscriptLookupSessionIDs(
		sessionID,
	) {
		sess, err := sessionSvc.GetSession(
			context.Background(),
			session.Key{
				AppName:   strings.TrimSpace(appName),
				UserID:    strings.TrimSpace(baseSessionID),
				SessionID: candidate,
			},
		)
		if err != nil {
			return nil, false
		}
		if sess != nil {
			return sess, true
		}
	}
	return nil, false
}

func trimAdminChatTranscriptText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) <=
		adminChatTranscriptTextLimit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(
		string(runes[:adminChatTranscriptTextLimit]),
	) + "..."
}

func resolveTrackedChatKnownUserLabels(
	state runtimePromptAdminState,
	tracked []wecomchannel.TrackedChatState,
	stateDir string,
) map[string]string {
	userIDs := collectTrackedChatKnownUserIDs(tracked)
	if len(userIDs) == 0 {
		return nil
	}
	return wecomchannel.ResolveKnownUserLabels(
		stateDir,
		state.WeComUserLookupCommand,
		state.WeComUserLabelMode,
		userIDs,
	)
}

func collectTrackedChatKnownUserIDs(
	tracked []wecomchannel.TrackedChatState,
) []string {
	if len(tracked) == 0 {
		return nil
	}
	collected := make([]string, 0, len(tracked))
	seen := make(map[string]struct{})
	for _, chat := range tracked {
		for _, userID := range chat.KnownUserIDs {
			userID = strings.TrimSpace(userID)
			if userID == "" {
				continue
			}
			if _, ok := seen[userID]; ok {
				continue
			}
			seen[userID] = struct{}{}
			collected = append(collected, userID)
		}
	}
	if len(collected) == 0 {
		return nil
	}
	return collected
}

func buildAdminKnownUsers(
	userIDs []string,
	labels map[string]string,
) []admin.KnownUserView {
	if len(userIDs) == 0 {
		return nil
	}
	knownUsers := make([]admin.KnownUserView, 0, len(userIDs))
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		knownUsers = append(knownUsers, admin.KnownUserView{
			UserID: userID,
			Label: strings.TrimSpace(
				labels[userID],
			),
		})
	}
	if len(knownUsers) == 0 {
		return nil
	}
	return knownUsers
}

func personaLabelIndex(
	state runtimePromptAdminState,
) map[string]string {
	labels := make(map[string]string)
	for _, def := range state.DefaultPersonaOptions {
		recordPersonaLabel(labels, def.ID, def.Name)
	}
	for _, store := range state.PersonaStores {
		for _, def := range store.Definitions {
			recordPersonaLabel(labels, def.ID, def.Name)
		}
	}
	return labels
}

func recordPersonaLabel(
	labels map[string]string,
	personaID string,
	name string,
) {
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = personaID
	}
	labels[personaID] = name
}

func personaLabelForID(
	personaID string,
	labels map[string]string,
) string {
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		personaID = personaapi.DefaultID
	}
	if label := strings.TrimSpace(labels[personaID]); label != "" {
		return label
	}
	if def, ok := personaapi.LookupBuiltin(personaID); ok {
		return firstNonEmptyString(
			strings.TrimSpace(def.Name),
			strings.TrimSpace(def.ID),
		)
	}
	return personaID
}
