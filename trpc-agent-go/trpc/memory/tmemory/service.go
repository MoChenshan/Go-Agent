// Package tmemory provides tMemory integration for trpc-agent-go.
package tmemory

import (
	"context"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Service provides tMemory integration for trpc-agent-go.
// It implements session.Ingestor for asynchronous session ingestion and
// exposes read-only memory tools (memory_search) for the agent.
type Service struct {
	opts serviceOpts
	c    *client

	ingestWorker *ingestWorker

	precomputedTools []tool.Tool

	// inflight tracks sessions that already have an ingest job in flight,
	// so concurrent IngestSession calls for the same session do not
	// duplicate work or race on the watermark. Keys are sessionKey(sess).
	inflight sync.Map // map[string]struct{}

	// watermarks holds the per-session "last successfully ingested
	// event timestamp". It lives inside the Service (not in
	// session.State) because most SessionService implementations
	// hand callers a sess.Clone() on every turn, so sess.SetState
	// changes never round-trip back to storage and would leave the
	// watermark perpetually unset.
	//
	// Key format: sessionKey(sess); value: time.Time.
	watermarks sync.Map
}

// NewService creates a new tMemory service.
func NewService(options ...ServiceOpt) (*Service, error) {
	opts := defaultOptions.clone()
	for _, opt := range options {
		opt(&opts)
	}
	resolveOptsFromEnv(&opts)

	c, err := newClient(opts)
	if err != nil {
		return nil, err
	}
	svc := &Service{opts: opts, c: c}
	svc.ingestWorker = newIngestWorker(c, opts)
	svc.precomputedTools = buildReadOnlyTools(svc)
	return svc, nil
}

// Tools returns the tMemory read-only tools exposed to the agent.
func (s *Service) Tools() []tool.Tool {
	return append([]tool.Tool(nil), s.precomputedTools...)
}

// IngestSession enqueues session transcript ingestion into tMemory.
// This implements the session.Ingestor interface.
//
// Concurrency / At-Least-Once semantics:
//   - For a given session, at most one ingest is in flight at a time.
//     Concurrent calls that observe an in-flight job return immediately;
//     the next IngestSession after the in-flight job completes will
//     re-scan and pick up any messages that were skipped.
//   - The per-session watermark is advanced only after a successful
//     ingest, so transient failures (HTTP errors, server-side errors,
//     queue overflow with canceled caller) keep the messages eligible
//     for the next scan.
//   - The watermark is held in Service.watermarks (process-local)
//     instead of session.State because most SessionService backends
//     hand out sess.Clone() on every turn, so writes to sess.State do
//     not round-trip to storage.
func (s *Service) IngestSession(ctx context.Context, sess *session.Session, _ ...session.IngestOption) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.ingestWorker == nil || sess == nil {
		return nil
	}
	if sess.AppName == "" || sess.UserID == "" {
		return nil
	}

	key := sessionKey(sess)
	if _, busy := s.inflight.LoadOrStore(key, struct{}{}); busy {
		// Another ingest for this session is already enqueued or
		// running. Skip this round; the next IngestSession call after
		// it completes will scan again from the same watermark and
		// pick up any messages we skip here.
		log.DebugfContext(ctx, "tmemory: ingest already in-flight for %s, skipping", key)
		return nil
	}
	// We "own" the in-flight slot from now on. Unless we successfully
	// hand it off to the worker, we must release it here.
	releaseSlot := true
	defer func() {
		if releaseSlot {
			s.inflight.Delete(key)
		}
	}()

	since := s.readWatermark(key)
	latestTs, messages := scanDeltaSince(sess, since)
	if len(messages) == 0 {
		return nil
	}

	onSuccess := func() {
		// Advance the watermark only after tMemory has actually
		// accepted this batch, so failed ingests remain re-scannable.
		// The in-flight slot is released by decorateJobLifecycle.
		s.writeWatermark(key, latestTs)
	}

	// Detach the job context from the caller's cancellation: ingest is
	// an async background task that should outlive the request that
	// produced it (e.g. the chat turn). The worker bounds it with
	// ingestJobTimeout to prevent unbounded execution.
	job := &ingestJob{
		Ctx:       context.WithoutCancel(ctx),
		Session:   sess,
		LatestTs:  latestTs,
		Messages:  messages,
		OnSuccess: onSuccess,
	}
	// Install lifecycle hooks before enqueue: once the job is on the
	// channel, the worker may pick it up immediately, so OnSuccess /
	// OnFailure must already be in place to keep in-flight bookkeeping
	// correct.
	decorateJobLifecycle(job, key, &s.inflight)
	if s.ingestWorker.tryEnqueue(ctx, job) {
		// Worker now owns the in-flight slot; it will release it via
		// the lifecycle hooks installed above (OnSuccess on success,
		// OnFailure on failure).
		releaseSlot = false
		return nil
	}

	// Caller context already done (canceled/timeout). Skip; next call
	// will re-scan since watermark was not advanced.
	if err := ctx.Err(); err != nil {
		log.DebugfContext(ctx, "tmemory: skip ingest for user %s/%s due to context error: %v",
			sess.AppName, sess.UserID, err)
		return nil
	}

	// Queue full: process synchronously so the caller still gets a
	// chance to ingest before the watermark would advance.
	log.DebugfContext(ctx, "tmemory: ingest queue full, processing synchronously for user %s/%s",
		sess.AppName, sess.UserID)
	timeout := s.opts.ingestJobTimeout
	if timeout <= 0 {
		timeout = defaultIngestJobTimeout
	}
	syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := s.ingestWorker.ingest(syncCtx, sess, latestTs, messages); err != nil {
		return err
	}
	s.writeWatermark(key, latestTs)
	return nil
}

