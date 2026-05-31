package tmemory

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions.clone()
	require.Equal(t, defaultHost, opts.host)
	require.Equal(t, defaultStrategyID, opts.strategyID)
	require.Equal(t, defaultSource, opts.source)
	require.Equal(t, defaultTimeout, opts.timeout)
	require.Equal(t, defaultAsyncIngestNum, opts.asyncIngestNum)
	require.NotEmpty(t, opts.recallConfig)
}

func TestClone_DeepCopy(t *testing.T) {
	orig := defaultOptions.clone()
	cloned := orig.clone()
	cloned.recallConfig["new_key"] = VectorRecallConfig{MemoryType: "vector"}
	require.NotContains(t, orig.recallConfig, "new_key")
}

func TestWithHost(t *testing.T) {
	var opts serviceOpts
	WithHost("http://example.com")(&opts)
	require.Equal(t, "http://example.com", opts.host)

	WithHost("")(&opts)
	require.Equal(t, "http://example.com", opts.host)
}

func TestWithAPIKey(t *testing.T) {
	var opts serviceOpts
	WithAPIKey("secret")(&opts)
	require.Equal(t, "secret", opts.apiKey)

	WithAPIKey("")(&opts)
	require.Equal(t, "secret", opts.apiKey)
}

func TestWithBizID(t *testing.T) {
	var opts serviceOpts
	WithBizID("mybiz")(&opts)
	require.Equal(t, "mybiz", opts.bizID)
}

func TestWithStrategyID(t *testing.T) {
	var opts serviceOpts
	WithStrategyID("42")(&opts)
	require.Equal(t, "42", opts.strategyID)
}

func TestWithSource(t *testing.T) {
	var opts serviceOpts
	WithSource("my-source")(&opts)
	require.Equal(t, "my-source", opts.source)
}

func TestWithTimeout(t *testing.T) {
	var opts serviceOpts
	WithTimeout(5 * time.Second)(&opts)
	require.Equal(t, 5*time.Second, opts.timeout)

	WithTimeout(0)(&opts)
	require.Equal(t, 5*time.Second, opts.timeout)

	WithTimeout(-1)(&opts)
	require.Equal(t, 5*time.Second, opts.timeout)
}

func TestWithHTTPClient(t *testing.T) {
	var opts serviceOpts
	custom := &http.Client{}
	WithHTTPClient(custom)(&opts)
	require.Same(t, custom, opts.client)

	WithHTTPClient(nil)(&opts)
	require.Same(t, custom, opts.client)
}

func TestWithRecallConfig(t *testing.T) {
	var opts serviceOpts
	cfg := map[string]any{"raw": VectorRecallConfig{MemoryType: "vector"}}
	WithRecallConfig(cfg)(&opts)
	require.Len(t, opts.recallConfig, 1)

	WithRecallConfig(nil)(&opts)
	require.Len(t, opts.recallConfig, 1)
}

func TestWithAsyncIngestNum(t *testing.T) {
	var opts serviceOpts
	WithAsyncIngestNum(3)(&opts)
	require.Equal(t, 3, opts.asyncIngestNum)

	WithAsyncIngestNum(0)(&opts)
	require.Equal(t, 3, opts.asyncIngestNum)
}

func TestWithIngestQueueSize(t *testing.T) {
	var opts serviceOpts
	WithIngestQueueSize(20)(&opts)
	require.Equal(t, 20, opts.ingestQueueSize)
}

func TestWithIngestJobTimeout(t *testing.T) {
	var opts serviceOpts
	WithIngestJobTimeout(60 * time.Second)(&opts)
	require.Equal(t, 60*time.Second, opts.ingestJobTimeout)
}

func TestResolveOptsFromEnv(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "env-key")
	t.Setenv(envTMemoryHost, "http://env-host")

	opts := serviceOpts{host: defaultHost, apiKey: ""}
	resolveOptsFromEnv(&opts)

	require.Equal(t, "env-key", opts.apiKey)
	require.Equal(t, "http://env-host", opts.host)
}

func TestResolveOptsFromEnv_NoOverwriteExplicit(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "env-key")
	t.Setenv(envTMemoryHost, "http://env-host")

	opts := serviceOpts{host: "http://custom-host", apiKey: "explicit-key"}
	resolveOptsFromEnv(&opts)

	require.Equal(t, "explicit-key", opts.apiKey)
	require.Equal(t, "http://custom-host", opts.host)
}

func TestDefaultRecallConfig(t *testing.T) {
	cfg := defaultRecallConfig()
	require.Len(t, cfg, 4)
	require.Contains(t, cfg, "raw")
	require.Contains(t, cfg, "episodic")
	require.Contains(t, cfg, "profile")
	require.Contains(t, cfg, "graph")
	require.IsType(t, VectorRecallConfig{}, cfg["raw"])
	require.IsType(t, GraphRecallConfig{}, cfg["graph"])
	require.IsType(t, ProfileRecallConfig{}, cfg["profile"])
}
