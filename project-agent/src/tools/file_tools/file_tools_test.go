
package filetools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// mkTempFile 在 t.TempDir() 下写一个文件，并返回绝对路径 + 允许该 root 的 Config。
func mkTempFile(t *testing.T, name, content string) (string, Config) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	cfg := Config{
		AllowRoots:   []string{dir},
		MaxReadBytes: 1 << 20,
	}
	return p, cfg
}

func TestResolvePath_WhitelistEnforced(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{AllowRoots: []string{dir}, MaxReadBytes: 4096}

	// 白名单内
	good := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(good, []byte("x"), 0o600)
	if _, err := resolvePath(cfg, good); err != nil {
		t.Fatalf("allowed path rejected: %v", err)
	}

	// 白名单外（系统 temp 的兄弟目录）
	other := t.TempDir()
	bad := filepath.Join(other, "b.txt")
	_ = os.WriteFile(bad, []byte("x"), 0o600)
	if _, err := resolvePath(cfg, bad); err == nil {
		t.Fatalf("expected path outside whitelist to be rejected")
	}
}

func TestDetect_JSON(t *testing.T) {
	p, cfg := mkTempFile(t, "x.json", `{"a":1,"b":[1,2,3]}`)
	tl := newDetectTool(cfg)

	out, err := callDetect(t, tl, p)
	if err != nil {
		t.Fatalf("detect err: %v", err)
	}
	if !out.OK || out.Kind != "json" {
		t.Fatalf("want kind=json, got kind=%q ok=%v msg=%q", out.Kind, out.OK, out.Message)
	}
	if out.SizeBytes == 0 {
		t.Fatalf("size_bytes should > 0")
	}
}

func TestDetect_Log_ByKeyword(t *testing.T) {
	// 扩展名 .txt，但内容有日志特征 → 应识别为 log
	content := `2026-04-20 10:00:00 INFO hello
2026-04-20 10:00:01 ERROR oom
2026-04-20 10:00:02 WARN slow
`
	p, cfg := mkTempFile(t, "x.txt", content)
	tl := newDetectTool(cfg)

	out, err := callDetect(t, tl, p)
	if err != nil {
		t.Fatalf("detect err: %v", err)
	}
	if out.Kind != "log" {
		t.Fatalf("want kind=log, got %q", out.Kind)
	}
}

func TestJSONQuery_Basic(t *testing.T) {
	p, cfg := mkTempFile(t, "x.json", `{"status":{"phase":"Running","replicas":3},"items":[{"name":"a"},{"name":"b"}]}`)
	tl := newJSONQueryTool(cfg)

	// 顶层 keys
	out := callJSONQuery(t, tl, p, "")
	if !out.OK || len(out.TopKeys) != 2 {
		t.Fatalf("top keys mismatch: %+v", out)
	}

	// 点路径
	out = callJSONQuery(t, tl, p, "$.status.phase")
	if !out.Exists || out.Value != "Running" {
		t.Fatalf("$.status.phase want Running, got %+v", out.Value)
	}

	// 索引路径
	out = callJSONQuery(t, tl, p, "$.items[1].name")
	if !out.Exists || out.Value != "b" {
		t.Fatalf("$.items[1].name want b, got %+v", out.Value)
	}

	// 不存在
	out = callJSONQuery(t, tl, p, "$.nope.xxx")
	if out.Exists {
		t.Fatalf("non-existent path should return exists=false")
	}
}

func TestLogAnalyze_CountsAndPatterns(t *testing.T) {
	content := `2026-04-20 10:15:58 ERROR [cache] load_inventory_cache: out of memory, used=4.1GB limit=2GB
2026-04-20 10:15:58 FATAL [game-core] init() panic: runtime: out of memory
2026-04-20 10:16:28 ERROR [cache] load_inventory_cache: out of memory, used=4.1GB limit=2GB
2026-04-20 10:16:28 FATAL [game-core] init() panic: runtime: out of memory
2026-04-20 10:17:33 ERROR [cache] load_inventory_cache: out of memory, used=4.1GB limit=2GB
2026-04-20 10:17:33 FATAL [game-core] init() panic: runtime: out of memory
2026-04-20 10:19:15 ERROR [api-gateway] upstream game-core: connection refused 10.0.0.12:8080
2026-04-20 10:19:16 ERROR [api-gateway] upstream game-core: connection refused 10.0.0.13:8080
`
	p, cfg := mkTempFile(t, "a.log", content)
	tl := newLogAnalyzeTool(cfg)

	out := callLogAnalyze(t, tl, p)
	if !out.OK {
		t.Fatalf("log_analyze not ok: %s", out.Message)
	}
	if out.LevelCount["ERROR"] != 5 {
		t.Fatalf("ERROR count mismatch: %+v", out.LevelCount)
	}
	if out.LevelCount["FATAL"] != 3 {
		t.Fatalf("FATAL count mismatch: %+v", out.LevelCount)
	}
	// 相同 OOM 行（经过 normalize）应该聚合为一个模式
	if len(out.TopPatterns) == 0 {
		t.Fatalf("expect top_patterns")
	}
	// 最高频模式 count 应 >= 3（三次 OOM ERROR 行）
	if out.TopPatterns[0].Count < 3 {
		t.Fatalf("top pattern count too small: %d, patterns=%+v", out.TopPatterns[0].Count, out.TopPatterns)
	}
	// 时间桶应抓到 10:15/10:16/10:17 中的某一分钟
	if len(out.TimeBuckets) == 0 || !strings.HasPrefix(out.TimeBuckets[0].Minute, "2026-04-20 10:") {
		t.Fatalf("time bucket unexpected: %+v", out.TimeBuckets)
	}
	// first_error / last_error 行号应有效
	if out.FirstError == nil || out.LastError == nil {
		t.Fatalf("first/last error missing")
	}
	if out.FirstError.Line >= out.LastError.Line {
		t.Fatalf("first_line(%d) should < last_line(%d)", out.FirstError.Line, out.LastError.Line)
	}
}

