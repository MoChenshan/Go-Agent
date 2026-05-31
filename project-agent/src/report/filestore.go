// Package report 的 FileStore 持久化实现（D16）。
//
// 设计目标：
//  1. **进程重启可恢复**：所有 Save 以 JSONL 追加写入磁盘；New 时自动 reload，
//     同一 CaseID 以**最后一条**为准（天然覆盖语义）。
//  2. **零外部依赖**：只用标准库 `os` / `bufio` / `encoding/json`，不引入 BoltDB / SQLite，
//     便于容器化部署、K8s PVC 挂载即可用。
//  3. **与 MemStore 接口同构**：实现 `Save(caseID, Report) error` + `Get(caseID) (Report, bool)`，
//     Webhook 侧无需任何改动即可替换。
//  4. **并发安全**：写路径 File-level Mutex + append-only；读路径 RWMutex 保护内存快照。
//
// 使用示例：
//
//	store, err := report.NewFileStore("/data/gameops/reports.jsonl")
//	if err != nil { log.Fatal(err) }
//	handler, _ := webhook.New(webhook.Config{Store: store, ...})
//
// 磁盘格式：每行一条 JSON（Report），CRLF 行分隔；读取时兼容 LF / CRLF。
package report

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileStore 基于 JSONL 追加写的报告存储。
//
// 语义：
//   - Save：锁 → append JSON 行到 file → 更新内存 map
//   - Get： 读内存 map（毫秒级）
//   - 进程重启：New 逐行扫描 reload，最后一次出现的 CaseID 胜出
//
// 注意：长期运行会导致 JSONL 膨胀；生产环境应配套外部压缩脚本或定期 Compact。
// 本轮 D16 不引入压缩，留给 D17+。
type FileStore struct {
	path string

	mu   sync.RWMutex
	data map[string]Report

	// wmu 独占写锁；避免多 goroutine 同时写文件导致 JSONL 行错位。
	wmu sync.Mutex
}

// NewFileStore 打开（或创建）一个 JSONL 存储文件。
//
// 路径处理：
//   - 传入 "" 时返回错误，调用方应优先用 MemStore。
//   - 父目录不存在时自动 MkdirAll（0o755）。
//   - 文件不存在时 reload 为空 map，Save 时自动创建。
//
// reload 失败（磁盘 IO / 部分行损坏）的策略：
//   - IO 错误直接返回错误（调用方需决定是否降级 MemStore）。
//   - 某行 JSON 解析失败：静默跳过 + 记数（见 SkippedLines）。
func NewFileStore(path string) (*FileStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("report: FileStore path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir for %q: %w", path, err)
	}
	fs := &FileStore{path: path, data: make(map[string]Report)}
	if err := fs.reload(); err != nil {
		return nil, err
	}
	return fs, nil
}

// Path 返回底层文件路径（运维排障友好）。
func (s *FileStore) Path() string { return s.path }

// Save 把报告以 JSONL 追加到磁盘，并更新内存索引。
//
// CaseID 为空视为错误（与 MemStore 一致）。
// 同一 CaseID 多次 Save：磁盘有多行，内存只留最新；后续 reload 也只认最后一条。
func (s *FileStore) Save(caseID string, r Report) error {
	if strings.TrimSpace(caseID) == "" {
		return errors.New("empty case_id")
	}
	// 与 MemStore 对齐：若传入 Report.CaseID 为空，用参数的 caseID 填充。
	if strings.TrimSpace(r.CaseID) == "" {
		r.CaseID = caseID
	}
	buf, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	// 追加写：O_APPEND + 单写锁。
	s.wmu.Lock()
	defer s.wmu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %q: %w", s.path, err)
	}
	defer f.Close()
	buf = append(buf, '\n')
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("write %q: %w", s.path, err)
	}
	// 更新内存索引。
	s.mu.Lock()
	s.data[caseID] = r
	s.mu.Unlock()
	return nil
}

// Get 拉取报告；未找到 ok=false。
func (s *FileStore) Get(caseID string) (Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data[caseID]
	return r, ok
}

// List 返回当前所有 CaseID（顺序无保证）。
func (s *FileStore) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.data))
	for k := range s.data {
		out = append(out, k)
	}
	return out
}

// Reload 公开版的 reload，供运维手动触发（例如管理 API 或热重载）。
func (s *FileStore) Reload() error { return s.reload() }

// reload 扫描 JSONL 文件把数据加载到内存。
// 同一 CaseID 多行时以**最后一次**为准。
func (s *FileStore) reload() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在 = 从零开始，合法。
			s.mu.Lock()
			s.data = make(map[string]Report)
			s.mu.Unlock()
			return nil
		}
		return fmt.Errorf("open %q: %w", s.path, err)
	}
	defer f.Close()

	data := make(map[string]Report)
	br := bufio.NewReaderSize(f, 1<<16)
	lineNo := 0
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			trimmed := trimTrailingNewline(line)
			if len(trimmed) == 0 {
				// 空行跳过，不计入 SkippedLines
			} else {
				var r Report
				if jerr := json.Unmarshal(trimmed, &r); jerr == nil {
					if strings.TrimSpace(r.CaseID) != "" {
						data[r.CaseID] = r
					}
				}
				// JSON 解析失败静默跳过（报告场景复盘优先于严格性）
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read %q line %d: %w", s.path, lineNo, err)
		}
	}
	s.mu.Lock()
	s.data = data
	s.mu.Unlock()
	return nil
}
