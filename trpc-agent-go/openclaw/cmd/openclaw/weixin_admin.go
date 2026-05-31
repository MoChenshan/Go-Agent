package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tlog "git.code.oa.com/trpc-go/trpc-go/log"
	weixinchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/weixin"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	weixinAdminPagePath = "/weixin"

	weixinAdminStatusPath = "/api/weixin/status"

	weixinAdminQREntryPath = "/channels/wx_qr"

	weixinAdminLoginStartPath  = "/api/weixin/login/start"
	weixinAdminLoginCancelPath = "/api/weixin/login/cancel"

	weixinAdminAccountRemovePath = "/api/weixin/accounts/remove"
	weixinAdminAccountResumePath = "/api/weixin/accounts/resume"

	weixinAdminRefreshInterval = 5 * time.Second

	weixinAdminQueryNotice = "notice"
	weixinAdminQueryError  = "error"

	weixinAdminFormRuntimeKey = "runtime_key"
	weixinAdminFormAccountID  = "account_id"
	weixinAdminFormBaseURL    = "base_url"
	weixinAdminFormBotType    = "bot_type"

	weixinAdminAnchorPrefix = "weixin-runtime-"

	weixinAdminStateReady       = "ready"
	weixinAdminStatePaused      = "paused"
	weixinAdminStateMissing     = "missing_token"
	weixinAdminStateCancelled   = "cancelled"
	weixinAdminStateFailed      = "failed"
	weixinAdminStateStarting    = "starting"
	weixinAdminSessionCancelled = "login cancelled"

	weixinAdminDefaultTitle = "Weixin Runtime"

	weixinLoginStatusWait       = "wait"
	weixinLoginStatusScanned    = "scaned"
	weixinLoginStatusRedirected = "scaned_but_redirect"
	weixinLoginStatusConfirmed  = "confirmed"
	weixinLoginStatusExpired    = "expired"

	weixinAdminQREntryRefreshSeconds = 2

	weixinAdminQREntryWaitTimeout = 1500 * time.Millisecond

	weixinAdminQREntryPollInterval = 50 * time.Millisecond

	weixinAdminQREntryChannelsLink = "../channels"
)

var (
	errWeixinLoginInProgress = errors.New(
		"weixin admin: login session already running",
	)
	errWeixinLoginNotRunning = errors.New(
		"weixin admin: no login session is running",
	)
)

var weixinAdminQREntryPageTemplate = template.Must(
	template.New("weixin-qr-entry").Parse(
		weixinAdminQREntryPageHTML,
	),
)

const weixinAdminQREntryPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.PageTitle}}</title>
  {{if .AutoRefresh}}
  <meta http-equiv="refresh" content="{{.RefreshSeconds}}">
  {{end}}
  <style>
    :root {
      color-scheme: light;
      --bg: #f5efe8;
      --panel: rgba(255, 252, 247, 0.94);
      --line: #d7cfc2;
      --ink: #1d1a16;
      --muted: #5f574d;
      --accent: #0f6f61;
      --shadow: 0 18px 40px rgba(35, 29, 22, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 24px;
      font-family: "Iowan Old Style", "Palatino Linotype", serif;
      color: var(--ink);
      background:
        radial-gradient(circle at top left, #fff8ef, transparent 38%),
        linear-gradient(180deg, #efe7dc 0%, var(--bg) 100%);
    }
    main {
      width: min(760px, 100%);
      padding: 32px;
      border: 1px solid rgba(215, 207, 194, 0.92);
      border-radius: 28px;
      background: var(--panel);
      box-shadow: var(--shadow);
    }
    .kicker {
      margin: 0 0 10px;
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.12em;
    }
    h1 {
      margin: 0 0 14px;
      font-size: 34px;
      line-height: 1.1;
    }
    p, li, a, code {
      font-size: 15px;
      line-height: 1.6;
    }
    code {
      background: rgba(15, 111, 97, 0.08);
      padding: 2px 6px;
      border-radius: 8px;
      word-break: break-all;
    }
    .subtle {
      margin: 0;
      color: var(--muted);
    }
    .panel {
      margin-top: 18px;
      padding: 18px 20px;
      border: 1px solid rgba(215, 207, 194, 0.92);
      border-radius: 20px;
      background: rgba(255, 253, 248, 0.88);
    }
    .panel h2 {
      margin: 0 0 10px;
      font-size: 18px;
    }
    .panel ul {
      margin: 10px 0 0;
      padding-left: 20px;
    }
    .actions {
      margin-top: 24px;
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
    }
    .action-link {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 44px;
      padding: 0 18px;
      border-radius: 999px;
      border: 1px solid rgba(15, 111, 97, 0.24);
      color: var(--accent);
      text-decoration: none;
      font-weight: 700;
      background: rgba(15, 111, 97, 0.08);
    }
  </style>
</head>
<body>
  <main>
    <p class="kicker">TRPC-CLAW admin</p>
    <h1>{{.Heading}}</h1>
    <p class="subtle">{{.Summary}}</p>
    <section class="panel">
      <h2>Runtime</h2>
      <p><code>{{.RuntimeTitle}}</code></p>
      {{if .Accounts}}
      <ul>
        {{range .Accounts}}
        <li>
          <code>{{.AccountID}}</code>
          {{if .UserID}} · <code>{{.UserID}}</code>{{end}}
          {{if .StateLabel}} · {{.StateLabel}}{{end}}
        </li>
        {{end}}
      </ul>
      {{end}}
    </section>
    <div class="actions">
      <a class="action-link" href="{{.ChannelsLink}}">
        Back to Channels
      </a>
    </div>
  </main>
</body>
</html>
`

type weixinAdminTargetProvider interface {
	WeixinAdminTarget() weixinchannel.AdminTarget
}

type weixinLoginRunner func(
	ctx context.Context,
	stateDir string,
	baseURL string,
	botType string,
	callbacks weixinchannel.LoginCallbacks,
) (weixinchannel.Account, error)

type weixinAdminService struct {
	runtimes []*weixinAdminRuntime
	index    map[string]*weixinAdminRuntime
}

type weixinAdminRuntime struct {
	Key            string
	Title          string
	StateDir       string
	DefaultBaseURL string
	Login          *weixinLoginSessionManager
}

type weixinLoginSessionManager struct {
	mu     sync.RWMutex
	runner weixinLoginRunner

	session *weixinLoginSession
	cancel  context.CancelFunc
	nextID  uint64
}

type weixinLoginSession struct {
	ID             string
	BaseURL        string
	BotType        string
	Status         string
	QRCodeURL      string
	Error          string
	SavedAccountID string
	StartedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
	Active         bool
}

type weixinAdminPageData struct {
	Notice            string
	Error             string
	AutoRefresh       bool
	RefreshIntervalMS int64
	Runtimes          []weixinAdminRuntimeView
}

type weixinAdminQREntryPageData struct {
	PageTitle      string
	Heading        string
	Summary        string
	RuntimeTitle   string
	ChannelsLink   string
	RefreshSeconds int
	AutoRefresh    bool
	Accounts       []weixinAdminAccountStateView
}

type weixinAdminRuntimeView struct {
	Key            string                        `json:"key"`
	Anchor         string                        `json:"anchor"`
	Title          string                        `json:"title"`
	StateDir       string                        `json:"state_dir"`
	DefaultBaseURL string                        `json:"default_base_url"`
	DefaultBotType string                        `json:"default_bot_type"`
	GeneratedAt    string                        `json:"generated_at"`
	AccountCount   int                           `json:"account_count"`
	LoadError      string                        `json:"load_error,omitempty"`
	Login          weixinAdminLoginSessionView   `json:"login"`
	Accounts       []weixinAdminAccountStateView `json:"accounts"`
}

type weixinAdminLoginSessionView struct {
	Exists         bool   `json:"exists"`
	Active         bool   `json:"active"`
	Status         string `json:"status,omitempty"`
	StatusLabel    string `json:"status_label,omitempty"`
	StatusClass    string `json:"status_class,omitempty"`
	QRCodeURL      string `json:"qr_code_url,omitempty"`
	Error          string `json:"error,omitempty"`
	BaseURL        string `json:"base_url,omitempty"`
	BotType        string `json:"bot_type,omitempty"`
	SavedAccountID string `json:"saved_account_id,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
}

type weixinAdminAccountStateView struct {
	AccountID        string `json:"account_id"`
	UserID           string `json:"user_id,omitempty"`
	BaseURL          string `json:"base_url,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	State            string `json:"state,omitempty"`
	StateLabel       string `json:"state_label,omitempty"`
	StateClass       string `json:"state_class,omitempty"`
	PauseRemaining   string `json:"pause_remaining,omitempty"`
	LastEventAt      string `json:"last_event_at,omitempty"`
	LastInboundAt    string `json:"last_inbound_at,omitempty"`
	LastOutboundAt   string `json:"last_outbound_at,omitempty"`
	LastError        string `json:"last_error,omitempty"`
	ContextPeerCount int    `json:"context_peer_count"`
	CanResume        bool   `json:"can_resume"`
}

type weixinAdminQREntryState struct {
	runtime  *weixinAdminRuntime
	session  weixinLoginSession
	accounts []weixinchannel.AdminAccountState
}

func collectRuntimeWeixinAdminTargets(
	channels []occhannel.Channel,
) []weixinchannel.AdminTarget {
	seen := make(map[string]weixinchannel.AdminTarget)
	for _, ch := range channels {
		provider, ok := ch.(weixinAdminTargetProvider)
		if !ok || provider == nil {
			continue
		}
		target := provider.WeixinAdminTarget()
		stateDir := strings.TrimSpace(target.StateDir)
		if stateDir == "" {
			continue
		}
		current, exists := seen[stateDir]
		if !exists {
			seen[stateDir] = target
			continue
		}
		if strings.TrimSpace(current.DefaultBaseURL) == "" &&
			strings.TrimSpace(target.DefaultBaseURL) != "" {
			seen[stateDir] = target
		}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]weixinchannel.AdminTarget, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func newWeixinAdminService(
	targets []weixinchannel.AdminTarget,
) *weixinAdminService {
	return newWeixinAdminServiceWithRunner(
		targets,
		weixinchannel.LoginWithQR,
	)
}

func newWeixinAdminServiceWithRunner(
	targets []weixinchannel.AdminTarget,
	runner weixinLoginRunner,
) *weixinAdminService {
	if len(targets) == 0 {
		return nil
	}

	svc := &weixinAdminService{
		runtimes: make([]*weixinAdminRuntime, 0, len(targets)),
		index:    make(map[string]*weixinAdminRuntime, len(targets)),
	}
	for i, target := range targets {
		key := weixinAdminRuntimeKey(i + 1)
		runtime := &weixinAdminRuntime{
			Key:            key,
			Title:          weixinAdminRuntimeTitle(i+1, len(targets)),
			StateDir:       strings.TrimSpace(target.StateDir),
			DefaultBaseURL: weixinAdminDefaultBaseURL(target),
			Login: newWeixinLoginSessionManager(
				runner,
			),
		}
		svc.runtimes = append(svc.runtimes, runtime)
		svc.index[key] = runtime
	}
	return svc
}

func weixinAdminDefaultBaseURL(
	target weixinchannel.AdminTarget,
) string {
	baseURL := strings.TrimSpace(target.DefaultBaseURL)
	if baseURL != "" {
		return baseURL
	}
	return "https://ilinkai.weixin.qq.com"
}

func weixinAdminRuntimeKey(index int) string {
	return "weixin-" + strconv.Itoa(index)
}

func weixinAdminRuntimeTitle(
	index int,
	total int,
) string {
	if total <= 1 {
		return weixinAdminDefaultTitle
	}
	return weixinAdminDefaultTitle + " " + strconv.Itoa(index)
}

func newWeixinLoginSessionManager(
	runner weixinLoginRunner,
) *weixinLoginSessionManager {
	return &weixinLoginSessionManager{
		runner: runner,
	}
}

func (m *weixinLoginSessionManager) Start(
	stateDir string,
	baseURL string,
	botType string,
) error {
	if m == nil {
		return fmt.Errorf("weixin admin: missing login manager")
	}

	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return fmt.Errorf("weixin admin: empty state dir")
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://ilinkai.weixin.qq.com"
	}
	botType = strings.TrimSpace(botType)
	if botType == "" {
		botType = weixinDefaultLoginBotType
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session != nil && m.session.Active {
		return errWeixinLoginInProgress
	}

	m.nextID++
	now := time.Now()
	session := &weixinLoginSession{
		ID:        fmt.Sprintf("login-%d", m.nextID),
		BaseURL:   baseURL,
		BotType:   botType,
		Status:    weixinAdminStateStarting,
		StartedAt: now,
		UpdatedAt: now,
		Active:    true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.session = session
	m.cancel = cancel

	go m.runLogin(
		ctx,
		stateDir,
		session.ID,
		baseURL,
		botType,
	)
	return nil
}

func (m *weixinLoginSessionManager) runLogin(
	ctx context.Context,
	stateDir string,
	sessionID string,
	baseURL string,
	botType string,
) {
	runner := m.runner
	if runner == nil {
		runner = weixinchannel.LoginWithQR
	}

	account, err := runner(
		ctx,
		stateDir,
		baseURL,
		botType,
		weixinchannel.LoginCallbacks{
			OnQRCode: func(qrURL string) {
				m.updateSession(sessionID, func(
					session *weixinLoginSession,
				) {
					session.QRCodeURL = strings.TrimSpace(qrURL)
				})
			},
			OnStatus: func(status string) {
				m.updateSession(sessionID, func(
					session *weixinLoginSession,
				) {
					session.Status = strings.TrimSpace(status)
				})
			},
		},
	)
	if err != nil {
		m.finishSession(sessionID, err, "")
		return
	}
	m.finishSession(sessionID, nil, account.AccountID)
}

func (m *weixinLoginSessionManager) updateSession(
	sessionID string,
	update func(*weixinLoginSession),
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session == nil || m.session.ID != sessionID {
		return
	}
	update(m.session)
	m.session.UpdatedAt = time.Now()
}

func (m *weixinLoginSessionManager) finishSession(
	sessionID string,
	err error,
	accountID string,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session == nil || m.session.ID != sessionID {
		return
	}

	now := time.Now()
	m.session.Active = false
	m.session.CompletedAt = cloneLocalTime(now)
	m.session.UpdatedAt = now
	if err == nil {
		m.session.Status = weixinLoginStatusConfirmed
		m.session.Error = ""
		m.session.SavedAccountID = strings.TrimSpace(accountID)
	} else if errors.Is(err, context.Canceled) {
		m.session.Status = weixinAdminStateCancelled
		m.session.Error = ""
	} else {
		m.session.Status = weixinAdminStateFailed
		m.session.Error = strings.TrimSpace(err.Error())
	}
	m.cancel = nil
}

func (m *weixinLoginSessionManager) Cancel() error {
	if m == nil {
		return errWeixinLoginNotRunning
	}

	m.mu.Lock()
	cancel := m.cancel
	active := m.session != nil && m.session.Active
	m.mu.Unlock()

	if !active || cancel == nil {
		return errWeixinLoginNotRunning
	}
	cancel()
	return nil
}

func (m *weixinLoginSessionManager) Close() {
	if m == nil {
		return
	}
	_ = m.Cancel()
}

func (m *weixinLoginSessionManager) Snapshot() weixinLoginSession {
	if m == nil {
		return weixinLoginSession{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.session == nil {
		return weixinLoginSession{}
	}

	snapshot := *m.session
	if m.session.CompletedAt != nil {
		snapshot.CompletedAt = cloneLocalTime(*m.session.CompletedAt)
	}
	return snapshot
}

func cloneLocalTime(value time.Time) *time.Time {
	cloned := value
	return &cloned
}

func (s *weixinAdminService) Close() {
	if s == nil {
		return
	}
	for _, runtime := range s.runtimes {
		if runtime.Login != nil {
			runtime.Login.Close()
		}
	}
}

func (s *weixinAdminService) snapshot() weixinAdminPageData {
	out := weixinAdminPageData{
		RefreshIntervalMS: int64(
			weixinAdminRefreshInterval / time.Millisecond,
		),
		Runtimes: make(
			[]weixinAdminRuntimeView,
			0,
			len(s.runtimes),
		),
	}
	if s == nil {
		return out
	}

	now := time.Now()
	for _, runtime := range s.runtimes {
		view := weixinAdminRuntimeView{
			Key:            runtime.Key,
			Anchor:         weixinAdminAnchor(runtime.Key),
			Title:          runtime.Title,
			StateDir:       runtime.StateDir,
			DefaultBaseURL: runtime.DefaultBaseURL,
			DefaultBotType: weixinDefaultLoginBotType,
			GeneratedAt:    formatAdminTime(now),
		}

		login := runtime.Login.Snapshot()
		if login.ID != "" {
			view.Login = buildWeixinAdminLoginSessionView(login)
			out.AutoRefresh = out.AutoRefresh || view.Login.Active
		}

		accountStates, err := weixinchannel.ListAdminAccountStates(
			runtime.StateDir,
		)
		if err != nil {
			view.LoadError = strings.TrimSpace(err.Error())
		} else {
			view.Accounts = buildWeixinAdminAccountViews(
				accountStates,
				now,
			)
			view.AccountCount = len(view.Accounts)
		}

		out.Runtimes = append(out.Runtimes, view)
	}
	return out
}

func waitForWeixinAdminQRCode(
	manager *weixinLoginSessionManager,
	timeout time.Duration,
) weixinLoginSession {
	if manager == nil {
		return weixinLoginSession{}
	}
	snapshot := manager.Snapshot()
	if timeout <= 0 {
		return snapshot
	}
	deadline := time.Now().Add(timeout)
	for snapshot.Active &&
		strings.TrimSpace(snapshot.QRCodeURL) == "" &&
		time.Now().Before(deadline) {
		time.Sleep(weixinAdminQREntryPollInterval)
		snapshot = manager.Snapshot()
	}
	return snapshot
}

func buildWeixinAdminLoginSessionView(
	session weixinLoginSession,
) weixinAdminLoginSessionView {
	status := strings.TrimSpace(session.Status)
	if status == "" {
		status = weixinAdminStateStarting
	}
	return weixinAdminLoginSessionView{
		Exists:         true,
		Active:         session.Active,
		Status:         status,
		StatusLabel:    weixinLoginStatusLabel(status),
		StatusClass:    weixinStatusClass(status),
		QRCodeURL:      strings.TrimSpace(session.QRCodeURL),
		Error:          strings.TrimSpace(session.Error),
		BaseURL:        strings.TrimSpace(session.BaseURL),
		BotType:        strings.TrimSpace(session.BotType),
		SavedAccountID: strings.TrimSpace(session.SavedAccountID),
		StartedAt:      formatAdminTime(session.StartedAt),
		UpdatedAt:      formatAdminTime(session.UpdatedAt),
		CompletedAt:    formatAdminTimePtrIf(session.CompletedAt),
	}
}

func buildWeixinAdminAccountViews(
	states []weixinchannel.AdminAccountState,
	now time.Time,
) []weixinAdminAccountStateView {
	out := make([]weixinAdminAccountStateView, 0, len(states))
	for _, state := range states {
		statusKey, statusLabel, canResume := weixinAccountStatus(
			state,
			now,
		)
		out = append(out, weixinAdminAccountStateView{
			AccountID:      state.Account.AccountID,
			UserID:         strings.TrimSpace(state.Account.UserID),
			BaseURL:        strings.TrimSpace(state.Account.BaseURL),
			UpdatedAt:      formatAdminTimeIf(state.Account.UpdatedAt),
			State:          statusKey,
			StateLabel:     statusLabel,
			StateClass:     weixinStatusClass(statusKey),
			PauseRemaining: formatPauseRemaining(state.Status, now),
			LastEventAt:    formatAdminTimePtr(state.Status.LastEventAt),
			LastInboundAt:  formatAdminTimePtr(state.Status.LastInboundAt),
			LastOutboundAt: formatAdminTimePtr(
				state.Status.LastOutboundAt,
			),
			LastError:        strings.TrimSpace(state.Status.LastError),
			ContextPeerCount: state.ContextPeerCount,
			CanResume:        canResume,
		})
	}
	return out
}

func weixinAccountStatus(
	state weixinchannel.AdminAccountState,
	now time.Time,
) (string, string, bool) {
	if strings.TrimSpace(state.Account.Token) == "" {
		return weixinAdminStateMissing, "Missing Token", false
	}

	if state.Status.PausedUntil != nil &&
		now.Before(*state.Status.PausedUntil) {
		return weixinAdminStatePaused, "Paused", true
	}
	return weixinAdminStateReady, "Ready", false
}

func formatPauseRemaining(
	status weixinchannel.RuntimeStatus,
	now time.Time,
) string {
	if status.PausedUntil == nil || !now.Before(*status.PausedUntil) {
		return ""
	}
	return "Until " + formatAdminTime(*status.PausedUntil) +
		" (" + status.PausedUntil.Sub(now).Round(time.Second).String() +
		" left)"
}

func weixinStatusClass(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return weixinAdminStateStarting
	}
	return strings.ReplaceAll(status, " ", "_")
}

func weixinLoginStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case weixinLoginStatusWait:
		return "Waiting For Scan"
	case weixinLoginStatusScanned:
		return "Scanned"
	case weixinLoginStatusRedirected:
		return "Redirected"
	case weixinLoginStatusConfirmed:
		return "Confirmed"
	case weixinLoginStatusExpired:
		return "Expired"
	case weixinAdminStateCancelled:
		return "Cancelled"
	case weixinAdminStateFailed:
		return "Failed"
	default:
		return "Starting"
	}
}

func formatAdminTime(value time.Time) string {
	if value.IsZero() {
		return "N/A"
	}
	return value.Format(time.RFC3339)
}

func formatAdminTimePtr(value *time.Time) string {
	if value == nil {
		return "N/A"
	}
	return formatAdminTime(*value)
}

func formatAdminTimePtrIf(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatAdminTime(*value)
}

func formatAdminTimeIf(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatAdminTime(value)
}

func weixinAdminAnchor(runtimeKey string) string {
	return weixinAdminAnchorPrefix + strings.TrimSpace(runtimeKey)
}

func wrapWeixinAdminHandler(
	base http.Handler,
	service *weixinAdminService,
) http.Handler {
	if service == nil {
		return base
	}

	mux := http.NewServeMux()
	mux.HandleFunc(
		weixinAdminPagePath,
		service.handlePage,
	)
	mux.HandleFunc(
		weixinAdminStatusPath,
		service.handleStatusJSON,
	)
	mux.HandleFunc(
		weixinAdminQREntryPath,
		service.handleQREntry,
	)
	mux.HandleFunc(
		weixinAdminLoginStartPath,
		service.handleStartLogin,
	)
	mux.HandleFunc(
		weixinAdminLoginCancelPath,
		service.handleCancelLogin,
	)
	mux.HandleFunc(
		weixinAdminAccountRemovePath,
		service.handleRemoveAccount,
	)
	mux.HandleFunc(
		weixinAdminAccountResumePath,
		service.handleResumeAccount,
	)
	if base != nil {
		mux.Handle("/", base)
	}
	return mux
}

func (s *weixinAdminService) handlePage(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeAdminRedirect(
		w,
		weixinAdminRedirectLocation(
			r.URL.Path,
			r.URL.Query(),
			"",
		),
		http.StatusSeeOther,
	)
}

func (s *weixinAdminService) handleStatusJSON(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, s.snapshot())
}

func (s *weixinAdminService) handleQREntry(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	state, statusCode, err := s.prepareQREntry(r.URL.Query())
	if err != nil {
		writeWeixinAdminQREntryPage(
			w,
			statusCode,
			buildWeixinAdminQREntryErrorPage(
				weixinAdminDefaultTitle,
				err.Error(),
			),
		)
		return
	}
	if len(state.accounts) > 0 && !state.session.Active {
		writeWeixinAdminQREntryPage(
			w,
			http.StatusOK,
			buildWeixinAdminQREntryLinkedPage(
				state.runtime.Title,
				buildWeixinAdminAccountViews(
					state.accounts,
					time.Now(),
				),
			),
		)
		return
	}
	qrURL := strings.TrimSpace(state.session.QRCodeURL)
	if qrURL != "" {
		writeAdminRedirect(w, qrURL, http.StatusSeeOther)
		return
	}
	writeWeixinAdminQREntryPage(
		w,
		http.StatusOK,
		buildWeixinAdminQREntryWaitingPage(
			state.runtime.Title,
		),
	)
}

func (s *weixinAdminService) handleStartLogin(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runtime, err := s.runtimeFromRequest(r)
	if err != nil {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			err.Error(),
			"",
		)
		return
	}

	baseURL := strings.TrimSpace(r.FormValue(weixinAdminFormBaseURL))
	if baseURL == "" {
		baseURL = runtime.DefaultBaseURL
	}
	botType := strings.TrimSpace(r.FormValue(weixinAdminFormBotType))
	if botType == "" {
		botType = weixinDefaultLoginBotType
	}

	err = runtime.Login.Start(
		runtime.StateDir,
		baseURL,
		botType,
	)
	if err != nil {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			err.Error(),
			weixinAdminAnchor(runtime.Key),
		)
		return
	}

	s.redirectWithMessage(
		w,
		r,
		weixinAdminQueryNotice,
		"Started Weixin QR login.",
		weixinAdminAnchor(runtime.Key),
	)
}

func (s *weixinAdminService) handleCancelLogin(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runtime, err := s.runtimeFromRequest(r)
	if err != nil {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			err.Error(),
			"",
		)
		return
	}

	if err := runtime.Login.Cancel(); err != nil {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			err.Error(),
			weixinAdminAnchor(runtime.Key),
		)
		return
	}

	s.redirectWithMessage(
		w,
		r,
		weixinAdminQueryNotice,
		weixinAdminSessionCancelled,
		weixinAdminAnchor(runtime.Key),
	)
}

func (s *weixinAdminService) handleRemoveAccount(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runtime, err := s.runtimeFromRequest(r)
	if err != nil {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			err.Error(),
			"",
		)
		return
	}
	accountID := strings.TrimSpace(
		r.FormValue(weixinAdminFormAccountID),
	)
	if accountID == "" {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			"account_id is required",
			weixinAdminAnchor(runtime.Key),
		)
		return
	}
	if err := weixinchannel.RemoveAccount(
		runtime.StateDir,
		accountID,
	); err != nil {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			err.Error(),
			weixinAdminAnchor(runtime.Key),
		)
		return
	}
	s.redirectWithMessage(
		w,
		r,
		weixinAdminQueryNotice,
		"Removed Weixin account "+accountID+".",
		weixinAdminAnchor(runtime.Key),
	)
}

func (s *weixinAdminService) handleResumeAccount(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runtime, err := s.runtimeFromRequest(r)
	if err != nil {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			err.Error(),
			"",
		)
		return
	}
	accountID := strings.TrimSpace(
		r.FormValue(weixinAdminFormAccountID),
	)
	if accountID == "" {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			"account_id is required",
			weixinAdminAnchor(runtime.Key),
		)
		return
	}
	if err := weixinchannel.ResumeAccount(
		runtime.StateDir,
		accountID,
	); err != nil {
		s.redirectWithMessage(
			w,
			r,
			weixinAdminQueryError,
			err.Error(),
			weixinAdminAnchor(runtime.Key),
		)
		return
	}
	s.redirectWithMessage(
		w,
		r,
		weixinAdminQueryNotice,
		"Resumed Weixin account "+accountID+".",
		weixinAdminAnchor(runtime.Key),
	)
}

func (s *weixinAdminService) runtimeFromKey(
	key string,
) (*weixinAdminRuntime, error) {
	if s == nil {
		return nil, fmt.Errorf("weixin admin is not available")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("runtime_key is required")
	}
	runtime, ok := s.index[key]
	if !ok {
		return nil, fmt.Errorf("unknown Weixin runtime %q", key)
	}
	return runtime, nil
}

func (s *weixinAdminService) runtimeFromQuery(
	values url.Values,
) (*weixinAdminRuntime, error) {
	if s == nil {
		return nil, fmt.Errorf("weixin admin is not available")
	}
	key := strings.TrimSpace(values.Get(weixinAdminFormRuntimeKey))
	if key != "" {
		return s.runtimeFromKey(key)
	}
	switch len(s.runtimes) {
	case 0:
		return nil, fmt.Errorf("no Weixin runtime is configured")
	case 1:
		return s.runtimes[0], nil
	default:
		return nil, fmt.Errorf(
			"multiple Weixin runtimes are configured; " +
				"open Channels or add runtime_key",
		)
	}
}

func (s *weixinAdminService) prepareQREntry(
	values url.Values,
) (weixinAdminQREntryState, int, error) {
	runtime, err := s.runtimeFromQuery(values)
	if err != nil {
		return weixinAdminQREntryState{}, http.StatusBadRequest, err
	}
	accounts, err := weixinchannel.ListAdminAccountStates(
		runtime.StateDir,
	)
	if err != nil {
		return weixinAdminQREntryState{},
			http.StatusInternalServerError,
			fmt.Errorf("load Weixin accounts: %w", err)
	}
	session := runtime.Login.Snapshot()
	if len(accounts) == 0 && !session.Active {
		err = runtime.Login.Start(
			runtime.StateDir,
			runtime.DefaultBaseURL,
			weixinDefaultLoginBotType,
		)
		if err != nil &&
			!errors.Is(err, errWeixinLoginInProgress) {
			return weixinAdminQREntryState{},
				http.StatusInternalServerError,
				fmt.Errorf("start Weixin login: %w", err)
		}
	}
	if runtime.Login != nil {
		session = waitForWeixinAdminQRCode(
			runtime.Login,
			weixinAdminQREntryWaitTimeout,
		)
	}
	return weixinAdminQREntryState{
		runtime:  runtime,
		session:  session,
		accounts: accounts,
	}, http.StatusOK, nil
}

func (s *weixinAdminService) runtimeFromRequest(
	r *http.Request,
) (*weixinAdminRuntime, error) {
	return s.runtimeFromKey(r.FormValue(weixinAdminFormRuntimeKey))
}

func (s *weixinAdminService) redirectWithMessage(
	w http.ResponseWriter,
	r *http.Request,
	queryKey string,
	message string,
	anchor string,
) {
	values := url.Values{}
	message = strings.TrimSpace(message)
	if message != "" {
		values.Set(queryKey, message)
	}
	writeAdminRedirect(
		w,
		weixinAdminRedirectLocation(
			r.URL.Path,
			values,
			anchor,
		),
		http.StatusSeeOther,
	)
}

func weixinAdminRedirectLocation(
	currentPath string,
	values url.Values,
	anchor string,
) string {
	location := channelsAdminPagePath
	if encoded := values.Encode(); encoded != "" {
		location += "?" + encoded
	}
	anchor = strings.TrimSpace(anchor)
	if anchor != "" {
		location += "#" + anchor
	}
	return adminRelativeReference(currentPath, location)
}

func writeAdminRedirect(
	w http.ResponseWriter,
	location string,
	statusCode int,
) {
	if statusCode == 0 {
		statusCode = http.StatusSeeOther
	}
	w.Header().Set("Location", strings.TrimSpace(location))
	w.WriteHeader(statusCode)
}

func buildWeixinAdminQREntryWaitingPage(
	runtimeTitle string,
) weixinAdminQREntryPageData {
	return weixinAdminQREntryPageData{
		PageTitle: "Preparing Weixin QR Page",
		Heading:   "Preparing Weixin QR page",
		Summary: "This admin URL will refresh automatically and " +
			"redirect to the latest Weixin QR page as soon as " +
			"it is ready.",
		RuntimeTitle:   strings.TrimSpace(runtimeTitle),
		ChannelsLink:   weixinAdminQREntryChannelsLink,
		RefreshSeconds: weixinAdminQREntryRefreshSeconds,
		AutoRefresh:    true,
	}
}

func buildWeixinAdminQREntryLinkedPage(
	runtimeTitle string,
	accounts []weixinAdminAccountStateView,
) weixinAdminQREntryPageData {
	return weixinAdminQREntryPageData{
		PageTitle: "Weixin Account Already Linked",
		Heading:   "Weixin account already linked",
		Summary: "This runtime already has a saved Weixin account. " +
			"Open Channels if you want to inspect it or start a " +
			"manual relogin.",
		RuntimeTitle: strings.TrimSpace(runtimeTitle),
		ChannelsLink: weixinAdminQREntryChannelsLink,
		Accounts:     accounts,
	}
}

func buildWeixinAdminQREntryErrorPage(
	runtimeTitle string,
	message string,
) weixinAdminQREntryPageData {
	return weixinAdminQREntryPageData{
		PageTitle:    "Weixin QR Entry Unavailable",
		Heading:      "Weixin QR entry is unavailable",
		Summary:      strings.TrimSpace(message),
		RuntimeTitle: strings.TrimSpace(runtimeTitle),
		ChannelsLink: weixinAdminQREntryChannelsLink,
	}
}

func writeWeixinAdminQREntryPage(
	w http.ResponseWriter,
	statusCode int,
	data weixinAdminQREntryPageData,
) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	if err := weixinAdminQREntryPageTemplate.Execute(w, data); err != nil {
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
	}
}

func logWeixinAdminStartup(
	adminURL string,
	service *weixinAdminService,
) {
	if service == nil {
		return
	}
	adminURL = strings.TrimRight(strings.TrimSpace(adminURL), "/")
	if adminURL == "" {
		return
	}
	tlog.Infof(
		"Weixin Admin: %s%s (legacy %s redirects here, QR "+
			"entry %s%s)",
		adminURL,
		channelsAdminPagePath,
		weixinAdminPagePath,
		adminURL,
		weixinAdminQREntryPath,
	)
}
