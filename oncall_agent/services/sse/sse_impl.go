// Package sse 包含SSE服务的注册
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"

	"git.code.oa.com/trpc-go/trpc-database/mysql"
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

	"git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/repo/mysql/feedback"
)

type sseServiceImpl struct {
	appName           string
	debug             bool
	entranceAgent     agent.Agent
	sessionService    session.Service
	agentRunner       runner.Runner
	specialCMDHandler map[string]func(context.Context, string, string) string
	feedbackCli       feedback.API
}

// NewSSEService 创建新的SSE服务
// debug参数控制是否在响应中输出调试信息（工具参数、token使用量等）
func NewSSEService(s session.Service, a agent.Agent, m mysql.Client, appName string, debug bool) API {
	sseImpl := &sseServiceImpl{
		sessionService: s,
		agentRunner: runner.NewRunner(
			appName,
			a,
			runner.WithSessionService(s),
		),
		feedbackCli:   feedback.New(m),
		appName:       appName,
		debug:         debug,
		entranceAgent: a,
	}
	sseImpl.createSpecialCMDHandler()
	return sseImpl
}

// HandleSSE 处理基础SSE请求
func (s *sseServiceImpl) HandleSSE(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	var reqBody Request
	bodyBytes, readErr := io.ReadAll(r.Body)
	log.InfoContextf(ctx, "request body: %s", string(bodyBytes))
	if readErr != nil {
		log.ErrorContextf(ctx, "Failed to read request body: %v", readErr)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return nil
	}

	if unmarshalErr := json.Unmarshal(bodyBytes, &reqBody); unmarshalErr != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return nil
	}

	// Get the tRPC HTTP response writer
	w = thttp.Response(ctx)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return nil
	}

	// 处理特殊命令
	if cmdHandler, exists := s.specialCMDHandler[reqBody.Content]; exists {
		if _, writeErr := w.Write([]byte(cmdHandler(ctx, reqBody.GetUserID(), reqBody.GetSessionID()))); writeErr != nil {
			log.ErrorContextf(ctx, "failed to write special cmd response: %v", writeErr)
		}
		flusher.Flush()
		return nil
	}

	// Run the agent through the runner.
	eventChan, err := s.agentRunner.Run(ctx, reqBody.GetUserID(), reqBody.GetSessionID(),
		model.NewUserMessage(reqBody.Content))
	if err != nil {
		log.ErrorContextf(ctx, "failed to run agent: %v", err)
		return fmt.Errorf("failed to run agent: %w", err)
	}
	return s.processStreamingResponse(ctx, w, eventChan)
}

// processStreamingResponse 处理流式响应，包含工具调用可视化
func (s *sseServiceImpl) processStreamingResponse(ctx context.Context, w http.ResponseWriter, eventChan <-chan *event.Event) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return nil
	}

	for event := range eventChan {
		log.DebugContextf(ctx, "event: %s", utils.MustToJSON(event))
		if event.Object == "chat.completion" && len(event.Choices) > 0 {
			log.InfoContextf(ctx, "full response:\n%s", event.Choices[0].Message.Content)
		}
		// Handle errors.
		if event.Error != nil {
			fmt.Printf("\n❌ Error: %s\n", event.Error.Message)
			continue
		}

		// Detect and display tool calls.
		if s.handleToolCalls(ctx, w, flusher, event) {
			continue
		}

		// Detect tool responses.
		if s.hasToolResponse(ctx, event) {
			continue
		}

		// Process streaming content.
		s.handleStreamContent(w, flusher, event)

		// Check if this is the final event.
		if s.handleFinalEvent(w, flusher, event) {
			break
		}
	}

	return nil
}

// handleToolCalls 处理工具调用显示，返回是否处理了工具调用
func (s *sseServiceImpl) handleToolCalls(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, event *event.Event) bool {
	if len(event.Choices) == 0 || len(event.Choices[0].Message.ToolCalls) == 0 {
		return false
	}
	log.DebugContextf(ctx, "🔧 CallableTool calls initiated:\n")
	for _, toolCall := range event.Choices[0].Message.ToolCalls {
		resp := fmt.Sprintf("\n*开始执行工具: %s*\n", toolCall.Function.Name)
		if s.debug {
			resp = fmt.Sprintf("\n*开始执行工具: %s。请求参数: %s*\n", toolCall.Function.Name, string(toolCall.Function.Arguments))
		}
		rspBody := Response{
			Data: Data{
				Response: resp,
				GlobalOutput: GlobalOutput{
					AnswerSuccess: 1,
				},
			},
		}
		_, _ = w.Write([]byte(rspBody.String()))
		flusher.Flush()
		if len(toolCall.Function.Arguments) == 0 {
			log.InfoContextf(ctx, "Execute tool %s (ID: %s)\n", toolCall.Function.Name, toolCall.ID)
		} else {
			log.InfoContextf(ctx, "     Args: %s\n", string(toolCall.Function.Arguments))
		}
	}
	return true
}

// hasToolResponse 检查是否有工具响应
func (s *sseServiceImpl) hasToolResponse(ctx context.Context, event *event.Event) bool {
	if event.Response == nil || len(event.Response.Choices) == 0 {
		return false
	}
	for _, choice := range event.Response.Choices {
		if choice.Message.Role == model.RoleTool && choice.Message.ToolID != "" {
			log.InfoContextf(ctx, "✅ CallableTool response (ID: %s): %s\n",
				choice.Message.ToolID,
				strings.TrimSpace(choice.Message.Content))
			return true
		}
	}
	return false
}

// handleStreamContent 处理流式内容
func (s *sseServiceImpl) handleStreamContent(w http.ResponseWriter, flusher http.Flusher, event *event.Event) {
	if len(event.Choices) == 0 {
		return
	}
	choice := event.Choices[0]
	if choice.Delta.Content == "" {
		return
	}
	rspBody := Response{
		Data: Data{
			Response: choice.Delta.Content,
			Finished: false,
		},
	}
	_, _ = w.Write([]byte(rspBody.String()))
	flusher.Flush()
}

// handleFinalEvent 处理最终事件，返回是否是最终事件
func (s *sseServiceImpl) handleFinalEvent(w http.ResponseWriter, flusher http.Flusher, event *event.Event) bool {
	if !event.Done || isToolEvent(event) || event.Author != s.entranceAgent.Info().Name {
		return false
	}
	resp := ""
	if s.debug {
		resp = fmt.Sprintf("\nToken usage: %s\n", utils.MustToJSON(event.Usage))
	}
	rspBody := Response{
		Data: Data{
			Response: resp,
			Finished: true,
			GlobalOutput: GlobalOutput{
				AnswerSuccess: 1,
			},
		},
	}
	_, _ = w.Write([]byte(rspBody.String()))
	flusher.Flush()
	return true
}

// isToolEvent 检查事件是否为工具响应（非最终响应）
func isToolEvent(event *event.Event) bool {
	if event.Response == nil {
		return false
	}
	if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
		return true
	}
	if len(event.Choices) > 0 && event.Choices[0].Message.ToolID != "" {
		return true
	}

	for _, choice := range event.Response.Choices {
		if choice.Message.Role == model.RoleTool {
			return true
		}
	}

	return false
}
