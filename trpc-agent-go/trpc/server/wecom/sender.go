package wecom

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"
)

const (
	streamSnapshotMinInterval = 120 * time.Millisecond
	streamSnapshotMinRunes    = 24
)

type messageSender interface {
	SendMarkdown(
		ctx context.Context,
		chatID string,
		content string,
	) error
	SendStream(
		ctx context.Context,
		chatID string,
		streamID string,
		content string,
		finish bool,
	) error
}

type wsReplyWriter interface {
	send(ctx context.Context, frame wsOutboundFrame) error
}

type aibotMarkdown struct {
	Content string `json:"content"`
}

type aibotStream struct {
	ID      string `json:"id,omitempty"`
	Finish  bool   `json:"finish"`
	Content string `json:"content,omitempty"`
}

type aibotWebSocketSender struct {
	writer wsReplyWriter
	reqID  string
}

func newAIBotWebSocketSender(
	writer wsReplyWriter,
	reqID string,
) *aibotWebSocketSender {
	return &aibotWebSocketSender{
		writer: writer,
		reqID:  reqID,
	}
}

func (s *aibotWebSocketSender) SendMarkdown(
	ctx context.Context,
	_ string,
	content string,
) error {
	if s == nil || s.writer == nil {
		return fmt.Errorf("wecom: websocket sender is nil")
	}
	frame := wsOutboundFrame{
		Command: wsCommandRespond,
		Headers: wsFrameHeaders{ReqID: s.reqID},
		Body: wsReplyBody{
			MsgType:  msgTypeMarkdown,
			Markdown: &aibotMarkdown{Content: content},
		},
	}
	return s.writer.send(ctx, frame)
}

func (s *aibotWebSocketSender) SendStream(
	ctx context.Context,
	_ string,
	streamID string,
	content string,
	finish bool,
) error {
	if s == nil || s.writer == nil {
		return fmt.Errorf("wecom: websocket sender is nil")
	}
	frame := wsOutboundFrame{
		Command: wsCommandRespond,
		Headers: wsFrameHeaders{ReqID: s.reqID},
		Body: wsReplyBody{
			MsgType: messageTypeStream,
			Stream: &aibotStream{
				ID:      streamID,
				Finish:  finish,
				Content: content,
			},
		},
	}
	return s.writer.send(ctx, frame)
}

type replyStreamState struct {
	streamID      string
	content       string
	lastSentRunes int
	lastSentAt    time.Time
	started       bool
}

func shouldSendSnapshot(state *replyStreamState, content string) bool {
	if state == nil {
		return false
	}
	if !state.started {
		return true
	}

	runes := utf8.RuneCountInString(content)
	if runes <= state.lastSentRunes {
		return false
	}
	if runes-state.lastSentRunes >= streamSnapshotMinRunes {
		return true
	}
	return time.Since(state.lastSentAt) >= streamSnapshotMinInterval
}

func markSnapshotSent(state *replyStreamState, content string) {
	if state == nil {
		return
	}
	state.started = true
	state.content = content
	state.lastSentRunes = utf8.RuneCountInString(content)
	state.lastSentAt = time.Now()
}
