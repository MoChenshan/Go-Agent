package wecom

import (
	"fmt"
	"net/url"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
)

const (
	groupSessionModeShared   = "shared"
	groupSessionModeIsolated = "isolated"

	defaultGroupSessionMode = groupSessionModeShared

	userLabelModeNameOrAlias = "name_or_alias"
	userLabelModeAliasOrName = "alias_or_name"
	userLabelModeName        = "name"
	userLabelModeAlias       = "alias"
	userLabelModeID          = "id"

	defaultUserLabelMode = userLabelModeAliasOrName

	pushTargetKindSingle  = "single"
	pushTargetKindGroup   = "group"
	pushTargetSeparator   = ":"
	pushTargetQueryMark   = "?"
	pushTargetMentionsKey = "mentions"
	pushTargetAssign      = "="
	pushTargetMentionsSep = ","

	chatTypeSingle = 1
	chatTypeGroup  = 2
)

type pushTarget struct {
	ChatID           string
	ChatType         int
	MentionedUserIDs []string
}

func buildRequestID(chatID, msgID string) string {
	return fmt.Sprintf("%s%s:%s", requestIDPrefix, chatID, msgID)
}

func buildSessionID(chatID, fromID string) string {
	if strings.TrimSpace(chatID) != "" {
		return fmt.Sprintf("%s:chat:%s", pluginType, chatID)
	}
	return fmt.Sprintf("%s:dm:%s", pluginType, fromID)
}

func buildScopedSessionID(
	chatID string,
	fromID string,
	groupMode string,
) string {
	if strings.TrimSpace(chatID) == "" {
		return buildSessionID(chatID, fromID)
	}
	if strings.TrimSpace(groupMode) == groupSessionModeIsolated {
		return fmt.Sprintf(
			"%s:chat:%s:user:%s",
			pluginType,
			chatID,
			fromID,
		)
	}
	return buildSessionID(chatID, fromID)
}

func buildCanonicalUserID(fromID string) string {
	return buildSessionID("", fromID)
}

func buildGatewayUserID(
	fromID string,
	fallbackAccountLabel string,
	identityLabels map[string]string,
) string {
	fromID = strings.TrimSpace(fromID)
	canonicalUserID := buildCanonicalUserID(fromID)
	label := ""
	if len(identityLabels) > 0 {
		label = strings.TrimSpace(identityLabels[fromID])
	}
	if label == "" {
		label = strings.TrimSpace(fallbackAccountLabel)
	}
	if !isReadableAccountLabel(label) || label == fromID {
		return canonicalUserID
	}
	return buildCanonicalUserID(label)
}

func isReadableAccountLabel(label string) bool {
	if label == "" {
		return false
	}
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func messageTransportUserID(msg WebhookMessage) string {
	if value := strings.TrimSpace(msg.From.UserID); value != "" {
		return value
	}
	return strings.TrimSpace(msg.From.Alias)
}

func parseChatPolicy(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return defaultChatPolicy, nil
	}
	switch v {
	case chatPolicyDisabled, chatPolicyOpen, chatPolicyAllowlist:
		return v, nil
	default:
		return "", fmt.Errorf("wecom channel: unsupported chat_policy: %s", raw)
	}
}

func parseGroupSessionMode(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return defaultGroupSessionMode, nil
	}
	switch value {
	case groupSessionModeShared, groupSessionModeIsolated:
		return value, nil
	default:
		return "", fmt.Errorf(
			"wecom channel: unsupported group_session_mode: %s",
			raw,
		)
	}
}

func parseUserLabelMode(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return defaultUserLabelMode, nil
	}
	switch value {
	case userLabelModeNameOrAlias,
		userLabelModeAliasOrName,
		userLabelModeName,
		userLabelModeAlias,
		userLabelModeID:
		return value, nil
	default:
		return "", fmt.Errorf(
			"wecom channel: unsupported user_label_mode: %s",
			raw,
		)
	}
}

func messageUserLabel(msg WebhookMessage, mode string) string {
	name := strings.TrimSpace(msg.From.Name)
	alias := strings.TrimSpace(msg.From.Alias)
	userID := messageUserID(msg)

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case userLabelModeAlias:
		return firstNonEmptyLabel(alias, userID)
	case userLabelModeName:
		return firstNonEmptyLabel(name, userID)
	case userLabelModeAliasOrName:
		return firstNonEmptyLabel(alias, name, userID)
	case userLabelModeID:
		return userID
	default:
		return firstNonEmptyLabel(name, alias, userID)
	}
}

