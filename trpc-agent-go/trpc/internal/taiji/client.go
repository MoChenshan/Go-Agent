// Package client is the go sdk for taiji.
package taiji

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// SSE event prefixes
const (
	sseDataPrefix  = "data:"
	sseEventPrefix = "event:"
	sseIDPrefix    = "id:"
	sseRetryPrefix = "retry:"

	applicationPrefix = "hyaide-application"
)

// SSEEvent represents a parsed SSE event
type SSEEvent struct {
	Data  []string
	Event string
	ID    string
	Retry string
}

// Reset clears all fields and reuses the underlying Data slice
func (e *SSEEvent) Reset() {
	e.Data = e.Data[:0] // Keep underlying array, just reset length
	e.Event = ""
	e.ID = ""
	e.Retry = ""
}

// Client represents a Taiji RAG platform client
type Client struct {
	httpClient   ihttp.HTTPClient
	taijiOpts    TaijiOption
	maxEventSize int
}

// Option defines configuration options for the Taiji client
type Option func(*Client)

// WithTaijiOption sets the Taiji configuration options
func WithTaijiOption(opt TaijiOption) Option {
	return func(c *Client) {
		c.taijiOpts = opt
	}
}

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(httpClient ihttp.HTTPClient) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithMaxEventSize sets the maximum size of the event channel.
func WithMaxEventSize(size int) Option {
	return func(c *Client) {
		c.maxEventSize = size
	}
}

// NewClient creates a new Taiji client with the given options
func NewClient(opts ...Option) *Client {
	client := &Client{}

	for _, opt := range opts {
		opt(client)
	}
	// if httpClient is not set, use the default http client
	if client.httpClient == nil {
		client.httpClient = ihttp.NewRequestHandler(client.taijiOpts.ServiceName)
	}
	return client
}

// CreateEmbedding creates embeddings for the given input
func (c *Client) CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	// Send request using the common method
	resp := &EmbeddingResponse{}
	if err := c.processRequest(ctx, http.MethodPost, EmbeddingsEndpoint, req, resp, nil, false); err != nil {
		return nil, err
	}

	return resp, nil
}

// Search performs embedding search for the given text
func (c *Client) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if strings.HasPrefix(c.taijiOpts.EmbIndex, applicationPrefix) {
		req.EmbIndex = c.taijiOpts.EmbIndex
	} else {
		req.EmbIndex = fmt.Sprintf("%s-%s-vdb-proxy", applicationPrefix, c.taijiOpts.EmbIndex)
	}

	resp := &SearchResponse{}
	if err := c.processRequest(ctx, "POST", SearchEndpoint, req, resp, nil, false); err != nil {
		return nil, err
	}
	return resp, nil
}

// UpdateIndexData performs incremental update operations on index data
func (c *Client) UpdateIndexData(ctx context.Context, req *IndexDataRequest) (*IndexDataResponse, error) {
	if c.taijiOpts.TaijiHYAPIURL == "" {
		return nil, fmt.Errorf("TAIJI HY API URL is required for update index data, please check your TaijiOptions")
	}

	req.EmbIndexID = c.taijiOpts.EmbIndex
	if req.Command != CommandAdd && req.Command != CommandDel && req.Command != CommandUpdate {
		return nil, fmt.Errorf("invalid command: %s, must be Add, Update, or Delete", req.Command)
	}

	resp := &IndexDataResponse{}
	if err := c.processRequest(ctx, http.MethodPost, IndexDataEndpoint, req, resp, nil, true); err != nil {
		return nil, err
	}
	return resp, nil
}

// AppCreate creates a new app with streaming support
func (c *Client) AppCreate(ctx context.Context, req *ChatRequest, chatHeaders *ChatHeaders, channelBufSize int) (<-chan *ChatResp, error) {
	headers := chatHeaders.ConvertToHeaders()
	headers["Content-Type"] = "application/json"
	if c.taijiOpts.Token != "" {
		headers["Authorization"] = "Bearer " + c.taijiOpts.Token
	}

	if strings.HasPrefix(c.taijiOpts.ApplicationID, applicationPrefix) {
		req.ForwardService = c.taijiOpts.ApplicationID
	} else {
		req.ForwardService = fmt.Sprintf("%s-%s", applicationPrefix, c.taijiOpts.ApplicationID)
	}

	// process streaming request
	if req.Stream {
		headers["Accept"] = "text/event-stream"
		return c.processChatStreaming(ctx, http.MethodPost, AppCreateEndpoint, req, headers, channelBufSize)
	}

	// process unary request
	respChan := make(chan *ChatResp, 1)
	defer close(respChan)
	headers["Accept"] = "application/json"
	resp := &ChatResp{}
	if err := c.processRequest(ctx, http.MethodPost, AppCreateEndpoint, req, resp, headers, false); err != nil {
		return nil, fmt.Errorf("failed to process request: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("failed to create app: %v", resp.Error)
	}
	respChan <- resp
	return respChan, nil
}

// processChatStreaming handles streaming chat requests using SSE protocol
func (c *Client) processChatStreaming(
	ctx context.Context,
	method, endpoint string,
	reqBody any,
	headers map[string]string,
	channelBufSize int,
) (<-chan *ChatResp, error) {
	resp, err := c.sendRequest(ctx, method, endpoint, reqBody, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	respChan := make(chan *ChatResp, channelBufSize)

	go func() {
		defer func() {
			resp.Body.Close()
			close(respChan)
		}()

		scanner := bufio.NewScanner(resp.Body)
		// Increase the scanner token limit beyond the default 64KB to handle larger SSE lines.
		buf := make([]byte, c.maxEventSize)
		scanner.Buffer(buf, c.maxEventSize)

		currentEvent := &SSEEvent{
			Data: make([]string, 0),
		}

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				c.sendErrorEvent(fmt.Sprintf("request cancelled: %v", ctx.Err()), -1, respChan)
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())

			// Handle empty lines (event separator in SSE)
			if line == "" {
				if len(currentEvent.Data) > 0 {
					c.processSSEEvent(currentEvent, respChan)
				}
				// Reset for next event
				currentEvent.Reset()
				continue
			}

			// Parse SSE line and accumulate event data
			fieldType, value := c.parseSSELine(line)
			switch fieldType {
			case "data":
				currentEvent.Data = append(currentEvent.Data, value)
			case "event":
				currentEvent.Event = value
			case "id":
				currentEvent.ID = value
			case "retry":
				currentEvent.Retry = value
			default:
				currentEvent.Data = append(currentEvent.Data, line)
			}
		}

		// Handle any remaining event data
		if len(currentEvent.Data) > 0 {
			c.processSSEEvent(currentEvent, respChan)
		}

		// Check for scanner errors
		if err := scanner.Err(); err != nil && err != io.EOF {
			c.sendErrorEvent(fmt.Sprintf("failed to read stream: %v", err), -1, respChan)
		}
	}()

	return respChan, nil
}

