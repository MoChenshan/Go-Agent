package main

import (
	"context"
	"fmt"
	"log"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"

	lkeevent "github.com/tencent-lke/lke-sdk-go/event"

	lkeeventhandler "github.com/tencent-lke/lke-sdk-go/eventhandler"

	lketool "github.com/tencent-lke/lke-sdk-go/tool"
)

// -----------------------------
// "原本的 LKE 业务代码"（示例）
// -----------------------------
//
// 这里模拟业务侧已经存在的两类资产：
// 1) LKE 的回调处理（EventHandler）——用于日志/埋点/队列写入等 side effects
// 2) 业务工具（FunctionTool）——用于被 LKE 调用
//
// 适配层会保留这些 side effects，同时把事件转换成 trpc-agent-go 的 event stream。

type originalEventHandler struct{}

func newOriginalEventHandler() *originalEventHandler { return &originalEventHandler{} }

func (h *originalEventHandler) OnReply(reply *lkeevent.ReplyEvent) {
	if reply == nil {
		return
	}
	if !reply.IsFromSelf && reply.IsFinal {
		log.Printf("[original handler] final reply: %s", reply.Content)
	}
}

func (h *originalEventHandler) OnThought(thought *lkeevent.AgentThoughtEvent) {
	if thought == nil || len(thought.Procedures) == 0 {
		return
	}
	last := thought.Procedures[len(thought.Procedures)-1]
	log.Printf("[original handler] thought: %s", last.Debugging.Content)
}

func (h *originalEventHandler) OnError(err *lkeevent.ErrorEvent) {
	if err == nil {
		return
	}
	if err.Error.Message != "" {
		log.Printf("[original handler] error: %s", err.Error.Message)
		return
	}
	log.Printf("[original handler] error: %+v", err.Error)
}

func (h *originalEventHandler) OnReference(_ *lkeevent.ReferenceEvent) {}

func (h *originalEventHandler) OnTokenStat(stat *lkeevent.TokenStatEvent) {
	if stat == nil {
		return
	}
	log.Printf("[original handler] token usage used=%d total=%d", stat.UsedCount, stat.TokenCount)
}

func (h *originalEventHandler) BeforeToolCallHook(toolCallCtx lkeeventhandler.ToolCallContext) {
	log.Printf("[original handler] tool start name=%s", toolCallCtx.CallToolName)
}

func (h *originalEventHandler) AfterToolCallHook(toolCallCtx lkeeventhandler.ToolCallContext) {
	if toolCallCtx.Err != nil {
		log.Printf("[original handler] tool failed name=%s err=%v", toolCallCtx.CallToolName, toolCallCtx.Err)
		return
	}
	log.Printf("[original handler] tool done name=%s", toolCallCtx.CallToolName)
}

type exampleRunLogger struct {
	ctx       context.Context
	sessionID string
}

func (l *exampleRunLogger) Info(msg string) {
	log.Printf("[lke sdk] info session=%s msg=%s", l.sessionID, msg)
}

func (l *exampleRunLogger) Error(msg string) {
	log.Printf("[lke sdk] error session=%s msg=%s", l.sessionID, msg)
}

type localActionTool struct{}

func (t *localActionTool) Name() string { return "local_action" }

func (t *localActionTool) Description() string { return "Execute a local action" }

func (t *localActionTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string", "description": "action input"},
		},
		"required": []string{"input"},
	}
}

func (t *localActionTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	input, _ := params["input"].(string)
	return fmt.Sprintf("local_action finished: input=%s", input), nil
}

func newLocalActionFunctionTool() (*lketool.FunctionTool, error) {
	toolImpl := &localActionTool{}
	return lketool.NewFunctionTool(
		toolImpl.Name(),
		toolImpl.Description(),
		toolImpl.Execute,
		toolImpl.Schema(),
	)
}
