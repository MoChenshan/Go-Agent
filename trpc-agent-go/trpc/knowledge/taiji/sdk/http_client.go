// Package sdk provides the taiji options for knowledge.
package sdk

import (
	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
)

// HTTPClient is the interface for HTTP client
type HTTPClient = ihttp.HTTPClient

// ClientBuilder is a function that builds an HTTP client
type ClientBuilder func(opts ...HTTPClientOption) HTTPClient

// HTTPClientOption is the option for HTTP client
type HTTPClientOption func(*httpClientOptions)

type httpClientOptions struct {
	Name string
}

// WithHTTPClientName sets the service name of the HTTP client.
func WithHTTPClientName(name string) HTTPClientOption {
	return func(opts *httpClientOptions) {
		opts.Name = name
	}
}

// defaultClientBuilder returns the default HTTP client builder.
func defaultClientBuilder(opts ...HTTPClientOption) HTTPClient {
	options := &httpClientOptions{}
	for _, opt := range opts {
		opt(options)
	}
	client := ihttp.NewRequestHandler(options.Name)
	return client
}
