package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// invokeTool 通过 tool 的 Call 接口调用 iwiki_search，返回解码后的 map。
func invokeTool(t *testing.T, tl tool.Tool, args map[string]any) map[string]any {
	t.Helper()
	ct, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("tool %q is not callable", tl.Declaration().Name)
	}
	buf, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw, err := ct.Call(context.Background(), buf)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	// 统一做一次 re-marshal，得到 map[string]any，避免框架返回类型差异
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v, raw=%s", err, string(b))
	}
	return out
}

func TestBuildIWikiTool_DisabledReturnsStub(t *testing.T) {
	cfg := IWikiConfig{Disabled: true, PaasID: "x", Token: "y"}
	tl, isStub := BuildIWikiTool(cfg)
	if !isStub {
		t.Fatalf("disabled must yield stub")
	}
	if tl == nil {
		t.Fatalf("tool is nil")
	}
	if got := tl.Declaration().Name; got != IWikiToolName {
		t.Errorf("tool name=%q", got)
	}

	out := invokeTool(t, tl, map[string]any{"query": "任何问题"})
	if ok, _ := out["ok"].(bool); ok {
		t.Errorf("stub must return ok=false, got %+v", out)
	}
	if stub, _ := out["stub"].(bool); !stub {
		t.Errorf("stub flag must be true")
	}
	msg, _ := out["message"].(string)
	if !strings.Contains(msg, "禁用") {
		t.Errorf("stub message should mention disabled, got %q", msg)
	}
}

func TestBuildIWikiTool_MissingCredReturnsStub(t *testing.T) {
	cfg := IWikiConfig{} // 缺 PaasID / Token
	tl, isStub := BuildIWikiTool(cfg)
	if !isStub {
		t.Fatalf("missing creds must yield stub")
	}
	out := invokeTool(t, tl, map[string]any{"query": "abc"})
	if ok, _ := out["ok"].(bool); ok {
		t.Errorf("stub must return ok=false")
	}
	msg, _ := out["message"].(string)
	if !strings.Contains(msg, "IWIKI_PAAS_ID") {
		t.Errorf("stub message should mention missing env vars, got %q", msg)
	}
}

func TestDefaultIWikiConfig_EnvDriven(t *testing.T) {
	// 为避免污染其他 test，走 Setenv 自动还原
	t.Setenv("IWIKI_PAAS_ID", "paas-123")
	t.Setenv("IWIKI_TOKEN", "tok-456")
	t.Setenv("IWIKI_MAX_RESULTS", "8")
	t.Setenv("IWIKI_SPACE_IDS", "10, 20,bad,30")
	t.Setenv("IWIKI_URL", "http://test-iwiki.example/api")
	_ = os.Unsetenv("IWIKI_DISABLE")

	cfg := DefaultIWikiConfig()
	if cfg.PaasID != "paas-123" || cfg.Token != "tok-456" {
		t.Errorf("cred parse failed: %+v", cfg)
	}
	if cfg.MaxResults != 8 {
		t.Errorf("max_results=%d", cfg.MaxResults)
	}
	if cfg.URL != "http://test-iwiki.example/api" {
		t.Errorf("url=%q", cfg.URL)
	}
	// SpaceIDs 应为 [10, 20, 30]，"bad" 忽略
	if len(cfg.SpaceIDs) != 3 || cfg.SpaceIDs[0] != 10 || cfg.SpaceIDs[2] != 30 {
		t.Errorf("space_ids=%v", cfg.SpaceIDs)
	}
	if cfg.Disabled {
		t.Errorf("should not be disabled")
	}
}

func TestDefaultIWikiConfig_DisableFlag(t *testing.T) {
	t.Setenv("IWIKI_DISABLE", "1")
	cfg := DefaultIWikiConfig()
	if !cfg.Disabled {
		t.Errorf("IWIKI_DISABLE=1 should mark disabled")
	}
}

func TestSnippet_RuneSafeTruncate(t *testing.T) {
	in := "中文abc中文abc中文abc中文abc" // 含中文，rune 长度 > byte 长度
	got := snippet(in, 5)
	// rune 截取前 5 个：中文abc
	want := "中文abc…"
	if got != want {
		t.Errorf("snippet=%q want=%q", got, want)
	}

	// 短文本原样返回
	if got := snippet("abc", 10); got != "abc" {
		t.Errorf("short text should pass through, got %q", got)
	}

	// max<=0 保持原样
	if got := snippet("xyz", 0); got != "xyz" {
		t.Errorf("zero max should pass through, got %q", got)
	}
}
