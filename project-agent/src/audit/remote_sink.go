// remote_sink.go D17.3 审计日志远端汇聚 Sink。
//
// 定位：把本地 audit.log 的 JSONL 同步推给远端聚合网关
// （Loki HTTP API / Vector HTTP source / Fluent Bit http input / Kafka REST proxy
// 等等），让多个 agent 副本的审计日志集中到一个可查询后端。
//
// 设计原则
//  1. **严格零新依赖**：只用 net/http + encoding/json stdlib。SRE 要什么聚合器自己选，
//     agent 进程不应该被绑定到 Kafka client / ES client 这类重型库。
//  2. **非阻塞**：Emit 路径是同步调用，绝不能阻塞业务主流程。所有远端 I/O 跑在后台 goroutine，
//     Write 只往 bounded channel 丢。
//  3. **本地永远是 source of truth**：远端 Sink 必须和本地文件 Sink 组合（MultiSink）使用；
//     远端挂了/满了/429 了，本地记录照常落；后续离线批量回灌即可。
//  4. **背压丢新 + 显式计数**：channel 满就丢新增日志并 atomic 累加 dropped 计数（暴露出来便于告警）。
//     这比阻塞 Emit 或阻塞主流程要好得多 —— 本地文件已有，远端 best-effort 即可。
//  5. **优雅关闭**：Close 阻塞等 flush 完 + close channel + 等 worker 退出（带超时），让容器 shutdown
//     时不会大面积丢日志。
//
// 用法（配合 MultiSink）：
//
//	remote, _ := audit.NewRemoteSink(audit.RemoteSinkConfig{
//	    URL:        "https://audit-gw.example.com/ingest",
//	    AuthHeader: "Bearer xxx",
//	    BatchSize:  50,
//	    FlushEvery: 2 * time.Second,
//	    BufferSize: 10000,
//	})
//	audit.SetSink(audit.NewMultiSink(audit.DefaultFileSink(), remote))
//	// 程序退出前：
//	_ = remote.Close(5 * time.Second)
package audit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RemoteSinkConfig RemoteSink 配置。
//
// 所有字段都有合理默认，调用方通常只需指定 URL。
type RemoteSinkConfig struct {
	// URL 远端聚合网关 endpoint（POST JSON）。必填。
	URL string
	// AuthHeader 完整 Authorization 头值（如 "Bearer xxx"），可选。
	AuthHeader string
	// ExtraHeaders 额外 HTTP 头（如 X-Tenant: gameops），可选。
	ExtraHeaders map[string]string

	// BatchSize 单次 HTTP 请求最多打包多少条；默认 50。
	// 大了省带宽，小了降延迟，50 对 audit 这种低 QPS 够了。
	BatchSize int
	// FlushEvery 最长等多久就强刷一次（即使没攒满 BatchSize）；默认 2s。
	FlushEvery time.Duration
	// BufferSize 内存 channel 容量；默认 10000 条。
	// 以 JSON 每条 ~500B 估算，10000 条约 5MB，可以容忍远端几分钟内不可达。
	BufferSize int

	// HTTPTimeout 单次 HTTP 请求超时；默认 5s。
	HTTPTimeout time.Duration
	// MaxRetries 5xx/网络错误的重试次数（指数退避）；默认 3。
	// 4xx 不重试（错误请求重试只会持续失败）。
	MaxRetries int

	// Client 可注入自定义 http.Client（测试用）；空则用内置。
	Client *http.Client
	// ErrorLogger 可选错误回调；空则写 stderr。
	ErrorLogger func(format string, args ...any)

	// ContentType 默认 "application/x-ndjson"（NDJSON batch）。
	// 若远端要求 application/json（数组形式），设为该值。
	ContentType string
}

// RemoteSink 远端汇聚 Sink（goroutine 安全、非阻塞）。
type RemoteSink struct {
	cfg    RemoteSinkConfig
	ch     chan []byte
	stopCh chan struct{}
	wg     sync.WaitGroup

	// 可观测计数
	enqueued  atomic.Int64
	dropped   atomic.Int64 // buffer 满丢弃
	delivered atomic.Int64 // 成功 POST 的条数
	failed    atomic.Int64 // 最终失败丢弃的条数（重试耗尽）

	closed atomic.Bool
}

