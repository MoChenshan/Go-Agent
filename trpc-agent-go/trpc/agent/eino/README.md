# 🚀 Eino ↔ tRPC-Agent-Go 集成方案

让你的 [Eino](https://github.com/cloudwego/eino) 代码快速集成到 [tRPC-Agent-Go](https://github.com/trpc-group/trpc-agent-go) 生态系统中。

## ✨ 我们的框架能做什么？

- 🔄 **Agent 迁移**：一行代码迁移 Chain/Graph/Workflow/ReAct Agent
- 🛠️ **工具转换**：Eino 工具自动转换为 tRPC 工具
- 📞 **回调集成**：OnStart/OnEnd/OnError 回调无缝集成
- 🌊 **流式处理**：OnEndWithStreamOutput 流式回调完整支持
- 🤝 **混合模式**：Eino Agent 与 tRPC 原生 Agent 协作

## 🚀 快速开始

### 1. 安装依赖

```bash
go get git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino
```

### 2. 快速集成（最常用）

```go
import teino "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino"

// 你现有的 Eino 代码
einoChain := compose.NewChain(...)
compiled, _ := einoChain.Compile(ctx)

// 一行代码完成迁移！
agent := teino.New(compiled, "agent-name")

// 在 tRPC 生态中使用
runner := runner.NewRunner("app", agent)
```

**就这么简单！** 完整示例: [`examples/00_quickstart/`](./examples/00_quickstart/)

## 📋 集成策略指南

### 🎯 选择一：使用 Eino 作为 Agent ⭐

**适合**: 现有 Eino 项目快速接入 tRPC 生态

保留现有 Eino 逻辑（工具、回调都不变），直接包装为 tRPC Agent：

```go
// 1. Chain 迁移
compiled, _ := einoChain.Compile(ctx)
agent := teino.New(compiled, "chain-agent")

// 2. Graph 迁移
compiled, _ := einoGraph.Compile(ctx)
agent := teino.New(compiled, "graph-agent")

// 3. Workflow 迁移
compiled, _ := einoWorkflow.Compile(ctx)
agent := teino.New(compiled, "workflow-agent")

// 4. ReAct Agent 迁移
reactAgent, _ := react.NewAgent(ctx, config)
agent := teino.NewReAct(reactAgent, "react-agent")

// 原有的 Eino 工具和回调（包括流式回调）都会自动保留
```

**使用方式**:
- **单独使用**: `runner.NewRunner("app", agent)` 
- **multiAgent 系统**: 作为其中一个 Agent，与其他 tRPC Agent 协作 🌟

**优势**: 零修改迁移，现有 Eino 工具和所有回调完全保留，可灵活集成到 multiAgent 架构

### 🎯 选择二：使用 tRPC-Agent-Go 原生 Agent

**适合**: 深度集成 tRPC 生态，享受原生性能

需要将 Eino 组件转换为 tRPC 组件：

```go
// 1. 工具转换
einoTool := &MyEinoTool{}
trpcTool := teino.NewTool(einoTool, "calculator")

// 2. 普通回调转换（OnStart/OnEnd/OnError）
handler := &MyEinoHandler{}
toolCallbacks := teino.NewToolCallbacks(handler)
modelCallbacks := teino.NewModelCallbacks(handler)

// 3. 创建 tRPC 原生 Agent
agent := llmagent.New("service",
    llmagent.WithTools([]tool.Tool{trpcTool}),
    llmagent.WithToolCallbacks(toolCallbacks),
    llmagent.WithModelCallbacks(modelCallbacks),
)

// 4. 如需流式回调支持
streamAgent := teino.NewStreamCallbackAgent(agent,
    teino.WithEinoCallbacks(streamHandler),
)
```

**优势**: 原生性能，完整 tRPC 功能，灵活配置

### 📚 示例索引

| 需求场景 | 推荐示例 | 说明 |
|---------|---------|------|
| 🚀 最快迁移 | [`00_quickstart`](./examples/00_quickstart/) | 一行代码迁移 Eino Agent |
| 🔄 全面迁移 | [`01_agent_migration`](./examples/01_agent_migration/) | Chain/Graph/Workflow/ReAct 全覆盖 |
| 🤝 混合使用 | [`02_multiagent_integration`](./examples/02_multiagent_integration/) | Eino + tRPC 原生 Agent 协作 |
| 🛠️ 工具转换 | [`03_tool_conversion`](./examples/03_tool_conversion/) | Eino 工具转 tRPC 工具 |
| 📞 普通回调 | [`04_basic_callbacks`](./examples/04_basic_callbacks/) | 基础回调集成（OnStart/OnEnd/OnError）|
| 🌊 流式回调 | [`05_streaming_callbacks`](./examples/05_streaming_callbacks/) | 高级流式回调处理 |
| 🏢 生产配置 | [`06_complete_scenario`](./examples/06_complete_scenario/) | 完整生产环境配置 |
| 🌟 真实大模型 | [`07_real_llm_multiagent`](./examples/07_real_llm_multiagent/) | 真实LLM驱动的MultiAgent系统 |

## 🎯 Eino 回调支持

### 支持的回调接口

我们支持 Eino 的主要回调接口：

- ✅ **OnStart** - 组件开始执行（普通回调 + 流式回调）
- ✅ **OnEnd** - 组件执行完成（普通回调 + 流式回调）
- ✅ **OnError** - 组件执行错误（普通回调 + 流式回调）
- ✅ **OnEndWithStreamOutput** - 流式输出处理（仅流式回调）
- ❌ **OnStartWithStreamInput** - 流式输入处理（tRPC 架构不支持）

**使用说明**：
- **普通回调**（`NewToolCallbacks`/`NewModelCallbacks`）: 只支持前3个接口
- **流式回调**（`NewStreamCallbackAgent`）: 支持所有4个有效接口

详细回调用法请参考: [`examples/04_basic_callbacks/`](./examples/04_basic_callbacks/) 和 [`examples/05_streaming_callbacks/`](./examples/05_streaming_callbacks/)


## 🔗 更多资源

- [Eino 框架](https://github.com/cloudwego/eino)
- [tRPC-Agent-Go](https://github.com/trpc-group/trpc-agent-go)
