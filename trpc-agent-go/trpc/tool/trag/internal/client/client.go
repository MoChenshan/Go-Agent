package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	defaultBaseURL             = "http://api.trag.woa.com"
	defaultTimeout             = 30 * time.Second
	defaultMaxRetry            = 3
	defaultRetryInterval       = 5 * time.Second
	defaultFunctionExecTimeout = 5 * time.Minute
	envBaseURL                 = "TRAG_BASE_URL"
)

// ClientOption is a function that configures a Client.
type ClientOption func(*Client)

// WithTimeout sets the HTTP client timeout for requests.
// Default timeout is 30 seconds if not specified.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// WithBaseURL sets the base URL for the TRAG API.
// If not specified, defaults to the TRAG_BASE_URL environment variable
// or "http://api.trag.woa.com" if the environment variable is not set.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithMaxRetry sets the maximum number of retry attempts for failed requests.
// Default is 3 retries if not specified.
func WithMaxRetry(maxRetry int) ClientOption {
	return func(c *Client) {
		c.maxRetry = maxRetry
	}
}

// WithRetryInterval sets the duration to wait between retry attempts.
// Default interval is 5 seconds if not specified.
func WithRetryInterval(interval time.Duration) ClientOption {
	return func(c *Client) {
		c.retryInterval = interval
	}
}

// WithCustomHeaders sets custom HTTP headers to be included in all requests.
// These headers are merged with the default headers (Authorization, User-Agent, Content-Type).
func WithCustomHeaders(headers map[string]string) ClientOption {
	return func(c *Client) {
		for k, v := range headers {
			c.customHeaders[k] = v
		}
	}
}

// WithFunctionExecTimeout sets the timeout for function execution.
// This is the maximum time to wait for a function to complete after submission.
// Default is 5 minutes if not specified.
func WithFunctionExecTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.functionExecTimeout = timeout
	}
}

// Client is an HTTP client for interacting with the TRAG API.
// It provides methods for function execution, file management, and tool discovery.
// The client includes automatic retry logic and customizable timeouts.
type Client struct {
	apiKey              string
	baseURL             string
	timeout             time.Duration
	maxRetry            int
	retryInterval       time.Duration
	functionExecTimeout time.Duration
	customHeaders       map[string]string
	httpClient          *http.Client
}

