package external

import (
	"context"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=cdkey_api.go --destination=cdkey_mock.go --package=external

// CdkeyAPI cdkey查询接口
type CdkeyAPI interface {
	// BatchQueryCdkey 批量查询cdkey
	BatchQueryCdkey(ctx context.Context, req domainmodel.BatchQueryCdkeyReq) (domainmodel.BatchQueryCdkeyRsp, error)
}
