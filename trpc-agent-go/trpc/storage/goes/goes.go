// Package goes is imported to inject the trpc-group/trpc-agent-go/storage/elasticsearch
// as a side effect to automatically serve for the internal version.
package goes

import (
	"fmt"

	"git.code.oa.com/trpc-go/trpc-go/client"
	database "git.woa.com/trpc-go/trpc-database/goes"
	storage "trpc.group/trpc-go/trpc-agent-go/storage/elasticsearch"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func init() {
	storage.SetClientBuilder(trpcClientBuilder)
}

// trpcClientBuilder builds an Elasticsearch Client using internal goes library.
// It follows the same pattern as redis storage:
// - builderOpts: for ES configuration and version detection
func trpcClientBuilder(builderOpts ...storage.ClientBuilderOpt) (any, error) {
	// Parse builder options.
	o := &storage.ClientBuilderOpts{}
	for _, opt := range builderOpts {
		opt(o)
	}

	// Convert extra options to tRPC options.
	var trpcOpts []client.Option
	for _, opt := range o.ExtraOptions {
		opt, ok := opt.(client.Option)
		if !ok {
			return nil, fmt.Errorf("trpc redis: invalid extra option %v, type %T, expect trpc client.Option", opt, opt)
		}
		trpcOpts = append(trpcOpts, opt)
	}
	// Extract service name from ExtraOptions or use default.
	serviceName := getServiceNameFromOpts(trpcOpts...)
	if serviceName == "" {
		serviceName = "trpc-agent-go-goes"
	}

	// Choose version if specified.
	switch o.Version {
	case storage.ESVersionV7:
		esClientV7, err := database.NewElasticClientV7(serviceName, trpcOpts...)
		return esClientV7, err
	case storage.ESVersionV8:
		esClientV8, err := database.NewElasticClientV8(serviceName, trpcOpts...)
		return esClientV8, err
	case storage.ESVersionV9, storage.ESVersionUnspecified:
		// ES v9 not supported internally, fallback to default.
		fallthrough
	default:
		return nil, fmt.Errorf("trpc goes: unsupported ES version: %v", o.Version)
	}
}

// getServiceNameFromOpts gets service name from TRPC options.
func getServiceNameFromOpts(opts ...client.Option) string {
	o := client.Options{}
	for _, opt := range opts {
		opt(&o)
	}
	return o.ServiceName
}
