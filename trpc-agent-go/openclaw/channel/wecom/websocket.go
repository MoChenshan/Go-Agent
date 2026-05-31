package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

const (
	wsCommandSubscribe         = "aibot_subscribe"
	wsCommandPing              = "ping"
	wsCommandRespond           = "aibot_respond_msg"
	wsCommandSend              = "aibot_send_msg"
	wsCommandRespondWelcome    = "aibot_respond_welcome_msg"
	wsCommandRespondUpdate     = "aibot_respond_update_msg"
	wsCommandUploadMediaInit   = "aibot_upload_media_init"
	wsCommandUploadMediaChunk  = "aibot_upload_media_chunk"
	wsCommandUploadMediaFinish = "aibot_upload_media_finish"

	wsCommandMsgCallback   = "aibot_msg_callback"
	wsCommandEventCallback = "aibot_event_callback"

	defaultWebSocketURL = "wss://openws.work.weixin.qq.com"

	defaultHeartbeatInterval = 30 * time.Second
	defaultReconnectDelay    = 3 * time.Second
	defaultWebSocketTimeout  = 10 * time.Second
	progressReplyAckTimeout  = 2 * time.Second

	wsReqIDPrefix       = "openclaw"
	wsReqIDSubscribe    = "subscribe"
	wsReqIDPing         = "ping"
	wsReqIDSend         = "send"
	wsReqIDUploadInit   = "upload-init"
	wsReqIDUploadChunk  = "upload-chunk"
	wsReqIDUploadFinish = "upload-finish"

	maxReplyMediaChunks = 100

	templateCardUpdateResponseType = "update_template_card"

	replyAckConflictErrCode = 6000
)

var wsReqIDCounter atomic.Uint64

var errReplyAckTimeout = errors.New(
	"wecom websocket: wait reply ack timeout",
)

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
	MsgType      string         `json:"msgtype,omitempty"`
	Markdown     *aibotMarkdown `json:"markdown,omitempty"`
	Stream       *aibotStream   `json:"stream,omitempty"`
	TemplateCard *templateCard  `json:"template_card,omitempty"`
	File         *aibotMediaRef `json:"file,omitempty"`
	Image        *aibotMediaRef `json:"image,omitempty"`
	Voice        *aibotMediaRef `json:"voice,omitempty"`
	Video        *aibotVideoRef `json:"video,omitempty"`
}

type wsTemplateCardUpdateBody struct {
	ResponseType string        `json:"response_type,omitempty"`
	TemplateCard *templateCard `json:"template_card,omitempty"`
}

type wsSendBody struct {
	ChatID       string         `json:"chatid,omitempty"`
	MsgType      string         `json:"msgtype,omitempty"`
	Markdown     *aibotMarkdown `json:"markdown,omitempty"`
	TemplateCard *templateCard  `json:"template_card,omitempty"`
	File         *aibotMediaRef `json:"file,omitempty"`
	Image        *aibotMediaRef `json:"image,omitempty"`
	Voice        *aibotMediaRef `json:"voice,omitempty"`
	Video        *aibotVideoRef `json:"video,omitempty"`
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

	ackMu       sync.Mutex
	pendingAcks map[string][]chan wsInboundFrame

	replyMu       sync.Mutex
	replyInflight map[string]chan struct{}
}

type replyAckError struct {
	code int
	msg  string
}

func (e *replyAckError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf(
		"wecom websocket: respond ack errcode=%d errmsg=%s",
		e.code,
		strings.TrimSpace(e.msg),
	)
}

func (s *webSocketSession) send(
	ctx context.Context,
	frame wsOutboundFrame,
) error {
	_, err := s.sendFrame(
		ctx,
		frame,
		shouldWaitReplyAck(
			frame,
			strings.TrimSpace(frame.Headers.ReqID),
		),
	)
	return err
}

func (s *webSocketSession) request(
	ctx context.Context,
	frame wsOutboundFrame,
) (wsInboundFrame, error) {
	reqID := strings.TrimSpace(frame.Headers.ReqID)
	if reqID == "" {
		return wsInboundFrame{}, errors.New(
			"wecom websocket: missing req_id",
		)
	}
	return s.sendFrame(ctx, frame, true)
}

