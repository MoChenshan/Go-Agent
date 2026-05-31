# PostgreSQL (PGVector) 向量存储示例

本示例演示如何使用 PostgreSQL 配合 pgvector 扩展进行持久化向量存储。

## 工作原理

### 匿名导入包说明

本示例通过匿名导入（side effect import）自动接入 `trpc-database` 中的 PostgreSQL 插件：

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"                 // tRPC 基础配置注入
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/postgres" // PostgreSQL 存储适配器注入
)
```

其中 `trpc/storage/postgres` 包会在 `init()` 函数中自动注册 PostgreSQL 客户端构建器，内部使用 `trpc-database/postgres` 库来创建数据库客户端。这样做的好处是：

- **零配置接入**：只需匿名导入包，无需手动初始化数据库连接
- **配置驱动**：所有连接参数通过 `trpc_go.yaml` 配置文件管理
- **连接池管理**：由 tRPC 框架统一管理连接池生命周期

### trpc_go.yaml 配置说明

> **插件文档**：完整配置项请参考 [trpc-database/postgres](https://git.woa.com/trpc-go/trpc-database/tree/master/postgres)

配置文件通过 `client.service` 和 `plugins.database.postgres` 节点自动接入 `trpc-database` 中的 PostgreSQL 插件。其中 `plugins.database` 是 tRPC 框架的数据库插件配置路径，`postgres` 是 `trpc-database/postgres` 包注册的插件名称：

```yaml
client:
  service:
    - name: trpc.agent.knowledge.postgres
      # DSN 格式: postgres://user:password@host:port/database?sslmode=disable
      # 使用环境变量: PGVECTOR_USER, PGVECTOR_PASSWORD, PGVECTOR_HOST, PGVECTOR_PORT, PGVECTOR_DATABASE
      target: postgres://${PGVECTOR_USER}:${PGVECTOR_PASSWORD}@${PGVECTOR_HOST}:${PGVECTOR_PORT}/${PGVECTOR_DATABASE}?sslmode=disable

plugins:
  database:
    postgres:
      max_idle: 20         # 最大空闲连接数
      max_open: 100        # 最大在线连接数
      max_lifetime: 180000 # 连接最大生命周期，单位 ms
```

> **环境变量替换**：tRPC 框架原生支持 `${VAR}` 格式的环境变量替换，启动时会自动用环境变量的值替换配置文件中的占位符。

代码中通过 `WithExtraOptions` 传入 tRPC 的 `client.Option` 选项，其中 `client.WithServiceName` 用于指定服务名称，框架会根据该名称自动匹配 `trpc_go.yaml` 中对应的配置项：

```go
vs, err := pgvector.New(
    // WithExtraOptions 用于传递 tRPC client.Option，这些选项会透传给底层的 trpc-database/postgres 客户端
    // client.WithServiceName 指定服务名称，必须与 trpc_go.yaml 中 client.service.name 一致
    pgvector.WithExtraOptions(client.WithServiceName("trpc.agent.knowledge.postgres")),
)
```

## 前置条件

1. 安装 PostgreSQL 并启用 pgvector 扩展：

```bash
# Docker 方式（推荐）
docker run -d \
  --name postgres-pgvector \
  -e POSTGRES_PASSWORD=yourpassword \
  -e POSTGRES_DB=vectordb \
  -p 5432:5432 \
  pgvector/pgvector:pg16
```

2. 设置环境变量（tRPC 框架会自动替换 yaml 中的 `${VAR}` 占位符）：

```bash
# PostgreSQL 连接配置（与 util.go 中的环境变量名一致）
export PGVECTOR_HOST=127.0.0.1
export PGVECTOR_PORT=5432
export PGVECTOR_USER=postgres
export PGVECTOR_PASSWORD=yourpassword
export PGVECTOR_DATABASE=vectordb

# LLM 配置
export OPENAI_BASE_URL=xxx
export OPENAI_API_KEY=xxx
export MODEL_NAME=xxx
```

## 运行

```bash
go run main.go
```
