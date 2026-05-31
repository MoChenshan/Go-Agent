# iWiki 知识库接入

[iWiki](https://iwiki.woa.com) 是公司内部的知识管理平台，提供 RAG OpenAPI 用于语义检索。trpc-agent-go 支持快速接入 iWiki 知识库服务。

> 完整示例代码：[examples/knowledge/trpc/iwiki](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/knowledge/trpc/iwiki)

## 前置条件

- 在[太湖平台](https://tai.it.woa.com)上创建应用，获取 PaasID 和 Token。参考[太湖接入文档](https://iwiki.woa.com/p/36307200)。
- 在太湖平台上为应用订阅对应环境的 iWiki recall 接口，否则会返回 `AGW.1403` 错误。
- 将太湖 PaasID 绑定到 iWiki，参考[绑定文档](https://iwiki.woa.com/p/4007030209)。
- 确保应用对目标 space 有访问权限，如需申请权限请访问：https://iwiki.woa.com/public-app/apply?source=iwiki

## 快速开始

```go
package main

import (
    "context"
    "log"

    "git.code.oa.com/trpc-go/trpc-go"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/iwiki"
    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/knowledge"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/runner"
    "trpc.group/trpc-go/trpc-agent-go/session/inmemory"

    _ "git.code.oa.com/trpc-go/trpc-naming-polaris"
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func main() {
    ctx := context.Background()

    // Initialize tRPC server to load trpc_go.yaml config
    trpc.NewServer()

    // 1. Create iWiki knowledge base
    kb := iwiki.New(
        iwiki.WithPaasID("your-paas-id"),
        iwiki.WithToken("your-token"),
        iwiki.WithURL("http://api-idc.sgw.woa.com/ebus/iwiki/prod"),
        iwiki.WithSearchConf(&iwiki.SearchConf{
            SpaceIDs: []int{12345},
        }),
    )

    // 2. Search
    result, err := kb.Search(ctx, &knowledge.SearchRequest{
        Query:      "your query",
        MaxResults: 5,
    })
    if err != nil {
        log.Fatalf("Search failed: %v", err)
    }

    // 3. Use with Agent
    llmAgent := llmagent.New(
        "iwiki-assistant",
        llmagent.WithModel(openai.New("deepseek-chat")),
        llmagent.WithKnowledge(kb),
    )

    sessionService := inmemory.NewSessionService()
    appRunner := runner.NewRunner("iwiki-chat", llmAgent, runner.WithSessionService(sessionService))
    _ = appRunner // Execute conversation...
    _ = result
}
```

## API 环境

iWiki 提供三种环境的 API 地址：

| 环境 | URL |
|------|-----|
| 正式环境 (prod) | `http://api-idc.sgw.woa.com/ebus/iwiki/prod` |
| 开发环境 (dev) | `http://api-idc.sgw.woa.com/ebus/iwiki/dev` |
| 预发布环境 (pre) | `http://api-idc.sgw.woa.com/ebus/iwiki/pre` |

> **注意**：使用前需要在[太湖平台](https://tai.it.woa.com)确认你的应用已订阅对应环境的 iWiki 接口，否则会返回 `AGW.1403` 错误。

### 不同区域调用地址

| 区域 | 调用地址格式 |
|------|-------------|
| IDC / DevCloud | `http://api-idc.sgw.woa.com/{太湖应用的访问路径}/{API接口path}` |
| 桌面和OA区域 | `http://api.sgw.woa.com/{太湖应用的访问路径}/{API接口path}` |

在 `trpc_go.yaml` 中配置 target 时，根据所在区域选择对应的域名：
- IDC/DevCloud 环境使用 `dns://api-idc.sgw.woa.com`
- 桌面/OA 环境使用 `dns://api.sgw.woa.com`

## 配置选项

### Knowledge 配置

通过 `iwiki.New()` 创建知识库实例：

| 选项 | 说明 | 必填 |
|------|------|------|
| `WithPaasID(id)` | 太湖平台注册的 PaasID | 是 |
| `WithToken(token)` | 太湖平台应用 Token，用于 Rio 签名计算 | 是 |
| `WithURL(url)` | iWiki API 地址，包含环境路径 | 是 |
| `WithServiceName(name)` | tRPC 服务名称，设置后底层 HTTP 客户端会通过 `trpc_go.yaml` 中对应 client service 的配置来管理连接（超时、协议等），从而接入 trpc-go 生态。与 `WithURL` 不冲突，`WithURL` 控制请求目标地址，`WithServiceName` 控制 trpc client 连接配置 | 可选 |
| `WithSearchConf(conf)` | 默认搜索配置（space_ids、doc_objs 等） | 可选 |
| `WithAdvancedParams(params)` | 高级搜索参数 | 可选 |
| `WithHTTPHeaders(headers)` | 自定义 HTTP 头，如 x-tai-identity 透传 | 可选 |
| `WithTRPCClientOptions(opts...)` | tRPC 客户端选项，用于在代码中动态设置超时、重试等参数，接入 trpc-go 生态。与 `WithServiceName` 类似，但通过代码而非配置文件控制 | 可选 |

### SearchConf 配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `SpaceIDs` | `[]int` | 限定搜索的 space ID 列表，查询方式参考 [QA文档 Q5](https://iwiki.woa.com/p/1855328733) |
| `DocObjs` | `[]DocObj` | 限定搜索的文档对象列表 |
| `Topics` | `[]Topic` | 限定搜索的 topic 列表 |
| `InternetEnabled` | `bool` | 是否启用互联网搜索 |

### AdvancedParams 配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `SkipPlanner` | `bool` | 跳过 planner |
| `SkipRerank` | `bool` | 跳过重排序 |
| `SkipInv` | `bool` | 跳过倒排检索 |
| `NotMerge` | `bool` | 不合并结果 |

## 接入 trpc-go 生态

通过 `WithServiceName` 可以将底层 HTTP 客户端纳入 trpc-go 的管理体系，使用 `trpc_go.yaml` 统一配置超时、协议等参数：

```go
trpc.NewServer()

kb := iwiki.New(
    iwiki.WithPaasID("your-paas-id"),
    iwiki.WithToken("your-token"),
    iwiki.WithURL("http://api-idc.sgw.woa.com/ebus/iwiki/prod"),
    iwiki.WithServiceName("trpc.test.knowledge.iwiki"),
)
```

对应的 `trpc_go.yaml` 配置：

```yaml
client:
  service:
    - name: trpc.test.knowledge.iwiki
      protocol: http_no_protocol
      timeout: 10000
```

也可以通过 `WithTRPCClientOptions` 在代码中动态设置：

```go
kb := iwiki.New(
    iwiki.WithPaasID("your-paas-id"),
    iwiki.WithToken("your-token"),
    iwiki.WithURL("http://api-idc.sgw.woa.com/ebus/iwiki/prod"),
    iwiki.WithTRPCClientOptions(client.WithTimeout(10 * time.Second)),
)
```

## 架构组件

| 组件 | 说明 |
|------|------|
| iWiki Client | 封装 HTTP 请求和 Rio 鉴权签名 |
| iWiki Retriever | 执行语义搜索，转换结果格式 |
| iWiki Knowledge | 核心知识库管理组件，实现 Knowledge 和 Retriever 接口 |

## 参考文档

- [iWiki RAG OpenAPI 文档](https://iwiki.woa.com/p/4015680433)
- [太湖接入 iWiki 文档](https://iwiki.woa.com/p/36307200)
- [太湖 PaasID 绑定 iWiki](https://iwiki.woa.com/p/4007030209)
- [iWiki OpenAPI 常见问题 QA](https://iwiki.woa.com/p/1855328733)
