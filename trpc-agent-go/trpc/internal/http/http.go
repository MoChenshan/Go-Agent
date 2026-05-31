// Package http is the internal function package for trpc.
package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"git.code.oa.com/trpc-go/trpc-go/codec"
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
)

const (
	defaultErrorRespBodyLimit = 4096

	schemeHTTP    = "http"
	schemeHTTPS   = "https"
	schemeDNS     = "dns"
	schemePolaris = "polaris"

	targetSep = "://"

	localhostHost = "localhost"

	hostLabelSep        = "."
	hostLabelHyphen     = '-'
	hostLabelUnderscore = '_'

	errMsgUnsupportedTargetScheme = "unsupported request target scheme"
	errMsgMissingTargetHost       = "request target host is required"
	errMsgUnexpectedTargetUser    = "request target must not contain user info"
	errMsgInvalidTargetHost       = "request target host is invalid"
	errMsgInvalidTargetPort       = "request target port is invalid"
	errMsgMissingTRPCClientName   = "tRPC client name is required"
)

// HTTPClient is the interface for HTTP client
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// RequestHandler is a handler for tRPC HTTP requests.
type RequestHandler struct {
	proxy    thttp.Client
	name     string
	fallback HTTPClient
}

type preparedProxyPath struct {
	value string
}

func (p preparedProxyPath) String() string {
	return p.value
}

type validatedTRPCTarget struct {
	value    string
	needsTLS bool
}

func (t validatedTRPCTarget) isSet() bool {
	return t.value != ""
}

func (t validatedTRPCTarget) option() client.Option {
	return client.WithTarget(t.value)
}

// NewRequestHandler creates a new tRPC HTTP request handler.
func NewRequestHandler(name string, opts ...client.Option) *RequestHandler {
	opts = append(
		opts,
		client.WithSerializationType(codec.SerializationTypeNoop),
	)
	proxy := thttp.NewClientProxy(name, opts...)
	return &RequestHandler{
		proxy:    proxy,
		name:     name,
		fallback: &http.Client{},
	}
}

// Do sends a request to the tRPC server and returns the response.
func (h *RequestHandler) Do(req *http.Request) (*http.Response, error) {
	if shouldUseDirectHTTP(req.URL) {
		return h.doDirect(req)
	}

	ctx := req.Context()

	reqHead, respHead := prepareHeaders(req)
	respBody := &codec.Body{}
	proxyPath := prepareProxyPath(req.URL)
	opts, err := prepareClientOptions(req, h.name, reqHead, respHead)
	if err != nil {
		return nil, err
	}

	switch req.Method {
	case http.MethodGet:
		if err := discardAndCloseBody(req.Body); err != nil {
			return nil, err
		}
		opts = append(opts, client.WithTimeout(0))
		err = h.get(ctx, proxyPath, respBody, opts...)
	case http.MethodPost:
		reqBody, readErr := readRequestBody(req.Body)
		if readErr != nil {
			return nil, readErr
		}
		opts = append(opts, client.WithTimeout(0))
		err = h.post(ctx, proxyPath, reqBody, respBody, opts...)
	default:
		return nil, fmt.Errorf("unsupported HTTP method: %s", req.Method)
	}

	if err != nil {
		// When tRPC returns an error (e.g., non-2xx status code), the response
		// may still contain valuable error details in the body. We should include
		// these details in the error message to help with debugging.
		return nil, enhanceErrorWithResponseBody(err, respHead.Response)
	}

	return respHead.Response, nil
}

func (h *RequestHandler) get(
	ctx context.Context,
	path preparedProxyPath,
	respBody *codec.Body,
	opts ...client.Option,
) error {
	return h.proxy.Get(ctx, path.String(), respBody, opts...)
}

func (h *RequestHandler) post(
	ctx context.Context,
	path preparedProxyPath,
	reqBody *codecBodyWrapper,
	respBody *codec.Body,
	opts ...client.Option,
) error {
	return h.proxy.Post(ctx, path.String(), reqBody, respBody, opts...)
}

