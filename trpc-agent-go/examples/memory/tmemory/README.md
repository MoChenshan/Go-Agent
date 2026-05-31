# tMemory 记忆集成示例

本示例演示如何将 **[tMemory](https://test-tmemory.woa.com)** 记忆服务与 trpc-agent-go 框架集成，构建具备跨会话长期记忆能力的聊天机器人。

## 概述

tMemory 是腾讯内部的托管记忆服务，与本地记忆后端（sqlite、redis 等）的区别在于：

| 特性 | 本地记忆 (sqlite/redis) | tMemory |
|------|------------------------|---------|
| 记忆提取 | 客户端 LLM 提取 | **服务端异步提取** |
| 记忆类型 | 单一向量/关键词搜索 | **多类型**（raw/episodic/profile/graph） |
| 存储位置 | 本地/自建数据库 | 云端托管 |
| 接入方式 | `memory.Service` 接口 | HTTP API + Bearer Token |

### 工作流程

```
用户输入 → Agent 对话 → IngestSession（推送对话到 tMemory）
                                ↓
                        tMemory 服务端异步提取记忆
                                ↓
下次对话 → Agent 调用 memory_search 工具 → Recall API → 返回记忆
```

## 前置条件

1. **tMemory API Key**：向 tMemory 团队申请
2. **OpenAI API Key**：用于 LLM 模型访问

## 环境变量

```bash
# 必需
export TMEMORY_API_KEY="your-tmemory-api-key"    # tMemory Bearer Token
export OPENAI_API_KEY="your-openai-api-key"       # OpenAI API 密钥

# 可选
export TMEMORY_HOST="http://test-tmemory.woa.com" # tMemory 服务地址（默认测试环境）
export OPENAI_BASE_URL="https://your-proxy.com"   # OpenAI 代理 URL
```

## 运行

```bash
cd examples/memory/tmemory

# 使用默认设置（gpt-5.2, public）
go run .

# 指定模型和业务 ID
go run . -model deepseek-chat -biz-id my-app

# 关闭流式输出
go run . -streaming=false
```

### 命令行参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-model` | 聊天模型名称 | `gpt-5.2` |
| `-biz-id` | tMemory 业务 ID | `public` |
| `-strategy-id` | tMemory 策略 ID | `1` |
| `-streaming` | 是否启用流式输出 | `true` |

## 使用方式

### 交互命令

| 命令 | 说明 |
|------|------|
| 普通文本 | 正常对话，每轮结束后自动 ingest 到 tMemory |
| `/new` | 创建新会话（记忆跨会话保留） |
| `/exit` | 退出 |

> 想直接验证召回，向 Assistant 提问 "What do you remember about me?" / "Recall what I told you" 即可——LLM 会自动调用 `memory_search` 工具走召回链路。

### 示例会话

```
🧠 tMemory Chat Example
Model: gpt-5.2
BizID: public
StrategyID: 1
UserID: user-example
Session: tmemory-session-1776070290
==================================================

👤 You: My name is Alice and I am a Go developer at Tencent.
🤖 Assistant: Nice to meet you, Alice! ...

  （等待策略累计到足够 item，并等待异步提取完成）

👤 You: What do you know about me?
🤖 Assistant: 🔧 Memory tool calls:
   • memory_search (ID: call_xxx)
     Args: map[query:user profile Alice]
✅ Tool response: {"results":[{"memory":"Alice is a Go developer at Tencent",...}]}

Based on my memory, you are Alice, a Go developer working at Tencent!
```

> **注意**：tMemory 的提取是异步且由策略控制的。`/v1/data/add` 成功只代表数据已入队，不代表记忆已经可召回；是否达到触发阈值、多久完成提取，都取决于当前使用的 `strategy_id` 和服务端队列状态。

## 架构

### 核心组件

```go
// 1. 创建 tMemory 服务（自动从环境变量读取 API Key）
svc, _ := tmemory.NewService(
    tmemory.WithBizID("public"),
    tmemory.WithStrategyID("1"),
)

// 2. 注册 memory_search 工具到 Agent
llmAgent := llmagent.New("assistant",
    llmagent.WithModel(openai.New("gpt-5.2")),
    llmagent.WithTools(svc.Tools()),  // 只读工具：memory_search
)

// 3. 创建 Runner，通过 SessionIngestor 自动推送对话
r := runner.NewRunner("app", llmAgent,
    runner.WithSessionService(sessioninmemory.NewSessionService()),
    runner.WithSessionIngestor(svc),
)
```

### tMemory API

| 接口 | 方法 | 路径 | 说明 |
|------|------|------|------|
| Ingest | POST | `/v1/data/add` | 推送对话数据，服务端异步提取记忆 |
| Status | GET | `/v1/request/status` | 查询 add 请求的解析/处理状态 |
| Flush | POST | `/v1/data/flush` | 手动强制触发提取（调试/测试可用） |
| Recall | POST | `/v1/memories/recall` | 多维度记忆召回（vector/graph/profile） |

### Recall 配置

默认配置支持四种记忆类型的召回：

```go
// 默认 recall 配置
{
    "raw":      VectorRecallConfig{TopK: 3, Threshold: 0.5},
    "episodic": VectorRecallConfig{TopK: 3, Threshold: 0.5},
    "profile":  ProfileRecallConfig{},
    "graph":    GraphRecallConfig{TopK: 2, Depth: 2, Threshold: 0.7},
}
```

可通过 `tmemory.WithRecallConfig()` 自定义。

## 与其他 Memory 示例的区别

| 特性 | `simple/` | `auto/` | `tmemory/`（本示例） |
|------|-----------|---------|---------------------|
| 记忆后端 | 本地 (sqlite/redis/...) | 本地 (sqlite/redis/...) | **tMemory 云服务** |
| 提取方式 | Agent 主动调用工具 | 客户端 LLM 自动提取 | **服务端异步提取** |
| 工具类型 | 6 个（增删改查） | 搜索为主 | **仅 memory_search** |
| 接口实现 | `memory.Service` | `memory.Service` | `tmemory.Service`（独立） |
| Ingest | 自动（runner 内部） | 自动（runner 内部） | **自动调用 `IngestSession`，实际提取时机由策略决定** |

## 故障排除

### 常见问题

1. **`tmemory api key is required`**：确认已设置 `TMEMORY_API_KEY` 环境变量
2. **Recall 返回空结果**：
   - 首次 ingest 后需等待策略达到触发条件，并等待异步提取完成
   - 检查当前 `strategy-id` 的触发阈值是否足够低，或是否需要更多对话 item
   - 检查 `biz-id` 和 `userID` 是否与 ingest 时一致
3. **`tmemory api request failed: status=401`**：API Key 无效或过期
4. **`tmemory api request failed: status=429`**：请求频率过高，内置自动重试

### 调试建议

- 通过让 Assistant 回答记忆相关的问题来触发 `memory_search` 工具调用，观察实际召回内容
- 必要时直接结合 tMemory API 文档里的 `/v1/request/status` 或 `/v1/data/flush` 端点排查 add 请求状态
- 检查 ingest 是否有 `⚠️` 警告输出
- 确认 `TMEMORY_HOST` 指向正确的环境

## 参考文档

- [tMemory API 文档](https://test-tmemory.woa.com/docs)
- [tMemory 模块源码](../../../trpc/memory/tmemory/)
- [Memory Examples 总览](../README.md)
