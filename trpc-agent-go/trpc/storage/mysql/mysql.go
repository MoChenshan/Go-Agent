// Package mysql is imported to inject the trpc-group/trpc-agent-go/storage/mysql
// as a side effect to automatically serve for the internal version.
package mysql

import (
	"context"
	"database/sql"
	"fmt"

	database "git.code.oa.com/trpc-go/trpc-database/mysql"
	"git.code.oa.com/trpc-go/trpc-go/client"
	storage "trpc.group/trpc-go/trpc-agent-go/storage/mysql"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func init() {
	storage.SetClientBuilder(trpcClientBuilder)
}

func trpcClientBuilder(builderOpts ...storage.ClientBuilderOpt) (storage.Client, error) {
	o := &storage.ClientBuilderOpts{}
	for _, opt := range builderOpts {
		opt(o)
	}

	// Convert extra options to tRPC options.
	var trpcOpts []client.Option
	for _, opt := range o.ExtraOptions {
		opt, ok := opt.(client.Option)
		if !ok {
			return nil, fmt.Errorf("trpc mysql: invalid extra option %v, type %T, expect trpc client.Option", opt, opt)
		}
		trpcOpts = append(trpcOpts, opt)
	}

	// Use DSN as target if provided.
	if o.DSN != "" {
		trpcOpts = append(trpcOpts, client.WithTarget(o.DSN))
	}

	serviceName := getServiceNameFromOpts(trpcOpts...)
	if serviceName == "" {
		serviceName = "trpc-agent-go-mysql"
	}

	// Create trpc mysql client proxy.
	clientProxy := database.NewClientProxy(serviceName, trpcOpts...)

	// Wrap the client proxy with an adapter that implements storage.Client.
	// Connection pool settings should be configured in trpc_go.yaml.
	return &clientAdapter{client: clientProxy}, nil
}

// clientAdapter wraps database.Client to implement storage.Client interface.
type clientAdapter struct {
	client database.Client
}

// Exec implements storage.Client.Exec.
func (c *clientAdapter) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.client.Exec(ctx, query, args...)
}

// Query implements storage.Client.Query.
func (c *clientAdapter) Query(ctx context.Context, next storage.NextFunc, query string, args ...any) error {
	// Adapt storage.NextFunc to database.NextFunc.
	dbNext := func(rows *sql.Rows) error {
		return next(rows)
	}
	return c.client.Query(ctx, dbNext, query, args...)
}

// QueryRow implements storage.Client.QueryRow.
func (c *clientAdapter) QueryRow(ctx context.Context, dest []any, query string, args ...any) error {
	return c.client.QueryRow(ctx, dest, query, args...)
}

// Transaction implements storage.Client.Transaction.
func (c *clientAdapter) Transaction(ctx context.Context, fn storage.TxFunc, opts ...storage.TxOption) error {
	// Adapt storage.TxFunc to database.TxFunc.
	dbFn := func(tx *sql.Tx) error {
		return fn(tx)
	}

	// Convert storage.TxOption to database.TxOption.
	var dbOpts []database.TxOption
	for _, opt := range opts {
		// Create a database.TxOption that applies the storage.TxOption.
		dbOpt := func(txOpts *sql.TxOptions) {
			opt(txOpts)
		}
		dbOpts = append(dbOpts, dbOpt)
	}

	return c.client.Transaction(ctx, dbFn, dbOpts...)
}

// Close implements storage.Client.Close.
// The trpc-database/mysql client doesn't have a Close method,
// so this is a no-op for compatibility.
func (c *clientAdapter) Close() error {
	return nil
}

func getServiceNameFromOpts(opts ...client.Option) string {
	o := client.Options{}
	for _, opt := range opts {
		opt(&o)
	}
	return o.ServiceName
}
