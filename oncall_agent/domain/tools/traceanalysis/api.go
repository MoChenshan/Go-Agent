// Package traceanalysis 提供分布式链路追踪分析功能
package traceanalysis

import (
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=api.go --destination=mock.go --package=traceanalysis

// Dep traceanalysis 依赖的外部接口
type Dep struct {
	// GalileoCli 伽利略日志与链路查询客户端
	GalileoCli domainext.GalileoAPI
	// LingshanCli 灵杉服务注册中心查询客户端
	LingshanCli domainext.LingshanAPI
	// ConditionLogCli 条件日志查询客户端
	ConditionLogCli domainext.ConditionLogAPI
	// WujiCli 无极配置客户端
	WujiCli domainmodel.WujiAPI
	// Cfg 链路分析配置
	Cfg TraceConfig
}

// TraceConfig 链路分析配置参数
type TraceConfig struct {
	// MaxTraceDepth traceanalysis最大递归深度
	MaxTraceDepth int
	// MaxSpanNum traceanalysis最大返回的span数量
	MaxSpanNum int
	// MaxSpanLogLength traceanalysis最大返回的span日志长度
	MaxSpanLogLength int
	// SelfTeamName 自己团队的名称
	SelfTeamName string
	// OtherTeamTruncateDepth 其他团队截断的trace深度
	OtherTeamTruncateDepth int
}
