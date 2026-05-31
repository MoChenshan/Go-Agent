# 🏢 Multi-Agent Integration

**展示内容**: 如何在现有的多Agent系统中集成Eino Agent，实现渐进式迁移。

## What This Shows

- 🔄 **渐进式迁移**: 不需要一次性替换所有Agent
- 🤝 **混合环境**: Eino Agent和tRPC Native Agent并存
- 📈 **平滑升级**: 现有系统零中断迁移

## Key Concepts

1. **保留现有Agent**: 你的tRPC Native Agent继续工作
2. **逐步添加Eino**: 新功能使用migrated Eino Agent
3. **统一管理**: 两种Agent使用相同的Runner和Session

## Code Highlights

```go
// 现有的tRPC Agent (不变)
nativeAgent := llmagent.New("native-agent",
    llmagent.WithModel(mockModel),
    llmagent.WithTools(tools),
)

// 新迁移的Eino Agent
einoAgent := teino.New(compiledChain, "eino-agent")

// 两者可以在同一系统中使用
runner1 := runner.NewRunner("app-native", nativeAgent)
runner2 := runner.NewRunner("app-eino", einoAgent)
```

## Run It

```bash
cd 02_multiagent_integration
go run main.go
```

## Expected Output

```
🏢 Multi-Agent Integration Demo
===============================

1️⃣ Existing tRPC Agent
  📝 Native tRPC Agent: Mock response to: Hello from native!

2️⃣ New Eino Agent (migrated)  
  📝 Migrated Eino Agent: Eino chain processed: Hello from Eino!

3️⃣ Mixed Environment Demo
  🔄 Running both agents in same system...
  📱 Native Agent: Mock response to: Process this request
  📱 Eino Agent: Eino chain processed: Process this request
  ✅ Both agents work seamlessly together!

✅ Integration complete!
💡 This shows how to gradually migrate existing systems
```

## When to Use This Pattern

- ✅ 你有现有的tRPC多Agent系统
- ✅ 想要渐进式迁移，不是一次性重写
- ✅ 需要新老Agent并存运行
- ✅ 希望平滑升级现有架构

## Next Steps

- **03_tool_conversion**: 如果需要迁移Eino工具
- **04_basic_callbacks**: 如果需要添加监控回调
