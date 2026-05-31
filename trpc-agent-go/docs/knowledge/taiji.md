# 太极知识库接入

[太极](https://taiji.woa.com/#/) 平台提供模型训练和 RAG 服务。trpc-agent-go 支持快速接入太极知识库服务。

> 完整示例代码：[examples/knowledge/trpc/taiji](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/knowledge/trpc/taiji)

## 快速开始

```go
package main

import (
    "context"
    "log"
    "time"

    "git.code.oa.com/trpc-go/trpc-go"
    knowledge "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/sdk"
    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/knowledge/source"
    filesource "trpc.group/trpc-go/trpc-agent-go/knowledge/source/file"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/runner"
    "trpc.group/trpc-go/trpc-agent-go/session/inmemory"

    _ "git.code.oa.com/trpc-go/trpc-naming-polaris" // Enable Polaris service discovery
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func main() {
    ctx := context.Background()
    
    // Initialize tRPC server to load trpc_go.yaml config
    trpc.NewServer()

    // 1. 创建太极选项配置
    taijiOption := sdk.NewTaijiOption(
        sdk.WithEmbIndex("your-embedding-index"),
        sdk.WithToken("your-taiji-token"),
        sdk.WithWSID("your-workspace-id"),
        sdk.WithTaijiHYAPIToken("your-hunyuan-api-token"),
        sdk.WithServiceName("trpc.test.knowledge.taiji"),
    )

    // 2. 创建数据源
    sources := []source.Source{
        filesource.New(
            []string{"./docs/api.md"},
            filesource.WithName("API Documentation"),
        ),
    }

    // 3. 创建太极知识库
    kb, err := knowledge.New(
        knowledge.WithTaijiOption(taijiOption),
        knowledge.WithSources(sources),
    )
    if err != nil {
        log.Fatalf("Failed to create Taiji knowledge: %v", err)
    }

    // 4. 加载文档（可选，如果已加载过可跳过）
    if _, err := kb.Load(ctx,
        knowledge.WithTaijiRateLimit(300*time.Millisecond, 5),
    ); err != nil {
        log.Fatalf("Failed to load knowledge base: %v", err)
    }

    // 5. 创建 Agent 并运行
    llmAgent := llmagent.New(
        "taiji-assistant",
        llmagent.WithModel(openai.New("deepseek-chat")),
        llmagent.WithKnowledge(kb),
    )

    sessionService := inmemory.NewSessionService()
    appRunner := runner.NewRunner("taiji-chat", llmAgent, runner.WithSessionService(sessionService))
    _ = appRunner // Execute conversation...
}
```

上述代码使用了 `WithServiceName("trpc.test.knowledge.taiji")`，需要配置 `trpc_go.yaml`：

```yaml
global:
  namespace: Development
  env_name: test

client:
  service:
    - name: trpc.test.knowledge.taiji
      # devcloud 环境太极服务地址
      target: ip://stream-server-online-openapi.turbotke.production.polaris:1081
      protocol: http_no_protocol
      timeout: 0

plugins:
  registry:
    polaris:
      heartbeat_interval: 3000
      protocol: grpc
  selector:
    polaris:
      namespace: Development
```

## 配置选项

### TaijiOption 配置

通过 `sdk.NewTaijiOption()` 创建配置：

| 选项 | 说明 | 获取方式 | 必填 |
|------|------|----------|------|
| `WithEmbIndex(id)` | 索引服务 ID（emb_index，数字 ID） | 参考 [太极API文档](https://iwiki.woa.com/p/4008515885) | 是 |
| `WithToken(token)` | 太极认证令牌（authorization） | 参考 [太极API文档](https://iwiki.woa.com/p/4008515885)，当前配置太极默认值即可 | 是 |
| `WithWSID(wsid)` | 工作空间 ID（wsid） | 参考 [太极API文档](https://iwiki.woa.com/p/4008515885) | 是 |
| `WithURL(url)` | 太极服务地址（优先级高于 ServiceName） | devcloud 环境：`http://stream-server-online-openapi.turbotke.production.polaris:1081` | 与 ServiceName 二选一 |
| `WithServiceName(name)` | tRPC 服务名称，通过 trpc_go.yaml 配置 | 见下方连接配置 | 与 URL 二选一 |
| `WithTRPCClientOptions(opts...)` | tRPC 客户端选项，用于配置超时、重试等参数 | 如 `client.WithTimeout(time.Minute*10)` | 可选 |
| `WithTaijiHYAPIToken(token)` | 混元平台 token，用于 Load 操作 | 混元平台右上角复制，参考 [太极索引服务API文档](https://iwiki.woa.com/p/4010689738) | Load 时必填 |
| `WithTaijiHYAPIURL(url)` | 混元 API 地址 | 默认 `http://hunyuanaide.taiji.woa.com`，参考 [太极索引服务API文档](https://iwiki.woa.com/p/4010689738) | 可选 |
| `WithClientBuilder(builder)` | 自定义 HTTP 客户端构建器 | - | 可选 |

### Knowledge 配置

| 选项 | 说明 |
|------|------|
| `WithTaijiOption(opt)` | 设置太极配置 |
| `WithSources(sources)` | 设置数据源 |
| `WithEmbedder(embedder)` | 设置自定义 Embedder |
| `WithRetriever(retriever)` | 设置自定义 Retriever |
| `WithReranker(reranker)` | 设置自定义 Reranker |
| `WithQueryEnhancer(enhancer)` | 设置查询增强器 |

### Load 配置

| 选项 | 说明 |
|------|------|
| `WithTaijiRateLimit(interval, burst)` | 设置限流，如 `300ms, 5` 表示约 3 QPS |
| `WithSrcParallelism(n)` | 源级并发数，默认 1 |
| `WithDocParallelism(n)` | 文档级并发数，默认 1 |

## 连接配置

太极服务支持两种连接方式：通过 `trpc_go.yaml` 配置或直接传入 URL。

### 方式一：通过 trpc_go.yaml 配置（推荐）

使用 `WithServiceName` 指定服务名称，通过 `trpc_go.yaml` 管理连接参数：

```go
// 需要先初始化 tRPC 以加载 trpc_go.yaml 配置
trpc.NewServer()

taijiOption := sdk.NewTaijiOption(
    sdk.WithEmbIndex("your-embedding-index"),
    sdk.WithToken("your-taiji-token"),
    sdk.WithWSID("your-workspace-id"),
    sdk.WithServiceName("trpc.test.knowledge.taiji"), // 对应 trpc_go.yaml 中的服务名
)
```

对应的 `trpc_go.yaml` 配置：

```yaml
global:
  namespace: Development
  env_name: test

client:
  service:
    - name: trpc.test.knowledge.taiji
      # devcloud 环境太极服务地址，也可以替换为太极提供的北极星服务
      target: ip://stream-server-online-openapi.turbotke.production.polaris:1081
      protocol: http_no_protocol
      timeout: 0  # 0 表示无超时限制，可根据实际需求设置

plugins:
  registry:
    polaris:
      heartbeat_interval: 3000
      protocol: grpc
  selector:
    polaris:
      namespace: Development
```

### 方式二：通过 URL 直接配置

使用 `WithURL` 直接传入太极服务地址，无需 `trpc_go.yaml` 配置：

```go
taijiOption := sdk.NewTaijiOption(
    sdk.WithEmbIndex("your-embedding-index"),
    sdk.WithToken("your-taiji-token"),
    sdk.WithWSID("your-workspace-id"),
    // devcloud 环境太极服务地址
    sdk.WithURL("http://stream-server-online-openapi.turbotke.production.polaris:1081"),
)
```

这种方式适用于：
- 快速测试和调试
- 连接参数需要动态获取的场景
- 不希望依赖配置文件的场景

> 注意：`WithURL` 优先级高于 `WithServiceName`，如果两者都设置，将使用 `WithURL` 的值。

## 架构组件

| 组件 | 说明 |
|------|------|
| Taiji SDK Client | 封装太极服务连接配置 |
| Taiji Embedder | 处理文本向量化（可选） |
| Taiji Retriever | 执行语义搜索和重排序 |
| Taiji Knowledge | 核心知识库管理组件 |


## 参考文档

- [太极API文档](https://iwiki.woa.com/p/4008515885)
- [太极索引服务API文档](https://iwiki.woa.com/p/4010689738)
