
// 蓝鲸监控-事件查询（bk-event）。
//
// 对接蓝鲸事件中心：POST /api/bk-monitor/prod/event/search/
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

// EventInput bk_event_query 工具入参。
type EventInput struct {
	BKBizID   int      `json:"bk_biz_id"   description:"蓝鲸业务 ID（必填）"`
	EventType []string `json:"event_type"  description:"事件类型：gse_custom_event / k8s_event / deploy_event，默认全部"`
	Keyword   string   `json:"keyword"     description:"关键字模糊匹配事件名/来源"`
	StartTime string   `json:"start_time"  description:"开始时间"`
	EndTime   string   `json:"end_time"    description:"结束时间"`
	Size      int      `json:"size"        description:"返回条数上限，默认 50，最大 200"`
}

// newEventTool 构造 bk_event_query 工具。
func newEventTool(client *bkapi.Client) tool.Tool {
	fn := func(ctx context.Context, in EventInput) (*Result, error) {
		if in.BKBizID == 0 {
			return nil, fmt.Errorf("bk_biz_id 为必填项")
		}
		size := in.Size
		if size <= 0 {
			size = 50
		}
		if size > 200 {
			size = 200
		}

		reqBody := map[string]any{
			"bk_biz_id":    in.BKBizID,
			"event_types":  in.EventType,
			"query_string": in.Keyword,
			"start_time":   in.StartTime,
			"end_time":     in.EndTime,
			"size":         size,
		}

		var respData map[string]any
		err := client.PostJSON(ctx, "/api/bk-monitor/prod/event/search/", reqBody, &respData)
		if errors.Is(err, bkapi.ErrMockMode) {
			return mockEvent(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("调用蓝鲸事件查询失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bk_event_query"),
		function.WithDescription("查询蓝鲸事件中心，包括 K8s 事件、发布事件、自定义上报事件。适用场景：定位故障前是否有发布变更、重启、扩缩容等动作。"),
	)
}

// mockEvent 返回预置事件样例数据。
func mockEvent(in EventInput) *Result {
	now := time.Now()
	events := []map[string]any{
		{
			"time":       now.Add(-20 * time.Minute).Format(time.RFC3339),
			"type":       "deploy_event",
			"source":     "bk-devops",
			"summary":    "流水线 letsgo-game-core #5321 执行发布 (mock)",
			"annotation": map[string]any{"version": "v1.2.3", "operator": "alice"},
		},
		{
			"time":       now.Add(-12 * time.Minute).Format(time.RFC3339),
			"type":       "k8s_event",
			"source":     "kube-apiserver",
			"summary":    "Pod letsgo/game-core-7f BackOff (mock)",
			"annotation": map[string]any{"reason": "CrashLoopBackOff"},
		},
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式",
		Data: map[string]any{
			"bk_biz_id": in.BKBizID,
			"total":     len(events),
			"events":    events,
		},
	}
}
