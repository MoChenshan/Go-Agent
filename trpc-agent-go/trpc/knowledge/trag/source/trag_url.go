// Package source provides the source for tRAG.
package source

import (
	"context"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

const (
	defaultURLSourceName = "tRAG URL Source"

	// TypeTRAGURL represents the type of tRAG URL source.
	TypeTRAGURL = "trag_url"
)

var _ source.Source = (*URLSource)(nil)

// URLSource represents a url source for tRAG.
// It only supports for tRAG URL source.
type URLSource struct {
	name     string
	metadata map[string]any
	urls     []string
}

// URLOption represents a functional option for configuring Source.
type URLOption func(*URLSource)

// WithURLName sets a custom name for the URL source.
func WithURLName(name string) URLOption {
	return func(s *URLSource) {
		s.name = name
	}
}

// WithURLMetadata sets additional metadata for the source.
func WithURLMetadata(metadata map[string]any) URLOption {
	return func(s *URLSource) {
		s.metadata = metadata
	}
}

// WithURLMetadataValue adds a single metadata key-value pair.
func WithURLMetadataValue(key string, value any) URLOption {
	return func(s *URLSource) {
		if s.metadata == nil {
			s.metadata = make(map[string]any)
		}
		s.metadata[key] = value
	}
}

// NewURLSource creates a new url source for tRAG.
// It only supports for tRAG .
func NewURLSource(urls []string, opts ...URLOption) *URLSource {
	s := &URLSource{
		name:     defaultURLSourceName,
		urls:     urls,
		metadata: make(map[string]any),
	}

	// Apply options first to capture chunk configuration.
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ReadDocuments reads all files and returns documents using appropriate readers.
func (s *URLSource) ReadDocuments(ctx context.Context) ([]*document.Document, error) {
	var docs []*document.Document
	for _, url := range s.urls {
		doc := &document.Document{
			ID:        GenerateDocumentID(TypeTRAGURL),
			Name:      fmt.Sprintf("%s-%s", s.name, url),
			Content:   url,
			Metadata:  s.metadata,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// Name returns the name of this source.
func (s *URLSource) Name() string {
	return s.name
}

// Type returns the type of this source.
func (s *URLSource) Type() string {
	return TypeTRAGURL
}

// GetMetadata returns the metadata of this source.
func (s *URLSource) GetMetadata() map[string]any {
	return s.metadata
}
