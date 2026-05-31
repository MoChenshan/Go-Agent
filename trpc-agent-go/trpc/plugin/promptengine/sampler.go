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
	"math/rand"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Plugin-level defaults.
const (
	defaultPluginName = "promptengine"
	defaultSampleRate = 0.0
	defaultMaxSteps   = 1000
	lastResponseKey   = "last_response"

	// teamMemberToolPrefix is the prefix for team-member tools (sub-agent
	// calls surfaced as tools). Their steps are filtered out so that the
	// sub-agent's own model/tool steps are recorded directly instead.
	teamMemberToolPrefix = "team-members-"

	// Truncation lengths for human-readable fields kept in the trace.
	inputTextMaxLen   = 1000
	outputTextMaxLen  = 1000
	toolResultMaxLen  = 2000
	toolArgsMaxLen    = 1000
	toolCallMaxLen    = 200
	reportFailTextLen = 256
)

// Context keys for passing builder identities between before/after callbacks.
type (
	modelBuilderKey struct{}
	toolBuilderKey  struct{}
)

// Sampler is a plugin.Plugin that samples, aggregates and exports
// execution traces from a trpc-agent-go Runner.
//
// A single Sampler is safe to reuse across concurrent Runner invocations.
// It keeps per-invocation state keyed by root invocation ID and writes exactly
// one trace per root Runner task (on AfterAgent of the root).
type Sampler struct {
	name               string
	writer             traceWriter
	maxSteps           int
	asyncQueueLen      int
	defaultStructureID string
	// runtimeConfig is read atomically on every sampling decision and can be
	// replaced through the HTTP control plane without restarting the Runner.
	runtimeConfig *configHolder
	states        *stateManager
	asyncWriter   *asyncWriter
	closeOnce     sync.Once
	closed        bool
	closeMu       sync.Mutex
	rng           *rand.Rand
	rngMu         sync.Mutex
}

// New creates a new Sampler with the given options.
//
// Default behaviour is listed below.
//   - sampling rate 0 (nothing is sampled until configured)
//   - Enabled=true (so SetSampleRate is the single knob to turn it on)
//   - writer: logWriter (compact JSON to the standard logger)
//   - synchronous writes (use WithAsyncWrite to enable back-pressure buffering)
func New(opts ...Option) *Sampler {
	s := &Sampler{
		name:          defaultPluginName,
		writer:        newLogWriter(),
		maxSteps:      defaultMaxSteps,
		states:        newStateManager(),
		rng:           rand.New(rand.NewSource(time.Now().UnixNano())),
		runtimeConfig: newConfigHolder(true, defaultSampleRate),
	}
	for _, opt := range opts {
		opt(s)
	}
	// Wrap writer in async if requested.
	if s.asyncQueueLen > 0 {
		s.asyncWriter = newAsyncWriter(s.writer, s.asyncQueueLen)
		s.writer = s.asyncWriter
	}
	return s
}

// Name implements plugin.Plugin.
func (s *Sampler) Name() string { return s.name }

// Register implements plugin.Plugin. It wires the sampler into the six
// agent/model/tool callbacks.
func (s *Sampler) Register(r *plugin.Registry) {
	if s == nil || r == nil {
		return
	}
	r.BeforeAgent(s.beforeAgent)
	r.AfterAgent(s.afterAgent)
	r.BeforeModel(s.beforeModel)
	r.AfterModel(s.afterModel)
	r.BeforeTool(s.beforeTool)
	r.AfterTool(s.afterTool)
}

// Close implements plugin.Closer. It drains the async writer (if used) and
// releases per-invocation state. Close is idempotent.
func (s *Sampler) Close(ctx context.Context) error {
	_ = ctx
	var err error
	s.closeOnce.Do(func() {
		s.closeMu.Lock()
		s.closed = true
		s.closeMu.Unlock()
		if s.asyncWriter != nil {
			err = s.asyncWriter.Close()
		}
		s.states.clear()
	})
	return err
}

