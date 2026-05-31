// Package model 包含跨领域共享的领域对象和原语
package model

import (
	"context"
	"time"

	carbon "github.com/dromara/carbon/v2"
	agentmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"
)

const (
	contextTemplate = `## 上下文信息

- 当前日期: {{date}}
- 当前时间戳(ms): {{currentTimestamp}}
- 今日开始时间戳(ms): {{startOfTodayTimestamp}}`
)

// FillSystemContextInfo 改写请求中上下文信息
var FillSystemContextInfo = func(ctx context.Context, req *agentmodel.Request) (*agentmodel.Response, error) {
	log.DebugContextf(ctx, "[fillSystemContextInfo] msg: %s", utils.MustToJSON(req))
	for i, msg := range req.Messages {
		if msg.Role == agentmodel.RoleSystem {
			req.Messages[i].Content = req.Messages[i].Content + utils.RenderString(contextTemplate, map[string]interface{}{
				"date":                  time.Now().Format("2006-01-02 15:04:05"),
				"currentTimestamp":      carbon.Now(carbon.Shanghai).TimestampMilli(),
				"startOfTodayTimestamp": carbon.Now(carbon.Shanghai).StartOfDay().TimestampMilli(),
			})
		}
	}
	return nil, nil
}
