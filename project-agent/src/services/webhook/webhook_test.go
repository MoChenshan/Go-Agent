package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"git.woa.com/trpc-go/gameops-agent/src/report"
)

// ---- fake runner ----

type fakeRunner struct {
	mu       sync.Mutex
	calls    int32
	lastUser string
	lastSess string
	lastMsg  string
	err      error
}

func (f *fakeRunner) Run(_ context.Context, userID, sessionID, prompt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	atomic.AddInt32(&f.calls, 1)
	f.lastUser = userID
	f.lastSess = sessionID
	f.lastMsg = prompt
	return f.err
}

func (f *fakeRunner) Calls() int { return int(atomic.LoadInt32(&f.calls)) }

func newTestHandler(t *testing.T, secret string, runner AgentRunner) (*Handler, *MemStore) {
	t.Helper()
	store := NewMemStore()
	h, err := New(Config{
		Runner:      runner,
		Store:       store,
		Secret:      secret,
		SyncForTest: true,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h, store
}

// 1. 蓝鲸告警正常请求 → 202，case 入库，prompt 喂给 runner
func TestHandler_BKAlarm_OK(t *testing.T) {
	t.Setenv("WEBHOOK_VERIFY_SIG", "0") // 本例关闭签名以简化
	runner := &fakeRunner{}
	h, store := newTestHandler(t, "", runner)

	payload := BKAlarmPayload{
		AlarmID:     "a1",
		AlarmName:   "mem_usage_high",
		Severity:    "high",
		Description: "game-core mem 95%",
		Service:     "game-core",
		Metric:      "memory.usage",
		CurrentValue: 95,
		Threshold:   90,
		StartTime:   "2026-04-21T03:00:00+08:00",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp acceptedResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resp json: %v", err)
	}
	if !resp.Accepted || resp.CaseID == "" {
		t.Fatalf("bad response: %+v", resp)
	}
	if runner.Calls() != 1 {
		t.Errorf("runner calls want=1 got=%d", runner.Calls())
	}
	if !strings.Contains(runner.lastMsg, "game-core mem 95%") {
		t.Errorf("prompt missing description: %q", runner.lastMsg)
	}
	if _, ok := store.Get(resp.CaseID); !ok {
		t.Errorf("report not saved for case=%s", resp.CaseID)
	}
}

// 2. 签名错误 → 401，runner 未被调用
func TestHandler_BKAlarm_BadSignature(t *testing.T) {
	t.Setenv("WEBHOOK_VERIFY_SIG", "1")
	runner := &fakeRunner{}
	h, _ := newTestHandler(t, "my-secret", runner)

	payload := BKAlarmPayload{AlarmID: "a2", AlarmName: "x", Severity: "low", Description: "d"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", bytes.NewReader(body))
	req.Header.Set("X-Signature", "sha256=deadbeef")
	rr := httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
	if runner.Calls() != 0 {
		t.Errorf("runner must not be called, got %d", runner.Calls())
	}
}

// 3. 签名正确 → 202
func TestHandler_BKAlarm_SignatureOK(t *testing.T) {
	t.Setenv("WEBHOOK_VERIFY_SIG", "1")
	runner := &fakeRunner{}
	secret := "s3cret"
	h, _ := newTestHandler(t, secret, runner)

	payload := BKAlarmPayload{AlarmID: "a3", AlarmName: "x", Severity: "low", Description: "d"}
	body, _ := json.Marshal(payload)
	sig := SignHMACSHA256(secret, body)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)
	rr := httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("want 202 got %d body=%s", rr.Code, rr.Body.String())
	}
}

// 4. 非法 JSON → 400
func TestHandler_InvalidJSON(t *testing.T) {
	t.Setenv("WEBHOOK_VERIFY_SIG", "0")
	h, _ := newTestHandler(t, "", &fakeRunner{})

	req := httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", strings.NewReader("{not json"))
	rr := httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
}

// 5. GET /method 不允许
func TestHandler_MethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t, "", &fakeRunner{})
	req := httptest.NewRequest(http.MethodGet, "/webhook/bk_alarm", nil)
	rr := httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rr.Code)
	}
}

