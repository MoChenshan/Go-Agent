package wecom

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/ingress"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimehint"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/conversation"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwproto"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
)

// --- Test fixtures ---

const (
	testCorpID = "test_corp_id"
	testToken  = "test_token"
	// 43-char base64 key (decodes to 32 bytes)
	testEncodingAESKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	testGIFBase64      = "R0lGODdhAQABAIAAAP///////ywAAAAAAQABAAAC" +
		"AkQBADs="
	testPNGBase64 = "" +
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lE" +
		"QVR42mP8/x8AAwMCAO7Zk1cAAAAASUVORK5CYII="
)

func testCredentials() (corpID, token, encodingAESKey string) {
	corpID = testCorpID
	token = testToken
	encodingAESKey = testEncodingAESKey
	return
}

func allowAnyMediaURL(_ *url.URL) error {
	return nil
}

func boolPtr(value bool) *bool {
	return &value
}

func stripReplyPrefixForTest(content string) string {
	content = strings.TrimSpace(content)
	separator := "\n\n"
	index := strings.Index(content, separator)
	if index < 0 {
		if isReplyPrefixBlockForTest(content) {
			return ""
		}
		return content
	}
	header := content[:index]
	if !isReplyPrefixBlockForTest(header) {
		return content
	}
	return strings.TrimSpace(content[index+len(separator):])
}

func isReplyPrefixBlockForTest(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	if !strings.HasPrefix(content, replyPrefixLeadMarker) {
		return false
	}
	return strings.Contains(content, replyPrefixEmojiPersona) ||
		strings.Contains(content, replyPrefixEmojiCommands) ||
		strings.Contains(content, replyPrefixEmojiHint) ||
		strings.Contains(content, replyPrefixEmojiAssistant) ||
		strings.Contains(content, replyPrefixEmojiWorkspace) ||
		strings.Contains(content, replyPrefixEmojiLink)
}

func testConfig() string {
	corpID, token, aesKey := testCredentials()
	return fmt.Sprintf(`
bot_mode: notification
corp_id: %s
agent_id: "1000002"
token: %s
encoding_aes_key: %s
callback_port: 0
webhook_url: "https://example.com/webhook"
bot_name: "TestBot"
aggregate_window: "0s"
reply_prefix:
  enabled: false
`, corpID, token, aesKey)
}

func testConfigWithPolicy(policy string, users ...string) string {
	cfg := testConfig()
	if policy != "" {
		cfg += fmt.Sprintf("chat_policy: %s\n", policy)
	}
	if len(users) > 0 {
		cfg += "allow_users:\n"
		for _, u := range users {
			cfg += fmt.Sprintf("  - %s\n", u)
		}
	}
	return cfg
}

func testWebSocketConfig() string {
	return `
bot_mode: ai
connection_mode: websocket
aibotid: bot1
secret: secret1
enable_stream: true
aggregate_window: "0s"
reply_prefix:
  enabled: false
`
}

func testAIWebhookConfig() string {
	corpID, token, aesKey := testCredentials()
	return fmt.Sprintf(`
bot_mode: ai
corp_id: %s
agent_id: "1000002"
token: %s
encoding_aes_key: %s
connection_mode: webhook
aggregate_window: "0s"
`, corpID, token, aesKey)
}

func testConfigWithRuntimeWorkspace(
	workdir string,
	scratchRoot string,
) string {
	cfg := testConfig()
	if strings.TrimSpace(workdir) != "" {
		cfg += fmt.Sprintf(
			"%s: %q\n",
			RuntimeDefaultWorkdirConfigKey,
			workdir,
		)
	}
	if strings.TrimSpace(scratchRoot) != "" {
		cfg += fmt.Sprintf(
			"%s: %q\n",
			RuntimeScratchRootConfigKey,
			scratchRoot,
		)
	}
	return cfg
}

// --- Registration tests ---

func TestInitRegistersWeComChannel(t *testing.T) {
	t.Parallel()

	f, ok := registry.LookupChannel(pluginType)
	require.True(t, ok)
	require.NotNil(t, f)
}

// --- Factory validation tests ---

func TestNewChannelRejectsNilGateway(t *testing.T) {
	t.Parallel()

	_, err := newChannel(registry.ChannelDeps{}, registry.PluginSpec{
		Type: pluginType,
	})
	require.ErrorContains(t, err, errNilGateway)
}

func TestNewChannelRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testConfig()+`unknown_key: 1`)
	_, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "corp",
			Config: cfg,
		},
	)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "not implemented")
}

func TestNewChannelValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		errText string
	}{
		{"missing bot_mode", `token: t
encoding_aes_key: k`, "bot_mode is required"},
		{"missing token", `bot_mode: notification
encoding_aes_key: k
webhook_url: http://x`, "token is required"},
		{"missing encoding_aes_key", `bot_mode: notification
token: t
webhook_url: http://x`, "encoding_aes_key is required"},
		{"missing webhook_url", `bot_mode: notification
token: t
encoding_aes_key: k`, "webhook_url is required"},
		{"websocket missing aibotid", `bot_mode: ai
connection_mode: websocket
secret: s`, "aibotid is required"},
		{"websocket missing secret", `bot_mode: ai
connection_mode: websocket
aibotid: bot1`, "secret is required"},
		{"notification websocket unsupported", `bot_mode: notification
token: t
encoding_aes_key: k
connection_mode: websocket
webhook_url: http://x`, "websocket mode only supports ai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := mustYAMLNode(t, tt.yaml)
			_, err := newChannel(
				registry.ChannelDeps{Gateway: stubGateway{}},
				registry.PluginSpec{Type: pluginType, Config: cfg},
			)
			require.ErrorContains(t, err, tt.errText)
		})
	}
}

func TestRenderRequestSystemPromptStructure(t *testing.T) {
	t.Parallel()

	rendered := RenderRequestSystemPromptStructure(
		"${TRPC_CLAW_WECOM_TURN_CONTEXT_NOTES:-}\n\n" +
			"${TRPC_CLAW_WECOM_RUNTIME_RULES:-}\n\n" +
			"${TRPC_CLAW_WECOM_BROWSER_NOTES:-}",
	)
	require.Equal(
		t,
		"[Turn context notes]\n\n[Runtime rules]\n\n[Browser notes]",
		rendered,
	)
}

func TestNewChannelRejectsInvalidChatPolicy(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testConfig()+`chat_policy: "invalid"`)
	_, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{Type: pluginType, Name: "test", Config: cfg},
	)
	require.ErrorContains(t, err, "unsupported chat_policy")
}

func TestNewChannelSuccess(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testConfig())
	ch, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, ch)
	require.Equal(t, "wecom", ch.ID())
}

func TestNewChannelUsesRuntimeWorkspaceDefaults(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	scratchRoot := filepath.Join(t.TempDir(), "scratch")
	cfg := mustYAMLNode(
		t,
		testConfigWithRuntimeWorkspace(workdir, scratchRoot),
	)
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  stubGateway{},
			StateDir: filepath.Join(t.TempDir(), "state"),
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: cfg,
		},
	)
	require.NoError(t, err)

	channel := ch.(*Channel)
	require.Equal(t, workdir, channel.defaultCodingWorkspace)
	require.Equal(t, scratchRoot, channel.codingScratchRoot)
	require.Equal(
		t,
		filepath.Join(scratchRoot, replyDeliveryOutputDirName),
		channel.codingArtifactOutputRoot,
	)
	require.NotEmpty(t, channel.runtimeTempRoot)
	info, err := os.Stat(scratchRoot)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestNewChannelUsesRuntimeReplyDeliveryRoots(t *testing.T) {
	t.Parallel()

	replyRoot := filepath.Join(t.TempDir(), "exports")
	cfg := mustYAMLNode(
		t,
		testConfig()+fmt.Sprintf(
			"%s:\n  - %q\n",
			RuntimeReplyDeliveryRootsConfigKey,
			replyRoot,
		),
	)
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  stubGateway{},
			StateDir: filepath.Join(t.TempDir(), "state"),
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: cfg,
		},
	)
	require.NoError(t, err)

	channel := ch.(*Channel)
	require.Equal(
		t,
		[]string{replyRoot},
		channel.runtimeReplyDeliveryRoots,
	)
}

func TestNewChannelUsesManagedUploadsRoot(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join(t.TempDir(), "state")
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  stubGateway{},
			StateDir: stateDir,
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: mustYAMLNode(t, testConfig()),
		},
	)
	require.NoError(t, err)

	channel := ch.(*Channel)
	require.Equal(
		t,
		filepath.Join(stateDir, "uploads"),
		channel.runtimeManagedUploadsRoot,
	)
}

func TestNewChannelWebSocketWithoutWebhookCrypto(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testWebSocketConfig())
	ch, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test-websocket",
			Config: cfg,
		},
	)
	require.NoError(t, err)

	channel := ch.(*Channel)
	require.Nil(t, channel.crypt)
	require.Equal(t, connectionModeWebSocket, channel.connectionMode)
	require.Equal(t, "bot1", channel.cfg.AIBotID)
}

func TestNewChannelWebSocketDisablesTextFastPath(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testWebSocketConfig())
	ch, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test-websocket",
			Config: cfg,
		},
	)
	require.NoError(t, err)

	channel := ch.(*Channel)
	require.NotNil(t, channel.aggregator)
	require.False(t, channel.aggregator.textFastPathEnabled)
}

func TestPrefetchMessageMediaReusesDownloadedFile(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	fileData := []byte("%PDF-1.7\nhello")
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		requests.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", mimeTypePDF)
		w.Header().Set(
			"Content-Disposition",
			`attachment; filename="report.pdf"`,
		)
		_, _ = w.Write(fileData)
	}))
	defer server.Close()

	ch := &Channel{
		mediaClient:       server.Client(),
		mediaPrefetch:     newMediaPrefetcher(defaultMediaPrefetchTTL),
		mediaURLValidator: allowAnyMediaURL,
	}
	ch.prefetchMessageMedia(WebhookMessage{
		MsgType: MsgTypeFile,
		File: FileContent{
			URL: server.URL,
		},
	})

	parts, err := ch.materializeContentParts(
		context.Background(),
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					URL: server.URL,
				},
			},
		},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.NotNil(t, parts[0].File)
	require.Equal(t, fileData, parts[0].File.Data)
	require.Equal(t, int32(1), requests.Load())
}

func TestMediaPrefetchSnapshotSurvivesMemoryTTLExpiry(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	fileData := []byte("%PDF-1.7\nsnapshot")
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		requests.Add(1)
		w.Header().Set("Content-Type", mimeTypePDF)
		w.Header().Set(
			"Content-Disposition",
			`attachment; filename="snapshot.pdf"`,
		)
		_, _ = w.Write(fileData)
	}))
	defer server.Close()

	snapshotDir := t.TempDir()
	ch1 := &Channel{
		mediaClient:       server.Client(),
		mediaURLValidator: allowAnyMediaURL,
		mediaPrefetch: newMediaPrefetcherWithSnapshotDir(
			20*time.Millisecond,
			snapshotDir,
			time.Hour,
		),
	}
	ch1.prefetchMessageMedia(WebhookMessage{
		MsgType: MsgTypeFile,
		File: FileContent{
			URL: server.URL,
		},
	})

	require.Eventually(t, func() bool {
		return requests.Load() == 1
	}, time.Second, 10*time.Millisecond)

	time.Sleep(40 * time.Millisecond)

	ch2 := &Channel{
		mediaClient:       server.Client(),
		mediaURLValidator: allowAnyMediaURL,
		mediaPrefetch: newMediaPrefetcherWithSnapshotDir(
			20*time.Millisecond,
			snapshotDir,
			time.Hour,
		),
	}
	parts, err := ch2.materializeContentParts(
		context.Background(),
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					URL: server.URL,
				},
			},
		},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.NotNil(t, parts[0].File)
	require.Equal(t, fileData, parts[0].File.Data)
	require.Equal(t, int32(1), requests.Load())
}

func TestNewChannelWithAllowlist(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testConfigWithPolicy("allowlist", "user1", "user2"))
	ch, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{Type: pluginType, Name: "test", Config: cfg},
	)
	require.NoError(t, err)
	c := ch.(*Channel)
	require.Equal(t, chatPolicyAllowlist, c.chatPolicy)
	require.Contains(t, c.allowUsers, "user1")
	require.Contains(t, c.allowUsers, "user2")
}

// --- Exported New() tests ---

func TestNewExportedConstructor(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testConfig())
	ch, err := New(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{Type: pluginType, Name: "test", Config: cfg},
		WithProcessingMessage("custom"),
	)
	require.NoError(t, err)
	c := ch.(*Channel)
	require.Equal(t, "custom", c.processingMessage)
}

// --- Crypto tests ---

func TestMsgCryptRoundTrip(t *testing.T) {
	t.Parallel()

	corpID, token, aesKey := testCredentials()
	mc, err := newMsgCrypt(token, aesKey, corpID)
	require.NoError(t, err)

	original := []byte(`{"msgid":"123","text":{"content":"hello"}}`)
	encrypted, err := mc.encrypt(original)
	require.NoError(t, err)

	// Build signature.
	timestamp := "1234567890"
	nonce := "testnonce"
	sig := buildSignature(token, timestamp, nonce, encrypted)

	body, _ := json.Marshal(EncryptedBody{Encrypt: encrypted})
	decrypted, err := mc.DecryptMsg(sig, timestamp, nonce, body)
	require.NoError(t, err)
	require.Equal(t, original, decrypted)
}

func TestMsgCryptVerifyURL(t *testing.T) {
	t.Parallel()

	corpID, token, aesKey := testCredentials()
	mc, err := newMsgCrypt(token, aesKey, corpID)
	require.NoError(t, err)

	original := []byte("echostr_content")
	encrypted, err := mc.encrypt(original)
	require.NoError(t, err)

	timestamp := "1234567890"
	nonce := "testnonce"
	sig := buildSignature(token, timestamp, nonce, encrypted)

	result, err := mc.VerifyURL(sig, timestamp, nonce, encrypted)
	require.NoError(t, err)
	require.Equal(t, original, result)
}

func TestMsgCryptBadSignature(t *testing.T) {
	t.Parallel()

	corpID, token, aesKey := testCredentials()
	mc, err := newMsgCrypt(token, aesKey, corpID)
	require.NoError(t, err)

	_, err = mc.VerifyURL("bad_signature", "ts", "n", "data")
	require.ErrorContains(t, err, "signature verification failed")
}

func TestNewMsgCryptBadKeyLength(t *testing.T) {
	t.Parallel()

	corpID, token, _ := testCredentials()
	_, err := newMsgCrypt(token, "short", corpID)
	require.ErrorContains(t, err, "43 chars")
}

// --- Message model tests ---

func TestExtractText(t *testing.T) {
	t.Parallel()

	ch := &Channel{cfg: channelCfg{BotName: "MyBot"}}

	tests := []struct {
		name string
		msg  WebhookMessage
		want string
	}{
		{
			"text message",
			WebhookMessage{MsgType: "text", Text: TextContent{Content: "hello world"}},
			"hello world",
		},
		{
			"text with mention",
			WebhookMessage{MsgType: "text", Text: TextContent{Content: "@MyBot hello"}},
			"hello",
		},
		{
			"image with url",
			WebhookMessage{MsgType: "image", Image: ImageContent{URL: "https://example.com/pic.jpg"}},
			"[image:https://example.com/pic.jpg]",
		},
		{
			"image without url",
			WebhookMessage{MsgType: "image"},
			"",
		},
		{
			"mixed text and image",
			WebhookMessage{MsgType: "mixed", MixedMessage: MixedMessageContent{
				MsgItem: []MixedMsgItem{
					{MsgType: "text", Text: TextContent{Content: "look at this"}},
					{MsgType: "image", Image: ImageContent{URL: "https://example.com/img.png"}},
					{MsgType: "text", Text: TextContent{Content: "nice?"}},
				},
			}},
			"look at this [image:https://example.com/img.png] nice?",
		},
		{
			"mixed text only",
			WebhookMessage{MsgType: "mixed", MixedMessage: MixedMessageContent{
				MsgItem: []MixedMsgItem{
					{MsgType: "text", Text: TextContent{Content: "part1 "}},
					{MsgType: "image"},
					{MsgType: "text", Text: TextContent{Content: "part2"}},
				},
			}},
			"part1  part2",
		},
		{
			"event",
			WebhookMessage{MsgType: "event"},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ch.extractText(tt.msg)
			require.Equal(t, tt.want, got)
		})
	}
}

