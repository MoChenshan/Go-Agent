
package filetools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// LogAnalyzeInput log_analyze 入参。
type LogAnalyzeInput struct {
	Path       string `json:"path" description:"日志文件路径（须在允许根目录下）"`
	TopK       int    `json:"top_k,omitempty" description:"高频模式返回前 K 条，默认 5，上限 20"`
	MaxLines   int    `json:"max_lines,omitempty" description:"最多分析多少行，默认 5000，上限 100000"`
	IncludeRaw bool   `json:"include_raw,omitempty" description:"是否返回每个聚集时间窗的代表行（默认 true）"`
}

// LogAnalyzeOutput log_analyze 出参。
type LogAnalyzeOutput struct {
	Result
	TotalLines   int                     `json:"total_lines"`
	Analyzed     int                     `json:"analyzed_lines"`
	LevelCount   map[string]int          `json:"level_count"`
	TopPatterns  []LogPattern            `json:"top_patterns"`
	TimeBuckets  []LogTimeBucket         `json:"time_buckets,omitempty"`
	FirstError   *LogLineRef             `json:"first_error,omitempty"`
	LastError    *LogLineRef             `json:"last_error,omitempty"`
	Hints        []string                `json:"hints"`
}

// LogPattern 高频错误模式。
type LogPattern struct {
	Pattern  string `json:"pattern"`  // 规范化后的模式（数字/路径/ID 被替换为 *）
	Count    int    `json:"count"`    // 出现次数
	Sample   string `json:"sample"`   // 一条原始样例
	FirstLine int   `json:"first_line"` // 首次出现行号
}

// LogTimeBucket 时间聚集桶（每分钟一个桶）。
type LogTimeBucket struct {
	Minute   string `json:"minute"`   // 如 "2026-04-20 10:15"
	Total    int    `json:"total"`    // 桶内总错误数
	SampleLine string `json:"sample_line,omitempty"`
}

