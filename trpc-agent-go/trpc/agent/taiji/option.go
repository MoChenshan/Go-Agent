package taiji

import "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/taiji/sdk"

type Option func(*Agent)

// WithAgentName sets the name for the agent.
func WithAgentName(name string) Option {
	return func(a *Agent) {
		a.name = name
	}
}

// WithAgentDescription sets the description for the agent.
func WithAgentDescription(desc string) Option {
	return func(a *Agent) {
		a.desc = desc
	}
}

// WithTaijiOption sets the taiji option for the agent.
func WithTaijiOption(taijiOpts sdk.TaijiOption) Option {
	return func(a *Agent) {
		a.taijiOpts = &taijiOpts
	}
}

// WithChannelBufSize sets the buffer size for the event channel.
func WithChannelBufSize(size int) Option {
	return func(a *Agent) {
		a.channelBufSize = size
	}
}

// WithStreaming sets the streaming mode of the taiji.
func WithStreaming(streaming bool) Option {
	return func(a *Agent) {
		a.streaming = streaming
	}
}

// WithMaxEventSize sets the maximum size of the scanner token.
// default is 128k
func WithMaxEventSize(size int) Option {
	return func(a *Agent) {
		a.maxEventSize = size
	}
}
