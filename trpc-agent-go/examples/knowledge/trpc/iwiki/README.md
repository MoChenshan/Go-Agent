# iWiki 知识库搜索示例

本示例展示了如何使用 tRPC Agent 搜索 iWiki 知识库。

## 概览

该示例创建了一个简单的搜索工具，通过 iWiki RAG OpenAPI 检索相关文档并显示相关性分数。它使用了 `iwiki.Knowledge` 实现，通过太湖（TAI）平台的 Rio 鉴权访问 iWiki API。

## 前置条件

- 在[太湖平台](https://tai.it.woa.com)上创建应用，获取 PaasID 和 Token。
- 在太湖平台上为应用订阅 iWiki 的 recall 接口。
- 将太湖 PaasID 绑定到 iWiki，参考：https://iwiki.woa.com/p/4007030209
- 确保应用对目标 space 有访问权限，如需申请权限请访问：https://iwiki.woa.com/public-app/apply?source=iwiki

更多信息参考：
- iWiki RAG OpenAPI 文档：https://iwiki.woa.com/p/4015680433
- 太湖接入 iWiki 文档：https://iwiki.woa.com/p/36307200
- 太湖 PaasID 绑定 iWiki：https://iwiki.woa.com/p/4007030209
- iWiki OpenAPI 常见问题 QA（含 SpaceID 查询方式）：https://iwiki.woa.com/p/1855328733

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

例如在 `trpc_go.yaml` 中配置 target 时：
- IDC/DevCloud 环境使用 `dns://api-idc.sgw.woa.com`
- 桌面/OA 环境使用 `dns://api.sgw.woa.com`

## 运行示例

您可以使用命令行参数或环境变量来配置示例。

### 环境变量

设置以下环境变量以避免每次运行都传递参数：

```bash
export IWIKI_URL="http://api-idc.sgw.woa.com/ebus/iwiki/prod"
export IWIKI_PAAS_ID="your-paas-id"
export IWIKI_TOKEN="your-token"
export IWIKI_SPACE_ID="your-space-id"
```

### 命令行参数

使用 `go run` 运行示例：

```bash
go run main.go \
  -url="http://api-idc.sgw.woa.com/ebus/iwiki/prod" \
  -paas_id="your-paas-id" \
  -token="your-token" \
  -space_id="12345" \
  -query="搜索关键词" \
  -top_k=5
```

可用参数：

- `-url`: iWiki API 地址 (默认: `$IWIKI_URL` 或 `http://api-idc.sgw.woa.com/ebus/iwiki/prod`)
- `-paas_id`: 太湖平台 PaasID (默认: `$IWIKI_PAAS_ID`)
- `-token`: 太湖平台应用 Token (默认: `$IWIKI_TOKEN`)
- `-space_id`: iWiki space ID，限定搜索范围 (默认: `$IWIKI_SPACE_ID`)
- `-service`: tRPC 服务名称 (默认: `trpc.test.knowledge.iwiki`)
- `-query`: 搜索关键词 (默认: `trpc pgvector score calculation`)
- `-top_k`: 返回结果数量 (默认: `5`)
- `-identity`: x-tai-identity 透传 header (可选)
- `-no_truncate`: 不截断搜索结果内容

### 使用示例

```bash
# 使用默认配置运行
go run main.go

# 指定开发环境和自定义查询
go run main.go -url="http://api-idc.sgw.woa.com/ebus/iwiki/dev" -query="Agent Builder" -no_truncate

# 通过环境变量配置后运行
export IWIKI_PAAS_ID="my_app"
export IWIKI_TOKEN="my_token"
go run main.go -query="K8s 核心组件"
```

### 示例输出

```
iWiki Knowledge Search Demo
==================================================
URL: http://api-idc.sgw.woa.com/ebus/iwiki/prod
Service Name: trpc.test.knowledge.iwiki
PaasID: glm_helper

Creating iWiki knowledge base...
Knowledge base created

Searching for 'trpc pgvector score calculation' (top_k=5)...
Found 5 results:
  1. [score=1.000] PGVector
     **该 iwiki 文档同步自外网仓库 main 分支上的[文档](...
     URL: https://iwiki.woa.com/p/4017111309
  ...

Demo completed!
```

## 代码结构

- `main.go`: 包含主应用程序逻辑，演示了如何初始化 `iwiki.Knowledge` 并调用 `Search` 方法。
- `trpc_go.yaml`: tRPC 客户端配置文件，配置目标服务地址和超时等参数。
