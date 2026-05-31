// Package tcvector is imported to inject the trpc-group/trpc-agent-go/storage/tcvector
// as a side effect to automatically serve for the internal version.
package tcvector

import (
	"fmt"
	"net/url"

	"git.code.oa.com/trpc-go/trpc-go/client"
	database "git.woa.com/trpc-go/trpc-database/tcvectordb"
	"trpc.group/trpc-go/trpc-agent-go/storage/tcvector"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func init() {
	tcvector.SetClientBuilder(trpcClientBuilder)
}

func trpcClientBuilder(builderOpts ...tcvector.ClientBuilderOpt) (tcvector.ClientInterface, error) {
	o := &tcvector.ClientBuilderOpts{}
	for _, opt := range builderOpts {
		opt(o)
	}

	// Convert extra options to tRPC options.
	var trpcOpts []client.Option
	for _, opt := range o.ExtraOptions {
		opt, ok := opt.(client.Option)
		if !ok {
			return nil, fmt.Errorf("trpc tcvector: invalid extra option %v, type %T, expect trpc client.Option", opt, opt)
		}
		trpcOpts = append(trpcOpts, opt)
	}

	// Get service name from options or use default.
	serviceName := getServiceNameFromOpts(trpcOpts...)
	if serviceName == "" {
		serviceName = "trpc-agent-go-tcvector"
	}

	// Build target from HTTPURL, UserName, Key if provided.
	// Format: tcvectordb://user:key@host/path
	if o.HTTPURL != "" {
		parsedURL, err := url.Parse(o.HTTPURL)
		if err != nil {
			return nil, fmt.Errorf("trpc tcvector: parse url %s: %w", o.HTTPURL, err)
		}
		trpcTarget := fmt.Sprintf("tcvectordb://%s@%s%s", url.UserPassword(o.UserName, o.Key), parsedURL.Host, parsedURL.Path)
		trpcOpts = append(trpcOpts, client.WithTarget(trpcTarget))
	}

	trpcClient, err := database.NewClient(serviceName, trpcOpts...)
	if err != nil {
		return nil, fmt.Errorf("trpc tcvector: new client %s: %w", serviceName, err)
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
