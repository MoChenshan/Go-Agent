package model

import (
	"github.com/tidwall/gjson"

	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"
)

// QueryTraceReq 伽利略trace查询请求
type QueryTraceReq struct {
	Target  string `protobuf:"bytes,1,opt,name=target,proto3" json:"target,omitempty"` //观测对象，如 PCG-123.galileo.apiserver
	TraceID string `protobuf:"bytes,2,opt,name=trace_id,json=traceId,proto3" json:"trace_id,omitempty"`
}

// QueryLogReq 伽利略日志查询请求
type QueryLogReq struct {
	Target         string         `json:"target" jsonschema:"required,description=需要查询日志的对象 格式为{{app}}.{{server}} 比如魔方接入层为magic.magic_access" validate:"required,pattern=^.+$"`       // 伽利略请求服务
	Namespace      string         `json:"namespace" jsonschema:"required,description=命名空间 正式环境/预发布环境: Production 测试环境: Development" validate:"required,oneof=Production Development"` // 命名空间（Production or Development）
	Start          int64          `json:"start" jsonschema:"required,description=查询的开始时间 **单位毫秒**"`                                                                                   // 查询的开始时间，单位ms
	End            int64          `json:"end" jsonschema:"required,description=查询的结束时间 **单位毫秒** 注意 查询开始和结束时间间隔不能超过24小时"`                                                              // 查询的结束时间，单位ms
	Limit          int32          `json:"limit" jsonschema:"required,description=最大查询条数 范围1-30"`                                                                                      // 最大查询 30 条
	TagWhere       *TagSearch     `json:"tag_where,omitempty" jsonschema:"description=日志需要包含的标签列表 不同服务日志上报的标签不一样"`                                                                    // 键值对查询
	MessageKeyword []string       `json:"message_keyword,omitempty" jsonschema:"description=message关键字搜索 多关键字之间关系为且（已废弃）"`                                                            // message 关键字搜索，多关键字之间关系为 且 deprecated
	Cursor         string         `json:"cursor,omitempty" jsonschema:"description=游标翻页查询 用于分页获取数据" validate:"tsecstr"`                                                               // 游标翻页查询
	Include        *MessageSearch `json:"include,omitempty" jsonschema:"description=需要包含的日志正文关键字列表"`                                                                                  // message 关键字搜索，包含搜索
	Exclude        *MessageSearch `json:"exclude,omitempty" jsonschema:"description=需要排除的日志正文关键字列表"`                                                                                  // message 关键字搜索，排除搜索
	// Query string `json:"query,omitempty" jsonschema:"伽利略查询语句，有查询语句时优先使用查询语句，没有查询语句时再使用其他条件搜索"`
	SortType LogSortType `json:"sort_type,omitempty" jsonschema:"description=排序方式：0-默认倒序 1-正序 2-倒序"` // 排序
}

// QueryLogRsp 伽利略日志查询响应
type QueryLogRsp struct {
	Code        int32        `json:"code,omitempty" jsonschema:"description=返回码"`
	Msg         string       `json:"msg,omitempty" jsonschema:"description=返回消息"`
	Total       int32        `json:"total,omitempty" jsonschema:"description=统计当前实际查询所得条数"` // 统计当前实际查询所得条数
	Logs        []*LogRecord `json:"logs,omitempty" jsonschema:"description=日志记录列表"`
	Cursor      string       `json:"cursor,omitempty" jsonschema:"description=游标翻页查询，用于获取下一页数据"` // 游标翻页查询
	HasNextPage bool         `json:"has_next_page,omitempty" jsonschema:"description=是否有下一页数据"`  // 是否有下一页
}

// LogRecord 伽利略日志记录
type LogRecord struct {
	Timestamp string            `json:"timestamp,omitempty" jsonschema:"description=时间戳，单位毫秒"`
	TraceID   string            `json:"trace_id,omitempty" jsonschema:"description=traceId，链路追踪标识"`
	SpanID    string            `json:"span_id,omitempty" jsonschema:"description=spanId，链路追踪中的span标识"`
	Message   string            `json:"message,omitempty" jsonschema:"description=日志消息内容"`
	Level     string            `json:"level,omitempty" jsonschema:"description=日志级别"`
	Tags      map[string]string `json:"tags,omitempty" jsonschema:"description=日志标签，键值对形式"`
}

