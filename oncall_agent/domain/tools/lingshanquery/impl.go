// Package lingshanquery 提供灵杉服务注册中心查询功能
package lingshanquery

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	toolName    = "query_service_info"
	defaultDesc = `query_service_info可以根据服务名列表查询服务信息，包括负责人、描述、所属团队以及系统等。
`
)

type lingshanQueryImpl struct {
	dep Dep
}

// New 新建lingshanquery工具
func New(dep Dep) tool.Tool {
	desc := defaultDesc
	if dep.WujiCli != nil {
		config := dep.WujiCli.GetLocalToolConfig(toolName)
		if config != nil && config.Description != "" {
			desc = config.Description
		}
	}
	impl := &lingshanQueryImpl{dep: dep}
	return function.NewFunctionTool(
		impl.getSrvDetailByNames,
		function.WithName(toolName),
		function.WithDescription(desc),
	)
}

func (l *lingshanQueryImpl) getSrvDetailByNames(ctx context.Context, req domainmodel.GetSrvDetailByNamesReq) (domainmodel.GetSrvDetailByNamesRsp, error) {
	return l.dep.LingshanCli.GetSrvDetailByNames(ctx, req)
}
