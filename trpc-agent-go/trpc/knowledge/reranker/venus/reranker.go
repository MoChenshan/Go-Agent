// Package venus provides a Reranker implementation compatible with Venus reranker service.
package venus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"

	"git.code.oa.com/trpc-go/trpc-go/client"
	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"
	"trpc.group/trpc-go/trpc-agent-go/log"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

// Reranker implements Reranker using Venus reranker service.
type Reranker struct {
	endpoint         string
	apiKey           string
	modelName        string
	serviceName      string
	topN             int
	trpcClientOption []client.Option
	httpClient       ihttp.HTTPClient
}

// Option configures Reranker.
type Option func(*Reranker)

// WithAPIKey sets the API key.
func WithAPIKey(key string) Option {
	return func(r *Reranker) {
		r.apiKey = key
	}
}

// WithModel sets the model name.
func WithModel(model string) Option {
	return func(r *Reranker) {
		r.modelName = model
	}
}

// WithTopN sets the TopN.
func WithTopN(n int) Option {
	return func(r *Reranker) {
		r.topN = n
	}
}

// WithServiceName sets the service name for the HTTP client.
// You can specify Venus Host By WithServiceName which bound a client service with target in trpc_go.yaml
func WithServiceName(name string) Option {
	return func(r *Reranker) {
		r.serviceName = name
	}
}

// WithEndpoint sets the endpoint URL.
func WithEndpoint(endpoint string) Option {
	return func(r *Reranker) {
		r.endpoint = endpoint
	}
}

// WithTrpcClientOption sets the tRPC client options directly.
// This allows users to configure the HTTP client without relying on trpc_go.yaml.
// Example:
//
//	venus.WithTrpcClientOption(
//	    client.WithTarget("dns://venus-service:8000"),
//	    client.WithTimeout(30000),
//	)
func WithTrpcClientOption(opts ...client.Option) Option {
	return func(r *Reranker) {
		r.trpcClientOption = append(r.trpcClientOption, opts...)
	}
}

// New creates a new Venus reranker.
func New(opts ...Option) (*Reranker, error) {
	r := &Reranker{
		topN: -1,
	}
	for _, opt := range opts {
		opt(r)
	}
	r.httpClient = ihttp.NewRequestHandler(r.serviceName, r.trpcClientOption...)
	return r, nil
}

// rerankRequest represents the request payload for Venus reranker.
type rerankRequest struct {
	Model     string   `json:"model,omitempty"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

// rerankResponse represents the response from Venus reranker.
type rerankResponse struct {
	Object  string         `json:"object"`
	Results []rerankResult `json:"results"`
	Model   string         `json:"model"`
	Usage   struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	ID      string `json:"id"`
	Created int64  `json:"created"`
}

// rerankResult represents a single rerank result from Venus.
type rerankResult struct {
	RelevanceScore float64 `json:"relevance_score"`
	Index          int     `json:"index"`
}

// Rerank implements the Reranker interface.
func (r *Reranker) Rerank(
	ctx context.Context,
	query *reranker.Query,
	results []*reranker.Result,
) ([]*reranker.Result, error) {
	if len(results) == 0 {
		return results, nil
	}

	docs := make([]string, len(results))
	for i, res := range results {
		if res.Document != nil {
			docs[i] = res.Document.Content
		} else {
			log.WarnfContext(ctx, "venus reranker: result[%d].Document is nil", i)
		}
	}

	req := rerankRequest{
		Model:     r.modelName,
		Query:     query.FinalQuery,
		Documents: docs,
	}

	reranked, err := r.doRerank(ctx, req, results)
	if err != nil {
		return nil, err
	}

	if r.topN > 0 && len(reranked) > r.topN {
		reranked = reranked[:r.topN]
	}
	return reranked, nil
}

// doRerank performs the reranking request to Venus service.
func (r *Reranker) doRerank(
	ctx context.Context,
	reqPayload rerankRequest,
	originalResults []*reranker.Result,
) ([]*reranker.Result, error) {
	reqBody, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", r.endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Map scores back to results
	rerankedResults := make([]*reranker.Result, 0, len(apiResp.Results))
	for _, d := range apiResp.Results {
		if d.Index >= 0 && d.Index < len(originalResults) {
			originalRes := originalResults[d.Index]
			newRes := *originalRes
			newRes.Score = d.RelevanceScore
			rerankedResults = append(rerankedResults, &newRes)
		} else {
			log.Warnf("venus reranker: invalid index from response: %d", d.Index)
		}
	}

	// Sort by score descending
	sort.Slice(rerankedResults, func(i, j int) bool {
		return rerankedResults[i].Score > rerankedResults[j].Score
	})

	return rerankedResults, nil
}
