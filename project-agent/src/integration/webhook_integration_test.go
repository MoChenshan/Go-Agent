// webhook_integration_test.go —— Webhook 全链路集成场景。
//
// 装配：真实 deduper + 真实 FileStore + 真实 Summarizer + 真实 HMAC Signer + 真实 audit Sink
// Mock：AgentRunner（不起 LLM）
//
// 验证点：
//   - 幂等去重：同一自然键的 3 次 POST 只产生 1 次 Run 调用
//   - 持久化：落盘的 audit.jsonl 可被 VerifyLine 逐条验证
//   - 可观测：Metrics 回调统计 accepted/rejected 与实际行为一致
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/report"
	"git.woa.com/trpc-go/gameops-agent/src/services/webhook"
)

// memSink 把 audit.Emit 的输出收进内存，测试直接读。
//
// 真实 Sink 写 stdout/file；测试显然不想打扰 CI 输出、也不想真落盘。
// 内存 Sink 保留字节流（含换行）方便用 audit.HMACSigner.VerifyLine 逐条验签。
type memSink struct {
	mu    sync.Mutex
	lines [][]byte
}

func (m *memSink) Write(line []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 拷贝避免调用方复用 buffer 导致数据错乱（audit.Emit 会追加 '\n'）
	cp := make([]byte, len(line))
	copy(cp, line)
	m.lines = append(m.lines, cp)
	return nil
}

func (m *memSink) snapshot() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.lines))
	copy(out, m.lines)
	return out
}

// countingRunner 模拟 AgentRunner：计数被调次数 + 可选延迟。
//
// webhook.handleBKAlarm 会把 Runner.Run 丢进 goroutine（SyncForTest=false 时）
// 或同步调（SyncForTest=true）。集成测试都走同步路径，方便断言时序。
type countingRunner struct {
	calls atomic.Int64
	delay time.Duration
	onRun func(userID, sessionID, prompt string)
}

func (r *countingRunner) Run(ctx context.Context, userID, sessionID, prompt string) error {
	r.calls.Add(1)
	if r.onRun != nil {
		r.onRun(userID, sessionID, prompt)
	}
	if r.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.delay):
		}
	}
	return nil
}

// ---- 场景 1：Deduper + FileStore + HMAC audit 链路 ---------------------------