// --- User permission tests ---

func TestIsUserAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  string
		users   map[string]struct{}
		userID  string
		allowed bool
	}{
		{"open allows anyone", chatPolicyOpen, nil, "anyone", true},
		{"disabled blocks all", chatPolicyDisabled, nil, "anyone", false},
		{"allowlist allows listed", chatPolicyAllowlist, map[string]struct{}{"user1": {}}, "user1", true},
		{"allowlist blocks unlisted", chatPolicyAllowlist, map[string]struct{}{"user1": {}}, "user2", false},
		{"allowlist nil blocks all", chatPolicyAllowlist, nil, "user1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ch := &Channel{chatPolicy: tt.policy, allowUsers: tt.users}
			require.Equal(t, tt.allowed, ch.isUserAllowed(tt.userID))
		})
	}
}

// --- handleMessage with policy tests ---

func TestHandleMessageBlockedByPolicy(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                stubGateway{},
		sender:            ms,
		chatPolicy:        chatPolicyDisabled,
		inflight:          newInflightRequests(),
		lanes:             newLaneLocker(),
		notAllowedMessage: defaultNotAllowedMessage,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "hello"},
	})
	require.NoError(t, err)
	require.Equal(t, defaultNotAllowedMessage, ms.lastText)
}

func TestHandleMessageAllowlistAllows(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                gw,
		sender:            ms,
		chatPolicy:        chatPolicyAllowlist,
		allowUsers:        map[string]struct{}{"user1": {}},
		inflight:          newInflightRequests(),
		lanes:             newLaneLocker(),
		sessionTracker:    newSessionTracker(),
		processingMessage: defaultProcessingMessage,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "hello"},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
}

func TestHandleMessageAllowlistBlocks(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                stubGateway{},
		sender:            ms,
		chatPolicy:        chatPolicyAllowlist,
		allowUsers:        map[string]struct{}{"user1": {}},
		inflight:          newInflightRequests(),
		lanes:             newLaneLocker(),
		sessionTracker:    newSessionTracker(),
		notAllowedMessage: defaultNotAllowedMessage,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user2"},
		Text:    TextContent{Content: "hello"},
	})
	require.NoError(t, err)
	require.Equal(t, defaultNotAllowedMessage, ms.lastText)
}

func TestHandleMessageQueuedRequestSkipsEarlyStreamHint(
	t *testing.T,
) {
	t.Parallel()

	gw := &blockingGateway{
		releaseCh: make(chan struct{}),
		startedCh: make(chan struct{}, 2),
	}
	writer1 := newRecordingWSWriter()
	writer2 := newRecordingWSWriter()
	ch := &Channel{
		cfg: channelCfg{
			BotName:            "Bot",
			EnableStream:       true,
			ReplyPrefix:        replyPrefixCfg{Enabled: boolPtr(false)},
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		gw:                gw,
		chatPolicy:        chatPolicyOpen,
		inflight:          newInflightRequests(),
		lanes:             newLaneLocker(),
		sessionTracker:    newSessionTracker(),
		aggregator:        newMessageAggregator(0),
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	msg1 := WebhookMessage{
		MsgID:         "m1",
		MsgType:       MsgTypeText,
		ChatID:        "chat1",
		From:          FromInfo{UserID: "user1"},
		Text:          TextContent{Content: "hello"},
		CallbackReqID: "req-1",
		ReplyWriter:   writer1,
	}
	msg2 := WebhookMessage{
		MsgID:         "m2",
		MsgType:       MsgTypeText,
		ChatID:        "chat1",
		From:          FromInfo{UserID: "user1"},
		Text:          TextContent{Content: "second"},
		CallbackReqID: "req-2",
		ReplyWriter:   writer2,
	}

	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- ch.handleMessage(context.Background(), msg1)
	}()

	select {
	case <-gw.startedCh:
	case <-time.After(time.Second):
		t.Fatal("expected first request to enter gateway")
	}

	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- ch.handleMessage(context.Background(), msg2)
	}()

	select {
	case <-writer2.ch:
		t.Fatal("queued websocket request sent early stream hint")
	case <-time.After(150 * time.Millisecond):
	}

	close(gw.releaseCh)

	require.NoError(t, <-errCh1)
	require.NoError(t, <-errCh2)

	firstFrame := writer2.waitFrame(t, time.Second)
	require.Equal(t, wsCommandRespond, firstFrame.Command)
	require.Equal(t, "req-2", firstFrame.Headers.ReqID)
	firstBody, ok := firstFrame.Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, MsgTypeStream, firstBody.MsgType)
	require.NotNil(t, firstBody.Stream)
	require.Equal(t, streamNativeThinkingPlaceholder, firstBody.Stream.Content)
	require.False(t, firstBody.Stream.Finish)
	require.NotNil(t, firstBody.Stream.Feedback)

	secondFrame := writer2.waitFrame(t, time.Second)
	require.Equal(t, wsCommandRespond, secondFrame.Command)
	require.Equal(t, "req-2", secondFrame.Headers.ReqID)
	secondBody, ok := secondFrame.Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, MsgTypeStream, secondBody.MsgType)
	require.NotNil(t, secondBody.Stream)
	require.Equal(
		t,
		nativeThinkingStreamContent(nil, "ok", true),
		secondBody.Stream.Content,
	)
	require.True(t, secondBody.Stream.Finish)
	require.Nil(t, secondBody.Stream.Feedback)
}

// --- handleMessage with commands ---

func TestHandleMessageHelp(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		cfg:            channelCfg{BotName: "Bot"},
		gw:             stubGateway{},
		sender:         ms,
		chatPolicy:     chatPolicyOpen,
		inflight:       newInflightRequests(),
		lanes:          newLaneLocker(),
		sessionTracker: newSessionTracker(),
		helpMessage:    defaultHelpMessage,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/help"},
	})
	require.NoError(t, err)
	require.NotNil(t, ms.lastTemplateCard)
	require.Contains(
		t,
		ms.lastTemplateCard.MainTitle.Title,
		controlCardTitleHelp,
	)
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		runtimeKeyword,
	)
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		controlHelpPageIndicator(controlHelpPageDefault, controlHelpPageLabelCommon),
	)
}

func TestHandleMessageHelpWithMultiWordMentionAndEmptyBotName(
	t *testing.T,
) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		gw:             stubGateway{},
		sender:         ms,
		chatPolicy:     chatPolicyOpen,
		inflight:       newInflightRequests(),
		lanes:          newLaneLocker(),
		sessionTracker: newSessionTracker(),
		helpMessage:    defaultHelpMessage,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType:  MsgTypeText,
		ChatID:   "chat1",
		ChatType: "group",
		From: FromInfo{
			UserID: "user1",
		},
		Text: TextContent{
			Content: "@My Bot /help",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, ms.lastTemplateCard)
	require.Contains(
		t,
		ms.lastTemplateCard.MainTitle.Title,
		controlCardTitleHelp,
	)
}

func TestHandleMessageRuntimeHelpAlias(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		gw:             stubGateway{},
		sender:         ms,
		chatPolicy:     chatPolicyOpen,
		inflight:       newInflightRequests(),
		lanes:          newLaneLocker(),
		sessionTracker: newSessionTracker(),
		helpMessage:    defaultHelpMessage,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/runtime help"},
	})
	require.NoError(t, err)
	require.Nil(t, ms.lastTemplateCard)
	require.Contains(t, ms.lastText, runtimeKeyword+" 运行时控制")
	require.Contains(
		t,
		ms.lastText,
		runtimeKeyword+" "+runtimeActionVersions,
	)
	require.Contains(
		t,
		ms.lastText,
		runtimeKeyword+" "+runtimeActionBundle,
	)
	require.Contains(
		t,
		ms.lastText,
		runtimeKeyword+" "+runtimeActionBundle+
			" "+runtimeActionFull,
	)
}

func TestHandleMessageStatus(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	runStatus := newRunStatusTracker()
	sessionTracker := newSessionTracker()
	baseSessionID := buildSessionID("chat1", "user1")
	sessionInfo := sessionTracker.getOrCreateSession(baseSessionID, 0)
	runStatus.start(
		sessionInfo.sessionID,
		"req1",
		progressTextReadingDocument,
	)
	runStatus.preview(
		sessionInfo.sessionID,
		"req1",
		"partial output",
	)

	ch := &Channel{
		cfg:              channelCfg{BotName: "Bot"},
		gw:               stubGateway{},
		sender:           ms,
		chatPolicy:       chatPolicyOpen,
		inflight:         newInflightRequests(),
		lanes:            newLaneLocker(),
		sessionTracker:   sessionTracker,
		runStatus:        runStatus,
		helpMessage:      defaultHelpMessage,
		runtimeModelName: "gpt-5.2",
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/status"},
	})
	require.NoError(t, err)
	require.NotNil(t, ms.lastTemplateCard)
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		statusLabelState+statusLineRunning,
	)
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		statusLabelStep+progressTextReadingDocument,
	)
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		displayLabelModel+"gpt-5.2",
	)
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		"partial output",
	)
}

func TestHandleMessageCancel(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                &cancelGateway{cancelResult: false},
		sender:            ms,
		chatPolicy:        chatPolicyOpen,
		inflight:          newInflightRequests(),
		lanes:             newLaneLocker(),
		sessionTracker:    newSessionTracker(),
		cancelNoopMessage: defaultCancelNoopMessage,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/cancel"},
	})
	require.NoError(t, err)
	require.Equal(t, defaultCancelNoopMessage, ms.lastText)
}

func TestHandleMessageRecallSwitchesSession(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	runStatus := newRunStatusTracker()
	sessionTracker := newSessionTracker()
	baseSessionID := buildSessionID("chat1", "user1")
	sessionTracker.sessions[baseSessionID] = &sessionInfo{
		sessionID:       "current-session",
		baseSessionID:   baseSessionID,
		recallSessionID: "previous-session",
		lastActivity:    time.Now(),
	}
	runStatus.start(
		"previous-session",
		"req-prev",
		progressTextSummarizing,
	)

	ch := &Channel{
		cfg:            channelCfg{BotName: "Bot"},
		gw:             stubGateway{},
		sender:         ms,
		chatPolicy:     chatPolicyOpen,
		inflight:       newInflightRequests(),
		lanes:          newLaneLocker(),
		sessionTracker: sessionTracker,
		runStatus:      runStatus,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/recall"},
	})
	require.NoError(t, err)
	require.Contains(t, ms.lastText, defaultRecallMessage)
	require.Contains(
		t,
		ms.lastText,
		statusLabelStep+progressTextSummarizing,
	)

	current := sessionTracker.getOrCreateSession(baseSessionID, 0)
	require.Equal(t, "previous-session", current.sessionID)
	require.Equal(t, "current-session", current.recallSessionID)
}

func TestHandleMessageSessionsListsHistory(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	sessionTracker := newSessionTracker()
	baseSessionID := buildSessionID("chat1", "user1")
	sessionTracker.sessions[baseSessionID] = &sessionInfo{
		sessionID:     "current-session",
		baseSessionID: baseSessionID,
		lastActivity:  time.Now(),
		history: []sessionHistoryEntry{
			{
				SessionID:    "current-session",
				LastActivity: time.Now(),
			},
			{
				SessionID:    "previous-session",
				LastActivity: time.Now().Add(-time.Minute),
			},
		},
	}

	ch := &Channel{
		cfg:            channelCfg{BotName: "Bot"},
		gw:             stubGateway{},
		sender:         ms,
		chatPolicy:     chatPolicyOpen,
		inflight:       newInflightRequests(),
		lanes:          newLaneLocker(),
		sessionTracker: sessionTracker,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/sessions"},
	})
	require.NoError(t, err)
	require.NotNil(t, ms.lastTemplateCard)
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		"最近会话（1=当前）：",
	)
	require.Contains(t, ms.lastTemplateCard.SubTitleText, "1. 当前 ·")
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		"2. previous-session",
	)
}

func TestHandleMessageSwitchChangesActiveSession(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	sessionTracker := newSessionTracker()
	baseSessionID := buildSessionID("chat1", "user1")
	sessionTracker.sessions[baseSessionID] = &sessionInfo{
		sessionID:     "current-session",
		baseSessionID: baseSessionID,
		lastActivity:  time.Now(),
		history: []sessionHistoryEntry{
			{
				SessionID:    "current-session",
				LastActivity: time.Now(),
			},
			{
				SessionID:    "previous-session",
				LastActivity: time.Now().Add(-time.Minute),
			},
		},
	}

	ch := &Channel{
		cfg:            channelCfg{BotName: "Bot"},
		gw:             stubGateway{},
		sender:         ms,
		chatPolicy:     chatPolicyOpen,
		inflight:       newInflightRequests(),
		lanes:          newLaneLocker(),
		sessionTracker: sessionTracker,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/switch 2"},
	})
	require.NoError(t, err)
	require.Contains(t, ms.lastText, "✅ 已切换到第 2 个会话。")

	current := sessionTracker.getOrCreateSession(baseSessionID, 0)
	require.Equal(t, "previous-session", current.sessionID)
}

func TestHandleMessageSessionsCommandWithLeadingMention(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	sessionTracker := newSessionTracker()
	baseSessionID := buildSessionID("chat1", "user1")
	sessionTracker.sessions[baseSessionID] = &sessionInfo{
		sessionID:     "current-session",
		baseSessionID: baseSessionID,
		lastActivity:  time.Now(),
		history: []sessionHistoryEntry{
			{
				SessionID:    "current-session",
				LastActivity: time.Now(),
			},
			{
				SessionID:    "previous-session",
				LastActivity: time.Now().Add(-time.Minute),
			},
		},
	}

	ch := &Channel{
		cfg:            channelCfg{BotName: "Bot"},
		gw:             stubGateway{},
		sender:         ms,
		chatPolicy:     chatPolicyOpen,
		inflight:       newInflightRequests(),
		lanes:          newLaneLocker(),
		sessionTracker: sessionTracker,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: "@X /sessions",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, ms.lastTemplateCard)
	require.Contains(
		t,
		ms.lastTemplateCard.SubTitleText,
		"最近会话（1=当前）：",
	)
}

func TestHandleMessagePersonaSetsRuntimeOverride(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/persona concise"},
	})
	require.NoError(t, err)
	require.Contains(t, ms.lastText, "当前聊天人格已切换为")

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "你好"},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(t, "你好", gw.lastReq.Text)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Runtime chat persona override: concise.",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		runtimePersonaPrimaryStyleRule,
	)
	require.Equal(t, "wecom:dm:user1", gw.lastReq.UserID)
	require.Equal(t, "wecom:chat:chat1", gw.lastReq.SessionID)

	annotation, ok, err := conversation.
		AnnotationFromRequestExtensions(
			gw.lastReq.Extensions,
		)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(
		t,
		conversation.HistoryModeShared,
		annotation.HistoryMode,
	)
	require.Equal(t, "wecom:chat:chat1", annotation.StorageUserID)
	require.Equal(t, "user1", annotation.ActorID)
}

func TestHandleMessageGroupIsolatedSessionUsesPerUserScope(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	cfg := mustYAMLNode(
		t,
		testConfig()+"group_session_mode: isolated\n",
	)
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  gw,
			StateDir: t.TempDir(),
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	ch.(*Channel).sender = ms

	err = ch.(*Channel).handleMessage(context.Background(), WebhookMessage{
		MsgID:    "m1",
		MsgType:  "text",
		ChatID:   "chat1",
		ChatType: "group",
		From: FromInfo{
			UserID: "user1",
			Name:   "Alice",
		},
		Text: TextContent{Content: "你好"},
		Quote: &QuoteContent{
			MsgType: MsgTypeText,
			Text: TextContent{
				Content: "之前的话",
			},
		},
	})
	require.NoError(t, err)
	require.Equal(
		t,
		"wecom:dm:user1",
		gw.lastReq.UserID,
	)

	annotation, ok, err := conversation.
		AnnotationFromRequestExtensions(
			gw.lastReq.Extensions,
		)
	require.NoError(t, err)
	require.True(t, ok)
	require.Empty(t, annotation.HistoryMode)
	require.Equal(t, "wecom:chat:chat1:user:user1", annotation.StorageUserID)
	require.Equal(t, "user1", annotation.ActorID)
	require.Equal(t, "Alice", annotation.ActorLabel)
	require.Equal(t, "之前的话", annotation.QuoteText)
}

