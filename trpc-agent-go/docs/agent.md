# 第三方 Agent

## 太极 Agent

太极 Agent 是基于腾讯太极平台的 AI Agent 实现，提供了与太极平台的完整集成，支持流式和非流式对话、多轮会话管理、多媒体内容处理等能力。

### 主要特性

- **流式/非流式响应**: 支持 SSE 流式输出和标准非流式响应两种模式
- **多轮对话**: 自动管理会话历史，支持上下文连续对话
- **灵活配置**: 支持自定义 HTTP Client、服务名称等配置
- **错误处理**: 完善的错误处理和事件转换机制

### 快速开始

#### 1. 创建太极 Agent

```go
import (
    "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/taiji"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/taiji/sdk"
)

// 创建太极配置，主要包括接口地址、token、agent id
taijiOpts := sdk.NewTaijiOption(
    // 参考 https://iwiki.woa.com/p/4008515885，下面的链接是devcloud环境的URL
    sdk.WithURL("http://stream-server-online-openapi.turbotke.production.polaris:1081"),
    // 参考 https://iwiki.woa.com/p/4008515885
    sdk.WithToken("your-taiji-token"),
    // 进入太极的工作空间 taiji.woa.com 点击你的agent -> 更多操作 -> 调用示例
    // 比如 curl -H 'Authorization: Bearer 7auGXxxxxx' http://stream-server-online-openapi.turbotke.production.polaris:1081/openapi/app_platform/app_create -d '{"query": "玉黛湖附近有哪些美食推荐", "messages": [{"role": "system", "content": ""},{"role": "user", "content": "玉黛湖附近有哪些美食推荐"}], "forward_service": "hyaide-application-18676", "query_id": "qid_123456"}'
    // 能看到 "forward_service": "hyaide-application-18676" 其中 18676 即你的 Agent 的 ApplicationID，
    sdk.WithApplicationID("18676"),
)

// 创建太极 Agent
agent, err := taiji.New(
    taiji.WithAgentName("my-assistant"),
    taiji.WithAgentDescription("A helpful AI assistant powered by Taiji"),
    taiji.WithTaijiOption(taijiOpts),
    taiji.WithStreaming(true), // 启用流式模式
)
if err != nil {
    log.Fatalf("failed to create taiji agent: %v", err)
}
```

#### 2. 使用 Runner 运行 Agent

```go
import (
    "trpc.group/trpc-go/trpc-agent-go/runner"
    "trpc.group/trpc-go/trpc-agent-go/model"
    sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

// 创建SessionService
sessionService := sessioninmemory.NewSessionService()

// 创建Runner
r := runner.NewRunner(
    "my-app",
    agent,
    runner.WithSessionService(sessionService),
)

// 运行 Agent
ctx := context.Background()
userID := "user123"
sessionID := "session-001"
message := model.NewUserMessage("Hello, how are you?")

eventChan, err := r.Run(ctx, userID, sessionID, message)
if err != nil {
    log.Fatalf("failed to run agent: %v", err)
}

// 处理事件
for event := range eventChan {
    if event.Error != nil {
        fmt.Printf("Error: %s\n", event.Error.Message)
        continue
    }
    
    if len(event.Choices) > 0 {
        choice := event.Choices[0]
        // 流式模式使用 Delta 内容
        fmt.Print(choice.Delta.Content)
    }
    
    if event.Done {
        fmt.Println()
        break
    }
}
```

### 配置选项

#### TaijiOption 配置

创建太极 Agent 需要先配置 `TaijiOption`：

```go
taijiOpts := sdk.NewTaijiOption(
    // 必填：认证 Token
    // 参考：https://iwiki.woa.com/p/4008515885
    sdk.WithToken("your-token"),
    
    // 必填：太极平台的应用 ID
    // 参考：https://iwiki.woa.com/p/4014591694
    sdk.WithApplicationID("your-app-id"),
    
    // 太极服务地址配置（两种方式二选一）：
    // 方式1：通过 WithServiceName 指定 trpc_go.yaml 中的 HTTP 客户端配置
    //        可以利用 tRPC 框架的服务发现、负载均衡、超时控制等能力
    //        配置示例见下方说明
    sdk.WithServiceName("trpc.app.client.taiji"),
    
    // 方式2：直接指定太极服务 URL
    //        简单直接，适用于固定地址的场景，下面的链接是devcloud环境的URL
    // sdk.WithURL("http://stream-server-online-openapi.turbotke.production.polaris:1081"),
    
    // 注意：WithURL 的优先级高于 WithServiceName
    //      如果同时配置，将使用 WithURL 指定的地址
    
    // 可选：自定义 HTTP 客户端构建器，用来自定义 http 客户端
    sdk.WithClientBuilder(customClientBuilder),
)
```

#### trpc_go.yaml 配置示例

如果使用 `WithServiceName` 方式配置太极服务地址，需要在 `trpc_go.yaml` 中添加对应的 HTTP 客户端配置：

```yaml
client:
  timeout: 3000  # 全局超时配置(ms)
  service:
    - name: trpc.app.client.taiji  # 与 WithServiceName 配置一致
      protocol: http                # 协议类型
      target: polaris://stream-server-online-sbs-10103  # Polaris 服务发现
      # 或使用直连地址， 下面的链接是devcloud环境的URL：
      # target: ip://stream-server-online-openapi.turbotke.production.polaris:1081
      timeout: 60000  # 针对此服务的超时配置(ms)，流式响应建议设置较长超时
```

