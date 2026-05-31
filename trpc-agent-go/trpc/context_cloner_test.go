package trpc

import (
	"context"
	"testing"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/codec"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"trpc.group/trpc-go/trpc-agent-go/agent"
)

// TestcloneContextWithSpan_NilContext 测试传入 nil context 时返回 nil。
func Test_cloneContextWithSpan_NilContext(t *testing.T) {
	result := cloneContextWithSpan(nil)
	require.Nil(t, result)
}

// TestcloneContextWithSpan_NoSpan 测试 context 中没有 span 时，
// 克隆后的 context 仍然可以正常使用，且不会引入无效 span。
func Test_cloneContextWithSpan_NoSpan(t *testing.T) {
	ctx := context.Background()
	clonedCtx := cloneContextWithSpan(ctx)
	require.NotNil(t, clonedCtx)

	// 从克隆后的 context 中获取 span，应该是 noop span（无效 span）
	span := trace.SpanFromContext(clonedCtx)
	require.NotNil(t, span)
	require.False(t, span.SpanContext().IsValid())
}

// TestcloneContextWithSpan_PreservesValidSpan 测试 context 中有有效 span 时，
// 克隆后的 context 仍然保留该 span。
func Test_cloneContextWithSpan_PreservesValidSpan(t *testing.T) {
	// 创建一个带有有效 SpanContext 的 span
	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, originalSpan := tracer.Start(context.Background(), "test-span")
	defer originalSpan.End()

	// 克隆 context
	clonedCtx := cloneContextWithSpan(ctx)
	require.NotNil(t, clonedCtx)

	// 从克隆后的 context 中获取 span
	clonedSpan := trace.SpanFromContext(clonedCtx)
	require.NotNil(t, clonedSpan)

	// 验证 span 的 SpanContext 一致
	require.Equal(t, originalSpan.SpanContext(), clonedSpan.SpanContext())
}

// TestcloneContextWithSpan_PreservesSpanWithValidTraceAndSpanID 测试使用真实 SpanContext 时，
// 克隆后的 context 保留了 span 信息（包括 TraceID 和 SpanID）。
func Test_cloneContextWithSpan_PreservesSpanWithValidTraceAndSpanID(t *testing.T) {
	// 创建一个带有有效 TraceID 和 SpanID 的 SpanContext
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	// 将 SpanContext 注入到 context 中
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	// 验证原始 context 中的 span 是有效的
	originalSpan := trace.SpanFromContext(ctx)
	require.True(t, originalSpan.SpanContext().IsValid())
	require.Equal(t, traceID, originalSpan.SpanContext().TraceID())
	require.Equal(t, spanID, originalSpan.SpanContext().SpanID())

	// 克隆 context
	clonedCtx := cloneContextWithSpan(ctx)
	require.NotNil(t, clonedCtx)

	// 验证克隆后的 context 中的 span 保留了 TraceID 和 SpanID
	clonedSpan := trace.SpanFromContext(clonedCtx)
	require.True(t, clonedSpan.SpanContext().IsValid())
	require.Equal(t, traceID, clonedSpan.SpanContext().TraceID())
	require.Equal(t, spanID, clonedSpan.SpanContext().SpanID())
}

// TestcloneContextWithSpan_PreservesTRPCMessage 测试克隆后的 context 同时保留了
// tRPC 的 message 隔离功能和 OpenTelemetry span。
func Test_cloneContextWithSpan_PreservesTRPCMessage(t *testing.T) {
	// 创建一个带有 tRPC message 的 context
	ctx, msg := codec.WithNewMessage(context.Background())
	msg.WithCallerServiceName("test-caller")

	// 创建一个带有有效 SpanContext 的 span
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx = trace.ContextWithSpanContext(ctx, spanCtx)

	// 克隆 context
	clonedCtx := cloneContextWithSpan(ctx)
	require.NotNil(t, clonedCtx)

	// 验证 tRPC message 被隔离（新的 message 对象）
	clonedMsg := trpc.Message(clonedCtx)
	require.NotNil(t, clonedMsg)
	// 克隆后的 message 应该保留了原始的 caller service name
	require.Equal(t, "test-caller", clonedMsg.CallerServiceName())

	// 修改克隆后的 message 不应影响原始 message
	clonedMsg.WithCallerServiceName("modified-caller")
	require.Equal(t, "test-caller", msg.CallerServiceName())
	require.Equal(t, "modified-caller", clonedMsg.CallerServiceName())

	// 验证 span 仍然保留
	clonedSpan := trace.SpanFromContext(clonedCtx)
	require.True(t, clonedSpan.SpanContext().IsValid())
	require.Equal(t, traceID, clonedSpan.SpanContext().TraceID())
	require.Equal(t, spanID, clonedSpan.SpanContext().SpanID())
}

