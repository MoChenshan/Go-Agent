# Taiji Agent 多轮对话示例

本示例演示如何使用 **[太极 (Taiji)](https://taiji.woa.com/web-llm/web?wsId=10144)** 服务构建多轮对话聊天机器人，支持流式输出和会话管理。


## 必需的环境变量

运行示例前，请设置以下环境变量：

```bash
# Taiji 服务配置
export TAIJI_TOKEN="your-taiji-token"                       # 太极认证令牌（authorization）（当前配置太极的默认值即可），参考https://iwiki.woa.com/p/4008515885

# 进入太极的工作空间 taiji.woa.com 点击你的agent -> 更多操作 -> 调用示例
# 比如 curl -H 'Authorization: Bearer 7auGXxxxxx' http://stream-server-online-openapi.turbotke.production.polaris:1081/openapi/app_platform/app_create -d '{"query": "玉黛湖附近有哪些美食推荐", "messages": [{"role": "system", "content": ""},{"role": "user", "content": "玉黛湖附近有哪些美食推荐"}], "forward_service": "hyaide-application-18676", "query_id": "qid_123456"}'
# 能看到 "forward_service": "hyaide-application-18676" 其中 18676 即你的 Agent 的 ApplicationID，
export TAIJI_APP_ID="your-app-id"

# 以下两个配置选择其一即可
export TAIJI_URL="http://stream-server-online-openapi.turbotke.production.polaris:1081" # 太极服务 devcloud 环境下的URL
# 或者
export TAIJI_SERVICE="trpc.test.taijiagent.taiji"                                       # 太极服务名，会通过服务名索引trpc_go.yaml，根据client配置的target路由，当前默认配置了devcloud环境的URL
```

> **注意**：`TAIJI_URL` 和 `TAIJI_SERVICE` 只需配置其中一个即可。如果两个都配置，系统会优先使用 `TAIJI_URL`。

## 快速开始

### 1. 导航到示例目录

```bash
cd examples/taijiagent
```

### 2. 基本使用

使用默认设置运行示例（内存会话存储 + 流式输出）：

```bash
go run main.go
```

### 3. 使用 Redis 会话存储

```bash
go run main.go -session=redis -redis-addr=localhost:6379
```

### 4. 禁用流式输出

```bash
go run main.go -streaming=false
```

## 配置选项

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-redis-addr` | `localhost:6379` | Redis 服务器地址 |
| `-session` | `inmemory` | 会话服务类型 (`inmemory` / `redis`) |
| `-streaming` | `true` | 是否启用流式输出 |

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `TAIJI_URL` | `http://stream-server-online-openapi.turbotke.production.polaris:1081` | 太极服务 URL |
| `TAIJI_SERVICE` | `trpc.test.taijiagent.taiji` | 太极服务 URL |
| `TAIJI_TOKEN` | `7xxxx` | 太极认证令牌 |
| `TAIJI_APP_ID` | `18676` | 太极应用 ID |

## 使用方法

### 示例会话

```
go run main.go
🚀 Multi-turn Chat with Taiji Agent
Taiji URL: http://stream-server-online-openapi.turbotke.production.polaris:1081
Streaming: true
Type 'exit' to end the conversation
==================================================
✅ Chat ready! Session: taiji-chat-session-1759226236

💡 Special commands:
   /history  - Show conversation history
   /new      - Start a new session
   /exit     - End the conversation

👤 You: say my name!
🤖 Assistant: Heisenberg!  

(Or if you're looking for something else, let me know—I can be whoever you need me to be! 😉)

👤 You: You are goddamn right!
🤖 Assistant: **"Yeah, science!"** 🔬⚗️  

(Glad we're on the same wavelength—*breaking bad* or not, you're the one who knocks! 😎)

👤 You: /history
🤖 Assistant: Here’s our conversation history so far:  

1. **You**: say my name!  
   **Me**: Heisenberg! (Or if you're looking for something else, let me know—I can be whoever you need me to be! 😉)  

2. **You**: You are goddamn right!  
   **Me**: "Yeah, science!" 🔬⚗️ (Glad we're on the same wavelength—*breaking bad* or not, you're the one who knocks! 😎)  

3. **You**: show our conversation history  
   **Me**: *(This response!)*  

Let me know if you’d like to add more to the saga—or rewrite it entirely. 😏

👤 You: 🔥
```

### 特殊命令

- **`/history`**: 显示当前会话的对话历史
- **`/new`**: 开始新的会话（重置对话历史）
- **`/exit`**: 结束对话并退出程序

## 代码架构

### 核心组件

1. **multiTurnChat**: 主要的对话管理结构体
   - 管理会话状态和配置
   - 处理用户输入和 AI 响应
   - 控制流式输出显示

2. **Taiji Agent**: 太极服务集成
   - 使用 `taiji.New()` 创建太极代理
   - 配置服务 URL、认证令牌等参数
   - 支持流式和非流式响应模式

3. **Session Service**: 会话管理
   - 内存存储：`sessioninmemory.NewSessionService()`
   - Redis 存储：`redis.NewService()`
   - 自动管理对话历史和上下文

4. **Runner**: 执行引擎
   - 使用 `runner.NewRunner()` 创建执行器
   - 协调代理和会话服务
   - 处理事件流和错误管理

### 关键函数

- **`setup()`**: 初始化太极代理和会话服务
- **`startChat()`**: 启动交互式对话循环
- **`processMessage()`**: 处理单次消息交换
- **`processResponse()`**: 处理 AI 响应事件流
- **`handleEvent()`**: 处理单个事件（内容、错误等）

### 流式输出处理

```go
// 根据流式模式提取内容
func (c *multiTurnChat) extractContent(choice model.Choice) string {
    if c.streaming {
        // 流式模式：使用增量内容
        return choice.Delta.Content
    }
    // 非流式模式：使用完整消息内容
    return choice.Message.Content
}
```

## Taiji Agent 构建

### 1. 配置太极选项

首先创建太极服务的配置选项，这些选项控制着与太极服务的连接和行为：

```go
// 创建太极配置选项
taijiOpts := sdk.NewTaijiOption(
   // URL 或者 ServiceName 二选一
   // sdk.WithServiceName(taijiServiceName),    // 太极服务名
    sdk.WithURL(taijiURL),                    // 太极服务 URL

    sdk.WithToken(taijiToken),                // 认证令牌
    sdk.WithStreaming(*streaming),            // 是否启用流式输出
    sdk.WithApplicationID(taijiAppID),        // 应用 ID
)
```

### 2. 创建太极 Agent 实例

使用配置选项创建太极 Agent，并设置 Agent 的基本属性：

```go
// 创建太极 Agent
appName := "multi-turn-chat"
agentName := "taiji-assistant"
taijiAgent := taiji.New(
    taiji.WithAgentName(agentName),                                           // Agent 名称
    taiji.WithAgentDescription("A helpful AI assistant powered by Taiji."),   // Agent 描述
    taiji.WithTaijiOption(taijiOpts),                                        // 太极配置选项
    taiji.WithChannelBufSize(512),                                           // 事件通道缓冲区大小（默认：256）
    taiji.WithMaxEventSize(256 * 1024 * 1024),                               // SSE 事件最大缓冲区大小（默认：128KB）
)
```

#### WithMaxEventSize 配置说明

`WithMaxEventSize` 用于设置 SSE（Server-Sent Events）流处理时的最大缓冲区大小。

**何时需要调整：**
- 当收到 `bufio.Scanner: token too long` 错误时
- 当太极服务返回的单行数据超过当前缓冲区大小时
- 处理大型 AI 响应或包含大量数据的流时

**配置建议：**
- **默认值**：128KB（用于放宽 `bufio.Scanner` 默认 64KB 的单行限制）
- **小型应用**：256KB
- **大型应用**：1MB 或更大
- **内存受限**：根据可用内存调整

**示例：**
```go
// 处理超大型响应
taiji.WithMaxEventSize(1024 * 1024), // 1MB

// 内存受限环境
taiji.WithMaxEventSize(128 * 1024),  // 128KB
```

### 3. 集成到 Runner 中
将太极 Agent 集成到 Runner 中，配置会话服务和其他运行时选项：

```go
// 创建 Runner 并集成太极 Agent
c.runner = runner.NewRunner(
    appName,                                    // 应用名称
    taijiAgent,                                // 太极 Agent 实例
    runner.WithSessionService(sessionService), // 会话服务（内存或 Redis）
)
```

### 4. 每次 Run 时指定 Taiji Context

可以通过 `agent.WithCustomAgentConfigs` 在每次调用 `runner.Run()` 时动态指定 Taiji Context，用于传递额外的上下文信息到太极服务：

```go
// 在 runner.Run() 时指定 Taiji Context
eventChan, err := c.runner.Run(
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
```

**说明**：
- `taiji.RunOptionsKey` 是固定的键值，用于标识 Taiji 配置
- `TaijiContext` 是一个 `map[string]any`，用于传递太极 Agent 调用时的上下文信息
- 这些上下文信息会被序列化为 JSON 并发送到太极服务
- 每次 `Run()` 调用都可以指定不同的 Context，实现动态配置

## 故障排除

### 常见问题

1. **连接失败**
   - 检查 `TAIJI_URL` 环境变量是否正确
   - 确认网络连接和服务可用性

2. **认证错误**
   - 验证 `TAIJI_TOKEN` 和 `TAIJI_APP_ID` 是否有效
   - 确认账号具有太极服务访问权限

3. **Redis 连接问题**
   - 检查 Redis 服务是否运行
   - 验证 `-redis-addr` 参数是否正确

4. **流式输出异常**
   - 尝试禁用流式模式：`-streaming=false`
   - 检查网络稳定性

5. **`bufio.Scanner: token too long` 错误**
   - **原因**：SSE 流中的单行数据超过了缓冲区大小
   - **解决方案**：增加 `WithMaxEventSize` 的值
   ```go
   taiji.WithMaxEventSize(512 * 1024 * 1024), // 增加到 512MB
   ```
   - **调试步骤**：
     1. 检查太极服务返回的数据大小
     2. 根据实际数据量调整缓冲区大小
     3. 确保系统有足够的可用内存

### 调试建议

- 启用详细日志查看具体错误信息
- 使用内存会话存储测试基本功能
- 检查环境变量配置是否完整

## 参考文档

- [太极 API 文档](https://iwiki.woa.com/p/4008515885)
- [tRPC-Agent-Go 框架文档](../../README.md)
- [Runner 使用指南](../runner/README.md)
- [Session 管理文档](../../docs/session.md)
