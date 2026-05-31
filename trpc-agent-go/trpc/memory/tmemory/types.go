package tmemory

// ingestRequest is the request body for POST /v1/data/add.
type ingestRequest struct {
	BizTraceID string         `json:"biz_trace_id,omitempty"`
	Metadata   ingestMetadata `json:"metadata"`
	StrategyID string         `json:"strategy_id,omitempty"`
	Dialogue   []dialogueTurn `json:"dialogue"`
}

type ingestMetadata struct {
	BizID     string `json:"biz_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Source    string `json:"source,omitempty"`
}

type dialogueTurn struct {
	Role        string       `json:"role"`
	Name        string       `json:"name"`
	Timestamp   string       `json:"timestamp"`
	Content     string       `json:"content"`
	Attachments []attachment `json:"attachments,omitempty"`
}

type attachment struct {
	SourceType string `json:"source_type,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

// ingestResponse is the response body for POST /v1/data/add.
type ingestResponse struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    ingestRespData `json:"data"`
}

type ingestRespData struct {
	RequestID string `json:"request_id"`
}

// recallRequest is the request body for POST /v1/memories/recall.
type recallRequest struct {
	BizID      string         `json:"biz_id"`
	UserID     string         `json:"user_id"`
	SessionID  string         `json:"session_id,omitempty"`
	StrategyID string         `json:"strategy_id,omitempty"`
	Query      string         `json:"query,omitempty"`
	Config     map[string]any `json:"config,omitempty"`
}

// RecallConfig types matching the OpenAPI spec.

// VectorRecallConfig is the recall config for vector-based memory types (raw, episodic).
type VectorRecallConfig struct {
	MemoryType          string  `json:"memory_type"`
	TopK                int     `json:"top_k,omitempty"`
	Threshold           float64 `json:"threshold,omitempty"`
	Filter              string  `json:"filter,omitempty"`
	EnableTimeAwareness *bool   `json:"enable_time_awareness,omitempty"`
	EnableCrossSession  *bool   `json:"enable_cross_session,omitempty"`
	EnableAgenticRecall *bool   `json:"enable_agentic_recall,omitempty"`
	EnableRerank        *bool   `json:"enable_rerank,omitempty"`
	RerankModel         string  `json:"rerank_model,omitempty"`
	EnableTimeDecay     *bool   `json:"enable_time_decay,omitempty"`
	TimeDecayHalfLife   float64 `json:"time_decay_half_life,omitempty"`
}

// GraphRecallConfig is the recall config for graph-based memory.
type GraphRecallConfig struct {
	MemoryType string  `json:"memory_type"`
	TopK       int     `json:"top_k,omitempty"`
	Threshold  float64 `json:"threshold,omitempty"`
	Depth      int     `json:"depth,omitempty"`
}

// ProfileRecallConfig is the recall config for profile-based memory.
type ProfileRecallConfig struct {
	MemoryType         string `json:"memory_type"`
	Filter             string `json:"filter,omitempty"`
	EnableCrossSession *bool  `json:"enable_cross_session,omitempty"`
}

// recallResponse is the response body for POST /v1/memories/recall.
type recallResponse struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    recallResult `json:"data"`
}

// recallResult is the recall payload returned by the internal recall call.
// Kept unexported because recall is an internal capability surfaced to the
// agent only via the memory_search tool; callers do not hold values of
// this type directly.
type recallResult struct {
	RetrievedMemories  map[string][]memoryItem `json:"retrieved_memories"`
	SynthesizedContext string                  `json:"synthesized_context"`
}

// memoryItem represents a single memory entry returned by tMemory recall.
// Unexported for the same reason as recallResult.
type memoryItem struct {
	ID         string         `json:"id,omitempty"`
	MemoryType string         `json:"memory_type"`
	MemoryName string         `json:"memory_name"`
	Content    string         `json:"content"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Score      *float64       `json:"score,omitempty"`
	Extras     map[string]any `json:"extras,omitempty"`
}