func (s *webSocketSession) sendFrame(
	ctx context.Context,
	frame wsOutboundFrame,
	waitAck bool,
) (wsInboundFrame, error) {
	if s == nil || s.conn == nil {
		return wsInboundFrame{}, errors.New(
			"wecom websocket: nil session",
		)
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return wsInboundFrame{}, fmt.Errorf(
			"wecom websocket: marshal frame: %w",
			err,
		)
	}

	var ackCh chan wsInboundFrame
	var release func()
	if waitAck {
		reqID := strings.TrimSpace(frame.Headers.ReqID)
		if reqID == "" {
			return wsInboundFrame{}, errors.New(
				"wecom websocket: missing req_id",
			)
		}
		release, err = s.acquireReplySendSlot(ctx, reqID)
		if err != nil {
			return wsInboundFrame{}, err
		}
		defer release()
		ackCh = s.registerReplyAck(reqID)
		defer s.unregisterReplyAck(reqID, ackCh)
	}

	s.mu.Lock()
	if err := s.conn.SetWriteDeadline(
		time.Now().Add(defaultWebSocketTimeout),
	); err != nil {
		s.mu.Unlock()
		return wsInboundFrame{}, fmt.Errorf(
			"wecom websocket: set write deadline: %w",
			err,
		)
	}
	if err := s.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		s.mu.Unlock()
		return wsInboundFrame{}, fmt.Errorf(
			"wecom websocket: write frame: %w",
			err,
		)
	}
	s.mu.Unlock()

	logWebSocketOutbound(ctx, frame)
	if !waitAck {
		return wsInboundFrame{}, nil
	}
	return waitReplyAckFrame(
		ctx,
		ackCh,
		replyAckTimeout(frame),
	)
}

func (s *webSocketSession) acquireReplySendSlot(
	ctx context.Context,
	reqID string,
) (func(), error) {
	reqID = strings.TrimSpace(reqID)
	if reqID == "" {
		return func() {}, nil
	}

	for {
		s.replyMu.Lock()
		if s.replyInflight == nil {
			s.replyInflight = make(map[string]chan struct{})
		}
		waitCh := s.replyInflight[reqID]
		if waitCh == nil {
			waitCh = make(chan struct{})
			s.replyInflight[reqID] = waitCh
			s.replyMu.Unlock()
			return func() {
				s.releaseReplySendSlot(reqID, waitCh)
			}, nil
		}
		s.replyMu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-waitCh:
		}
	}
}

func (s *webSocketSession) releaseReplySendSlot(
	reqID string,
	waitCh chan struct{},
) {
	if s == nil || waitCh == nil {
		return
	}

	s.replyMu.Lock()
	defer s.replyMu.Unlock()

	current := s.replyInflight[reqID]
	if current != waitCh {
		return
	}
	delete(s.replyInflight, reqID)
	close(waitCh)
}

func (c *Channel) runWebSocket(ctx context.Context) error {
	reconnectDelay := c.reconnectDelay
	if reconnectDelay <= 0 {
		reconnectDelay = defaultReconnectDelay
	}

	lock, err := acquireProcessLock(c.wsInstanceLockPath)
	if err != nil {
		return fmt.Errorf(
			"wecom websocket: aibot %s is already active "+
				"in another process: %w",
			strings.TrimSpace(c.cfg.AIBotID),
			err,
		)
	}
	defer func() {
		if lock == nil {
			return
		}
		if closeErr := lock.Close(); closeErr != nil {
			log.WarnfContext(
				ctx,
				"wecom websocket: release instance lock: %v",
				closeErr,
			)
		}
	}()

	for {
		err := c.runWebSocketSession(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		log.WarnfContext(
			ctx,
			"wecom websocket: session failed: %v",
			err,
		)

		timer := time.NewTimer(reconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Channel) runWebSocketSession(ctx context.Context) error {
	dialer := c.wsDialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}

	conn, _, err := dialer.DialContext(ctx, c.wsURL, nil)
	if err != nil {
		return fmt.Errorf("wecom websocket: dial: %w", err)
	}
	defer conn.Close()

	session := &webSocketSession{
		conn:        conn,
		pendingAcks: make(map[string][]chan wsInboundFrame),
	}
	if err := session.send(ctx, wsOutboundFrame{
		Command: wsCommandSubscribe,
		Headers: wsFrameHeaders{
			ReqID: nextWSReqID(wsReqIDSubscribe),
		},
		Body: wsSubscribeBody{
			BotID:  c.cfg.AIBotID,
			Secret: c.wsSecret,
		},
	}); err != nil {
		return err
	}
	c.setWebSocketPushWriter(session)
	defer c.clearWebSocketPushWriter(session)

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.readWebSocketFrames(ctx, session)
	}()
	go c.flushPendingRuntimeCompletionNotices(ctx)

	interval := c.heartbeatInterval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
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

func (c *Channel) readWebSocketFrames(
	ctx context.Context,
	session *webSocketSession,
) error {
	for {
		_, data, err := session.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf(
				"wecom websocket: read frame: %w",
				err,
			)
		}
		if err := c.handleWebSocketFrame(ctx, session, data); err != nil {
			log.WarnfContext(
				ctx,
				"wecom websocket: handle frame failed: %v",
				err,
			)
		}
	}
}

func (c *Channel) handleWebSocketFrame(
	ctx context.Context,
	session *webSocketSession,
	data []byte,
) error {
	var frame wsInboundFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return fmt.Errorf("wecom websocket: unmarshal frame: %w", err)
	}

	if session != nil && session.deliverReplyAck(frame) {
		return nil
	}

	if frame.ErrCode != 0 {
		log.WarnfContext(
			ctx,
			"wecom websocket: command=%s req_id=%s errcode=%d "+
				"errmsg=%s",
			frame.Command,
			frame.Headers.ReqID,
			frame.ErrCode,
			frame.ErrMsg,
		)
	}

	switch frame.Command {
	case wsCommandMsgCallback, wsCommandEventCallback:
		var msg WebhookMessage
		if err := json.Unmarshal(frame.Body, &msg); err != nil {
			return fmt.Errorf(
				"wecom websocket: unmarshal callback: %w",
				err,
			)
		}
		if frame.Command == wsCommandEventCallback &&
			msg.MsgType == "" {
			msg.MsgType = MsgTypeEvent
		}
		msg.CallbackReqID = frame.Headers.ReqID
		msg.ReplyWriter = session
		msg.RawBody = append(
			json.RawMessage(nil),
			frame.Body...,
		)

		go func() {
			if err := c.processIncomingMessage(ctx, msg); err != nil {
				log.WarnfContext(
					ctx,
					"wecom websocket: process callback failed: %v",
					err,
				)
			}
		}()
		return nil
	default:
		return nil
	}
}

