package weixin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
)

const (
	testAccountID = "bot-1"
	testPeerID    = "user-1@im.wechat"
	testToken     = "test-token"
)

type stubGateway struct {
	mu       sync.Mutex
	reply    string
	requests []gwclient.MessageRequest
}

func (g *stubGateway) SendMessage(
	_ context.Context,
	req gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	g.mu.Lock()
	g.requests = append(g.requests, req)
	g.mu.Unlock()
	return gwclient.MessageResponse{Reply: g.reply}, nil
}

func (g *stubGateway) Cancel(
	_ context.Context,
	_ string,
) (bool, error) {
	return false, nil
}

func (g *stubGateway) lastRequest() gwclient.MessageRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.requests) == 0 {
		return gwclient.MessageRequest{}
	}
	return g.requests[len(g.requests)-1]
}

func (g *stubGateway) requestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.requests)
}

type fakeBackend struct {
	t *testing.T

	mu sync.Mutex

	updates      []getUpdatesResponse
	updateCalls  int
	sendBodies   []sendMessageRequest
	configBodies []getConfigRequest
	typingBodies []sendTypingRequest
}

func newFakeBackend(
	t *testing.T,
	updates []getUpdatesResponse,
) *fakeBackend {
	t.Helper()
	return &fakeBackend{t: t, updates: updates}
}

func (f *fakeBackend) handler(
	w http.ResponseWriter,
	r *http.Request,
) {
	f.t.Helper()

	switch r.URL.Path {
	case "/" + endpointGetUpdates:
		f.handleGetUpdates(w, r)
	case "/" + endpointSendMessage:
		f.handleSendMessage(w, r)
	case "/" + endpointGetConfig:
		f.handleGetConfig(w, r)
	case "/" + endpointSendTyping:
		f.handleSendTyping(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeBackend) handleGetUpdates(
	w http.ResponseWriter,
	r *http.Request,
) {
	var body map[string]any
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))

	f.mu.Lock()
	defer f.mu.Unlock()

	index := f.updateCalls
	f.updateCalls++
	if index >= len(f.updates) {
		writeJSONResponse(f.t, w, getUpdatesResponse{
			apiErrorResponse: apiErrorResponse{Ret: 0},
		})
		return
	}
	writeJSONResponse(f.t, w, f.updates[index])
}

func (f *fakeBackend) handleSendMessage(
	w http.ResponseWriter,
	r *http.Request,
) {
	var body sendMessageRequest
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))

	f.mu.Lock()
	f.sendBodies = append(f.sendBodies, body)
	f.mu.Unlock()

	writeJSONResponse(
		f.t,
		w,
		sendMessageResponse{
			apiErrorResponse: apiErrorResponse{Ret: 0},
		},
	)
}

func (f *fakeBackend) handleGetConfig(
	w http.ResponseWriter,
	r *http.Request,
) {
	var body getConfigRequest
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))

	f.mu.Lock()
	f.configBodies = append(f.configBodies, body)
	f.mu.Unlock()

	writeJSONResponse(
		f.t,
		w,
		getConfigResponse{
			apiErrorResponse: apiErrorResponse{Ret: 0},
			TypingTicket:     "typing-ticket",
		},
	)
}

func (f *fakeBackend) handleSendTyping(
	w http.ResponseWriter,
	r *http.Request,
) {
	var body sendTypingRequest
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))

	f.mu.Lock()
	f.typingBodies = append(f.typingBodies, body)
	f.mu.Unlock()

	writeJSONResponse(
		f.t,
		w,
		sendTypingResponse{
			apiErrorResponse: apiErrorResponse{Ret: 0},
		},
	)
}

func (f *fakeBackend) sendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sendBodies)
}

func (f *fakeBackend) latestSend() sendMessageRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sendBodies) == 0 {
		return sendMessageRequest{}
	}
	return f.sendBodies[len(f.sendBodies)-1]
}

func (f *fakeBackend) typingStatuses() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int, 0, len(f.typingBodies))
	for _, item := range f.typingBodies {
		out = append(out, item.Status)
	}
	return out
}

func writeJSONResponse(
	t *testing.T,
	w http.ResponseWriter,
	body any,
) {
	t.Helper()
	w.Header().Set(headerContentType, contentTypeJSON)
	require.NoError(t, json.NewEncoder(w).Encode(body))
}

