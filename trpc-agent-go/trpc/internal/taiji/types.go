package taiji

// API endpoints
const (
	// AppCreateEndpoint is the endpoint for creating an app.
	AppCreateEndpoint = "/openapi/app_platform/app_create"
	// EmbeddingsEndpoint is the endpoint for embedding API
	EmbeddingsEndpoint = "/openapi/embeddings"
	// SearchEndpoint is the endpoint for search API
	SearchEndpoint = "/openapi/app_platform/emb_search"
	// IndexDataEndpoint is the endpoint for index data API
	IndexDataEndpoint = "/api/embedding/embIndexData"

	// CommandAdd is the command for adding data
	CommandAdd = "Add"
	// CommandDel is the command for deleting data
	CommandDel = "Del"
	// CommandUpdate is the command for updating data
	CommandUpdate = "Update"
)

// ErrorMsg represents error information in search response
type ErrorMsg struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	RetCode int    `json:"ret_code"`
}

// ChatRequest represents the request structure for Taiji chat API
type ChatRequest struct {
	// QueryID is a unique identifier for tracking this request in logs
	QueryID string `json:"query_id"`

	// Query is the input text corresponding to the input box in the one-stop debugging dashboard
	Query string `json:"query"`

	// ForwardService is the forwarding service name, format: hyaide-application-{app_id}
	ForwardService string `json:"forward_service"`

	// Messages is the historical conversation context, must be alternating user/assistant Q&A pairs
	Messages []Message `json:"messages"`

	// MultiMedias corresponds to the file selection feature in the one-stop debugging dashboard
	MultiMedias []Multimedia `json:"multimedias,omitempty"`

	// Stream indicates whether to enable streaming, default is false
	Stream bool `json:"stream,omitempty"`

	// EnablePromptFilter indicates whether to enable prompt risk control, default is false
	EnablePromptFilter bool `json:"enable_prompt_filter,omitempty"`

	// UseSafetyTruthModel indicates whether to use trusted model after prompt filter is triggered, default is false
	UseSafetyTruthModel bool `json:"use_safety_truth_model,omitempty"`

	// SafetyTruthModel specifies the model service group in the same business space when using trusted model
	SafetyTruthModel string `json:"safety_truth_model,omitempty"`

	// Wordings is the reply text used when prompt filter is enabled but security whitelist is not hit
	Wordings string `json:"wordings,omitempty"`

	// EnableAnswerFilter indicates whether to enable answer risk control, default is false
	EnableAnswerFilter bool `json:"enable_answer_filter,omitempty"`

	// AgentSessionID if provided, will retrieve historical chat information based on this ID
	AgentSessionID string `json:"agent_session_id,omitempty"`

	// Context is used to pass business information, can be used with context parsing plugin
	Context string `json:"context,omitempty"`

	// Params contains additional parameter configurations
	Params *ChatParams `json:"params,omitempty"`
}

// Message represents a single message in the conversation
type Message struct {
	// Role must be either "user" or "assistant"
	Role string `json:"role"`

	// Content is the text content of the message
	Content string `json:"content"`

	// Agent contains agent information including multimedia list (optional)
	Agent *MessageAgent `json:"agent,omitempty"`
}

// MessageAgent represents agent information in a message
type MessageAgent struct {
	// MultiMedias is the list of multimedia files
	MultiMedias []Multimedia `json:"multimedias,omitempty"`
}

// Multimedia represents a multimedia file
type Multimedia struct {
	// Type is the file type, e.g., "docx"
	Type string `json:"type"`

	// URL is the file address
	URL string `json:"url"`

	// FileName is the name of the file
	FileName string `json:"file_name"`

	// MediaID is the unique identifier of the file
	MediaID string `json:"media_id"`
}

// ChatParams represents additional parameters for chat request
type ChatParams struct {
	// EnableDebugStr indicates whether to return debug information, default is off, set to 1 to enable
	EnableDebugStr int `json:"enable_debug_str,omitempty"`

	// EnableStepRun indicates whether to return call chain logs, default is off, set to 1 to enable (only supported by old version orchestration)
	EnableStepRun int `json:"enable_step_run,omitempty"`

	// SessionExpireTime is the memory time for historical chat information in seconds, default is 259200 seconds
	SessionExpireTime int `json:"session_expire_time,omitempty"`

	// SessionRecallMessageSize is the number of historical information rounds to recall, default is 10 rounds
	SessionRecallMessageSize int `json:"session_recall_message_size,omitempty"`
}

// ChatHeaders represents the HTTP headers for chat request
type ChatHeaders struct {
	// Staffname is the user ID, will be recorded in logs for data analysis
	Staffname string `header:"Staffname,omitempty"`
}

// ConvertToHeaders converts the ChatHeaders to a map of headers
func (c *ChatHeaders) ConvertToHeaders() map[string]string {
	headers := make(map[string]string)
	if c.Staffname != "" {
		headers["Staffname"] = c.Staffname
	}
	return headers
}

// ChatResp represents the response structure for Taiji chat API
type ChatResp struct {
	Message          string    `json:"message"`
	QueryID          string    `json:"query_id"`
	Result           string    `json:"result"`
	ReasoningContent string    `json:"reasoning_content"`
	Error            *ErrorMsg `json:"error,omitempty"`
	ErrorMsg         string    `json:"err_msg,omitempty"`
	RetCode          int       `json:"retcode"`
}

// EmbeddingRequest represents the request payload for embedding API
type EmbeddingRequest struct {
	QueryID                     string `json:"query_id"`
	Input                       string `json:"input"` // string or []string
	Model                       string `json:"model"`
	ReturnLastContextEmbeddings *int   `json:"return_last_context_embeddings,omitempty"`
}

// EmbeddingResponse represents the response from the embedding API
type EmbeddingResponse struct {
	Data    []EmbeddingData `json:"data"`
	Created int64           `json:"created"`
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Object  string          `json:"object"`
	GPUName string          `json:"gpu_name,omitempty"`
	Usage   Usage           `json:"usage"`
}

// EmbeddingData represents individual embedding data
type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// SearchRequest represents the request payload for search API
type SearchRequest struct {
	QueryID  string `json:"query_id"`
	Text     string `json:"text"`
	K        int    `json:"k"`
	EmbIndex string `json:"emb_index"`
}

// SearchResponse represents the response from the search API
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	RetCode int            `json:"retcode"`
	Error   *ErrorMsg      `json:"error,omitempty"`
}

// SearchResult represents individual search result
type SearchResult struct {
	Index  string  `json:"index"`
	Metric float64 `json:"metric"`
	Value  string  `json:"value"`
}

// IndexDataRequest represents the request payload for index data API
type IndexDataRequest struct {
	EmbIndexID string          `json:"emb_index_id"`
	Command    string          `json:"command"` // Update/Add/Delete
	Data       []IndexDataItem `json:"data"`
}

// IndexDataItem represents individual data item for index operations
type IndexDataItem struct {
	ID    string `json:"id"`
	Value string `json:"value,omitempty"` // Required for Update/Add, omitted for Delete
	Query string `json:"query,omitempty"` // Required for Update/Add, omitted for Delete
}

// IndexDataResponse represents the response from the index data API
type IndexDataResponse struct {
	Status int                   `json:"status"`
	Msg    string                `json:"msg"`
	Data   IndexDataResponseData `json:"data"`
}

// IndexDataResponseData represents the data field in index data response
type IndexDataResponseData struct {
	Result []IndexDataResult `json:"result"`
}

// IndexDataResult represents individual result in index data response
type IndexDataResult struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}
