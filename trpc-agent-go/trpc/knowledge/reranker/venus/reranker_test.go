package venus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"
)

func TestVenusReranker_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rerankRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "default", req.Model)

		w.Write([]byte(`{
			"object": "rerank",
			"results": [
				{"relevance_score": 0.89599609375, "index": 1},
				{"relevance_score": 0.00007665157318115234, "index": 0},
				{"relevance_score": 0.00007665157318115234, "index": 2}
			],
			"model": "default",
			"id": "venus-041e13df-6652-4fa1-8af8-666f7f193680"
		}`))
	}))
	defer server.Close()

	r, err := New(WithEndpoint(server.URL), WithModel("default"))
	assert.NoError(t, err)

	query := &reranker.Query{FinalQuery: "what is a panda?"}
	results := []*reranker.Result{
		{Document: &document.Document{Content: "Justice Juan M. Merchan will hear arguments"}},
		{Document: &document.Document{Content: "The giant panda (Ailuropoda melanoleuca)"}},
		{Document: &document.Document{Content: "Paris is in France."}},
	}

	reranked, err := r.Rerank(context.Background(), query, results)
	assert.NoError(t, err)
	assert.Len(t, reranked, 3)
	// Sorted by score descending
	assert.Equal(t, 0.89599609375, reranked[0].Score)
	assert.Contains(t, reranked[0].Document.Content, "panda")
}

func TestVenusReranker_EmptyInput(t *testing.T) {
	r, err := New(WithEndpoint("http://localhost:8000"))
	assert.NoError(t, err)
	query := &reranker.Query{FinalQuery: "test"}
	reranked, err := r.Rerank(context.Background(), query, []*reranker.Result{})
	assert.NoError(t, err)
	assert.Empty(t, reranked)
}

func TestVenusReranker_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	r, err := New(WithEndpoint(server.URL))
	assert.NoError(t, err)

	query := &reranker.Query{FinalQuery: "test"}
	results := []*reranker.Result{{Document: &document.Document{Content: "D0"}}}

	_, err = r.Rerank(context.Background(), query, results)
	assert.Error(t, err)
}

func TestVenusReranker_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid"))
	}))
	defer server.Close()

	r, err := New(WithEndpoint(server.URL))
	assert.NoError(t, err)

	query := &reranker.Query{FinalQuery: "test"}
	results := []*reranker.Result{{Document: &document.Document{Content: "D0"}}}

	_, err = r.Rerank(context.Background(), query, results)
	assert.Error(t, err)
}

func TestVenusReranker_TopN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rerankResponse{
			Object: "rerank",
			Results: []rerankResult{
				{RelevanceScore: 0.9, Index: 0},
				{RelevanceScore: 0.8, Index: 1},
				{RelevanceScore: 0.7, Index: 2},
			},
			Model: "default",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	r, err := New(WithEndpoint(server.URL), WithTopN(2))
	assert.NoError(t, err)

	query := &reranker.Query{FinalQuery: "test"}
	results := []*reranker.Result{
		{Document: &document.Document{Content: "D0"}},
		{Document: &document.Document{Content: "D1"}},
		{Document: &document.Document{Content: "D2"}},
	}

	reranked, err := r.Rerank(context.Background(), query, results)
	assert.NoError(t, err)
	assert.Len(t, reranked, 2)
	assert.Equal(t, 0.9, reranked[0].Score)
	assert.Equal(t, 0.8, reranked[1].Score)
}

func TestVenusReranker_Options(t *testing.T) {
	r, err := New(
		WithAPIKey("key"),
		WithModel("custom-model"),
		WithEndpoint("http://custom"),
		WithTopN(10),
		WithServiceName("trpc.test.venus"),
	)
	assert.NoError(t, err)

	assert.Equal(t, "key", r.apiKey)
	assert.Equal(t, "custom-model", r.modelName)
	assert.Equal(t, "http://custom", r.endpoint)
	assert.Equal(t, 10, r.topN)
	assert.Equal(t, "trpc.test.venus", r.serviceName)
	assert.NotNil(t, r.httpClient)
}

func TestVenusReranker_EmptyEndpoint(t *testing.T) {
	// Empty endpoint is now allowed
	r, err := New()
	assert.NoError(t, err)
	assert.NotNil(t, r)
}

