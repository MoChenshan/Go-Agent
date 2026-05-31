package tmemory

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIngestRequest_MarshalJSON(t *testing.T) {
	req := ingestRequest{
		BizTraceID: "trace-1",
		Metadata: ingestMetadata{
			BizID:     "biz1",
			UserID:    "user1",
			SessionID: "sess1",
			Source:    "test",
		},
		StrategyID: "1",
		Dialogue: []dialogueTurn{
			{Role: "user", Name: "用户", Timestamp: "2026-01-01T00:00:00Z", Content: "hello"},
			{Role: "assistant", Name: "助手", Timestamp: "2026-01-01T00:00:01Z", Content: "hi"},
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	require.Equal(t, "trace-1", m["biz_trace_id"])
	require.Equal(t, "1", m["strategy_id"])

	dialogue, ok := m["dialogue"].([]any)
	require.True(t, ok)
	require.Len(t, dialogue, 2)
}

func TestIngestResponse_UnmarshalJSON(t *testing.T) {
	raw := `{"code":0,"message":"success","data":{"request_id":"req-123"}}`
	var resp ingestResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "success", resp.Message)
	require.Equal(t, "req-123", resp.Data.RequestID)
}

func TestRecallRequest_MarshalJSON(t *testing.T) {
	req := recallRequest{
		BizID:      "biz1",
		UserID:     "user1",
		SessionID:  "sess1",
		StrategyID: "2",
		Query:      "test query",
		Config: map[string]any{
			"raw": VectorRecallConfig{MemoryType: "vector", TopK: 3, Threshold: 0.5},
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	require.Equal(t, "biz1", m["biz_id"])
	require.Equal(t, "test query", m["query"])

	cfg, ok := m["config"].(map[string]any)
	require.True(t, ok)

	rawCfg, ok := cfg["raw"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "vector", rawCfg["memory_type"])
}

func TestRecallResponse_UnmarshalJSON(t *testing.T) {
	raw := `{
		"code": 0,
		"message": "ok",
		"data": {
			"retrieved_memories": {
				"raw": [
					{
						"id": "mem-1",
						"memory_type": "vector",
						"memory_name": "raw",
						"content": "user likes coffee",
						"score": 0.95
					},
					{
						"id": "mem-2",
						"memory_type": "vector",
						"memory_name": "raw",
						"content": ""
					}
				],
				"profile": []
			},
			"synthesized_context": "summary text"
		}
	}`
	var resp recallResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	require.Zero(t, resp.Code)

	rawItems := resp.Data.RetrievedMemories["raw"]
	require.Len(t, rawItems, 2)
	require.Equal(t, "mem-1", rawItems[0].ID)
	require.Equal(t, "user likes coffee", rawItems[0].Content)
	require.NotNil(t, rawItems[0].Score)
	require.Equal(t, 0.95, *rawItems[0].Score)
	require.Nil(t, rawItems[1].Score)
	require.Equal(t, "summary text", resp.Data.SynthesizedContext)
}

func TestVectorRecallConfig_JSON(t *testing.T) {
	cfg := VectorRecallConfig{
		MemoryType: "vector",
		TopK:       5,
		Threshold:  0.8,
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	require.Equal(t, "vector", m["memory_type"])
	require.Equal(t, float64(5), m["top_k"])
}

func TestGraphRecallConfig_JSON(t *testing.T) {
	cfg := GraphRecallConfig{
		MemoryType: "graph",
		TopK:       2,
		Depth:      3,
		Threshold:  0.6,
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	require.Equal(t, float64(3), m["depth"])
}

func TestProfileRecallConfig_JSON(t *testing.T) {
	boolTrue := true
	cfg := ProfileRecallConfig{
		MemoryType:         "profile",
		Filter:             "some-filter",
		EnableCrossSession: &boolTrue,
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	require.Equal(t, "some-filter", m["filter"])
	require.Equal(t, true, m["enable_cross_session"])
}
