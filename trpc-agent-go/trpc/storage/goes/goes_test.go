package goes

import (
	"testing"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"github.com/stretchr/testify/assert"
	storage "trpc.group/trpc-go/trpc-agent-go/storage/elasticsearch"
)

func TestGetServiceNameFromOpts(t *testing.T) {
	var optA client.Option = func(o *client.Options) { o.ServiceName = "svcA" }
	var optB client.Option = func(o *client.Options) { o.ServiceName = "svcB" }
	name := getServiceNameFromOpts(optA, optB)
	assert.Equal(t, "svcB", name)

	// No opts -> empty.
	assert.Equal(t, "", getServiceNameFromOpts())
}

func TestBuilder_UnsupportedVersion(t *testing.T) {
	_, err := trpcClientBuilder(storage.WithVersion(storage.ESVersionV9))
	assert.Error(t, err)
}

func TestBuilder_InvalidExtraOption(t *testing.T) {
	// Pass a non-client.Option as extra option.
	invalidOpt := "not-a-client-option"
	_, err := trpcClientBuilder(
		storage.WithExtraOptions(invalidOpt),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid extra option")
}

func TestBuilder_V7_V8_ErrorPaths(t *testing.T) {
	// Without actual DB config, the internal goes will fail, but builder should plumb options correctly.
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "svc" }
	_, err := trpcClientBuilder(storage.WithVersion(storage.ESVersionV7), storage.WithExtraOptions(svcOpt))
	assert.Error(t, err)

	_, err = trpcClientBuilder(storage.WithVersion(storage.ESVersionV8), storage.WithExtraOptions(svcOpt))
	assert.Error(t, err)
}
