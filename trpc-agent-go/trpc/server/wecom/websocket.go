package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

const (
	wsCommandSubscribe = "aibot_subscribe"
	wsCommandPing      = "ping"
	wsCommandRespond   = "aibot_respond_msg"

	wsCommandMessageCallback = "aibot_msg_callback"
	wsCommandEventCallback   = "aibot_event_callback"

	msgTypeMarkdown = "markdown"

	defaultWebSocketTimeout = 10 * time.Second

	wsReqIDPrefix    = "wecom"
	wsReqIDSubscribe = "subscribe"
	wsReqIDPing      = "ping"
)

var wsReqIDCounter atomic.Uint64

type websocketDialer interface {
	DialContext(
		ctx context.Context,
		urlStr string,
		reqHeader http.Header,
	) (*websocket.Conn, *http.Response, error)
}

type wsFrameHeaders struct {
	ReqID string `json:"req_id,omitempty"`
}

type wsReplyBody struct {
	MsgType  string         `json:"msgtype,omitempty"`
	Markdown *aibotMarkdown `json:"markdown,omitempty"`
	Stream   *aibotStream   `json:"stream,omitempty"`
}

type wsSubscribeBody struct {
	BotID  string `json:"bot_id"`
	Secret string `json:"secret"`
}

type wsOutboundFrame struct {
	Command string         `json:"cmd,omitempty"`
	Headers wsFrameHeaders `json:"headers,omitempty"`
	Body    any            `json:"body,omitempty"`
}

type wsInboundFrame struct {
	Command string          `json:"cmd,omitempty"`
	Headers wsFrameHeaders  `json:"headers,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
	ErrCode int             `json:"errcode,omitempty"`
	ErrMsg  string          `json:"errmsg,omitempty"`
}

type webSocketSession struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *webSocketSession) send(
	ctx context.Context,
	frame wsOutboundFrame,
) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("wecom: marshal websocket frame: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.conn.SetWriteDeadline(
		time.Now().Add(defaultWebSocketTimeout),
	); err != nil {
		return fmt.Errorf("wecom: set websocket deadline: %w", err)
	}
	if err := s.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("wecom: write websocket frame: %w", err)
	}
	logWebSocketReply(ctx, frame)
	return nil
}

// Run connects to a WeCom AI bot websocket and serves messages until
// the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	for {
		err := s.runWebSocketSession(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		log.WarnfContext(
			ctx,
			"wecom: websocket session failed: %v",
			err,
		)

		timer := time.NewTimer(s.cfg.ReconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Server) runWebSocketSession(ctx context.Context) error {
	dialer := s.wsDialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}

	conn, _, err := dialer.DialContext(ctx, s.cfg.WebSocketURL, nil)
	if err != nil {
		return fmt.Errorf("wecom: dial websocket: %w", err)
	}
	defer conn.Close()

	session := &webSocketSession{conn: conn}
	if err := session.send(ctx, wsOutboundFrame{
		Command: wsCommandSubscribe,
		Headers: wsFrameHeaders{
			ReqID: nextWSReqID(wsReqIDSubscribe),
		},
		Body: wsSubscribeBody{
			BotID:  s.cfg.BotID,
			Secret: s.cfg.Secret,
		},
	}); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.readWebSocketFrames(ctx, session)
	}()

	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-ticker.C:
			if err := session.send(ctx, wsOutboundFrame{
				Command: wsCommandPing,
				Headers: wsFrameHeaders{
					ReqID: nextWSReqID(wsReqIDPing),
				},
			}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) readWebSocketFrames(
	ctx context.Context,
	session *webSocketSession,
) error {
	for {
		_, data, err := session.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("wecom: read websocket frame: %w", err)
		}
		if err := s.handleWebSocketFrame(ctx, session, data); err != nil {
			log.WarnfContext(
				ctx,
				"wecom: handle websocket frame failed: %v",
				err,
			)
		}
	}
}

func (s *Server) handleWebSocketFrame(
	ctx context.Context,
	session *webSocketSession,
	data []byte,
) error {
	var frame wsInboundFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return fmt.Errorf("wecom: unmarshal websocket frame: %w", err)
	}

	if frame.ErrCode != 0 {
		log.WarnfContext(
			ctx,
			"wecom: frame command=%s req_id=%s errcode=%d "+
				"errmsg=%s",
			frame.Command,
			frame.Headers.ReqID,
			frame.ErrCode,
			frame.ErrMsg,
		)
	}

	switch frame.Command {
	case wsCommandMessageCallback, wsCommandEventCallback:
		var msg WebhookMessage
		if err := json.Unmarshal(frame.Body, &msg); err != nil {
			return fmt.Errorf("wecom: unmarshal callback body: %w", err)
		}
		if frame.Command == wsCommandEventCallback &&
			msg.MsgType == "" {
			msg.MsgType = messageTypeEvent
		}
		msg.CallbackReqID = frame.Headers.ReqID
		msg.ReplyWriter = session

		go func() {
			if err := s.handleIncomingMessage(ctx, msg); err != nil {
				log.WarnfContext(
					ctx,
					"wecom: handle message failed: %v",
					err,
				)
			}
		}()
	}

	return nil
}

func nextWSReqID(kind string) string {
	value := wsReqIDCounter.Add(1)
	return fmt.Sprintf("%s-%s-%d", wsReqIDPrefix, kind, value)
}

func logWebSocketReply(
	ctx context.Context,
	frame wsOutboundFrame,
) {
	body, ok := frame.Body.(wsReplyBody)
	if !ok {
		return
	}

	if body.Markdown != nil {
		log.InfofContext(
			ctx,
			"wecom: send markdown req_id=%s len=%d",
			frame.Headers.ReqID,
			len([]rune(body.Markdown.Content)),
		)
		return
	}
	if body.Stream != nil {
		log.InfofContext(
			ctx,
			"wecom: send stream req_id=%s stream_id=%s "+
				"finish=%t len=%d",
			frame.Headers.ReqID,
			body.Stream.ID,
			body.Stream.Finish,
			len([]rune(body.Stream.Content)),
		)
	}
}
