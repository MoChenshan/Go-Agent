// Package source provides the source for tRAG.
package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

const (
	defaultFileSourceName = "tRAG File Source"
	typeTRAGFile          = "trag_file"
)

var _ source.Source = (*FileSource)(nil)

// FileSource represents a file source for tRAG that does NOT perform chunking.
// It passes raw file content directly to tRAG platform for server-side processing.
// This avoids double-chunking (client-side + server-side).
type FileSource struct {
	name      string
	metadata  map[string]any
	filePaths []string
}

// FileOption is an option for the FileSource.
type FileOption func(*FileSource)

// WithFileSourceName sets the name of the file source.
func WithFileSourceName(name string) FileOption {
	return func(s *FileSource) {
		s.name = name
	}
}

// WithFileMetadata sets metadata for the file source.
func WithFileMetadata(metadata map[string]any) FileOption {
	return func(s *FileSource) {
		s.metadata = metadata
	}
}

// NewFileSource creates a new file source for tRAG.
// Unlike standard file sources, this does NOT perform chunking.
// Files are read as whole documents and sent to tRAG platform for processing.
func NewFileSource(filePaths []string, opts ...FileOption) *FileSource {
	s := &FileSource{
		name:      defaultFileSourceName,
		filePaths: filePaths,
		metadata:  make(map[string]any),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ReadDocuments reads all files and returns documents WITHOUT chunking.
// Each file becomes a single document with its raw content.
// tRAG platform will handle chunking on the server side.
func (s *FileSource) ReadDocuments(ctx context.Context) ([]*document.Document, error) {
	if len(s.filePaths) == 0 {
		return nil, nil
	}

	var docs []*document.Document
	for _, filePath := range s.filePaths {
		doc, err := s.readSingleFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		docs = append(docs, doc)
	}

	return docs, nil
}

// readSingleFile reads a single file and returns a document with raw content.
func (s *FileSource) readSingleFile(filePath string) (*document.Document, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	metadata := make(map[string]any)
	for k, v := range s.metadata {
		metadata[k] = v
	}
	metadata[source.MetaSource] = typeTRAGFile
	metadata[source.MetaFilePath] = filePath
	metadata[source.MetaFileName] = filepath.Base(filePath)
	metadata[source.MetaFileExt] = filepath.Ext(filePath)
	metadata[source.MetaFileSize] = fileInfo.Size()
	metadata[source.MetaModifiedAt] = fileInfo.ModTime().UTC()

	doc := &document.Document{
		ID:        GenerateDocumentID(typeTRAGFile),
		Name:      filepath.Base(filePath),
		Content:   string(content),
		Metadata:  metadata,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	return doc, nil
}

// Name returns the name of this source.
func (s *FileSource) Name() string {
	return s.name
}

// Type returns the type of this source.
func (s *FileSource) Type() string {
	return typeTRAGFile
}

// GetMetadata returns the metadata of this source.
func (s *FileSource) GetMetadata() map[string]any {
	return s.metadata
}
