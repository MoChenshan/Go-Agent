
// Package bcsapi 提供 BCS（蓝鲸容器服务）HTTP API 客户端。
//
// 设计目标：
//   - 统一 BCS Gateway 鉴权（`Authorization: Bearer <token>` + `X-Project-ID`）
//   - 与 bkapi.Client 同样支持 Mock 模式，在未配置凭据时返回样例数据
//
// 环境变量：
//   - BCS_GATEWAY_URL      BCS Gateway 基础域名（如 https://bcs-api.xxx.com）
//   - BCS_TOKEN            BCS 访问 Token
//   - BCS_API_MOCK         "1"/"true" 强制启用 Mock 模式
package bcsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// Client BCS Gateway 客户端。
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	mockMode   bool
}

// Option 配置选项。
type Option func(*Client)

// WithBaseURL 覆盖默认 BaseURL。
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithToken 显式指定 BCS Token。
func WithToken(token string) Option { return func(c *Client) { c.token = token } }

// WithTimeout 指定超时。
func WithTimeout(t time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = t }
}

// WithMockMode 强制 Mock 模式。
func WithMockMode(enabled bool) Option { return func(c *Client) { c.mockMode = enabled } }

// NewClient 创建 Client，默认从环境变量读取。
func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:    os.Getenv("BCS_GATEWAY_URL"),
		token:      os.Getenv("BCS_TOKEN"),
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	if isTruthy(os.Getenv("BCS_API_MOCK")) {
		c.mockMode = true
	}
	if c.baseURL == "" || c.token == "" {
		c.mockMode = true
	}
	return c
}

// IsMock 返回是否 Mock 模式。
func (c *Client) IsMock() bool { return c.mockMode }

// Get 发送 GET 请求。query 会被追加为 URL 参数。
func (c *Client) Get(ctx context.Context, path string, query map[string]string, respOut any) error {
	if c.mockMode {
		return ErrMockMode
	}
	fullURL := strings.TrimRight(c.baseURL, "/") + path
	if len(query) > 0 {
		q := url.Values{}
		for k, v := range query {
			q.Set(k, v)
		}
		fullURL += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)
	return c.do(req, respOut)
}

// GetRaw 发送 GET 请求并返回原始响应字节（不做 JSON 解析）。
//
// 用途：K8s 的 pod logs / exec 等 API 返回 text/plain（甚至 octet-stream），
// 不能复用 Get 的 json.Unmarshal 路径。
//
// 行为契约：
//   - Mock 模式下返回 (nil, ErrMockMode)，调用方可自行判定 errors.Is 走伪造数据
//   - 非 2xx 响应按 Get 同款形态返错（含 status + 截断 body 便于排查）
//   - 返回的 bytes 已经全部读进内存（不是流），调用方无需关闭 io.ReadCloser
//   - maxBytes <= 0 表示不限制；> 0 时在内存里超限后立即截断并追加 "...(truncated)" 尾巴
//
// 为什么要加 maxBytes：Pod 日志可能上百 MB，全部读进内存会 OOM；LLM 上下文也装不下。
// 这里的截断是硬防线，上层 bcs_pod_logs_tail 还会在业务层再做一次友好截断（附元信息）。
func (c *Client) GetRaw(ctx context.Context, path string, query map[string]string, maxBytes int) ([]byte, error) {
	if c.mockMode {
		return nil, ErrMockMode
	}
	fullURL := strings.TrimRight(c.baseURL, "/") + path
	if len(query) > 0 {
		q := url.Values{}
		for k, v := range query {
			q.Set(k, v)
		}
		fullURL += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	// 对 text/plain 端点声明 Accept：K8s 的 logs API 兼容，且能避免某些代理强转 JSON
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "text/plain, */*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if maxBytes > 0 {
		// +1 是为了判断"是否真的被截断"：读到 maxBytes+1 字节说明超限
		reader = io.LimitReader(resp.Body, int64(maxBytes)+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bcs http %d: %s", resp.StatusCode, truncate(string(body), 512))
	}

	// 超限截断（注意是 > maxBytes 不是 >= maxBytes，因为 LimitReader 多读了 1 字节做判断）
	if maxBytes > 0 && len(body) > maxBytes {
		body = append(body[:maxBytes], []byte("\n...(truncated)")...)
	}
	return body, nil
}

// PostJSON 发送 JSON POST 请求。
func (c *Client) PostJSON(ctx context.Context, path string, reqBody any, respOut any) error {
	return c.sendJSON(ctx, http.MethodPost, path, reqBody, respOut, "")
}

// PutJSON 发送 JSON PUT 请求。
//
// 语义差别：PUT 表达"幂等写入最终状态"，与 POST 的"追加/触发"区分。
// scale 场景下 PUT /replicas 表示"把副本数设为目标值"（即使多次调用结果一致），
// 与 K8s 社区 API 惯例保持一致。
func (c *Client) PutJSON(ctx context.Context, path string, reqBody any, respOut any) error {
	return c.sendJSON(ctx, http.MethodPut, path, reqBody, respOut, "")
}

// DeleteJSON 发送 DELETE 请求，可选携带 JSON body。
//
// 部分 K8s API 要求 DELETE 携带 body（如 Eviction API 的 `policy/v1/Eviction` 对象、
// 带 gracePeriodSeconds 的 Pod DELETE）。当 reqBody 为 nil 时退化为普通 DELETE。
func (c *Client) DeleteJSON(ctx context.Context, path string, reqBody any, respOut any) error {
	return c.sendJSON(ctx, http.MethodDelete, path, reqBody, respOut, "")
}

// PatchJSON 发送 PATCH 请求，使用 Strategic Merge Patch（K8s 主流补丁格式）。
//
// 典型用途：rollout restart 通过给 deployment spec.template 打一个时间戳注解实现：
//
//	{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"<ts>"}}}}}
//
// Content-Type 切换为 `application/strategic-merge-patch+json`。
// 如需 JSON Patch（RFC 6902）或 Apply Patch，可另加方法；当前场景仅 strategic 足够。
func (c *Client) PatchJSON(ctx context.Context, path string, reqBody any, respOut any) error {
	return c.sendJSON(ctx, http.MethodPatch, path, reqBody, respOut, "application/strategic-merge-patch+json")
}

// sendJSON 内部通用 JSON 写请求工厂，抽出来避免 Post/Put/Delete/Patch 多份几乎一致的代码。
//
// contentTypeOverride 为空时使用默认 "application/json"；
// PATCH 场景需要指定 "application/strategic-merge-patch+json"。
func (c *Client) sendJSON(ctx context.Context, method, path string, reqBody any, respOut any, contentTypeOverride string) error {
	if c.mockMode {
		return ErrMockMode
	}
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		body = bytes.NewReader(b)
	}
	fullURL := strings.TrimRight(c.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)
	if contentTypeOverride != "" {
		req.Header.Set("Content-Type", contentTypeOverride)
	}
	return c.do(req, respOut)
}

// setAuth 设置鉴权头。
func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

// do 执行并解析。
func (c *Client) do(req *http.Request, respOut any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bcs http %d: %s", resp.StatusCode, truncate(string(body), 512))
	}
	if respOut == nil {
		return nil
	}
	if err := json.Unmarshal(body, respOut); err != nil {
		return fmt.Errorf("unmarshal: %w; body=%s", err, truncate(string(body), 512))
	}
	return nil
}

// ErrMockMode 表示客户端处于 Mock 模式。
var ErrMockMode = fmt.Errorf("bcsapi: running in mock mode (set BCS_GATEWAY_URL/BCS_TOKEN to enable real calls)")

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
