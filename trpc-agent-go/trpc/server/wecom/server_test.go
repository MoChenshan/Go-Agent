package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := New(nil, Config{})
	require.ErrorContains(t, err, "runner cannot be nil")

	_, err = New(&fakeRunner{}, Config{Secret: "secret"})
	require.ErrorContains(t, err, "AI bot id cannot be empty")

	_, err = New(&fakeRunner{}, Config{BotID: "bot"})
	require.ErrorContains(t, err, "secret cannot be empty")
}

func TestHandleIncomingMessageWithSenderRunsRunner(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{
		stream: func(requestID string) <-chan *event.Event {
			return buildEventStream(requestID, "hello world")
		},
	}
	s, err := New(r, Config{
		BotID:        "bot",
		Secret:       "secret",
		EnableStream: true,
	})
	require.NoError(t, err)

	sender := &recordingSender{}
	msg := WebhookMessage{
		MsgID:   "msg-1",
		ChatID:  "chat-1",
		MsgType: messageTypeText,
		From:    FromInfo{UserID: "user-1"},
		Text:    TextContent{Content: "hello"},
	}

	require.NoError(t, s.handleIncomingMessageWithSender(
		context.Background(),
		msg,
		sender,
	))
	require.Len(t, r.calls, 1)
	require.Equal(t, "user-1", r.calls[0].userID)
	require.Equal(t, "wecom:chat:chat-1", r.calls[0].sessionID)
	require.Equal(t, "hello", r.calls[0].content)
	require.NotEmpty(t, r.calls[0].requestID)

	require.Len(t, sender.streams, 3)
	require.Equal(t, defaultProcessingMessage, sender.streams[0].content)
	require.Equal(t, "hello ", sender.streams[1].content)
	require.Equal(t, "hello world", sender.streams[2].content)
	require.True(t, sender.streams[2].finish)
}

func TestHandleIncomingMessageWithSenderHandlesCommands(t *testing.T) {
	t.Parallel()

	r := &fakeManagedRunner{}
	s, err := New(r, Config{BotID: "bot", Secret: "secret"})
	require.NoError(t, err)

	sessionID := s.sessions.Active("chat-1", "user-1")
	s.inflight.Set(sessionID, "req-1")

	sender := &recordingSender{}
	msg := WebhookMessage{
		ChatID:  "chat-1",
		MsgType: messageTypeText,
		From:    FromInfo{UserID: "user-1"},
		Text:    TextContent{Content: "/cancel"},
	}
	require.NoError(t, s.handleIncomingMessageWithSender(
		context.Background(),
		msg,
		sender,
	))
	require.Equal(t, []string{"req-1"}, r.canceled)
	require.Equal(t, defaultCancelOKMessage, sender.markdowns[0])

	msg.Text.Content = "/new"
	require.NoError(t, s.handleIncomingMessageWithSender(
		context.Background(),
		msg,
		sender,
	))
	require.Len(t, sender.markdowns, 2)
	require.Equal(t, defaultNewSessionMessage, sender.markdowns[1])
	require.NotEqual(
		t,
		sessionID,
		s.sessions.Active("chat-1", "user-1"),
	)
}

func TestHandleIncomingMessageWithSenderHandlesEnterChat(t *testing.T) {
	t.Parallel()

	s, err := New(
		&fakeRunner{},
		Config{BotID: "bot", Secret: "secret"},
		WithWelcomeMessage("welcome"),
	)
	require.NoError(t, err)

	sender := &recordingSender{}
	msg := WebhookMessage{
		MsgType: messageTypeEvent,
		From:    FromInfo{UserID: "user-1"},
		Event: EventContent{
			EventType: eventTypeEnterChat,
		},
	}
	require.NoError(t, s.handleIncomingMessageWithSender(
		context.Background(),
		msg,
		sender,
	))
	require.Equal(t, []string{"welcome"}, sender.markdowns)
}

func TestHandleIncomingMessageUsesCallbackSender(t *testing.T) {
	t.Parallel()

	s, err := New(&fakeRunner{}, Config{
		BotID:  "bot",
		Secret: "secret",
	})
	require.NoError(t, err)

	writer := &capturingReplyWriter{}
	msg := WebhookMessage{
		ChatID:        "chat-1",
		MsgType:       messageTypeText,
		From:          FromInfo{UserID: "user-1"},
		Text:          TextContent{Content: "/help"},
		CallbackReqID: "req-1",
		ReplyWriter:   writer,
	}

	require.NoError(t, s.handleIncomingMessage(context.Background(), msg))
	require.Len(t, writer.frames, 1)

	body, ok := writer.frames[0].Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, msgTypeMarkdown, body.MsgType)
	require.Equal(t, defaultHelpMessage, body.Markdown.Content)
}

