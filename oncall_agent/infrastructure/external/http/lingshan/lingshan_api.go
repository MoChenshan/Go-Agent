// Package lingshan 包含灵山相关调用函数, 主要用于查询线上服务信息
// 拉取伽利略trace时，仅有服务的名称(app.server), 而没有服务的中文描述，因此需要
// 调用灵山接口获取服务详情
package lingshan

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
)

// New 创建灵山客户端
// secretID: 太湖平台X-Gateway-SecretId
// secretKey: 太湖平台X-Gateway-SecretKey
func New(secretID, secretKey string) domainext.LingshanAPI {
	return &lingshanImpl{
		secretID:  secretID,
		secretKey: secretKey,
	}
}