// NonClosable returns a plugin.Plugin that forwards to this sampler but is
// NOT a plugin.Closer. Use it instead of passing *Sampler directly to
// runner.WithPlugins(...) when you intend this sampler to be a *process-wide
// singleton shared across multiple Runners:
//
//	sampler := promptengine.New(...)                          // Create once per process.
//	r := runner.NewRunner(appName, agent,
//	    runner.WithPlugins(sampler.NonClosable()),             // Safe for each Runner.
//	)
//
// Background: runner.Runner.Close propagates to plugin.Manager.Close, which
// walks every plugin and calls Close on those that implement plugin.Closer.
// Sampler implements Closer (it closes its asyncWriter channel), so a
// Runner tearing down would also tear down the shared sampler; the next
// Runner reusing the same singleton would then panic with
// "send on closed channel" on the next trace write. The wrapper returned
// here has no Close method, so plugin.Manager skips it and the shared core
// stays alive for the life of the process.
//
// Multiple NonClosable wrappers may be returned for the same sampler and
// attached to different Runners. Per-invocation state inside the core is
// keyed by invocationID (sync.Map), so callbacks routed through distinct
// Runner.Registries never collide.
//
// The wrapper's Name is the sampler's own plugin name (typically
// "promptengine"). Two wrappers returned from the same sampler therefore
// share a Name, which is fine: plugin.Manager only de-duplicates names
// within a single Manager, not across Runners.
//
// Callers MUST NOT hand-call Close on the underlying *Sampler while
// NonClosable wrappers are still attached to live Runners, except at
// process shutdown. Doing so would resurrect the exact failure mode this
// wrapper exists to prevent.
func (s *Sampler) NonClosable() plugin.Plugin {
	if s == nil {
		return nil
	}
	return &nonClosablePlugin{core: s, name: s.name}
}

// getConfig returns a deep copy of the current runtime configuration.
// The returned pointer is owned by the caller and safe to mutate.
func (s *Sampler) getConfig() *runtimeConfig {
	return s.runtimeConfig.Load().clone()
}

// setConfig atomically installs a new runtime configuration. If the new
// configuration is invalid, the existing configuration is left unchanged and
// the error is returned.
func (s *Sampler) setConfig(config *runtimeConfig) error {
	if config == nil {
		return errors.New("config must not be nil")
	}
	if err := config.validate(); err != nil {
		return err
	}
	s.runtimeConfig.Store(config.clone())
	return nil
}

// getAppConfig returns the runtimeConfig that will be used for invocations
// whose resolved appName equals app. When app has a registered override the
// override copy is returned and isOverride is true; otherwise the default
// config copy is returned and isOverride is false.
//
// The returned pointer is owned by the caller and safe to mutate.
func (s *Sampler) getAppConfig(app string) (cfg *runtimeConfig, isOverride bool) {
	snap := s.runtimeConfig.loadSnapshot()
	if app != "" {
		if override, ok := snap.overrides[app]; ok && override != nil {
			return override.clone(), true
		}
	}
	return snap.defaults.clone(), false
}

