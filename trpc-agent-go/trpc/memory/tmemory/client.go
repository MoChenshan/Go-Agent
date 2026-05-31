package tmemory

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	httpHeaderAuthorization = "Authorization"
	httpHeaderAccept        = "Accept"
	httpHeaderContentType   = "Content-Type"
	httpContentTypeJSON     = "application/json"
	maxResponseBodySize     = 10 << 20
	maxRetries              = 3
	retryBaseBackoff        = 200 * time.Millisecond
	retryMaxBackoff         = 2 * time.Second
	maxErrorBodyPreview     = 512
)

type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("tmemory api request failed: status=%d body=%s", e.StatusCode, e.Body)
}

type client struct {
	host    string
	apiKey  string
	hc      *http.Client
	timeout time.Duration
}

func newClient(opts serviceOpts) (*client, error) {
	if opts.apiKey == "" {
		return nil, errors.New("tmemory api key is required (set TMEMORY_API_KEY or use WithAPIKey)")
	}
	hc := opts.client
	if hc == nil {
		hc = &http.Client{}
	}
	return &client{
		host:    strings.TrimRight(opts.host, "/"),
		apiKey:  opts.apiKey,
		hc:      hc,
		timeout: opts.timeout,
	}, nil
}

// close releases idle HTTP connections held by the underlying transport.
// It is safe to call multiple times. The HTTP client is not nil-ed out
// because callers may have supplied a shared *http.Client via
// WithHTTPClient and we should not invalidate it for them.
func (c *client) close() {
	if c == nil || c.hc == nil {
		return
	}
	if t, ok := c.hc.Transport.(interface{ CloseIdleConnections() }); ok {
		t.CloseIdleConnections()
		return
	}
	// http.Client also exposes CloseIdleConnections (Go 1.12+).
	c.hc.CloseIdleConnections()
}

func (c *client) doJSON(
	ctx context.Context,
	method string,
	path string,
	in any,
	out any,
) error {
	return c.do(ctx, method, path, in, out, false)
}

// doJSONIdempotent is like doJSON, but retries on transient failures
// (429/5xx/network) regardless of HTTP method. Callers must guarantee
// the request is safe to send multiple times (e.g. read-only POSTs or
// POSTs deduplicated by a stable trace id).
func (c *client) doJSONIdempotent(
	ctx context.Context,
	method string,
	path string,
	in any,
	out any,
) error {
	return c.do(ctx, method, path, in, out, true)
}

func (c *client) do(
	ctx context.Context,
	method string,
	path string,
	in any,
	out any,
	idempotent bool,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	urlStr, err := joinURL(c.host, path)
	if err != nil {
		return fmt.Errorf("tmemory: build url failed: %w", err)
	}

	var payload []byte
	if in != nil {
		payload, err = json.Marshal(in)
		if err != nil {
			return fmt.Errorf("tmemory: marshal request failed: %w", err)
		}
	}

	// Retry on transient failures for GETs (always safe) and for POSTs
	// the caller has explicitly marked as idempotent.
	attempts := 1
	if method == http.MethodGet || idempotent {
		attempts = maxRetries + 1
	}
	for i := 0; i < attempts; i++ {
		err = c.doJSONOnce(ctx, method, urlStr, payload, out)
		if err == nil {
			return nil
		}
		if !shouldRetry(err) || i == attempts-1 {
			return err
		}
		t := time.NewTimer(retrySleep(i, cryptoJitter))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
	return err
}

func (c *client) doJSONOnce(
	ctx context.Context,
	method string,
	urlStr string,
	payload []byte,
	out any,
) error {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return fmt.Errorf("tmemory: build request failed: %w", err)
	}
	req.Header.Set(httpHeaderAuthorization, "Bearer "+c.apiKey)
	req.Header.Set(httpHeaderAccept, httpContentTypeJSON)
	if payload != nil {
		req.Header.Set(httpHeaderContentType, httpContentTypeJSON)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("tmemory: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize+1))
	if err != nil {
		return fmt.Errorf("tmemory: read response failed: %w", err)
	}
	if len(respBody) > maxResponseBodySize {
		return errors.New("tmemory: response body too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := string(respBody)
		if len(preview) > maxErrorBodyPreview {
			preview = preview[:maxErrorBodyPreview] + "...(truncated)"
		}
		return &apiError{StatusCode: resp.StatusCode, Body: preview}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("tmemory: unmarshal response failed: %w", err)
	}
	return nil
}

func shouldRetry(err error) bool {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func retrySleep(attempt int, jitterFn func(max int64) int64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := min(time.Duration(1<<attempt)*retryBaseBackoff, retryMaxBackoff)
	if jitterFn == nil || d <= 1 {
		return d
	}
	jitterMax := int64(d / 2)
	if jitterMax <= 0 {
		return d
	}
	jitter := time.Duration(jitterFn(jitterMax))
	if jitter < 0 {
		jitter = 0
	}
	if jitter > d/2 {
		jitter = d / 2
	}
	return d/2 + jitter
}

func cryptoJitter(max int64) int64 {
	if max <= 0 {
		return 0
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(max))
	if err != nil {
		return 0
	}
	return n.Int64()
}

// joinURL safely joins a base host (which may include scheme and a base
// path) with an API path, normalizing slashes. It avoids the pitfalls of
// naive string concatenation when host or path have extra/missing slashes.
func joinURL(host, path string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", errors.New("empty host")
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	// url.JoinPath handles leading/trailing slashes and resolves "." / "..".
	joined, err := url.JoinPath(u.Path, path)
	if err != nil {
		return "", err
	}
	u.Path = joined
	return u.String(), nil
}
