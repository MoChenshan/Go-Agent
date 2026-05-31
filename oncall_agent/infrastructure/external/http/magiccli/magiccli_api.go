// Package magiccli 包含魔方crypto客户端接口
package magiccli

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
)

// New 创建魔方CLI客户端（无状态）
func New() domainext.MagicCliAPI {
	return &magicCliImpl{}
}
