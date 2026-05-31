package sdk

import (
	"errors"
)

var (
	// defaultTaijiHYAPIURL is the default url for taiji hunyuan aide api
	defaultTaijiHYAPIURL = "http://hunyuanaide.taiji.woa.com"
)

// Option is an option for the TRagOption.
type Option func(*TaijiOption)

// TaijiOption is the option for the tRAG client.
type TaijiOption struct {
	// EmbIndex is the index id of your embeddings service.
	// refer https://iwiki.woa.com/p/4008515885
	EmbIndex string
	// WSID is the workspace id.
	WSID string
	// Token is the token for the taiji.
	Token string
	// URL is the url for the taiji.
	URL string

	// ClientBuilder is the client builder for the taiji, optional field.
	ClientBuilder ClientBuilder
	// ServiceName is the service name of taiji.
	ServiceName string

	// TaijiHYAPIToken is the token for the taiji hy api, used for load/update document,optional field.
	// refer https://iwiki.woa.com/p/4010689738
	TaijiHYAPIToken string
	// TaijiHYAPIURL is the url for the taiji hy api, used for load/update document,optional field.
	TaijiHYAPIURL string
}

// NewTaijiOption creates a new TaijiOption with functional options.
func NewTaijiOption(opts ...Option) TaijiOption {
	opt := &TaijiOption{
		TaijiHYAPIURL: defaultTaijiHYAPIURL,
		ClientBuilder: defaultClientBuilder,
	}
	for _, o := range opts {
		o(opt)
	}
	return *opt
}

// WithEmbIndex sets the embedding index id.
func WithEmbIndex(embIndex string) Option {
	return func(opt *TaijiOption) {
		opt.EmbIndex = embIndex
	}
}

// WithWSID sets the workspace id.
func WithWSID(wsid string) Option {
	return func(opt *TaijiOption) {
		opt.WSID = wsid
	}
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

// WithTaijiHYAPIToken sets the taiji hy api token.
func WithTaijiHYAPIToken(token string) Option {
	return func(opt *TaijiOption) {
		opt.TaijiHYAPIToken = token
	}
}

// WithTaijiHYAPIURL sets the taiji hy api url.
func WithTaijiHYAPIURL(url string) Option {
	return func(opt *TaijiOption) {
		opt.TaijiHYAPIURL = url
	}
}

// CheckTaijiOption checks the taiji option.
func CheckTaijiOption(opt *TaijiOption) error {
	if opt.EmbIndex == "" {
		return errors.New("taiji embedding index id is empty")
	}
	if opt.WSID == "" {
		return errors.New("taiji workspace id is empty")
	}
	if opt.Token == "" {
		return errors.New("taiji auth token is empty")
	}
	if opt.URL == "" && opt.ServiceName == "" {
		return errors.New("taiji url or service name is empty")
	}
	return nil
}
