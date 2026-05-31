// Package tapdapi 封装 TAPD 开放 API 客户端。
//
// D8 升级：从 Mock 骨架升级为真实 HTTP 客户端。
//   - 认证：HTTP Basic Auth（API User : API Password）
//   - 返回格式：{"status":1,"data":[...],"info":"success"}，status=1 表示成功
//   - 端点：GET  https://api.tapd.cn/bugs             查询缺陷
//           POST https://api.tapd.cn/bugs             创建缺陷（软写）
//
// 环境变量：
//   - TAPD_BASE_URL    （默认 https://api.tapd.cn）
//   - TAPD_USER        （API 用户名）
//   - TAPD_TOKEN       （API 密码 / App Secret）
//   - TAPD_WORKSPACE_ID（默认工作区）
//   - TAPD_API_MOCK    （"1"/"true" 强制 Mock）
package tapdapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// ErrMockMode 当前客户端处于 mock 模式。
var ErrMockMode = errors.New("tapd client in mock mode (TAPD_USER/TAPD_TOKEN 未配置或 TAPD_API_MOCK=1)")

// Client TAPD 客户端。
type Client struct {
	BaseURL     string
	User        string
	Token       string // API 秘钥（Basic auth 的 password）
	WorkspaceID string // 默认工作区
	Mock        bool
	httpClient  *http.Client
}

// Option 配置选项。
type Option func(*Client)

// WithBaseURL 覆盖默认 BaseURL。
func WithBaseURL(u string) Option { return func(c *Client) { c.BaseURL = u } }

// WithCredentials 显式指定凭据。
func WithCredentials(user, token string) Option {
	return func(c *Client) {
		c.User = user
		c.Token = token
	}
}

// WithWorkspaceID 设置默认工作区。
func WithWorkspaceID(id string) Option { return func(c *Client) { c.WorkspaceID = id } }

// WithTimeout 指定 HTTP 超时。
func WithTimeout(t time.Duration) Option { return func(c *Client) { c.httpClient.Timeout = t } }

// WithHTTPClient 注入自定义 HTTP Client（单测 httptest 用）。
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithMockMode 强制 Mock。
func WithMockMode(enabled bool) Option { return func(c *Client) { c.Mock = enabled } }

// NewClient 从环境变量构造。
func NewClient(opts ...Option) *Client {
	c := &Client{
		BaseURL:     firstNonEmpty(os.Getenv("TAPD_BASE_URL"), "https://api.tapd.cn"),
		User:        os.Getenv("TAPD_USER"),
		Token:       os.Getenv("TAPD_TOKEN"),
		WorkspaceID: os.Getenv("TAPD_WORKSPACE_ID"),
		httpClient:  &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	if isTruthy(os.Getenv("TAPD_API_MOCK")) {
		c.Mock = true
	}
	if c.User == "" || c.Token == "" || c.BaseURL == "" {
		c.Mock = true
	}
	return c
}

// IsMock 是否 mock 模式。
func (c *Client) IsMock() bool { return c.Mock }

// -----------------------------------------------------------------------------
// 通用 HTTP 请求（TAPD 响应外壳）
// -----------------------------------------------------------------------------

// envelope TAPD 接口统一外壳。
type envelope struct {
	Status int             `json:"status"`
	Data   json.RawMessage `json:"data"`
	Info   string          `json:"info"`
}

// doJSON 通用请求。GET 走 query string，POST 走 form-urlencoded（TAPD 官方格式）。
func (c *Client) doJSON(ctx context.Context, method, path string, params map[string]string, dataOut any) error {
	if c.Mock {
		return ErrMockMode
	}

	fullURL := strings.TrimRight(c.BaseURL, "/") + path
	var req *http.Request
	var err error

	switch method {
	case http.MethodGet:
		values := url.Values{}
		for k, v := range params {
			if v != "" {
				values.Set(k, v)
			}
		}
		if enc := values.Encode(); enc != "" {
			fullURL += "?" + enc
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	case http.MethodPost:
		values := url.Values{}
		for k, v := range params {
			if v != "" {
				values.Set(k, v)
			}
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, fullURL,
			bytes.NewReader([]byte(values.Encode())))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	default:
		return fmt.Errorf("unsupported method: %s", method)
	}
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(c.User, c.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tapd http %d: %s", resp.StatusCode, truncate(string(body), 512))
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("unmarshal envelope: %w; body=%s", err, truncate(string(body), 512))
	}
	if env.Status != 1 {
		return fmt.Errorf("tapd api status=%d info=%s", env.Status, env.Info)
	}
	if dataOut == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, dataOut); err != nil {
		return fmt.Errorf("unmarshal data: %w; data=%s", err, truncate(string(env.Data), 512))
	}
	return nil
}

// -----------------------------------------------------------------------------
// 领域方法
// -----------------------------------------------------------------------------

// Bug TAPD 缺陷精简模型（仅保留常用字段；实际返回字段远多于此，未列出字段会自动丢弃）。
type Bug struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Owner       string `json:"current_owner,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Severity    string `json:"severity,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Created     string `json:"created,omitempty"`
}

// bugQueryResp TAPD /bugs GET 返回的结构是 []{"Bug":{...}}。
type bugQueryResp []struct {
	Bug Bug `json:"Bug"`
}

// QueryBugsInput 查询缺陷入参。
type QueryBugsInput struct {
	WorkspaceID string
	Keyword     string // TAPD 使用 title 字段模糊匹配
	Status      string
	Owner       string
	Limit       int
}

// QueryBugs 查询 TAPD 缺陷。
// 对应 GET /bugs。
func (c *Client) QueryBugs(ctx context.Context, in QueryBugsInput) ([]Bug, error) {
	if c.Mock {
		return nil, ErrMockMode
	}
	ws := firstNonEmpty(in.WorkspaceID, c.WorkspaceID)
	if ws == "" {
		return nil, errors.New("workspace_id required")
	}

	params := map[string]string{"workspace_id": ws}
	if in.Keyword != "" {
		params["title"] = in.Keyword
	}
	if in.Status != "" {
		params["status"] = in.Status
	}
	if in.Owner != "" {
		params["current_owner"] = in.Owner
	}
	if in.Limit > 0 {
		params["limit"] = fmt.Sprintf("%d", in.Limit)
	}

	var raw bugQueryResp
	if err := c.doJSON(ctx, http.MethodGet, "/bugs", params, &raw); err != nil {
		return nil, err
	}
	out := make([]Bug, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.Bug)
	}
	return out, nil
}

// CreateBugInput 创建缺陷入参。
type CreateBugInput struct {
	WorkspaceID string
	Title       string
	Description string
	Owner       string
	Priority    string
	Severity    string
}

// CreateBug 创建 TAPD 缺陷（软写）。
// 对应 POST /bugs。
func (c *Client) CreateBug(ctx context.Context, in CreateBugInput) (*Bug, error) {
	if c.Mock {
		return nil, ErrMockMode
	}
	ws := firstNonEmpty(in.WorkspaceID, c.WorkspaceID)
	if ws == "" {
		return nil, errors.New("workspace_id required")
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("title required")
	}

	params := map[string]string{
		"workspace_id":  ws,
		"title":         in.Title,
		"description":   in.Description,
		"current_owner": in.Owner,
		"priority":      in.Priority,
		"severity":      in.Severity,
	}

	// TAPD 创建接口成功返回 {"Bug": {...}}，故用 wrapper 解析。
	var wrapper struct {
		Bug Bug `json:"Bug"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/bugs", params, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Bug, nil
}

// -----------------------------------------------------------------------------
// 工具函数
// -----------------------------------------------------------------------------

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}