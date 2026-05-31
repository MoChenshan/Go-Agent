package webhook

import (
	"sync"
	"testing"
	"time"
)

// fakeClock 用于在测试中精确推进"现在"，避开真实时间的抖动。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// 1. window<=0 → newDeduper 返回 nil；nil 上所有方法安全。
func TestDeduper_Disabled(t *testing.T) {
	if d := newDeduper(0, nil); d != nil {
		t.Fatalf("window=0 want nil, got %v", d)
	}
	if d := newDeduper(-1, nil); d != nil {
		t.Fatalf("window<0 want nil")
	}
	var nilD *deduper
	if got := nilD.Lookup("bk_alarm", "k"); got != "" {
		t.Errorf("nil Lookup want empty, got %q", got)
	}
	nilD.Record("bk_alarm", "k", "c1") // no panic
	if nilD.Size() != 0 {
		t.Errorf("nil Size want 0")
	}
	nilD.Stop() // no panic
}

// 2. Record → Lookup 命中；再 Lookup 同键幂等。
func TestDeduper_HitWithinWindow(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	d := newDeduper(10*time.Minute, clk.Now)
	defer d.Stop()

	if got := d.Lookup("bk_alarm", "alarm-1|t0"); got != "" {
		t.Fatalf("miss want empty, got %q", got)
	}
	d.Record("bk_alarm", "alarm-1|t0", "case-001")
	if got := d.Lookup("bk_alarm", "alarm-1|t0"); got != "case-001" {
		t.Errorf("hit want case-001, got %q", got)
	}
	// 再次 Lookup 仍命中
	if got := d.Lookup("bk_alarm", "alarm-1|t0"); got != "case-001" {
		t.Errorf("second hit want case-001, got %q", got)
	}
	if d.Size() != 1 {
		t.Errorf("size want 1, got %d", d.Size())
	}
}

// 3. 过期后 Lookup miss，并在 Lookup 内顺手清理该条目。
func TestDeduper_ExpireOnLookup(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	d := newDeduper(5*time.Minute, clk.Now)
	defer d.Stop()

	d.Record("tapd", "bug_update|1001", "case-X")
	if d.Size() != 1 {
		t.Fatalf("precondition failed")
	}
	// 推进超过窗口
	clk.Advance(6 * time.Minute)
	if got := d.Lookup("tapd", "bug_update|1001"); got != "" {
		t.Errorf("expired want empty, got %q", got)
	}
	// Lookup 顺手删了它
	if d.Size() != 0 {
		t.Errorf("expire lazy-delete failed, size=%d", d.Size())
	}
}

// 4. 不同 source 即便 natural 相同也不串号。
func TestDeduper_SourceIsolation(t *testing.T) {
	d := newDeduper(time.Minute, nil)
	defer d.Stop()
	d.Record("bk_alarm", "k", "case-bk")
	d.Record("tapd", "k", "case-tapd")
	if got := d.Lookup("bk_alarm", "k"); got != "case-bk" {
		t.Errorf("bk want case-bk, got %q", got)
	}
	if got := d.Lookup("tapd", "k"); got != "case-tapd" {
		t.Errorf("tapd want case-tapd, got %q", got)
	}
}

// 5. 空 natural / 空 caseID：Record 跳过，Lookup miss。
func TestDeduper_SkipEmpty(t *testing.T) {
	d := newDeduper(time.Minute, nil)
	defer d.Stop()

	d.Record("bk_alarm", "", "case-1")   // natural 空
	d.Record("bk_alarm", "   ", "case-1") // 空白
	d.Record("bk_alarm", "k", "")        // caseID 空
	if d.Size() != 0 {
		t.Errorf("empty inputs must be skipped, size=%d", d.Size())
	}
	if got := d.Lookup("bk_alarm", ""); got != "" {
		t.Errorf("empty Lookup want miss")
	}
}

