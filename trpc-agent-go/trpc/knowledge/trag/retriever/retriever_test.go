package retriever

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"git.woa.com/trag/trag-sdk/go-trag"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/embedder"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/query"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker/topk"
	agentRetriever "trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
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

func clearTestDocument(t *testing.T) {
	// Get environment variables
	tragToken := getEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode := getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode := getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode := getEnvOrDefault("TRAG_COLLECTION_CODE", "bge-large-en")

	// Clear test document
	client := trag.NewTRag(trag.WithToken(tragToken))
	resp, err := client.CleanDocumentRequest(context.Background(), &trag.CleanDocumentsRequest{
		RagCode:        tragRagCode,
		NamespaceCode:  tragNamespaceCode,
		CollectionCode: tragCollectionCode,
	})
	if err != nil || resp.Code != 0 {
		t.Logf("Failed to clear test document: %v, resp: %v", err, resp)
	}
}

func loadDocument(t *testing.T) {
	tragToken := getEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode := getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode := getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode := getEnvOrDefault("TRAG_COLLECTION_CODE", "")
	tragEmbeddingModel := getEnvOrDefault("TRAG_EMBEDDING_MODEL", "bge-large-en")

	code := 1

	client := trag.NewTRag(trag.WithToken(tragToken))
	for i := 0; i < 10; i++ {
		importDocumentReq := &trag.ImportDocumentRequest{
			RagCode:        tragRagCode,
			NamespaceCode:  tragNamespaceCode,
			CollectionCode: tragCollectionCode,
			EmbeddingModel: tragEmbeddingModel,
			Documents: []struct {
				ID             string         `json:"id"`
				Vector         []float64      `json:"vector,omitempty"`
				EmbeddingQuery string         `json:"embeddingQuery,omitempty"`
				Doc            string         `json:"doc"`
				DocKeyValue    map[string]any `json:"docKeyValue,omitempty"`
			}{
				{
					ID:             fmt.Sprintf("test-%d", code),
					Doc:            fmt.Sprintf("i am code %d, my content is content-%d-%d", code, code, code),
					EmbeddingQuery: fmt.Sprintf("i am code %d, my content is content-%d-%d", code, code, code),
				},
			},
		}
		resp, err := client.ImportDocumentRequest(context.Background(), importDocumentReq)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Code != 0 {
			t.Fatal(resp.Message)
		}
		code++
	}
}

type mockEmbedder struct {
	embedding []float64
	err       error
}

func (m *mockEmbedder) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	return m.embedding, m.err
}

func (m *mockEmbedder) GetEmbeddingWithUsage(ctx context.Context, text string) ([]float64, map[string]any, error) {
	return m.embedding, nil, m.err
}

func (m *mockEmbedder) GetDimensions() int {
	return len(m.embedding)
}

// Mock query enhancer for testing
type mockQueryEnhancer struct {
	enhanced string
	err      error
}

func (m *mockQueryEnhancer) EnhanceQuery(ctx context.Context, req *query.Request) (*query.Enhanced, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &query.Enhanced{Enhanced: m.enhanced}, nil
}

// Mock reranker for testing
type mockReranker struct {
	results []*reranker.Result
	err     error
}

func (m *mockReranker) Rerank(ctx context.Context, query *reranker.Query, results []*reranker.Result) ([]*reranker.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.results != nil {
		return m.results, nil
	}
	return results, nil
}

func TestNewRetriever(t *testing.T) {
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
			errorMsg:    "tRAG option is nil",
		},
		{
			name: "valid tRAG option",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
			},
			expectError: false,
		},
		{
			name: "with embedder",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
				WithEmbedder(&mockEmbedder{embedding: []float64{0.1, 0.2, 0.3}}),
			},
			expectError: false,
		},
		{
			name: "with query enhancer",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
				WithQueryEnhancer(&mockQueryEnhancer{enhanced: "enhanced query"}),
			},
			expectError: false,
		},
		{
			name: "with reranker",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
				WithReRanker(&mockReranker{}),
			},
			expectError: false,
		},
		{
			name: "with all options",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
				WithEmbedder(&mockEmbedder{embedding: []float64{0.1, 0.2, 0.3}}),
				WithQueryEnhancer(&mockQueryEnhancer{enhanced: "enhanced query"}),
				WithReRanker(&mockReranker{}),
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retriever, err := New(tt.options...)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, retriever)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, retriever)
			}
		})
	}
}

func TestRetriever_Close(t *testing.T) {
	option := createValidTRagOption()
	retriever := &Retriever{
		tragOption: &option,
	}

	err := retriever.Close()
	assert.NoError(t, err)
}

