package wecom

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type capturingReplyWriter struct {
	frames []wsOutboundFrame
}

func (w *capturingReplyWriter) send(
	_ context.Context,
	frame wsOutboundFrame,
) error {
	w.frames = append(w.frames, frame)
	return nil
}

func TestAIBotWebSocketSender(t *testing.T) {
	t.Parallel()

	writer := &capturingReplyWriter{}
	sender := newAIBotWebSocketSender(writer, "req-1")

	require.NoError(t, sender.SendMarkdown(
		context.Background(),
		"chat-1",
		"hello",
	))
	require.NoError(t, sender.SendStream(
		context.Background(),
		"chat-1",
		"stream-1",
		"partial",
		false,
	))

	require.Len(t, writer.frames, 2)
	require.Equal(t, wsCommandRespond, writer.frames[0].Command)
	require.Equal(t, "req-1", writer.frames[0].Headers.ReqID)

	body, ok := writer.frames[0].Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, msgTypeMarkdown, body.MsgType)
	require.Equal(t, "hello", body.Markdown.Content)

	body, ok = writer.frames[1].Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, messageTypeStream, body.MsgType)
	require.Equal(t, "stream-1", body.Stream.ID)
	require.Equal(t, "partial", body.Stream.Content)
	require.False(t, body.Stream.Finish)
}

func TestAIBotWebSocketSenderRejectsNilWriter(t *testing.T) {
	t.Parallel()

	sender := &aibotWebSocketSender{}

	err := sender.SendMarkdown(context.Background(), "chat-1", "hello")
	require.ErrorContains(t, err, "websocket sender is nil")

	err = sender.SendStream(
		context.Background(),
		"chat-1",
		"stream-1",
		"hello",
		true,
	)
	require.ErrorContains(t, err, "websocket sender is nil")
}

func TestReplyStreamState(t *testing.T) {
	t.Parallel()

	state := &replyStreamState{}
	require.True(t, shouldSendSnapshot(state, "hello"))

	markSnapshotSent(state, "hello")
	require.False(t, shouldSendSnapshot(state, "hello"))
	require.False(t, shouldSendSnapshot(state, "hello world"))

	state.lastSentAt = time.Now().Add(-streamSnapshotMinInterval)
	require.True(t, shouldSendSnapshot(state, "hello world"))

	markSnapshotSent(state, "hello world")
	state.lastSentAt = time.Now()
	require.True(t, shouldSendSnapshot(
		state,
		"hello world "+strings.Repeat("a", streamSnapshotMinRunes),
	))
}

func TestSenderHelpers(t *testing.T) {
	t.Parallel()

	_, err := senderForMessage(WebhookMessage{})
	require.ErrorContains(t, err, "missing reply writer")

	writer := &capturingReplyWriter{}
	_, err = senderForMessage(WebhookMessage{ReplyWriter: writer})
	require.ErrorContains(t, err, "missing req id")

	sender, err := senderForMessage(WebhookMessage{
		ReplyWriter:   writer,
		CallbackReqID: "req-1",
	})
	require.NoError(t, err)
	require.NoError(t, sender.SendMarkdown(
		context.Background(),
		"chat-1",
		"hello",
	))
	require.Len(t, writer.frames, 1)
}

func TestSendMarkdownChunks(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("a", maxReplyRunes+12)
	sender := &recordingSender{}

	require.NoError(t, sendMarkdownChunks(
		context.Background(),
		sender,
		"chat-1",
		content,
	))
	require.Len(t, sender.markdowns, 2)
	require.Len(t, []rune(sender.markdowns[0]), maxReplyRunes)
	require.Len(t, []rune(sender.markdowns[1]), 12)
}
