// judge_prompt_store_test.go D17.2.1 — Prompt YAML 热加载全链路单测。
//
// 测试面覆盖：
//  1. Snapshot IsEmpty / nil-safe
//  2. Store Get/Replace 原子性 + 并发
//  3. YAML Loader happy / 各种错误路径
//  4. Watcher 初次加载 / 热 reload / 解析失败不清空 / 未变化 short-circuit / Stop 幂等
//  5. LLMJudge × PromptStore 端到端：Score 真的用了新 prompt（复用 fakeJudgeModel.lastReq）
package eval

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"context"
)

// ---------------- Snapshot & Store ----------------

// TestJudgePromptSnapshot_IsEmpty 覆盖 nil / 零值 / 非空。
func TestJudgePromptSnapshot_IsEmpty(t *testing.T) {
	var nilSnap *JudgePromptSnapshot
	if !nilSnap.IsEmpty() {
		t.Fatal("nil snapshot 应视为 empty")
	}
	if !(&JudgePromptSnapshot{}).IsEmpty() {
		t.Fatal("零值 snapshot 应 empty")
	}
	if (&JudgePromptSnapshot{SystemPrompt: "hi"}).IsEmpty() {
		t.Fatal("含 SystemPrompt 不应 empty")
	}
	if (&JudgePromptSnapshot{Dimensions: []JudgeDimension{{Name: "x"}}}).IsEmpty() {
		t.Fatal("含 Dimensions 不应 empty")
	}
}

// TestJudgePromptStore_NilSafe nil store 的所有方法都不应 panic。
func TestJudgePromptStore_NilSafe(t *testing.T) {
	var s *JudgePromptStore
	if got := s.Get(); got != nil {
		t.Fatalf("nil store Get 应 nil, got %+v", got)
	}
	s.Replace(&JudgePromptSnapshot{SystemPrompt: "x"}) // 静默 no-op
}

// TestJudgePromptStore_GetReplace 基本读写原子。
func TestJudgePromptStore_GetReplace(t *testing.T) {
	s := NewJudgePromptStore()
	if snap := s.Get(); snap != nil {
		t.Fatalf("初始 Get 应 nil, got %+v", snap)
	}
	s.Replace(&JudgePromptSnapshot{SystemPrompt: "v1"})
	if snap := s.Get(); snap == nil || snap.SystemPrompt != "v1" {
		t.Fatalf("Replace 后 Get 应 v1, got %+v", snap)
	}
	s.Replace(&JudgePromptSnapshot{SystemPrompt: "v2"})
	if snap := s.Get(); snap.SystemPrompt != "v2" {
		t.Fatalf("第二次 Replace 后应 v2, got %+v", snap)
	}
	s.Replace(nil)
	if snap := s.Get(); snap != nil {
		t.Fatalf("Replace(nil) 后应 nil, got %+v", snap)
	}
}

