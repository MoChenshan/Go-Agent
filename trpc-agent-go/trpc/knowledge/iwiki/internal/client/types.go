// Package client provides the internal HTTP client for iWiki RAG knowledge base.
package client

const (
	// apiPath is the fixed API endpoint path for iWiki RAG search.
	apiPath = "/tencent/api/openapi/v1/recall"
)

// SearchRequest is the request for iWiki RAG search.
type SearchRequest struct {
	Query          string          `json:"query"`
	SearchID       string          `json:"search_id,omitempty"`
	TopK           int             `json:"top_k,omitempty"`
	SearchConf     *SearchConf     `json:"search_conf"`
	AdvancedParams *AdvancedParams `json:"advanced_params,omitempty"`
}

// SearchConf is the search configuration.
type SearchConf struct {
	SpaceIDs        []int    `json:"space_ids,omitempty"`
	DocObjs         []DocObj `json:"doc_objs,omitempty"`
	Topics          []Topic  `json:"topics,omitempty"`
	InternetEnabled bool     `json:"internet_enabled,omitempty"`
}

// DocObj represents a document object in search configuration.
type DocObj struct {
	DocID    int  `json:"doc_id"`
	IsFolder bool `json:"is_folder,omitempty"`
}

// Topic represents a topic object in search configuration.
type Topic struct {
	TopicID string   `json:"topic_id"`
	FileIDs []string `json:"file_ids,omitempty"`
}

// AdvancedParams is the advanced parameters for search tuning.
type AdvancedParams struct {
	SkipPlanner bool `json:"skip_planner,omitempty"`
	SkipRerank  bool `json:"skip_rerank,omitempty"`
	SkipInv     bool `json:"skip_inv,omitempty"`
	NotMerge    bool `json:"not_merge,omitempty"`
}

// SearchResponse is the response from iWiki RAG search.
type SearchResponse struct {
	Code      string        `json:"code"`
	Msg       string        `json:"msg"`
	Data      []SearchChunk `json:"data"`
	RequestID string        `json:"request_id"`
	SearchID  string        `json:"search_id,omitempty"`
	ErrorIDs  []ErrorID     `json:"error_ids,omitempty"`

	// ErrCode and ErrMsg are returned by the API gateway (AGW) on errors.
	ErrCode string `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
}

// ErrorID represents a per-resource error returned by the iWiki API.
type ErrorID struct {
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// SearchChunk represents a single search result chunk.
type SearchChunk struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Content      string `json:"content"`
	Source       string `json:"source"`
	FileType     string `json:"file_type"`
	AttachmentID string `json:"attachment_id"`
	Creator      string `json:"creator"`
	LastModifier string `json:"last_modifier"`
	CreateTime   string `json:"create_time"`
	UpdateTime   string `json:"update_time"`
}
