
# A2A 与 智研-监控宝 可观测性集成示例

本示例演示如何在 tRPC-Agent-Go 框架下，结合 tRPC 插件配置将 ** 智研-可观测平台** 集成到 Agent-to-Agent (A2A) 通信中，实现多智能体工作流的链路追踪与指标采集。

## 概述

本 示例包含：
- **可观测性**：通过智研-监控宝平台实现全链路追踪与指标采集
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
2. **智研-监控宝平台权限**：确保有 智研-监控宝  可观测平台访问权限
3. **Go**：1.23.0 及以上版本
4. **tRPC 配置**：已正确配置 `trpc_go.yaml`

### 配置说明
1. **环境变量**：
   ```bash
   export OPENAI_API_KEY="your-deepseek-api-key"
   export OPENAI_BASE_URL="https://api.deepseek.com/v1"
   ```
2. **智研-监控宝配置**（`trpc_go.yaml`）：

### 运行示例
1. 启动带智研-监控宝的 A2A 服务端：
   ```bash
   cd .server
   go run . -model "deepseek-chat"
   ```
2. 启动客户端：
   ```bash
   cd ./client
   go run . -message "write quantum computing summary" -timeout 60s
   ```

## tRPC 服务与智研-监控宝集成

适用于基于 tRPC 框架的服务。

#### 1. 前置条件
请参考 [智研-应用性能监控上报-Opentelemetry协议上报-Go](https://iwiki.woa.com/p/4006985186) 完成基础配置。

#### 2. 集成代码
```go
// 导入 zhiyan 初始化，init 函数自动完成集成
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan"
```
#### 3. 配置文件
确保 `trpc_go.yaml` 包含 telemetry-opentelemetry 插件配置，该插件的配置说明见 https://git.woa.com/opentelemetry/opentelemetry-go-ecosystem ：
```yaml
plugins:
  telemetry: # 注意缩进层级关系
    opentelemetry:
```

### 自动埋点
框架会自动采集：
1. **Agent Span**：单个智能体执行链路
2. **Tool Span**：工具调用与执行链路
3. **Model Span**：大模型 API 调用链路

## 可观测性看板
服务启动后，可在智研-监控宝 平台实时监控智能体运行状态。

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

## 查看追踪数据

在监控宝-应用性能监控-链路查询上查看：

![zhiyan-trace-overall](../../../../docs/img/telemetry/zhiyan-plugin-trace-overall.png)

具体的 trace 展示类似下面的效果：
![zhiyan-trace-datil](../../../../docs/img/telemetry/zhiyan-plugin-trace-detail.png)


## 参考资料

- [智研监控宝-OpenTelemetry 概述](https://iwiki.woa.com/p/4006985193)
- [智研监控宝-自定义监控上报对接opentelemetry使用手册](https://iwiki.woa.com/p/4007004639)
- [智研监控宝-使用OpenTelemetry 上报日志到日志汇](https://iwiki.woa.com/p/4006985192)
- [智研 AMP 上报例子](http://git.woa.com/zhiyanapm/apm-go-example)
- [telemetry-opentelemetry 插件](git.woa.com/opentelemetry/opentelemetry-go-ecosystem)