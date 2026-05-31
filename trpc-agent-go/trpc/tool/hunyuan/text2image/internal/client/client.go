package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"
)

const (
	defaultModel   = "hunyuan-image-v3.0-v1.0.4"
	defaultPath    = "/openapi/v1/images/ar/generations"
	defaultBaseURL = "http://hunyuanapi.woa.com"

	defaultTimeout = time.Minute
)

// Client is the client for the Hunyuan text2image API
type Client struct {
	baseURL       string
	apiKey        string
	path          string
	model         string
	httpClient    *http.Client
	customHeaders map[string]string
}

// NewClient creates a new client
func NewClient(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}

	cfg := Options{
		baseURL: defaultBaseURL,
		model:   defaultModel,
		path:    defaultPath,
		timeout: defaultTimeout,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	c := &Client{
		apiKey:  apiKey,
		baseURL: cfg.baseURL,
		path:    cfg.path,
		model:   cfg.model,
		customHeaders: map[string]string{
			"Content-Type":  "application/json",
			"Authorization": fmt.Sprintf("Bearer %s", apiKey),
		},
	}
	c.httpClient = &http.Client{
		Timeout: cfg.timeout,
	}
	return c, nil
}

// Generate generates images
func (c *Client) Generate(ctx context.Context, baseData *BaseImageRequest) (*ImageResponse, error) {
	data := &ImageRequest{
		BaseImageRequest: *baseData,
	}
	data.Model = c.model

	req, err := c.initPostReq(ctx, data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to init post req: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	imageResp, err := parseImageResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return imageResp, nil

}

func (c *Client) initPostReq(ctx context.Context, data any, headers map[string]string) (*http.Request, error) {
	reqURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	reqURL.Path = path.Join(c.path)

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(req, headers)
	return req, nil
}

// setHeaders sets the request headers
func (c *Client) setHeaders(req *http.Request, additionalHeaders map[string]string) {
	// Set custom headers
	for k, v := range c.customHeaders {
		req.Header.Set(k, v)
	}

	// Set additional headers
	for k, v := range additionalHeaders {
		req.Header.Set(k, v)
	}
}

func parseImageResponse(resp *http.Response) (*ImageResponse, error) {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var imageResp ImageResponse
	if err := json.Unmarshal(body, &imageResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &imageResp, nil
}
