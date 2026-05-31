// Package postgres is imported to inject the trpc.group/trpc-go/trpc-agent-go/storage/postgres
// as a side effect to automatically serve for the internal version.
package postgres

import (
	"context"
	"database/sql"
	"fmt"

	database "git.code.oa.com/trpc-go/trpc-database/postgres"
	"git.code.oa.com/trpc-go/trpc-go/client"
	storage "trpc.group/trpc-go/trpc-agent-go/storage/postgres"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func init() {
	storage.SetClientBuilder(trpcClientBuilder)
}

func trpcClientBuilder(ctx context.Context, builderOpts ...storage.ClientBuilderOpt) (storage.Client, error) {
	o := &storage.ClientBuilderOpts{}
	for _, opt := range builderOpts {
		opt(o)
	}

	// Convert extra options to tRPC options.
	var trpcOpts []client.Option
	for _, opt := range o.ExtraOptions {
		opt, ok := opt.(client.Option)
		if !ok {
			return nil, fmt.Errorf("trpc postgres: invalid extra option %v, type %T, expect trpc client.Option", opt, opt)
		}
		trpcOpts = append(trpcOpts, opt)
	}

	// Connection string will be used as target.
	if o.ConnString != "" {
		trpcOpts = append(trpcOpts, client.WithTarget(o.ConnString))
	}

	serviceName := getServiceNameFromOpts(trpcOpts...)
	if serviceName == "" {
		serviceName = "trpc-agent-go-postgres"
	}

	trpcClient := database.NewClientProxy(serviceName, trpcOpts...)

	return &postgresClientAdapter{
		client: trpcClient,
	}, nil
}

func getServiceNameFromOpts(opts ...client.Option) string {
	o := client.Options{}
	for _, opt := range opts {
		opt(&o)
	}
	return o.ServiceName
}

// postgresClientAdapter adapts the trpc-database postgres client to storage.Client interface.
type postgresClientAdapter struct {
	client database.Client
}

// ExecContext executes a query that doesn't return rows.
func (a *postgresClientAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return a.client.Exec(ctx, query, args...)
}

// Query executes a query that returns rows and passes them to the handler.
// The rows are automatically closed after the handler returns.
func (a *postgresClientAdapter) Query(ctx context.Context, handler storage.HandlerFunc, query string, args ...any) error {
	result, err := a.client.QueryRows(ctx, query, args...)
	if err != nil {
		return err
	}
	defer result.Close()
	rows := result.Rows()
	if err := handler(rows); err != nil {
		return err
	}
	return rows.Err()
}

// Transaction executes a function within a transaction.
// The transaction is automatically committed if the function returns nil,
// or rolled back if the function returns an error or panics.
func (a *postgresClientAdapter) Transaction(ctx context.Context, fn storage.TxFunc) error {
	// The trpc client's Transaction method has compatible signature,
	// so we can directly delegate to it
	return a.client.Transaction(ctx, database.TxFunc(fn))
}

// Close closes the database connection pool and releases all resources.
// For tRPC client, this is a no-op as the connection lifecycle is managed by the tRPC framework.
func (a *postgresClientAdapter) Close() error {
	// tRPC client doesn't require explicit cleanup
	// The connection pool is managed by the tRPC framework
	return nil
}
