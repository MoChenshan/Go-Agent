
# A2A 与 伽利略 可观测性集成示例

本示例演示如何在 tRPC-Agent-Go 框架下，结合 tRPC 插件配置将 ** 伽利略-可观测平台** 集成到 Agent-to-Agent (A2A) 通信中，实现多智能体工作流的链路追踪与指标采集。

## 概述

本 示例包含：
- **可观测性**：通过伽利略平台实现全链路追踪与指标采集
- **多智能体系统**：规划 → 研究 → 写作的多智能体链式协作
- **遥测集成**：追踪与指标无缝集成
- **生产级监控**：真实场景下的智能体可观测模式

## 🏗️ 架构图

```
┌─────────────┐    ┌─────────────────────────────────────┐
│   A2A       │    │           A2A Server                │
│   Client    │───▶│  ┌─────────────────────────────────┐│
│             │    │  │       Chain Agent               ││
└─────────────┘    │  │  ┌─────────┬─────────┬─────────┐││
                   │  │  │Planning │Research │Writing ││││
                   │  │  │ Agent   │ Agent   │ Agent  ││││
                   │  │  └─────────┴─────────┴─────────┘││
                   │  └─────────────────────────────────┘│
                   │           │                         │
                   │           ▼                         │
                   │    [DeepSeek API]                   │
                   └─────────────────┬───────────────────┘
                                     │
                   ┌─────────────────▼───────────────────┐
                   │         Galileo Platform            │
                   │  ┌─────────┬─────────┬─────────────┐│
                   │  │ Traces  │ Metrics │    Logs     ││
                   │  │         │         │     (todo)  ││
                   │  └─────────┴─────────┴─────────────┘│
                   └─────────────────────────────────────┘
```

## 主要特性

### 分布式链路追踪
- 实现智能体链路的端到端追踪
- 可视化智能体间的流转
- 性能瓶颈定位
- 跨服务调用关联

## 快速开始

### 前置条件
1. **DeepSeek API Key**：请前往 [DeepSeek API](https://api-docs.deepseek.com/) 获取
2. **伽利略平台权限**：确保有 伽利略 可观测平台访问权限
3. **Go**：1.23.0 及以上版本
4. **tRPC 配置**：已正确配置 `trpc_go.yaml`

### 配置说明
1. **环境变量**：
   ```bash
   export OPENAI_API_KEY="your-deepseek-api-key"
   export OPENAI_BASE_URL="https://api.deepseek.com/v1"
   ```
2. **伽利略配置**（`trpc_go.yaml`）：

### 运行示例
1. 启动带 伽利略 的 A2A 服务端：
   ```bash
   cd .server
   go run . -model "deepseek-chat"
   ```
2. 启动客户端：
   ```bash
   cd ./client
   go run . -message "write quantum computing summary" -timeout 60s
   ```

## tRPC 服务与 伽利略 集成

适用于基于 tRPC 框架的服务。

#### 1. 前置条件
请参考 [Galileo 官方文档 - GO (tRPC) 集成指南](https://iwiki.woa.com/p/4009274553) 完成基础配置。

#### 2. 集成代码
```go
// 导入 galileo 初始化，init 函数自动完成集成
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
```
#### 3. 配置文件
确保 `trpc_go.yaml` 包含 Galileo 插件配置：
```yaml
plugins:
   telemetry:
      galileo:
         # Galileo related configuration
         verbose: "error"
         # ... other configuration items
         config: # 本地配置
            opentelemetry_push:
               enable: true
               url: otlp.j.woa.com:80
```

### 自动埋点
框架会自动采集：
1. **Agent Span**：单个智能体执行链路
2. **Tool Span**：工具调用与执行链路
3. **Model Span**：大模型 API 调用链路

## 可观测性看板
服务启动后，可在伽利略 平台实时监控智能体运行状态。

### 链路追踪分析
典型链路结构：
```
A2A Request
├── Planning Agent Execution
│   ├── Model API Call (DeepSeek)
│   └── Response Processing
├── Research Agent Execution  
│   ├── Tool: web_search
│   ├── Tool: knowledge_base
│   └── Model API Call (DeepSeek)
└── Writing Agent Execution
    ├── Model API Call (DeepSeek)
    └── Final Response Generation
```


## 🧪 集成验证

集成完成后，您可以通过以下方法进行验证：

1. **检查日志**: 启动时应该看到成功的 Galileo 初始化日志
2. **追踪数据**: 检查 Galileo 平台上是否有追踪数据上报
3. **错误监控**: 确认没有相关的错误日志

## 📚 参考文档

- [Galileo GO (tRPC) 集成指南](https://iwiki.woa.com/p/4009274553)
- [Galileo GO (通用) 集成指南](https://iwiki.woa.com/p/4013979751)
- [Galileo SDK Tracer 实现文档](https://iwiki.woa.com/p/4012224483)