func mustConfigNode(t *testing.T, raw string) *yaml.Node {
	t.Helper()
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(raw), &node))
	return &node
}

func waitForCondition(
	t *testing.T,
	condition func() bool,
) {
	t.Helper()
	require.Eventually(
		t,
		condition,
		5*time.Second,
		20*time.Millisecond,
	)
}

func newTestChannel(
	t *testing.T,
	globalStateDir string,
	baseURL string,
	cfg string,
	gateway registry.GatewayClient,
) *Channel {
	t.Helper()

	if cfg == "" {
		cfg = ""
	}
	node := mustConfigNode(t, cfg)
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  gateway,
			StateDir: globalStateDir,
		},
		registry.PluginSpec{
			Type:   pluginType,
			Config: node,
		},
	)
	require.NoError(t, err)
	result := ch.(*Channel)
	if baseURL != "" {
		result.baseURL = baseURL
	}
	return result
}

func TestInitRegistersWeixinChannel(t *testing.T) {
	t.Parallel()

	factory, ok := registry.LookupChannel(pluginType)
	require.True(t, ok)
	require.NotNil(t, factory)
}

func TestNewChannelRejectsNilGateway(t *testing.T) {
	t.Parallel()

	_, err := newChannel(registry.ChannelDeps{}, registry.PluginSpec{
		Type: pluginType,
	})
	require.ErrorContains(t, err, "nil gateway")
}

func TestRunProcessesInboundTextAndPersistsState(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := resolveStateDir(tmpDir, "")
	serverState := newFakeBackend(t, []getUpdatesResponse{
		{
			apiErrorResponse: apiErrorResponse{Ret: 0},
			Messages: []weixinMessage{{
				MessageID:    1001,
				FromUserID:   testPeerID,
				ToUserID:     testAccountID,
				MessageType:  messageTypeUser,
				MessageState: messageStateNew,
				ContextToken: "ctx-1001",
				ItemList: []messageItem{{
					Type: messageItemTypeText,
					TextItem: &textItem{
						Text: "hello claw",
					},
				}},
			}},
			GetUpdatesBuf: "cursor-1001",
		},
	})
	server := httptest.NewServer(http.HandlerFunc(serverState.handler))
	defer server.Close()

	require.NoError(t, SaveAccount(stateDir, Account{
		AccountID: testAccountID,
		Token:     testToken,
		BaseURL:   server.URL,
	}))

	gateway := &stubGateway{reply: "reply from gateway"}
	channel := newTestChannel(
		t,
		tmpDir,
		server.URL,
		"enable_typing: true\n",
		gateway,
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = channel.Run(ctx)
	}()

	waitForCondition(t, func() bool {
		return serverState.sendCount() >= 1
	})
	cancel()
	<-done

	require.Equal(t, 1, gateway.requestCount())
	req := gateway.lastRequest()
	require.Equal(t, pluginType, req.Channel)
	require.Equal(t, testPeerID, req.UserID)
	require.Equal(
		t,
		buildSessionID(testAccountID, testPeerID),
		req.SessionID,
	)
	require.Equal(t, "hello claw", req.Text)

	target, ok, err := delivery.TargetFromRequestExtensions(req.Extensions)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, pluginType, target.Channel)
	require.Equal(
		t,
		buildTextTarget(testAccountID, testPeerID),
		target.Target,
	)

	sendBody := serverState.latestSend()
	require.Equal(t, testPeerID, sendBody.Message.ToUserID)
	require.Equal(t, "ctx-1001", sendBody.Message.ContextToken)
	require.Len(t, sendBody.Message.ItemList, 1)
	require.Equal(
		t,
		"reply from gateway",
		sendBody.Message.ItemList[0].TextItem.Text,
	)

	waitForCondition(t, func() bool {
		status := channel.state.statusSnapshot(testAccountID)
		return status.LastInboundAt != nil &&
			status.LastOutboundAt != nil
	})
	reloaded, err := loadChannelState(stateDir)
	require.NoError(t, err)
	require.Equal(t, "cursor-1001", reloaded.cursor(testAccountID))
	require.Equal(
		t,
		"ctx-1001",
		reloaded.contextToken(testAccountID, testPeerID),
	)
	require.ElementsMatch(
		t,
		[]int{typingStatusActive, typingStatusCancel},
		serverState.typingStatuses(),
	)
}

