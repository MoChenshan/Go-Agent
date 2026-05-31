package sdk

import (
	"testing"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"github.com/stretchr/testify/assert"
)

func TestWithTRPCClientOptions(t *testing.T) {
	opts := &httpClientOptions{}

	// Test adding multiple options
	opt1 := client.WithTarget("test-target")
	opt2 := client.WithServiceName("test-service")

	WithHTTPTRPCClientOptions(opt1, opt2)(opts)

	assert.Equal(t, 2, len(opts.TRPCClientOptions))
}

func TestWithHTTPClientName(t *testing.T) {
	opts := &httpClientOptions{}
	name := "test-client"

	WithHTTPClientName(name)(opts)

	assert.Equal(t, name, opts.Name)
}

func TestDefaultClientBuilder(t *testing.T) {
	// Test if it runs without panic
	client := defaultClientBuilder(
		WithHTTPClientName("test"),
		WithHTTPTRPCClientOptions(client.WithTarget("dns://example.com")),
	)
	assert.NotNil(t, client)
}
