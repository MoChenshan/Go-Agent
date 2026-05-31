package eino

// Config contains configuration options for eino adapters.
type Config struct {
	// Debug enables debug logging for the adapter.
	Debug bool

	// ChunkSize sets the maximum chunk size for streaming operations.
	ChunkSize int

	// BufferSize sets the buffer size for event channels.
	BufferSize int
}

// Option is a function that modifies the Config.
type Option func(*Config)

// WithDebug enables or disables debug mode.
func WithDebug(enable bool) Option {
	return func(c *Config) {
		c.Debug = enable
	}
}

// WithChunkSize sets the maximum chunk size for streaming operations.
func WithChunkSize(size int) Option {
	return func(c *Config) {
		c.ChunkSize = size
	}
}

// WithBufferSize sets the buffer size for event channels.
func WithBufferSize(size int) Option {
	return func(c *Config) {
		c.BufferSize = size
	}
}

// buildConfig creates a Config from the provided options.
func buildConfig(options ...Option) *Config {
	config := &Config{
		Debug:      false, // Reasonable default
		ChunkSize:  1024,  // Tested default value
		BufferSize: 100,   // Moderate buffer size
	}

	for _, option := range options {
		option(config)
	}

	return config
}
