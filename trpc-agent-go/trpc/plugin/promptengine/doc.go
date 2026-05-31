//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package promptengine provides a Runner-scoped sampling plugin that collects
// execution traces and forwards them to the log_collector service.
//
// A single Sampler registers itself against the six standard callbacks
// (before/after Agent/Model/Tool) and emits exactly one trace per root Runner
// invocation. Sub-agent invocations do not emit their own trace; their steps
// are merged into the root trace so that callers see a single DAG.
//
// The package intentionally exposes a small API surface: callers configure
// sampling, choose the built-in tRPC writer, and optionally expose the HTTP
// control plane. Trace assembly and the writer contract stay internal so the
// framework can evolve the log_collector payload without forcing application
// code to depend on intermediate structs.
//
// Typical usage with this repository's tRPC distribution:
//
//	sampler := promptengine.New(
//	    promptengine.WithSampleRate(1.0),
//	    promptengine.WithTRPCWriter(),      // caller resolved from trpc.GlobalConfig()
//	    promptengine.WithAsyncWrite(100),   // recommended for production
//	)
//
// Runtime configuration - the Enabled flag, sample rate and business
// isolation token - can be updated through the HTTP control plane. The
// effective token is captured at invocation start and sent with that
// invocation's trace upload.
//
// The sampler also exposes a standalone HTTP control-plane handler via
// Sampler.ConfigHandler. The handler serves GET / PUT / DELETE on
// default and per-app configurations. By default it is permissive: every
// request is served without authentication. Callers that need access
// control should wrap the returned handler in their own HTTP middleware.
// The handler does not own a specific URL prefix; the host process mounts
// it at any ServeMux path. See ConfigHandler's documentation and the
// package README for the wire contract.
package promptengine
