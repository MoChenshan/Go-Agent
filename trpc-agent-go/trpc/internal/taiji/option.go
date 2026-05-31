package taiji

// TaijiOption is the option for the tRAG client.
type TaijiOption struct {
	// Token is the token for the taiji.
	Token string
	// ServiceName is the service name for the taiji.
	ServiceName string
	// URL is the url for the taiji.
	// refer https://iwiki.woa.com/p/4008515885
	URL string
	// TaijiHYAPIToken is the token for the taiji hy api, used for load/update document,optional field.
	// refer https://iwiki.woa.com/p/4010689738
	TaijiHYAPIToken string
	// TaijiHYAPIURL is the url for the taiji hy api, used for load/update document,optional field.
	TaijiHYAPIURL string
	// KnowledgeOption is the options for the taiji knowledge.
	KnowledgeOption
	// TaijiAgentOpts is the options for the tRAG client.
	AgentOption
}

// KnowledgeOption is the options for the taiji knowledge.
type KnowledgeOption struct {
	// EmbIndex is the index id of your embeddings service.
	// refer https://iwiki.woa.com/p/4008515885
	EmbIndex string
	// WSID is the workspace id.
	WSID string
}

type AgentOption struct {
	// HTTPClient is the http client for the tRAG client.
	ApplicationID string
}
