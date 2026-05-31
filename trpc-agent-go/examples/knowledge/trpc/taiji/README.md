# 太极知识集成示例

本示例演示如何将 **[太极知识库](https://iwiki.woa.com/p/4014601151)** 与 tRPC-Agent-Go 框架集成，构建知识增强型聊天机器人。

## 前置条件

1. **太极服务访问权限**: 您需要访问腾讯 [太极](https://taiji.woa.com/web-llm/web?wsId=10144) 服务的权限
2. **环境变量**: 必需的 太极 配置（见下文）
3. **OpenAI API 密钥**: 用于 LLM 模型访问（设置 `OPENAI_API_KEY`）

## 必需的环境变量

运行示例前，请设置以下环境变量：

```bash
# Taiji 服务配置
export TAIJI_SERVICE="trpc.test.knowledge.taiji"          # 太极服务名，会通过服务名索引trpc_go.yaml，根据client配置的target路由，当前默认配置了devcloud环境的URL，也可以通过WithURL的方式直接指定URL，不依赖trpc_go.yaml
export TAIJI_TOKEN="your-taiji-token"                     # 太极认证令牌（authorization）（当前配置太极的默认值即可），参考https://iwiki.woa.com/p/4008515885
export TAIJI_WSID="your-workspace-id"                     # 工作空间 ID（wsid），参考https://iwiki.woa.com/p/4008515885
export TAIJI_EMBEDDING_INDEX_ID="your-embedding-index"    # 索引服务 ID（emb_index），这里填入数字ID即可，参考https://iwiki.woa.com/p/4008515885

# Taiji 混元 API 配置
export TAIJI_HY_API_TOKEN="your-hunyuan-api-token"         # 太极索引服务 API token, token为混元平台右上角的token，直接复制过来即可，参考https://iwiki.woa.com/p/4010689738
export TAIJI_HY_API_URL="http://hunyuanaide.taiji.woa.com" # 太极索引服务 API URL，目前只有这个服务入口，参考https://iwiki.woa.com/p/4010689738

# LLM 模型配置
export OPENAI_API_KEY="your-openai-api-key"               # OpenAI API 密钥
export OPENAI_BASE_URL="URL_ADDRESS-openai-proxy.com"     # OpenAI 代理 URL
```

## 运行

1. 导航到示例目录：
   ```bash
   cd examples/knowledge/taiji
   ```

2. 安装依赖：
   ```bash
   go mod tidy
   ```

## 使用方法

### 基本使用

使用默认设置运行示例：
```bash
go run . 
```

### 指定不同模型

使用不同的 LLM 模型运行：
```bash
go run . -model="gpt-4o-mini"
go run . -model="deepseek-chat"
```

### 控制数据加载

使用 `-load_data` 标志控制是否加载数据到太极（默认为 true）：
```bash
go run . -load_data=false  # 跳过数据加载，如果知识库已加载
```

### 示例会话

```
🧠 Taiji Knowledge-Enhanced Chat Demo
Model: claude-4-sonnet-20250514
Type 'exit' to end the conversation
Available tools: knowledge_search, calculator, current_time
==================================================
plugin selector-polaris setup succeed, time elapsed: 7.151903ms
plugin registry-polaris setup succeed, time elapsed: 5.546252ms
✅ Taiji chat ready! Session: taiji-session-1755570991
📚 Taiji knowledge base loaded with sample documents

💡 Special commands:
   /history  - Show conversation history
   /new      - Start a new session
   /exit      - End the conversation

🔍 Try asking questions like:
   - What is Taiji?
   - Explain the Transformer architecture.
   - What is a Large Language Model?
   - How does Byte-pair encoding work?
   - What is an N-gram model?
   - Calculate 15 * 23
   - What time is it in PST?
   - What tools are available in this chat demo?
👤 You: query MOE
🤖 Assistant: I'll search for information about MOE in the Taiji knowledge base.
🔧 Tool calls initiated:
   • knowledge_search (ID: toolu_vrtx_01TJ2MuvVnheVwRWVQjvM7S9)
     Args: {"query": "MOE"}

🔄 Executing tools...
✅ Tool response (ID: toolu_vrtx_01TJ2MuvVnheVwRWVQjvM7S9): {"text":"ed in Transformer MoE that affect the training stability and final performance. The OLMoE report describes these in some detail.As of 2023, models large enough to use MoE tend to be large language models, where each expert has on the order of 10 billion parameters. Other than language models, Vision MoE is a Transformer model with MoE layers. They demonstrated it by training a model with 15 billion parameters. MoE Transformer has also been applied for diffusion models.\n\nA series of large language models from Google used MoE. GShard uses MoE with up to top-2 experts per layer. Specifically, the top-1 expert is always selected, and the top-2th expert is selected with probability proportional to that experts' weight according to the gating function. Later, GLaM demonstrated a language model with 1.2 trillion parameters, each MoE layer using top-2 out of 64 experts. Switch Transformers use top-1 in all MoE layers.","score":0.3745403289794922,"message":"Found relevant content (score: 0.37)"}
Based on the search results, MOE refers to **Mixture of Experts**, which is an important architectural technique used in large-scale machine learning models, particularly in Transformer architectures. Here's what I found:

## Key Information about MOE (Mixture of Experts):

**What it is:**
- A neural network architecture that uses multiple "expert" networks within a single model
- Each expert typically has around 10 billion parameters in modern large language models

**How it works:**
- Uses a gating function to select which experts to activate for each input
- Common approaches include:
  - **Top-1**: Only the highest-weighted expert is selected
  - **Top-2**: The top expert is always selected, plus a second expert selected probabilistically

**Notable implementations:**
- **GShard**: Uses MoE with up to top-2 experts per layer
- **GLaM**: A 1.2 trillion parameter language model using top-2 out of 64 experts per MoE layer
- **Switch Transformers**: Uses top-1 selection in all MoE layers
- **Vision MoE**: Applied to computer vision with 15 billion parameters
- **OLMoE**: Mentioned as having detailed reporting on training stability and performance factors

**Applications:**
- Large language models (most common as of 2023)
- Computer vision models
- Diffusion models

The MoE architecture allows for scaling model capacity while keeping computational costs manageable by only activating a subset of experts for each input.
```

## 架构

### 组件

1. **Taiji SDK Client** (`sdk/taiji_option.go`): 封装腾讯 Taiji 服务的连接配置，包括工作空间 ID、Embedding索引 ID、混元 API 配置等核心参数
2. **Taiji Embedder** (`embedder/embedder.go`): 使用 Taiji Embedding服务处理文本向量化，支持获取Embedding维度和使用统计
3. **Taiji Retriever** (`retriever/retriever.go`): 在 Taiji 向量存储中执行语义搜索，支持查询增强和重排序
4. **Taiji Knowledge** (`knowledge.go`): 核心知识库管理组件，协调文档加载、并发处理和搜索功能

## 快速开始

**配置说明**:
- **本地文件处理**: 使用 `trpc.group/trpc-go/trpc-agent-go/knowledge/source/file.Source` 或 `trpc.group/trpc-go/trpc-agent-go/knowledge/source/url.Source` 处理本地文件和 URL 资源，trpc-agent-go 会将文档切分成块然后上传到 Taiji
- **限流处理**: 支持通过 `WithTaijiRateLimit` 配置限流参数，避免 API 调用过于频繁
- **embedding 处理**: Taiji 使用内置的Embedding模型，无需额外指定 embedder

### Taiji 选项配置

示例演示了完整的 Taiji 配置选项：

```go
// 创建 Taiji 选项
taijiOption := sdk.NewTaijiOption(
    // 基础配置
    sdk.WithToken(taijiToken),                    // 太极认证令牌
    sdk.WithWSID(taijiWSID),                      // 工作空间 ID
    sdk.WithEmbIndex(taijiEmbeddingIndexID),      // 索引服务 ID（数字ID）
    
    // 混元 API 配置（用于加载数据）
    sdk.WithTaijiHYAPIToken(taijiHYAPIToken),     // 混元平台token
    sdk.WithTaijiHYAPIURL(taijiHYAPIURL),         // 混元API地址
    
    // 太极服务地址配置（两种方式二选一）：
    // 方式1：通过 WithServiceName 指定 trpc_go.yaml 中的 HTTP 客户端配置
    //        可以利用 tRPC 框架的服务发现、负载均衡、超时控制等能力
    //        配置示例见下方 trpc_go.yaml
    sdk.WithServiceName("trpc.test.knowledge.taiji"),
    
    // 方式2：直接指定太极服务 URL
    //        简单直接，适用于固定地址的场景
    // sdk.WithURL(taijiURL),
    
    // 注意：WithURL 的优先级高于 WithServiceName
    //      如果同时配置，将使用 WithURL 指定的地址
)
```

#### trpc_go.yaml 配置

如果使用 `WithServiceName` 方式，需要在 `trpc_go.yaml` 中添加对应的 HTTP 客户端配置：

```yaml
client:
  timeout: 3000  # 全局超时配置(ms)
  service:
    - name: trpc.test.knowledge.taiji  # 与 WithServiceName 配置一致
      protocol: http                    # 协议类型
      target: polaris://stream-server-online-sbs-10103  # Polaris 服务发现
      # 或使用直连地址，下面的链接是devcloud环境的URL：
      # target: ip://stream-server-online-openapi.turbotke.production.polaris:1081
      timeout: 60000  # 针对此服务的超时配置(ms)，太极服务建议设置较长超时
```

**配置说明**：
- `name`：服务名称，需与代码中 `WithServiceName()` 的参数一致
- `protocol`：固定为 `http`
- `target`：太极服务地址，支持以下格式：
  - `polaris://服务名`：使用 Polaris 服务发现（推荐）
  - `ip://host:port`：直连地址
  - `dns://域名:端口`：DNS 解析
- `timeout`：HTTP 请求超时时间（毫秒），建议设置 60000ms 以上

### 数据源配置

示例支持多种数据源类型：

```go
// Create diverse sources showcasing different types.
sources := []source.Source{
    // URL 源 - 客户端下载后上传
    urlsource.New(
        []string{
            "https://cloud.tencent.com/document/product/1709/94945",
        },
        urlsource.WithName("Large Language Model"),
        urlsource.WithMetadataValue("topic", "Large Language Model"),
        urlsource.WithMetadataValue("source", "official"),
    ),

    // 文件源 - 本地 Markdown 文件
    filesource.New(
        []string{
            "../data/llm.md",
        },
        filesource.WithName("Large Language Model"),
        filesource.WithMetadataValue("type", "documentation"),
    ),

    // 目录源 - 批量处理目录文件
    dirsource.New(
        []string{
            "../dir",
        },
        dirsource.WithName("Data Directory"),
    ),
}
```

### 知识库创建与加载

```go
// 创建太极知识库，太极知识库不需要配置Embedder
kb, err := knowledge.New(
    knowledge.WithTaijiOption(taijiOption),
    knowledge.WithSources(sources),
)
if err != nil {
    return fmt.Errorf("failed to create Taiji knowledge: %w", err)
}
```

**注意**：如果知识库文档已经加载完成，请勿再调用 `Load` 方法，以避免不必要的资源消耗和潜在的重复导入问题。

```go
// 加载知识库（支持并发控制和限流）
if *loadData {
    documentIDs, err := kb.Load(ctx,
        knowledge.WithTaijiRateLimit(300*time.Millisecond, 5),   // 限流：3 QPS，burst=5
    )
    if err != nil {
        return fmt.Errorf("failed to load Taiji knowledge base: %w", err)
    }
    log.Printf("Successfully loaded %d documents to Taiji knowledge base", len(documentIDs))
}
```

### 定义 LLM 配置

```go
// 配置生成参数
genConfig := model.GenerationConfig{
    MaxTokens:   intPtr(2000),
    Temperature: floatPtr(0.7),
    Stream:      true, // 启用流式响应
}

// 创建 LLM 代理
llmAgent := llmagent.New(
    "taiji-assistant",
    llmagent.WithModel(modelInstance),
    llmagent.WithDescription("具有 Taiji 知识库访问能力的 AI 助手"),
    llmagent.WithInstruction("使用 knowledge_search 工具从 Taiji 知识库中查找相关信息"),
    llmagent.WithGenerationConfig(genConfig),
    llmagent.WithKnowledge(kb), // 设置Knowledge
)
```

#### 运行 LLM Agent
定义好了带有知识库的LLM Agent，使用Runner发起调用即可

```go
c.runner = runner.NewRunner(
   appName,
   llmAgent,
   runner.WithSessionService(sessionService),
)
```

### 故障排除

**常见问题**:

1. **认证失败**: 检查 `TAIJI_TOKEN` 和相关认证信息是否正确
2. **连接超时**: 检查 `trpc_go.yaml` 中的太极服务配置和网络连接状态
3. **限流错误**: 调整 `WithTaijiRateLimit` 参数，降低请求频率
4. **文档加载失败**: 检查数据源路径和文件格式是否支持

**调试建议**:
- 启用详细日志查看具体错误信息
- 使用 `-load_data=false` 跳过数据加载测试基本功能
- 检查环境变量配置是否完整

### 参考文档

- [太极API文档](https://iwiki.woa.com/p/4008515885)
- [太极索引服务API文档](https://iwiki.woa.com/p/4010689738)
- [tRPC-Agent-Go 框架文档](../../README.md)