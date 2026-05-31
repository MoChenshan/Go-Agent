## 内部接入

### 内网版本无缝接入 tRPC Redis 插件

内网版本用户只需引入内网 Redis 存储包即可自动接入 tRPC 的 Redis 插件生态：

```go
import (
    // 启用内网增强
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"

    // 引入内网 Redis 存储支持，自动接入 tRPC Redis 插件
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis"

    // 正常使用 GitHub 版本的 API
    "trpc.group/trpc-go/trpc-agent-go/memory/redis"
)

func main() {
    // Redis 记忆服务会自动使用 tRPC 的 Redis 客户端配置
    memoryService, err := memoryredis.NewService(
        memoryredis.WithRedisClientURL("redis://localhost:6379"),
    )
    // ... 其他代码保持不变
}
```

### Model 配置说明

#### Auto Memory Extractor Model 配置

Auto Memory（自动记忆提取）模式下，需要配置 Extractor 来自动提取用户信息。Extractor 的 Model **需要**工具调用能力，因此**需要**配置 `tool_choice` 等与工具调用相关的 extra field。

推荐使用内部平台提供的封装好的 Model 实现：

**太极平台（推荐）**：

```go
import (
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"
    "trpc.group/trpc-go/trpc-agent-go/memory/extractor"
)

extractorModel := taiji.NewOpenAI("DeepSeek-V3_2-Online-32k",
    taiji.WithAPIKey("your-api-key"),
    taiji.WithBaseURL("http://api.taiji.woa.com/openapi"),
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
    taiji.WithOpenAIInfer(true),  // 启用 openai_infer 模式
    taiji.WithToolChoice(),         // 启用 tool_choice: "auto"，用于工具调用
)

memExtractor := extractor.NewExtractor(extractorModel)
memoryService := memoryinmemory.NewMemoryService(
    memoryinmemory.WithExtractor(memExtractor),
)
```

**混元平台（推荐）**：

```go
import (
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model/hunyuan"
    "trpc.group/trpc-go/trpc-agent-go/memory/extractor"
)

extractorModel := hunyuan.NewOpenAI("hunyuan-a13b",
    hunyuan.WithAPIKey("your-api-key"),
    hunyuan.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    // 混元平台无需额外配置工具调用字段
)

memExtractor := extractor.NewExtractor(extractorModel)
memoryService := memoryinmemory.NewMemoryService(
    memoryinmemory.WithExtractor(memExtractor),
)
```

**通用 OpenAI 兼容 Model**：

```go
import (
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/memory/extractor"
)

extractorModel := openai.New("gpt-4",
    openai.WithAPIKey("your-api-key"),
    openai.WithBaseURL("https://api.openai.com/v1"),
    openai.WithExtraFields(map[string]any{
        "tool_choice": "auto",  // 太极平台必须配置 tool_choice，混元平台不需要
    }),
)

memExtractor := extractor.NewExtractor(extractorModel)
memoryService := memoryinmemory.NewMemoryService(
    memoryinmemory.WithExtractor(memExtractor),
)
```

**重要提示**：

- Auto Memory Extractor 需要模型的工具调用能力来提取用户信息
- 必须配置 `tool_choice: "auto"` 等与工具调用相关的字段
- 太极平台需要同时配置 `WithOpenAIInfer(true)` 和 `WithToolChoice()`
- 混元平台无需额外配置工具调用字段
- 推荐使用 `trpc/model/taiji` 或 `trpc/model/hunyuan` 提供的封装实现，内部已正确处理平台特性

### tMemory 集成

