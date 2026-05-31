package trag

// Option is a function that configures a ToolSet.
type Option func(*Options)

// Options holds configuration options for creating a TRAG toolset.
type Options struct {
	apiKey    string
	funcNames []string
}

// WithFuncNames specifies which functions to load from the toolset.
// If not provided, all available functions in the toolset will be loaded.
func WithFuncNames(funcNames ...string) Option {
	return func(o *Options) {
		o.funcNames = funcNames
	}
}

// WithAPIKey sets the API key for authentication with the TRAG platform.
// If not provided, the API key will be read from the TRAG_API_KEY environment variable.
func WithAPIKey(apiKey string) Option {
	return func(o *Options) {
		o.apiKey = apiKey
	}
}
