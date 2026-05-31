package main

import (
	publicsubagent "git.woa.com/trpc-go/trpc-agent-go/openclaw/subagent"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

type subagentAwareChannel interface {
	SetSubagentService(publicsubagent.Service)
}

func injectSubagentService(
	channels []occhannel.Channel,
	svc publicsubagent.Service,
) {
	if svc == nil {
		return
	}
	for _, ch := range channels {
		aware, ok := ch.(subagentAwareChannel)
		if !ok || aware == nil {
			continue
		}
		aware.SetSubagentService(svc)
	}
}
