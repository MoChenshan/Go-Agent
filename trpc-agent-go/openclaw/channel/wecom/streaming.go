package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/progress"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
)

const (
	streamEventRunStarted   = "run.started"
	streamEventRunIgnored   = "run.ignored"
	streamEventRunProgress  = "run.progress"
	streamEventPublicDelta  = "public.delta"
	streamEventPublicDone   = "public.completed"
	streamEventThoughtDelta = "thought.delta"
	streamEventThoughtDone  = "thought.completed"
	streamEventMsgDelta     = "message.delta"
	streamEventMsgDone      = "message.completed"
	streamEventRunCanceled  = "run.canceled"
	streamEventRunDone      = "run.completed"
	streamEventRunError     = "run.error"

	streamStagePreparing          = "preparing"
	streamStageReadingDocument    = "reading_document"
	streamStageReadingSpreadsheet = "reading_spreadsheet"
	streamStageRunningTool        = "running_tool"
	streamStageSummarizing        = "summarizing"

	streamToolStatusRunning   = "running"
	streamToolStatusCompleted = "completed"

	progressTextPreparing          = "正在准备请求"
	progressTextReadingDocument    = "正在读取文件"
	progressTextReadingSpreadsheet = "正在提取表格内容"
	progressTextRunningTool        = "正在运行工具"
	progressTextSummarizing        = "正在整理答案"
	progressTextRunningCommand     = "正在执行本地命令"

	replyStreamIDPrefix = "wecom-stream-"
	defaultStreamMode   = streamSnapshotModeFull

	streamSnapshotModeFull        = "full"
	streamSnapshotModeContentOnly = "content_only"
	streamSnapshotModeFinalOnly   = "final_only"

	defaultStreamDisplayMode        = streamDisplayModeNativeThinking
	streamDisplayModeNativeThinking = "native_thinking"
	streamDisplayModeLegacy         = "legacy"

	streamNativeThinkingOpenTag  = "<think>"
	streamNativeThinkingCloseTag = "</think>"
	streamNativeThinkingMarker   = streamNativeThinkingOpenTag +
		streamNativeThinkingCloseTag
	streamNativeThinkingText        = "思考中..."
	streamNativeThinkingPlaceholder = streamNativeThinkingOpenTag +
		streamNativeThinkingText
	streamNativeThinkingDoneText         = "处理完成。"
	streamNativeThinkingToolLabel        = "工具调用"
	streamNativeThinkingToolSep          = "："
	streamNativeThinkingToolDetailSep    = " · "
	streamNativeThinkingToolQuerySep     = "?"
	streamNativeThinkingToolNameMaxRunes = 64
	streamNativeThinkingToolInfoMaxRunes = 96

	streamNativeThinkingMinInterval = 360 * time.Millisecond
	streamNativeThinkingMinGrowth   = 16

	streamFeedbackIDPrefix   = "wecom-feedback-"
	streamFeedbackIDMaxBytes = 256
	streamFeedbackIDHashBase = 16

	streamSnapshotMinInterval = 120 * time.Millisecond
	streamIdleCheckInterval   = 120 * time.Millisecond
	streamIdleFlushAfter      = 6800 * time.Millisecond
	streamStatusPulseInterval = time.Second
	replyStreamFallbackAfter  = 5*time.Minute + 30*time.Second

	progressSummaryPrepareEN   = "Preparing request"
	progressSummaryDocumentEN  = "Reading document"
	progressSummarySheetEN     = "Reading spreadsheet"
	progressSummaryToolEN      = "Running local tool"
	progressSummaryAnsweringEN = "Preparing final answer"
	progressSummaryRunPrefixEN = "Running "
	progressSummaryGoTestEN    = "Running go test"
	progressSummaryPytestEN    = "Running pytest"
	progressSummaryNPMTestEN   = "Running npm test"
	progressSummaryGitEN       = "Running git command"
	progressSummaryInspectEN   = "Inspecting workspace"
	toolNameExecCommand        = "exec_command"
	toolDetailGoTest           = "go test"
	toolDetailPytest           = "pytest"
	toolDetailNPMTest          = "npm test"
	toolDetailGit              = "git"
	toolDetailInspect          = "inspect"
	toolNameReadFile           = "fs_read_file"
	toolNameSaveFile           = "fs_save_file"
	toolNameListDir            = "fs_list_dir"
	toolNameSearch             = "fs_search"
	toolNameApplyPatch         = "apply_patch"
	streamCanceledText         = "已取消当前请求。"
	streamSectionSep           = "\n\n"

	statusPulseOne         = "."
	statusPulseTwo         = ".."
	statusPulseThree       = "..."
	statusPulseCN          = "…"
	statusPulseCompatSep   = " "
	statusPulseCompatOne   = "[●○○]"
	statusPulseCompatTwo   = "[○●○]"
	statusPulseCompatThree = "[○○●]"

	streamDeadlineThinkLine = "任务仍在继续"
	streamDeadlineNotice    = "由于企业微信单次流式回复最长约 6 分钟，" +
		"这条处理中消息先结束，后续结果会继续发送。"
)

var (
	contextIfaceType  = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorIfaceType    = reflect.TypeOf((*error)(nil)).Elem()
	requestType       = reflect.TypeOf(gwclient.MessageRequest{})
	streamOptionsType = reflect.TypeOf(
		(*gwclient.MessageStreamOptions)(nil),
	)
	replyStreamSeq atomic.Uint64
	statusPulseSeq = [...]string{
		statusPulseOne,
		statusPulseTwo,
		statusPulseThree,
	}
	statusPulseTrimSeq = [...]string{
		statusPulseThree,
		statusPulseTwo,
		statusPulseOne,
	}
	statusPulseCompatSeq = [...]string{
		statusPulseCompatOne,
		statusPulseCompatTwo,
		statusPulseCompatThree,
	}
)

type gatewayStreamEvent struct {
	Type             string                  `json:"type"`
	RequestID        string                  `json:"request_id,omitempty"`
	Delta            string                  `json:"delta,omitempty"`
	Reply            string                  `json:"reply,omitempty"`
	Usage            *gwclient.Usage         `json:"usage,omitempty"`
	Stage            string                  `json:"stage,omitempty"`
	Summary          string                  `json:"summary,omitempty"`
	ElapsedMS        int64                   `json:"elapsed_ms,omitempty"`
	Ignored          bool                    `json:"ignored,omitempty"`
	Object           string                  `json:"object,omitempty"`
	ToolName         string                  `json:"tool_name,omitempty"`
	ToolDetail       string                  `json:"tool_detail,omitempty"`
	ToolCallID       string                  `json:"tool_call_id,omitempty"`
	ToolStatus       string                  `json:"tool_status,omitempty"`
	Thinking         string                  `json:"thinking,omitempty"`
	Reasoning        string                  `json:"reasoning,omitempty"`
	ReasoningContent string                  `json:"reasoning_content,omitempty"`
	Thoughts         string                  `json:"thoughts,omitempty"`
	Message          string                  `json:"message,omitempty"`
	ToolCalls        []gatewayStreamToolCall `json:"tool_calls,omitempty"`
	Error            *gatewayStreamError     `json:"error,omitempty"`
}

type gatewayStreamError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

type gatewayStreamToolCall struct {
	ID       string                        `json:"id,omitempty"`
	Name     string                        `json:"name,omitempty"`
	ToolName string                        `json:"tool_name,omitempty"`
	Function *gatewayStreamToolCallPayload `json:"function,omitempty"`
}

type gatewayStreamToolCallPayload struct {
	Name string `json:"name,omitempty"`
}

type nativeThinkingToolActivity struct {
	id     string
	name   string
	detail string
}

type replyStreamState struct {
	id                  string
	sender              messageSender
	started             bool
	startedAt           time.Time
	lastSnapshotAt      time.Time
	finished            bool
	progressDisabled    bool
	lastSent            string
	builder             strings.Builder
	replyPending        strings.Builder
	publicBuilder       strings.Builder
	publicPending       strings.Builder
	thoughtBuilder      strings.Builder
	thoughtPending      strings.Builder
	toolActivityBuilder strings.Builder
	replyPendingSince   time.Time
	preAnswerLastAt     time.Time
	publicPendingSince  time.Time
	thoughtPendingSince time.Time
	visibleNarrative    string
	visibleReplyText    bool
	displayPrefix       string
	rewrite             func(string) string
	statusBase          string
	statusPulseStep     int
	toolActivityCount   int
	toolActivityKeys    map[string]struct{}
	progress            *progress.State
	nativeThinking      bool
	feedbackID          string
	feedbackSent        bool
}

