package wecome2e_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
)

const (
	wsCommandSubscribe         = "aibot_subscribe"
	wsCommandRespond           = "aibot_respond_msg"
	wsCommandRespondWelcome    = "aibot_respond_welcome_msg"
	wsCommandRespondUpdate     = "aibot_respond_update_msg"
	wsCommandSend              = "aibot_send_msg"
	wsCommandUploadMediaInit   = "aibot_upload_media_init"
	wsCommandUploadMediaChunk  = "aibot_upload_media_chunk"
	wsCommandUploadMediaFinish = "aibot_upload_media_finish"
	wsCommandMsgCallback       = "aibot_msg_callback"
	msgTypeMarkdown            = "markdown"
	msgTypeStream              = "stream"
)

type wsFrameHeaders struct {
	ReqID string `json:"req_id,omitempty"`
}

type wsInboundFrame struct {
	Command string          `json:"cmd,omitempty"`
	Headers wsFrameHeaders  `json:"headers,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

type aibotMarkdown struct {
	Content string `json:"content,omitempty"`
}

type aibotStream struct {
	ID      string `json:"id,omitempty"`
	Finish  bool   `json:"finish"`
	Content string `json:"content,omitempty"`
}

type aibotMediaRef struct {
	MediaID string `json:"media_id,omitempty"`
}

type aibotVideoRef struct {
	MediaID     string `json:"media_id,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type wsReplyBody struct {
	MsgType  string         `json:"msgtype,omitempty"`
	Markdown *aibotMarkdown `json:"markdown,omitempty"`
	Stream   *aibotStream   `json:"stream,omitempty"`
	File     *aibotMediaRef `json:"file,omitempty"`
	Image    *aibotMediaRef `json:"image,omitempty"`
	Voice    *aibotMediaRef `json:"voice,omitempty"`
	Video    *aibotVideoRef `json:"video,omitempty"`
}

type wsSendBody struct {
	ChatID   string         `json:"chatid,omitempty"`
	MsgType  string         `json:"msgtype,omitempty"`
	Markdown *aibotMarkdown `json:"markdown,omitempty"`
}

type wsUploadMediaInitBody struct {
	Type        string `json:"type"`
	Filename    string `json:"filename"`
	TotalSize   int    `json:"total_size"`
	TotalChunks int    `json:"total_chunks"`
}

type wsUploadMediaInitAck struct {
	UploadID string `json:"upload_id,omitempty"`
}

type wsUploadMediaChunkBody struct {
	UploadID   string `json:"upload_id"`
	ChunkIndex int    `json:"chunk_index"`
	Base64Data string `json:"base64_data"`
}

type wsUploadMediaFinishBody struct {
	UploadID string `json:"upload_id"`
}

type wsUploadMediaFinishAck struct {
	Type    string `json:"type,omitempty"`
	MediaID string `json:"media_id,omitempty"`
}

type fakeWeComWebSocketServer struct {
	server           *http.Server
	listener         net.Listener
	containerWSURL   string
	mu               sync.Mutex
	conn             *websocket.Conn
	frames           []capturedWSFrame
	completedUploads []capturedUpload
	uploads          map[string]*uploadAccumulator
	backgroundErr    error
	writeMu          sync.Mutex
}

type capturedWSFrame struct {
	Command  string          `json:"cmd,omitempty"`
	Headers  wsFrameHeaders  `json:"headers,omitempty"`
	Body     json.RawMessage `json:"body,omitempty"`
	Received time.Time
}

type uploadAccumulator struct {
	msgType     string
	filename    string
	totalSize   int
	totalChunks int
	data        []byte
}

type capturedUpload struct {
	UploadID    string
	MediaID     string
	MsgType     string
	Filename    string
	TotalSize   int
	TotalChunks int
	Data        []byte
}

