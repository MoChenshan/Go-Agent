### Venus Reranker

Venus 提供多种模型服务，其中也包括 reranker 模型，更多信息请参考 [Venus 文档](https://iwiki.woa.com/p/4010378552)。

服务入口请以 Venus 实际部署为准。可以通过 tRPC 服务名配置 API 子路径，也可以直接配置完整 URL，例如 `http://v2.open.venus.oa.com/llmproxy/v1/rerank`。

#### 方式一：通过 trpc_go.yaml 配置服务名

通过 `WithServiceName` 指定 trpc_go.yaml 中配置的 client service，`WithEndpoint` 仅需配置 API 子路径。

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/reranker/venus"

rerank, err := venus.New(
    venus.WithServiceName("trpc.venus.Reranker"), // trpc_go.yaml 中配置的服务名
    venus.WithEndpoint("/v1/rerank"),             // API 子路径
    venus.WithModel("default"),
    venus.WithTopN(5),
)
```

trpc_go.yaml 配置：

```yaml
client:
  service:
    - name: trpc.venus.Reranker
      target: dns://ai.woa.com:443
      protocol: http
      network: tcp
```

#### 方式二：通过 tRPC Client Option 配置

通过 `WithTrpcClientOption` 直接传入 client option，无需依赖 trpc_go.yaml 配置文件。

```go
import (
    "git.code.oa.com/trpc-go/trpc-go/client"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/reranker/venus"
)

rerank, err := venus.New(
    venus.WithEndpoint("/v1/rerank"),
    venus.WithModel("default"),
    venus.WithTrpcClientOption(
        client.WithTarget("dns://ai.woa.com:443"),
        client.WithTimeout(30000),
    ),
)
```

#### 方式三：直接配置完整 URL（不依赖 tRPC）

不使用 tRPC 寻址能力，直接通过 `WithEndpoint` 配置完整的服务 URL。

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/reranker/venus"

rerank, err := venus.New(
    venus.WithEndpoint("http://v2.open.venus.oa.com/llmproxy/v1/rerank"), // 完整 URL
    venus.WithModel("default"),
    venus.WithAPIKey("your-api-key"),
    venus.WithTopN(5),
)
```

#### 配置项说明

| 配置项 | 说明 | 必填 |
|--------|------|------|
| `WithServiceName(string)` | trpc_go.yaml 中配置的服务名 | 方式一必填 |
| `WithEndpoint(string)` | API 端点（子路径或完整 URL） | 是 |
| `WithModel(string)` | 模型名称 | 否 |
| `WithTopN(int)` | 返回结果数量，默认 `-1`（返回全部） | 否 |
| `WithAPIKey(string)` | API Key | 否 |
| `WithTrpcClientOption(...client.Option)` | tRPC client 配置选项 | 方式二使用 |

详细示例请参考 [examples/knowledge/trpc/venusreranker/](https://git.woa.com/trpc-go/trpc-agent-go/tree/main/examples/knowledge/trpc/venusreranker) 目录。
