// Package iwiki provides the iWiki RAG knowledge base implementation.
package iwiki

import (
	"context"
	"fmt"
	"net/http"

	"git.code.oa.com/trpc-go/trpc-go/client"
	iclient "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/iwiki/internal/client"
	internalretriever "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/iwiki/internal/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
)

// Option is an option for the Knowledge instance.
type Option func(*Knowledge)

var _ knowledge.Knowledge = (*Knowledge)(nil)

// Knowledge implements Knowledge and Retrieve interfaces for iWiki RAG.
type Knowledge struct {
	opt       *options
	retriever *internalretriever.Retriever
}

type options struct {
	url               string
	paasID            string
	token             string
	serviceName       string
	headers           http.Header
	trpcClientOptions []client.Option
	searchConf        *iclient.SearchConf
	advancedParams    *iclient.AdvancedParams
}

// SearchConf re-exports the internal client SearchConf type.
type SearchConf = iclient.SearchConf

// DocObj re-exports the internal client DocObj type.
type DocObj = iclient.DocObj

// Topic re-exports the internal client Topic type.
type Topic = iclient.Topic

// AdvancedParams re-exports the internal client AdvancedParams type.
type AdvancedParams = iclient.AdvancedParams

// WithURL sets the base URL for Knowledge.
// e.g., "http://api-idc.sgw.woa.com/ebus/iwiki/prod" (IDC/DevCloud)
//
//	"http://api.sgw.woa.com/ebus/iwiki/prod" (Desktop/OA)
func WithURL(url string) Option {
	return func(k *Knowledge) {
		k.opt.url = url
	}
}

// WithPaasID sets the PaasID registered on TAI platform (tai.it.woa.com).
func WithPaasID(paasID string) Option {
	return func(k *Knowledge) {
		k.opt.paasID = paasID
	}
}

// WithToken sets the application token from TAI platform for Rio signature computation.
func WithToken(token string) Option {
	return func(k *Knowledge) {
		k.opt.token = token
	}
}

// WithServiceName sets the service name for Knowledge.
func WithServiceName(serviceName string) Option {
	return func(k *Knowledge) {
		k.opt.serviceName = serviceName
	}
}

// WithHTTPHeaders sets additional custom HTTP headers for Knowledge.
// e.g., x-tai-identity for identity passthrough.
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

// WithSearchConf sets the default search configuration for Knowledge.
func WithSearchConf(conf *SearchConf) Option {
	return func(k *Knowledge) {
		k.opt.searchConf = conf
	}
}

// WithAdvancedParams sets the default advanced parameters for Knowledge.
func WithAdvancedParams(params *AdvancedParams) Option {
	return func(k *Knowledge) {
		k.opt.advancedParams = params
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
		internalretriever.WithPaasID(k.opt.paasID),
		internalretriever.WithToken(k.opt.token),
		internalretriever.WithServiceName(k.opt.serviceName),
		internalretriever.WithHTTPHeaders(k.opt.headers),
		internalretriever.WithTRPCClientOptions(k.opt.trpcClientOptions...),
	}
	if k.opt.searchConf != nil {
		retrieverOpts = append(retrieverOpts, internalretriever.WithSearchConf(k.opt.searchConf))
	}
	if k.opt.advancedParams != nil {
		retrieverOpts = append(retrieverOpts, internalretriever.WithAdvancedParams(k.opt.advancedParams))
	}
	k.retriever = internalretriever.New(retrieverOpts...)
	return k
}

// Search performs semantic search using iWiki RAG knowledge base.
func (k *Knowledge) Search(ctx context.Context, req *knowledge.SearchRequest) (*knowledge.SearchResult, error) {
	queryReq := &retriever.Query{
		Text:      req.Query,
		Limit:     req.MaxResults,
		MinScore:  req.MinScore,
		SessionID: req.SessionID,
	}

	result, err := k.retriever.Retrieve(ctx, queryReq)
	if err != nil {
		return nil, err
	}

	if len(result.Documents) == 0 {
		return nil, fmt.Errorf("no relevant documents found")
	}

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

// Load is not implemented for iWiki RAG.
func (k *Knowledge) Load(ctx context.Context, opts ...any) error {
	return fmt.Errorf("Load is not implemented")
}
