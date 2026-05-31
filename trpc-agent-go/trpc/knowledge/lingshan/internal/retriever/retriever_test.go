package retriever

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/lingshan/internal/client"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/searchfilter"
)

func TestRetriever_Retrieve(t *testing.T) {
	mockResp := &client.RetrieveKnowledgeResp{}
	mockResp.Code = 0
	mockResp.Msg = "success"
	mockResp.Data.Results = []struct {
		Score    float32        `json:"score"`
		Metadata map[string]any `json:"metadata"`
		Chunk    struct {
			Content    string `json:"content"`
			ChunkIndex int    `json:"chunkIndex"`
			CharCount  int    `json:"charCount"`
		} `json:"chunk"`
	}{
		{
			Score: 0.8,
			Metadata: map[string]any{
				"key": "top-level",
			},
			Chunk: struct {
				Content    string `json:"content"`
				ChunkIndex int    `json:"chunkIndex"`
				CharCount  int    `json:"charCount"`
			}{
				Content: "content1",
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req client.RetrieveKnowledgeReq
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)
		assert.Equal(t, "kb1", req.KnowledgeBaseID)
		assert.Equal(t, "query", req.Query)
		// Check filter
		assert.NotNil(t, req.Filter)
		assert.Equal(t, "year", req.Filter.Field)
		assert.Equal(t, "FILTER_OPERATOR_EQ", req.Filter.Operator)
		assert.Equal(t, float64(2023), req.Filter.Value)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	r := New(
		WithURL(ts.URL),
		WithKnowledgeBaseID("kb1"),
	)

	queryReq := &retriever.Query{
		Text: "query",
		Filter: &retriever.QueryFilter{
			FilterCondition: &searchfilter.UniversalFilterCondition{
				Field:    "year",
				Operator: searchfilter.OperatorEqual,
				Value:    2023,
			},
		},
	}

	res, err := r.Retrieve(context.Background(), queryReq)
	assert.NoError(t, err)
	assert.Len(t, res.Documents, 1)
	assert.Equal(t, "content1", res.Documents[0].Document.Content)
	assert.InDelta(t, 0.8, res.Documents[0].Score, 0.0001)
	assert.Equal(t, "top-level", res.Documents[0].Document.Metadata["key"])
}

func TestRetriever_convertFilter_Metadata(t *testing.T) {
	r := &Retriever{}
	filter := &retriever.QueryFilter{
		Metadata: map[string]any{
			"type": "paper",
		},
	}
	cond := r.convertFilter(filter)
	assert.NotNil(t, cond)
	assert.Equal(t, "type", cond.Field)
	assert.Equal(t, "FILTER_OPERATOR_EQ", cond.Operator)
	assert.Equal(t, "paper", cond.Value)
}
