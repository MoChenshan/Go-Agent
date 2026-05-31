//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package debug provides a HTTP server for debugging and testing.
package debug

import (
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

// Server exposes HTTP endpoints compatible with the ADK Web UI. Internally it
// reuses the trpc-agent-go components for sessions, runners and events.
//
// This server is intended for debugging and manual testing.
type Server struct {
	agents map[string]agent.Agent
	router *mux.Router

	mu      sync.RWMutex
	runners map[string]runner.Runner

	sessionSvc session.Service
	runnerOpts []runner.Option // Extra options applied when creating a runner.

	traces         map[string]attribute.Set // key: event_id
	memoryExporter *inMemoryExporter
}

// Option configures the Server instance.
type Option func(*Server)

// WithSessionService allows providing a custom session storage backend.
// If omitted, an in-memory implementation is used.
func WithSessionService(svc session.Service) Option {
	return func(s *Server) { s.sessionSvc = svc }
}

// WithRunnerOptions appends additional runner.Option values applied when the
// server lazily constructs a Runner for an agent.
func WithRunnerOptions(opts ...runner.Option) Option {
	return func(s *Server) { s.runnerOpts = append(s.runnerOpts, opts...) }
}

// New creates a new debug HTTP server with explicit agent registration. The
// behaviour can be tweaked via functional options.
func New(agents map[string]agent.Agent, opts ...Option) *Server {
	s := &Server{
		agents:         agents,
		router:         mux.NewRouter(),
		runners:        make(map[string]runner.Runner),
		traces:         make(map[string]attribute.Set),
		memoryExporter: newInMemoryExporter(),
		sessionSvc:     sessioninmemory.NewSessionService(),
	}

	for _, opt := range opts {
		opt(s)
	}

	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders: []string{"*"},
		ExposedHeaders: []string{"Content-Length", "Content-Type"},
	})
	// Add CORS middleware for ADK Web compatibility.
	s.router.Use(c.Handler)
	// Register all REST endpoints.
	s.registerRoutes()

	var tracerProvider *sdktrace.TracerProvider
	if _, ok := atrace.TracerProvider.(noop.TracerProvider); ok {
		tracerProvider = sdktrace.NewTracerProvider()
	} else if tp, ok := atrace.TracerProvider.(*sdktrace.TracerProvider); ok {
		tracerProvider = tp
	} else {
		log.Errorf(
			"atrace.Tracer: %T provider is not sdktrace.TracerProvider",
			atrace.TracerProvider,
		)
		tracerProvider = sdktrace.NewTracerProvider()
	}

	tracerProvider.RegisterSpanProcessor(
		sdktrace.NewSimpleSpanProcessor(
			newApiServerSpanExporter(s.traces),
		),
	)
	tracerProvider.RegisterSpanProcessor(
		sdktrace.NewSimpleSpanProcessor(s.memoryExporter),
	)
	atrace.TracerProvider = tracerProvider
	atrace.Tracer = atrace.TracerProvider.Tracer(instrumentName)

	return s
}

// Handler returns the http.Handler for the server.
func (s *Server) Handler() http.Handler { return s.router }

// registerRoutes sets up all REST endpoints expected by ADK Web.
func (s *Server) registerRoutes() {
	s.router.HandleFunc("/list-apps", s.handleListApps).Methods(http.MethodGet)

	// Session APIs.
	s.router.HandleFunc(
		"/apps/{appName}/users/{userId}/sessions",
		s.handleListSessions,
	).Methods(http.MethodGet)
	s.router.HandleFunc(
		"/apps/{appName}/users/{userId}/sessions",
		s.handleCreateSession,
	).Methods(http.MethodPost)
	s.router.HandleFunc(
		"/apps/{appName}/users/{userId}/sessions/{sessionId}",
		s.handleGetSession,
	).Methods(http.MethodGet)

	// Debug APIs.
	s.router.HandleFunc(
		"/debug/trace/{event_id}",
		s.handleEventTrace,
	).Methods(http.MethodGet)
	s.router.HandleFunc(
		"/debug/trace/session/{session_id}",
		s.handleSessionTrace,
	).Methods(http.MethodGet)

	// Runner APIs.
	s.router.HandleFunc("/run", s.handleRun).Methods(http.MethodPost)
	s.router.HandleFunc("/run_sse", s.handleRunSSE).Methods(http.MethodPost)

	// OPTIONS handlers to allow CORS pre-flight.
	preflight := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
	s.router.HandleFunc("/run", preflight).Methods(http.MethodOptions)
	s.router.HandleFunc("/run_sse", preflight).Methods(http.MethodOptions)
}
