package filter

import (
	"context"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/client"
	"git.code.oa.com/trpc-go/trpc-go/filter"
)

// WithRPCNameOption returns an Option that sets rpc name of backend service.
func WithRPCNameOption(name string) client.Option {
	return client.WithFilter(NewRPCNameSetterFilter(name))
}

// NewRPCNameSetterFilter returns a filter that sets rpc name of backend service.
func NewRPCNameSetterFilter(name string) filter.ClientFilter {
	return func(ctx context.Context, req, rsp any, next filter.ClientHandleFunc) error {
		trpc.Message(ctx).WithClientRPCName(name)
		return next(ctx, req, rsp)
	}
}
