// Package retriever is a knowledge retriever that uses tRAG for semantic search.
package retriever

import (
	"context"
	"fmt"

	"git.woa.com/trag/trag-sdk/go-trag"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/query"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
)

// defaultMaxResults is the default maximum number of results to return.
const defaultMaxResults = 10

// Option is an option for the retriever.
type Option func(*Retriever)

// WithTRagOption sets the tRAG option for the retriever.
func WithTRagOption(tragOption sdk.TRagOption) Option {
	return func(r *Retriever) {
		r.tragOption = &tragOption
	}
}

// WithEmbedder sets the embedder for the retriever.
func WithEmbedder(embedder embedder.Embedder) Option {
	return func(r *Retriever) {
		r.embedder = embedder
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

// Retriever is a knowledge retriever that uses tRAG for semantic search.
type Retriever struct {
	tragOption    *sdk.TRagOption
	embedder      embedder.Embedder
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
	if err := sdk.CheckTRagOption(retriever.tragOption); err != nil {
		return nil, err
	}
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

	var embedding []float64
	var err error
	if r.embedder != nil {
		embedding, err = r.embedder.GetEmbedding(ctx, finalQuery)
		if err != nil {
			return nil, err
		}
	}

	filterExpr, err := buildFilterExpr(queryReq.Filter)
	if err != nil {
		return nil, fmt.Errorf("failed to build filter expression: %w", err)
	}

	searchResults, err := r.search(ctx, embedding, finalQuery, r.getMaxResults(queryReq.Limit), queryReq.MinScore, filterExpr)
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

func (r *Retriever) getMaxResults(maxResult int) int {
	if maxResult > 0 {
		return maxResult
	}
	return r.maxResults
}

// Close implements the Retriever interface.
func (r *Retriever) Close() error {
	return nil
}

func (r *Retriever) search(
	ctx context.Context,
	embedding []float64,
	query string,
	limit int,
	minScore float64,
	filterExpr string,
) (*vectorstore.SearchResult, error) {
	if len(embedding) == 0 && query == "" {
		return nil, fmt.Errorf("embedding and query are both empty")
	}

	searchDocReq := &trag.SearchDocumentRequest{
		RagCode:        r.tragOption.RagCode,
		NamespaceCode:  r.tragOption.NamespaceCode,
		CollectionCode: r.tragOption.CollectionCode,
		Limit:          int32(limit),
		FilterExpr:     filterExpr,
	}

	// if embedding offer, use embedding, else use query and set embedding model
	if len(embedding) > 0 {
		searchDocReq.Vector = embedding
	} else if query != "" {
		searchDocReq.Doc = query
		searchDocReq.EmbeddingModel = r.tragOption.EmbeddingModel
	}

	searchDocResp, err := r.tragOption.Client.SearchDocumentRequest(ctx, searchDocReq)
	if err != nil {
		return nil, fmt.Errorf("search document failed: %w", err)
	}

	if searchDocResp.Code != 0 {
		return nil, fmt.Errorf("search document failed: %s, trace: %s, code: %d",
			searchDocResp.Message, searchDocResp.TraceID, searchDocResp.Code)
	}
	if len(searchDocResp.Data) == 0 {
		return &vectorstore.SearchResult{
			Results: []*vectorstore.ScoredDocument{},
		}, nil
	}

	var scoreDocs []*vectorstore.ScoredDocument
	for _, doc := range searchDocResp.Data {
		// Filter by minimum score if specified
		if minScore > 0 && doc.Score < minScore {
			continue
		}

		scoredDoc := &vectorstore.ScoredDocument{
			Document: &document.Document{
				ID:       doc.ID,
				Name:     doc.ID,
				Content:  doc.Doc,
				Metadata: make(map[string]any),
			},
			Score: doc.Score,
		}
		for k, v := range doc.DocKeyValue {
			scoredDoc.Document.Metadata[k] = v
		}
		if len(doc.DocFields) > 0 {
			for _, v := range doc.DocFields {
				scoredDoc.Document.Metadata[v.Name] = v.Value
			}
		}
		scoreDocs = append(scoreDocs, scoredDoc)
	}
	return &vectorstore.SearchResult{
		Results: scoreDocs,
	}, nil
}
