package backends

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/memory"
	memextractor "trpc.group/trpc-go/trpc-agent-go/memory/extractor"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

const (
	defaultAsyncMemoryNum   = 1
	defaultMemoryQueueSize  = 10
	defaultMemoryJobTimeout = 30 * time.Second

	memoryNotFoundErrPart = "memory with id"
	notFoundErrPart       = "not found"
)

type sqliteAutoMemoryConfig struct {
	Extractor        memextractor.MemoryExtractor
	EnabledTools     map[string]struct{}
	AsyncMemoryNum   int
	MemoryQueueSize  int
	MemoryJobTimeout time.Duration
}

type sqliteAutoMemoryJob struct {
	Ctx      context.Context
	UserKey  memory.UserKey
	Session  *session.Session
	LatestTs time.Time
	Messages []model.Message
}

type sqliteMemoryOperator interface {
	ReadMemories(
		ctx context.Context,
		userKey memory.UserKey,
		limit int,
	) ([]*memory.Entry, error)
	SearchMemories(
		ctx context.Context,
		userKey memory.UserKey,
		query string,
		opts ...memory.SearchOption,
	) ([]*memory.Entry, error)
	AddMemory(
		ctx context.Context,
		userKey memory.UserKey,
		memoryText string,
		topics []string,
		opts ...memory.AddOption,
	) error
	UpdateMemory(
		ctx context.Context,
		memoryKey memory.Key,
		memoryText string,
		topics []string,
		opts ...memory.UpdateOption,
	) error
	DeleteMemory(ctx context.Context, memoryKey memory.Key) error
	ClearMemories(ctx context.Context, userKey memory.UserKey) error
}

type sqliteAutoMemoryWorker struct {
	config   sqliteAutoMemoryConfig
	operator sqliteMemoryOperator

	mu      sync.RWMutex
	started bool
	jobCh   []chan *sqliteAutoMemoryJob
	wg      sync.WaitGroup
}

func newSQLiteAutoMemoryWorker(
	config sqliteAutoMemoryConfig,
	operator sqliteMemoryOperator,
) *sqliteAutoMemoryWorker {
	config.EnabledTools = cloneEnabledTools(config.EnabledTools)
	return &sqliteAutoMemoryWorker{
		config:   config,
		operator: operator,
	}
}

func (w *sqliteAutoMemoryWorker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started || w.config.Extractor == nil {
		return
	}

	workers := w.config.AsyncMemoryNum
	if workers <= 0 {
		workers = defaultAsyncMemoryNum
	}
	queueSize := w.config.MemoryQueueSize
	if queueSize <= 0 {
		queueSize = defaultMemoryQueueSize
	}

	w.jobCh = make([]chan *sqliteAutoMemoryJob, workers)
	w.wg.Add(workers)
	for i := 0; i < workers; i++ {
		w.jobCh[i] = make(chan *sqliteAutoMemoryJob, queueSize)
		go func(ch chan *sqliteAutoMemoryJob) {
			defer w.wg.Done()
			for job := range ch {
				w.processJob(job)
			}
		}(w.jobCh[i])
	}
	w.started = true
}

func (w *sqliteAutoMemoryWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return
	}
	for _, ch := range w.jobCh {
		close(ch)
	}
	w.wg.Wait()
	w.jobCh = nil
	w.started = false
}

func (w *sqliteAutoMemoryWorker) EnqueueJob(
	ctx context.Context,
	sess *session.Session,
) error {
	if w.config.Extractor == nil || sess == nil {
		return nil
	}

	userKey := memory.UserKey{
		AppName: sess.AppName,
		UserID:  sess.UserID,
	}
	if err := userKey.CheckUserKey(); err != nil {
		return nil
	}

	since := readLastExtractAt(sess)
	latestTs, messages := scanDeltaSince(sess, since)
	if len(messages) == 0 {
		return nil
	}

	extractCtx := &memextractor.ExtractionContext{
		UserKey:       userKey,
		Messages:      messages,
		LastExtractAt: nil,
	}
	if !since.IsZero() {
		sinceUTC := since.UTC()
		extractCtx.LastExtractAt = &sinceUTC
	}
	if !w.config.Extractor.ShouldExtract(extractCtx) {
		return nil
	}

	job := &sqliteAutoMemoryJob{
		Ctx:      context.WithoutCancel(ctx),
		UserKey:  userKey,
		Session:  sess,
		LatestTs: latestTs,
		Messages: messages,
	}
	if w.tryEnqueueJob(ctx, userKey, job) {
		return nil
	}

	if ctx.Err() != nil {
		return nil
	}

	timeout := w.config.MemoryJobTimeout
	if timeout <= 0 {
		timeout = defaultMemoryJobTimeout
	}
	syncCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		timeout,
	)
	defer cancel()

	if err := w.createAutoMemory(syncCtx, userKey, messages); err != nil {
		return err
	}
	writeLastExtractAt(sess, latestTs)
	return nil
}

