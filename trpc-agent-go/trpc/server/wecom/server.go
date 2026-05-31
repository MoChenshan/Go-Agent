package wecom

import (
	"context"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// Server bridges a Runner to a WeCom AI bot websocket.
type Server struct {
	runner  runner.Runner
	managed runner.ManagedRunner
	cfg     Config
	opts    options

	wsDialer websocketDialer

	lanes    *laneLocker
	inflight *inflightRequests
	sessions *sessionStore
}

// New creates a WeCom AI bot websocket server backed by a Runner.
func New(
	r runner.Runner,
	cfg Config,
	opts ...Option,
) (*Server, error) {
	cfg = normalizeConfig(cfg)
	if err := validateConfig(r, cfg); err != nil {
		return nil, err
	}

	resolved := defaultOptions()
	for _, opt := range opts {
		opt(&resolved)
	}

	server := &Server{
		runner:   r,
		cfg:      cfg,
		opts:     resolved,
		lanes:    newLaneLocker(),
		inflight: newInflightRequests(),
		sessions: newSessionStore(),
	}
	if managed, ok := r.(runner.ManagedRunner); ok {
		server.managed = managed
	}
	return server, nil
}

func (s *Server) handleIncomingMessage(
	ctx context.Context,
	msg WebhookMessage,
) error {
	sender, err := senderForMessage(msg)
	if err != nil {
		return err
	}
	return s.handleIncomingMessageWithSender(ctx, msg, sender)
}

func (s *Server) handleIncomingMessageWithSender(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
) error {
	if strings.TrimSpace(msg.MsgType) == messageTypeEvent {
		return s.handleEventMessage(ctx, msg, sender)
	}

	text, ok := extractMessageText(msg, s.cfg.BotName)
	if !ok {
		return sender.SendMarkdown(
			ctx,
			msg.ChatID,
			s.opts.unsupportedMessage,
		)
	}

	command := parseCommand(text)
	if command.name != "" {
		return s.handleCommand(ctx, msg, sender, command)
	}
	return s.handleTextMessage(ctx, msg, sender, text)
}

func (s *Server) handleEventMessage(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
) error {
	if strings.TrimSpace(msg.Event.EventType) != eventTypeEnterChat {
		return nil
	}
	return sender.SendMarkdown(
		ctx,
		msg.ChatID,
		s.opts.welcomeMessage,
	)
}

func (s *Server) handleCommand(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	command parsedCommand,
) error {
	userID := messageUserID(msg)
	switch command.name {
	case commandHelp:
		return sender.SendMarkdown(
			ctx,
			msg.ChatID,
			s.opts.helpMessage,
		)
	case commandNew:
		s.sessions.Reset(msg.ChatID, userID)
		return sender.SendMarkdown(
			ctx,
			msg.ChatID,
			s.opts.newSessionMessage,
		)
	case commandCancel:
		return s.handleCancelCommand(ctx, msg, sender)
	default:
		return nil
	}
}

func (s *Server) handleCancelCommand(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
) error {
	if s.managed == nil {
		return sender.SendMarkdown(
			ctx,
			msg.ChatID,
			s.opts.cancelUnsupportedMessage,
		)
	}

	sessionID := s.sessions.Active(msg.ChatID, messageUserID(msg))
	requestID := s.inflight.Get(sessionID)
	if requestID == "" {
		return sender.SendMarkdown(
			ctx,
			msg.ChatID,
			s.opts.cancelNoopMessage,
		)
	}
	if !s.managed.Cancel(requestID) {
		return sender.SendMarkdown(
			ctx,
			msg.ChatID,
			s.opts.cancelNoopMessage,
		)
	}
	return sender.SendMarkdown(
		ctx,
		msg.ChatID,
		s.opts.cancelOKMessage,
	)
}

func (s *Server) handleTextMessage(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	text string,
) error {
	userID := messageUserID(msg)
	laneKey := baseSessionID(msg.ChatID, userID)
	return s.lanes.withLockErrNotify(laneKey, func() {
		_ = sender.SendMarkdown(
			ctx,
			msg.ChatID,
			s.opts.queuedMessage,
		)
	}, func() error {
		return s.runRequest(ctx, msg, sender, text, userID)
	})
}

func (s *Server) runRequest(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	text string,
	userID string,
) error {
	sessionID := s.sessions.Active(msg.ChatID, userID)
	requestID := buildRequestID(msg)
	runOpts := []agent.RunOption{
		agent.WithRequestID(requestID),
	}

	s.inflight.Set(sessionID, requestID)
	defer s.inflight.Clear(sessionID, requestID)

	eventCh, err := s.runner.Run(
		ctx,
		userID,
		sessionID,
		model.NewUserMessage(text),
		runOpts...,
	)
	if err != nil {
		return sender.SendMarkdown(
			ctx,
			msg.ChatID,
			s.opts.internalErrorMessage,
		)
	}

	return s.replyFromEvents(ctx, msg, sender, eventCh)
}

func (s *Server) replyFromEvents(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	eventCh <-chan *event.Event,
) error {
	state := &replyStreamState{
		streamID: buildStreamID(buildRequestID(msg)),
	}
	if s.cfg.EnableStream {
		if err := sender.SendStream(
			ctx,
			msg.ChatID,
			state.streamID,
			s.opts.processingMessage,
			false,
		); err != nil {
			return err
		}
	}

	finalText, errText, err := consumeEventStream(
		ctx,
		sender,
		msg.ChatID,
		s.cfg.EnableStream,
		state,
		eventCh,
	)
	if err != nil {
		return err
	}

	if errText != "" {
		return s.sendFinalError(ctx, msg, sender, state, errText)
	}
	if strings.TrimSpace(finalText) == "" {
		finalText = s.opts.emptyReplyMessage
	}
	if s.cfg.EnableStream {
		return sender.SendStream(
			ctx,
			msg.ChatID,
			state.streamID,
			finalText,
			true,
		)
	}
	return sendMarkdownChunks(
		ctx,
		sender,
		msg.ChatID,
		finalText,
	)
}

func (s *Server) sendFinalError(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	state *replyStreamState,
	errText string,
) error {
	if !s.cfg.EnableStream {
		return sender.SendMarkdown(ctx, msg.ChatID, errText)
	}
	return sender.SendStream(
		ctx,
		msg.ChatID,
		state.streamID,
		errText,
		true,
	)
}

func consumeEventStream(
	ctx context.Context,
	sender messageSender,
	chatID string,
	enableStream bool,
	state *replyStreamState,
	eventCh <-chan *event.Event,
) (string, string, error) {
	finalText := ""
	for {
		select {
		case <-ctx.Done():
			return finalText, "", ctx.Err()
		case evt, ok := <-eventCh:
			if !ok {
				return finalText, "", nil
			}
			if evt == nil || evt.Response == nil {
				continue
			}
			if evt.IsError() {
				return finalText, responseErrorMessage(evt), nil
			}

			if partial := partialReply(evt); partial != "" {
				finalText += partial
				if enableStream &&
					shouldSendSnapshot(state, finalText) {
					if err := sender.SendStream(
						ctx,
						chatID,
						state.streamID,
						finalText,
						false,
					); err != nil {
						return finalText, "", err
					}
					markSnapshotSent(state, finalText)
				}
			}

			if complete := finalReply(evt); complete != "" {
				finalText = complete
			}
			if evt.IsRunnerCompletion() {
				return finalText, "", nil
			}
		}
	}
}

func partialReply(evt *event.Event) string {
	if evt == nil || evt.Response == nil {
		return ""
	}
	for _, choice := range evt.Response.Choices {
		if choice.Message.Role == model.RoleTool {
			continue
		}
		if choice.Delta.Content != "" {
			return choice.Delta.Content
		}
	}
	return ""
}

func finalReply(evt *event.Event) string {
	if evt == nil || evt.Response == nil {
		return ""
	}
	for _, choice := range evt.Response.Choices {
		if choice.Message.Role == model.RoleTool {
			continue
		}
		content := strings.TrimSpace(choice.Message.Content)
		if content != "" {
			return content
		}
	}
	return ""
}

func responseErrorMessage(evt *event.Event) string {
	if evt != nil &&
		evt.Response != nil &&
		evt.Response.Error != nil &&
		strings.TrimSpace(evt.Response.Error.Message) != "" {
		return evt.Response.Error.Message
	}
	return defaultInternalErrorMessage
}

func senderForMessage(msg WebhookMessage) (messageSender, error) {
	if msg.ReplyWriter == nil {
		return nil, fmt.Errorf("wecom: callback is missing reply writer")
	}
	reqID := strings.TrimSpace(msg.CallbackReqID)
	if reqID == "" {
		return nil, fmt.Errorf("wecom: callback is missing req id")
	}
	return newAIBotWebSocketSender(msg.ReplyWriter, reqID), nil
}

func sendMarkdownChunks(
	ctx context.Context,
	sender messageSender,
	chatID string,
	content string,
) error {
	for _, chunk := range splitRunes(content, maxReplyRunes) {
		if err := sender.SendMarkdown(ctx, chatID, chunk); err != nil {
			return err
		}
	}
	return nil
}
