// Package external 定义外部服务接口，由 infrastructure/external 实现
package external

import (
	"context"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=galileo_api.go --destination=galileo_mock.go --package=external

// GalileoAPI 伽利略日志与链路查询接口
type GalileoAPI interface {
	// QueryLog 日志查询接口
	QueryLog(ctx context.Context, req *domainmodel.QueryLogReq) (*domainmodel.QueryLogRsp, error)
	// QueryTrace traceId查询接口
	QueryTrace(ctx context.Context, req *domainmodel.QueryTraceReq) (*domainmodel.QueryTraceRsp, error)
}