func TestRunReloadsAccountsAddedAfterStartup(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := resolveStateDir(tmpDir, "")
	serverState := newFakeBackend(t, []getUpdatesResponse{
		{
			apiErrorResponse: apiErrorResponse{Ret: 0},
			Messages: []weixinMessage{{
				MessageID:    2001,
				FromUserID:   testPeerID,
				ToUserID:     testAccountID,
				MessageType:  messageTypeUser,
				MessageState: messageStateNew,
				ContextToken: "ctx-2001",
				ItemList: []messageItem{{
					Type: messageItemTypeText,
					TextItem: &textItem{
						Text: "hello after login",
					},
				}},
			}},
			GetUpdatesBuf: "cursor-2001",
		},
	})
	server := httptest.NewServer(http.HandlerFunc(serverState.handler))
	defer server.Close()

	gateway := &stubGateway{reply: "reply after login"}
	channel := newTestChannel(
		t,
		tmpDir,
		server.URL,
		"enable_typing: false\nerror_backoff: 10ms\n",
		gateway,
	)
	channel.accountRefreshInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = channel.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, SaveAccount(stateDir, Account{
		AccountID: testAccountID,
		Token:     testToken,
		BaseURL:   server.URL,
	}))

	waitForCondition(t, func() bool {
		return gateway.requestCount() >= 1
	})
	cancel()
	<-done

	require.Equal(t, 1, gateway.requestCount())
	require.Equal(t, 1, serverState.sendCount())
}

func TestHandleInboundRuntimeStatusSkipsGateway(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := resolveStateDir(tmpDir, "")
	serverState := newFakeBackend(t, nil)
	server := httptest.NewServer(http.HandlerFunc(serverState.handler))
	defer server.Close()

	require.NoError(t, SaveAccount(stateDir, Account{
		AccountID: testAccountID,
		Token:     testToken,
		BaseURL:   server.URL,
	}))

	gateway := &stubGateway{reply: "should not be used"}
	channel := newTestChannel(
		t,
		tmpDir,
		server.URL,
		"enable_runtime_commands: true\n",
		gateway,
	)

	err := channel.handleInboundMessage(
		context.Background(),
		Account{
			AccountID: testAccountID,
			Token:     testToken,
			BaseURL:   server.URL,
		},
		weixinMessage{
			FromUserID:   testPeerID,
			ToUserID:     testAccountID,
			MessageType:  messageTypeUser,
			MessageState: messageStateNew,
			ContextToken: "ctx-status",
			ItemList: []messageItem{{
				Type: messageItemTypeText,
				TextItem: &textItem{
					Text: "/runtime status",
				},
			}},
		},
	)
	require.NoError(t, err)

	require.Zero(t, gateway.requestCount())
	waitForCondition(t, func() bool {
		return serverState.sendCount() >= 1
	})
	body := serverState.latestSend()
	require.Contains(
		t,
		body.Message.ItemList[0].TextItem.Text,
		"Runtime status",
	)
}

func TestRunPausesOnSessionExpired(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := resolveStateDir(tmpDir, "")
	serverState := newFakeBackend(t, []getUpdatesResponse{{
		apiErrorResponse: apiErrorResponse{
			Ret:     1,
			ErrCode: sessionExpiredErrCode,
			ErrMsg:  "session expired",
		},
	}})
	server := httptest.NewServer(http.HandlerFunc(serverState.handler))
	defer server.Close()

	require.NoError(t, SaveAccount(stateDir, Account{
		AccountID: testAccountID,
		Token:     testToken,
		BaseURL:   server.URL,
	}))

	channel := newTestChannel(
		t,
		tmpDir,
		server.URL,
		"error_backoff: 10ms\n",
		&stubGateway{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = channel.Run(ctx)
	}()

	waitForCondition(t, func() bool {
		status := channel.state.statusSnapshot(testAccountID)
		return status.PausedUntil != nil
	})
	cancel()
	<-done

	status := channel.state.statusSnapshot(testAccountID)
	require.NotNil(t, status.PausedUntil)
	require.Contains(t, status.LastError, "session expired")
}
