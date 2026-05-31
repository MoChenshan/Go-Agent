package tcvector

import (
	"testing"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/storage/tcvector"
)

func TestBuilder_WithValidOptions(t *testing.T) {
	client, err := trpcClientBuilder(
		tcvector.WithClientBuilderHTTPURL("http://localhost:8080"),
		tcvector.WithClientBuilderUserName("testuser"),
		tcvector.WithClientBuilderKey("testkey"),
	)
	// The client should be created successfully without actual connection.
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilder_InvalidURL(t *testing.T) {
	_, err := trpcClientBuilder(
		tcvector.WithClientBuilderHTTPURL(":invalid-url:"),
		tcvector.WithClientBuilderUserName("testuser"),
		tcvector.WithClientBuilderKey("testkey"),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse url")
}

func TestBuilder_MissingHTTPURL(t *testing.T) {
	_, err := trpcClientBuilder(
		tcvector.WithClientBuilderUserName("testuser"),
		tcvector.WithClientBuilderKey("testkey"),
	)
	// Without HTTPURL, it relies on trpc_go.yaml configuration.
	// Since the default service name is not configured, it should fail.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestBuilder_EmptyCredentials(t *testing.T) {
	// Test with empty username and key.
	_, err := trpcClientBuilder(
		tcvector.WithClientBuilderHTTPURL("http://localhost:8080"),
		tcvector.WithClientBuilderUserName(""),
		tcvector.WithClientBuilderKey(""),
	)
	// Should fail because username is required by tcvectordb.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing username")
}

func TestBuilder_OnlyHTTPURL(t *testing.T) {
	// Test with only HTTPURL, no username/key.
	_, err := trpcClientBuilder(
		tcvector.WithClientBuilderHTTPURL("http://localhost:8080"),
	)
	// Should fail because username is required by tcvectordb.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing username")
}

func TestBuilder_URLParsing(t *testing.T) {
	// Test with different URL formats.
	testCases := []struct {
		name     string
		url      string
		userName string
		key      string
	}{
		{"HTTP URL", "http://localhost:8080", "testuser", "testkey"},
		{"HTTPS URL", "https://api.example.com", "testuser", "testkey"},
		{"URL with path", "http://localhost:8080/api/v1", "testuser", "testkey"},
		{"URL with port", "http://localhost:9999", "testuser", "testkey"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := trpcClientBuilder(
				tcvector.WithClientBuilderHTTPURL(tc.url),
				tcvector.WithClientBuilderUserName(tc.userName),
				tcvector.WithClientBuilderKey(tc.key),
			)
			// Should create client successfully.
			assert.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}

func TestBuilder_SpecialCharactersInCredentials(t *testing.T) {
	// Test with special characters in username and key.
	c, err := trpcClientBuilder(
		tcvector.WithClientBuilderHTTPURL("http://localhost:8080"),
		tcvector.WithClientBuilderUserName("user@example.com"),
		tcvector.WithClientBuilderKey("key!@#$%^&*()"),
	)
	// Should handle special characters properly.
	assert.NoError(t, err)
	assert.NotNil(t, c)
}

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

func TestBuilder_WithExtraOptions(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-tcvector" }
	c, err := trpcClientBuilder(
		tcvector.WithClientBuilderHTTPURL("http://localhost:8080"),
		tcvector.WithClientBuilderUserName("testuser"),
		tcvector.WithClientBuilderKey("testkey"),
		tcvector.WithExtraOptions(svcOpt),
	)
	assert.NoError(t, err)
	assert.NotNil(t, c)
}

func TestBuilder_InvalidExtraOption(t *testing.T) {
	// Pass a non-client.Option as extra option.
	invalidOpt := "not-a-client-option"
	_, err := trpcClientBuilder(
		tcvector.WithClientBuilderHTTPURL("http://localhost:8080"),
		tcvector.WithClientBuilderUserName("testuser"),
		tcvector.WithClientBuilderKey("testkey"),
		tcvector.WithExtraOptions(invalidOpt),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid extra option")
}

func TestBuilder_WithServiceNameOnly(t *testing.T) {
	// Test with only service name, no URL.
	// This relies on trpc_go.yaml configuration.
	// Without proper configuration, it should fail with "service name not found" error.
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-tcvector-service" }
	_, err := trpcClientBuilder(
		tcvector.WithExtraOptions(svcOpt),
	)
	// Should fail because the service is not configured in trpc_go.yaml.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestBuilder_MultipleExtraOptions(t *testing.T) {
	var svcOpt client.Option = func(o *client.Options) { o.ServiceName = "test-tcvector" }
	var timeoutOpt client.Option = func(o *client.Options) { o.Timeout = 5000 }
	c, err := trpcClientBuilder(
		tcvector.WithClientBuilderHTTPURL("http://localhost:8080"),
		tcvector.WithClientBuilderUserName("testuser"),
		tcvector.WithClientBuilderKey("testkey"),
		tcvector.WithExtraOptions(svcOpt, timeoutOpt),
	)
	assert.NoError(t, err)
	assert.NotNil(t, c)
}
