package audit

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer 建立一个可控行为的 httptest server。
// handler 可以断言请求 + 按需返回不同 status code。
func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

// TestNewRemoteSink_URLRequired URL 必填。
func TestNewRemoteSink_URLRequired(t *testing.T) {
	if _, err := NewRemoteSink(RemoteSinkConfig{}); err == nil {
		t.Fatal("缺 URL 应返错")
	}
}

// TestNewRemoteSink_DefaultsFilled 默认值正确回填。
func TestNewRemoteSink_DefaultsFilled(t *testing.T) {
	r, err := NewRemoteSink(RemoteSinkConfig{URL: "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(time.Second)
	if r.cfg.BatchSize != 50 || r.cfg.BufferSize != 10000 ||
		r.cfg.HTTPTimeout != 5*time.Second || r.cfg.MaxRetries != 3 {
		t.Fatalf("默认值回填错误: %+v", r.cfg)
	}
	if r.cfg.ContentType != "application/x-ndjson" {
		t.Fatalf("默认 ContentType 应为 ndjson: %s", r.cfg.ContentType)
	}
}

// TestRemoteSink_HappyPath 完整投递路径：Write 入队 → worker batch → HTTP 200。
func TestRemoteSink_HappyPath(t *testing.T) {
	var received [][]byte
	var mu sync.Mutex
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		if ct := r.Header.Get("Content-Type"); ct != "application/x-ndjson" {
			t.Errorf("Content-Type 错误: %s", ct)
		}
		if a := r.Header.Get("Authorization"); a != "Bearer token-xyz" {
			t.Errorf("Authorization 错误: %s", a)
		}
		if tn := r.Header.Get("X-Tenant"); tn != "gameops" {
			t.Errorf("X-Tenant 错误: %s", tn)
		}
		w.WriteHeader(http.StatusOK)
	})

	r, err := NewRemoteSink(RemoteSinkConfig{
		URL:          srv.URL,
		AuthHeader:   "Bearer token-xyz",
		ExtraHeaders: map[string]string{"X-Tenant": "gameops"},
		BatchSize:    3,
		FlushEvery:   50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 写 3 条 → 触发立即 flush
	for i := 0; i < 3; i++ {
		line := []byte(fmt.Sprintf(`{"ts":"t%d","action":"a"}`+"\n", i))
		if err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}

	// 等 worker flush（ticker 节拍 + batch 条件任一触发）
	waitUntil(t, 2*time.Second, func() bool {
		return r.Stats().Delivered == 3
	})

	if err := r.Close(2 * time.Second); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	st := r.Stats()
	if st.Delivered != 3 || st.Failed != 0 || st.Dropped != 0 {
		t.Fatalf("计数异常: %+v", st)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("server 未收到任何请求")
	}
	// 拼起来应含 3 条 JSON
	joined := string(joinBytes(received))
	for i := 0; i < 3; i++ {
		if !strings.Contains(joined, fmt.Sprintf(`"ts":"t%d"`, i)) {
			t.Fatalf("缺第 %d 条", i)
		}
	}
}

// TestRemoteSink_FlushOnTicker 仅靠 ticker 节拍也能刷出（未达 BatchSize）。
func TestRemoteSink_FlushOnTicker(t *testing.T) {
	var callCnt atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCnt.Add(1)
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	r, _ := NewRemoteSink(RemoteSinkConfig{
		URL:        srv.URL,
		BatchSize:  100, // 故意设大
		FlushEvery: 80 * time.Millisecond,
	})
	defer r.Close(time.Second)

	_ = r.Write([]byte(`{"a":1}` + "\n"))
	_ = r.Write([]byte(`{"a":2}` + "\n"))

	waitUntil(t, time.Second, func() bool {
		return r.Stats().Delivered == 2
	})
	if callCnt.Load() < 1 {
		t.Fatal("ticker 应至少触发一次 flush")
	}
}

// TestRemoteSink_BackpressureDrops buffer 满应丢弃 + 计数。
func TestRemoteSink_BackpressureDrops(t *testing.T) {
	// server 故意阻塞，制造积压
	block := make(chan struct{})
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	})
	r, _ := NewRemoteSink(RemoteSinkConfig{
		URL:        srv.URL,
		BatchSize:  1,
		BufferSize: 4, // 小 buffer 方便触发满
		FlushEvery: time.Hour,
	})

	// 写 20 条，buffer 只有 4 → 至少 ~15 条被 drop
	for i := 0; i < 20; i++ {
		_ = r.Write([]byte(fmt.Sprintf(`{"i":%d}`+"\n", i)))
	}
	close(block)
	_ = r.Close(2 * time.Second)

	st := r.Stats()
	if st.Dropped == 0 {
		t.Fatalf("应有 drop 计数, got %+v", st)
	}
	if st.Enqueued+st.Dropped != 20 {
		t.Fatalf("enqueued+dropped 应 ==20, got %+v", st)
	}
}

