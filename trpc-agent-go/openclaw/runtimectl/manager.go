package runtimectl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
)

const (
	DefaultLifecycleExitCode = 75
	DefaultForceExitAfter    = 10 * time.Second
	DefaultMinTargetVersion  = "v0.0.48"

	StateIdle     State = "idle"
	StateDraining State = "draining"
	StateReady    State = "ready"

	ActionRestart ActionKind = "restart"
	ActionUpgrade ActionKind = "upgrade"

	ModeGraceful ActionMode = "graceful"
	ModeForce    ActionMode = "force"

	RequestPhaseAccepted RequestPhase = "accepted"
	RequestPhaseRunning  RequestPhase = "running"

	runtimeDirName   = "runtime"
	lifecycleDirName = "lifecycle"

	statusFileName    = "status.json"
	intentJSONName    = "intent.json"
	intentEnvFileName = "intent.env"
)

var (
	ErrActionInProgress = errors.New(
		"runtime lifecycle action already in progress",
	)
	ErrAdmissionClosed = errors.New(
		"runtime is draining and cannot admit new requests",
	)
)

type State string

type ActionKind string

type ActionMode string

type RequestPhase string

type Options struct {
	CurrentVersion   string
	StateDir         string
	ReleaseBaseURL   string
	MinTargetVersion string
	ForceExitAfter   time.Duration
	ExitCode         int
	HTTPClient       *http.Client
	OnReadyToExit    func(Intent)
}

type ActionRequest struct {
	Kind          ActionKind `json:"kind"`
	Mode          ActionMode `json:"mode"`
	TargetVersion string     `json:"target_version,omitempty"`
	TargetChannel string     `json:"target_channel,omitempty"`
	Actor         string     `json:"actor,omitempty"`
	Source        string     `json:"source,omitempty"`
}

type PendingAction struct {
	ID             string     `json:"id"`
	Kind           ActionKind `json:"kind"`
	Mode           ActionMode `json:"mode"`
	TargetVersion  string     `json:"target_version,omitempty"`
	TargetChannel  string     `json:"target_channel,omitempty"`
	Actor          string     `json:"actor,omitempty"`
	Source         string     `json:"source,omitempty"`
	RequestedAt    time.Time  `json:"requested_at"`
	CurrentVersion string     `json:"current_version"`
	Summary        []string   `json:"summary,omitempty"`
}