func TestHandleMessageWebSocketAddsDeliveryTarget(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ch := mustCreateWebSocketChannel(t, gw)
	ch.sender = &mockSender{}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: MsgTypeText,
		ChatID:  "chat1",
		From: FromInfo{
			UserID: "user1",
		},
		Text: TextContent{Content: "你好"},
	})
	require.NoError(t, err)

	target, ok, err := delivery.TargetFromRequestExtensions(
		gw.lastReq.Extensions,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, pluginType, target.Channel)
	require.Equal(t, "group:chat1", target.Target)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"WeCom cron delivery: current group chat target "+
			"is group:chat1.",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Use the resolved participant-name table",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"do not support guaranteed real @ mentions",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"mapped canonical label exactly in the message body",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"faithful to the user's original request",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"shorter todo summary",
	)
}

func TestHandleMessageWebSocketIgnoresMentionedUsersInDeliveryTarget(
	t *testing.T,
) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ch := mustCreateWebSocketChannel(t, gw)
	ch.sender = &mockSender{}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: MsgTypeText,
		ChatID:  "chat1",
		From: FromInfo{
			UserID: "user1",
		},
		Text: TextContent{
			Content:       "@TestBot @user2 每天提醒一次",
			MentionedList: []string{"user2", "user2"},
		},
	})
	require.NoError(t, err)

	target, ok, err := delivery.TargetFromRequestExtensions(
		gw.lastReq.Extensions,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(
		t,
		"group:chat1",
		target.Target,
	)
}

func TestHandleMessageSharedGroupAddsSpeakerScopedMemoryPromptNote(
	t *testing.T,
) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:    "m1",
		MsgType:  "text",
		ChatID:   "chat1",
		ChatType: "group",
		From: FromInfo{
			UserID: "user1",
			Name:   "Alice",
		},
		Text: TextContent{Content: "以后对我说文言文"},
	})
	require.NoError(t, err)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"WeCom shared-chat speaker memory: current speaker for this turn is Alice (user_id=user1).",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"treat it as speaker-scoped rather than a group-wide rule",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"write such a preference into durable memory",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"`- [speaker:user1] reply in classical Chinese.`",
	)
	require.NotContains(t, gw.lastReq.RequestSystemPrompt, "MEMORY.md")
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"apply speaker-scoped bullets only when they match the current speaker",
	)
}

func TestHandleMessagePersonaSaveSetsRuntimeOverride(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: "/persona save 爱心 " +
				"你是一个更像产品合伙人的助手。",
		},
	})
	require.NoError(t, err)
	require.Contains(t, ms.lastText, "已保存并启用人格")

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "你好"},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(t, "你好", gw.lastReq.Text)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Runtime chat persona override: 爱心.",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		runtimePersonaPrimaryStyleRule,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"产品合伙人的助手",
	)
}

func TestHandleMessagePersonaPromptCreatesRuntimeOverride(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: "/persona 你是一个有爱心的人。",
		},
	})
	require.NoError(t, err)
	require.Contains(t, ms.lastText, "已保存并启用人格")

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "你好"},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(t, "你好", gw.lastReq.Text)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Runtime chat persona override: 有爱心的人.",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		runtimePersonaPrimaryStyleRule,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"有爱心的人",
	)
}

func TestHandleMessagePersonaSwitchesFromSavedPersona(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: "/persona save 爱心 " +
				"你是一个更像产品合伙人的助手。",
		},
	})
	require.NoError(t, err)

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "/persona concise"},
	})
	require.NoError(t, err)

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "你好"},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(t, "你好", gw.lastReq.Text)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Runtime chat persona override: concise.",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		runtimePersonaPrimaryStyleRule,
	)
	require.NotContains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"产品合伙人的助手",
	)
}

func TestHandleMessagePlacesRuntimePersonaNoteFirst(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateWebSocketChannel(t, gw)
	ch.sender = ms

	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)

	baseSessionID := buildSessionID("chat1", "user1")
	ch.sessionTracker.setPersona(baseSessionID, personaapi.ConciseID)
	ch.sessionTracker.setWorkspace(baseSessionID, repoDir)

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "把结果文档发回来"},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)

	workspaceNote := buildCodingWorkspaceNote(
		repoDir,
		ch.defaultCodingWorkspace,
		ch.codingScratchRoot,
	)
	personaIndex := strings.Index(
		gw.lastReq.RequestSystemPrompt,
		runtimePersonaOverridePromptHeader,
	)
	transportIndex := strings.Index(
		gw.lastReq.RequestSystemPrompt,
		wecomAIBotWebSocketTransportNote,
	)
	workspaceIndex := strings.Index(
		gw.lastReq.RequestSystemPrompt,
		workspaceNote,
	)
	require.NotEqual(t, -1, personaIndex)
	require.NotEqual(t, -1, transportIndex)
	require.NotEqual(t, -1, workspaceIndex)
	require.Less(t, personaIndex, transportIndex)
	require.Less(t, personaIndex, workspaceIndex)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		runtimePersonaHistoryOverrideRule,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		runtimePersonaConsistencyRule,
	)
}

func TestHandleMessageSanitizesDeepSeekReplyBoundaryToken(
	t *testing.T,
) {
	t.Parallel()

	gw := &recordingGateway{
		reply: "你好" + deepSeekEOSMarkerWide,
	}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.runtimeModelName = "deepseek-v3.2"

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "你好"},
	})
	require.NoError(t, err)
	require.Equal(t, "你好", ms.lastMarkdown)
}

func TestReplyContextPrefixUsesFriendlySingleLineWhenEnabled(
	t *testing.T,
) {
	t.Parallel()

	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)

	baseSessionID := buildSessionID("chat1", "user1")
	tracker := newSessionTracker()
	tracker.setPersona(baseSessionID, personaapi.CoachID)
	tracker.setWorkspace(baseSessionID, repoDir)
	runStatus := newRunStatusTracker()
	runStatus.start(baseSessionID, "req-1", defaultProcessingMessage)
	runStatus.setUsage(
		baseSessionID,
		"req-1",
		&gwclient.Usage{TotalTokens: 12345},
		200000,
	)
	runStatus.finish(
		baseSessionID,
		"req-1",
		defaultCompletedStatusSummary,
		"hello",
	)

	ch := &Channel{
		cfg: channelCfg{
			BotName: "Streambot2",
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(true),
			},
		},
		sessionTracker: tracker,
		personas:       personaapi.NewRegistry(t.TempDir()),
		runStatus:      runStatus,
	}

	prefix := ch.replyContextPrefix(baseSessionID)
	require.True(
		t,
		strings.HasPrefix(prefix, replyPrefixLeadMarker),
	)
	require.Contains(
		t,
		prefix,
		replyPrefixEmojiPersona+"人格：教练",
	)
	require.Contains(
		t,
		prefix,
		replyPrefixEmojiContext+"上下文：12.3K / 200K (6.2%)",
	)
	require.Contains(
		t,
		prefix,
		replyPrefixEmojiCommands+
			"常用："+helpKeyword+" "+personaKeyword+
			" "+statusKeyword,
	)
	require.Contains(
		t,
		prefix,
		replyPrefixEmojiHint+replyPrefixDefaultHint,
	)
	require.NotContains(t, prefix, "Streambot2")
	require.NotContains(
		t,
		prefix,
		repoDir,
	)
}

func TestReplyContextPrefixUsesConfiguredLinksAndCommands(
	t *testing.T,
) {
	t.Parallel()

	tracker := newSessionTracker()
	baseSessionID := buildSessionID("chat1", "user1")
	tracker.setPersona(baseSessionID, personaapi.ConciseID)

	ch := &Channel{
		cfg: channelCfg{
			BotName: "Streambot2",
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(true),
				Fields: []string{
					replyPrefixFieldLinks,
					replyPrefixFieldCommands,
					replyPrefixFieldPersona,
				},
				Links: []replyPrefixLinkCfg{
					{
						Label: "Web IDE",
						URL:   "https://ide.example.com",
					},
				},
				Commands: []string{newKeyword, helpKeyword},
			},
		},
		sessionTracker: tracker,
		personas:       personaapi.NewRegistry(t.TempDir()),
	}

	prefix := ch.replyContextPrefix(baseSessionID)
	require.Equal(
		t,
		replyPrefixLeadMarker+
			replyPrefixEmojiLink+
			"Web IDE: https://ide.example.com | "+
			replyPrefixEmojiCommands+
			"常用："+newKeyword+" "+helpKeyword+
			" | "+replyPrefixEmojiPersona+"人格：简洁",
		prefix,
	)
}

func TestReplyContextPrefixDisabledByDefault(t *testing.T) {
	t.Parallel()

	tracker := newSessionTracker()
	baseSessionID := buildSessionID("chat1", "user1")
	tracker.setPersona(baseSessionID, personaapi.ConciseID)

	ch := &Channel{
		cfg: channelCfg{
			ReplyPrefix: replyPrefixCfg{},
		},
		sessionTracker: tracker,
		personas:       personaapi.NewRegistry(t.TempDir()),
	}

	require.False(t, resolveReplyPrefixEnabled(ch.cfg.ReplyPrefix))

	prefix := ch.replyContextPrefix(baseSessionID)
	require.Empty(t, prefix)
}

func TestReplyContextPrefixWorkspaceLineUsesCompactLabel(
	t *testing.T,
) {
	t.Parallel()

	tracker := newSessionTracker()
	baseSessionID := buildSessionID("chat1", "user1")
	repoDir := filepath.Join(t.TempDir(), "openclaw")
	tracker.setWorkspace(baseSessionID, repoDir)

	ch := &Channel{
		cfg: channelCfg{
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(true),
				Fields:  []string{replyPrefixFieldWorkspace},
			},
		},
		sessionTracker: tracker,
	}

	prefix := ch.replyContextPrefix(baseSessionID)
	require.Equal(
		t,
		replyPrefixLeadMarker+
			replyPrefixEmojiWorkspace+"工作区：openclaw",
		prefix,
	)
}

func TestHandlePersonaTemplateCardEventQuickButton(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockSender{}
	tracker := newSessionTracker()
	ch := &Channel{
		sender:         sender,
		sessionTracker: tracker,
		personas:       personaapi.NewRegistry(t.TempDir()),
	}

	err := ch.handlePersonaTemplateCardEvent(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			From: FromInfo{
				UserID: "user1",
			},
		},
		&TemplateCardEvent{
			EventKey: personaCardQuickEventKey(
				personaapi.SnarkyID,
			),
			TaskID: "persona-task-quick",
		},
	)
	require.NoError(t, err)

	current := tracker.getOrCreateSession(
		buildSessionID("chat1", "user1"),
		0,
	)
	require.Equal(t, personaapi.SnarkyID, current.personaID)
	require.True(t, current.personaPinned)
	require.NotNil(t, sender.lastUpdatedCard)
	require.Contains(
		t,
		sender.lastUpdatedCard.SubTitleText,
		personaCardChangedNote,
	)
	require.Equal(
		t,
		"毒舌"+personaCardCurrentSuffix,
		sender.lastUpdatedCard.ButtonList[0].Text,
	)
}

func TestHandleMessageWorkspaceSetsRuntimeOverride(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(repoDir, agentsDocFileName),
			[]byte("repo instructions"),
			0o600,
		),
	)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.defaultCodingWorkspace = ""
	ch.codingScratchRoot = filepath.Join(t.TempDir(), "scratch")

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: workspaceKeyword + " " + repoDir,
		},
	})
	require.NoError(t, err)
	require.Contains(t, ms.lastText, "代码工作区已设置为")

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "帮我看看这个仓库"},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Runtime chat coding workspace override:",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Use "+repoDir+" as the default workdir",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Treat current repo, current workspace, or this repo",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Keep direct uploads and generated artifacts out",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Effective AGENTS.md: "+
			filepath.Join(repoDir, agentsDocFileName),
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Scratch repo root for standalone tasks: "+
			ch.codingScratchRoot,
	)
}

func TestHandleMessageRefreshesActiveCardAfterToolNameChange(
	t *testing.T,
) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms

	baseSessionID := buildSessionID("chat1", "user1")
	require.True(
		t,
		ch.sendHomeControlCard(
			context.Background(),
			"chat1",
			baseSessionID,
			ms,
		),
	)
	require.NotNil(t, ms.lastTemplateCard)

	toolImpl := setAssistantNameTool{stateDir: ch.stateDir}
	gw.onSend = func(
		_ context.Context,
		_ gwclient.MessageRequest,
	) {
		_, err := toolImpl.setGlobalName("阿爪")
		require.NoError(t, err)
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "你好"},
	})
	require.NoError(t, err)
	require.NotNil(t, ms.lastUpdatedCard)
	require.Equal(
		t,
		ms.lastTemplateCard.TaskID,
		ms.lastUpdatedCard.TaskID,
	)
	require.Contains(
		t,
		ms.lastUpdatedCard.MainTitle.Title,
		"阿爪 · "+controlCardTitleHome,
	)
}

func TestHandleMessageDoesNotInferWorkspaceNoteFromText(
	t *testing.T,
) {
	t.Parallel()

	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.defaultCodingWorkspace = repoDir
	ch.codingScratchRoot = filepath.Join(t.TempDir(), "scratch")

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: "帮我看下这个仓库的 Go 代码结构",
		},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.NotContains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Runtime default coding workspace:",
	)
	require.NotContains(t, gw.lastReq.RequestSystemPrompt, repoDir)
}

func TestHandleMessageCodeTaskFallsBackToUserAgentsDoc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalAgentsPath := filepath.Join(home, ".trpc-agent-go", "AGENTS.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(globalAgentsPath), 0o755))
	require.NoError(
		t,
		os.WriteFile(globalAgentsPath, []byte("global instructions"), 0o600),
	)

	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.defaultCodingWorkspace = ""
	baseSessionID := buildSessionID("chat1", "user1")
	ch.sessionTracker.setWorkspace(baseSessionID, repoDir)

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: "帮我看下这个仓库的 Go 代码结构",
		},
	})
	require.NoError(t, err)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Effective AGENTS.md: "+globalAgentsPath,
	)
}

