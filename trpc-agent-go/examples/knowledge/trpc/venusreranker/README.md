# Venus Reranker 示例

本示例演示如何在 tRPC 框架中使用 **Venus Reranker** 对检索结果进行重排序。

## 功能概述

Venus Reranker 是一个基于 Cross-Encoder 的重排序服务，可以对初始检索结果进行精细化排序，提高搜索相关性。相比 Embedding 相似度（Bi-Encoder），Cross-Encoder 能够更准确地理解 query 和 document 之间的语义关系。

## 前置条件

1. **Venus 服务**: 需要部署或访问 Venus Reranker 服务
2. **tRPC 框架**: 本示例基于 tRPC-Go 框架

## 环境变量

运行示例前，请根据需要设置以下环境变量：

```bash
# Venus 服务配置
export VENUS_URL="/v1/rerank"        # Venus 服务端点路径或完整 URL
export VENUS_API_KEY="your-api-key"  # (可选) Venus API Key
```

## 使用方法

### 基本用法

```bash
cd examples/knowledge/trpc/venusreranker
go run . -endpoint="/v1/rerank"
```

### 命令行参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-endpoint` | Venus 服务端点路径或完整 URL | `VENUS_URL` 环境变量或 `/v1/rerank` |
| `-model` | Reranker 模型名称 | `default` |
| `-api-key` | Venus API Key | `VENUS_API_KEY` 环境变量 |
| `-topn` | 返回 Top N 结果（0 表示全部返回） | `0` |

### 示例运行

```bash
# 方式1：使用 trpc_go.yaml 中的服务名配置，endpoint 使用 API 子路径
go run . -endpoint="/v1/rerank" -model="default"

# 方式2：直接配置完整 URL
export VENUS_URL="http://v2.open.venus.oa.com/llmproxy/v1/rerank"
export VENUS_API_KEY="your-api-key"
go run . -model="server:your-model-id"

# 限制返回结果数量
go run . -endpoint="/v1/rerank" -topn=3
```

## 代码集成

### 创建 Venus Reranker

```go
import (
    "git.code.oa.com/trpc-go/trpc-go/client"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/reranker/venus"
)

// 方式1：使用 WithTrpcClientOption 直接配置（推荐，不依赖 trpc_go.yaml）
reranker, err := venus.New(
    venus.WithEndpoint("/v1/rerank"),
    venus.WithTrpcClientOption(
        client.WithTarget("dns://venus-service:8000"),
        client.WithTimeout(30000),
    ),
    venus.WithModel("default"),
    venus.WithTopN(5),
)

// 方式2：使用 tRPC 服务发现（依赖 trpc_go.yaml）
reranker, err := venus.New(
    venus.WithEndpoint("/v1/rerank"),
    venus.WithServiceName("trpc.test.venus.reranker"),
    venus.WithModel("default"),
    venus.WithAPIKey("your-api-key"),  // 可选
)
```

### 与 Knowledge 集成

```go
import (
    "git.code.oa.com/trpc-go/trpc-go/client"
    "trpc.group/trpc-go/trpc-agent-go/knowledge"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/reranker/venus"
)

// 创建 Venus reranker
reranker, err := venus.New(
    venus.WithEndpoint("/v1/rerank"),
    venus.WithTrpcClientOption(
        client.WithTarget("dns://venus-service:8000"),
    ),
    venus.WithModel("default"),
)
if err != nil {
    log.Fatal(err)
}

// 创建知识库并配置 reranker
kb := knowledge.New(
    knowledge.WithReranker(reranker),
    // ... 其他配置
)
```

## 配置方式对比

### WithTrpcClientOption（推荐）

直接在代码中配置 tRPC client 参数，**不依赖 trpc_go.yaml** 配置文件：

```go
reranker, err := venus.New(
    venus.WithEndpoint("/v1/rerank"),
    venus.WithTrpcClientOption(
        client.WithTarget("dns://venus-service:8000"),  // 服务地址
        client.WithTimeout(30000),                       // 超时时间(ms)
    ),
    venus.WithModel("default"),
)
```

**优点**：
- 配置集中在代码中，便于管理
- 不依赖外部配置文件
- 适合动态配置场景

### WithServiceName

通过服务名从 `trpc_go.yaml` 中查找配置：

```go
reranker, err := venus.New(
    venus.WithEndpoint("/v1/rerank"),
    venus.WithServiceName("trpc.test.venus.reranker"),
    venus.WithModel("default"),
)
```

需要在 `trpc_go.yaml` 中配置：

```yaml
global:
  namespace: Development
  env_name: test

client:
  timeout: 30000
  service:
    - name: trpc.test.venus.reranker
      target: dns://venus-reranker.example.com
      protocol: http
      timeout: 60000
```

**配置说明**：
- `name`：服务名称，需与代码中 `WithServiceName()` 的参数一致
- `protocol`：固定为 `http`
- `target`：Venus 服务地址，支持以下格式：
  - `polaris://服务名`：使用 Polaris 服务发现（推荐）
  - `dns://域名:端口`：DNS 解析
  - `ip://host:port`：直连地址
- `timeout`：HTTP 请求超时时间（毫秒）

## API 格式

### 请求格式

```json
{
    "model": "default",
    "query": "what is a panda?",
    "documents": [
        "Justice Juan M. Merchan will hear arguments...",
        "The giant panda (Ailuropoda melanoleuca)...",
        "Paris is in France."
    ]
}
```

### 响应格式

```json
{
    "object": "rerank",
    "results": [
        {
            "relevance_score": 0.89599609375,
            "index": 1
        },
        {
            "relevance_score": 0.00007665157318115234,
            "index": 0
        },
        {
            "relevance_score": 0.00007665157318115234,
            "index": 2
        }
    ],
    "model": "default",
    "usage": {
        "prompt_tokens": 303,
        "total_tokens": 303
    },
    "id": "venus-041e13df-6652-4fa1-8af8-666f7f193680",
    "created": 1713940083
}
```

## 示例输出

```
Venus Reranker Demo
==================================================
Endpoint: /v1/rerank
Service Name: trpc.test.venus.reranker
Model: default

======================================================================
Case: Panda Question
Query: what is a panda?
======================================================================

--- Original Order ---
1. Justice Juan M. Merchan will hear arguments over whether the former president violated his gag order.
2. The giant panda (Ailuropoda melanoleuca), sometimes called a panda bear or simply panda.
3. Paris is in France.

--- Reranked Results (by relevance score) ---
1. [Score: 0.8959961] The giant panda (Ailuropoda melanoleuca), sometimes called a panda bear or simply panda.
2. [Score: 0.0000767] Justice Juan M. Merchan will hear arguments over whether the former president violated his gag order.
3. [Score: 0.0000767] Paris is in France.
```

## 故障排除

**常见问题**:

1. **连接失败**: 检查 `-endpoint` 或 trpc_go.yaml 中的服务地址是否正确
2. **认证失败**: 如果 Venus 服务需要认证，请设置正确的 `VENUS_API_KEY`
3. **超时错误**: 使用 `WithTrpcClientOption(client.WithTimeout(60000))` 增加超时时间
4. **服务发现失败**: 检查 `trpc_go.yaml` 中的服务配置是否正确

## 参考文档

- [tRPC-Agent-Go 框架文档](../../../../README.md)
- [Knowledge Reranker 文档](../../../../docs/knowledge/reranker.md)