// NewClient creates a new TRAG API client with the specified API key and options.
//
// The API key is required for authentication. The base URL can be configured via
// WithBaseURL option or the TRAG_BASE_URL environment variable.
//
// Example:
//
//	client, err := client.NewClient("your-api-key",
//		client.WithTimeout(60*time.Second),
//		client.WithMaxRetry(5),
//	)
//
// Returns an error if the API key is empty.
func NewClient(apiKey string, opts ...ClientOption) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}

	baseURL := os.Getenv(envBaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	c := &Client{
		apiKey:              apiKey,
		baseURL:             baseURL,
		timeout:             defaultTimeout,
		maxRetry:            defaultMaxRetry,
		retryInterval:       defaultRetryInterval,
		functionExecTimeout: defaultFunctionExecTimeout,
		customHeaders: map[string]string{
			"Content-Type": "application/json",
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	c.httpClient = &http.Client{
		Timeout: c.timeout,
	}

	return c, nil
}

// get performs an HTTP GET request with retry logic
func (c *Client) get(ctx context.Context, uri string, params map[string]string, headers map[string]string) (*TragResponse, error) {
	reqURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	reqURL.Path = filepath.Join(reqURL.Path, uri)

	// Add query parameters
	if len(params) > 0 {
		q := reqURL.Query()
		for k, v := range params {
			if v != "" {
				q.Add(k, v)
			}
		}
		reqURL.RawQuery = q.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetry; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		c.setHeaders(req, headers)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < c.maxRetry {
				time.Sleep(c.retryInterval)
				continue
			}
			return nil, fmt.Errorf("request failed after %d retries: %w", c.maxRetry, err)
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			if attempt < c.maxRetry {
				time.Sleep(c.retryInterval)
				continue
			}
			return nil, fmt.Errorf("request failed after %d retries: %w", c.maxRetry, lastErr)
		}

		var tragResp TragResponse
		if err := json.Unmarshal(body, &tragResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		return &tragResp, nil
	}

	return nil, lastErr
}

// post performs an HTTP POST request with retry logic
func (c *Client) post(ctx context.Context, uri string, data any, headers map[string]string) (*TragResponse, error) {
	reqURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	reqURL.Path = filepath.Join(reqURL.Path, uri)

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request data: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetry; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		c.setHeaders(req, headers)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < c.maxRetry {
				time.Sleep(c.retryInterval)
				continue
			}
			return nil, fmt.Errorf("request failed after %d retries: %w", c.maxRetry, err)
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			if attempt < c.maxRetry {
				time.Sleep(c.retryInterval)
				continue
			}
			return nil, fmt.Errorf("request failed after %d retries: %w", c.maxRetry, lastErr)
		}

		var tragResp TragResponse
		if err := json.Unmarshal(body, &tragResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		return &tragResp, nil
	}

	return nil, lastErr
}

// setHeaders sets the request headers
func (c *Client) setHeaders(req *http.Request, additionalHeaders map[string]string) {
	// Set system headers
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("User-Agent", "trag-go-sdk")

	// Set custom headers
	for k, v := range c.customHeaders {
		req.Header.Set(k, v)
	}

	// Set additional headers
	for k, v := range additionalHeaders {
		req.Header.Set(k, v)
	}
}

// QueryFunctionMeta queries function metadata from the TRAG platform.
//
// Parameters:
//   - ctx: Context for the request
//   - toolsCode: The toolset code/name to query
//   - functionNames: List of specific function names to query, or nil/empty for all functions
//
// Returns function definitions containing metadata such as parameters, return types,
// and descriptions. Returns an error if no functions are found or the request fails.
func (c *Client) QueryFunctionMeta(ctx context.Context, toolsCode string, functionNames []string) ([]*FunctionDefinition, error) {
	uri := "/v1/tragtools/plugin/function/list"

	data := map[string]any{
		"functionNameList": functionNames,
		"toolsCode":        toolsCode,
	}

	resp, err := c.post(ctx, uri, data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query function meta: %w", err)
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("query function meta failed: code=%d, msg=%s, traceId=%s", resp.Code, resp.Message, resp.TraceID)
	}

	var metaResp FunctionMetaResponse
	if err := json.Unmarshal(resp.Data, &metaResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal function meta response: %w", err)
	}

	if len(metaResp.Functions) == 0 {
		return nil, fmt.Errorf("no functions found for toolsCode=%s, functionNames=%v", toolsCode, functionNames)
	}

	result := make([]*FunctionDefinition, 0, len(metaResp.Functions))
	for _, fn := range metaResp.Functions {
		funcName, _ := fn["functionName"].(string)
		description, _ := fn["description"].(string)
		paramType, _ := fn["parameterType"].(map[string]any)
		returnType, _ := fn["returnType"].(map[string]any)

		toolVersionID := fmt.Sprintf("%d/%d", metaResp.ToolsID, metaResp.Version)
		fullName := fmt.Sprintf("%s/%s", toolVersionID, funcName)

		def := &FunctionDefinition{
			ToolsID:       metaResp.ToolsID,
			ToolsVersion:  metaResp.Version,
			ToolVersionID: toolVersionID,
			FunctionName:  funcName,
			FullName:      fullName,
			ParameterType: paramType,
			ReturnType:    returnType,
			Description:   description,
		}
		result = append(result, def)
	}

	return result, nil
}

// SubmitFunctionRequest submits a function execution request to TRAG.
//
// Parameters:
//   - ctx: Context for the request
//   - functionName: Full function name in format "toolsId/version/functionName"
//   - params: Function parameters as a map
//
// Returns a response containing the task ID for tracking execution status.
// Use QueryFunctionExecutionStatus or RetrieveFunctionResult to get the result.
func (c *Client) SubmitFunctionRequest(ctx context.Context, functionName string, params map[string]any) (*TragResponse, error) {
	uri := "/v1/tragtools/task/submit"

	data := map[string]any{
		"toolsVersionFunction": functionName,
		"params":               params,
	}

	return c.post(ctx, uri, data, nil)
}

// QueryFunctionExecutionStatus queries the current execution status of a submitted function.
//
// Parameters:
//   - ctx: Context for the request
//   - taskID: The task ID returned from SubmitFunctionRequest
//
// Returns the execution status (init, running, success, failed, or timeout).
func (c *Client) QueryFunctionExecutionStatus(ctx context.Context, taskID string) (*TragResponse, error) {
	uri := "/v1/tragtools/task/query"
	params := map[string]string{
		"taskId": taskID,
	}

	return c.get(ctx, uri, params, nil)
}

// QueryFunctionExecutionResult queries the execution result of a completed function.
//
// Parameters:
//   - ctx: Context for the request
//   - taskID: The task ID returned from SubmitFunctionRequest
//
// This method should only be called after the function has finished executing.
// Use QueryFunctionExecutionStatus to check if execution is complete, or use
// RetrieveFunctionResult to automatically wait for completion.
func (c *Client) QueryFunctionExecutionResult(ctx context.Context, taskID string) (*TragResponse, error) {
	uri := "/v1/tragtools/task/result/retrieve"
	params := map[string]string{
		"taskId": taskID,
	}

	return c.get(ctx, uri, params, nil)
}

// RetrieveFunctionResult waits for a function to complete and retrieves its result.
//
// This is a convenience method that combines status polling with result retrieval.
// It continuously polls the execution status every second until the function completes
// or the timeout is reached.
//
// Parameters:
//   - ctx: Context for the request (can be used for cancellation)
//   - taskID: The task ID returned from SubmitFunctionRequest
//   - timeout: Maximum time to wait for completion (0 means no timeout)
//
// Returns the function result once execution completes, or an error if the timeout
// is exceeded or execution fails.
func (c *Client) RetrieveFunctionResult(ctx context.Context, taskID string, timeout time.Duration) (*TragResponse, error) {
	startTime := time.Now()

	for {
		if timeout > 0 && time.Since(startTime) > timeout {
			return nil, fmt.Errorf("function execution timeout")
		}

		statusResp, err := c.QueryFunctionExecutionStatus(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("failed to query function status: %w", err)
		}

		if !statusResp.IsSuccess() {
			return nil, fmt.Errorf("query status failed: %s", statusResp.Message)
		}

		var statusData FunctionStatusData
		if err := json.Unmarshal(statusResp.Data, &statusData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal status data: %w", err)
		}

		if statusData.Status.IsFinished() {
			return c.QueryFunctionExecutionResult(ctx, taskID)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(1 * time.Second):
			// Continue waiting
		}
	}
}

// UploadFile uploads a file to the TRAG platform.
//
// The upload process involves three steps:
//  1. Request an upload URL from TRAG
//  2. Upload the file content to the provided URL
//  3. Report the upload status back to TRAG
//
// Parameters:
//   - ctx: Context for the request
//   - fileName: Name of the file to upload
//   - filePath: Directory path containing the file
//   - fileType: MIME type of the file (use FileContentType constants)
//   - fileSource: Source of the file (FileSourceUserUpload or FileSourceUserOutput)
//   - operator: Operator/user identifier (defaults to "anonymous" if empty)
//
// Returns the file ID assigned by TRAG, which can be used for download or deletion.
func (c *Client) UploadFile(ctx context.Context, fileName, filePath string, fileType FileContentType, fileSource FileSource, operator string) (string, error) {
	if operator == "" {
		operator = "anonymous"
	}

	fullFilePath := filepath.Join(filePath, fileName)
	fileInfo, err := os.Stat(fullFilePath)
	if err != nil {
		return "", fmt.Errorf("file not found: %w", err)
	}

	// Get upload URL
	uploadResp, err := c.genUploadFileURL(ctx, fileName, fileInfo.Size(), fileType, fileSource, operator)
	if err != nil {
		return "", fmt.Errorf("failed to get upload URL: %w", err)
	}

	if !uploadResp.IsSuccess() {
		return "", fmt.Errorf("get upload URL failed: code=%d, msg=%s", uploadResp.Code, uploadResp.Message)
	}

	var uploadData UploadFileResponse
	if err := json.Unmarshal(uploadResp.Data, &uploadData); err != nil {
		return "", fmt.Errorf("failed to unmarshal upload response: %w", err)
	}

	// Upload file to URL
	file, err := os.Open(fullFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadData.UploadURL, file)
	if err != nil {
		return "", fmt.Errorf("failed to create PUT request: %w", err)
	}

	putResp, err := c.httpClient.Do(putReq)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(putResp.Body)
		return "", fmt.Errorf("file upload failed: HTTP %d: %s", putResp.StatusCode, string(body))
	}

	// Report file status
	reportResp, err := c.reportFileStatus(ctx, uploadData.FileID)
	if err != nil {
		return "", fmt.Errorf("failed to report file status: %w", err)
	}

	if !reportResp.IsSuccess() {
		return "", fmt.Errorf("report file status failed: code=%d, msg=%s", reportResp.Code, reportResp.Message)
	}

	return uploadData.FileID, nil
}

