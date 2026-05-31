// Package devopsapi 封装蓝盾（BK-CI / DevOps）开放 API 客户端。
//
// D9 升级：从 Mock 骨架升级为可用于生产的真实 HTTP 客户端。
//   - 认证：X-DEVOPS-UID + X-DEVOPS-ACCESS-TOKEN（蓝盾 OpenAPI 标准头）
//     也支持单独 X-DEVOPS-ACCESS-TOKEN；若后端要求 Bearer 可由调用方设置。
//   - 端点：
//       GET  /ms/process/api/service/builds/:projectId/:pipelineId/history
//       POST /ms/process/api/service/builds/:projectId/:pipelineId/start
//       POST /ms/process/api/service/builds/:projectId/:pipelineId/:buildId/cancel
//   - Mock：未配置 DEVOPS_TOKEN 或 DEVOPS_API_MOCK=1 时进入 Mock 模式
//
// 环境变量：
//   - DEVOPS_BASE_URL（默认 https://devops.woa.com）
//   - DEVOPS_TOKEN   （X-DEVOPS-ACCESS-TOKEN）
//   - DEVOPS_USER    （X-DEVOPS-UID，可选；蓝盾 v4 之后逐步要求）
//   - DEVOPS_API_MOCK（"1"/"true" 强制 Mock）
package devopsapi

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
var ErrMockMode = errors.New("devops client in mock mode (DEVOPS_TOKEN 未配置或 DEVOPS_API_MOCK=1)")

// Client 蓝盾 BK-CI 客户端。
type Client struct {
	BaseURL    string
	Token      string // X-DEVOPS-ACCESS-TOKEN
	User       string // X-DEVOPS-UID（可选）
	Mock       bool
	httpClient *http.Client
}

// Option 配置选项。
type Option func(*Client)

// WithBaseURL 覆盖默认 BaseURL。
func WithBaseURL(u string) Option { return func(c *Client) { c.BaseURL = u } }

// WithToken 显式指定 Token。
func WithToken(tok string) Option { return func(c *Client) { c.Token = tok } }

// WithUser 显式指定操作人（X-DEVOPS-UID）。
func WithUser(uid string) Option { return func(c *Client) { c.User = uid } }

// WithTimeout 指定 HTTP 超时。
func WithTimeout(t time.Duration) Option { return func(c *Client) { c.httpClient.Timeout = t } }

// WithHTTPClient 注入自定义 HTTP Client（单测 httptest 用）。
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithMockMode 强制 Mock。
func WithMockMode(enabled bool) Option { return func(c *Client) { c.Mock = enabled } }

