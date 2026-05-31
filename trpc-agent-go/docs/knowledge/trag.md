# tRAG 知识库接入

[tRAG](https://trag.woa.com/) 是一个检索增强生成（RAG）业务应用全生命周期管理平台。

## 快速开始

```go
package main

import (
    "context"
    "log"
    "time"

    "git.woa.com/trag/trag-sdk/go-trag"
    knowledge "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
    tragsource "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/source"
    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/knowledge/source"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/runner"
    "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func main() {
    ctx := context.Background()

    // 1. 创建 tRAG 客户端和配置
    tragClient := trag.NewTRag(trag.WithToken("your-trag-token"))
    tragOption := sdk.NewTRagOption(
        sdk.WithClient(tragClient),
        sdk.WithInstanceCode("your-rag-instance-code"),
        sdk.WithNamespaceCode("your-namespace-code"),
        sdk.WithCollectionCode("your-collection-code"),
        sdk.WithPolicyCode("your-policy-code"),
    )

    // 2. 创建数据源（推荐使用 tragsource）
    sources := []source.Source{
        tragsource.NewFileSource(
            []string{"./docs/api.md"},
            tragsource.WithFileMetadata(map[string]any{"type": "doc"}),
        ),
    }

    // 3. 创建 tRAG 知识库
    kb, err := knowledge.New(
        knowledge.WithTRagOption(*tragOption),
        knowledge.WithSources(sources),
    )
    if err != nil {
        log.Fatalf("Failed to create tRAG knowledge: %v", err)
    }

    // 4. 加载文档
    if err := kb.Load(ctx,
        knowledge.WithTRagRateLimit(300*time.Millisecond, 5),
    ); err != nil {
        log.Fatalf("Failed to load knowledge base: %v", err)
    }

    // 5. 创建 Agent 并运行
    llmAgent := llmagent.New(
        "trag-assistant",
        llmagent.WithModel(openai.New("claude-4-sonnet-20250514")),
        llmagent.WithKnowledge(kb),
    )

    sessionService := inmemory.NewSessionService()
    appRunner := runner.NewRunner("trag-chat", llmAgent, runner.WithSessionService(sessionService))
    // 执行对话...
}
```

## 配置选项

### TRagOption 配置

通过 `sdk.NewTRagOption()` 创建配置：

| 选项 | 说明 | 必填 |
|------|------|------|
| `WithClient(client)` | tRAG 客户端实例 | 是 |
| `WithInstanceCode(code)` | RAG 实例代码 | 是 |
| `WithNamespaceCode(code)` | 命名空间代码 | 是 |
| `WithCollectionCode(code)` | 集合代码 | 是 |
| `WithPolicyCode(code)` | 策略代码，加载文档数据时必填，仅查询可不填 | 加载时必填 |
| `WithEmbeddingModel(model)` | Embedding 模型名称 | 可选 |


### Knowledge 配置

| 选项 | 说明 |
|------|------|
| `WithTRagOption(opt)` | 设置 tRAG 配置 |
| `WithSources(sources)` | 设置数据源 |
| `WithDisableRemoteChunking(bool)` | 禁用远程分块，使用本地分块 |
| `WithEmbedder(embedder)` | 设置自定义 Embedder，可选配置，配置之后检索时会计算一次 Embedding 传递给 tRAG 匹配 |
| `WithRetriever(retriever)` | 设置自定义 Retriever，可选配置 |
| `WithQueryEnhancer(enhancer)` | 设置自定义 QueryEnhancer，可选配置 |
| `WithReranker(reranker)` | 设置自定义 Reranker，可选配置 |
| `WithImportDocumentHook(hook)` | 添加文档导入钩子，可选配置 |

### Load 配置

| 选项 | 说明 |
|------|------|
| `WithTRagRateLimit(interval, burst)` | 设置限流 |
| `WithSrcParallelism(n)` | 源级并发数 |
| `WithDocParallelism(n)` | 文档级并发数 |

## 数据源

### tRAG 专用数据源（推荐）

默认模式下**必须使用** `tragsource` 包提供的专用数据源：

```go
import tragsource "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/source"
```

| 数据源 | 创建方法 | 说明 |
|--------|----------|------|
| 文本源 | `tragsource.NewTextSource(texts)` | 程序生成的文本 |
| 文件源 | `tragsource.NewFileSource(files)` | 本地文件 |
| 目录源 | `tragsource.NewDirectorySource(dir)` | 批量本地文件 |
| URL 源 | `tragsource.NewURLSource(urls)` | 远程文件链接 |

**使用示例**：

```go
sources := []source.Source{
    // 文本源
    tragsource.NewTextSource(
        []tragsource.TextContent{
            {ID: "doc1", Name: "AI Overview", Content: "..."},
        },
        tragsource.WithTextMetadata(map[string]any{"category": "ai"}),
    ),

    // 文件源
    tragsource.NewFileSource(
        []string{"./docs/api.md"},
        tragsource.WithFileMetadata(map[string]any{"type": "doc"}),
    ),

    // 目录源
    tragsource.NewDirectorySource(
        "./docs",
        tragsource.WithRecursive(true),
        tragsource.WithFileExtFilter([]string{".md", ".txt"}),
    ),

    // URL 源
    tragsource.NewURLSource(
        []string{"https://example.com/doc.pdf"},
        tragsource.WithURLName("Technical Documentation"),
    ),
}
```

### 分块模式

| WithDisableRemoteChunking | 数据源 | 行为 |
|---------------------------|--------|------|
| `false`（默认） | `tragsource.*` | ✅ 推荐：服务端分块 |
| `false`（默认） | 通用 source | ⚠️ 双重分块 |
| `true` | 通用 source | ✅ 客户端分块 |
| `true` | `tragsource.*` | ⚠️ 不分块 |

**本地分块模式**：

```go
kb, _ := knowledge.New(
    knowledge.WithTRagOption(*tragOption),
    knowledge.WithSources(sources),
    knowledge.WithDisableRemoteChunking(true),
)
```

## 文档删除

```go
// 按过滤条件删除
count, err := kb.Delete(ctx, knowledge.WithFilterExpr("source_type == 'trag_file'"))

// 按文档 ID 删除
count, err := kb.Delete(ctx, knowledge.WithDocumentIDs([]string{"id1", "id2"}))
```

## 导入 Hook

```go
hook := func(next knowledge.ImportDocumentFunc) knowledge.ImportDocumentFunc {
    return func(ctx context.Context, src source.Source, doc *document.Document) (*knowledge.ImportResult, error) {
        log.Infof("Importing: %s", doc.ID)
        result, err := next(ctx, src, doc)
        if err == nil {
            log.Infof("Imported: documents=%d", result.DocumentNum)
        }
        return result, err
    }
}

kb, _ := knowledge.New(
    knowledge.WithTRagOption(*tragOption),
    knowledge.WithImportDocumentHook(hook),
)
```

## 架构组件

| 组件 | 说明 |
|------|------|
| tRAG SDK Client | 封装 tRAG 服务连接 |
| tRAG Source | 专用数据源，服务端分块 |
| tRAG Retriever | 语义搜索和重排序 |
| tRAG Knowledge | 核心知识库管理 |


## 示例参考
- [tRAG 简单示例](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/knowledge/trpc/trag_simple) - 基础使用，数据源配置
- [tRAG 数据管理示例](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/knowledge/trpc/trag_data_manage) - Import Hook，文档删除与重导入
- [tRAG 过滤示例](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/knowledge/trpc/trag_filter) - 检索过滤，Agentic Filter
- [tRAG 本地分块示例](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/knowledge/trpc/trag_local_chunking) - 本地分块模式