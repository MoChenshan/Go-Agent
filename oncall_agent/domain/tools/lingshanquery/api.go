// Package lingshanquery 提供灵杉服务注册中心查询功能
package lingshanquery

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=api.go --destination=mock.go --package=lingshanquery

// Dep lingshanquery 依赖的外部接口
type Dep struct {
	// LingshanCli 灵杉服务注册中心查询客户端
	LingshanCli domainext.LingshanAPI
	// WujiCli 无极配置客户端
	WujiCli domainmodel.WujiAPI
}