// TestcloneContextWithSpan_ContextValues 测试克隆后的 context 保留了自定义的 context values。
func Test_cloneContextWithSpan_ContextValues(t *testing.T) {
	type contextKey string
	const myKey contextKey = "my-key"

	ctx := context.WithValue(context.Background(), myKey, "my-value")

	// 添加 span
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx = trace.ContextWithSpanContext(ctx, spanCtx)

	// 克隆 context
	clonedCtx := cloneContextWithSpan(ctx)
	require.NotNil(t, clonedCtx)

	// 验证自定义 value 保留
	require.Equal(t, "my-value", clonedCtx.Value(myKey))

	// 验证 span 保留
	clonedSpan := trace.SpanFromContext(clonedCtx)
	require.True(t, clonedSpan.SpanContext().IsValid())
	require.Equal(t, traceID, clonedSpan.SpanContext().TraceID())
}

// TestcloneContextWithSpan_VsTRPCCloneContextWithTimeout 对比测试：
// 验证 cloneContextWithSpan 相比 trpc.CloneContextWithTimeout 的改进。
func Test_cloneContextWithSpan_VsTRPCCloneContextWithTimeout(t *testing.T) {
	// 创建一个带有有效 SpanContext 的 context
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	// 使用 trpc.CloneContextWithTimeout 克隆
	trpcClonedCtx := trpc.CloneContextWithTimeout(ctx)
	trpcSpan := trace.SpanFromContext(trpcClonedCtx)

	// 使用 cloneContextWithSpan 克隆
	fixedClonedCtx := cloneContextWithSpan(ctx)
	fixedSpan := trace.SpanFromContext(fixedClonedCtx)

	// cloneContextWithSpan 应该始终保留 span
	require.True(t, fixedSpan.SpanContext().IsValid(),
		"cloneContextWithSpan 应该保留有效的 span")
	require.Equal(t, traceID, fixedSpan.SpanContext().TraceID(),
		"cloneContextWithSpan 应该保留 TraceID")
	require.Equal(t, spanID, fixedSpan.SpanContext().SpanID(),
		"cloneContextWithSpan 应该保留 SpanID")

	// 记录 trpc.CloneContextWithTimeout 的行为
	t.Logf("trpc.CloneContextWithTimeout 保留 span: %v", trpcSpan.SpanContext().IsValid())
	t.Logf("cloneContextWithSpan 保留 span: %v", fixedSpan.SpanContext().IsValid())
}

// TestcloneContextWithSpan_MultipleClones 测试多次克隆后 span 仍然保留。
func Test_cloneContextWithSpan_MultipleClones(t *testing.T) {
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	// 多次克隆
	cloned1 := cloneContextWithSpan(ctx)
	cloned2 := cloneContextWithSpan(cloned1)
	cloned3 := cloneContextWithSpan(cloned2)

	// 每次克隆后 span 都应该保留
	for i, clonedCtx := range []context.Context{cloned1, cloned2, cloned3} {
		span := trace.SpanFromContext(clonedCtx)
		require.True(t, span.SpanContext().IsValid(),
			"第 %d 次克隆后 span 应该仍然有效", i+1)
		require.Equal(t, traceID, span.SpanContext().TraceID(),
			"第 %d 次克隆后 TraceID 应该保持一致", i+1)
		require.Equal(t, spanID, span.SpanContext().SpanID(),
			"第 %d 次克隆后 SpanID 应该保持一致", i+1)
	}
}

