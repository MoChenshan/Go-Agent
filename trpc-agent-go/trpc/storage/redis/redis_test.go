package redis

import (
	"testing"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"github.com/stretchr/testify/assert"
	storage "trpc.group/trpc-go/trpc-agent-go/storage/redis"
)

func TestGetServiceNameFromOpts(t *testing.T) {
	var optA client.Option = func(o *client.Options) { o.ServiceName = "svcA" }
	var optB client.Option = func(o *client.Options) { o.ServiceName = "svcB" }
	name := getServiceNameFromOpts(optA, optB)
	assert.Equal(t, "svcB", name)

	// No opts -> empty.
	assert.Equal(t, "", getServiceNameFromOpts())
}

func TestBuilder_WithURL(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-redis" }
	_, err := trpcClientBuilder(
		storage.WithClientBuilderURL("redis://localhost:6379"),
		storage.WithExtraOptions(svcOpt),
	)
	// Expected to fail because no actual Redis server is running.
	assert.Error(t, err)
}

func TestBuilder_WithoutURL(t *testing.T) {
	_, err := trpcClientBuilder()
	// Should fail because URL is required.
	assert.Error(t, err)
}

func TestBuilder_InvalidExtraOption(t *testing.T) {
	// Pass a non-client.Option as extra option.
	invalidOpt := "not-a-client-option"
	_, err := trpcClientBuilder(
		storage.WithClientBuilderURL("redis://localhost:6379"),
		storage.WithExtraOptions(invalidOpt),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid extra option")
}

func TestBuilder_DefaultServiceName(t *testing.T) {
	// Test that default service name is used when not specified.
	_, err := trpcClientBuilder(
		storage.WithClientBuilderURL("redis://localhost:6379"),
	)
	assert.Error(t, err)
}

func TestBuilder_WithValidOptions(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-redis" }
	var targetOpt client.Option = func(o *client.Options) { o.Target = "redis://localhost:6379" }
	_, err := trpcClientBuilder(
		storage.WithClientBuilderURL("redis://localhost:6379"),
		storage.WithExtraOptions(svcOpt, targetOpt),
	)
	// Expected to fail because no actual Redis server is running.
	assert.Error(t, err)
}