// LogLineRef 日志行引用。
type LogLineRef struct {
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// levelRe 捕获日志级别（匹配常见框架）：
//   logrus / zap / log4j / glog / klog / k8s event / trpc-go
var levelRe = regexp.MustCompile(`\b(FATAL|ERROR|WARN(?:ING)?|INFO|DEBUG|TRACE)\b`)

// timeRe 捕获时间戳（精确到分钟）：
//   2026-04-20 10:15:30 / 2026-04-20T10:15:30 / 2026/04/20 10:15:30
var timeRe = regexp.MustCompile(`(\d{4}[-/]\d{2}[-/]\d{2})[ T](\d{2}:\d{2})`)

// normalizeRes 用于生成模式：数字 / UUID / 十六进制地址 / 路径 → 占位符
var (
	reUUID   = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reHex    = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	reIP     = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(?::\d+)?\b`)
	reNumber = regexp.MustCompile(`\b\d{2,}\b`)
	rePath   = regexp.MustCompile(`(?:/[\w.\-]+){2,}`)
)

// newLogAnalyzeTool 构造 log_analyze。
//
// 核心算法：
//  1. 逐行扫描（最多 MaxLines），命中 ERROR/WARN/FATAL 的行纳入统计
//  2. 规范化（mask 数字/UUID/IP/路径）后做计数 → Top-K 频次
//  3. 按分钟聚合时间戳，找错误集中窗口
//  4. 记录 First/Last Error 锚点，便于 LLM 继续用 file_read_slice 读上下文
func newLogAnalyzeTool(cfg Config) tool.Tool {
	fn := func(_ context.Context, in LogAnalyzeInput) (*LogAnalyzeOutput, error) {
		abs, err := resolvePath(cfg, in.Path)
		if err != nil {
			return &LogAnalyzeOutput{Result: Result{OK: false, Message: err.Error()}}, nil
		}
		topK := clamp(in.TopK, 5, 20)
		maxLines := clamp(in.MaxLines, 5000, 100000)
		// in.IncludeRaw 当前默认附 sample；预留给未来做超大文件只返回计数的精简模式。
		_ = in.IncludeRaw

		f, err := os.Open(abs)
		if err != nil {
			return &LogAnalyzeOutput{Result: Result{OK: false, Message: err.Error()}}, nil
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

		levelCount := map[string]int{}
		patternHit := map[string]*LogPattern{}
		timeBucket := map[string]*LogTimeBucket{}
		var firstErr, lastErr *LogLineRef

		totalLines := 0
		analyzed := 0
		for sc.Scan() {
			totalLines++
			if totalLines > maxLines {
				// 仍可以继续计 totalLines 但不再分析，性能考虑直接 break
				totalLines = maxLines
				break
			}
			line := sc.Text()
			if line == "" {
				continue
			}

			// 级别
			level := ""
			if m := levelRe.FindStringSubmatch(line); m != nil {
				level = strings.ToUpper(m[1])
				if level == "WARNING" {
					level = "WARN"
				}
				levelCount[level]++
			}

			// 只对错误类聚合
			isError := level == "ERROR" || level == "FATAL"
			if !isError {
				// 兜底：行里包含典型错误关键字
				up := strings.ToUpper(line)
				if strings.Contains(up, "PANIC") || strings.Contains(up, "EXCEPTION") || strings.Contains(up, "OOMKILLED") {
					level = "ERROR"
					levelCount["ERROR"]++
					isError = true
				}
			}
			if !isError {
				continue
			}
			analyzed++

			// 首次 / 末次错误
			ref := &LogLineRef{Line: totalLines, Content: truncate(line, 500)}
			if firstErr == nil {
				firstErr = ref
			}
			lastErr = ref

			// 模式聚合
			pat := normalizePattern(line)
			if p, ok := patternHit[pat]; ok {
				p.Count++
			} else {
				patternHit[pat] = &LogPattern{
					Pattern:   truncate(pat, 300),
					Count:     1,
					Sample:    truncate(line, 500),
					FirstLine: totalLines,
				}
			}

			// 时间桶聚合
			if tm := timeRe.FindStringSubmatch(line); tm != nil {
				key := fmt.Sprintf("%s %s", strings.ReplaceAll(tm[1], "/", "-"), tm[2])
				if b, ok := timeBucket[key]; ok {
					b.Total++
				} else {
					timeBucket[key] = &LogTimeBucket{
						Minute:     key,
						Total:      1,
						SampleLine: truncate(line, 300),
					}
				}
			}
		}
		if err := sc.Err(); err != nil {
			return &LogAnalyzeOutput{Result: Result{OK: false, Message: err.Error()}}, nil
		}

		// Top-K 模式
		patterns := make([]LogPattern, 0, len(patternHit))
		for _, p := range patternHit {
			patterns = append(patterns, *p)
		}
		sort.Slice(patterns, func(i, j int) bool { return patterns[i].Count > patterns[j].Count })
		if len(patterns) > topK {
			patterns = patterns[:topK]
		}

		// 时间桶按错误数倒序，再按时间升序兜底
		buckets := make([]LogTimeBucket, 0, len(timeBucket))
		for _, b := range timeBucket {
			buckets = append(buckets, *b)
		}
		sort.Slice(buckets, func(i, j int) bool {
			if buckets[i].Total != buckets[j].Total {
				return buckets[i].Total > buckets[j].Total
			}
			return buckets[i].Minute < buckets[j].Minute
		})
		if len(buckets) > 10 {
			buckets = buckets[:10]
		}

		return &LogAnalyzeOutput{
			Result:      Result{OK: true},
			TotalLines:  totalLines,
			Analyzed:    analyzed,
			LevelCount:  levelCount,
			TopPatterns: patterns,
			TimeBuckets: buckets,
			FirstError:  firstErr,
			LastError:   lastErr,
			Hints:       buildLogHints(levelCount, patterns, buckets),
		}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("log_analyze"),
		function.WithDescription(
			"扫描日志文件，统计错误级别分布、时间聚集窗口、高频错误模式。"+
				"适用场景：用户上传 .log / .txt 运维日志后第一步深度分析工具。"+
				"产出：level_count（各级别计数）、top_patterns（去噪后的高频模式）、"+
				"time_buckets（错误集中的分钟桶）、first/last_error（行号锚点，供 file_read_slice 定位上下文）。"),
	)
}

// normalizePattern 对单行日志做去噪，生成一个稳定的 pattern 作为聚合 key。
func normalizePattern(s string) string {
	s = reUUID.ReplaceAllString(s, "<uuid>")
	s = reHex.ReplaceAllString(s, "<hex>")
	s = reIP.ReplaceAllString(s, "<ip>")
	s = rePath.ReplaceAllString(s, "<path>")
	s = timeRe.ReplaceAllString(s, "<time>")
	s = reNumber.ReplaceAllString(s, "<n>")
	// 折叠连续空白
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

func buildLogHints(levels map[string]int, patterns []LogPattern, buckets []LogTimeBucket) []string {
	var hints []string
	if levels["FATAL"] > 0 {
		hints = append(hints, fmt.Sprintf("存在 %d 条 FATAL 级别日志，强烈建议优先查看 first_error/last_error 指向的行。", levels["FATAL"]))
	}
	if levels["ERROR"] > 100 {
		hints = append(hints, "ERROR 数量很高，建议聚焦 top_patterns[0] 的模式深入定位。")
	}
	if len(buckets) > 0 && buckets[0].Total >= 5 {
		hints = append(hints, fmt.Sprintf("时间窗 %s 错误集中（%d 条），可用 file_read_slice 读取该时段附近行上下文。", buckets[0].Minute, buckets[0].Total))
	}
	if len(patterns) == 0 {
		hints = append(hints, "未发现错误级别日志；若用户认为有异常，可切换到 file_read_slice 按关键字过滤。")
	} else {
		hints = append(hints, "若需转到 DiagnosisAgent 查监控指标，可使用 time_buckets[0].minute 作为查询时间窗。")
	}
	return hints
}
