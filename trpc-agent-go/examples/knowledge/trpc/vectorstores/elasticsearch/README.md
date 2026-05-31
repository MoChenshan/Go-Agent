# Elasticsearch 向量存储示例

本示例演示如何使用 Elasticsearch 进行可扩展的向量搜索。

## 工作原理

### 匿名导入包说明

本示例通过匿名导入（side effect import）自动接入 `trpc-database` 中的 Elasticsearch 插件：

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"              // tRPC 基础配置注入
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/goes" // ES 存储适配器注入
)
```

其中 `trpc/storage/goes` 包会在 `init()` 函数中自动注册 Elasticsearch 客户端构建器，内部使用 `trpc-database/goes` 库来创建 ES 客户端。这样做的好处是：

- **零配置接入**：只需匿名导入包，无需手动初始化 ES 客户端
- **配置驱动**：所有连接参数通过 `trpc_go.yaml` 配置文件管理
- **版本支持**：自动支持 ES v7 和 v8 版本（通过 `trpc-database/goes` 提供）

### trpc_go.yaml 配置说明

> **插件文档**：完整配置项请参考 [trpc-database/goes](https://git.woa.com/trpc-go/trpc-database/tree/master/goes)

配置文件通过 `plugins.database.goes` 节点自动接入 `trpc-database` 中的 Elasticsearch 插件。其中 `plugins.database` 是 tRPC 框架的数据库插件配置路径，`goes` 是 `trpc-database/goes` 包注册的插件名称，框架启动时会自动读取该配置并初始化 ES 客户端：

```yaml
client:
  service:
    - name: trpc.agent.knowledge.es  # 服务名称，代码中需要引用

plugins:
  database:
    goes:
      clientoptions:
        - name: trpc.agent.knowledge.es       # 需要和 client.service.name 保持一致
          url: ${ELASTICSEARCH_HOSTS}         # ES 连接地址，使用环境变量
          user: ${ELASTICSEARCH_USERNAME}     # 用户名，使用环境变量
          password: ${ELASTICSEARCH_PASSWORD} # 密码，使用环境变量
          timeout: 1000000                    # 超时时间，单位 ms，默认 1000ms
          log:
            enabled: true                     # 是否开启日志
            request_enabled: true             # 是否开启请求日志
            response_enabled: true            # 是否开启响应日志
          enable_trace: true                  # 是否复制 ES 响应供 filter 使用
```

> **环境变量替换**：tRPC 框架原生支持 `${VAR}` 格式的环境变量替换，启动时会自动用环境变量的值替换配置文件中的占位符。

代码中通过 `WithExtraOptions` 传入 tRPC 的 `client.Option` 选项，其中 `client.WithServiceName` 用于指定服务名称，框架会根据该名称自动匹配 `trpc_go.yaml` 中对应的配置项：

```go
vs, err := elasticsearch.New(
    // WithExtraOptions 用于传递 tRPC client.Option，这些选项会透传给底层的 trpc-database/goes 客户端
    // client.WithServiceName 指定服务名称，必须与 trpc_go.yaml 中 clientoptions.name 一致
    elasticsearch.WithExtraOptions(client.WithServiceName("trpc.agent.knowledge.es")),
    // WithVersion 指定 ES 版本，支持 ESVersionV7、ESVersionV8
    elasticsearch.WithVersion(string(esstorage.ESVersionV8)),
)
```

## 前置条件

1. 启动 Elasticsearch：

```bash
# Docker 方式（推荐）
docker run -d \
  --name elasticsearch \
  -p 9200:9200 \
  -p 9300:9300 \
  -e "discovery.type=single-node" \
  -e "xpack.security.enabled=false" \
  docker.elastic.co/elasticsearch/elasticsearch:8.11.0
```

2. 设置环境变量（tRPC 框架会自动替换 yaml 中的 `${VAR}` 占位符）：

```bash
# ES 连接配置
export ELASTICSEARCH_HOSTS=http://localhost:9200
export ELASTICSEARCH_USERNAME=elastic
export ELASTICSEARCH_PASSWORD=your-password

# LLM 配置
export OPENAI_API_KEY=your-api-key
```

## 运行

```bash
go run main.go
```

## 版本支持

- **v7**: Elasticsearch 7.x
- **v8**: Elasticsearch 8.0-8.7

> 注意：内部版本暂不支持 v9，如需使用 ES 8.8+ 请使用开源版本。