**配置说明**：
- `name`：服务名称，需与代码中 `WithServiceName()` 的参数一致
- `protocol`：固定为 `http`
- `target`：太极服务地址，支持以下格式：
  - `polaris://服务名`：使用 Polaris 服务发现（推荐）
  - `ip://host:port`：直连地址
  - `dns://域名:端口`：DNS 解析
- `timeout`：HTTP 请求超时时间（毫秒），流式响应建议设置 60000ms 以上，非流式可以设置更短

#### Agent 配置

使用 `Option` 函数配置 Agent 行为：

```go
agent, err := taiji.New(
    // Agent 名称（用于事件标识）
    taiji.WithAgentName("assistant"),
    
    // Agent 描述
    taiji.WithAgentDescription("AI assistant"),
    
    // 太极平台配置
    taiji.WithTaijiOption(taijiOpts),
    
    // 启用/禁用流式模式
    taiji.WithStreaming(true),
    
    // 事件通道缓冲区大小（默认：256）
    taiji.WithChannelBufSize(512),
)
```

### 流式模式 vs 非流式模式

#### 流式模式 (Streaming)

流式模式使用 SSE 协议，实时返回生成的内容：

```go
agent, _ := taiji.New(
    taiji.WithStreaming(true),
    // 其他选项...
)

// 处理流式响应
for event := range eventChan {
    if len(event.Choices) > 0 {
        // 流式模式使用 Delta 内容
        content := event.Choices[0].Delta.Content
        fmt.Print(content)
    }
    
    if event.Done {
        // 最后一个事件包含完整消息
        fullContent := event.Choices[0].Message.Content
        fmt.Printf("\n[Full message: %s]\n", fullContent)
        break
    }
}
```

#### 非流式模式 (Non-Streaming)

非流式模式一次性返回完整响应：

```go
agent, _ := taiji.New(
    taiji.WithStreaming(false),
    // 其他选项...
)

// 处理非流式响应
for event := range eventChan {
    if event.Done && len(event.Choices) > 0 {
        // 非流式模式使用 Message 内容
        content := event.Choices[0].Message.Content
        fmt.Printf("Response: %s\n", content)
        break
    }
}
```

### 会话历史管理

太极 Agent 自动处理多轮对话的会话历史：

会话历史的构建逻辑：

1. 从 `invocation.Session.Events` 中提取历史消息
2. 过滤掉工具调用相关的事件
3. 转换消息格式为太极平台的格式
4. 保留多媒体内容（URL 格式）
5. 标记其他 Agent 的回复

### 动态指定 Taiji Context

可以通过 `agent.WithCustomAgentConfigs` 在每次调用 `runner.Run()` 时动态指定 Taiji Context，用于传递额外的上下文信息到太极服务：

```go
import (
    "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/taiji"
    "trpc.group/trpc-go/trpc-agent-go/agent"
)

// 在 runner.Run() 时指定 Taiji Context
eventChan, err := runner.Run(
    ctx,
    userID,
    sessionID,
    message,
    // 通过 WithCustomAgentConfigs 指定 Taiji Context
    agent.WithCustomAgentConfigs(map[string]any{
        taiji.RunOptionsKey: &taiji.RunOptions{
            TaijiContext: map[string]any{
                "user_level": "vip",
                "region": "beijing",
                "custom_param": "value",
            },
        },
    }),
)
if err != nil {
    log.Fatalf("failed to run agent: %v", err)
}
```

**说明**：
- `taiji.RunOptionsKey` 是固定的键值，用于标识 Taiji 配置
- `TaijiContext` 是一个 `map[string]any`，用于传递太极 Agent 调用时的上下文信息
- 这些上下文信息会被序列化为 JSON 并发送到太极服务
- 每次 `Run()` 调用都可以指定不同的 Context，实现动态配置


### 自定义 HTTP Client

如果需要使用自定义的 HTTP Client（例如集成 tRPC 的服务发现）：

```go
import ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"

// 自定义客户端构建器
customBuilder := func(opts ...sdk.HTTPClientOption) sdk.HTTPClient {
    // 解析选项
    options := &httpClientOptions{}
    for _, opt := range opts {
        opt(options)
    }
    
    // 创建自定义的 HTTP 客户端
    client := myCustomHTTPClient(options.Name)
    return client
}

// 使用自定义构建器
taijiOpts := sdk.NewTaijiOption(
    sdk.WithURL("http://..."),
    sdk.WithToken("token"),
    sdk.WithApplicationID("app-id"),
    sdk.WithClientBuilder(customBuilder),
    sdk.WithServiceName("taiji-service"),
)
```

### 完整示例

参考 `examples/taijiagent/main.go` 查看完整的多轮对话示例，包括：

- 流式和非流式模式切换
- Redis/内存会话管理
- 特殊命令处理（/history, /new, /exit）
- 错误处理
- 会话切换

运行示例：

```bash
# 使用内存会话
go run examples/taijiagent/main.go \
    -session=inmemory \
    -streaming=true

# 使用 Redis 会话
go run examples/taijiagent/main.go \
    -session=redis \
    -redis-addr=localhost:6379 \
    -streaming=true
```

### 性能调优

#### 1. 调整 Channel Buffer Size

```go
// 对于高吞吐量场景
taiji.WithChannelBufSize(1024)
```

#### 2. 使用连接池

```go
// 配置带连接池的 HTTP 客户端
customClient := &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
    },
}
```

### 参考链接

- [太极平台文档](https://iwiki.woa.com/p/4008515885)
- [应用 ID 配置](https://iwiki.woa.com/p/4014591694)