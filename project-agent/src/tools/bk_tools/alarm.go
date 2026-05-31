
// 蓝鲸监控-告警查询（bk-alarm）。
//
// 对接蓝鲸监控告警：POST /api/bk-monitor/prod/alert/search/
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

// AlarmInput bk_alarm_query 工具入参。
type AlarmInput struct {
	BKBizID   int      `json:"bk_biz_id"    description:"蓝鲸业务 ID（必填）"`
	Status    []string `json:"status"       description:"告警状态过滤：ABNORMAL / RECOVERED / CLOSED，默认 [ABNORMAL]"`
	Severity  []int    `json:"severity"     description:"告警级别：1=致命 2=预警 3=提醒，默认全部"`
	Keyword   string   `json:"keyword"      description:"关键字模糊匹配告警名/内容"`
	StartTime string   `json:"start_time"   description:"开始时间"`
	EndTime   string   `json:"end_time"     description:"结束时间"`
	PageSize  int      `json:"page_size"    description:"每页数量，默认 20，最大 100"`
}

// newAlarmTool 构造 bk_alarm_query 工具。
func newAlarmTool(client *bkapi.Client) tool.Tool {
	fn := func(ctx context.Context, in AlarmInput) (*Result, error) {
		if in.BKBizID == 0 {
			return nil, fmt.Errorf("bk_biz_id 为必填项")
		}
		if len(in.Status) == 0 {
			in.Status = []string{"ABNORMAL"}
		}
		pageSize := in.PageSize
		if pageSize <= 0 {
			pageSize = 20
		}
		if pageSize > 100 {
			pageSize = 100
		}

		reqBody := map[string]any{
			"bk_biz_ids": []int{in.BKBizID},
			"status":     in.Status,
			"severity":   in.Severity,
			"query_string": in.Keyword,
			"start_time": in.StartTime,
			"end_time":   in.EndTime,
			"page_size":  pageSize,
		}

		var respData map[string]any
		err := client.PostJSON(ctx, "/api/bk-monitor/prod/alert/search/", reqBody, &respData)
		if errors.Is(err, bkapi.ErrMockMode) {
			return mockAlarm(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("调用蓝鲸告警查询失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bk_alarm_query"),
		function.WithDescription("查询蓝鲸监控告警列表。适用场景：查看当前业务未恢复的告警、按关键字搜索历史告警、判断故障影响面。"),
	)
}

// mockAlarm 返回预置告警样例数据。
func mockAlarm(in AlarmInput) *Result {
	now := time.Now()
	alerts := []map[string]any{
		{
			"alert_id":   "MOCK-ALERT-0001",
			"name":       "CPU 使用率持续高于阈值",
			"severity":   1,
			"status":     "ABNORMAL",
			"begin_time": now.Add(-15 * time.Minute).Format(time.RFC3339),
			"target":     "ip=10.1.1.100,module=game-core",
			"message":    "CPU usage 92.3% 超过阈值 85% 持续 10min (mock)",
		},
		{
			"alert_id":   "MOCK-ALERT-0002",
			"name":       "Pod CrashLoopBackOff",
			"severity":   1,
			"status":     "ABNORMAL",
			"begin_time": now.Add(-8 * time.Minute).Format(time.RFC3339),
			"target":     "namespace=letsgo,pod=game-core-7f",
			"message":    "Pod 重启 5 次 (mock)",
		},
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式",
		Data: map[string]any{
			"bk_biz_id": in.BKBizID,
			"total":     len(alerts),
			"alerts":    alerts,
		},
	}
}
