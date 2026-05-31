package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClient_Search_Success(t *testing.T) {
	var receivedReq SearchRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		err := json.NewDecoder(r.Body).Decode(&receivedReq)
		assert.NoError(t, err)

		resp := &SearchResponse{
			Code: "Ok",
			Msg:  "ok",
			Data: []SearchChunk{
				{ID: "1", Title: "Doc1", Content: "content1"},
			},
			RequestID: "req-123",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := New(
		WithURL(ts.URL),
		WithPaasID("test-paas"),
		WithToken("test-token"),
	)

	req := &SearchRequest{
		Query: "test query",
		TopK:  5,
		SearchConf: &SearchConf{
			SpaceIDs: []int{100},
		},
	}

	resp, err := c.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Ok", resp.Code)
	assert.Len(t, resp.Data, 1)
	assert.Equal(t, "Doc1", resp.Data[0].Title)

	// Verify request body was sent correctly.
	assert.Equal(t, "test query", receivedReq.Query)
	assert.Equal(t, 5, receivedReq.TopK)
	assert.Equal(t, []int{100}, receivedReq.SearchConf.SpaceIDs)
}

func TestClient_Search_WithAdvancedParams(t *testing.T) {
	var receivedReq SearchRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		resp := &SearchResponse{
			Code: "Ok",
			Data: []SearchChunk{{ID: "1", Content: "c"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))

	req := &SearchRequest{
		Query:      "q",
		SearchConf: &SearchConf{},
		AdvancedParams: &AdvancedParams{
			SkipPlanner: true,
			SkipRerank:  true,
		},
	}

	_, err := c.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, receivedReq.AdvancedParams)
	assert.True(t, receivedReq.AdvancedParams.SkipPlanner)
	assert.True(t, receivedReq.AdvancedParams.SkipRerank)
}

func TestClient_Search_RioHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "my-paas", r.Header.Get("X-Rio-Paasid"))
		assert.NotEmpty(t, r.Header.Get("X-Rio-Timestamp"))
		assert.NotEmpty(t, r.Header.Get("X-Rio-Nonce"))
		assert.NotEmpty(t, r.Header.Get("X-Rio-Signature"))

		resp := &SearchResponse{Code: "Ok", Data: []SearchChunk{{ID: "1"}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := New(WithURL(ts.URL), WithPaasID("my-paas"), WithToken("my-token"))
	_, err := c.Search(context.Background(), &SearchRequest{Query: "q", SearchConf: &SearchConf{}})
	assert.NoError(t, err)
}

func TestClient_Search_CustomHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "identity-val", r.Header.Get("X-Tai-Identity"))

		resp := &SearchResponse{Code: "Ok", Data: []SearchChunk{{ID: "1"}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	headers := http.Header{}
	headers.Set("X-Tai-Identity", "identity-val")

	c := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"), WithHTTPHeaders(headers))
	_, err := c.Search(context.Background(), &SearchRequest{Query: "q", SearchConf: &SearchConf{}})
	assert.NoError(t, err)
}

func TestClient_Search_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))
	_, err := c.Search(context.Background(), &SearchRequest{Query: "q", SearchConf: &SearchConf{}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestClient_Search_GatewayError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &SearchResponse{
			ErrCode: "AGW.1403",
			ErrMsg:  "Forbidden",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))
	_, err := c.Search(context.Background(), &SearchRequest{Query: "q", SearchConf: &SearchConf{}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AGW.1403")
	assert.Contains(t, err.Error(), "gateway")
}

func TestClient_Search_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &SearchResponse{
			Code: "Token.Expired",
			Msg:  "Token expired",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))
	_, err := c.Search(context.Background(), &SearchRequest{Query: "q", SearchConf: &SearchConf{}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Token.Expired")
}

func TestClient_Search_ErrorIDs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &SearchResponse{
			Code: "Ok",
			Data: []SearchChunk{},
			ErrorIDs: []ErrorID{
				{ID: 123, Type: "space", Message: "no permission"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := New(WithURL(ts.URL), WithPaasID("p"), WithToken("t"))
	_, err := c.Search(context.Background(), &SearchRequest{Query: "q", SearchConf: &SearchConf{}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no permission")
	assert.Contains(t, err.Error(), "space")
}

func TestClient_Search_URLPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/ebus/iwiki/prod"+apiPath, r.URL.Path)

		resp := &SearchResponse{Code: "Ok", Data: []SearchChunk{{ID: "1"}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := New(WithURL(ts.URL+"/ebus/iwiki/prod"), WithPaasID("p"), WithToken("t"))
	_, err := c.Search(context.Background(), &SearchRequest{Query: "q", SearchConf: &SearchConf{}})
	assert.NoError(t, err)
}

func TestClient_Search_TrailingSlash(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, apiPath, r.URL.Path)

		resp := &SearchResponse{Code: "Ok", Data: []SearchChunk{{ID: "1"}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// URL with trailing slash should be trimmed.
	c := New(WithURL(ts.URL+"/"), WithPaasID("p"), WithToken("t"))
	_, err := c.Search(context.Background(), &SearchRequest{Query: "q", SearchConf: &SearchConf{}})
	assert.NoError(t, err)
}

func TestRioSign(t *testing.T) {
	sig := rioSign("1234567890", "token123", "nonce456")
	assert.NotEmpty(t, sig)
	// Verify deterministic.
	sig2 := rioSign("1234567890", "token123", "nonce456")
	assert.Equal(t, sig, sig2)
	// Verify uppercase hex.
	for _, c := range sig {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F'),
			"signature should be uppercase hex, got: %c", c)
	}
}