// NewRemoteSink 构造 + 启动后台 worker。URL 必填。
func NewRemoteSink(cfg RemoteSinkConfig) (*RemoteSink, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("RemoteSink: URL is required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = 2 * time.Second
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 10000
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 5 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	if strings.TrimSpace(cfg.ContentType) == "" {
		cfg.ContentType = "application/x-ndjson"
	}

	r := &RemoteSink{
		cfg:    cfg,
		ch:     make(chan []byte, cfg.BufferSize),
		stopCh: make(chan struct{}),
	}
	r.wg.Add(1)
	go r.runWorker()
	return r, nil
}

// Write 实现 Sink 接口。非阻塞：channel 满直接丢弃 + 计数。
//
// 返回值约定：保持和 defaultSink 一致 —— 永远返回 nil（丢弃只记计数不上报 error，
// 避免把 Emit 的 stderr 刷爆）。远端递送健康状况通过 Stats() 暴露。
func (r *RemoteSink) Write(line []byte) error {
	if r.closed.Load() {
		r.dropped.Add(1)
		return nil
	}
	// 复制一份，避免调用方复用 buffer 带来的 race。
	cp := make([]byte, len(line))
	copy(cp, line)
	select {
	case r.ch <- cp:
		r.enqueued.Add(1)
	default:
		r.dropped.Add(1)
	}
	return nil
}

// Stats 导出可观测计数，便于暴露给 /metrics 或日志。
func (r *RemoteSink) Stats() RemoteSinkStats {
	return RemoteSinkStats{
		Enqueued:  r.enqueued.Load(),
		Dropped:   r.dropped.Load(),
		Delivered: r.delivered.Load(),
		Failed:    r.failed.Load(),
	}
}

// SnapshotStats 与 observability.RemoteSinkStatsProvider 接口对齐，
// 便于 OTel MetricsPump 零反射地读取四元组快照。
// 这里通过"方法而非接口断言"的方式解耦：audit 包不依赖 observability 包，
// observability 包也不依赖 audit 包，保持单向依赖。
func (r *RemoteSink) SnapshotStats() (enqueued, delivered, dropped, failed int64) {
	return r.enqueued.Load(), r.delivered.Load(), r.dropped.Load(), r.failed.Load()
}

// RemoteSinkStats 计数快照。
type RemoteSinkStats struct {
	Enqueued  int64
	Dropped   int64
	Delivered int64
	Failed    int64
}

// Close 停止 worker，阻塞直到 flush 完或超时。
//
// 典型调用：main 函数的 defer remote.Close(5*time.Second)。
func (r *RemoteSink) Close(timeout time.Duration) error {
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(r.stopCh)
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("RemoteSink.Close: timeout after %s; stats=%+v",
			timeout, r.Stats())
	}
}

