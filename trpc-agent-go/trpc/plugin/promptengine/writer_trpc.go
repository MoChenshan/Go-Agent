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
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/client"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/plugin/promptengine/internal/proto"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// Default values for the tRPC writer. The target service name is the log
// collector's fixed Polaris name and the timeout matches a sensible
// production default.
const (
	defaultTRPCTarget  = "polaris://trpc.trs.prompt_log_collector.LogCollector"
	defaultTRPCTimeout = 3 * time.Second
	fallbackCaller     = "unknown"
)

// trpcWriter is a traceWriter that uploads traces to the log_collector tRPC
// service. It is the default way to ship traces from a trpc-agent-go Runner
// to the centralised collector.
//
// The writer is safe for concurrent use. The sampler passes the effective
// business token to each Write call.
type trpcWriter struct {
	proxy proto.LogCollectorClientProxy
	// caller is the service identifier of the *calling* process. When empty,
	// it is resolved lazily from trpc.GlobalConfig().Server.Service[0].Name.
	caller string
	// target is the callee's service name for tRPC naming / routing. It
	// propagates via client.WithTarget on each invocation.
	target string
	// timeout bounds a single ReportTrace RPC.
	timeout time.Duration
	// resolvedCaller caches the result of resolveCaller so that we only
	// dereference GlobalConfig once.
	resolvedCaller atomic.Value // string
	resolveOnce    sync.Once
}

// TRPCWriterOption configures the built-in tRPC writer.
type TRPCWriterOption interface {
	applyTRPCWriter(*trpcWriter)
}

type trpcWriterOptionFunc func(*trpcWriter)

func (f trpcWriterOptionFunc) applyTRPCWriter(w *trpcWriter) {
	f(w)
}

// WithTRPCCaller explicitly sets the caller service name, overriding the
// default lookup via trpc.GlobalConfig(). Useful when the binary does not
// initialise a tRPC server but still needs to report traces.
func WithTRPCCaller(caller string) TRPCWriterOption {
	return trpcWriterOptionFunc(func(w *trpcWriter) { w.caller = caller })
}

// WithTRPCTarget sets the callee target, defaulting to
// "polaris://trpc.trs.prompt_log_collector.LogCollector".
func WithTRPCTarget(target string) TRPCWriterOption {
	return trpcWriterOptionFunc(func(w *trpcWriter) {
		if target != "" {
			w.target = target
		}
	})
}

// WithTRPCTimeout sets the per-call timeout.
func WithTRPCTimeout(d time.Duration) TRPCWriterOption {
	return trpcWriterOptionFunc(func(w *trpcWriter) {
		if d > 0 {
			w.timeout = d
		}
	})
}

// withTRPCClient injects a pre-built LogCollectorClientProxy.
func withTRPCClient(proxy proto.LogCollectorClientProxy) TRPCWriterOption {
	return trpcWriterOptionFunc(func(w *trpcWriter) {
		if proxy != nil {
			w.proxy = proxy
		}
	})
}

// newTRPCWriter creates a trpcWriter wired to the log_collector service.
// Most users do not need to pass any options: the writer will read the caller
// name from the local tRPC configuration on first use.
func newTRPCWriter(opts ...TRPCWriterOption) *trpcWriter {
	w := &trpcWriter{
		target:  defaultTRPCTarget,
		timeout: defaultTRPCTimeout,
	}
	for _, opt := range opts {
		if opt != nil {
			opt.applyTRPCWriter(w)
		}
	}
	if w.proxy == nil {
		w.proxy = proto.NewLogCollectorClientProxy()
	}
	return w
}

// resolveCaller returns the service name to send as ReportTraceRequest.Caller.
//
// Resolution order is listed below.
//  1. An explicit value set via WithTRPCCaller.
//  2. trpc.GlobalConfig().Server.Service[0].Name, captured lazily on first Write.
//  3. "unknown" as a last-resort fallback (warned once).
func (w *trpcWriter) resolveCaller(ctx context.Context) string {
	if w.caller != "" {
		return w.caller
	}
	w.resolveOnce.Do(func() {
		name := readCallerFromGlobalConfig()
		if name == "" {
			log.WarnfContext(ctx,
				"[promptengine] tRPC writer: trpc.GlobalConfig has no server service, "+
					"falling back to caller=%q; pass WithTRPCCaller(...) to override",
				fallbackCaller,
			)
			name = fallbackCaller
		}
		w.resolvedCaller.Store(name)
	})
	v := w.resolvedCaller.Load()
	if v == nil {
		return fallbackCaller
	}
	return v.(string)
}

// readCallerFromGlobalConfig reads the first service name from the current
// tRPC global configuration. It returns an empty string if the configuration
// is missing or has no services.
//
// Defined as a package-level var (not a closure) so tests can override it.
var readCallerFromGlobalConfig = func() string {
	cfg := trpc.GlobalConfig()
	if cfg == nil {
		return ""
	}
	if len(cfg.Server.Service) == 0 {
		return ""
	}
	svc := cfg.Server.Service[0]
	if svc == nil {
		return ""
	}
	return svc.Name
}

// Write implements traceWriter. It serialises the trace to JSON and invokes
// LogCollector.ReportTrace. Errors are logged here (so they are never
// silently swallowed) and also returned to the caller so that the async writer /
// Sampler can log contextual information.
func (w *trpcWriter) Write(ctx context.Context, trace *trace, token string) error {
	if trace == nil {
		return nil
	}
	data, err := json.Marshal(trace)
	if err != nil {
		log.ErrorfContext(ctx,
			"[promptengine] tRPC writer: marshal trace failed: invocation_id=%s err=%v",
			trace.InvocationID, err,
		)
		return fmt.Errorf("marshal trace: %w", err)
	}
	// Detach from the caller's cancel/deadline chain so that an async writer
	// handing us an already-cancelled ctx can still upload; then layer on our
	// own timeout so the call remains bounded.
	rpcCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.timeout)
	defer cancel()
	caller := w.resolveCaller(rpcCtx)
	req := &proto.ReportTraceRequest{
		Caller:    caller,
		TraceJson: string(data),
		Token:     token,
	}
	rsp, err := w.proxy.ReportTrace(rpcCtx, req, client.WithTarget(w.target))
	if err != nil {
		log.ErrorfContext(ctx,
			"[promptengine] tRPC writer: ReportTrace rpc failed: "+
				"invocation_id=%s caller=%s target=%s err=%v",
			trace.InvocationID, caller, w.target, err,
		)
		return fmt.Errorf("report trace rpc: %w", err)
	}
	if rsp.GetCode() != 0 {
		log.ErrorfContext(ctx,
			"[promptengine] tRPC writer: ReportTrace biz failure: "+
				"invocation_id=%s caller=%s code=%d message=%s",
			trace.InvocationID, caller, rsp.GetCode(), rsp.GetMessage(),
		)
		return fmt.Errorf("report trace code=%d message=%s",
			rsp.GetCode(), rsp.GetMessage())
	}
	return nil
}