type Status struct {
	State           State          `json:"state"`
	CurrentVersion  string         `json:"current_version"`
	ActiveRequests  int            `json:"active_requests"`
	RunningRequests int            `json:"running_requests"`
	QueuedRequests  int            `json:"queued_requests"`
	Pending         *PendingAction `json:"pending,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ExitCode        int            `json:"exit_code"`
}

type ActionResult struct {
	Status  Status `json:"status"`
	Started bool   `json:"started"`
}

type Intent struct {
	Action         ActionKind `json:"action"`
	Mode           ActionMode `json:"mode"`
	TargetVersion  string     `json:"target_version,omitempty"`
	TargetChannel  string     `json:"target_channel,omitempty"`
	CurrentVersion string     `json:"current_version"`
	ActionID       string     `json:"action_id"`
	Actor          string     `json:"actor,omitempty"`
	Source         string     `json:"source,omitempty"`
	RequestedAt    time.Time  `json:"requested_at"`
	ExitCode       int        `json:"exit_code"`
}

type AdmissionError struct {
	Status Status
}

func (e *AdmissionError) Error() string {
	if e == nil {
		return ErrAdmissionClosed.Error()
	}
	return ErrAdmissionClosed.Error()
}

type RequestAbort struct {
	UserMessage string
	Reason      string
}

func (e *RequestAbort) Error() string {
	if e == nil {
		return "runtime request aborted"
	}
	if strings.TrimSpace(e.Reason) != "" {
		return e.Reason
	}
	if strings.TrimSpace(e.UserMessage) != "" {
		return e.UserMessage
	}
	return "runtime request aborted"
}

type Handle struct {
	manager *Manager
	id      string
	ctx     context.Context
	cancel  context.CancelCauseFunc

	mu         sync.Mutex
	phase      RequestPhase
	abort      func(context.Context)
	done       bool
	requestID  string
	requestTag string
}

type Manager struct {
	client releaseinfo.Client

	currentVersion   string
	stateDir         string
	minTargetVersion string
	forceExitAfter   time.Duration
	exitCode         int
	onReadyToExit    func(Intent)

	mu          sync.Mutex
	updatedAt   time.Time
	pending     *PendingAction
	handles     map[string]*Handle
	forceTimer  *time.Timer
	exitOnce    bool
	sequenceNum uint64
}

func NewManager(opts Options) *Manager {
	manager := &Manager{
		client: releaseinfo.Client{
			BaseURL:    strings.TrimSpace(opts.ReleaseBaseURL),
			HTTPClient: opts.HTTPClient,
		},
		currentVersion:   strings.TrimSpace(opts.CurrentVersion),
		stateDir:         strings.TrimSpace(opts.StateDir),
		minTargetVersion: strings.TrimSpace(opts.MinTargetVersion),
		forceExitAfter:   opts.ForceExitAfter,
		exitCode:         opts.ExitCode,
		onReadyToExit:    opts.OnReadyToExit,
		handles:          make(map[string]*Handle),
		updatedAt:        time.Now(),
	}
	if manager.minTargetVersion == "" {
		manager.minTargetVersion = DefaultMinTargetVersion
	}
	if manager.forceExitAfter <= 0 {
		manager.forceExitAfter = DefaultForceExitAfter
	}
	if manager.exitCode == 0 {
		manager.exitCode = DefaultLifecycleExitCode
	}
	manager.persistLocked()
	return manager
}

func (m *Manager) Status() Status {
	if m == nil {
		return Status{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) AdmitRequest(
	ctx context.Context,
	requestTag string,
) (*Handle, error) {
	if m == nil {
		return nil, nil
	}

	m.mu.Lock()
	if m.pending != nil {
		status := m.snapshotLocked()
		m.mu.Unlock()
		return nil, &AdmissionError{Status: status}
	}

	handleID := m.nextHandleIDLocked()
	reqCtx, cancel := context.WithCancelCause(ctx)
	handle := &Handle{
		manager:    m,
		id:         handleID,
		ctx:        reqCtx,
		cancel:     cancel,
		phase:      RequestPhaseAccepted,
		requestTag: strings.TrimSpace(requestTag),
	}
	m.handles[handleID] = handle
	m.updatedAt = time.Now()
	m.persistLocked()
	m.mu.Unlock()
	return handle, nil
}

func (m *Manager) RequestAction(
	ctx context.Context,
	req ActionRequest,
) (ActionResult, error) {
	if m == nil {
		return ActionResult{}, fmt.Errorf("nil lifecycle manager")
	}

	pending, err := m.resolvePendingAction(ctx, req)
	if err != nil {
		return ActionResult{}, err
	}

	var (
		aborters []func(context.Context)
		ready    bool
		intent   *Intent
	)

	m.mu.Lock()
	if m.pending != nil {
		result := ActionResult{
			Status:  m.snapshotLocked(),
			Started: false,
		}
		m.mu.Unlock()
		return result, ErrActionInProgress
	}

	m.pending = pending
	m.updatedAt = time.Now()
	if pending.Mode == ModeForce {
		aborters = m.collectAbortersLocked()
		m.armForceTimerLocked()
	}
	m.persistLocked()
	result := ActionResult{
		Status:  m.snapshotLocked(),
		Started: true,
	}
	if len(m.handles) == 0 {
		intent = m.markReadyLocked()
		ready = intent != nil
	}
	m.mu.Unlock()

	runAborters(aborters)
	if ready {
		m.fireReady(intent)
	}
	return result, nil
}

func (m *Manager) ListVersions(
	ctx context.Context,
) (releaseinfo.Index, error) {
	if m == nil {
		return releaseinfo.Index{}, fmt.Errorf("nil lifecycle manager")
	}
	return m.client.FetchIndex(ctx)
}

func (m *Manager) FetchChangeSummary(
	ctx context.Context,
	version string,
	limit int,
) ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("nil lifecycle manager")
	}
	return m.client.FetchChangeSummary(ctx, version, limit)
}

func (m *Manager) FetchChangelog(
	ctx context.Context,
	version string,
) (string, error) {
	if m == nil {
		return "", fmt.Errorf("nil lifecycle manager")
	}
	return m.client.FetchChangelog(ctx, version)
}

func (m *Manager) resolvePendingAction(
	ctx context.Context,
	req ActionRequest,
) (*PendingAction, error) {
	req.Kind = normalizeActionKind(req.Kind)
	req.Mode = normalizeActionMode(req.Mode)
	switch req.Kind {
	case ActionRestart, ActionUpgrade:
	default:
		return nil, fmt.Errorf(
			"unsupported lifecycle action %q",
			req.Kind,
		)
	}
	switch req.Mode {
	case ModeGraceful, ModeForce:
	default:
		return nil, fmt.Errorf(
			"unsupported lifecycle mode %q",
			req.Mode,
		)
	}

	targetVersion := strings.TrimSpace(req.TargetVersion)
	targetChannel := strings.TrimSpace(req.TargetChannel)
	summary := []string(nil)
	if req.Kind == ActionUpgrade {
		if targetVersion == "" {
			if targetChannel == "" {
				targetChannel = releaseinfo.ChannelLatest
			}
			latest, err := m.client.FetchChannelVersion(
				ctx,
				targetChannel,
			)
			if err != nil {
				return nil, err
			}
			targetVersion = latest
		} else if targetChannel != "" {
			return nil, fmt.Errorf(
				"target version and channel cannot both be set",
			)
		}
		if releaseinfo.CompareVersions(
			targetVersion,
			m.minTargetVersion,
		) < 0 {
			return nil, fmt.Errorf(
				"target version %s is below minimum %s",
				targetVersion,
				m.minTargetVersion,
			)
		}
		notes, err := m.client.FetchChannelChangeSummary(
			ctx,
			targetChannel,
			targetVersion,
			3,
		)
		if err == nil {
			summary = notes
		}
	}

	return &PendingAction{
		ID:             strconv.FormatUint(m.nextSequence(), 10),
		Kind:           req.Kind,
		Mode:           req.Mode,
		TargetVersion:  targetVersion,
		TargetChannel:  targetChannel,
		Actor:          strings.TrimSpace(req.Actor),
		Source:         strings.TrimSpace(req.Source),
		RequestedAt:    time.Now(),
		CurrentVersion: m.currentVersion,
		Summary:        summary,
	}, nil
}

func (m *Manager) snapshotLocked() Status {
	status := Status{
		State:          StateIdle,
		CurrentVersion: m.currentVersion,
		ActiveRequests: len(m.handles),
		UpdatedAt:      m.updatedAt,
		ExitCode:       m.exitCode,
	}
	for _, handle := range m.handles {
		handle.mu.Lock()
		phase := handle.phase
		handle.mu.Unlock()
		if phase == RequestPhaseRunning {
			status.RunningRequests++
			continue
		}
		status.QueuedRequests++
	}
	if m.pending != nil {
		status.Pending = clonePending(m.pending)
		if m.exitOnce {
			status.State = StateReady
		} else {
			status.State = StateDraining
		}
	}
	return status
}

func (m *Manager) nextHandleIDLocked() string {
	return "req-" + strconv.FormatUint(
		m.nextSequence(),
		10,
	)
}

func (m *Manager) nextSequence() uint64 {
	return atomic.AddUint64(&m.sequenceNum, 1)
}

func (m *Manager) collectAbortersLocked() []func(context.Context) {
	if len(m.handles) == 0 {
		return nil
	}
	aborters := make([]func(context.Context), 0, len(m.handles))
	cause := cancelCauseForPending(m.pending)
	for _, handle := range m.handles {
		handle.mu.Lock()
		cancel := handle.cancel
		abort := handle.abort
		handle.mu.Unlock()
		if cancel != nil {
			cancel(cause)
		}
		if abort != nil {
			aborters = append(aborters, abort)
		}
	}
	return aborters
}

func (m *Manager) armForceTimerLocked() {
	if m.forceTimer != nil {
		m.forceTimer.Stop()
	}
	m.forceTimer = time.AfterFunc(
		m.forceExitAfter,
		func() {
			var intent *Intent
			m.mu.Lock()
			intent = m.markReadyLocked()
			m.mu.Unlock()
			m.fireReady(intent)
		},
	)
}

func (m *Manager) markReadyLocked() *Intent {
	if m.pending == nil || m.exitOnce {
		return nil
	}
	m.exitOnce = true
	m.updatedAt = time.Now()
	m.persistLocked()
	return &Intent{
		Action:         m.pending.Kind,
		Mode:           m.pending.Mode,
		TargetVersion:  m.pending.TargetVersion,
		TargetChannel:  m.pending.TargetChannel,
		CurrentVersion: m.currentVersion,
		ActionID:       m.pending.ID,
		Actor:          m.pending.Actor,
		Source:         m.pending.Source,
		RequestedAt:    m.pending.RequestedAt,
		ExitCode:       m.exitCode,
	}
}

func (m *Manager) fireReady(intent *Intent) {
	if m == nil || intent == nil || m.onReadyToExit == nil {
		return
	}
	m.onReadyToExit(*intent)
}

func (m *Manager) releaseHandle(handleID string) {
	if m == nil {
		return
	}

	var intent *Intent
	m.mu.Lock()
	delete(m.handles, handleID)
	m.updatedAt = time.Now()
	m.persistLocked()
	if len(m.handles) == 0 && m.pending != nil {
		intent = m.markReadyLocked()
	}
	m.mu.Unlock()
	m.fireReady(intent)
}

func (m *Manager) persistLocked() {
	if strings.TrimSpace(m.stateDir) == "" {
		return
	}
	status := m.snapshotLocked()
	statusPath := filepath.Join(
		m.lifecycleDir(),
		statusFileName,
	)
	_ = writeJSONFile(statusPath, status)
	if status.Pending == nil {
		_ = os.Remove(filepath.Join(
			m.lifecycleDir(),
			intentJSONName,
		))
		_ = os.Remove(filepath.Join(
			m.lifecycleDir(),
			intentEnvFileName,
		))
		return
	}
	intent := Intent{
		Action:         status.Pending.Kind,
		Mode:           status.Pending.Mode,
		TargetVersion:  status.Pending.TargetVersion,
		TargetChannel:  status.Pending.TargetChannel,
		CurrentVersion: status.CurrentVersion,
		ActionID:       status.Pending.ID,
		Actor:          status.Pending.Actor,
		Source:         status.Pending.Source,
		RequestedAt:    status.Pending.RequestedAt,
		ExitCode:       status.ExitCode,
	}
	_ = writeJSONFile(
		filepath.Join(m.lifecycleDir(), intentJSONName),
		intent,
	)
	_ = writeEnvFile(
		filepath.Join(m.lifecycleDir(), intentEnvFileName),
		intent,
	)
}

func (m *Manager) lifecycleDir() string {
	return filepath.Join(
		m.stateDir,
		runtimeDirName,
		lifecycleDirName,
	)
}

func (h *Handle) Context() context.Context {
	if h == nil {
		return context.Background()
	}
	return h.ctx
}

func (h *Handle) MarkRunning() {
	if h == nil || h.manager == nil {
		return
	}
	h.mu.Lock()
	h.phase = RequestPhaseRunning
	h.mu.Unlock()

	h.manager.mu.Lock()
	h.manager.updatedAt = time.Now()
	h.manager.persistLocked()
	h.manager.mu.Unlock()
}

func (h *Handle) SetAbort(
	requestID string,
	fn func(context.Context),
) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.requestID = strings.TrimSpace(requestID)
	h.abort = fn
	h.mu.Unlock()
}

func (h *Handle) Done() {
	if h == nil || h.manager == nil {
		return
	}
	h.mu.Lock()
	if h.done {
		h.mu.Unlock()
		return
	}
	h.done = true
	h.mu.Unlock()
	h.manager.releaseHandle(h.id)
}

func UserMessageFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	cause := context.Cause(ctx)
	if cause == nil {
		return ""
	}
	abort := &RequestAbort{}
	if errors.As(cause, &abort) {
		return strings.TrimSpace(abort.UserMessage)
	}
	return ""
}

func normalizeActionKind(kind ActionKind) ActionKind {
	switch ActionKind(strings.ToLower(strings.TrimSpace(string(kind)))) {
	case ActionRestart:
		return ActionRestart
	case ActionUpgrade:
		return ActionUpgrade
	default:
		return kind
	}
}

func normalizeActionMode(mode ActionMode) ActionMode {
	switch ActionMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case ModeForce:
		return ModeForce
	case ModeGraceful:
		return ModeGraceful
	default:
		return mode
	}
}

func clonePending(pending *PendingAction) *PendingAction {
	if pending == nil {
		return nil
	}
	cloned := *pending
	if len(pending.Summary) > 0 {
		cloned.Summary = append(
			[]string(nil),
			pending.Summary...,
		)
	}
	return &cloned
}

func cancelCauseForPending(
	pending *PendingAction,
) error {
	if pending == nil {
		return &RequestAbort{
			UserMessage: "The request was canceled by runtime " +
				"maintenance.",
		}
	}
	switch pending.Kind {
	case ActionUpgrade:
		return &RequestAbort{
			UserMessage: "The request was canceled because " +
				"the runtime is forcing an upgrade.",
			Reason: "runtime force upgrade",
		}
	default:
		return &RequestAbort{
			UserMessage: "The request was canceled because " +
				"the runtime is forcing a restart.",
			Reason: "runtime force restart",
		}
	}
}

func runAborters(aborters []func(context.Context)) {
	if len(aborters) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	for _, abort := range aborters {
		if abort == nil {
			continue
		}
		abort(ctx)
	}
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func writeEnvFile(path string, intent Intent) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lines := []string{
		"TRPC_CLAW_LIFECYCLE_ACTION=" + shellValue(
			string(intent.Action),
		),
		"TRPC_CLAW_LIFECYCLE_MODE=" + shellValue(
			string(intent.Mode),
		),
		"TRPC_CLAW_LIFECYCLE_TARGET_VERSION=" + shellValue(
			intent.TargetVersion,
		),
		"TRPC_CLAW_LIFECYCLE_TARGET_CHANNEL=" + shellValue(
			intent.TargetChannel,
		),
		"TRPC_CLAW_LIFECYCLE_CURRENT_VERSION=" + shellValue(
			intent.CurrentVersion,
		),
		"TRPC_CLAW_LIFECYCLE_ACTION_ID=" + shellValue(
			intent.ActionID,
		),
		"TRPC_CLAW_LIFECYCLE_ACTOR=" + shellValue(
			intent.Actor,
		),
		"TRPC_CLAW_LIFECYCLE_SOURCE=" + shellValue(
			intent.Source,
		),
		"TRPC_CLAW_LIFECYCLE_EXIT_CODE=" + shellValue(
			strconv.Itoa(intent.ExitCode),
		),
	}
	data := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(data), 0o600)
}

func shellValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "'", `'"'"'`)
	return "'" + value + "'"
}
