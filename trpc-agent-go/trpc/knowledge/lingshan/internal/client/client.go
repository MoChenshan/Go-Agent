// Package client provides the internal HTTP client for LingShan knowledge base.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"git.code.oa.com/trpc-go/trpc-go/client"
	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
)

// Option is the option for the Client instance.
type Option func(*options)

type options struct {
	url               string
	serviceName       string
	knowledgeBaseID   string
	headers           http.Header
	trpcClientOptions []client.Option
}

// WithURL sets the URL for the client.
func WithURL(url string) Option {
	return func(o *options) {
		o.url = url
	}
}

// WithServiceName sets the service name for the client.
func WithServiceName(name string) Option {
	return func(o *options) {
		o.serviceName = name
	}
}

// WithKnowledgeBaseID sets the knowledge base ID for the client.
func WithKnowledgeBaseID(id string) Option {
	return func(o *options) {
		o.knowledgeBaseID = id
	}
}

// WithHTTPHeaders sets the custom HTTP headers for the client.
func WithHTTPHeaders(headers http.Header) Option {
	return func(o *options) {
		o.headers = headers
	}
}

// WithTRPCClientOptions sets the tRPC client options for the client.
func WithTRPCClientOptions(opts ...client.Option) Option {
	return func(o *options) {
		o.trpcClientOptions = append(o.trpcClientOptions, opts...)
	}
}

// Client is the client for LingShan Knowledge Base.
type Client struct {
	opt        *options
	httpClient ihttp.HTTPClient
}

// New creates a new Client.
func New(opts ...Option) *Client {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	return &Client{
		opt:        o,
		httpClient: ihttp.NewRequestHandler(o.serviceName, o.trpcClientOptions...),
	}
}

// Search performs the search request.
func (c *Client) Search(ctx context.Context, req *RetrieveKnowledgeReq) (*RetrieveKnowledgeResp, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := strings.TrimSuffix(c.opt.url, "/") + retrieveKnowledgeEndpoint
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, vs := range c.opt.headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var retrieveResp RetrieveKnowledgeResp
	if err := json.Unmarshal(body, &retrieveResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &retrieveResp, nil
}
