// Package embedder is a knowledge embedder that uses taiji platform for semantic search.
package embedder

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	client "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/taiji"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/sdk"
)

// Option is the option for tRAG embedder
type Option func(*Embedder)

// WithTaijiOption sets the token and wsid for taiji platform
func WithTaijiOption(opt sdk.TaijiOption) Option {
	return func(e *Embedder) {
		e.taijiOpt = opt
	}
}

// WithEmbeddingModel sets the embedding model for taiji platform
func WithEmbeddingModel(model string) Option {
	return func(e *Embedder) {
		e.embeddingModel = model
	}
}

// Embedder is an embedder that uses TRAG to generate embeddings for text
type Embedder struct {
	taijiOpt       sdk.TaijiOption
	embeddingModel string
	dimensions     atomic.Int32
	client         *client.Client
}

// New creates a new Embedder instance.
func New(opts ...Option) (*Embedder, error) {
	e := &Embedder{}
	for _, opt := range opts {
		opt(e)
	}
	var httpClient ihttp.HTTPClient
	serviceName := e.taijiOpt.ServiceName
	if e.taijiOpt.ClientBuilder != nil {
		httpClient = e.taijiOpt.ClientBuilder(sdk.WithHTTPClientName(serviceName))
	}
	internalTaijiOption := client.TaijiOption{
		URL:             e.taijiOpt.URL,
		Token:           e.taijiOpt.Token,
		ServiceName:     e.taijiOpt.ServiceName,
		TaijiHYAPIURL:   e.taijiOpt.TaijiHYAPIURL,
		TaijiHYAPIToken: e.taijiOpt.TaijiHYAPIToken,
		KnowledgeOption: client.KnowledgeOption{
			EmbIndex: e.taijiOpt.EmbIndex,
			WSID:     e.taijiOpt.WSID,
		},
	}
	e.client = client.NewClient(client.WithTaijiOption(internalTaijiOption), client.WithHTTPClient(httpClient))
	return e, nil
}

// GetEmbedding generates an embedding vector for the given text
func (e *Embedder) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	embedding, err := e.getEmbedding(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding: %w", err)
	}
	return embedding, nil
}

// GetEmbeddingWithUsage generates an embedding vector for the given text,
// Returns the embedding vector and nil usage info
func (e *Embedder) GetEmbeddingWithUsage(ctx context.Context, text string) ([]float64, map[string]any, error) {
	embedding, err := e.getEmbedding(ctx, text)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get embedding with usage: %w", err)
	}
	return embedding, nil, nil
}

// GetDimensions returns the dimensionality of the embeddings produced by this embedder.
func (e *Embedder) GetDimensions() int {
	if e.dimensions.Load() == 0 {
		testText := "0"
		embedding, err := e.getEmbedding(context.Background(), testText)
		if err != nil {
			return 0
		}
		e.dimensions.Store(int32(len(embedding)))
	}
	return int(e.dimensions.Load())
}

func (e *Embedder) getEmbedding(ctx context.Context, text string) ([]float64, error) {
	if e.client == nil {
		return nil, errors.New("taiji client not initialized")
	}
	queryID := fmt.Sprintf("%s-%d", "trpc-agent-go", time.Now().UnixNano())
	embeddingReq := &client.EmbeddingRequest{
		Input:   text,
		QueryID: queryID,
		Model:   e.embeddingModel,
	}
	resp, err := e.client.CreateEmbedding(ctx, embeddingReq)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("embedding response is nil")
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embedding failed,  queryID: %s", queryID)
	}
	return resp.Data[0].Embedding, nil
}
