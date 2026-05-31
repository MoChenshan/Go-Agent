# 🌟 真实大模型 MultiAgent 系统集成演示

展示如何将 Eino 适配器与真实大语言模型集成，构建生产级的 MultiAgent 协作系统。

## 🎯 演示内容

### 完整的 MultiAgent 工作流

```
📋 任务规划器 → 🔗 内容处理器 → 🕸️ 决策路由器 → 📊 数据分析器 → ✍️ 报告生成器
   (tRPC原生)     (Eino Chain)    (Eino Graph)     (Eino Workflow)   (tRPC原生)
```

### 混合架构优势

- **Eino Agent**: 利用现有的 Eino 逻辑和工具
- **tRPC Agent**: 享受原生性能和生态
- **真实 LLM**: OpenAI/DeepSeek 等真实模型驱动
- **流式处理**: 完整的流式输出和回调支持

## 🚀 快速开始

### 1. 环境配置

```bash
# OpenAI 配置
export OPENAI_API_KEY="your-api-key"
export OPENAI_MODEL_NAME="gpt-4"
export OPENAI_BASE_URL="https://api.openai.com/v1"

# 或者 DeepSeek 配置
export OPENAI_API_KEY="your-deepseek-key"
export OPENAI_MODEL_NAME="deepseek-chat"
export OPENAI_BASE_URL="https://api.deepseek.com/v1"
```

### 2. 运行演示

```bash
# 构建和运行
go run main.go
```

## 💡 适合场景

### 学习场景
- **框架能力验证**: 验证 Eino → tRPC 集成的真实效果
- **MultiAgent 架构**: 理解多智能体协作模式
- **混合系统设计**: 学习如何组合不同框架的优势

### 生产场景  
- **复杂业务流程**: 需要多步骤协作的业务场景
- **智能决策系统**: 结合规则和AI的决策流程
- **内容处理管道**: 从分析到生成的完整链路

## 🔧 技术特点

### 真实 LLM 集成
- 支持 OpenAI、DeepSeek 等主流模型
- 完整的 API 调用和错误处理
- 流式输出和实时反馈

### 生产级配置
- 环境变量配置管理
- 错误处理和重试机制
- 交互式用户体验

### 完整工作流
- **任务规划**: 分析用户需求，制定处理策略
- **内容处理**: 提取关键信息，格式化数据
- **决策路由**: 智能选择处理路径和策略
- **数据分析**: 深度分析和洞察生成
- **报告生成**: 专业的结构化输出

## 🎪 示例对话

```
输入: 策划一次团队建设活动

输出:
📋 [任务规划器]: 分析团队建设需求...
🔗 [内容处理器]: 提取关键信息: 活动类型、目标、参与者...
🕸️ [决策路由器]: 评估复杂度，选择处理策略...
📊 [数据分析器]: 预算区间 2000-5000元，成功概率 85%...
✍️ [报告生成器]: # 团队建设活动策划方案
## 方案一：户外拓展训练
## 方案二：室内团建工坊
...
```

## 🚨 注意事项

- **API 费用**: 使用真实 LLM 会产生 API 调用费用
- **网络连接**: 需要稳定的网络访问大模型服务
- **配置管理**: 请妥善保管您的 API Key

## 🔗 相关示例

- [`02_multiagent_integration`](../02_multiagent_integration/) - 基础 MultiAgent 演示
- [`06_complete_scenario`](../06_complete_scenario/) - 完整配置示例
- [`05_streaming_callbacks`](../05_streaming_callbacks/) - 流式回调处理

