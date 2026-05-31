# 灵山（LingShan）知识库搜索示例

本示例展示了如何使用 tRPC Agent 搜索灵山知识库。

## 概览

该示例创建了一个简单的搜索工具，它可以直接调用灵山知识库检索相关文档并显示相关性分数。它使用了 `lingshan.Knowledge` 实现。

## 前置条件

-   运行中的灵山服务实例。
-   有效的知识库 ID (Knowledge Base ID)。

## 运行示例

您可以使用命令行参数或环境变量来配置示例。

### 环境变量

设置以下环境变量以避免每次运行都传递参数：

```bash
export LINGSHAN_URL="http://your-lingshan-service-url"
export LINGSHAN_SERVICE_NAME="trpc.your.lingshan.service"
export LINGSHAN_KB_ID="your-knowledge-base-id"

# 如果需要启用 Embedder（取决于具体配置），请设置以下环境变量：
export OPENAI_API_KEY="sk-..."
export OPENAI_BASE_URL="https://api.openai.com/v1"
```

### 命令行参数

使用 `go run` 运行示例：

```bash
go run main.go -url="http://..." -service_name="trpc..." -kb_id="your-kb-id" -query="公司有哪些福利？"
```

可用参数：

-   `-url`: 灵山服务 URL (默认: $LINGSHAN_URL)。
-   `-service`: 灵山服务名称 (默认: $LINGSHAN_SERVICE_NAME)。
-   `-kb_id`: 灵山知识库 ID (默认: $LINGSHAN_KB_ID)。
-   `-query`: 搜索关键词 (默认: "query something about llm")。

### 使用示例

```bash
export LINGSHAN_KB_ID="kb-123456"
go run main.go -query="搜索关键词"
```

运行后，程序将输出最相关的文档及其匹配分数。

## 代码结构

-   `main.go`: 包含主应用程序逻辑，演示了如何初始化 `lingshan.Knowledge` 并调用 `Search` 方法。
