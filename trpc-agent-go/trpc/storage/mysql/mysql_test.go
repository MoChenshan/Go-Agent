package mysql

import (
	"context"
	"database/sql"
	"testing"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	storage "trpc.group/trpc-go/trpc-agent-go/storage/mysql"
)

func TestGetServiceNameFromOpts(t *testing.T) {
	t.Run("multiple options - last wins", func(t *testing.T) {
		var optA client.Option = func(o *client.Options) { o.ServiceName = "svcA" }
		var optB client.Option = func(o *client.Options) { o.ServiceName = "svcB" }
		name := getServiceNameFromOpts(optA, optB)
		assert.Equal(t, "svcB", name)
	})

	t.Run("no options - empty string", func(t *testing.T) {
		assert.Equal(t, "", getServiceNameFromOpts())
	})

	t.Run("single option", func(t *testing.T) {
		var opt client.Option = func(o *client.Options) { o.ServiceName = "my-service" }
		name := getServiceNameFromOpts(opt)
		assert.Equal(t, "my-service", name)
	})
}

func TestBuilder_WithDSN(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-mysql" }
	client, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://user:password@tcp(localhost:3306)/testdb"),
		storage.WithExtraOptions(svcOpt),
	)
	// Client creation should succeed, but actual connection may fail.
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.IsType(t, &clientAdapter{}, client)
}

func TestBuilder_WithoutDSN(t *testing.T) {
	client, err := trpcClientBuilder()
	// Should succeed even without DSN.
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilder_InvalidExtraOption(t *testing.T) {
	// Pass a non-client.Option as extra option.
	invalidOpt := "not-a-client-option"
	_, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://user:password@tcp(localhost:3306)/testdb"),
		storage.WithExtraOptions(invalidOpt),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid extra option")
}

func TestBuilder_DefaultServiceName(t *testing.T) {
	// Test that default service name is used when not specified.
	client, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://user:password@tcp(localhost:3306)/testdb"),
	)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilder_WithValidOptions(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-mysql" }
	var targetOpt client.Option = func(o *client.Options) { o.Target = "dsn://user:password@tcp(localhost:3306)/testdb" }
	client, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://user:password@tcp(localhost:3306)/testdb"),
		storage.WithExtraOptions(svcOpt, targetOpt),
	)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilder_WithConnectionPoolSettings(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-mysql" }
	client, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://user:password@tcp(localhost:3306)/testdb"),
		storage.WithMaxOpenConns(100),
		storage.WithMaxIdleConns(10),
		storage.WithExtraOptions(svcOpt),
	)
	// Connection pool settings are ignored by trpcClientBuilder.
	// They should be configured in trpc_go.yaml.
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilder_MultipleExtraOptions(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-mysql" }
	var timeoutOpt client.Option = func(o *client.Options) { o.Timeout = 5000 }
	client, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://user:password@tcp(localhost:3306)/testdb"),
		storage.WithExtraOptions(svcOpt, timeoutOpt),
	)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilder_DSNOverridesTarget(t *testing.T) {
	var targetOpt client.Option = func(o *client.Options) { o.Target = "dsn://old-target" }
	client, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://new-target"),
		storage.WithExtraOptions(targetOpt),
	)
	// DSN should be appended after extra options, so it takes precedence.
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestClientAdapter_Interface(t *testing.T) {
	// Verify that clientAdapter implements storage.Client interface.
	var _ storage.Client = (*clientAdapter)(nil)
}

func TestClientAdapter_Close(t *testing.T) {
	adapter := &clientAdapter{}
	err := adapter.Close()
	// Close should be a no-op and return nil.
	assert.NoError(t, err)
}

func TestClientAdapter_MethodsExist(t *testing.T) {
	// Create a client adapter to verify all methods exist.
	client, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://user:password@tcp(localhost:3306)/testdb"),
	)
	require.NoError(t, err)
	require.NotNil(t, client)

	adapter, ok := client.(*clientAdapter)
	require.True(t, ok, "client should be a *clientAdapter")

	ctx := context.Background()

	// Test that methods can be called (they will fail due to no real DB).
	t.Run("Exec exists", func(t *testing.T) {
		_, err := adapter.Exec(ctx, "SELECT 1")
		// Error is expected since there's no real database.
		assert.Error(t, err)
	})

	t.Run("Query exists", func(t *testing.T) {
		err := adapter.Query(ctx, func(rows *sql.Rows) error {
			return nil
		}, "SELECT 1")
		// Error is expected since there's no real database.
		assert.Error(t, err)
	})

	t.Run("QueryRow exists", func(t *testing.T) {
		var result int
		err := adapter.QueryRow(ctx, []any{&result}, "SELECT 1")
		// Error is expected since there's no real database.
		assert.Error(t, err)
	})

	t.Run("Transaction exists", func(t *testing.T) {
		err := adapter.Transaction(ctx, func(tx *sql.Tx) error {
			return nil
		})
		// Error is expected since there's no real database.
		assert.Error(t, err)
	})

	t.Run("Close exists", func(t *testing.T) {
		err := adapter.Close()
		// Close should succeed (no-op).
		assert.NoError(t, err)
	})
}

func TestBuilder_EmptyServiceName(t *testing.T) {
	// When no service name is provided, should use default.
	client, err := trpcClientBuilder()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilder_OnlyDSN(t *testing.T) {
	client, err := trpcClientBuilder(
		storage.WithClientBuilderDSN("dsn://user:password@tcp(localhost:3306)/testdb"),
	)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilder_OnlyServiceName(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "my-service" }
	client, err := trpcClientBuilder(
		storage.WithExtraOptions(svcOpt),
	)
	require.NoError(t, err)
	assert.NotNil(t, client)
}