// Close stops background workers and releases resources, including any
// idle HTTP connections held by the underlying client.
func (s *Service) Close() error {
	if s.ingestWorker != nil {
		s.ingestWorker.Stop()
	}
	if s.c != nil {
		s.c.close()
	}
	return nil
}

// sessionKey returns a per-session key used to serialize ingests.
func sessionKey(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	return sess.AppName + "/" + sess.UserID + "/" + sess.ID
}

// readWatermark returns the last successfully ingested event timestamp
// for the given session key, or the zero time if none has been recorded
// yet (in which case the next scan starts from the beginning of the
// session's event log).
func (s *Service) readWatermark(key string) time.Time {
	v, ok := s.watermarks.Load(key)
	if !ok {
		return time.Time{}
	}
	t, _ := v.(time.Time)
	return t
}

// writeWatermark advances the per-session watermark to ts, but never
// moves it backwards (defensive against out-of-order callbacks).
func (s *Service) writeWatermark(key string, ts time.Time) {
	if ts.IsZero() {
		return
	}
	for {
		v, ok := s.watermarks.Load(key)
		if !ok {
			if _, loaded := s.watermarks.LoadOrStore(key, ts); !loaded {
				return
			}
			// Lost the race: re-read and try CAS-style update.
			continue
		}
		cur, _ := v.(time.Time)
		if !ts.After(cur) {
			return
		}
		if s.watermarks.CompareAndSwap(key, cur, ts) {
			return
		}
		// Another writer advanced the watermark; retry to ensure
		// monotonicity.
	}
}

// decorateJobLifecycle wraps the job's OnSuccess/OnFailure so that the
// in-flight slot for the session is released exactly once when the
// worker is done with the job — whether ingest succeeded or failed.
// This guarantees we don't leak in-flight markers and that a subsequent
// IngestSession after a failure can retry the same (un-watermarked)
// messages.
func decorateJobLifecycle(job *ingestJob, key string, inflight *sync.Map) {
	if job == nil {
		return
	}
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			inflight.Delete(key)
		})
	}
	originalOnSuccess := job.OnSuccess
	originalOnFailure := job.OnFailure
	job.OnSuccess = func() {
		if originalOnSuccess != nil {
			originalOnSuccess()
		}
		release()
	}
	job.OnFailure = func() {
		if originalOnFailure != nil {
			originalOnFailure()
		}
		release()
	}
}