// setAppConfig atomically installs a per-app override. A PUT-like complete
// replacement: the whole runtimeConfig for app is replaced with the
// supplied value. Returns an error when cfg fails validation. The empty app
// string is rejected as it would collide with "use default".
func (s *Sampler) setAppConfig(app string, cfg *runtimeConfig) error {
	if app == "" {
		return errors.New("app name must not be empty")
	}
	if cfg == nil {
		return errors.New("config must not be nil")
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	s.runtimeConfig.StoreAppConfig(app, cfg.clone())
	return nil
}

// deleteAppConfig removes a previously registered per-app override. It
// returns true if an override was removed and false when no such override
// existed.
func (s *Sampler) deleteAppConfig(app string) bool {
	if app == "" {
		return false
	}
	return s.runtimeConfig.deleteAppConfig(app)
}

// listAppConfigs returns a deep copy of all registered per-app overrides.
// The returned map is owned by the caller and safe to mutate; mutations do
// not affect the sampler's internal state.
func (s *Sampler) listAppConfigs() map[string]*runtimeConfig {
	snap := s.runtimeConfig.loadSnapshot()
	out := make(map[string]*runtimeConfig, len(snap.overrides))
	for k, v := range snap.overrides {
		out[k] = v.clone()
	}
	return out
}

// resolveAppName extracts the appName associated with an invocation. It is
// used to look up the per-app override that should apply to the current
// sampling decision. The resolution order mirrors how the Runner propagates
// app identity:
//
//  1. inv.RunOptions.AppName (set by runner.WithAppName on a specific run)
//  2. inv.Session.AppName    (set when the runner attached a session to the
//     invocation)
//  3. "" (no app known)
//
// The empty string falls back to the default runtimeConfig in the sampler's
// configHolder snapshot.
func resolveAppName(inv *agent.Invocation) string {
	if inv == nil {
		return ""
	}
	if name := inv.RunOptions.AppName; name != "" {
		return name
	}
	if inv.Session != nil && inv.Session.AppName != "" {
		return inv.Session.AppName
	}
	return ""
}

// samplingDecision decides whether a root invocation should be sampled and
// captures the token that should be used if the trace is later reported. It
// consults the per-app override before falling back to the default
// runtimeConfig. The entire lookup is one atomic.Load on the hot path.
func (s *Sampler) samplingDecision(inv *agent.Invocation) (sampled bool, samplerToken string) {
	snap := s.runtimeConfig.loadSnapshot()
	cfg := snap.effective(resolveAppName(inv))
	if cfg == nil || !cfg.Enabled {
		return false, ""
	}
	samplerToken = cfg.SamplerToken
	switch {
	case cfg.SampleRate <= 0:
		return false, samplerToken
	case cfg.SampleRate >= 1:
		return true, samplerToken
	}
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	return s.rng.Float64() < cfg.SampleRate, samplerToken
}

// shouldSample reports whether a root invocation should be sampled. Passing a
// nil invocation is equivalent to passing an invocation with no appName; in
// that case the default runtimeConfig is used.
func (s *Sampler) shouldSample(inv *agent.Invocation) bool {
	sampled, _ := s.samplingDecision(inv)
	return sampled
}

// Helper functions are defined below.

// getRootInvocationID walks up the parent chain to the root invocation's ID.
func getRootInvocationID(inv *agent.Invocation) string {
	for inv.GetParentInvocation() != nil {
		inv = inv.GetParentInvocation()
	}
	return inv.InvocationID
}

// isSubAgentInvocation reports whether the invocation has a parent.
func isSubAgentInvocation(inv *agent.Invocation) bool {
	return inv.GetParentInvocation() != nil
}

// Agent callback hooks are defined below.

// beforeAgent initialises per-invocation state for the root agent. Sub-agents
// reuse their root's state so that all their steps are merged into one trace.
func (s *Sampler) beforeAgent(
	ctx context.Context,
	args *agent.BeforeAgentArgs,
) (*agent.BeforeAgentResult, error) {
	_ = ctx
	if args == nil || args.Invocation == nil {
		return nil, nil
	}
	inv := args.Invocation
	if isSubAgentInvocation(inv) {
		return nil, nil
	}
	sampled, samplerToken := s.samplingDecision(inv)
	structureID := s.defaultStructureID
	if structureID == "" {
		structureID = inv.AgentName
	}
	s.states.getOrCreate(inv.InvocationID, inv.AgentName, structureID, samplerToken, sampled)
	return nil, nil
}

// afterAgent builds the aggregate trace for the root invocation and hands it
// to the writer. Errors are logged but never surfaced back to the Runner so
// that trace upload failures cannot break user-visible execution.
func (s *Sampler) afterAgent(
	ctx context.Context,
	args *agent.AfterAgentArgs,
) (*agent.AfterAgentResult, error) {
	if args == nil || args.Invocation == nil {
		return nil, nil
	}
	inv := args.Invocation
	// Sub-agents do not emit their own trace.
	if isSubAgentInvocation(inv) {
		return nil, nil
	}
	state := s.states.get(inv.InvocationID)
	if state == nil || !state.sampled {
		s.states.delete(inv.InvocationID)
		return nil, nil
	}
	trace := s.buildTrace(state, args)
	// Clean up state before writing to avoid accidental re-use.
	s.states.delete(inv.InvocationID)
	if err := s.writer.Write(ctx, trace, state.samplerToken); err != nil {
		// Writer implementations already log their own errors; we add a
		// top-level entry with the invocation ID so that operators can
		// correlate dropped traces even when the writer's log is filtered.
		log.ErrorfContext(ctx,
			"[promptengine] trace write failed, dropped: invocation_id=%s err=%v",
			trace.InvocationID, err,
		)
	}
	return nil, nil
}

// buildTrace converts the accumulated state into the wire-level trace.
func (s *Sampler) buildTrace(state *invocationState, args *agent.AfterAgentArgs) *trace {
	endTime := time.Now()
	steps := state.getSteps()
	status := traceStatusCompleted
	var errMsg string
	if args.Error != nil {
		status = traceStatusFailed
		errMsg = args.Error.Error()
	}
	finalOutput := traceOutputFromEvent(args.FullResponseEvent)
	if finalOutput == nil {
		finalOutput = lastModelStepOutput(steps)
	}
	return &trace{
		StructureID:  state.structureID,
		InvocationID: state.invocationID,
		AgentName:    state.agentName,
		Status:       status,
		Input:        traceInputFromInvocation(args.Invocation),
		FinalOutput:  finalOutput,
		Steps:        steps,
		StartTime:    state.startTime,
		EndTime:      endTime,
		Duration:     endTime.Sub(state.startTime),
		Error:        errMsg,
	}
}

func traceInputFromInvocation(inv *agent.Invocation) *traceInput {
	if inv == nil || inv.Message.Content == "" {
		return nil
	}
	return &traceInput{Text: truncate(inv.Message.Content, inputTextMaxLen)}
}

func traceOutputFromEvent(ev *event.Event) *traceOutput {
	if ev == nil {
		return nil
	}
	if output := traceOutputFromStructuredPayload(ev.StructuredOutput); output != nil {
		return output
	}
	if output := traceOutputFromStateDelta(ev.StateDelta); output != nil {
		return output
	}
	if ev.Response == nil || len(ev.Response.Choices) == 0 {
		return nil
	}
	text := ev.Response.Choices[0].Message.Content
	if text == "" {
		return nil
	}
	return &traceOutput{Text: truncate(text, outputTextMaxLen)}
}

func traceOutputFromStructuredPayload(payload any) *traceOutput {
	if payload == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil || len(data) == 0 {
		return nil
	}
	return &traceOutput{Text: truncate(string(data), outputTextMaxLen)}
}

func traceOutputFromStateDelta(stateDelta map[string][]byte) *traceOutput {
	if len(stateDelta) == 0 {
		return nil
	}
	raw := stateDelta[lastResponseKey]
	if len(raw) == 0 {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil || text == "" {
		return nil
	}
	return &traceOutput{Text: truncate(text, outputTextMaxLen)}
}

func lastModelStepOutput(steps []traceStep) *traceOutput {
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if step.StepType == stepTypeModel && step.Output != nil && step.Output.Text != "" {
			return &traceOutput{Text: step.Output.Text}
		}
	}
	return nil
}

// Model callback hooks are defined below.

// beforeModel opens a model step in the root invocation's state.
func (s *Sampler) beforeModel(
	ctx context.Context,
	args *model.BeforeModelArgs,
) (*model.BeforeModelResult, error) {
	if args == nil || args.Request == nil {
		return nil, nil
	}
	inv, ok := agent.InvocationFromContext(ctx)
	if !ok || inv == nil {
		return nil, nil
	}
	state := s.states.get(getRootInvocationID(inv))
	if state == nil || !state.sampled {
		return nil, nil
	}
	if state.stepCount() >= s.maxSteps {
		return nil, nil
	}
	stepID := state.nextStepID()
	// Use the last message's content as the textual "input" fingerprint.
	var inputText string
	msgCount := len(args.Request.Messages)
	if msgCount > 0 {
		inputText = args.Request.Messages[msgCount-1].Content
	}
	input := &traceInput{
		Text:         truncate(inputText, inputTextMaxLen),
		MessageCount: msgCount,
	}
	builder := newStepBuilder(stepID, inv.AgentName, stepTypeModel).
		withInput(input)
	if isSubAgentInvocation(inv) {
		builder.withNodeKind(nodeKindMember)
	} else {
		builder.withNodeKind(nodeKindCoordinator)
	}
	if lastID := state.getLastStepID(); lastID != "" {
		builder.withPredecessors(lastID)
	}
	// Key the builder by the current invocation ID so nested agents don't
	// overwrite each other's in-flight builders.
	builderKey := inv.InvocationID + ":model"
	state.setBuilder(builderKey, builder)
	// Pre-allocate the streaming accumulator so that afterModel frames,
	// partial or terminal, always find a live container. The accumulator
	// is released in afterModel after the terminal frame is processed, or
	// via state.clearAccumulators() if the invocation ends prematurely.
	state.getOrCreateAccumulator(builderKey)
	return &model.BeforeModelResult{
		Context: context.WithValue(ctx, modelBuilderKey{}, builderKey),
	}, nil
}

// afterModel finalises the model step created in beforeModel. In streaming
// mode each model call produces multiple afterModel invocations: N partial
// frames (IsPartial=true) plus a terminal frame (IsPartial=false). Text,
// usage and tool_calls are aggregated in a per-step streamingAccumulator;
// the step is only committed on the terminal frame (or when the Response
// is nil, which we treat as terminal to ensure the builder is drained).
func (s *Sampler) afterModel(
	ctx context.Context,
	args *model.AfterModelArgs,
) (*model.AfterModelResult, error) {
	inv, ok := agent.InvocationFromContext(ctx)
	if !ok || inv == nil {
		return nil, nil
	}
	state := s.states.get(getRootInvocationID(inv))
	if state == nil || !state.sampled {
		return nil, nil
	}
	builderKey, ok := ctx.Value(modelBuilderKey{}).(string)
	if !ok || builderKey == "" {
		return nil, nil
	}
	// Merge this frame into the accumulator. append is nil-safe for both
	// args and args.Response, so partial frames with empty Choices (e.g.
	// openai usage-only frame) still correctly update the usage snapshot.
	acc := state.getAccumulator(builderKey)
	if args != nil {
		acc.append(args.Response)
	}
	// Defer step commit until the terminal frame (or until a terminal
	// "args==nil / response==nil" signal arrives, which also ends the
	// stream).
	if args != nil && args.Response != nil && args.Response.IsPartial {
		return nil, nil
	}
	builder := state.getBuilder(builderKey)
	if builder == nil {
		// No matching builder means beforeModel was filtered out (e.g.
		// maxSteps reached). Drop the accumulator and return.
		state.deleteAccumulator(builderKey)
		return nil, nil
	}
	var errMsg string
	if args != nil && args.Error != nil {
		errMsg = args.Error.Error()
	}
	// Snapshot the aggregated view. text/usage/toolCalls come from the
	// accumulator's last-wins / delta-appended view built up across all
	// frames in this model step.
	var (
		text      string
		usage     *model.Usage
		toolCalls []model.ToolCall
	)
	if acc != nil {
		text, usage, toolCalls = acc.snapshot()
	}
	// Defensive fallback: if the accumulator never saw a populated
	// Choices[0] (e.g. Response was nil throughout), try to pull text /
	// tool_calls directly from the terminal Response before giving up.
	if text == "" && len(toolCalls) == 0 &&
		args != nil && args.Response != nil && len(args.Response.Choices) > 0 {
		msg := args.Response.Choices[0].Message
		if msg.Content != "" {
			text = msg.Content
		}
		if len(msg.ToolCalls) > 0 {
			toolCalls = msg.ToolCalls
		}
	}
	if text == "" && len(toolCalls) > 0 {
		text = formatToolCalls(toolCalls)
	}
	output := &traceOutput{Text: truncate(text, outputTextMaxLen)}
	if usage != nil {
		output.TokenUsage = &tokenUsage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		}
	}
	step := builder.build(output, errMsg)
	state.addStep(step)
	state.setLastStepID(step.StepID)
	state.deleteAccumulator(builderKey)
	return nil, nil
}