func newFakeWeComWebSocketServer(
	containerHost string,
) (*fakeWeComWebSocketServer, error) {
	server := &fakeWeComWebSocketServer{
		uploads: make(map[string]*uploadAccumulator),
	}
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			server.setBackgroundErr(err)
			return
		}
		server.setConn(conn)
		server.readLoop(conn)
	})}
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			server.setBackgroundErr(err)
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	server.server = httpServer
	server.listener = listener
	server.containerWSURL = fmt.Sprintf("ws://%s:%d", containerHost, port)
	return server, nil
}

func (s *fakeWeComWebSocketServer) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	conn := s.conn
	s.conn = nil
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	if s.server != nil {
		_ = s.server.Close()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

func (s *fakeWeComWebSocketServer) setConn(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = conn
}

func (s *fakeWeComWebSocketServer) currentConn() *websocket.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn
}

func (s *fakeWeComWebSocketServer) setBackgroundErr(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backgroundErr == nil {
		s.backgroundErr = err
	}
}

func (s *fakeWeComWebSocketServer) backgroundErrValue() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backgroundErr
}

func (s *fakeWeComWebSocketServer) requireHealthy(t *testing.T) {
	t.Helper()
	require.NoError(t, s.backgroundErrValue())
}

func (s *fakeWeComWebSocketServer) readLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var frame capturedWSFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			s.setBackgroundErr(err)
			return
		}
		frame.Received = time.Now()
		s.appendFrame(frame)
		s.maybeAckFrame(frame)
	}
}

func (s *fakeWeComWebSocketServer) appendFrame(frame capturedWSFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, frame)
}

func (s *fakeWeComWebSocketServer) maybeAckFrame(frame capturedWSFrame) {
	switch frame.Command {
	case wsCommandRespond,
		wsCommandRespondWelcome,
		wsCommandRespondUpdate,
		wsCommandSend:
		s.setBackgroundErr(s.writeInboundFrame(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: frame.Headers.ReqID},
			Body:    json.RawMessage(`{}`),
		}))
	case wsCommandUploadMediaInit:
		var body wsUploadMediaInitBody
		if err := json.Unmarshal(frame.Body, &body); err != nil {
			s.setBackgroundErr(err)
			return
		}
		uploadID := "upload-" + strings.TrimSpace(frame.Headers.ReqID)
		s.mu.Lock()
		s.uploads[uploadID] = &uploadAccumulator{
			msgType:     body.Type,
			filename:    body.Filename,
			totalSize:   body.TotalSize,
			totalChunks: body.TotalChunks,
		}
		s.mu.Unlock()
		ackBody, err := json.Marshal(wsUploadMediaInitAck{
			UploadID: uploadID,
		})
		if err != nil {
			s.setBackgroundErr(err)
			return
		}
		s.setBackgroundErr(s.writeInboundFrame(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: frame.Headers.ReqID},
			Body:    ackBody,
		}))
	case wsCommandUploadMediaChunk:
		var body wsUploadMediaChunkBody
		if err := json.Unmarshal(frame.Body, &body); err != nil {
			s.setBackgroundErr(err)
			return
		}
		chunk, err := base64.StdEncoding.DecodeString(body.Base64Data)
		if err != nil {
			s.setBackgroundErr(err)
			return
		}
		s.mu.Lock()
		acc := s.uploads[body.UploadID]
		if acc == nil {
			s.mu.Unlock()
			s.setBackgroundErr(fmt.Errorf("missing upload accumulator for %s", body.UploadID))
			return
		}
		acc.data = append(acc.data, chunk...)
		s.mu.Unlock()
		s.setBackgroundErr(s.writeInboundFrame(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: frame.Headers.ReqID},
			Body:    json.RawMessage(`{}`),
		}))
	case wsCommandUploadMediaFinish:
		var body wsUploadMediaFinishBody
		if err := json.Unmarshal(frame.Body, &body); err != nil {
			s.setBackgroundErr(err)
			return
		}
		s.mu.Lock()
		acc := s.uploads[body.UploadID]
		if acc == nil {
			s.mu.Unlock()
			s.setBackgroundErr(fmt.Errorf("missing upload accumulator for %s", body.UploadID))
			return
		}
		mediaID := fmt.Sprintf("media-%d", len(s.completedUploads)+1)
		upload := capturedUpload{
			UploadID:    body.UploadID,
			MediaID:     mediaID,
			MsgType:     acc.msgType,
			Filename:    acc.filename,
			TotalSize:   acc.totalSize,
			TotalChunks: acc.totalChunks,
			Data:        append([]byte(nil), acc.data...),
		}
		s.completedUploads = append(s.completedUploads, upload)
		delete(s.uploads, body.UploadID)
		s.mu.Unlock()
		ackBody, err := json.Marshal(wsUploadMediaFinishAck{
			Type:    upload.MsgType,
			MediaID: upload.MediaID,
		})
		if err != nil {
			s.setBackgroundErr(err)
			return
		}
		s.setBackgroundErr(s.writeInboundFrame(wsInboundFrame{
			Headers: wsFrameHeaders{ReqID: frame.Headers.ReqID},
			Body:    ackBody,
		}))
	}
}

