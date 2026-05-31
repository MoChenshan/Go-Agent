
package filetools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// JSONQueryInput json_query 入参。
type JSONQueryInput struct {
	Path  string `json:"path" description:"JSON 文件路径（须在允许根目录下）"`
	Query string `json:"query" description:"类 JSONPath 表达式，例如 $.status.phase 或 $.items[0].metadata.name；留空则返回顶层 keys"`
}

// JSONQueryOutput json_query 出参。
type JSONQueryOutput struct {
	Result
	Query   string   `json:"query"`
	Value   any      `json:"value,omitempty"`   // 命中时的值
	Exists  bool     `json:"exists"`            // 是否命中
	TopKeys []string `json:"top_keys,omitempty"` // 未给 query 时返回顶层 keys
}

// newJSONQueryTool 构造 json_query。
//
// 仅支持 dot + [index] 两种分段，覆盖 95% 的运维查询场景：
//   - $.status.phase
//   - $.spec.containers[0].image
//   - items[3].name（允许省略 $）
func newJSONQueryTool(cfg Config) tool.Tool {
	fn := func(_ context.Context, in JSONQueryInput) (*JSONQueryOutput, error) {
		abs, err := resolvePath(cfg, in.Path)
		if err != nil {
			return &JSONQueryOutput{Result: Result{OK: false, Message: err.Error()}}, nil
		}
		data, truncated, _, err := readLimited(abs, 0, cfg.MaxReadBytes)
		if err != nil {
			return &JSONQueryOutput{Result: Result{OK: false, Message: err.Error()}}, nil
		}
		if truncated {
			return &JSONQueryOutput{Result: Result{
				OK:      false,
				Message: fmt.Sprintf("JSON 文件超出单次处理上限 %d 字节，无法完整解析；请先用 file_read_slice 分段处理。", cfg.MaxReadBytes),
			}}, nil
		}

		var root any
		if err := json.Unmarshal(data, &root); err != nil {
			return &JSONQueryOutput{Result: Result{OK: false, Message: fmt.Sprintf("JSON 解析失败：%v", err)}}, nil
		}

		if strings.TrimSpace(in.Query) == "" {
			return &JSONQueryOutput{
				Result:  Result{OK: true},
				Query:   "",
				Exists:  true,
				TopKeys: topKeys(root),
			}, nil
		}

		val, ok := jsonWalk(root, in.Query)
		return &JSONQueryOutput{
			Result: Result{OK: true},
			Query:  in.Query,
			Value:  val,
			Exists: ok,
		}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("json_query"),
		function.WithDescription(
			"从 JSON 文件按 JSONPath-lite 提取字段。"+
				"表达式支持 $.a.b[0].c 形式（$ 可省略）；query 留空返回顶层 keys 方便探索结构。"+
				"适用场景：K8s 资源 YAML/JSON、蓝鲸告警 dump、压测结果报告。"),
	)
}

// topKeys 取顶层 key 列表；若顶层是数组，返回 "[len=N]"。
func topKeys(v any) []string {
	switch t := v.(type) {
	case map[string]any:
		ks := make([]string, 0, len(t))
		for k := range t {
			ks = append(ks, k)
		}
		return ks
	case []any:
		return []string{fmt.Sprintf("[len=%d]", len(t))}
	}
	return nil
}

// segRe 匹配单个路径段：key 或 key[N] 或 [N]
var segRe = regexp.MustCompile(`^([A-Za-z_][\w\-]*)?((?:\[\d+\])*)$`)

// jsonWalk 按表达式走 root。
func jsonWalk(root any, expr string) (any, bool) {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, "$")
	expr = strings.TrimPrefix(expr, ".")
	if expr == "" {
		return root, true
	}
	cur := root
	for _, seg := range strings.Split(expr, ".") {
		m := segRe.FindStringSubmatch(seg)
		if m == nil {
			return nil, false
		}
		// 先走 key
		if key := m[1]; key != "" {
			mp, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			val, exists := mp[key]
			if !exists {
				return nil, false
			}
			cur = val
		}
		// 再走 [N] 可能有多个
		if idx := m[2]; idx != "" {
			for _, part := range strings.Split(strings.Trim(idx, "[]"), "][") {
				n, err := strconv.Atoi(part)
				if err != nil {
					return nil, false
				}
				arr, ok := cur.([]any)
				if !ok || n < 0 || n >= len(arr) {
					return nil, false
				}
				cur = arr[n]
			}
		}
	}
	return cur, true
}