type gatewayStreamReadResult struct {
	evt gatewayStreamEvent
	err error
}

func normalizeStreamSnapshotMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "":
		return defaultStreamMode
	case streamSnapshotModeFull:
		return streamSnapshotModeFull
	case streamSnapshotModeFinalOnly:
		return streamSnapshotModeFinalOnly
	case streamSnapshotModeContentOnly:
		return streamSnapshotModeContentOnly
	default:
		return defaultStreamMode
	}
}

func normalizeStreamDisplayMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", streamDisplayModeNativeThinking:
		return defaultStreamDisplayMode
	case streamDisplayModeLegacy:
		return streamDisplayModeLegacy
	default:
		return streamDisplayModeLegacy
	}
}

func (c *Channel) usesNativeThinkingStream() bool {
	if c == nil {
		return false
	}
	if c.botMode != botModeAI ||
		c.connectionMode != connectionModeWebSocket {
		return false
	}
	return normalizeStreamDisplayMode(c.cfg.StreamDisplayMode) ==
		streamDisplayModeNativeThinking
}

func streamModeSendsPlaceholder(mode string) bool {
	return mode == streamSnapshotModeFull
}

func streamModeSendsProgress(mode string) bool {
	return mode == streamSnapshotModeFull
}

func streamModeSendsDeltas(mode string) bool {
	return mode != streamSnapshotModeFinalOnly
}

