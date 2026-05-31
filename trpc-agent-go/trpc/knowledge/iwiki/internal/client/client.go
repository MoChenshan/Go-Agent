// Package client provides the internal HTTP client for iWiki RAG knowledge base.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go/client"
	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
)

// Option is the option for the Client instance.
type Option func(*options)

type options struct {
	url               string
	paasID            string
	token             string
	serviceName       string
	headers           http.Header
	trpcClientOptions []client.Option
}

// WithURL sets the base URL for the client.
// e.g., "http://api-idc.sgw.woa.com/ebus/iwiki/prod"
func WithURL(url string) Option {
	return func(o *options) {
		o.url = url
	}
}

// WithPaasID sets the PaasID for Rio authentication.
func WithPaasID(paasID string) Option {
	return func(o *options) {
		o.paasID = paasID
	}
}

// WithToken sets the token for Rio signature computation.
func WithToken(token string) Option {
	return func(o *options) {
		o.token = token
	}
}

// WithServiceName sets the service name for the client.
func WithServiceName(name string) Option {
	return func(o *options) {
		o.serviceName = name
	}
}

// WithHTTPHeaders sets additional custom HTTP headers for the client.
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

// Client is the HTTP client for iWiki RAG knowledge base.
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

// rioSign computes the Rio signature: SHA256(timestamp + token + nonce + timestamp) uppercased.
func rioSign(timestamp, token, nonce string) string {
	signStr := timestamp + token + nonce + timestamp
	return fmt.Sprintf("%X", sha256.Sum256([]byte(signStr)))
}

// Search performs the iWiki RAG search request.
func (c *Client) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := strings.TrimSuffix(c.opt.url, "/") + apiPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Set Rio authentication headers.
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := strconv.Itoa(rand.Intn(1000000))
	signature := rioSign(timestamp, c.opt.token, nonce)

	httpReq.Header.Set("X-Rio-Paasid", c.opt.paasID)
	httpReq.Header.Set("X-Rio-Timestamp", timestamp)
	httpReq.Header.Set("X-Rio-Nonce", nonce)
	httpReq.Header.Set("X-Rio-Signature", signature)

	// Set additional custom headers.
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

	var searchResp SearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check for API gateway (AGW) errors first.
	if searchResp.ErrCode != "" {
		return nil, fmt.Errorf("iwiki gateway error: errcode=%s, errmsg=%s", searchResp.ErrCode, searchResp.ErrMsg)
	}

	if searchResp.Code != "Ok" {
		return nil, fmt.Errorf("iwiki search error: code=%s, msg=%s", searchResp.Code, searchResp.Msg)
	}

	// Report per-resource errors (e.g. permission issues) when no data is returned.
	if len(searchResp.Data) == 0 && len(searchResp.ErrorIDs) > 0 {
		var msgs []string
		for _, e := range searchResp.ErrorIDs {
			msgs = append(msgs, fmt.Sprintf("[%s:%d] %s", e.Type, e.ID, e.Message))
		}
		return nil, fmt.Errorf("iwiki search returned no data due to errors: %s",
			strings.Join(msgs, "; "))
	}

	return &searchResp, nil
}
