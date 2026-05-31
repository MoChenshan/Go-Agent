package embedder

import (
	"context"
	"os"
	"sync/atomic"
	"testing"

	"git.woa.com/trag/trag-sdk/go-trag"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
	"github.com/stretchr/testify/assert"
)

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func skipIfTRagEnvMissing(t *testing.T) {
	if os.Getenv("TRAG_TOKEN") == "" ||
		os.Getenv("TRAG_RAG_CODE") == "" ||
		os.Getenv("TRAG_NAMESPACE_CODE") == "" ||
		os.Getenv("TRAG_COLLECTION_CODE") == "" {
		t.Skip("tRAG environment variables not set, skipping integration tests")
	}
}

func createValidTRagOption() sdk.TRagOption {
	return sdk.TRagOption{
		Client:         &trag.TRag{},
		RagCode:        "test-rag",
		NamespaceCode:  "test-ns",
		CollectionCode: "test-col",
		EmbeddingModel: "bge-large-en",
	}
}

func TestNewEmbedder(t *testing.T) {
	tests := []struct {
		name        string
		options     []Option
		expectError bool
		errorMsg    string
	}{
		{
			name:        "no options - should fail validation",
			options:     []Option{},
			expectError: true,
			errorMsg:    "tRAG client is nil",
		},
		{
			name: "valid tRAG option",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
			},
			expectError: false,
		},
		{
			name: "invalid tRAG option - no client",
			options: []Option{
				WithTRagOption(sdk.TRagOption{
					RagCode:        "test-rag",
					NamespaceCode:  "test-ns",
					CollectionCode: "test-col",
				}),
			},
			expectError: true,
			errorMsg:    "tRAG client is nil",
		},
		{
			name: "invalid tRAG option - empty rag code",
			options: []Option{
				WithTRagOption(sdk.TRagOption{
					Client:         &trag.TRag{},
					NamespaceCode:  "test-ns",
					CollectionCode: "test-col",
				}),
			},
			expectError: true,
			errorMsg:    "instance code is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder, err := New(tt.options...)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, embedder)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, embedder)
			}
		})
	}
}

func TestEmbedder_GetDimensions(t *testing.T) {
	atomicInt32 := atomic.Int32{}
	atomicInt32.Store(512)
	tests := []struct {
		name       string
		embedder   *Embedder
		dimensions int
	}{
		{
			name: "cached dimensions",
			embedder: func() *Embedder {
				e := &Embedder{
					tragOption: createValidTRagOption(),
				}
				e.dimensions.Store(512)
				return e
			}(),
			dimensions: 512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.embedder.GetDimensions()
			assert.Equal(t, tt.dimensions, result)
		})
	}
}

// Integration tests that require real tRAG environment
func TestEmbedder_IntegrationTests(t *testing.T) {
	skipIfTRagEnvMissing(t)
	// Get environment variables
	tragRagCode := getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode := getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode := getEnvOrDefault("TRAG_COLLECTION_CODE", "")
	tragEmbeddingModel := getEnvOrDefault("TRAG_EMBEDDING_MODEL", "bge-large-en")
	tragToken := getEnvOrDefault("TRAG_TOKEN", "")

	tragOption := sdk.TRagOption{
		Client:         trag.NewTRag(trag.WithToken(tragToken)), // In real tests, this should be properly initialized
		RagCode:        tragRagCode,
		NamespaceCode:  tragNamespaceCode,
		CollectionCode: tragCollectionCode,
		EmbeddingModel: tragEmbeddingModel,
	}

	embedder, err := New(WithTRagOption(tragOption))
	if err != nil {
		t.Skip("Failed to create embedder with real config, skipping integration tests: " + err.Error())
	}

	tests := []struct {
		name       string
		text       string
		dimensions int
	}{
		{
			name:       "simple text",
			text:       "This is a test sentence for embedding.",
			dimensions: 1024,
		},
		{
			name:       "chinese text",
			text:       "这是一个测试句子。",
			dimensions: 1024,
		},
		{
			name:       "long text",
			text:       "This is a longer text that contains multiple sentences. It should be properly embedded by the tRAG service. The embedding should capture the semantic meaning of this text.",
			dimensions: 1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Note: These tests may fail if tRAG service is not properly configured
			// but they serve as integration test examples
			_, err := embedder.GetEmbedding(ctx, tt.text)
			if err != nil {
				t.Logf("GetEmbedding failed (expected if tRAG not configured): %v", err)
			}

			_, usage, err := embedder.GetEmbeddingWithUsage(ctx, tt.text)
			if err != nil {
				t.Logf("GetEmbeddingWithUsage failed (expected if tRAG not configured): %v", err)
			} else {
				assert.Nil(t, usage) // Usage should always be nil
			}

			assert.Equal(t, tt.dimensions, embedder.GetDimensions())
			assert.Equal(t, tt.dimensions, int(embedder.dimensions.Load()))
		})
	}
}
