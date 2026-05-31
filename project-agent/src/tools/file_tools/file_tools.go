
// Package filetools 实现 FileAnalystAgent 所需的本地文件分析工具。
//
// 设计思路：
//   - 纯本地计算工具，不依赖外部 API，也不需要凭据
//   - 出于安全考虑：
//     * 所有路径必须位于 FILE_ANALYZE_ROOT 下（默认 data/samples/ 与 /tmp/）
//     * 单次读取字节数有硬上限（默认 1 MiB），防止 OOM
//     * 不允许追踪软链接跳出 root
//   - 工具集合：
//     * file_detect    — 文件类型识别 + 基本统计
//     * file_read_slice — 分段读取（offset/size）
//     * json_query     — JSON 按路径查询
//     * log_analyze    — 日志错误分布 + 时间聚集 + 高频模式
//
// 未来（D7+）接入 Skills 系统后，可将这里的能力升级为 Skill：
//   - log_pattern   日志模板挖掘（drain3 风格）
//   - csv_compare   CSV 对比（性能报告）
package filetools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/tools"
)

// Target 工具分组名，所有 Agent 默认都可以调用（类似 util_tools 的地位）。
// 在 targeted 分发时，file-local 通常被所有 Agent 纳入白名单。
const Target = "file-local"

// Result 是所有 file 工具统一的返回结构。
type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Config 包级配置。
type Config struct {
	// AllowRoots 允许访问的根目录白名单。路径必须在任一根之下才会被处理。
	// 默认：["data/samples", os.TempDir()]
	AllowRoots []string
	// MaxReadBytes 单次读取/分析的字节数硬上限（默认 1 MiB）。
	MaxReadBytes int64
}

// DefaultConfig 从环境变量加载。
func DefaultConfig() Config {
	c := Config{
		AllowRoots:   []string{filepath.Clean("data/samples"), os.TempDir()},
		MaxReadBytes: 1 << 20, // 1 MiB
	}
	if v := os.Getenv("FILE_ANALYZE_ROOT"); v != "" {
		// 允许多根，以系统路径分隔符或分号切分
		parts := strings.FieldsFunc(v, func(r rune) bool {
			return r == ';' || r == os.PathListSeparator
		})
		if len(parts) > 0 {
			roots := make([]string, 0, len(parts))
			for _, p := range parts {
				roots = append(roots, filepath.Clean(p))
			}
			c.AllowRoots = roots
		}
	}
	return c
}

// NewAll 返回所有文件分析工具。
func NewAll(cfg Config) []tool.Tool {
	if cfg.MaxReadBytes <= 0 {
		cfg = DefaultConfig()
	}
	return []tool.Tool{
		newDetectTool(cfg),
		newReadSliceTool(cfg),
		newJSONQueryTool(cfg),
		newLogAnalyzeTool(cfg),
	}
}

// NewAllTargeted 返回带 target 的版本，target=file-local。
func NewAllTargeted(cfg Config) []tools.TargetedTool {
	all := NewAll(cfg)
	out := make([]tools.TargetedTool, 0, len(all))
	for _, t := range all {
		out = append(out, tools.TargetedTool{Target: Target, Tool: t})
	}
	return out
}

// resolvePath 校验并规范化用户传入路径：
//  1. Clean + Abs
//  2. 必须落在 AllowRoots 任一根之下
//  3. 不允许是目录
//
// 返回校验后的绝对路径。
func resolvePath(cfg Config, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path 不能为空")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve abs: %w", err)
	}
	abs = filepath.Clean(abs)

	// 白名单校验
	allowed := false
	for _, root := range cfg.AllowRoots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(rel, "..") && rel != "" {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("路径 %q 不在允许访问的白名单内（允许根：%v）", p, cfg.AllowRoots)
	}

	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", p, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("%q 是目录，file 工具只支持文件", p)
	}
	return abs, nil
}

// readLimited 从 path 读取最多 limit 字节；超过则截断并标记 truncated。
func readLimited(path string, offset, limit int64) (data []byte, truncated bool, total int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, 0, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, false, 0, err
	}
	total = st.Size()

	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []byte{}, false, total, nil
	}

	if _, err := f.Seek(offset, 0); err != nil {
		return nil, false, total, err
	}

	remain := total - offset
	n := remain
	if limit > 0 && n > limit {
		n = limit
		truncated = true
	}
	buf := make([]byte, n)
	if _, err := f.Read(buf); err != nil {
		return nil, false, total, err
	}
	return buf, truncated, total, nil
}
