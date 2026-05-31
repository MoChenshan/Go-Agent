// Package sdk provides the option for the tRAG client.
package sdk

import (
	"errors"
	"net/http"

	"git.woa.com/trag/trag-sdk/go-trag"
	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// NewTRPCTRagClient creates a new tRAG client with TRPC transport.
// name is the name of the TRPC service.
func NewTRPCTRagClient(name string, opts ...trag.Option) *trag.TRag {
	httpHandler := ihttp.NewRequestHandler(name)
	httpClient := &http.Client{
		Transport: &trpcRoundTripper{handler: httpHandler},
	}
	opts = append(opts, trag.WithHttpCli(httpClient))
	client := trag.NewTRag(opts...)
	return client
}

type trpcRoundTripper struct {
	handler *ihttp.RequestHandler
}

func (t *trpcRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.handler.Do(req)
}

// Option is an option for the TRagOption.
type Option func(*TRagOption)

// TRagOption is the option for the tRAG client.
type TRagOption struct {
	Client         *trag.TRag
	RagCode        string
	NamespaceCode  string
	CollectionCode string
	EmbeddingModel string
	Policy         string
}

// CheckTRagOption checks the TRagOption.
func CheckTRagOption(opts *TRagOption) error {
	if opts == nil {
		return errors.New("tRAG option is nil")
	}
	if opts.Client == nil {
		return errors.New("tRAG client is nil")
	}
	if opts.RagCode == "" {
		return errors.New("instance code is empty")
	}
	if opts.NamespaceCode == "" {
		return errors.New("namespace code is empty")
	}
	if opts.CollectionCode == "" {
		return errors.New("collection code is empty")
	}
	if opts.Policy == "" {
		log.Info("tRAG policy code is empty, it may lead to source import failure")
	}
	if opts.EmbeddingModel == "" {
		log.Info("tRAG embedding model is empty")
	}

	return nil
}

// NewTRagOption creates a new TRagOption instance.
func NewTRagOption(opts ...Option) *TRagOption {
	tragOpts := &TRagOption{}
	for _, opt := range opts {
		opt(tragOpts)
	}
	return tragOpts
}

// WithClient sets the tRAG client for the TRagOption.
func WithClient(client *trag.TRag) Option {
	return func(k *TRagOption) {
		k.Client = client
	}
}

// WithInstanceCode sets the instance code for the TRagOption.
func WithInstanceCode(instanceCode string) Option {
	return func(k *TRagOption) {
		k.RagCode = instanceCode
	}
}

// WithNamespaceCode sets the namespace code for the TRagOption.
func WithNamespaceCode(namespaceCode string) Option {
	return func(k *TRagOption) {
		k.NamespaceCode = namespaceCode
	}
}

// WithCollectionCode sets the collection code for the TRagOption.
func WithCollectionCode(collectionCode string) Option {
	return func(k *TRagOption) {
		k.CollectionCode = collectionCode
	}
}

// WithEmbeddingModel sets the embedding model for the TRagOption.
func WithEmbeddingModel(embeddingModel string) Option {
	return func(k *TRagOption) {
		k.EmbeddingModel = embeddingModel
	}
}

// WithPolicyCode sets the policy for the TRagOption.
func WithPolicyCode(policy string) Option {
	return func(k *TRagOption) {
		k.Policy = policy
	}
}
