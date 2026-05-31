package text2image

import "time"

// Option is a function that configures a ToolSet.
type Option func(*Options)

// Options holds configuration options for creating a text2image toolset.
type Options struct {
	apiKey    string
	model     string
	baseURL   string
	imagePath string
	name      string
	timeout   time.Duration
}

// WithAPIKey sets the API key for the text2image toolset.
func WithAPIKey(apiKey string) Option {
	return func(o *Options) {
		o.apiKey = apiKey
	}
}

// WithModel sets the image model for the text2image toolset.e.g. hunyuan-image-v3.0-v1.0.4
func WithModel(model string) Option {
	return func(o *Options) {
		o.model = model
	}
}

// WithBaseURL sets the base URL for the text2image toolset.e.g. http://hunyuanapi.woa.com
func WithBaseURL(baseURL string) Option {
	return func(o *Options) {
		o.baseURL = baseURL
	}
}

// WithImagePath sets the image path for the text2image toolset. e.g. /openapi/v1/images/ar/generations
func WithImagePath(imagePath string) Option {
	return func(o *Options) {
		o.imagePath = imagePath
	}
}

// WithName sets the name for the text2image toolset.
func WithName(name string) Option {
	return func(o *Options) {
		o.name = name
	}
}

// WithTimeout sets the timeout for the text2image toolset.
func WithTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.timeout = timeout
	}
}
