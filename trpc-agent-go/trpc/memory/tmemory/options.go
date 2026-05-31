package tmemory

import (
	"net/http"
	"os"
	"time"
)

const (
	envTMemoryAPIKey = "TMEMORY_API_KEY"
	envTMemoryHost   = "TMEMORY_HOST"

	defaultHost             = "http://test-tmemory.woa.com"
	defaultTimeout          = 10 * time.Second
	defaultStrategyID       = "1"
	defaultSource           = "trpc-agent-go"
	defaultAsyncIngestNum   = 1
	defaultIngestQueueSize  = 10
	defaultIngestJobTimeout = 30 * time.Second
)

type serviceOpts struct {
	host       string
	apiKey     string
	bizID      string
	strategyID string
	source     string

	timeout time.Duration
	client  *http.Client

	recallConfig map[string]any

	asyncIngestNum   int
	ingestQueueSize  int
	ingestJobTimeout time.Duration
}

func (o serviceOpts) clone() serviceOpts {
	if o.recallConfig != nil {
		cloned := make(map[string]any, len(o.recallConfig))
		for k, v := range o.recallConfig {
			cloned[k] = v
		}
		o.recallConfig = cloned
	}
	return o
}

var defaultOptions = serviceOpts{
	host:             defaultHost,
	strategyID:       defaultStrategyID,
	source:           defaultSource,
	timeout:          defaultTimeout,
	asyncIngestNum:   defaultAsyncIngestNum,
	ingestQueueSize:  defaultIngestQueueSize,
	ingestJobTimeout: defaultIngestJobTimeout,
	recallConfig:     defaultRecallConfig(),
}

func defaultRecallConfig() map[string]any {
	return map[string]any{
		"raw": VectorRecallConfig{
			MemoryType: "vector",
			TopK:       3,
			Threshold:  0.5,
		},
		"episodic": VectorRecallConfig{
			MemoryType: "vector",
			TopK:       3,
			Threshold:  0.5,
		},
		"profile": ProfileRecallConfig{
			MemoryType: "profile",
		},
		"graph": GraphRecallConfig{
			MemoryType: "graph",
			TopK:       2,
			Depth:      2,
			Threshold:  0.7,
		},
	}
}

// ServiceOpt configures a tmemory service.
type ServiceOpt func(*serviceOpts)

// WithHost sets the tMemory server host.
func WithHost(host string) ServiceOpt {
	return func(opts *serviceOpts) {
		if host != "" {
			opts.host = host
		}
	}
}

// WithAPIKey sets the tMemory API key (Bearer token).
func WithAPIKey(apiKey string) ServiceOpt {
	return func(opts *serviceOpts) {
		if apiKey != "" {
			opts.apiKey = apiKey
		}
	}
}

// WithBizID sets the business ID for tMemory requests.
func WithBizID(bizID string) ServiceOpt {
	return func(opts *serviceOpts) {
		if bizID != "" {
			opts.bizID = bizID
		}
	}
}

// WithStrategyID sets the strategy ID for ingest and recall requests.
func WithStrategyID(strategyID string) ServiceOpt {
	return func(opts *serviceOpts) {
		if strategyID != "" {
			opts.strategyID = strategyID
		}
	}
}

// WithSource sets the source identifier for ingest metadata.
func WithSource(source string) ServiceOpt {
	return func(opts *serviceOpts) {
		if source != "" {
			opts.source = source
		}
	}
}

// WithTimeout sets the HTTP timeout for tMemory requests.
func WithTimeout(timeout time.Duration) ServiceOpt {
	return func(opts *serviceOpts) {
		if timeout > 0 {
			opts.timeout = timeout
		}
	}
}

// WithHTTPClient injects a custom HTTP client.
func WithHTTPClient(c *http.Client) ServiceOpt {
	return func(opts *serviceOpts) {
		if c != nil {
			opts.client = c
		}
	}
}

// WithRecallConfig sets the recall configuration for each memory type.
// Keys are memory names (e.g. "raw", "episodic", "profile", "graph"),
// values should be VectorRecallConfig, GraphRecallConfig, or ProfileRecallConfig.
func WithRecallConfig(config map[string]any) ServiceOpt {
	return func(opts *serviceOpts) {
		if config != nil {
			opts.recallConfig = config
		}
	}
}

// WithAsyncIngestNum sets the number of async ingest workers.
func WithAsyncIngestNum(num int) ServiceOpt {
	return func(opts *serviceOpts) {
		if num > 0 {
			opts.asyncIngestNum = num
		}
	}
}

// WithIngestQueueSize sets the queue size for async ingest jobs.
func WithIngestQueueSize(size int) ServiceOpt {
	return func(opts *serviceOpts) {
		if size > 0 {
			opts.ingestQueueSize = size
		}
	}
}

// WithIngestJobTimeout sets the timeout for each ingest job.
func WithIngestJobTimeout(timeout time.Duration) ServiceOpt {
	return func(opts *serviceOpts) {
		if timeout > 0 {
			opts.ingestJobTimeout = timeout
		}
	}
}

func resolveOptsFromEnv(opts *serviceOpts) {
	if opts.host == defaultHost {
		if h := os.Getenv(envTMemoryHost); h != "" {
			opts.host = h
		}
	}
	if opts.apiKey == "" {
		opts.apiKey = os.Getenv(envTMemoryAPIKey)
	}
}
