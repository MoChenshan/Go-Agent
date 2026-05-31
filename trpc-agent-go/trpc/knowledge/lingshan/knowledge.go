// Package lingshan provides the LingShan knowledge base implementation.
package lingshan

import (
	"context"
	"fmt"
	"net/http"

	"git.code.oa.com/trpc-go/trpc-go/client"
	internalretriever "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/lingshan/internal/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
)

// Option is an option for the Knowledge instance.
type Option func(*Knowledge)

var _ knowledge.Knowledge = (*Knowledge)(nil)

// Knowledge implements Knowledge and Retrieve interfaces.
type Knowledge struct {
	opt       *options
	retriever *internalretriever.Retriever
}

type options struct {
	url               string
	serviceName       string
	knowledgeBaseID   string
	headers           http.Header
	trpcClientOptions []client.Option
}

// WithURL sets the URL for Knowledge.
func WithURL(url string) Option {
	return func(k *Knowledge) {
		k.opt.url = url
	}
}

// WithKnowledgeBaseID sets the knowledge base ID for Knowledge.
func WithKnowledgeBaseID(id string) Option {
	return func(k *Knowledge) {
		k.opt.knowledgeBaseID = id
	}
}

// WithServiceName sets the service name for Knowledge.
func WithServiceName(serviceName string) Option {
	return func(k *Knowledge) {
		k.opt.serviceName = serviceName
	}
}

// WithHTTPHeaders sets the custom HTTP headers for Knowledge.
func WithHTTPHeaders(headers http.Header) Option {
	return func(k *Knowledge) {
		k.opt.headers = headers
	}
}

// WithTRPCClientOptions sets the tRPC client options for Knowledge.
func WithTRPCClientOptions(opts ...client.Option) Option {
	return func(k *Knowledge) {
		k.opt.trpcClientOptions = append(k.opt.trpcClientOptions, opts...)
	}
}

// New creates a new Knowledge instance.
func New(opts ...Option) *Knowledge {
	k := &Knowledge{
		opt: &options{},
	}
	for _, opt := range opts {
		opt(k)
	}

	retrieverOpts := []internalretriever.Option{
		internalretriever.WithURL(k.opt.url),
		internalretriever.WithServiceName(k.opt.serviceName),
		internalretriever.WithKnowledgeBaseID(k.opt.knowledgeBaseID),
		internalretriever.WithHTTPHeaders(k.opt.headers),
		internalretriever.WithTRPCClientOptions(k.opt.trpcClientOptions...),
	}
	k.retriever = internalretriever.New(retrieverOpts...)
	return k
}

// Search performs semantic search using LingShan Knowledge Base.
func (k *Knowledge) Search(ctx context.Context, req *knowledge.SearchRequest) (*knowledge.SearchResult, error) {
	queryReq := &retriever.Query{
		Text:     req.Query,
		Limit:    req.MaxResults,
		MinScore: req.MinScore,
	}

	// Helper to convert knowledge.SearchFilter to retriever.QueryFilter
	if req.SearchFilter != nil {
		queryReq.Filter = &retriever.QueryFilter{
			DocumentIDs:     req.SearchFilter.DocumentIDs,
			Metadata:        req.SearchFilter.Metadata,
			FilterCondition: req.SearchFilter.FilterCondition,
		}
	}

	result, err := k.retriever.Retrieve(ctx, queryReq)
	if err != nil {
		return nil, err
	}

	if len(result.Documents) == 0 {
		return nil, fmt.Errorf("no relevant documents found")
	}

	// Convert back to knowledge.SearchResult
	bestDoc := result.Documents[0]

	docs := make([]*knowledge.Result, len(result.Documents))
	for i, d := range result.Documents {
		docs[i] = &knowledge.Result{
			Document: d.Document,
			Score:    d.Score,
		}
	}

	return &knowledge.SearchResult{
		Document:  bestDoc.Document,
		Score:     bestDoc.Score,
		Text:      bestDoc.Document.Content,
		Documents: docs,
	}, nil
}

// Retrieve implements the Retriever interface.
func (k *Knowledge) Retrieve(ctx context.Context, queryReq *retriever.Query) (*retriever.Result, error) {
	return k.retriever.Retrieve(ctx, queryReq)
}

// Load is not implemented as per requirements.
func (k *Knowledge) Load(ctx context.Context, opts ...any) error {
	return fmt.Errorf("Load is not implemented")
}