[tMemory](https://test-tmemory.woa.com) 是腾讯内部的托管记忆服务，与本地记忆后端（sqlite、redis 等）的主要区别在于：

- **服务端异步提取**：对话数据推送到 tMemory 后，由服务端异步提取记忆（raw / episodic / profile / graph 多种类型），无需客户端配置 Extractor 或 LLM
- **跨会话持久化**：记忆存储在云端，天然支持跨会话、跨设备的长期记忆
- **只读工具**：Agent 侧只暴露 `memory_search` 工具用于召回，写入由 ingest 管道自动完成

#### 快速接入

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/memory/tmemory"

    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/runner"
    sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func main() {
    // 1. 创建 tMemory 服务（API Key 从 TMEMORY_API_KEY 环境变量自动读取）
    svc, err := tmemory.NewService(
        tmemory.WithBizID("my-app"),        // 业务标识
        tmemory.WithStrategyID("1"),         // 策略 ID
    )
    if err != nil { panic(err) }
    defer svc.Close()

    // 2. 创建 Agent，注册 memory_search 工具
    llmAgent := llmagent.New("assistant",
        llmagent.WithModel(openai.New("gpt-5.2")),
        llmagent.WithTools(svc.Tools()),
    )

    // 3. 创建 Runner，通过 WithSessionIngestor 自动推送对话到 tMemory
    r := runner.NewRunner("my-app", llmAgent,
        runner.WithSessionService(sessioninmemory.NewSessionService()),
        runner.WithSessionIngestor(svc),
    )
    defer r.Close()

    // 4. 正常使用 runner.Run() 进行对话
    // Runner 每轮对话结束后自动调用 svc.IngestSession()
}
```

#### 环境变量

| 变量              | 说明                           | 默认值                        |
| ----------------- | ------------------------------ | ----------------------------- |
| `TMEMORY_API_KEY` | **必需**。tMemory Bearer Token | -                             |
| `TMEMORY_HOST`    | tMemory 服务地址               | `http://test-tmemory.woa.com` |

#### 配置选项

```go
tmemory.NewService(
    tmemory.WithAPIKey("key"),           // 也可显式传入，优先于环境变量
    tmemory.WithHost("http://..."),      // 自定义服务地址
    tmemory.WithBizID("my-app"),         // 业务 ID（也可留空，自动使用 appName）
    tmemory.WithStrategyID("1"),         // ingest/recall 策略 ID
    tmemory.WithSource("my-source"),     // ingest 来源标识
    tmemory.WithTimeout(10*time.Second), // HTTP 超时
    tmemory.WithRecallConfig(map[string]any{  // 自定义 recall 配置
        "raw":      tmemory.VectorRecallConfig{MemoryType: "vector", TopK: 5},
        "episodic": tmemory.VectorRecallConfig{MemoryType: "vector", TopK: 3},
        "profile":  tmemory.ProfileRecallConfig{MemoryType: "profile"},
        "graph":    tmemory.GraphRecallConfig{MemoryType: "graph", TopK: 2, Depth: 2},
    }),
    tmemory.WithAsyncIngestNum(2),                // 异步 ingest 工作线程数
    tmemory.WithIngestQueueSize(20),              // ingest 队列缓冲大小
    tmemory.WithIngestJobTimeout(30*time.Second), // 单次 ingest 超时
)
```

#### 工作流程

```
用户输入 → Agent 对话 → Runner 自动调用 IngestSession
                                ↓
                  tMemory 服务端异步提取记忆（raw/episodic/profile/graph）
                                ↓
下次对话 → Agent 调用 memory_search → Recall API → 返回多维度记忆
```

#### 与本地记忆后端的对比

| 特性     | 本地后端 (sqlite/redis/...)  | tMemory                                                 |
| -------- | ---------------------------- | ------------------------------------------------------- |
| 记忆提取 | 客户端 LLM 提取 (Extractor)  | 服务端异步提取                                          |
| 记忆类型 | 单一向量/关键词搜索          | 多类型 (raw/episodic/profile/graph)                     |
| 接入方式 | `runner.WithMemoryService()` | `runner.WithSessionIngestor()` + `llmagent.WithTools()` |
| 工具类型 | 6 个 (增删改查)              | 仅 `memory_search` (只读)                               |
| 存储位置 | 本地/自建数据库              | 云端托管                                                |

#### 什么时候适合用 tMemory

- 需要**跨会话、跨设备**的长期记忆，希望记忆天然存储在云端，而不是绑在本地进程或单个存储实例上
- 希望直接获得**多维记忆**能力，例如同时召回原始片段、经历总结、用户画像和实体关系，而不是只做单路向量检索
- 不想在业务侧维护 Extractor、提示词、记忆落库流程，而是接受由 tMemory 服务端统一做异步提取

如果你的场景更强调以下能力，通常更适合优先考虑本地 memory 后端：

- 需要**写入后立即可读**，希望同一轮或下一轮稳定召回刚写入的记忆
- 需要业务侧自己掌控记忆的 CRUD、提取时机、存储部署方式
- 需要离线、单机或强内网隔离运行，不希望依赖托管记忆服务

#### 从框架用户的角度理解

从 `trpc-agent-go` 使用者的角度，可以把 tMemory 理解成两条链路：

1. **写入链路**：`runner.WithSessionIngestor(svc)` 负责在每轮对话后，把 session transcript 推送到 tMemory
2. **读取链路**：`llmagent.WithTools(svc.Tools())` 暴露 `memory_search`，让 Agent 在需要时触发 Recall

也就是说，框架侧只负责“推送对话”和“提供召回工具”，并不在本地做记忆提取。`raw / episodic / profile / graph` 这些记忆类型，都是由 tMemory 服务端基于对话内容异步提炼出来的。

#### 四类记忆如何理解

- `raw`：更接近原始对话片段，适合回忆用户说过的具体事实、表述和上下文细节
- `episodic`：更接近“经历”或“事件”级别的抽象总结，适合回忆阶段性行为、任务过程或历史互动
- `profile`：更接近稳定画像，例如身份、偏好、习惯、长期约束
- `graph`：更接近实体与关系网络，适合处理“谁和谁有关”“某事和某概念如何关联”这类关联型召回

默认 Recall 配置会同时覆盖这些记忆类型；如果你的业务更偏重某一类，可以通过 `tmemory.WithRecallConfig()` 调整召回权重、阈值和深度。

#### `strategy_id` 对接入方意味着什么

- `strategy_id` 不只是一个平台侧参数，它会直接影响**多久触发提取、提取哪些记忆、跨 session 共享行为**等实际效果
- 同一个 `biz_id` / `user_id`，使用不同 `strategy_id`，召回体验可能明显不同
- SDK 会在 ingest 和 recall 两侧都透传 `strategy_id`，因此接入时应尽量保持一致，并与 tMemory 平台侧确认当前策略的触发规则

#### 注意事项

- tMemory 的记忆提取是**异步**且**策略驱动**的；`/v1/data/add` 成功只表示数据已被接受，记忆何时可召回取决于当前策略的触发阈值和服务端处理进度
- 当前 SDK 保持最小集成，只封装 `IngestSession -> /v1/data/add` 和内部召回 `-> /v1/memories/recall` 两条基础链路；如需排查 add 请求状态或手动触发提取，可直接参考 tMemory API 文档中的 `/v1/request/status` 和 `/v1/data/flush`
- 召回默认做**跨会话**召回，不按 sessionID 过滤，这样新会话也能召回历史记忆
- 召回能力通过 `memory_search` 工具暴露给 Agent，无需直接调用底层 API

### 参考资源

- [内网示例](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/memory)
- [tMemory 示例](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/memory/tmemory)
- [tMemory API 文档](https://test-tmemory.woa.com/docs)
- [tMemory 架构介绍（KM）](https://km.woa.com/articles/show/649461)
- [tMemory 模块源码](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/trpc/memory/tmemory)
