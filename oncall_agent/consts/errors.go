// Package consts 定义错误码、枚举和全局常量
package consts

import "git.code.oa.com/trpc-go/trpc-go/errs"

// 业务错误码定义
// 使用方式: errs.New(consts.ErrCodeInvalidParam, "invalid param")
const (
	// ErrCodeInvalidParam 无效参数
	ErrCodeInvalidParam = 10001
	// ErrCodeDBQuery 数据库查询错误
	ErrCodeDBQuery = 10002
)

// 预定义错误
var (
	// ErrInvalidParam 无效参数错误
	ErrInvalidParam = errs.New(ErrCodeInvalidParam, "invalid param")
)
