package wecom

import (
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/runner"
)

const (
	defaultWebSocketURL = "wss://openws.work.weixin.qq.com"

	defaultHeartbeatInterval = 30 * time.Second
	defaultReconnectDelay    = 3 * time.Second

	defaultProcessingMessage = "Thinking..."
	defaultHelpMessage       = "Commands:\n" +
		"/help show this help message\n" +
		"/new start a new session\n" +
		"/cancel cancel the current request"
	defaultWelcomeMessage = "Send a message to start chatting.\n" +
		"Use /help to see available commands."
	defaultNewSessionMessage        = "Started a new session."
	defaultCancelOKMessage          = "Canceled."
	defaultCancelNoopMessage        = "There is no active request to cancel."
	defaultCancelUnsupportedMessage = "Cancel is unavailable " +
		"with the current runner."
	defaultQueuedMessage = "The previous request is still " +
		"running. This message is queued."
	defaultUnsupportedMessage = "This WeCom server currently " +
		"supports text messages only."
	defaultInternalErrorMessage = "The request failed. Please " +
		"try again later."
	defaultEmptyReplyMessage = "The run finished without a " +
		"text reply."
)

// Config controls how the server connects to a WeCom AI bot
// websocket.
type Config struct {
	BotID             string
	Secret            string
	BotName           string
	WebSocketURL      string
	HeartbeatInterval time.Duration
	ReconnectDelay    time.Duration
	EnableStream      bool
}

type options struct {
	processingMessage        string
	helpMessage              string
	welcomeMessage           string
	newSessionMessage        string
	cancelOKMessage          string
	cancelNoopMessage        string
	cancelUnsupportedMessage string
	queuedMessage            string
	unsupportedMessage       string
	internalErrorMessage     string
	emptyReplyMessage        string
}

func defaultOptions() options {
	return options{
		processingMessage:        defaultProcessingMessage,
		helpMessage:              defaultHelpMessage,
		welcomeMessage:           defaultWelcomeMessage,
		newSessionMessage:        defaultNewSessionMessage,
		cancelOKMessage:          defaultCancelOKMessage,
		cancelNoopMessage:        defaultCancelNoopMessage,
		cancelUnsupportedMessage: defaultCancelUnsupportedMessage,
		queuedMessage:            defaultQueuedMessage,
		unsupportedMessage:       defaultUnsupportedMessage,
		internalErrorMessage:     defaultInternalErrorMessage,
		emptyReplyMessage:        defaultEmptyReplyMessage,
	}
}

// Option customizes the user-facing messages of a WeCom server.
type Option func(*options)

// WithHelpMessage overrides the default /help response.
func WithHelpMessage(message string) Option {
	return func(opts *options) {
		opts.helpMessage = strings.TrimSpace(message)
	}
}

// WithWelcomeMessage overrides the default enter-chat response.
func WithWelcomeMessage(message string) Option {
	return func(opts *options) {
		opts.welcomeMessage = strings.TrimSpace(message)
	}
}

// WithProcessingMessage overrides the placeholder stream message.
func WithProcessingMessage(message string) Option {
	return func(opts *options) {
		opts.processingMessage = strings.TrimSpace(message)
	}
}

// WithNewSessionMessage overrides the default /new response.
func WithNewSessionMessage(message string) Option {
	return func(opts *options) {
		opts.newSessionMessage = strings.TrimSpace(message)
	}
}

// WithUnsupportedMessage overrides the unsupported-message response.
func WithUnsupportedMessage(message string) Option {
	return func(opts *options) {
		opts.unsupportedMessage = strings.TrimSpace(message)
	}
}

func normalizeConfig(cfg Config) Config {
	cfg.BotID = strings.TrimSpace(cfg.BotID)
	cfg.Secret = strings.TrimSpace(cfg.Secret)
	cfg.BotName = strings.TrimSpace(cfg.BotName)
	cfg.WebSocketURL = strings.TrimSpace(cfg.WebSocketURL)
	if cfg.WebSocketURL == "" {
		cfg.WebSocketURL = defaultWebSocketURL
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = defaultHeartbeatInterval
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = defaultReconnectDelay
	}
	return cfg
}

func validateConfig(r runner.Runner, cfg Config) error {
	if r == nil {
		return fmt.Errorf("wecom: runner cannot be nil")
	}
	if cfg.BotID == "" {
		return fmt.Errorf("wecom: AI bot id cannot be empty")
	}
	if cfg.Secret == "" {
		return fmt.Errorf("wecom: secret cannot be empty")
	}
	if cfg.WebSocketURL == "" {
		return fmt.Errorf("wecom: websocket url cannot be empty")
	}
	return nil
}
