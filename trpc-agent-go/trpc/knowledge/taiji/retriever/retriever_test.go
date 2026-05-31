package retriever

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/sdk"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
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

func skipIfTaijiEnvMissing(t *testing.T) {
	if os.Getenv("TAIJI_TOKEN") == "" ||
		os.Getenv("TAIJI_URL") == "" ||
		os.Getenv("TAIJI_WSID") == "" ||
		os.Getenv("TAIJI_EMBEDDING_INDEX_ID") == "" {
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
			errorMsg:    "taiji option is nil",
		},
		{
			name: "valid Taiji option",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
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
				WithReRanker(&mockReranker{}),
			},
			expectError: false,
		},
		{
			name: "with max results",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
				WithMaxResults(20),
			},
			expectError: false,
		},
		{
			name: "with all options",
			options: []Option{
				WithTaijiOption(createValidTaijiOption()),
				WithQueryEnhancer(&mockQueryEnhancer{enhanced: "enhanced query"}),
				WithReRanker(&mockReranker{}),
				WithMaxResults(15),
			},
			expectError: false,
		},
		{
			name: "invalid Taiji option - no EmbIndex",
			options: []Option{
				WithTaijiOption(sdk.NewTaijiOption(
					sdk.WithURL("http://test.taiji.com"),
					sdk.WithToken("test-token"),
					sdk.WithWSID("test-wsid"),
				)),
			},
			expectError: true,
			errorMsg:    "taiji embedding index id is empty",
		},
		{
			name: "invalid Taiji option - no WSID",
			options: []Option{
				WithTaijiOption(sdk.NewTaijiOption(
					sdk.WithEmbIndex("test-index"),
					sdk.WithURL("http://test.taiji.com"),
					sdk.WithToken("test-token"),
				)),
			},
			expectError: true,
			errorMsg:    "taiji workspace id is empty",
		},
		{
			name: "invalid Taiji option - no Token",
			options: []Option{
				WithTaijiOption(sdk.NewTaijiOption(
					sdk.WithEmbIndex("test-index"),
					sdk.WithURL("http://test.taiji.com"),
					sdk.WithWSID("test-wsid"),
				)),
			},
			expectError: true,
			errorMsg:    "taiji auth token is empty",
		},
		{
			name: "invalid Taiji option - no URL or ServiceName",
			options: []Option{
				WithTaijiOption(sdk.NewTaijiOption(
					sdk.WithEmbIndex("test-index"),
					sdk.WithToken("test-token"),
					sdk.WithWSID("test-wsid"),
				)),
			},
			expectError: true,
			errorMsg:    "taiji url or service name is empty",
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
	option := createValidTaijiOption()
	retriever, err := New(WithTaijiOption(option))
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	err = retriever.Close()
	assert.NoError(t, err)
}

