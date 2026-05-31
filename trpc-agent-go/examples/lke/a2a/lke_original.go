package main

import (
	"context"
	"fmt"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"

	lkeevent "github.com/tencent-lke/lke-sdk-go/event"

	lkeeventhandler "github.com/tencent-lke/lke-sdk-go/eventhandler"

	lketool "github.com/tencent-lke/lke-sdk-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// -----------------------------
// "原本的 LKE 业务代码"（示例）
// -----------------------------
//
// 这里模拟业务侧已经存在的两类资产：
// 1) LKE 的回调处理（EventHandler）——用于日志/埋点/队列写入等 side effects
// 2) 业务工具（FunctionTool）——用于被 LKE 调用

type originalEventHandler struct{}

func newOriginalEventHandler() *originalEventHandler { return &originalEventHandler{} }

func (h *originalEventHandler) OnReply(reply *lkeevent.ReplyEvent) {
	if reply == nil {
		return
	}
	if !reply.IsFromSelf && reply.IsFinal {
		log.Infof("[lke original handler] final reply generated")
	}
}

func (h *originalEventHandler) OnThought(thought *lkeevent.AgentThoughtEvent) {
	if thought == nil {
		return
	}
	if len(thought.Procedures) > 0 {
		log.Infof("[lke original handler] thought event received")
	}
}

func (h *originalEventHandler) OnError(err *lkeevent.ErrorEvent) {
	if err == nil {
		return
	}
	log.Warnf("[lke original handler] error: code=%d", err.Error.Code)
}

func (h *originalEventHandler) OnReference(_ *lkeevent.ReferenceEvent) {}

func (h *originalEventHandler) OnTokenStat(stat *lkeevent.TokenStatEvent) {
	if stat == nil {
		return
	}
	log.Infof("[lke original handler] token stat used=%d", stat.UsedCount)
}

func (h *originalEventHandler) BeforeToolCallHook(toolCallCtx lkeeventhandler.ToolCallContext) {
	log.Infof("[lke original handler] tool start: %s", toolCallCtx.CallToolName)
}

func (h *originalEventHandler) AfterToolCallHook(toolCallCtx lkeeventhandler.ToolCallContext) {
	if toolCallCtx.Err != nil {
		log.Warnf("[lke original handler] tool failed: %s", toolCallCtx.CallToolName)
		return
	}
	log.Infof("[lke original handler] tool done: %s", toolCallCtx.CallToolName)
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
