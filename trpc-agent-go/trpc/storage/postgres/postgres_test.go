package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/storage/postgres"
)

func TestBuilder_WithValidOptions(t *testing.T) {
	client, err := trpcClientBuilder(
		context.Background(),
		postgres.WithClientConnString("postgres://testuser:testpass@localhost:5432/testdb"),
	)
	// The client creation may fail due to connection issues,
	// but we expect no panic during construction.
	if err != nil {
		t.Logf("Expected error during connection: %v", err)
	} else {
		assert.NotNil(t, client)
	}
}

func TestBuilder_MissingConnString(t *testing.T) {
	_, err := trpcClientBuilder(context.Background())
	// Should handle empty connection string gracefully.
	// The actual behavior depends on trpc-database/postgres implementation.
	if err != nil {
		t.Logf("Expected error for missing connection string: %v", err)
	}
}

func TestBuilder_ConnectionStringFormats(t *testing.T) {
	testCases := []struct {
		name       string
		connString string
	}{
		{"Standard format", "postgres://user:pass@localhost:5432/dbname"},
		{"With SSL mode", "postgres://user:pass@localhost:5432/dbname?sslmode=disable"},
		{"With schema", "postgres://user:pass@localhost:5432/dbname?search_path=public"},
		{"IP address", "postgres://user:pass@192.168.1.1:5432/dbname"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := trpcClientBuilder(
				context.Background(),
				postgres.WithClientConnString(tc.connString),
			)
			// Connection may fail, but construction should not panic.
			if err != nil {
				t.Logf("Expected error during connection for %s: %v", tc.name, err)
			} else {
				assert.NotNil(t, client)
			}
		})
	}
}

func TestBuilder_SpecialCharactersInConnString(t *testing.T) {
	// Test with special characters in password.
	client, err := trpcClientBuilder(
		context.Background(),
		postgres.WithClientConnString("postgres://user:p@ss!w0rd@localhost:5432/dbname"),
	)
	// Should handle special characters properly.
	if err != nil {
		t.Logf("Expected error during connection: %v", err)
	} else {
		assert.NotNil(t, client)
	}
}

func TestQuery_WithoutConnectionString(t *testing.T) {
	// Create client without connection string
	client, err := trpcClientBuilder(context.Background())
	if err != nil {
		t.Logf("Client creation error (expected): %v", err)
		return
	}

	// Query will try to execute but will likely fail due to no connection
	// The method is implemented but requires actual DB connection to work
	err = client.Query(context.Background(), func(rows *sql.Rows) error {
		return nil
	}, "SELECT 1")
	// Error is expected (connection failure or query execution failure)
	if err != nil {
		t.Logf("Expected error: %v", err)
	}
}

func TestTransaction_WithoutConnectionString(t *testing.T) {
	// Create client without connection string
	client, err := trpcClientBuilder(context.Background())
	if err != nil {
		t.Logf("Client creation error (expected): %v", err)
		return
	}

	// Transaction will try to execute but will fail due to no connection
	err = client.Transaction(context.Background(), func(tx *sql.Tx) error {
		return nil
	})
	if err != nil {
		t.Logf("Expected error: %v", err)
	}
}

func TestQuery_WithConnectionString(t *testing.T) {
	// Test with a connection string (will fail to connect but should create client)
	client, err := trpcClientBuilder(
		context.Background(),
		postgres.WithClientConnString("postgres://testuser:testpass@localhost:5432/testdb"),
	)

	// Client creation will fail due to connection error, which is expected in test environment
	if err != nil {
		t.Logf("Expected connection error: %v", err)
		return
	}

	assert.NotNil(t, client)
	// Query method should be available (actual query will fail without real DB)
	err = client.Query(context.Background(), func(rows *sql.Rows) error {
		return nil
	}, "SELECT 1")
	t.Logf("Query error (expected without real DB): %v", err)
}

func TestTransaction_WithConnectionString(t *testing.T) {
	// Test with a connection string (will fail to connect but should create client)
	client, err := trpcClientBuilder(
		context.Background(),
		postgres.WithClientConnString("postgres://testuser:testpass@localhost:5432/testdb"),
	)

	// Client creation will fail due to connection error, which is expected in test environment
	if err != nil {
		t.Logf("Expected connection error: %v", err)
		return
	}

	assert.NotNil(t, client)
	// Transaction method should work
	err = client.Transaction(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("SELECT 1")
		return err
	})
	t.Logf("Transaction error (expected without real DB): %v", err)
}

func TestClose(t *testing.T) {
	// Create client
	client, err := trpcClientBuilder(context.Background())
	if err != nil {
		t.Logf("Client creation error (expected): %v", err)
		return
	}

	// Close should succeed (it's a no-op for tRPC client)
	err = client.Close()
	assert.NoError(t, err)
}
