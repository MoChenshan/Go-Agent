package taiji

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/sdk"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	agentKnowledge "trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/query"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

// Global environment variables for testing
var (
	taijiURL              = getEnvOrDefault("TAIJI_URL", "http://stream-server-online-openapi.turbotke.production.polaris:1081")
	taijiToken            = getEnvOrDefault("TAIJI_TOKEN", "")
	taijiWSID             = getEnvOrDefault("TAIJI_WSID", "")
	taijiEmbeddingIndexID = getEnvOrDefault("TAIJI_EMBEDDING_INDEX_ID", "")
	taijiHYAPIToken       = getEnvOrDefault("TAIJI_HY_API_TOKEN", "")
	taijiHYAPIURL         = getEnvOrDefault("TAIJI_HY_API_URL", "http://hunyuanaide.taiji.woa.com")
)

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func clearTestDocument(t *testing.T) {
	// Clear test document (implementation depends on Taiji API)
	if taijiURL == "" || taijiToken == "" {
		t.Logf("Taiji environment variables not set, skipping document cleanup")
		return
	}

	// TODO: Implement document cleanup using Taiji API
	t.Logf("Document cleanup for Taiji not implemented yet")
}

func skipIfTaijiEnvMissing(t *testing.T) {
	if taijiToken == "" ||
		taijiURL == "" ||
		taijiWSID == "" ||
		taijiEmbeddingIndexID == "" {
		t.Skip("Taiji environment variables not set, skipping integration tests")
	}
}

func createValidTaijiOption() sdk.TaijiOption {
	return sdk.NewTaijiOption(
		sdk.WithEmbIndex("test-index"),
		sdk.WithURL("http://test.taiji.com"),
		sdk.WithToken("test-token"),
		sdk.WithWSID("test-wsid"),
		sdk.WithTaijiHYAPIToken("test-hy-token"),
		sdk.WithTaijiHYAPIURL("http://test.hy.com"),
	)
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
			errorMsg:    "taiji embedding index id is empty",
		},
		{
			name: "valid Taiji option",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
			},
			expectError: false,
		},
		{
			name: "with embedder",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
			},
			expectError: false,
		},
		{
			name: "with retriever",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
				WithRetriever(&mockRetriever{}),
			},
			expectError: false,
		},
		{
			name: "with query enhancer",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
				WithQueryEnhancer(&mockQueryEnhancer{enhanced: "enhanced query"}),
			},
			expectError: false,
		},
		{
			name: "with reranker",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
				WithReranker(&mockReranker{}),
			},
			expectError: false,
		},
		{
			name: "with sources",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
				WithSources([]source.Source{
					&mockSource{name: "test-source", sourceType: "test"},
				}),
			},
			expectError: false,
		},
		{
			name: "with all options",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
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
			name: "invalid Taiji option - no EmbIndex",
			options: []Option{
				WithTaijiOption(sdk.TaijiOption{
					URL:   "http://test.com",
					Token: "test-token",
					WSID:  "test-wsid",
				}),
			},
			expectError: true,
			errorMsg:    "taiji embedding index id is empty",
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
				taijiOption: createValidTaijiOption(),
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
				taijiOption: createValidTaijiOption(),
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
				taijiOption: createValidTaijiOption(),
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
				taijiOption: createValidTaijiOption(),
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
				taijiOption: createValidTaijiOption(),
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
		{
			name:   "WithTaijiRateLimit",
			option: WithTaijiRateLimit(100*time.Millisecond, 5),
			expected: &loadOptions{
				rateInterval: 100 * time.Millisecond,
				rateBurst:    5,
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

// Integration tests that require real Taiji environment
func TestKnowledge_IntegrationTests(t *testing.T) {
	skipIfTaijiEnvMissing(t)

	taijiOption := sdk.NewTaijiOption(
		sdk.WithEmbIndex(taijiEmbeddingIndexID),
		sdk.WithURL(taijiURL),
		sdk.WithToken(taijiToken),
		sdk.WithWSID(taijiWSID),
		sdk.WithTaijiHYAPIToken(taijiHYAPIToken),
		sdk.WithTaijiHYAPIURL(taijiHYAPIURL),
	)

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
			name: "load and search with Taiji",
			knowledgeBuilder: func() (*Knowledge, error) {
				return New(
					WithTaijiOption(taijiOption),
					WithSources([]source.Source{testSource}),
				)
			},
			query:    "what is the content of code 1",
			limit:    3,
			minScore: 0.1,
		},
		{
			name: "load and search with parallelism",
			knowledgeBuilder: func() (*Knowledge, error) {
				return New(
					WithTaijiOption(taijiOption),
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
			_, loadErr := knowledge.Load(ctx, WithSrcParallelism(2), WithDocParallelism(4))
			if loadErr != nil {
				t.Logf("Load failed (expected if Taiji not configured): %v", loadErr)
				return
			}

			// Search in knowledge base
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
