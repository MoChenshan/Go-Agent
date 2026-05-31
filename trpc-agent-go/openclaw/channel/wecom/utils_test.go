package wecom

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
)

func TestBuildRequestID(t *testing.T) {
	t.Parallel()

	id := buildRequestID("chat1", "msg1")
	require.Equal(t, "wecom:chat1:msg1", id)
}

func TestBuildSessionID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "wecom:chat:chat1", buildSessionID("chat1", "user1"))
	require.Equal(t, "wecom:dm:user1", buildSessionID("", "user1"))
}

func TestBuildDefaultDeliveryTarget(t *testing.T) {
	t.Parallel()

	target, ok := buildDefaultDeliveryTarget("chat1", "user1")
	require.True(t, ok)
	require.Equal(
		t,
		delivery.Target{
			Channel: pluginType,
			Target:  "group:chat1",
		},
		target,
	)

	target, ok = buildDefaultDeliveryTarget("", "user1")
	require.True(t, ok)
	require.Equal(
		t,
		delivery.Target{
			Channel: pluginType,
			Target:  "single:user1",
		},
		target,
	)

	_, ok = buildDefaultDeliveryTarget("", "")
	require.False(t, ok)
}

func TestBuildDeliveryTargetWithMentions(t *testing.T) {
	t.Parallel()

	target, ok := buildDeliveryTarget(
		"chat1",
		"user1",
		[]string{"user2", "user3", "user2"},
	)
	require.True(t, ok)
	require.Equal(
		t,
		delivery.Target{
			Channel: pluginType,
			Target:  "group:chat1?mentions=user2,user3",
		},
		target,
	)
}

func TestParsePushTarget(t *testing.T) {
	t.Parallel()

	target, err := parsePushTarget("group:chat1")
	require.NoError(t, err)
	require.Equal(t, "chat1", target.ChatID)
	require.Equal(t, chatTypeGroup, target.ChatType)

	target, err = parsePushTarget("single:user1")
	require.NoError(t, err)
	require.Equal(t, "user1", target.ChatID)
	require.Equal(t, chatTypeSingle, target.ChatType)
	require.Empty(t, target.MentionedUserIDs)

	target, err = parsePushTarget(
		"group:chat1?mentions=user2,user3",
	)
	require.NoError(t, err)
	require.Equal(t, "chat1", target.ChatID)
	require.Equal(t, chatTypeGroup, target.ChatType)
	require.Equal(
		t,
		[]string{"user2", "user3"},
		target.MentionedUserIDs,
	)

	_, err = parsePushTarget("chat1")
	require.Error(t, err)

	_, err = parsePushTarget("other:user1")
	require.Error(t, err)

	_, err = parsePushTarget("group:chat1?mentions=%zz")
	require.Error(t, err)
}

func TestBuildScopedSessionID(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"wecom:chat:chat1",
		buildScopedSessionID(
			"chat1",
			"user1",
			groupSessionModeShared,
		),
	)
	require.Equal(
		t,
		"wecom:chat:chat1:user:user1",
		buildScopedSessionID(
			"chat1",
			"user1",
			groupSessionModeIsolated,
		),
	)
	require.Equal(
		t,
		"wecom:dm:user1",
		buildScopedSessionID(
			"",
			"user1",
			groupSessionModeIsolated,
		),
	)
}

func TestBuildGatewayUserID(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"wecom:dm:wineguo",
		buildGatewayUserID(
			"T80660013A",
			"",
			map[string]string{"T80660013A": "wineguo"},
		),
	)
	require.Equal(
		t,
		"wecom:dm:alice.dev",
		buildGatewayUserID(
			"T00010001",
			"",
			map[string]string{"T00010001": "alice.dev"},
		),
	)
	require.Equal(
		t,
		"wecom:dm:T00010001",
		buildGatewayUserID(
			"T00010001",
			"",
			map[string]string{"T00010001": "郭琪周"},
		),
	)
	require.Equal(
		t,
		"wecom:dm:T00010001",
		buildGatewayUserID(
			"T00010001",
			"",
			map[string]string{"T00010001": "bad:name"},
		),
	)
	require.Equal(
		t,
		"wecom:dm:T00010001",
		buildGatewayUserID("T00010001", "", nil),
	)
	require.Equal(
		t,
		"wecom:dm:zeronezhang",
		buildGatewayUserID("T00010002", "zeronezhang", nil),
	)
}

func TestBuildGatewayTraceName(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"claw-1-wecom-person",
		buildGatewayTraceName(
			"claw-1",
			wecomTraceTransportPerson,
		),
	)
	require.Equal(
		t,
		"claw-1-wecom-group",
		buildGatewayTraceName(
			"claw-1",
			wecomTraceTransportGroup,
		),
	)
	require.Equal(
		t,
		"openclaw-wecom-person",
		buildGatewayTraceName(
			"",
			"",
		),
	)
}

