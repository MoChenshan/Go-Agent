package tmemory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClient_MissingAPIKey(t *testing.T) {
	_, err := newClient(serviceOpts{host: "http://localhost", apiKey: ""})
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
	if !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewClient_Success(t *testing.T) {
	c, err := newClient(serviceOpts{host: "http://localhost/", apiKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.host != "http://localhost" {
		t.Fatalf("expected trailing slash trimmed, got %q", c.host)
	}
	if c.apiKey != "test-key" {
		t.Fatalf("unexpected apiKey: %q", c.apiKey)
	}
}

func TestDoJSON_PostSuccess(t *testing.T) {
	type reqBody struct {
		Name string `json:"name"`
	}
	type respBody struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("unexpected auth header: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != httpContentTypeJSON {
			t.Errorf("unexpected content-type: %q", got)
		}

		body, _ := io.ReadAll(r.Body)
		var req reqBody
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal request failed: %v", err)
		}
		if req.Name != "test" {
			t.Errorf("unexpected name: %q", req.Name)
		}

		w.Header().Set("Content-Type", httpContentTypeJSON)
		_ = json.NewEncoder(w).Encode(respBody{Code: 0, Message: "ok"})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "test-key", client: srv.Client()})

	var resp respBody
	err := c.doJSON(context.Background(), http.MethodPost, "/test", reqBody{Name: "test"}, &resp)
	if err != nil {
		t.Fatalf("doJSON failed: %v", err)
	}
	if resp.Code != 0 || resp.Message != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestDoJSON_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	err := c.doJSON(context.Background(), http.MethodPost, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected apiError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", apiErr.StatusCode)
	}
}

func TestDoJSON_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{
		host:    srv.URL,
		apiKey:  "k",
		client:  srv.Client(),
		timeout: 50 * time.Millisecond,
	})
	err := c.doJSON(context.Background(), http.MethodPost, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDoJSON_GetRetries(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client(), timeout: 5 * time.Second})
	err := c.doJSON(context.Background(), http.MethodGet, "/retry", nil, nil)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestDoJSON_PostNoRetry(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	err := c.doJSON(context.Background(), http.MethodPost, "/no-retry", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Fatalf("POST should not retry, got %d attempts", got)
	}
}

func TestDoJSON_NilOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"data":"ignored"}`))
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	err := c.doJSON(context.Background(), http.MethodPost, "/nil", nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestDoJSON_ResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		// Write more than maxResponseBodySize
		_, _ = w.Write(make([]byte, maxResponseBodySize+2))
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	err := c.doJSON(context.Background(), http.MethodPost, "/big", nil, nil)
	if err == nil {
		t.Fatal("expected error for oversized response")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"429", &apiError{StatusCode: 429}, true},
		{"500", &apiError{StatusCode: 500}, true},
		{"503", &apiError{StatusCode: 503}, true},
		{"400", &apiError{StatusCode: 400}, false},
		{"net error", &net.DNSError{IsTimeout: true}, true},
		{"generic error", errors.New("fail"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetry(tt.err); got != tt.expect {
				t.Fatalf("shouldRetry(%v) = %v, want %v", tt.err, got, tt.expect)
			}
		})
	}
}

func TestRetrySleep(t *testing.T) {
	// Fixed jitter for deterministic testing.
	fixedJitter := func(max int64) int64 { return max / 2 }

	d0 := retrySleep(0, fixedJitter)
	d1 := retrySleep(1, fixedJitter)
	d2 := retrySleep(2, fixedJitter)

	if d0 <= 0 {
		t.Fatal("expected positive duration")
	}
	if d1 <= d0 {
		t.Fatal("expected d1 > d0 (exponential backoff)")
	}
	// d2 should not exceed retryMaxBackoff.
	if d2 > retryMaxBackoff {
		t.Fatalf("d2=%v exceeds max %v", d2, retryMaxBackoff)
	}
}

func TestRetrySleep_NilJitter(t *testing.T) {
	d := retrySleep(0, nil)
	if d != retryBaseBackoff {
		t.Fatalf("expected base backoff %v, got %v", retryBaseBackoff, d)
	}
}

func TestCryptoJitter(t *testing.T) {
	for i := 0; i < 20; i++ {
		j := cryptoJitter(100)
		if j < 0 || j >= 100 {
			t.Fatalf("jitter %d out of range [0,100)", j)
		}
	}
	if cryptoJitter(0) != 0 {
		t.Fatal("expected 0 for max=0")
	}
	if cryptoJitter(-1) != 0 {
		t.Fatal("expected 0 for max<0")
	}
}

func TestApiError_Error(t *testing.T) {
	e := &apiError{StatusCode: 502, Body: "bad gateway"}
	s := e.Error()
	if !strings.Contains(s, "502") || !strings.Contains(s, "bad gateway") {
		t.Fatalf("unexpected error string: %s", s)
	}
}

// TestDoJSONIdempotent_RetriesPOSTOn5xx verifies that POSTs sent through
// doJSONIdempotent are retried on transient 5xx errors, even though
// regular POSTs via doJSON are not.
func TestDoJSONIdempotent_RetriesPOSTOn5xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "try again")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{
		host:    srv.URL,
		apiKey:  "k",
		client:  srv.Client(),
		timeout: 5 * time.Second,
	})

	var resp map[string]any
	err := c.doJSONIdempotent(context.Background(), http.MethodPost, "/v1/test", map[string]string{"a": "b"}, &resp)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("expected 3 attempts (2 retries), got %d", got)
	}
	if resp["ok"] != true {
		t.Fatalf("expected ok=true in response, got %v", resp)
	}
}

// TestDoJSON_DoesNotRetryPOST verifies that the non-idempotent POST
// path returns the first error without retrying.
func TestDoJSON_DoesNotRetryPOST(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{
		host:    srv.URL,
		apiKey:  "k",
		client:  srv.Client(),
		timeout: 5 * time.Second,
	})

	err := c.doJSON(context.Background(), http.MethodPost, "/v1/test", map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 attempt for non-idempotent POST, got %d", got)
	}
}

// TestDoJSONIdempotent_GivesUpAfterMaxRetries verifies that retries are
// bounded so a perpetually failing endpoint does not hang the caller.
func TestDoJSONIdempotent_GivesUpAfterMaxRetries(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{
		host:    srv.URL,
		apiKey:  "k",
		client:  srv.Client(),
		timeout: 5 * time.Second,
	})

	err := c.doJSONIdempotent(context.Background(), http.MethodPost, "/v1/test", nil, nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected wrapped 500 apiError, got: %v", err)
	}
	// maxRetries+1 == 4 attempts.
	if got := atomic.LoadInt32(&attempts); got != int32(maxRetries+1) {
		t.Fatalf("expected %d attempts, got %d", maxRetries+1, got)
	}
}

// TestDoJSONIdempotent_DoesNotRetry4xx verifies that non-retryable
// statuses (e.g. 400) are surfaced immediately even on the idempotent
// path, since they indicate caller-side errors.
func TestDoJSONIdempotent_DoesNotRetry4xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{
		host:    srv.URL,
		apiKey:  "k",
		client:  srv.Client(),
		timeout: 5 * time.Second,
	})

	err := c.doJSONIdempotent(context.Background(), http.MethodPost, "/v1/test", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected 1 attempt for non-retryable 4xx, got %d", got)
	}
}
