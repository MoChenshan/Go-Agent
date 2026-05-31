package tapdapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewClient_MockMode 验证未配置凭据进入 Mock。
func TestNewClient_MockMode(t *testing.T) {
	t.Setenv("TAPD_USER", "")
	t.Setenv("TAPD_TOKEN", "")
	t.Setenv("TAPD_API_MOCK", "")
	c := NewClient()
	if !c.IsMock() {
		t.Fatal("expected mock mode")
	}
	if _, err := c.QueryBugs(context.Background(), QueryBugsInput{WorkspaceID: "1"}); !errors.Is(err, ErrMockMode) {
		t.Fatalf("expected ErrMockMode, got %v", err)
	}
}

// TestNewClient_ForceMock 验证 TAPD_API_MOCK 强制开关。
func TestNewClient_ForceMock(t *testing.T) {
	t.Setenv("TAPD_API_MOCK", "true")
	c := NewClient(WithCredentials("u", "p"))
	if !c.IsMock() {
		t.Fatal("expected forced mock")
	}
}

// TestQueryBugs_Real 验证真实调用路径。
func TestQueryBugs_Real(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Basic Auth 检查
		if u, p, ok := r.BasicAuth(); !ok || u != "api-user" || p != "api-pwd" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Query().Get("workspace_id") != "12345" {
			t.Errorf("missing workspace_id: %s", r.URL.RawQuery)
		}
		// 按 TAPD 标准响应外壳返回
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": 1,
			"info":   "success",
			"data": []map[string]any{
				{"Bug": map[string]any{
					"id":       "1001",
					"title":    "OOM in game-core",
					"status":   "new",
					"priority": "high",
				}},
				{"Bug": map[string]any{
					"id":     "1002",
					"title":  "routesvr panic",
					"status": "in_progress",
				}},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(
		WithBaseURL(srv.URL),
		WithCredentials("api-user", "api-pwd"),
		WithWorkspaceID("12345"),
		WithHTTPClient(srv.Client()),
	)
	if c.IsMock() {
		t.Fatal("expected non-mock client")
	}
	bugs, err := c.QueryBugs(context.Background(), QueryBugsInput{})
	if err != nil {
		t.Fatalf("QueryBugs error: %v", err)
	}
	if len(bugs) != 2 || bugs[0].Title != "OOM in game-core" {
		t.Fatalf("unexpected bugs: %+v", bugs)
	}
}

// TestQueryBugs_APIError 验证 envelope status!=1 被识别为错误。
func TestQueryBugs_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":0,"info":"workspace_id invalid"}`))
	}))
	defer srv.Close()

	c := NewClient(
		WithBaseURL(srv.URL),
		WithCredentials("u", "p"),
		WithWorkspaceID("1"),
		WithHTTPClient(srv.Client()),
	)
	_, err := c.QueryBugs(context.Background(), QueryBugsInput{})
	if err == nil || !strings.Contains(err.Error(), "status=0") {
		t.Fatalf("expected envelope error, got %v", err)
	}
}

// TestCreateBug_Real 验证创建缺陷接口使用 form-urlencoded + 成功返回。
func TestCreateBug_Real(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/x-www-form-urlencoded") {
			t.Errorf("expected form-urlencoded, got %s", ct)
		}
		body, _ := io.ReadAll(r.Body)
		bs := string(body)
		if !strings.Contains(bs, "title=oom+bug") && !strings.Contains(bs, "title=oom%20bug") {
			t.Errorf("title not encoded correctly: %s", bs)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": 1,
			"info":   "success",
			"data": map[string]any{
				"Bug": map[string]any{
					"id":     "9001",
					"title":  "oom bug",
					"status": "new",
				},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(
		WithBaseURL(srv.URL),
		WithCredentials("u", "p"),
		WithWorkspaceID("1"),
		WithHTTPClient(srv.Client()),
	)
	bug, err := c.CreateBug(context.Background(), CreateBugInput{
		Title: "oom bug", Description: "...",
	})
	if err != nil {
		t.Fatalf("CreateBug error: %v", err)
	}
	if bug.ID != "9001" || bug.Title != "oom bug" {
		t.Fatalf("unexpected: %+v", bug)
	}
}