func TestHandleCancelCommandBranches(t *testing.T) {
	t.Parallel()

	msg := WebhookMessage{
		ChatID:  "chat-1",
		MsgType: messageTypeText,
		From:    FromInfo{UserID: "user-1"},
		Text:    TextContent{Content: "/cancel"},
	}

	s, err := New(&fakeRunner{}, Config{
		BotID:  "bot",
		Secret: "secret",
	})
	require.NoError(t, err)

	sender := &recordingSender{}
	require.NoError(t, s.handleIncomingMessageWithSender(
		context.Background(),
		msg,
		sender,
	))
	require.Equal(
		t,
		[]string{defaultCancelUnsupportedMessage},
		sender.markdowns,
	)

	managed := &fakeManagedRunner{}
	s, err = New(managed, Config{BotID: "bot", Secret: "secret"})
	require.NoError(t, err)

	sender = &recordingSender{}
	require.NoError(t, s.handleIncomingMessageWithSender(
		context.Background(),
		msg,
		sender,
	))
	require.Equal(t, []string{defaultCancelNoopMessage}, sender.markdowns)
}

func TestHandleIncomingMessageWithSenderIgnoresOtherEvents(t *testing.T) {
	t.Parallel()

	s, err := New(&fakeRunner{}, Config{
		BotID:  "bot",
		Secret: "secret",
	})
	require.NoError(t, err)

	sender := &recordingSender{}
	msg := WebhookMessage{
		MsgType: messageTypeEvent,
		Event: EventContent{
			EventType: "leave_chat",
		},
	}

	require.NoError(t, s.handleIncomingMessageWithSender(
		context.Background(),
		msg,
		sender,
	))
	require.Empty(t, sender.markdowns)
}

func TestReplyFromEventsHandlesError(t *testing.T) {
	t.Parallel()

	s, err := New(&fakeRunner{}, Config{
		BotID:        "bot",
		Secret:       "secret",
		EnableStream: true,
	})
	require.NoError(t, err)

	ch := make(chan *event.Event, 1)
	ch <- &event.Event{
		Response: &model.Response{
			Error: &model.ResponseError{Message: "runner failed"},
		},
	}
	close(ch)

	sender := &recordingSender{}
	require.NoError(t, s.replyFromEvents(
		context.Background(),
		WebhookMessage{
			ChatID: "chat-1",
			MsgID:  "msg-1",
		},
		sender,
		ch,
	))
	require.Len(t, sender.streams, 2)
	require.Equal(t, defaultProcessingMessage, sender.streams[0].content)
	require.Equal(t, "runner failed", sender.streams[1].content)
	require.True(t, sender.streams[1].finish)
}

func TestReplyHelpers(t *testing.T) {
	t.Parallel()

	require.Empty(t, partialReply(nil))
	require.Empty(t, finalReply(nil))
	require.Equal(t, defaultInternalErrorMessage, responseErrorMessage(
		&event.Event{Response: &model.Response{}},
	))

	partial := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:    model.RoleTool,
						Content: "ignored",
					},
				},
				{
					Delta: model.Message{
						Role:    model.RoleAssistant,
						Content: "hello",
					},
				},
			},
		},
	}
	require.Equal(t, "hello", partialReply(partial))

	final := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:    model.RoleTool,
						Content: "ignored",
					},
				},
				{
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "  final answer  ",
					},
				},
			},
		},
	}
	require.Equal(t, "final answer", finalReply(final))
}

