// Package main 是 GameOps Agent 的"对话回放器"。
//
// 用途：
//   - 喂入一份历史对话（jsonl，每行一个 case），离线跑一遍 Agent，
//     输出每步的工具选择、响应、judge 分数，便于回归 / A/B / 离线评测。
//
// 输入 jsonl 字段：
//
//	{
//	  "case_id":   "alarm-001",
//	  "user_input":"PodOOMKilled，namespace=prod",
//	  "context":   { ... 任意业务字段 ... },
//	  "expected":  "应该至少调用 bcs.list_pods 与 logs.search 两个工具"
//	}
//
// 输出：
//   - <out>/cases.json      所有 case 的执行记录
//   - <out>/summary.md      汇总（成功率、tool 命中率、平均延迟）
//
// 运行：
//
//	go run ./src/cmd/replay -in eval/data/replay_cases.jsonl -out /tmp/replay
//
// 注意：本工具内置 mock entrance，零外部依赖即可运行；如需对接真实 Agent，
//      请用 -url http://localhost:8080/v1/agent 模式（HTTP 调用）。
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type replayCase struct {
	CaseID    string                 `json:"case_id"`
	UserInput string                 `json:"user_input"`
	Context   map[string]interface{} `json:"context,omitempty"`
	Expected  string                 `json:"expected,omitempty"`
}

type replayResult struct {
	CaseID      string        `json:"case_id"`
	UserInput   string        `json:"user_input"`
	Output      string        `json:"output"`
	ToolCalls   []string      `json:"tool_calls"`
	Latency     time.Duration `json:"latency_ns"`
	Status      string        `json:"status"`
	JudgeScore  float64       `json:"judge_score"`
	JudgeReason string        `json:"judge_reason,omitempty"`
}

var (
	flagIn  = flag.String("in", "", "jsonl 输入路径")
	flagOut = flag.String("out", "/tmp/replay", "输出目录")
	flagURL = flag.String("url", "", "若提供则 HTTP 调用真实 Agent；否则用内置 mock")
)

func main() {
	flag.Parse()
	if *flagIn == "" {
		fmt.Fprintln(os.Stderr, "用法：replay -in cases.jsonl [-url http://...] -out /tmp/replay")
		os.Exit(2)
	}
	if err := os.MkdirAll(*flagOut, 0o755); err != nil {
		exitf("mkdir: %v", err)
	}

	cases, err := loadCases(*flagIn)
	if err != nil {
		exitf("loadCases: %v", err)
	}
	fmt.Printf("[replay] %d cases from %s\n", len(cases), *flagIn)

	results := make([]replayResult, 0, len(cases))
	for i, c := range cases {
		var r replayResult
		if *flagURL != "" {
			r = runHTTP(c, *flagURL)
		} else {
			r = runMock(c)
		}
		fmt.Printf("[%d/%d] %s [%s] tools=%v latency=%s\n",
			i+1, len(cases), c.CaseID, r.Status, r.ToolCalls, r.Latency)
		results = append(results, r)
	}

	if err := writeJSON(filepath.Join(*flagOut, "cases.json"), results); err != nil {
		exitf("writeJSON: %v", err)
	}
	if err := writeSummary(filepath.Join(*flagOut, "summary.md"), results); err != nil {
		exitf("writeSummary: %v", err)
	}
	fmt.Printf("\n✅ replay done\n   cases.json   → %s/cases.json\n   summary.md   → %s/summary.md\n", *flagOut, *flagOut)
}

func loadCases(path string) ([]replayCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []replayCase
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c replayCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("parse %q: %w", line, err)
		}
		out = append(out, c)
	}
	return out, scanner.Err()
}

// runMock 内置最简 mock：基于关键字猜 tool。
func runMock(c replayCase) replayResult {
	start := time.Now()
	var tools []string
	low := strings.ToLower(c.UserInput)
	if strings.Contains(low, "pod") || strings.Contains(low, "oom") {
		tools = append(tools, "bcs.list_pods", "logs.search")
	}
	if strings.Contains(low, "tapd") || strings.Contains(low, "bug") {
		tools = append(tools, "tapd.search")
	}
	if strings.Contains(low, "wiki") || strings.Contains(low, "doc") {
		tools = append(tools, "iwiki.search")
	}
	if len(tools) == 0 {
		tools = []string{"llm.directly_answer"}
	}
	score := 1.0
	reason := "mock judge: 至少命中一个工具"
	if c.Expected != "" {
		hit := 0
		for _, t := range tools {
			if strings.Contains(c.Expected, t) {
				hit++
			}
		}
		score = float64(hit) / float64(maxInt(1, strings.Count(c.Expected, ",")+1))
		reason = fmt.Sprintf("expected hit ratio=%.2f", score)
	}
	return replayResult{
		CaseID:      c.CaseID,
		UserInput:   c.UserInput,
		Output:      fmt.Sprintf("[mock answer for %s]", c.CaseID),
		ToolCalls:   tools,
		Latency:     time.Since(start),
		Status:      "success",
		JudgeScore:  score,
		JudgeReason: reason,
	}
}

// runHTTP 通过真实 Agent HTTP 入口 /v1/agent 调用。
func runHTTP(c replayCase, url string) replayResult {
	start := time.Now()
	body, _ := json.Marshal(map[string]interface{}{
		"session_id": "replay-" + c.CaseID,
		"user_id":    "replay",
		"input":      c.UserInput,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return replayResult{CaseID: c.CaseID, UserInput: c.UserInput, Status: "error:" + err.Error(), Latency: time.Since(start)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return replayResult{
		CaseID:    c.CaseID,
		UserInput: c.UserInput,
		Output:    string(raw),
		Status:    resp.Status,
		Latency:   time.Since(start),
	}
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeSummary(path string, rs []replayResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var (
		total       = len(rs)
		ok          = 0
		latencies   = make([]time.Duration, 0, len(rs))
		toolCount   = map[string]int{}
		scoreSum    float64
	)
	for _, r := range rs {
		if strings.HasPrefix(r.Status, "success") || r.Status == "200 OK" {
			ok++
		}
		latencies = append(latencies, r.Latency)
		for _, t := range r.ToolCalls {
			toolCount[t]++
		}
		scoreSum += r.JudgeScore
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := pick(latencies, 50)
	p95 := pick(latencies, 95)

	fmt.Fprintf(f, "# Replay Summary\n\n")
	fmt.Fprintf(f, "- 总用例：%d\n", total)
	fmt.Fprintf(f, "- 成功率：%d/%d = %.1f%%\n", ok, total, 100*float64(ok)/float64(maxInt(1, total)))
	fmt.Fprintf(f, "- 平均 judge 分数：%.3f\n", scoreSum/float64(maxInt(1, total)))
	fmt.Fprintf(f, "- 延迟 P50/P95：%s / %s\n\n", p50, p95)
	fmt.Fprintf(f, "## 工具命中频率\n\n")
	type kv struct {
		k string
		v int
	}
	var tcs []kv
	for k, v := range toolCount {
		tcs = append(tcs, kv{k, v})
	}
	sort.Slice(tcs, func(i, j int) bool { return tcs[i].v > tcs[j].v })
	for _, x := range tcs {
		fmt.Fprintf(f, "- `%s`: %d\n", x.k, x.v)
	}
	return nil
}

func pick(xs []time.Duration, p int) time.Duration {
	if len(xs) == 0 {
		return 0
	}
	idx := (len(xs) - 1) * p / 100
	return xs[idx]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func exitf(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
