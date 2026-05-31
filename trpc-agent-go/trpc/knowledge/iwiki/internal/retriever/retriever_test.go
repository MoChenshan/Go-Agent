package retriever

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	iclient "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/iwiki/internal/client"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
)

func newMockServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func okHandler(resp *iclient.SearchResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func TestRetriever_Retrieve_Success(t *testing.T) {
	mockResp := &iclient.SearchResponse{
		Code: "Ok",
		Data: []iclient.SearchChunk{
			{
				ID:           "1",
				Title:        "Title1",
				URL:          "https://example.com/1",
				Content:      "content1",
				Creator:      "user1",
				LastModifier: "user2",
				CreateTime:   "2025-01-01",
				UpdateTime:   "2025-01-02",
			},
			{
				ID:      "2",
				Title:   "Title2",
				Content: "content2",
			},
		},
	}

	ts := newMockServer(t, okHandler(mockResp))
	defer ts.Close()

	r := New(
		WithURL(ts.URL),
		WithPaasID("p"),
		WithToken("t"),
		WithSearchConf(&iclient.SearchConf{SpaceIDs: []int{100}}),
	)

	result, err := r.Retrieve(context.Background(), &retriever.Query{
		Text:  "query",
		Limit: 10,
	})
	assert.NoError(t, err)
	assert.Len(t, result.Documents, 2)

	doc0 := result.Documents[0]
	assert.Equal(t, "1", doc0.Document.ID)
	assert.Equal(t, "Title1", doc0.Document.Name)
	assert.Equal(t, "content1", doc0.Document.Content)
	assert.Equal(t, 1.0, doc0.Score)

	doc1 := result.Documents[1]
	assert.Equal(t, "2", doc1.Document.ID)
	assert.Equal(t, 0.5, doc1.Score)

	// Verify metadata.
	assert.Equal(t, "user1", doc0.Document.Metadata["creator"])
	assert.Equal(t, "user2", doc0.Document.Metadata["last_modifier"])
}

func TestRetriever_Retrieve_EmptyResults(t *testing.T) {
	mockResp := &iclient.SearchResponse{
		Code: "Ok",
		Data: []iclient.SearchChunk{},
	}

	ts := newMockServer(t, okHandler(mockResp))
	defer ts.Close()

	r := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))

	result, err := r.Retrieve(context.Background(), &retriever.Query{Text: "q"})
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Documents)
}

func TestRetriever_Retrieve_WithAdvancedParams(t *testing.T) {
	var receivedReq iclient.SearchRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		resp := &iclient.SearchResponse{
			Code: "Ok",
			Data: []iclient.SearchChunk{{ID: "1", Content: "c"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	r := New(
		WithURL(ts.URL),
		WithPaasID("p"),
		WithToken("t"),
		WithAdvancedParams(&iclient.AdvancedParams{
			SkipPlanner: true,
			NotMerge:    true,
		}),
	)

	_, err := r.Retrieve(context.Background(), &retriever.Query{Text: "q"})
	assert.NoError(t, err)
	assert.NotNil(t, receivedReq.AdvancedParams)
	assert.True(t, receivedReq.AdvancedParams.SkipPlanner)
	assert.True(t, receivedReq.AdvancedParams.NotMerge)
}

func TestRetriever_Retrieve_WithSessionID(t *testing.T) {
	var receivedReq iclient.SearchRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		resp := &iclient.SearchResponse{
			Code: "Ok",
			Data: []iclient.SearchChunk{{ID: "1", Content: "c"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	r := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))

	_, err := r.Retrieve(context.Background(), &retriever.Query{
		Text:      "q",
		SessionID: "session-abc",
	})
	assert.NoError(t, err)
	assert.Equal(t, "session-abc", receivedReq.SearchID)
}

func TestRetriever_Retrieve_NilSearchConf(t *testing.T) {
	var receivedReq iclient.SearchRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		resp := &iclient.SearchResponse{
			Code: "Ok",
			Data: []iclient.SearchChunk{{ID: "1", Content: "c"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// No WithSearchConf — should default to empty SearchConf.
	r := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))

	_, err := r.Retrieve(context.Background(), &retriever.Query{Text: "q"})
	assert.NoError(t, err)
	assert.NotNil(t, receivedReq.SearchConf)
}

func TestRetriever_Retrieve_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &iclient.SearchResponse{
			Code: "Token.Expired",
			Msg:  "Token expired",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	r := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))

	_, err := r.Retrieve(context.Background(), &retriever.Query{Text: "q"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Token.Expired")
}

func TestRetriever_Retrieve_ScoreCalculation(t *testing.T) {
	mockResp := &iclient.SearchResponse{
		Code: "Ok",
		Data: []iclient.SearchChunk{
			{ID: "1", Content: "c1"},
			{ID: "2", Content: "c2"},
			{ID: "3", Content: "c3"},
			{ID: "4", Content: "c4"},
		},
	}

	ts := newMockServer(t, okHandler(mockResp))
	defer ts.Close()

	r := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))

	result, err := r.Retrieve(context.Background(), &retriever.Query{Text: "q"})
	assert.NoError(t, err)
	assert.Len(t, result.Documents, 4)

	// score = (len - i) / len
	assert.Equal(t, 1.0, result.Documents[0].Score)  // 4/4
	assert.Equal(t, 0.75, result.Documents[1].Score) // 3/4
	assert.Equal(t, 0.5, result.Documents[2].Score)  // 2/4
	assert.Equal(t, 0.25, result.Documents[3].Score) // 1/4
}

func TestRetriever_Close(t *testing.T) {
	r := &Retriever{}
	assert.NoError(t, r.Close())
}