func TestRunWebSocketSessionSubscribesAndReplies(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 4)
	serverErr := make(chan error, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		var frame wsOutboundFrame
		if err := conn.ReadJSON(&frame); err != nil {
			serverErr <- err
			return
		}
		received <- frame

		body, err := json.Marshal(WebhookMessage{
			MsgID:   "msg-1",
			ChatID:  "chat-1",
			MsgType: messageTypeText,
			From:    FromInfo{UserID: "user-1"},
			Text:    TextContent{Content: "hello"},
		})
		if err != nil {
			serverErr <- err
			return
		}
		if err := conn.WriteJSON(wsInboundFrame{
			Command: wsCommandMessageCallback,
			Headers: wsFrameHeaders{ReqID: "req-1"},
			Body:    body,
		}); err != nil {
			serverErr <- err
			return
		}

		for i := 0; i < 3; i++ {
			frame = wsOutboundFrame{}
			if err := conn.ReadJSON(&frame); err != nil {
				serverErr <- err
				return
			}
			received <- frame
		}

		serverErr <- nil
	}))
	defer ts.Close()

	r := &fakeRunner{
		stream: func(requestID string) <-chan *event.Event {
			return buildEventStream(requestID, "hello world")
		},
	}
	s, err := New(r, Config{
		BotID:             "bot-1",
		Secret:            "secret-1",
		WebSocketURL:      "ws" + strings.TrimPrefix(ts.URL, "http"),
		HeartbeatInterval: time.Hour,
		EnableStream:      true,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)
	subscribeBody, ok := subscribe.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "bot-1", subscribeBody["bot_id"])
	require.Equal(t, "secret-1", subscribeBody["secret"])

	firstReply := <-received
	require.Equal(t, wsCommandRespond, firstReply.Command)
	require.Equal(t, "req-1", firstReply.Headers.ReqID)

	secondReply := <-received
	require.Equal(t, wsCommandRespond, secondReply.Command)

	lastReply := <-received
	require.Equal(t, wsCommandRespond, lastReply.Command)

	require.NoError(t, <-serverErr)
	cancel()
	require.Error(t, <-errCh)
}

type fakeRunner struct {
	mu     sync.Mutex
	calls  []runCall
	stream func(requestID string) <-chan *event.Event
	err    error
}

type runCall struct {
	userID    string
	sessionID string
	content   string
	requestID string
}

func (f *fakeRunner) Run(
	_ context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	if f.err != nil {
		return nil, f.err
	}

	var options agent.RunOptions
	for _, runOpt := range runOpts {
		runOpt(&options)
	}

	f.mu.Lock()
	f.calls = append(f.calls, runCall{
		userID:    userID,
		sessionID: sessionID,
		content:   message.Content,
		requestID: options.RequestID,
	})
	f.mu.Unlock()

	if f.stream != nil {
		return f.stream(options.RequestID), nil
	}

	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func (f *fakeRunner) Close() error {
	return nil
}

type fakeManagedRunner struct {
	fakeRunner
	canceled []string
}

func (f *fakeManagedRunner) Cancel(requestID string) bool {
	f.canceled = append(f.canceled, requestID)
	return true
}

func (f *fakeManagedRunner) RunStatus(
	string,
) (runner.RunStatus, bool) {
	return runner.RunStatus{}, false
}

type recordingSender struct {
	markdowns []string
	streams   []streamCall
}

type streamCall struct {
	streamID string
	content  string
	finish   bool
}

func (s *recordingSender) SendMarkdown(
	_ context.Context,
	_ string,
	content string,
) error {
	s.markdowns = append(s.markdowns, content)
	return nil
}

func (s *recordingSender) SendStream(
	_ context.Context,
	_ string,
	streamID string,
	content string,
	finish bool,
) error {
	s.streams = append(s.streams, streamCall{
		streamID: streamID,
		content:  content,
		finish:   finish,
	})
	return nil
}

func buildEventStream(
	requestID string,
	finalText string,
) <-chan *event.Event {
	ch := make(chan *event.Event, 3)
	ch <- &event.Event{
		RequestID: requestID,
		Response: &model.Response{
			Object:    model.ObjectTypeChatCompletionChunk,
			IsPartial: true,
			Choices: []model.Choice{{
				Delta: model.Message{
					Role:    model.RoleAssistant,
					Content: "hello ",
				},
			}},
		},
	}
	ch <- &event.Event{
		RequestID: requestID,
		Response: &model.Response{
			Object: model.ObjectTypeChatCompletion,
			Done:   true,
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: finalText,
				},
			}},
		},
	}
	ch <- &event.Event{
		RequestID: requestID,
		Response: &model.Response{
			Object: model.ObjectTypeRunnerCompletion,
			Done:   true,
		},
	}
	close(ch)
	return ch
}
