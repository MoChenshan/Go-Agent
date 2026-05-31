package lingshan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/lingshan/internal/client"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
)

func TestKnowledge_Search(t *testing.T) {
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
			Score: 0.95,
			Metadata: map[string]any{
				"key": "value",
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	k := New(
		WithURL(ts.URL),
		WithKnowledgeBaseID("kb1"),
	)

	req := &knowledge.SearchRequest{
		Query: "query",
	}

	result, err := k.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "content1", result.Text)
	assert.Len(t, result.Documents, 1)
	assert.Equal(t, "value", result.Documents[0].Document.Metadata["key"])
}

func TestKnowledge_Load(t *testing.T) {
	k := &Knowledge{}
	err := k.Load(context.Background())
	assert.Error(t, err)
	assert.Equal(t, "Load is not implemented", err.Error())
}
