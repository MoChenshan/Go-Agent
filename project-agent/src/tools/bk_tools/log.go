
// 蓝鲸监控-日志查询（bk-log）。
//
// 对接蓝鲸日志平台：POST /api/bk-log/prod/search/
package bktools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
)

// LogInput bk_log_query 工具入参。
type LogInput struct {
	BKBizID   int    `json:"bk_biz_id"   description:"蓝鲸业务 ID（必填）"`
	IndexSet  string `json:"index_set"   description:"索引集 ID 或名称（必填）"`
	Query     string `json:"query"       description:"查询语句（KQL / Lucene 语法），如 'level:ERROR AND host:1.2.3.4'"`
	StartTime string `json:"start_time"  description:"开始时间，RFC3339"`
	EndTime   string `json:"end_time"    description:"结束时间"`
	Size      int    `json:"size"        description:"返回条数上限，默认 50，最大 500"`
}

// newLogTool 构造 bk_log_query 工具。
func newLogTool(client *bkapi.Client) tool.Tool {
	fn := func(ctx context.Context, in LogInput) (*Result, error) {
		if in.BKBizID == 0 || in.IndexSet == "" {
			return nil, fmt.Errorf("bk_biz_id 和 index_set 为必填项")
		}
		size := in.Size
		if size <= 0 {
			size = 50
		}
		if size > 500 {
			size = 500
		}

		reqBody := map[string]any{
			"bk_biz_id":     in.BKBizID,
			"index_set_id":  in.IndexSet,
			"keyword":       in.Query,
			"start_time":    in.StartTime,
			"end_time":      in.EndTime,
			"size":          size,
			"sort_list":     [][]string{{"@timestamp", "desc"}},
		}

		var respData map[string]any
		err := client.PostJSON(ctx, "/api/bk-log/prod/search/", reqBody, &respData)
		if errors.Is(err, bkapi.ErrMockMode) {
			return mockLog(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("调用蓝鲸日志查询失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bk_log_query"),
		function.WithDescription("查询蓝鲸日志平台的应用/系统日志。适用场景：定位某时间段的 ERROR/WARN 日志、按关键字搜索异常堆栈。"),
	)
}

// mockLog 返回预置日志样例数据。
func mockLog(in LogInput) *Result {
	now := time.Now()
	hits := []map[string]any{
		{
			"timestamp": now.Add(-2 * time.Minute).Format(time.RFC3339),
			"level":     "ERROR",
			"host":      "10.1.1.100",
			"message":   "connection reset by peer: redis://game-cache:6379 (mock)",
		},
		{
			"timestamp": now.Add(-5 * time.Minute).Format(time.RFC3339),
			"level":     "WARN",
			"host":      "10.1.1.100",
			"message":   "slow query detected, latency=1.2s (mock)",
		},
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式",
		Data: map[string]any{
			"index_set": in.IndexSet,
			"query":     in.Query,
			"total":     len(hits),
			"hits":      hits,
		},
	}
}
