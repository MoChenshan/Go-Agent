
package filetools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool/function"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// DetectInput file_detect 入参。
type DetectInput struct {
	Path string `json:"path" description:"文件绝对路径或相对路径（必须在允许根目录下，如 data/samples/xxx.log）"`
}

// DetectOutput file_detect 出参。
type DetectOutput struct {
	Result
	Kind      string   `json:"kind"`                // 文件类型：json / yaml / log / text / binary / unknown
	Ext       string   `json:"ext"`                 // 原始扩展名
	SizeBytes int64    `json:"size_bytes"`          // 文件总字节
	LineCount int      `json:"line_count"`          // 行数（仅文本类文件有意义）
	Preview   string   `json:"preview,omitempty"`   // 前 512 字节预览（可读文本时）
	Hints     []string `json:"hints,omitempty"`     // 给 LLM 的下一步建议
}

// newDetectTool 构造 file_detect 工具。
//
// 识别规则（优先级）：
//  1. 扩展名 → 初判
//  2. 首字节/首行二次确认
//     - `{` 或 `[` 开头 → json
//     - 含 `:` 且行首无引号 → yaml
//     - 含大量 `ERROR/WARN/INFO/DEBUG/FATAL` 或典型时间戳 → log
//     - 否则 text / binary
func newDetectTool(cfg Config) tool.Tool {
	fn := func(_ context.Context, in DetectInput) (*DetectOutput, error) {
		abs, err := resolvePath(cfg, in.Path)
		if err != nil {
			return &DetectOutput{Result: Result{OK: false, Message: err.Error()}}, nil
		}
		// 读取前 8KB 用于判别
		data, _, total, err := readLimited(abs, 0, 8*1024)
		if err != nil {
			return &DetectOutput{Result: Result{OK: false, Message: err.Error()}}, nil
		}

		ext := strings.ToLower(filepath.Ext(abs))
		kind := classify(ext, data)

		// 行数估算（仅针对文本类）
		lineCount := 0
		if kind != "binary" {
			lineCount = bytes.Count(data, []byte{'\n'})
			// 若文件大于采样区间，做近似估算：按字节比例外推
			if total > int64(len(data)) && lineCount > 0 {
				lineCount = int(float64(lineCount) * float64(total) / float64(len(data)))
			}
		}

		preview := ""
		if kind != "binary" {
			pn := 512
			if len(data) < pn {
				pn = len(data)
			}
			preview = string(data[:pn])
		}

		out := &DetectOutput{
			Result:    Result{OK: true},
			Kind:      kind,
			Ext:       ext,
			SizeBytes: total,
			LineCount: lineCount,
			Preview:   preview,
			Hints:     hintsFor(kind),
		}
		return out, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("file_detect"),
		function.WithDescription(
			"识别文件类型并返回基本统计（大小/行数/前 512 字节预览）。"+
				"**分析任何文件前第一步必须调用它**，根据 kind 决定后续使用哪个工具："+
				"kind=json → json_query；kind=log/text → log_analyze；kind=yaml → file_read_slice。"),
	)
}

// classify 判别文件种类。
func classify(ext string, head []byte) string {
	// 二进制判定：前 512 字节中若有 NUL 且非全 UTF8 可读，则视为 binary
	if isBinary(head) {
		return "binary"
	}
	trim := bytes.TrimLeft(head, " \t\r\n")
	switch ext {
	case ".json":
		if json.Valid(trim) || bytes.HasPrefix(trim, []byte("{")) || bytes.HasPrefix(trim, []byte("[")) {
			return "json"
		}
	case ".yaml", ".yml":
		return "yaml"
	case ".log":
		return "log"
	}
	// 扩展名兜不住时按内容猜
	if bytes.HasPrefix(trim, []byte("{")) || bytes.HasPrefix(trim, []byte("[")) {
		if json.Valid(trim) {
			return "json"
		}
	}
	// 日志特征：首 2KB 命中 ERROR/WARN/INFO 等词频 >=2
	upper := bytes.ToUpper(head)
	hits := 0
	for _, kw := range [][]byte{[]byte("ERROR"), []byte("WARN"), []byte("INFO"), []byte("DEBUG"), []byte("FATAL")} {
		hits += bytes.Count(upper, kw)
		if hits >= 2 {
			return "log"
		}
	}
	return "text"
}

// isBinary 简单启发式：前 N 字节中若 NUL 字节占比 > 1%，视为二进制。
func isBinary(head []byte) bool {
	if len(head) == 0 {
		return false
	}
	n := 0
	for _, b := range head {
		if b == 0 {
			n++
		}
	}
	return float64(n)/float64(len(head)) > 0.01
}

func hintsFor(kind string) []string {
	switch kind {
	case "json":
		return []string{
			"建议用 json_query(path=$.xxx) 按 JSONPath 提取关键字段，而不是全量读取。",
		}
	case "yaml":
		return []string{
			"建议用 file_read_slice 分段查看，避免一次加载全文。",
		}
	case "log":
		return []string{
			"建议先调用 log_analyze 获取错误分布与时间聚集。",
			"定位到可疑时间窗后，再用 file_read_slice 读取对应行区间。",
		}
	case "text":
		return []string{
			"通用文本；若怀疑是日志，直接尝试 log_analyze。",
		}
	case "binary":
		return []string{
			fmt.Sprintf("疑似二进制文件，不适合文本分析；请让用户确认文件来源。"),
		}
	}
	return nil
}
