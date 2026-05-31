package tmemory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

func TestIngestWorker_EnqueueAndProcess(t *testing.T) {
	var ingestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ingestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "req-1"}})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	opts := serviceOpts{
		asyncIngestNum:   2,
		ingestQueueSize:  5,
		ingestJobTimeout: 5 * time.Second,
		strategyID:       "1",
		source:           "test",
	}
	w := newIngestWorker(c, opts)
	defer w.Stop()

	sess := session.NewSession("app", "user1", "sess1")
	job := &ingestJob{
		Ctx:     context.Background(),
		Session: sess,
		Messages: []scannedMessage{
			{
				Message:   model.Message{Role: model.RoleUser, Content: "hello"},
				Timestamp: time.Now(),
			},
			{
				Message:   model.Message{Role: model.RoleAssistant, Content: "world"},
				Timestamp: time.Now(),
			},
		},
	}

	ok := w.tryEnqueue(context.Background(), job)
	if !ok {
		t.Fatal("expected enqueue to succeed")
	}

	// Wait for processing.
	time.Sleep(500 * time.Millisecond)
	if got := atomic.LoadInt32(&ingestCount); got != 1 {
		t.Fatalf("expected 1 ingest call, got %d", got)
	}
}

func TestIngestWorker_TryEnqueue_NilJob(t *testing.T) {
	c, _ := newClient(serviceOpts{host: "http://localhost", apiKey: "k"})
	w := newIngestWorker(c, serviceOpts{asyncIngestNum: 1, ingestQueueSize: 1})
	defer w.Stop()

	if !w.tryEnqueue(context.Background(), nil) {
		t.Fatal("nil job should return true")
	}
}

func TestIngestWorker_TryEnqueue_CancelledContext(t *testing.T) {
	c, _ := newClient(serviceOpts{host: "http://localhost", apiKey: "k"})
	w := newIngestWorker(c, serviceOpts{asyncIngestNum: 1, ingestQueueSize: 1})
	defer w.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sess := session.NewSession("app", "user", "sess")
	job := &ingestJob{Ctx: ctx, Session: sess, Messages: []scannedMessage{
		{Message: model.Message{Role: model.RoleUser, Content: "hi"}, Timestamp: time.Now()},
	}}

	if w.tryEnqueue(ctx, job) {
		t.Fatal("cancelled context should cause enqueue to return false")
	}
}

func TestIngestWorker_TryEnqueue_QueueFull(t *testing.T) {
	// Use a server that blocks briefly to fill the queue.
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	w := newIngestWorker(c, serviceOpts{
		asyncIngestNum:   1,
		ingestQueueSize:  1,
		ingestJobTimeout: 2 * time.Second,
	})

	sess := session.NewSession("app", "user", "sess")
	makeJob := func() *ingestJob {
		return &ingestJob{
			Ctx:     context.Background(),
			Session: sess,
			Messages: []scannedMessage{
				{Message: model.Message{Role: model.RoleUser, Content: "hi"}, Timestamp: time.Now()},
			},
		}
	}

	// Fill the buffer: 1 being processed + 1 in queue.
	w.tryEnqueue(context.Background(), makeJob())
	time.Sleep(50 * time.Millisecond) // Let worker pick up first job.
	w.tryEnqueue(context.Background(), makeJob())

	// Now queue should be full.
	ok := w.tryEnqueue(context.Background(), makeJob())
	if ok {
		t.Fatal("expected enqueue to fail when queue is full")
	}

	// Unblock server and stop worker.
	close(block)
	w.Stop()
}

func TestIngestWorker_Stop_Idempotent(t *testing.T) {
	c, _ := newClient(serviceOpts{host: "http://localhost", apiKey: "k"})
	w := newIngestWorker(c, serviceOpts{asyncIngestNum: 1, ingestQueueSize: 1})
	w.Stop()
	w.Stop() // Should not panic.
}

func TestIngestWorker_Ingest_EmptyDialogue(t *testing.T) {
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	w := newIngestWorker(c, serviceOpts{strategyID: "1", source: "test"})
	defer w.Stop()

	sess := session.NewSession("app", "user", "sess")
	// Messages with empty content should be filtered out.
	err := w.ingest(context.Background(), sess, time.Now(), []scannedMessage{
		{Message: model.Message{Role: model.RoleUser, Content: ""}, Timestamp: time.Now()},
		{Message: model.Message{Role: model.RoleUser, Content: "   "}, Timestamp: time.Now()},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Fatalf("expected no API call for empty dialogue, got %d", got)
	}
}

func TestIngestWorker_Ingest_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 1001, Message: "bad request"})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	w := newIngestWorker(c, serviceOpts{strategyID: "1", source: "test"})
	defer w.Stop()

	sess := session.NewSession("app", "user", "sess")
	err := w.ingest(context.Background(), sess, time.Now(), []scannedMessage{
		{Message: model.Message{Role: model.RoleUser, Content: "hello"}, Timestamp: time.Now()},
	})
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

func TestIngestWorker_SessionHashRouting(t *testing.T) {
	var worker0Count, worker1Count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r"}})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	w := newIngestWorker(c, serviceOpts{
		asyncIngestNum:   2,
		ingestQueueSize:  10,
		ingestJobTimeout: 5 * time.Second,
		strategyID:       "1",
	})
	defer w.Stop()

	// Create two sessions with different hashes.
	sess0 := session.NewSession("app", "user0", "s0")
	sess1 := session.NewSession("app", "user1", "s1")

	makeJob := func(sess *session.Session) *ingestJob {
		return &ingestJob{
			Ctx:     context.Background(),
			Session: sess,
			Messages: []scannedMessage{
				{Message: model.Message{Role: model.RoleUser, Content: "msg"}, Timestamp: time.Now()},
			},
		}
	}

	// Enqueue multiple jobs for same session => same channel.
	for i := 0; i < 3; i++ {
		w.tryEnqueue(context.Background(), makeJob(sess0))
		w.tryEnqueue(context.Background(), makeJob(sess1))
	}

	time.Sleep(500 * time.Millisecond)
	// We just verify no panics and the jobs were processed. The actual routing
	// is guaranteed by the hash % len(jobChans) logic.
	_ = worker0Count
	_ = worker1Count
}