// Tool callback hooks are defined below.

// beforeTool opens a tool step, skipping team-member tool wrappers.
func (s *Sampler) beforeTool(
	ctx context.Context,
	args *tool.BeforeToolArgs,
) (*tool.BeforeToolResult, error) {
	if args == nil {
		return nil, nil
	}
	// Sub-agent dispatch tools are skipped; their underlying model/tool
	// steps are recorded directly via the aggregated state.
	if strings.HasPrefix(args.ToolName, teamMemberToolPrefix) {
		return nil, nil
	}
	inv, ok := agent.InvocationFromContext(ctx)
	if !ok || inv == nil {
		return nil, nil
	}
	state := s.states.get(getRootInvocationID(inv))
	if state == nil || !state.sampled {
		return nil, nil
	}
	if state.stepCount() >= s.maxSteps {
		return nil, nil
	}
	stepID := state.nextStepID()
	input := &traceInput{
		Text:          fmt.Sprintf("Tool call: %s", args.ToolName),
		ToolName:      args.ToolName,
		ToolArguments: truncate(string(args.Arguments), toolArgsMaxLen),
	}
	builder := newStepBuilder(stepID, args.ToolName, stepTypeTool).
		withInput(input).
		withNodeKind(nodeKindTool)
	if lastID := state.getLastStepID(); lastID != "" {
		builder.withPredecessors(lastID)
	}
	// Keyed by tool-call ID because the same tool can be invoked several
	// times within one invocation.
	builderKey := "tool:" + args.ToolCallID
	state.setBuilder(builderKey, builder)
	return &tool.BeforeToolResult{
		Context: context.WithValue(ctx, toolBuilderKey{}, builderKey),
	}, nil
}