func TestHandleMessageMixedImageUsesImageContentPart(t *testing.T) {
	t.Parallel()

	imageData := mustBase64Decode(t, testPNGBase64)
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(server.Close)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.mediaClient = server.Client()

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1",
		MsgType: MsgTypeMixed,
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MixedMessage: MixedMessageContent{
			MsgItem: []MixedMsgItem{
				{
					MsgType: MsgTypeImage,
					Image: ImageContent{
						URL: server.URL + "/image.png",
					},
				},
				{
					MsgType: MsgTypeText,
					Text: TextContent{
						Content: "这是什么？",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(
		t,
		expectedRequestText(
			"这是什么？",
			1,
			gw.lastReq.ContentParts,
		),
		gw.lastReq.Text,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		currentTurnAttachmentNote(1),
	)
	require.Len(t, gw.lastReq.ContentParts, 2)
	require.Equal(
		t,
		gwproto.PartTypeImage,
		gw.lastReq.ContentParts[0].Type,
	)
	require.NotNil(t, gw.lastReq.ContentParts[0].Image)
	require.Empty(t, gw.lastReq.ContentParts[0].Image.URL)
	require.Equal(
		t,
		imageFormatPNG,
		gw.lastReq.ContentParts[0].Image.Format,
	)
	require.Equal(
		t,
		imageData,
		gw.lastReq.ContentParts[0].Image.Data,
	)
	require.Equal(
		t,
		gwproto.PartTypeFile,
		gw.lastReq.ContentParts[1].Type,
	)
	require.NotNil(t, gw.lastReq.ContentParts[1].File)
	require.Equal(
		t,
		"image_0.png",
		gw.lastReq.ContentParts[1].File.Filename,
	)
	require.Equal(
		t,
		mimeTypePNG,
		gw.lastReq.ContentParts[1].File.Format,
	)
	require.Equal(
		t,
		imageData,
		gw.lastReq.ContentParts[1].File.Data,
	)
}

func TestHandleMessageMixedEmojiFallbackUsesImageContentPart(
	t *testing.T,
) {
	t.Parallel()

	imageData := mustBase64Decode(t, testGIFBase64)
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", mimeTypeGIF)
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(server.Close)

	rawBody, err := json.Marshal(map[string]any{
		"msgtype": MsgTypeMixed,
		"mixed": map[string]any{
			"msg_item": []map[string]any{
				{
					"msgtype": "emotion",
					"emotion": map[string]any{
						"url": server.URL + "/emoji.gif",
					},
				},
				{
					"msgtype": MsgTypeText,
					"text": map[string]any{
						"content": "这是什么",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.mediaClient = server.Client()

	err = ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1-emoji",
		MsgType: MsgTypeMixed,
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MixedMessage: MixedMessageContent{
			MsgItem: []MixedMsgItem{{
				MsgType: MsgTypeText,
				Text: TextContent{
					Content: "这是什么",
				},
			}},
		},
		RawBody: rawBody,
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(
		t,
		expectedRequestText(
			"这是什么",
			1,
			gw.lastReq.ContentParts,
		),
		gw.lastReq.Text,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		currentTurnAttachmentNote(1),
	)
	require.Len(t, gw.lastReq.ContentParts, 2)
	require.Equal(
		t,
		gwproto.PartTypeImage,
		gw.lastReq.ContentParts[0].Type,
	)
	require.NotNil(t, gw.lastReq.ContentParts[0].Image)
	require.Equal(
		t,
		imageFormatGIF,
		gw.lastReq.ContentParts[0].Image.Format,
	)
	require.Equal(
		t,
		imageData,
		gw.lastReq.ContentParts[0].Image.Data,
	)
	require.Equal(
		t,
		gwproto.PartTypeFile,
		gw.lastReq.ContentParts[1].Type,
	)
	require.NotNil(t, gw.lastReq.ContentParts[1].File)
	require.Equal(
		t,
		"image_0.gif",
		gw.lastReq.ContentParts[1].File.Filename,
	)
	require.Equal(
		t,
		mimeTypeGIF,
		gw.lastReq.ContentParts[1].File.Format,
	)
	require.Equal(
		t,
		imageData,
		gw.lastReq.ContentParts[1].File.Data,
	)
}

func TestHandleMessageImageOnlyUsesDefaultAnalyzeText(t *testing.T) {
	t.Parallel()

	imageData := mustBase64Decode(t, testPNGBase64)
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", mimeTypePNG)
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(server.Close)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.mediaClient = server.Client()

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m1-image-only",
		MsgType: MsgTypeImage,
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Image: ImageContent{
			URL: server.URL + "/image.png",
		},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(
		t,
		expectedRequestText(
			defaultImageAnalyzeText,
			1,
			gw.lastReq.ContentParts,
		),
		gw.lastReq.Text,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		currentTurnAttachmentNote(1),
	)
	require.Len(t, gw.lastReq.ContentParts, 2)
	require.Equal(
		t,
		gwproto.PartTypeImage,
		gw.lastReq.ContentParts[0].Type,
	)
	require.Equal(
		t,
		gwproto.PartTypeFile,
		gw.lastReq.ContentParts[1].Type,
	)
}

func TestHandleMessageMixedTextAndFileUsesFileContentPart(t *testing.T) {
	t.Parallel()

	fileData := []byte("%PDF-1.7\nhello")
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", mimeTypePDF)
		w.Header().Set(
			"Content-Disposition",
			`attachment; filename="report.pdf"`,
		)
		_, _ = w.Write(fileData)
	}))
	t.Cleanup(server.Close)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.mediaClient = server.Client()

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m2",
		MsgType: MsgTypeMixed,
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MixedMessage: MixedMessageContent{
			MsgItem: []MixedMsgItem{
				{
					MsgType: MsgTypeText,
					Text: TextContent{
						Content: "提取第一页文字",
					},
				},
				{
					MsgType: MsgTypeFile,
					File: FileContent{
						URL: server.URL + "/report",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(
		t,
		expectedRequestText(
			"提取第一页文字",
			1,
			gw.lastReq.ContentParts,
		),
		gw.lastReq.Text,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		currentTurnAttachmentNote(1),
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		runtimeSessionUploadsDirEnvName,
	)
	require.Len(t, gw.lastReq.ContentParts, 1)
	require.Equal(
		t,
		gwproto.PartTypeFile,
		gw.lastReq.ContentParts[0].Type,
	)
	require.NotNil(t, gw.lastReq.ContentParts[0].File)
	require.Empty(t, gw.lastReq.ContentParts[0].File.URL)
	require.Equal(
		t,
		"report.pdf",
		gw.lastReq.ContentParts[0].File.Filename,
	)
	require.Equal(
		t,
		mimeTypePDF,
		gw.lastReq.ContentParts[0].File.Format,
	)
	require.Equal(
		t,
		fileData,
		gw.lastReq.ContentParts[0].File.Data,
	)
}

func TestHandleMessageDisambiguatesDuplicateFileNames(
	t *testing.T,
) {
	t.Parallel()

	fileData := []byte("%PDF-1.7\nhello")
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", mimeTypeOctetStream)
		_, _ = w.Write(fileData)
	}))
	t.Cleanup(server.Close)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.mediaClient = server.Client()

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:   "m2-duplicate-files",
		MsgType: MsgTypeMixed,
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MixedMessage: MixedMessageContent{
			MsgItem: []MixedMsgItem{
				{
					MsgType: MsgTypeText,
					Text: TextContent{
						Content: "合并这两个 pdf",
					},
				},
				{
					MsgType: MsgTypeFile,
					File: FileContent{
						URL: server.URL + "/download-a",
					},
				},
				{
					MsgType: MsgTypeFile,
					File: FileContent{
						URL: server.URL + "/download-b",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(
		t,
		expectedRequestText(
			"合并这两个 pdf",
			2,
			gw.lastReq.ContentParts,
		),
		gw.lastReq.Text,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		currentTurnAttachmentNote(2),
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		attachmentNoteStart,
	)
	require.Len(t, gw.lastReq.ContentParts, 2)
	require.Equal(
		t,
		"attachment.pdf",
		gw.lastReq.ContentParts[0].File.Filename,
	)
	require.Equal(
		t,
		"attachment-2.pdf",
		gw.lastReq.ContentParts[1].File.Filename,
	)
}

func TestHandleMessageTextWithQuotedFileUsesFileContentPart(
	t *testing.T,
) {
	t.Parallel()

	fileData := []byte("%PDF-1.7\nhello")
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", mimeTypePDF)
		w.Header().Set(
			"Content-Disposition",
			`attachment; filename="report.pdf"`,
		)
		_, _ = w.Write(fileData)
	}))
	t.Cleanup(server.Close)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.mediaClient = server.Client()

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:    "m2-quoted-file",
		MsgType:  MsgTypeText,
		ChatID:   "chat1",
		ChatType: "group",
		From:     FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: "总结下第五页",
		},
		Quote: &QuoteContent{
			MsgType: MsgTypeFile,
			File: FileContent{
				URL: server.URL + "/report",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(
		t,
		expectedRequestText(
			"总结下第五页",
			1,
			gw.lastReq.ContentParts,
		),
		gw.lastReq.Text,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		currentTurnAttachmentNote(1),
	)
	require.Len(t, gw.lastReq.ContentParts, 1)
	require.Equal(
		t,
		gwproto.PartTypeFile,
		gw.lastReq.ContentParts[0].Type,
	)
	require.NotNil(t, gw.lastReq.ContentParts[0].File)
	require.Empty(t, gw.lastReq.ContentParts[0].File.URL)
	require.Equal(
		t,
		"report.pdf",
		gw.lastReq.ContentParts[0].File.Filename,
	)
	require.Equal(
		t,
		mimeTypePDF,
		gw.lastReq.ContentParts[0].File.Format,
	)
	require.Equal(
		t,
		fileData,
		gw.lastReq.ContentParts[0].File.Data,
	)
}

func TestHandleMessageTextWithQuotedMixedImageUsesContentPart(
	t *testing.T,
) {
	t.Parallel()

	imageData := mustBase64Decode(t, testPNGBase64)
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", mimeTypePNG)
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(server.Close)

	gw := &recordingGateway{reply: "ok"}
	ms := &mockSender{}
	ch := mustCreateChannelWithGW(t, gw)
	ch.sender = ms
	ch.mediaClient = server.Client()

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgID:    "m2-quoted-mixed",
		MsgType:  MsgTypeText,
		ChatID:   "chat1",
		ChatType: "group",
		From:     FromInfo{UserID: "user1"},
		Text: TextContent{
			Content: "看看引用里的图片",
		},
		Quote: &QuoteContent{
			MsgType: MsgTypeMixed,
			Mixed: MixedMessageContent{
				MsgItem: []MixedMsgItem{
					{
						MsgType: MsgTypeText,
						Text: TextContent{
							Content: "ignored",
						},
					},
					{
						MsgType: MsgTypeImage,
						Image: ImageContent{
							URL: server.URL + "/image.png",
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Equal(
		t,
		expectedRequestText(
			"看看引用里的图片",
			1,
			gw.lastReq.ContentParts,
		),
		gw.lastReq.Text,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		currentTurnAttachmentNote(1),
	)
	require.Len(t, gw.lastReq.ContentParts, 2)
	require.Equal(
		t,
		gwproto.PartTypeImage,
		gw.lastReq.ContentParts[0].Type,
	)
	require.Equal(
		t,
		gwproto.PartTypeFile,
		gw.lastReq.ContentParts[1].Type,
	)
}

func TestResolveImageDataDecryptsWecomPayload(t *testing.T) {
	t.Parallel()

	ch := mustCreateChannel(t)
	imageData := mustBase64Decode(t, testPNGBase64)
	encrypted := encryptWecomFileData(t, ch.crypt, imageData)

	data, format, err := ch.resolveImageData(
		"https://ww-aibot-img.example.com/image",
		encrypted,
		contentPartDecryptHint{},
	)
	require.NoError(t, err)
	require.Equal(t, imageData, data)
	require.Equal(t, imageFormatPNG, format)
}

func TestResolveFileDataDecryptsWecomPayload(t *testing.T) {
	t.Parallel()

	ch := mustCreateChannel(t)
	fileData := []byte("%PDF-1.7\nhello")
	encrypted := encryptWecomFileData(t, ch.crypt, fileData)

	data, mimeType, ext, err := ch.resolveFileData(
		"https://ww-aibot-img.example.com/file",
		fetchedMedia{
			contentType: mimeTypeOctetStream,
			data:        encrypted,
		},
		contentPartDecryptHint{},
	)
	require.NoError(t, err)
	require.Equal(t, fileData, data)
	require.Equal(t, mimeTypePDF, mimeType)
	require.Equal(t, ".pdf", ext)
}

func TestResolveImageDataDecryptsExplicitAESKey(t *testing.T) {
	t.Parallel()

	imageData := mustBase64Decode(t, testPNGBase64)
	encrypted := encryptWecomFileDataWithEncodingAESKey(
		t,
		testEncodingAESKey,
		imageData,
	)

	ch := &Channel{}
	data, format, err := ch.resolveImageData(
		"https://ww-aibot-img.example.com/image",
		encrypted,
		contentPartDecryptHint{
			AESKey: testEncodingAESKey,
		},
	)
	require.NoError(t, err)
	require.Equal(t, imageData, data)
	require.Equal(t, imageFormatPNG, format)
}

func TestResolveFileDataDecryptsExplicitAESKey(t *testing.T) {
	t.Parallel()

	fileData := []byte("%PDF-1.7\nhello")
	encrypted := encryptWecomFileDataWithEncodingAESKey(
		t,
		testEncodingAESKey,
		fileData,
	)

	ch := &Channel{}
	data, mimeType, ext, err := ch.resolveFileData(
		"https://ww-aibot-img.example.com/file",
		fetchedMedia{
			contentType: mimeTypeOctetStream,
			data:        encrypted,
		},
		contentPartDecryptHint{
			AESKey: testEncodingAESKey,
		},
	)
	require.NoError(t, err)
	require.Equal(t, fileData, data)
	require.Equal(t, mimeTypePDF, mimeType)
	require.Equal(t, ".pdf", ext)
}

func TestDownloadMediaRejectsUntrustedHost(t *testing.T) {
	t.Parallel()

	_, err := (&Channel{}).downloadMedia(
		context.Background(),
		"https://example.com/file",
	)
	require.ErrorContains(t, err, "untrusted download host")
}

func TestDetectFileTypeXLSX(t *testing.T) {
	t.Parallel()

	data := buildZipData(t, []string{
		"[Content_Types].xml",
		"xl/workbook.xml",
		"xl/worksheets/sheet1.xml",
	})

	mimeType, ext := detectFileType(data, mimeTypeZIP, "")
	require.Equal(t, mimeTypeXLSX, mimeType)
	require.Equal(t, ".xlsx", ext)
}

// --- HTTP handler tests ---

func TestHandleVerifyGET(t *testing.T) {
	t.Parallel()

	ch := mustCreateChannel(t)

	_, token, _ := testCredentials()

	echoContent := []byte("test_echo_string")
	encrypted, err := ch.crypt.encrypt(echoContent)
	require.NoError(t, err)

	timestamp := "1234567890"
	nonce := "testnonce"
	sig := buildSignature(token, timestamp, nonce, encrypted)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/wecom/callback?msg_signature=%s&timestamp=%s&nonce=%s&echostr=%s",
			sig, timestamp, nonce, url.QueryEscape(encrypted)),
		nil)
	w := httptest.NewRecorder()
	ch.handleHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, string(echoContent), w.Body.String())
}

func TestMountHTTPRegistersVerifyEndpoint(t *testing.T) {
	t.Parallel()

	ch := mustCreateChannel(t)

	require.Equal(
		t,
		ingress.DefaultHTTPServiceName,
		ch.HTTPServiceName(),
	)
	require.Equal(t, []string{"/wecom/callback"}, ch.HTTPPatterns())

	mux := http.NewServeMux()
	require.NoError(t, ch.MountHTTP(mux))

	echoContent := []byte("mounted_echo_string")
	encrypted, err := ch.crypt.encrypt(echoContent)
	require.NoError(t, err)

	timestamp := "1234567890"
	nonce := "testnonce"
	sig := buildSignature(testToken, timestamp, nonce, encrypted)

	req := httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf(
			"/wecom/callback?msg_signature=%s&timestamp=%s&nonce=%s&echostr=%s",
			sig,
			timestamp,
			nonce,
			url.QueryEscape(encrypted),
		),
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, string(echoContent), w.Body.String())
}

func TestHandleCallbackPOST(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "test reply"}
	ch := mustCreateChannelWithGW(t, gw)

	msg := WebhookMessage{
		MsgID:    "msg001",
		ChatID:   "chat001",
		ChatType: "group",
		From:     FromInfo{UserID: "user1", Name: "Test User"},
		MsgType:  "text",
		Text:     TextContent{Content: "@TestBot hello"},
	}
	msgBytes, _ := json.Marshal(msg)

	encrypted, err := ch.crypt.encrypt(msgBytes)
	require.NoError(t, err)

	_, token, _ := testCredentials()
	timestamp := "1234567890"
	nonce := "testnonce"
	sig := buildSignature(token, timestamp, nonce, encrypted)

	body, _ := json.Marshal(EncryptedBody{Encrypt: encrypted})

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/wecom/callback?msg_signature=%s&timestamp=%s&nonce=%s",
			sig, timestamp, nonce),
		strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	ch.handleHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	// processMessage is async, test it directly.
	err = ch.processMessage(context.Background(), sig, timestamp, nonce, body)
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
}

func TestHandleCallbackPOSTEnterChatReturnsWelcomeCard(t *testing.T) {
	t.Parallel()

	ch := mustCreateAIWebhookChannel(t, stubGateway{})
	ch.runtimeModelName = "gpt-5.2"

	msg := WebhookMessage{
		MsgType: MsgTypeEvent,
		From:    FromInfo{UserID: "user1"},
		Event: EventContent{
			EventType: eventTypeEnterChat,
		},
	}
	msgBytes, err := json.Marshal(msg)
	require.NoError(t, err)

	encrypted, err := ch.crypt.encrypt(msgBytes)
	require.NoError(t, err)

	_, token, _ := testCredentials()
	timestamp := "1234567890"
	nonce := "enter-chat-nonce"
	sig := buildSignature(token, timestamp, nonce, encrypted)

	body, err := json.Marshal(EncryptedBody{Encrypt: encrypted})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf(
			"/wecom/callback?msg_signature=%s&timestamp=%s&nonce=%s",
			sig,
			timestamp,
			nonce,
		),
		strings.NewReader(string(body)),
	)
	w := httptest.NewRecorder()
	ch.handleHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.NotEmpty(t, w.Body.String())
	require.NotEqual(t, "success", w.Body.String())

	var replyEnvelope encryptedReplyBody
	require.NoError(
		t,
		json.Unmarshal(w.Body.Bytes(), &replyEnvelope),
	)

	replyPlaintext, err := ch.crypt.DecryptMsg(
		replyEnvelope.MsgSignature,
		strconv.FormatInt(replyEnvelope.Timestamp, 10),
		replyEnvelope.Nonce,
		w.Body.Bytes(),
	)
	require.NoError(t, err)

	var reply callbackReplyBody
	require.NoError(t, json.Unmarshal(replyPlaintext, &reply))
	require.Equal(t, msgTypeTemplateCard, reply.MsgType)
	require.NotNil(t, reply.TemplateCard)
	require.Equal(
		t,
		templateCardTypeButtonInteraction,
		reply.TemplateCard.CardType,
	)
	require.Len(
		t,
		reply.TemplateCard.ButtonList,
		6,
	)
	require.Equal(
		t,
		"欢迎回来，先点需要的面板就行。",
		reply.TemplateCard.MainTitle.Desc,
	)
}

func TestHandleCallbackPOSTEnterChatWelcomeDisabled(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(
		t,
		testAIWebhookConfig()+`enter_chat_welcome: false
`,
	)
	chPlugin, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test-ai-webhook",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	ch := chPlugin.(*Channel)

	msg := WebhookMessage{
		MsgType: MsgTypeEvent,
		From:    FromInfo{UserID: "user1"},
		Event: EventContent{
			EventType: eventTypeEnterChat,
		},
	}
	msgBytes, err := json.Marshal(msg)
	require.NoError(t, err)

	encrypted, err := ch.crypt.encrypt(msgBytes)
	require.NoError(t, err)

	_, token, _ := testCredentials()
	timestamp := "1234567890"
	nonce := "enter-chat-nonce"
	sig := buildSignature(token, timestamp, nonce, encrypted)

	body, err := json.Marshal(EncryptedBody{Encrypt: encrypted})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf(
			"/wecom/callback?msg_signature=%s&timestamp=%s&nonce=%s",
			sig,
			timestamp,
			nonce,
		),
		strings.NewReader(string(body)),
	)
	w := httptest.NewRecorder()
	ch.handleHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Body.String())
}

func TestHandleMessageSkipsUnhandledEvent(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		gw:               stubGateway{},
		botMode:          botModeAI,
		sender:           ms,
		chatPolicy:       chatPolicyOpen,
		enterChatWelcome: true,
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: MsgTypeEvent,
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Event: EventContent{
			EventType: "unknown_event",
		},
	})
	require.NoError(t, err)
	require.Empty(t, ms.lastText)
}

// --- Sender tests ---

func TestWebhookSenderSendMarkdown(t *testing.T) {
	t.Parallel()

	var receivedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer ts.Close()

	sender := newWebhookSender(ts.URL, ts.Client())
	err := sender.SendMarkdown(context.Background(), "chat1", "**hello**")
	require.NoError(t, err)

	var payload webhookPayload
	require.NoError(t, json.Unmarshal(receivedBody, &payload))
	require.Equal(t, "markdown_v2", payload.MsgType)
	require.Equal(t, "**hello**", payload.MarkdownV2.Content)
}

func TestWebhookSenderSendText(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer ts.Close()

	sender := newWebhookSender(ts.URL, ts.Client())
	err := sender.SendText(context.Background(), "chat1", "hello")
	require.NoError(t, err)
}

func TestWebhookSenderHandlesError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":40001,"errmsg":"invalid token"}`))
	}))
	defer ts.Close()

	sender := newWebhookSender(ts.URL, ts.Client())
	err := sender.SendText(context.Background(), "chat1", "hello")
	require.ErrorContains(t, err, "errcode=40001")
}