// 6. Record 覆盖：同 key 后写胜出，expire 重置。
func TestDeduper_Overwrite(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	d := newDeduper(10*time.Minute, clk.Now)
	defer d.Stop()

	d.Record("bk_alarm", "k", "case-old")
	clk.Advance(5 * time.Minute)
	d.Record("bk_alarm", "k", "case-new") // 覆盖 + 续期
	clk.Advance(7 * time.Minute)          // 若没续期已过期；续期了则还在
	if got := d.Lookup("bk_alarm", "k"); got != "case-new" {
		t.Errorf("want case-new after overwrite+renew, got %q", got)
	}
}

// 7. gcOnce 直接清掉全部过期；保留未过期。
func TestDeduper_GCOnce(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	d := newDeduper(5*time.Minute, clk.Now)
	defer d.Stop()

	d.Record("bk_alarm", "old-1", "c1")
	d.Record("bk_alarm", "old-2", "c2")
	clk.Advance(4 * time.Minute)
	d.Record("bk_alarm", "fresh", "c3")
	clk.Advance(2 * time.Minute) // old-* 过期；fresh 未过期
	d.gcOnce()
	if d.Size() != 1 {
		t.Errorf("after gc want 1, got %d", d.Size())
	}
	if got := d.Lookup("bk_alarm", "fresh"); got != "c3" {
		t.Errorf("fresh must survive, got %q", got)
	}
}

// 8. Stop 幂等且能让 gcLoop 退出（这里只验证多次调用不 panic，
//    goroutine 退出由 race 检测在 CI 中兜底）。
func TestDeduper_StopIdempotent(t *testing.T) {
	d := newDeduper(time.Minute, nil)
	d.Stop()
	d.Stop()
	d.Stop()
}

// 9. 并发 Record / Lookup 不 race（与 -race 联合生效）。
func TestDeduper_ConcurrentSafe(t *testing.T) {
	d := newDeduper(time.Minute, nil)
	defer d.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				d.Record("bk_alarm", keyN(id, j), caseN(id, j))
			}
		}(i)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = d.Lookup("bk_alarm", keyN(id, j))
			}
		}(i)
	}
	wg.Wait()
	if d.Size() == 0 {
		t.Errorf("expect some entries recorded")
	}
}

func keyN(i, j int) string  { return "k-" + itoa(i) + "-" + itoa(j) }
func caseN(i, j int) string { return "c-" + itoa(i) + "-" + itoa(j) }
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(buf[i:])
	if neg {
		return "-" + s
	}
	return s
}

// 10. bkNaturalKey: AlarmID+StartTime；两者都空 → 空串（幂等降级）。
func TestBKNaturalKey(t *testing.T) {
	cases := []struct {
		name string
		in   BKAlarmPayload
		want string
	}{
		{"both empty", BKAlarmPayload{}, ""},
		{"only id", BKAlarmPayload{AlarmID: "A1"}, "A1|"},
		{"only st", BKAlarmPayload{StartTime: "2026-04-22T10:00"}, "|2026-04-22T10:00"},
		{"full", BKAlarmPayload{AlarmID: "A1", StartTime: "t0"}, "A1|t0"},
		{"trim", BKAlarmPayload{AlarmID: "  A1  ", StartTime: " t0 "}, "A1|t0"},
	}
	for _, c := range cases {
		if got := bkNaturalKey(c.in); got != c.want {
			t.Errorf("%s: want %q got %q", c.name, c.want, got)
		}
	}
}

// 11. tapdNaturalKey: Event+BugID；都空 → 空串。
func TestTAPDNaturalKey(t *testing.T) {
	cases := []struct {
		name string
		in   TAPDPayload
		want string
	}{
		{"both empty", TAPDPayload{}, ""},
		{"only event", TAPDPayload{Event: "bug_update"}, "bug_update|"},
		{"only bug", TAPDPayload{Bug: &TAPDBug{ID: "1001"}}, "|1001"},
		{"full", TAPDPayload{Event: "bug_update", Bug: &TAPDBug{ID: "1001"}}, "bug_update|1001"},
	}
	for _, c := range cases {
		if got := tapdNaturalKey(c.in); got != c.want {
			t.Errorf("%s: want %q got %q", c.name, c.want, got)
		}
	}
}
