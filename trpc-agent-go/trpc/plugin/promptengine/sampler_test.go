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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
)

// captureWriter is a traceWriter that records each Write call.
type captureWriter struct {
	mu       sync.Mutex
	traces   []*trace
	token    string
	writeErr error
}

func (w *captureWriter) Write(_ context.Context, t *trace, token string) error {
	w.mu.Lock()
	w.traces = append(w.traces, t)
	w.token = token
	w.mu.Unlock()
	return w.writeErr
}

func (w *captureWriter) snapshot() (int, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.traces), w.token
}

func TestNew_Defaults(t *testing.T) {
	s := New()
	require.NotNil(t, s)
	assert.Equal(t, defaultPluginName, s.Name())
	assert.Equal(t, defaultMaxSteps, s.maxSteps)
	cfg := s.getConfig()
	assert.True(t, cfg.Enabled)
	assert.InDelta(t, 0.0, cfg.SampleRate, 0)
	assert.Empty(t, cfg.SamplerToken)
}

func TestNew_WithOptions(t *testing.T) {
	cw := &captureWriter{}
	s := New(
		WithName("custom"),
		WithSampleRate(0.5),
		WithEnabled(true),
		withWriter(cw),
		WithMaxSteps(42),
		WithStructureID("struct-v1"),
		WithSamplerToken("initial-token"),
	)
	assert.Equal(t, "custom", s.Name())
	assert.Equal(t, 42, s.maxSteps)
	assert.Equal(t, "struct-v1", s.defaultStructureID)
	cfg := s.getConfig()
	assert.True(t, cfg.Enabled)
	assert.InDelta(t, 0.5, cfg.SampleRate, 0)
	assert.Equal(t, "initial-token", cfg.SamplerToken)
}

func TestWithSampleRate_Clamps(t *testing.T) {
	s := New(WithSampleRate(2.5))
	assert.InDelta(t, 1.0, s.getConfig().SampleRate, 0)
	s2 := New(WithSampleRate(-0.5))
	assert.InDelta(t, 0.0, s2.getConfig().SampleRate, 0)
}

func TestShouldSample(t *testing.T) {
	t.Run("disabled_never_samples", func(t *testing.T) {
		s := New(WithEnabled(false), WithSampleRate(1.0))
		for i := 0; i < 50; i++ {
			assert.False(t, s.shouldSample(nil))
		}
	})
	t.Run("rate_zero_never_samples", func(t *testing.T) {
		s := New(WithSampleRate(0))
		for i := 0; i < 50; i++ {
			assert.False(t, s.shouldSample(nil))
		}
	})
	t.Run("rate_one_always_samples", func(t *testing.T) {
		s := New(WithSampleRate(1))
		for i := 0; i < 50; i++ {
			assert.True(t, s.shouldSample(nil))
		}
	})
	t.Run("rate_half_has_both", func(t *testing.T) {
		s := New(WithSampleRate(0.5))
		var sampled, skipped int
		for i := 0; i < 500; i++ {
			if s.shouldSample(nil) {
				sampled++
			} else {
				skipped++
			}
		}
		assert.Greater(t, sampled, 0)
		assert.Greater(t, skipped, 0)
	})
}

func TestSetConfig_Validation(t *testing.T) {
	s := New()
	err := s.setConfig(nil)
	assert.Error(t, err)
	err = s.setConfig(&runtimeConfig{SampleRate: 2})
	assert.Error(t, err)
	err = s.setConfig(&runtimeConfig{Enabled: true, SampleRate: 0.7, SamplerToken: "t"})
	require.NoError(t, err)
	got := s.getConfig()
	assert.Equal(t, true, got.Enabled)
	assert.InDelta(t, 0.7, got.SampleRate, 0)
	assert.Equal(t, "t", got.SamplerToken)
}

func TestAfterAgent_WritesDefaultToken(t *testing.T) {
	cw := &captureWriter{}
	s := New(withWriter(cw), WithSampleRate(1.0))
	require.NoError(t, s.setConfig(&runtimeConfig{
		Enabled: true, SampleRate: 1.0, SamplerToken: "new-token",
	}))
	inv := &agent.Invocation{InvocationID: "inv-token", AgentName: "root"}
	_, err := s.beforeAgent(context.Background(), &agent.BeforeAgentArgs{Invocation: inv})
	require.NoError(t, err)
	_, err = s.afterAgent(context.Background(), &agent.AfterAgentArgs{Invocation: inv})
	require.NoError(t, err)
	_, tok := cw.snapshot()
	assert.Equal(t, "new-token", tok)
}