func TestGatewayTraceAttributesUsesStableNameAndMessageMetadata(t *testing.T) {
	t.Parallel()

	attrs := gatewayTraceAttributes(
		gwclient.MessageRequest{
			From:      "T80660013A",
			Thread:    "wecom:dm:T80660013A",
			MessageID: "msg-1",
			Text:      "hello",
			UserID:    "wecom:dm:wineguo",
			SessionID: "wecom:dm:T80660013A",
			RequestID: "req-1",
		},
		gatewayTraceIdentity{
			TraceOwner:    "claw-1",
			ClawID:        "claw-1",
			TransportKind: wecomTraceTransportPerson,
			ActorLabel:    "wineguo",
		},
	)

	values := make(map[string]string, len(attrs))
	boolValues := make(map[string]bool, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value.AsString()
		boolValues[string(attr.Key)] = attr.Value.AsBool()
	}
	require.Equal(
		t,
		"claw-1-wecom-person",
		values[langfuseTraceNameAttribute],
	)
	require.True(t, boolValues[langfuseInternalAsRoot])
	require.NotContains(
		t,
		values[langfuseTraceNameAttribute],
		"msg-1",
	)
	require.Equal(t, "msg-1", values[langfuseMetadataMessageID])
	require.Equal(t, "claw-1", values[langfuseMetadataClawID])
	require.Equal(t, "wecom:dm:wineguo", values[langfuseSessionIDAttribute])
	require.Equal(
		t,
		"wecom:dm:T80660013A",
		values[langfuseMetadataTransportSID],
	)
	require.Equal(
		t,
		wecomTraceTransportPerson,
		values[langfuseMetadataTransportKind],
	)
}

func TestBuildGatewayTraceSessionID(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"wecom:dm:wineguo:1779938395",
		buildGatewayTraceSessionID(
			"wecom:dm:T80660013A:1779938395",
			"wineguo",
		),
	)
	require.Equal(
		t,
		"wecom:thread:group-1",
		buildGatewayTraceSessionID("wecom:thread:group-1", "wineguo"),
	)
}

func TestResolveGatewayTraceIdentity(t *testing.T) {
	t.Setenv(traceNameClawIDEnv, "claw-1")

	identity := resolveGatewayTraceIdentity(
		"wineguo",
		"chat1",
		wecomTraceChatTypeGroup,
		"app-1",
		"bot-1",
		"channel-1",
	)
	require.Equal(t, "claw-1", identity.TraceOwner)
	require.Equal(t, "claw-1", identity.ClawID)
	require.Equal(t, wecomTraceTransportGroup, identity.TransportKind)
	require.Equal(t, "wineguo", identity.ActorLabel)
}

func TestResolveGatewayTraceIdentityFallsBackToAppName(t *testing.T) {
	identity := resolveGatewayTraceIdentity(
		"",
		"",
		"",
		"app-1",
		"bot-1",
		"channel-1",
	)
	require.Equal(t, "app-1", identity.TraceOwner)
	require.Empty(t, identity.ClawID)
	require.Equal(t, wecomTraceTransportPerson, identity.TransportKind)
}

func TestResolveWeComTraceTransportKind(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		wecomTraceTransportGroup,
		resolveWeComTraceTransportKind(
			"",
			wecomTraceChatTypeGroup,
		),
	)
	require.Equal(
		t,
		wecomTraceTransportPerson,
		resolveWeComTraceTransportKind(
			"chat1",
			wecomTraceChatTypeSingle,
		),
	)
	require.Equal(
		t,
		wecomTraceTransportPerson,
		resolveWeComTraceTransportKind("chat1", ""),
	)
	require.Equal(
		t,
		wecomTraceTransportPerson,
		resolveWeComTraceTransportKind("", ""),
	)
}

func TestParseChatPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
		err   bool
	}{
		{"", chatPolicyOpen, false},
		{"open", chatPolicyOpen, false},
		{"disabled", chatPolicyDisabled, false},
		{"allowlist", chatPolicyAllowlist, false},
		{"Open", chatPolicyOpen, false},
		{"ALLOWLIST", chatPolicyAllowlist, false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := parseChatPolicy(tt.input)
			if tt.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseGroupSessionMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
		err   bool
	}{
		{"", groupSessionModeShared, false},
		{"shared", groupSessionModeShared, false},
		{"isolated", groupSessionModeIsolated, false},
		{"SHARED", groupSessionModeShared, false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := parseGroupSessionMode(tt.input)
			if tt.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseUserLabelMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
		err   bool
	}{
		{"", defaultUserLabelMode, false},
		{"name_or_alias", userLabelModeNameOrAlias, false},
		{"alias_or_name", userLabelModeAliasOrName, false},
		{"name", userLabelModeName, false},
		{"alias", userLabelModeAlias, false},
		{"id", userLabelModeID, false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := parseUserLabelMode(tt.input)
			if tt.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolvedIdentityLabels(t *testing.T) {
	t.Parallel()

	labels := resolvedIdentityLabels(
		userLabelModeAliasOrName,
		map[string]userIdentity{
			"T1": {
				UserID:      "T1",
				AccountName: "alice",
			},
			"T2": {
				UserID:      "T2",
				AccountName: "alice",
			},
			"T3": {
				UserID:      "T3",
				DisplayName: "张三",
			},
		},
	)

	require.Equal(t, "alice(T1)", labels["T1"])
	require.Equal(t, "alice(T2)", labels["T2"])
	require.Equal(t, "张三", labels["T3"])
}

func TestBuildIdentityPromptNoteUsesCanonicalLabels(t *testing.T) {
	t.Parallel()

	note := buildIdentityPromptNote(
		map[string]string{
			"T1": "zeronezhang",
			"T2": "Alice",
		},
	)

	require.Contains(
		t,
		note,
		"Use the mapped label exactly when referring to a participant",
	)
	require.Contains(t, note, "- T1 => zeronezhang")
	require.Contains(t, note, "- T2 => Alice")
}

func TestCanonicalizeResolvedParticipantMentions(t *testing.T) {
	t.Parallel()

	got := canonicalizeResolvedParticipantMentions(
		"@X 每 10s 定时给 @zeronezhang(张子良) 和 "+
			"nanjianyang（南建阳） 发一句话",
		map[string]string{
			"T1": "zeronezhang",
			"T2": "nanjianyang",
		},
	)

	require.Equal(
		t,
		"@X 每 10s 定时给 @zeronezhang 和 nanjianyang 发一句话",
		got,
	)
}

func TestMessageUserLabel(t *testing.T) {
	t.Parallel()

	msg := WebhookMessage{
		From: FromInfo{
			UserID: "u1",
			Name:   "中文名",
			Alias:  "EnglishName",
		},
	}

	require.Equal(
		t,
		"中文名",
		messageUserLabel(msg, userLabelModeNameOrAlias),
	)
	require.Equal(
		t,
		"EnglishName",
		messageUserLabel(msg, userLabelModeAliasOrName),
	)
	require.Equal(
		t,
		"EnglishName",
		messageUserLabel(msg, userLabelModeAlias),
	)
	require.Equal(
		t,
		"u1",
		messageUserLabel(msg, userLabelModeID),
	)
}

func TestBuildAllowSet(t *testing.T) {
	t.Parallel()

	require.Nil(t, buildAllowSet(nil))
	require.Nil(t, buildAllowSet([]string{}))
	require.Nil(t, buildAllowSet([]string{"", "  "}))

	m := buildAllowSet([]string{"a", "b", "  c  "})
	require.Len(t, m, 3)
	_, ok := m["c"]
	require.True(t, ok)
}

func TestSplitRunes(t *testing.T) {
	t.Parallel()

	t.Run("short text", func(t *testing.T) {
		t.Parallel()
		parts := splitRunes("hello", 100)
		require.Equal(t, []string{"hello"}, parts)
	})

	t.Run("split at newline", func(t *testing.T) {
		t.Parallel()
		text := strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10)
		parts := splitRunes(text, 15)
		require.Len(t, parts, 2)
		require.Equal(t, strings.Repeat("a", 10)+"\n", parts[0])
	})
}

func TestSplitReplyText(t *testing.T) {
	t.Parallel()

	reply := strings.Repeat("a", maxReplyRunes) + "\nnext"
	parts := splitReplyText(reply)
	require.Len(t, parts, 2)
	require.NotContains(t, parts[0], continuedReplyPrefix)
	require.True(
		t,
		strings.HasPrefix(parts[1], continuedReplyPrefix),
	)
}

type singleReplyMockSender struct {
	mockSender
}

func (s *singleReplyMockSender) supportsMultipartReplies() bool {
	return false
}

func TestSendTextReplySplitsLongContent(t *testing.T) {
	t.Parallel()

	sender := &mockSender{}
	content := strings.Repeat("a", maxReplyRunes) + "\nnext"

	err := sendTextReply(
		context.Background(),
		sender,
		"chat1",
		content,
	)
	require.NoError(t, err)
	require.Equal(t, splitReplyText(content), sender.textCalls)
}

func TestSendTextReplyTruncatesForSingleReplySender(
	t *testing.T,
) {
	t.Parallel()

	sender := &singleReplyMockSender{}
	content := strings.Repeat("长", maxReplyRunes+50)

	err := sendTextReply(
		context.Background(),
		sender,
		"chat1",
		content,
	)
	require.NoError(t, err)
	require.Len(t, sender.textCalls, 1)
	require.Contains(
		t,
		sender.lastText,
		commandReplyTruncationNotice,
	)
	require.LessOrEqual(
		t,
		len([]rune(sender.lastText)),
		maxReplyRunes,
	)
}

func TestSplitIndex(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, splitIndex([]rune("a"), 4))
	require.Equal(t, 2, splitIndex([]rune("a "), 4))
	require.Equal(t, 3, splitIndex([]rune("a\n\n"), 4))
	require.Equal(t, 2, splitIndex([]rune("a\n"), 4))
	require.Equal(t, 4, splitIndex([]rune("abcd"), 4))
}
