// Package galileo 是调用伽利略API的封装，用于获取服务日志和拉取trace
// 参考文档
// https://iwiki.woa.com/p/4007673553
// https://iwiki.woa.com/p/4007645390
package galileo

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
)

// New 创建伽利略客户端
// bkAppCode: 蓝鲸平台应用code
// bkAppToken: 蓝鲸平台应用token
func New(bkAppCode, bkAppToken string) domainext.GalileoAPI {
	return &galileoImpl{
		bkAppCode:  bkAppCode,
		bkAppToken: bkAppToken,
	}
}
