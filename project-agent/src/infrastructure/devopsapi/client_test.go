package devopsapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewClient_MockMode Token 为空则 mock。
func TestNewClient_MockMode(t *testing.T) {
	t.Setenv("DEVOPS_TOKEN", "")
	t.Setenv("DEVOPS_API_MOCK", "")
	c := NewClient()
	if !c.IsMock() {
		t.Fatal("expected mock mode")
	}
	if _, err := c.BuildHistory(context.Background(), BuildHistoryInput{ProjectID: "p", PipelineID: "pl"}); !errors.Is(err, ErrMockMode) {
		t.Fatalf("expected ErrMockMode, got %v", err)
	}
}

// TestNewClient_ForceMock DEVOPS_API_MOCK=1 强制 mock。
func TestNewClient_ForceMock(t *testing.T) {
	t.Setenv("DEVOPS_API_MOCK", "1")
	c := NewClient(WithToken("dummy"))
	if !c.IsMock() {
		t.Fatal("expected forced mock")
	}
}

// TestBuildHistory_Real 使用 httptest 模拟蓝盾 API。
func TestBuildHistory_Real(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-DEVOPS-ACCESS-TOKEN") != "real-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if !strings.Contains(r.URL.Path, "/history") {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// 蓝盾 envelope + records 格式
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  0,
			"message": "",
			"data": map[string]any{
				"records": []map[string]any{
					{"id": "b-001", "status": "SUCCEED", "startUser": "alice"},
					{"id": "b-002", "status": "FAILED", "startUser": "bob"},
				},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(
		WithBaseURL(srv.URL),
		WithToken("real-token"),
		WithUser("test-uid"),
		WithHTTPClient(srv.Client()),
	)
	if c.IsMock() {
		t.Fatal("expected non-mock client")
	}
	builds, err := c.BuildHistory(context.Background(), BuildHistoryInput{
		ProjectID: "proj-x", PipelineID: "pl-001", Limit: 10,
	})
	if err != nil {
		t.Fatalf("BuildHistory error: %v", err)
	}
	if len(builds) != 2 || builds[0].BuildID != "b-001" {
		t.Fatalf("unexpected builds: %+v", builds)
	}
}

// TestBuildHistory_APIError envelope status!=0 被识别为错误。
func TestBuildHistory_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":2001,"message":"pipeline not found","data":null}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithToken("t"), WithHTTPClient(srv.Client()))
	_, err := c.BuildHistory(context.Background(), BuildHistoryInput{
		ProjectID: "p", PipelineID: "pl",
	})
	if err == nil || !strings.Contains(err.Error(), "status=2001") {
		t.Fatalf("expected envelope error, got %v", err)
	}
}

// TestPipelineStart_Real 验证触发流水线。
func TestPipelineStart_Real(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/start") {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		// 验证 UID 头
		if r.Header.Get("X-DEVOPS-UID") != "alice" {
			t.Errorf("expected X-DEVOPS-UID, got %q", r.Header.Get("X-DEVOPS-UID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  0,
			"message": "",
			"data":    map[string]any{"id": "b-999", "status": "PENDING"},
		})
	}))
	defer srv.Close()

	c := NewClient(
		WithBaseURL(srv.URL),
		WithToken("t"),
		WithUser("alice"),
		WithHTTPClient(srv.Client()),
	)
	res, err := c.PipelineStart(context.Background(), PipelineStartInput{
		ProjectID:  "p",
		PipelineID: "pl",
		Params:     map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("PipelineStart error: %v", err)
	}
	if res.BuildID != "b-999" {
		t.Fatalf("unexpected: %+v", res)
	}
}

// TestBuildCancel_Real 验证取消构建。
func TestBuildCancel_Real(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/b-001/cancel") {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		hit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":0,"message":"","data":null}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithToken("t"), WithHTTPClient(srv.Client()))
	if err := c.BuildCancel(context.Background(), "p", "pl", "b-001"); err != nil {
		t.Fatalf("BuildCancel error: %v", err)
	}
	if !hit {
		t.Fatal("server not hit")
	}
}

// TestBuildCancel_HTTPError 验证 403 错误。
func TestBuildCancel_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithToken("t"), WithHTTPClient(srv.Client()))
	err := c.BuildCancel(context.Background(), "p", "pl", "b")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}