// genUploadFileURL generates a URL for file upload
func (c *Client) genUploadFileURL(ctx context.Context, fileName string, fileSize int64, fileType FileContentType, fileSource FileSource, operator string) (*TragResponse, error) {
	uri := "/v1/tragtools/task/dispatcher/file/upload"

	data := map[string]any{
		"operator":    operator,
		"fileName":    fileName,
		"fileSize":    fileSize,
		"fileSource":  string(fileSource),
		"contentType": string(fileType),
	}

	return c.post(ctx, uri, data, nil)
}

// reportFileStatus reports the file upload status
func (c *Client) reportFileStatus(ctx context.Context, fileID string) (*TragResponse, error) {
	uri := "/v1/tragtools/task/dispatcher/file/report"
	params := map[string]string{
		"fileId": fileID,
	}

	return c.get(ctx, uri, params, nil)
}

// DeleteFile deletes a file from the TRAG platform.
//
// Parameters:
//   - ctx: Context for the request
//   - fileID: The file ID returned from UploadFile
//
// Returns an error if the file doesn't exist or deletion fails.
func (c *Client) DeleteFile(ctx context.Context, fileID string) (*TragResponse, error) {
	uri := "/v1/tragtools/task/dispatcher/file/delete"
	params := map[string]string{
		"fileId": fileID,
	}

	// Using POST with params in URL
	reqURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	reqURL.Path = filepath.Join(reqURL.Path, uri)
	q := reqURL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	reqURL.RawQuery = q.Encode()

	return c.post(ctx, reqURL.String(), map[string]any{}, nil)
}

