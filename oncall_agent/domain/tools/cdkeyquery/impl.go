// Package cdkeyquery 提供cdkey查询功能
package cdkeyquery

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	toolName    = "cdkey_query"
	defaultDesc = `cdkey_query可用于查询cdkey发放信息。批量查询时，可以指定多个子查询，每个子查询可选包括vuid、cdkey号。
	若指定vuid，则会查询该vuid下所有cdkey的发放信息。
	若指定cdkey号，则会查询该cdkey的发放信息。
	若都指定，则会查询该vuid下指定cdkey的发放信息。
	每个子查询最多返回最新的20条记录。
`
)

type cdkeyQueryImpl struct {
	dep Dep
}

// New 新建cdkeyquery工具
func New(dep Dep) tool.Tool {
	desc := defaultDesc
	if dep.WujiCli != nil {
		config := dep.WujiCli.GetLocalToolConfig(toolName)
		if config != nil && config.Description != "" {
			desc = config.Description
		}
	}
	impl := &cdkeyQueryImpl{dep: dep}
	return function.NewFunctionTool(
		impl.batchQueryCdkey,
		function.WithName(toolName),
		function.WithDescription(desc),
	)
}

func (c *cdkeyQueryImpl) batchQueryCdkey(ctx context.Context, req domainmodel.BatchQueryCdkeyReq) (domainmodel.BatchQueryCdkeyRsp, error) {
	return c.dep.CdkeyCli.BatchQueryCdkey(ctx, req)
}
