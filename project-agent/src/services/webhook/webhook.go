// Package webhook 的 HTTP Handler 实现。
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/report"
)

// AgentRunner 抽象"把一条 Prompt 喂给 Agent"的能力。
//
// 生产环境由 runner.Runner 实现；测试环境用 fake 替代，以便验证
// Handler 的签名校验 / Schema 解析 / 异步语义，而不启动真实 LLM。
type AgentRunner interface {
	Run(ctx context.Context, userID, sessionID, prompt string) error
}

// AgentRunnerFunc 便利的函数适配器。
type AgentRunnerFunc func(ctx context.Context, userID, sessionID, prompt string) error

// Run 实现 AgentRunner。
func (f AgentRunnerFunc) Run(ctx context.Context, userID, sessionID, prompt string) error {
	return f(ctx, userID, sessionID, prompt)
}

// ReportStore 保存已完成案件的报告；内存实现（见 NewMemStore）足以支撑单实例 Demo。
type ReportStore interface {
	Save(caseID string, r report.Report) error
	Get(caseID string) (report.Report, bool)
}

// Config Handler 可调参数。
type Config struct {
	// Runner Agent 入口（必填）。
	Runner AgentRunner
	// Store 报告存储（必填）。
	Store ReportStore
	// Secret HMAC 签名密钥（可选）。为空或 WEBHOOK_VERIFY_SIG=0 时跳过校验。
	Secret string
	// AsyncTimeout 后台 Agent 执行超时（默认 3 分钟）。
	AsyncTimeout time.Duration
	// Clock 用于测试注入固定时间。
	Clock func() time.Time
	// Logger 日志钩子（失败/告警；生产环境接 log/slog）。
	Logger func(format string, args ...any)
	// Metrics D16：可观测性埋点钩子（source=bk_alarm|tapd，outcome=accepted|rejected|signature_failed|malformed）。
	// 空时 no-op，保持包对 observability 的零依赖。
	Metrics func(source, outcome string)
	// SyncForTest 置 true 时以同步方式执行 Agent（仅单测用）。
	SyncForTest bool
	// Summarizer D16：可选的 Report 总结器；为 nil 时 Outcome 走旧模板文案。
	Summarizer report.SummarizerClient
	// DedupeWindow D16：幂等窗口；>0 时启用（建议 10m），<=0 关闭。
	DedupeWindow time.Duration
}

// Handler Webhook HTTP Handler。
type Handler struct {
	cfg  Config
	dedu *deduper // D16：幂等去重；DedupeWindow<=0 时为 nil
}

// New 创建 Handler。runner/store 任一为空即返回错误。
func New(cfg Config) (*Handler, error) {
	if cfg.Runner == nil {
		return nil, errors.New("webhook: Runner is required")
	}
	if cfg.Store == nil {
		return nil, errors.New("webhook: Store is required")
	}
	if cfg.AsyncTimeout <= 0 {
		cfg.AsyncTimeout = 3 * time.Minute
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string, ...any) {}
	}
	if cfg.Metrics == nil {
		cfg.Metrics = func(string, string) {}
	}
	h := &Handler{cfg: cfg}
	if cfg.DedupeWindow > 0 {
		h.dedu = newDeduper(cfg.DedupeWindow, cfg.Clock)
	}
	return h, nil
}

// Shutdown 释放内部资源（停止幂等 GC goroutine）。多次调用幂等。
func (h *Handler) Shutdown() {
	if h == nil {
		return
	}
	h.dedu.Stop()
}

// Mount 注册 HTTP 路由到给定 mux：
//
//	POST /webhook/bk_alarm
//	POST /webhook/tapd
//	GET  /v1/report/{case_id}?format=markdown|json
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/webhook/bk_alarm", h.handleBKAlarm)
	mux.HandleFunc("/webhook/tapd", h.handleTAPD)
	mux.HandleFunc("/v1/report/", h.handleGetReport)
}

// —— 蓝鲸告警入口 ——

func (h *Handler) handleBKAlarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		h.cfg.Metrics("bk_alarm", "rejected")
		return
	}
	body, ok := h.readAndVerify(w, r)
	if !ok {
		h.cfg.Metrics("bk_alarm", "signature_failed")
		return
	}
	var payload BKAlarmPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid bk_alarm payload: "+err.Error())
		h.cfg.Metrics("bk_alarm", "malformed")
		return
	}
	if strings.TrimSpace(payload.AlarmID) == "" && strings.TrimSpace(payload.Description) == "" && strings.TrimSpace(payload.AlarmName) == "" {
		writeError(w, http.StatusBadRequest, "bk_alarm payload is empty")
		h.cfg.Metrics("bk_alarm", "malformed")
		return
	}
	// D16：幂等命中直接返回已有 caseID，不再分派。
	if existing := h.dedu.Lookup("bk_alarm", bkNaturalKey(payload)); existing != "" {
		h.cfg.Logger("[webhook] bk_alarm deduped case=%s", existing)
		writeAccepted(w, existing)
		h.cfg.Metrics("bk_alarm", "accepted")
		return
	}
	caseID := h.newCaseID("bk", payload.AlarmID)
	title := payload.CaseTitle()
	severity := report.Severity(strings.ToLower(strings.TrimSpace(payload.Severity)))
	bg := h.bkBackground(payload)
	refs := []report.Reference{}
	if payload.DashboardURL != "" {
		refs = append(refs, report.Reference{Kind: "dashboard", Title: payload.AlarmName, URL: payload.DashboardURL})
	}
	h.dispatch(r.Context(), caseID, title, severity, bg, refs, payload.Prompt(), "bk_alarm_webhook")
	h.dedu.Record("bk_alarm", bkNaturalKey(payload), caseID)
	writeAccepted(w, caseID)
	h.cfg.Metrics("bk_alarm", "accepted")
}

