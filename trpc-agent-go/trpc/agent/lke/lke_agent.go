package lke

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/lke/internal/collector"
	lkesdk "github.com/tencent-lke/lke-sdk-go"
	"github.com/tencent-lke/lke-sdk-go/agentastool"
	lkeeventhandler "github.com/tencent-lke/lke-sdk-go/eventhandler"
	"github.com/tencent-lke/lke-sdk-go/mcpserversse"
	lkemodel "github.com/tencent-lke/lke-sdk-go/model"
	"github.com/tencent-lke/lke-sdk-go/runlog"
	lketool "github.com/tencent-lke/lke-sdk-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Client is the LKE client configuration surface exposed to business code during setup.
//
// Intentionally excludes:
// - Run / RunWithContext: business should not execute the request during setup
// - SetEventHandler: the adapter always installs its own bridge handler
// - Close / Open: lifecycle is managed by the adapter per invocation
type Client interface {
	// Tool registration (optional).
	AddFunctionTools(agentName string, tools []*lketool.FunctionTool)
	AddMcpTools(agentName string, mcpServerSse *mcpserversse.McpServerSse, selectedToolNames []string) ([]*lketool.McpTool, error)
	AddAgentAsTool(agentName string, agentAsToolName string, toolName string, toolDescription string) (*agentastool.AgentAsTool, error)

	// Agent topology (optional).
	AddAgents(agents []lkemodel.Agent)
	AddHandoffs(sourceAgentName string, targetAgentNames []string)

	// Client runtime configuration (optional).
	SetEndpoint(endpoint string)
	SetMock(mock bool)
	SetEnableSystemOpt(enable bool)
	SetStartAgent(agentName string)
	SetHttpClient(cli *http.Client)
	SetMaxToolTurns(maxToolTurns uint)
	SetToolRunTimeout(toolRunTimeout time.Duration)
	SetRunLogger(logger runlog.RunLogger)
}

// ClientBuilder creates a new LKE client for a single invocation.
//
// The returned client will always have its EventHandler overridden by the adapter,
// so business code should not rely on any EventHandler installed by the builder.
type ClientBuilder func(ctx context.Context, invocation *agent.Invocation) (lkesdk.LkeClient, error)

// HandlerFactory creates a per-invocation business EventHandler.
//
// The returned handler is used for preserving/implementing business callbacks, and it is
// invoked before the adapter bridges events into trpc-agent-go's event stream.
type HandlerFactory func(ctx context.Context, invocation *agent.Invocation) (lkeeventhandler.EventHandler, error)

// RunOptionsFactory creates per-invocation options passed to the LKE SDK RunWithContext call.
type RunOptionsFactory func(ctx context.Context, invocation *agent.Invocation) (*lkemodel.Options, error)

// ClientSetup configures a newly created LKE client for a single invocation.
// It is called after the client is created and before RunWithContext is invoked.
type ClientSetup func(ctx context.Context, invocation *agent.Invocation, client Client) error

// Agent adapts Tencent Cloud LKE SDK clients to the trpc-agent-go Agent interface.
//
// Key design choice: the underlying lkeClient is created per Run() (per invocation),
// so it can safely carry per-user/per-session state and support concurrent sessions.
type Agent struct {
	name        string
	info        agent.Info
	options     *config
	closeOnce   sync.Once
	closed      atomic.Bool
	sessionLock *keyedSemaphore
}

// config holds configuration options for LKE Agent.
type config struct {
	// name of the agent
	name string
	// description of the agent
	description string
	// botAppKey for default client builder
	botAppKey string
	// handlerFactory builds a per-invocation business handler (optional)
	handlerFactory HandlerFactory
	// debug enables debug logging
	debug bool
	// bufferSize sets the event channel buffer size
	bufferSize int
	// defaultRunOptions are used when runOptionsFactory is not provided.
	defaultRunOptions *lkemodel.Options
	// runOptionsFactory builds per-invocation options (optional).
	runOptionsFactory RunOptionsFactory
	// mock enables LKE SDK mock mode for each created client
	mock bool
	// enableEventBypass controls whether to send events to trpc-agent-go event stream
	// When true (default): LKE events are converted to trpc-agent-go events
	// When false: Only original handler logic works, no event stream
	enableEventBypass bool
	// clientBuilder creates a new LKE client per invocation
	clientBuilder ClientBuilder
	// clientSetups configure the newly created client per invocation
	clientSetups []ClientSetup
	// visitorBizID resolves the user identifier for LKE client creation
	visitorBizID func(invocation *agent.Invocation) string
	// sessionID resolves the session identifier for LKE client creation
	sessionID func(invocation *agent.Invocation) string
	// lockKey controls whether/how to serialize invocations (e.g. per session)
	lockKey func(invocation *agent.Invocation) string
}

