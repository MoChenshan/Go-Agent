package proxy

// taiji agent api: https://iwiki.woa.com/p/4008515885#AppCreate

// AppCreateRequest HunYuan AppCreate API Request struct.
type AppCreateRequest struct {
	Query          string    `json:"query"`
	ForwardService string    `json:"forward_service"`
	QueryID        string    `json:"query_id"`
	Stream         bool      `json:"stream"`
	Messages       []Message `json:"messages"`
	// ... other query parameters
}

// Message defines which Role presents what Content.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AppCreateResponse HunYuan AppCreate API Response struct.
type AppCreateResponse struct {
	Created    int64          `json:"created"`
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	Version    string         `json:"version"`
	Choices    []Choice       `json:"choices"`
	SearchInfo map[string]any `json:"search_info"`
	Processes  map[string]any `json:"processes"`
	Usage      Usage          `json:"usage"`
}

// Choice define the candidate data.
type Choice struct {
	Delta Delta `json:"delta"`
}

// Delta defines the content of the candidate.
type Delta struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Usage defines the extra usage data.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type AppCreateErrorMessage struct {
	QueryID string `json:"query_id"`
	Retcode int    `json:"retcode"`
	ErrMsg  string `json:"err_msg"`
}
