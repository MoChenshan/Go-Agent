package gongfengapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewClient_MockMode 验证未配置 Token 时自动进入 Mock。
func TestNewClient_MockMode(t *testing.T) {
	t.Setenv("GONGFENG_TOKEN", "")
	t.Setenv("GONGFENG_API_MOCK", "")
	c := NewClient()
	if !c.IsMock() {
		t.Fatal("expected mock mode when GONGFENG_TOKEN empty")
	}

	if _, err := c.CreateMR(context.Background(), CreateMRInput{ProjectID: "x"}); !errors.Is(err, ErrMockMode) {
		t.Fatalf("expected ErrMockMode, got %v", err)
	}
}

// TestNewClient_ForceMock 验证 GONGFENG_API_MOCK 环境开关。
func TestNewClient_ForceMock(t *testing.T) {
	t.Setenv("GONGFENG_API_MOCK", "1")
	c := NewClient(WithToken("dummy"))
	if !c.IsMock() {
		t.Fatal("expected mock mode when GONGFENG_API_MOCK=1")
	}
}

// TestCreateMR_Real 使用 httptest 模拟工蜂 API。
func TestCreateMR_Real(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "real-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/merge_requests") {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		// 解析请求 body，校验字段存在
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["source_branch"] != "feat/x" {
			t.Errorf("unexpected body: %v", body)
		}
		// 返回 MR 样例
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"iid":           42,
			"title":         body["title"],
			"state":         "opened",
			"source_branch": body["source_branch"],
			"target_branch": body["target_branch"],
			"web_url":       "https://git.example/mr/42",
			"created_at":    "2026-04-20T12:00:00Z",
		})
	}))
	defer srv.Close()

	c := NewClient(
		WithBaseURL(srv.URL),
		WithToken("real-token"),
		WithHTTPClient(srv.Client()),
	)
	if c.IsMock() {
		t.Fatal("expected non-mock client")
	}

	mr, err := c.CreateMR(context.Background(), CreateMRInput{
		ProjectID:    "video/game-core",
		SourceBranch: "feat/x",
		TargetBranch: "master",
		Title:        "fix: oom",
		Description:  "desc",
		Reviewers:    []string{"u1", "u2"},
	})
	if err != nil {
		t.Fatalf("CreateMR error: %v", err)
	}
	if mr.IID != 42 || mr.State != "opened" || mr.WebURL == "" {
		t.Fatalf("unexpected MR: %+v", mr)
	}
}

// TestCreateMR_HTTPError 验证非 2xx 响应被归一化为 error。
func TestCreateMR_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"403 forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(
		WithBaseURL(srv.URL),
		WithToken("real-token"),
		WithHTTPClient(srv.Client()),
	)
	_, err := c.CreateMR(context.Background(), CreateMRInput{
		ProjectID:    "p",
		SourceBranch: "s",
		TargetBranch: "t",
		Title:        "x",
	})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

// TestMergeMR_Real 验证 MR 合并接口。
func TestMergeMR_Real(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if !strings.Contains(r.URL.Path, "/merge_requests/42/merge") {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"iid":       42,
			"state":     "merged",
			"merged_at": "2026-04-20T13:00:00Z",
		})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithToken("t"), WithHTTPClient(srv.Client()))
	mr, err := c.MergeMR(context.Background(), MergeMRInput{
		ProjectID: "p", MRIid: 42, MergeMessage: "reason",
	})
	if err != nil {
		t.Fatalf("MergeMR error: %v", err)
	}
	if mr.State != "merged" {
		t.Fatalf("expected merged state, got %+v", mr)
	}
}

// TestGetMR_Real 验证查询 MR。
func TestGetMR_Real(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"iid": 7, "state": "opened"})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithToken("t"), WithHTTPClient(srv.Client()))
	mr, err := c.GetMR(context.Background(), "p", 7)
	if err != nil {
		t.Fatalf("GetMR error: %v", err)
	}
	if mr.IID != 7 {
		t.Fatalf("unexpected: %+v", mr)
	}
}
