package trag

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"git.woa.com/trag/trag-sdk/go-trag"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	agentKnowledge "trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/query"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func clearTestDocument(t *testing.T) {
	// Get environment variables
	tragToken := getEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode := getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode := getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode := getEnvOrDefault("TRAG_COLLECTION_CODE", "")

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

// Mock implementations for testing
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

func (m *mockEmbedder) GetDimensionsWithContext(ctx context.Context) int {
	return len(m.embedding)
}

type mockRetriever struct {
	documents []*retriever.RelevantDocument
	err       error
}

func (m *mockRetriever) Retrieve(ctx context.Context, query *retriever.Query) (*retriever.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &retriever.Result{Documents: m.documents}, nil
}

func (m *mockRetriever) Close() error {
	return nil
}

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

type mockSource struct {
	name       string
	sourceType string
	documents  []*document.Document
	err        error
}

func (m *mockSource) Name() string {
	return m.name
}

func (m *mockSource) Type() string {
	return m.sourceType
}

func (m *mockSource) ReadDocuments(ctx context.Context) ([]*document.Document, error) {
	return m.documents, m.err
}

func (m *mockSource) GetMetadata() map[string]any {
	return nil
}

func TestNew(t *testing.T) {
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
			name: "with embedder",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
				WithEmbedder(&mockEmbedder{embedding: []float64{0.1, 0.2, 0.3}}),
			},
			expectError: false,
		},
		{
			name: "with retriever",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
				WithRetriever(&mockRetriever{}),
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
				WithReranker(&mockReranker{}),
			},
			expectError: false,
		},
		{
			name: "with sources",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
				WithSources([]source.Source{
					&mockSource{name: "test-source", sourceType: "test"},
				}),
			},
			expectError: false,
		},
		{
			name: "with all options",
			options: []Option{
				WithTRagOption(createValidTRagOption()),
				WithEmbedder(&mockEmbedder{embedding: []float64{0.1, 0.2, 0.3}}),
				WithRetriever(&mockRetriever{}),
				WithQueryEnhancer(&mockQueryEnhancer{enhanced: "enhanced query"}),
				WithReranker(&mockReranker{}),
				WithSources([]source.Source{
					&mockSource{name: "test-source", sourceType: "test"},
				}),
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
			knowledge, err := New(tt.options...)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, knowledge)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, knowledge)
			}
		})
	}
}