func shouldWaitReplyAck(
	frame wsOutboundFrame,
	reqID string,
) bool {
	switch frame.Command {
	case wsCommandRespond,
		wsCommandRespondWelcome,
		wsCommandRespondUpdate:
		return reqID != ""
	default:
		return false
	}
}

func replyAckTimeout(frame wsOutboundFrame) time.Duration {
	if isProgressStreamFrame(frame) {
		return progressReplyAckTimeout
	}
	return defaultWebSocketTimeout
}

func isProgressStreamFrame(frame wsOutboundFrame) bool {
	body, ok := frame.Body.(wsReplyBody)
	if !ok {
		bodyPtr, okPtr := frame.Body.(*wsReplyBody)
		if !okPtr || bodyPtr == nil {
			return false
		}
		body = *bodyPtr
	}
	return body.MsgType == MsgTypeStream &&
		body.Stream != nil &&
		!body.Stream.Finish
}

func (s *webSocketSession) registerReplyAck(
	reqID string,
) chan wsInboundFrame {
	ch := make(chan wsInboundFrame, 1)

	s.ackMu.Lock()
	defer s.ackMu.Unlock()

	s.pendingAcks[reqID] = append(s.pendingAcks[reqID], ch)
	return ch
}

func (s *webSocketSession) unregisterReplyAck(
	reqID string,
	target chan wsInboundFrame,
) {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()

	queue := s.pendingAcks[reqID]
	if len(queue) == 0 {
		return
	}

	next := queue[:0]
	for _, ch := range queue {
		if ch == target {
			continue
		}
		next = append(next, ch)
	}
	if len(next) == 0 {
		delete(s.pendingAcks, reqID)
		return
	}
	s.pendingAcks[reqID] = next
}

func (s *webSocketSession) deliverReplyAck(
	frame wsInboundFrame,
) bool {
	if s == nil {
		return false
	}

	reqID := strings.TrimSpace(frame.Headers.ReqID)
	if reqID == "" {
		return false
	}

	s.ackMu.Lock()
	queue := s.pendingAcks[reqID]
	if len(queue) == 0 {
		s.ackMu.Unlock()
		return false
	}
	ch := queue[0]
	if len(queue) == 1 {
		delete(s.pendingAcks, reqID)
	} else {
		s.pendingAcks[reqID] = queue[1:]
	}
	s.ackMu.Unlock()

	ch <- frame
	return true
}

func waitReplyAck(
	ctx context.Context,
	ackCh <-chan wsInboundFrame,
) error {
	_, err := waitReplyAckFrame(
		ctx,
		ackCh,
		defaultWebSocketTimeout,
	)
	return err
}

