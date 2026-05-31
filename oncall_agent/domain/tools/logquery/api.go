// Package logquery 提供基于伽利略平台的日志查询功能
package logquery

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=api.go --destination=mock.go --package=logquery

// Dep logquery 依赖的外部接口
type Dep struct {
	// GalileoCli 伽利略日志与链路查询客户端
	GalileoCli domainext.GalileoAPI
	// WujiCli 无极配置客户端
	WujiCli domainmodel.WujiAPI
}
