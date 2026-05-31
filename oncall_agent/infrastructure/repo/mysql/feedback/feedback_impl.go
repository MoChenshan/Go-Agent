// Package feedback 实现用户反馈存储库，提供反馈数据的持久化能力。
package feedback

import (
	"context"
	"fmt"

	"git.code.oa.com/trpc-go/trpc-database/mysql"

	_ "embed"
)

//go:embed sql/store_feedback.sql
var storeFeedbackSQL string

type feedbackImpl struct {
	mysqlCli mysql.Client
}

// New 创建一个 feedbackImpl 实例
func New(mysqlCli mysql.Client) API {
	return &feedbackImpl{
		mysqlCli: mysqlCli,
	}
}

func (f *feedbackImpl) StoreFeedback(ctx context.Context, sessionID, userID, content string, isPositive bool) error {
	// Execute the query with the provided parameters
	_, err := f.mysqlCli.Exec(ctx, storeFeedbackSQL, sessionID, userID, content, isPositive)
	if err != nil {
		return fmt.Errorf("failed to store feedback: %w", err)
	}

	return nil
}
