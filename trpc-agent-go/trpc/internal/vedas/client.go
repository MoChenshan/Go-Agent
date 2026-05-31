// Package vedas provides vedas go sdk client.
package vedas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
)

const (
	defaultMaxEventSize = 128 * 1024
	defaultVedasURL     = "http://api.open.vedas.woa.com"
)

// Client represents a vedas agent client
type Client struct {
	// token is the token for the vedas.
	// refer https://venus.woa.com/#/openapi/accountManage/personalAccount
	token string
	// appGroupID indicates user group id.
	// refer https://iwiki.woa.com/p/4014325318
	appGroupID int
	// planID indicates single chat plan id.
	// refer https://iwiki.woa.com/p/4014325318
	planID string
	// httpClient is the HTTP client.
	// if nil, http.DefaultClient will be used
	httpClient ihttp.HTTPClient
	// url is the default url for the vedas.
	// defaultVedasURL in default.
	// refer https://venus.woa.com/#/openapi/accountManage/personalAccount
	url string
	// maxEventSize is the maximum size of a scanner token when reading SSE lines.
	// if 0, defaultMaxEventSize will be used
	maxEventSize int
}

// NewClient creates a new vedas client with the given options
func NewClient(opts ...Option) *Client {
	client := &Client{
		maxEventSize: defaultMaxEventSize,
		url:          defaultVedasURL,
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// Option defines configuration options for the vedas client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(httpClient ihttp.HTTPClient) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithMaxEventSize sets the maximum size of the scanner token.
func WithMaxEventSize(size int) Option {
	return func(c *Client) {
		c.maxEventSize = size
	}
}

// WithAppGroupID sets the app group ID.
func WithAppGroupID(appGroupID int) Option {
	return func(c *Client) {
		c.appGroupID = appGroupID
	}
}

// WithToken sets the token for the vedas.
func WithToken(token string) Option {
	return func(c *Client) {
		c.token = token
	}
}

// CreatePlan creates a vedas plan.
func (c *Client) CreatePlan(ctx context.Context, req *CreatePlanRequest) (CommonResponse[CreatePlanResponse], error) {
	// set default app_group_id.
	req.AppGroupID = c.appGroupID
	resp := CommonResponse[CreatePlanResponse]{}
	if err := c.commonVedasCall(ctx, PlanCreateEndpoint, http.MethodPost, req, &resp, nil); err != nil {
		return CommonResponse[CreatePlanResponse]{}, err
	}
	if resp.Error() != nil {
		return CommonResponse[CreatePlanResponse]{}, resp.Error()
	}
	c.planID = resp.Data.PlanID
	return resp, nil
}

// QueryPlan queries a vedas plan.
func (c *Client) QueryPlan(ctx context.Context, req *PlanQueryRequest) (*PlanQueryResponse, error) {
	if c.planID == "" && req.PlanID == "" {
		return nil, fmt.Errorf("plan_id is required")
	}
	if c.planID != "" && req.PlanID == "" {
		req.PlanID = c.planID
	}
	resp := CommonResponse[PlanQueryResponse]{}
	if err := c.commonVedasCall(ctx, PlanQueryEndpoint, http.MethodGet, req, &resp, nil); err != nil {
		return nil, err
	}
	if resp.Error() != nil {
		return nil, resp.Error()
	}
	return &resp.Data, nil
}

// CreateAttachment creates a vedas attachment.
func (c *Client) CreateAttachment(ctx context.Context, req *AttachmentPresignRequest) (*AttachmentPresignResponse, error) {
	resp := CommonResponse[AttachmentPresignResponse]{}
	if err := c.commonVedasCall(ctx, AttachmentsPresignEndpoint, http.MethodPost, req, &resp, nil); err != nil {
		return nil, err
	}
	if resp.Error() != nil {
		return nil, resp.Error()
	}
	return &resp.Data, nil
}

const (
	uploadSuccess = 1
)

// CompleteAttachment completes a vedas attachment by update file status.
// upload_status: 1 for uploaded.
func (c *Client) CompleteAttachment(ctx context.Context, req *AttachmentUpdateRequest) (*AttachmentUpdateResponse, error) {
	resp := CommonResponse[AttachmentUpdateResponse]{}
	req.UploadStatus = uploadSuccess
	if err := c.commonVedasCall(ctx, AttachmentsStatusEndpoint, http.MethodPost, req, &resp, nil); err != nil {
		return nil, err
	}
	if resp.Error() != nil {
		return nil, resp.Error()
	}
	return &resp.Data, nil
}

// FileList lists vedas files.
func (c *Client) FileList(ctx context.Context, req *FileListRequest) (FileListResponse, error) {
	resp := CommonResponse[[]FileInfo]{}
	endPoint := fmt.Sprintf("%s?category=%s&plan_id=%s", PlanFilesEndpoint, req.Category, req.PlanID)
	if err := c.commonVedasCall(ctx, endPoint, http.MethodGet, req, &resp, nil); err != nil {
		return nil, err
	}
	if resp.Error() != nil {
		return nil, resp.Error()
	}
	return resp.Data, nil
}

// DownloadFile downloads a vedas file.
func (c *Client) DownloadFile(ctx context.Context, req *DownloadFileRequest) (io.ReadCloser, error) {
	if c.url == "" || c.token == "" {
		return nil, fmt.Errorf("url and token are required")
	}

	endPoint := fmt.Sprintf("%s?plan_id=%s&file_id=%s", PlanFilesDownloadEndpoint, req.PlanID, req.FileID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endPoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: HTTP %d - %s", resp.StatusCode, string(body))
	}
	return resp.Body, nil
}

// commonVedasCall is a common method for sending HTTP requests to the vedas API.
func (c *Client) commonVedasCall(
	ctx context.Context,
	endpoint, method string,
	reqBody, rspBody any,
	headers map[string]string,
) error {
	if c.url == "" || c.token == "" {
		return fmt.Errorf("url and token are required")
	}
	req, err := c.buildRequest(ctx, method, endpoint, headers, reqBody)
	if err != nil {
		return err
	}
	return c.sendRequest(req, rspBody)
}

func (c *Client) buildRequest(
	ctx context.Context,
	method, endpoint string,
	headers map[string]string,
	reqBody any,
) (*http.Request, error) {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	url := fmt.Sprintf("%s%s", c.url, endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	return httpReq, nil
}

// sendRequest sends an HTTP request to the vedas API.
func (c *Client) sendRequest(httpReq *http.Request, rspBody any) error {
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(rspBody); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}