func (h *Handler) bkBackground(p BKAlarmPayload) string {
	var sb strings.Builder
	if p.Description != "" {
		sb.WriteString(p.Description)
	}
	if p.Metric != "" {
		fmt.Fprintf(&sb, "\n指标：%s", p.Metric)
		if p.Threshold != 0 {
			fmt.Fprintf(&sb, "，阈值 %.2f", p.Threshold)
		}
		if p.CurrentValue != 0 {
			fmt.Fprintf(&sb, "，当前值 %.2f", p.CurrentValue)
		}
	}
	if p.StartTime != "" {
		fmt.Fprintf(&sb, "\n开始时间：%s", p.StartTime)
	}
	if p.Service != "" || p.Module != "" {
		fmt.Fprintf(&sb, "\n服务：%s/%s", p.Module, p.Service)
	}
	return strings.TrimSpace(sb.String())
}

// —— TAPD 入口 ——

func (h *Handler) handleTAPD(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		h.cfg.Metrics("tapd", "rejected")
		return
	}
	body, ok := h.readAndVerify(w, r)
	if !ok {
		h.cfg.Metrics("tapd", "signature_failed")
		return
	}
	var payload TAPDPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid tapd payload: "+err.Error())
		h.cfg.Metrics("tapd", "malformed")
		return
	}
	if payload.Event == "" {
		writeError(w, http.StatusBadRequest, "tapd payload missing event")
		h.cfg.Metrics("tapd", "malformed")
		return
	}
	// D16：幂等命中直接返回已有 caseID，不再分派。
	if existing := h.dedu.Lookup("tapd", tapdNaturalKey(payload)); existing != "" {
		h.cfg.Logger("[webhook] tapd deduped case=%s", existing)
		writeAccepted(w, existing)
		h.cfg.Metrics("tapd", "accepted")
		return
	}
	bugID := ""
	if payload.Bug != nil {
		bugID = payload.Bug.ID
	}
	caseID := h.newCaseID("tapd", bugID)
	title := payload.CaseTitle()
	severity := report.Severity("")
	if payload.Bug != nil {
		severity = report.Severity(strings.ToLower(strings.TrimSpace(payload.Bug.Severity)))
	}
	bg := h.tapdBackground(payload)
	refs := []report.Reference{}
	if payload.Bug != nil && payload.Bug.URL != "" {
		refs = append(refs, report.Reference{Kind: "tapd", Title: "BUG-" + payload.Bug.ID, URL: payload.Bug.URL})
	}
	h.dispatch(r.Context(), caseID, title, severity, bg, refs, payload.Prompt(), "tapd_webhook")
	h.dedu.Record("tapd", tapdNaturalKey(payload), caseID)
	writeAccepted(w, caseID)
	h.cfg.Metrics("tapd", "accepted")
}

func (h *Handler) tapdBackground(p TAPDPayload) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "事件：%s", p.Event)
	if p.WorkspaceID != "" {
		fmt.Fprintf(&sb, "，项目 %s", p.WorkspaceID)
	}
	if p.Bug != nil {
		if p.Bug.ID != "" {
			fmt.Fprintf(&sb, "\nBug ID：%s", p.Bug.ID)
		}
		if p.Bug.Priority != "" {
			fmt.Fprintf(&sb, "，优先级 %s", p.Bug.Priority)
		}
		if p.Bug.Status != "" {
			fmt.Fprintf(&sb, "，状态 %s", p.Bug.Status)
		}
		if p.Bug.Description != "" {
			fmt.Fprintf(&sb, "\n%s", p.Bug.Description)
		}
	}
	return strings.TrimSpace(sb.String())
}

// —— 报告查询 ——

