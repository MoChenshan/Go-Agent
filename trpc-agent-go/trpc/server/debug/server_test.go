//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package debug

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	testAgentName = "test-agent"
	testAgentDesc = "test description"
)

// mockAgent is a simple mock agent for testing.
type mockAgent struct {
	name        string
	description string
}

func (m *mockAgent) Info() agent.Info {
	return agent.Info{
		Name:        m.name,
		Description: m.description,
	}
}

func (m *mockAgent) Tools() []tool.Tool { return nil }

func (m *mockAgent) SubAgents() []agent.Agent { return nil }

func (m *mockAgent) FindSubAgent(name string) agent.Agent { return nil }

func (m *mockAgent) Run(
	ctx context.Context,
	inv *agent.Invocation,
) (<-chan *event.Event, error) {
	// Return a simple event channel for testing.
	events := make(chan *event.Event, 1)
	go func() {
		defer close(events)
		events <- &event.Event{
			Response: &model.Response{
				Choices: []model.Choice{{
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "test response",
					},
				}},
			},
		}
	}()
	return events, nil
}

type fakeRunner struct {
	events []*event.Event
	err    error
}

type ctxCapturingRunner struct {
	ctx context.Context
}

func (f *fakeRunner) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan *event.Event, len(f.events))
	go func() {
		for _, evt := range f.events {
			ch <- evt
		}
		close(ch)
	}()
	return ch, nil
}

func (f *fakeRunner) Close() error { return nil }

func (f *ctxCapturingRunner) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	f.ctx = ctx
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func (f *ctxCapturingRunner) Close() error {
	return nil
}

type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

type noFlusherRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newNoFlusherRecorder() *noFlusherRecorder {
	return &noFlusherRecorder{header: make(http.Header)}
}

func (r *noFlusherRecorder) Header() http.Header {
	return r.header
}

func (r *noFlusherRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}

func (r *noFlusherRecorder) WriteHeader(status int) {
	r.status = status
}

func (r *noFlusherRecorder) StatusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func (r *noFlusherRecorder) BodyString() string {
	return r.body.String()
}

func TestNew(t *testing.T) {
	agents := map[string]agent.Agent{
		testAgentName: &mockAgent{
			name:        testAgentName,
			description: testAgentDesc,
		},
	}

	server := New(agents)
	assert.NotNilf(t, server, "New() returned nil")

	assert.Len(t, server.agents, 1)

	assert.NotNilf(t, server.agents[testAgentName], "agent not found in server")
}

func TestNew_WithOptions(t *testing.T) {
	agents := map[string]agent.Agent{
		testAgentName: &mockAgent{
			name:        testAgentName,
			description: testAgentDesc,
		},
	}

	// Test with custom session service.
	customSessionSvc := &mockSessionService{}
	server := New(agents, WithSessionService(customSessionSvc))

	assert.Equal(t, customSessionSvc, server.sessionSvc)
}

func TestServer_Handler(t *testing.T) {
	agents := map[string]agent.Agent{
		testAgentName: &mockAgent{
			name:        testAgentName,
			description: testAgentDesc,
		},
	}

	server := New(agents)
	handler := server.Handler()

	assert.NotNilf(t, handler, "Handler() returned nil")
}

func recordEvalSession(
	t *testing.T,
	srv *Server,
	appName string,
	userID string,
	sessionID string,
) *session.Session {
	ctx := context.Background()
	sess, err := srv.sessionSvc.CreateSession(ctx, session.Key{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	}, session.StateMap{})
	require.NoError(t, err)

	require.NoError(
		t,
		srv.sessionSvc.AppendEvent(
			ctx,
			sess,
			newUserMessageEvent("invocation-1", "calc add 1 2"),
		),
	)
	require.NoError(
		t,
		srv.sessionSvc.AppendEvent(
			ctx,
			sess,
			newToolCallEvent("invocation-1"),
		),
	)
	require.NoError(
		t,
		srv.sessionSvc.AppendEvent(
			ctx,
			sess,
			newAssistantFinalEvent("invocation-1", "calc result: 3"),
		),
	)
	return sess
}

func newUserMessageEvent(invocationID, content string) *event.Event {
	rsp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleUser,
				Content: content,
			},
		}},
		Done: true,
	}
	return event.NewResponseEvent(invocationID, string(model.RoleUser), rsp)
}

func newToolCallEvent(invocationID string) *event.Event {
	args := json.RawMessage(`{"operation":"add","a":1,"b":2}`)
	rsp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{
					{
						ID: "tool-call-1",
						Function: model.FunctionDefinitionParam{
							Name:      "calculator",
							Arguments: args,
						},
					},
				},
			},
		}},
	}
	return event.NewResponseEvent(invocationID, string(model.RoleAssistant), rsp)
}

func newAssistantFinalEvent(invocationID, content string) *event.Event {
	rsp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: content,
			},
		}},
		Done: true,
	}
	return event.NewResponseEvent(invocationID, string(model.RoleAssistant), rsp)
}

type fakeEvalRunner struct {
	events []*event.Event
}

