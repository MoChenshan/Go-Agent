
// Package bkapi 提供蓝鲸 API Gateway（APIGW）的通用 HTTP 客户端。
//
// 设计目标：
//   - 统一 `X-Bkapi-Authorization` 鉴权头构造（bk_app_code + bk_app_secret）。
//   - 提供 Mock 模式：未配置凭据时返回预置样例数据，方便本地/离线开发。
//   - 提供统一的请求/响应封装（JSON Body + 超时 + 错误归一化）。
//
// 参考实现：oncall_agent/infrastructure/external/http/galileo/galileo_impl.go
//
// 环境变量：
//   - BK_APP_CODE        蓝鲸应用 ID
//   - BK_APP_SECRET      蓝鲸应用密钥
//   - BK_APIGW_BASE_URL  蓝鲸 APIGW 基础域名（如 https://bkapi.paas.woa.com）
//   - BK_API_MOCK        设置为 "1"/"true" 则强制走 Mock 模式
package bkapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// 默认超时。
const defaultTimeout = 30 * time.Second

// Client 蓝鲸 APIGW 客户端。
type Client struct {
	baseURL    string
	appCode    string
	appSecret  string
	httpClient *http.Client
	mockMode   bool
}

// Option 配置选项。
type Option func(*Client)

// WithBaseURL 覆盖默认 BaseURL。
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

// WithCredentials 显式指定凭据（优先级高于环境变量）。
func WithCredentials(appCode, appSecret string) Option {
	return func(c *Client) {
		c.appCode = appCode
		c.appSecret = appSecret
	}
}

// WithTimeout 指定 HTTP 超时。
func WithTimeout(t time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = t }
}

// WithMockMode 强制启用 Mock 模式。
func WithMockMode(enabled bool) Option {
	return func(c *Client) { c.mockMode = enabled }
}

// NewClient 创建 Client。
//
// 默认从环境变量读取凭据；如果 BK_APP_CODE 或 BK_APP_SECRET 任一为空，
// 或 BK_API_MOCK=1，则自动启用 Mock 模式。
func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:   os.Getenv("BK_APIGW_BASE_URL"),
		appCode:   os.Getenv("BK_APP_CODE"),
		appSecret: os.Getenv("BK_APP_SECRET"),
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
	for _, o := range opts {
		o(c)
	}
	// 自动判定 Mock 模式
	if isTruthy(os.Getenv("BK_API_MOCK")) {
		c.mockMode = true
	}
	if c.appCode == "" || c.appSecret == "" || c.baseURL == "" {
		c.mockMode = true
	}
	return c
}

// IsMock 返回当前是否为 Mock 模式。
func (c *Client) IsMock() bool {
	return c.mockMode
}

// GetJSON 发送 GET 请求，query 参数以 map 形式传入；respOut 接收反序列化结果。
//
// Mock 模式下返回 ErrMockMode，调用方应使用 Mock 数据兜底。
func (c *Client) GetJSON(ctx context.Context, path string, query map[string]string, respOut any) error {
	if c.mockMode {
		return ErrMockMode
	}
	fullURL := strings.TrimRight(c.baseURL, "/") + path
	if len(query) > 0 {
		parts := make([]string, 0, len(query))
		for k, v := range query {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		sep := "?"
		if strings.Contains(fullURL, "?") {
			sep = "&"
		}
		fullURL += sep + strings.Join(parts, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setAuthHeaders(req)
	return c.do(req, respOut)
}

// PostJSON 发送 JSON POST 请求并解析响应。
//
// path：相对路径（不含 baseURL），示例："/api/bk-monitor/prod/metrics/query"。
// reqBody：请求体（会被 json.Marshal）；可以为 nil。
// respOut：响应反序列化目标；传 nil 表示丢弃响应。
//
// Mock 模式下返回 ErrMockMode，调用方应使用 Mock 数据兜底。
func (c *Client) PostJSON(ctx context.Context, path string, reqBody any, respOut any) error {
	return c.sendJSON(ctx, http.MethodPost, path, reqBody, respOut)
}

// PutJSON 发送 JSON PUT 请求。
//
// 语义：幂等写入目标状态（与 POST 的"追加/触发"区分）。
// 告警静默场景典型使用：PUT /silence/{id}（如有）更新静默窗口。
func (c *Client) PutJSON(ctx context.Context, path string, reqBody any, respOut any) error {
	return c.sendJSON(ctx, http.MethodPut, path, reqBody, respOut)
}

// DeleteJSON 发送 DELETE 请求，可选携带 JSON body。
//
// 告警静默场景典型使用：DELETE /silence/{id} 或 POST /silence/cancel（按后端实际路径）。
func (c *Client) DeleteJSON(ctx context.Context, path string, reqBody any, respOut any) error {
	return c.sendJSON(ctx, http.MethodDelete, path, reqBody, respOut)
}

// sendJSON 通用 JSON 写请求工厂，抽出避免 PostJSON/PutJSON/DeleteJSON 三份重复代码。
func (c *Client) sendJSON(ctx context.Context, method, path string, reqBody any, respOut any) error {
	if c.mockMode {
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
	fullURL := strings.TrimRight(c.baseURL, "/") + path
	httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setAuthHeaders(httpReq)
	return c.do(httpReq, respOut)
}

// do 实际执行 HTTP 请求并处理响应体/状态码。
func (c *Client) do(req *http.Request, respOut any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bkapi http %d: %s", resp.StatusCode, truncate(string(body), 512))
	}
	if respOut == nil {
		return nil
	}
	if err := json.Unmarshal(body, respOut); err != nil {
		return fmt.Errorf("unmarshal response: %w; body=%s", err, truncate(string(body), 512))
	}
	return nil
}

// setAuthHeaders 设置蓝鲸 APIGW 鉴权头。
//
// 认证格式：X-Bkapi-Authorization: {"bk_app_code":"xxx","bk_app_secret":"yyy"}
func (c *Client) setAuthHeaders(req *http.Request) {
	auth := map[string]string{
		"bk_app_code":   c.appCode,
		"bk_app_secret": c.appSecret,
	}
	authJSON, _ := json.Marshal(auth)
	req.Header.Set("X-Bkapi-Authorization", string(authJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

// ErrMockMode 表示当前客户端处于 Mock 模式，调用方应使用 Mock 数据兜底。
var ErrMockMode = fmt.Errorf("bkapi: running in mock mode (set BK_APP_CODE/BK_APP_SECRET/BK_APIGW_BASE_URL to enable real calls)")

// isTruthy 判断字符串是否表示 true。
func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// truncate 截断字符串。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
