package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestRunWebSocketSessionHandlesMessageCallback(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 2)
	replied := make(chan wsOutboundFrame, 1)
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
			MsgID:   "msg1",
			ChatID:  "chat1",
			From:    FromInfo{UserID: "user1"},
			MsgType: MsgTypeText,
			Text: TextContent{
				Content: "hello",
			},
		})
		if err != nil {
			serverErr <- err
			return
		}

		if err := conn.WriteJSON(wsInboundFrame{
			Command: wsCommandMsgCallback,
			Headers: wsFrameHeaders{ReqID: "req-ws-1"},
			Body:    body,
		}); err != nil {
			serverErr <- err
			return
		}

		frame = wsOutboundFrame{}
		if err := conn.ReadJSON(&frame); err != nil {
			serverErr <- err
			return
		}
		replied <- frame
		if err := conn.WriteJSON(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: "req-ws-1"},
		}); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ch := &Channel{
		gw: &recordingGateway{reply: "ok"},
		cfg: channelCfg{
			AIBotID: "bot1",
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		chatPolicy:        chatPolicyOpen,
		connectionMode:    connectionModeWebSocket,
		wsURL:             wsURL,
		wsSecret:          "secret1",
		heartbeatInterval: time.Hour,
		lanes:             newLaneLocker(),
		inflight:          newInflightRequests(),
		sessionTracker:    newSessionTracker(),
		aggregator:        newMessageAggregator(0),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)
	require.NotEmpty(t, subscribe.Headers.ReqID)
	subscribeBody, ok := subscribe.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "bot1", subscribeBody["bot_id"])
	require.Equal(t, "secret1", subscribeBody["secret"])

	reply := <-replied
	require.Equal(t, wsCommandRespond, reply.Command)
	require.Equal(t, "req-ws-1", reply.Headers.ReqID)
	replyBody, ok := reply.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, msgTypeMarkdown, replyBody["msgtype"])
	markdown, ok := replyBody["markdown"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "ok", markdown["content"])

	require.NoError(t, <-serverErr)

	cancel()
	requireWebSocketSessionExit(t, <-errCh)
}

func TestRunWebSocketSessionHandlesEnterChatEvent(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 2)
	replied := make(chan wsOutboundFrame, 1)
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
			MsgID:   "evt1",
			ChatID:  "chat1",
			From:    FromInfo{UserID: "user1"},
			MsgType: MsgTypeEvent,
			Event: EventContent{
				EventType: eventTypeEnterChat,
			},
		})
		if err != nil {
			serverErr <- err
			return
		}

		if err := conn.WriteJSON(wsInboundFrame{
			Command: wsCommandEventCallback,
			Headers: wsFrameHeaders{ReqID: "req-enter-1"},
			Body:    body,
		}); err != nil {
			serverErr <- err
			return
		}

		frame = wsOutboundFrame{}
		if err := conn.ReadJSON(&frame); err != nil {
			serverErr <- err
			return
		}
		replied <- frame
		if err := conn.WriteJSON(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: "req-enter-1"},
		}); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ch := &Channel{
		gw:                stubGateway{},
		cfg:               channelCfg{AIBotID: "bot1"},
		botMode:           botModeAI,
		chatPolicy:        chatPolicyOpen,
		enterChatWelcome:  true,
		connectionMode:    connectionModeWebSocket,
		runtimeModelName:  "gpt-5.2",
		wsURL:             wsURL,
		wsSecret:          "secret1",
		heartbeatInterval: time.Hour,
		lanes:             newLaneLocker(),
		inflight:          newInflightRequests(),
		sessionTracker:    newSessionTracker(),
		aggregator:        newMessageAggregator(0),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)
	require.NotEmpty(t, subscribe.Headers.ReqID)

	reply := <-replied
	require.Equal(t, wsCommandRespondWelcome, reply.Command)
	require.Equal(t, "req-enter-1", reply.Headers.ReqID)
	replyBody, ok := reply.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, msgTypeTemplateCard, replyBody["msgtype"])
	templateCard, ok := replyBody["template_card"].(map[string]any)
	require.True(t, ok)
	require.Equal(
		t,
		templateCardTypeButtonInteraction,
		templateCard["card_type"],
	)
	mainTitle, ok := templateCard["main_title"].(map[string]any)
	require.True(t, ok)
	require.Equal(
		t,
		"欢迎回来，先点需要的面板就行。",
		mainTitle["desc"],
	)
	buttonList, ok := templateCard["button_list"].([]any)
	require.True(t, ok)
	require.Len(t, buttonList, 6)

	require.NoError(t, <-serverErr)

	cancel()
	requireWebSocketSessionExit(t, <-errCh)
}