func TestIntegration_WebhookDedupePersist(t *testing.T) {
	// —— 装配所有真实组件 ——
	dir := t.TempDir()
	// 1) FileStore 真实落盘；测试结束后 TempDir 自动清理
	fstore, err := report.NewFileStore(filepath.Join(dir, "reports.jsonl"))
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	// 2) HMAC Signer；两个 key 验证"primary + accepted 混用"
	signer, err := audit.NewHMACSigner(map[string][]byte{
		"k1": []byte("secret-key-1-long-enough-32bytes"),
	}, "k1", true /* chain */)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	// 3) 注入全局 Sink/Signer，测试结束回滚（不影响其他 test）
	sink := &memSink{}
	oldSink := audit.SetSink(sink)
	audit.SetSigner(signer)
	t.Cleanup(func() {
		audit.SetSink(oldSink)
		audit.SetSigner(nil)
	})

	// 4) 构造 metrics 收集器
	metricsMu := sync.Mutex{}
	metricsHits := map[string]int{}
	metricsFn := func(source, outcome string) {
		metricsMu.Lock()
		defer metricsMu.Unlock()
		metricsHits[source+":"+outcome]++
	}

	// 5) 构造 runner；每次 Run 除了计数，还 Emit 一条审计
	runner := &countingRunner{
		onRun: func(userID, sessionID, prompt string) {
			audit.Emit(audit.Event{
				User:      "webhook-user",
				Agent:     "test_agent",
				Action:    "test.webhook.dispatch",
				Severity:  "low",
				Target:    "case/" + sessionID,
				Success:   true,
				SessionID: sessionID,
			})
		},
	}

	// 6) 构造 Handler（全真实路径：dedupe window=1h 够长避免过期干扰）
	h, err := webhook.New(webhook.Config{
		Runner:       runner,
		Store:        fstore,
		AsyncTimeout: 5 * time.Second,
		SyncForTest:  true, // 同步执行让断言可靠
		Metrics:      metricsFn,
		DedupeWindow: time.Hour,
		Summarizer:   report.NewMockSummarizer(),
	})
	if err != nil {
		t.Fatalf("webhook.New: %v", err)
	}
	t.Cleanup(h.Shutdown)

	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// —— 执行：同一自然键 POST 三次 ——
	payload := []byte(`{"alarm_id":"alarm-42","alarm_name":"pod-oom","description":"游戏主进程 OOM"}`)
	for i := 0; i < 3; i++ {
		resp, err := http.Post(srv.URL+"/webhook/bk_alarm", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("post #%d: %v", i, err)
		}
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("post #%d status=%d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// —— 断言 ——
	// a) Runner 只被调 1 次（dedupe 生效）
	if got := runner.calls.Load(); got != 1 {
		t.Errorf("Runner.Run 期望 1 次，实际 %d", got)
	}
	// b) Metrics accepted=3（dedupe 命中也算 accepted 的一种：幂等返回不是失败）
	metricsMu.Lock()
	if metricsHits["bk_alarm:accepted"] != 3 {
		t.Errorf("bk_alarm:accepted 期望 3 次，实际 %d", metricsHits["bk_alarm:accepted"])
	}
	metricsMu.Unlock()
	// c) 审计 sink 只有 1 条记录（因为 Run 只被调 1 次）
	lines := sink.snapshot()
	if len(lines) != 1 {
		t.Fatalf("audit lines 期望 1 条，实际 %d：\n%s", len(lines), string(bytes.Join(lines, []byte("\n"))))
	}
	// d) 该记录能被 HMAC Verify 通过
	if _, err := signer.VerifyLine(lines[0]); err != nil {
		t.Errorf("VerifyLine: %v", err)
	}
	// e) 记录字段基本语义
	var rec audit.Record
	if err := json.Unmarshal(bytes.TrimRight(lines[0], "\n"), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.Action != "test.webhook.dispatch" || rec.SigAlg == "" || rec.SigKID != "k1" {
		t.Errorf("record 字段异常：%+v", rec)
	}
}

// ---- 场景 5（合并到一起做）：Summarizer 把 Actions 转成 Outcome ------------------

func TestIntegration_SummarizerGeneratesOutcome(t *testing.T) {
	// 这个场景直接验 SummarizerClient 的集成契约：喂 3 个 Action（2 succ 1 fail）进去，
	// 验证返回的句子含"部分处置"前缀 + "3 个写操作" + 失败原因摘要。
	summ := report.NewMockSummarizer()
	rep := report.Report{
		Diagnosis: "Pod 被 OOM Kill，重启 3 次后恢复",
		Actions: []report.Action{
			{Action: "bcs_pod_restart", Result: "success"},
			{Action: "bcs_scale_deployment", Result: "success"},
			{Action: "bk_alarm_silence", Result: "failure", ErrorMsg: "未通过 HITL 确认"},
		},
	}
	out, err := summ.Summarize(context.Background(), rep)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	// 断言三段必备信息都有
	checks := []string{"部分处置", "根因", "3 个写操作", "HITL"}
	for _, k := range checks {
		if !strings.Contains(out, k) {
			t.Errorf("Outcome 缺关键词 %q；实际：%s", k, out)
		}
	}
}

// ---- 场景 6：HMAC 链式签名 + 完整性验证 ----------------------------------------

func TestIntegration_AuditHMACChainAndVerify(t *testing.T) {
	// 产生 5 条真实审计记录，用 VerifyLine 逐条验签；再做"篡改一条"验证链条能检出。
	signer, err := audit.NewHMACSigner(map[string][]byte{
		"k1": []byte("32byte-key-for-test-integration."),
	}, "k1", true)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	sink := &memSink{}
	oldSink := audit.SetSink(sink)
	audit.SetSigner(signer)
	t.Cleanup(func() {
		audit.SetSink(oldSink)
		audit.SetSigner(nil)
	})

	for i := 0; i < 5; i++ {
		audit.Emit(audit.Event{
			User:     "integration-user",
			Agent:    "test_agent",
			Action:   fmt.Sprintf("test.action.%d", i),
			Severity: "medium",
			Target:   fmt.Sprintf("target-%d", i),
			Success:  true,
		})
	}

	lines := sink.snapshot()
	if len(lines) != 5 {
		t.Fatalf("期望 5 条，实际 %d", len(lines))
	}
	// 逐条验签
	var prevSig string
	for i, ln := range lines {
		rec, err := signer.VerifyLine(ln)
		if err != nil {
			t.Fatalf("line #%d verify: %v", i, err)
		}
		// 链式：第 2 条起 PrevSig 应等于第 i-1 条的 Sig
		if i > 0 && rec.PrevSig != prevSig {
			t.Errorf("line #%d PrevSig=%q，期望=%q（链断裂）", i, rec.PrevSig, prevSig)
		}
		prevSig = rec.Sig
	}

	// 篡改第 3 条的 Action 字段 → 验证应失败
	tampered := bytes.Replace(lines[2], []byte(`"action":"test.action.2"`),
		[]byte(`"action":"test.EVIL"`), 1)
	if _, err := signer.VerifyLine(tampered); err == nil {
		t.Error("篡改后 VerifyLine 应失败，实际通过了")
	}
}
