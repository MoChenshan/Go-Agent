package main

import (
	"context"
	"fmt"
	"time"

	lkeeventhandler "github.com/tencent-lke/lke-sdk-go/eventhandler"
	lketool "github.com/tencent-lke/lke-sdk-go/tool"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/lke"
	"trpc.group/trpc-go/trpc-agent-go/agent"
)

// -----------------------------
// "适配/胶水层"（示例）
// -----------------------------
//
// 这里模拟业务侧为了把 LKE SDK 融入现有框架而补的胶水：
// - 组装 LKE client（endpoint / tools / timeout 等）
// - 把每次请求的 userID / sessionID 映射到 LKE 所需字段
// - 最终产出一个标准的 trpc-agent-go Agent（可作为 subAgent，也可对外暴露为 A2A 服务）

type lkeClientSetup struct {
	endpoint      string
	agentName     string
	functionTools []*lketool.FunctionTool
}

func newLKEClientSetup(endpoint, agentName string) (*lkeClientSetup, error) {
	localTool, err := newLocalActionFunctionTool()
	if err != nil {
		return nil, err
	}
	if agentName == "" {
		agentName = "ExampleAgent"
	}
	return &lkeClientSetup{
		endpoint:      endpoint,
		agentName:     agentName,
		functionTools: []*lketool.FunctionTool{localTool},
	}, nil
}

func (s *lkeClientSetup) SetupClient(_ context.Context, _ *agent.Invocation, client lke.Client) error {
	if client == nil {
		return fmt.Errorf("client is nil")
	}
	if s.endpoint != "" {
		client.SetEndpoint(s.endpoint)
	}
	client.AddFunctionTools(s.agentName, s.functionTools)
	return nil
}

func resolveVisitorBizID(inv *agent.Invocation) string {
	if inv == nil || inv.Session == nil {
		return "anon-user"
	}
	if inv.Session.UserID != "" {
		return inv.Session.UserID
	}
	return "anon-user"
}

func resolveSessionID(inv *agent.Invocation) string {
	if inv == nil || inv.Session == nil {
		return fmt.Sprintf("anon-session-%d", time.Now().UnixNano())
	}
	if inv.Session.ID != "" {
		return inv.Session.ID
	}
	return fmt.Sprintf("anon-session-%d", time.Now().UnixNano())
}

func newLKEAgent(botAppKey string, mock bool, agentName string, setup *lkeClientSetup) agent.Agent {
	return lke.New(
		botAppKey,
		lke.WithName(agentName),
		lke.WithDescription("LKE client adapted as an agent"),
		lke.WithHandlerFactory(func(_ context.Context, _ *agent.Invocation) (lkeeventhandler.EventHandler, error) {
			return newOriginalEventHandler(), nil
		}),
		lke.WithMock(mock),
		lke.WithVisitorBizIDResolver(resolveVisitorBizID),
		lke.WithSessionIDResolver(resolveSessionID),
		lke.WithClientSetup(setup.SetupClient),
	)
}