func (h *RequestHandler) doDirect(req *http.Request) (*http.Response, error) {
	switch req.Method {
	case http.MethodGet, http.MethodPost:
	default:
		return nil, fmt.Errorf("unsupported HTTP method: %s", req.Method)
	}

	resp, err := h.fallback.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, enhanceErrorWithResponseBody(
			fmt.Errorf("unexpected HTTP status: %s", resp.Status),
			resp,
		)
	}
	return resp, nil
}

// prepareHeaders prepares the request and response headers.
func prepareHeaders(
	req *http.Request,
) (*thttp.ClientReqHeader, *thttp.ClientRspHeader) {
	reqHead := &thttp.ClientReqHeader{Header: make(http.Header)}
	for headerKey, v := range req.Header {
		reqHead.Header[headerKey] = v
	}
	respHead := &thttp.ClientRspHeader{
		ManualReadBody: true,
	}
	return reqHead, respHead
}

// preparePath returns the correct path, using an empty string for root.
func preparePath(path string) string {
	if path == "/" {
		return ""
	}
	return path
}

func prepareProxyPath(u *url.URL) preparedProxyPath {
	if u == nil {
		return preparedProxyPath{}
	}
	path := preparePath(u.Path)
	path = handleQueryParams(path, u)
	return preparedProxyPath{value: path}
}

// prepareClientOptions prepares the client options including target and TLS.
func prepareClientOptions(
	req *http.Request,
	handlerName string,
	reqHead *thttp.ClientReqHeader,
	respHead *thttp.ClientRspHeader,
) ([]client.Option, error) {
	opts := []client.Option{
		client.WithReqHead(reqHead),
		client.WithRspHead(respHead),
		client.WithCurrentSerializationType(codec.SerializationTypeNoop),
	}
	target, err := extractTargetFromURL(req.URL, handlerName)
	if err != nil {
		return nil, err
	}
	if target.isSet() {
		opts = append(opts, target.option())
		if target.needsTLS {
			opts = append(opts, client.WithTLS("", "", "root", ""))
		}
	}
	return opts, nil
}

// handleQueryParams appends query parameters to the path if needed.
func handleQueryParams(path string, u *url.URL) string {
	if u.RawQuery != "" {
		unescapedPath, err := url.PathUnescape(path)
		if err != nil {
			unescapedPath = path
		}
		if !strings.Contains(unescapedPath, "?") {
			path = path + "?" + u.RawQuery
		}
	}
	return path
}

// discardAndCloseBody discards and closes the request body for GET requests.
func discardAndCloseBody(body io.ReadCloser) error {
	if body != nil {
		if _, copyErr := io.Copy(io.Discard, body); copyErr != nil {
			return fmt.Errorf("failed to discard request body: %w", copyErr)
		}
		if closeErr := body.Close(); closeErr != nil {
			return fmt.Errorf("failed to close request body: %w", closeErr)
		}
	}
	return nil
}

// readRequestBody reads and closes the request body for POST requests.
func readRequestBody(body io.ReadCloser) (*codecBodyWrapper, error) {
	var reqBodyBytes []byte
	var err error
	if body != nil {
		reqBodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		body.Close()
	}
	return &codecBodyWrapper{Body: &codec.Body{Data: reqBodyBytes}}, nil
}

type codecBodyWrapper struct {
	*codec.Body
}

func (b *codecBodyWrapper) MarshalJSON() ([]byte, error) {
	return b.Data, nil
}

