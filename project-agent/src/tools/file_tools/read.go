
package filetools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ReadSliceInput file_read_slice 入参。
type ReadSliceInput struct {
	Path      string `json:"path" description:"文件路径（须在允许根目录下）"`
	Mode      string `json:"mode,omitempty" description:"读取模式：byte（按字节） 或 line（按行），默认 line"`
	Offset    int64  `json:"offset,omitempty" description:"起始偏移（byte 模式：字节；line 模式：行号，从 1 起）"`
	Size      int64  `json:"size,omitempty" description:"读取大小（byte 模式：字节数；line 模式：行数），默认 200 行 / 4KB，上限受 MaxReadBytes"`
	Keyword   string `json:"keyword,omitempty" description:"可选：只返回包含该关键词的行（大小写敏感，仅 line 模式生效）"`
}

// ReadSliceOutput file_read_slice 出参。
type ReadSliceOutput struct {
	Result
	Mode       string `json:"mode"`
	Offset     int64  `json:"offset"`
	Size       int64  `json:"size"`
	TotalBytes int64  `json:"total_bytes"`
	TotalLines int    `json:"total_lines,omitempty"`
	Truncated  bool   `json:"truncated"`
	Content    string `json:"content"`
}

// newReadSliceTool 安全读取文件的指定片段。
//
// 说明：
//   - byte 模式：读取 [offset, offset+size)，适合二进制结构前缀探测 / 超长单行 JSON。
//   - line 模式：跳过前 Offset-1 行，读取随后 Size 行，适合日志、YAML。
//   - Keyword 仅对 line 模式生效，作为轻量 grep 使用。
func newReadSliceTool(cfg Config) tool.Tool {
	fn := func(_ context.Context, in ReadSliceInput) (*ReadSliceOutput, error) {
		abs, err := resolvePath(cfg, in.Path)
		if err != nil {
			return &ReadSliceOutput{Result: Result{OK: false, Message: err.Error()}}, nil
		}
		mode := in.Mode
		if mode == "" {
			mode = "line"
		}
		switch mode {
		case "byte":
			return readByte(cfg, abs, in)
		case "line":
			return readLine(cfg, abs, in)
		default:
			return &ReadSliceOutput{Result: Result{OK: false, Message: fmt.Sprintf("不支持的 mode: %s", mode)}}, nil
		}
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("file_read_slice"),
		function.WithDescription(
			"按字节或按行读取文件片段，带硬上限（默认 1 MiB），防止 OOM。"+
				"适用场景：已由 file_detect 或 log_analyze 定位到可疑区间后，精确读取上下文。"+
				"line 模式可配合 keyword 做轻量过滤。"),
	)
}

func readByte(cfg Config, abs string, in ReadSliceInput) (*ReadSliceOutput, error) {
	limit := in.Size
	if limit <= 0 {
		limit = 4 * 1024
	}
	if limit > cfg.MaxReadBytes {
		limit = cfg.MaxReadBytes
	}
	data, truncated, total, err := readLimited(abs, in.Offset, limit)
	if err != nil {
		return &ReadSliceOutput{Result: Result{OK: false, Message: err.Error()}}, nil
	}
	return &ReadSliceOutput{
		Result:     Result{OK: true},
		Mode:       "byte",
		Offset:     in.Offset,
		Size:       int64(len(data)),
		TotalBytes: total,
		Truncated:  truncated,
		Content:    string(data),
	}, nil
}

func readLine(cfg Config, abs string, in ReadSliceInput) (*ReadSliceOutput, error) {
	startLine := in.Offset
	if startLine < 1 {
		startLine = 1
	}
	want := in.Size
	if want <= 0 {
		want = 200
	}

	f, err := os.Open(abs)
	if err != nil {
		return &ReadSliceOutput{Result: Result{OK: false, Message: err.Error()}}, nil
	}
	defer f.Close()

	st, _ := f.Stat()
	totalBytes := int64(0)
	if st != nil {
		totalBytes = st.Size()
	}

	sc := bufio.NewScanner(f)
	// 扩大单行 buffer，避免超长日志行报错
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		buf        bytes.Buffer
		lineNo     int64
		emitted    int64
		totalLines int
		truncated  bool
	)
	for sc.Scan() {
		lineNo++
		totalLines++
		if lineNo < startLine {
			continue
		}
		if emitted >= want {
			// 仍继续计数 totalLines 以便返回文件总行数
			continue
		}
		line := sc.Text()
		if in.Keyword != "" && !bytes.Contains([]byte(line), []byte(in.Keyword)) {
			continue
		}
		if int64(buf.Len())+int64(len(line)) > cfg.MaxReadBytes {
			truncated = true
			break
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		emitted++
	}
	if err := sc.Err(); err != nil {
		return &ReadSliceOutput{Result: Result{OK: false, Message: err.Error()}}, nil
	}

	return &ReadSliceOutput{
		Result:     Result{OK: true},
		Mode:       "line",
		Offset:     startLine,
		Size:       emitted,
		TotalBytes: totalBytes,
		TotalLines: totalLines,
		Truncated:  truncated,
		Content:    buf.String(),
	}, nil
}