func TestAIBotWebSocketSenderSendStreamWithFeedback(
	t *testing.T,
) {
	t.Parallel()

	writer := newAckWSWriter()
	sender := newAIBotWebSocketSender(writer, "req-1")

	err := sender.SendStreamWithFeedback(
		context.Background(),
		"chat1",
		"stream-1",
		streamNativeThinkingPlaceholder,
		false,
		"feedback-1",
	)
	require.NoError(t, err)
	require.Len(t, writer.frames, 1)

	frame := writer.frames[0]
	require.Equal(t, wsCommandRespond, frame.Command)
	body, ok := frame.Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, MsgTypeStream, body.MsgType)
	require.NotNil(t, body.Stream)
	require.Equal(t, "stream-1", body.Stream.ID)
	require.Equal(t, streamNativeThinkingPlaceholder, body.Stream.Content)
	require.False(t, body.Stream.Finish)
	require.NotNil(t, body.Stream.Feedback)
	require.Equal(t, "feedback-1", body.Stream.Feedback.ID)
}

func TestAIBotWebSocketSenderSendLocalFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reportPath := filepath.Join(root, "report.md")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	writer := newAckWSWriter()
	sender := newAIBotWebSocketSender(writer, "req-1")

	err := sender.SendLocalFile(
		context.Background(),
		"chat1",
		reportPath,
	)
	require.NoError(t, err)
	require.Len(t, writer.frames, 4)

	initFrame := writer.frames[0]
	require.Equal(t, wsCommandUploadMediaInit, initFrame.Command)
	initBody, ok := initFrame.Body.(wsUploadMediaInitBody)
	require.True(t, ok)
	require.Equal(t, MsgTypeFile, initBody.Type)
	require.Equal(t, "report.md", initBody.Filename)
	require.Equal(t, 5, initBody.TotalSize)
	require.Equal(t, 1, initBody.TotalChunks)
	require.NotEmpty(t, initBody.MD5)

	chunkFrame := writer.frames[1]
	require.Equal(t, wsCommandUploadMediaChunk, chunkFrame.Command)
	chunkBody, ok := chunkFrame.Body.(wsUploadMediaChunkBody)
	require.True(t, ok)
	require.Equal(t, "upload-1", chunkBody.UploadID)
	require.Equal(t, 0, chunkBody.ChunkIndex)
	require.Equal(
		t,
		base64.StdEncoding.EncodeToString([]byte("hello")),
		chunkBody.Base64Data,
	)

	finishFrame := writer.frames[2]
	require.Equal(t, wsCommandUploadMediaFinish, finishFrame.Command)

	replyFrame := writer.frames[3]
	require.Equal(t, wsCommandRespond, replyFrame.Command)
	replyBody, ok := replyFrame.Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, MsgTypeFile, replyBody.MsgType)
	require.Equal(t, "media-1", replyBody.File.MediaID)
}

func TestClassifyLocalReplyMediaImage(t *testing.T) {
	t.Parallel()

	media := classifyLocalReplyMedia("chart.png", []byte("img"))

	require.Equal(t, MsgTypeImage, media.MsgType)
	require.Equal(t, "chart.png", media.Filename)
}

func TestClassifyLocalReplyMediaNameOverrideKeepsSourceExt(
	t *testing.T,
) {
	t.Parallel()

	media := classifyLocalReplyMediaWithOptions(
		"/tmp/chart.png",
		[]byte("img"),
		localReplyMediaOptions{Filename: "report"},
	)

	require.Equal(t, MsgTypeImage, media.MsgType)
	require.Equal(t, "report.png", media.Filename)
}

func TestAIBotWebSocketSenderSendLocalImage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	imagePath := filepath.Join(root, "chart.png")
	require.NoError(
		t,
		os.WriteFile(imagePath, []byte("png"), 0o600),
	)

	writer := newAckWSWriter()
	sender := newAIBotWebSocketSender(writer, "req-1")

	err := sender.SendLocalFile(
		context.Background(),
		"chat1",
		imagePath,
	)
	require.NoError(t, err)

	replyFrame := writer.frames[len(writer.frames)-1]
	replyBody, ok := replyFrame.Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, MsgTypeImage, replyBody.MsgType)
	require.Equal(t, "media-1", replyBody.Image.MediaID)
}

func TestAIBotWebSocketSenderSendLocalFileNeedsRequestWriter(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	reportPath := filepath.Join(root, "report.md")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	sender := newAIBotWebSocketSender(newRecordingWSWriter(), "req-1")
	err := sender.SendLocalFile(
		context.Background(),
		"chat1",
		reportPath,
	)

	require.ErrorContains(t, err, "media upload not supported")
}

func TestSendWebSocketPushText(t *testing.T) {
	t.Parallel()

	writer := newAckWSWriter()

	err := sendWebSocketPushText(
		context.Background(),
		writer,
		pushTarget{
			ChatID:   "chat1",
			ChatType: chatTypeGroup,
		},
		"hello",
	)
	require.NoError(t, err)
	require.Len(t, writer.frames, 1)

	frame := writer.frames[0]
	require.Equal(t, wsCommandSend, frame.Command)
	require.NotEmpty(t, frame.Headers.ReqID)

	body, ok := frame.Body.(wsSendBody)
	require.True(t, ok)
	require.Equal(t, "chat1", body.ChatID)
	require.Equal(t, msgTypeMarkdown, body.MsgType)
	require.Equal(t, "hello", body.Markdown.Content)
}

func TestSendWebSocketPushTextIgnoresMentionPrefixes(t *testing.T) {
	t.Parallel()

	writer := newAckWSWriter()

	err := sendWebSocketPushText(
		context.Background(),
		writer,
		pushTarget{
			ChatID:           "chat1",
			ChatType:         chatTypeGroup,
			MentionedUserIDs: []string{"user2", "user3"},
		},
		"hello",
	)
	require.NoError(t, err)
	require.Len(t, writer.frames, 1)

	frame := writer.frames[0]
	body, ok := frame.Body.(wsSendBody)
	require.True(t, ok)
	require.Equal(
		t,
		"hello",
		body.Markdown.Content,
	)
}

func TestSendWebSocketPushMessageSendsTextAndImage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	imagePath := filepath.Join(root, "chart.png")
	require.NoError(
		t,
		os.WriteFile(imagePath, []byte("png"), 0o600),
	)

	writer := newAckWSWriter()
	err := sendWebSocketPushMessage(
		context.Background(),
		writer,
		pushTarget{
			ChatID:   "chat1",
			ChatType: chatTypeGroup,
		},
		occhannel.OutboundMessage{
			Text: "hello",
			Files: []occhannel.OutboundFile{{
				Path: imagePath,
				Name: "screen",
			}},
		},
	)
	require.NoError(t, err)
	require.Len(t, writer.frames, 5)

	textBody, ok := writer.frames[0].Body.(wsSendBody)
	require.True(t, ok)
	require.Equal(t, msgTypeMarkdown, textBody.MsgType)
	require.Equal(t, "hello", textBody.Markdown.Content)

	initBody, ok := writer.frames[1].Body.(wsUploadMediaInitBody)
	require.True(t, ok)
	require.Equal(t, MsgTypeImage, initBody.Type)
	require.Equal(t, "screen.png", initBody.Filename)

	mediaBody, ok := writer.frames[4].Body.(wsSendBody)
	require.True(t, ok)
	require.Equal(t, wsCommandSend, writer.frames[4].Command)
	require.Equal(t, MsgTypeImage, mediaBody.MsgType)
	require.Equal(t, "chat1", mediaBody.ChatID)
	require.Equal(t, "media-1", mediaBody.Image.MediaID)
}

func TestSendWebSocketPushMessageReturnsFileError(t *testing.T) {
	t.Parallel()

	writer := newAckWSWriter()
	err := sendWebSocketPushMessage(
		context.Background(),
		writer,
		pushTarget{
			ChatID:   "chat1",
			ChatType: chatTypeGroup,
		},
		occhannel.OutboundMessage{
			Files: []occhannel.OutboundFile{{
				Path: filepath.Join(t.TempDir(), "missing.png"),
			}},
		},
	)

	require.ErrorContains(t, err, "read reply file")
	require.Empty(t, writer.frames)
}

func TestChannelSendTextUsesLiveWebSocketWriter(t *testing.T) {
	t.Parallel()

	ch := mustCreateWebSocketChannel(t, stubGateway{})
	writer := newAckWSWriter()
	ch.setWebSocketPushWriter(writer)
	defer ch.clearWebSocketPushWriter(writer)

	err := ch.SendText(
		context.Background(),
		"group:chat1",
		"hello",
	)
	require.NoError(t, err)
	require.Len(t, writer.frames, 1)
	require.Equal(t, wsCommandSend, writer.frames[0].Command)
}

func TestChannelSendMessageUsesLiveWebSocketWriter(t *testing.T) {
	t.Parallel()

	ch := mustCreateWebSocketChannel(t, stubGateway{})
	writer := newAckWSWriter()
	ch.setWebSocketPushWriter(writer)
	defer ch.clearWebSocketPushWriter(writer)

	err := ch.SendMessage(
		context.Background(),
		"group:chat1",
		occhannel.OutboundMessage{Text: "hello"},
	)
	require.NoError(t, err)
	require.Len(t, writer.frames, 1)
	require.Equal(t, wsCommandSend, writer.frames[0].Command)
}

// --- Gateway error handling tests ---

func TestCallGatewayAndReply4xxSilent(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	gw := &errorGateway{
		statusCode: http.StatusBadRequest,
		errMsg:     "bad request",
	}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                gw,
		sender:            ms,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(context.Background(), WebhookMessage{
		ChatID: "chat1",
	}, "hello", nil, nil, "user1", "req1",
		"wecom:chat:chat1:1234567890", ms)
	// Should not return error for 4xx.
	require.NoError(t, err)
	// Should have sent an error message.
	require.NotEmpty(t, ms.lastMarkdown)
}

func TestCallGatewayAndReply5xxPropagates(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	gw := &errorGateway{
		statusCode: http.StatusInternalServerError,
		errMsg:     "internal error",
	}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                gw,
		sender:            ms,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(context.Background(), WebhookMessage{
		ChatID: "chat1",
	}, "hello", nil, nil, "user1", "req1",
		"wecom:chat:chat1:1234567890", ms)
	// Should propagate error for 5xx.
	require.Error(t, err)
}

func TestCallGatewayAndReplySanitizesGatewayErrorMessage(
	t *testing.T,
) {
	t.Parallel()

	const requestID = "req1"

	ms := &mockSender{}
	gw := &errorGateway{
		statusCode: http.StatusBadRequest,
		errMsg:     testProviderStreamError,
	}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                gw,
		sender:            ms,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(context.Background(), WebhookMessage{
		ChatID: "chat1",
	}, "hello", nil, nil, "user1", requestID,
		"wecom:chat:chat1:1234567890", ms)
	require.NoError(t, err)
	require.Equal(
		t,
		appendGatewayErrorID(
			testProviderStreamError,
			requestID,
		),
		ms.lastMarkdown,
	)
}

func TestCallGatewayAndReplyPreservesGatewayReplyText(
	t *testing.T,
) {
	t.Parallel()

	ms := &mockSender{}
	gw := &recordingGateway{
		reply: runnerExecutionErrorMessageEN,
	}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                gw,
		sender:            ms,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(context.Background(), WebhookMessage{
		ChatID: "chat1",
	}, "hello", nil, nil, "user1", "req1",
		"wecom:chat:chat1:1234567890", ms)
	require.NoError(t, err)
	require.Equal(
		t,
		runnerExecutionErrorMessageEN,
		ms.lastMarkdown,
	)
}

func TestCallGatewayAndReplyAlwaysAddsExternalLookupPromptRule(
	t *testing.T,
) {
	t.Parallel()

	ms := &mockSender{}
	gw := &recordingGateway{
		reply: "腾讯控股（00700.HK）最新价 412.60 港元。",
	}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                gw,
		sender:            ms,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"wecom:chat:chat1:1234567890",
		ms,
	)
	require.NoError(t, err)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		runtimeExternalLookupPromptRule,
	)
}