// NewClient 从环境变量构造；未配置凭据进入 Mock。
func NewClient(opts ...Option) *Client {
	c := &Client{
		BaseURL:    firstNonEmpty(os.Getenv("DEVOPS_BASE_URL"), "https://devops.woa.com"),
		Token:      os.Getenv("DEVOPS_TOKEN"),
		User:       os.Getenv("DEVOPS_USER"),
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	if isTruthy(os.Getenv("DEVOPS_API_MOCK")) {
		c.Mock = true
	}
	if c.Token == "" || c.BaseURL == "" {
		c.Mock = true
	}
	return c
}

// IsMock 是否 mock 模式。
func (c *Client) IsMock() bool { return c.Mock }

// -----------------------------------------------------------------------------
// 通用 HTTP
// -----------------------------------------------------------------------------

// devopsEnvelope 蓝盾标准外壳：{"status":0, "message":"", "data":...}
// status==0 表示成功；非 0 视为业务错误。
type devopsEnvelope struct {
	Status  int             `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// DoJSON 通用 JSON 请求。
func (c *Client) DoJSON(ctx context.Context, method, path string, query url.Values, reqBody any, dataOut any) error {
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
	if enc := query.Encode(); enc != "" {
		fullURL += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-DEVOPS-ACCESS-TOKEN", c.Token)
	if c.User != "" {
		req.Header.Set("X-DEVOPS-UID", c.User)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("devops http %d: %s", resp.StatusCode, truncate(string(body), 512))
	}
	// 蓝盾返回一般为 envelope；若不是也兼容直接透传。
	var env devopsEnvelope
	if err := json.Unmarshal(body, &env); err == nil && (env.Status != 0 || len(env.Data) > 0 || env.Message != "") {
		if env.Status != 0 {
			return fmt.Errorf("devops api status=%d message=%s", env.Status, env.Message)
		}
		if dataOut == nil || len(env.Data) == 0 {
			return nil
		}
		if err := json.Unmarshal(env.Data, dataOut); err != nil {
			return fmt.Errorf("unmarshal data: %w; data=%s", err, truncate(string(env.Data), 512))
		}
		return nil
	}
	// 非 envelope 的简单响应：直接反序列化
	if dataOut == nil {
		return nil
	}
	if err := json.Unmarshal(body, dataOut); err != nil {
		return fmt.Errorf("unmarshal response: %w; body=%s", err, truncate(string(body), 512))
	}
	return nil
}

// -----------------------------------------------------------------------------
// 领域方法
// -----------------------------------------------------------------------------

// Build 构建记录精简模型。
type Build struct {
	BuildID     string `json:"id"`
	BuildNum    int    `json:"buildNum,omitempty"`
	Status      string `json:"status"`
	StartTime   int64  `json:"startTime,omitempty"`
	EndTime     int64  `json:"endTime,omitempty"`
	StartUser   string `json:"startUser,omitempty"`
	TriggerType string `json:"trigger,omitempty"`
}

// BuildHistoryInput 查询构建历史入参。
type BuildHistoryInput struct {
	ProjectID  string
	PipelineID string
	Limit      int
}

// BuildHistory 查询流水线构建历史。
// 对应 GET /ms/process/api/service/builds/:projectId/:pipelineId/history
func (c *Client) BuildHistory(ctx context.Context, in BuildHistoryInput) ([]Build, error) {
	if c.Mock {
		return nil, ErrMockMode
	}
	if in.ProjectID == "" || in.PipelineID == "" {
		return nil, errors.New("project_id & pipeline_id required")
	}
	q := url.Values{}
	if in.Limit > 0 {
		q.Set("pageSize", fmt.Sprintf("%d", in.Limit))
	}
	// 蓝盾历史接口 data 可能是 {"records":[...]} 或者 [...]；两种都尽力兼容
	var wrapper struct {
		Records []Build `json:"records"`
	}
	path := fmt.Sprintf("/ms/process/api/service/builds/%s/%s/history",
		url.PathEscape(in.ProjectID), url.PathEscape(in.PipelineID))
	if err := c.DoJSON(ctx, http.MethodGet, path, q, nil, &wrapper); err == nil && len(wrapper.Records) > 0 {
		return wrapper.Records, nil
	}
	// 退化：直接尝试 []Build
	var list []Build
	if err := c.DoJSON(ctx, http.MethodGet, path, q, nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// PipelineStartInput 触发/重跑流水线入参。
type PipelineStartInput struct {
	ProjectID  string
	PipelineID string
	BuildID    string // 可选，作为重跑的基准
	Params     map[string]string
}

// PipelineStartResult 触发结果。
type PipelineStartResult struct {
	BuildID string `json:"id"`
	Status  string `json:"status,omitempty"`
}

// PipelineStart 触发/重跑流水线。
// 对应 POST /ms/process/api/service/builds/:projectId/:pipelineId/start
func (c *Client) PipelineStart(ctx context.Context, in PipelineStartInput) (*PipelineStartResult, error) {
	if c.Mock {
		return nil, ErrMockMode
	}
	if in.ProjectID == "" || in.PipelineID == "" {
		return nil, errors.New("project_id & pipeline_id required")
	}

	body := map[string]any{}
	if len(in.Params) > 0 {
		body["params"] = in.Params
	}
	if in.BuildID != "" {
		body["buildId"] = in.BuildID
	}

	q := url.Values{}
	if in.BuildID != "" {
		q.Set("retryStart", "true")
	}

	path := fmt.Sprintf("/ms/process/api/service/builds/%s/%s/start",
		url.PathEscape(in.ProjectID), url.PathEscape(in.PipelineID))
	var result PipelineStartResult
	if err := c.DoJSON(ctx, http.MethodPost, path, q, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// BuildCancel 取消指定构建。
// 对应 POST /ms/process/api/service/builds/:projectId/:pipelineId/:buildId/cancel
func (c *Client) BuildCancel(ctx context.Context, projectID, pipelineID, buildID string) error {
	if c.Mock {
		return ErrMockMode
	}
	if projectID == "" || pipelineID == "" || buildID == "" {
		return errors.New("project_id / pipeline_id / build_id required")
	}
	path := fmt.Sprintf("/ms/process/api/service/builds/%s/%s/%s/cancel",
		url.PathEscape(projectID), url.PathEscape(pipelineID), url.PathEscape(buildID))
	return c.DoJSON(ctx, http.MethodPost, path, nil, nil, nil)
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