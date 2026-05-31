package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kbsource "trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

func TestFileSource_ReadDocuments(t *testing.T) {
	tempDir := t.TempDir()

	testContent := "This is a test document with more than 1024 characters. " +
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
		"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
		"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris. " +
		"This content should NOT be chunked by the file source. " +
		"It should be uploaded as a single document to TRag platform. " +
		"The TRag platform will handle chunking based on the configured policy."

	testFile := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	fileSource := NewFileSource([]string{testFile})
	docs, err := fileSource.ReadDocuments(context.Background())

	require.NoError(t, err)
	assert.Len(t, docs, 1, "Should return exactly 1 document (no chunking)")
	assert.Equal(t, testContent, docs[0].Content, "Content should match original")
	assert.Equal(t, "test.txt", docs[0].Name)
	assert.Equal(t, testFile, docs[0].Metadata[kbsource.MetaFilePath])
	assert.True(t, strings.HasPrefix(docs[0].ID, typeTRAGFile+"_"))
}

func TestFileSource_MultipleFiles(t *testing.T) {
	tempDir := t.TempDir()

	file1 := filepath.Join(tempDir, "doc1.txt")
	file2 := filepath.Join(tempDir, "doc2.md")

	err := os.WriteFile(file1, []byte("Content 1"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("Content 2"), 0644)
	require.NoError(t, err)

	source := NewFileSource([]string{file1, file2})
	docs, err := source.ReadDocuments(context.Background())

	require.NoError(t, err)
	assert.Len(t, docs, 2, "Should return 2 documents")
	assert.Equal(t, "Content 1", docs[0].Content)
	assert.Equal(t, "Content 2", docs[1].Content)
}

func TestFileSource_Name_Type(t *testing.T) {
	source := NewFileSource([]string{})

	assert.Equal(t, defaultFileSourceName, source.Name())
	assert.Equal(t, typeTRAGFile, source.Type())
}

func TestFileSource_WithOptions(t *testing.T) {
	customName := "Custom Source"
	metadata := map[string]any{"key": "value"}

	source := NewFileSource(
		[]string{},
		WithFileSourceName(customName),
		WithFileMetadata(metadata),
	)

	assert.Equal(t, customName, source.Name())
	assert.Equal(t, "value", source.GetMetadata()["key"])
}