// TestJudgePromptStore_Concurrent 并发 Get/Replace 不应 race / panic。
func TestJudgePromptStore_Concurrent(t *testing.T) {
	s := NewJudgePromptStore()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					s.Replace(&JudgePromptSnapshot{
						SystemPrompt: "w",
						Dimensions:   []JudgeDimension{{Name: "d", Threshold: float64(id)}},
					})
				}
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = s.Get()
				}
			}
		}()
	}
	time.Sleep(80 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// ---------------- YAML Loader ----------------

// TestParseJudgePromptYAML_Happy 常规解析。
func TestParseJudgePromptYAML_Happy(t *testing.T) {
	data := []byte(`version: v1.1
system_prompt: |
  你是评审员
dimensions:
  - name: Acc
    threshold: 0.9
    criterion: 根因准确
  - name: Ev
    threshold: 0.7
    criterion: 证据充分`)
	snap, err := parseJudgePromptYAML(data)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if snap.Version != "v1.1" {
		t.Fatalf("version 不对: %q", snap.Version)
	}
	if !strings.Contains(snap.SystemPrompt, "评审员") {
		t.Fatalf("SystemPrompt 不对: %q", snap.SystemPrompt)
	}
	if len(snap.Dimensions) != 2 {
		t.Fatalf("dims len 不对: %d", len(snap.Dimensions))
	}
	if snap.Dimensions[0].Name != "Acc" || snap.Dimensions[0].Threshold != 0.9 {
		t.Fatalf("dim[0] 不对: %+v", snap.Dimensions[0])
	}
}

// TestParseJudgePromptYAML_ThresholdClamp 越界 threshold 应夹到 [0,1]。
func TestParseJudgePromptYAML_ThresholdClamp(t *testing.T) {
	data := []byte(`dimensions:
  - name: A
    threshold: 2.5
  - name: B
    threshold: -0.3`)
	snap, err := parseJudgePromptYAML(data)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if snap.Dimensions[0].Threshold != 1 {
		t.Fatalf("A threshold 应夹到 1, got %v", snap.Dimensions[0].Threshold)
	}
	if snap.Dimensions[1].Threshold != 0 {
		t.Fatalf("B threshold 应夹到 0, got %v", snap.Dimensions[1].Threshold)
	}
}

// TestParseJudgePromptYAML_ErrorPaths 各类错误都应返 err。
func TestParseJudgePromptYAML_ErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"bad yaml", "::: not yaml :::"},
		{"empty", ""},
		{"missing name", "dimensions:\n  - threshold: 0.5\n"},
		{"dup name", "dimensions:\n  - name: A\n    threshold: 0.5\n  - name: A\n    threshold: 0.6\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseJudgePromptYAML([]byte(tc.data)); err == nil {
				t.Fatal("应返错")
			}
		})
	}
}

// TestLoadJudgePromptFromFile_NotFound 空路径 / 不存在文件。
func TestLoadJudgePromptFromFile_NotFound(t *testing.T) {
	if _, err := LoadJudgePromptFromFile(""); err == nil {
		t.Fatal("空路径应返错")
	}
	if _, err := LoadJudgePromptFromFile(filepath.Join(t.TempDir(), "no-such.yaml")); err == nil {
		t.Fatal("不存在文件应返错")
	}
}

// TestLoadJudgePromptFromFile_Happy 真实文件加载。
func TestLoadJudgePromptFromFile_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.yaml")
	if err := os.WriteFile(path,
		[]byte("system_prompt: hello\ndimensions:\n  - name: X\n    threshold: 0.5\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadJudgePromptFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if snap.SystemPrompt != "hello" || len(snap.Dimensions) != 1 {
		t.Fatalf("不对: %+v", snap)
	}
}

// ---------------- Watcher ----------------

// TestJudgePromptWatcher_EmptyPath 空路径 Start 应 no-op。
func TestJudgePromptWatcher_EmptyPath(t *testing.T) {
	store := NewJudgePromptStore()
	w := NewJudgePromptWatcher(JudgePromptWatcherConfig{Store: store})
	w.Start()
	w.Stop()
	if store.Get() != nil {
		t.Fatal("空路径不应改 store")
	}
}

