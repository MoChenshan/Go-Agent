// Package conditionlog 包含魔方系统特有的条件日志查询接口
package conditionlog

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
)

// New 创建条件日志客户端（无状态，通过tRPC调用）
func New() domainext.ConditionLogAPI {
	return &conditionLogImpl{}
}
