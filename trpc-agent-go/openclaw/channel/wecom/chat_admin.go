package wecom

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	trackedChatKindDM        = "dm"
	trackedChatKindGroup     = "group"
	trackedChatKindGroupUser = "group_user"

	trackedChatKindDMLabel        = "Direct message"
	trackedChatKindGroupLabel     = "Group chat"
	trackedChatKindGroupUserLabel = "Group chat · isolated user"

	trackedChatDisplayDMPrefix        = "DM · "
	trackedChatDisplayGroupPrefix     = "Group · "
	trackedChatDisplayGroupUserPrefix = "Group user · "
	trackedChatDisplaySeparator       = " / "

	groupSessionPrefix        = pluginType + ":chat:"
	groupSessionUserSeparator = ":user:"
)

type TrackedChatState struct {
	BaseSessionID    string
	DisplayLabel     string
	Kind             string
	KindLabel        string
	CurrentSessionID string
	RecallSessionID  string
	LastActivity     time.Time
	Epoch            int64
	AssistantAlias   string
	PersonaID        string
	PersonaPinned    bool
	WorkspacePath    string
	KnownUserIDs     []string
	History          []TrackedChatSessionLine
}

type TrackedChatSessionLine struct {
	SessionID    string
	LastActivity time.Time
}

type KnownUserIdentity struct {
	UserID       string `json:"user_id,omitempty"`
	AccountName  string `json:"account_name,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	EmailAddress string `json:"email_address,omitempty"`
}

func FormatTrackedChatDisplayLabel(
	chat TrackedChatState,
	knownUserLabels map[string]string,
) string {
	displayLabel := strings.TrimSpace(chat.DisplayLabel)
	if len(knownUserLabels) == 0 {
		return displayLabel
	}
	switch strings.TrimSpace(chat.Kind) {
	case trackedChatKindDM:
		userID := strings.TrimPrefix(
			strings.TrimSpace(chat.BaseSessionID),
			directMessageSessionPrefix,
		)
		userLabel := trackedChatKnownUserDisplay(
			userID,
			knownUserLabels[userID],
		)
		if userLabel == "" {
			return displayLabel
		}
		return trackedChatDisplayDMPrefix + userLabel
	case trackedChatKindGroupUser:
		groupID, userID := trackedChatGroupUserParts(chat.BaseSessionID)
		if groupID == "" || userID == "" {
			return displayLabel
		}
		userLabel := trackedChatKnownUserDisplay(
			userID,
			knownUserLabels[userID],
		)
		if userLabel == "" {
			return displayLabel
		}
		return trackedChatDisplayGroupUserPrefix + groupID +
			trackedChatDisplaySeparator + userLabel
	default:
		return displayLabel
	}
}

func trackedChatGroupUserParts(baseSessionID string) (string, string) {
	baseSessionID = strings.TrimSpace(baseSessionID)
	if !strings.HasPrefix(baseSessionID, groupSessionPrefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(baseSessionID, groupSessionPrefix)
	parts := strings.SplitN(rest, groupSessionUserSeparator, 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func trackedChatKnownUserDisplay(userID string, label string) string {
	userID = strings.TrimSpace(userID)
	label = strings.TrimSpace(label)
	switch {
	case label != "" && userID != "" && label != userID:
		return label + " (" + userID + ")"
	case label != "":
		return label
	default:
		return userID
	}
}

func ListTrackedChats(stateDir string) ([]TrackedChatState, error) {
	path := sessionTrackerStorePath(stateDir)
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	return sharedSessionTrackerWithPath(path).listTrackedChats(), nil
}

func ResolveKnownUserLabels(
	stateDir string,
	configuredCommand string,
	rawMode string,
	userIDs []string,
) map[string]string {
	mode, err := parseUserLabelMode(rawMode)
	if err != nil {
		return nil
	}
	if mode == userLabelModeID {
		return nil
	}
	resolver := newUserIdentityResolver(
		strings.TrimSpace(stateDir),
		configuredCommand,
	)
	if resolver == nil {
		return nil
	}
	profiles := resolver.ResolveUsers(context.Background(), userIDs)
	if len(profiles) == 0 {
		return nil
	}
	return resolvedIdentityLabels(mode, profiles)
}

func ResolveKnownUserIdentities(
	stateDir string,
	configuredCommand string,
	userIDs []string,
) map[string]KnownUserIdentity {
	resolver := newUserIdentityResolver(
		strings.TrimSpace(stateDir),
		configuredCommand,
	)
	if resolver == nil {
		return nil
	}
	profiles := resolver.ResolveUsers(context.Background(), userIDs)
	if len(profiles) == 0 {
		return nil
	}
	out := make(map[string]KnownUserIdentity, len(profiles))
	for userID, profile := range profiles {
		out[userID] = KnownUserIdentity{
			UserID:       strings.TrimSpace(profile.UserID),
			AccountName:  strings.TrimSpace(profile.AccountName),
			DisplayName:  strings.TrimSpace(profile.DisplayName),
			EmailAddress: strings.TrimSpace(profile.EmailAddress),
		}
	}
	return out
}

// TranscriptLookupSessionIDs returns compatible lookup candidates for
// a tracked WeCom chat session. Canonical session IDs are preferred,
// and the legacy thread-prefixed storage key is kept as a fallback for
// older persisted session rows.
func TranscriptLookupSessionIDs(sessionID string) []string {
	sessionID = canonicalWeComSessionID(sessionID)
	if sessionID == "" {
		return nil
	}
	candidates := []string{sessionID}
	if !strings.HasPrefix(sessionID, pluginType+":") {
		return candidates
	}
	candidates = append(
		candidates,
		wecomThreadSessionPrefix+sessionID,
	)
	return candidates
}

func (t *sessionTracker) listTrackedChats() []TrackedChatState {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	baseIDs := make([]string, 0, len(t.sessions))
	seen := make(map[string]struct{}, len(t.sessions))
	for baseID := range t.sessions {
		canonical := canonicalWeComSessionID(baseID)
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		baseIDs = append(baseIDs, canonical)
	}
	sort.Strings(baseIDs)

	out := make([]TrackedChatState, 0, len(baseIDs))
	migrated := false
	for _, baseID := range baseIDs {
		_, info, changed := t.resolveSessionStateLocked(baseID)
		if changed {
			migrated = true
		}
		if info == nil {
			continue
		}
		out = append(out, trackedChatStateFromInfo(info))
	}
	if migrated {
		t.persistLockedWarn()
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LastActivity.Equal(out[j].LastActivity) {
			return out[i].BaseSessionID < out[j].BaseSessionID
		}
		return out[i].LastActivity.After(out[j].LastActivity)
	})
	return out
}

func trackedChatStateFromInfo(info *sessionInfo) TrackedChatState {
	if info == nil {
		return TrackedChatState{}
	}

	kind, kindLabel, displayLabel := trackedChatDescriptor(
		info.baseSessionID,
	)
	history := make([]TrackedChatSessionLine, 0, len(info.history))
	for _, entry := range info.history {
		history = append(history, TrackedChatSessionLine{
			SessionID:    strings.TrimSpace(entry.SessionID),
			LastActivity: entry.LastActivity,
		})
	}

	return TrackedChatState{
		BaseSessionID:    strings.TrimSpace(info.baseSessionID),
		DisplayLabel:     displayLabel,
		Kind:             kind,
		KindLabel:        kindLabel,
		CurrentSessionID: strings.TrimSpace(info.sessionID),
		RecallSessionID: strings.TrimSpace(
			info.recallSessionID,
		),
		LastActivity:   info.lastActivity,
		Epoch:          info.epoch,
		AssistantAlias: normalizeAssistantAlias(info.assistantAlias),
		PersonaID:      strings.TrimSpace(info.personaID),
		PersonaPinned:  info.personaPinned,
		WorkspacePath: strings.TrimSpace(
			info.workspacePath,
		),
		KnownUserIDs: append(
			make([]string, 0, len(info.knownUserIDs)),
			info.knownUserIDs...,
		),
		History: history,
	}
}

func trackedChatDescriptor(baseSessionID string) (
	string,
	string,
	string,
) {
	baseSessionID = strings.TrimSpace(baseSessionID)
	if baseSessionID == "" {
		return "", "", ""
	}

	if strings.HasPrefix(baseSessionID, directMessageSessionPrefix) {
		userID := strings.TrimPrefix(
			baseSessionID,
			directMessageSessionPrefix,
		)
		return trackedChatKindDM, trackedChatKindDMLabel,
			trackedChatDisplayDMPrefix + userID
	}

	if !strings.HasPrefix(baseSessionID, groupSessionPrefix) {
		return "", "", baseSessionID
	}

	rest := strings.TrimPrefix(baseSessionID, groupSessionPrefix)
	parts := strings.SplitN(rest, groupSessionUserSeparator, 2)
	if len(parts) == 1 {
		return trackedChatKindGroup, trackedChatKindGroupLabel,
			trackedChatDisplayGroupPrefix + parts[0]
	}

	return trackedChatKindGroupUser,
		trackedChatKindGroupUserLabel,
		trackedChatDisplayGroupUserPrefix + parts[0] +
			trackedChatDisplaySeparator + parts[1]
}
