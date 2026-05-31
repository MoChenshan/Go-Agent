//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package promptengine

// Option configures a Sampler at construction time.
type Option func(*Sampler)

// WithName overrides the plugin name. Plugins registered in the same plugin
// manager must have unique names.
func WithName(name string) Option {
	return func(s *Sampler) {
		if name != "" {
			s.name = name
		}
	}
}

// WithSampleRate sets the sampling rate in [0, 1]. Values outside that range
// are clamped. A rate of 0 disables sampling, 1 samples every invocation.
//
// The rate can also be updated at runtime via the HTTP ConfigHandler.
func WithSampleRate(rate float64) Option {
	return func(s *Sampler) {
		if rate < 0 {
			rate = 0
		}
		if rate > 1 {
			rate = 1
		}
		cfg := s.runtimeConfig.Load()
		s.runtimeConfig.Store(&runtimeConfig{
			Enabled:      cfg.Enabled,
			SampleRate:   rate,
			SamplerToken: cfg.SamplerToken,
		})
	}
}

// WithEnabled toggles the master sampling switch. When disabled, no
// invocation is sampled regardless of sample rate.
func WithEnabled(enabled bool) Option {
	return func(s *Sampler) {
		cfg := s.runtimeConfig.Load()
		s.runtimeConfig.Store(&runtimeConfig{
			Enabled:      enabled,
			SampleRate:   cfg.SampleRate,
			SamplerToken: cfg.SamplerToken,
		})
	}
}

// WithSamplerToken sets the initial business isolation token (SamplerToken).
// This token is forwarded to the log collector as ReportTraceRequest.Token.
// It is a tenant / app label, not an access credential; the log collector
// is responsible for deciding which SamplerToken values to accept.
//
// The token can also be updated at runtime via the HTTP ConfigHandler.
func WithSamplerToken(token string) Option {
	return func(s *Sampler) {
		cfg := s.runtimeConfig.Load()
		s.runtimeConfig.Store(&runtimeConfig{
			Enabled:      cfg.Enabled,
			SampleRate:   cfg.SampleRate,
			SamplerToken: token,
		})
	}
}

// withWriter installs a custom traceWriter. Passing nil is a no-op.
func withWriter(w traceWriter) Option {
	return func(s *Sampler) {
		if w != nil {
			s.writer = w
		}
	}
}

// WithTRPCWriter installs the tRPC-based trace writer that uploads each trace
// to the log_collector service. This is the recommended writer for
// production deployments that already use this repository's internal tRPC
// distribution ("git.code.oa.com/trpc-go/trpc-go").
//
// Typical usage follows.
//
//	sampler := promptengine.New(
//	    promptengine.WithSampleRate(1.0),
//	    promptengine.WithTRPCWriter(),
//	    promptengine.WithAsyncWrite(100),
//	)
func WithTRPCWriter(opts ...TRPCWriterOption) Option {
	return func(s *Sampler) {
		s.writer = newTRPCWriter(opts...)
	}
}

// WithMaxSteps caps the number of steps recorded per invocation. Once the
// cap is reached, further steps are dropped silently to bound memory use.
func WithMaxSteps(n int) Option {
	return func(s *Sampler) {
		if n > 0 {
			s.maxSteps = n
		}
	}
}

// WithAsyncWrite enables background trace uploads with the given queue
// length. Writes become non-blocking: when the queue saturates, the trace is
// dropped.
//
// A queue length of 0 or less keeps synchronous behaviour.
func WithAsyncWrite(queueLen int) Option {
	return func(s *Sampler) {
		s.asyncQueueLen = queueLen
	}
}

// WithStructureID sets a default structure ID stamped onto every trace when
// the invocation has no explicit structure of its own. Defaults to the root
// agent name.
func WithStructureID(id string) Option {
	return func(s *Sampler) {
		s.defaultStructureID = id
	}
}
