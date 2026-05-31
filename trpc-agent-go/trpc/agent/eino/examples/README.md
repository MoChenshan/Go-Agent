# 📚 Eino Integration Examples

欢迎使用tRPC-Agent-Go的Eino集成示例！这些示例展示了如何将现有的Eino代码迁移到tRPC生态系统。

## 🎯 学习路径 (建议按顺序学习)

### 1. 快速开始
- **[00_quickstart](./00_quickstart/)** - 30秒快速体验Eino到tRPC的迁移

### 2. 核心迁移
- **[01_agent_migration](./01_agent_migration/)** - 完整的Agent迁移指南 (Chain/Graph/ReAct)
- **[02_multiagent_integration](./02_multiagent_integration/)** - 在现有多Agent系统中的集成

### 3. 工具生态
- **[03_tool_conversion](./03_tool_conversion/)** - 将Eino工具转换为tRPC工具

### 4. 监控集成
- **[04_basic_callbacks](./04_basic_callbacks/)** - 基础回调转换 (OnStart/OnEnd/OnError)
- **[05_streaming_callbacks](./05_streaming_callbacks/)** - 高级流式回调 (OnEndWithStreamOutput)

### 5. 生产应用
- **[06_complete_scenario](./06_complete_scenario/)** - 生产级完整集成示例

## 🚀 根据你的需求选择

### 如果你想要...

#### 🔄 **快速迁移现有Agent**
→ 从 `00_quickstart` 开始，然后看 `01_agent_migration`

#### 🏢 **在现有系统中添加Eino Agent**  
→ 直接看 `02_multiagent_integration`

#### 🔧 **复用现有Eino工具**
→ 直接看 `03_tool_conversion`

#### 📊 **添加监控和日志**
→ 看 `04_basic_callbacks`

#### 🌊 **实现复杂流式处理 (如ChatBuffer)**
→ 看 `05_streaming_callbacks`

#### 🏭 **了解完整的生产迁移方案**
→ 直接看 `06_complete_scenario`

## 📋 功能对照表

| 示例 | Agent迁移 | 工具转换 | 基础回调 | 流式回调 | 难度 |
|------|-----------|----------|----------|----------|------|
| 00_quickstart | ✅ | ❌ | ❌ | ❌ | ⭐ |
| 01_agent_migration | ✅ | ❌ | ❌ | ❌ | ⭐⭐ |
| 02_multiagent_integration | ✅ | ❌ | ❌ | ❌ | ⭐⭐ |
| 03_tool_conversion | ❌ | ✅ | ❌ | ❌ | ⭐⭐ |
| 04_basic_callbacks | ❌ | ❌ | ✅ | ❌ | ⭐⭐⭐ |
| 05_streaming_callbacks | ❌ | ❌ | ❌ | ✅ | ⭐⭐⭐⭐ |
| 06_complete_scenario | ✅ | ✅ | ✅ | ✅ | ⭐⭐⭐⭐⭐ |

## 🛠️ 运行示例

每个示例目录都有独立的`README.md`和可运行的代码：

```bash
# 进入任意示例目录
cd 00_quickstart

# 运行示例
go run main.go

# 查看详细说明
cat README.md
```

## 📖 核心概念

### Agent迁移
- **Chain**: `teino.New(compiledChain, "name")`
- **Graph**: `teino.New(compiledGraph, "name")`  
- **ReAct**: `teino.NewReAct(reactAgent, "name")`

### 工具转换
- **基础转换**: `teino.NewTool(einoTool)`
- **带配置**: `teino.NewTool(einoTool, teino.WithTimeout(30))`

### 回调转换
- **工具回调**: `teino.NewToolCallbacks(einoHandler)`
- **模型回调**: `teino.NewModelCallbacks(einoHandler)`
- **流式回调**: `teino.NewStreamCallbackAgent(agent, teino.WithEinoCallbacks(handler))`

## 🔗 相关资源

- **[主包文档](../README.md)** - 完整的API文档和使用指南
- **[架构说明](../doc/)** - 详细的技术文档和设计说明

## 🤝 需要帮助？

如果遇到问题或有疑问：

1. 查看对应示例的`README.md`
2. 检查主包的文档
3. 确保Go版本 >= 1.19
4. 检查依赖是否正确安装
