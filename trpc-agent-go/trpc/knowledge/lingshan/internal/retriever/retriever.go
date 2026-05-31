// Package retriever implements the knowledge retriever interface for LingShan.
package retriever

import (
	"context"
	"net/http"

	"git.code.oa.com/trpc-go/trpc-go/client"
	iclient "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/lingshan/internal/client"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
)

// Retriever implements the knowledge.Retriever interface for LingShan knowledge base.
type Retriever struct {
	client          *iclient.Client
	knowledgeBaseID string
}

// Option is the option for the Retriever instance.
type Option func(*options)

type options struct {
	url               string
	serviceName       string
	knowledgeBaseID   string
	headers           http.Header
	trpcClientOptions []client.Option
}

// WithURL sets the URL for the retriever.
func WithURL(url string) Option {
	return func(o *options) {
		o.url = url
	}
}

// WithServiceName sets the service name for the retriever.
func WithServiceName(name string) Option {
	return func(o *options) {
		o.serviceName = name
	}
}

// WithKnowledgeBaseID sets the knowledge base ID for the retriever.
func WithKnowledgeBaseID(id string) Option {
	return func(o *options) {
		o.knowledgeBaseID = id
	}
}

// WithHTTPHeaders sets the custom HTTP headers for the retriever.
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

// New creates a new Retriever instance.
func New(opts ...Option) *Retriever {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	clientOpts := []iclient.Option{
		iclient.WithURL(o.url),
		iclient.WithServiceName(o.serviceName),
		iclient.WithKnowledgeBaseID(o.knowledgeBaseID),
		iclient.WithHTTPHeaders(o.headers),
		iclient.WithTRPCClientOptions(o.trpcClientOptions...),
	}

	return &Retriever{
		client:          iclient.New(clientOpts...),
		knowledgeBaseID: o.knowledgeBaseID,
	}
}

// Retrieve finds most relevant documents.
func (r *Retriever) Retrieve(ctx context.Context, queryReq *retriever.Query) (*retriever.Result, error) {
	pbReq := &iclient.RetrieveKnowledgeReq{
		KnowledgeBaseID: r.knowledgeBaseID,
		Query:           queryReq.Text,
		TopK:            int32(queryReq.Limit),
		ScoreThreshold:  float32(queryReq.MinScore),
	}

	if queryReq.Filter != nil {
		pbReq.Filter = r.convertFilter(queryReq.Filter)
	}

	retrieveResp, err := r.client.Search(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	if len(retrieveResp.Data.Results) == 0 {
		return &retriever.Result{}, nil
	}

	relevantDocs := make([]*retriever.RelevantDocument, len(retrieveResp.Data.Results))
	for i, res := range retrieveResp.Data.Results {
		relevantDocs[i] = &retriever.RelevantDocument{
			Document: &document.Document{
				Content:  res.Chunk.Content,
				Metadata: res.Metadata,
			},
			Score: float64(res.Score),
		}
	}

	return &retriever.Result{
		Documents: relevantDocs,
	}, nil
}

func (r *Retriever) convertFilter(filter *retriever.QueryFilter) *iclient.FilterCondition {
	if filter == nil {
		return nil
	}

	// Handle UniversalFilterCondition
	if filter.FilterCondition != nil {
		converter := &ConditionConverter{}
		if converted, _ := converter.Convert(filter.FilterCondition); converted != nil {
			return converted
		}
	}

	// Construct from Metadata (AND condition of equality checks)
	if len(filter.Metadata) > 0 {
		var conditions []*iclient.FilterCondition
		for k, v := range filter.Metadata {
			conditions = append(conditions, &iclient.FilterCondition{
				Field:    k,
				Operator: iclient.FilterOperatorEQ,
				Value:    v,
			})
		}
		if len(conditions) == 1 {
			return conditions[0]
		}
		return &iclient.FilterCondition{
			Operator:   iclient.FilterOperatorAND,
			Conditions: conditions,
		}
	}
	return nil
}

// Close implements Retriever interface.
func (r *Retriever) Close() error {
	return nil
}