// enhanceErrorWithResponseBody reads the response body and includes it
// in the error message.
// This is crucial for LLM API errors where the detailed error
// information is in the response body.
func enhanceErrorWithResponseBody(err error, resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Read the response body (limit to 4KB to avoid memory issues)
	bodyBytes, readErr := io.ReadAll(
		io.LimitReader(resp.Body, defaultErrorRespBodyLimit),
	)
	resp.Body.Close()

	if readErr != nil {
		return fmt.Errorf(
			"failed to send request: %w "+
				"(failed to read error response body: %v)",
			err,
			readErr,
		)
	}

	// If body is empty, just return the original error
	if len(bodyBytes) == 0 {
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Include the complete response body in the error message
	// Don't parse or filter - return everything to avoid data loss
	bodyStr := string(bodyBytes)
	return fmt.Errorf(
		"failed to send request: %w (response body: %s)",
		err,
		bodyStr,
	)
}

func shouldUseDirectHTTP(u *url.URL) bool {
	if u == nil {
		return false
	}
	return u.Scheme == schemeHTTP || u.Scheme == schemeHTTPS
}

// extractTargetFromURL extracts the tRPC target from a trusted URL target.
func extractTargetFromURL(
	u *url.URL,
	handlerName string,
) (validatedTRPCTarget, error) {
	if u == nil || u.Scheme == "" {
		return validatedTRPCTarget{}, nil
	}

	switch u.Scheme {
	case schemeHTTP, schemeHTTPS:
		return validatedTRPCTarget{}, nil
	case schemeDNS:
		host, err := validatedDNSHost(u)
		if err != nil {
			return validatedTRPCTarget{}, err
		}
		return validatedTRPCTarget{value: buildTarget(schemeDNS, host)}, nil
	case schemePolaris:
		if handlerName == "" {
			return validatedTRPCTarget{}, fmt.Errorf(
				"%s for %s target",
				errMsgMissingTRPCClientName,
				schemePolaris,
			)
		}
		host, err := validatedPolarisHost(u)
		if err != nil {
			return validatedTRPCTarget{}, err
		}
		return validatedTRPCTarget{
			value: buildTarget(schemePolaris, host),
		}, nil
	default:
		return validatedTRPCTarget{}, fmt.Errorf(
			"%s: %s",
			errMsgUnsupportedTargetScheme,
			u.Scheme,
		)
	}
}

func validatedDNSHost(u *url.URL) (string, error) {
	if u == nil {
		return "", nil
	}
	if u.User != nil {
		return "", fmt.Errorf("%s", errMsgUnexpectedTargetUser)
	}

	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("%s", errMsgMissingTargetHost)
	}
	if port := u.Port(); port != "" && !isValidPort(port) {
		return "", fmt.Errorf("%s: %s", errMsgInvalidTargetPort, port)
	}
	return u.Host, nil
}

func validatedPolarisHost(u *url.URL) (string, error) {
	if u == nil {
		return "", nil
	}
	if u.User != nil {
		return "", fmt.Errorf("%s", errMsgUnexpectedTargetUser)
	}

	host := strings.TrimSuffix(u.Hostname(), hostLabelSep)
	if host == "" {
		return "", fmt.Errorf("%s", errMsgMissingTargetHost)
	}
	if net.ParseIP(host) != nil {
		return "", fmt.Errorf("%s: %s", errMsgInvalidTargetHost, host)
	}
	if !isValidPolarisHost(host) {
		return "", fmt.Errorf("%s: %s", errMsgInvalidTargetHost, host)
	}
	if u.Port() != "" {
		return "", fmt.Errorf("%s: %s", errMsgInvalidTargetPort, u.Port())
	}
	return host, nil
}

func buildTarget(scheme, host string) string {
	return scheme + targetSep + host
}

func isValidPolarisHost(host string) bool {
	if host == "" || strings.EqualFold(host, localhostHost) {
		return false
	}

	labels := strings.Split(host, hostLabelSep)
	for _, label := range labels {
		if !isValidPolarisHostLabel(label) {
			return false
		}
	}
	return true
}

func isValidPolarisHostLabel(label string) bool {
	if label == "" {
		return false
	}
	for i := 0; i < len(label); i++ {
		ch := label[i]
		if isAlphaNum(ch) ||
			ch == hostLabelHyphen ||
			ch == hostLabelUnderscore {
			continue
		}
		return false
	}
	return true
}

func isAlphaNum(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9')
}

func isValidPort(port string) bool {
	value, err := strconv.Atoi(port)
	return err == nil && value > 0 && value <= 65535
}