func quoteTextPreview(quote *QuoteContent) string {
	if quote == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	for _, item := range quoteItems(quote) {
		switch item.MsgType {
		case MsgTypeText:
			text := strings.TrimSpace(item.Text.Content)
			if text != "" {
				parts = append(parts, text)
			}
		case MsgTypeImage:
			parts = append(parts, "[image]")
		case MsgTypeFile:
			parts = append(parts, "[file]")
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func buildAllowSet(users []string) map[string]struct{} {
	if len(users) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(users))
	for _, u := range users {
		u = strings.TrimSpace(u)
		if u != "" {
			m[u] = struct{}{}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func defaultString(raw, fallback string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	return value
}

func firstNonEmptyLabel(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func buildDefaultDeliveryTarget(
	chatID string,
	fromID string,
) (delivery.Target, bool) {
	return buildDeliveryTarget(chatID, fromID, nil)
}

func buildDeliveryTarget(
	chatID string,
	fromID string,
	mentionedUserIDs []string,
) (delivery.Target, bool) {
	if value := strings.TrimSpace(chatID); value != "" {
		return delivery.Target{
			Channel: pluginType,
			Target: buildPushTargetWithMentions(
				pushTargetKindGroup,
				value,
				mentionedUserIDs,
			),
		}, true
	}
	if value := strings.TrimSpace(fromID); value != "" {
		return delivery.Target{
			Channel: pluginType,
			Target:  buildPushTarget(pushTargetKindSingle, value),
		}, true
	}
	return delivery.Target{}, false
}

func buildPushTarget(kind, value string) string {
	kind = strings.TrimSpace(kind)
	value = strings.TrimSpace(value)
	if kind == "" || value == "" {
		return ""
	}
	return kind + pushTargetSeparator + value
}

func buildPushTargetWithMentions(
	kind string,
	value string,
	mentionedUserIDs []string,
) string {
	base := buildPushTarget(kind, value)
	if base == "" || kind != pushTargetKindGroup {
		return base
	}

	mentionedUserIDs = sanitizeKnownUserIDs(mentionedUserIDs)
	if len(mentionedUserIDs) == 0 {
		return base
	}
	return base + pushTargetQueryMark +
		pushTargetMentionsKey + pushTargetAssign +
		strings.Join(
			mentionedUserIDs,
			pushTargetMentionsSep,
		)
}

func parsePushTarget(raw string) (pushTarget, error) {
	value := strings.TrimSpace(raw)
	base, rawQuery, _ := strings.Cut(
		value,
		pushTargetQueryMark,
	)
	parts := strings.SplitN(base, pushTargetSeparator, 2)
	if len(parts) != 2 {
		return pushTarget{}, fmt.Errorf(
			"wecom channel: invalid push target: %s",
			raw,
		)
	}

	kind := strings.TrimSpace(parts[0])
	chatID := strings.TrimSpace(parts[1])
	if chatID == "" {
		return pushTarget{}, fmt.Errorf(
			"wecom channel: empty push target id",
		)
	}

	mentionedUserIDs, err := parsePushTargetMentionedUserIDs(
		rawQuery,
	)
	if err != nil {
		return pushTarget{}, err
	}

	switch kind {
	case pushTargetKindSingle:
		return pushTarget{
			ChatID:   chatID,
			ChatType: chatTypeSingle,
		}, nil
	case pushTargetKindGroup:
		return pushTarget{
			ChatID:           chatID,
			ChatType:         chatTypeGroup,
			MentionedUserIDs: mentionedUserIDs,
		}, nil
	default:
		return pushTarget{}, fmt.Errorf(
			"wecom channel: unsupported push target kind: %s",
			kind,
		)
	}
}

func parsePushTargetMentionedUserIDs(
	rawQuery string,
) ([]string, error) {
	rawQuery = strings.TrimSpace(rawQuery)
	if rawQuery == "" {
		return nil, nil
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf(
			"wecom channel: invalid push target query: %w",
			err,
		)
	}

	rawUserIDs, ok := values[pushTargetMentionsKey]
	if !ok {
		return nil, nil
	}

	userIDs := make([]string, 0, len(rawUserIDs))
	for _, rawUserID := range rawUserIDs {
		if strings.TrimSpace(rawUserID) == "" {
			continue
		}
		userIDs = append(
			userIDs,
			strings.Split(
				rawUserID,
				pushTargetMentionsSep,
			)...,
		)
	}
	return sanitizeKnownUserIDs(userIDs), nil
}

func splitReplyText(content string) []string {
	parts := splitRunes(content, maxReplyRunes)
	for i := 1; i < len(parts); i++ {
		parts[i] = continuedReplyPrefix + parts[i]
	}
	return parts
}

// splitRunes splits text into chunks of at most maxRunes runes,
// preferring to break at newlines.
func splitRunes(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}

	out := make([]string, 0, (len(runes)/maxRunes)+1)
	for len(runes) > 0 {
		if len(runes) <= maxRunes {
			out = append(out, string(runes))
			break
		}
		cut := splitIndex(runes[:maxRunes], maxRunes)
		out = append(out, string(runes[:cut]))
		runes = runes[cut:]
	}
	return out
}

func splitIndex(segment []rune, maxRunes int) int {
	if len(segment) <= 1 {
		return len(segment)
	}

	min := maxRunes / 2
	if min < 1 {
		min = 1
	}

	// 优先在双换行处断开。
	for i := len(segment) - 1; i > 0; i-- {
		if segment[i] == '\n' && segment[i-1] == '\n' && i+1 >= min {
			return i + 1
		}
	}
	// 其次在单换行处断开。
	for i := len(segment) - 1; i >= 0; i-- {
		if segment[i] == '\n' && i+1 >= min {
			return i + 1
		}
	}
	// 最后在空格处断开。
	for i := len(segment) - 1; i >= 0; i-- {
		if segment[i] == ' ' || segment[i] == '\t' {
			if i+1 >= min {
				return i + 1
			}
		}
	}
	return len(segment)
}

// describeMessageTypes returns a comma-separated list of message types for logging.
func describeMessageTypes(msgs []WebhookMessage) string {
	types := make([]string, len(msgs))
	for i, m := range msgs {
		types[i] = m.MsgType
	}
	return strings.Join(types, ", ")
}
