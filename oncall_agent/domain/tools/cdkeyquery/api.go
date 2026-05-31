// Package cdkeyquery 提供cdkey查询功能
package cdkeyquery

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=api.go --destination=mock.go --package=cdkeyquery

// Dep cdkeyquery 依赖的外部接口
type Dep struct {
	// CdkeyCli cdkey查询客户端
	CdkeyCli domainext.CdkeyAPI
	// WujiCli 无极配置客户端
	WujiCli domainmodel.WujiAPI
}