func waitReplyAckFrame(
	ctx context.Context,
	ackCh <-chan wsInboundFrame,
	timeout time.Duration,
) (wsInboundFrame, error) {
	if timeout <= 0 {
		timeout = defaultWebSocketTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return wsInboundFrame{}, ctx.Err()
	case <-timer.C:
		return wsInboundFrame{}, errReplyAckTimeout
	case frame := <-ackCh:
		if frame.ErrCode == 0 {
			return frame, nil
		}
		return wsInboundFrame{}, &replyAckError{
			code: frame.ErrCode,
			msg:  strings.TrimSpace(frame.ErrMsg),
		}
	}
}

func nextWSReqID(kind string) string {
	id := wsReqIDCounter.Add(1)
	return fmt.Sprintf("%s-%s-%d", wsReqIDPrefix, kind, id)
}

func logWebSocketOutbound(
	ctx context.Context,
	frame wsOutboundFrame,
) {
	switch body := frame.Body.(type) {
	case wsSendBody:
		logWebSocketSendBody(ctx, frame.Headers.ReqID, body)
	case *wsSendBody:
		if body != nil {
			logWebSocketSendBody(ctx, frame.Headers.ReqID, *body)
		}
	case wsReplyBody:
		logWebSocketReplyBody(ctx, frame.Headers.ReqID, body)
	case *wsReplyBody:
		if body != nil {
			logWebSocketReplyBody(ctx, frame.Headers.ReqID, *body)
		}
	case wsTemplateCardUpdateBody:
		logWebSocketCardUpdateBody(
			ctx,
			frame.Headers.ReqID,
			body,
		)
	case *wsTemplateCardUpdateBody:
		if body != nil {
			logWebSocketCardUpdateBody(
				ctx,
				frame.Headers.ReqID,
				*body,
			)
		}
	case wsUploadMediaInitBody:
		log.InfofContext(
			ctx,
			"wecom websocket: upload init req_id=%s type=%s "+
				"filename=%q bytes=%d chunks=%d",
			frame.Headers.ReqID,
			body.Type,
			body.Filename,
			body.TotalSize,
			body.TotalChunks,
		)
	case wsUploadMediaChunkBody:
		log.InfofContext(
			ctx,
			"wecom websocket: upload chunk req_id=%s "+
				"upload_id=%s index=%d",
			frame.Headers.ReqID,
			body.UploadID,
			body.ChunkIndex,
		)
	case wsUploadMediaFinishBody:
		log.InfofContext(
			ctx,
			"wecom websocket: upload finish req_id=%s "+
				"upload_id=%s",
			frame.Headers.ReqID,
			body.UploadID,
		)
	}
}

func (c *Channel) setWebSocketPushWriter(writer wsRequestWriter) {
	c.wsPushWriterMu.Lock()
	defer c.wsPushWriterMu.Unlock()
	c.wsPushWriter = writer
}

func (c *Channel) clearWebSocketPushWriter(writer wsRequestWriter) {
	c.wsPushWriterMu.Lock()
	defer c.wsPushWriterMu.Unlock()
	if c.wsPushWriter == writer {
		c.wsPushWriter = nil
	}
}

func (c *Channel) webSocketPushWriter() wsRequestWriter {
	c.wsPushWriterMu.RLock()
	defer c.wsPushWriterMu.RUnlock()
	return c.wsPushWriter
}

