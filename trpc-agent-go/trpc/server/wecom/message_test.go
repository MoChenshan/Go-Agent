package wecom

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessageHelpers(t *testing.T) {
	t.Parallel()

	require.Equal(t, commandHelp, parseCommand(" /HELP ").name)
	require.Equal(t, commandNew, parseCommand(" /clear ").name)
	require.Equal(t, commandCancel, parseCommand("/cancel").name)
	require.Empty(t, parseCommand("hello").name)

	require.Equal(t, "user-1", messageUserID(WebhookMessage{
		From: FromInfo{UserID: " user-1 "},
	}))
	require.Equal(t, "alias-1", messageUserID(WebhookMessage{
		From: FromInfo{Alias: " alias-1 "},
	}))
	require.Equal(t, defaultUnknownUserID, messageUserID(WebhookMessage{}))

	require.Equal(
		t,
		"wecom:chat:chat-1",
		baseSessionID(" chat-1 ", "user-1"),
	)
	require.Equal(t, "wecom:dm:user-1", baseSessionID("", "user-1"))

	require.Equal(t, "wecom:chat-1:msg-1", buildRequestID(WebhookMessage{
		ChatID: " chat-1 ",
		MsgID:  " msg-1 ",
	}))
	require.Equal(t, "wecom:alias-1:req-1", buildRequestID(WebhookMessage{
		From:          FromInfo{Alias: "alias-1"},
		CallbackReqID: " req-1 ",
	}))

	require.Equal(t, "wecom-stream-req-1", buildStreamID(" req-1 "))
	require.True(t, strings.HasPrefix(buildStreamID(""), streamIDPrefix))
}

func TestExtractMessageText(t *testing.T) {
	t.Parallel()

	text, ok := extractMessageText(WebhookMessage{
		MsgType: messageTypeText,
		Text: TextContent{
			Content: " @assistant hello ",
		},
	}, "assistant")
	require.True(t, ok)
	require.Equal(t, "hello", text)

	text, ok = extractMessageText(WebhookMessage{
		MsgType: messageTypeMixed,
		MixedMessage: MixedMessageContent{
			MsgItem: []MixedMessageItem{
				{
					MsgType: messageTypeText,
					Text: TextContent{
						Content: "first",
					},
				},
				{MsgType: "image"},
				{
					MsgType: messageTypeText,
					Text: TextContent{
						Content: " @assistant second ",
					},
				},
			},
		},
	}, "assistant")
	require.True(t, ok)
	require.Equal(t, "first\nsecond", text)

	_, ok = extractMessageText(WebhookMessage{
		MsgType: messageTypeMixed,
		MixedMessage: MixedMessageContent{
			MsgItem: []MixedMessageItem{{MsgType: "image"}},
		},
	}, "assistant")
	require.False(t, ok)

	_, ok = extractMessageText(WebhookMessage{MsgType: "image"}, "")
	require.False(t, ok)
}

func TestNormalizeIncomingTextAndSplitRunes(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"hello there",
		normalizeIncomingText(" @assistant hello there ", "assistant"),
	)
	require.Equal(t, "hello", normalizeIncomingText(" hello ", ""))

	require.Equal(t, []string{"hello"}, splitRunes("hello", 0))
	require.Equal(t, []string{"hello"}, splitRunes("hello", 10))
	require.Equal(t, []string{"abc\n", "def\n", "ghi"}, splitRunes(
		"abc\ndef\nghi",
		6,
	))
	require.Equal(t, []string{"hel", "lo"}, splitRunes("hello", 3))
}

func TestOptionOverrides(t *testing.T) {
	t.Parallel()

	opts := defaultOptions()
	WithHelpMessage(" help ")(&opts)
	WithWelcomeMessage(" welcome ")(&opts)
	WithProcessingMessage(" processing ")(&opts)
	WithNewSessionMessage(" new ")(&opts)
	WithUnsupportedMessage(" unsupported ")(&opts)

	require.Equal(t, "help", opts.helpMessage)
	require.Equal(t, "welcome", opts.welcomeMessage)
	require.Equal(t, "processing", opts.processingMessage)
	require.Equal(t, "new", opts.newSessionMessage)
	require.Equal(t, "unsupported", opts.unsupportedMessage)
}