func (c *Channel) streamGatewayReply(
	ctx context.Context,
	msg WebhookMessage,
	gwReq gwclient.MessageRequest,
	sender messageSender,
	state *replyStreamState,
) (bool, error) {
	streamSender, ok := sender.(streamingSender)
	if !ok {
		return false, nil
	}

	stream, ok, err := openGatewayStream(
		ctx,
		c.gw,
		gwReq,
		state != nil && state.nativeThinking,
	)
	if !ok {
		return false, nil
	}
	if err != nil {
		rawErrMsg := err.Error()
		errMsg := sanitizeGatewayErrorMessage(
			rawErrMsg,
			gwReq.RequestID,
		)
		logGatewayFailure(
			ctx,
			"gateway stream open",
			gwReq.RequestID,
			rawErrMsg,
			err,
		)
		c.runStatus.fail(
			gwReq.Thread,
			gwReq.RequestID,
			errMsg,
			"",
		)
		if state != nil && state.started {
			_ = finishReplyStream(
				ctx,
				streamSender,
				msg.ChatID,
				state,
				errMsg,
			)
			return true, err
		}
		_ = sender.SendMarkdown(
			ctx,
			msg.ChatID,
			errMsg,
		)
		return true, err
	}

	if state == nil {
		state = &replyStreamState{
			id:       buildReplyStreamID(),
			sender:   sender,
			progress: progress.NewState(),
			feedbackID: buildStreamFeedbackID(
				gwReq.RequestID,
				msg.MsgID,
			),
		}
	}
	state.nativeThinking = c.usesNativeThinkingStream()
	placeholder := currentStatusText(
		c.processingMessage,
		state,
	)
	snapshotMode := normalizeStreamSnapshotMode(
		c.cfg.StreamSnapshotMode,
	)
	eventCh := openGatewayStreamEvents(ctx, stream)
	idleTicker := time.NewTicker(streamIdleCheckInterval)
	defer idleTicker.Stop()
	pulseTicker := time.NewTicker(streamStatusPulseInterval)
	defer pulseTicker.Stop()

	for eventCh != nil {
		select {
		case result, ok := <-eventCh:
			if !ok {
				eventCh = nil
				continue
			}
			if result.err != nil {
				return true, c.handleStreamReadError(
					ctx,
					msg,
					sender,
					streamSender,
					state,
					gwReq.Thread,
					gwReq.RequestID,
					result.err,
				)
			}

			evt := result.evt
			switch evt.Type {
			case streamEventRunStarted:
				sendStartedSnapshot := state == nil ||
					!state.started ||
					strings.TrimSpace(state.lastSent) == ""
				if sendStartedSnapshot {
					placeholder = streamPlaceholder(
						c.processingMessage,
						state,
					)
				} else {
					placeholder = strings.TrimSpace(
						state.lastSent,
					)
				}
				c.runStatus.start(
					gwReq.Thread,
					gwReq.RequestID,
					visibleRunStatusPlaceholder(
						placeholder,
						c.processingMessage,
						state,
					),
				)
				if placeholder == "" && !state.nativeThinking {
					continue
				}
				if !sendStartedSnapshot {
					continue
				}
				if !streamModeSendsPlaceholder(snapshotMode) {
					continue
				}
				if state.nativeThinking {
					sent, err := sendReplySnapshot(
						ctx,
						streamSender,
						msg.ChatID,
						state,
						streamNativeThinkingPlaceholder,
						false,
					)
					if err != nil {
						return true, err
					}
					if sent {
						markStatusPulseSent(
							state,
							c.processingMessage,
						)
					}
					continue
				}
				content := recordProgressActivity(
					state,
					placeholder,
				)
				content = renderCurrentSnapshot(
					state,
					content,
					true,
				)
				sent, err := sendReplySnapshot(
					ctx,
					streamSender,
					msg.ChatID,
					state,
					content,
					false,
				)
				if err != nil {
					return true, err
				}
				if sent {
					markStatusPulseSent(
						state,
						placeholder,
					)
				}
			case streamEventRunProgress:
				stableChanged := false
				if shouldFlushPreAnswerBoundary(evt, state) {
					stableChanged = flushPreAnswerBoundary(state)
				}
				if stableChanged {
					resetStatusPulseCycle(state)
				}
				toolChanged := recordNativeThinkingToolActivity(
					state,
					evt,
				)
				if toolChanged {
					resetStatusPulseCycle(state)
					state.lastSnapshotAt = time.Time{}
				}
				summary := progressSummaryText(
					evt,
					placeholder,
					state,
				)
				c.runStatus.progress(
					gwReq.Thread,
					gwReq.RequestID,
					evt.Stage,
					summary,
					time.Duration(evt.ElapsedMS)*time.Millisecond,
				)
				if state.nativeThinking {
					if !streamModeSendsProgress(snapshotMode) {
						continue
					}
					sent, err := sendNativeThinkingProcessSnapshot(
						ctx,
						streamSender,
						msg.ChatID,
						state,
					)
					if err != nil {
						return true, err
					}
					if sent {
						resetStatusPulseCycle(state)
					}
					continue
				}
				if !streamModeSendsProgress(snapshotMode) {
					continue
				}
				content := progressSnapshotText(
					evt,
					placeholder,
					state,
				)
				if content == "" {
					continue
				}
				sent, err := sendReplySnapshot(
					ctx,
					streamSender,
					msg.ChatID,
					state,
					content,
					false,
				)
				if err != nil {
					return true, err
				}
				if sent {
					markStatusPulseSent(state, summary)
				}
			case streamEventRunIgnored:
				c.runStatus.finish(
					gwReq.Thread,
					gwReq.RequestID,
					defaultIgnoredStatusSummary,
					"",
				)
				if state.started {
					if err := finishReplyStream(
						ctx,
						streamSender,
						msg.ChatID,
						state,
						defaultIgnoredStatusSummary,
					); err != nil {
						return true, err
					}
				}
				return true, nil
			case streamEventPublicDelta:
				if evt.Delta == "" || hasStableReply(state) {
					continue
				}
				now := time.Now()
				appendPendingDelta(
					&state.publicPending,
					&state.publicPendingSince,
					evt.Delta,
					now,
				)
				markPreAnswerDelta(state, now)
				preview := rewriteReplyContent(
					state,
					publicPreviewText(state),
				)
				c.runStatus.preview(
					gwReq.Thread,
					gwReq.RequestID,
					preview,
				)
				if state.nativeThinking {
					if !streamModeSendsDeltas(snapshotMode) {
						continue
					}
					sent, err := sendNativeThinkingProcessSnapshot(
						ctx,
						streamSender,
						msg.ChatID,
						state,
					)
					if err != nil {
						return true, err
					}
					if sent {
						resetStatusPulseCycle(state)
					}
				}
			case streamEventPublicDone:
				if hasStableReply(state) {
					continue
				}
				if strings.TrimSpace(evt.Reply) == "" &&
					!laneHasText(
						nil,
						&state.publicPending,
					) {
					continue
				}
				commitLaneText(
					&state.publicBuilder,
					&state.publicPending,
					&state.publicPendingSince,
					evt.Reply,
				)
				preview := rewriteReplyContent(
					state,
					publicPreviewText(state),
				)
				c.runStatus.preview(
					gwReq.Thread,
					gwReq.RequestID,
					preview,
				)
				if state.nativeThinking {
					if !streamModeSendsDeltas(snapshotMode) {
						continue
					}
					sent, err := sendNativeThinkingProcessSnapshot(
						ctx,
						streamSender,
						msg.ChatID,
						state,
					)
					if err != nil {
						return true, err
					}
					if sent {
						resetStatusPulseCycle(state)
					}
				}
			case streamEventThoughtDelta:
				if evt.Delta == "" || hasStableReply(state) {
					continue
				}
				now := time.Now()
				appendPendingDelta(
					&state.thoughtPending,
					&state.thoughtPendingSince,
					evt.Delta,
					now,
				)
				markPreAnswerDelta(state, now)
				preview := rewriteReplyContent(
					state,
					thoughtPreviewText(state),
				)
				c.runStatus.preview(
					gwReq.Thread,
					gwReq.RequestID,
					preview,
				)
			case streamEventThoughtDone:
				if hasStableReply(state) {
					continue
				}
				if strings.TrimSpace(evt.Reply) == "" &&
					!laneHasText(
						nil,
						&state.thoughtPending,
					) {
					continue
				}
				commitLaneText(
					&state.thoughtBuilder,
					&state.thoughtPending,
					&state.thoughtPendingSince,
					evt.Reply,
				)
				preview := rewriteReplyContent(
					state,
					thoughtPreviewText(state),
				)
				c.runStatus.preview(
					gwReq.Thread,
					gwReq.RequestID,
					preview,
				)
			case streamEventMsgDelta:
				if evt.Delta == "" {
					continue
				}
				now := time.Now()
				needsVisibleReset := prepareReplyPendingSegment(
					state,
				)
				appendPendingDelta(
					&state.replyPending,
					&state.replyPendingSince,
					evt.Delta,
					now,
				)
				markPreAnswerDelta(state, now)
				preview := rewriteReplyContent(
					state,
					replyPreviewText(state),
				)
				c.runStatus.preview(
					gwReq.Thread,
					gwReq.RequestID,
					preview,
				)
				if !streamModeSendsDeltas(snapshotMode) {
					continue
				}
				if state.nativeThinking {
					continue
				}
				if !needsVisibleReset {
					continue
				}
				snapshot := renderCurrentSnapshot(
					state,
					currentStatusBase(state),
					true,
				)
				sent, err := sendReplySnapshot(
					ctx,
					streamSender,
					msg.ChatID,
					state,
					snapshot,
					false,
				)
				if err != nil {
					return true, err
				}
				if sent {
					resetStatusPulseCycle(state)
				}
			case streamEventMsgDone:
				content := completedStreamContent(
					evt.Reply,
					replyPreviewText(state),
				)
				if err := c.finishGatewayStreamReply(
					ctx,
					msg,
					sender,
					streamSender,
					state,
					gwReq,
					gwReq.Thread,
					gwReq.RequestID,
					evt.Usage,
					content,
				); err != nil {
					return true, err
				}
			case streamEventRunDone:
				c.recordContextUsage(
					gwReq.Thread,
					gwReq.RequestID,
					evt.Usage,
				)
				c.refreshReplyDisplayPrefix(
					gwReq.Thread,
					state,
				)
				if err := closeReplyStream(
					ctx,
					streamSender,
					msg.ChatID,
					state,
				); err != nil {
					return true, err
				}
				return true, nil
			case streamEventRunCanceled:
				preview := rewriteReplyContent(
					state,
					streamPreviewText(state),
				)
				c.runStatus.finish(
					gwReq.Thread,
					gwReq.RequestID,
					streamCanceledText,
					preview,
				)
				content := streamCanceledContent(
					streamPreviewText(state),
				)
				if state.started {
					if _, err := sendReplySnapshot(
						ctx,
						streamSender,
						msg.ChatID,
						state,
						content,
						true,
					); err != nil {
						return true, err
					}
					return true, nil
				}
				_ = sender.SendMarkdown(
					ctx,
					msg.ChatID,
					streamCanceledText,
				)
				return true, nil
			case streamEventRunError:
				rawErrMsg := ""
				if evt.Error != nil {
					rawErrMsg = strings.TrimSpace(
						evt.Error.Message,
					)
				}
				errMsg := sanitizeGatewayErrorMessage(
					rawErrMsg,
					gwReq.RequestID,
				)
				logGatewayFailure(
					ctx,
					"gateway stream run",
					gwReq.RequestID,
					rawErrMsg,
					nil,
				)
				c.runStatus.fail(
					gwReq.Thread,
					gwReq.RequestID,
					errMsg,
					rewriteReplyContent(
						state,
						streamPreviewText(state),
					),
				)
				if state.started {
					content := streamErrorContent(
						errMsg,
						streamPreviewText(state),
					)
					_, _ = sendReplySnapshot(
						ctx,
						streamSender,
						msg.ChatID,
						state,
						content,
						true,
					)
					return true, nil
				}
				_ = sender.SendMarkdown(ctx, msg.ChatID, errMsg)
				return true, nil
			}
		case <-idleTicker.C:
			if state.nativeThinking {
				continue
			}
			if !shouldFlushIdlePreAnswerBoundary(
				snapshotMode,
				state,
				time.Now(),
			) {
				continue
			}
			stableChanged := flushPreAnswerBoundary(state)
			if !stableChanged {
				continue
			}
			resetStatusPulseCycle(state)
			snapshot := renderCurrentSnapshot(
				state,
				currentStatusBase(state),
				true,
			)
			sent, err := sendReplySnapshot(
				ctx,
				streamSender,
				msg.ChatID,
				state,
				snapshot,
				false,
			)
			if err != nil {
				return true, err
			}
			if sent {
				markStatusPulseSent(
					state,
					currentStatusBase(state),
				)
			}
		case <-pulseTicker.C:
			if state.nativeThinking {
				continue
			}
			if !shouldSendStatusPulse(
				snapshotMode,
				state,
			) {
				continue
			}
			content := statusPulseSnapshotText(state)
			if content == "" {
				continue
			}
			sent, err := sendReplySnapshot(
				ctx,
				streamSender,
				msg.ChatID,
				state,
				content,
				false,
			)
			if err != nil {
				return true, err
			}
			if sent {
				markStatusPulseSent(
					state,
					currentStatusBase(state),
				)
			}
		}
	}

	if err := closeReplyStream(
		ctx,
		streamSender,
		msg.ChatID,
		state,
	); err != nil {
		return true, err
	}
	return true, nil
}

func buildReplyStreamID() string {
	seq := replyStreamSeq.Add(1)
	return fmt.Sprintf(
		"%s%d-%d",
		replyStreamIDPrefix,
		time.Now().UnixNano(),
		seq,
	)
}

func buildStreamFeedbackID(requestID string, msgID string) string {
	base := strings.TrimSpace(requestID)
	if base == "" {
		base = strings.TrimSpace(msgID)
	}
	if base == "" {
		return ""
	}
	feedbackID := streamFeedbackIDPrefix + base
	if len([]byte(feedbackID)) <= streamFeedbackIDMaxBytes {
		return feedbackID
	}

	hash := fnv.New64a()
	_, _ = hash.Write([]byte(base))
	return streamFeedbackIDPrefix +
		strconv.FormatUint(hash.Sum64(), streamFeedbackIDHashBase)
}

func visibleRunStatusPlaceholder(
	placeholder string,
	processingMessage string,
	state *replyStreamState,
) string {
	if state == nil || !state.nativeThinking {
		return placeholder
	}
	if strings.TrimSpace(placeholder) != streamNativeThinkingPlaceholder {
		return placeholder
	}
	return normalizeStatusBase(processingMessage)
}

