package tmemory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFlattenRecallResults_Nil(t *testing.T) {
	require.Nil(t, flattenRecallResults(nil))
}

func TestFlattenRecallResults_Empty(t *testing.T) {
	data := &recallResult{RetrievedMemories: map[string][]memoryItem{}}
	results := flattenRecallResults(data)
	require.Empty(t, results)
}

func TestFlattenRecallResults_SkipsEmptyContent(t *testing.T) {
	score := 0.9
	data := &recallResult{
		RetrievedMemories: map[string][]memoryItem{
			"raw": {
				{ID: "1", Content: "valid", Score: &score},
				{ID: "2", Content: ""},
				{ID: "3", Content: "   "},
			},
		},
	}
	results := flattenRecallResults(data)
	require.Len(t, results, 1)
	require.Equal(t, "1", results[0].ID)
	require.Equal(t, "valid", results[0].Memory)
	require.Equal(t, 0.9, results[0].Score)
	require.Equal(t, "raw", results[0].Kind)
}

func TestFlattenRecallResults_NilScore(t *testing.T) {
	data := &recallResult{
		RetrievedMemories: map[string][]memoryItem{
			"profile": {
				{ID: "p1", Content: "user profile", Score: nil},
			},
		},
	}
	results := flattenRecallResults(data)
	require.Len(t, results, 1)
	require.Zero(t, results[0].Score)
}

func TestFlattenRecallResults_MultipleMemoryTypes(t *testing.T) {
	score1 := 0.8
	score2 := 0.7
	data := &recallResult{
		RetrievedMemories: map[string][]memoryItem{
			"raw": {
				{ID: "r1", Content: "raw memory", Score: &score1},
			},
			"graph": {
				{ID: "g1", Content: "graph memory", Score: &score2},
			},
			"empty": {},
		},
	}
	results := flattenRecallResults(data)
	require.Len(t, results, 2)
}

func TestRecall_Success(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotReq    recallRequest
		decodeErr error
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		decodeErr = json.NewDecoder(r.Body).Decode(&gotReq)

		score := 0.95
		resp := recallResponse{
			Code:    0,
			Message: "ok",
			Data: recallResult{
				RetrievedMemories: map[string][]memoryItem{
					"raw": {{ID: "m1", MemoryType: "vector", MemoryName: "raw", Content: "likes coffee", Score: &score}},
				},
				SynthesizedContext: "user likes coffee",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	svc := &Service{
		opts: serviceOpts{recallConfig: defaultRecallConfig(), strategyID: "7"},
		c:    c,
	}

	data, err := svc.recall(context.Background(), "biz1", "user1", "sess1", "coffee")
	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/v1/memories/recall", gotPath)
	require.Equal(t, "biz1", gotReq.BizID)
	require.Equal(t, "user1", gotReq.UserID)
	require.Equal(t, "coffee", gotReq.Query)
	require.Equal(t, "7", gotReq.StrategyID)
	require.Len(t, data.RetrievedMemories["raw"], 1)
	require.Equal(t, "user likes coffee", data.SynthesizedContext)
}

func TestRecall_NonZeroCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := recallResponse{Code: 1001, Message: "invalid biz_id"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	svc := &Service{
		opts: serviceOpts{recallConfig: defaultRecallConfig()},
		c:    c,
	}

	_, err := svc.recall(context.Background(), "biz1", "user1", "sess1", "query")
	require.Error(t, err)
}

func TestRecall_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client(), timeout: 5 * time.Second})
	svc := &Service{
		opts: serviceOpts{recallConfig: defaultRecallConfig()},
		c:    c,
	}

	_, err := svc.recall(context.Background(), "biz1", "user1", "", "query")
	require.Error(t, err)
}
