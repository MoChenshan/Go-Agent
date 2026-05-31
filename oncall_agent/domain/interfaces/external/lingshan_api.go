package external

import (
	"context"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=lingshan_api.go --destination=lingshan_mock.go --package=external

// LingshanAPI 灵杉服务注册中心查询接口
type LingshanAPI interface {
	// GetSrvDetailByNames 获取服务详情，包括所属团队、概况、负责人等
	GetSrvDetailByNames(ctx context.Context, req domainmodel.GetSrvDetailByNamesReq) (
		domainmodel.GetSrvDetailByNamesRsp, error)
}
