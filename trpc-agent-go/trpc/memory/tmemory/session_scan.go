package tmemory

import (
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

type scannedMessage struct {
	Message   model.Message
	Timestamp time.Time
}

func scanDeltaSince(sess *session.Session, since time.Time) (time.Time, []scannedMessage) {
	if sess == nil {
		return time.Time{}, nil
	}
	var latestTs time.Time
	var messages []scannedMessage

	sess.EventMu.RLock()
	defer sess.EventMu.RUnlock()

	for _, e := range sess.Events {
		if !since.IsZero() && !e.Timestamp.After(since) {
			continue
		}
		if e.Timestamp.After(latestTs) {
			latestTs = e.Timestamp
		}
		if e.Response == nil {
			continue
		}
		for _, choice := range e.Response.Choices {
			msg := choice.Message
			if msg.Role == model.RoleTool || msg.ToolID != "" || len(msg.ToolCalls) > 0 {
				continue
			}
			if msg.Role != model.RoleUser && msg.Role != model.RoleAssistant {
				continue
			}
			if msg.Content == "" && len(msg.ContentParts) == 0 {
				continue
			}
			messages = append(messages, scannedMessage{
				Message:   msg,
				Timestamp: e.Timestamp,
			})
		}
	}
	return latestTs, messages
}

func messageText(msg model.Message) string {
	if strings.TrimSpace(msg.Content) != "" {
		return strings.TrimSpace(msg.Content)
	}
	if len(msg.ContentParts) == 0 {
		return ""
	}
	var parts []string
	for _, part := range msg.ContentParts {
		if part.Type != model.ContentTypeText || part.Text == nil {
			continue
		}
		if strings.TrimSpace(*part.Text) == "" {
			continue
		}
		parts = append(parts, strings.TrimSpace(*part.Text))
	}
	return strings.Join(parts, "\n")
}

func roleToName(role model.Role) string {
	switch role {
	case model.RoleUser:
		return "用户"
	case model.RoleAssistant:
		return "助手"
	default:
		return string(role)
	}
}
