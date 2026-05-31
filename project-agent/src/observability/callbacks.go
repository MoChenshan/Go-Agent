// Package observability 的 Agent 回调适配器：把 OTel Span / Counter
// 接入 trpc-agent-go 的 model.Callbacks / tool.Callbacks 抽象。
//
// 为什么不直接在 agents 包里写：
//   - 保持 observability 包"可独立禁用"的边界：移除 Init 调用 + 本文件，
//     即可完全脱离 OTel 依赖
//   - 集中 GenAI Semantic Convention 的实现，避免各 Agent 重复写 attribute key
package observability

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	agentmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ---------------------------------------------------------------------------
// LLM Model Callbacks
//
// BeforeModel：开启 Span，挂到 ctx（借助 span key）。
// AfterModel ：关闭 Span，写入 usage / finish_reason / 错误。
//
// 由于 trpc-agent-go 的 BeforeModel 返回的 ctx 不会被 AfterModel 拿到（两个
// 回调拿到的是同一次 Request 的 *model.Request / *model.Response，但 ctx
// 是同一棵"生命周期 ctx"），这里采用 sync.Map 以 Request 指针为 key 暂存
// Span 引用；AfterModel 取出并 End。参考 OTel 官方 SDK 里"trpc-a2a-go
// middleware"同款套路。
// ---------------------------------------------------------------------------

// modelSpanRegistry 按 *Request 指针索引正在进行的 Span。
var modelSpanRegistry sync.Map // key: *agentmodel.Request, value: trace.Span

// NewLLMModelCallback 返回可直接 append 到全局 BeforeModel / AfterModel 的回调对。
func NewLLMModelCallback() (
	before agentmodel.BeforeModelCallbackStructured,
	after agentmodel.AfterModelCallbackStructured,
) {
	before = func(ctx context.Context, args *agentmodel.BeforeModelArgs) (*agentmodel.BeforeModelResult, error) {
		if args == nil || args.Request == nil {
			return nil, nil
		}
		req := args.Request
		agentName := agentNameFromCtx(ctx)
		opts := LLMSpanOptions{
			Operation:      OperationChat,
			Model:          modelNameFromCtx(ctx),
			AgentName:      agentName,
			ConversationID: conversationFromCtx(ctx),
		}
		applyGenConfig(&opts, req)
		_, span := StartLLMSpan(ctx, opts)
		modelSpanRegistry.Store(req, span)
		return nil, nil
	}

	after = func(ctx context.Context, args *agentmodel.AfterModelArgs) (*agentmodel.AfterModelResult, error) {
		if args == nil || args.Request == nil {
			return nil, nil
		}
		v, ok := modelSpanRegistry.LoadAndDelete(args.Request)
		if !ok {
			return nil, nil
		}
		span, _ := v.(trace.Span)

		res := LLMSpanResult{}
		if args.Response != nil {
			res.ResponseModel = args.Response.Model
			if args.Response.Usage != nil {
				res.InputTokens = args.Response.Usage.PromptTokens
				res.OutputTokens = args.Response.Usage.CompletionTokens
			}
			res.FinishReasons = extractFinishReasons(args.Response)
			if args.Response.Error != nil {
				res.Err = toError(args.Response.Error.Message)
			}
		}
		FinishLLMSpan(span, res)

		status := StatusOK
		if res.Err != nil {
			status = StatusError
		}
		IncAgentLLMCall(ctx, agentNameFromCtx(ctx), status)
		return nil, nil
	}
	return before, after
}

// ---------------------------------------------------------------------------
// Tool Callbacks
// ---------------------------------------------------------------------------

// toolSpanRegistry 按 *BeforeToolArgs 索引；BeforeTool / AfterTool 都可能拿到同一个指针。
var toolSpanRegistry sync.Map // key: string "ToolName@<args-ptr>", value: trace.Span

