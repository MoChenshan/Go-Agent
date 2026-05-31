# 腾讯云向量数据库 (TCVectorDB) 示例

本示例演示如何使用腾讯云向量数据库进行向量存储，支持两种连接模式：

1. **Config 模式**：通过 `trpc_go.yaml` 配置文件管理连接参数
2. **HTTPURL 模式**：通过代码直接传递 URL/username/password

## 工作原理

### 匿名导入包说明

本示例通过匿名导入（side effect import）自动接入 `trpc-database` 中的 TCVectorDB 插件：

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"                  // tRPC 基础配置注入
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/tcvector" // TCVectorDB 存储适配器注入
)
```

其中 `trpc/storage/tcvector` 包会在 `init()` 函数中自动注册 TCVectorDB 客户端构建器，内部使用 `trpc-database/tcvectordb` 库来创建数据库客户端。这样做的好处是：

- **零配置接入**：只需匿名导入包，无需手动初始化数据库连接
- **配置驱动**：所有连接参数通过 `trpc_go.yaml` 配置文件管理
- **统一管理**：由 tRPC 框架统一管理客户端生命周期

### 连接模式

#### 模式一：Config 模式（trpc_go.yaml 配置）

> **插件文档**：完整配置项请参考 [trpc-database/tcvectordb](https://git.woa.com/trpc-go/trpc-database/tree/master/tcvectordb)

配置文件通过 `plugins.database.tcvectordb` 节点自动接入 `trpc-database` 中的 TCVectorDB 插件。其中 `plugins.database` 是 tRPC 框架的数据库插件配置路径，`tcvectordb` 是 `trpc-database/tcvectordb` 包注册的插件名称：

```yaml
client:
  service:
    - name: trpc.agent.knowledge.tcvector  # 服务名称，代码中需要引用

plugins:
  database:
    tcvectordb:
      client_options:
        - service_name: trpc.agent.knowledge.tcvector  # 需要和 client.service.name 保持一致
          url: ${TCVECTOR_URL}                         # 数据库的 URL 地址，使用环境变量
          username: ${TCVECTOR_USERNAME}               # 数据库的用户名，使用环境变量
          key: ${TCVECTOR_PASSWORD}                    # 数据库的密钥，使用环境变量
          timeout: 10s                                 # 单次 HTTP 请求处理的超时时间
```

> **环境变量替换**：tRPC 框架原生支持 `${VAR}` 格式的环境变量替换，启动时会自动用环境变量的值替换配置文件中的占位符。

代码中通过 `WithExtraOptions` 传入 tRPC 的 `client.Option` 选项，其中 `client.WithServiceName` 用于指定服务名称，框架会根据该名称自动匹配 `trpc_go.yaml` 中对应的配置项：

```go
vs, err := tcvector.New(
    // WithExtraOptions 用于传递 tRPC client.Option，这些选项会透传给底层的 trpc-database/tcvectordb 客户端
    // client.WithServiceName 指定服务名称，必须与 trpc_go.yaml 中 client_options.service_name 一致
    tcvector.WithExtraOptions(client.WithServiceName("trpc.agent.knowledge.tcvector")),
)
```

#### 模式二：HTTPURL 模式（代码直接传参）

通过 `WithURL`、`WithUsername`、`WithPassword` 直接在代码中传递连接参数，无需依赖 `trpc_go.yaml` 中的 `plugins.database.tcvectordb` 配置：

```go
vs, err := tcvector.New(
    tcvector.WithURL(url),           // 数据库 URL
    tcvector.WithUsername(username), // 用户名
    tcvector.WithPassword(password), // 密码
)
```

这种模式适用于：
- 连接参数需要动态获取的场景
- 不希望在配置文件中暴露敏感信息
- 快速测试和调试

## 前置条件

1. 申请腾讯云向量数据库实例

2. 设置环境变量：

```bash
# TCVectorDB 连接配置
export TCVECTOR_URL=http://your-tcvector-host:80
export TCVECTOR_USERNAME=root
export TCVECTOR_PASSWORD=your-api-key

# LLM 配置
export OPENAI_BASE_URL=xxx
export OPENAI_API_KEY=xxx
export MODEL_NAME=xxx
```

## 运行

```bash
go run main.go
```

示例会依次测试两种连接模式，每种模式都会完整执行：创建 VectorStore → 加载知识 → 创建 Agent → 执行查询。

## 优势

- **云原生**：全托管的向量数据库服务
- **混合搜索**：支持向量 + 文本混合检索
- **可扩展**：自动扩缩容，高可用
- **持久化存储**：向量数据在重启后依然保留
