// Package main 提供一个零外部依赖的端到端 demo。
//
// 目的：评审/面试现场，无需配置 BCS / iWiki / TAPD / OpenAI 凭据，
//      也能 5 秒内拉起 Agent，看到 webhook → 弹性链 → 审计 → 报告 完整链路。
//
// 启动：
//
//	cd project-agent && go run ./src/cmd/demo
//
// 默认监听 :8090。可通过 -addr 修改。
//
// 演示步骤：
//
//	curl http://localhost:8090/healthz
//	curl -X POST http://localhost:8090/demo/alarm \
//	  -H 'Content-Type: application/json' \
//	  -d '{"alert":"pod-OOMKilled","pod":"game-master-0","ns":"prod"}'
//	curl http://localhost:8090/demo/audit/last
//
// 这份 demo 故意不引入真实下游：所有外部调用走内置的"假装成功"桩，
// 用来证明韧性/审计/熔断三件套本身可独立工作。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"git.woa.com/trpc-go/gameops-agent/pkg/resilience"
)

var (
	flagAddr = flag.String("addr", ":8090", "HTTP 监听地址")
)

// fakeAuditEntry 是 demo 用的最小审计记录。
type fakeAuditEntry struct {
	TS       time.Time              `json:"ts"`
	CaseID   string                 `json:"case_id"`
	Action   string                 `json:"action"`
	Outcome  string                 `json:"outcome"`
	Latency  time.Duration          `json:"latency_ns"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type demoServer struct {
	mu       sync.Mutex
	audits   []fakeAuditEntry
	chain    resilience.Chain
	startup  time.Time
}

func newDemoServer() *demoServer {
	// 组装弹性链：限流 -> 熔断 -> 重试（演示串联效果）。
	rl := resilience.NewRateLimiter(resilience.RateLimitConfig{
		Capacity:      10,
		RatePerSecond: 50,
	})
	br := resilience.NewBreaker(resilience.BreakerConfig{
		Name:                "demo",
		MinRequests:         5,
		FailureRate:         0.6,
		ConsecutiveFailures: 5,
		OpenTimeout:         2 * time.Second,
	})
	chain := resilience.Chain{
		Limiter: rl,
		Breaker: br,
		Retry: &resilience.RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     100 * time.Millisecond,
		},
	}
	return &demoServer{
		chain:   chain,
		startup: time.Now(),
	}
}

func (s *demoServer) handleAlarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	caseID := fmt.Sprintf("demo-%d", time.Now().UnixNano())

	start := time.Now()
	// 跑一次"诊断 → 修复"的弹性链调用（内部桩）。
	err := s.chain.Do(r.Context(), func(ctx context.Context) error {
		// 故意设计 30% 失败概率来触发重试 / 熔断
		if time.Now().UnixNano()%10 < 3 {
			return fmt.Errorf("transient: bcs api 503")
		}
		return nil
	})
	outcome := "success"
	if err != nil {
		outcome = "failed:" + err.Error()
	}

	s.mu.Lock()
	s.audits = append(s.audits, fakeAuditEntry{
		TS:       time.Now(),
		CaseID:   caseID,
		Action:   "diagnose+repair",
		Outcome:  outcome,
		Latency:  time.Since(start),
		Metadata: payload,
	})
	if len(s.audits) > 200 {
		s.audits = s.audits[len(s.audits)-200:]
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"case_id": caseID,
		"outcome": outcome,
		"latency": time.Since(start).String(),
	})
}

func (s *demoServer) handleAuditLast(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if len(s.audits) == 0 {
		_, _ = w.Write([]byte("[]"))
		return
	}
	_ = json.NewEncoder(w).Encode(s.audits[len(s.audits)-1])
}

func (s *demoServer) handleAuditAll(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.audits)
}

func (s *demoServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"uptime":  time.Since(s.startup).String(),
		"audits":  len(s.audits),
		"version": "demo-1.0",
	})
}

func main() {
	flag.Parse()

	srv := newDemoServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", srv.handleHealth)
	mux.HandleFunc("/demo/alarm", srv.handleAlarm)
	mux.HandleFunc("/demo/audit/last", srv.handleAuditLast)
	mux.HandleFunc("/demo/audit", srv.handleAuditAll)

	httpSrv := &http.Server{
		Addr:              *flagAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("[demo] shutting down ...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	log.Printf("🎬 GameOps Agent demo listening on %s", *flagAddr)
	log.Printf("   curl http://localhost%s/healthz", *flagAddr)
	log.Printf("   curl -X POST http://localhost%s/demo/alarm -d '{\"alert\":\"oom\"}'", *flagAddr)
	log.Printf("   curl http://localhost%s/demo/audit/last", *flagAddr)

	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
