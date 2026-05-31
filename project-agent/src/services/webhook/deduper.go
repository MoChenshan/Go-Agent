// Package webhook 的幂等去重（D16）。
//
// 问题：蓝鲸告警 / TAPD Webhook 在 HTTP 失败时会指数退避重试，
// 同一告警若多次分派会：
//   - 多一份 caseID / 占用多一份 MemStore / FileStore 空间；
//   - 多跑一次 Agent，消耗 token + 可能多下发写操作（哪怕 HITL，也会发 plan）。
//
// 策略：基于 (source, natural_key, bucket_time) 做幂等键。
//   - natural_key：蓝鲸用 AlarmID+StartTime；TAPD 用 Event+BugID。
//   - bucket_time：防"同一告警长时间后再真来一次"误判，每 N 分钟分桶；
//     默认 10 分钟，可由 Config.DedupeWindow 调整。
//
// 命中缓存时直接返回已有 caseID，不重复分派。
//
// 实现用 sync.Map + 定期 GC；对 QPS 友好，不引入外部依赖。
package webhook

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// deduper 幂等缓存。
//
// 并发语义：
//   - Lookup: 读锁；大多数请求走读路径
//   - Record: 写锁；首次出现的请求才进写路径
//   - GC: 独立 goroutine 周期性清理过期条目（New 时启动，Stop 时退出）
type deduper struct {
	window time.Duration
	clock  func() time.Time

	mu    sync.Mutex
	cache map[string]dedupEntry

	stop chan struct{}
}

type dedupEntry struct {
	caseID string
	expire time.Time
}

// newDeduper window<=0 时返回 nil（等价于关闭幂等）。
func newDeduper(window time.Duration, clock func() time.Time) *deduper {
	if window <= 0 {
		return nil
	}
	if clock == nil {
		clock = time.Now
	}
	d := &deduper{
		window: window,
		clock:  clock,
		cache:  make(map[string]dedupEntry),
		stop:   make(chan struct{}),
	}
	go d.gcLoop()
	return d
}

// Stop 停止 GC goroutine；Handler.Shutdown 时调用。多次调用幂等。
func (d *deduper) Stop() {
	if d == nil {
		return
	}
	select {
	case <-d.stop:
		return
	default:
		close(d.stop)
	}
}

// Lookup 查询已存在的 caseID；miss 时返回空串。
func (d *deduper) Lookup(source, natural string) string {
	if d == nil || strings.TrimSpace(natural) == "" {
		return ""
	}
	key := d.makeKey(source, natural)
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.cache[key]
	if !ok {
		return ""
	}
	if d.clock().After(entry.expire) {
		delete(d.cache, key)
		return ""
	}
	return entry.caseID
}

// Record 记录一次分派：natural 为空时视为无法去重，直接跳过。
func (d *deduper) Record(source, natural, caseID string) {
	if d == nil || strings.TrimSpace(natural) == "" || strings.TrimSpace(caseID) == "" {
		return
	}
	key := d.makeKey(source, natural)
	d.mu.Lock()
	d.cache[key] = dedupEntry{
		caseID: caseID,
		expire: d.clock().Add(d.window),
	}
	d.mu.Unlock()
}

// Size 返回当前缓存条目数（单测用）。
func (d *deduper) Size() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.cache)
}

// makeKey: source|natural → sha1 hex（防 key 内容过长 / 含特殊字符）。
func (d *deduper) makeKey(source, natural string) string {
	h := sha1.New()
	h.Write([]byte(source))
	h.Write([]byte{0})
	h.Write([]byte(natural))
	return hex.EncodeToString(h.Sum(nil))
}

// gcLoop 每 window/2（至少 30s）扫一次，清掉过期条目；Stop 时退出。
func (d *deduper) gcLoop() {
	interval := d.window / 2
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			d.gcOnce()
		case <-d.stop:
			return
		}
	}
}

func (d *deduper) gcOnce() {
	now := d.clock()
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, v := range d.cache {
		if now.After(v.expire) {
			delete(d.cache, k)
		}
	}
}

// —— 领域层 natural key 构造 ——

// bkNaturalKey 蓝鲸告警的天然幂等键：AlarmID + StartTime。
// 都为空时返回空串（幂等降级为关闭）。
func bkNaturalKey(p BKAlarmPayload) string {
	id := strings.TrimSpace(p.AlarmID)
	st := strings.TrimSpace(p.StartTime)
	if id == "" && st == "" {
		return ""
	}
	return id + "|" + st
}

// tapdNaturalKey TAPD 事件的天然幂等键：Event + BugID。
func tapdNaturalKey(p TAPDPayload) string {
	ev := strings.TrimSpace(p.Event)
	bug := ""
	if p.Bug != nil {
		bug = strings.TrimSpace(p.Bug.ID)
	}
	if ev == "" && bug == "" {
		return ""
	}
	return ev + "|" + bug
}