// runWorker 后台 goroutine：按 BatchSize 或 FlushEvery 触发刷新。
func (r *RemoteSink) runWorker() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.cfg.FlushEvery)
	defer ticker.Stop()

	buf := make([][]byte, 0, r.cfg.BatchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		r.deliver(buf)
		// 清空但保留容量
		buf = buf[:0]
	}

	for {
		select {
		case <-r.stopCh:
			// 排空 channel（已写入的尽力投递）。
			for {
				select {
				case line := <-r.ch:
					buf = append(buf, line)
					if len(buf) >= r.cfg.BatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case line := <-r.ch:
			buf = append(buf, line)
			if len(buf) >= r.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// deliver 执行一次 HTTP POST，带指数退避重试。
//
// 失败策略：
//   - 网络错误 / 5xx  → 重试（上限 MaxRetries）
//   - 4xx           → 不重试（请求本身有问题，重试也是徒劳）
//   - 全部耗尽       → failed 计数 + err 日志；不再留用 —— 本地文件还在，走离线回灌。
func (r *RemoteSink) deliver(batch [][]byte) {
	body := encodeBatch(batch, r.cfg.ContentType)
	n := int64(len(batch))

	var lastErr error
	backoff := 200 * time.Millisecond
	for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
		retriable, err := r.doPost(body)
		if err == nil {
			r.delivered.Add(n)
			return
		}
		lastErr = err
		if !retriable {
			break
		}
		if attempt < r.cfg.MaxRetries {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	r.failed.Add(n)
	r.logf("[audit-remote] deliver %d records failed: %v", n, lastErr)
}

// doPost 单次 POST。返回 (retriable, err)。
func (r *RemoteSink) doPost(body []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.HTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", r.cfg.ContentType)
	if r.cfg.AuthHeader != "" {
		req.Header.Set("Authorization", r.cfg.AuthHeader)
	}
	for k, v := range r.cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := r.cfg.Client.Do(req)
	if err != nil {
		return true, err // 网络错误一律可重试
	}
	defer resp.Body.Close()
	// 读掉 body，避免 keep-alive 连接被污染
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}
	// 5xx 可重试；4xx 不可重试（请求本身有问题）。
	retriable := resp.StatusCode >= 500 || resp.StatusCode == 429
	return retriable, fmt.Errorf("http %d", resp.StatusCode)
}

// encodeBatch 根据 ContentType 决定是 NDJSON 还是 JSON 数组。
//
// NDJSON（默认）：每行一个对象，天然匹配 Loki / Vector / Fluent Bit；
// JSON array：`[{...},{...}]`，匹配某些"批量 ingest"风格的 API。
func encodeBatch(lines [][]byte, contentType string) []byte {
	if strings.HasPrefix(contentType, "application/json") &&
		!strings.Contains(contentType, "ndjson") {
		// JSON array 形式：把每行原 JSON 串起来
		var b bytes.Buffer
		b.WriteByte('[')
		for i, line := range lines {
			if i > 0 {
				b.WriteByte(',')
			}
			// 每行末尾已含 '\n'，去掉避免破坏 JSON
			trimmed := bytes.TrimRight(line, "\n")
			b.Write(trimmed)
		}
		b.WriteByte(']')
		return b.Bytes()
	}
	// NDJSON 默认：直接拼
	var b bytes.Buffer
	b.Grow(estimateSize(lines))
	for _, line := range lines {
		b.Write(line)
		if len(line) > 0 && line[len(line)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}

func estimateSize(lines [][]byte) int {
	n := 0
	for _, l := range lines {
		n += len(l) + 1
	}
	return n
}

// logf 内部错误输出。
func (r *RemoteSink) logf(format string, args ...any) {
	if r.cfg.ErrorLogger != nil {
		r.cfg.ErrorLogger(format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// ----- MultiSink -----

// MultiSink 把一条日志同时写入多个 Sink。
//
// 使用场景：本地文件 + 远端网关；任一子 Sink 失败不影响其他子 Sink。
type MultiSink struct {
	sinks []Sink
}

// NewMultiSink 构造。nil sub-sink 会被过滤。
func NewMultiSink(sinks ...Sink) *MultiSink {
	clean := make([]Sink, 0, len(sinks))
	for _, s := range sinks {
		if s != nil {
			clean = append(clean, s)
		}
	}
	return &MultiSink{sinks: clean}
}

// Write 实现 Sink 接口。返回**第一个**非 nil err（其他子 Sink 仍会被尝试）。
func (m *MultiSink) Write(line []byte) error {
	var firstErr error
	for _, s := range m.sinks {
		if err := s.Write(line); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ----- 便捷构造 -----

// FileSink 纯文件 Sink（不含 stdout 分流），显式指定路径。
//
// 与 defaultSink 的区别：defaultSink 靠 AUDIT_SINK/AUDIT_FILE 环境变量动态决定行为；
// FileSink 是显式路径，便于在代码里组合进 MultiSink。
type FileSink struct {
	Path string
	mu   sync.Mutex
}

// NewFileSink 构造。
func NewFileSink(path string) *FileSink {
	return &FileSink{Path: path}
}

// Write 实现 Sink 接口。每次调用 open/close，牺牲一点性能换"不吃住 fd、rotate 友好"。
func (f *FileSink) Write(line []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fd, err := os.OpenFile(f.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fd.Close()
	_, err = fd.Write(line)
	return err
}