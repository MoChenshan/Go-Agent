package debug

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug/internal/schema"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

func TestServer_handleListApps(t *testing.T) {
	agents := map[string]agent.Agent{
		"agent1": &mockAgent{name: "agent1", description: "first agent"},
		"agent2": &mockAgent{name: "agent2", description: "second agent"},
	}

	server := New(agents)
	req := httptest.NewRequest(http.MethodGet, "/list-apps", nil)
	w := httptest.NewRecorder()

	server.handleListApps(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "expected status 200, got %d", w.Code)

	var apps []string
	if err := json.Unmarshal(w.Body.Bytes(), &apps); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	assert.Equal(t, 2, len(apps), "expected 2 apps, got %d", len(apps))

	// Check that both agent names are present.
	found := make(map[string]bool)
	for _, app := range apps {
		found[app] = true
	}

	assert.True(t, found["agent1"] && found["agent2"])
}

func TestServer_handleCreateSession(t *testing.T) {
	agents := map[string]agent.Agent{
		"test-agent": &mockAgent{
			name:        "test-agent",
			description: "test description",
		},
	}

	server := New(agents)
	path := "/apps/test-agent/users/test-user/sessions"
	req := httptest.NewRequest(http.MethodPost, path, nil)
	w := httptest.NewRecorder()

	// Set up the route variables that gorilla/mux would normally set.
	req = mux.SetURLVars(req, map[string]string{
		"appName": "test-agent",
		"userId":  "test-user",
	})

	server.handleCreateSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "expected status 200, got %d", w.Code)

	var session schema.ADKSession
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	assert.Equal(t, "test-agent", session.AppName)
	assert.Equal(t, "test-user", session.UserID)
	assert.NotEmpty(t, session.ID)
}

func TestHandleListSessions_FiltersEvalSessions(t *testing.T) {
	now := time.Now()
	customSvc := &mockSessionService{
		listSessionsResult: []*session.Session{
			{
				ID:        "sess-1",
				AppName:   "app",
				UserID:    "user",
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "eval-123",
				AppName:   "app",
				UserID:    "user",
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "sess-2",
				AppName:   "app",
				UserID:    "user",
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}

	server := New(map[string]agent.Agent{}, WithSessionService(customSvc))

	listSessionsPath := "/apps/app/users/user/sessions"
	req := httptest.NewRequest(http.MethodGet, listSessionsPath, nil)
	req = mux.SetURLVars(req, map[string]string{
		"appName": "app",
		"userId":  "user",
	})
	w := httptest.NewRecorder()

	server.handleListSessions(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var sessionsResp []schema.ADKSession
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &sessionsResp))
	assert.Equal(t, 2, len(sessionsResp))
	ids := []string{sessionsResp[0].ID, sessionsResp[1].ID}
	assert.Contains(t, ids, "sess-1")
	assert.Contains(t, ids, "sess-2")
}

func TestHandleGetSession_NotFound(t *testing.T) {
	customSvc := &mockSessionService{}
	server := New(map[string]agent.Agent{}, WithSessionService(customSvc))

	getPath := "/apps/app/users/user/sessions/unknown"
	req := httptest.NewRequest(http.MethodGet, getPath, nil)
	req = mux.SetURLVars(req, map[string]string{
		"appName":   "app",
		"userId":    "user",
		"sessionId": "unknown",
	})
	w := httptest.NewRecorder()

	server.handleGetSession(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleGetSession_Success(t *testing.T) {
	now := time.Now()
	sessionObj := &session.Session{
		ID:        "sess-1",
		AppName:   "app",
		UserID:    "user",
		CreatedAt: now,
		UpdatedAt: now,
		State:     session.StateMap{"key": []byte("value")},
	}

	customSvc := &mockSessionService{
		getSessionResult: sessionObj,
	}
	server := New(map[string]agent.Agent{}, WithSessionService(customSvc))

	getSessionPath := "/apps/app/users/user/sessions/sess-1"
	req := httptest.NewRequest(http.MethodGet, getSessionPath, nil)
	req = mux.SetURLVars(req, map[string]string{
		"appName":   "app",
		"userId":    "user",
		"sessionId": "sess-1",
	})
	w := httptest.NewRecorder()

	server.handleGetSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp schema.ADKSession
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "sess-1", resp.ID)
	assert.Equal(t, "app", resp.AppName)
	assert.Equal(t, "user", resp.UserID)
}