// TestRemoteSink_RetryOn5xx 5xx 应重试。
func TestRemoteSink_RetryOn5xx(t *testing.T) {
	var hits atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		n := hits.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	r, _ := NewRemoteSink(RemoteSinkConfig{
		URL:        srv.URL,
		BatchSize:  1,
		MaxRetries: 3,
		FlushEvery: 50 * time.Millisecond,
	})

	_ = r.Write([]byte(`{"ok":1}` + "\n"))
	waitUntil(t, 3*time.Second, func() bool {
		return r.Stats().Delivered == 1
	})
	_ = r.Close(2 * time.Second)

	st := r.Stats()
	if st.Delivered != 1 || st.Failed != 0 {
		t.Fatalf("应重试后成功, got %+v", st)
	}
	if hits.Load() < 3 {
		t.Fatalf("应至少 3 次 hit, got %d", hits.Load())
	}
}

// TestRemoteSink_NoRetryOn4xx 4xx 不应重试。
func TestRemoteSink_NoRetryOn4xx(t *testing.T) {
	var hits atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	})
	r, _ := NewRemoteSink(RemoteSinkConfig{
		URL:        srv.URL,
		BatchSize:  1,
		MaxRetries: 5, // 即便设了 5 次也不应重试
		FlushEvery: 50 * time.Millisecond,
	})

	_ = r.Write([]byte(`{"ok":1}` + "\n"))
	waitUntil(t, time.Second, func() bool {
		return r.Stats().Failed == 1
	})
	_ = r.Close(time.Second)

	if h := hits.Load(); h != 1 {
		t.Fatalf("4xx 应只打一次，实际 %d", h)
	}
	if r.Stats().Failed != 1 {
		t.Fatalf("应 1 条 failed, got %+v", r.Stats())
	}
}

// TestRemoteSink_Retry429 429 应重试（限流常见）。
func TestRemoteSink_Retry429(t *testing.T) {
	var hits atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	r, _ := NewRemoteSink(RemoteSinkConfig{
		URL: srv.URL, BatchSize: 1, MaxRetries: 3,
		FlushEvery: 50 * time.Millisecond,
	})
	_ = r.Write([]byte(`{"x":1}` + "\n"))
	waitUntil(t, 2*time.Second, func() bool {
		return r.Stats().Delivered == 1
	})
	_ = r.Close(time.Second)
	if r.Stats().Delivered != 1 {
		t.Fatalf("429 重试后应成功, got %+v", r.Stats())
	}
}

// TestRemoteSink_WriteAfterClose 关闭后 Write 应 drop 而非 panic。
func TestRemoteSink_WriteAfterClose(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r, _ := NewRemoteSink(RemoteSinkConfig{URL: srv.URL, FlushEvery: 10 * time.Millisecond})
	_ = r.Close(time.Second)
	if err := r.Write([]byte(`{"x":1}` + "\n")); err != nil {
		t.Fatalf("Close 后 Write 不应 err: %v", err)
	}
	if r.Stats().Dropped != 1 {
		t.Fatalf("Close 后 Write 应 drop 计数+1, got %+v", r.Stats())
	}
}

// TestEncodeBatch_NDJSON NDJSON 默认形式。
func TestEncodeBatch_NDJSON(t *testing.T) {
	lines := [][]byte{[]byte(`{"a":1}` + "\n"), []byte(`{"a":2}` + "\n")}
	out := encodeBatch(lines, "application/x-ndjson")
	if !strings.Contains(string(out), `{"a":1}`) || !strings.Contains(string(out), `{"a":2}`) {
		t.Fatalf("NDJSON 输出不对: %s", out)
	}
	if strings.HasPrefix(string(out), "[") {
		t.Fatalf("NDJSON 不应以 [ 开头: %s", out)
	}
}