// RemoveRedundantInfo 删除请求日志中多余的魔方登陆态信息，防止占用过多上下文
func (l *LogRecord) RemoveRedundantInfo() {
	if req := l.Tags["req"]; req != "" {
		// 判断是否是魔方请求
		if query := gjson.Get(req, "context.cgi_req_data.query"); query.Exists() {
			// 是魔方请求，删除多余的登陆态信息
			reqMap := utils.MustJSONToMap(req)
			delete(reqMap, "context")
			reqMap["query"] = query.String()
			l.Tags["req"] = utils.MustToJSON(reqMap)
		}
	}
}

// LogSortType 日志排序方式
type LogSortType int32

const (
	LogSortTypeSortTypeDefault LogSortType = 0 // 默认正序
	LogSortTypeSortTypeAsc     LogSortType = 1 // 正序
	LogSortTypeSortTypeDesc    LogSortType = 2 // 倒序
)

// MessageSearchSearchType 日志搜索类型
type MessageSearchSearchType int32

const (
	MessageSearchSearchTypeDefault MessageSearchSearchType = 0 // 默认搜索
	MessageSearchSearchTypeCaseins MessageSearchSearchType = 1 // 忽略大小写搜索
	MessageSearchSearchTypeToken   MessageSearchSearchType = 2 // 单词搜索
	MessageSearchSearchTypeSubstr  MessageSearchSearchType = 3 // 子串搜索
	MessageSearchSearchTypeRegular MessageSearchSearchType = 4 // 正则搜索
)

// MessageSearchFilterType 日志搜索过滤类型
type MessageSearchFilterType int32

const (
	MessageSearchEmpty MessageSearchFilterType = 0 // 默认为或
	MessageSearchOr    MessageSearchFilterType = 1
	MessageSearchAnd   MessageSearchFilterType = 2
)

// MessageSearch 日志搜索
type MessageSearch struct {
	Keyword []string                `json:"keyword,omitempty" jsonschema:"description=关键字列表"`
	Filter  MessageSearchFilterType `json:"filter,omitempty" jsonschema:"description=关键词之间逻辑关系：0-默认为或，1-或，2-且"`                 // 关键词之间逻辑关系
	Search  MessageSearchSearchType `json:"search,omitempty" jsonschema:"description=搜索逻辑：0-默认搜索，1-忽略大小写，2-单词搜索，3-子串搜索，4-正则搜索"` // 搜索逻辑
}

// LogTagsFields 日志标签字段
// name 与 value 采用自适应
// 即，len(value)=1 ==> name=value, len(value)>1 ==> name in [value];
type LogTagsFields struct {
	Name   string   `json:"name,omitempty" jsonschema:"description=标签名称"`
	Values []string `json:"values,omitempty" jsonschema:"description=标签值列表，支持多个值匹配"`
}

// TagSearch 日志标签搜索
type TagSearch struct {
	TraceID   string           `json:"trace_id,omitempty" jsonschema:"description=traceId查询条件"`
	Level     []string         `json:"level,omitempty" jsonschema:"description=日志级别列表，支持多个级别筛选"`
	OtherTags []*LogTagsFields `json:"other_tags,omitempty" jsonschema:"description=其他标签查询条件"`
}

// QueryTraceRsp represents the complete trace structure
type QueryTraceRsp struct {
	Code  int    `json:"code,omitempty"`
	Msg   string `json:"msg,omitempty"`
	Trace Trace  `json:"trace,omitempty"`
}

// Trace contains the processes and spans
type Trace struct {
	Processes map[string]Process `json:"processes,omitempty"`
	Spans     []Span             `json:"spans,omitempty"`
}

// Process represents a service in the trace
type Process struct {
	ServiceName string `json:"service_name,omitempty"`
	Tags        []Tag  `json:"tags,omitempty"`
}

// Span represents a single operation in the trace
type Span struct {
	TraceID       string      `json:"trace_id,omitempty"`
	SpanID        string      `json:"span_id,omitempty"`
	Duration      string      `json:"duration,omitempty"`
	OperationName string      `json:"operation_name,omitempty"`
	ProcessID     string      `json:"process_id,omitempty"`
	Tags          []Tag       `json:"tags,omitempty"`
	Logs          []LogEntry  `json:"logs,omitempty"`
	References    []Reference `json:"references,omitempty"`
}

// Tag represents key-value pairs in spans and processes
type Tag struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// LogEntry contains event logs with fields
type LogEntry struct {
	Fields []Field `json:"fields,omitempty"`
}

// Field represents a log field
type Field struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// Reference represents parent-child relationships
type Reference struct {
	RefType string `json:"ref_type,omitempty"`
	SpanID  string `json:"span_id,omitempty"`
}
