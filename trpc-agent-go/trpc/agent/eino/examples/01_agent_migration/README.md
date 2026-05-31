# 🔄 Complete Agent Migration Guide

**展示内容**: 全面的Eino Agent迁移模式，包括Chain、Graph、WorkFlow、ReAct Agent等不同类型的迁移方法。

## What This Shows

- 🔗 **Chain迁移**: 将Eino Chain适配为tRPC Agent
- 📊 **Graph迁移**: 复杂Graph结构的迁移模式
- 🔄 **WorkFlow迁移**: 步骤化工作流的迁移方法
- 🤖 **ReAct Agent迁移**: 特殊ReAct Agent的专门处理
- ⚙️ **配置选项**: 生产环境的性能调优参数

## Migration Patterns

### Chain Migration
```go
// 标准Eino Chain
chain := compose.NewChain[map[string]any, *schema.Message]()
chain.AppendLambda(businessLogic)
compiled, _ := chain.Compile(ctx)

// 迁移到tRPC Agent (带配置)
agent := teino.New(compiled, "chain-agent",
    teino.WithChunkSize(2048),
    teino.WithBufferSize(150),
)
```

### Graph Migration  
```go
// 标准Eino Graph
graph := compose.NewGraph[map[string]any, *schema.Message]()
graph.AddLambdaNode("processor", nodeLogic)
graph.AddEdge(compose.StartNodeName, "processor")
compiled, _ := graph.Compile(ctx)

// 迁移到tRPC Agent
agent := teino.New(compiled, "graph-agent")
```

### WorkFlow Migration
```go
// 标准的Eino WorkFlow
workflow := compose.NewWorkflow[map[string]any, any]()

// 添加工作流节点
workflow.AddLambdaNode("input_validation", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (any, error) {
    // 输入验证逻辑
    return map[string]any{
        "validated_input": userMsg,
        "status": "validated",
    }, nil
})).AddInput(compose.START)

workflow.AddLambdaNode("processing", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (any, error) {
    // 处理逻辑
    return &schema.Message{
        Role:    schema.Assistant,
        Content: fmt.Sprintf("WorkFlow processed: %s with step-by-step logic", validatedInput),
    }, nil
})).AddInput("input_validation")

// 设置工作流输出
workflow.End().AddInput("processing")

compiled, _ := workflow.Compile(ctx)

// 迁移到tRPC Agent，工作流特定选项
agent := teino.New(compiled, "workflow-agent",
    teino.WithChunkSize(2048),     // 工作流较大数据块
    teino.WithBufferSize(150),     // 更多步骤缓冲
)
```

### ReAct Agent Migration
```go
// 创建真实的ReAct Agent
func createRealReActAgent(ctx context.Context) *einoReact.Agent {
    // 实现正确的ChatModel接口
    mockModel := &properChatModel{}
    
    // 实现正确的BaseTool接口
    calculatorTool := &calculatorTool{}
    
    // 配置ReAct Agent
    config := &einoReact.AgentConfig{
        Model: mockModel,
        ToolsConfig: compose.ToolsNodeConfig{
            Tools: []einoTool.BaseTool{calculatorTool},
        },
        MaxStep: 3,
    }

    // 创建真实的eino ReAct Agent
    agent, _ := einoReact.NewAgent(ctx, config)
    return agent
}

// 迁移到tRPC Agent - 使用专门的ReAct包装器
reactAgent := createRealReActAgent(ctx)
agent := teino.NewReAct(reactAgent, "react-agent",
    teino.WithChunkSize(1024),
    teino.WithBufferSize(100),
    teino.WithDebug(false),
)
```



## Run It

```bash
cd 01_agent_migration
go run main.go
```

## Expected Output

```
🔄 Complete Agent Migration Guide
================================

1️⃣ Migrating Eino Chain
------------------------
  📝 Response: Chain processed: Chain migration test

2️⃣ Migrating Eino Graph
------------------------
  📝 Response: Graph processed: Graph migration test

3️⃣ Migrating Eino WorkFlow
---------------------------
  📝 Response: WorkFlow processed: your question with step-by-step logic

4️⃣ Migrating ReAct Agent
-------------------------
  📝 Response: ReAct Agent reasoning:
Thought: I need to process 'your question'
Action: Analyzing the request
Observation: Request understood
Final Answer: ReAct processed your request successfully
  💡 Note: For production ReAct Agents, use:
     reactAgent, _ := react.NewAgent(ctx, reactConfig)
     agent := teino.NewReAct(reactAgent, "react-agent")

✅ All migrations complete!
💡 Next: See 02_multiagent_integration for mixed environments
```

## Configuration Options

| Option | 用途 | 推荐值 |
|--------|------|--------|
| `WithChunkSize(size)` | 设置流式处理块大小 | 生产: 2048-4096 |
| `WithBufferSize(size)` | 设置事件缓冲区大小 | 生产: 100-200 |
| `WithDebug(bool)` | 开启调试模式 | 开发: true, 生产: false |

## When to Use Each Pattern

### ✅ Chain Migration (`teino.New`)
- 线性处理流程
- 简单的管道式操作
- 大多数基础Agent迁移

### ✅ Graph Migration (`teino.New`)  
- 复杂的条件分支
- 多路径执行流程
- 高级业务逻辑编排

### ✅ ReAct Migration (`teino.NewReAct`)
- 推理+行动模式的Agent
- 需要工具调用的复杂Agent
- 多步骤问题解决Agent

## Common Migration Steps

1. **保持Eino代码不变**: 你的Chain/Graph构建逻辑无需修改
2. **编译**: 使用标准的`Compile(ctx)`方法
3. **选择迁移方法**: `New()` vs `NewReAct()`
4. **添加配置**: 根据生产需求添加性能参数
5. **测试**: 验证迁移后的功能完整性

## Next Steps

- **02_multiagent_integration**: 在现有多Agent系统中集成
- **03_tool_conversion**: 如果需要迁移Eino工具
- **05_streaming_callbacks**: 如果需要复杂的流式处理