// afterTool finalises the tool step created in beforeTool. Team-member tools
// that were filtered out in beforeTool have no matching builder and are a
// no-op here.
func (s *Sampler) afterTool(
	ctx context.Context,
	args *tool.AfterToolArgs,
) (*tool.AfterToolResult, error) {
	if args == nil {
		return nil, nil
	}
	inv, ok := agent.InvocationFromContext(ctx)
	if !ok || inv == nil {
		return nil, nil
	}
	state := s.states.get(getRootInvocationID(inv))
	if state == nil || !state.sampled {
		return nil, nil
	}
	builderKey := "tool:" + args.ToolCallID
	builder := state.getBuilder(builderKey)
	if builder == nil {
		// Expected for team-members-* tools that were filtered out.
		return nil, nil
	}
	var errMsg string
	if args.Error != nil {
		errMsg = args.Error.Error()
	}
	resultStr := formatToolResult(args.Result)
	output := &traceOutput{
		Text:       truncate(resultStr, outputTextMaxLen),
		ToolResult: truncate(resultStr, toolResultMaxLen),
	}
	step := builder.build(output, errMsg)
	state.addStep(step)
	state.setLastStepID(step.StepID)
	return nil, nil
}

// Formatting helper functions are defined below.

// truncate keeps at most maxLen runes and appends an ellipsis when truncation occurred.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "..."
}

// formatToolCalls renders a list of ToolCalls into a concise string so that
// the trace can record "the model asked to call X(args)" even when the model
// produced no textual Content.
func formatToolCalls(toolCalls []model.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(toolCalls))
	for _, tc := range toolCalls {
		if tc.Function.Name == "" {
			continue
		}
		args := string(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		parts = append(parts,
			fmt.Sprintf("-> %s(%s)", tc.Function.Name, truncate(args, toolCallMaxLen)))
	}
	return strings.Join(parts, "\n")
}

// formatToolResult renders a tool result into a display string. JSON-encoded
// structured results use json.Marshal; primitives fall back to fmt.
func formatToolResult(result any) string {
	if result == nil {
		return ""
	}
	switch v := result.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	default:
		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Sprintf("%v", result)
		}
		return string(data)
	}
}

// Compile-time interface compliance checks.
var (
	_ plugin.Plugin = (*Sampler)(nil)
	_ plugin.Closer = (*Sampler)(nil)
)