func TestRunWebSocketSessionSupportsProactiveSend(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 2)
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

		frame = wsOutboundFrame{}
		if err := conn.ReadJSON(&frame); err != nil {
			serverErr <- err
			return
		}
		received <- frame
		if err := conn.WriteJSON(wsInboundFrame{
			Headers: wsFrameHeaders{
				ReqID: frame.Headers.ReqID,
			},
		}); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ch := &Channel{
		gw: &recordingGateway{reply: "ok"},
		cfg: channelCfg{
			AIBotID: "bot1",
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		chatPolicy:        chatPolicyOpen,
		connectionMode:    connectionModeWebSocket,
		wsURL:             wsURL,
		wsSecret:          "secret1",
		heartbeatInterval: time.Hour,
		lanes:             newLaneLocker(),
		inflight:          newInflightRequests(),
		sessionTracker:    newSessionTracker(),
		aggregator:        newMessageAggregator(0),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)

	require.Eventually(t, func() bool {
		return ch.webSocketPushWriter() != nil
	}, time.Second, 10*time.Millisecond)

	require.NoError(
		t,
		ch.SendText(
			context.Background(),
			"group:chat1",
			"hello",
		),
	)

	pushFrame := <-received
	require.Equal(t, wsCommandSend, pushFrame.Command)
	body, ok := pushFrame.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "chat1", body["chatid"])
	require.Equal(t, msgTypeMarkdown, body["msgtype"])
	_, hasChatType := body["chat_type"]
	require.False(t, hasChatType)

	require.NoError(t, <-serverErr)

	cancel()
	requireWebSocketSessionExit(t, <-errCh)
}

func TestRunWebSocketSessionHandlesPersonasCommand(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 2)
	replied := make(chan wsOutboundFrame, 1)
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
			MsgID:   "msg-personas-1",
			ChatID:  "chat1",
			From:    FromInfo{UserID: "user1"},
			MsgType: MsgTypeText,
			Text: TextContent{
				Content: personaKeyword,
			},
		})
		if err != nil {
			serverErr <- err
			return
		}

		if err := conn.WriteJSON(wsInboundFrame{
			Command: wsCommandMsgCallback,
			Headers: wsFrameHeaders{ReqID: "req-personas-1"},
			Body:    body,
		}); err != nil {
			serverErr <- err
			return
		}

		frame = wsOutboundFrame{}
		if err := conn.ReadJSON(&frame); err != nil {
			serverErr <- err
			return
		}
		replied <- frame
		if err := conn.WriteJSON(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: "req-personas-1"},
		}); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ch := &Channel{
		gw: stubGateway{},
		cfg: channelCfg{
			AIBotID: "bot1",
			BotName: "Streambot2",
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		chatPolicy:        chatPolicyOpen,
		connectionMode:    connectionModeWebSocket,
		wsURL:             wsURL,
		wsSecret:          "secret1",
		heartbeatInterval: time.Hour,
		lanes:             newLaneLocker(),
		inflight:          newInflightRequests(),
		sessionTracker:    newSessionTracker(),
		aggregator:        newMessageAggregator(0),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)

	reply := <-replied
	require.Equal(t, wsCommandRespond, reply.Command)
	require.Equal(t, "req-personas-1", reply.Headers.ReqID)
	replyBody, ok := reply.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, msgTypeTemplateCard, replyBody["msgtype"])
	templateCard, ok := replyBody["template_card"].(map[string]any)
	require.True(t, ok)
	require.Equal(
		t,
		templateCardTypeButtonInteraction,
		templateCard["card_type"],
	)

	require.NoError(t, <-serverErr)

	cancel()
	requireWebSocketSessionExit(t, <-errCh)
}

