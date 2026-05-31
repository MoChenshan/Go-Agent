// Package cdkey 包含cdkey相关接口
package cdkey

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
)

// New 创建cdkey客户端
// esUsername: ES账户名
// esPassword: ES密码
// flowPath: 兑换流水地址（取决于ES index）
func New(esUsername, esPassword, flowPath string) domainext.CdkeyAPI {
	return &cdkeyImpl{
		esUsername: esUsername,
		esPassword: esPassword,
		flowPath:   flowPath,
	}
}
