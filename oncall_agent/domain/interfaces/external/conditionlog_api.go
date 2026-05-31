package external

import "context"

//go:generate mockgen --source=conditionlog_api.go --destination=conditionlog_mock.go --package=external

// ConditionLogAPI 条件日志查询接口
type ConditionLogAPI interface {
	// GetConditionLog 获取条件日志
	GetConditionLog(ctx context.Context, start, end int64, traceID string) (string, error)
}
