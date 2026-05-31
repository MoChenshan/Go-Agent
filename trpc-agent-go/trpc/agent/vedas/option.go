package vedas

// Option is an option for the vedas agent.
type Option func(*Agent)

// WithName sets the name for the agent.
func WithName(name string) Option {
	return func(a *Agent) {
		a.name = name
	}
}

// WithDescription sets the description for the agent.
func WithDescription(desc string) Option {
	return func(a *Agent) {
		a.desc = desc
	}
}

// WithConfigs sets the vedas configs for the agent.
func WithConfigs(configs *Configs) Option {
	return func(a *Agent) {
		a.configs = configs
	}
}

// WithMaxEventSize sets the maximum size of the scanner token for SSE lines.
func WithMaxEventSize(size int) Option {
	return func(a *Agent) {
		a.maxEventSize = size
	}
}