// TestJudgePromptWatcher_InitialLoad Start 时立即同步加载。
func TestJudgePromptWatcher_InitialLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(path,
		[]byte("system_prompt: v1\ndimensions:\n  - name: A\n    threshold: 0.5\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	store := NewJudgePromptStore()
	w := NewJudgePromptWatcher(JudgePromptWatcherConfig{
		Path: path, Store: store, Interval: time.Hour,
	})
	w.Start()
	defer w.Stop()
	snap := store.Get()
	if snap == nil || snap.SystemPrompt != "v1" {
		t.Fatalf("首次加载应立即填 store, got %+v", snap)
	}
	if w.Reloads() != 1 {
		t.Fatalf("reload 计数应 1, got %d", w.Reloads())
	}
}

// TestJudgePromptWatcher_HotReload 改文件后应 reload。
func TestJudgePromptWatcher_HotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(path,
		[]byte("system_prompt: old\ndimensions:\n  - name: A\n    threshold: 0.5\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	store := NewJudgePromptStore()
	w := NewJudgePromptWatcher(JudgePromptWatcherConfig{
		Path: path, Store: store, Interval: 30 * time.Millisecond,
	})
	w.Start()
	defer w.Stop()

	if store.Get().SystemPrompt != "old" {
		t.Fatal("首次应加载 old")
	}

	// sleep 一下再写，保险让 mtime 跳变
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path,
		[]byte("system_prompt: NEW\ndimensions:\n  - name: B\n    threshold: 0.6\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if snap := store.Get(); snap != nil && snap.SystemPrompt == "NEW" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if snap := store.Get(); snap == nil || snap.SystemPrompt != "NEW" {
		t.Fatalf("热加载失败: %+v", snap)
	}
	if w.Reloads() < 2 {
		t.Fatalf("reload 计数应 >=2, got %d", w.Reloads())
	}
	if w.Errors() != 0 {
		t.Fatalf("不应有 error, got %d", w.Errors())
	}
}

// TestJudgePromptWatcher_BadYAMLKeepsOld 坏 YAML 保留旧 snapshot。
func TestJudgePromptWatcher_BadYAMLKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(path,
		[]byte("system_prompt: good\ndimensions:\n  - name: A\n    threshold: 0.5\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	store := NewJudgePromptStore()
	w := NewJudgePromptWatcher(JudgePromptWatcherConfig{
		Path: path, Store: store, Interval: 30 * time.Millisecond,
	})
	w.Start()
	defer w.Stop()

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte(":::: bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.Errors() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if w.Errors() == 0 {
		t.Fatal("bad YAML 应触发 error 计数")
	}
	if snap := store.Get(); snap == nil || snap.SystemPrompt != "good" {
		t.Fatalf("坏 YAML 不应清空 store, got %+v", snap)
	}
}

// TestJudgePromptWatcher_UnchangedSkips 文件未变化不应重复 reload。
func TestJudgePromptWatcher_UnchangedSkips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(path,
		[]byte("system_prompt: x\ndimensions:\n  - name: A\n    threshold: 0.5\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	store := NewJudgePromptStore()
	w := NewJudgePromptWatcher(JudgePromptWatcherConfig{
		Path: path, Store: store, Interval: 20 * time.Millisecond,
	})
	w.Start()
	defer w.Stop()

	time.Sleep(200 * time.Millisecond)
	if got := w.Reloads(); got != 1 {
		t.Fatalf("未变化不应重复 reload, got %d", got)
	}
}

// TestJudgePromptWatcher_StopIdempotent Stop 幂等。
func TestJudgePromptWatcher_StopIdempotent(t *testing.T) {
	w := NewJudgePromptWatcher(JudgePromptWatcherConfig{Store: NewJudgePromptStore()})
	w.Start()
	w.Stop()
	w.Stop() // 第二次不应 panic / block
}

// ---------------- LLMJudge × PromptStore 端到端 ----------------
//
// 复用 judge_llm_test.go 里的 fakeJudgeModel 断言 lastReq 里的 system message。

// systemPromptOf 从 fakeJudgeModel.lastReq 抽出 system message 内容。
func systemPromptOf(fm *fakeJudgeModel) string {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if fm.lastReq == nil {
		return ""
	}
	for _, m := range fm.lastReq.Messages {
		if string(m.Role) == "system" {
			return m.Content
		}
	}
	return ""
}

// TestLLMJudge_PromptStore_Override Store 快照应覆盖默认 SystemPrompt。
func TestLLMJudge_PromptStore_Override(t *testing.T) {
	fm := &fakeJudgeModel{
		resp: `{"scores":[{"dimension":"CustomDim","score":0.9,"reason":"ok"}]}`,
	}
	store := NewJudgePromptStore()
	store.Replace(&JudgePromptSnapshot{
		SystemPrompt: "!CUSTOM SYSTEM PROMPT!",
		Dimensions:   []JudgeDimension{{Name: "CustomDim", Threshold: 0.5, Criterion: "c"}},
	})

	j, err := NewLLMJudge(LLMJudgeConfig{Model: fm, PromptStore: store})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := j.Score(context.Background(), JudgeInput{
		CaseID: "c1", UserQuery: "q", FinalAnswer: "a",
	})
	if err != nil {
		t.Fatalf("Score err: %v", err)
	}
	if got := systemPromptOf(fm); !strings.Contains(got, "!CUSTOM SYSTEM PROMPT!") {
		t.Fatalf("system prompt 未走 store: %q", got)
	}
	if len(rep.Scores) != 1 || rep.Scores[0].Dimension != "CustomDim" {
		t.Fatalf("维度未走 store: %+v", rep.Scores)
	}
	if !rep.AllPass || rep.AvgScore < 0.9 {
		t.Fatalf("打分异常: %+v", rep)
	}
}

// TestLLMJudge_PromptStore_NilFallback store=nil 时走默认 prompt（D17.2 行为）。
func TestLLMJudge_PromptStore_NilFallback(t *testing.T) {
	fm := &fakeJudgeModel{resp: `{"scores":[]}`}
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm})
	_, _ = j.Score(context.Background(), JudgeInput{CaseID: "c1"})
	if !strings.Contains(systemPromptOf(fm), "严格") {
		t.Fatalf("store=nil 应走默认 system prompt, got %q", systemPromptOf(fm))
	}
}

// TestLLMJudge_PromptStore_EmptySnapFallback 空 snap 也应走默认。
func TestLLMJudge_PromptStore_EmptySnapFallback(t *testing.T) {
	fm := &fakeJudgeModel{resp: `{"scores":[]}`}
	store := NewJudgePromptStore() // 不 Replace
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm, PromptStore: store})
	_, _ = j.Score(context.Background(), JudgeInput{CaseID: "c1"})
	if !strings.Contains(systemPromptOf(fm), "严格") {
		t.Fatalf("空 store 应走默认, got %q", systemPromptOf(fm))
	}
}