func TestReadSlice_LineKeyword(t *testing.T) {
	content := ""
	for i := 1; i <= 20; i++ {
		if i%5 == 0 {
			content += "2026-04-20 10:00 ERROR bad\n"
		} else {
			content += "2026-04-20 10:00 INFO ok\n"
		}
	}
	p, cfg := mkTempFile(t, "a.log", content)
	tl := newReadSliceTool(cfg)

	out := callReadSlice(t, tl, ReadSliceInput{Path: p, Mode: "line", Offset: 1, Size: 100, Keyword: "ERROR"})
	if !out.OK {
		t.Fatalf("read_slice not ok: %s", out.Message)
	}
	lines := strings.Split(strings.TrimRight(out.Content, "\n"), "\n")
	if len(lines) != 4 { // 第 5/10/15/20 行
		t.Fatalf("keyword filter: expect 4 lines, got %d\n%s", len(lines), out.Content)
	}
	for _, l := range lines {
		if !strings.Contains(l, "ERROR") {
			t.Fatalf("unexpected non-match line: %s", l)
		}
	}
}

func TestNormalizePattern(t *testing.T) {
	cases := []struct {
		in   string
		want string // 部分匹配：必须包含该子串
	}{
		{"pid=12345 user=abc", "pid=<n>"},
		{"addr 10.0.0.1:8080 timeout", "addr <ip> timeout"},
		{"path /var/log/app.log missing", "path <path> missing"},
		{"uuid f47ac10b-58cc-4372-a567-0e02b2c3d479 seen", "uuid <uuid> seen"},
	}
	for _, c := range cases {
		got := normalizePattern(c.in)
		if !strings.Contains(got, c.want) {
			t.Errorf("normalizePattern(%q) = %q, want contains %q", c.in, got, c.want)
		}
	}
}

// --- 辅助：通过 tool.Tool 的 Call 接口调用，以便测试路径包含序列化环节 ---

func callDetect(t *testing.T, _ any, path string) (*DetectOutput, error) {
	t.Helper()
	// 直接调用内部函数语义更精确；但我们用反射或强类型包一下不够简洁。
	// 这里选择重新走一次内部函数逻辑：构造 cfg 从 path 的 dir
	cfg := Config{AllowRoots: []string{filepath.Dir(path)}, MaxReadBytes: 1 << 20}
	// 复用 newDetectTool 构造内部 fn 无法直接拿到；我们改为通过 tool Call。
	return invokeDetect(t, cfg, path)
}

func invokeDetect(t *testing.T, cfg Config, path string) (*DetectOutput, error) {
	t.Helper()
	tl := newDetectTool(cfg)
	ct, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("newDetectTool did not return CallableTool: %T", tl)
	}
	raw, err := ct.Call(context.Background(), []byte(`{"path":`+jsonQuote(path)+`}`))
	if err != nil {
		return nil, err
	}
	out, ok := raw.(*DetectOutput)
	if !ok {
		t.Fatalf("detect result type mismatch: %T", raw)
	}
	return out, nil
}

func callJSONQuery(t *testing.T, _ any, path, q string) *JSONQueryOutput {
	t.Helper()
	cfg := Config{AllowRoots: []string{filepath.Dir(path)}, MaxReadBytes: 1 << 20}
	tl := newJSONQueryTool(cfg)
	ct := tl.(tool.CallableTool)
	raw, err := ct.Call(context.Background(), []byte(`{"path":`+jsonQuote(path)+`,"query":`+jsonQuote(q)+`}`))
	if err != nil {
		t.Fatalf("json_query call: %v", err)
	}
	return raw.(*JSONQueryOutput)
}

func callLogAnalyze(t *testing.T, _ any, path string) *LogAnalyzeOutput {
	t.Helper()
	cfg := Config{AllowRoots: []string{filepath.Dir(path)}, MaxReadBytes: 1 << 20}
	tl := newLogAnalyzeTool(cfg)
	ct := tl.(tool.CallableTool)
	raw, err := ct.Call(context.Background(), []byte(`{"path":`+jsonQuote(path)+`}`))
	if err != nil {
		t.Fatalf("log_analyze call: %v", err)
	}
	return raw.(*LogAnalyzeOutput)
}

func callReadSlice(t *testing.T, _ any, in ReadSliceInput) *ReadSliceOutput {
	t.Helper()
	cfg := Config{AllowRoots: []string{filepath.Dir(in.Path)}, MaxReadBytes: 1 << 20}
	tl := newReadSliceTool(cfg)
	ct := tl.(tool.CallableTool)
	b := `{"path":` + jsonQuote(in.Path) +
		`,"mode":` + jsonQuote(in.Mode) +
		`,"offset":` + itoa(in.Offset) +
		`,"size":` + itoa(in.Size) +
		`,"keyword":` + jsonQuote(in.Keyword) + `}`
	raw, err := ct.Call(context.Background(), []byte(b))
	if err != nil {
		t.Fatalf("read_slice call: %v", err)
	}
	return raw.(*ReadSliceOutput)
}

func jsonQuote(s string) string {
	// 简易 JSON 字符串引用（仅测试用）
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
