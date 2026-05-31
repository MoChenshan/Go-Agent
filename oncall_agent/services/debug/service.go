// Package debug 包含调试服务的实现
package debug

import (
	"context"
	"encoding/json"

	"git.code.oa.com/trpc-go/trpc-go/log"
	pb "git.woa.com/trpcprotocol/magic/oncall_agent_oncall_agent_debug"

	magictool "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/magictool"
)

// debugSrvImpl 调试服务实现
type debugSrvImpl struct {
	toolDep magictool.Dep
}

// New 新建服务
func New(dep magictool.Dep) pb.DebugService {
	return &debugSrvImpl{
		toolDep: dep,
	}
}

// GetModuleTypeInfo 获取模块类型信息
func (s *debugSrvImpl) GetModuleTypeInfo(ctx context.Context, req *pb.GetModuleTypeInfoReq) (*pb.GetModuleTypeInfoRsp, error) {
	magicModTypeTool := magictool.NewMagicToolImpl(s.toolDep)
	rsp, err := magicModTypeTool.GetMagicModTypeInfo(ctx, magictool.GetMagicModTypeInfoReq{
		ModTypeIDs: req.ModTypeId,
		ModNames:   req.ModName,
	})
	if err != nil {
		log.ErrorContextf(ctx, "GetMagicModTypeInfo err: %+v", err)
		return nil, err
	}
	rspJSON, err := json.Marshal(rsp)
	if err != nil {
		log.ErrorContextf(ctx, "json marshall err: %+v", err)
		return nil, err
	}

	return &pb.GetModuleTypeInfoRsp{
		Msg: string(rspJSON),
	}, nil
}

// GetActSummary 获取活动概要
func (s *debugSrvImpl) GetActSummary(ctx context.Context, req *pb.GetActSummaryReq) (*pb.GetActSummaryRsp, error) {
	magicModTypeTool := magictool.NewMagicToolImpl(s.toolDep)
	rsp, err := magicModTypeTool.GetMagicActInfo(ctx, magictool.GetMagicActInfoReq{
		ActID: req.ActId,
	})
	if err != nil {
		log.ErrorContextf(ctx, "GetMagicActInfo err: %+v", err)
		return nil, err
	}
	rspJSON, err := json.Marshal(rsp)
	if err != nil {
		log.ErrorContextf(ctx, "json marshall err: %+v", err)
		return nil, err
	}

	return &pb.GetActSummaryRsp{
		Msg: string(rspJSON),
	}, nil
}