func sendReplySnapshot(
	ctx context.Context,
	sender streamingSender,
	chatID string,
	state *replyStreamState,
	content string,
	finish bool,
) (bool, error) {
	if state == nil {
		return false, fmt.Errorf("wecom stream: nil reply state")
	}
	if sender == nil {
		return false, fmt.Errorf("wecom stream: nil streaming sender")
	}
	if state.finished {
		return false, nil
	}
	if state.progressDisabled && !finish {
		return false, nil
	}
	if shouldRewriteReplySnapshotContent(state, content, finish) {
		content = rewriteReplyContent(state, content)
	} else {
		content = strings.TrimSpace(content)
	}
	now := time.Now()
	if shouldThrottleNativeThinkingSnapshot(state, content, finish, now) {
		return false, nil
	}
	if state.nativeThinking {
		content = nativeThinkingStreamContent(state, content, finish)
	}
	if shouldRotateReplyStreamSegment(state, now) {
		if err := rotateReplyStreamSegment(
			ctx,
			sender,
			chatID,
			state,
			now,
		); err != nil {
			return false, err
		}
	}
	if !finish && state.started && content == state.lastSent {
		return false, nil
	}
	if shouldThrottleStreamSnapshot(state, finish, now) {
		return false, nil
	}
	feedbackSent, err := sendStreamSnapshotFrame(
		ctx,
		sender,
		chatID,
		state,
		content,
		finish,
	)
	if err != nil {
		if finish && shouldAcceptFinalStreamDeliveryError(err) {
			markReplySnapshotSent(
				state,
				content,
				finish,
				now,
				feedbackSent,
			)
			return true, nil
		}
		if !finish && shouldDisableReplyProgress(err) {
			state.progressDisabled = true
			markReplySnapshotSent(
				state,
				content,
				finish,
				now,
				feedbackSent,
			)
			return true, nil
		}
		if finish && state.sender != nil && !state.started {
			if fallbackErr := sendMarkdownReply(
				ctx,
				state.sender,
				chatID,
				content,
			); fallbackErr == nil {
				state.finished = true
				state.lastSent = content
				return true, nil
			}
		}
		return false, err
	}
	markReplySnapshotSent(
		state,
		content,
		finish,
		now,
		feedbackSent,
	)
	return true, nil
}

func sendStreamSnapshotFrame(
	ctx context.Context,
	sender streamingSender,
	chatID string,
	state *replyStreamState,
	content string,
	finish bool,
) (bool, error) {
	if state == nil {
		return false, fmt.Errorf("wecom stream: nil reply state")
	}
	if state.nativeThinking &&
		!state.feedbackSent &&
		strings.TrimSpace(state.feedbackID) != "" {
		feedbackSender, ok := sender.(feedbackStreamingSender)
		if ok {
			return true, feedbackSender.SendStreamWithFeedback(
				ctx,
				chatID,
				state.id,
				content,
				finish,
				state.feedbackID,
			)
		}
	}
	return false, sender.SendStream(
		ctx,
		chatID,
		state.id,
		content,
		finish,
	)
}

func shouldRewriteReplySnapshotContent(
	state *replyStreamState,
	content string,
	finish bool,
) bool {
	if state == nil || !state.nativeThinking || finish {
		return true
	}
	return strings.TrimSpace(content) != ""
}

func sendNativeThinkingProcessSnapshot(
	ctx context.Context,
	sender streamingSender,
	chatID string,
	state *replyStreamState,
) (bool, error) {
	return sendReplySnapshot(ctx, sender, chatID, state, "", false)
}

func nativeThinkingStreamContent(
	state *replyStreamState,
	visible string,
	finish bool,
) string {
	visible = strings.TrimSpace(visible)
	if strings.HasPrefix(visible, streamNativeThinkingOpenTag) {
		visible = nativeThinkingAnswerText(visible, finish)
	}
	if finish {
		return visible
	}

	thinking := nativeThinkingReasoningText(state)
	if thinking == "" {
		thinking = streamNativeThinkingText
	}
	if visible == "" {
		return streamNativeThinkingOpenTag + thinking
	}

	thinkBlock := streamNativeThinkingOpenTag + thinking +
		streamNativeThinkingCloseTag
	return thinkBlock + "\n" + visible
}

func nativeThinkingAnswerText(content string, finish bool) string {
	if !strings.HasPrefix(content, streamNativeThinkingOpenTag) {
		return content
	}
	if closeIndex := strings.Index(
		content,
		streamNativeThinkingCloseTag,
	); closeIndex >= 0 {
		afterCloseStart := closeIndex + len(streamNativeThinkingCloseTag)
		return strings.TrimSpace(content[afterCloseStart:])
	}
	if !finish {
		return ""
	}
	return nativeThinkingVisibleContent(content)
}

func nativeThinkingReasoningText(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	appendNativeThinkingPart(&parts, state.visibleNarrative)
	appendNativeThinkingPart(&parts, publicPreviewText(state))
	appendNativeThinkingPart(&parts, builderText(
		&state.toolActivityBuilder,
	))
	return strings.Join(parts, streamSectionSep)
}

func appendNativeThinkingPart(parts *[]string, text string) {
	if parts == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	for _, existing := range *parts {
		if existing == text {
			return
		}
	}
	*parts = append(*parts, text)
}

func recordNativeThinkingToolActivity(
	state *replyStreamState,
	evt gatewayStreamEvent,
) bool {
	if state == nil || !state.nativeThinking {
		return false
	}
	changed := false
	for _, activity := range nativeThinkingToolActivities(evt) {
		if nativeThinkingToolActivitySeen(state, activity) {
			continue
		}
		line := nativeThinkingToolActivityText(
			state.toolActivityCount+1,
			activity.name,
			activity.detail,
		)
		if line == "" {
			continue
		}
		state.toolActivityCount++
		markNativeThinkingToolActivitySeen(state, activity)
		before := builderText(&state.toolActivityBuilder)
		appendLaneSegment(&state.toolActivityBuilder, line)
		if builderText(&state.toolActivityBuilder) != before {
			changed = true
		}
	}
	return changed
}

func nativeThinkingToolActivities(
	evt gatewayStreamEvent,
) []nativeThinkingToolActivity {
	if !nativeThinkingToolStatusVisible(evt.ToolStatus) {
		return nil
	}
	activities := make([]nativeThinkingToolActivity, 0, len(evt.ToolCalls)+1)
	for _, call := range evt.ToolCalls {
		if name := gatewayToolCallName(call); name != "" {
			activities = append(activities, nativeThinkingToolActivity{
				id:     strings.TrimSpace(call.ID),
				name:   name,
				detail: nativeThinkingToolDetail(evt, name),
			})
		}
	}
	if len(activities) > 0 {
		return uniqueNativeThinkingToolActivities(activities)
	}
	if name := sanitizeNativeThinkingToolName(evt.ToolName); name != "" {
		activities = append(activities, nativeThinkingToolActivity{
			id:     strings.TrimSpace(evt.ToolCallID),
			name:   name,
			detail: nativeThinkingToolDetail(evt, name),
		})
	}
	return uniqueNativeThinkingToolActivities(activities)
}

func nativeThinkingToolDetail(
	evt gatewayStreamEvent,
	toolName string,
) string {
	if detail := sanitizeNativeThinkingToolInfo(evt.ToolDetail); detail != "" {
		return detail
	}
	toolName = sanitizeNativeThinkingToolName(toolName)
	if toolName != toolNameExecCommand {
		return ""
	}
	return sanitizeNativeThinkingToolInfo(
		execCommandNativeThinkingToolDetail(evt.Summary),
	)
}

func execCommandNativeThinkingToolDetail(summary string) string {
	switch strings.TrimSpace(summary) {
	case progressSummaryGoTestEN:
		return toolDetailGoTest
	case progressSummaryPytestEN:
		return toolDetailPytest
	case progressSummaryNPMTestEN:
		return toolDetailNPMTest
	case progressSummaryGitEN:
		return toolDetailGit
	case progressSummaryInspectEN:
		return toolDetailInspect
	default:
		return ""
	}
}

func nativeThinkingToolStatusVisible(status string) bool {
	switch strings.TrimSpace(status) {
	case "", streamToolStatusRunning:
		return true
	default:
		return false
	}
}

func gatewayToolCallName(call gatewayStreamToolCall) string {
	if name := sanitizeNativeThinkingToolName(call.ToolName); name != "" {
		return name
	}
	if name := sanitizeNativeThinkingToolName(call.Name); name != "" {
		return name
	}
	if call.Function == nil {
		return ""
	}
	return sanitizeNativeThinkingToolName(call.Function.Name)
}