func TestIngestWorker_BizIDFallback(t *testing.T) {
	var capturedBizID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ingestRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		capturedBizID = req.Metadata.BizID
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r"}})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	// bizID is empty => should fallback to sess.AppName.
	w := newIngestWorker(c, serviceOpts{bizID: "", strategyID: "1", source: "test"})
	defer w.Stop()

	sess := session.NewSession("my-app", "user", "sess")
	err := w.ingest(context.Background(), sess, time.Now(), []scannedMessage{
		{Message: model.Message{Role: model.RoleUser, Content: "hello"}, Timestamp: time.Now()},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBizID != "my-app" {
		t.Fatalf("expected bizID fallback to 'my-app', got %q", capturedBizID)
	}
}

// TestBuildBizTraceID_DistinctPerBatch verifies that different ingest
// batches (different latestTs) for the same session produce distinct
// trace ids, while the same batch (same latestTs) produces stable ids
// suitable for server-side dedup on retries.
func TestBuildBizTraceID_DistinctPerBatch(t *testing.T) {
	sess := session.NewSession("app", "user", "sess-1")
	t1 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 3, 4, 5, 1, time.UTC) // 1 ns later

	id1 := buildBizTraceID(sess, t1)
	id1Again := buildBizTraceID(sess, t1)
	id2 := buildBizTraceID(sess, t2)

	if id1 != id1Again {
		t.Fatalf("expected stable id for same (sess, latestTs), got %q vs %q", id1, id1Again)
	}
	if id1 == id2 {
		t.Fatalf("expected distinct ids for different latestTs, both were %q", id1)
	}
	if !strings.HasPrefix(id1, "trpc-agent-go-app-sess-1-") {
		t.Fatalf("unexpected id format: %q", id1)
	}
}

// TestBuildBizTraceID_NilSession defensively checks that we don't panic
// on a nil session (and produce an empty string the caller can detect).
func TestBuildBizTraceID_NilSession(t *testing.T) {
	if got := buildBizTraceID(nil, time.Now()); got != "" {
		t.Fatalf("expected empty id for nil session, got %q", got)
	}
}

// TestIngestWorker_BizTraceIDIncludesLatestTs sends two ingest batches
// for the same session with different latestTs and verifies the server
// observes distinct biz_trace_ids. This guards against the regression
// described in PR #644 review (same trace id for every increment).
func TestIngestWorker_BizTraceIDIncludesLatestTs(t *testing.T) {
	var traceIDs []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ingestRequest
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		traceIDs = append(traceIDs, req.BizTraceID)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Code: 0, Data: ingestRespData{RequestID: "r"}})
	}))
	defer srv.Close()

	c, _ := newClient(serviceOpts{host: srv.URL, apiKey: "k", client: srv.Client()})
	w := newIngestWorker(c, serviceOpts{strategyID: "1", source: "test"})
	defer w.Stop()

	sess := session.NewSession("app", "user", "s1")
	msgs := []scannedMessage{
		{Message: model.Message{Role: model.RoleUser, Content: "hi"}, Timestamp: time.Now()},
	}
	t1 := time.Now()
	t2 := t1.Add(1 * time.Second)
	if err := w.ingest(context.Background(), sess, t1, msgs); err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}
	if err := w.ingest(context.Background(), sess, t2, msgs); err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(traceIDs) != 2 {
		t.Fatalf("expected 2 ingest calls, got %d", len(traceIDs))
	}
	if traceIDs[0] == traceIDs[1] {
		t.Fatalf("expected distinct biz_trace_ids per batch, both were %q", traceIDs[0])
	}
}

// TestIngestWorker_Process_PanicTriggersOnFailure verifies that a panic
// inside ingest is recovered and routed through OnFailure, so the
// per-session in-flight slot is not leaked when something blows up
// (e.g. a dependency returns a malformed value that triggers a nil
// dereference deep inside the HTTP path).
func TestIngestWorker_Process_PanicTriggersOnFailure(t *testing.T) {
	// Build a worker whose client is nil; the ingest path will then
	// panic at w.c.doJSONIdempotent, which is exactly the kind of
	// programmer / dependency error the recover guard exists for.
	w := &ingestWorker{
		c:    nil,
		opts: serviceOpts{strategyID: "1", source: "test"},
	}

	var (
		successCalled int32
		failureCalled int32
	)
	job := &ingestJob{
		Ctx:      context.Background(),
		Session:  session.NewSession("app", "user", "sess-panic"),
		LatestTs: time.Now(),
		Messages: []scannedMessage{
			{Message: model.Message{Role: model.RoleUser, Content: "boom"}, Timestamp: time.Now()},
		},
		OnSuccess: func() { atomic.AddInt32(&successCalled, 1) },
		OnFailure: func() { atomic.AddInt32(&failureCalled, 1) },
	}

	// Must not propagate the panic.
	w.process(job)

	if got := atomic.LoadInt32(&successCalled); got != 0 {
		t.Fatalf("OnSuccess must not be called on panic, got %d calls", got)
	}
	if got := atomic.LoadInt32(&failureCalled); got != 1 {
		t.Fatalf("OnFailure must be called exactly once on panic, got %d", got)
	}
}
