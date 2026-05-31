//go:build iwiki
// +build iwiki

// iwiki_tool_real.go: 启用 `-tags iwiki` 时生效的真实 iwiki 接入。
//
// 该文件自行实现 iWiki RAG OpenAPI 客户端（Rio 签名 + HTTP POST），
// 不依赖任何内网包，仅需标准库 + 公开版 trpc-agent-go。
//
// 编译方式：
//
//	go build -tags iwiki ./...
//
// 前置条件：
//   - 配置环境变量 IWIKI_PAAS_ID / IWIKI_TOKEN
//   - 能访问 iWiki API（内网网络环境）
package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	frameworkkb "trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ---- iWiki HTTP Client (自行实现，无内网依赖) ----

// iwikiClient 是 iWiki RAG OpenAPI 的轻量 HTTP 客户端。
type iwikiClient struct {
	url        string
	paasID     string
	token      string
	spaceIDs   []int
	httpClient *http.Client
}

// iwikiSearchRequest iWiki 搜索请求体。
type iwikiSearchRequest struct {
	Query      string           `json:"query"`
	TopK       int              `json:"top_k,omitempty"`
	SearchConf *iwikiSearchConf `json:"search_conf"`
}

// iwikiSearchConf 搜索配置。
type iwikiSearchConf struct {
	SpaceIDs []int `json:"space_ids,omitempty"`
}

// iwikiSearchResponse iWiki 搜索响应。
type iwikiSearchResponse struct {
	Code      string             `json:"code"`
	Msg       string             `json:"msg"`
	Data      []iwikiSearchChunk `json:"data"`
	RequestID string             `json:"request_id"`
	ErrCode   string             `json:"errcode,omitempty"`
	ErrMsg    string             `json:"errmsg,omitempty"`
	ErrorIDs  []iwikiErrorID     `json:"error_ids,omitempty"`
}

