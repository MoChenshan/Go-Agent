package source

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kbsource "trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

func TestTextSource_ReadDocuments(t *testing.T) {
	longContent := "This is a very long text content that would normally be chunked by standard sources. " +
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
		"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
		"However, with TRag text source, it will be uploaded as a single document. " +
		"The TRag platform will handle chunking based on the configured policy."

	texts := []TextContent{
		{
			ID:      "doc1",
			Name:    "Document 1",
			Content: longContent,
		},
		{
			ID:      "doc2",
			Name:    "Document 2",
			Content: "Short content",
		},
	}

	source := NewTextSource(texts)
	docs, err := source.ReadDocuments(context.Background())

	require.NoError(t, err)
	assert.Len(t, docs, 2, "Should return exactly 2 documents (no chunking)")
	assert.Equal(t, longContent, docs[0].Content, "First document content should match")
	assert.Equal(t, "Short content", docs[1].Content, "Second document content should match")
	assert.Equal(t, "doc1", docs[0].ID)
	assert.Equal(t, "Document 1", docs[0].Name)
	assert.Equal(t, "doc2", docs[1].ID)
	assert.Equal(t, "Document 2", docs[1].Name)
}

func TestTextSource_MultipleTexts(t *testing.T) {
	texts := []TextContent{
		{ID: "text_1", Name: "Text 1", Content: "First text content"},
		{ID: "text_2", Name: "Text 2", Content: "Second text content"},
		{ID: "text_3", Name: "Text 3", Content: "Third text content"},
	}

	source := NewTextSource(texts)
	docs, err := source.ReadDocuments(context.Background())

	require.NoError(t, err)
	assert.Len(t, docs, 3)

	assert.Equal(t, "First text content", docs[0].Content)
	assert.Equal(t, "text_1", docs[0].ID)
	assert.Equal(t, "Text 1", docs[0].Name)

	assert.Equal(t, "Second text content", docs[1].Content)
	assert.Equal(t, "text_2", docs[1].ID)
	assert.Equal(t, "Text 2", docs[1].Name)

	assert.Equal(t, "Third text content", docs[2].Content)
	assert.Equal(t, "text_3", docs[2].ID)
	assert.Equal(t, "Text 3", docs[2].Name)
}

func TestTextSource_EmptyTexts(t *testing.T) {
	source := NewTextSource([]TextContent{})
	docs, err := source.ReadDocuments(context.Background())

	require.NoError(t, err)
	assert.Nil(t, docs)
}

func TestTextSource_Name_Type(t *testing.T) {
	source := NewTextSource([]TextContent{})

	assert.Equal(t, defaultTextSourceName, source.Name())
	assert.Equal(t, typeTRAGText, source.Type())
}

func TestTextSource_WithOptions(t *testing.T) {
	customName := "Custom Text Source"
	metadata := map[string]any{
		"category": "documentation",
		"version":  "1.0",
	}

	source := NewTextSource(
		[]TextContent{{Content: "test"}},
		WithTextSourceName(customName),
		WithTextMetadata(metadata),
	)

	assert.Equal(t, customName, source.Name())
	assert.Equal(t, "documentation", source.GetMetadata()["category"])
	assert.Equal(t, "1.0", source.GetMetadata()["version"])
}

func TestTextSource_AutoGenerateID(t *testing.T) {
	tests := []struct {
		name         string
		text         TextContent
		expectID     string
		expectedName string
	}{
		{
			name:         "With ID and Name",
			text:         TextContent{ID: "custom-id", Name: "Custom Name", Content: "content"},
			expectID:     "custom-id",
			expectedName: "Custom Name",
		},
		{
			name:         "With Name only",
			text:         TextContent{Name: "Only Name", Content: "content"},
			expectedName: "Only Name",
		},
		{
			name: "Without ID and Name",
			text: TextContent{Content: "content"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := NewTextSource([]TextContent{tt.text})
			docs, err := source.ReadDocuments(context.Background())

			require.NoError(t, err)
			require.Len(t, docs, 1)

			assert.Equal(t, tt.expectedName, docs[0].Name)

			if tt.expectID != "" {
				assert.Equal(t, tt.expectID, docs[0].ID)
			} else {
				assert.NotEmpty(t, docs[0].ID, "ID should be auto-generated")
				assert.True(t, strings.HasPrefix(docs[0].ID, typeTRAGText+"_"))
			}
		})
	}
}

func TestTextSource_Metadata(t *testing.T) {
	source := NewTextSource(
		[]TextContent{{ID: "test", Content: "test content"}},
		WithTextMetadata(map[string]any{"key": "value"}),
	)

	docs, err := source.ReadDocuments(context.Background())
	require.NoError(t, err)
	require.Len(t, docs, 1)

	assert.Equal(t, typeTRAGText, docs[0].Metadata[kbsource.MetaSource])
	assert.Equal(t, defaultTextSourceName, docs[0].Metadata[kbsource.MetaSourceName])
}