// Option defines a configuration option for LKE Agent
type Option func(*config)

// WithName sets the agent name.
func WithName(name string) Option {
	return func(c *config) {
		c.name = name
	}
}

// WithDescription sets the agent description.
func WithDescription(description string) Option {
	return func(c *config) {
		c.description = description
	}
}

// WithHandler sets a shared (global) business EventHandler.
//
// If you need per-invocation state (e.g. per request/session), prefer WithHandlerFactory.
func WithHandler(handler lkeeventhandler.EventHandler) Option {
	return func(c *config) {
		c.handlerFactory = func(ctx context.Context, invocation *agent.Invocation) (lkeeventhandler.EventHandler, error) {
			return handler, nil
		}
	}
}

// WithBufferSize sets the event channel buffer size.
func WithBufferSize(size int) Option {
	return func(c *config) {
		c.bufferSize = size
	}
}

// WithEventBypass controls whether to send events to trpc-agent-go event stream.
func WithEventBypass(enable bool) Option {
	return func(c *config) {
		c.enableEventBypass = enable
	}
}

// WithDefaultRunOptions sets the default LKE run options.
//
// Note: the same pointer will be reused across invocations unless you use WithRunOptionsFactory.
// If you need per-invocation CustomVariables (map) or other mutable fields, prefer WithRunOptionsFactory.
func WithDefaultRunOptions(opts *lkemodel.Options) Option {
	return func(c *config) {
		c.defaultRunOptions = opts
	}
}

// WithMock enables/disables LKE SDK mock mode for each created client.
func WithMock(enable bool) Option {
	return func(c *config) {
		c.mock = enable
	}
}

// WithClientBuilder overrides the default client creation logic.
func WithClientBuilder(builder ClientBuilder) Option {
	return func(c *config) {
		c.clientBuilder = builder
	}
}

// WithClientSetup adds a per-invocation setup hook for the created client.
func WithClientSetup(setup ClientSetup) Option {
	return func(c *config) {
		if setup == nil {
			return
		}
		c.clientSetups = append(c.clientSetups, setup)
	}
}

// WithHandlerFactory sets a per-invocation business EventHandler factory.
func WithHandlerFactory(factory HandlerFactory) Option {
	return func(c *config) {
		c.handlerFactory = factory
	}
}

// WithRunOptionsFactory sets a per-invocation run options factory.
func WithRunOptionsFactory(factory RunOptionsFactory) Option {
	return func(c *config) {
		c.runOptionsFactory = factory
	}
}

// WithVisitorBizIDResolver customizes how visitorBizID (user id) is derived from invocation.
func WithVisitorBizIDResolver(resolver func(invocation *agent.Invocation) string) Option {
	return func(c *config) {
		c.visitorBizID = resolver
	}
}

// WithSessionIDResolver customizes how sessionID (task id) is derived from invocation.
func WithSessionIDResolver(resolver func(invocation *agent.Invocation) string) Option {
	return func(c *config) {
		c.sessionID = resolver
	}
}

// WithLockKey customizes invocation serialization key.
// Return empty string to disable serialization.
func WithLockKey(lockKey func(invocation *agent.Invocation) string) Option {
	return func(c *config) {
		c.lockKey = lockKey
	}
}

// WithDebug enables debug logging.
func WithDebug(debug bool) Option {
	return func(c *config) {
		c.debug = debug
	}
}