func TestRetriever_Retrieve_DocKeyValueMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/trag/collection/document/search", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"traceId": "trace-1",
			"code": 0,
			"message": "ok",
			"data": [
				{
					"id": "doc-1",
					"score": 0.9,
					"doc": "matched content",
					"docKeyValue": {
						"doc_id": "123",
						"source": "iwiki"
					},
					"docFields": [
						{"name": "field_name", "value": "field_value"}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	tragOption := sdk.TRagOption{
		Client:         trag.NewTRag(trag.WithHost(server.URL), trag.WithToken("token")),
		RagCode:        "test-rag",
		NamespaceCode:  "test-ns",
		CollectionCode: "test-col",
		EmbeddingModel: "bge-large-en",
	}
	retriever, err := New(WithTRagOption(tragOption))
	assert.NoError(t, err)

	result, err := retriever.Retrieve(context.Background(), &agentRetriever.Query{
		Text:  "query",
		Limit: 1,
	})
	assert.NoError(t, err)
	assert.Len(t, result.Documents, 1)
	assert.Equal(t, "123", result.Documents[0].Document.Metadata["doc_id"])
	assert.Equal(t, "iwiki", result.Documents[0].Document.Metadata["source"])
	assert.Equal(t, "field_value", result.Documents[0].Document.Metadata["field_name"])
}

// Integration tests that require real tRAG environment
func TestRetriever_IntegrationTests(t *testing.T) {
	skipIfTRagEnvMissing(t)
	loadDocument(t)
	defer func() {
		clearTestDocument(t)
	}()

	// Get environment variables
	tragRagCode := getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode := getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode := getEnvOrDefault("TRAG_COLLECTION_CODE", "")
	tragEmbeddingModel := getEnvOrDefault("TRAG_EMBEDDING_MODEL", "bge-large-en")
	tragToken := getEnvOrDefault("TRAG_TOKEN", "")

	tragOption := sdk.TRagOption{
		Client:         trag.NewTRag(trag.WithToken(tragToken)),
		RagCode:        tragRagCode,
		NamespaceCode:  tragNamespaceCode,
		CollectionCode: tragCollectionCode,
		EmbeddingModel: tragEmbeddingModel,
	}

	retriever, err := New(WithTRagOption(tragOption))
	if err != nil {
		t.Skip("Failed to create retriever with real config, skipping integration tests: " + err.Error())
	}
	embedder, err := embedder.New(embedder.WithTRagOption(tragOption))
	if err != nil {
		t.Skip("Failed to create embedder with real config, skipping integration tests: " + err.Error())
	}

	tests := []struct {
		retriever func() (*Retriever, error)
		name      string
		text      string
		limit     int
		minScore  float64
	}{
		{
			name: "without embedding model",
			retriever: func() (*Retriever, error) {
				tragOptionCopy := tragOption
				tragOptionCopy.EmbeddingModel = ""
				retriever, err := New(WithTRagOption(tragOptionCopy))
				if err != nil {
					return nil, err
				}
				return retriever, nil
			},
			text:     "what is the content of code 1",
			limit:    3,
			minScore: 0.1,
		},
		{
			name: "with embedding model",
			retriever: func() (*Retriever, error) {
				retriever, err := New(WithTRagOption(tragOption))
				if err != nil {
					return nil, err
				}
				return retriever, nil
			},
			text:     "what is the content of code 1",
			limit:    3,
			minScore: 0.1,
		},
		{
			name: "with specific embedder",
			retriever: func() (*Retriever, error) {
				tragOptionCopy := tragOption
				tragOptionCopy.EmbeddingModel = ""
				retriever, err := New(WithTRagOption(tragOptionCopy), WithEmbedder(embedder))
				if err != nil {
					return nil, err
				}
				return retriever, nil
			},
			text:     "what is the content of code 1",
			limit:    3,
			minScore: 0.1,
		},
		{
			name: "with query enhancer",
			retriever: func() (*Retriever, error) {
				retriever, err := New(WithTRagOption(tragOption), WithQueryEnhancer(query.NewPassthroughEnhancer()))
				if err != nil {
					return nil, err
				}
				return retriever, nil
			},
			text:     "what is the content of code 1",
			limit:    3,
			minScore: 0.1,
		},
		{
			name: "with reranker",
			retriever: func() (*Retriever, error) {
				tragOptionCopy := tragOption
				tragOptionCopy.EmbeddingModel = ""
				retriever, err := New(WithTRagOption(tragOptionCopy), WithReRanker(topk.New()))
				if err != nil {
					return nil, err
				}
				return retriever, nil
			},
			text:     "what is the content of code 1",
			limit:    3,
			minScore: 0.1,
		},
		{
			name: "with all options",
			retriever: func() (*Retriever, error) {
				tragOptionCopy := tragOption
				tragOptionCopy.EmbeddingModel = ""
				retriever, err := New(WithTRagOption(tragOptionCopy),
					WithQueryEnhancer(query.NewPassthroughEnhancer()),
					WithReRanker(topk.New()))
				if err != nil {
					return nil, err
				}
				return retriever, nil
			},
			text:     "what is the content of code 1",
			limit:    3,
			minScore: 0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create query struct inline since we can't import the correct type
			query := struct {
				Text     string
				Limit    int
				MinScore float64
			}{
				Text:     tt.text,
				Limit:    tt.limit,
				MinScore: tt.minScore,
			}

			// Note: This would normally call retriever.Retrieve but we can't due to type issues
			// This serves as a placeholder for integration tests
			retriever, err := tt.retriever()
			if err != nil {
				t.Fatalf("Failed to create retriever: %v", err)
			}
			result, err := retriever.Retrieve(context.Background(), &agentRetriever.Query{
				Text:     query.Text,
				Limit:    query.Limit,
				MinScore: query.MinScore,
			})
			if err != nil {
				t.Fatalf("Failed to retrieve: %v", err)
			}

			if len(result.Documents) > query.Limit {
				t.Fatalf("Failed to retrieve over limit: %v", result)
			}

			if len(result.Documents) == 0 {
				t.Fatalf("Failed to retrieve no documents: %v", result)
			}

			if !strings.Contains(result.Documents[0].Document.Content, "i am code") {
				t.Fatalf("Failed to retrieve: %v", result.Documents[0].Document.Content)
			}
		})
	}

	// Test Close
	err = retriever.Close()
	assert.NoError(t, err)
}
