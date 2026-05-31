// Package gongfengapi 封装工蜂 Git（TGit / git.woa.com）开放 API 客户端。
//
// D8 升级：从 Mock 骨架升级为可用于生产的真实 HTTP 客户端。
//   - 认证：PRIVATE-TOKEN Header（Personal Access Token / Project Token 均可）
//   - 端点：GET  /api/v3/projects/:id                              查询项目
//           GET  /api/v3/projects/:id/merge_requests/:iid          查询 MR
//           POST /api/v3/projects/:id/merge_requests               创建 MR
//           PUT  /api/v3/projects/:id/merge_requests/:iid/merge    合并 MR（严禁自动化）
//   - Mock：未配置 GONGFENG_TOKEN 时自动进入 Mock 模式，调用方使用 ErrMockMode 判定并转预置样例
//
// 环境变量：
//   - GONGFENG_BASE_URL   （默认 https://git.woa.com/api/v3）
//   - GONGFENG_TOKEN      （PRIVATE-TOKEN）
//   - GONGFENG_API_MOCK   （"1"/"true" 强制 Mock）
package gongfengapi

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

// 默认超时。
const defaultTimeout = 30 * time.Second

// ErrMockMode 返回给上层表示当前客户端处于 Mock 模式，调用方应转用自己的 mock 数据。
var ErrMockMode = errors.New("gongfeng client in mock mode (GONGFENG_TOKEN 未配置或 GONGFENG_API_MOCK=1)")

// Client 工蜂 Git 客户端。
type Client struct {
	BaseURL    string
	Token      string
	Mock       bool
	httpClient *http.Client
}

// Option 配置选项。
type Option func(*Client)

// WithBaseURL 覆盖默认 BaseURL。
func WithBaseURL(u string) Option {
	return func(c *Client) { c.BaseURL = u }
}

// WithToken 显式指定 Token（优先级高于环境变量）。
func WithToken(tok string) Option {
	return func(c *Client) { c.Token = tok }
}

// WithTimeout 指定 HTTP 超时。
func WithTimeout(t time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = t }
}

// WithHTTPClient 注入自定义 HTTP Client（用于单测 httptest）。
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithMockMode 强制启用 Mock 模式。
func WithMockMode(enabled bool) Option {
	return func(c *Client) { c.Mock = enabled }
}

