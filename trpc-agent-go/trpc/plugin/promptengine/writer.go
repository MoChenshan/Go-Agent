//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package promptengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// traceWriter is the destination for completed execution traces.
//
// Implementations must be safe for concurrent use because a single
// Sampler instance may be shared across concurrent Runner invocations.
type traceWriter interface {
	// Write delivers a trace to the underlying destination. Returning an
	// error does not abort the Runner: the sampler merely logs it.
	Write(ctx context.Context, trace *trace, token string) error
}

// logWriter serialises traces to JSON and emits them through the standard
// trpc-agent-go logger at info level.
//
// It is the default writer when WithTRPCWriter is not used.
type logWriter struct{}

// newLogWriter creates a logWriter emitting compact JSON.
func newLogWriter() *logWriter { return &logWriter{} }

// Write implements traceWriter.
func (w *logWriter) Write(ctx context.Context, trace *trace, token string) error {
	_ = token
	if trace == nil {
		return nil
	}
	data, err := json.Marshal(trace)
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}
	log.InfofContext(ctx, "[promptengine] log writer trace: invocation_id=%s bytes=%d body=%s",
		trace.InvocationID, len(data), string(data))
	return nil
}

// nopWriter discards all traces. It is useful for tests and for disabling the
// plugin without unregistering it.
type nopWriter struct{}

// newNopWriter creates a nopWriter.
func newNopWriter() *nopWriter { return &nopWriter{} }

// Write implements traceWriter.
func (w *nopWriter) Write(_ context.Context, _ *trace, _ string) error { return nil }

// errAsyncQueueFull is returned by asyncWriter.Write when the internal queue
// is saturated. Callers should treat it as a signal to either raise the queue
// length (WithAsyncWrite) or accept the back-pressure.
var errAsyncQueueFull = errors.New("promptengine: async write queue full")

// asyncWriter wraps another traceWriter and performs the actual Write on a
// dedicated goroutine. It is intended for production use where the Runner
// hot path should not block on network I/O.
type asyncWriter struct {
	writer   traceWriter
	ch       chan *asyncJob
	done     chan struct{}
	queueLen int
}

type asyncJob struct {
	ctx   context.Context
	trace *trace
	token string
}

// newAsyncWriter wraps the given writer and starts the background worker.
// A queueLen of 0 or less is normalised to 100.
func newAsyncWriter(writer traceWriter, queueLen int) *asyncWriter {
	if queueLen <= 0 {
		queueLen = 100
	}
	w := &asyncWriter{
		writer:   writer,
		ch:       make(chan *asyncJob, queueLen),
		done:     make(chan struct{}),
		queueLen: queueLen,
	}
	go w.run()
	return w
}

// run drains the queue until it is closed.
func (w *asyncWriter) run() {
	for job := range w.ch {
		if err := w.writer.Write(job.ctx, job.trace, job.token); err != nil {
			log.ErrorfContext(job.ctx,
				"[promptengine] async write failed: %v", err)
		}
	}
	close(w.done)
}

// Write implements traceWriter. It is non-blocking: when the queue is full it
// returns errAsyncQueueFull instead of blocking the Runner.
//
// The caller's context is detached from its cancel / deadline chain before
// being handed to the underlying writer so that the upload can outlive the
// request (context values such as tRPC metadata are preserved).
func (w *asyncWriter) Write(ctx context.Context, trace *trace, token string) error {
	detached := context.WithoutCancel(ctx)
	select {
	case w.ch <- &asyncJob{ctx: detached, trace: trace, token: token}:
		return nil
	default:
		return errAsyncQueueFull
	}
}

// Close stops the asyncWriter worker and waits for the queue to drain.
func (w *asyncWriter) Close() error {
	close(w.ch)
	<-w.done
	return nil
}