func uniqueNativeThinkingToolActivities(
	activities []nativeThinkingToolActivity,
) []nativeThinkingToolActivity {
	seen := make(map[string]struct{}, len(activities))
	unique := make([]nativeThinkingToolActivity, 0, len(activities))
	for _, activity := range activities {
		activity.id = strings.TrimSpace(activity.id)
		activity.name = sanitizeNativeThinkingToolName(activity.name)
		activity.detail = sanitizeNativeThinkingToolInfo(
			activity.detail,
		)
		if activity.name == "" {
			continue
		}
		key := activity.name
		if activity.id != "" {
			key = activity.id
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, activity)
	}
	return unique
}

func sanitizeNativeThinkingToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" ||
		len([]rune(name)) > streamNativeThinkingToolNameMaxRunes {
		return ""
	}
	for _, char := range name {
		if isNativeThinkingToolNameRune(char) {
			continue
		}
		return ""
	}
	return name
}

func sanitizeNativeThinkingToolInfo(info string) string {
	info = strings.TrimSpace(info)
	if info == "" {
		return ""
	}
	info = stripNativeThinkingToolInfoQuery(info)
	var builder strings.Builder
	lastSpace := false
	runeCount := 0
	for _, char := range info {
		if runeCount >= streamNativeThinkingToolInfoMaxRunes {
			break
		}
		if isNativeThinkingToolInfoRune(char) {
			builder.WriteRune(char)
			lastSpace = char == ' '
			runeCount++
			continue
		}
		if !lastSpace {
			builder.WriteRune(' ')
			lastSpace = true
			runeCount++
		}
	}
	return strings.TrimSpace(builder.String())
}

func stripNativeThinkingToolInfoQuery(info string) string {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return ""
	}
	for i, field := range fields {
		if before, _, ok := strings.Cut(
			field,
			streamNativeThinkingToolQuerySep,
		); ok {
			fields[i] = before
		}
	}
	return strings.Join(fields, " ")
}

func isNativeThinkingToolInfoRune(char rune) bool {
	switch {
	case unicode.IsLetter(char), unicode.IsDigit(char):
		return true
	case char == '_', char == '-', char == '.', char == '/',
		char == ':', char == ' ', char == '*', char == '#',
		char == '@', char == '[', char == ']':
		return true
	default:
		return false
	}
}

func isNativeThinkingToolNameRune(char rune) bool {
	switch {
	case char >= 'a' && char <= 'z':
		return true
	case char >= 'A' && char <= 'Z':
		return true
	case char >= '0' && char <= '9':
		return true
	case char == '_', char == '-', char == '.':
		return true
	default:
		return false
	}
}

func nativeThinkingToolActivitySeen(
	state *replyStreamState,
	activity nativeThinkingToolActivity,
) bool {
	if state == nil {
		return false
	}
	key := nativeThinkingToolActivityKey(activity)
	if key == "" {
		return false
	}
	_, ok := state.toolActivityKeys[key]
	return ok
}

func markNativeThinkingToolActivitySeen(
	state *replyStreamState,
	activity nativeThinkingToolActivity,
) {
	if state == nil {
		return
	}
	key := nativeThinkingToolActivityKey(activity)
	if key == "" {
		return
	}
	if state.toolActivityKeys == nil {
		state.toolActivityKeys = make(map[string]struct{})
	}
	state.toolActivityKeys[key] = struct{}{}
}

func nativeThinkingToolActivityKey(
	activity nativeThinkingToolActivity,
) string {
	if id := strings.TrimSpace(activity.id); id != "" {
		return id
	}
	return strings.TrimSpace(activity.name)
}

func nativeThinkingToolActivityText(
	index int,
	name string,
	detail string,
) string {
	name = sanitizeNativeThinkingToolName(name)
	if index <= 0 || name == "" {
		return ""
	}
	if detail = sanitizeNativeThinkingToolInfo(detail); detail != "" {
		name += streamNativeThinkingToolDetailSep + detail
	}
	return streamNativeThinkingToolLabel + " " +
		strconv.Itoa(index) + streamNativeThinkingToolSep + name
}

func shouldThrottleNativeThinkingSnapshot(
	state *replyStreamState,
	content string,
	finish bool,
	now time.Time,
) bool {
	if state == nil || !state.nativeThinking || finish || !state.started {
		return false
	}
	content = strings.TrimSpace(content)
	if content == "" {
		content = nativeThinkingReasoningText(state)
	}
	if content == "" {
		return false
	}
	previous := nativeThinkingVisibleContent(state.lastSent)
	if content == previous {
		return true
	}
	if previous == "" {
		return false
	}
	if endsWithNativeThinkingFlushRune(content) {
		return false
	}
	if now.Sub(state.lastSnapshotAt) >= streamNativeThinkingMinInterval {
		return false
	}
	return len([]rune(content))-len([]rune(previous)) <
		streamNativeThinkingMinGrowth
}

func endsWithNativeThinkingFlushRune(content string) bool {
	var last rune
	for _, char := range content {
		if strings.TrimSpace(string(char)) == "" {
			continue
		}
		last = char
	}
	if last == 0 {
		return false
	}
	return isNativeThinkingFlushRune(last)
}

func isNativeThinkingFlushRune(char rune) bool {
	switch char {
	case '.', '!', '?', ':', ';',
		'。', '！', '？', '：', '；':
		return true
	default:
		return false
	}
}

func nativeThinkingVisibleContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || content == streamNativeThinkingPlaceholder ||
		content == streamNativeThinkingMarker {
		return ""
	}
	if strings.HasPrefix(content, streamNativeThinkingMarker) {
		return strings.TrimSpace(
			strings.TrimPrefix(content, streamNativeThinkingMarker),
		)
	}
	if strings.HasPrefix(content, streamNativeThinkingOpenTag) {
		if closeIndex := strings.Index(
			content,
			streamNativeThinkingCloseTag,
		); closeIndex >= 0 {
			afterCloseStart := closeIndex + len(streamNativeThinkingCloseTag)
			return strings.TrimSpace(content[afterCloseStart:])
		}
		visible := strings.TrimSpace(
			strings.TrimPrefix(content, streamNativeThinkingOpenTag),
		)
		if visible == streamNativeThinkingText {
			return ""
		}
		return visible
	}
	return content
}

func markReplySnapshotSent(
	state *replyStreamState,
	content string,
	finish bool,
	now time.Time,
	feedbackSent bool,
) {
	if state.startedAt.IsZero() {
		state.startedAt = now
	}
	state.started = true
	state.finished = finish
	state.lastSent = content
	state.lastSnapshotAt = now
	if feedbackSent {
		state.feedbackSent = true
	}
}

func shouldAcceptFinalStreamDeliveryError(err error) bool {
	// These ACK errors are returned after the stream frame is written.
	// Falling back to markdown can duplicate the final reply in WeCom.
	return isRecoverableReplyDeliveryError(err)
}

func shouldDisableReplyProgress(err error) bool {
	return errors.Is(err, errReplyAckTimeout) ||
		isReplyAckConflictError(err)
}

func isReplyAckConflictError(err error) bool {
	var ackErr *replyAckError
	if !errors.As(err, &ackErr) {
		return false
	}
	return ackErr.code == replyAckConflictErrCode
}

func isRecoverableReplyDeliveryError(err error) bool {
	return errors.Is(err, errReplyAckTimeout) ||
		isReplyAckConflictError(err)
}

func shouldRotateReplyStreamSegment(
	state *replyStreamState,
	now time.Time,
) bool {
	return state != nil &&
		state.started &&
		!state.finished &&
		!state.startedAt.IsZero() &&
		now.Sub(state.startedAt) >= replyStreamFallbackAfter
}

func rotateReplyStreamSegment(
	ctx context.Context,
	sender streamingSender,
	chatID string,
	state *replyStreamState,
	now time.Time,
) error {
	if state == nil || sender == nil {
		return nil
	}
	handoff := rewriteReplyContent(
		state,
		richStreamDeadlineContent(),
	)
	if strings.TrimSpace(handoff) != "" {
		if state.nativeThinking {
			handoff = nativeThinkingStreamContent(state, handoff, true)
		}
		if err := sender.SendStream(
			ctx,
			chatID,
			state.id,
			handoff,
			true,
		); err != nil {
			return err
		}
	}
	resetReplyStreamSegment(state, now)
	return nil
}

