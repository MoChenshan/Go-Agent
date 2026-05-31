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
	defaultDirSourceName = "tRAG Directory Source"
	typeTRAGDirectory    = "trag_directory"
)

var _ source.Source = (*DirectorySource)(nil)

// DirectorySource represents a directory source for tRAG that does NOT perform chunking.
// It reads all files from a directory and passes raw content to tRAG platform.
type DirectorySource struct {
	name          string
	metadata      map[string]any
	dirPath       string
	recursive     bool
	fileExtFilter []string
}

// DirOption is an option for the DirectorySource.
type DirOption func(*DirectorySource)

// WithDirSourceName sets the name of the directory source.
func WithDirSourceName(name string) DirOption {
	return func(s *DirectorySource) {
		s.name = name
	}
}

// WithDirMetadata sets metadata for the directory source.
func WithDirMetadata(metadata map[string]any) DirOption {
	return func(s *DirectorySource) {
		s.metadata = metadata
	}
}

// WithRecursive enables or disables recursive directory traversal.
func WithRecursive(recursive bool) DirOption {
	return func(s *DirectorySource) {
		s.recursive = recursive
	}
}

// WithFileExtFilter sets file extension filters (e.g., []string{".txt", ".md"}).
// If empty, all files are included.
func WithFileExtFilter(exts []string) DirOption {
	return func(s *DirectorySource) {
		s.fileExtFilter = exts
	}
}

// NewDirectorySource creates a new directory source for tRAG.
// Unlike standard directory sources, this does NOT perform chunking.
func NewDirectorySource(dirPath string, opts ...DirOption) *DirectorySource {
	s := &DirectorySource{
		name:      defaultDirSourceName,
		dirPath:   dirPath,
		metadata:  make(map[string]any),
		recursive: false,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ReadDocuments reads all files from the directory WITHOUT chunking.
func (s *DirectorySource) ReadDocuments(ctx context.Context) ([]*document.Document, error) {
	if s.dirPath == "" {
		return nil, fmt.Errorf("directory path is empty")
	}

	fileInfo, err := os.Stat(s.dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat directory: %w", err)
	}
	if !fileInfo.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", s.dirPath)
	}

	var filePaths []string
	if s.recursive {
		filePaths, err = s.collectFilesRecursive(s.dirPath)
	} else {
		filePaths, err = s.collectFilesFlat(s.dirPath)
	}
	if err != nil {
		return nil, err
	}

	var docs []*document.Document
	for _, filePath := range filePaths {
		if !s.shouldIncludeFile(filePath) {
			continue
		}

		doc, err := s.readSingleFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		docs = append(docs, doc)
	}

	return docs, nil
}

// collectFilesFlat collects files from directory (non-recursive).
func (s *DirectorySource) collectFilesFlat(dirPath string) ([]string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var filePaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filePaths = append(filePaths, filepath.Join(dirPath, entry.Name()))
	}

	return filePaths, nil
}

// collectFilesRecursive collects files from directory (recursive).
func (s *DirectorySource) collectFilesRecursive(dirPath string) ([]string, error) {
	var filePaths []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			filePaths = append(filePaths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}
	return filePaths, nil
}

// shouldIncludeFile checks if a file should be included based on extension filter.
func (s *DirectorySource) shouldIncludeFile(filePath string) bool {
	if len(s.fileExtFilter) == 0 {
		return true
	}

	ext := filepath.Ext(filePath)
	for _, allowedExt := range s.fileExtFilter {
		if ext == allowedExt {
			return true
		}
	}
	return false
}

// readSingleFile reads a single file and returns a document with raw content.
func (s *DirectorySource) readSingleFile(filePath string) (*document.Document, error) {
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
	metadata[source.MetaSource] = typeTRAGDirectory
	metadata[source.MetaFilePath] = filePath
	metadata[source.MetaFileName] = filepath.Base(filePath)
	metadata[source.MetaFileExt] = filepath.Ext(filePath)
	metadata[source.MetaFileSize] = fileInfo.Size()
	metadata[source.MetaModifiedAt] = fileInfo.ModTime().UTC()

	doc := &document.Document{
		ID:        GenerateDocumentID(typeTRAGDirectory),
		Name:      filepath.Base(filePath),
		Content:   string(content),
		Metadata:  metadata,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	return doc, nil
}

// Name returns the name of this source.
func (s *DirectorySource) Name() string {
	return s.name
}

// Type returns the type of this source.
func (s *DirectorySource) Type() string {
	return typeTRAGDirectory
}

// GetMetadata returns the metadata of this source.
func (s *DirectorySource) GetMetadata() map[string]any {
	return s.metadata
}