func (w *sqliteAutoMemoryWorker) tryEnqueueJob(
	ctx context.Context,
	userKey memory.UserKey,
	job *sqliteAutoMemoryJob,
) bool {
	if ctx.Err() != nil {
		return false
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.started || len(w.jobCh) == 0 {
		return false
	}

	index := hashUserKey(userKey) % len(w.jobCh)
	select {
	case w.jobCh[index] <- job:
		return true
	default:
		return false
	}
}

func (w *sqliteAutoMemoryWorker) processJob(job *sqliteAutoMemoryJob) {
	defer func() {
		if r := recover(); r != nil {
			logAutoMemoryWarn(
				context.Background(),
				"panic in sqlite memory worker: %v",
				r,
			)
		}
	}()

	ctx := job.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := w.config.MemoryJobTimeout
	if timeout <= 0 {
		timeout = defaultMemoryJobTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := w.createAutoMemory(ctx, job.UserKey, job.Messages); err != nil {
		logAutoMemoryWarn(
			ctx,
			"sqlite auto_memory: job failed for user %s/%s: %v",
			job.UserKey.AppName,
			job.UserKey.UserID,
			err,
		)
		return
	}
	writeLastExtractAt(job.Session, job.LatestTs)
}

func (w *sqliteAutoMemoryWorker) createAutoMemory(
	ctx context.Context,
	userKey memory.UserKey,
	messages []model.Message,
) error {
	if w.config.Extractor == nil {
		return nil
	}

	existing, err := w.searchRelevantMemories(ctx, userKey, messages)
	if err != nil {
		logAutoMemoryWarn(
			ctx,
			"sqlite auto_memory: search existing memories failed "+
				"for user %s/%s: %v",
			userKey.AppName,
			userKey.UserID,
			err,
		)
		existing = nil
	}

	ops, err := w.config.Extractor.Extract(ctx, messages, existing)
	if err != nil {
		return fmt.Errorf("sqlite auto_memory: extract failed: %w", err)
	}

	for _, op := range ops {
		w.executeOperation(ctx, userKey, op)
	}
	return nil
}

func (w *sqliteAutoMemoryWorker) searchRelevantMemories(
	ctx context.Context,
	userKey memory.UserKey,
	messages []model.Message,
) ([]*memory.Entry, error) {
	query := buildSearchQuery(messages)
	if query == "" {
		return nil, nil
	}
	return w.operator.SearchMemories(ctx, userKey, query)
}

func buildSearchQuery(messages []model.Message) string {
	var builder strings.Builder
	for _, msg := range messages {
		if msg.Role != model.RoleUser || msg.Content == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(msg.Content)
	}
	return builder.String()
}

func (w *sqliteAutoMemoryWorker) executeOperation(
	ctx context.Context,
	userKey memory.UserKey,
	op *memextractor.Operation,
) {
	if op == nil {
		return
	}
	if !w.operationEnabled(op.Type) {
		return
	}

	switch op.Type {
	case memextractor.OperationAdd:
		if err := w.operator.AddMemory(
			ctx,
			userKey,
			op.Memory,
			op.Topics,
		); err != nil {
			logAutoMemoryWarn(
				ctx,
				"sqlite auto_memory: add failed for user %s/%s: %v",
				userKey.AppName,
				userKey.UserID,
				err,
			)
		}
	case memextractor.OperationUpdate:
		key := memory.Key{
			AppName:  userKey.AppName,
			UserID:   userKey.UserID,
			MemoryID: op.MemoryID,
		}
		err := w.operator.UpdateMemory(ctx, key, op.Memory, op.Topics)
		if isMemoryNotFoundError(err) &&
			w.toolEnabled(memory.AddToolName) {
			err = w.operator.AddMemory(
				ctx,
				userKey,
				op.Memory,
				op.Topics,
			)
		}
		if err != nil {
			logAutoMemoryWarn(
				ctx,
				"sqlite auto_memory: update failed for user %s/%s, "+
					"memory_id=%s: %v",
				userKey.AppName,
				userKey.UserID,
				op.MemoryID,
				err,
			)
		}
	case memextractor.OperationDelete:
		key := memory.Key{
			AppName:  userKey.AppName,
			UserID:   userKey.UserID,
			MemoryID: op.MemoryID,
		}
		if err := w.operator.DeleteMemory(ctx, key); err != nil {
			logAutoMemoryWarn(
				ctx,
				"sqlite auto_memory: delete failed for user %s/%s, "+
					"memory_id=%s: %v",
				userKey.AppName,
				userKey.UserID,
				op.MemoryID,
				err,
			)
		}
	case memextractor.OperationClear:
		if err := w.operator.ClearMemories(ctx, userKey); err != nil {
			logAutoMemoryWarn(
				ctx,
				"sqlite auto_memory: clear failed for user %s/%s: %v",
				userKey.AppName,
				userKey.UserID,
				err,
			)
		}
	}
}

func (w *sqliteAutoMemoryWorker) operationEnabled(
	op memextractor.OperationType,
) bool {
	switch op {
	case memextractor.OperationAdd:
		return w.toolEnabled(memory.AddToolName)
	case memextractor.OperationUpdate:
		return w.toolEnabled(memory.UpdateToolName)
	case memextractor.OperationDelete:
		return w.toolEnabled(memory.DeleteToolName)
	case memextractor.OperationClear:
		return w.toolEnabled(memory.ClearToolName)
	default:
		return false
	}
}

func (w *sqliteAutoMemoryWorker) toolEnabled(name string) bool {
	if len(w.config.EnabledTools) == 0 {
		return true
	}
	_, ok := w.config.EnabledTools[name]
	return ok
}

func isMemoryNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, memoryNotFoundErrPart) &&
		strings.Contains(msg, notFoundErrPart)
}