func TestAfterAgent_WritesPerAppToken(t *testing.T) {
	cw := &captureWriter{}
	s := New(withWriter(cw), WithSampleRate(1.0), WithSamplerToken("default-token"))
	require.NoError(t, s.setAppConfig("app-a", &runtimeConfig{
		Enabled: true, SampleRate: 1.0, SamplerToken: "app-token",
	}))
	inv := &agent.Invocation{
		InvocationID: "inv-app-token",
		AgentName:    "root",
		RunOptions:   agent.RunOptions{AppName: "app-a"},
	}
	_, err := s.beforeAgent(context.Background(), &agent.BeforeAgentArgs{Invocation: inv})
	require.NoError(t, err)
	_, err = s.afterAgent(context.Background(), &agent.AfterAgentArgs{Invocation: inv})
	require.NoError(t, err)
	_, tok := cw.snapshot()
	assert.Equal(t, "app-token", tok)
}

func TestAsyncWriter_PreservesToken(t *testing.T) {
	cw := &captureWriter{}
	s := New(withWriter(cw), WithSampleRate(1.0), WithAsyncWrite(10))
	defer s.Close(context.Background())
	require.NoError(t, s.setConfig(&runtimeConfig{
		Enabled: true, SampleRate: 1.0, SamplerToken: "through-async",
	}))
	inv := &agent.Invocation{InvocationID: "inv-async-token", AgentName: "root"}
	_, err := s.beforeAgent(context.Background(), &agent.BeforeAgentArgs{Invocation: inv})
	require.NoError(t, err)
	_, err = s.afterAgent(context.Background(), &agent.AfterAgentArgs{Invocation: inv})
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	_, tok := cw.snapshot()
	assert.Equal(t, "through-async", tok)
}

func TestRegister_RegistersAllSixHooks(t *testing.T) {
	s := New(WithSampleRate(1.0))
	mgr, err := plugin.NewManager(s)
	require.NoError(t, err)
	require.NotNil(t, mgr)
	// The manager trims empty callback sets; non-nil means at least one hook
	// was registered for each plumbing point.
	assert.NotNil(t, mgr.AgentCallbacks())
	assert.NotNil(t, mgr.ModelCallbacks())
	assert.NotNil(t, mgr.ToolCallbacks())
}

func TestClose_Idempotent(t *testing.T) {
	s := New(WithAsyncWrite(4))
	require.NoError(t, s.Close(context.Background()))
	// Second close must not panic.
	require.NoError(t, s.Close(context.Background()))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 10))
	assert.Equal(t, "abcd...", truncate("abcdef", 4))
	assert.Equal(t, "你好...", truncate("你好世界", 2))
	assert.Equal(t, "", truncate("abc", 0))
	assert.Equal(t, "", truncate("", 5))
}

func TestFormatToolResult(t *testing.T) {
	assert.Equal(t, "", formatToolResult(nil))
	assert.Equal(t, "hi", formatToolResult("hi"))
	assert.Equal(t, "raw", formatToolResult([]byte("raw")))
	assert.Equal(t, "boom", formatToolResult(errors.New("boom")))
	// Structured value goes through json.Marshal.
	assert.Equal(t, `{"a":1}`, formatToolResult(map[string]int{"a": 1}))
}

func TestBuildTraceAddsRootInputFromInvocation(t *testing.T) {
	s := New()
	state := newInvocationState("inv-root", "root", "structure", "", true)
	trace := s.buildTrace(state, &agent.AfterAgentArgs{
		Invocation: &agent.Invocation{
			Message: model.NewUserMessage("match-123"),
		},
	})
	require.NotNil(t, trace.Input)
	assert.Equal(t, "match-123", trace.Input.Text)
}

