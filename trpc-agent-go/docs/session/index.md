## 内部接入

内部版本可以无缝接入 tRPC Redis 插件，用户只需引入内部 Redis 存储包即可自动接入 tRPC 的 Redis 插件生态：

### 通过 URL 指定 (以 Redis Session 为例)

```go
import (
    // 启用内部增强
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
    
    // 引入内部 Redis 存储支持，自动接入 tRPC Redis 插件
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis"
    
    // 正常使用 GitHub 版本的 API
    "trpc.group/trpc-go/trpc-agent-go/session/redis"
)

func main() {
    // Redis 会话服务会自动使用 tRPC 的 Redis 客户端配置
    sessionService, err := redis.NewService(
        redis.WithURL("redis://localhost:6379/0"),
    )
    // ... 其他代码保持不变
}
```

### 结合 trpc_go.yaml 配置文件

```go
import (
	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/client"
    // 启用内部增强
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
    // 引入内部 Redis 存储支持，自动接入 tRPC Redis 插件
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis"
    // 正常使用 GitHub 版本的 API
	"trpc.group/trpc-go/trpc-agent-go/session/redis"
)

func main() {
	// Load config from trpc_go.yaml
	_ = trpc.NewServer()
    // Resolve backend by service name. The actual target (e.g., redis://...) is read from trpc_go.yaml.
	sessionService, err := redis.NewService(
		redis.WithExtraOptions(client.WithServiceName("trpc.test.helloworld.redis")),
	)
    // ...
}
```

**配置文件**

```yaml
global:  # 全局配置
  namespace: Development  # 环境类型，分正式 production 和非正式 development 两种类型
  env_name: test  # 环境名称，非正式环境下多环境的名称


client:  # 客户端调用的后端配置
  timeout: 10000  # 针对所有后端的请求最长处理时间
  namespace: Development  # 针对所有后端的环境
  service:  # 针对单个后端的配置
    - name: trpc.test.helloworld.redis  # 后端服务的 service name
      target: redis://username:password@127.0.0.1:6379  # 请求服务地址（使用 docker-compose 创建的用户名/密码）

plugins:  # 插件配置
  log:  # 日志配置
    default:  # 默认日志的配置，可支持多输出
      - writer: console  # 控制台标准输出 默认
        level: debug  # 标准输出日志的级别
      - writer: file  # 本地文件日志
        level: info  # 本地文件滚动日志的级别
        writer_config:
          filename: ./trpc.log  # 本地文件滚动日志存放的路径
          max_size: 10  # 本地文件滚动日志的大小 单位 MB
          max_backups: 10  # 最大日志文件数
          max_age: 7  # 最大日志保留天数
          compress: false  # 日志文件是否压缩
```

### 超时说明（LLM/流式调用）

- 内部 HTTP 适配器对 GET/POST 默认使用 `client.WithTimeout(0)`，避免 LLM 流式长连接被过小的全局超时（例如 500ms）提前切断。
- 建议采用以下方式进行显式超时控制：
  - 在业务调用处用 `context.WithTimeout` 包裹：`ctx, cancel := context.WithTimeout(ctx, 60*time.Second)`，并传入 `agent.Run`。
  - 对 OpenAI 兼容 SDK 使用请求级超时：`openaiopt.WithRequestTimeout(30*time.Second)`（`github.com/openai/openai-go/option`）。

### Model 配置说明

#### Session Summary Model 配置

Session Summary（会话摘要）使用的 Model 不需要工具调用，因此**不需要**配置 `tool_choice` 等与工具调用相关的 extra field。如果使用了这些字段，部分模型可能会报错。

推荐使用内部平台提供的封装好的 Model 实现：

**太极平台（推荐）**：

```go
import (
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"
)

summaryModel := taiji.NewOpenAI("DeepSeek-V3_2-Online-32k",
    taiji.WithAPIKey("your-api-key"),
    taiji.WithBaseURL("http://api.taiji.woa.com/openapi"),
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
    // 对于 Summary，无需配置 WithToolChoice 等工具调用相关字段
)
```

**混元平台（推荐）**：

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/model/hunyuan"

summaryModel := hunyuan.NewOpenAI("hunyuan-a13b",
    hunyuan.WithAPIKey("your-api-key"),
    hunyuan.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    // 对于 Summary，无需配置 WithToolChoice 等工具调用相关字段
)
```

**通用 OpenAI 兼容 Model**：

```go
import "trpc.group/trpc-go/trpc-agent-go/model/openai"

summaryModel := openai.New("gpt-4",
    openai.WithAPIKey("your-api-key"),
    openai.WithBaseURL("https://api.openai.com/v1"),
    // 不要添加 tool_choice 等工具调用相关字段
)
```

**重要提示**：

- Summary 只需要模型的文本生成能力，不需要工具调用功能
- 配置 `tool_choice` 等字段时，如果模型没有提供 tools 数组，可能导致 API 调用失败
- 推荐使用 `trpc/model/taiji` 或 `trpc/model/hunyuan` 提供的封装实现，内部已正确处理平台特性
