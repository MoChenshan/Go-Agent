package client

import "time"

// Option is a function that configures a Client.
type Option func(*Options)

type Options struct {
	baseURL string
	path    string
	model   string
	timeout time.Duration
}

// WithBaseURL sets the base URL for the Client.
func WithBaseURL(baseURL string) Option {
	return func(c *Options) {
		c.baseURL = baseURL
	}
}

// WithPath sets the path for the Client.
func WithPath(path string) Option {
	return func(c *Options) {
		c.path = path
	}
}

// WithModel sets the model for the Client.
func WithModel(model string) Option {
	return func(c *Options) {
		c.model = model
	}
}

// WithTimeout sets the timeout for the Client.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Options) {
		c.timeout = timeout
	}
}
