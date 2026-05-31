package main

import (
	"context"
	"testing"

	publicsubagent "git.woa.com/trpc-go/trpc-agent-go/openclaw/subagent"
	"github.com/stretchr/testify/require"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

type subagentAwareTestChannel struct {
	svc publicsubagent.Service
}

func (c *subagentAwareTestChannel) ID() string {
	return "subagent-aware-test"
}

func (c *subagentAwareTestChannel) Run(context.Context) error {
	return nil
}

func (c *subagentAwareTestChannel) Close(chan struct{}) error {
	return nil
}

func (c *subagentAwareTestChannel) SetSubagentService(
	svc publicsubagent.Service,
) {
	c.svc = svc
}

type stubSubagentService struct{}

func (s *stubSubagentService) ListForUser(
	userID string,
	filter publicsubagent.ListFilter,
) []publicsubagent.Run {
	return nil
}

func (s *stubSubagentService) GetForUser(
	userID string,
	runID string,
) (*publicsubagent.Run, error) {
	return nil, publicsubagent.ErrRunNotFound
}

func (s *stubSubagentService) CancelForUser(
	userID string,
	runID string,
) (*publicsubagent.Run, bool, error) {
	return nil, false, publicsubagent.ErrRunNotFound
}

func TestInjectSubagentService(t *testing.T) {
	t.Parallel()

	svc := &stubSubagentService{}
	stub := &subagentAwareTestChannel{}

	injectSubagentService(
		[]occhannel.Channel{stub},
		svc,
	)

	require.Same(t, svc, stub.svc)
}
