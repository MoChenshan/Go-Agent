// Package client provides the internal HTTP client for LingShan knowledge base.
package client

const (
	retrieveKnowledgeEndpoint = "/agui/knowledge/retrieve"

	// Filter operators for knowledge base search.
	FilterOperatorUnspecified = "FILTER_OPERATOR_UNSPECIFIED"
	FilterOperatorEQ          = "FILTER_OPERATOR_EQ"
	FilterOperatorNE          = "FILTER_OPERATOR_NE"
	FilterOperatorGT          = "FILTER_OPERATOR_GT"
	FilterOperatorGTE         = "FILTER_OPERATOR_GTE"
	FilterOperatorLT          = "FILTER_OPERATOR_LT"
	FilterOperatorLTE         = "FILTER_OPERATOR_LTE"
	FilterOperatorIN          = "FILTER_OPERATOR_IN"
	FilterOperatorNotIN       = "FILTER_OPERATOR_NOT_IN"
	FilterOperatorLike        = "FILTER_OPERATOR_LIKE"
	FilterOperatorNotLike     = "FILTER_OPERATOR_NOT_LIKE"
	FilterOperatorBetween     = "FILTER_OPERATOR_BETWEEN"
	FilterOperatorAND         = "FILTER_OPERATOR_AND"
	FilterOperatorOR          = "FILTER_OPERATOR_OR"
)

// FilterCondition defines the filter condition for knowledge base search.
type FilterCondition struct {
	Field      string             `json:"field,omitempty"`
	Operator   string             `json:"operator,omitempty"`
	Value      any                `json:"value,omitempty"`
	Conditions []*FilterCondition `json:"conditions,omitempty"`
}

// RetrieveKnowledgeReq is the request for knowledge base retrieval.
type RetrieveKnowledgeReq struct {
	KnowledgeBaseID string           `json:"knowledgeBaseId"`
	Query           string           `json:"query"`
	TopK            int32            `json:"topK"`
	ScoreThreshold  float32          `json:"scoreThreshold"`
	Filter          *FilterCondition `json:"filter,omitempty"`
}

// RetrieveKnowledgeResp is the response for knowledge base retrieval.
type RetrieveKnowledgeResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Results []struct {
			Score    float32        `json:"score"`
			Metadata map[string]any `json:"metadata"`
			Chunk    struct {
				Content    string `json:"content"`
				ChunkIndex int    `json:"chunkIndex"`
				CharCount  int    `json:"charCount"`
			} `json:"chunk"`
		} `json:"results"`
	} `json:"data"`
}