func TestVenusReranker_WithTrpcClientOption(t *testing.T) {
	// Start a mock server to simulate Venus service
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/rerank", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req rerankRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "test-model", req.Model)
		assert.Equal(t, "what is trpc?", req.Query)

		resp := rerankResponse{
			Object: "rerank",
			Results: []rerankResult{
				{RelevanceScore: 0.92, Index: 0},
				{RelevanceScore: 0.78, Index: 1},
			},
			Model: "test-model",
			ID:    "test-id",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Extract host:port from server.URL (e.g., "http://127.0.0.1:12345" -> "127.0.0.1:12345")
	serverAddr := server.URL[len("http://"):]

	// Test using WithTrpcClientOption with ip:// target (方式二)
	r, err := New(
		WithEndpoint("/v1/rerank"),
		WithModel("test-model"),
		WithTrpcClientOption(
			client.WithTarget("ip://"+serverAddr),
		),
	)
	assert.NoError(t, err)
	assert.NotNil(t, r.httpClient)

	query := &reranker.Query{FinalQuery: "what is trpc?"}
	results := []*reranker.Result{
		{Document: &document.Document{Content: "tRPC is a high-performance RPC framework"}},
		{Document: &document.Document{Content: "gRPC is another RPC framework"}},
	}

	reranked, err := r.Rerank(context.Background(), query, results)
	assert.NoError(t, err)
	assert.Len(t, reranked, 2)
	assert.Equal(t, 0.92, reranked[0].Score)
	assert.Equal(t, 0.78, reranked[1].Score)
}

func TestVenusReranker_WithFullURL(t *testing.T) {
	// Start a mock server to simulate Venus service
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/rerank", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req rerankRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "test-model", req.Model)
		assert.Equal(t, "what is golang?", req.Query)

		resp := rerankResponse{
			Object: "rerank",
			Results: []rerankResult{
				{RelevanceScore: 0.95, Index: 0},
				{RelevanceScore: 0.85, Index: 1},
			},
			Model: "test-model",
			ID:    "test-id",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Test using full URL endpoint (方式三)
	r, err := New(
		WithEndpoint(server.URL+"/v1/rerank"),
		WithModel("test-model"),
	)
	assert.NoError(t, err)

	query := &reranker.Query{FinalQuery: "what is golang?"}
	results := []*reranker.Result{
		{Document: &document.Document{Content: "Go is a programming language"}},
		{Document: &document.Document{Content: "Python is also popular"}},
	}

	reranked, err := r.Rerank(context.Background(), query, results)
	assert.NoError(t, err)
	assert.Len(t, reranked, 2)
	assert.Equal(t, 0.95, reranked[0].Score)
	assert.Equal(t, 0.85, reranked[1].Score)
}

func TestVenusReranker_WithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		resp := rerankResponse{
			Object: "rerank",
			Results: []rerankResult{
				{RelevanceScore: 0.9, Index: 0},
			},
			Model: "default",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	r, err := New(WithEndpoint(server.URL), WithAPIKey("test-api-key"))
	assert.NoError(t, err)

	query := &reranker.Query{FinalQuery: "test"}
	results := []*reranker.Result{{Document: &document.Document{Content: "D0"}}}

	_, err = r.Rerank(context.Background(), query, results)
	assert.NoError(t, err)
}

func TestVenusReranker_NilDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rerankResponse{
			Object: "rerank",
			Results: []rerankResult{
				{RelevanceScore: 0.9, Index: 0},
			},
			Model: "default",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	r, err := New(WithEndpoint(server.URL))
	assert.NoError(t, err)

	query := &reranker.Query{FinalQuery: "test"}
	results := []*reranker.Result{{Document: nil}}

	reranked, err := r.Rerank(context.Background(), query, results)
	assert.NoError(t, err)
	assert.Len(t, reranked, 1)
}

func TestVenusReranker_InvalidIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rerankResponse{
			Object: "rerank",
			Results: []rerankResult{
				{RelevanceScore: 0.9, Index: 99}, // Invalid index
			},
			Model: "default",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	r, err := New(WithEndpoint(server.URL))
	assert.NoError(t, err)

	query := &reranker.Query{FinalQuery: "test"}
	results := []*reranker.Result{{Document: &document.Document{Content: "D0"}}}

	reranked, err := r.Rerank(context.Background(), query, results)
	assert.NoError(t, err)
	assert.Empty(t, reranked) // Invalid index should be skipped
}
