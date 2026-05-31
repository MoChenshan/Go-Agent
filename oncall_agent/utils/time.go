// Package utils 包含通用工具函数（无领域逻辑）
package utils

import (
	"context"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	timestampMSToDateTimeName        = "timestamp_ms_to_date_time"
	timestampMSToDateTimeDefaultDesc = "时间戳(ms)转日期时间"
	dateTimeToTimestampMSName        = "date_time_to_timestamp_ms"
	dateTimeToTimestampMSDefaultDesc = "日期时间转时间戳(ms)"
)

// TimestampMSToDateTimeRequest 时间戳转日期时间请求
type TimestampMSToDateTimeRequest struct {
	Timestamp int64 `json:"timestamp" jsonschema:"required,description=时间戳（单位：毫秒）"`
}

// DateTimeToTimestampMSRequest 日期时间转时间戳请求
type DateTimeToTimestampMSRequest struct {
	DateTime string `json:"date_time" jsonschema:"required,description=日期时间，格式为 2006-01-02 15:04:05"`
}

// TimestampMSToDateTime 时间戳(ms)转日期时间
func TimestampMSToDateTime(_ context.Context, req TimestampMSToDateTimeRequest) (string, error) {
	return time.UnixMilli(req.Timestamp).Format("2006-01-02 15:04:05"), nil
}

// DateTimeToTimestampMS 日期时间转时间戳(ms)
func DateTimeToTimestampMS(_ context.Context, req DateTimeToTimestampMSRequest) (int64, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", req.DateTime, time.Local)
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}

// NewTimestampMSToDateTimeTool 新建时间戳转日期时间工具，desc为空时使用默认描述
func NewTimestampMSToDateTimeTool(desc string) tool.Tool {
	if desc == "" {
		desc = timestampMSToDateTimeDefaultDesc
	}
	return function.NewFunctionTool(
		TimestampMSToDateTime,
		function.WithName(timestampMSToDateTimeName),
		function.WithDescription(desc),
	)
}

// NewDateTimeToTimestampMSTool 新建日期时间转时间戳工具，desc为空时使用默认描述
func NewDateTimeToTimestampMSTool(desc string) tool.Tool {
	if desc == "" {
		desc = dateTimeToTimestampMSDefaultDesc
	}
	return function.NewFunctionTool(
		DateTimeToTimestampMS,
		function.WithName(dateTimeToTimestampMSName),
		function.WithDescription(desc),
	)
}
