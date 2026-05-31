// Package retriever implements the knowledge retriever interface for iWiki RAG.
package retriever

import (
	"context"
	"net/http"

	"git.code.oa.com/trpc-go/trpc-go/client"
	iclient "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/iwiki/internal/client"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
)

// Retriever implements the retriever.Retriever interface for iWiki RAG.
type Retriever struct {
	client         *iclient.Client
	searchConf     *iclient.SearchConf
	advancedParams *iclient.AdvancedParams
}

// Option is the option for the Retriever instance.
type Option func(*options)

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

// WithURL sets the base URL for the retriever.
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

// WithServiceName sets the service name for the retriever.
func WithServiceName(name string) Option {
	return func(o *options) {
		o.serviceName = name
	}
}

// WithHTTPHeaders sets additional custom HTTP headers for the retriever.
func WithHTTPHeaders(headers http.Header) Option {
	return func(o *options) {
		o.headers = headers
	}
}

// WithTRPCClientOptions sets the tRPC client options for the retriever.
func WithTRPCClientOptions(opts ...client.Option) Option {
	return func(o *options) {
		o.trpcClientOptions = append(o.trpcClientOptions, opts...)
	}
}

// WithSearchConf sets the default search configuration for the retriever.
func WithSearchConf(conf *iclient.SearchConf) Option {
	return func(o *options) {
		o.searchConf = conf
	}
}

// WithAdvancedParams sets the default advanced parameters for the retriever.
func WithAdvancedParams(params *iclient.AdvancedParams) Option {
	return func(o *options) {
		o.advancedParams = params
	}
}

// New creates a new Retriever instance.
func New(opts ...Option) *Retriever {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	clientOpts := []iclient.Option{
		iclient.WithURL(o.url),
		iclient.WithPaasID(o.paasID),
		iclient.WithToken(o.token),
		iclient.WithServiceName(o.serviceName),
		iclient.WithHTTPHeaders(o.headers),
		iclient.WithTRPCClientOptions(o.trpcClientOptions...),
	}

	return &Retriever{
		client:         iclient.New(clientOpts...),
		searchConf:     o.searchConf,
		advancedParams: o.advancedParams,
	}
}

// Retrieve finds the most relevant documents from iWiki RAG.
func (r *Retriever) Retrieve(ctx context.Context, queryReq *retriever.Query) (*retriever.Result, error) {
	searchConf := r.searchConf
	if searchConf == nil {
		searchConf = &iclient.SearchConf{}
	}

	req := &iclient.SearchRequest{
		Query:          queryReq.Text,
		TopK:           queryReq.Limit,
		SearchConf:     searchConf,
		AdvancedParams: r.advancedParams,
	}

	if queryReq.SessionID != "" {
		req.SearchID = queryReq.SessionID
	}

	searchResp, err := r.client.Search(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(searchResp.Data) == 0 {
		return &retriever.Result{}, nil
	}

	relevantDocs := make([]*retriever.RelevantDocument, len(searchResp.Data))
	for i, chunk := range searchResp.Data {
		metadata := map[string]any{
			"title":         chunk.Title,
			"url":           chunk.URL,
			"source":        chunk.Source,
			"file_type":     chunk.FileType,
			"creator":       chunk.Creator,
			"last_modifier": chunk.LastModifier,
			"create_time":   chunk.CreateTime,
			"update_time":   chunk.UpdateTime,
		}

		// The API returns results in descending order of relevance.
		// Use inverse index as a synthetic score (higher is better).
		score := float64(len(searchResp.Data)-i) / float64(len(searchResp.Data))

		relevantDocs[i] = &retriever.RelevantDocument{
			Document: &document.Document{
				ID:       chunk.ID,
				Name:     chunk.Title,
				Content:  chunk.Content,
				Metadata: metadata,
			},
			Score: score,
		}
	}

	return &retriever.Result{
		Documents: relevantDocs,
	}, nil
}

// Close implements Retriever interface.
func (r *Retriever) Close() error {
	return nil
}