// NewClient 从环境变量构造客户端；未配置凭据时进入 Mock 模式。
//
// 环境变量：
//   - GONGFENG_BASE_URL （默认 https://git.woa.com/api/v3）
//   - GONGFENG_TOKEN    （PRIVATE-TOKEN）
//   - GONGFENG_API_MOCK （"1"/"true" 强制 Mock）
func NewClient(opts ...Option) *Client {
	c := &Client{
		BaseURL:    firstNonEmpty(os.Getenv("GONGFENG_BASE_URL"), "https://git.woa.com/api/v3"),
		Token:      os.Getenv("GONGFENG_TOKEN"),
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	if isTruthy(os.Getenv("GONGFENG_API_MOCK")) {
		c.Mock = true
	}
	if c.Token == "" || c.BaseURL == "" {
		c.Mock = true
	}
	return c
}

// IsMock 是否处于 mock 模式。
func (c *Client) IsMock() bool { return c.Mock }

// -----------------------------------------------------------------------------
// 通用 HTTP 请求
// -----------------------------------------------------------------------------

// DoJSON 通用 JSON 请求方法。
//
// method：HTTP 方法，如 http.MethodGet/Post/Put。
// path  ：相对路径（不含 baseURL），示例 "/projects/123/merge_requests"。
// reqBody：请求体；传 nil 表示无 body。
// respOut：响应反序列化目标；传 nil 表示丢弃响应。
//
// Mock 模式下返回 ErrMockMode。
func (c *Client) DoJSON(ctx context.Context, method, path string, reqBody any, respOut any) error {
	if c.Mock {
		return ErrMockMode
	}

	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	fullURL := strings.TrimRight(c.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gongfeng http %d: %s", resp.StatusCode, truncate(string(body), 512))
	}
	if respOut == nil {
		return nil
	}
	if err := json.Unmarshal(body, respOut); err != nil {
		return fmt.Errorf("unmarshal response: %w; body=%s", err, truncate(string(body), 512))
	}
	return nil
}

// -----------------------------------------------------------------------------
// 领域方法（供 tools 层调用）
// -----------------------------------------------------------------------------

// MergeRequest 工蜂 MR 精简模型。
type MergeRequest struct {
	IID          int      `json:"iid"`
	Title        string   `json:"title"`
	State        string   `json:"state"`
	SourceBranch string   `json:"source_branch"`
	TargetBranch string   `json:"target_branch"`
	WebURL       string   `json:"web_url"`
	CreatedAt    string   `json:"created_at"`
	MergedAt     string   `json:"merged_at,omitempty"`
	Author       any      `json:"author,omitempty"`
	Reviewers    []string `json:"-"`
}

// CreateMRInput 创建 MR 入参。
type CreateMRInput struct {
	ProjectID    string
	SourceBranch string
	TargetBranch string
	Title        string
	Description  string
	Reviewers    []string
}

// CreateMR 创建 Merge Request。
//
// 对应 POST /projects/:id/merge_requests。
// Mock 模式返回 ErrMockMode，调用方应使用预置样例。
func (c *Client) CreateMR(ctx context.Context, in CreateMRInput) (*MergeRequest, error) {
	if c.Mock {
		return nil, ErrMockMode
	}
	if in.ProjectID == "" {
		return nil, errors.New("project_id required")
	}

	// 工蜂对接 GitLab v3 风格 API：`id` 支持 URL-encoded namespace/path
	projectPart := url.PathEscape(in.ProjectID)
	body := map[string]any{
		"source_branch": in.SourceBranch,
		"target_branch": in.TargetBranch,
		"title":         in.Title,
		"description":   in.Description,
	}
	if len(in.Reviewers) > 0 {
		body["reviewer_usernames"] = in.Reviewers
	}

	var mr MergeRequest
	if err := c.DoJSON(ctx, http.MethodPost,
		"/projects/"+projectPart+"/merge_requests", body, &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

// MergeMRInput 合并 MR 入参。
type MergeMRInput struct {
	ProjectID     string
	MRIid         int
	MergeMessage  string // 可选，合并提交 message
	SquashMessage string // 可选，squash 场景
}

// MergeMR 合并 Merge Request（最高危险，RepairAgent 不建议自动调用）。
// 对应 PUT /projects/:id/merge_requests/:iid/merge。
func (c *Client) MergeMR(ctx context.Context, in MergeMRInput) (*MergeRequest, error) {
	if c.Mock {
		return nil, ErrMockMode
	}
	if in.ProjectID == "" || in.MRIid <= 0 {
		return nil, errors.New("project_id & mr_iid required")
	}

	projectPart := url.PathEscape(in.ProjectID)
	body := map[string]any{}
	if in.MergeMessage != "" {
		body["merge_commit_message"] = in.MergeMessage
	}
	if in.SquashMessage != "" {
		body["squash_commit_message"] = in.SquashMessage
	}

	var mr MergeRequest
	if err := c.DoJSON(ctx, http.MethodPut,
		fmt.Sprintf("/projects/%s/merge_requests/%d/merge", projectPart, in.MRIid),
		body, &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

// GetMR 查询指定 MR。
func (c *Client) GetMR(ctx context.Context, projectID string, mrIid int) (*MergeRequest, error) {
	if c.Mock {
		return nil, ErrMockMode
	}
	projectPart := url.PathEscape(projectID)
	var mr MergeRequest
	if err := c.DoJSON(ctx, http.MethodGet,
		fmt.Sprintf("/projects/%s/merge_requests/%d", projectPart, mrIid),
		nil, &mr); err != nil {
		return nil, err
	}
	return &mr, nil
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