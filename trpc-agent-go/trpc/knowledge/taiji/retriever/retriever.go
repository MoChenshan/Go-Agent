// Package retriever is a knowledge retriever that uses Taiji for semantic search.
package retriever

import (
	"context"
	"fmt"
	"time"

	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	client "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/taiji"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/sdk"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/query"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
)

// defaultMaxResults is the default maximum number of results to return.
const defaultMaxResults = 10

// Option is an option for the retriever.
type Option func(*Retriever)

// WithTaijiOption sets the Taiji option for the retriever.
func WithTaijiOption(taijiOption sdk.TaijiOption) Option {
	return func(r *Retriever) {
		r.taijiOption = &taijiOption
	}
}

// WithQueryEnhancer sets the query enhancer for the retriever.
func WithQueryEnhancer(queryEnhancer query.Enhancer) Option {
	return func(r *Retriever) {
		r.queryEnhancer = queryEnhancer
	}
}

// WithReRanker sets the re-ranker for the retriever.
func WithReRanker(reRanker reranker.Reranker) Option {
	return func(r *Retriever) {
		r.reranker = reRanker
	}
}

// WithMaxResults sets the maximum number of results to return.
func WithMaxResults(max int) Option {
	return func(r *Retriever) {
		if max <= 0 {
			max = defaultMaxResults
		}
		r.maxResults = max
	}
}

// Retriever is a knowledge retriever that uses Taiji for semantic search.
type Retriever struct {
	taijiOption   *sdk.TaijiOption
	client        *client.Client
	queryEnhancer query.Enhancer
	reranker      reranker.Reranker
	maxResults    int
}

// New creates a new retriever.
func New(opts ...Option) (*Retriever, error) {
	retriever := &Retriever{maxResults: defaultMaxResults}
	for _, opt := range opts {
		opt(retriever)
	}
	if retriever.taijiOption == nil {
		return nil, fmt.Errorf("taiji option is nil")
	}
	if err := sdk.CheckTaijiOption(retriever.taijiOption); err != nil {
		return nil, err
	}

	var httpClient ihttp.HTTPClient
	serviceName := retriever.taijiOption.ServiceName
	if retriever.taijiOption.ClientBuilder != nil {
		httpClient = retriever.taijiOption.ClientBuilder(sdk.WithHTTPClientName(serviceName))
	}
	internalTaijiOption := client.TaijiOption{
		URL:             retriever.taijiOption.URL,
		Token:           retriever.taijiOption.Token,
		ServiceName:     retriever.taijiOption.ServiceName,
		TaijiHYAPIURL:   retriever.taijiOption.TaijiHYAPIURL,
		TaijiHYAPIToken: retriever.taijiOption.TaijiHYAPIToken,
		KnowledgeOption: client.KnowledgeOption{
			EmbIndex: retriever.taijiOption.EmbIndex,
			WSID:     retriever.taijiOption.WSID,
		},
	}
	retriever.client = client.NewClient(client.WithTaijiOption(internalTaijiOption), client.WithHTTPClient(httpClient))
	return retriever, nil
}

// Retrieve finds the most relevant documents for a given query.
func (r *Retriever) Retrieve(ctx context.Context, queryReq *retriever.Query) (*retriever.Result, error) {
	finalQuery := queryReq.Text
	if r.queryEnhancer != nil {
		queryReq := &query.Request{
			Query: finalQuery,
		}
		enhanced, err := r.queryEnhancer.EnhanceQuery(ctx, queryReq)
		if err != nil {
			return nil, err
		}
		finalQuery = enhanced.Enhanced
	}

	searchResults, err := r.search(ctx, finalQuery, r.getMaxResults(queryReq.Limit), queryReq.MinScore)
	if err != nil {
		return nil, err
	}

	rerankerResults := make([]*reranker.Result, len(searchResults.Results))
	for i, doc := range searchResults.Results {
		rerankerResults[i] = &reranker.Result{
			Document: doc.Document,
			Score:    doc.Score,
		}
	}

	// Step 5: Rerank results (if reranker is available).
	if r.reranker != nil {
		rerankerQuery := &reranker.Query{
			Text:       queryReq.Text,
			FinalQuery: finalQuery,
		}
		rerankerResults, err = r.reranker.Rerank(ctx, rerankerQuery, rerankerResults)
		if err != nil {
			return nil, err
		}
	}

	// Step 6: Convert back to retriever format.
	finalResults := make([]*retriever.RelevantDocument, len(rerankerResults))
	for i, result := range rerankerResults {
		finalResults[i] = &retriever.RelevantDocument{
			Document: result.Document,
			Score:    result.Score,
		}
	}

	return &retriever.Result{
		Documents: finalResults,
	}, nil
}

// Close implements the Retriever interface.
func (r *Retriever) Close() error {
	return nil
}

func (r *Retriever) search(
	ctx context.Context,
	query string,
	limit int,
	minScore float64,
) (*vectorstore.SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query are empty")
	}

	// Use Taiji's search API
	searchReq := &client.SearchRequest{
		QueryID: fmt.Sprintf("trpc-agent-go-%v", time.Now().UnixMilli()),
		Text:    query,
		K:       limit,
	}

	searchResp, err := r.client.Search(ctx, searchReq)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if searchResp.RetCode != 0 {
		errorMsg := "unknown error"
		if searchResp.Error != nil {
			errorMsg = searchResp.Error.Message
		}
		return nil, fmt.Errorf("search failed: retcode %d, error: %s", searchResp.RetCode, errorMsg)
	}

	if len(searchResp.Results) == 0 {
		return &vectorstore.SearchResult{
			Results: []*vectorstore.ScoredDocument{},
		}, nil
	}

	scoredDocs := make([]*vectorstore.ScoredDocument, 0, len(searchResp.Results))
	for _, result := range searchResp.Results {
		score := 1 - result.Metric
		// Filter by minimum score if specified
		if minScore > 0 && score < minScore {
			continue
		}

		scoredDocs = append(scoredDocs, &vectorstore.ScoredDocument{
			Document: &document.Document{
				ID:       result.Index,
				Name:     result.Index,
				Content:  result.Value,
				Metadata: make(map[string]any),
			},
			Score: score,
		})
	}

	return &vectorstore.SearchResult{
		Results: scoredDocs,
	}, nil
}

func (r *Retriever) getMaxResults(maxResult int) int {
	if maxResult > 0 {
		return maxResult
	}
	return r.maxResults
}
