package cost

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// EstimateTokens
// ---------------------------------------------------------------------------

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		in   string
		min  int // 允许 ±10% 误差，给区间
		max  int
	}{
		{"empty", "", 0, 0},
		{"pure-ascii", strings.Repeat("a", 400), 95, 105},
		{"pure-cjk", strings.Repeat("你", 100), 155, 165},
		{"mixed", "你好 hello world 世界", 8, 18},
		{"only-emoji", "😀😎🎉", 2, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.in)
			if got < tt.min || got > tt.max {
				t.Fatalf("got=%d, expect in [%d,%d]", got, tt.min, tt.max)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tracker
// ---------------------------------------------------------------------------

func TestTrackerRecord(t *testing.T) {
	tr := NewTracker(nil)
	cost := tr.Record("s1", "deepseek-chat", 1000, 500, 200)
	// 预期：(1000-200)/1000*0.00027 + 200/1000*0.00007 + 500/1000*0.00110
	// = 0.000216 + 0.000014 + 0.000550 = 0.000780
	if cost < 0.00077 || cost > 0.00079 {
		t.Fatalf("unexpected cost: %f", cost)
	}
	snap := tr.Snapshot("s1")
	if snap.InputTokens != 1000 || snap.OutputTokens != 500 || snap.CachedTokens != 200 {
		t.Fatalf("snapshot mismatch: %+v", snap)
	}
	if snap.LastModel != "deepseek-chat" {
		t.Fatalf("last model mismatch: %s", snap.LastModel)
	}
	if tr.GlobalIn() != 1000 || tr.GlobalOut() != 500 {
		t.Fatalf("global counter mismatch")
	}
}

func TestTrackerUnknownModelFallback(t *testing.T) {
	tr := NewTracker(nil)
	cost := tr.Record("s1", "no-such-model", 1000, 500, 0)
	// 应当按 deepseek-chat 价目记账，不能返回 0 静默丢账
	if cost <= 0 {
		t.Fatalf("unknown model should fall back, got %f", cost)
	}
}

func TestTrackerConcurrent(t *testing.T) {
	tr := NewTracker(nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Record("s1", "deepseek-chat", 100, 50, 0)
		}()
	}
	wg.Wait()
	snap := tr.Snapshot("s1")
	if snap.Calls != 100 || snap.InputTokens != 10000 || snap.OutputTokens != 5000 {
		t.Fatalf("race-condition lost updates: %+v", snap)
	}
}

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

func TestRouterPick(t *testing.T) {
	r := NewRouter("deepseek-chat", "deepseek-reasoner")
	tests := []struct {
		name string
		hint RouteHint
		want string
	}{
		{"budget-exhausted-force-cheap",
			RouteHint{PromptTokens: 100000, BudgetLeftUSD: 0, NeedReasoning: true, UserTier: "enterprise"},
			"deepseek-chat"},
		{"enterprise-reasoning-premium",
			RouteHint{PromptTokens: 500, BudgetLeftUSD: -1, NeedReasoning: true, UserTier: "enterprise"},
			"deepseek-reasoner"},
		{"long-prompt-premium",
			RouteHint{PromptTokens: 9000, BudgetLeftUSD: -1, UserTier: "free"},
			"deepseek-reasoner"},
		{"multi-tool-reasoning-premium",
			RouteHint{PromptTokens: 500, BudgetLeftUSD: -1, NeedReasoning: true, ToolCallCount: 4, UserTier: "pro"},
			"deepseek-reasoner"},
		{"default-cheap",
			RouteHint{PromptTokens: 500, BudgetLeftUSD: -1, UserTier: "free"},
			"deepseek-chat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Pick(tt.hint); got != tt.want {
				t.Fatalf("got=%s, want=%s", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PromptCache
// ---------------------------------------------------------------------------

func TestPromptCacheBasic(t *testing.T) {
	c := NewPromptCache(3, time.Second)
	c.Put("a", "Aresp", 10)
	c.Put("b", "Bresp", 10)
	if v, ok := c.Get("a"); !ok || v != "Aresp" {
		t.Fatalf("get a failed: %s %v", v, ok)
	}
	if _, ok := c.Get("zzz"); ok {
		t.Fatalf("missing key should miss")
	}
}

func TestPromptCacheLRUEviction(t *testing.T) {
	c := NewPromptCache(2, time.Hour)
	c.Put("a", "A", 0)
	c.Put("b", "B", 0)
	_, _ = c.Get("a") // 触摸 a 让 b 成为 LRU
	c.Put("c", "C", 0)
	if _, ok := c.Get("b"); ok {
		t.Fatalf("b should be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatalf("a should still be there")
	}
}

func TestPromptCacheTTL(t *testing.T) {
	c := NewPromptCache(8, 30*time.Millisecond)
	c.Put("a", "A", 0)
	time.Sleep(50 * time.Millisecond)
	if _, ok := c.Get("a"); ok {
		t.Fatalf("entry should have expired")
	}
}

func TestPromptCacheStats(t *testing.T) {
	c := NewPromptCache(8, time.Hour)
	c.Put("a", "A", 0)
	_, _ = c.Get("a")
	_, _ = c.Get("a")
	_, _ = c.Get("nope")
	hits, misses, hitRate := c.Stats()
	if hits != 2 || misses != 1 {
		t.Fatalf("hits=%d misses=%d", hits, misses)
	}
	if hitRate < 0.65 || hitRate > 0.68 {
		t.Fatalf("hit rate=%f", hitRate)
	}
}

func TestPromptCacheUpdate(t *testing.T) {
	c := NewPromptCache(8, time.Hour)
	c.Put("a", "v1", 0)
	c.Put("a", "v2", 0)
	v, ok := c.Get("a")
	if !ok || v != "v2" {
		t.Fatalf("update failed: got=%s ok=%v", v, ok)
	}
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

func TestServiceFlow(t *testing.T) {
	svc := NewService(Config{
		Cheap:         "deepseek-chat",
		Premium:       "deepseek-reasoner",
		CacheCapacity: 4,
		CacheTTL:      time.Hour,
	})
	model := svc.PickModel(RouteHint{PromptTokens: 100, UserTier: "free", BudgetLeftUSD: -1})
	if model != "deepseek-chat" {
		t.Fatalf("expect cheap, got %s", model)
	}
	prompt := "你好"
	if _, ok := svc.Lookup(model, prompt); ok {
		t.Fatalf("first lookup must miss")
	}
	cost := svc.Store(nil, "s1", model, prompt, "answer", 10, 5, 0)
	if cost <= 0 {
		t.Fatalf("cost must be positive")
	}
	if got, ok := svc.Lookup(model, prompt); !ok || got != "answer" {
		t.Fatalf("second lookup must hit")
	}
	tot := svc.Snapshot("s1")
	if tot.Calls != 1 {
		t.Fatalf("calls=%d", tot.Calls)
	}
	hits, _, _ := svc.CacheStats()
	if hits != 1 {
		t.Fatalf("cache hits=%d", hits)
	}
}

func TestKeyStable(t *testing.T) {
	a := Key("m", "p")
	b := Key("m", "p")
	if a != b {
		t.Fatalf("key should be stable")
	}
	c := Key("m2", "p")
	if a == c {
		t.Fatalf("model boundary must affect key")
	}
}