// NewToolSpanCallback 返回可 Register 到任意 *tool.Callbacks 的 Before/After 对。
// 由调用方决定挂到哪些 Agent（通常是有 FunctionTool 的 RepairAgent/DiagnosisAgent）。
func NewToolSpanCallback() (
	before tool.BeforeToolCallbackStructured,
	after tool.AfterToolCallbackStructured,
) {
	before = func(ctx context.Context, args *tool.BeforeToolArgs) (*tool.BeforeToolResult, error) {
		if args == nil {
			return nil, nil
		}
		_, span := StartToolSpan(ctx, ToolSpanOptions{
			ToolName:  args.ToolName,
			AgentName: agentNameFromCtx(ctx),
		})
		toolSpanRegistry.Store(toolKey(args), span)
		return nil, nil
	}

	after = func(ctx context.Context, args *tool.AfterToolArgs) (*tool.AfterToolResult, error) {
		if args == nil {
			return nil, nil
		}
		// AfterTool 可能拿到不同指针（框架 wrap 后新建了 args），
		// 这里用 ToolName 兜底匹配最早的 entry（保证总能 End）。
		span := popToolSpan(args)
		FinishToolSpan(span, args.Error)

		status := StatusOK
		if args.Error != nil {
			status = StatusError
		}
		IncAgentToolCall(ctx, agentNameFromCtx(ctx), args.ToolName, status)
		return nil, nil
	}
	return before, after
}

// toolKey 从 BeforeToolArgs 拼 key。
func toolKey(args *tool.BeforeToolArgs) string {
	return args.ToolName
}

// popToolSpan 取最早的同名 span（兼容 before/after 参数指针不一致的情况）。
func popToolSpan(args *tool.AfterToolArgs) trace.Span {
	if args == nil {
		return nil
	}
	if v, ok := toolSpanRegistry.LoadAndDelete(args.ToolName); ok {
		sp, _ := v.(trace.Span)
		return sp
	}
	return nil
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

func agentNameFromCtx(ctx context.Context) string {
	if inv, ok := agent.InvocationFromContext(ctx); ok && inv != nil && inv.AgentName != "" {
		return inv.AgentName
	}
	return ""
}

func conversationFromCtx(ctx context.Context) string {
	if inv, ok := agent.InvocationFromContext(ctx); ok && inv != nil {
		return inv.InvocationID
	}
	return ""
}

// modelNameFromCtx 从 invocation 的 Model 接口提取模型名。
//
// 背景：上游 trpc-agent-go 的 *model.Request 自 v0.x 起移除了 Model string 字段，
// 模型名统一从 Invocation.Model.Info().Name 取。这里做一次空值保护，
// 不强制要求 Model 接口非 nil，便于单测里 mock。
func modelNameFromCtx(ctx context.Context) string {
	inv, ok := agent.InvocationFromContext(ctx)
	if !ok || inv == nil || inv.Model == nil {
		return ""
	}
	return inv.Model.Info().Name
}

func applyGenConfig(opts *LLMSpanOptions, req *agentmodel.Request) {
	if req == nil {
		return
	}
	g := req.GenerationConfig
	if g.Temperature != nil {
		t := *g.Temperature
		opts.Temperature = &t
	}
	if g.TopP != nil {
		tp := *g.TopP
		opts.TopP = &tp
	}
	if g.MaxTokens != nil {
		opts.MaxTokens = *g.MaxTokens
	}
}

func extractFinishReasons(resp *agentmodel.Response) []string {
	if resp == nil || len(resp.Choices) == 0 {
		return nil
	}
	out := make([]string, 0, len(resp.Choices))
	for _, c := range resp.Choices {
		// 上游 v0.x 起 Choice.FinishReason 改为 *string，
		// 空指针视为未提供，不参与上报；非空才转成字符串塞进 span。
		if c.FinishReason != nil && *c.FinishReason != "" {
			out = append(out, *c.FinishReason)
		}
	}
	return out
}

// toError 把字符串包装成 error 以便 RecordError。
type stringErr string

func (s stringErr) Error() string { return string(s) }
func toError(msg string) error {
	if msg == "" {
		return nil
	}
	return stringErr(msg)
}