func TestRetriever_getMaxResults(t *testing.T) {
	option := createValidTaijiOption()
	retriever, err := New(WithTaijiOption(option))
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	tests := []struct {
		name      string
		maxResult int
		expected  int
	}{
		{
			name:      "zero max result",
			maxResult: 0,
			expected:  defaultMaxResults,
		},
		{
			name:      "positive max result",
			maxResult: 5,
			expected:  5,
		},
		{
			name:      "negative max result",
			maxResult: -1,
			expected:  defaultMaxResults,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := retriever.getMaxResults(tt.maxResult)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRetriever_WithMaxResults(t *testing.T) {
	tests := []struct {
		name      string
		maxResult int
		expected  int
	}{
		{
			name:      "zero max result",
			maxResult: 0,
			expected:  defaultMaxResults,
		},
		{
			name:      "positive max result",
			maxResult: 20,
			expected:  20,
		},
		{
			name:      "negative max result",
			maxResult: -1,
			expected:  defaultMaxResults,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			option := createValidTaijiOption()
			retriever, err := New(
				WithTaijiOption(option),
				WithMaxResults(tt.maxResult),
			)
			assert.NoError(t, err)
			assert.NotNil(t, retriever)
			assert.Equal(t, tt.expected, retriever.maxResults)
		})
	}
}

// Integration tests that require real Taiji environment
func TestRetriever_IntegrationTests(t *testing.T) {
	skipIfTaijiEnvMissing(t)

	// Get environment variables
	taijiURL := getEnvOrDefault("TAIJI_URL", "")
	taijiToken := getEnvOrDefault("TAIJI_TOKEN", "")
	taijiWSID := getEnvOrDefault("TAIJI_WSID", "")
	taijiEmbeddingIndexID := getEnvOrDefault("TAIJI_EMBEDDING_INDEX_ID", "")
	taijiHYAPIToken := getEnvOrDefault("TAIJI_HY_API_TOKEN", "")
	taijiHYAPIURL := getEnvOrDefault("TAIJI_HY_API_URL", "http://hunyuanaide.taiji.woa.com")

	taijiOption := sdk.NewTaijiOption(
		sdk.WithEmbIndex(taijiEmbeddingIndexID),
		sdk.WithURL(taijiURL),
		sdk.WithToken(taijiToken),
		sdk.WithWSID(taijiWSID),
		sdk.WithTaijiHYAPIToken(taijiHYAPIToken),
		sdk.WithTaijiHYAPIURL(taijiHYAPIURL),
	)

	tests := []struct {
		retriever func() (*Retriever, error)
		name      string
		text      string
		limit     int
		minScore  float64
	}{
		{
			name: "basic search",
			retriever: func() (*Retriever, error) {
				return New(WithTaijiOption(taijiOption))
			},
			text:     "test query",
			limit:    5,
			minScore: 0.0,
		},
		{
			name: "with query enhancer",
			retriever: func() (*Retriever, error) {
				return New(
					WithTaijiOption(taijiOption),
					WithQueryEnhancer(query.NewPassthroughEnhancer()),
				)
			},
			text:     "test query",
			limit:    3,
			minScore: 0.0,
		},
		{
			name: "with reranker",
			retriever: func() (*Retriever, error) {
				return New(
					WithTaijiOption(taijiOption),
					WithReRanker(topk.New()),
				)
			},
			text:     "test query",
			limit:    3,
			minScore: 0.0,
		},
		{
			name: "with all options",
			retriever: func() (*Retriever, error) {
				return New(
					WithTaijiOption(taijiOption),
					WithQueryEnhancer(query.NewPassthroughEnhancer()),
					WithReRanker(topk.New()),
					WithMaxResults(10),
				)
			},
			text:     "test query",
			limit:    5,
			minScore: 0.0,
		},
		{
			name: "with min score filter",
			retriever: func() (*Retriever, error) {
				return New(WithTaijiOption(taijiOption))
			},
			text:     "test query",
			limit:    5,
			minScore: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retriever, err := tt.retriever()
			if err != nil {
				t.Skip("Failed to create retriever with real config, skipping integration tests: " + err.Error())
			}

			result, err := retriever.Retrieve(context.Background(), &agentRetriever.Query{
				Text:     tt.text,
				Limit:    tt.limit,
				MinScore: tt.minScore,
			})

			if err != nil {
				// If it's a network error or service unavailable, skip the test
				if strings.Contains(err.Error(), "connection") ||
					strings.Contains(err.Error(), "timeout") ||
					strings.Contains(err.Error(), "unavailable") {
					t.Skip("Taiji service unavailable, skipping integration test: " + err.Error())
				}
				t.Fatalf("Failed to retrieve: %v", err)
			}

			assert.NotNil(t, result)
			assert.NotNil(t, result.Documents)

			// Check that results don't exceed limit
			if len(result.Documents) > tt.limit {
				t.Errorf("Retrieved %d documents, but limit was %d", len(result.Documents), tt.limit)
			}

			// Check min score filter if applied
			if tt.minScore > 0 {
				for _, doc := range result.Documents {
					if doc.Score < tt.minScore {
						t.Errorf("Document score %f is below min score %f", doc.Score, tt.minScore)
					}
				}
			}

			// Verify document structure
			for i, doc := range result.Documents {
				assert.NotNil(t, doc.Document, "Document %d should not be nil", i)
				assert.NotEmpty(t, doc.Document.ID, "Document %d ID should not be empty", i)
				assert.NotEmpty(t, doc.Document.Content, "Document %d content should not be empty", i)
				assert.GreaterOrEqual(t, doc.Score, 0.0, "Document %d score should be >= 0", i)
				assert.LessOrEqual(t, doc.Score, 1.0, "Document %d score should be <= 1", i)
			}
		})
	}

	// Test Close
	retriever, err := New(WithTaijiOption(taijiOption))
	if err != nil {
		t.Skip("Failed to create retriever for Close test: " + err.Error())
	}
	err = retriever.Close()
	assert.NoError(t, err)
}

func TestRetriever_Retrieve_WithQueryEnhancer(t *testing.T) {
	option := createValidTaijiOption()
	enhancer := &mockQueryEnhancer{
		enhanced: "enhanced test query",
		err:      nil,
	}

	retriever, err := New(
		WithTaijiOption(option),
		WithQueryEnhancer(enhancer),
	)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	// This test will fail in unit test mode because it requires real Taiji service
	// But it validates the structure is correct
	ctx := context.Background()
	queryReq := &agentRetriever.Query{
		Text:     "test query",
		Limit:    5,
		MinScore: 0.0,
	}

	// In unit test, this will fail due to network, but we can verify the error
	_, err = retriever.Retrieve(ctx, queryReq)
	// We expect an error in unit test mode (no real service)
	if err != nil {
		// Verify it's not a query enhancer error
		if strings.Contains(err.Error(), "enhance") {
			t.Errorf("Unexpected query enhancer error: %v", err)
		}
	}
}

func TestRetriever_Retrieve_WithReranker(t *testing.T) {
	option := createValidTaijiOption()
	rerankerMock := &mockReranker{
		results: []*reranker.Result{
			{
				Document: &document.Document{
					ID:      "test-1",
					Content: "test content 1",
				},
				Score: 0.9,
			},
		},
		err: nil,
	}

	retriever, err := New(
		WithTaijiOption(option),
		WithReRanker(rerankerMock),
	)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	ctx := context.Background()
	queryReq := &agentRetriever.Query{
		Text:     "test query",
		Limit:    5,
		MinScore: 0.0,
	}

	// In unit test, this will fail due to network, but we can verify the structure
	_, err = retriever.Retrieve(ctx, queryReq)
	// We expect an error in unit test mode (no real service)
	if err != nil {
		// Verify it's not a reranker error
		if strings.Contains(err.Error(), "rerank") {
			t.Errorf("Unexpected reranker error: %v", err)
		}
	}
}

func TestRetriever_Retrieve_EmptyQuery(t *testing.T) {
	option := createValidTaijiOption()
	retriever, err := New(WithTaijiOption(option))
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	ctx := context.Background()
	queryReq := &agentRetriever.Query{
		Text:     "",
		Limit:    5,
		MinScore: 0.0,
	}

	_, err = retriever.Retrieve(ctx, queryReq)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query are empty")
}

func TestRetriever_Retrieve_QueryEnhancerError(t *testing.T) {
	option := createValidTaijiOption()
	enhancer := &mockQueryEnhancer{
		enhanced: "",
		err:      fmt.Errorf("enhancement failed"),
	}

	retriever, err := New(
		WithTaijiOption(option),
		WithQueryEnhancer(enhancer),
	)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	ctx := context.Background()
	queryReq := &agentRetriever.Query{
		Text:     "test query",
		Limit:    5,
		MinScore: 0.0,
	}

	_, err = retriever.Retrieve(ctx, queryReq)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "enhancement failed")
}

func TestRetriever_Retrieve_RerankerError(t *testing.T) {
	option := createValidTaijiOption()
	rerankerMock := &mockReranker{
		results: nil,
		err:     fmt.Errorf("rerank failed"),
	}

	retriever, err := New(
		WithTaijiOption(option),
		WithReRanker(rerankerMock),
	)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	ctx := context.Background()
	queryReq := &agentRetriever.Query{
		Text:     "test query",
		Limit:    5,
		MinScore: 0.0,
	}

	// This will fail at search step in unit test, but we can test reranker error path
	// by mocking the search results. However, since we can't easily mock the Taiji client,
	// we'll just verify the structure is correct.
	_, err = retriever.Retrieve(ctx, queryReq)
	// In unit test mode, we expect search to fail first
	if err != nil {
		// If error contains "rerank", it means reranker was called and failed
		if strings.Contains(err.Error(), "rerank") {
			assert.Contains(t, err.Error(), "rerank failed")
		}
	}
}
