// Package source provides the source for tRAG.
package source

import (
	"context"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

const (
	defaultTextSourceName = "tRAG Text Source"
	typeTRAGText          = "trag_text"
)

// GenerateDocumentID generates a unique ID for a document.
func GenerateDocumentID(name string) string {
	// Simple ID generation based on name and timestamp.
	return strings.ReplaceAll(name, " ", "_") + "_" + time.Now().Format("20060102150405")
}

var _ source.Source = (*TextSource)(nil)

// TextSource represents a text source for tRAG that does NOT perform chunking.
// It accepts raw text content and passes it directly to tRAG platform.
// This is useful for programmatically created content or in-memory text.
type TextSource struct {
	name     string
	metadata map[string]any
	texts    []TextContent
}

// TextContent represents a single text document.
type TextContent struct {
	ID      string
	Name    string
	Content string
}

// TextOption is an option for the TextSource.
type TextOption func(*TextSource)

// WithTextSourceName sets the name of the text source.
func WithTextSourceName(name string) TextOption {
	return func(s *TextSource) {
		s.name = name
	}
}

// WithTextMetadata sets metadata for the text source.
func WithTextMetadata(metadata map[string]any) TextOption {
	return func(s *TextSource) {
		s.metadata = metadata
	}
}

// NewTextSource creates a new text source for tRAG.
// Unlike standard text sources, this does NOT perform chunking.
// Text content is sent directly to tRAG platform for server-side processing.
//
// Example:
//
//	texts := []TextContent{
//	    {ID: "doc1", Name: "Document 1", Content: "Long text content..."},
//	    {ID: "doc2", Name: "Document 2", Content: "Another long text..."},
//	}
//	source := NewTextSource(texts)
func NewTextSource(texts []TextContent, opts ...TextOption) *TextSource {
	s := &TextSource{
		name:     defaultTextSourceName,
		texts:    texts,
		metadata: make(map[string]any),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ReadDocuments returns documents from text content WITHOUT chunking.
// Each TextContent becomes a single document with its raw content.
// tRAG platform will handle chunking on the server side.
func (s *TextSource) ReadDocuments(ctx context.Context) ([]*document.Document, error) {
	if len(s.texts) == 0 {
		return nil, nil
	}

	var docs []*document.Document
	for _, text := range s.texts {
		doc := s.createDocument(text)
		docs = append(docs, doc)
	}

	return docs, nil
}

// createDocument creates a document from TextContent.
func (s *TextSource) createDocument(text TextContent) *document.Document {
	metadata := make(map[string]any)
	for k, v := range s.metadata {
		metadata[k] = v
	}
	metadata[source.MetaSource] = typeTRAGText
	metadata[source.MetaSourceName] = s.name

	name := text.Name
	if name == "" {
		name = text.ID
	}

	id := text.ID
	if id == "" {
		id = GenerateDocumentID(typeTRAGText)
	}

	return &document.Document{
		ID:        id,
		Name:      name,
		Content:   text.Content,
		Metadata:  metadata,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

// Name returns the name of this source.
func (s *TextSource) Name() string {
	return s.name
}

// Type returns the type of this source.
func (s *TextSource) Type() string {
	return typeTRAGText
}

// GetMetadata returns the metadata of this source.
func (s *TextSource) GetMetadata() map[string]any {
	return s.metadata
}
