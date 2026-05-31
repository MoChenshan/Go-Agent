# 🔧 Tool Conversion

**展示内容**: 如何将Eino工具转换为tRPC工具，在tRPC原生Agent中使用。

## What This Shows

- 🔄 **工具复用**: 你的Eino工具可以在tRPC Agent中使用
- ⚙️ **配置选项**: 转换时可以自定义名称、描述、超时等
- 🏗️ **无缝集成**: 转换后的工具完全兼容tRPC生态

## Key API

```go
// 基础转换 (一行代码!)
trpcTool := teino.NewTool(einoTool)

// 带配置的转换
trpcTool := teino.NewTool(einoTool,
    teino.WithName("custom_name"),
    teino.WithDescription("Custom description"),  
    teino.WithTimeout(30), // 30秒超时
)
```

## Run It

```bash
cd 03_tool_conversion
go run main.go
```

## Expected Output

```
🔧 Tool Conversion Demo
=======================

1️⃣ Basic Eino Tool Conversion
------------------------------
  🧮 Testing converted calculator:
    ✅ eino_calculator: map[inputs:map[a:10 b:5] operation:add result:15]
  🌤️ Testing converted weather tool:
    ✅ eino_weather: Weather in Beijing: Sunny, 25°C (from Eino tool)

2️⃣ Tool Conversion with Options
-------------------------------
  🎛️ Testing tool with custom config:
    ✅ advanced_calculator: map[inputs:map[a:7 b:6] operation:multiply result:42]

3️⃣ Using Converted Tools in tRPC Agent
--------------------------------------
  🤖 Agent response: Mock response to: Can you help me calculate 15 + 25?
  ✅ Your Eino tools are now fully integrated!

✅ Tool conversion complete!
💡 Your Eino tools now work with tRPC native agents
```

## When to Use This

- ✅ 你有现有的Eino工具生态
- ✅ 想在tRPC原生Agent中复用这些工具
- ✅ 需要逐步迁移工具而不是Agent
- ✅ 希望保持工具的原有逻辑不变

## Supported Tool Types

- **BaseTool**: 只读工具，支持基本信息查询
- **InvokableTool**: 可调用工具，支持执行操作
- **StreamableTool**: 流式工具，支持流式输出

## Next Steps

- **04_basic_callbacks**: 如果需要添加监控回调
- **02_multiagent_integration**: 如果需要在多Agent系统中使用