// New creates a new LKE Agent adapter.
//
// The returned Agent creates a new lkeClient per Run(), using invocation's user/session
// identifiers by default. This is required for multi-user / multi-session workloads.
func New(botAppKey string, opts ...Option) agent.Agent {
	if botAppKey == "" {
		panic("botAppKey cannot be empty")
	}

	// Default configuration
	config := &config{
		name:              "lke-agent",
		description:       "LKE Agent adapted for trpc-agent-go",
		botAppKey:         botAppKey,
		bufferSize:        50,
		enableEventBypass: true,
		mock:              false,
		visitorBizID: func(inv *agent.Invocation) string {
			if inv == nil || inv.Session == nil || inv.Session.UserID == "" {
				return "unknown_user"
			}
			return inv.Session.UserID
		},
		sessionID: func(inv *agent.Invocation) string {
			if inv == nil {
				return ""
			}
			if inv.Session != nil && inv.Session.ID != "" {
				return inv.Session.ID
			}
			if inv.InvocationID != "" {
				return inv.InvocationID
			}
			return ""
		},
		lockKey: func(inv *agent.Invocation) string {
			if inv == nil || inv.Session == nil {
				return ""
			}
			return inv.Session.ID
		},
	}

	// Apply all options
	for _, opt := range opts {
		opt(config)
	}

	// Validate configuration
	if config.bufferSize <= 0 {
		config.bufferSize = 50 // Fallback to default
	}
	if config.name == "" {
		config.name = "lke-agent" // Fallback to default
	}
	if config.description == "" {
		config.description = "LKE Agent adapted for trpc-agent-go"
	}

	// Smart defaults: if no business handler and bypass disabled, enable bypass anyway
	// because otherwise there would be no way to get events
	if !config.enableEventBypass && config.handlerFactory == nil {
		config.enableEventBypass = true
	}

	if config.clientBuilder == nil {
		config.clientBuilder = func(ctx context.Context, invocation *agent.Invocation) (lkesdk.LkeClient, error) {
			visitorBizID := config.visitorBizID(invocation)
			sessionID := config.sessionID(invocation)
			if visitorBizID == "" {
				return nil, fmt.Errorf("visitorBizID is empty")
			}
			if sessionID == "" {
				return nil, fmt.Errorf("sessionID is empty")
			}
			client := lkesdk.NewLkeClient(config.botAppKey, visitorBizID, sessionID, nil)
			client.SetMock(config.mock)
			return client, nil
		}
	}

	return &Agent{
		name: config.name,
		info: agent.Info{
			Name:        config.name,
			Description: config.description,
		},
		options:     config,
		sessionLock: newKeyedSemaphore(),
	}
}