// TestEncodeBatch_JSONArray application/json 形式打包成数组。
func TestEncodeBatch_JSONArray(t *testing.T) {
	lines := [][]byte{[]byte(`{"a":1}` + "\n"), []byte(`{"a":2}` + "\n")}
	out := encodeBatch(lines, "application/json")
	s := string(out)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		t.Fatalf("JSON array 应以 [] 包裹: %s", s)
	}
	if !strings.Contains(s, `{"a":1},{"a":2}`) {
		t.Fatalf("数组内容不对: %s", s)
	}
}

// TestMultiSink_WritesToAll MultiSink 应同时写到所有子 Sink。
func TestMultiSink_WritesToAll(t *testing.T) {
	a := &MemorySink{}
	b := &MemorySink{}
	m := NewMultiSink(a, nil, b) // nil 应被过滤
	line := []byte(`{"x":1}` + "\n")
	if err := m.Write(line); err != nil {
		t.Fatal(err)
	}
	if len(a.Snapshot()) != 1 || len(b.Snapshot()) != 1 {
		t.Fatal("两个子 Sink 都应收到一行")
	}
}

// TestMultiSink_PartialFailure 一个子 Sink 失败不影响其他。
func TestMultiSink_PartialFailure(t *testing.T) {
	fail := sinkFn(func(line []byte) error { return fmt.Errorf("boom") })
	ok := &MemorySink{}
	m := NewMultiSink(fail, ok)
	err := m.Write([]byte(`{"x":1}` + "\n"))
	if err == nil {
		t.Fatal("应返回第一个非 nil err")
	}
	if len(ok.Snapshot()) != 1 {
		t.Fatal("第二个 Sink 仍应被写入")
	}
}

// TestFileSink_AppendLines 文件 Sink 多次写入应追加。
func TestFileSink_AppendLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	s := NewFileSink(path)
	for _, l := range []string{`{"a":1}` + "\n", `{"a":2}` + "\n"} {
		if err := s.Write([]byte(l)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `{"a":1}`) || !strings.Contains(string(data), `{"a":2}`) {
		t.Fatalf("文件内容不对: %s", data)
	}
}

// TestAuditEmit_WithMultiSink 端到端：Emit → MultiSink(Memory+Remote)
// 验证 D17.3 的完整装配路径。
func TestAuditEmit_WithMultiSink(t *testing.T) {
	var received atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	remote, _ := NewRemoteSink(RemoteSinkConfig{
		URL: srv.URL, BatchSize: 1, FlushEvery: 30 * time.Millisecond,
	})
	mem := &MemorySink{}
	multi := NewMultiSink(mem, remote)

	old := SetSink(multi)
	t.Cleanup(func() { SetSink(old); _ = remote.Close(time.Second) })

	Emit(Event{
		User: "alice", Agent: "repair_agent",
		Action: "gongfeng.mr.merge", Severity: "high",
		Target: "proj!1", Success: true,
	})

	// 本地 mem 应立即有
	if len(mem.Snapshot()) != 1 {
		t.Fatalf("mem 应立即收到 1 条, got %d", len(mem.Snapshot()))
	}
	// 远端等 worker 刷
	waitUntil(t, 2*time.Second, func() bool {
		return remote.Stats().Delivered == 1
	})
	if remote.Stats().Delivered != 1 {
		t.Fatalf("远端应收到 1 条, got %+v", remote.Stats())
	}
	if received.Load() < 1 {
		t.Fatal("httptest server 应至少收到 1 次请求")
	}
}

// ----- helpers -----

// waitUntil 轮询条件，超时失败。
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("wait condition timeout after %s", timeout)
}

// joinBytes 纯工具：拼接多个 []byte。
func joinBytes(parts [][]byte) []byte {
	var n int
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// sinkFn 把函数适配成 Sink。
type sinkFn func([]byte) error

func (f sinkFn) Write(line []byte) error { return f(line) }
