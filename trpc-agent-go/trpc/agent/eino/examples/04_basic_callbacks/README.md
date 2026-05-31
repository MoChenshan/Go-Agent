# 📊 Basic Callbacks Integration

**展示内容**: 如何将Eino的基础回调(OnStart/OnEnd/OnError)转换后用于tRPC原生Agent。

## What This Shows

- 🔄 **回调复用**: 你的Eino监控逻辑可以用于tRPC Agent
- 📊 **分类监控**: 分别监控工具调用和模型调用
- 🎯 **节点过滤**: 可以选择性监控特定的工具或模型
- 📝 **零代码修改**: Eino回调代码保持不变

## Key APIs

```go
// 工具回调转换
toolCallbacks := teino.NewToolCallbacks(einoHandler,
    teino.WithCallbackNodeFilter("specific_tool"), // 可选：只监控特定工具
)

// 模型回调转换  
modelCallbacks := teino.NewModelCallbacks(einoHandler)

// 在tRPC Agent中使用
agent := llmagent.New("agent",
    llmagent.WithToolCallbacks(toolCallbacks),
    llmagent.WithModelCallbacks(modelCallbacks),
)
```

## Run It

```bash
cd 04_basic_callbacks
go run main.go
```

## Expected Output

```
📊 Basic Callbacks Integration
==============================

1️⃣ Tool Callbacks (OnStart/OnEnd/OnError)
------------------------------------------
  🎬 Running: Tool callback demo
  📝 Input: Please calculate 10 + 5
[ToolMonitor] 🚀 OnStart: calculator (type: tool)
[ToolMonitor] ✅ OnEnd: calculator
  🤖 Response: Mock response to: Please calculate 10 + 5
  📊 Check logs above for callback monitoring output

2️⃣ Model Callbacks (OnStart/OnEnd/OnError)  
-------------------------------------------
  🎬 Running: Model callback demo
  📝 Input: Hello, how are you?
[ModelMonitor] 🚀 OnStart: model-callback-model (type: model)
[ModelMonitor] ✅ OnEnd: model-callback-model
  🤖 Response: Mock response to: Hello, how are you?
  📊 Check logs above for callback monitoring output

3️⃣ Combined Tool + Model Callbacks
-----------------------------------
  🎬 Running: Combined callback demo
  📝 Input: Calculate 7 * 8 and tell me the weather
[FullMonitor] 🚀 OnStart: combined-callback-model (type: model)
[FullMonitor] ✅ OnEnd: combined-callback-model
[FullMonitor] 🚀 OnStart: calculator (type: tool)
[FullMonitor] ✅ OnEnd: calculator
  🤖 Response: Mock response to: Calculate 7 * 8 and tell me the weather
  📊 Check logs above for callback monitoring output
  💡 Notice: Only calculator tool is monitored (due to node filter)

✅ Basic callbacks integration complete!
💡 Your Eino monitoring logic now works with tRPC agents
```

## Use Cases

### ✅ 监控和日志
- 记录工具调用次数和耗时
- 监控模型调用情况
- 错误追踪和告警

### ✅ 性能分析
- 统计各个组件的执行时间
- 分析工具使用模式
- 优化Agent性能

### ✅ 调试和问题排查
- 详细的执行日志
- 错误发生时的上下文信息
- 请求链路追踪

## Callback Mapping

| Eino Callback | tRPC Callback | 触发时机 |
|--------------|---------------|----------|
| `OnStart` | `BeforeToolCallback` / `BeforeModelCallback` | 工具/模型调用前 |
| `OnEnd` | `AfterToolCallback` / `AfterModelCallback` | 工具/模型调用成功后 |
| `OnError` | `AfterToolCallback` / `AfterModelCallback` | 工具/模型调用失败后 |

## Next Steps

- **05_streaming_callbacks**: 如果需要复杂的流式回调处理
- **03_tool_conversion**: 如果还需要转换工具