// DownloadFile downloads a file from the TRAG platform to local storage.
//
// The method first obtains a temporary download URL from TRAG, then downloads
// the file content and saves it to the specified path.
//
// Parameters:
//   - ctx: Context for the request
//   - fileID: The file ID returned from UploadFile
//   - savePath: Directory path where the file should be saved
//   - expireMs: Download URL expiration time in milliseconds (defaults to 3600000 = 1 hour if 0)
//
// Returns the full path to the downloaded file.
func (c *Client) DownloadFile(ctx context.Context, fileID, savePath string, expireMs int64) (string, error) {
	if expireMs == 0 {
		expireMs = 3600000 // Default 1 hour
	}

	downloadResp, err := c.GetFileDownloadURL(ctx, fileID, expireMs)
	if err != nil {
		return "", fmt.Errorf("failed to get download URL: %w", err)
	}

	if !downloadResp.IsSuccess() {
		return "", fmt.Errorf("get download URL failed: code=%d, msg=%s", downloadResp.Code, downloadResp.Message)
	}

	var downloadData DownloadFileResponse
	if err := json.Unmarshal(downloadResp.Data, &downloadData); err != nil {
		return "", fmt.Errorf("failed to unmarshal download response: %w", err)
	}

	// Download file
	resp, err := http.Get(downloadData.DownloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	fullPath := filepath.Join(savePath, downloadData.FileName)
	out, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("failed to save file: %w", err)
	}

	return fullPath, nil
}

// GetFileDownloadURL obtains a temporary download URL for a file.
//
// Parameters:
//   - ctx: Context for the request
//   - fileID: The file ID to get download URL for
//   - expireMs: URL expiration time in milliseconds
//
// Returns a response containing the download URL and file metadata.
func (c *Client) GetFileDownloadURL(ctx context.Context, fileID string, expireMs int64) (*TragResponse, error) {
	uri := "/v1/tragtools/task/dispatcher/file/download"
	params := map[string]string{
		"fileId":   fileID,
		"expireMs": fmt.Sprintf("%d", expireMs),
	}

	return c.get(ctx, uri, params, nil)
}

// GetFileDownloadURLBatch obtains temporary download URLs for multiple files.
//
// Parameters:
//   - ctx: Context for the request
//   - fileIDs: List of file IDs to get download URLs for
//   - expireMs: URL expiration time in milliseconds
//
// Returns a response containing download URLs and metadata for all requested files.
func (c *Client) GetFileDownloadURLBatch(ctx context.Context, fileIDs []string, expireMs int64) (*TragResponse, error) {
	uri := "/v1/tragtools/task/dispatcher/file/download/batch"

	data := map[string]any{
		"fileIdList":   fileIDs,
		"expireTimeMs": expireMs,
	}

	return c.post(ctx, uri, data, nil)
}

// GetTools queries tools from TRAG and returns them as tool.Tool interfaces.
//
// This method combines QueryFunctionMeta with tool creation, returning tools
// that can be directly used with agent systems.
//
// Parameters:
//   - ctx: Context for the request
//   - toolSetName: The toolset code/name to query
//   - funcNames: List of specific function names to load, or nil/empty for all functions
//
// Returns a slice of tools implementing the tool.CallableTool interface.
func (c *Client) GetTools(ctx context.Context, toolSetName string, funcNames []string) ([]tool.Tool, error) {
	defs, err := c.QueryFunctionMeta(ctx, toolSetName, funcNames)
	if err != nil {
		return nil, err
	}

	tools := make([]tool.Tool, 0, len(defs))
	for _, def := range defs {
		t := &TragTool{
			client: c,
			def:    def,
		}
		tools = append(tools, t)
	}

	return tools, nil
}
