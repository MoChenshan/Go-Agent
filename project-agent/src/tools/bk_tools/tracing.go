
// 蓝鲸监控-APM Trace 查询（bk-tracing）。
//
// 对接蓝鲸 APM：POST /api/bk-monitor/prod/apm/trace/query/
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

// TracingInput bk_tracing_query 工具入参。
type TracingInput struct {
	BKBizID     int    `json:"bk_biz_id"     description:"蓝鲸业务 ID（必填）"`
	AppName     string `json:"app_name"      description:"APM 应用名（必填），如 'letsgo-game-core'"`
	TraceID     string `json:"trace_id"      description:"指定 TraceID 直接查询单条全链路；若为空则按条件查询 Trace 列表"`
	ServiceName string `json:"service_name"  description:"服务名过滤"`
	MinDuration int    `json:"min_duration_ms" description:"最小耗时过滤（毫秒），用于筛选慢请求"`
	HasError    bool   `json:"has_error"     description:"是否只返回包含 Error 的 Trace"`
	StartTime   string `json:"start_time"    description:"开始时间"`
	EndTime     string `json:"end_time"      description:"结束时间"`
	Size        int    `json:"size"          description:"返回条数，默认 20"`
}

// newTracingTool 构造 bk_tracing_query 工具。
func newTracingTool(client *bkapi.Client) tool.Tool {
	fn := func(ctx context.Context, in TracingInput) (*Result, error) {
		if in.BKBizID == 0 || in.AppName == "" {
			return nil, fmt.Errorf("bk_biz_id 和 app_name 为必填项")
		}
		size := in.Size
		if size <= 0 {
			size = 20
		}

		reqBody := map[string]any{
			"bk_biz_id":       in.BKBizID,
			"app_name":        in.AppName,
			"trace_id":        in.TraceID,
			"service_name":    in.ServiceName,
			"min_duration_ms": in.MinDuration,
			"has_error":       in.HasError,
			"start_time":      in.StartTime,
			"end_time":        in.EndTime,
			"size":            size,
		}

		var respData map[string]any
		err := client.PostJSON(ctx, "/api/bk-monitor/prod/apm/trace/query/", reqBody, &respData)
		if errors.Is(err, bkapi.ErrMockMode) {
			return mockTracing(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("调用蓝鲸 APM Trace 查询失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bk_tracing_query"),
		function.WithDescription("查询蓝鲸 APM 分布式链路追踪（Trace / Span）。适用场景：根据 TraceID 定位慢请求、错误链路、跨服务调用瓶颈。"),
	)
}

// mockTracing 返回预置 Trace 样例数据。
func mockTracing(in TracingInput) *Result {
	now := time.Now()
	traces := []map[string]any{
		{
			"trace_id":       "mock-trace-7c9b",
			"root_service":   firstNonEmpty(in.ServiceName, in.AppName),
			"duration_ms":    1820,
			"status":         "ERROR",
			"start_time":     now.Add(-3 * time.Minute).Format(time.RFC3339),
			"span_count":     12,
			"error_span":     "game-core -> redis GET latency=1.6s, timeout",
		},
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式",
		Data: map[string]any{
			"app_name": in.AppName,
			"total":    len(traces),
			"traces":   traces,
		},
	}
}