func TestRunWebSocketSessionHandlesWelcomeCommand(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 2)
	replied := make(chan wsOutboundFrame, 1)
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
			MsgID:   "msg-welcome-1",
			ChatID:  "chat1",
			From:    FromInfo{UserID: "user1"},
			MsgType: MsgTypeText,
			Text: TextContent{
				Content: welcomeKeyword,
			},
		})
		if err != nil {
			serverErr <- err
			return
		}

		if err := conn.WriteJSON(wsInboundFrame{
			Command: wsCommandMsgCallback,
			Headers: wsFrameHeaders{ReqID: "req-welcome-1"},
			Body:    body,
		}); err != nil {
			serverErr <- err
			return
		}

		frame = wsOutboundFrame{}
		if err := conn.ReadJSON(&frame); err != nil {
			serverErr <- err
			return
		}
		replied <- frame
		if err := conn.WriteJSON(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: "req-welcome-1"},
		}); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ch := &Channel{
		gw: stubGateway{},
		cfg: channelCfg{
			AIBotID: "bot1",
			BotName: "Streambot2",
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		chatPolicy:        chatPolicyOpen,
		connectionMode:    connectionModeWebSocket,
		wsURL:             wsURL,
		wsSecret:          "secret1",
		heartbeatInterval: time.Hour,
		lanes:             newLaneLocker(),
		inflight:          newInflightRequests(),
		sessionTracker:    newSessionTracker(),
		aggregator:        newMessageAggregator(0),
		runtimeModelName:  "gpt-5.2",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)

	reply := <-replied
	require.Equal(t, wsCommandRespond, reply.Command)
	require.Equal(t, "req-welcome-1", reply.Headers.ReqID)
	replyBody, ok := reply.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, msgTypeTemplateCard, replyBody["msgtype"])
	templateCard, ok := replyBody["template_card"].(map[string]any)
	require.True(t, ok)
	mainTitle, ok := templateCard["main_title"].(map[string]any)
	require.True(t, ok)
	require.Equal(
		t,
		"Streambot2 · "+controlCardTitleHome,
		mainTitle["title"],
	)

	require.NoError(t, <-serverErr)

	cancel()
	requireWebSocketSessionExit(t, <-errCh)
}

