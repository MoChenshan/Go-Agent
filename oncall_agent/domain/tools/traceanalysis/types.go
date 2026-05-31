// Package traceanalysis 提供分布式链路追踪分析功能
package traceanalysis

import (
	"time"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

// AnalysisTraceReq 分析链路的请求
type AnalysisTraceReq struct {
	TraceID    string `json:"trace_id" jsonschema:"required,description=需要分析的trace id"`
	Target     string `json:"target" jsonschema:"required,description=需要分析的目标服务，格式为{app}.{server}"`
	RootSpanID string `json:"root_span_id,omitempty" jsonschema:"description=需要分析的根span id，用于多次分批拉取span，可选"`
	Namespace  string `json:"namespace" jsonschema:"required,description=需要查询的namespace，Production/Development"`
	Start      int64  `json:"start" jsonschema:"required,description=需要查询的开始时间，单位毫秒"`
	End        int64  `json:"end" jsonschema:"required,description=需要查询的结束时间，单位毫秒"`
}

// AnalysisTraceRsp 分析链路的响应
type AnalysisTraceRsp struct {
	TraceSummary      string `json:"trace_summary" jsonschema:"description=层级调用链路"`
	ConditionGroupMap string `json:"condition_group_map" jsonschema:"description=若本次业务请求涉及魔方条件计算，此字段包含每个条件组下的所有条件计算结果"`
}

// Service 服务信息
type Service struct {
	Name        string
	Description string
	Team        string
}

// CallInfo 从span中提取的调用信息
type CallInfo struct {
	ServiceName        string // 上报日志的服务名 app.server
	ServiceTeamName    string // 上报日志的服务所属团队
	ServiceDescription string // 上报日志的服务描述
	ServiceDomain      string // 上报日志的服务所属系统
	CallerService      string
	CallerMethod       string
	CalleeService      string
	CalleeMethod       string
	CalleeInfo         Service
	Duration           time.Duration
	StatusCode         string
	StatusMessage      string
	StartTime          string
	SpanKind           string // client: 主调 server: 被调
	OperationName      string
	SpanID             string
	Logs               []domainmodel.LogEntry
}

// SpanNode 树状结构中的span节点
type SpanNode struct {
	Span     domainmodel.Span
	Children []*SpanNode
	Level    int
	SpanID   string
}
