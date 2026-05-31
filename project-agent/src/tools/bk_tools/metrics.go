
// 蓝鲸监控-指标查询（bk-metrics）。
//
// 对接蓝鲸监控 API：POST /api/bk-monitor/prod/time_series/unify_query/
// 真实字段以 BK-Monitor OpenAPI 文档为准，此处使用通用的 data_label+method+where 模式。
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

// MetricsInput bk_metrics_query 工具入参。
type MetricsInput struct {
	BKBizID    int    `json:"bk_biz_id"    description:"蓝鲸业务 ID（必填）"`
	DataLabel  string `json:"data_label"   description:"指标分类 data_label，如 'system' / 'process'（必填）"`
	MetricName string `json:"metric_name"  description:"指标名，如 'cpu_usage' / 'mem_usage'（必填）"`
	Method     string `json:"method"       description:"聚合函数：MEAN / SUM / MAX / MIN / COUNT，默认 MEAN"`
	StartTime  string `json:"start_time"   description:"开始时间，RFC3339 或 '2026-01-02 15:04:05'"`
	EndTime    string `json:"end_time"     description:"结束时间，格式同 start_time"`
	IntervalS  int    `json:"interval_sec" description:"采样粒度（秒），默认 60"`
	// Where 过滤条件，map[field]value，例如 {"bk_target_ip":"1.2.3.4"}
	Where map[string]string `json:"where" description:"过滤条件，如 {\"bk_target_ip\":\"1.2.3.4\"}"`
}

// newMetricsTool 构造 bk_metrics_query 工具。
func newMetricsTool(client *bkapi.Client) tool.Tool {
	fn := func(ctx context.Context, in MetricsInput) (*Result, error) {
		if in.BKBizID == 0 || in.MetricName == "" {
			return nil, fmt.Errorf("bk_biz_id 和 metric_name 为必填项")
		}
		method := in.Method
		if method == "" {
			method = "MEAN"
		}
		interval := in.IntervalS
		if interval <= 0 {
			interval = 60
		}

		// 构造真实请求体（字段名按 BK-Monitor OpenAPI 通用约定）
		reqBody := map[string]any{
			"bk_biz_id": in.BKBizID,
			"query_configs": []map[string]any{
				{
					"data_source_label": "bk_monitor",
					"data_type_label":   "time_series",
					"table":             in.DataLabel,
					"metrics": []map[string]any{
						{"field": in.MetricName, "method": method, "alias": "a"},
					},
					"filter_dict": in.Where,
					"interval":    interval,
				},
			},
			"start_time": in.StartTime,
			"end_time":   in.EndTime,
		}

		var respData map[string]any
		err := client.PostJSON(ctx, "/api/bk-monitor/prod/time_series/unify_query/", reqBody, &respData)
		if errors.Is(err, bkapi.ErrMockMode) {
			return mockMetrics(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("调用蓝鲸监控指标查询失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bk_metrics_query"),
		function.WithDescription("查询蓝鲸监控时序指标数据（CPU/内存/网络/磁盘/业务指标）。适用场景：查看某主机/集群在某时间段的指标曲线，用于故障定位。"),
	)
}

// mockMetrics 返回预置指标样例数据。
func mockMetrics(in MetricsInput) *Result {
	now := time.Now().Unix()
	points := make([][2]int64, 0, 5)
	for i := int64(4); i >= 0; i-- {
		points = append(points, [2]int64{now - i*60, 30 + i*5})
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式（未配置 BK_APP_CODE/BK_APP_SECRET/BK_APIGW_BASE_URL）",
		Data: map[string]any{
			"metric":      in.MetricName,
			"method":      firstNonEmpty(in.Method, "MEAN"),
			"data_label":  in.DataLabel,
			"series": []map[string]any{
				{
					"target": fmt.Sprintf("%s.%s", in.DataLabel, in.MetricName),
					"where":  in.Where,
					"points": points, // 每个元素：[timestamp_sec, value]
				},
			},
		},
	}
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
