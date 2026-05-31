# 🌊 Streaming Callbacks Integration

**展示内容**: 如何使用StreamCallbackAgent处理复杂的流式回调，如ChatBuffer等高级流式处理场景。

## What This Shows

- 🌊 **复杂流式处理**: 支持复杂的OnEndWithStreamOutput回调
- 🧠 **智能缓冲**: 实现ChatBuffer类型的智能流式缓冲
- 🎯 **节点过滤**: 只处理特定节点的流式输出
- 🔄 **透明集成**: 流式处理对Agent使用者完全透明

## Key API

```go
// 创建StreamCallbackAgent
streamCallbackAgent := teino.NewStreamCallbackAgent(baseAgent,
    teino.WithEinoCallbacks(yourStreamingHandler),
    teino.WithNodeFilter("chat_model"), // 可选：只处理特定节点
)

// 正常使用，流式处理在后台进行
runner := runner.NewRunner("app", streamCallbackAgent)
```

## Run It

```bash
cd 05_streaming_callbacks  
go run main.go
```

## Expected Output

```
🌊 Streaming Callbacks Integration
==================================

1️⃣ Simple Streaming Callback
-----------------------------
  🎬 Running: Simple streaming demo
  📝 Input: Tell me a story
[SimpleStreaming] 📦 Stream chunk 1 from streaming_model
[SimpleStreaming] 📦 Stream chunk 2 from streaming_model
[SimpleStreaming] 📦 Stream ended for streaming_model, processed 5 chunks
  🤖 Response: Mock response to: Tell me a story
  🌊 Streaming callback processing completed

2️⃣ ChatBuffer-style Streaming
------------------------------
  🎬 Running: ChatBuffer demo
  📝 Input: Generate a detailed response about AI
[ChatBuffer] 💬 Processing streaming output from chat_model
[ChatBuffer] 📤 Buffered content: Mock streaming response to: Generate
[ChatBuffer] 📤 Buffered content: a detailed response about AI
[ChatBuffer] 📝 Chat buffer processing complete
  🤖 Response: Mock streaming response to: Generate a detailed response about AI
  🌊 Streaming callback processing completed
  💡 Check logs above for intelligent buffering output

3️⃣ Streaming with Node Filtering
---------------------------------
  🎬 Running: Filtered streaming demo
  📝 Input: Process this complex request
[FilteredBuffer] 💬 Processing streaming output from filtered-model
[FilteredBuffer] 📤 Buffered content: Mock streaming response to: Process
[FilteredBuffer] 📤 Buffered content: this complex request
[FilteredBuffer] 📝 Chat buffer processing complete
  🤖 Response: Mock streaming response to: Process this complex request
  🌊 Streaming callback processing completed
  🎯 Only specified nodes are processed by the streaming callback

✅ Streaming callbacks integration complete!
💡 Your complex Eino streaming logic (like ChatBuffer) now works with tRPC!
```

## Use Cases

### ✅ ChatBuffer实现
- 智能流式内容缓冲
- 实时内容分类处理
- thinking/content模式切换

### ✅ 实时数据处理
- 流式数据转换
- 实时事件生成
- WebSocket消息转发

### ✅ 高级监控
- 流式性能监控
- 实时数据统计
- 流量分析

## Architecture

```
User Request
     ↓
tRPC Agent (base)
     ↓
StreamCallbackAgent (wrapper)
     ↓                    ↓
Original Stream    →   Eino Streaming Callback
     ↓                    ↓
User Response      ←   Your Processing Logic
```

## Important Notes

### ⚠️ OnStartWithStreamInput限制
- tRPC架构基于单Message输入，不支持流式输入
- OnStartWithStreamInput在tRPC环境下不会被触发
- 这不影响OnEndWithStreamOutput的正常使用

### ✅ 完全支持的回调
- ✅ OnStart - 组件开始
- ✅ OnEnd - 组件结束  
- ✅ OnError - 错误处理
- ✅ **OnEndWithStreamOutput** - 流式输出处理(重点)

## Next Steps

- **06_complete_scenario**: 查看生产级的完整集成示例
- **01_agent_migration**: 如果还需要迁移Agent
- **03_tool_conversion**: 如果还需要转换工具
