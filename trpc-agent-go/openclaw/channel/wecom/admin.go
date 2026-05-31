package wecom

import (
	"strings"
	"time"
)

const (
	adminActivationReasonAIModeRequired    = "ai_mode_required"
	adminActivationReasonWebSocketRequired = "" +
		"websocket_mode_required"
	adminActivationReasonNotConnected = "not_connected"
)

type AdminTarget struct {
	Name               string
	StateDir           string
	BotMode            string
	ConnectionMode     string
	AIBotID            string
	WebSocketURL       string
	CallbackPath       string
	EnableStream       bool
	StreamSnapshotMode string
	ChatPolicy         string
	RuntimeAdminPolicy string
	UserLabelMode      string
}

type AdminActivationStatus struct {
	Supported bool   `json:"supported"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type AdminChatSummary struct {
	TrackedChats       int        `json:"tracked_chats"`
	DirectChats        int        `json:"direct_chats"`
	GroupChats         int        `json:"group_chats"`
	GroupUserChats     int        `json:"group_user_chats"`
	LastActivity       *time.Time `json:"last_activity,omitempty"`
	WorkspaceChats     int        `json:"workspace_chats"`
	PersonaPinnedChats int        `json:"persona_pinned_chats"`
}

func (c *Channel) WeComAdminTarget() AdminTarget {
	if c == nil {
		return AdminTarget{}
	}
	return AdminTarget{
		Name:           strings.TrimSpace(c.name),
		StateDir:       strings.TrimSpace(c.stateDir),
		BotMode:        strings.TrimSpace(c.botMode),
		ConnectionMode: strings.TrimSpace(c.connectionMode),
		AIBotID:        strings.TrimSpace(c.cfg.AIBotID),
		WebSocketURL:   strings.TrimSpace(c.wsURL),
		CallbackPath:   strings.TrimSpace(c.cfg.CallbackPath),
		EnableStream:   c.cfg.EnableStream,
		StreamSnapshotMode: normalizeStreamSnapshotMode(
			c.cfg.StreamSnapshotMode,
		),
		ChatPolicy:         strings.TrimSpace(c.chatPolicy),
		RuntimeAdminPolicy: strings.TrimSpace(c.runtimeAdminPolicy),
		UserLabelMode:      strings.TrimSpace(c.userLabelMode),
	}
}

func BuildAdminChatSummary(
	stateDir string,
) (AdminChatSummary, error) {
	chats, err := ListTrackedChats(stateDir)
	if err != nil {
		return AdminChatSummary{}, err
	}

	var summary AdminChatSummary
	for _, chat := range chats {
		summary.TrackedChats++
		switch strings.TrimSpace(chat.Kind) {
		case trackedChatKindDM:
			summary.DirectChats++
		case trackedChatKindGroup:
			summary.GroupChats++
		case trackedChatKindGroupUser:
			summary.GroupUserChats++
		}
		if !chat.LastActivity.IsZero() {
			if summary.LastActivity == nil ||
				summary.LastActivity.Before(chat.LastActivity) {
				summary.LastActivity = cloneAdminTime(chat.LastActivity)
			}
		}
		if strings.TrimSpace(chat.WorkspacePath) != "" {
			summary.WorkspaceChats++
		}
		if chat.PersonaPinned {
			summary.PersonaPinnedChats++
		}
	}
	return summary, nil
}

func cloneAdminTime(value time.Time) *time.Time {
	cloned := value
	return &cloned
}

func (c *Channel) WeComAdminActivationStatus() AdminActivationStatus {
	if c == nil {
		return AdminActivationStatus{}
	}
	if c.botMode != botModeAI {
		return AdminActivationStatus{
			Reason: adminActivationReasonAIModeRequired,
		}
	}
	if c.connectionMode != connectionModeWebSocket {
		return AdminActivationStatus{
			Reason: adminActivationReasonWebSocketRequired,
		}
	}
	if c.webSocketPushWriter() == nil {
		return AdminActivationStatus{
			Supported: true,
			Reason:    adminActivationReasonNotConnected,
		}
	}
	return AdminActivationStatus{
		Supported: true,
		Available: true,
	}
}

func (c *Channel) WeComAdminAllowsUser(userID string) bool {
	if c == nil {
		return false
	}
	return c.isUserAllowed(strings.TrimSpace(userID))
}

func BuildAdminDirectMessageTarget(userID string) string {
	return buildPushTarget(
		pushTargetKindSingle,
		strings.TrimSpace(userID),
	)
}