func TestCallGatewayAndReplyDoesNotRetryExternalLookupFallback(
	t *testing.T,
) {
	t.Parallel()

	const firstReply = "当前股价需要联网实时查询，告诉我看港股还是美股。"

	ms := &mockSender{}
	gw := &scriptedGateway{
		replies: []string{
			firstReply,
		},
	}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                gw,
		sender:            ms,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1"},
		"搜索下腾讯股票",
		nil,
		nil,
		"user1",
		"req1",
		"wecom:chat:chat1:1234567890",
		ms,
	)
	require.NoError(t, err)
	require.Equal(
		t,
		firstReply,
		ms.lastMarkdown,
	)
	require.Len(t, gw.requests, 1)
}

func TestCallGatewayAndReplyRefreshesContextPrefixOnFinalReply(
	t *testing.T,
) {
	t.Parallel()

	ms := &mockSender{}
	gw := &recordingGateway{
		reply: "hello",
		usage: &gwclient.Usage{TotalTokens: 12345},
	}
	ch := &Channel{
		cfg: channelCfg{
			BotName: "Bot",
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(true),
				Fields:  []string{replyPrefixFieldContext},
			},
		},
		gw:                gw,
		sender:            ms,
		runStatus:         newRunStatusTracker(),
		processingMessage: defaultProcessingMessage,
		runtimeModelName:  "gpt-5.2",
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"wecom:chat:chat1:1234567890",
		ms,
	)
	require.NoError(t, err)
	require.Contains(
		t,
		ms.lastMarkdown,
		replyPrefixEmojiContext+"上下文：12.3K / 400K (3.1%)",
	)
	require.Contains(t, ms.lastMarkdown, "hello")
}

func TestSanitizeGatewayErrorMessagePreservesSpecificMessage(
	t *testing.T,
) {
	t.Parallel()

	const requestID = "req1"

	require.Equal(
		t,
		appendGatewayErrorID("bad request", requestID),
		sanitizeGatewayErrorMessage("bad request", requestID),
	)
}

func TestSanitizeGatewayErrorMessagePreservesRawProviderText(
	t *testing.T,
) {
	t.Parallel()

	const requestID = "req1"

	require.Equal(
		t,
		appendGatewayErrorID(
			"unable to resolve target",
			requestID,
		),
		sanitizeGatewayErrorMessage(
			"unable to resolve target",
			requestID,
		),
	)
}

func TestAIBotPromptNotesForSendBackRequest(t *testing.T) {
	t.Parallel()

	ch := &Channel{botMode: botModeAI}
	got := ch.aiBotPromptNotes()

	require.Contains(t, got, wecomAIBotWebhookTransportNote)
	require.Contains(t, got, wecomAIBotWebhookSendBackNote)
}

func TestAIBotPromptNotesForWebSocketSendBackRequest(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{
		botMode:        botModeAI,
		connectionMode: connectionModeWebSocket,
	}

	got := ch.aiBotPromptNotes()

	require.Contains(
		t,
		got,
		wecomAIBotWebSocketTransportNote,
	)
	require.Contains(
		t,
		got,
		wecomAIBotWebSocketSendBackNote,
	)
	require.Contains(t, got, replyFileMarkerPrefix)
	require.Contains(
		t,
		got,
		"runtime artifact output root",
	)
	require.Contains(
		t,
		got,
		"`trpc-claw inspect deps`",
	)
	require.Contains(
		t,
		got,
		"explicit CJK-capable font",
	)
	require.Contains(
		t,
		got,
		"trpc-claw-doc-helper verify-pdf",
	)
	require.Contains(
		t,
		got,
		"trpc-claw-doc-helper ensure-fonts",
	)
	require.Contains(
		t,
		got,
		"trpc-claw-doc-helper ensure-tessdata chi_sim eng",
	)
}

func TestAIBotPromptNotesAlwaysIncludeDeliveryNote(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{botMode: botModeAI}
	got := ch.aiBotPromptNotes()

	require.Contains(t, got, wecomAIBotWebhookTransportNote)
	require.Contains(t, got, wecomAIBotWebhookSendBackNote)
}

func TestBuildRuntimeRequestSystemPromptAvoidsDuplicateNotes(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{
		botMode:                  botModeAI,
		connectionMode:           connectionModeWebSocket,
		codingArtifactOutputRoot: "/tmp/out",
		runtimeTempRoot:          "/tmp/tmp",
	}
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"",
		"",
		"",
		replyUXProfile{},
		nil,
	)
	require.Equal(
		t,
		1,
		strings.Count(got, wecomAIBotWebSocketTransportNote),
	)
	require.Equal(
		t,
		1,
		strings.Count(got, wecomAIBotWebSocketSendBackNote),
	)
	require.Contains(t, got, runtimeAssistantNameToolPromptRule)
	require.Equal(t, 1, strings.Count(got, "Current turn time:"))
	require.Contains(t, got, "Cron authoring:")
}

func TestBuildRuntimeRequestSystemPromptPreservesCustomTemplate(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{}
	ch.SetRequestSystemPromptTemplate(
		"${TRPC_CLAW_WECOM_CRON_AUTHORING_NOTE:-}",
	)
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"",
		"",
		"",
		replyUXProfile{},
		nil,
	)

	require.Contains(t, got, "Cron authoring:")
	require.NotContains(t, got, "Current turn time:")
}

func TestBuildRuntimeRequestSystemPromptRendersRuntimeRulesInTemplate(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{}
	ch.SetRequestSystemPromptTemplate(
		"${TRPC_CLAW_WECOM_RUNTIME_RULES:-}",
	)
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"",
		"",
		"",
		replyUXProfile{},
		nil,
	)

	require.Contains(t, got, "Cron authoring:")
	require.Equal(t, 1, strings.Count(got, "Current turn time:"))
}

func TestBuildRuntimeRequestSystemPromptRendersTimeNoteInTemplate(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{}
	ch.SetRequestSystemPromptTemplate(
		"${TRPC_CLAW_WECOM_CURRENT_TIME_NOTE:-}",
	)
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"",
		"",
		"",
		replyUXProfile{},
		nil,
	)

	require.NotContains(t, got, "Cron authoring:")
	require.Equal(t, 1, strings.Count(got, "Current turn time:"))
}

func TestBuildCurrentTimePromptNoteIncludesScheduleAnchor(
	t *testing.T,
) {
	t.Parallel()

	const (
		testZoneName       = "CST"
		testUTCOffsetHours = 8
		currentTimeText    = "2026-05-15T13:50:47+08:00"
	)
	location := time.FixedZone(
		testZoneName,
		testUTCOffsetHours*60*60,
	)
	now := time.Date(
		2026,
		time.May,
		15,
		13,
		50,
		47,
		0,
		location,
	)

	got := buildCurrentTimePromptNote(now)

	require.Contains(t, got, currentTimeText)
	require.Contains(t, got, "UTC offset: +08:00")
	require.Contains(t, got, "zone label: CST")
	require.Contains(t, got, "source of truth for now")
	require.Contains(t, got, "relative times in this turn")
	require.Contains(t, got, "creating schedules from")
	require.Contains(t, got, "use this current turn time as the anchor")
	require.Contains(t, got, "numeric UTC offset")
}

func TestBuildRuntimeRequestSystemPromptRendersLookupNoteInTemplate(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{}
	ch.SetRequestSystemPromptTemplate(
		"${TRPC_CLAW_WECOM_EXTERNAL_LOOKUP_NOTE:-}",
	)
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"",
		"",
		"hello",
		replyUXProfile{},
		nil,
	)
	require.Contains(t, got, runtimeExternalLookupPromptRule)
	require.NotContains(t, got, "Current turn time:")
}

func TestBuildRuntimeRequestSystemPromptIncludesAssistantAlias(
	t *testing.T,
) {
	t.Parallel()

	baseSessionID := "wecom:dm:user1"
	tracker := newSessionTracker()
	info := tracker.setAssistantAlias(baseSessionID, "彪子")

	ch := &Channel{
		sessionTracker: tracker,
		cfg: channelCfg{
			BotName: "TestBot",
		},
	}
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		info.sessionID,
		"",
		"",
		replyUXProfile{},
		nil,
	)
	require.Contains(
		t,
		got,
		"For this chat, your current name is 彪子.",
	)
	require.Contains(
		t,
		got,
		"overrides the global assistant name",
	)
}

func TestBuildRuntimeRequestSystemPromptUsesLegacyThreadAlias(
	t *testing.T,
) {
	t.Parallel()

	tracker := newSessionTracker()
	tracker.sessions["wecom:thread:wecom:chat:group1"] = &sessionInfo{
		baseSessionID:  "wecom:thread:wecom:chat:group1",
		sessionID:      "wecom:thread:wecom:chat:group1",
		assistantAlias: "奥特曼",
		history: []sessionHistoryEntry{{
			SessionID: "wecom:thread:wecom:chat:group1",
		}},
	}

	ch := &Channel{
		sessionTracker: tracker,
		cfg: channelCfg{
			BotName: "TestBot",
		},
	}
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"wecom:chat:group1:123",
		"",
		"",
		replyUXProfile{},
		nil,
	)
	require.Contains(
		t,
		got,
		"For this chat, your current name is 奥特曼.",
	)
}

func TestBuildRuntimeRequestSystemPromptIncludesCronAuthoringNote(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{}
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"",
		"",
		"",
		replyUXProfile{},
		nil,
	)

	require.Contains(t, got, "Cron authoring:")
	require.Contains(
		t,
		got,
		"replies with exactly that text",
	)
	require.Contains(
		t,
		got,
		"faithful to the user's original request",
	)
	require.Contains(
		t,
		got,
		"Preserve the stated scope, recipients, time "+
			"windows, and checklist items.",
	)
	require.Contains(
		t,
		got,
		"collapsing them into a shorter todo summary",
	)
}

func TestBuildRuntimeRequestSystemPromptIncludesBrowserRuntimeNote(
	t *testing.T,
) {
	t.Setenv(
		runtimehint.BrowserRuntimeEnvName,
		"/tmp/trpc-claw-browser-runtime",
	)
	t.Setenv(
		runtimehint.BrowserModeEnvName,
		runtimehint.BrowserModeHeadless,
	)

	ch := &Channel{}
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"",
		"",
		"",
		replyUXProfile{},
		nil,
	)

	require.Contains(t, got, "Managed browser runtime:")
	require.Contains(t, got, "Prefer `web_fetch`")
	require.Contains(t, got, "trpc-claw-browser-runtime mcp-stdio")
	require.Contains(t, got, "headless browser automation")
	require.Contains(t, got, runtimehint.BrowserModeEnvName)
	require.Contains(t, got, runtimehint.BrowserPathEnvName)
	require.Contains(
		t,
		got,
		"Treat prior browser or MCP failures in chat history",
	)
}

func TestBuildRuntimeRequestSystemPromptIncludesBrowserDoctorFact(
	t *testing.T,
) {
	helperPath := filepath.Join(
		t.TempDir(),
		runtimehint.BrowserRuntimeName,
	)
	err := os.WriteFile(
		helperPath,
		[]byte(
			"#!/bin/sh\n"+
				"if [ \"$1\" != \"doctor\" ]; then\n"+
				"  exit 1\n"+
				"fi\n"+
				"cat <<'EOF'\n"+
				"doctor_status=ready\n"+
				"lane=playwright_mcp_stdio\n"+
				"mode=headless\n"+
				"browser=chromium\n"+
				"browser_path=/usr/bin/chromium-browser\n"+
				"doctor_detail=ready\n"+
				"EOF\n",
		),
		0o755,
	)
	require.NoError(t, err)
	t.Setenv(
		runtimehint.BrowserRuntimeEnvName,
		helperPath,
	)
	t.Setenv(
		runtimehint.BrowserModeEnvName,
		runtimehint.BrowserModeHeadless,
	)

	ch := &Channel{}
	got := ch.buildRuntimeRequestSystemPrompt(
		context.Background(),
		"",
		"",
		"",
		replyUXProfile{},
		nil,
	)

	require.Contains(t, got, "Current turn browser runtime fact:")
	require.Contains(t, got, "doctor_status=ready")
	require.Contains(t, got, "lane=playwright_mcp_stdio")
	require.Contains(
		t,
		got,
		"attempt the browser tool instead of repeating",
	)
}

func TestBuildSpeakerScopedMemoryPromptNote(t *testing.T) {
	t.Parallel()

	got := buildSpeakerScopedMemoryPromptNote("T00010001", "alice.dev")
	require.Contains(
		t,
		got,
		"WeCom shared-chat speaker memory: current speaker for this turn is alice.dev (user_id=T00010001).",
	)
	require.Contains(
		t,
		got,
		"`- [speaker:T00010001] reply in classical Chinese.`",
	)
	require.Contains(t, got, "durable memory")
	require.NotContains(t, got, "MEMORY.md")
	require.Contains(
		t,
		got,
		"ignore speaker-scoped bullets written for other participants",
	)

	require.Empty(t, buildSpeakerScopedMemoryPromptNote("", ""))
}

func TestBuildReplyUXPromptNotesIncludesImageHandlingNote(
	t *testing.T,
) {
	t.Parallel()

	notes := buildReplyUXPromptNotes(buildReplyUXProfile(
		[]gwproto.ContentPart{
			{Type: gwproto.PartTypeImage},
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					Filename: "image_0.png",
				},
			},
		},
	))

	require.Contains(
		t,
		notes,
		"Raster images from this turn are already available",
	)
	require.Contains(
		t,
		notes,
		"prefer the image part as the primary input",
	)
}

func TestHandleMessageUsesResolvedIdentityLabel(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	writeIdentityLookupSkill(
		t,
		filepath.Join(
			stateDir,
			skillsDirName,
			localSkillsDirName,
			"identity-lookup",
		),
	)
	cachePath := userIdentityCachePath(stateDir)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(cachePath), 0o755),
	)
	cacheState := userIdentityCacheState{
		Version: userIdentityCacheVersion,
		Users: map[string]userIdentity{
			"T00010001": {
				UserID:      "T00010001",
				AccountName: "alice.dev",
				DisplayName: "郭琪周",
				UpdatedAt:   time.Now(),
			},
			"T00010002": {
				UserID:      "T00010002",
				AccountName: "bob.dev",
				DisplayName: "张子良",
				UpdatedAt:   time.Now(),
			},
		},
	}
	cacheData, err := json.Marshal(cacheState)
	require.NoError(t, err)
	require.NoError(
		t,
		os.WriteFile(cachePath, cacheData, 0o600),
	)

	gw := &recordingGateway{reply: "ok"}
	cfg := mustYAMLNode(
		t,
		testConfig()+"user_label_mode: alias_or_name\n",
	)
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  gw,
			StateDir: stateDir,
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	ch.(*Channel).sender = &mockSender{}
	ch.(*Channel).sessionTracker.recordKnownUsers(
		"wecom:chat:chat1",
		[]string{"T00010002"},
	)

	err = ch.(*Channel).handleMessage(
		context.Background(),
		WebhookMessage{
			MsgID:   "m1",
			MsgType: MsgTypeText,
			ChatID:  "chat1",
			From: FromInfo{
				UserID: "T00010001",
			},
			Text: TextContent{Content: "你好"},
		},
	)
	require.NoError(t, err)

	annotation, ok, err := conversation.
		AnnotationFromRequestExtensions(
			gw.lastReq.Extensions,
		)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "alice.dev", annotation.ActorLabel)
	require.Equal(t, "wecom:dm:alice.dev", gw.lastReq.UserID)
	require.Equal(t, "wecom:chat:chat1", gw.lastReq.SessionID)
	require.Equal(t, "wecom:chat:chat1", annotation.StorageUserID)
	require.Equal(
		t,
		map[string]string{
			"T00010001": "alice.dev",
			"T00010002": "bob.dev",
		},
		annotation.ActorLabels,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"- T00010001 => alice.dev",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"- T00010002 => bob.dev",
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"Use the mapped label exactly when referring to a participant",
	)
}

