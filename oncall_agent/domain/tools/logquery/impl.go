// Package logquery 提供基于伽利略平台的日志查询功能
package logquery

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	toolName    = "log_query"
	defaultDesc = `log_query 是一个基于伽利略平台的智能化日志查询工具，用于查询和分析分布式系统的日志数据。
# 核心功能
支持通过目标服务、时间范围、日志级别、traceId等多维度条件查询日志，提供灵活的日志搜索和过滤能力。

# 查询能力
- 支持按目标服务（target）查询，格式：{app}.{server}
- 支持按时间范围查询（start/end，单位毫秒）
- 支持按日志级别过滤（level）
- 支持按traceId查询完整调用链日志
- 支持关键字搜索（包含/排除模式）

# 使用限制
- 由于每个服务上报日志时的tags不一定相同，过滤参数中的tags必须严格按样例传入，可以少传tags，代表不按此tags过滤，但是不能传入样例没有的tags（服务日志没有上报）。
- 支持Production（正式/预发布）和Development（测试）环境
- 查询结果按时间倒序排列（可配置排序方式）
- 只能查询最近7天的日志
- 查询起止时间间隔不能超过24小时

# 数据格式
返回结构化的日志记录，包含时间戳、traceId、spanId、日志内容、日志级别和标签信息。若没有返回结果，很可能是目标服务未上报伽利略日志。

# 游标分页机制
当查询超时返回时，会通过cursor参数提供分片游标，业务方可使用此cursor继续从中断位置查询。对于稀疏数据：
- 可能首次只返回少量数据（如1条）但hasNextPage=true
- 需要持续使用cursor进行后续查询，直到累积达到所需条数或hasNextPage=false
`
)

type logQueryImpl struct {
	dep Dep
}

// New 新建logquery工具
func New(dep Dep) tool.Tool {
	desc := defaultDesc
	if dep.WujiCli != nil {
		config := dep.WujiCli.GetLocalToolConfig(toolName)
		if config != nil && config.Description != "" {
			desc = config.Description
		}
	}
	impl := &logQueryImpl{dep: dep}
	return function.NewFunctionTool(
		impl.queryLog,
		function.WithName(toolName),
		function.WithDescription(desc),
	)
}

func (l *logQueryImpl) queryLog(ctx context.Context, req domainmodel.QueryLogReq) (domainmodel.QueryLogRsp, error) {
	// 伽利略日志查询接口，需要将target转换为PCG-123.{target}
	req.Target = "PCG-123." + req.Target
	rsp, err := l.dep.GalileoCli.QueryLog(ctx, &req)
	if err != nil {
		return domainmodel.QueryLogRsp{}, err
	}
	// 魔方业务特殊逻辑：删除请求日志中冗余的登陆态信息
	for i := range rsp.Logs {
		rsp.Logs[i].RemoveRedundantInfo()
	}
	return *rsp, nil
}
