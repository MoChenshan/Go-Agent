package iwiki

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/iwiki/internal/client"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
)

func TestKnowledge_Search(t *testing.T) {
	mockResp := &client.SearchResponse{
		Code: "Ok",
		Msg:  "ok",
		Data: []client.SearchChunk{
			{
				ID:      "0",
				Title:   "Test Title",
				URL:     "https://example.com",
				Content: "test content",
				Creator: "test_user",
			},
			{
				ID:      "1",
				Title:   "Test Title 2",
				Content: "test content 2",
			},
		},
		RequestID: "test-req-id",
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
		WithSearchConf(&SearchConf{
			SpaceIDs: []int{123},
		}),
	)

	req := &knowledge.SearchRequest{
		Query:      "test query",
		MaxResults: 10,
	}

	result, err := k.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test content", result.Text)
	assert.Len(t, result.Documents, 2)
	assert.Equal(t, "Test Title", result.Documents[0].Document.Name)
	assert.Equal(t, "Test Title 2", result.Documents[1].Document.Name)
}

func TestKnowledge_Search_NoResults(t *testing.T) {
	mockResp := &client.SearchResponse{
		Code: "Ok",
		Msg:  "ok",
		Data: []client.SearchChunk{},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
	)

	req := &knowledge.SearchRequest{
		Query: "no results query",
	}

	result, err := k.Search(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no relevant documents found")
}

func TestKnowledge_Search_APIError(t *testing.T) {
	mockResp := &client.SearchResponse{
		Code: "Token.Expired",
		Msg:  "Token expired",
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
	)

	req := &knowledge.SearchRequest{
		Query: "test query",
	}

	result, err := k.Search(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "Token.Expired")
}

func TestKnowledge_Search_RioSignature(t *testing.T) {
	paasID := "my-paasid"
	token := "my-secret-token"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Rio headers are set.
		assert.Equal(t, paasID, r.Header.Get("X-Rio-Paasid"))
		assert.NotEmpty(t, r.Header.Get("X-Rio-Timestamp"))
		assert.NotEmpty(t, r.Header.Get("X-Rio-Nonce"))
		assert.NotEmpty(t, r.Header.Get("X-Rio-Signature"))

		// Verify the signature is correct.
		timestamp := r.Header.Get("X-Rio-Timestamp")
		nonce := r.Header.Get("X-Rio-Nonce")
		signature := r.Header.Get("X-Rio-Signature")
		expected := fmt.Sprintf("%X", sha256.Sum256([]byte(timestamp+token+nonce+timestamp)))
		assert.Equal(t, expected, signature)

		mockResp := &client.SearchResponse{
			Code: "Ok",
			Msg:  "ok",
			Data: []client.SearchChunk{
				{ID: "0", Title: "Result", Content: "content"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID(paasID),
		WithToken(token),
	)

	req := &knowledge.SearchRequest{
		Query: "test query",
	}

	result, err := k.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestKnowledge_Search_WithCustomHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify both Rio headers and custom headers are present.
		assert.NotEmpty(t, r.Header.Get("X-Rio-Paasid"))
		assert.NotEmpty(t, r.Header.Get("X-Rio-Signature"))
		assert.Equal(t, "custom-identity", r.Header.Get("X-Tai-Identity"))

		mockResp := &client.SearchResponse{
			Code: "Ok",
			Msg:  "ok",
			Data: []client.SearchChunk{
				{ID: "0", Title: "Result", Content: "content"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	headers := http.Header{}
	headers.Set("X-Tai-Identity", "custom-identity")

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
		WithHTTPHeaders(headers),
	)

	req := &knowledge.SearchRequest{
		Query: "test query",
	}

	result, err := k.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestKnowledge_Search_WithDocObjs(t *testing.T) {
	var receivedReq client.SearchRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		mockResp := &client.SearchResponse{
			Code: "Ok",
			Msg:  "ok",
			Data: []client.SearchChunk{
				{ID: "0", Title: "Doc Result", Content: "doc content"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
		WithSearchConf(&SearchConf{
			DocObjs: []DocObj{
				{DocID: 4015680433, IsFolder: false},
			},
		}),
	)

	req := &knowledge.SearchRequest{
		Query: "K8s core components",
	}

	result, err := k.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "K8s core components", receivedReq.Query)
	assert.Len(t, receivedReq.SearchConf.DocObjs, 1)
	assert.Equal(t, 4015680433, receivedReq.SearchConf.DocObjs[0].DocID)
}

func TestKnowledge_Search_WithAdvancedParams(t *testing.T) {
	var receivedReq client.SearchRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		mockResp := &client.SearchResponse{
			Code: "Ok",
			Data: []client.SearchChunk{
				{ID: "0", Title: "Result", Content: "content"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
		WithAdvancedParams(&AdvancedParams{
			SkipPlanner: true,
			SkipRerank:  true,
		}),
	)

	req := &knowledge.SearchRequest{
		Query: "test query",
	}

	result, err := k.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, receivedReq.AdvancedParams)
	assert.True(t, receivedReq.AdvancedParams.SkipPlanner)
	assert.True(t, receivedReq.AdvancedParams.SkipRerank)
}

func TestKnowledge_Search_HTTPServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
	)

	req := &knowledge.SearchRequest{
		Query: "test query",
	}

	result, err := k.Search(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "500")
}

func TestKnowledge_Search_GatewayError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mockResp := &client.SearchResponse{
			ErrCode: "AGW.1403",
			ErrMsg:  "Forbidden",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
	)

	req := &knowledge.SearchRequest{
		Query: "test query",
	}

	result, err := k.Search(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "AGW.1403")
}

func TestKnowledge_Retrieve(t *testing.T) {
	mockResp := &client.SearchResponse{
		Code: "Ok",
		Data: []client.SearchChunk{
			{ID: "0", Title: "Doc", Content: "content"},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
	)

	result, err := k.Retrieve(context.Background(), &retriever.Query{Text: "test"})
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Documents, 1)
	assert.Equal(t, "Doc", result.Documents[0].Document.Name)
}

func TestKnowledge_Retrieve_EmptyResults(t *testing.T) {
	mockResp := &client.SearchResponse{
		Code: "Ok",
		Data: []client.SearchChunk{},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
	)

	result, err := k.Retrieve(context.Background(), &retriever.Query{Text: "test"})
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Documents)
}

func TestKnowledge_Search_DocumentMetadata(t *testing.T) {
	mockResp := &client.SearchResponse{
		Code: "Ok",
		Data: []client.SearchChunk{
			{
				ID:           "0",
				Title:        "Title",
				URL:          "https://iwiki.woa.com/p/123",
				Content:      "content",
				Source:       "iwiki",
				FileType:     "markdown",
				Creator:      "alice",
				LastModifier: "bob",
				CreateTime:   "2025-01-01",
				UpdateTime:   "2025-01-02",
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithPaasID("test-paasid"),
		WithToken("test-token"),
	)

	result, err := k.Search(context.Background(), &knowledge.SearchRequest{Query: "q"})
	assert.NoError(t, err)
	assert.Equal(t, "Title", result.Documents[0].Document.Name)
	assert.Equal(t, "content", result.Documents[0].Document.Content)
	assert.Equal(t, "alice", result.Documents[0].Document.Metadata["creator"])
	assert.Equal(t, "bob", result.Documents[0].Document.Metadata["last_modifier"])
	assert.Equal(t, "https://iwiki.woa.com/p/123", result.Documents[0].Document.Metadata["url"])
}

func TestKnowledge_Load(t *testing.T) {
	k := &Knowledge{opt: &options{}}
	err := k.Load(context.Background())
	assert.Error(t, err)
	assert.Equal(t, "Load is not implemented", err.Error())
}