func (f *fakeEvalRunner) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, len(f.events))
	for _, evt := range f.events {
		e := *evt
		ch <- &e
	}
	close(ch)
	return ch, nil
}

func (f *fakeEvalRunner) Close() error {
	return nil
}

func performJSONRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	payload any,
) *httptest.ResponseRecorder {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewReader(data)
	}
	return performRequest(t, handler, method, path, body)
}

func performRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body io.Reader,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func decodeBody(t *testing.T, r io.Reader, v any) {
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, v))
}

func newRunnerToolCallEvent(invocationID string) *event.Event {
	args := json.RawMessage(`{"operation":"add","a":1,"b":2}`)
	resp := &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID: "runner-tool",
					Function: model.FunctionDefinitionParam{
						Name:      "calculator",
						Arguments: args,
					},
				}},
			},
		}},
	}
	return &event.Event{Response: resp, InvocationID: invocationID}
}

func newRunnerFinalEvent(invocationID, content string) *event.Event {
	resp := &model.Response{
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: content,
			},
		}},
	}
	return &event.Event{Response: resp, InvocationID: invocationID}
}

// mockSessionService is a simple mock session service for testing.
type mockSessionService struct {
	sessions           map[string]*session.Session
	listSessionsResult []*session.Session
	listSessionsErr    error
	getSessionResult   *session.Session
	getSessionErr      error
}

func (m *mockSessionService) CreateSession(
	ctx context.Context,
	key session.Key,
	state session.StateMap,
	options ...session.Option,
) (*session.Session, error) {
	now := time.Now()
	sess := &session.Session{
		ID:        "mock-session-id",
		AppName:   key.AppName,
		UserID:    key.UserID,
		CreatedAt: now,
		UpdatedAt: now,
		State:     state,
	}
	return sess, nil
}

func (m *mockSessionService) GetSession(
	ctx context.Context,
	key session.Key,
	options ...session.Option,
) (*session.Session, error) {
	return m.getSessionResult, m.getSessionErr
}

func (m *mockSessionService) ListSessions(
	ctx context.Context,
	userKey session.UserKey,
	options ...session.Option,
) ([]*session.Session, error) {
	if m.listSessionsResult != nil {
		return m.listSessionsResult, m.listSessionsErr
	}
	return []*session.Session{}, m.listSessionsErr
}

func (m *mockSessionService) DeleteSession(
	ctx context.Context,
	key session.Key,
	options ...session.Option,
) error {
	return nil
}

func (m *mockSessionService) UpdateAppState(
	ctx context.Context,
	appName string,
	state session.StateMap,
) error {
	return nil
}

func (m *mockSessionService) DeleteAppState(
	ctx context.Context,
	appName string,
	key string,
) error {
	return nil
}

func (m *mockSessionService) ListAppStates(
	ctx context.Context,
	appName string,
) (session.StateMap, error) {
	return session.StateMap{}, nil
}

func (m *mockSessionService) UpdateUserState(
	ctx context.Context,
	userKey session.UserKey,
	state session.StateMap,
) error {
	return nil
}

func (m *mockSessionService) ListUserStates(
	ctx context.Context,
	userKey session.UserKey,
) (session.StateMap, error) {
	return session.StateMap{}, nil
}

func (m *mockSessionService) DeleteUserState(
	ctx context.Context,
	userKey session.UserKey,
	key string,
) error {
	return nil
}

func (m *mockSessionService) UpdateSessionState(
	ctx context.Context,
	key session.Key,
	state session.StateMap,
) error {
	return nil
}

func (m *mockSessionService) AppendEvent(
	ctx context.Context,
	sess *session.Session,
	event *event.Event,
	options ...session.Option,
) error {
	return nil
}

func (m *mockSessionService) Close() error {
	return nil
}

// Implement new session.Service summary methods.
func (m *mockSessionService) CreateSessionSummary(
	ctx context.Context,
	sess *session.Session,
	filterKey string,
	force bool,
) error {
	return nil
}

func (m *mockSessionService) EnqueueSummaryJob(
	ctx context.Context,
	sess *session.Session,
	filterKey string,
	force bool,
) error {
	return nil
}

func (m *mockSessionService) GetSessionSummaryText(
	ctx context.Context,
	sess *session.Session,
	opts ...session.SummaryOption,
) (string, bool) {
	return "", false
}

func TestServerGetRunnerCache(t *testing.T) {
	server := New(map[string]agent.Agent{
		"app": &mockAgent{name: "app"},
	})

	first, err := server.getRunner("app")
	assert.NoError(t, err)

	second, err := server.getRunner("app")
	assert.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestServerGetRunnerMissingAgent(t *testing.T) {
	server := New(map[string]agent.Agent{})

	r, err := server.getRunner("missing")
	assert.Nil(t, r)
	assert.EqualError(t, err, "agent not found")
}

func TestWithRunnerOptionsAppends(t *testing.T) {
	flag := false
	opt := WithRunnerOptions(func(o *runner.Options) {
		flag = true
	})
	server := New(map[string]agent.Agent{}, opt)

	assert.Len(t, server.runnerOpts, 1)
	server.runnerOpts[0](&runner.Options{})
	assert.True(t, flag)
}