func TestRunWebSocketSessionHandlesPersonaCardEvent(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 2)
	replied := make(chan wsOutboundFrame, 1)
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
			MsgID:   "evt-persona-1",
			ChatID:  "chat1",
			From:    FromInfo{UserID: "user1"},
			MsgType: MsgTypeEvent,
			Event: EventContent{
				EventType: eventTypeTemplateCard,
				TemplateCardEvent: &TemplateCardEvent{
					EventKey: personaCardEventApply,
					TaskID:   "persona-task-1",
					SelectedItems: TemplateCardSelectedItems{
						SelectedItem: []TemplateCardSelectedItem{
							{
								QuestionKey: personaCardQuestionKey,
								OptionIDs: TemplateCardOptionIDs{
									OptionID: []string{
										personaapi.ConciseID,
									},
								},
							},
						},
					},
				},
			},
		})
		if err != nil {
			serverErr <- err
			return
		}

		if err := conn.WriteJSON(wsInboundFrame{
			Command: wsCommandEventCallback,
			Headers: wsFrameHeaders{ReqID: "req-persona-evt-1"},
			Body:    body,
		}); err != nil {
			serverErr <- err
			return
		}

		frame = wsOutboundFrame{}
		if err := conn.ReadJSON(&frame); err != nil {
			serverErr <- err
			return
		}
		replied <- frame
		if err := conn.WriteJSON(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: "req-persona-evt-1"},
		}); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	tracker := newSessionTracker()
	ch := &Channel{
		gw: stubGateway{},
		cfg: channelCfg{
			AIBotID: "bot1",
			BotName: "Streambot2",
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		chatPolicy:        chatPolicyOpen,
		connectionMode:    connectionModeWebSocket,
		wsURL:             wsURL,
		wsSecret:          "secret1",
		heartbeatInterval: time.Hour,
		lanes:             newLaneLocker(),
		inflight:          newInflightRequests(),
		sessionTracker:    tracker,
		aggregator:        newMessageAggregator(0),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)

	reply := <-replied
	require.Equal(t, wsCommandRespondUpdate, reply.Command)
	require.Equal(t, "req-persona-evt-1", reply.Headers.ReqID)
	replyBody, ok := reply.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(
		t,
		templateCardUpdateResponseType,
		replyBody["response_type"],
	)
	templateCard, ok := replyBody["template_card"].(map[string]any)
	require.True(t, ok)
	require.Equal(
		t,
		templateCardTypeButtonInteraction,
		templateCard["card_type"],
	)

	current := tracker.getOrCreateSession(
		buildSessionID("chat1", "user1"),
		0,
	)
	require.Equal(t, personaapi.ConciseID, current.personaID)
	require.True(t, current.personaPinned)

	require.NoError(t, <-serverErr)

	cancel()
	requireWebSocketSessionExit(t, <-errCh)
}

func TestRunWebSocketSessionHandlesStreamReply(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 3)
	replied := make(chan wsOutboundFrame, 2)
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
			MsgID:   "msg2",
			ChatID:  "chat2",
			From:    FromInfo{UserID: "user2"},
			MsgType: MsgTypeText,
			Text: TextContent{
				Content: "stream please",
			},
		})
		if err != nil {
			serverErr <- err
			return
		}

		if err := conn.WriteJSON(wsInboundFrame{
			Command: wsCommandMsgCallback,
			Headers: wsFrameHeaders{ReqID: "req-ws-2"},
			Body:    body,
		}); err != nil {
			serverErr <- err
			return
		}

		for range 2 {
			frame = wsOutboundFrame{}
			if err := conn.ReadJSON(&frame); err != nil {
				serverErr <- err
				return
			}
			replied <- frame
			if err := conn.WriteJSON(wsInboundFrame{
				Headers: wsFrameHeaders{ReqID: "req-ws-2"},
			}); err != nil {
				serverErr <- err
				return
			}
		}
		serverErr <- nil
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ch := &Channel{
		gw: &fakeStreamGateway{
			events: []fakeStreamEvent{
				{Type: streamEventRunStarted},
				{Type: streamEventMsgDone, Reply: "hello"},
				{Type: streamEventRunDone},
			},
		},
		cfg: channelCfg{
			AIBotID:            "bot1",
			EnableStream:       true,
			ReplyPrefix:        replyPrefixCfg{Enabled: boolPtr(false)},
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		chatPolicy:        chatPolicyOpen,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
		wsURL:             wsURL,
		wsSecret:          "secret1",
		heartbeatInterval: time.Hour,
		lanes:             newLaneLocker(),
		inflight:          newInflightRequests(),
		sessionTracker:    newSessionTracker(),
		aggregator:        newMessageAggregator(0),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)
	require.NotEmpty(t, subscribe.Headers.ReqID)

	first := <-replied
	require.Equal(t, wsCommandRespond, first.Command)
	require.Equal(t, "req-ws-2", first.Headers.ReqID)
	firstBody, ok := first.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, MsgTypeStream, firstBody["msgtype"])
	firstStream, ok := firstBody["stream"].(map[string]any)
	require.True(t, ok)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		firstStream["content"],
	)
	require.Equal(t, false, firstStream["finish"])
	firstFeedback, ok := firstStream["feedback"].(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, firstFeedback["id"])

	second := <-replied
	require.Equal(t, wsCommandRespond, second.Command)
	require.Equal(t, "req-ws-2", second.Headers.ReqID)
	secondBody, ok := second.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, MsgTypeStream, secondBody["msgtype"])
	secondStream, ok := secondBody["stream"].(map[string]any)
	require.True(t, ok)
	require.Equal(
		t,
		nativeThinkingStreamContent(nil, "hello", true),
		secondStream["content"],
	)
	require.Equal(t, true, secondStream["finish"])
	require.NotContains(t, secondStream, "feedback")

	require.NoError(t, <-serverErr)

	cancel()
	requireWebSocketSessionExit(t, <-errCh)
}