func (s *fakeWeComWebSocketServer) writeInboundFrame(
	frame wsInboundFrame,
) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	conn := s.currentConn()
	if conn == nil {
		return fmt.Errorf("websocket connection is not ready")
	}
	return conn.WriteJSON(frame)
}

func (s *fakeWeComWebSocketServer) sendMessageCallback(
	t *testing.T,
	reqID string,
	msg wecomchannel.WebhookMessage,
) {
	t.Helper()
	body, err := json.Marshal(msg)
	require.NoError(t, err)
	require.NoError(t, s.writeInboundFrame(wsInboundFrame{
		Command: wsCommandMsgCallback,
		Headers: wsFrameHeaders{ReqID: reqID},
		Body:    body,
	}))
}

func (s *fakeWeComWebSocketServer) frameCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

func (s *fakeWeComWebSocketServer) uploadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.completedUploads)
}

func (s *fakeWeComWebSocketServer) waitForUploads(
	t *testing.T,
	start int,
	minCount int,
	timeout time.Duration,
) []capturedUpload {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		s.requireHealthy(t)
		s.mu.Lock()
		if len(s.completedUploads)-start >= minCount {
			out := append([]capturedUpload(nil), s.completedUploads[start:]...)
			s.mu.Unlock()
			return out
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting uploads after index %d", start)
		}
		time.Sleep(wecomE2EPollInterval)
	}
}

func (s *fakeWeComWebSocketServer) waitForFrame(
	t *testing.T,
	start int,
	timeout time.Duration,
	match func(capturedWSFrame) bool,
) capturedWSFrame {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		s.requireHealthy(t)
		s.mu.Lock()
		frames := append([]capturedWSFrame(nil), s.frames...)
		s.mu.Unlock()
		if start < 0 {
			start = 0
		}
		for i := start; i < len(frames); i++ {
			if match(frames[i]) {
				return frames[i]
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting websocket frame after index %d", start)
		}
		time.Sleep(wecomE2EPollInterval)
	}
}

func (s *fakeWeComWebSocketServer) waitForReplyTextContains(
	t *testing.T,
	start int,
	reqID string,
	want string,
	timeout time.Duration,
) string {
	t.Helper()
	frame := s.waitForFrame(
		t,
		start,
		timeout,
		func(frame capturedWSFrame) bool {
			if strings.TrimSpace(frame.Headers.ReqID) != reqID {
				return false
			}
			text, ok, final := frame.replyText()
			return ok && final && strings.Contains(text, want)
		},
	)
	text, ok, _ := frame.replyText()
	require.True(t, ok)
	return text
}