func (c *Client) sendErrorEvent(msg string, retCode int, respChan chan *ChatResp) {
	resp := &ChatResp{
		Error: &ErrorMsg{
			Message: msg,
			RetCode: retCode,
		},
	}
	select {
	case respChan <- resp:
	default:
		log.Warnf("failed to send error event, channel may be full or closed: %s", msg)
	}
}

// parseSSELine parses a single SSE line and returns the field type and value
func (c *Client) parseSSELine(line string) (fieldType, value string) {
	line = strings.TrimSpace(line)

	// Handle comments (lines starting with :)
	if strings.HasPrefix(line, ":") {
		return "", ""
	}

	// Parse different SSE fields
	switch {
	case strings.HasPrefix(line, sseDataPrefix):
		return "data", strings.TrimSpace(strings.TrimPrefix(line, sseDataPrefix))
	case strings.HasPrefix(line, sseEventPrefix):
		return "event", strings.TrimSpace(strings.TrimPrefix(line, sseEventPrefix))
	case strings.HasPrefix(line, sseIDPrefix):
		return "id", strings.TrimSpace(strings.TrimPrefix(line, sseIDPrefix))
	case strings.HasPrefix(line, sseRetryPrefix):
		return "retry", strings.TrimSpace(strings.TrimPrefix(line, sseRetryPrefix))
	default:
		// Handle field without colon (treat as data)
		if colonIndex := strings.Index(line, ":"); colonIndex == -1 {
			return "data", line
		}
		return "", ""
	}
}

// processSSEEvent processes accumulated SSE event data and sends it to the response channel
func (c *Client) processSSEEvent(event *SSEEvent, respChan chan *ChatResp) {
	if len(event.Data) == 0 {
		return
	}

	// Join multiple data lines with newlines
	eventData := strings.Join(event.Data, "\n")
	if eventData == "" {
		return
	}

	resp := &ChatResp{}
	if err := json.Unmarshal([]byte(eventData), resp); err != nil {
		c.sendErrorEvent("failed to decode response", -1, respChan)
		return
	}

	// the last event, skip it
	if strings.TrimSpace(resp.Result) == "<finish>" {
		return
	}
	respChan <- resp
}

func (c *Client) processRequest(
	ctx context.Context,
	method, endpoint string,
	reqBody any,
	respBody any,
	headers map[string]string,
	isHYAPI bool,
) error {
	var resp *http.Response
	var err error
	if isHYAPI {
		resp, err = c.sendHYAPIRequest(ctx, method, endpoint, reqBody, headers)
	} else {
		resp, err = c.sendRequest(ctx, method, endpoint, reqBody, headers)
	}

	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// sendRequest is a common method for sending HTTP requests to the Taiji API
// it is used for normal operations
func (c *Client) sendRequest(
	ctx context.Context,
	method, endpoint string,
	reqBody any,
	headers map[string]string,
) (*http.Response, error) {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	baseURL := strings.TrimSuffix(c.taijiOpts.URL, "/")
	url := fmt.Sprintf("%s%s", baseURL, endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.taijiOpts.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.taijiOpts.Token)
	}
	if c.taijiOpts.WSID != "" {
		httpReq.Header.Set("wsid", c.taijiOpts.WSID)
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	return resp, nil
}

// sendHYAPIRequest sends an HTTP request to the Taiji HY API
// it is used for opertion knowledge index data, so it need strict authorization by TAIJI-HY-AIDE-API-TOKEN and different url
func (c *Client) sendHYAPIRequest(
	ctx context.Context,
	method, endpoint string,
	reqBody any,
	headers map[string]string,
) (*http.Response, error) {
	if c.taijiOpts.TaijiHYAPIURL == "" {
		return nil, fmt.Errorf("Taiji HY API URL is required")
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	baseURL := strings.TrimSuffix(c.taijiOpts.TaijiHYAPIURL, "/")
	url := fmt.Sprintf("%s%s", baseURL, endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.taijiOpts.TaijiHYAPIToken != "" {
		httpReq.Header.Set("TAIJI-HY-AIDE-API-TOKEN", "token "+c.taijiOpts.TaijiHYAPIToken)
	}
	if c.taijiOpts.WSID != "" {
		httpReq.Header.Set("wsid", c.taijiOpts.WSID)
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	return resp, nil
}
