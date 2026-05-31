package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 1. New 在目录不存在时自动 MkdirAll
func TestFileStore_New_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "path")
	path := filepath.Join(dir, "reports.jsonl")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if s.Path() != path {
		t.Errorf("path mismatch: %s", s.Path())
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

// 2. 空路径报错
func TestFileStore_New_EmptyPath(t *testing.T) {
	if _, err := NewFileStore(""); err == nil {
		t.Fatalf("empty path must error")
	}
	if _, err := NewFileStore("   "); err == nil {
		t.Fatalf("blank path must error")
	}
}

// 3. Save → Get 往返
func TestFileStore_SaveGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	r := Report{CaseID: "c1", Title: "T1", Severity: SeverityHigh, Outcome: "ok"}
	if err := s.Save("c1", r); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := s.Get("c1")
	if !ok {
		t.Fatalf("get miss")
	}
	if got.Title != "T1" || got.Outcome != "ok" || got.Severity != SeverityHigh {
		t.Errorf("got mismatch: %+v", got)
	}
	// Get 未存在
	if _, ok := s.Get("not-exist"); ok {
		t.Errorf("should miss")
	}
}

// 4. Save 空 CaseID 报错
func TestFileStore_SaveEmptyCaseID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	s, _ := NewFileStore(path)
	if err := s.Save("", Report{}); err == nil {
		t.Fatalf("empty case_id must error")
	}
	if err := s.Save("  ", Report{}); err == nil {
		t.Fatalf("blank case_id must error")
	}
}

// 5. 同一 CaseID 多次 Save，以最后一次为准（reload 也生效）
func TestFileStore_Overwrite_AcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	s, _ := NewFileStore(path)
	_ = s.Save("c1", Report{CaseID: "c1", Title: "v1"})
	_ = s.Save("c1", Report{CaseID: "c1", Title: "v2"})
	_ = s.Save("c1", Report{CaseID: "c1", Title: "v3"})

	got, _ := s.Get("c1")
	if got.Title != "v3" {
		t.Errorf("in-memory want v3, got %q", got.Title)
	}

	// 新实例 reload：磁盘 3 行，最后一行胜出
	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got2, ok := s2.Get("c1")
	if !ok || got2.Title != "v3" {
		t.Errorf("reload want v3, got %+v", got2)
	}
}

// 6. List 返回全部 CaseID
func TestFileStore_List(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	s, _ := NewFileStore(path)
	_ = s.Save("a", Report{CaseID: "a"})
	_ = s.Save("b", Report{CaseID: "b"})
	_ = s.Save("a", Report{CaseID: "a"}) // 重复不增加
	got := s.List()
	if len(got) != 2 {
		t.Errorf("want 2, got %d (%v)", len(got), got)
	}
}

// 7. reload 跳过损坏行，合法行照常加载
func TestFileStore_Reload_SkipsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	// 手动写入：1 个正常 + 1 个损坏 + 1 个正常 + 空行
	content := `{"case_id":"c1","title":"good-1","version":"v1"}
this is not json
{"case_id":"c2","title":"good-2","version":"v1"}

`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if len(s.List()) != 2 {
		t.Errorf("want 2 after reload, got %v", s.List())
	}
	if r, _ := s.Get("c1"); r.Title != "good-1" {
		t.Errorf("c1 want good-1, got %q", r.Title)
	}
	if r, _ := s.Get("c2"); r.Title != "good-2" {
		t.Errorf("c2 want good-2, got %q", r.Title)
	}
}

// 8. CaseID 与参数不一致时，以参数为键；Report.CaseID 为空时自动补
func TestFileStore_Save_CaseIDSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	s, _ := NewFileStore(path)
	_ = s.Save("key-a", Report{CaseID: ""}) // 空 → 用 key
	got, ok := s.Get("key-a")
	if !ok {
		t.Fatalf("miss")
	}
	if got.CaseID != "key-a" {
		t.Errorf("caseID auto-fill failed: %q", got.CaseID)
	}
}

// 9. 文件不存在 → New 成功且 List 为空
func TestFileStore_New_NotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.jsonl")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("want empty")
	}
	// Save 后文件被创建
	_ = s.Save("x", Report{CaseID: "x"})
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"case_id":"x"`) {
		t.Errorf("content mismatch: %s", data)
	}
}

// 10. Reload 公开 API 可显式触发，同步外部改动
func TestFileStore_ExplicitReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	s, _ := NewFileStore(path)
	// 外部直接写一行进来
	_ = os.WriteFile(path, []byte(`{"case_id":"ext","title":"from outside","version":"v1"}`+"\n"), 0o644)
	if _, ok := s.Get("ext"); ok {
		t.Fatalf("should not see ext before reload")
	}
	if err := s.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := s.Get("ext")
	if !ok || got.Title != "from outside" {
		t.Errorf("reload failed: %+v", got)
	}
}
