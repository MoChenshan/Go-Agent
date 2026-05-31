package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClient_Search(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify custom headers
		assert.Equal(t, "Bearer xxx", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Verify request body
		var req RetrieveKnowledgeReq
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)
		assert.Equal(t, "kb1", req.KnowledgeBaseID)
		assert.Equal(t, "query1", req.Query)

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": {
				"results": [{
					"score": 0.9,
					"chunk": {
						"content": "content1",
						"chunkIndex": 0,
						"charCount": 8
					},
					"dataSourceId": "source1",
					"dataSourceItemId": "item1",
					"dataSourceItemName": "item name",
					"metadata": {
						"key": "value"
					}
				}]
			}
		}`))
	}))
	defer ts.Close()

	customHeaders := http.Header{}
	customHeaders.Set("Authorization", "Bearer xxx")

	c := New(
		WithURL(ts.URL),
		WithServiceName("test.service"),
		WithKnowledgeBaseID("kb1"),
		WithHTTPHeaders(customHeaders),
	)

	req := &RetrieveKnowledgeReq{
		KnowledgeBaseID: "kb1",
		Query:           "query1",
		TopK:            5,
		ScoreThreshold:  0.5,
	}

	resp, err := c.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Data.Results, 1)
	assert.Equal(t, "content1", resp.Data.Results[0].Chunk.Content)
	assert.Equal(t, 0, resp.Data.Results[0].Chunk.ChunkIndex)
	assert.Equal(t, 8, resp.Data.Results[0].Chunk.CharCount)
	assert.Equal(t, "value", resp.Data.Results[0].Metadata["key"])
}

func TestClient_Search_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := New(WithURL(ts.URL))
	req := &RetrieveKnowledgeReq{}
	_, err := c.Search(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}
