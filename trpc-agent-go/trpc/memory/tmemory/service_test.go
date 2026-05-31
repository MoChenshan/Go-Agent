package tmemory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

func TestNewService_MissingAPIKey(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "")

	_, err := NewService()
	require.Error(t, err)
}

func TestNewService_Success(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "test-key")

	svc, err := NewService(WithBizID("test-biz"))
	require.NoError(t, err)
	defer svc.Close()

	require.Equal(t, "test-biz", svc.opts.bizID)
	require.NotNil(t, svc.c)
	require.NotNil(t, svc.ingestWorker)
}

func TestService_Tools(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "test-key")

	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	tools := svc.Tools()
	require.Len(t, tools, 1)

	tools2 := svc.Tools()
	require.NotSame(t, &tools[0], &tools2[0])
}

func TestService_IngestSession_NilSession(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "test-key")

	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	require.NoError(t, svc.IngestSession(context.Background(), nil))
}

func TestService_IngestSession_EmptyFields(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "test-key")

	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	sess := session.NewSession("", "user", "sess")
	require.NoError(t, svc.IngestSession(context.Background(), sess))

	sess2 := session.NewSession("app", "", "sess")
	require.NoError(t, svc.IngestSession(context.Background(), sess2))
}

func TestService_IngestSession_NoMessages(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "test-key")

	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	sess := session.NewSession("app", "user", "sess")
	require.NoError(t, svc.IngestSession(context.Background(), sess))
}

func TestService_IngestSession_WithMessages(t *testing.T) {
	var ingestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ingestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r1"}})
	}))
	defer srv.Close()

	svc, err := NewService(
		WithAPIKey("test-key"),
		WithHost(srv.URL),
		WithHTTPClient(srv.Client()),
		WithBizID("test-biz"),
	)
	require.NoError(t, err)
	defer svc.Close()

	sess := session.NewSession("app", "user", "sess")
	now := time.Now()
	sess.Events = []event.Event{
		{
			Timestamp: now,
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "hello"}},
				},
			},
		},
		{
			Timestamp: now.Add(time.Second),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleAssistant, Content: "hi there"}},
				},
			},
		},
	}

	require.NoError(t, svc.IngestSession(context.Background(), sess))
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&ingestCount) == 1
	}, time.Second, 20*time.Millisecond)
}

func TestService_IngestSession_WatermarkPreventsReprocessing(t *testing.T) {
	var ingestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ingestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r1"}})
	}))
	defer srv.Close()

	svc, err := NewService(
		WithAPIKey("test-key"),
		WithHost(srv.URL),
		WithHTTPClient(srv.Client()),
	)
	require.NoError(t, err)
	defer svc.Close()

	sess := session.NewSession("app", "user", "sess")
	now := time.Now()
	sess.Events = []event.Event{
		{
			Timestamp: now,
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "hello"}},
				},
			},
		},
	}

	require.NoError(t, svc.IngestSession(context.Background(), sess))
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&ingestCount) == 1
	}, time.Second, 20*time.Millisecond)

	require.NoError(t, svc.IngestSession(context.Background(), sess))
	require.Never(t, func() bool {
		return atomic.LoadInt32(&ingestCount) > 1
	}, 300*time.Millisecond, 20*time.Millisecond)
}

func TestService_IngestSession_NilContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r"}})
	}))
	defer srv.Close()

	svc, err := NewService(
		WithAPIKey("test-key"),
		WithHost(srv.URL),
		WithHTTPClient(srv.Client()),
	)
	require.NoError(t, err)
	defer svc.Close()

	sess := session.NewSession("app", "user", "sess")
	sess.Events = []event.Event{
		{
			Timestamp: time.Now(),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "hello"}},
				},
			},
		},
	}

	// Intentionally pass a nil context to verify IngestSession's defensive
	// fallback (treating nil as context.Background()). Static analyzers
	// flag this with SA1012, but here it is the unit under test.
	//nolint:staticcheck // SA1012: testing nil-context fallback path
	require.NoError(t, svc.IngestSession(nil, sess))
}

func TestService_Close_Idempotent(t *testing.T) {
	t.Setenv(envTMemoryAPIKey, "test-key")

	svc, err := NewService()
	require.NoError(t, err)
	require.NoError(t, svc.Close())
	require.NoError(t, svc.Close())
}

// makeSessionWithEvent returns a session with a single user-message event
// at time `at`, so scanDeltaSince has something to find.
func makeSessionWithEvent(at time.Time) *session.Session {
	sess := session.NewSession("app", "user", "sess")
	sess.Events = []event.Event{
		{
			Timestamp: at,
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "hello"}},
				},
			},
		},
	}
	return sess
}

// TestService_IngestSession_FailureKeepsWatermark verifies that a failed
// ingest does NOT advance the per-session watermark, so the next
// IngestSession can re-scan and retry the same messages.
func TestService_IngestSession_FailureKeepsWatermark(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest) // non-retryable, ingest fails
	}))
	defer srv.Close()

	svc, err := NewService(
		WithAPIKey("k"),
		WithHost(srv.URL),
		WithHTTPClient(srv.Client()),
		WithIngestQueueSize(1),
		WithAsyncIngestNum(1),
		WithIngestJobTimeout(2*time.Second),
	)
	require.NoError(t, err)
	defer svc.Close()

	sess := makeSessionWithEvent(time.Now())

	require.NoError(t, svc.IngestSession(context.Background(), sess))

	// Wait for the async worker to finish (failure path).
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&attempts) >= 1
	}, 2*time.Second, 20*time.Millisecond, "worker never observed the request")
	// Give OnFailure a moment to run.
	time.Sleep(50 * time.Millisecond)

	// Watermark must NOT have been advanced.
	if got := svc.readWatermark(sessionKey(sess)); !got.IsZero() {
		t.Fatalf("expected watermark to remain unset after failed ingest, got %v", got)
	}
}

