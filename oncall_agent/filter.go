// Package main 包含请求过滤器
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cast"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/errs"
	"git.code.oa.com/trpc-go/trpc-go/filter"
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"
)

// reqFilter 请求过滤器，统一打印req/rsp/cost_time
func reqFilter(ctx context.Context, req interface{}, next filter.ServerHandleFunc) (interface{}, error) {
	msg := trpc.Message(ctx)
	service, env, method := msg.CalleeServiceName(), trpc.GlobalConfig().Global.EnvName, msg.CalleeMethod()
	log.WithContextFields(ctx, "service", service, "method", method, "env", env, "req", utils.MustToJSON(req))
	start := time.Now()
	rsp, err := next(ctx, req)
	ret, cost := cast.ToString(errs.Code(err)), fmt.Sprintf("%d", time.Since(start))
	log.WithContextFields(ctx, "ret", ret, "cost_time", cost, "rsp", utils.MustToJSON(rsp))
	if err == nil {
		log.InfoContextf(ctx, "Leaving function: %s", method)
	} else {
		log.ErrorContextf(ctx, "Leaving function: %s with error message: %v", method, err)
	}
	return rsp, err
}
