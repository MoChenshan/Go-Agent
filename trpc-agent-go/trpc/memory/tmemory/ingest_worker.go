package tmemory

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

type ingestJob struct {
	Ctx      context.Context
	Session  *session.Session
	LatestTs time.Time
	Messages []scannedMessage
	// OnSuccess, if non-nil, is invoked synchronously after a successful
	// ingest. It is used by the service to advance the per-session
	// watermark only when data has actually been accepted by tMemory.
	OnSuccess func()
	// OnFailure, if non-nil, is invoked synchronously after the worker
	// gives up on this job (ingest returned an error). It is used by
	// the service to release per-session in-flight bookkeeping so that
	// the next IngestSession can retry the same un-watermarked messages.
	OnFailure func()
}

type ingestWorker struct {
	c    *client
	opts serviceOpts

	jobChans []chan *ingestJob
	timeout  time.Duration

	mu      sync.RWMutex
	wg      sync.WaitGroup
	started bool
}

func newIngestWorker(c *client, opts serviceOpts) *ingestWorker {
	num := opts.asyncIngestNum
	if num <= 0 {
		num = defaultAsyncIngestNum
	}
	queueSize := opts.ingestQueueSize
	if queueSize <= 0 {
		queueSize = defaultIngestQueueSize
	}
	w := &ingestWorker{
		c:        c,
		opts:     opts,
		timeout:  opts.ingestJobTimeout,
		jobChans: make([]chan *ingestJob, num),
	}
	for i := 0; i < num; i++ {
		w.jobChans[i] = make(chan *ingestJob, queueSize)
	}
	w.start()
	return w
}

func (w *ingestWorker) start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return
	}
	w.wg.Add(len(w.jobChans))
	for _, ch := range w.jobChans {
		go func(ch chan *ingestJob) {
			defer w.wg.Done()
			for job := range ch {
				w.process(job)
			}
		}(ch)
	}
	w.started = true
}

func (w *ingestWorker) Stop() {
	w.mu.Lock()
	if !w.started || len(w.jobChans) == 0 {
		w.mu.Unlock()
		return
	}
	for _, ch := range w.jobChans {
		close(ch)
	}
	w.started = false
	w.mu.Unlock()
	w.wg.Wait()
}

func (w *ingestWorker) tryEnqueue(ctx context.Context, job *ingestJob) bool {
	if job == nil {
		return true
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return false
		}
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.started || len(w.jobChans) == 0 {
		return false
	}
	idx := 0
	if job.Session != nil {
		idx = job.Session.Hash
	}
	if idx < 0 {
		idx = -idx
	}
	idx = idx % len(w.jobChans)
	select {
	case w.jobChans[idx] <- job:
		return true
	default:
		return false
	}
}

func (w *ingestWorker) process(job *ingestJob) {
	if job == nil || job.Session == nil {
		return
	}
	// Recover from any panic inside ingest so a single bad job cannot
	// take down the worker goroutine and, more importantly, cannot
	// leak the per-session in-flight slot. Routing the panic through
	// OnFailure mirrors the regular failure path: the slot is
	// released and the watermark stays put, so the next IngestSession
	// call will re-scan the same messages.
	defer func() {
		if r := recover(); r != nil {
			log.ErrorfContext(context.Background(),
				"tmemory: panic in ingest worker for session %s: %v",
				job.Session.ID, r)
			if job.OnFailure != nil {
				job.OnFailure()
			}
		}
	}()
	ctx := job.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if w.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.timeout)
		defer cancel()
	}
	if err := w.ingest(ctx, job.Session, job.LatestTs, job.Messages); err != nil {
		log.WarnfContext(ctx, "tmemory: ingest failed for session %s: %v",
			job.Session.ID, err)
		if job.OnFailure != nil {
			job.OnFailure()
		}
		return
	}
	if job.OnSuccess != nil {
		job.OnSuccess()
	}
}

func (w *ingestWorker) ingest(
	ctx context.Context,
	sess *session.Session,
	latestTs time.Time,
	messages []scannedMessage,
) error {
	if sess == nil {
		return fmt.Errorf("tmemory: ingest called with nil session")
	}
	dialogue := make([]dialogueTurn, 0, len(messages))
	for _, m := range messages {
		content := messageText(m.Message)
		if content == "" {
			continue
		}
		dialogue = append(dialogue, dialogueTurn{
			Role:        m.Message.Role.String(),
			Name:        roleToName(m.Message.Role),
			Timestamp:   m.Timestamp.Format(time.RFC3339),
			Content:     content,
			Attachments: []attachment{},
		})
	}
	if len(dialogue) == 0 {
		return nil
	}

	bizID := w.opts.bizID
	if bizID == "" {
		bizID = sess.AppName
	}

	req := ingestRequest{
		// Per-batch trace id: stable across retries of the same batch
		// (so tMemory can dedupe transient retries server-side), but
		// unique across distinct batches of the same session.
		BizTraceID: buildBizTraceID(sess, latestTs),
		Metadata: ingestMetadata{
			BizID:     bizID,
			UserID:    sess.UserID,
			SessionID: sess.ID,
			Source:    w.opts.source,
		},
		Dialogue:   dialogue,
		StrategyID: w.opts.strategyID,
	}

	var resp ingestResponse
	// Ingest is idempotent at the tMemory side via biz_trace_id, so we
	// retry transient failures (429/5xx/network) on the POST.
	if err := w.c.doJSONIdempotent(ctx, http.MethodPost, "/v1/data/add", req, &resp); err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("tmemory: ingest failed: code=%d message=%s", resp.Code, resp.Message)
	}
	log.DebugfContext(ctx, "tmemory: ingest success, request_id=%s", resp.Data.RequestID)
	return nil
}

// buildBizTraceID composes a per-batch trace id for ingest requests.
//
// Format: trpc-agent-go-{AppName}-{SessionID}-{LatestTsUnixNano}.
//
// The latest timestamp suffix gives each ingest batch a distinct trace
// id (so server-side status queries / dedup can tell batches apart),
// while keeping it stable for retries of the same batch.
func buildBizTraceID(sess *session.Session, latestTs time.Time) string {
	if sess == nil {
		return ""
	}
	if latestTs.IsZero() {
		// Fall back to a per-call timestamp if we somehow have no
		// batch watermark; this still satisfies uniqueness.
		latestTs = time.Now()
	}
	return fmt.Sprintf("trpc-agent-go-%s-%s-%d",
		sess.AppName, sess.ID, latestTs.UnixNano())
}