func logWebSocketReplyBody(
	ctx context.Context,
	reqID string,
	body wsReplyBody,
) {
	switch body.MsgType {
	case msgTypeMarkdown:
		content := ""
		if body.Markdown != nil {
			content = body.Markdown.Content
		}
		log.InfofContext(
			ctx,
			"wecom websocket: send req_id=%s msgtype=%s "+
				"content_len=%d",
			reqID,
			body.MsgType,
			len([]rune(content)),
		)
	case MsgTypeStream:
		if body.Stream == nil {
			return
		}
		if !shouldLogStreamReply(body.Stream) {
			return
		}
		log.InfofContext(
			ctx,
			"wecom websocket: send req_id=%s msgtype=%s "+
				"stream_id=%s finish=%t content_len=%d",
			reqID,
			body.MsgType,
			body.Stream.ID,
			body.Stream.Finish,
			len([]rune(body.Stream.Content)),
		)
	case msgTypeTemplateCard:
		cardType := ""
		taskID := ""
		if body.TemplateCard != nil {
			cardType = body.TemplateCard.CardType
			taskID = body.TemplateCard.TaskID
		}
		log.InfofContext(
			ctx,
			"wecom websocket: send req_id=%s msgtype=%s "+
				"card_type=%s task_id=%s",
			reqID,
			body.MsgType,
			cardType,
			taskID,
		)
	case MsgTypeFile:
		if body.File == nil {
			return
		}
		log.InfofContext(
			ctx,
			"wecom websocket: send req_id=%s msgtype=%s "+
				"media_id=%s",
			reqID,
			body.MsgType,
			body.File.MediaID,
		)
	case MsgTypeImage:
		if body.Image == nil {
			return
		}
		log.InfofContext(
			ctx,
			"wecom websocket: send req_id=%s msgtype=%s "+
				"media_id=%s",
			reqID,
			body.MsgType,
			body.Image.MediaID,
		)
	case MsgTypeVoice:
		if body.Voice == nil {
			return
		}
		log.InfofContext(
			ctx,
			"wecom websocket: send req_id=%s msgtype=%s "+
				"media_id=%s",
			reqID,
			body.MsgType,
			body.Voice.MediaID,
		)
	case MsgTypeVideo:
		if body.Video == nil {
			return
		}
		log.InfofContext(
			ctx,
			"wecom websocket: send req_id=%s msgtype=%s "+
				"media_id=%s",
			reqID,
			body.MsgType,
			body.Video.MediaID,
		)
	}
}

func logWebSocketSendBody(
	ctx context.Context,
	reqID string,
	body wsSendBody,
) {
	switch body.MsgType {
	case msgTypeMarkdown:
		content := ""
		if body.Markdown != nil {
			content = body.Markdown.Content
		}
		log.InfofContext(
			ctx,
			"wecom websocket: push req_id=%s chatid=%s "+
				"msgtype=%s content_len=%d",
			reqID,
			body.ChatID,
			body.MsgType,
			len([]rune(content)),
		)
	case msgTypeTemplateCard:
		cardType := ""
		if body.TemplateCard != nil {
			cardType = body.TemplateCard.CardType
		}
		log.InfofContext(
			ctx,
			"wecom websocket: push req_id=%s chatid=%s "+
				"msgtype=%s card_type=%s",
			reqID,
			body.ChatID,
			body.MsgType,
			cardType,
		)
	case MsgTypeFile, MsgTypeImage, MsgTypeVoice, MsgTypeVideo:
		logWebSocketPushMediaBody(ctx, reqID, body)
	}
}

func logWebSocketPushMediaBody(
	ctx context.Context,
	reqID string,
	body wsSendBody,
) {
	mediaID := webSocketPushMediaID(body)
	if mediaID == "" {
		return
	}
	log.InfofContext(
		ctx,
		"wecom websocket: push req_id=%s chatid=%s "+
			"msgtype=%s media_id=%s",
		reqID,
		body.ChatID,
		body.MsgType,
		mediaID,
	)
}

func webSocketPushMediaID(body wsSendBody) string {
	switch body.MsgType {
	case MsgTypeImage:
		if body.Image != nil {
			return body.Image.MediaID
		}
	case MsgTypeVoice:
		if body.Voice != nil {
			return body.Voice.MediaID
		}
	case MsgTypeVideo:
		if body.Video != nil {
			return body.Video.MediaID
		}
	default:
		if body.File != nil {
			return body.File.MediaID
		}
	}
	return ""
}

func shouldLogStreamReply(stream *aibotStream) bool {
	if stream == nil {
		return false
	}
	if stream.Finish {
		return true
	}
	return !isPulseOnlyStreamContent(stream.Content)
}

func isPulseOnlyStreamContent(content string) bool {
	content = strings.TrimSpace(content)
	switch content {
	case statusPulseOne,
		statusPulseTwo,
		statusPulseThree,
		statusPulseCN,
		statusPulseCompatOne,
		statusPulseCompatTwo,
		statusPulseCompatThree,
		streamNativeThinkingPlaceholder:
		return true
	default:
		return false
	}
}

func logWebSocketCardUpdateBody(
	ctx context.Context,
	reqID string,
	body wsTemplateCardUpdateBody,
) {
	cardType := ""
	taskID := ""
	if body.TemplateCard != nil {
		cardType = body.TemplateCard.CardType
		taskID = body.TemplateCard.TaskID
	}
	log.InfofContext(
		ctx,
		"wecom websocket: update card req_id=%s "+
			"response_type=%s card_type=%s task_id=%s",
		reqID,
		body.ResponseType,
		cardType,
		taskID,
	)
}
