// Package embedder is a knowledge embedder that uses tRAG for semantic search.
package embedder

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"git.woa.com/trag/trag-sdk/go-trag"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
)

// Option is the option for tRAG embedder
type Option func(*Embedder)

// WithTRagOption sets the tRAG option for the embedder.
func WithTRagOption(tragOption sdk.TRagOption) Option {
	return func(e *Embedder) {
		e.tragOption = tragOption
	}
}

// Embedder is an embedder that uses TRAG to generate embeddings for text
type Embedder struct {
	tragOption sdk.TRagOption
	dimensions atomic.Int32
}

// New creates a new Embedder instance.
func New(opts ...Option) (*Embedder, error) {
	e := &Embedder{}
	for _, opt := range opts {
		opt(e)
	}
	if err := sdk.CheckTRagOption(&e.tragOption); err != nil {
		return nil, err
	}
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
	if e.tragOption.Client == nil {
		return nil, errors.New("tRAG client is nil")
	}
	embeddingReq := &trag.EmbeddingsRequest{
		RagCode:       e.tragOption.RagCode,
		NamespaceCode: e.tragOption.NamespaceCode,
		Model:         e.tragOption.EmbeddingModel,
		Input:         []string{text},
	}
	resp, err := e.tragOption.Client.Embeddings(ctx, embeddingReq)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("embedding response is nil")
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("embedding failed: %s, trace: %s", resp.Message, resp.TraceID)
	}
	if len(resp.Data.EmbeddingList) == 0 {
		return nil, fmt.Errorf("embedding failed: %s, trace: %s", resp.Message, resp.TraceID)
	}
	return resp.Data.EmbeddingList[0], nil
}