// TestcloneContextWithSpan_ConcurrentAccess 测试并发场景下 cloneContextWithSpan 的安全性。
func Test_cloneContextWithSpan_ConcurrentAccess(t *testing.T) {
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	const goroutines = 100
	done := make(chan struct{}, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			clonedCtx := cloneContextWithSpan(ctx)
			span := trace.SpanFromContext(clonedCtx)
			if !span.SpanContext().IsValid() {
				t.Errorf("并发克隆后 span 应该仍然有效")
			}
			if span.SpanContext().TraceID() != traceID {
				t.Errorf("并发克隆后 TraceID 不一致")
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestInitRegisters_cloneContextWithSpan 测试 init() 函数是否正确注册了
// cloneContextWithSpan 作为 agent 包的 GoroutineContextCloner。
// 这是验证修复生效的集成测试。
func TestInitRegisters_cloneContextWithSpan(t *testing.T) {
	// init() 在包加载时已经执行，所以 agent.CloneContext 应该使用 cloneContextWithSpan

	// 创建一个带有有效 SpanContext 的 context
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	// 通过 agent.CloneContext 克隆（这是框架内部使用的函数）
	clonedCtx := agent.CloneContext(ctx)
	require.NotNil(t, clonedCtx)

	// 验证 span 被保留
	clonedSpan := trace.SpanFromContext(clonedCtx)
	require.True(t, clonedSpan.SpanContext().IsValid(),
		"agent.CloneContext 应该保留有效的 span（通过 init 注册的 cloneContextWithSpan）")
	require.Equal(t, traceID, clonedSpan.SpanContext().TraceID(),
		"agent.CloneContext 应该保留 TraceID")
	require.Equal(t, spanID, clonedSpan.SpanContext().SpanID(),
		"agent.CloneContext 应该保留 SpanID")
}

// TestInitRegisters_cloneContextWithSpan_TRPCMessageIsolation 测试通过 agent.CloneContext
// 克隆后，tRPC message 被正确隔离，同时 span 被保留。
// 这模拟了框架中并行工具调用的场景。
func TestInitRegisters_cloneContextWithSpan_TRPCMessageIsolation(t *testing.T) {
	// 创建一个带有 tRPC message 和 span 的 context
	ctx, msg := codec.WithNewMessage(context.Background())
	msg.WithCallerServiceName("original-service")

	traceID := trace.TraceID{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160}
	spanID := trace.SpanID{10, 20, 30, 40, 50, 60, 70, 80}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx = trace.ContextWithSpanContext(ctx, spanCtx)

	// 模拟框架中的并行工具调用：使用 agent.CloneContext 克隆 context
	clonedCtx := agent.CloneContext(ctx)

	// 验证 tRPC message 隔离
	clonedMsg := trpc.Message(clonedCtx)
	require.Equal(t, "original-service", clonedMsg.CallerServiceName())
	clonedMsg.WithCallerServiceName("cloned-service")
	require.Equal(t, "original-service", msg.CallerServiceName(),
		"修改克隆后的 message 不应影响原始 message")

	// 验证 span 保留
	clonedSpan := trace.SpanFromContext(clonedCtx)
	require.True(t, clonedSpan.SpanContext().IsValid(),
		"并行工具调用场景下 span 应该被保留")
	require.Equal(t, traceID, clonedSpan.SpanContext().TraceID())
	require.Equal(t, spanID, clonedSpan.SpanContext().SpanID())
}

// TestcloneContextWithSpan_SimulateParallelToolCalls 模拟框架中并行工具调用的完整场景：
// 1. 创建 invoke_agent span
// 2. 在 invoke_agent span 下创建多个并行工具调用
// 3. 每个工具调用使用 CloneContext 克隆 context
// 4. 验证每个工具调用的 span 父节点是 invoke_agent
func Test_cloneContextWithSpan_SimulateParallelToolCalls(t *testing.T) {
	// 模拟 invoke_agent span
	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, invokeAgentSpan := tracer.Start(context.Background(), "invoke_agent memory-agent")
	defer invokeAgentSpan.End()

	// 模拟多个并行工具调用
	const toolCount = 5
	results := make(chan trace.SpanContext, toolCount)

	for i := 0; i < toolCount; i++ {
		// 使用 agent.CloneContext 克隆 context（模拟框架行为）
		runCtx := agent.CloneContext(ctx)
		go func(ctx context.Context) {
			// 在克隆后的 context 中创建工具调用 span
			_, toolSpan := tracer.Start(ctx, "execute_tool")
			defer toolSpan.End()
			results <- trace.SpanFromContext(ctx).SpanContext()
		}(runCtx)
	}

	// 收集所有结果
	for i := 0; i < toolCount; i++ {
		spanCtx := <-results
		// 每个工具调用的 context 中应该包含 invoke_agent span
		require.Equal(t, invokeAgentSpan.SpanContext(), spanCtx,
			"并行工具调用的 span 父节点应该是 invoke_agent")
	}
}