func resetReplyStreamSegment(
	state *replyStreamState,
	now time.Time,
) {
	if state == nil {
		return
	}
	state.id = buildReplyStreamID()
	state.started = false
	state.finished = false
	state.startedAt = now
	state.lastSnapshotAt = time.Time{}
	state.lastSent = ""
}

func shouldThrottleStreamSnapshot(
	state *replyStreamState,
	finish bool,
	now time.Time,
) bool {
	return false
}

func finishReplyStream(
	ctx context.Context,
	sender streamingSender,
	chatID string,
	state *replyStreamState,
	content string,
) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return closeReplyStream(
			ctx,
			sender,
			chatID,
			state,
		)
	}
	_, err := sendReplySnapshot(
		ctx,
		sender,
		chatID,
		state,
		content,
		true,
	)
	return err
}

func closeReplyStream(
	ctx context.Context,
	sender streamingSender,
	chatID string,
	state *replyStreamState,
) error {
	if state == nil || !state.started || state.finished {
		return nil
	}
	_, err := sendReplySnapshot(
		ctx,
		sender,
		chatID,
		state,
		finalStreamContent(state),
		true,
	)
	return err
}

func completedStreamContent(reply, accumulated string) string {
	if reply != "" {
		return reply
	}
	return accumulated
}

func appendPendingDelta(
	pending *strings.Builder,
	pendingSince *time.Time,
	delta string,
	now time.Time,
) {
	if pending == nil || delta == "" {
		return
	}
	if pendingSince != nil && pending.Len() == 0 {
		*pendingSince = now
	}
	pending.WriteString(delta)
}

func resetPendingSince(pendingSince *time.Time) {
	if pendingSince == nil {
		return
	}
	*pendingSince = time.Time{}
}

func (c *Channel) finishGatewayStreamReply(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	streamSender streamingSender,
	state *replyStreamState,
	gwReq gwclient.MessageRequest,
	sessionID string,
	requestID string,
	usage *gwclient.Usage,
	content string,
) error {
	c.recordContextUsage(sessionID, requestID, usage)
	c.refreshReplyDisplayPrefix(sessionID, state)
	deliveryPlan := c.buildReplyDeliveryPlan(
		sessionID,
		content,
	)
	deliveryOutcome := c.sendReplyDeliveryFiles(
		ctx,
		sender,
		msg.ChatID,
		deliveryPlan,
		c.replyDeliveryProgressCallback(
			ctx,
			msg,
			sender,
			state,
			sessionID,
			requestID,
		),
	)
	content = finalizeReplyDeliveryText(
		deliveryPlan.cleanReply,
		deliveryOutcome,
	)
	finalContent := rewriteReplyContent(
		state,
		content,
	)
	c.runStatus.finish(
		sessionID,
		requestID,
		defaultCompletedStatusSummary,
		finalContent,
	)
	_, err := sendReplySnapshot(
		ctx,
		streamSender,
		msg.ChatID,
		state,
		content,
		true,
	)
	return err
}

func progressSnapshotText(
	evt gatewayStreamEvent,
	placeholder string,
	state *replyStreamState,
) string {
	summary := progressSummaryText(
		evt,
		placeholder,
		state,
	)
	if summary == "" {
		return ""
	}
	if state == nil || state.progress == nil {
		return renderStatusOnlySnapshot(
			state,
			placeholder,
			true,
		)
	}
	content := recordProgressActivity(state, summary)
	return renderCurrentSnapshot(
		state,
		content,
		true,
	)
}

func streamErrorContent(errMsg, accumulated string) string {
	if accumulated != "" {
		return accumulated
	}
	return errMsg
}

func streamCanceledContent(accumulated string) string {
	if strings.TrimSpace(accumulated) == "" {
		return streamCanceledText
	}
	return accumulated + "\n\n" + streamCanceledText
}

func (c *Channel) handleStreamReadError(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	streamSender streamingSender,
	state *replyStreamState,
	sessionID string,
	requestID string,
	err error,
) error {
	rawErrMsg := ""
	if err != nil {
		rawErrMsg = err.Error()
	}
	errMsg := sanitizeGatewayErrorMessage(
		rawErrMsg,
		requestID,
	)
	logGatewayFailure(
		ctx,
		"gateway stream read",
		requestID,
		rawErrMsg,
		err,
	)
	preview := ""
	if state != nil {
		preview = rewriteReplyContent(
			state,
			streamPreviewText(state),
		)
	}
	c.runStatus.fail(
		sessionID,
		requestID,
		errMsg,
		preview,
	)
	if state != nil && state.started {
		content := streamErrorContent(
			errMsg,
			preview,
		)
		_, _ = sendReplySnapshot(
			ctx,
			streamSender,
			msg.ChatID,
			state,
			content,
			true,
		)
		return err
	}
	_ = sender.SendMarkdown(ctx, msg.ChatID, errMsg)
	return err
}

func progressSummaryText(
	evt gatewayStreamEvent,
	placeholder string,
	state *replyStreamState,
) string {
	summary := strings.TrimSpace(evt.Summary)
	switch strings.TrimSpace(evt.Stage) {
	case streamStagePreparing:
		return localizeProgressSummary(
			summary,
			progressSummaryPrepareEN,
			progressTextPreparing,
			placeholder,
		)
	case streamStageReadingDocument:
		return localizeDocumentSummary(summary)
	case streamStageReadingSpreadsheet:
		return localizeSpreadsheetSummary(summary)
	case streamStageRunningTool:
		return localizeToolSummary(summary)
	case streamStageSummarizing:
		return localizeProgressSummary(
			summary,
			progressSummaryAnsweringEN,
			progressTextSummarizing,
			progressTextSummarizing,
		)
	default:
		if summary != "" {
			return summary
		}
		return strings.TrimSpace(placeholder)
	}
}

func localizeProgressSummary(
	summary string,
	defaultSummary string,
	localized string,
	fallback string,
) string {
	summary = strings.TrimSpace(summary)
	switch {
	case summary == "":
		if strings.TrimSpace(localized) != "" {
			return localized
		}
		return strings.TrimSpace(fallback)
	case summary == defaultSummary:
		return localized
	default:
		return summary
	}
}

func localizeDocumentSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	switch {
	case summary == "",
		summary == progressSummaryDocumentEN:
		return progressTextReadingDocument
	case strings.HasPrefix(
		summary,
		progressSummaryDocumentEN+" page ",
	):
		page := strings.TrimSpace(
			strings.TrimPrefix(
				summary,
				progressSummaryDocumentEN+" page ",
			),
		)
		if page == "" {
			return progressTextReadingDocument
		}
		return progressTextReadingDocument + "（第 " + page + " 页）"
	default:
		return summary
	}
}

func localizeSpreadsheetSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	switch {
	case summary == "",
		summary == progressSummarySheetEN:
		return progressTextReadingSpreadsheet
	case strings.HasPrefix(summary, progressSummarySheetEN):
		return progressTextReadingSpreadsheet
	default:
		return summary
	}
}

func localizeToolSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	toolName := streamToolName(summary)
	switch {
	case summary == "",
		summary == progressSummaryToolEN:
		return progressTextRunningTool
	case summary == progressSummaryGoTestEN:
		return "正在运行 go test"
	case summary == progressSummaryPytestEN:
		return "正在运行 pytest"
	case summary == progressSummaryNPMTestEN:
		return "正在运行 npm test"
	case summary == progressSummaryGitEN:
		return "正在执行 git 命令"
	case summary == progressSummaryInspectEN:
		return "正在检查工作区"
	case toolName != "":
		if toolName == toolNameExecCommand {
			return progressTextRunningCommand
		}
		if localized := localizedToolName(toolName); localized != "" {
			return localized
		}
		return "正在运行 " + toolName
	default:
		return summary
	}
}

func localizedToolName(toolName string) string {
	switch toolName {
	case toolNameReadFile:
		return "正在读取工作区文件"
	case toolNameSaveFile:
		return "正在写入工作区文件"
	case toolNameListDir:
		return "正在查看工作区目录"
	case toolNameSearch:
		return "正在搜索工作区"
	case toolNameApplyPatch:
		return "正在修改文件"
	default:
		return ""
	}
}

func streamToolName(summary string) string {
	if !strings.HasPrefix(summary, progressSummaryRunPrefixEN) {
		return ""
	}
	return strings.TrimSpace(
		strings.TrimPrefix(
			summary,
			progressSummaryRunPrefixEN,
		),
	)
}