func (h *Handler) handleGetReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	caseID := strings.TrimPrefix(r.URL.Path, "/v1/report/")
	if caseID == "" || caseID == "/" {
		writeError(w, http.StatusBadRequest, "missing case_id")
		return
	}
	rec, ok := h.cfg.Store.Get(caseID)
	if !ok {
		writeError(w, http.StatusNotFound, "report not found: "+caseID)
		return
	}
	format := report.Format(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))))
	if format == "" {
		format = report.FormatJSON
	}
	body, err := report.Render(rec, format)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch format {
	case report.FormatMarkdown:
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// —— 核心分派 ——

// dispatch 生成初始 Report 骨架入库，并异步触发 Agent。
func (h *Handler) dispatch(
	ctx context.Context,
	caseID, title string,
	severity report.Severity,
	background string,
	refs []report.Reference,
	prompt, source string,
) {
	b := report.NewBuilder(caseID).
		SetTitle(title).
		SetSeverity(severity).
		SetBackground(background).
		AddTimeline(report.TimelineItem{
			TS:      h.cfg.Clock().Format(time.RFC3339),
			Actor:   "system",
			Kind:    "alarm",
			Message: "webhook 接入：source=" + source,
		})
	for _, ref := range refs {
		b.AddReference(ref)
	}
	h.cfg.Store.Save(caseID, b.Build())

	run := func() {
		asyncCtx, cancel := context.WithTimeout(context.Background(), h.cfg.AsyncTimeout)
		defer cancel()
		err := h.cfg.Runner.Run(asyncCtx, "webhook", caseID, prompt)
		if err != nil {
			h.cfg.Logger("[webhook] agent run failed case=%s err=%v", caseID, err)
		}
		final := b.AddTimeline(report.TimelineItem{
			TS:      h.cfg.Clock().Format(time.RFC3339),
			Actor:   "coordinator",
			Kind:    "outcome",
			Message: h.outcomeMessage(err),
		}).Build()
		// D16：成功路径走 Summarizer 生成人话 Outcome；失败路径保留错误原文。
		if err != nil {
			final.Outcome = "Agent 执行失败：" + err.Error()
		} else {
			fallback := "Agent 已完成自动处置，详情见时间轴。"
			final.Outcome = report.SummarizeOrFallback(asyncCtx, h.cfg.Summarizer, final, fallback, h.cfg.Logger)
		}
		h.cfg.Store.Save(caseID, final)
	}
	if h.cfg.SyncForTest {
		run()
		return
	}
	go run()
	_ = ctx
}

func (h *Handler) outcomeMessage(err error) string {
	if err == nil {
		return "Agent 正常收敛"
	}
	return "Agent 执行失败：" + err.Error()
}

// —— 签名校验 & 工具函数 ——

// readAndVerify 读 body 并做 HMAC 校验，失败直接写响应并返回 false。
func (h *Handler) readAndVerify(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MiB 上限
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return nil, false
	}
	defer r.Body.Close()
	if !h.verifyEnabled() {
		return body, true
	}
	sig := r.Header.Get("X-Signature")
	if sig == "" {
		writeError(w, http.StatusUnauthorized, "missing X-Signature header")
		return nil, false
	}
	if !verifyHMACSHA256(h.cfg.Secret, body, sig) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return nil, false
	}
	return body, true
}

// verifyEnabled 决定是否校验签名：
//
//	env WEBHOOK_VERIFY_SIG=0 → 永远放行（demo / 联调）
//	Secret 为空                → 无密钥也无法校验，视为关闭
//	其余情况                   → 校验
func (h *Handler) verifyEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("WEBHOOK_VERIFY_SIG")))
	if v == "0" || v == "false" || v == "off" || v == "no" {
		return false
	}
	return strings.TrimSpace(h.cfg.Secret) != ""
}

// verifyHMACSHA256 校验 "sha256=<hex>" 格式签名；大小写不敏感。
func verifyHMACSHA256(secret string, body []byte, sig string) bool {
	sig = strings.TrimSpace(sig)
	sig = strings.TrimPrefix(sig, "sha256=")
	sig = strings.TrimPrefix(sig, "SHA256=")
	expected, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), expected)
}

// SignHMACSHA256 供测试 / 联调侧生成签名。
func SignHMACSHA256(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) newCaseID(source, id string) string {
	now := h.cfg.Clock()
	base := now.Format("20060102-150405")
	if strings.TrimSpace(id) == "" {
		return fmt.Sprintf("case-%s-%s", source, base)
	}
	return fmt.Sprintf("case-%s-%s-%s", source, id, base)
}

// —— 响应辅助 ——

type acceptedResponse struct {
	Accepted bool   `json:"accepted"`
	CaseID   string `json:"case_id"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeAccepted(w http.ResponseWriter, caseID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(acceptedResponse{Accepted: true, CaseID: caseID})
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: msg})
}

// —— 内存 ReportStore ——

// MemStore 单实例报告内存存储；线程安全。
type MemStore struct {
	mu   sync.RWMutex
	data map[string]report.Report
}

// NewMemStore 创建内存 Store。
func NewMemStore() *MemStore {
	return &MemStore{data: make(map[string]report.Report)}
}

// Save 覆盖式保存；caseID 为空会返回错误。
func (m *MemStore) Save(caseID string, r report.Report) error {
	if strings.TrimSpace(caseID) == "" {
		return errors.New("empty case_id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[caseID] = r
	return nil
}

// Get 拉取报告；未找到时 ok=false。
func (m *MemStore) Get(caseID string) (report.Report, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.data[caseID]
	return r, ok
}

// List 返回所有 CaseID（排序无保证，测试用）。
func (m *MemStore) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.data))
	for k := range m.data {
		out = append(out, k)
	}
	return out
}