// TestService_IngestSession_SuccessAdvancesWatermark verifies the happy
// path: a successful ingest advances the watermark so the next
// IngestSession does not re-send the same messages.
func TestService_IngestSession_SuccessAdvancesWatermark(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r"}})
	}))
	defer srv.Close()

	svc, err := NewService(
		WithAPIKey("k"),
		WithHost(srv.URL),
		WithHTTPClient(srv.Client()),
		WithIngestQueueSize(1),
		WithAsyncIngestNum(1),
		WithIngestJobTimeout(2*time.Second),
	)
	require.NoError(t, err)
	defer svc.Close()

	eventAt := time.Now()
	sess := makeSessionWithEvent(eventAt)

	require.NoError(t, svc.IngestSession(context.Background(), sess))

	// Wait for watermark to be advanced.
	require.Eventually(t, func() bool {
		return !svc.readWatermark(sessionKey(sess)).IsZero()
	}, 2*time.Second, 20*time.Millisecond, "watermark was never advanced")

	// A second IngestSession should be a no-op (no new messages since
	// watermark) and not fire another HTTP call.
	before := atomic.LoadInt32(&attempts)
	require.NoError(t, svc.IngestSession(context.Background(), sess))
	time.Sleep(100 * time.Millisecond)
	after := atomic.LoadInt32(&attempts)
	if after != before {
		t.Fatalf("expected no extra ingest call when nothing new since watermark, before=%d after=%d", before, after)
	}
}

// TestService_IngestSession_ConcurrentSerialized verifies that two
// concurrent IngestSession calls for the same session do not produce
// two HTTP ingests; the second observes the in-flight slot and skips.
func TestService_IngestSession_ConcurrentSerialized(t *testing.T) {
	block := make(chan struct{})
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		<-block // hold the in-flight job until the test releases it
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r"}})
	}))
	defer srv.Close()

	svc, err := NewService(
		WithAPIKey("k"),
		WithHost(srv.URL),
		WithHTTPClient(srv.Client()),
		WithIngestQueueSize(2),
		WithAsyncIngestNum(1),
		WithIngestJobTimeout(5*time.Second),
	)
	require.NoError(t, err)
	defer svc.Close()

	sess := makeSessionWithEvent(time.Now())

	// First call enqueues the job (worker picks it up and blocks).
	require.NoError(t, svc.IngestSession(context.Background(), sess))
	// Wait until the worker actually started the HTTP call.
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&attempts) == 1
	}, 2*time.Second, 20*time.Millisecond)

	// Second call while the first is in flight: should observe the
	// in-flight slot and return without scheduling another HTTP call.
	require.NoError(t, svc.IngestSession(context.Background(), sess))
	// Give it some time to (incorrectly) trigger another call.
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 in-flight ingest, got %d", got)
	}

	// Release the worker and drain.
	close(block)
	require.Eventually(t, func() bool {
		return !svc.readWatermark(sessionKey(sess)).IsZero()
	}, 2*time.Second, 20*time.Millisecond)
}

// TestService_IngestSession_QueueFullSyncFallbackAdvancesWatermark
// covers the synchronous fallback path: when the async queue is full,
// IngestSession ingests synchronously and advances the watermark on
// success.
func TestService_IngestSession_QueueFullSyncFallbackAdvancesWatermark(t *testing.T) {
	hold := make(chan struct{})
	var totalCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&totalCalls, 1)
		// First call (the "blocker" sitting in the worker) waits on
		// hold so the queue actually fills.
		if n == 1 {
			<-hold
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r"}})
	}))
	defer srv.Close()

	svc, err := NewService(
		WithAPIKey("k"),
		WithHost(srv.URL),
		WithHTTPClient(srv.Client()),
		WithIngestQueueSize(1),
		WithAsyncIngestNum(1),
		WithIngestJobTimeout(5*time.Second),
	)
	require.NoError(t, err)
	defer svc.Close()

	// Session A: occupies the worker (and the in-flight slot for A).
	sessA := makeSessionWithEvent(time.Now())
	sessA.ID = "sess-a"
	require.NoError(t, svc.IngestSession(context.Background(), sessA))
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&totalCalls) == 1
	}, 2*time.Second, 20*time.Millisecond)

	// Session B & C: B sits in the queue; C arrives when the queue
	// is full, forcing the synchronous fallback.
	sessB := makeSessionWithEvent(time.Now())
	sessB.ID = "sess-b"
	require.NoError(t, svc.IngestSession(context.Background(), sessB))

	sessC := makeSessionWithEvent(time.Now())
	sessC.ID = "sess-c"
	// This one should go through the sync fallback and succeed.
	require.NoError(t, svc.IngestSession(context.Background(), sessC))
	if got := svc.readWatermark(sessionKey(sessC)); got.IsZero() {
		t.Fatal("expected sync-fallback ingest to advance watermark for sessC")
	}

	// Drain the rest.
	close(hold)
}
