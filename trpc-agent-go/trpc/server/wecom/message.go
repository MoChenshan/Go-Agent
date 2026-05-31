package wecom

import (
	"fmt"
	"strings"
	"time"
)

const (
	commandHelp   = "/help"
	commandNew    = "/new"
	commandClear  = "/clear"
	commandCancel = "/cancel"

	messageTypeText   = "text"
	messageTypeEvent  = "event"
	messageTypeMixed  = "mixed"
	messageTypeStream = "stream"

	eventTypeEnterChat = "enter_chat"

	requestIDPrefix = "wecom"
	streamIDPrefix  = "wecom-stream-"

	defaultUnknownUserID = "unknown-user"

	maxReplyRunes = 1800
)

// Message types sent by WeCom websocket callbacks.
type WebhookMessage struct {
	MsgID         string              `json:"msgid,omitempty"`
	ChatID        string              `json:"chatid,omitempty"`
	MsgType       string              `json:"msgtype,omitempty"`
	From          FromInfo            `json:"from,omitempty"`
	Text          TextContent         `json:"text,omitempty"`
	Event         EventContent        `json:"event,omitempty"`
	MixedMessage  MixedMessageContent `json:"mixed,omitempty"`
	CallbackReqID string              `json:"-"`
	ReplyWriter   wsReplyWriter       `json:"-"`
}

// FromInfo describes the user who sent a callback message.
type FromInfo struct {
	UserID string `json:"userid,omitempty"`
	Name   string `json:"name,omitempty"`
	Alias  string `json:"alias,omitempty"`
}

// TextContent contains the text body of a callback.
type TextContent struct {
	Content string `json:"content,omitempty"`
}

// EventContent contains an event callback payload.
type EventContent struct {
	EventType string `json:"eventtype,omitempty"`
}

// MixedMessageContent contains text and attachment fragments.
type MixedMessageContent struct {
	MsgItem []MixedMessageItem `json:"msg_item,omitempty"`
}

// MixedMessageItem is one item inside a mixed callback payload.
type MixedMessageItem struct {
	MsgType string      `json:"msgtype,omitempty"`
	Text    TextContent `json:"text,omitempty"`
}

type parsedCommand struct {
	name string
}

func parseCommand(text string) parsedCommand {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case commandHelp:
		return parsedCommand{name: commandHelp}
	case commandNew, commandClear:
		return parsedCommand{name: commandNew}
	case commandCancel:
		return parsedCommand{name: commandCancel}
	default:
		return parsedCommand{}
	}
}

func messageUserID(msg WebhookMessage) string {
	userID := strings.TrimSpace(msg.From.UserID)
	if userID != "" {
		return userID
	}
	userID = strings.TrimSpace(msg.From.Alias)
	if userID != "" {
		return userID
	}
	return defaultUnknownUserID
}

func baseSessionID(chatID, userID string) string {
	cleanedChatID := strings.TrimSpace(chatID)
	if cleanedChatID != "" {
		return fmt.Sprintf("wecom:chat:%s", cleanedChatID)
	}
	return fmt.Sprintf("wecom:dm:%s", strings.TrimSpace(userID))
}

func buildRequestID(msg WebhookMessage) string {
	parts := []string{requestIDPrefix}
	cleanedChatID := strings.TrimSpace(msg.ChatID)
	if cleanedChatID != "" {
		parts = append(parts, cleanedChatID)
	} else {
		parts = append(parts, messageUserID(msg))
	}

	msgID := strings.TrimSpace(msg.MsgID)
	if msgID == "" {
		msgID = strings.TrimSpace(msg.CallbackReqID)
	}
	if msgID == "" {
		msgID = time.Now().UTC().Format(time.RFC3339Nano)
	}
	parts = append(parts, msgID)
	return strings.Join(parts, ":")
}

func buildStreamID(requestID string) string {
	cleaned := strings.TrimSpace(requestID)
	if cleaned == "" {
		cleaned = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return streamIDPrefix + cleaned
}

func extractMessageText(msg WebhookMessage, botName string) (string, bool) {
	switch strings.TrimSpace(msg.MsgType) {
	case messageTypeText:
		return normalizeIncomingText(msg.Text.Content, botName), true
	case messageTypeMixed:
		return extractMixedText(msg, botName)
	default:
		return "", false
	}
}

func extractMixedText(
	msg WebhookMessage,
	botName string,
) (string, bool) {
	parts := make([]string, 0, len(msg.MixedMessage.MsgItem))
	for _, item := range msg.MixedMessage.MsgItem {
		if strings.TrimSpace(item.MsgType) != messageTypeText {
			continue
		}
		text := normalizeIncomingText(item.Text.Content, botName)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

func normalizeIncomingText(text, botName string) string {
	text = strings.TrimSpace(text)
	if botName == "" {
		return text
	}
	mention := "@" + botName
	text = strings.ReplaceAll(text, mention, "")
	return strings.TrimSpace(text)
}

func splitRunes(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}

	chunks := make([]string, 0, (len(runes)/maxRunes)+1)
	for len(runes) > 0 {
		if len(runes) <= maxRunes {
			chunks = append(chunks, string(runes))
			break
		}

		split := maxRunes
		for i := maxRunes - 1; i >= maxRunes/2; i-- {
			if runes[i] == '\n' {
				split = i + 1
				break
			}
		}

		chunks = append(chunks, string(runes[:split]))
		runes = runes[split:]
	}
	return chunks
}
