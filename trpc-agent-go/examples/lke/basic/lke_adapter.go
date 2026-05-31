package main

import (
	"context"
	"fmt"
	"time"

	lkeeventhandler "github.com/tencent-lke/lke-sdk-go/eventhandler"
	lkemodel "github.com/tencent-lke/lke-sdk-go/model"
	lketool "github.com/tencent-lke/lke-sdk-go/tool"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/lke"
	"trpc.group/trpc-go/trpc-agent-go/agent"
)

// lkeClientSetup simulates the "glue code" a business often needs:
// - set endpoint / logger
// - register tools to LKE client
// - pass per-request options (variables, throttles, ...)
//
// It is installed per invocation via lke.WithClientSetup(...),
// because the underlying LKE client is created per Run().
type lkeClientSetup struct {
	endpoint       string
	agentName      string
	functionTools  []*lketool.FunctionTool
	toolRunTimeout time.Duration
}

func newLKEClientSetup(endpoint string) (*lkeClientSetup, error) {
	localTool, err := newLocalActionFunctionTool()
	if err != nil {
		return nil, err
	}
	return &lkeClientSetup{
		endpoint:       endpoint,
		agentName:      "ExampleAgent",
		functionTools:  []*lketool.FunctionTool{localTool},
		toolRunTimeout: 30 * time.Second,
	}, nil
}

func (s *lkeClientSetup) ResolveVisitorBizID(inv *agent.Invocation) string {
	if inv == nil || inv.Session == nil || inv.Session.UserID == "" {
		return "anon-user"
	}
	return inv.Session.UserID
}

func (s *lkeClientSetup) ResolveSessionID(inv *agent.Invocation) string {
	if inv != nil && inv.Session != nil && inv.Session.ID != "" {
		return inv.Session.ID
	}
	if inv != nil && inv.InvocationID != "" {
		return inv.InvocationID
	}
	return fmt.Sprintf("anon-session-%d", time.Now().UnixNano())
}

// SetupClient is called once per invocation (per Run).
// It installs business code into the newly created LKE client.
func (s *lkeClientSetup) SetupClient(ctx context.Context, inv *agent.Invocation, client lke.Client) error {
	if client == nil {
		return fmt.Errorf("client is nil")
	}

	if s.endpoint != "" {
		client.SetEndpoint(s.endpoint)
	}

	if inv != nil && inv.Session != nil && inv.Session.ID != "" {
		client.SetRunLogger(&exampleRunLogger{
			ctx:       trpc.CloneContext(ctx),
			sessionID: inv.Session.ID,
		})
	}

	// In a real business, agentName should match the agent configured in LKE.
	client.AddFunctionTools(s.agentName, s.functionTools)

	client.SetToolRunTimeout(s.toolRunTimeout)
	client.SetEnableSystemOpt(true)
	client.SetMaxToolTurns(8)
	return nil
}

// BuildRunOptions shows how business passes per-invocation options down to the LKE SDK.
func (s *lkeClientSetup) BuildRunOptions(_ context.Context, inv *agent.Invocation) (*lkemodel.Options, error) {
	userID := s.ResolveVisitorBizID(inv)
	sessionID := s.ResolveSessionID(inv)
	return &lkemodel.Options{
		CustomVariables: map[string]string{
			"example_user_id":    userID,
			"example_session_id": sessionID,
		},
	}, nil
}

func newLKESubAgent(botAppKey string, mock bool, setup *lkeClientSetup) agent.Agent {
	return lke.New(
		botAppKey,
		lke.WithName("lke-sub-agent"),
		lke.WithDescription("LKE client adapted as a sub agent"),
		lke.WithHandlerFactory(func(_ context.Context, _ *agent.Invocation) (lkeeventhandler.EventHandler, error) {
			return newOriginalEventHandler(), nil
		}),
		lke.WithMock(mock),
		lke.WithVisitorBizIDResolver(setup.ResolveVisitorBizID),
		lke.WithSessionIDResolver(setup.ResolveSessionID),
		lke.WithClientSetup(setup.SetupClient),
		lke.WithRunOptionsFactory(setup.BuildRunOptions),
	)
}