func TestRunWebSocketSessionSendsPingReqID(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan wsOutboundFrame, 2)
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

		for range 2 {
			var frame wsOutboundFrame
			if err := conn.ReadJSON(&frame); err != nil {
				serverErr <- err
				return
			}
			received <- frame
		}
		serverErr <- nil
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ch := &Channel{
		gw:                &recordingGateway{reply: "ok"},
		cfg:               channelCfg{AIBotID: "bot1"},
		botMode:           botModeAI,
		chatPolicy:        chatPolicyOpen,
		connectionMode:    connectionModeWebSocket,
		wsURL:             wsURL,
		wsSecret:          "secret1",
		heartbeatInterval: 10 * time.Millisecond,
		lanes:             newLaneLocker(),
		inflight:          newInflightRequests(),
		sessionTracker:    newSessionTracker(),
		aggregator:        newMessageAggregator(0),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ch.runWebSocketSession(ctx)
	}()

	subscribe := <-received
	require.Equal(t, wsCommandSubscribe, subscribe.Command)
	require.NotEmpty(t, subscribe.Headers.ReqID)

	ping := <-received
	require.Equal(t, wsCommandPing, ping.Command)
	require.NotEmpty(t, ping.Headers.ReqID)

	require.NoError(t, <-serverErr)
	require.Error(t, <-errCh)
}