func TestHandleMessageCanonicalizesParticipantMentions(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	writeIdentityLookupSkill(
		t,
		filepath.Join(
			stateDir,
			skillsDirName,
			localSkillsDirName,
			"identity-lookup",
		),
	)
	cachePath := userIdentityCachePath(stateDir)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(cachePath), 0o755),
	)
	cacheState := userIdentityCacheState{
		Version: userIdentityCacheVersion,
		Users: map[string]userIdentity{
			"T00010001": {
				UserID:      "T00010001",
				AccountName: "wineguo",
				DisplayName: "郭琪周",
				UpdatedAt:   time.Now(),
			},
			"T00010002": {
				UserID:      "T00010002",
				AccountName: "zeronezhang",
				DisplayName: "张子良",
				UpdatedAt:   time.Now(),
			},
		},
	}
	cacheData, err := json.Marshal(cacheState)
	require.NoError(t, err)
	require.NoError(
		t,
		os.WriteFile(cachePath, cacheData, 0o600),
	)

	gw := &recordingGateway{reply: "ok"}
	cfg := mustYAMLNode(
		t,
		testConfig()+"user_label_mode: alias_or_name\n",
	)
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  gw,
			StateDir: stateDir,
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	ch.(*Channel).sender = &mockSender{}
	ch.(*Channel).sessionTracker.recordKnownUsers(
		"wecom:chat:chat1",
		[]string{"T00010002"},
	)

	err = ch.(*Channel).handleMessage(
		context.Background(),
		WebhookMessage{
			MsgID:   "m1",
			MsgType: MsgTypeText,
			ChatID:  "chat1",
			From: FromInfo{
				UserID: "T00010001",
			},
			Text: TextContent{
				Content: "@TestBot 每 10s 定时给 " +
					"@zeronezhang(张子良) 发一句随机发财话",
			},
		},
	)
	require.NoError(t, err)
	require.Contains(
		t,
		gw.lastReq.Text,
		"@zeronezhang 发一句随机发财话",
	)
	require.NotContains(t, gw.lastReq.Text, "张子良")
}

func TestHandleMessageUsesIdentityCacheWithoutCapability(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	cachePath := userIdentityCachePath(stateDir)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(cachePath), 0o755),
	)
	cacheState := userIdentityCacheState{
		Version: userIdentityCacheVersion,
		Users: map[string]userIdentity{
			"T00010001": {
				UserID:      "T00010001",
				AccountName: "alice.dev",
				DisplayName: "Alice",
				UpdatedAt:   time.Now(),
			},
		},
	}
	cacheData, err := json.Marshal(cacheState)
	require.NoError(t, err)
	require.NoError(
		t,
		os.WriteFile(cachePath, cacheData, 0o600),
	)

	gw := &recordingGateway{reply: "ok"}
	cfg := mustYAMLNode(
		t,
		testConfig()+"user_label_mode: alias_or_name\n",
	)
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  gw,
			StateDir: stateDir,
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	channel := ch.(*Channel)
	channel.sender = &mockSender{}
	require.NotNil(t, channel.identityResolver)

	err = channel.handleMessage(
		context.Background(),
		WebhookMessage{
			MsgID:   "m1",
			MsgType: MsgTypeText,
			ChatID:  "chat1",
			From: FromInfo{
				UserID: "T00010001",
			},
			Text: TextContent{Content: "你好"},
		},
	)
	require.NoError(t, err)

	annotation, ok, err := conversation.
		AnnotationFromRequestExtensions(
			gw.lastReq.Extensions,
		)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "alice.dev", annotation.ActorLabel)
	require.Equal(
		t,
		map[string]string{"T00010001": "alice.dev"},
		annotation.ActorLabels,
	)
	require.Contains(
		t,
		gw.lastReq.RequestSystemPrompt,
		"T00010001 => alice.dev",
	)
}

func TestCapabilityCommandPathUsesFrontMatterMetadata(
	t *testing.T,
) {
	t.Parallel()

	skillDir := filepath.Join(t.TempDir(), "identity-lookup")
	writeIdentityLookupSkill(t, skillDir)
	linuxPath := filepath.Join(
		skillDir,
		"scripts",
		"rtx_user_linux",
	)

	path, ok := capabilityCommandPath(
		skillDir,
		wecomUserLookupCapability,
		platformLinux,
	)
	require.True(t, ok)
	require.Equal(t, linuxPath, path)
}

func TestResolveUserIdentityLookupCommandPrefersConfig(
	t *testing.T,
) {
	t.Parallel()

	stateDir := t.TempDir()
	fallbackDir := filepath.Join(
		stateDir,
		skillsDirName,
		localSkillsDirName,
		"identity-lookup",
	)
	writeIdentityLookupSkill(t, fallbackDir)

	commandDir := t.TempDir()
	commandPath := filepath.Join(commandDir, "lookup")
	require.NoError(
		t,
		os.WriteFile(commandPath, []byte("#!/bin/sh\n"), 0o755),
	)

	got := resolveUserIdentityLookupCommand(
		stateDir,
		commandPath,
	)
	require.Equal(t, commandPath, got)
}

func writeIdentityLookupSkill(t *testing.T, skillDir string) {
	t.Helper()

	scriptDir := filepath.Join(skillDir, "scripts")
	require.NoError(t, os.MkdirAll(scriptDir, 0o755))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(skillDir, skillFileBaseName),
			[]byte(`---
name: identity-lookup
description: lookup
metadata:
  openclaw:
    capabilities:
      wecom_user_lookup:
        linux: scripts/rtx_user_linux
        darwin: scripts/rtx_user_darwin
---
`),
			0o600,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(scriptDir, "rtx_user_linux"),
			[]byte("#!/bin/sh\n"),
			0o755,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(scriptDir, "rtx_user_darwin"),
			[]byte("#!/bin/sh\n"),
			0o755,
		),
	)
}

func TestBuildReplyDeliveryPlanKeepsWorkspaceFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reportPath := filepath.Join(root, "report.md")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	ch := &Channel{
		defaultCodingWorkspace: root,
		codingScratchRoot:      filepath.Join(root, "scratch"),
	}

	plan := ch.buildReplyDeliveryPlan(
		"session1",
		"已生成。\n"+
			replyFileMarkerPrefix+
			reportPath+
			replyFileMarkerSuffix,
	)

	require.Equal(t, "已生成。", plan.cleanReply)
	require.Equal(t, 1, plan.requested)
	require.Equal(t, []string{reportPath}, plan.paths)
}

func TestBuildReplyDeliveryPlanAcceptsMediaLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reportPath := filepath.Join(root, "voice.amr")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	ch := &Channel{
		defaultCodingWorkspace: root,
	}

	plan := ch.buildReplyDeliveryPlan(
		"session1",
		"语音已生成。\nMEDIA:"+reportPath,
	)

	require.Equal(t, "语音已生成。", plan.cleanReply)
	require.Equal(t, 1, plan.requested)
	require.Equal(t, []string{reportPath}, plan.paths)
}

func TestBuildReplyDeliveryPlanAcceptsRuntimeReplyRoot(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	replyRoot := filepath.Join(t.TempDir(), "exports")
	require.NoError(t, os.MkdirAll(replyRoot, 0o755))
	reportPath := filepath.Join(replyRoot, "report.md")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	ch := &Channel{
		defaultCodingWorkspace: root,
		runtimeReplyDeliveryRoots: []string{
			replyRoot,
		},
	}

	plan := ch.buildReplyDeliveryPlan(
		"session1",
		replyFileMarkerPrefix+
			reportPath+
			replyFileMarkerSuffix,
	)

	require.Equal(t, 1, plan.requested)
	require.Equal(t, []string{reportPath}, plan.paths)
}

func TestBuildReplyDeliveryPlanAcceptsRuntimeTempRoot(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	tempRoot := filepath.Join(t.TempDir(), "runtime-tmp")
	require.NoError(t, os.MkdirAll(tempRoot, 0o755))
	reportPath := filepath.Join(tempRoot, "report.md")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	ch := &Channel{
		defaultCodingWorkspace: root,
		runtimeTempRoot:        tempRoot,
	}

	plan := ch.buildReplyDeliveryPlan(
		"session1",
		replyFileMarkerPrefix+
			reportPath+
			replyFileMarkerSuffix,
	)

	require.Equal(t, 1, plan.requested)
	require.Equal(t, []string{reportPath}, plan.paths)
}

func TestBuildReplyDeliveryPlanAcceptsManagedUploadsRoot(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	uploadRoot := filepath.Join(t.TempDir(), "uploads")
	require.NoError(t, os.MkdirAll(uploadRoot, 0o755))
	reportPath := filepath.Join(uploadRoot, "report.md")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	ch := &Channel{
		defaultCodingWorkspace:    root,
		runtimeManagedUploadsRoot: uploadRoot,
	}

	plan := ch.buildReplyDeliveryPlan(
		"session1",
		replyFileMarkerPrefix+
			reportPath+
			replyFileMarkerSuffix,
	)

	require.Equal(t, 1, plan.requested)
	require.Equal(t, []string{reportPath}, plan.paths)
}

func TestBuildReplyDeliveryPlanRecoversScratchOutputFile(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	scratchRoot := filepath.Join(t.TempDir(), "scratch")
	outputDir := filepath.Join(
		scratchRoot,
		replyDeliveryOutputDirName,
	)
	require.NoError(t, os.MkdirAll(outputDir, 0o755))
	reportPath := filepath.Join(outputDir, "sample_demo.pdf")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	ch := &Channel{
		defaultCodingWorkspace: root,
		codingScratchRoot:      scratchRoot,
	}

	plan := ch.buildReplyDeliveryPlan(
		"session1",
		replyFileMarkerPrefix+
			filepath.Join(root, "sample_demo.pdf")+
			replyFileMarkerSuffix,
	)

	require.Equal(t, 1, plan.requested)
	require.Equal(t, []string{reportPath}, plan.paths)
	require.Empty(t, plan.issues)
}

func TestBuildReplyDeliveryPlanRejectsAmbiguousFallback(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	scratchRoot := filepath.Join(t.TempDir(), "scratch")
	replyRoot := filepath.Join(t.TempDir(), "exports")
	require.NoError(
		t,
		os.MkdirAll(
			filepath.Join(scratchRoot, replyDeliveryOutputDirName),
			0o755,
		),
	)
	require.NoError(t, os.MkdirAll(replyRoot, 0o755))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(
				scratchRoot,
				replyDeliveryOutputDirName,
				"sample_demo.pdf",
			),
			[]byte("one"),
			0o600,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(replyRoot, "sample_demo.pdf"),
			[]byte("two"),
			0o600,
		),
	)

	ch := &Channel{
		defaultCodingWorkspace: root,
		codingScratchRoot:      scratchRoot,
		runtimeReplyDeliveryRoots: []string{
			replyRoot,
		},
	}

	plan := ch.buildReplyDeliveryPlan(
		"session1",
		replyFileMarkerPrefix+
			filepath.Join(root, "sample_demo.pdf")+
			replyFileMarkerSuffix,
	)

	require.Equal(t, 1, plan.requested)
	require.Empty(t, plan.paths)
	require.Len(t, plan.issues, 1)
	require.Contains(
		t,
		plan.issues[0].note,
		"找到多个同名待回传文件",
	)
}

func TestBuildReplyDeliveryPlanRejectsOutsideWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	otherDir := t.TempDir()
	reportPath := filepath.Join(otherDir, "report.md")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	ch := &Channel{defaultCodingWorkspace: root}
	plan := ch.buildReplyDeliveryPlan(
		"session1",
		replyFileMarkerPrefix+
			reportPath+
			replyFileMarkerSuffix,
	)

	require.Equal(t, 1, plan.requested)
	require.Empty(t, plan.paths)
}

func TestNormalizeReplyDeliveryPathRequiresAbsolutePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, err := normalizeReplyDeliveryPath("report.md", []string{root})

	require.ErrorContains(t, err, "must be absolute")
}

func TestNormalizeReplyDeliveryPathRejectsSymlinkFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outsideDir := t.TempDir()
	targetPath := filepath.Join(outsideDir, "report.md")
	linkPath := filepath.Join(root, "report.md")
	require.NoError(
		t,
		os.WriteFile(targetPath, []byte("hello"), 0o600),
	)
	require.NoError(t, os.Symlink(targetPath, linkPath))

	_, err := normalizeReplyDeliveryPath(linkPath, []string{root})

	require.ErrorIs(t, err, errReplyDeliveryPathSymlink)
}

func TestNormalizeReplyDeliveryPathRejectsSymlinkAncestorEscape(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	outsideDir := t.TempDir()
	linkDir := filepath.Join(root, "link")
	targetPath := filepath.Join(outsideDir, "report.md")
	require.NoError(
		t,
		os.WriteFile(targetPath, []byte("hello"), 0o600),
	)
	require.NoError(t, os.Symlink(outsideDir, linkDir))

	_, err := normalizeReplyDeliveryPath(
		filepath.Join(linkDir, "report.md"),
		[]string{root},
	)

	require.ErrorIs(t, err, errReplyDeliveryPathOutsideRoots)
}

func TestPathWithinRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	child := filepath.Join(root, "sub", "report.md")
	require.True(t, pathWithinRoot(child, root))
	require.False(t, pathWithinRoot("/tmp/report.md", root))
}

func TestSendReplyDeliveryFilesWithoutLocalFileSender(t *testing.T) {
	t.Parallel()

	ch := &Channel{}
	outcome := ch.sendReplyDeliveryFiles(
		context.Background(),
		&mockSender{},
		"chat1",
		replyDeliveryPlan{
			requested: 1,
			paths:     []string{"/tmp/report.md"},
		},
		nil,
	)

	require.Equal(t, 1, outcome.requested)
	require.Equal(t, 0, outcome.sent)
	require.Equal(t, 1, outcome.failed)
	require.Contains(
		t,
		formatReplyDeliveryIssues(outcome.issues),
		replyDeliverySenderNote,
	)
}

func TestFinalizeReplyDeliveryText(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		replyDeliverySuccessMessage,
		finalizeReplyDeliveryText(
			"",
			replyDeliveryOutcome{
				requested: 1,
				sent:      1,
			},
		),
	)
	require.Contains(
		t,
		finalizeReplyDeliveryText(
			"done",
			replyDeliveryOutcome{
				requested: 1,
				failed:    1,
				issues: []replyDeliveryIssue{
					{
						code: replyDeliveryIssueSend,
						note: "report.bin 25.0 MB，超过企微普通文件 20.0 MB 上限。",
					},
				},
			},
		),
		replyDeliveryFailedMessage,
	)
	require.Contains(
		t,
		finalizeReplyDeliveryText(
			"done",
			replyDeliveryOutcome{
				requested: 1,
				failed:    1,
				issues: []replyDeliveryIssue{
					{
						code: replyDeliveryIssueSend,
						note: "report.bin 25.0 MB，超过企微普通文件 20.0 MB 上限。",
					},
				},
			},
		),
		replyDeliveryReasonHeader,
	)
	require.Contains(
		t,
		finalizeReplyDeliveryText(
			"done",
			replyDeliveryOutcome{
				requested: 2,
				sent:      1,
				failed:    1,
			},
		),
		replyDeliveryPartialMessage,
	)
	require.Contains(
		t,
		finalizeReplyDeliveryText(
			"done",
			replyDeliveryOutcome{
				requested: 1,
				failed:    1,
				issues: []replyDeliveryIssue{
					{
						code: replyDeliveryIssueMissing,
						note: "未找到要回传的文件：sample_demo.pdf。",
					},
				},
			},
		),
		replyDeliveryUnverifiedFileMessage,
	)
	require.Equal(
		t,
		replyDeliveryOutcomeNote(
			replyDeliveryUnverifiedFileMessage,
			[]replyDeliveryIssue{
				{
					code: replyDeliveryIssueMissing,
					note: "未找到要回传的文件：sample_demo.pdf。",
				},
			},
		),
		finalizeReplyDeliveryText(
			"我给你随便生成了几份示例文件。",
			replyDeliveryOutcome{
				requested: 1,
				failed:    1,
				issues: []replyDeliveryIssue{
					{
						code: replyDeliveryIssueMissing,
						note: "未找到要回传的文件：sample_demo.pdf。",
					},
				},
			},
		),
	)
}

func TestAppendReplyDeliveryNoteAvoidsDuplicates(t *testing.T) {
	t.Parallel()

	reply := appendReplyDeliveryNote(
		replyDeliveryFailedMessage,
		replyDeliveryFailedMessage,
	)

	require.Equal(t, replyDeliveryFailedMessage, reply)
}

func TestValidateLocalReplyMediaRejectsOversize(t *testing.T) {
	t.Parallel()

	err := validateLocalReplyMedia(localReplyMedia{
		MsgType:  MsgTypeFile,
		Filename: "report.bin",
		Data:     make([]byte, replyFileMaxBytes+1),
	})

	require.ErrorContains(t, err, "exceeds size limit")
}

