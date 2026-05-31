// Package sse 包含SSE服务的注册
package sse

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/session"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"
)

func (s *sseServiceImpl) createSpecialCMDHandler() {
	s.specialCMDHandler = map[string](func(ctx context.Context, userID, sessionID string) string){
		"/clear": s.clearHistory,
		"/list":  s.listHistory,
		"":       s.emptyMsgHandler,
		"/y":     s.storePositiveFeedback,
		"/n":     s.storeNegativeFeedback,
	}
}

// emptyMsgHandler 响应空请求，用于企微服务器ping探活
func (s *sseServiceImpl) emptyMsgHandler(_ context.Context, _, _ string) string {
	return "OK"
}

// clearHistory 清除用户历史对话
func (s *sseServiceImpl) clearHistory(ctx context.Context, userID, sessionID string) string {
	var response string
	if err := s.sessionService.DeleteSession(ctx, session.Key{
		AppName:   s.appName,
		UserID:    userID,
		SessionID: sessionID,
	}); err != nil {
		response = "删除历史对话失败，错误信息：" + err.Error()
	} else {
		response = "已清除历史对话"
	}

	return constructResponse(response)
}

func (s *sseServiceImpl) listHistory(ctx context.Context, userID, sessionID string) string {
	var response string
	sess, err := s.sessionService.GetSession(ctx, session.Key{
		AppName:   s.appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		response = "获取历史对话失败，错误信息：" + err.Error()
	} else {
		response = utils.MustToJSON(sess)
	}

	return constructResponse(response)
}

func (s *sseServiceImpl) storePositiveFeedback(ctx context.Context, userID, sessionID string) string {
	sess, err := s.sessionService.GetSession(ctx, session.Key{
		AppName:   s.appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		log.ErrorContextf(ctx, "获取会话失败，错误信息："+err.Error())
		return constructResponse("获取会话失败，错误信息：" + err.Error())
	}
	if err := s.feedbackCli.StoreFeedback(ctx, sessionID, userID, utils.MustToJSON(sess), true); err != nil {
		log.ErrorContextf(ctx, "存储反馈失败，错误信息："+err.Error())
		return constructResponse("存储反馈失败，错误信息：" + err.Error())
	}
	return constructResponse("反馈已收到，感谢您的反馈")
}

func (s *sseServiceImpl) storeNegativeFeedback(ctx context.Context, userID, sessionID string) string {
	sess, err := s.sessionService.GetSession(ctx, session.Key{
		AppName:   s.appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		log.ErrorContextf(ctx, "获取会话失败，错误信息："+err.Error())
		return constructResponse("获取会话失败，错误信息：" + err.Error())
	}
	if err := s.feedbackCli.StoreFeedback(ctx, sessionID, userID, utils.MustToJSON(sess), false); err != nil {
		log.ErrorContextf(ctx, "存储反馈失败，错误信息："+err.Error())
		return constructResponse("存储反馈失败，错误信息：" + err.Error())
	}
	return constructResponse("反馈已收到，感谢您的反馈")
}

func constructResponse(response string) string {
	return Response{
		Data: Data{
			Response: response,
			Finished: true,
			GlobalOutput: GlobalOutput{
				AnswerSuccess: 1,
			},
		},
	}.String()
}