func TestWSFrameJSONUsesOfficialCmdField(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(wsOutboundFrame{
		Command: "aibot_subscribe",
		Headers: wsFrameHeaders{ReqID: "req-1"},
		Body: map[string]string{
			"bot_id": "bot-1",
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"cmd":"aibot_subscribe",
		"headers":{"req_id":"req-1"},
		"body":{"bot_id":"bot-1"}
	}`, string(data))
	require.NotContains(t, string(data), `"command"`)

	var frame wsInboundFrame
	err = json.Unmarshal([]byte(`{
		"cmd":"aibot_msg_callback",
		"headers":{"req_id":"req-1"},
		"body":{"msgid":"msg1","msgtype":"text","text":{"content":"hi"}}
	}`), &frame)
	require.NoError(t, err)
	require.Equal(t, wsCommandMsgCallback, frame.Command)
	require.Equal(t, "req-1", frame.Headers.ReqID)
}

func TestHandleWebSocketFrameDeliversReplyAck(t *testing.T) {
	t.Parallel()

	session := &webSocketSession{
		pendingAcks: make(map[string][]chan wsInboundFrame),
	}
	ackCh := session.registerReplyAck("req-ack-1")

	ch := &Channel{}
	err := ch.handleWebSocketFrame(
		context.Background(),
		session,
		[]byte(`{
			"headers":{"req_id":"req-ack-1"},
			"errcode":6000,
			"errmsg":"version conflict"
		}`),
	)
	require.NoError(t, err)

	select {
	case ack := <-ackCh:
		require.Equal(t, 6000, ack.ErrCode)
		require.Equal(t, "version conflict", ack.ErrMsg)
	case <-time.After(time.Second):
		t.Fatal("expected websocket ack to be delivered")
	}
}

func TestWaitReplyAckReturnsError(t *testing.T) {
	t.Parallel()

	ackCh := make(chan wsInboundFrame, 1)
	ackCh <- wsInboundFrame{
		ErrCode: 6000,
		ErrMsg:  "version conflict",
	}

	err := waitReplyAck(context.Background(), ackCh)
	require.ErrorContains(
		t,
		err,
		"respond ack errcode=6000",
	)
}

func TestWebSocketSessionSerializesSameReplyReqID(
	t *testing.T,
) {
	t.Parallel()

	session := &webSocketSession{
		replyInflight: make(map[string]chan struct{}),
	}
	release, err := session.acquireReplySendSlot(
		context.Background(),
		"req-serial-1",
	)
	require.NoError(t, err)

	secondDone := make(chan error, 1)
	go func() {
		nextRelease, err := session.acquireReplySendSlot(
			context.Background(),
			"req-serial-1",
		)
		if err == nil {
			nextRelease()
		}
		secondDone <- err
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second slot acquired too early: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	release()
	require.NoError(t, <-secondDone)
}

func TestReplyAckTimeoutUsesProgressDeadline(t *testing.T) {
	t.Parallel()

	progressFrame := wsOutboundFrame{
		Command: wsCommandRespond,
		Headers: wsFrameHeaders{ReqID: "req-1"},
		Body: wsReplyBody{
			MsgType: MsgTypeStream,
			Stream: &aibotStream{
				ID:      "stream-1",
				Content: statusPulseOne,
				Finish:  false,
			},
		},
	}
	finalFrame := wsOutboundFrame{
		Command: wsCommandRespond,
		Headers: wsFrameHeaders{ReqID: "req-1"},
		Body: wsReplyBody{
			MsgType: MsgTypeStream,
			Stream: &aibotStream{
				ID:      "stream-1",
				Content: "done",
				Finish:  true,
			},
		},
	}

	require.Equal(
		t,
		progressReplyAckTimeout,
		replyAckTimeout(progressFrame),
	)
	require.Equal(
		t,
		defaultWebSocketTimeout,
		replyAckTimeout(finalFrame),
	)
}

func TestShouldLogStreamReply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stream *aibotStream
		want   bool
	}{
		{
			name:   "nil stream",
			stream: nil,
			want:   false,
		},
		{
			name: "pulse one",
			stream: &aibotStream{
				Content: statusPulseOne,
			},
			want: false,
		},
		{
			name: "pulse three with spaces",
			stream: &aibotStream{
				Content: " " + statusPulseThree + " ",
			},
			want: false,
		},
		{
			name: "compat pulse",
			stream: &aibotStream{
				Content: statusPulseCompatTwo,
			},
			want: false,
		},
		{
			name: "unicode pulse",
			stream: &aibotStream{
				Content: statusPulseCN,
			},
			want: false,
		},
		{
			name: "non-empty comment",
			stream: &aibotStream{
				Content: "Inspecting repository layout",
			},
			want: true,
		},
		{
			name: "comment with dots suffix",
			stream: &aibotStream{
				Content: "Inspecting repository layout...",
			},
			want: true,
		},
		{
			name: "finish pulse still logs",
			stream: &aibotStream{
				Content: statusPulseThree,
				Finish:  true,
			},
			want: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(
				t,
				tc.want,
				shouldLogStreamReply(tc.stream),
			)
		})
	}
}

func requireWebSocketSessionExit(t *testing.T, err error) {
	t.Helper()

	if errors.Is(err, context.Canceled) {
		return
	}
	require.ErrorContains(t, err, "unexpected EOF")
}
