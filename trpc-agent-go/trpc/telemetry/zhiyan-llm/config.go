package zhiyanllm

import (
	"os"

	zhiyanllm "git.woa.com/zhiyan-monitor/sdk/llm_go_sdk"
)

const (
	// Environment variable keys
	envAPIEndpoint = "ZHIYANLLM_API_ENDPOINT"
	envAPIKey      = "ZHIYANLLM_API_KEY"
	envAppName     = "ZHIYANLLM_APP_NAME"
)

// getEnv gets an environment variable
func getEnv(key string) string {
	return os.Getenv(key)
}

// Option defines a function type for configuring the client
type Option func(*config)

// config holds the configuration for the client
type config struct {
	apiEndpoint                 string
	apiKey                      string
	appName                     string
	attributeValueLengthLimit   int
	attributeCountLimit         int
	eventCountLimit             int
	attributePerEventCountLimit int
}

// WithAPIEndpoint sets the API endpoint
func WithAPIEndpoint(endpoint string) Option {
	return func(c *config) {
		c.apiEndpoint = endpoint
	}
}

// WithAPIKey sets the API key
func WithAPIKey(key string) Option {
	return func(c *config) {
		c.apiKey = key
	}
}

// WithAppName sets the application name
func WithAppName(name string) Option {
	return func(c *config) {
		c.appName = name
	}
}

// WithAttributeValueLengthLimit sets the attribute value length limit
func WithAttributeValueLengthLimit(limit int) Option {
	return func(c *config) {
		c.attributeValueLengthLimit = limit
	}
}

// WithAttributeCountLimit sets the attribute count limit
func WithAttributeCountLimit(limit int) Option {
	return func(c *config) {
		c.attributeCountLimit = limit
	}
}

// WithEventCountLimit sets the event count limit
func WithEventCountLimit(limit int) Option {
	return func(c *config) {
		c.eventCountLimit = limit
	}
}

// WithAttributePerEventCountLimit sets the attribute per event count limit
func WithAttributePerEventCountLimit(limit int) Option {
	return func(c *config) {
		c.attributePerEventCountLimit = limit
	}
}

// newDefaultClientConfig creates a new config with default values
func newDefaultClientConfig() *config {
	return &config{
		apiEndpoint:                 getEnv(envAPIEndpoint),
		apiKey:                      getEnv(envAPIKey),
		appName:                     getEnv(envAppName),
		attributeCountLimit:         zhiyanllm.DEFAULT_ATTRIBUTE_COUNT_LIMIT,
		attributeValueLengthLimit:   zhiyanllm.DEFAULT_ATTRIBUTE_VALUE_LENGTH_LIMIT,
		eventCountLimit:             zhiyanllm.DEFAULT_EVENT_COUNT_LIMIT,
		attributePerEventCountLimit: zhiyanllm.DEFAULT_ATTRIBUTE_PER_EVENT_COUNT_LIMIT,
	}
}

// fixConfig applies maximum limits to configuration values and returns the fixed config
func fixConfig(c *config) *config {
	c.attributeCountLimit = min(c.attributeCountLimit, zhiyanllm.MAX_ATTRIBUTE_COUNT_LIMIT)
	c.attributeValueLengthLimit = min(c.attributeValueLengthLimit, zhiyanllm.MAX_ATTRIBUTE_VALUE_LENGTH_LIMIT)
	c.eventCountLimit = min(c.eventCountLimit, zhiyanllm.MAX_EVENT_COUNT_LIMIT)
	c.attributePerEventCountLimit = min(c.attributePerEventCountLimit, zhiyanllm.MAX_ATTRIBUTE_PER_EVENT_COUNT_LIMIT)
	return c
}