// iwikiSearchChunk 单条搜索结果。
type iwikiSearchChunk struct {
	Content  string  `json:"content"`
	Score    float64 `json:"score"`
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	SpaceID  int     `json:"space_id"`
	DocID    int     `json:"doc_id"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// iwikiErrorID 错误 ID。
type iwikiErrorID struct {
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

const iwikiAPIPath = "/tencent/api/openapi/v1/recall"

// rioSign 计算 Rio 签名：SHA256(timestamp + token + nonce + timestamp) 大写。
func rioSign(timestamp, token, nonce string) string {
	signStr := timestamp + token + nonce + timestamp
	return fmt.Sprintf("%X", sha256.Sum256([]byte(signStr)))
}

// search 执行 iWiki RAG 搜索。
func (c *iwikiClient) search(ctx context.Context, query string, topK int) (*iwikiSearchResponse, error) {
	req := &iwikiSearchRequest{
		Query: query,
		TopK:  topK,
	}
	if len(c.spaceIDs) > 0 {
		req.SearchConf = &iwikiSearchConf{SpaceIDs: c.spaceIDs}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimSuffix(c.url, "/") + iwikiAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Rio 鉴权头
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := strconv.Itoa(rand.Intn(1000000))
	signature := rioSign(timestamp, c.token, nonce)

	httpReq.Header.Set("X-Rio-Paasid", c.paasID)
	httpReq.Header.Set("X-Rio-Timestamp", timestamp)
	httpReq.Header.Set("X-Rio-Nonce", nonce)
	httpReq.Header.Set("X-Rio-Signature", signature)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var searchResp iwikiSearchResponse
	if err := json.Unmarshal(respBody, &searchResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if searchResp.ErrCode != "" {
		return nil, fmt.Errorf("iwiki gateway error: errcode=%s, errmsg=%s", searchResp.ErrCode, searchResp.ErrMsg)
	}
	if searchResp.Code != "Ok" {
		return nil, fmt.Errorf("iwiki error: code=%s, msg=%s", searchResp.Code, searchResp.Msg)
	}

	if len(searchResp.Data) == 0 && len(searchResp.ErrorIDs) > 0 {
		var msgs []string
		for _, e := range searchResp.ErrorIDs {
			msgs = append(msgs, fmt.Sprintf("[%s:%d] %s", e.Type, e.ID, e.Message))
		}
		return nil, fmt.Errorf("iwiki no data: %s", strings.Join(msgs, "; "))
	}

	return &searchResp, nil
}

// ---- buildIWikiTool 真实实现 ----

// buildIWikiTool 启用 -tags iwiki 时的真实实现。
func buildIWikiTool(cfg IWikiConfig) (tool.Tool, bool) {
	if cfg.Disabled {
		return iwikiStubTool("iWiki 知识库已被禁用（IWIKI_DISABLE=1）。"), true
	}
	if cfg.PaasID == "" || cfg.Token == "" {
		return iwikiStubTool(
			"iWiki 知识库未就绪：缺少 IWIKI_PAAS_ID/IWIKI_TOKEN；" +
				"请配置后重启服务，当前请改用 knowledge_search 或基于常识回答。"), true
	}

	client := &iwikiClient{
		url:      cfg.URL,
		paasID:   cfg.PaasID,
		token:    cfg.Token,
		spaceIDs: cfg.SpaceIDs,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	return newIWikiFuncTool(client, cfg.MaxResults), false
}

// iwikiSearchInput iwiki_search 工具入参。
type iwikiSearchInput struct {
	Query      string  `json:"query"       description:"检索内容，保留完整的问题或关键词短语"`
	MaxResults int     `json:"max_results" description:"返回条数，范围 1-20，留空默认 5"`
	MinScore   float64 `json:"min_score"   description:"相似度阈值（0-1），低于该分数的结果会被过滤，留空为 0"`
}

// iwikiSearchOutput iwiki_search 工具出参。
type iwikiSearchOutput struct {
	OK       bool         `json:"ok"`
	Query    string       `json:"query"`
	Count    int          `json:"count"`
	Results  []iwikiEntry `json:"results"`
	Warning  string       `json:"warning,omitempty"`
	ErrorMsg string       `json:"error,omitempty"`
}

// iwikiEntry 单条检索结果。
type iwikiEntry struct {
	Title    string            `json:"title"`
	URL      string            `json:"url,omitempty"`
	Score    float64           `json:"score"`
	Snippet  string            `json:"snippet"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// newIWikiFuncTool 构造真实的 iwiki_search 工具。
func newIWikiFuncTool(client *iwikiClient, defaultLimit int) tool.Tool {
	fn := func(ctx context.Context, in iwikiSearchInput) (*iwikiSearchOutput, error) {
		q := strings.TrimSpace(in.Query)
		if q == "" {
			return &iwikiSearchOutput{OK: false, ErrorMsg: "query 不能为空"}, nil
		}
		limit := in.MaxResults
		if limit <= 0 || limit > 20 {
			limit = defaultLimit
			if limit <= 0 {
				limit = 5
			}
		}

		resp, err := client.search(ctx, q, limit)
		if err != nil {
			return &iwikiSearchOutput{
				OK:       false,
				Query:    q,
				ErrorMsg: fmt.Sprintf("iWiki 检索失败：%v", err),
				Warning:  "可改用 knowledge_search（本地 runbook）或直接基于常识答复，并提示用户后端知识库不可用。",
			}, nil
		}

		out := &iwikiSearchOutput{OK: true, Query: q}
		for _, chunk := range resp.Data {
			if in.MinScore > 0 && chunk.Score < in.MinScore {
				continue
			}
			entry := iwikiEntry{
				Title:   chunk.Title,
				URL:     chunk.URL,
				Score:   chunk.Score,
				Snippet: snippet(chunk.Content, 400),
			}
			if chunk.Metadata != nil {
				md := make(map[string]string)
				for k, v := range chunk.Metadata {
					md[k] = fmt.Sprintf("%v", v)
				}
				entry.Metadata = md
			}
			out.Results = append(out.Results, entry)
		}
		out.Count = len(out.Results)

		if out.Count == 0 {
			out.Warning = "iWiki 未返回相关文档，建议改用 knowledge_search 或直接基于常识答复。"
		}
		return out, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName(IWikiToolName),
		function.WithDescription(
			"iWiki 云端知识库检索：用于查询公司内部 iWiki 上沉淀的架构文档、规范、SOP、历史方案。"+
				"适用场景：本地 knowledge_search 未命中，或问题明显属于跨团队/全公司范围的知识时。"+
				"调用时请给出完整问题或关键词短语，避免单字或缩写。"),
	)
}

// 确保 frameworkkb 包被引用（用于接口兼容性检查）
var _ = (*frameworkkb.SearchRequest)(nil)