func TestBuildTraceFinalOutputPrefersStructuredOutput(t *testing.T) {
	s := New()
	state := newInvocationState("inv-root", "root", "structure", "", true)
	trace := s.buildTrace(state, &agent.AfterAgentArgs{
		Invocation: &agent.Invocation{},
		FullResponseEvent: event.New(
			"inv-root",
			"root",
			event.WithStructuredOutputPayload(map[string]any{"answer": "ok"}),
		),
	})
	require.NotNil(t, trace.FinalOutput)
	assert.JSONEq(t, `{"answer":"ok"}`, trace.FinalOutput.Text)
}

func TestBuildTraceFinalOutputUsesGraphLastResponse(t *testing.T) {
	s := New()
	state := newInvocationState("inv-root", "root", "structure", "", true)
	raw, err := json.Marshal("graph final")
	require.NoError(t, err)
	trace := s.buildTrace(state, &agent.AfterAgentArgs{
		Invocation: &agent.Invocation{},
		FullResponseEvent: event.New(
			"inv-root",
			"graph",
			event.WithStateDelta(map[string][]byte{lastResponseKey: raw}),
			event.WithResponse(&model.Response{
				Done: true,
				Choices: []model.Choice{
					{Message: model.NewAssistantMessage("response final")},
				},
			}),
		),
	})
	require.NotNil(t, trace.FinalOutput)
	assert.Equal(t, "graph final", trace.FinalOutput.Text)
}

// Writer layer tests are defined below.

func TestLogWriter_Write_NilTrace(t *testing.T) {
	w := newLogWriter()
	require.NoError(t, w.Write(context.Background(), nil, ""))
}

func TestNopWriter(t *testing.T) {
	w := newNopWriter()
	require.NoError(t, w.Write(context.Background(), sampleTrace(), ""))
}

func TestAsyncWriter_Write_Succeeds(t *testing.T) {
	cw := &captureWriter{}
	aw := newAsyncWriter(cw, 4)
	for i := 0; i < 4; i++ {
		require.NoError(t, aw.Write(context.Background(), sampleTrace(), ""))
	}
	require.NoError(t, aw.Close())
	n, _ := cw.snapshot()
	assert.Equal(t, 4, n)
}

func TestAsyncWriter_QueueFull_ReturnsError(t *testing.T) {
	// A blocking writer holds up the worker so the queue fills.
	block := make(chan struct{})
	entered := make(chan struct{})
	slow := &slowWriter{gate: block, entered: entered}
	aw := newAsyncWriter(slow, 1)
	// 1st enqueues, consumed by the worker which then blocks on `gate`.
	require.NoError(t, aw.Write(context.Background(), sampleTrace(), ""))
	<-entered
	// 2nd fills the 1-slot queue.
	require.NoError(t, aw.Write(context.Background(), sampleTrace(), ""))
	// 3rd must fail fast.
	err := aw.Write(context.Background(), sampleTrace(), "")
	assert.ErrorIs(t, err, errAsyncQueueFull)
	close(block)
	require.NoError(t, aw.Close())
}

func TestAsyncWriter_ForwardsToken(t *testing.T) {
	cw := &captureWriter{}
	aw := newAsyncWriter(cw, 4)
	defer aw.Close()
	require.NoError(t, aw.Write(context.Background(), sampleTrace(), "forwarded"))
	time.Sleep(20 * time.Millisecond)
	_, tok := cw.snapshot()
	assert.Equal(t, "forwarded", tok)
}

// slowWriter blocks on a gate channel, useful for simulating a clogged sink.
type slowWriter struct {
	gate    chan struct{}
	entered chan struct{}
}

func (s *slowWriter) Write(_ context.Context, _ *trace, _ string) error {
	if s.entered != nil {
		select {
		case <-s.entered:
		default:
			close(s.entered)
		}
	}
	<-s.gate
	return nil
}

func TestAsyncWriter_DetachesFromCancelledCtx(t *testing.T) {
	cw := &captureWriter{}
	aw := newAsyncWriter(cw, 4)
	defer aw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel the context before the async write.
	require.NoError(t, aw.Write(ctx, sampleTrace(), ""))
	// Give the worker a moment to flush.
	time.Sleep(20 * time.Millisecond)
	n, _ := cw.snapshot()
	assert.Equal(t, 1, n, "async write should complete despite pre-cancelled ctx")
}
