# 🚀 快速开始: Eino 到 tRPC Agent

**30秒演示** 展示如何从 Eino 迁移到 tRPC Agent。

## 展示内容

- ✨ **一行迁移**: `teino.New(compiled, "name")`
- 🔄 **无缝集成**: 与 tRPC Runner 完美配合
- 🎯 **最小改动**: 你的 Eino 代码保持不变

## 核心代码

```go
// 你现有的 Eino 代码
compiled, _ := chain.Compile(ctx)

// 一行代码完成迁移！
agent := teino.New(compiled, "agent-name")

// 在 tRPC 生态系统中使用
runner := runner.NewRunner("app", agent)
```

## 运行方式

```bash
cd 00_quickstart
go run main.go
```

## 预期输出

```
🚀 Quickstart: Eino to tRPC Agent in 30 seconds
============================================

📝 Response:
🤖 Eino says: Hello! You asked about 'How does this work?'

✅ Done! Your Eino Chain is now a tRPC Agent!
💡 Next: See 01_agent_migration for more details
```

## 下一步

- **01_agent_migration**: 完整的迁移模式
- **03_tool_conversion**: 如果需要迁移工具
- **05_streaming_callbacks**: 如果需要复杂回调
