package audit

import (
	"strconv"
	"testing"
	"time"
)

// BenchmarkHMACSign_Single 单条签名（无链式）。
//
// 期望吞吐：现代 x86_64 单核 ≥ 1M ops/s（HMAC-SHA256 ~500ns/op）。
// 用于回归性能：当某次 PR 把签名耗时拖到 5x 以上时及时发现。
func BenchmarkHMACSign_Single(b *testing.B) {
	signer := mustBenchSigner(b, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := newBenchRecord(i)
		if err := signer.Sign(rec); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHMACSign_Chain 链式签名（PrevSig 注入）。
// 链式相对单条增加一次 sha 比较 + 字段拷贝，期望开销 < 30%。
func BenchmarkHMACSign_Chain(b *testing.B) {
	signer := mustBenchSigner(b, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := newBenchRecord(i)
		if err := signer.Sign(rec); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHMACVerify 验签。
func BenchmarkHMACVerify(b *testing.B) {
	signer := mustBenchSigner(b, false)
	rec := newBenchRecord(42)
	if err := signer.Sign(rec); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := signer.Verify(rec); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHMACSign_Parallel 并发场景（实际审计写入是多协程并发的）。
func BenchmarkHMACSign_Parallel(b *testing.B) {
	signer := mustBenchSigner(b, true)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			rec := newBenchRecord(i)
			if err := signer.Sign(rec); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// ----- helpers -----

// mustBenchSigner 给 benchmark 专用，避免与 hmac_test.go 中签名不同的同名 helper 冲突。
func mustBenchSigner(tb testing.TB, chain bool) *HMACSigner {
	tb.Helper()
	keys := map[string][]byte{
		"k1": []byte("0123456789abcdef0123456789abcdef"), // 32B
	}
	s, err := NewHMACSigner(keys, "k1", chain)
	if err != nil {
		tb.Fatalf("NewHMACSigner: %v", err)
	}
	return s
}

func newBenchRecord(i int) *Record {
	return &Record{
		TS:       time.Now().UTC().Format(time.RFC3339Nano),
		User:     "bench-user",
		Agent:    "repair_agent",
		Action:   "bcs.pod.restart",
		Severity: "high",
		Target:   "BCS-K8S-001/ns-bench/game-core",
		Params: map[string]any{
			"replicas":   3,
			"reason":     "OOM rollback",
			"iter":       strconv.Itoa(i),
			"note":       "this is a moderately long note that exercises json encoding cost",
		},
		Reason:    "auto-bench",
		Result:    "success",
		SessionID: "s-bench-" + strconv.Itoa(i),
	}
}
