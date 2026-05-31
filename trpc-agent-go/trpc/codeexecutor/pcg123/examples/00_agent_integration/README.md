# 🤖 Agent框架 + PCG123代码执行器集成示例

这个示例展示了如何将PCG123代码执行器与tRPC-Agent-Go的LLM Agent框架深度集成，创建一个具有强大代码执行能力的智能数据分析助手。

## 🎯 示例特点

### 核心功能
- **🧠 智能对话**: 基于LLM的自然语言理解
- **🐍 代码执行**: 集成PCG123 Python 3.10执行环境
- **📊 数据分析**: 自动生成和执行数据分析代码
- **🔄 流式输出**: 实时显示Agent思考和执行过程
- **📈 统计报告**: 自动生成详细的分析报告

### 技术架构
```
用户查询 → LLM Agent → 代码生成 → PCG123执行 → 结果分析 → 智能回复
```

## 🚀 快速开始

### 1. 环境准备

```bash
# 设置OpenAI配置（用于LLM模型）
export OPENAI_API_KEY="your-openai-api-key"
export OPENAI_BASE_URL="https://api.openai.com/v1"  # 或其他兼容的API端点

# 设置PCG123凭证（用于代码执行）
export PCG123_SECRET_ID="your-pcg123-secret-id"
export PCG123_SECRET_KEY="your-pcg123-secret-key"
```

### 2. 运行示例

```bash
# 基础运行
go run main.go

# 指定模型
go run main.go -model gpt-4

# 查看帮助
go run main.go -h
```

### 3. 预期输出

```
🚀 Agent框架 + PCG123代码执行器集成示例
==========================================
🔧 配置信息:
- 模型名称: deepseek-chat
- OpenAI SDK 将自动从环境变量读取 OPENAI_API_KEY 和 OPENAI_BASE_URL
- PCG123 将从环境变量读取 PCG123_SECRET_ID 和 PCG123_SECRET_KEY

🔧 创建PCG123代码执行器...
🤖 创建OpenAI模型实例...
🧠 创建具有代码执行能力的LLM Agent...
🏃 创建Agent运行器...

📝 用户查询: 请分析这组销售数据并生成报告：[120, 150, 180, 200, 175, 220, 195, 240, 210, 185]...

🔄 开始执行Agent任务...
📊 处理Agent事件流:
==================================================
🎯 Agent ID: pcg123_data_analyst
🔗 调用ID: xxx-xxx-xxx

我来帮您分析这组销售数据。让我编写Python代码来进行全面的数据分析...

[Agent开始执行代码并生成分析报告]
```

## 📋 代码架构解析

### 主要组件

#### 1. PCG123代码执行器配置
```go
pcgConf := pcg123.Config{
    Language:  pcg123.LanguagePython310,  // Python 3.10环境
    SecretID:  secretID,                  // PCG123应用ID
    SecretKey: secretKey,                 // PCG123密钥
}
```

#### 2. LLM Agent创建
```go
llmAgent := llmagent.New(
    agentName,
    llmagent.WithModel(modelInstance),           // OpenAI兼容模型
    llmagent.WithDescription("数据分析助手"),     // Agent描述
    llmagent.WithInstruction(systemInstruction), // 系统指令
    llmagent.WithGenerationConfig(genConfig),    // 生成配置
    llmagent.WithCodeExecutor(codeExecutor),     // 关键：绑定代码执行器
)
```

#### 3. 事件流处理
```go
for event := range eventChan {
    // 处理流式输出
    if choice.Delta.Content != "" {
        fmt.Print(choice.Delta.Content)  // 实时显示
    }
    
    // 处理完成状态
    if event.Done {
        break
    }
}
```

## 🔧 配置选项

### 命令行参数
| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-model` | `deepseek-chat` | 使用的LLM模型名称 |

### 环境变量
| 变量名 | 必需 | 说明 |
|--------|------|------|
| `OPENAI_API_KEY` | ✅ | OpenAI API密钥 |
| `OPENAI_BASE_URL` | ✅ | API端点URL |
| `PCG123_SECRET_ID` | ✅ | PCG123应用ID |
| `PCG123_SECRET_KEY` | ✅ | PCG123密钥 |

## 🎨 自定义Agent

### 修改系统指令
```go
func getSystemInstruction() string {
    return `你是一个专业的[领域]助手，具有强大的Python代码执行能力。
    
    ## 核心能力
    - [自定义能力1]
    - [自定义能力2]
    
    ## 工作流程
    1. [步骤1]
    2. [步骤2]
    ...`
}
```

### 调整生成配置
```go
genConfig := model.GenerationConfig{
    MaxTokens:   intPtr(4000),     // 增加最大Token数
    Temperature: floatPtr(0.3),    // 降低随机性
    Stream:      true,             // 启用流式输出
}
```

## 🔍 使用场景

### 1. **数据科学分析**
- 统计分析和报告生成
- 数据可视化（文本描述）
- 趋势预测和建模

### 2. **业务智能**
- 销售数据分析
- 财务报表处理
- KPI计算和监控

### 3. **教育培训**
- 编程教学助手
- 算法演示和解释
- 交互式学习环境

### 4. **研发支持**
- 原型验证
- 算法测试
- 数据处理脚本生成

## 🚨 注意事项

### 安全考虑
- ⚠️ **代码执行安全**: PCG123在隔离环境中执行代码，但仍需谨慎
- 🔐 **凭证管理**: 使用环境变量，避免硬编码敏感信息
- 🛡️ **输入验证**: 对用户输入进行适当的验证和清理

### 性能优化
- 🎯 **模型选择**: 根据任务复杂度选择合适的模型
- ⏱️ **超时设置**: 为长时间运行的代码设置合理超时
- 💾 **资源管理**: 及时释放执行器资源

### 错误处理
- 🔧 **连接重试**: 网络问题时的重试机制
- 📝 **日志记录**: 详细的错误日志和调试信息
- 🚫 **优雅降级**: 代码执行失败时的备选方案

## 🔗 相关资源

- **[PCG123代码执行器文档](../README.md)** - 完整的PCG123 API文档
- **[tRPC-Agent-Go框架](https://github.com/trpc-group/trpc-agent-go)** - Agent框架核心文档
- **[LLM Agent指南](../../../../../examples/llmagent/README.md)** - Agent开发指南
- **[更多示例](../)** - 其他PCG123使用示例

## 🤝 扩展开发

想要基于这个示例开发更复杂的应用？

1. **添加更多工具**: 集成文件操作、网络请求等工具
2. **多Agent协作**: 创建多个专业化的Agent
3. **持久化存储**: 添加会话状态和历史记录
4. **Web界面**: 开发Web UI进行交互
5. **API服务**: 将Agent封装为REST API服务

## 📞 获取帮助

遇到问题？
1. 检查环境变量配置
2. 确认网络连接正常
3. 查看控制台日志输出
4. 参考其他示例的实现方式

---

**💡 提示**: 这个示例展示了Agent框架的强大能力 - 通过自然语言与用户交互，并自动生成和执行代码来解决实际问题。这是构建智能应用的理想起点！