// 6. TAPD 链路：事件为空 → 400；正常事件 → 202 且 prompt 含 title
func TestHandler_TAPD(t *testing.T) {
	t.Setenv("WEBHOOK_VERIFY_SIG", "0")
	runner := &fakeRunner{}
	h, store := newTestHandler(t, "", runner)

	// 空事件
	empty, _ := json.Marshal(TAPDPayload{})
	rr := httptest.NewRecorder()
	h.handleTAPD(rr, httptest.NewRequest(http.MethodPost, "/webhook/tapd", bytes.NewReader(empty)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty event, got %d", rr.Code)
	}

	// 正常事件
	payload := TAPDPayload{
		Event:       "bug_create",
		WorkspaceID: "42",
		Bug: &TAPDBug{
			ID: "B-1", Title: "Pod OOMKilled",
			Severity: "high", Priority: "P1", Status: "new",
			URL: "https://tapd.example.com/bugs/B-1",
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook/tapd", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	h.handleTAPD(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp acceptedResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !strings.Contains(runner.lastMsg, "Pod OOMKilled") {
		t.Errorf("prompt missing bug title: %q", runner.lastMsg)
	}
	rec, ok := store.Get(resp.CaseID)
	if !ok {
		t.Fatalf("report not saved")
	}
	if len(rec.References) == 0 || rec.References[0].Kind != "tapd" {
		t.Errorf("tapd ref not attached: %+v", rec.References)
	}
}

// 7. GET /v1/report/{id} 返回 JSON / Markdown
func TestHandler_GetReport(t *testing.T) {
	h, store := newTestHandler(t, "", &fakeRunner{})
	caseID := "case-x-123"
	r := report.NewBuilder(caseID).SetTitle("T").Build()
	_ = store.Save(caseID, r)

	// JSON
	rr := httptest.NewRecorder()
	h.handleGetReport(rr, httptest.NewRequest(http.MethodGet, "/v1/report/"+caseID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
		t.Errorf("want json content-type, got %s", rr.Header().Get("Content-Type"))
	}
	var parsed report.Report
	if err := json.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.CaseID != caseID {
		t.Errorf("case_id mismatch: %s", parsed.CaseID)
	}

	// Markdown
	rr = httptest.NewRecorder()
	h.handleGetReport(rr, httptest.NewRequest(http.MethodGet, "/v1/report/"+caseID+"?format=markdown", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 md, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/markdown") {
		t.Errorf("want markdown content-type, got %s", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "# 修复报告") {
		t.Errorf("md body missing header: %s", rr.Body.String())
	}

	// 404
	rr = httptest.NewRecorder()
	h.handleGetReport(rr, httptest.NewRequest(http.MethodGet, "/v1/report/not-exist", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rr.Code)
	}
}

// 8. Agent 失败应写入 outcome 并保留 case（不回滚）
func TestHandler_AgentFailure_OutcomePersisted(t *testing.T) {
	t.Setenv("WEBHOOK_VERIFY_SIG", "0")
	runner := &fakeRunner{err: fmt.Errorf("llm down")}
	h, store := newTestHandler(t, "", runner)

	body, _ := json.Marshal(BKAlarmPayload{AlarmID: "a-fail", AlarmName: "x", Description: "d"})
	req := httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", rr.Code)
	}
	var resp acceptedResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	rec, ok := store.Get(resp.CaseID)
	if !ok {
		t.Fatalf("report should still be saved on failure")
	}
	if !strings.Contains(rec.Outcome, "llm down") {
		t.Errorf("outcome should include error: %s", rec.Outcome)
	}
	// 应至少有 2 条 timeline：alarm + outcome
	if len(rec.Timeline) < 2 {
		t.Errorf("timeline want>=2, got %d: %+v", len(rec.Timeline), rec.Timeline)
	}
}

// 9. Mount 挂载后，路径 400（method 走 mux），签名关闭下正常返回 202
func TestHandler_Mount(t *testing.T) {
	t.Setenv("WEBHOOK_VERIFY_SIG", "0")
	h, _ := newTestHandler(t, "", &fakeRunner{})
	mux := http.NewServeMux()
	h.Mount(mux)
	body, _ := json.Marshal(BKAlarmPayload{AlarmID: "a-m", Description: "d"})
	req := httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// 10. verifyHMACSHA256 单测（大小写前缀鲁棒）
func TestVerifyHMACSHA256(t *testing.T) {
	secret := "k"
	body := []byte("payload")
	sig := SignHMACSHA256(secret, body)
	if !verifyHMACSHA256(secret, body, sig) {
		t.Errorf("valid sig should pass")
	}
	// 大写前缀
	if !verifyHMACSHA256(secret, body, strings.ToUpper(sig[:len("sha256=")])+sig[len("sha256="):]) {
		t.Errorf("uppercase prefix should pass")
	}
	// 错 secret
	if verifyHMACSHA256("wrong", body, sig) {
		t.Errorf("wrong secret must not pass")
	}
	// 非法 hex
	if verifyHMACSHA256(secret, body, "sha256=zzzz") {
		t.Errorf("invalid hex must not pass")
	}
}

// 11. Metrics 钩子：accepted / malformed / rejected / signature_failed 四种 outcome 均应触发
func TestHandler_MetricsHook(t *testing.T) {
	t.Setenv("WEBHOOK_VERIFY_SIG", "1")
	var mu sync.Mutex
	hits := map[string]int{}
	record := func(source, outcome string) {
		mu.Lock()
		defer mu.Unlock()
		hits[source+":"+outcome]++
	}
	store := NewMemStore()
	h, err := New(Config{
		Runner:      &fakeRunner{},
		Store:       store,
		Secret:      "s3cret",
		SyncForTest: true,
		Metrics:     record,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	// accepted（带正确签名）
	body, _ := json.Marshal(BKAlarmPayload{AlarmID: "a1", Description: "d"})
	req := httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", bytes.NewReader(body))
	req.Header.Set("X-Signature", SignHMACSHA256("s3cret", body))
	rr := httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("accepted path want 202 got %d", rr.Code)
	}

	// malformed（坏 JSON，带正确签名）
	badBody := []byte("{not json")
	req = httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", bytes.NewReader(badBody))
	req.Header.Set("X-Signature", SignHMACSHA256("s3cret", badBody))
	rr = httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed path want 400 got %d", rr.Code)
	}

	// signature_failed（错签名）
	body2, _ := json.Marshal(BKAlarmPayload{AlarmID: "a2", Description: "d"})
	req = httptest.NewRequest(http.MethodPost, "/webhook/bk_alarm", bytes.NewReader(body2))
	req.Header.Set("X-Signature", "sha256=deadbeef")
	rr = httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("bad sig path want 401 got %d", rr.Code)
	}

	// rejected（GET 被拒）
	req = httptest.NewRequest(http.MethodGet, "/webhook/bk_alarm", nil)
	rr = httptest.NewRecorder()
	h.handleBKAlarm(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("rejected path want 405 got %d", rr.Code)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, key := range []string{
		"bk_alarm:accepted",
		"bk_alarm:malformed",
		"bk_alarm:signature_failed",
		"bk_alarm:rejected",
	} {
		if hits[key] == 0 {
			t.Errorf("metrics hook did not fire for %q; all hits=%+v", key, hits)
		}
	}
}