func cloneEnabledTools(
	in map[string]struct{},
) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for name := range in {
		out[name] = struct{}{}
	}
	return out
}

func hashUserKey(userKey memory.UserKey) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(userKey.AppName))
	_, _ = hasher.Write([]byte(userKey.UserID))
	return int(hasher.Sum32())
}

func readLastExtractAt(sess *session.Session) time.Time {
	raw, ok := sess.GetState(memory.SessionStateKeyAutoMemoryLastExtractAt)
	if !ok || len(raw) == 0 {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, string(raw))
	if err != nil {
		return time.Time{}
	}
	return ts
}

func writeLastExtractAt(sess *session.Session, ts time.Time) {
	sess.SetState(
		memory.SessionStateKeyAutoMemoryLastExtractAt,
		[]byte(ts.UTC().Format(time.RFC3339Nano)),
	)
}

func scanDeltaSince(
	sess *session.Session,
	since time.Time,
) (time.Time, []model.Message) {
	var (
		latestTs time.Time
		out      []model.Message
	)

	sess.EventMu.RLock()
	defer sess.EventMu.RUnlock()

	for _, evt := range sess.Events {
		if !since.IsZero() && !evt.Timestamp.After(since) {
			continue
		}
		if evt.Timestamp.After(latestTs) {
			latestTs = evt.Timestamp
		}
		if evt.Response == nil {
			continue
		}
		for _, choice := range evt.Response.Choices {
			msg := choice.Message
			if msg.Role == model.RoleTool || msg.ToolID != "" {
				continue
			}
			if len(msg.ToolCalls) > 0 {
				continue
			}
			if msg.Content == "" && len(msg.ContentParts) == 0 {
				continue
			}
			out = append(out, msg)
		}
	}
	return latestTs, out
}