func (s *fakeWeComWebSocketServer) waitForFinalReplyText(
	t *testing.T,
	start int,
	reqID string,
	timeout time.Duration,
) string {
	t.Helper()
	frame := s.waitForFrame(
		t,
		start,
		timeout,
		func(frame capturedWSFrame) bool {
			if strings.TrimSpace(frame.Headers.ReqID) != reqID {
				return false
			}
			_, ok, final := frame.replyText()
			return ok && final
		},
	)
	text, ok, _ := frame.replyText()
	require.True(t, ok)
	return text
}

func (s *fakeWeComWebSocketServer) matchingFrames(
	start int,
	match func(capturedWSFrame) bool,
) []capturedWSFrame {
	s.mu.Lock()
	frames := append([]capturedWSFrame(nil), s.frames...)
	s.mu.Unlock()
	if start < 0 {
		start = 0
	}
	out := make([]capturedWSFrame, 0)
	for i := start; i < len(frames); i++ {
		if match(frames[i]) {
			out = append(out, frames[i])
		}
	}
	return out
}

func (s *fakeWeComWebSocketServer) waitForMatchingFrames(
	t *testing.T,
	start int,
	minCount int,
	timeout time.Duration,
	match func(capturedWSFrame) bool,
) []capturedWSFrame {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		s.requireHealthy(t)
		matches := s.matchingFrames(start, match)
		if len(matches) >= minCount {
			return matches
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"timeout waiting %d matching websocket frames after index %d",
				minCount,
				start,
			)
		}
		time.Sleep(wecomE2EPollInterval)
	}
}

func (s *fakeWeComWebSocketServer) requireMatchingFrameCountStable(
	t *testing.T,
	start int,
	expected int,
	duration time.Duration,
	match func(capturedWSFrame) bool,
) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for {
		s.requireHealthy(t)
		matches := s.matchingFrames(start, match)
		if len(matches) != expected {
			t.Fatalf(
				"expected %d matching websocket frames after index %d, got %d",
				expected,
				start,
				len(matches),
			)
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(wecomE2EPollInterval)
	}
}

func (f capturedWSFrame) replyText() (string, bool, bool) {
	switch f.Command {
	case wsCommandRespond, wsCommandRespondWelcome, wsCommandRespondUpdate:
		var body wsReplyBody
		if err := json.Unmarshal(f.Body, &body); err != nil {
			return "", false, false
		}
		switch body.MsgType {
		case msgTypeMarkdown:
			if body.Markdown == nil {
				return "", false, false
			}
			return body.Markdown.Content, true, true
		case msgTypeStream:
			if body.Stream == nil {
				return "", false, false
			}
			return body.Stream.Content, true, body.Stream.Finish
		default:
			return "", false, false
		}
	case wsCommandSend:
		var body wsSendBody
		if err := json.Unmarshal(f.Body, &body); err != nil {
			return "", false, false
		}
		if body.Markdown == nil {
			return "", false, false
		}
		return body.Markdown.Content, true, true
	default:
		return "", false, false
	}
}

func (f capturedWSFrame) replyMedia() (string, string, bool) {
	if f.Command != wsCommandRespond {
		return "", "", false
	}
	var body wsReplyBody
	if err := json.Unmarshal(f.Body, &body); err != nil {
		return "", "", false
	}
	switch body.MsgType {
	case wecomchannel.MsgTypeFile:
		if body.File == nil {
			return "", "", false
		}
		return wecomchannel.MsgTypeFile, body.File.MediaID, true
	case wecomchannel.MsgTypeImage:
		if body.Image == nil {
			return "", "", false
		}
		return wecomchannel.MsgTypeImage, body.Image.MediaID, true
	case wecomchannel.MsgTypeVoice:
		if body.Voice == nil {
			return "", "", false
		}
		return wecomchannel.MsgTypeVoice, body.Voice.MediaID, true
	case wecomchannel.MsgTypeVideo:
		if body.Video == nil {
			return "", "", false
		}
		return wecomchannel.MsgTypeVideo, body.Video.MediaID, true
	default:
		return "", "", false
	}
}
