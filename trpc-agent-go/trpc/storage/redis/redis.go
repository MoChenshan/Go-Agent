// Package redis is imported to inject the trpc-group/trpc-agent-go/session/redis
// as a side effect to automatically serve for the internal version.
package redis

import (
	"fmt"

	"git.code.oa.com/trpc-go/trpc-go/client"
	database "git.woa.com/trpc-go/trpc-database/goredis/v3"
	"github.com/redis/go-redis/v9"
	storage "trpc.group/trpc-go/trpc-agent-go/storage/redis"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func init() {
	storage.SetClientBuilder(trpcClientBuilder)
}

func trpcClientBuilder(builderOpts ...storage.ClientBuilderOpt) (redis.UniversalClient, error) {
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

	// URL will cover the target option.
	if o.URL != "" {
		trpcOpts = append(trpcOpts, client.WithTarget(o.URL))
	}

	serviceName := getServiceNameFromOpts(trpcOpts...)
	if serviceName == "" {
		serviceName = "trpc-agent-go-redis"
	}
	trpcClient, err := database.New(serviceName, trpcOpts...)
	if err != nil {
		return nil, fmt.Errorf("trpc redis: new client %s: %w", serviceName, err)
	}
	return trpcClient, nil
}

func getServiceNameFromOpts(opts ...client.Option) string {
	o := client.Options{}
	for _, opt := range opts {
		opt(&o)
	}
	return o.ServiceName
}
