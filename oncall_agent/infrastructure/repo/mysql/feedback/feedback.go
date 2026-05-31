// Package feedback 包含用户反馈存储接口定义。
package feedback

import "context"

//go:generate mockgen -source=feedback.go -destination=feedback_mock.go -package=feedback

// API 定义了用户反馈的接口
type API interface {
	// StoreFeedback 存储用户反馈
	StoreFeedback(ctx context.Context, sessionID, userID, content string, isPositive bool) error
}
