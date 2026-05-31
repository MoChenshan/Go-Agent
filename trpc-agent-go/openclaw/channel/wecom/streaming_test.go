package wecom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/progress"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwproto"
)

type fakeStreamEvent struct {
	Type             string               `json:"type"`
	Delta            string               `json:"delta,omitempty"`
	Reply            string               `json:"reply,omitempty"`
	Usage            *gwclient.Usage      `json:"usage,omitempty"`
	Stage            string               `json:"stage,omitempty"`
	Summary          string               `json:"summary,omitempty"`
	ToolName         string               `json:"tool_name,omitempty"`
	ToolDetail       string               `json:"tool_detail,omitempty"`
	ToolCallID       string               `json:"tool_call_id,omitempty"`
	ToolStatus       string               `json:"tool_status,omitempty"`
	Thinking         string               `json:"thinking,omitempty"`
	Reasoning        string               `json:"reasoning,omitempty"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	Thoughts         string               `json:"thoughts,omitempty"`
	Message          string               `json:"message,omitempty"`
	ToolCalls        []fakeStreamToolCall `json:"tool_calls,omitempty"`
	Error            *gatewayStreamError  `json:"error,omitempty"`
	Ignored          bool                 `json:"ignored,omitempty"`
}

type fakeStreamToolCall struct {
	ID       string                   `json:"id,omitempty"`
	Name     string                   `json:"name,omitempty"`
	ToolName string                   `json:"tool_name,omitempty"`
	Function *fakeStreamToolCallParts `json:"function,omitempty"`
}

type fakeStreamToolCallParts struct {
	Name string `json:"name,omitempty"`
}

type fakeStreamGateway struct {
	events        []fakeStreamEvent
	streamErr     error
	reply         string
	sendErr       error
	sendCalled    bool
	streamCalled  bool
	cancelCalled  bool
	cancelResult  bool
	cancelErr     error
	lastReq       gwclient.MessageRequest
	optionsCalled bool
	lastOptions   *gwclient.MessageStreamOptions
}

type gwclientStreamGateway struct {
	*fakeStreamGateway
	events []gwclient.StreamEvent
}

func (g *gwclientStreamGateway) StreamMessage(
	_ context.Context,
	req gwclient.MessageRequest,
) (<-chan gwclient.StreamEvent, error) {
	g.streamCalled = true
	g.lastReq = req
	if g.streamErr != nil {
		return nil, g.streamErr
	}
	ch := make(chan gwclient.StreamEvent, len(g.events))
	for _, evt := range g.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (g *fakeStreamGateway) SendMessage(
	_ context.Context,
	req gwclient.MessageRequest,
) (gwclient.MessageResponse, error) {
	g.sendCalled = true
	g.lastReq = req
	if g.sendErr != nil {
		return gwclient.MessageResponse{}, g.sendErr
	}
	reply := g.reply
	if strings.TrimSpace(reply) == "" {
		reply = "fallback"
	}
	return gwclient.MessageResponse{
		Reply: reply,
	}, nil
}

func (g *fakeStreamGateway) StreamMessage(
	_ context.Context,
	req gwclient.MessageRequest,
) (<-chan fakeStreamEvent, error) {
	g.streamCalled = true
	g.lastReq = req
	if g.streamErr != nil {
		return nil, g.streamErr
	}
	ch := make(chan fakeStreamEvent, len(g.events))
	for _, evt := range g.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (g *fakeStreamGateway) StreamMessageWithOptions(
	ctx context.Context,
	req gwclient.MessageRequest,
	opts *gwclient.MessageStreamOptions,
) (<-chan fakeStreamEvent, error) {
	g.optionsCalled = true
	g.lastOptions = cloneMessageStreamOptions(opts)
	return g.StreamMessage(ctx, req)
}

func (g *gwclientStreamGateway) StreamMessageWithOptions(
	_ context.Context,
	req gwclient.MessageRequest,
	opts *gwclient.MessageStreamOptions,
) (<-chan gwclient.StreamEvent, error) {
	g.optionsCalled = true
	g.lastReq = req
	g.lastOptions = cloneMessageStreamOptions(opts)
	if g.streamErr != nil {
		return nil, g.streamErr
	}
	ch := make(chan gwclient.StreamEvent, len(g.events))
	for _, evt := range g.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func cloneMessageStreamOptions(
	opts *gwclient.MessageStreamOptions,
) *gwclient.MessageStreamOptions {
	if opts == nil {
		return nil
	}
	cloned := *opts
	return &cloned
}

func (g *fakeStreamGateway) Cancel(
	context.Context,
	string,
) (bool, error) {
	g.cancelCalled = true
	return g.cancelResult, g.cancelErr
}

type mockStreamingSender struct {
	lastMarkdown  string
	markdownCalls []string
	streamCalls   []streamSendCall
	filePaths     []string
	sendFileErr   error
	keepPrefix    bool
	markdownErr   error
	streamErr     error
	streamErrs    []error
}

type streamSendCall struct {
	streamID   string
	content    string
	finish     bool
	feedbackID string
}

const (
	testProcessingPulse     = statusPulseOne
	testPreparingPulse      = statusPulseThree
	testReadingPulse        = statusPulseOne
	testProviderStreamError = "error reading response body: " +
		"stream error: stream ID 1; INTERNAL_ERROR; " +
		"received from peer"
)

func requirePulseContent(
	t *testing.T,
	content string,
) {
	t.Helper()
	require.Contains(
		t,
		[]string{
			statusPulseOne,
			statusPulseTwo,
			statusPulseThree,
		},
		content,
	)
}

func (m *mockStreamingSender) SendText(
	context.Context,
	string,
	string,
) error {
	return nil
}

func (m *mockStreamingSender) SendMarkdown(
	_ context.Context,
	_ string,
	content string,
) error {
	if !m.keepPrefix {
		content = stripReplyPrefixForTest(content)
	}
	m.lastMarkdown = content
	m.markdownCalls = append(m.markdownCalls, content)
	return m.markdownErr
}

func (m *mockStreamingSender) SendStream(
	_ context.Context,
	_ string,
	streamID string,
	content string,
	finish bool,
) error {
	return m.sendStream(streamID, content, finish, "")
}

func (m *mockStreamingSender) SendStreamWithFeedback(
	_ context.Context,
	_ string,
	streamID string,
	content string,
	finish bool,
	feedbackID string,
) error {
	return m.sendStream(streamID, content, finish, feedbackID)
}

func (m *mockStreamingSender) sendStream(
	streamID string,
	content string,
	finish bool,
	feedbackID string,
) error {
	if !m.keepPrefix {
		content = stripReplyPrefixForTest(content)
	}
	m.streamCalls = append(m.streamCalls, streamSendCall{
		streamID:   streamID,
		content:    content,
		finish:     finish,
		feedbackID: feedbackID,
	})
	if len(m.streamErrs) > 0 {
		err := m.streamErrs[0]
		m.streamErrs = m.streamErrs[1:]
		return err
	}
	return m.streamErr
}

func (m *mockStreamingSender) SendLocalFile(
	_ context.Context,
	_ string,
	path string,
) error {
	if m.sendFileErr != nil {
		return m.sendFileErr
	}
	m.filePaths = append(m.filePaths, path)
	return nil
}

func TestCallGatewayAndReplyUsesStreamMessage(t *testing.T) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDelta, Delta: "he"},
			{Type: streamEventMsgDelta, Delta: "llo"},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.False(t, gw.sendCalled)
	require.False(t, gw.optionsCalled)
	require.Len(t, sender.streamCalls, 2)
	requirePulseContent(t, sender.streamCalls[0].content)
	require.False(t, sender.streamCalls[0].finish)
	require.Equal(t, "hello", sender.streamCalls[1].content)
	require.True(t, sender.streamCalls[1].finish)
	require.True(t, strings.HasPrefix(
		sender.streamCalls[0].streamID,
		replyStreamIDPrefix,
	))
}

func TestNormalizeStreamDisplayModeDefaultsToNativeThinking(
	t *testing.T,
) {
	t.Parallel()

	require.Equal(
		t,
		streamDisplayModeNativeThinking,
		normalizeStreamDisplayMode(""),
	)
	require.Equal(
		t,
		streamDisplayModeNativeThinking,
		normalizeStreamDisplayMode(streamDisplayModeNativeThinking),
	)
	require.Equal(
		t,
		streamDisplayModeLegacy,
		normalizeStreamDisplayMode(streamDisplayModeLegacy),
	)
	require.Equal(
		t,
		streamDisplayModeLegacy,
		normalizeStreamDisplayMode("unknown"),
	)
}

func TestUsesNativeThinkingStreamDefaultsForAIBotWebSocket(
	t *testing.T,
) {
	t.Parallel()

	ch := &Channel{
		botMode:        botModeAI,
		connectionMode: connectionModeWebSocket,
	}
	require.True(t, ch.usesNativeThinkingStream())

	ch.cfg.StreamDisplayMode = streamDisplayModeLegacy
	require.False(t, ch.usesNativeThinkingStream())

	ch.cfg.StreamDisplayMode = streamDisplayModeNativeThinking
	ch.connectionMode = connectionModeWebhook
	require.False(t, ch.usesNativeThinkingStream())
}

func TestCallGatewayAndReplyUsesNativeThinkingStream(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDelta, Delta: "he"},
			{Type: streamEventMsgDelta, Delta: "llo"},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.False(t, gw.sendCalled)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.NotEmpty(t, sender.streamCalls[0].feedbackID)
	require.Equal(
		t,
		"hello",
		sender.streamCalls[1].content,
	)
	require.NotContains(
		t,
		sender.streamCalls[1].content,
		streamNativeThinkingOpenTag,
	)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyNativeThinkingSnapshotModes(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name          string
		snapshotMode  string
		wantContents  []string
		wantFinish    []bool
		wantFeedbacks []bool
	}{
		{
			name:         streamSnapshotModeContentOnly,
			snapshotMode: streamSnapshotModeContentOnly,
			wantContents: []string{
				nativeThinkingStreamContent(
					nil,
					"hello",
					true,
				),
			},
			wantFinish:    []bool{true},
			wantFeedbacks: []bool{true},
		},
		{
			name:         streamSnapshotModeFinalOnly,
			snapshotMode: streamSnapshotModeFinalOnly,
			wantContents: []string{
				nativeThinkingStreamContent(
					nil,
					"hello",
					true,
				),
			},
			wantFinish:    []bool{true},
			wantFeedbacks: []bool{true},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gw := &fakeStreamGateway{
				events: []fakeStreamEvent{
					{Type: streamEventRunStarted},
					{Type: streamEventMsgDelta, Delta: "he"},
					{Type: streamEventMsgDelta, Delta: "llo"},
					{Type: streamEventMsgDone, Reply: "hello"},
					{Type: streamEventRunDone},
				},
			}
			sender := &mockStreamingSender{}
			ch := &Channel{
				gw: gw,
				cfg: channelCfg{
					EnableStream:       true,
					StreamSnapshotMode: tt.snapshotMode,
				},
				botMode:           botModeAI,
				connectionMode:    connectionModeWebSocket,
				processingMessage: defaultProcessingMessage,
			}

			err := ch.callGatewayAndReply(
				context.Background(),
				WebhookMessage{
					ChatID: "chat1",
					MsgID:  "msg1",
				},
				"hello",
				nil,
				nil,
				"user1",
				"req1",
				"session1",
				sender,
			)
			require.NoError(t, err)
			require.Len(t, sender.streamCalls, len(tt.wantContents))
			for i, want := range tt.wantContents {
				require.Equal(t, want, sender.streamCalls[i].content)
				require.Equal(
					t,
					tt.wantFinish[i],
					sender.streamCalls[i].finish,
				)
				hasFeedback := sender.streamCalls[i].feedbackID != ""
				require.Equal(t, tt.wantFeedbacks[i], hasFeedback)
			}
		})
	}
}

func TestCallGatewayAndReplyRunIgnoredClosesNativeThinkingStream(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventRunIgnored},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.False(t, gw.sendCalled)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.NotEmpty(t, sender.streamCalls[0].feedbackID)
	require.Equal(
		t,
		nativeThinkingStreamContent(
			nil,
			defaultIgnoredStatusSummary,
			true,
		),
		sender.streamCalls[1].content,
	)
	require.True(t, sender.streamCalls[1].finish)
	require.Empty(t, sender.streamCalls[1].feedbackID)
	require.Empty(t, sender.markdownCalls)
}

func TestCallGatewayAndReplyRunIgnoredAfterAckTimeoutClosesNativeStream(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventRunIgnored},
		},
	}
	sender := &mockStreamingSender{
		streamErrs: []error{errReplyAckTimeout},
	}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.NotEmpty(t, sender.streamCalls[0].feedbackID)
	require.Equal(
		t,
		nativeThinkingStreamContent(
			nil,
			defaultIgnoredStatusSummary,
			true,
		),
		sender.streamCalls[1].content,
	)
	require.True(t, sender.streamCalls[1].finish)
	require.Empty(t, sender.streamCalls[1].feedbackID)
	require.Empty(t, sender.markdownCalls)
}

func TestCallGatewayAndReplyNativeThinkingHidesThoughts(
	t *testing.T,
) {
	t.Parallel()

	const thought = "我先看一下仓库结构"

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventThoughtDelta,
				Delta: thought,
			},
			{
				Type:  streamEventRunProgress,
				Stage: streamStageRunningTool,
			},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeNativeThinking,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.Equal(t, "hello", sender.streamCalls[1].content)
	require.True(t, sender.streamCalls[1].finish)
	for _, call := range sender.streamCalls {
		require.NotContains(t, call.content, thought)
		require.NotContains(t, call.content, progressTextRunningTool)
	}
}

func TestCallGatewayAndReplyNativeThinkingShowsPublicProcess(
	t *testing.T,
) {
	t.Parallel()

	const process = "我先确认仓库结构"
	const answer = "hello"

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventPublicDelta,
				Delta: process,
			},
			{Type: streamEventMsgDone, Reply: answer},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeNativeThinking,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 3)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.Equal(
		t,
		streamNativeThinkingOpenTag+process,
		sender.streamCalls[1].content,
	)
	require.NotContains(t, sender.streamCalls[1].content, answer)
	require.Equal(t, answer, sender.streamCalls[2].content)
	require.NotContains(
		t,
		sender.streamCalls[2].content,
		streamNativeThinkingOpenTag,
	)
	require.True(t, sender.streamCalls[2].finish)
}

func TestCallGatewayAndReplyNativeThinkingMovesPreToolText(
	t *testing.T,
) {
	t.Parallel()

	const process = "我先查看子目录数量"
	const answer = "openclaw 下面有 364 个文件"

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDelta, Delta: process},
			{
				Type:    streamEventRunProgress,
				Stage:   streamStageRunningTool,
				Summary: progressSummaryInspectEN,
			},
			{Type: streamEventMsgDone, Reply: answer},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeNativeThinking,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"看下文件数量",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 3)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.Contains(t, sender.streamCalls[1].content, process)
	require.NotContains(
		t,
		sender.streamCalls[1].content,
		"正在检查工作区",
	)
	require.NotContains(t, sender.streamCalls[1].content, answer)
	require.Equal(t, answer, sender.streamCalls[2].content)
	require.True(t, sender.streamCalls[2].finish)
}

func TestCallGatewayAndReplyNativeThinkingShowsToolCalls(
	t *testing.T,
) {
	t.Parallel()

	const (
		process  = "我先查看这些文件"
		answer   = "这些文件主要负责知识库加载。"
		toolName = "exec_command"
	)

	gw := &gwclientStreamGateway{
		fakeStreamGateway: &fakeStreamGateway{},
		events: []gwclient.StreamEvent{
			{Type: gwproto.StreamEventTypeRunStarted},
			{Type: gwproto.StreamEventTypeMessageDelta, Delta: process},
			{
				Type:       gwproto.StreamEventTypeRunProgress,
				Stage:      gwproto.StreamProgressStageRunningTool,
				Summary:    progressSummaryGoTestEN,
				ToolName:   toolName,
				ToolDetail: "go test ./channel/wecom",
				ToolCallID: "call-1",
				ToolStatus: gwproto.StreamToolStatusRunning,
			},
			{
				Type:       gwproto.StreamEventTypeRunProgress,
				Stage:      gwproto.StreamProgressStageSummarizing,
				Summary:    progressSummaryAnsweringEN,
				ToolName:   toolName,
				ToolCallID: "call-1",
				ToolStatus: gwproto.StreamToolStatusCompleted,
			},
			{
				Type:       gwproto.StreamEventTypeRunProgress,
				Stage:      gwproto.StreamProgressStageRunningTool,
				Summary:    progressSummaryGitEN,
				ToolName:   toolName,
				ToolDetail: "git status",
				ToolCallID: "call-2",
				ToolStatus: gwproto.StreamToolStatusRunning,
			},
			{
				Type:       gwproto.StreamEventTypeRunProgress,
				Stage:      gwproto.StreamProgressStageRunningTool,
				Summary:    progressSummaryGitEN,
				ToolName:   toolName,
				ToolDetail: "git status",
				ToolCallID: "call-2",
				ToolStatus: gwproto.StreamToolStatusRunning,
			},
			{Type: gwproto.StreamEventTypeMessageCompleted, Reply: answer},
			{Type: gwproto.StreamEventTypeRunCompleted},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeNativeThinking,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"分析 knowledge",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.True(t, gw.optionsCalled)
	require.NotNil(t, gw.lastOptions)
	require.True(t, gw.lastOptions.ProgressAfterTextDelta)
	require.Len(t, sender.streamCalls, 4)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.Contains(t, sender.streamCalls[1].content, process)
	require.Contains(
		t,
		sender.streamCalls[1].content,
		nativeThinkingToolActivityText(
			1,
			toolName,
			"go test ./channel/wecom",
		),
	)
	require.Contains(
		t,
		sender.streamCalls[2].content,
		nativeThinkingToolActivityText(
			2,
			toolName,
			"git status",
		),
	)
	require.NotContains(
		t,
		sender.streamCalls[2].content,
		progressTextRunningTool,
	)
	require.NotContains(t, sender.streamCalls[2].content, answer)
	require.Equal(t, answer, sender.streamCalls[3].content)
	require.True(t, sender.streamCalls[3].finish)
}

func TestRecordNativeThinkingToolActivityDedupe(
	t *testing.T,
) {
	t.Parallel()

	const toolName = "exec_command"

	state := &replyStreamState{nativeThinking: true}
	firstProgress := gatewayStreamEvent{
		Type:       streamEventRunProgress,
		Stage:      streamStageRunningTool,
		Summary:    progressSummaryGoTestEN,
		ToolName:   toolName,
		ToolCallID: "call-1",
		ToolStatus: streamToolStatusRunning,
	}
	require.True(t, recordNativeThinkingToolActivity(state, firstProgress))
	firstCompleted := firstProgress
	firstCompleted.Stage = streamStageSummarizing
	firstCompleted.ToolStatus = streamToolStatusCompleted
	require.False(t, recordNativeThinkingToolActivity(state, firstCompleted))
	require.Equal(
		t,
		nativeThinkingToolActivityText(1, toolName, toolDetailGoTest),
		builderText(&state.toolActivityBuilder),
	)

	secondProgress := firstProgress
	secondProgress.Summary = progressSummaryGitEN
	secondProgress.ToolCallID = "call-2"
	require.True(t, recordNativeThinkingToolActivity(state, secondProgress))
	require.Contains(
		t,
		builderText(&state.toolActivityBuilder),
		nativeThinkingToolActivityText(2, toolName, toolDetailGit),
	)
	beforeCompletedWithoutID := builderText(&state.toolActivityBuilder)
	completedWithoutID := firstProgress
	completedWithoutID.ToolCallID = ""
	completedWithoutID.Stage = streamStageSummarizing
	completedWithoutID.ToolStatus = streamToolStatusCompleted
	require.False(
		t,
		recordNativeThinkingToolActivity(state, completedWithoutID),
	)
	require.Equal(
		t,
		beforeCompletedWithoutID,
		builderText(&state.toolActivityBuilder),
	)

	completedOnlyState := &replyStreamState{nativeThinking: true}
	require.False(
		t,
		recordNativeThinkingToolActivity(
			completedOnlyState,
			completedWithoutID,
		),
	)
	require.Empty(t, builderText(&completedOnlyState.toolActivityBuilder))

	legacyState := &replyStreamState{nativeThinking: true}
	legacyEvent := gatewayStreamEvent{
		Type:     streamEventRunProgress,
		Stage:    streamStageRunningTool,
		ToolName: toolName,
	}
	require.True(t, recordNativeThinkingToolActivity(legacyState, legacyEvent))
	require.False(t, recordNativeThinkingToolActivity(legacyState, legacyEvent))

	toolCallsState := &replyStreamState{nativeThinking: true}
	firstToolCall := gatewayStreamEvent{
		Type:  streamEventRunProgress,
		Stage: streamStageRunningTool,
		ToolCalls: []gatewayStreamToolCall{
			{
				ID: "call-1",
				Function: &gatewayStreamToolCallPayload{
					Name: toolName,
				},
			},
		},
	}
	require.True(
		t,
		recordNativeThinkingToolActivity(toolCallsState, firstToolCall),
	)
	require.False(
		t,
		recordNativeThinkingToolActivity(toolCallsState, firstToolCall),
	)

	secondToolCall := firstToolCall
	secondToolCall.ToolCalls[0].ID = "call-2"
	require.True(
		t,
		recordNativeThinkingToolActivity(toolCallsState, secondToolCall),
	)
	require.Contains(
		t,
		builderText(&toolCallsState.toolActivityBuilder),
		nativeThinkingToolActivityText(2, toolName, ""),
	)
}

func TestRecordNativeThinkingToolActivityIgnoresSummary(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{nativeThinking: true}
	event := gatewayStreamEvent{
		Type:     streamEventRunProgress,
		Stage:    streamStageRunningTool,
		Summary:  "Running exec_command --token secret",
		ToolName: "exec_command --token secret",
	}

	require.False(t, recordNativeThinkingToolActivity(state, event))
	require.Empty(t, builderText(&state.toolActivityBuilder))
}

func TestSanitizeNativeThinkingToolInfoKeepsUsefulDetail(
	t *testing.T,
) {
	t.Parallel()

	require.Equal(
		t,
		"navigate example.com/a",
		sanitizeNativeThinkingToolInfo(
			"navigate example.com/a?token=secret",
		),
	)
	require.Equal(
		t,
		"搜索 tokenizer.go",
		sanitizeNativeThinkingToolInfo("搜索 tokenizer.go"),
	)
	require.Len(
		t,
		[]rune(sanitizeNativeThinkingToolInfo(
			strings.Repeat("a", streamNativeThinkingToolInfoMaxRunes+8),
		)),
		streamNativeThinkingToolInfoMaxRunes,
	)
}

func TestCallGatewayAndReplyNativeThinkingRunDoneDoesNotPromoteDelta(
	t *testing.T,
) {
	t.Parallel()

	const process = "我先查看子目录数量"

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDelta, Delta: process},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeNativeThinking,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"看下文件数量",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.NotContains(t, sender.streamCalls[1].content, process)
	require.Equal(t, streamNativeThinkingDoneText, sender.streamCalls[1].content)
	require.NotContains(
		t,
		sender.streamCalls[1].content,
		streamNativeThinkingOpenTag,
	)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamsAttachmentMarker(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	reportPath := filepath.Join(root, "report.md")
	require.NoError(
		t,
		os.WriteFile(reportPath, []byte("hello"), 0o600),
	)

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type: streamEventMsgDone,
				Reply: "报告已生成。\n" +
					replyFileMarkerPrefix +
					reportPath +
					replyFileMarkerSuffix,
			},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:                botModeAI,
		connectionMode:         connectionModeWebSocket,
		processingMessage:      defaultProcessingMessage,
		defaultCodingWorkspace: root,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"please send back",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Equal(t, []string{reportPath}, sender.filePaths)
	require.GreaterOrEqual(t, len(sender.streamCalls), 2)
	requirePulseContent(t, sender.streamCalls[0].content)
	require.Equal(
		t,
		"报告已生成。",
		sender.streamCalls[len(sender.streamCalls)-1].content,
	)
	require.True(
		t,
		sender.streamCalls[len(sender.streamCalls)-1].finish,
	)
}

func TestCallGatewayAndReplyUsesFullSnapshotsByDefault(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventRunProgress,
				Stage: streamStagePreparing,
			},
			{Type: streamEventMsgDelta, Delta: "he"},
			{Type: streamEventMsgDelta, Delta: "llo"},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream: true,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 3)
	requirePulseContent(t, sender.streamCalls[0].content)
	require.False(t, sender.streamCalls[0].finish)
	requirePulseContent(t, sender.streamCalls[1].content)
	require.False(t, sender.streamCalls[1].finish)
	require.Equal(t, "hello", sender.streamCalls[2].content)
	require.True(t, sender.streamCalls[2].finish)
}

func TestBuildReplyStreamIDIsUnique(t *testing.T) {
	t.Parallel()

	first := buildReplyStreamID()
	second := buildReplyStreamID()

	require.NotEmpty(t, first)
	require.NotEmpty(t, second)
	require.NotEqual(t, first, second)
	require.True(
		t,
		strings.HasPrefix(first, replyStreamIDPrefix),
	)
	require.True(
		t,
		strings.HasPrefix(second, replyStreamIDPrefix),
	)
}

func TestCallGatewayAndReplyUsesContentOnlySnapshots(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventRunProgress,
				Stage: streamStagePreparing,
			},
			{Type: streamEventMsgDelta, Delta: "he"},
			{Type: streamEventMsgDelta, Delta: "llo"},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeContentOnly,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 1)
	require.Equal(t, "hello", sender.streamCalls[0].content)
	require.True(t, sender.streamCalls[0].finish)
}

func TestCallGatewayAndReplyUsesFinalOnlySnapshots(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventRunProgress,
				Stage: streamStagePreparing,
			},
			{Type: streamEventMsgDelta, Delta: "he"},
			{Type: streamEventMsgDelta, Delta: "llo"},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFinalOnly,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 1)
	require.Equal(t, "hello", sender.streamCalls[0].content)
	require.True(t, sender.streamCalls[0].finish)
}

func TestCallGatewayAndReplyStreamIncludesWorkspacePrefix(
	t *testing.T,
) {
	t.Parallel()

	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{keepPrefix: true}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream: true,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(true),
				Fields: []string{
					replyPrefixFieldAssistant,
					replyPrefixFieldPersona,
					replyPrefixFieldWorkspace,
					replyPrefixFieldCommands,
				},
			},
		},
		botMode:                botModeAI,
		processingMessage:      defaultProcessingMessage,
		defaultCodingWorkspace: repoDir,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"fix the failing repository test",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.Contains(
		t,
		sender.streamCalls[0].content,
		replyPrefixEmojiWorkspace+
			"工作区："+replyPrefixWorkspaceDefault,
	)
	require.Contains(
		t,
		sender.streamCalls[0].content,
		testProcessingPulse,
	)
	require.Contains(
		t,
		sender.streamCalls[1].content,
		replyPrefixEmojiAssistant+"trpc-claw",
	)
	require.Contains(t, sender.streamCalls[1].content, "hello")
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamRefreshesContextPrefixOnFinalReply(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventMsgDone,
				Reply: "hello",
				Usage: &gwclient.Usage{TotalTokens: 12345},
			},
			{
				Type:  streamEventRunDone,
				Usage: &gwclient.Usage{TotalTokens: 12345},
			},
		},
	}
	sender := &mockStreamingSender{keepPrefix: true}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream: true,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(true),
				Fields:  []string{replyPrefixFieldContext},
			},
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
		runtimeModelName:  "gpt-5.2",
		runStatus:         newRunStatusTracker(),
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"fix the failing repository test",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.NotContains(
		t,
		sender.streamCalls[0].content,
		replyPrefixEmojiContext+"上下文：",
	)
	require.Contains(
		t,
		sender.streamCalls[1].content,
		replyPrefixEmojiContext+"上下文：12.3K / 400K (3.1%)",
	)
	require.Contains(t, sender.streamCalls[1].content, "hello")
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamOmitsWorkspacePrefixWhenDisabled(
	t *testing.T,
) {
	t.Parallel()

	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream: true,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:                botModeAI,
		processingMessage:      defaultProcessingMessage,
		defaultCodingWorkspace: repoDir,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"fix the failing repository test",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.NotContains(
		t,
		sender.streamCalls[0].content,
		workspaceReplyLabelPath,
	)
	require.NotContains(
		t,
		sender.streamCalls[1].content,
		workspaceReplyLabelPath,
	)
}

func TestCallGatewayAndReplyUsesProgressSnapshots(t *testing.T) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventRunProgress,
				Stage: streamStagePreparing,
			},
			{
				Type:  streamEventRunProgress,
				Stage: streamStageReadingDocument,
			},
			{
				Type:  streamEventRunProgress,
				Stage: streamStageReadingDocument,
			},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 5)
	requirePulseContent(t, sender.streamCalls[0].content)
	requirePulseContent(t, sender.streamCalls[1].content)
	requirePulseContent(t, sender.streamCalls[2].content)
	requirePulseContent(t, sender.streamCalls[3].content)
	require.Equal(t, "hello", sender.streamCalls[4].content)
	require.True(t, sender.streamCalls[4].finish)
}

func TestCallGatewayAndReplyUsesNarrativeSnapshots(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:    streamEventRunProgress,
				Stage:   streamStagePreparing,
				Summary: progressSummaryPrepareEN,
			},
			{
				Type:    streamEventRunProgress,
				Stage:   streamStageRunningTool,
				Summary: "Running fs_search",
			},
			{Type: streamEventMsgDelta, Delta: "hello"},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 4)
	requirePulseContent(t, sender.streamCalls[0].content)
	requirePulseContent(t, sender.streamCalls[1].content)
	requirePulseContent(t, sender.streamCalls[2].content)
	require.Equal(t, "hello", sender.streamCalls[3].content)
	require.True(t, sender.streamCalls[3].finish)
}

func TestCallGatewayAndReplyDefersReplyUntilCompletion(
	t *testing.T,
) {
	t.Parallel()

	const finalReply = "已读取 open PR 列表。"

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventRunProgress,
				Stage: streamStagePreparing,
			},
			{Type: streamEventMsgDelta, Delta: "我会先读 open "},
			{Type: streamEventMsgDelta, Delta: "PR 列表。"},
			{
				Type:  streamEventRunProgress,
				Stage: streamStageRunningTool,
			},
			{
				Type:  streamEventMsgDone,
				Reply: finalReply,
			},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 4)
	requirePulseContent(t, sender.streamCalls[0].content)
	requirePulseContent(t, sender.streamCalls[1].content)
	require.Equal(
		t,
		"我会先读 open PR 列表。"+
			streamSectionSep+statusPulseOne,
		sender.streamCalls[2].content,
	)
	require.Equal(
		t,
		finalReply,
		sender.streamCalls[3].content,
	)
	require.True(t, sender.streamCalls[3].finish)
}

func TestCallGatewayAndReplyDisplaysThoughtDelta(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventThoughtDelta,
				Delta: "我先看一下仓库结构",
			},
			{
				Type:    streamEventRunProgress,
				Stage:   streamStageRunningTool,
				Summary: "Running local tool",
			},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 3)
	requirePulseContent(t, sender.streamCalls[0].content)
	require.Equal(
		t,
		"我先看一下仓库结构"+
			streamSectionSep+statusPulseOne,
		sender.streamCalls[1].content,
	)
	require.Equal(t, "hello", sender.streamCalls[2].content)
	require.True(t, sender.streamCalls[2].finish)
}

func TestCallGatewayAndReplyStreamDoesNotRetryExternalLookupFallback(
	t *testing.T,
) {
	t.Parallel()

	const firstReply = "当前股价需要联网实时查询，告诉我看港股还是美股。"

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventMsgDone,
				Reply: firstReply,
			},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"搜索下腾讯股票",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.True(t, gw.streamCalled)
	require.False(t, gw.sendCalled)
	require.NotEmpty(t, sender.streamCalls)
	last := sender.streamCalls[len(sender.streamCalls)-1]
	require.Equal(
		t,
		nativeThinkingStreamContent(nil, firstReply, true),
		last.content,
	)
	require.True(t, last.finish)
}

func TestStatusPulseSnapshotTextKeepsStableReplyBody(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{
		started:         true,
		statusBase:      progressTextRunningTool,
		statusPulseStep: 2,
	}
	state.builder.WriteString("我会先读 open PR 列表。")

	require.Equal(
		t,
		"我会先读 open PR 列表。"+
			streamSectionSep+statusPulseThree,
		statusPulseSnapshotText(state),
	)
}

func TestResetStatusPulseCycleRestartsInlinePulse(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{
		started:         true,
		statusBase:      progressTextRunningTool,
		statusPulseStep: 1,
	}
	state.builder.WriteString("我会先读 open PR 列表。")

	resetStatusPulseCycle(state)

	require.Equal(
		t,
		"我会先读 open PR 列表。"+
			streamSectionSep+statusPulseOne,
		statusPulseSnapshotText(state),
	)
}

func TestCallGatewayAndReplyDisplaysThoughtCompletion(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventThoughtDone,
				Reply: "我先看一下仓库结构",
			},
			{
				Type:  streamEventRunProgress,
				Stage: streamStagePreparing,
			},
			{Type: streamEventMsgDelta, Delta: "hello"},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 3)
	requirePulseContent(t, sender.streamCalls[0].content)
	requirePulseContent(t, sender.streamCalls[1].content)
	require.Equal(t, "hello", sender.streamCalls[2].content)
	require.True(t, sender.streamCalls[2].finish)
}

func TestCallGatewayAndReplyDisplaysPublicDelta(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventPublicDelta,
				Delta: "我先确认仓库结构",
			},
			{
				Type:    streamEventRunProgress,
				Stage:   streamStageRunningTool,
				Summary: "Running local tool",
			},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 3)
	requirePulseContent(t, sender.streamCalls[0].content)
	require.Equal(
		t,
		"我先确认仓库结构"+
			streamSectionSep+statusPulseOne,
		sender.streamCalls[1].content,
	)
	require.Equal(t, "hello", sender.streamCalls[2].content)
	require.True(t, sender.streamCalls[2].finish)
}

func TestCallGatewayAndReplyAppendsPublicComments(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventPublicDone,
				Reply: "我先确认仓库结构",
			},
			{
				Type:  streamEventPublicDone,
				Reply: "我找到现有文档并继续核对源码",
			},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	requirePulseContent(t, sender.streamCalls[0].content)
	require.Equal(t, "hello", sender.streamCalls[1].content)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyKeepsLatestPublicComment(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventPublicDone,
				Reply: "我先确认仓库结构",
			},
			{
				Type:    streamEventRunProgress,
				Stage:   streamStageRunningTool,
				Summary: "Running local tool",
			},
			{
				Type:  streamEventPublicDone,
				Reply: "我改成再核对当前分支状态",
			},
			{
				Type:    streamEventRunProgress,
				Stage:   streamStageRunningTool,
				Summary: "Running local tool",
			},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 4)
	requirePulseContent(t, sender.streamCalls[0].content)
	require.Equal(
		t,
		"我先确认仓库结构"+
			streamSectionSep+statusPulseOne,
		sender.streamCalls[1].content,
	)
	require.Equal(
		t,
		"我改成再核对当前分支状态"+
			streamSectionSep+statusPulseOne,
		sender.streamCalls[2].content,
	)
	require.Equal(t, "hello", sender.streamCalls[3].content)
	require.True(t, sender.streamCalls[3].finish)
}

func TestCallGatewayAndReplyPrefersPublicOverThought(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventThoughtDone,
				Reply: "先想一下",
			},
			{
				Type:  streamEventPublicDone,
				Reply: "我先确认仓库结构",
			},
			{
				Type:  streamEventRunProgress,
				Stage: streamStagePreparing,
			},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamDisplayMode:  streamDisplayModeLegacy,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 3)
	requirePulseContent(t, sender.streamCalls[0].content)
	requirePulseContent(t, sender.streamCalls[1].content)
	require.Equal(t, "hello", sender.streamCalls[2].content)
	require.True(t, sender.streamCalls[2].finish)
}

func TestCallGatewayAndReplyKeepsPrefixedProgressSnapshots(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventRunProgress,
				Stage: streamStagePreparing,
			},
			{
				Type:  streamEventRunProgress,
				Stage: streamStageReadingDocument,
			},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{keepPrefix: true}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(true),
				Fields: []string{
					replyPrefixFieldPersona,
					replyPrefixFieldCommands,
					replyPrefixFieldHint,
				},
			},
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 4)
	require.Contains(
		t,
		sender.streamCalls[0].content,
		replyPrefixLeadMarker+replyPrefixEmojiPersona,
	)
	require.Equal(
		t,
		statusPulseOne,
		stripReplyPrefixForTest(
			sender.streamCalls[0].content,
		),
	)
	require.Equal(
		t,
		statusPulseTwo,
		stripReplyPrefixForTest(
			sender.streamCalls[1].content,
		),
	)
	require.Contains(
		t,
		sender.streamCalls[2].content,
		replyPrefixLeadMarker+replyPrefixEmojiPersona,
	)
	require.Equal(
		t,
		statusPulseThree,
		stripReplyPrefixForTest(
			sender.streamCalls[2].content,
		),
	)
	require.Contains(
		t,
		sender.streamCalls[3].content,
		replyPrefixLeadMarker+replyPrefixEmojiPersona,
	)
	require.Contains(t, sender.streamCalls[3].content, "hello")
	require.True(t, sender.streamCalls[3].finish)
}

func TestProgressSummaryTextLocalizesToolSummary(t *testing.T) {
	t.Parallel()

	content := progressSummaryText(gatewayStreamEvent{
		Type:      streamEventRunProgress,
		Stage:     streamStageRunningTool,
		Summary:   "Running exec_command",
		ElapsedMS: 2500,
	}, defaultProcessingMessage, nil)

	require.Equal(t, progressTextRunningCommand, content)
}

func TestProgressSummaryTextLocalizesSpecificToolSummary(
	t *testing.T,
) {
	t.Parallel()

	content := progressSummaryText(gatewayStreamEvent{
		Type:    streamEventRunProgress,
		Stage:   streamStageRunningTool,
		Summary: progressSummaryGoTestEN,
	}, defaultProcessingMessage, nil)
	require.Equal(t, "正在运行 go test", content)

	content = progressSummaryText(gatewayStreamEvent{
		Type:    streamEventRunProgress,
		Stage:   streamStageRunningTool,
		Summary: "Running fs_read_file",
	}, defaultProcessingMessage, nil)
	require.Equal(t, "正在读取工作区文件", content)
}

func TestProgressSummaryTextLocalizesDocumentPage(t *testing.T) {
	t.Parallel()

	content := progressSummaryText(gatewayStreamEvent{
		Type:    streamEventRunProgress,
		Stage:   streamStageReadingDocument,
		Summary: "Reading document page 3",
	}, defaultProcessingMessage, nil)

	require.Equal(t, "正在读取文件（第 3 页）", content)
}

func TestProgressSnapshotTextBuildsNarrativeSnapshot(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{
		started:  true,
		progress: progress.NewState(),
	}
	content := progressSnapshotText(gatewayStreamEvent{
		Type:      streamEventRunProgress,
		Stage:     streamStageRunningTool,
		Summary:   "Running exec_command",
		ElapsedMS: 2500,
	}, defaultProcessingMessage, state)

	require.Equal(
		t,
		statusPulseOne,
		content,
	)
}

func TestProgressSnapshotTextAnimatesStatusSuffix(t *testing.T) {
	t.Parallel()

	state := &replyStreamState{
		started:  true,
		progress: progress.NewState(),
	}

	first := progressSnapshotText(gatewayStreamEvent{
		Type:  streamEventRunProgress,
		Stage: streamStagePreparing,
	}, defaultProcessingMessage, state)
	markStatusPulseSent(state, progressTextPreparing)
	second := progressSnapshotText(gatewayStreamEvent{
		Type:  streamEventRunProgress,
		Stage: streamStagePreparing,
	}, defaultProcessingMessage, state)
	markStatusPulseSent(state, progressTextPreparing)
	third := progressSnapshotText(gatewayStreamEvent{
		Type:  streamEventRunProgress,
		Stage: streamStagePreparing,
	}, defaultProcessingMessage, state)
	markStatusPulseSent(state, progressTextPreparing)
	fourth := progressSnapshotText(gatewayStreamEvent{
		Type:  streamEventRunProgress,
		Stage: streamStagePreparing,
	}, defaultProcessingMessage, state)

	require.Equal(
		t,
		statusPulseOne,
		first,
	)
	require.Equal(
		t,
		statusPulseTwo,
		second,
	)
	require.Equal(
		t,
		statusPulseThree,
		third,
	)
	require.Equal(
		t,
		statusPulseOne,
		fourth,
	)
}

func TestProgressSnapshotTextKeepsStatusSuffixOnBaseChange(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{
		started:         true,
		progress:        progress.NewState(),
		statusBase:      progressTextPreparing,
		statusPulseStep: 2,
	}

	content := progressSnapshotText(
		gatewayStreamEvent{
			Type:  streamEventRunProgress,
			Stage: streamStageReadingDocument,
		},
		defaultProcessingMessage,
		state,
	)

	require.Equal(t, statusPulseThree, content)
}

func TestStreamPlaceholderAnimatesStatusSuffix(t *testing.T) {
	t.Parallel()

	state := &replyStreamState{}

	first := streamPlaceholder(defaultProcessingMessage, state)
	markStatusPulseSent(state, defaultProcessingMessage)
	second := streamPlaceholder(defaultProcessingMessage, state)
	markStatusPulseSent(state, defaultProcessingMessage)
	third := streamPlaceholder(defaultProcessingMessage, state)
	markStatusPulseSent(state, defaultProcessingMessage)
	fourth := streamPlaceholder(defaultProcessingMessage, state)

	require.Equal(t, statusPulseOne, first)
	require.Equal(t, statusPulseTwo, second)
	require.Equal(t, statusPulseThree, third)
	require.Equal(t, testProcessingPulse, fourth)
}

func TestNormalizeStatusBaseStripsPulseSuffixes(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"正在思考中",
		normalizeStatusBase("正在思考中 [●○○]"),
	)
	require.Equal(
		t,
		"正在思考中",
		normalizeStatusBase("正在思考中..."),
	)
}

func TestStatusPulseSnapshotSendsWithoutThrottling(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockStreamingSender{}
	state := &replyStreamState{
		id:              "stream1",
		sender:          sender,
		started:         true,
		startedAt:       time.Now().Add(-2 * time.Second),
		lastSnapshotAt:  time.Now().Add(-200 * time.Millisecond),
		statusBase:      normalizeStatusBase(defaultProcessingMessage),
		statusPulseStep: 1,
	}

	content := statusPulseSnapshotText(state)
	sent, err := sendReplySnapshot(
		context.Background(),
		sender,
		"chat1",
		state,
		content,
		false,
	)
	require.NoError(t, err)
	require.True(t, sent)
	require.Equal(
		t,
		statusPulseTwo,
		content,
	)
	require.Equal(t, 1, state.statusPulseStep)
	require.Len(t, sender.streamCalls, 1)
}

func TestShouldThrottleStreamSnapshotDisabled(t *testing.T) {
	t.Parallel()

	now := time.Unix(200, 0)
	state := &replyStreamState{
		started:        true,
		startedAt:      now.Add(-2 * time.Second),
		lastSnapshotAt: now.Add(-200 * time.Millisecond),
	}
	require.False(
		t,
		shouldThrottleStreamSnapshot(state, false, now),
	)

	require.False(
		t,
		shouldThrottleStreamSnapshot(state, false, now),
	)

	require.False(
		t,
		shouldThrottleStreamSnapshot(state, true, now),
	)
}

func TestNativeThinkingStreamContentUsesPlainFinalAnswer(t *testing.T) {
	t.Parallel()

	const answer = "hello"

	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		nativeThinkingStreamContent(nil, "", false),
	)
	require.Equal(
		t,
		streamNativeThinkingOpenTag+streamNativeThinkingText+
			streamNativeThinkingCloseTag+"\n"+answer,
		nativeThinkingStreamContent(nil, answer, false),
	)
	require.Contains(
		t,
		nativeThinkingStreamContent(nil, answer, false),
		streamNativeThinkingCloseTag,
	)
	require.Equal(t, answer, nativeThinkingStreamContent(nil, answer, true))
	require.NotContains(
		t,
		nativeThinkingStreamContent(nil, answer, true),
		streamNativeThinkingOpenTag,
	)
}

func TestNativeThinkingStreamContentNormalizesTaggedAnswer(t *testing.T) {
	t.Parallel()

	const (
		answer = "hello"
		hidden = "hidden"
	)

	closedTagged := nativeThinkingStreamContent(
		nil,
		streamNativeThinkingOpenTag+hidden+
			streamNativeThinkingCloseTag+"\n"+answer,
		true,
	)
	require.Equal(
		t,
		nativeThinkingStreamContent(nil, answer, true),
		closedTagged,
	)
	require.NotContains(t, closedTagged, hidden)
	require.Equal(t, answer, closedTagged)
	require.NotContains(t, closedTagged, streamNativeThinkingOpenTag)

	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		nativeThinkingStreamContent(
			nil,
			streamNativeThinkingOpenTag+hidden,
			false,
		),
	)
	unclosedFinal := nativeThinkingStreamContent(
		nil,
		streamNativeThinkingOpenTag+answer,
		true,
	)
	require.Equal(
		t,
		nativeThinkingStreamContent(nil, answer, true),
		unclosedFinal,
	)
	require.Equal(t, answer, unclosedFinal)
	require.NotContains(t, unclosedFinal, streamNativeThinkingOpenTag)
}

func TestNativeThinkingReasoningTextSkipsLocalStatus(t *testing.T) {
	t.Parallel()

	const (
		modelProcess = "我先确认目录结构"
		publicNote   = "再读取文件数量"
	)

	state := &replyStreamState{
		statusBase: progressTextPreparing,
	}

	require.Empty(t, nativeThinkingReasoningText(state))

	state.visibleNarrative = modelProcess
	state.publicPending.WriteString(publicNote)

	require.Equal(
		t,
		modelProcess+streamSectionSep+publicNote,
		nativeThinkingReasoningText(state),
	)
	require.NotContains(
		t,
		nativeThinkingReasoningText(state),
		progressTextPreparing,
	)
}

func TestSendNativeThinkingProcessSnapshotSkipsReplyPrefix(t *testing.T) {
	t.Parallel()

	const (
		prefix  = "> prefix"
		process = "我先确认仓库结构"
	)

	sender := &mockStreamingSender{}
	state := &replyStreamState{
		id:             "stream1",
		started:        true,
		nativeThinking: true,
		displayPrefix:  prefix,
	}
	state.publicPending.WriteString(process)

	sent, err := sendNativeThinkingProcessSnapshot(
		context.Background(),
		sender,
		"chat1",
		state,
	)
	require.NoError(t, err)
	require.True(t, sent)
	require.Len(t, sender.streamCalls, 1)
	require.Equal(
		t,
		streamNativeThinkingOpenTag+process,
		sender.streamCalls[0].content,
	)
	require.NotContains(t, sender.streamCalls[0].content, prefix)
}

func TestShouldThrottleNativeThinkingSnapshotCoalescesDeltas(
	t *testing.T,
) {
	t.Parallel()

	now := time.Unix(200, 0)
	state := &replyStreamState{
		nativeThinking: true,
		started:        true,
		lastSnapshotAt: now.Add(-100 * time.Millisecond),
		lastSent:       nativeThinkingStreamContent(nil, "he", false),
	}

	require.True(
		t,
		shouldThrottleNativeThinkingSnapshot(
			state,
			"hello",
			false,
			now,
		),
	)
	require.False(
		t,
		shouldThrottleNativeThinkingSnapshot(
			state,
			"hello.",
			false,
			now,
		),
	)
	require.False(
		t,
		shouldThrottleNativeThinkingSnapshot(
			state,
			"hello",
			true,
			now,
		),
	)
}

func TestStableNarrativeSnapshotPrefersCommittedReply(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{}
	state.visibleNarrative = "我先看一下仓库结构"

	require.Equal(
		t,
		"我先看一下仓库结构",
		stableNarrativeSnapshot(state),
	)

	state.builder.WriteString("最终正文")

	require.Equal(t, "最终正文", stableNarrativeSnapshot(state))
}

func TestShouldFlushIdlePreAnswerBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(400, 0)
	state := &replyStreamState{}
	state.replyPending.WriteString("我先拉取 PR 列表")
	state.preAnswerLastAt = now.Add(-streamIdleFlushAfter)

	require.True(
		t,
		shouldFlushIdlePreAnswerBoundary(
			streamSnapshotModeFull,
			state,
			now,
		),
	)

	state.builder.WriteString("最终正文")
	require.False(
		t,
		shouldFlushIdlePreAnswerBoundary(
			streamSnapshotModeFull,
			state,
			now,
		),
	)
}

func TestFlushIdlePreAnswerBoundaryClearsPendingBuffers(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{}
	state.replyPending.WriteString("我先拉取 PR 列表")
	state.publicPending.WriteString("public")
	state.thoughtPending.WriteString("thought")
	state.preAnswerLastAt = time.Unix(500, 0)

	require.True(t, flushPreAnswerBoundary(state))
	require.Equal(t, "我先拉取 PR 列表", state.visibleNarrative)
	require.Zero(t, state.replyPending.Len())
	require.Zero(t, state.publicPending.Len())
	require.Zero(t, state.thoughtPending.Len())
	require.True(t, state.preAnswerLastAt.IsZero())
}

func TestFlushPreAnswerBoundaryReplacesVisibleReplySegments(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{
		visibleNarrative: "第一段",
		visibleReplyText: true,
	}
	state.replyPending.WriteString("第二段")

	require.True(t, flushPreAnswerBoundary(state))
	require.Equal(t, "第二段", state.visibleNarrative)
}

func TestFlushPreAnswerBoundaryKeepsLatestPreviewText(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{
		visibleNarrative: "第一段",
		visibleReplyText: true,
	}
	state.replyPending.WriteString("第二段。")

	require.True(t, flushPreAnswerBoundary(state))
	require.Equal(t, "第二段。", state.visibleNarrative)
}

func TestPrepareReplyPendingSegmentKeepsVisibleReplyText(
	t *testing.T,
) {
	t.Parallel()

	state := &replyStreamState{
		visibleNarrative: "第一段",
		visibleReplyText: true,
	}

	require.False(t, prepareReplyPendingSegment(state))
	require.Equal(t, "第一段", state.visibleNarrative)
}

func TestCallGatewayAndReplyStreamErrorSendsMarkdown(t *testing.T) {
	t.Parallel()

	const requestID = "req1"

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{
				Type: streamEventRunError,
				Error: &gatewayStreamError{
					Message: "boom",
				},
			},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw:      gw,
		cfg:     channelCfg{EnableStream: true},
		botMode: botModeAI,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1"},
		"hello",
		nil,
		nil,
		"user1",
		requestID,
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Equal(
		t,
		appendGatewayErrorID("boom", requestID),
		sender.lastMarkdown,
	)
	require.Empty(t, sender.streamCalls)
}

func TestCallGatewayAndReplyStreamsCompletionWithoutDelta(t *testing.T) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.True(t, gw.streamCalled)
	require.Empty(t, sender.lastMarkdown)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		statusPulseOne,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.Equal(t, "hello", sender.streamCalls[1].content)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamDonePreservesReplyText(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		reply: runnerExecutionErrorMessageEN,
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventMsgDone,
				Reply: runnerExecutionErrorMessageEN,
			},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		runnerExecutionErrorMessageEN,
		sender.streamCalls[1].content,
	)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamDoneDoesNotRetryReplyText(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:  streamEventMsgDone,
				Reply: runnerExecutionErrorMessageEN,
			},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.False(t, gw.sendCalled)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		runnerExecutionErrorMessageEN,
		sender.streamCalls[1].content,
	)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamOpenErrorDoesNotFallback(t *testing.T) {
	t.Parallel()

	const requestID = "req1"

	gw := &fakeStreamGateway{
		streamErr: context.DeadlineExceeded,
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw:      gw,
		cfg:     channelCfg{EnableStream: true},
		botMode: botModeAI,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		requestID,
		"session1",
		sender,
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, gw.streamCalled)
	require.False(t, gw.sendCalled)
	require.Equal(
		t,
		appendGatewayErrorID(
			context.DeadlineExceeded.Error(),
			requestID,
		),
		sender.lastMarkdown,
	)
	require.Empty(t, sender.streamCalls)
}

func TestCallGatewayAndReplyStreamErrorFinishesStream(t *testing.T) {
	t.Parallel()

	const requestID = "req1"

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type: streamEventRunError,
				Error: &gatewayStreamError{
					Message: "boom",
				},
			},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		requestID,
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Empty(t, sender.lastMarkdown)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		statusPulseOne,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.Equal(
		t,
		appendGatewayErrorID("boom", requestID),
		sender.streamCalls[1].content,
	)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamErrorPreservesProviderMessage(
	t *testing.T,
) {
	t.Parallel()

	const requestID = "req1"

	gw := &fakeStreamGateway{
		reply: runnerExecutionErrorMessageEN,
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type: streamEventRunError,
				Error: &gatewayStreamError{
					Message: testProviderStreamError,
				},
			},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		requestID,
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		appendGatewayErrorID(
			testProviderStreamError,
			requestID,
		),
		sender.streamCalls[1].content,
	)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamErrorDoesNotRetryProviderMessage(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type: streamEventRunError,
				Error: &gatewayStreamError{
					Message: testProviderStreamError,
				},
			},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.False(t, gw.sendCalled)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		appendGatewayErrorID(
			testProviderStreamError,
			"req1",
		),
		sender.streamCalls[1].content,
	)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyStreamCanceledFinishesStream(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventRunCanceled},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Empty(t, sender.lastMarkdown)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		testProcessingPulse,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.Equal(
		t,
		streamCanceledText,
		sender.streamCalls[1].content,
	)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyIgnoresProgressAckTimeout(
	t *testing.T,
) {
	t.Parallel()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDelta, Delta: "he"},
			{Type: streamEventMsgDone, Reply: "hello"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{
		streamErrs: []error{
			errReplyAckTimeout,
		},
	}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		testProcessingPulse,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.Equal(t, "hello", sender.streamCalls[1].content)
	require.True(t, sender.streamCalls[1].finish)
	require.False(t, gw.cancelCalled)
}

func TestCallGatewayAndReplyCancelsRunAfterFinalSendFailure(
	t *testing.T,
) {
	t.Parallel()

	finalSendErr := errors.New("final send failed")
	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDone, Reply: "hello"},
		},
		cancelResult: true,
	}
	sender := &mockStreamingSender{
		streamErrs: []error{
			nil,
			finalSendErr,
		},
		markdownErr: finalSendErr,
	}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.ErrorIs(t, err, finalSendErr)
	require.True(t, gw.cancelCalled)
	require.Empty(t, sender.markdownCalls)
}

func TestCallGatewayAndReplyFinishesOnRecoverableFinalSendError(
	t *testing.T,
) {
	t.Parallel()

	const finalReply = "hello"

	finalSendErr := &replyAckError{
		code: replyAckConflictErrCode,
		msg:  "version conflict",
	}
	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{Type: streamEventMsgDone, Reply: finalReply},
		},
		cancelResult: true,
	}
	sender := &mockStreamingSender{
		streamErrs: []error{
			nil,
			finalSendErr,
		},
	}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.False(t, gw.cancelCalled)
	require.Empty(t, sender.markdownCalls)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(t, finalReply, sender.streamCalls[1].content)
	require.True(t, sender.streamCalls[1].finish)
}

func TestCallGatewayAndReplyFallsBackWithoutStream(t *testing.T) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw:      gw,
		cfg:     channelCfg{EnableStream: true},
		botMode: botModeAI,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Empty(t, sender.lastMarkdown)
	require.NotEmpty(t, sender.streamCalls)
	last := sender.streamCalls[len(sender.streamCalls)-1]
	require.Equal(t, "ok", last.content)
	require.True(t, last.finish)
}

func TestCallGatewayAndReplyWithStateStreamsFallbackReply(
	t *testing.T,
) {
	t.Parallel()

	gw := &recordingGateway{reply: "ok"}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
		},
		botMode:           botModeAI,
		processingMessage: defaultProcessingMessage,
	}
	state := newReplyStreamState(
		"req1",
		"msg1",
		sender,
		true,
	)

	err := ch.callGatewayAndReplyWithState(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"hello",
		nil,
		nil,
		"user1",
		"req1",
		"session1",
		nil,
		sender,
		state,
	)
	require.NoError(t, err)
	require.True(t, gw.sendCalled)
	require.Empty(t, sender.lastMarkdown)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		testProcessingPulse,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.Equal(t, "ok", sender.streamCalls[1].content)
	require.True(t, sender.streamCalls[1].finish)
}

func TestFinishReplyStreamRotatesAfterLifetime(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockStreamingSender{}
	state := newReplyStreamState(
		"req1",
		"msg1",
		sender,
		true,
	)
	require.NotNil(t, state)

	state.started = true
	state.startedAt = time.Now().Add(
		-replyStreamFallbackAfter - time.Second,
	)
	state.lastSent = defaultProcessingMessage

	reply := strings.Repeat("x", maxReplyRunes+10)
	err := finishReplyStream(
		context.Background(),
		sender,
		"chat1",
		state,
		reply,
	)
	require.NoError(t, err)
	require.True(t, state.finished)
	require.Empty(t, sender.markdownCalls)
	require.Len(t, sender.streamCalls, 2)
	require.True(t, sender.streamCalls[0].finish)
	require.Equal(
		t,
		streamDeadlineThinkLine+"\n\n"+streamDeadlineNotice,
		sender.streamCalls[0].content,
	)
	require.NotEqual(
		t,
		sender.streamCalls[0].streamID,
		sender.streamCalls[1].streamID,
	)
	require.True(t, sender.streamCalls[1].finish)
	require.Equal(t, reply, sender.streamCalls[1].content)
}

func TestRotateReplyStreamSegmentClosesNativeThinkingHandoff(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockStreamingSender{}
	state := newReplyStreamState(
		"req1",
		"msg1",
		sender,
		true,
	)
	require.NotNil(t, state)

	now := time.Now()
	state.nativeThinking = true
	state.started = true
	state.startedAt = now.Add(-replyStreamFallbackAfter)
	state.lastSent = streamNativeThinkingPlaceholder

	err := rotateReplyStreamSegment(
		context.Background(),
		sender,
		"chat1",
		state,
		now,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 1)
	require.True(t, sender.streamCalls[0].finish)
	require.Equal(
		t,
		nativeThinkingStreamContent(
			nil,
			richStreamDeadlineContent(),
			true,
		),
		sender.streamCalls[0].content,
	)
	require.Equal(
		t,
		richStreamDeadlineContent(),
		sender.streamCalls[0].content,
	)
	require.NotContains(
		t,
		sender.streamCalls[0].content,
		streamNativeThinkingOpenTag,
	)
}

func TestSendReplySnapshotDisablesProgressOnConflict(
	t *testing.T,
) {
	t.Parallel()

	conflictErr := &replyAckError{
		code: replyAckConflictErrCode,
		msg:  "version conflict",
	}
	sender := &mockStreamingSender{
		streamErr: conflictErr,
	}
	state := newReplyStreamState(
		"req1",
		"msg1",
		sender,
		true,
	)
	require.NotNil(t, state)

	sent, err := sendReplySnapshot(
		context.Background(),
		sender,
		"chat1",
		state,
		testProcessingPulse,
		false,
	)
	require.NoError(t, err)
	require.True(t, sent)
	require.True(t, state.progressDisabled)
	require.True(t, state.started)
	require.Equal(t, testProcessingPulse, state.lastSent)
}

func TestSendReplySnapshotAcceptsRecoverableFinalSendError(
	t *testing.T,
) {
	t.Parallel()

	const finalReply = "hello"

	conflictErr := &replyAckError{
		code: replyAckConflictErrCode,
		msg:  "version conflict",
	}
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "ack timeout",
			err:  errReplyAckTimeout,
		},
		{
			name: "ack conflict",
			err:  conflictErr,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sender := &mockStreamingSender{
				streamErr: tt.err,
			}
			state := newReplyStreamState(
				"req1",
				"msg1",
				sender,
				true,
			)
			require.NotNil(t, state)

			sent, err := sendReplySnapshot(
				context.Background(),
				sender,
				"chat1",
				state,
				finalReply,
				true,
			)
			require.NoError(t, err)
			require.True(t, sent)
			require.True(t, state.finished)
			require.Equal(t, finalReply, state.lastSent)
			require.Empty(t, sender.markdownCalls)
			require.Len(t, sender.streamCalls, 1)
			require.Equal(t, finalReply, sender.streamCalls[0].content)
			require.True(t, sender.streamCalls[0].finish)
		})
	}
}

func TestSendReplySnapshotRotatesStreamAtDeadline(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockStreamingSender{}
	state := newReplyStreamState(
		"req1",
		"msg1",
		sender,
		true,
	)
	require.NotNil(t, state)

	state.started = true
	state.startedAt = time.Now().Add(
		-replyStreamFallbackAfter - time.Second,
	)
	state.statusBase = progressTextReadingDocument

	sent, err := sendReplySnapshot(
		context.Background(),
		sender,
		"chat1",
		state,
		renderProgressSnapshot(
			state,
			progressTextReadingDocument,
			true,
		),
		false,
	)
	require.NoError(t, err)
	require.True(t, sent)
	require.False(t, state.finished)
	require.Len(t, sender.streamCalls, 2)
	require.True(t, sender.streamCalls[0].finish)
	require.Equal(
		t,
		streamDeadlineThinkLine+"\n\n"+streamDeadlineNotice,
		sender.streamCalls[0].content,
	)
	require.NotEqual(
		t,
		sender.streamCalls[0].streamID,
		sender.streamCalls[1].streamID,
	)
	require.False(t, sender.streamCalls[1].finish)
	require.Equal(
		t,
		statusPulseOne,
		sender.streamCalls[1].content,
	)
	require.Empty(t, sender.markdownCalls)
}

func TestSendReplySnapshotSanitizesDeepSeekBoundaryToken(
	t *testing.T,
) {
	t.Parallel()

	sender := &mockStreamingSender{}
	state := &replyStreamState{
		id: "stream1",
		rewrite: func(content string) string {
			return sanitizeReplyModelOutput(
				"deepseek-v3.2",
				content,
			)
		},
	}

	sent, err := sendReplySnapshot(
		context.Background(),
		sender,
		"chat1",
		state,
		"你好"+deepSeekEOSMarkerWide,
		true,
	)
	require.NoError(t, err)
	require.True(t, sent)
	require.Len(t, sender.streamCalls, 1)
	require.Equal(t, "你好", sender.streamCalls[0].content)
}

func TestCallGatewayAndReplyAttachmentFailureClosesNativeThinkingStream(
	t *testing.T,
) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	gw := &fakeStreamGateway{}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		connectionMode:    connectionModeWebSocket,
		processingMessage: defaultProcessingMessage,
		mediaClient:       server.Client(),
		mediaURLValidator: allowAnyMediaURL,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{
			ChatID: "chat1",
			MsgID:  "msg1",
		},
		"总结这个文件",
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					URL: server.URL,
				},
			},
		},
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.False(t, gw.streamCalled)
	require.False(t, gw.sendCalled)
	require.Len(t, sender.streamCalls, 2)
	require.Equal(
		t,
		streamNativeThinkingPlaceholder,
		sender.streamCalls[0].content,
	)
	require.False(t, sender.streamCalls[0].finish)
	require.NotEmpty(t, sender.streamCalls[0].feedbackID)
	require.Equal(
		t,
		defaultAttachmentReadFailedMessage,
		sender.streamCalls[1].content,
	)
	require.NotContains(
		t,
		sender.streamCalls[1].content,
		streamNativeThinkingOpenTag,
	)
	require.True(t, sender.streamCalls[1].finish)
	require.Empty(t, sender.streamCalls[1].feedbackID)
	require.Empty(t, sender.markdownCalls)
}

func TestCallGatewayAndReplyStreamsImageContentParts(t *testing.T) {
	t.Parallel()

	imageData := mustBase64Decode(t, testPNGBase64)
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageData)
	}))
	defer server.Close()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventMsgDelta, Delta: "ok"},
			{Type: streamEventMsgDone, Reply: "ok"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		mediaClient:       server.Client(),
		mediaURLValidator: allowAnyMediaURL,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg1"},
		"这张图是什么？",
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeImage,
				Image: &gwproto.ImagePart{
					URL: server.URL,
				},
			},
		},
		nil,
		"user1",
		"req1",
		"session1",
		sender,
	)
	require.NoError(t, err)
	require.True(t, gw.streamCalled)
	require.False(t, gw.sendCalled)
	require.Len(t, gw.lastReq.ContentParts, 2)
	require.NotNil(t, gw.lastReq.ContentParts[0].Image)
	require.Equal(
		t,
		imageData,
		gw.lastReq.ContentParts[0].Image.Data,
	)
	require.NotNil(t, gw.lastReq.ContentParts[1].File)
	require.Equal(
		t,
		imageData,
		gw.lastReq.ContentParts[1].File.Data,
	)
	require.NotEmpty(t, sender.streamCalls)
	require.True(
		t,
		sender.streamCalls[len(sender.streamCalls)-1].finish,
	)
}

func TestCallGatewayAndReplyStreamsFileContentParts(t *testing.T) {
	t.Parallel()

	fileData := []byte("%PDF-1.7\nhello")
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set(
			"Content-Disposition",
			`attachment; filename="report.pdf"`,
		)
		_, _ = w.Write(fileData)
	}))
	defer server.Close()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventMsgDelta, Delta: "done"},
			{Type: streamEventMsgDone, Reply: "done"},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		mediaClient:       server.Client(),
		mediaURLValidator: allowAnyMediaURL,
	}

	err := ch.callGatewayAndReply(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg2"},
		"总结这个文件",
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					URL: server.URL,
				},
			},
		},
		nil,
		"user1",
		"req2",
		"session2",
		sender,
	)
	require.NoError(t, err)
	require.True(t, gw.streamCalled)
	require.False(t, gw.sendCalled)
	require.Len(t, gw.lastReq.ContentParts, 1)
	require.NotNil(t, gw.lastReq.ContentParts[0].File)
	require.Equal(
		t,
		fileData,
		gw.lastReq.ContentParts[0].File.Data,
	)
	require.Equal(
		t,
		"report.pdf",
		gw.lastReq.ContentParts[0].File.Filename,
	)
	require.NotEmpty(t, sender.streamCalls)
	require.True(
		t,
		sender.streamCalls[len(sender.streamCalls)-1].finish,
	)
}

func TestCallGatewayAndReplyStreamsMergeHintsAndRewritesNames(
	t *testing.T,
) {
	t.Parallel()

	fileData := []byte("%PDF-1.7\nhello")
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		w.Header().Set("Content-Type", mimeTypePDF)
		if r.URL.Path == "/0.pdf" {
			w.Header().Set(
				"Content-Disposition",
				`attachment; filename="0.pdf"`,
			)
		}
		_, _ = w.Write(fileData)
	}))
	defer server.Close()

	gw := &fakeStreamGateway{
		events: []fakeStreamEvent{
			{Type: streamEventRunStarted},
			{
				Type:    streamEventRunProgress,
				Stage:   streamStageRunningTool,
				Summary: "Running exec_command",
			},
			{
				Type:  streamEventMsgDone,
				Reply: "顺序：attachment.pdf -> 0.pdf",
			},
			{Type: streamEventRunDone},
		},
	}
	sender := &mockStreamingSender{}
	ch := &Channel{
		gw: gw,
		cfg: channelCfg{
			EnableStream:       true,
			StreamSnapshotMode: streamSnapshotModeFull,
			ReplyPrefix: replyPrefixCfg{
				Enabled: boolPtr(false),
			},
		},
		botMode:           botModeAI,
		mediaClient:       server.Client(),
		mediaURLValidator: allowAnyMediaURL,
	}
	state := newReplyStreamState(
		"req-merge",
		"msg-merge",
		sender,
		true,
	)

	err := ch.callGatewayAndReplyWithState(
		context.Background(),
		WebhookMessage{ChatID: "chat1", MsgID: "msg-merge"},
		"合并这两个 pdf",
		[]gwproto.ContentPart{
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					URL: server.URL + "/first",
				},
			},
			{
				Type: gwproto.PartTypeFile,
				File: &gwproto.FilePart{
					URL: server.URL + "/0.pdf",
				},
			},
		},
		nil,
		"user1",
		"req-merge",
		"session-merge",
		nil,
		sender,
		state,
	)
	require.NoError(t, err)
	require.Len(t, sender.streamCalls, 4)
	require.Equal(
		t,
		statusPulseOne,
		sender.streamCalls[0].content,
	)
	require.Equal(
		t,
		statusPulseTwo,
		sender.streamCalls[1].content,
	)
	require.Equal(
		t,
		statusPulseThree,
		sender.streamCalls[2].content,
	)
	require.Equal(
		t,
		"顺序：第 1 个上传的 PDF -> 0.pdf",
		sender.streamCalls[3].content,
	)
	require.True(t, sender.streamCalls[3].finish)
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
}