func streamPlaceholder(
	processingMessage string,
	state *replyStreamState,
) string {
	if state != nil && strings.TrimSpace(state.lastSent) != "" {
		return strings.TrimSpace(state.lastSent)
	}
	return visibleStatusText(state)
}

func currentStatusText(
	processingMessage string,
	state *replyStreamState,
) string {
	if state != nil && strings.TrimSpace(state.lastSent) != "" {
		return strings.TrimSpace(state.lastSent)
	}
	return normalizeStatusBase(processingMessage)
}

func visibleStatusText(state *replyStreamState) string {
	if state == nil {
		return statusPulseOne
	}
	idx := state.statusPulseStep % len(statusPulseSeq)
	return statusPulseSeq[idx]
}

func animatedStatusText(
	text string,
	state *replyStreamState,
) string {
	base := normalizeStatusBase(text)
	if base == "" {
		return ""
	}
	if state == nil {
		return base
	}
	syncStatusPulseBase(state, base)
	idx := state.statusPulseStep % len(statusPulseSeq)
	suffix := statusPulseSeq[idx]
	return formatStatusPulse(base, suffix)
}

func markStatusPulseSent(
	state *replyStreamState,
	text string,
) {
	if state == nil {
		return
	}
	base := normalizeStatusBase(text)
	if base != "" {
		syncStatusPulseBase(state, base)
	}
	state.statusPulseStep++
}

func syncStatusPulseBase(
	state *replyStreamState,
	base string,
) {
	if state == nil || strings.TrimSpace(base) == "" {
		return
	}
	state.statusBase = base
}

func resetStatusPulseCycle(state *replyStreamState) {
	if state == nil {
		return
	}
	state.statusPulseStep = 0
}

func normalizeStatusBase(text string) string {
	text = strings.TrimSpace(text)
	for _, suffix := range statusPulseCompatSeq {
		fullSuffix := statusPulseCompatSep + suffix
		if strings.HasSuffix(text, fullSuffix) {
			return trimStatusPulseSuffix(
				text,
				fullSuffix,
			)
		}
	}
	for _, suffix := range statusPulseTrimSeq {
		if strings.HasSuffix(text, suffix) {
			return trimStatusPulseSuffix(text, suffix)
		}
	}
	if strings.HasSuffix(text, statusPulseCN) {
		return trimStatusPulseSuffix(text, statusPulseCN)
	}
	return text
}

func formatStatusPulse(base, suffix string) string {
	base = strings.TrimSpace(base)
	suffix = strings.TrimSpace(suffix)
	if base == "" {
		return ""
	}
	if suffix == "" {
		return base
	}
	return base + suffix
}

func trimStatusPulseSuffix(text, suffix string) string {
	return strings.TrimSpace(
		strings.TrimSuffix(text, suffix),
	)
}

func shouldSendStatusPulse(
	mode string,
	state *replyStreamState,
) bool {
	return streamModeSendsPlaceholder(mode) &&
		state != nil &&
		!state.nativeThinking &&
		state.started &&
		!state.finished &&
		strings.TrimSpace(
			normalizeStatusBase(state.statusBase),
		) != ""
}

func statusPulseSnapshotText(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	return renderCurrentSnapshot(
		state,
		currentStatusBase(state),
		true,
	)
}

func recordProgressActivity(
	state *replyStreamState,
	message string,
) string {
	base := normalizeStatusBase(message)
	if base == "" {
		return ""
	}
	syncStatusPulseBase(state, base)
	if state == nil || state.progress == nil {
		return base
	}
	state.progress.Apply(progress.Event{
		Kind:    progress.KindActivity,
		Message: base,
	})
	return base
}

func renderProgressSnapshot(
	state *replyStreamState,
	fallback string,
	animate bool,
) string {
	return renderStatusOnlySnapshot(
		state,
		fallback,
		animate,
	)
}

func renderCurrentSnapshot(
	state *replyStreamState,
	fallback string,
	animate bool,
) string {
	if state == nil {
		return renderStatusOnlySnapshot(
			state,
			fallback,
			animate,
		)
	}
	narrative := stableNarrativeSnapshot(state)
	if narrative == "" {
		return renderProgressSnapshot(
			state,
			fallback,
			animate,
		)
	}
	if !animate {
		return narrative
	}
	return joinNarrativeAndStatus(
		narrative,
		renderStatusOnlySnapshot(
			state,
			currentStatusBase(state),
			true,
		),
	)
}

func renderStatusOnlySnapshot(
	state *replyStreamState,
	fallback string,
	animate bool,
) string {
	base := visibleStatusBase(fallback)
	if base == "" {
		base = currentStatusBase(state)
	}
	if base == "" {
		if animate {
			return visibleStatusText(state)
		}
		return ""
	}
	if !animate {
		return normalizeStatusBase(base)
	}
	return animatedStatusText(base, state)
}

func stableNarrativeSnapshot(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	if reply := replyCommittedText(state); reply != "" {
		return reply
	}
	return strings.TrimSpace(state.visibleNarrative)
}

func streamPreviewText(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	if preview := replyPreviewText(state); preview != "" {
		return preview
	}
	if state.nativeThinking {
		return ""
	}
	if preview := strings.TrimSpace(state.visibleNarrative); preview != "" {
		return preview
	}
	if preview := publicPreviewText(state); preview != "" {
		return preview
	}
	if preview := thoughtPreviewText(state); preview != "" {
		return preview
	}
	return ""
}

func replyCommittedText(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	return strings.TrimSpace(state.builder.String())
}

func replyPreviewText(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	return strings.TrimSpace(
		state.builder.String() + state.replyPending.String(),
	)
}

func publicPreviewText(state *replyStreamState) string {
	return lanePreviewText(
		&state.publicBuilder,
		&state.publicPending,
	)
}

func thoughtPreviewText(state *replyStreamState) string {
	return lanePreviewText(
		&state.thoughtBuilder,
		&state.thoughtPending,
	)
}

func lanePreviewText(
	committed *strings.Builder,
	pending *strings.Builder,
) string {
	return strings.TrimSpace(
		builderText(committed) + builderRawText(pending),
	)
}

func hasStableReply(state *replyStreamState) bool {
	return state != nil && replyCommittedText(state) != ""
}

func prepareReplyPendingSegment(
	state *replyStreamState,
) bool {
	if state == nil || state.replyPending.Len() != 0 {
		return false
	}
	state.lastSnapshotAt = time.Time{}
	if state.visibleReplyText {
		return false
	}
	if strings.TrimSpace(state.visibleNarrative) == "" {
		return false
	}
	state.visibleNarrative = ""
	return true
}

func markPreAnswerDelta(
	state *replyStreamState,
	now time.Time,
) {
	if state == nil {
		return
	}
	state.preAnswerLastAt = now
}

func shouldFlushIdlePreAnswerBoundary(
	snapshotMode string,
	state *replyStreamState,
	now time.Time,
) bool {
	if !streamModeSendsDeltas(snapshotMode) || state == nil {
		return false
	}
	if hasStableReply(state) || state.preAnswerLastAt.IsZero() {
		return false
	}
	if strings.TrimSpace(preAnswerPreviewText(state)) == "" {
		return false
	}
	return now.Sub(state.preAnswerLastAt) >= streamIdleFlushAfter
}

func shouldFlushPreAnswerBoundary(
	evt gatewayStreamEvent,
	state *replyStreamState,
) bool {
	if state == nil {
		return false
	}
	if len(evt.ToolCalls) > 0 {
		return true
	}
	switch evt.Stage {
	case streamStageRunningTool:
		return true
	default:
		return evt.ToolName != ""
	}
}

func flushPreAnswerBoundary(state *replyStreamState) bool {
	if state == nil {
		return false
	}
	segment := preAnswerPreviewText(state)
	clearPreAnswerBuffers(state)
	if segment == "" {
		return false
	}
	segment = strings.TrimSpace(segment)
	if state.visibleReplyText {
		// Keep only the latest public preview. Full-snapshot
		// rendering turns repeated planning text into visible spam.
		if strings.TrimSpace(state.visibleNarrative) == segment {
			return false
		}
		state.visibleNarrative = segment
		return true
	}
	state.visibleNarrative = segment
	state.visibleReplyText = true
	return true
}

func preAnswerPreviewText(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	if preview := strings.TrimSpace(
		builderRawText(&state.replyPending),
	); preview != "" {
		return preview
	}
	if preview := publicPreviewText(state); preview != "" {
		return preview
	}
	if state.nativeThinking {
		return ""
	}
	return thoughtPreviewText(state)
}