func TestKnowledge_Search_UnitTests(t *testing.T) {
	tests := []struct {
		name        string
		knowledge   *Knowledge
		request     *knowledge.SearchRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "no retriever configured",
			knowledge: &Knowledge{
				tragOption: createValidTRagOption(),
			},
			request: &knowledge.SearchRequest{
				Query: "test query",
			},
			expectError: true,
			errorMsg:    "retriever not configured",
		},
		{
			name: "with mock retriever - no documents",
			knowledge: &Knowledge{
				tragOption: createValidTRagOption(),
				retriever: &mockRetriever{
					documents: []*retriever.RelevantDocument{},
				},
			},
			request: &knowledge.SearchRequest{
				Query: "test query",
			},
			expectError: true,
			errorMsg:    "no relevant documents found",
		},
		{
			name: "with mock retriever - has documents",
			knowledge: &Knowledge{
				tragOption: createValidTRagOption(),
				retriever: &mockRetriever{
					documents: []*retriever.RelevantDocument{
						{
							Document: &document.Document{
								ID:      "doc1",
								Content: "test content",
							},
							Score: 0.9,
						},
					},
				},
			},
			request: &knowledge.SearchRequest{
				Query:      "test query",
				MaxResults: 5,
				MinScore:   0.1,
			},
			expectError: false,
		},
		{
			name: "with query enhancer",
			knowledge: &Knowledge{
				tragOption: createValidTRagOption(),
				retriever: &mockRetriever{
					documents: []*retriever.RelevantDocument{
						{
							Document: &document.Document{
								ID:      "doc1",
								Content: "enhanced content",
							},
							Score: 0.8,
						},
					},
				},
				queryEnhancer: &mockQueryEnhancer{
					enhanced: "enhanced test query",
				},
			},
			request: &knowledge.SearchRequest{
				Query:     "test query",
				UserID:    "user123",
				SessionID: "session456",
				History: []knowledge.ConversationMessage{
					{Role: "user", Content: "previous question"},
					{Role: "assistant", Content: "previous answer"},
				},
			},
			expectError: false,
		},
		{
			name: "retriever error",
			knowledge: &Knowledge{
				tragOption: createValidTRagOption(),
				retriever: &mockRetriever{
					err: assert.AnError,
				},
			},
			request: &knowledge.SearchRequest{
				Query: "test query",
			},
			expectError: true,
			errorMsg:    "retrieval failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := tt.knowledge.Search(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestLoadOptions(t *testing.T) {
	tests := []struct {
		name     string
		option   LoadOption
		expected *loadOptions
	}{
		{
			name:   "WithSrcParallelism",
			option: WithSrcParallelism(4),
			expected: &loadOptions{
				srcParallelism: 4,
			},
		},
		{
			name:   "WithDocParallelism",
			option: WithDocParallelism(8),
			expected: &loadOptions{
				docParallelism: 8,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &loadOptions{}
			tt.option(opts)
			assert.Equal(t, tt.expected, opts)
		})
	}
}

func TestConvertConversationHistory(t *testing.T) {
	tests := []struct {
		name     string
		input    []knowledge.ConversationMessage
		expected []query.ConversationMessage
	}{
		{
			name:     "empty history",
			input:    []knowledge.ConversationMessage{},
			expected: []query.ConversationMessage{},
		},
		{
			name: "single message",
			input: []knowledge.ConversationMessage{
				{Role: "user", Content: "test message"},
			},
			expected: []query.ConversationMessage{
				{Role: "user", Content: "test message"},
			},
		},
		{
			name: "multiple messages",
			input: []knowledge.ConversationMessage{
				{Role: "user", Content: "question"},
				{Role: "assistant", Content: "answer"},
			},
			expected: []query.ConversationMessage{
				{Role: "user", Content: "question"},
				{Role: "assistant", Content: "answer"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertConversationHistory(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Integration tests that require real tRAG environment
func TestKnowledge_IntegrationTests(t *testing.T) {
	skipIfTRagEnvMissing(t)
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

	// Create test documents source
	testDocs := make([]*document.Document, 10)
	for i := 0; i < 10; i++ {
		code := i + 1
		testDocs[i] = &document.Document{
			ID:      fmt.Sprintf("test-%d", code),
			Content: fmt.Sprintf("i am code %d, my content is content-%d-%d", code, code, code),
		}
	}

	testSource := &mockSource{
		name:       "integration-test-source",
		sourceType: "test",
		documents:  testDocs,
	}

	tests := []struct {
		name             string
		knowledgeBuilder func() (*Knowledge, error)
		query            string
		limit            int
		minScore         float64
	}{
		{
			name: "load and search with embedding model",
			knowledgeBuilder: func() (*Knowledge, error) {
				return New(
					WithTRagOption(tragOption),
					WithSources([]source.Source{testSource}),
				)
			},
			query:    "what is the content of code 1",
			limit:    3,
			minScore: 0.1,
		},
		{
			name: "load and search without embedding model",
			knowledgeBuilder: func() (*Knowledge, error) {
				tragOptionCopy := tragOption
				tragOptionCopy.EmbeddingModel = ""
				return New(
					WithTRagOption(tragOptionCopy),
					WithSources([]source.Source{testSource}),
				)
			},
			query:    "what is the content of code 2",
			limit:    5,
			minScore: 0.0,
		},
		{
			name: "load and search with specific embedder",
			knowledgeBuilder: func() (*Knowledge, error) {
				tragOptionCopy := tragOption
				tragOptionCopy.EmbeddingModel = ""
				return New(
					WithTRagOption(tragOptionCopy),
					WithSources([]source.Source{testSource}),
				)
			},
			query:    "what is the content of code 2",
			limit:    5,
			minScore: 0.0,
		},
		{
			name: "load and search with parallelism",
			knowledgeBuilder: func() (*Knowledge, error) {
				return New(
					WithTRagOption(tragOption),
					WithSources([]source.Source{testSource}),
				)
			},
			query:    "what is the content of code 3",
			limit:    2,
			minScore: 0.2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create knowledge instance
			knowledge, err := tt.knowledgeBuilder()
			if err != nil {
				t.Skip("Failed to create knowledge with real config, skipping test: " + err.Error())
			}

			// Load documents into knowledge base
			loadErr := knowledge.Load(ctx, WithSrcParallelism(2), WithDocParallelism(4))
			if loadErr != nil {
				t.Logf("Load failed (expected if tRAG not configured): %v", loadErr)
				return
			}

			// Search in knowledge base - using inline struct since we can't import the correct type
			searchReq := struct {
				Query      string
				MaxResults int
				MinScore   float64
			}{
				Query:      tt.query,
				MaxResults: tt.limit,
				MinScore:   tt.minScore,
			}

			result, err := knowledge.Search(ctx, &agentKnowledge.SearchRequest{
				Query:      searchReq.Query,
				MaxResults: searchReq.MaxResults,
				MinScore:   searchReq.MinScore,
			})
			if err != nil {
				t.Errorf("Search failed: %v", err)
				return
			}

			if !strings.Contains(result.Document.Content, "i am code") {
				t.Fatalf("Failed to retrieve: %v", result.Document.Content)
			}

			clearTestDocument(t)
			time.Sleep(10 * time.Second)
		})
	}
}

func TestAddDocumentHook(t *testing.T) {
	skipIfTRagEnvMissing(t)

	ctx := context.Background()

	var beforeImportCalls []string
	var afterImportCalls []string
	var metadataAdded bool

	// Create a tracking hook using middleware pattern
	trackingHook := func(next ImportDocumentFunc) ImportDocumentFunc {
		return func(ctx context.Context, src source.Source, doc *document.Document) (*ImportResult, error) {
			// Before import
			beforeImportCalls = append(beforeImportCalls, doc.ID)
			t.Logf("Before import: doc_id=%s, source=%s", doc.ID, src.Name())

			// Call next
			result, err := next(ctx, src, doc)

			// After import
			if err == nil {
				afterImportCalls = append(afterImportCalls, doc.ID)
				t.Logf("After import: doc_id=%s, source=%s", doc.ID, src.Name())
			}

			return result, err
		}
	}

	// Create a metadata enrichment hook
	metadataHook := func(next ImportDocumentFunc) ImportDocumentFunc {
		return func(ctx context.Context, src source.Source, doc *document.Document) (*ImportResult, error) {
			// Before import: add metadata
			if doc.Metadata == nil {
				doc.Metadata = make(map[string]any)
			}
			doc.Metadata["import_time"] = time.Now().Format(time.RFC3339)
			doc.Metadata["source_name"] = src.Name()
			metadataAdded = true

			// Call next
			return next(ctx, src, doc)
		}
	}

	testDocs := []*document.Document{
		{ID: "hook-test-doc-1", Content: "test content for hook 1"},
	}

	testSource := &mockSource{
		name:       "test-source-with-hook",
		sourceType: "test",
		documents:  testDocs,
	}

	// Get TRag config from environment
	tragToken := getEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode := getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode := getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode := getEnvOrDefault("TRAG_COLLECTION_CODE", "")

	tragOption := sdk.TRagOption{
		Client:         trag.NewTRag(trag.WithToken(tragToken)),
		RagCode:        tragRagCode,
		NamespaceCode:  tragNamespaceCode,
		CollectionCode: tragCollectionCode,
		Policy:         "replace",
	}

	// Create knowledge with hooks
	k, err := New(
		WithTRagOption(tragOption),
		WithSources([]source.Source{testSource}),
		WithImportDocumentHook(metadataHook), // This executes first (outer wrapper)
		WithImportDocumentHook(trackingHook), // This executes second (inner wrapper)
	)
	assert.NoError(t, err)

	// Load documents
	err = k.Load(ctx)
	if err != nil {
		t.Logf("Load failed (expected if TRag not configured): %v", err)
		return
	}

	// Verify hooks were called
	assert.Equal(t, []string{"hook-test-doc-1"}, beforeImportCalls, "Before import hook should be called")
	assert.Equal(t, []string{"hook-test-doc-1"}, afterImportCalls, "After import hook should be called")
	assert.True(t, metadataAdded, "Metadata should be added by hook")

	clearTestDocument(t)
}

func TestAddDocumentHook_ErrorHandling(t *testing.T) {
	skipIfTRagEnvMissing(t)

	ctx := context.Background()

	// Create a hook that returns error before calling next
	errorHook := func(next ImportDocumentFunc) ImportDocumentFunc {
		return func(ctx context.Context, src source.Source, doc *document.Document) (*ImportResult, error) {
			if doc.ID == "error-doc" {
				return nil, fmt.Errorf("hook error for doc: %s", doc.ID)
			}
			return next(ctx, src, doc)
		}
	}

	testDocs := []*document.Document{
		{ID: "error-doc", Content: "will fail"},
	}

	testSource := &mockSource{
		name:       "test-source-error",
		sourceType: "test",
		documents:  testDocs,
	}

	// Get TRag config from environment
	tragToken := getEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode := getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode := getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode := getEnvOrDefault("TRAG_COLLECTION_CODE", "")

	tragOption := sdk.TRagOption{
		Client:         trag.NewTRag(trag.WithToken(tragToken)),
		RagCode:        tragRagCode,
		NamespaceCode:  tragNamespaceCode,
		CollectionCode: tragCollectionCode,
		Policy:         "replace",
	}

	k, err := New(
		WithTRagOption(tragOption),
		WithSources([]source.Source{testSource}),
		WithImportDocumentHook(errorHook),
	)
	assert.NoError(t, err)

	// Load should fail due to hook error
	err = k.Load(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hook error for doc")
}