// Run implements agent.Agent interface.
func (a *Agent) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
	if invocation == nil {
		return nil, fmt.Errorf("invocation cannot be nil")
	}
	if a.closed.Load() {
		return nil, fmt.Errorf("agent %s is closed", a.name)
	}

	eventChan := make(chan *event.Event, a.options.bufferSize)

	go func() {
		defer close(eventChan)
		if a.closed.Load() {
			errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, fmt.Sprintf("agent %s is closed", a.name))
			_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
			return
		}

		lockKey := ""
		if a.options.lockKey != nil {
			lockKey = a.options.lockKey(invocation)
		}
		release, err := a.sessionLock.Acquire(ctx, lockKey)
		if err != nil {
			if a.options.enableEventBypass {
				errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, fmt.Sprintf("LKE execution cancelled: %v", err))
				_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
			}
			return
		}
		if release != nil {
			defer release()
		}

		var bizHandler lkeeventhandler.EventHandler
		if a.options.handlerFactory != nil {
			h, err := a.options.handlerFactory(ctx, invocation)
			if err != nil {
				if a.options.enableEventBypass {
					errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, fmt.Sprintf("failed to create LKE business handler: %v", err))
					_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
				}
				return
			}
			bizHandler = h
		}

		eventCollector := collector.New(bizHandler, a.options.enableEventBypass, a.name, a.options.debug)
		if a.options.enableEventBypass {
			eventCollector.StartProcessing(ctx, eventChan, invocation)
			defer eventCollector.StopProcessing()
		}

		// Extract query from invocation
		query := invocation.Message.Content
		if query == "" {
			// Empty query indicates caller error, should not provide meaningless default
			if a.options.enableEventBypass {
				errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, "Empty query provided: invocation.Message.Content is required")
				_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
			}
			return
		}

		lkeClient, err := a.options.clientBuilder(ctx, invocation)
		if err != nil {
			if a.options.enableEventBypass {
				errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, fmt.Sprintf("failed to create LKE client: %v", err))
				_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
			}
			return
		}
		if lkeClient == nil {
			if a.options.enableEventBypass {
				errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, "failed to create LKE client: nil client")
				_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
			}
			return
		}
		defer func() {
			defer func() {
				if r := recover(); r != nil && a.options.debug {
					log.Warnf("[LKEAgent:%s] lkeClient.Close panic recovered: %v", a.name, r)
				}
			}()
			lkeClient.Close()
		}()

		if a.options.mock {
			lkeClient.SetMock(true)
		}

		for _, setup := range a.options.clientSetups {
			if setup == nil {
				continue
			}
			if err := setup(ctx, invocation, lkeClient); err != nil {
				if a.options.enableEventBypass {
					errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, fmt.Sprintf("failed to setup LKE client: %v", err))
					_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
				}
				return
			}
		}

		// Force install our bridge handler as the very last step so that business
		// code cannot accidentally break the trpc-agent-go event stream.
		lkeClient.SetEventHandler(eventCollector)

		// Resolve per-invocation LKE options.
		lkeOpts := a.options.defaultRunOptions
		if a.options.runOptionsFactory != nil {
			opts, err := a.options.runOptionsFactory(ctx, invocation)
			if err != nil {
				if a.options.enableEventBypass {
					errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, fmt.Sprintf("failed to build LKE run options: %v", err))
					_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
				}
				return
			}
			lkeOpts = opts
		}

		// Call LKE client (this will trigger callbacks which feed our bypass)
		finalReply, err := lkeClient.RunWithContext(ctx, query, lkeOpts)

		// Send final result or error only if bypass is enabled
		if a.options.enableEventBypass {
			if err != nil {
				errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name,
					model.ErrorTypeAPIError,
					fmt.Sprintf("LKE execution failed: %v", err))
				_ = agent.EmitEvent(ctx, invocation, eventChan, errorEvent)
			} else if finalReply != nil && !finalReply.IsFromSelf && !eventCollector.HasFinalReplyEvent() {
				response := &model.Response{
					Object:  model.ObjectTypeChatCompletion,
					Created: time.Now().Unix(),
					Done:    true,
					Choices: []model.Choice{{
						Index: 0,
						Message: model.Message{
							Content: finalReply.Content,
							Role:    model.RoleAssistant,
						},
					}},
				}
				finalEvent := event.NewResponseEvent(invocation.InvocationID, a.name, response)
				_ = agent.EmitEvent(ctx, invocation, eventChan, finalEvent)
			}
		}
	}()

	return eventChan, nil
}

// Tools implements agent.Agent interface.
// LKE agents manage their tools internally, so we return an empty slice.
// The actual tools are configured within the LKE client.
func (a *Agent) Tools() []tool.Tool {
	// LKE SDK manages tools internally through its client configuration
	// We don't expose them here to avoid conflicts with trpc-agent-go tool management
	return []tool.Tool{}
}

// Info implements agent.Agent interface.
func (a *Agent) Info() agent.Info {
	return a.info
}

// SubAgents implements agent.Agent interface.
func (a *Agent) SubAgents() []agent.Agent {
	// LKE agents don't have sub-agents in the trpc-agent-go sense
	return []agent.Agent{}
}

// FindSubAgent implements agent.Agent interface.
func (a *Agent) FindSubAgent(name string) agent.Agent {
	// LKE agents don't have sub-agents in the trpc-agent-go sense
	return nil
}

// Close prevents future invocations.
func (a *Agent) Close() error {
	a.closeOnce.Do(func() {
		a.closed.Store(true)
	})
	return nil
}