func clearPreAnswerBuffers(state *replyStreamState) {
	if state == nil {
		return
	}
	resetBuilder(&state.replyPending)
	resetPendingSince(&state.replyPendingSince)
	resetBuilder(&state.publicBuilder)
	resetBuilder(&state.publicPending)
	resetPendingSince(&state.publicPendingSince)
	resetBuilder(&state.thoughtBuilder)
	resetBuilder(&state.thoughtPending)
	resetPendingSince(&state.thoughtPendingSince)
	state.preAnswerLastAt = time.Time{}
}

func currentStatusBase(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	return visibleStatusBase(state.statusBase)
}

func visibleStatusBase(text string) string {
	return ""
}

func joinNarrativeAndStatus(
	body string,
	status string,
) string {
	body = strings.TrimSpace(body)
	status = strings.TrimSpace(status)
	switch {
	case body == "":
		return status
	case status == "":
		return body
	default:
		return body + streamSectionSep + status
	}
}

func laneHasText(
	committed *strings.Builder,
	pending *strings.Builder,
) bool {
	return laneSnapshotText(committed, pending) != ""
}

func laneSnapshotText(
	committed *strings.Builder,
	pending *strings.Builder,
) string {
	return joinNarrativeAndStatus(
		builderText(committed),
		builderText(pending),
	)
}

func builderText(builder *strings.Builder) string {
	if builder == nil {
		return ""
	}
	return strings.TrimSpace(builder.String())
}

func builderRawText(builder *strings.Builder) string {
	if builder == nil {
		return ""
	}
	return builder.String()
}

func commitLaneText(
	committed *strings.Builder,
	pending *strings.Builder,
	pendingSince *time.Time,
	reply string,
) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		reply = builderText(pending)
	}
	if reply == "" {
		resetBuilder(pending)
		resetPendingSince(pendingSince)
		return
	}
	appendLaneSegment(committed, reply)
	resetBuilder(pending)
	resetPendingSince(pendingSince)
}

func appendLaneSegment(
	builder *strings.Builder,
	segment string,
) {
	if builder == nil {
		return
	}
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return
	}
	existing := builderText(builder)
	if existing == segment ||
		strings.HasSuffix(
			existing,
			streamSectionSep+segment,
		) {
		return
	}
	if existing != "" {
		builder.WriteString(streamSectionSep)
	}
	builder.WriteString(segment)
}

func resetBuilder(builder *strings.Builder) {
	if builder == nil {
		return
	}
	builder.Reset()
}

func richStreamDeadlineContent() string {
	return streamDeadlineThinkLine + "\n\n" +
		streamDeadlineNotice
}

func rewriteReplyContent(
	state *replyStreamState,
	content string,
) string {
	if state == nil {
		return strings.TrimSpace(content)
	}
	rewritten := strings.TrimSpace(content)
	if state.nativeThinking &&
		rewritten == streamNativeThinkingPlaceholder {
		return streamNativeThinkingPlaceholder
	}
	if state.rewrite != nil {
		rewritten = state.rewrite(rewritten)
	}
	return applyReplyDisplayPrefix(
		state.displayPrefix,
		rewritten,
	)
}

func applyReplyDisplayPrefix(
	prefix string,
	content string,
) string {
	prefix = strings.TrimSpace(prefix)
	content = strings.TrimSpace(content)
	if prefix == "" {
		return content
	}
	if content == "" {
		return prefix
	}
	if strings.HasPrefix(content, prefix) {
		return content
	}
	return prefix + "\n\n" + content
}

func finalStreamContent(state *replyStreamState) string {
	if state == nil {
		return ""
	}
	if state.nativeThinking {
		if strings.TrimSpace(state.lastSent) ==
			streamNativeThinkingPlaceholder {
			return streamNativeThinkingDoneText
		}
		return replyCommittedText(state)
	}
	if preview := streamPreviewText(state); preview != "" {
		return preview
	}
	return state.lastSent
}

func openGatewayStream(
	ctx context.Context,
	gw gatewayClient,
	req gwclient.MessageRequest,
	progressAfterDelta bool,
) (reflect.Value, bool, error) {
	if progressAfterDelta {
		stream, ok, err := openGatewayStreamWithOptions(ctx, gw, req)
		if ok || err != nil {
			return stream, ok, err
		}
	}
	return openGatewayStreamPlain(ctx, gw, req)
}

func openGatewayStreamWithOptions(
	ctx context.Context,
	gw gatewayClient,
	req gwclient.MessageRequest,
) (reflect.Value, bool, error) {
	method := reflect.ValueOf(gw).MethodByName("StreamMessageWithOptions")
	if !validGatewayStreamMethod(method, 3) {
		return reflect.Value{}, false, nil
	}

	methodType := method.Type()
	if methodType.In(2) != streamOptionsType {
		return reflect.Value{}, false, nil
	}
	opts := &gwclient.MessageStreamOptions{
		ProgressAfterTextDelta: true,
	}
	return callGatewayStreamMethod(
		method,
		[]reflect.Value{
			reflect.ValueOf(ctx),
			reflect.ValueOf(req),
			reflect.ValueOf(opts),
		},
	)
}

func openGatewayStreamPlain(
	ctx context.Context,
	gw gatewayClient,
	req gwclient.MessageRequest,
) (reflect.Value, bool, error) {
	method := reflect.ValueOf(gw).MethodByName("StreamMessage")
	if !validGatewayStreamMethod(method, 2) {
		return reflect.Value{}, false, nil
	}

	return callGatewayStreamMethod(
		method,
		[]reflect.Value{
			reflect.ValueOf(ctx),
			reflect.ValueOf(req),
		},
	)
}

func validGatewayStreamMethod(method reflect.Value, numIn int) bool {
	if !method.IsValid() {
		return false
	}
	methodType := method.Type()
	if methodType.NumIn() != numIn ||
		methodType.NumOut() != 2 {
		return false
	}
	if !methodType.In(0).Implements(contextIfaceType) ||
		methodType.In(1) != requestType {
		return false
	}
	if methodType.Out(0).Kind() != reflect.Chan ||
		!methodType.Out(1).Implements(errorIfaceType) {
		return false
	}
	return true
}

func callGatewayStreamMethod(
	method reflect.Value,
	args []reflect.Value,
) (reflect.Value, bool, error) {
	values := method.Call(args)
	if !values[1].IsNil() {
		err, _ := values[1].Interface().(error)
		return reflect.Value{}, true, err
	}
	if values[0].IsNil() {
		return reflect.Value{}, true, fmt.Errorf(
			"wecom stream: nil gateway stream",
		)
	}
	return values[0], true, nil
}

func openGatewayStreamEvents(
	ctx context.Context,
	stream reflect.Value,
) <-chan gatewayStreamReadResult {
	ch := make(chan gatewayStreamReadResult, 1)
	go func() {
		defer close(ch)
		for {
			evt, open, err := readGatewayStreamEvent(
				ctx,
				stream,
			)
			if err != nil {
				select {
				case ch <- gatewayStreamReadResult{err: err}:
				case <-ctx.Done():
				}
				return
			}
			if !open {
				return
			}
			select {
			case ch <- gatewayStreamReadResult{evt: evt}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func readGatewayStreamEvent(
	ctx context.Context,
	stream reflect.Value,
) (gatewayStreamEvent, bool, error) {
	if done := ctx.Done(); done != nil {
		chosen, value, ok := reflect.Select([]reflect.SelectCase{
			{
				Dir:  reflect.SelectRecv,
				Chan: stream,
			},
			{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(done),
			},
		})
		if chosen == 1 {
			return gatewayStreamEvent{}, false, ctx.Err()
		}
		if !ok {
			return gatewayStreamEvent{}, false, nil
		}
		return decodeGatewayStreamEvent(value)
	}

	value, ok := stream.Recv()
	if !ok {
		return gatewayStreamEvent{}, false, nil
	}
	return decodeGatewayStreamEvent(value)
}

func decodeGatewayStreamEvent(
	value reflect.Value,
) (gatewayStreamEvent, bool, error) {
	data, err := json.Marshal(value.Interface())
	if err != nil {
		return gatewayStreamEvent{}, false, err
	}

	var evt gatewayStreamEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return gatewayStreamEvent{}, false, err
	}
	return evt, true, nil
}