// TestLLMJudge_PromptStore_InputDimsWins 输入显式 Dimensions 优先。
func TestLLMJudge_PromptStore_InputDimsWins(t *testing.T) {
	fm := &fakeJudgeModel{
		resp: `{"scores":[{"dimension":"InputDim","score":0.8,"reason":"r"}]}`,
	}
	store := NewJudgePromptStore()
	store.Replace(&JudgePromptSnapshot{
		Dimensions: []JudgeDimension{{Name: "StoreDim", Threshold: 0.5}},
	})
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm, PromptStore: store})
	rep, err := j.Score(context.Background(), JudgeInput{
		CaseID: "c1", UserQuery: "q", FinalAnswer: "a",
		Dimensions: []JudgeDimension{{Name: "InputDim", Threshold: 0.5, Criterion: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Scores) != 1 || rep.Scores[0].Dimension != "InputDim" {
		t.Fatalf("输入维度应优先, got %+v", rep.Scores)
	}
}

// TestLLMJudge_PromptStore_HotReloadE2E Watcher + Store + LLMJudge 端到端。
func TestLLMJudge_PromptStore_HotReloadE2E(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(path,
		[]byte("system_prompt: V1\ndimensions:\n  - name: D\n    threshold: 0.5\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	store := NewJudgePromptStore()
	w := NewJudgePromptWatcher(JudgePromptWatcherConfig{
		Path: path, Store: store, Interval: 30 * time.Millisecond,
	})
	w.Start()
	defer w.Stop()

	fm := &fakeJudgeModel{resp: `{"scores":[{"dimension":"D","score":0.9,"reason":"ok"}]}`}
	j, _ := NewLLMJudge(LLMJudgeConfig{Model: fm, PromptStore: store})

	// 1. 初始应收到 V1
	if _, err := j.Score(context.Background(), JudgeInput{CaseID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemPromptOf(fm), "V1") {
		t.Fatalf("初始 prompt 应含 V1, got %q", systemPromptOf(fm))
	}

	// 2. 改 YAML → 等 watcher → 再 Score 应看到 V2
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path,
		[]byte("system_prompt: V2\ndimensions:\n  - name: D\n    threshold: 0.5\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if store.Get() != nil && store.Get().SystemPrompt == "V2" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if store.Get() == nil || store.Get().SystemPrompt != "V2" {
		t.Fatalf("watcher 应已 reload 到 V2")
	}

	if _, err := j.Score(context.Background(), JudgeInput{CaseID: "c2"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemPromptOf(fm), "V2") {
		t.Fatalf("reload 后 prompt 应含 V2, got %q", systemPromptOf(fm))
	}
}