func TestReplyMediaLimitErrorAddsVideoGuidance(t *testing.T) {
	t.Parallel()

	err := validateLocalReplyMedia(localReplyMedia{
		MsgType:    MsgTypeFile,
		Filename:   "clip.mov",
		Data:       make([]byte, replyFileMaxBytes+1),
		HintedType: MsgTypeVideo,
	})

	var limitErr *replyMediaLimitError
	require.ErrorAs(t, err, &limitErr)
	require.Contains(t, limitErr.UserNote(), "转成 mp4")
}

func TestLoadLocalReplyMediaRejectsEmptyFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reportPath := filepath.Join(root, "empty.txt")
	require.NoError(t, os.WriteFile(reportPath, nil, 0o600))

	_, err := loadLocalReplyMedia(reportPath)

	require.ErrorContains(t, err, "reply file is empty")
}

func TestSendUploadedReplyMediaVideo(t *testing.T) {
	t.Parallel()

	writer := newRecordingWSWriter()
	sender := newAIBotWebSocketSender(writer, "req-1")

	err := sender.sendUploadedReplyMedia(
		context.Background(),
		localReplyMedia{
			MsgType: MsgTypeVideo,
			Title:   "demo",
		},
		"media-1",
	)
	require.NoError(t, err)

	frame := writer.waitFrame(t, time.Second)
	require.Equal(t, wsCommandRespond, frame.Command)
	body, ok := frame.Body.(wsReplyBody)
	require.True(t, ok)
	require.Equal(t, MsgTypeVideo, body.MsgType)
	require.Equal(t, "media-1", body.Video.MediaID)
	require.Equal(t, "demo", body.Video.Title)
}

func TestUnmarshalWSFrameBodyEmpty(t *testing.T) {
	t.Parallel()

	err := unmarshalWSFrameBody(wsInboundFrame{}, &wsUploadMediaInitAck{})

	require.ErrorContains(t, err, "empty ack body")
}

func TestRewriteReplyContentWithProfileHidesSyntheticPDFNames(
	t *testing.T,
) {
	t.Parallel()

	profile := buildReplyUXProfile(
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					Filename: "attachment.pdf",
					Format:   mimeTypePDF,
				},
			},
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					Filename: "0.pdf",
					Format:   mimeTypePDF,
				},
			},
		},
	)

	reply := rewriteReplyContentWithProfile(
		profile,
		"顺序：attachment.pdf -> 0.pdf",
	)

	require.Equal(t, "顺序：第 1 个上传的 PDF -> 0.pdf", reply)
}

func TestInitialReplyHintContentForMergeFiles(t *testing.T) {
	t.Parallel()

	hint := initialReplyHintContent(
		defaultProcessingMessage,
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{URL: "https://example.com/a"},
			},
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{URL: "https://example.com/b"},
			},
		},
	)

	require.Equal(t, "已收到 2 个附件，正在读取内容...", hint)
}

func TestPreGatewayReplyHintContentForAttachments(t *testing.T) {
	t.Parallel()

	hint := preGatewayReplyHintContent(buildReplyUXProfile(
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					Filename: "attachment.pdf",
					Format:   mimeTypePDF,
				},
			},
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					Filename: "0.pdf",
					Format:   mimeTypePDF,
				},
			},
		},
	))

	require.Equal(t, "已读取 2 个附件，正在准备处理...", hint)
}

func TestInitialReplyHintContentForProcessingMessage(
	t *testing.T,
) {
	t.Parallel()

	hint := initialReplyHintContent(
		defaultProcessingMessage,
		nil,
	)

	require.Equal(t, defaultProcessingMessage, hint)
}

func TestInitialReplyHintContentForSingleAttachment(
	t *testing.T,
) {
	t.Parallel()

	hint := initialReplyHintContent(
		defaultProcessingMessage,
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					Filename: "report.pdf",
					Format:   mimeTypePDF,
				},
			},
		},
	)

	require.Equal(t, defaultAttachmentReadText, hint)
}

func TestPreGatewayReplyHintContentWithoutAttachments(t *testing.T) {
	t.Parallel()

	hint := preGatewayReplyHintContent(buildReplyUXProfile(nil))

	require.Empty(t, hint)
}

// --- Custom messages tests ---

func TestNewChannelCustomMessagesViaOptions(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testConfig())
	ch, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{Type: pluginType, Name: "test", Config: cfg},
		WithProcessingMessage("思考中请等待..."),
		WithCancelNoopMessage("没有任务在执行"),
		WithCancelFailedMessage("取消操作失败了"),
		WithCancelOKMessage("已经取消了"),
		WithNotAllowedMessage("您无权使用"),
		WithHelpMessage("自定义帮助"),
	)
	require.NoError(t, err)
	c := ch.(*Channel)
	require.Equal(t, "思考中请等待...", c.processingMessage)
	require.Equal(t, "没有任务在执行", c.cancelNoopMessage)
	require.Equal(t, "取消操作失败了", c.cancelFailedMessage)
	require.Equal(t, "已经取消了", c.cancelOKMessage)
	require.Equal(t, "您无权使用", c.notAllowedMessage)
	require.Equal(t, "自定义帮助", c.helpMessage)
}

func TestNewChannelDefaultMessages(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testConfig())
	ch, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{Type: pluginType, Name: "test", Config: cfg},
	)
	require.NoError(t, err)
	c := ch.(*Channel)
	require.Equal(t, defaultProcessingMessage, c.processingMessage)
	require.Equal(t, defaultCancelNoopMessage, c.cancelNoopMessage)
	require.Equal(t, defaultCancelFailedMessage, c.cancelFailedMessage)
	require.Equal(t, defaultCancelOKMessage, c.cancelOKMessage)
	require.Equal(t, defaultNotAllowedMessage, c.notAllowedMessage)
	require.Equal(t, defaultHelpMessage, c.helpMessage)
	require.Zero(t, c.sessionTimeout)
	require.True(t, c.enterChatWelcome)
}

func TestNewChannelCanDisableEnterChatWelcome(t *testing.T) {
	t.Parallel()

	cfg := mustYAMLNode(t, testWebSocketConfig()+`enter_chat_welcome: false
`)
	ch, err := newChannel(
		registry.ChannelDeps{Gateway: stubGateway{}},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test-websocket",
			Config: cfg,
		},
	)
	require.NoError(t, err)

	c := ch.(*Channel)
	require.False(t, c.enterChatWelcome)
}

func TestHandleMessageCustomNotAllowed(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	ch := &Channel{
		cfg:               channelCfg{BotName: "Bot"},
		gw:                stubGateway{},
		sender:            ms,
		chatPolicy:        chatPolicyDisabled,
		inflight:          newInflightRequests(),
		lanes:             newLaneLocker(),
		notAllowedMessage: "自定义拒绝消息",
	}

	err := ch.handleMessage(context.Background(), WebhookMessage{
		MsgType: "text",
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		Text:    TextContent{Content: "hello"},
	})
	require.NoError(t, err)
	require.Equal(t, "自定义拒绝消息", ms.lastText)
}

// --- Test helpers ---

func mustYAMLNode(t *testing.T, content string) *yaml.Node {
	t.Helper()
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(content), &node))
	return &node
}

func mustCreateChannel(t *testing.T) *Channel {
	t.Helper()
	return mustCreateChannelWithGW(t, stubGateway{})
}

func expectedRequestText(
	text string,
	attachmentCount int,
	parts []gwproto.ContentPart,
) string {
	_ = attachmentCount
	_ = parts
	return text
}

func mustCreateChannelWithGW(t *testing.T, gw registry.GatewayClient) *Channel {
	t.Helper()
	cfg := mustYAMLNode(t, testConfig())
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  gw,
			StateDir: t.TempDir(),
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	channel := ch.(*Channel)
	channel.mediaURLValidator = allowAnyMediaURL
	return channel
}

func mustCreateWebSocketChannel(
	t *testing.T,
	gw registry.GatewayClient,
) *Channel {
	t.Helper()
	cfg := mustYAMLNode(t, testWebSocketConfig())
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  gw,
			StateDir: t.TempDir(),
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test-websocket",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	channel := ch.(*Channel)
	channel.mediaURLValidator = allowAnyMediaURL
	return channel
}

func mustCreateAIWebhookChannel(
	t *testing.T,
	gw registry.GatewayClient,
) *Channel {
	t.Helper()
	cfg := mustYAMLNode(t, testAIWebhookConfig())
	ch, err := newChannel(
		registry.ChannelDeps{
			Gateway:  gw,
			StateDir: t.TempDir(),
		},
		registry.PluginSpec{
			Type:   pluginType,
			Name:   "test-ai-webhook",
			Config: cfg,
		},
	)
	require.NoError(t, err)
	return ch.(*Channel)
}

func buildSignature(token, timestamp, nonce, encrypt string) string {
	strs := []string{token, timestamp, nonce, encrypt}
	sort.Strings(strs)
	h := sha1.New()
	h.Write([]byte(strings.Join(strs, "")))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func mustBase64Decode(t *testing.T, raw string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(raw)
	require.NoError(t, err)
	return data
}

func encryptWecomFileData(
	t *testing.T,
	crypt *msgCrypt,
	plaintext []byte,
) []byte {
	t.Helper()
	block, err := aes.NewCipher(crypt.aesKey)
	require.NoError(t, err)

	padded := pkcs7PadForTest(plaintext, aesKeySize)
	encrypted := make([]byte, len(padded))
	iv := crypt.aesKey[:aes.BlockSize]
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(encrypted, padded)
	return encrypted
}

func encryptWecomFileDataWithEncodingAESKey(
	t *testing.T,
	encodingAESKey string,
	plaintext []byte,
) []byte {
	t.Helper()
	aesKey, err := ParseEncodingAESKey(encodingAESKey)
	require.NoError(t, err)

	block, err := aes.NewCipher(aesKey)
	require.NoError(t, err)

	padded := pkcs7PadForTest(plaintext, aesKeySize)
	encrypted := make([]byte, len(padded))
	iv := aesKey[:aes.BlockSize]
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(encrypted, padded)
	return encrypted
}

func pkcs7PadForTest(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	if padding == 0 {
		padding = blockSize
	}
	out := make([]byte, len(data)+padding)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

func buildZipData(t *testing.T, names []string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, name := range names {
		fileWriter, err := writer.Create(name)
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("test"))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buf.Bytes()
}

type stubGateway struct{}

func (stubGateway) SendMessage(
	_ context.Context,
	_ gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	return gwclient.MessageResponse{}, nil
}

func (stubGateway) Cancel(context.Context, string) (bool, error) {
	return false, nil
}

type recordingGateway struct {
	reply      string
	usage      *gwclient.Usage
	sendCalled bool
	lastReq    gwclient.MessageRequest
	onSend     func(context.Context, gwclient.MessageRequest)
}

func (g *recordingGateway) SendMessage(
	ctx context.Context,
	req gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	g.sendCalled = true
	g.lastReq = req
	if g.onSend != nil {
		g.onSend(ctx, req)
	}
	return gwclient.MessageResponse{
		Reply: g.reply,
		Usage: g.usage,
	}, nil
}

func (g *recordingGateway) Cancel(context.Context, string) (bool, error) {
	return false, nil
}

type scriptedGateway struct {
	replies  []string
	requests []gwclient.MessageRequest
}

func (g *scriptedGateway) SendMessage(
	_ context.Context,
	req gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	g.requests = append(g.requests, req)
	index := len(g.requests) - 1
	reply := ""
	if index >= 0 && index < len(g.replies) {
		reply = g.replies[index]
	}
	return gwclient.MessageResponse{Reply: reply}, nil
}

func (g *scriptedGateway) Cancel(context.Context, string) (bool, error) {
	return false, nil
}

type cancelGateway struct {
	cancelResult  bool
	cancelErr     error
	lastRequestID string
}

func (g *cancelGateway) SendMessage(
	_ context.Context,
	_ gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	return gwclient.MessageResponse{}, nil
}

func (g *cancelGateway) Cancel(_ context.Context, requestID string) (bool, error) {
	g.lastRequestID = requestID
	return g.cancelResult, g.cancelErr
}

type errorGateway struct {
	statusCode int
	errMsg     string
}

func (g *errorGateway) SendMessage(
	_ context.Context,
	_ gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	return gwclient.MessageResponse{
		StatusCode: g.statusCode,
		Error:      &gwclient.APIError{Message: g.errMsg},
	}, fmt.Errorf("gateway error: %s", g.errMsg)
}

func (g *errorGateway) Cancel(context.Context, string) (bool, error) {
	return false, nil
}

type blockingGateway struct {
	releaseCh chan struct{}
	startedCh chan struct{}
}

func (g *blockingGateway) SendMessage(
	_ context.Context,
	_ gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	select {
	case g.startedCh <- struct{}{}:
	default:
	}
	<-g.releaseCh
	return gwclient.MessageResponse{Reply: "ok"}, nil
}

func (g *blockingGateway) Cancel(context.Context, string) (bool, error) {
	return false, nil
}

type recordingWSWriter struct {
	mu     sync.Mutex
	frames []wsOutboundFrame
	ch     chan wsOutboundFrame
}

func newRecordingWSWriter() *recordingWSWriter {
	return &recordingWSWriter{
		ch: make(chan wsOutboundFrame, 8),
	}
}

func (w *recordingWSWriter) send(
	_ context.Context,
	frame wsOutboundFrame,
) error {
	w.mu.Lock()
	w.frames = append(w.frames, frame)
	w.mu.Unlock()

	w.ch <- frame
	return nil
}

func (w *recordingWSWriter) waitFrame(
	t *testing.T,
	timeout time.Duration,
) wsOutboundFrame {
	t.Helper()

	select {
	case frame := <-w.ch:
		return frame
	case <-time.After(timeout):
		t.Fatal("expected websocket frame")
		return wsOutboundFrame{}
	}
}

type ackWSWriter struct {
	mu     sync.Mutex
	frames []wsOutboundFrame
}

func newAckWSWriter() *ackWSWriter {
	return &ackWSWriter{}
}

func (w *ackWSWriter) send(
	_ context.Context,
	frame wsOutboundFrame,
) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frames = append(w.frames, frame)
	return nil
}

func (w *ackWSWriter) request(
	_ context.Context,
	frame wsOutboundFrame,
) (wsInboundFrame, error) {
	if err := w.send(context.Background(), frame); err != nil {
		return wsInboundFrame{}, err
	}

	switch frame.Command {
	case wsCommandUploadMediaInit:
		return buildWSAckFrame(
			frame.Headers.ReqID,
			wsUploadMediaInitAck{UploadID: "upload-1"},
		)
	case wsCommandUploadMediaChunk:
		return buildWSAckFrame(frame.Headers.ReqID, struct{}{})
	case wsCommandUploadMediaFinish:
		return buildWSAckFrame(
			frame.Headers.ReqID,
			wsUploadMediaFinishAck{MediaID: "media-1"},
		)
	default:
		return buildWSAckFrame(frame.Headers.ReqID, struct{}{})
	}
}

func buildWSAckFrame(
	reqID string,
	body any,
) (wsInboundFrame, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return wsInboundFrame{}, err
	}
	return wsInboundFrame{
		Headers: wsFrameHeaders{ReqID: reqID},
		Body:    data,
	}, nil
}

type mockSender struct {
	lastText         string
	lastMarkdown     string
	lastChatID       string
	lastTemplateCard *templateCard
	lastUpdatedCard  *templateCard
	textCalls        []string
	markdownCalls    []string
	keepPrefix       bool
}

func (m *mockSender) SendText(_ context.Context, chatID, content string) error {
	m.lastChatID = chatID
	if !m.keepPrefix {
		content = stripReplyPrefixForTest(content)
	}
	m.lastText = content
	m.textCalls = append(m.textCalls, content)
	return nil
}

func (m *mockSender) SendMarkdown(_ context.Context, chatID, content string) error {
	m.lastChatID = chatID
	if !m.keepPrefix {
		content = stripReplyPrefixForTest(content)
	}
	m.lastMarkdown = content
	m.markdownCalls = append(m.markdownCalls, content)
	return nil
}

func (m *mockSender) SendTemplateCard(
	_ context.Context,
	chatID string,
	card *templateCard,
) error {
	m.lastChatID = chatID
	m.lastTemplateCard = card
	return nil
}

func (m *mockSender) UpdateTemplateCard(
	_ context.Context,
	card *templateCard,
) error {
	m.lastUpdatedCard = card
	return nil
}
