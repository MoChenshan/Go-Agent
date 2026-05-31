package sdk

import "git.code.oa.com/trpc-go/trpc-go/client"

// Option is an option for the TRagOption.
type Option func(*TaijiOption)

// TaijiOption is the option for the tRAG client.
type TaijiOption struct {
	// Token is the token for the taiji.
	// refer https://iwiki.woa.com/p/4008515885
	Token string
	// URL is the url for the taiji.
	// refer https://iwiki.woa.com/p/4008515885
	URL string

	// ClientBuilder is the client builder for the taiji, optional field.
	ClientBuilder ClientBuilder
	// ServiceName is the service name of taiji.
	ServiceName string

	// ApplicationID is the application id of the agent.
	// refer https://iwiki.woa.com/p/4014591694
	ApplicationID string

	// TRPCClientOptions is the tRPC client options.
	TRPCClientOptions []client.Option
}

// NewTaijiOption creates a new TaijiOption with functional options.
func NewTaijiOption(opts ...Option) TaijiOption {
	opt := &TaijiOption{
		ClientBuilder: defaultClientBuilder,
	}
	for _, o := range opts {
		o(opt)
	}
	return *opt
}

// WithToken sets the auth token.
func WithToken(token string) Option {
	return func(opt *TaijiOption) {
		opt.Token = token
	}
}

// WithURL sets the taiji url.
// You can specify Taiji Host By WithURL
// WithURL has higher priority than WithServiceName
func WithURL(url string) Option {
	return func(opt *TaijiOption) {
		opt.URL = url
	}
}

// WithClientBuilder sets the client builder.
func WithClientBuilder(builder ClientBuilder) Option {
	return func(opt *TaijiOption) {
		opt.ClientBuilder = builder
	}
}

// WithServiceName sets the service name of the HTTP client.
// You can specify Taiji Host By WithServiceName which bound a client service with target in trpc_go.yaml
// WithServiceName has lower priority than WithURL
func WithServiceName(name string) Option {
	return func(opt *TaijiOption) {
		opt.ServiceName = name
	}
}

// WithApplicationID sets the application id of the taiji.
func WithApplicationID(appID string) Option {
	return func(opt *TaijiOption) {
		opt.ApplicationID = appID
	}
}

// WithTRPCClientOptions sets the tRPC client options.
func WithTRPCClientOptions(opts ...client.Option) Option {
	return func(opt *TaijiOption) {
		if opt == nil {
			return
		}
		opt.TRPCClientOptions = append(opt.TRPCClientOptions, opts...)
	}
}
