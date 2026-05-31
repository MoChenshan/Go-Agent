# A2A tRPC Polaris 示例

本示例展示了如何将 Agent-to-Agent (A2A) 通信功能接入 tRPC 框架，并使用 Polaris 进行服务发现，实现基于 tRPC 的分布式智能体通信解决方案。

## 示例说明

本示例演示了完整的 A2A + tRPC + Polaris 集成方案：
- **A2A 服务端**：启动一个远程 A2A Agent 服务器，注册到 Polaris
- **A2A 客户端**：通过 Polaris 服务发现连接远程 Agent
- **服务发现**：使用 Polaris 实现智能体的自动发现和路由

### 核心特性

1. **Polaris 服务发现**：A2A 服务端注册到 Polaris，客户端通过 Polaris 服务名自动发现
2. **自定义 BasePath**：支持在 Polaris URL 中指定 A2A 服务的路径（如 `/a2a`）
3. **Agent Card URL**：客户端通过 `polaris://service-name/path` 格式获取智能体卡片

## 快速开始

### 前置条件

1. **Polaris 服务注册**：在 Polaris 上注册服务 `trpc.agent.test.a2a`（参见[环境配置](#环境配置)）
2. **API 密钥**：设置 OpenAI 或 DeepSeek API 密钥
3. **Go 环境**：Go 1.18+

### 运行示例

```bash
# 1. 设置 API 密钥
export OPENAI_API_KEY="your-api-key"
export OPENAI_BASE_URL="https://api.deepseek.com/v1"

# 2. 进入示例目录
cd examples/a2aagent/trpc/polaris

# 3. 运行示例
go run main.go -model=deepseek-v3-0324

# 4. 开始对话
You: tell me a joke
🤖 Agent: Why don't scientists trust atoms? Because they make up everything! 😄
```

## A2A 功能介绍

Agent-to-Agent (A2A) 是一个分布式智能体通信协议，允许智能体跨网络边界进行通信和协作。

相关项目：
1. **开源 trpc a2a** [trpc-a2a-go](https://github.com/trpc-group/trpc-a2a-go/)，完整的 A2A 协议 Go 语言实现，可独立使用
2. **内部 trpc a2a**： [trpc-a2a-go](https://git.woa.com/trpc-go/trpc-a2a-go/)，基于开源版本的 trpc-go 生态适配版本，专门设计用于快速将 A2A 服务接入 trpc-go 内部生态系统


trpc-agent-go 中 a2a server 和 a2a agent 的能力底层依赖的是开源 trpc a2a，内部 trpc a2a 的初衷用于接入trpc-go生态，所以这里 trpc-agent-go 中 a2a server 和 a2a agent 如果需要接入tRPC生态的话，还需要依赖一下内部的trpc a2a 项目。


### 核心功能

1. **分布式智能体通信**: 支持智能体在不同服务器、不同网络环境中进行通信
2. **智能体发现**: 通过标准化的智能体卡片机制，自动发现和连接远程智能体
3. **协议转换**: 在本地智能体事件和 A2A 协议消息之间进行透明转换
4. **会话管理**: 支持多轮对话和会话状态管理
5. **流式通信**: 支持实时流式消息传输

### A2A 架构

```
┌─────────────────────┐    A2A Protocol     ┌─────────────────────┐
│    A2A Client       │ ◄─────────────────► │     A2A Server      │
│                     │    HTTP/JSON        │                     │
│ ┌─────────────────┐ │                     │ ┌─────────────────┐ │
│ │   A2A Agent     │ │                     │ │   LLM Agent     │ │
│ │   (Proxy)       │ │                     │ │   (Actual)      │ │
│ └─────────────────┘ │                     │ └─────────────────┘ │
└─────────────────────┘                     └─────────────────────┘
```

### 智能体卡片机制

A2A 使用智能体卡片来描述智能体的元数据和能力：

```json
{
  "name": "agent_joker",
  "description": "i am a remote agent, i can tell a joke",
  "url": "http://127.0.0.1:8088",
  "capabilities": ["chat", "joke"]
}
```

## tRPC 接入方案

本示例展示了如何将 A2A 功能接入 tRPC 框架，快速接入 tRPC 生态。

## tRPC 接入核心组件

### 1. tRPC 服务端接入（支持 Polaris）

将 A2A 服务端注册到 tRPC 服务器，并支持 Polaris 服务发现：

```go
import (
    "git.code.oa.com/trpc-go/trpc-go"
    "git.code.oa.com/trpc-go/trpc-go/server"
    a2a "trpc.group/trpc-go/trpc-agent-go/server/a2a"
    a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
)

// 创建 tRPC 服务
server := trpc.NewServer()

// 设置 Polaris 服务地址和路径
// 格式：polaris://service-name/base-path
host := "polaris://trpc.agent.test.a2a/a2a"

// 创建 A2A 服务端
a2aServer, err := a2a.New(
    a2a.WithHost(host),
    a2a.WithAgent(remoteAgent, streaming),
)

// 注册到 tRPC 服务器
a2atrpc.RegisterA2AServer(server, "trpc.app.agent.joker", a2aServer)

// 启动 tRPC 服务
server.Serve()
```

**关键接入点**:
- 使用 `polaris://service-name/path` 格式指定服务地址
- `/a2a` 是 A2A 协议的 BasePath，Agent Card 将发布在 `/a2a/.well-known/agent-card.json`
- 服务端会自动注册到 Polaris（通过 tRPC 配置）

### 2. tRPC 客户端接入（支持 Polaris）

使用 tRPC HTTP 客户端通过 Polaris 连接 A2A 服务：

```go
import (
    a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
    "trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
    a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
)

// 创建 tRPC HTTP 请求处理器
trpcHTTPHandler := a2atrpc.NewA2ATRPCHTTPReqHandler("trpc.app.client.joker")

// 配置 A2A 客户端使用 tRPC
a2aClientOptions := []a2aclient.Option{
    a2aclient.WithHTTPReqHandler(trpcHTTPHandler),
}

// 创建 A2A 智能体客户端
// 使用 Polaris 服务名 + BasePath 格式
a2aAgent, err := a2aagent.New(
    a2aagent.WithAgentCardURL("polaris://trpc.agent.test.a2a/a2a"),
    a2aagent.WithA2AClientExtraOptions(a2aClientOptions...),
)
```

**关键接入点**:
- 使用 `polaris://service-name/path` 格式作为 Agent Card URL
- tRPC HTTP 处理器会自动通过 Polaris 解析服务地址
- Agent Card 将从 `polaris://trpc.agent.test.a2a/a2a/.well-known/agent-card.json` 获取

### 3. tRPC 配置集成（Polaris 模式）

通过 `trpc_go.yaml` 配置文件管理服务和 Polaris 集成：

```yaml
global:
  namespace: Development              # Polaris 命名空间

server:
  service:
    - name: trpc.app.agent.joker      # A2A 服务端标识
      ip: 0.0.0.0                     # 监听所有网卡
      port: 8088
      protocol: http_no_protocol      # HTTP 协议支持

client:
  service:
    - name: trpc.app.client.joker     # A2A 客户端标识
      target: polaris://trpc.agent.test.a2a  # Polaris 服务名
      protocol: http
      timeout: 0                      # 0 表示使用默认超时

plugins:
  selector:
    polaris:                          # Polaris 服务发现
      protocol: grpc
  registry:
    polaris:                          # Polaris 服务注册
      protocol: grpc
```

**配置要点**:
- 服务端使用 `0.0.0.0` 监听所有网卡，便于 Polaris 注册
- 客户端使用 `polaris://service-name` 格式进行服务发现
- 必须配置 `plugins.selector.polaris` 和 `plugins.registry.polaris`
- `namespace` 必须与 Polaris 上的服务命名空间一致

## tRPC 接入步骤

### 步骤 1: 服务端接入（Polaris 模式）

1. **创建 LLM 智能体**
   ```go
   import (
       "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
       "trpc.group/trpc-go/trpc-agent-go/model/openai"
   )

   modelInstance := openai.New("deepseek-chat")

   remoteAgent := llmagent.New(
       "agent_joker",
       llmagent.WithModel(modelInstance),
       llmagent.WithDescription("i am a remote agent, i can tell a joke"),
       llmagent.WithInstruction("i am a remote agent, i can tell a joke"),
   )
   ```

2. **设置 Polaris 服务地址**
   ```go
   // 格式：polaris://service-name/base-path
   // service-name: Polaris 上注册的服务名
   // base-path: A2A 协议的路径前缀（可选，默认为空）
   host := "polaris://trpc.agent.test.a2a/a2a"
   ```

3. **创建并注册 A2A 服务端**
   ```go
   import (
       a2a "trpc.group/trpc-go/trpc-agent-go/server/a2a"
       a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
   )

   a2aServer, err := a2a.New(
       a2a.WithHost(host),
       a2a.WithAgent(remoteAgent, streaming),
   )

   // 注册到 tRPC 服务器
   // 服务名必须与 trpc_go.yaml 中的 server.service.name 一致
   a2atrpc.RegisterA2AServer(server, "trpc.app.agent.joker", a2aServer)
   ```

### 步骤 2: 客户端接入（Polaris 模式）

1. **创建 tRPC HTTP 处理器**
   ```go
   import (
       a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
   )

   // 服务名必须与 trpc_go.yaml 中的 client.service.name 一致
   // 这个 trpcHTTPHandler 会劫持所有的 HTTP 请求，将请求按照 tRPC 的方式发送至目标服务
   trpcHTTPHandler := a2atrpc.NewA2ATRPCHTTPReqHandler("trpc.app.client.joker")
   ```

2. **配置 A2A 客户端选项**
   ```go
   import (
       a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
   )

   a2aClientOptions := []a2aclient.Option{
       a2aclient.WithHTTPReqHandler(trpcHTTPHandler),
   }
   ```

3. **创建 A2A 智能体（使用 Polaris URL）**
   ```go
   import (
       "trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
   )

   // 使用 Polaris 服务名 + BasePath
   // 格式：polaris://service-name/base-path
   // 由于 HTTP 请求已经被 trpcHTTPHandler 劫持了，这里 AgentCardURL 主要用于提供 HTTP 请求的子路径，这里设置的 子路径是 /a2a
   a2aAgent, err := a2aagent.New(
       a2aagent.WithAgentCardURL("polaris://trpc.agent.test.a2a/a2a"),
       a2aagent.WithA2AClientExtraOptions(a2aClientOptions...),
   )
   ```

**重要说明**:
- Agent Card URL 格式：`polaris://service-name/base-path`
- 实际请求路径：`/base-path/.well-known/agent-card.json`
- 例如：`polaris://trpc.agent.test.a2a/a2a` → `/a2a/.well-known/agent-card.json`

### 步骤 3: 配置文件设置（Polaris 模式）

在 `trpc_go.yaml` 中配置服务、客户端和 Polaris 插件：

```yaml
global:
  namespace: Development              # Polaris 命名空间

server:
  service:
    - name: trpc.app.agent.joker      # A2A 服务端标识
      ip: 0.0.0.0                     # 监听所有网卡
      port: 8088
      protocol: http_no_protocol

client:
  service:
    - name: trpc.app.client.joker     # A2A 客户端标识
      target: polaris://trpc.agent.test.a2a  # Polaris 服务名
      protocol: http
      timeout: 0

plugins:
  log:
    default:
      - writer: console
        level: info
  selector:
    polaris:                          # Polaris 服务发现
      protocol: grpc
  registry:
    polaris:                          # Polaris 服务注册
      protocol: grpc
```

**配置说明**:
- `global.namespace`: 必须与 Polaris 上的服务命名空间一致
- `server.service.ip`: 使用 `0.0.0.0` 以便 Polaris 可以访问
- `client.service.target`: 使用 `polaris://service-name` 格式
- `plugins.selector.polaris`: 启用 Polaris 服务发现
- `plugins.registry.polaris`: 启用 Polaris 服务注册

## 构建和运行

```bash
# 进入示例目录
cd examples/a2aagent/trpc/polaris

# 使用默认设置运行 (deepseek-chat 模型)
go run main.go

# 使用自定义模型运行
go run main.go -model gpt-4o-mini

# 禁用流式模式
go run main.go -streaming=false
```

## 环境配置

### 1. API 密钥配置

为所选模型设置必要的 API 密钥：

```bash
# DeepSeek 配置
export OPENAI_API_KEY="your-deepseek-api-key"
export OPENAI_BASE_URL="https://api.deepseek.com/v1"

# OpenAI 配置
export OPENAI_API_KEY="your-openai-api-key"
# OpenAI 不需要设置 OPENAI_BASE_URL
```

### 2. Polaris 服务注册

**重要**：在运行示例之前，需要在 Polaris 上注册服务。

#### 方式 1：通过 Polaris 控制台注册

1. 访问 Polaris 控制台：http://polaris.woa.com
2. 创建服务：
   - 命名空间：`Development`
   - 服务名：`trpc.agent.test.a2a`
   - 协议：`HTTP`
3. 添加实例：
   - IP：服务器的实际 IP 地址
   - 端口：`8088`
   - 权重：`100`

#### 方式 2：通过 tRPC 自动注册

在 `trpc_go.yaml` 中配置自动注册：

```yaml
plugins:
  registry:
    polaris:
      protocol: grpc
      heartbeat_interval: 5000  # 心跳间隔（毫秒）
```

然后在代码中启用服务注册（示例代码已包含此配置）。

**注意**：
- 服务名 `trpc.agent.test.a2a` 需要与代码中的 Polaris URL 一致
- 命名空间必须与 `global.namespace` 一致
- 确保服务器 IP 可以被客户端访问

## 示例交互

```bash
❯ go run main.go -model=deepseek-v3-0324
plugin log-default setup succeed, time elapsed: 103.265µs
2025-10-27 10:50:53.054 INFO    trpc-naming-polaris@v0.5.27/naming.go:232       naming-polaris starts to set polaris-go logs config: &{DirPath:./polaris/log Level:default MaxBackups:5 MaxSize:50}
2025-10-27 10:50:53.054 INFO    trpc-naming-polaris@v0.5.27/naming.go:238       naming-polaris starts to set polaris-go logs level: false
plugin selector-polaris setup succeed, time elapsed: 9.178264ms
plugin registry-polaris setup succeed, time elapsed: 6.795804ms
🚀 Remote A2A Agent Server Started
==================================================
Service:     trpc.app.agent.joker
Host:        polaris://trpc.agent.test.a2a/a2a
Agent Name:  agent_joker
Description: i am a remote agent, i can tell a joke
==================================================

2025-10-27 10:50:53.071 INFO    server/service.go:211   process: 1079107, http_no_protocol service: trpc.app.agent.joker launch success, tcp: 0.0.0.0:8088, serving ...

🤖 A2A Agent Connected
==================================================
Name:        agent_joker
Description: i am a remote agent, i can tell a joke
URL:         polaris://trpc.agent.test.a2a/a2a
==================================================

💬 Chat with the remote agent
Commands:
  /new  - Start a new session
  /exit - Quit the chat

You: say my name !
🤖 Assistant: Alright, let's channel our inner Walter White with a dramatic pause... *ahem*  

**"You're... Heisenberg!"**  

*(Bonus joke to honor the request: Why did the scarecrow win an award? Because he was outstanding in his field! 🌾😄)*  

Now, how can I assist you today? 😊

You: You're goddamn right!
🤖 Assistant: **"Damn right, I am!** 🔥 *— Walter White nodding in approval*  

*(Bonus joke because why not: What did one ocean say to the other ocean? Nothing, they just waved. 🌊😎)*  

What’s next, boss? Cooking up some plans or just enjoying the vibe? 😏

You: /new
🆕 Started new session: session_1761533513980978311
   (Conversation history has been reset)


You: /exit
👋 Goodbye!
```

## tRPC 接入优势

### 1. 无缝集成
- **零侵入接入**: A2A 功能通过适配器模式接入 tRPC，无需修改现有 tRPC 服务代码
- **配置驱动**: 通过 `trpc_go.yaml` 统一管理 A2A 服务和客户端配置
- **服务发现**: 利用 tRPC 的服务名机制进行智能体服务发现

### 2. 高性能通信
- **连接复用**: 利用 tRPC 的连接池机制，避免频繁建立连接
- **协议优化**: 基于 tRPC 的 HTTP 协议栈，提供高效的消息传输
- **超时控制**: 通过 tRPC 客户端配置实现精确的超时控制

### 3. 运维友好
- **监控集成**: 自动集成 tRPC 的监控指标和链路追踪
- **日志统一**: A2A 通信日志与 tRPC 服务日志统一管理
- **服务治理**: 支持 tRPC 的负载均衡、熔断等服务治理能力

## 示例特性

### 远程 A2A Agent 服务
- 启动一个基于 tRPC 的 A2A Agent 服务器
- 支持流式和非流式响应
- 自动注册到 tRPC 服务框架

### 自动智能体发现
- 从 `/.well-known/agent.json` 端点获取智能体卡片
- 验证智能体元数据和能力
- 基于发现的信息配置客户端

### 交互式聊天界面
- 与远程智能体实时对话
- 使用 `/new` 命令进行会话管理
- 使用 `/exit` 命令优雅退出
- 支持流式响应显示

## tRPC + Polaris 接入关键代码

### 服务端接入代码

```go
func runRemoteAgent(server *server.Server, agentName, desc string) string {
    // 1. 创建 LLM 智能体
    remoteAgent := buildRemoteAgent(agentName, desc)

    // 2. 设置 Polaris 服务地址
    // 格式：polaris://service-name/base-path
    host := "polaris://trpc.agent.test.a2a/a2a"

    // 3. 创建 A2A 服务端
    a2aServer, err := a2a.New(
        a2a.WithHost(host),
        a2a.WithAgent(remoteAgent, *streaming),
    )
    if err != nil {
        log.Fatalf("Failed to create a2a server: %v", err)
    }

    // 4. 注册 A2A 服务端到 tRPC 服务器
    a2atrpc.RegisterA2AServer(server, "trpc.app.agent.joker", a2aServer)

    return host
}
```

### 客户端接入代码

```go
func startChat(host string) {
    // 1. 创建 tRPC HTTP 请求处理器 (劫持 HTTP 流量)
    trpcHTTPHandler := a2atrpc.NewA2ATRPCHTTPReqHandler("trpc.app.client.joker")

    // 2. 配置 A2A 客户端选项
    a2aClientOptions := []a2aclient.Option{
        a2aclient.WithHTTPReqHandler(trpcHTTPHandler),
    }

    // 3. 创建 A2A 智能体客户端（使用 Polaris URL）
    a2aAgent, err := a2aagent.New(
        // 使用 Polaris 服务名 + BasePath (设置子路径: /a2a)
        a2aagent.WithAgentCardURL("polaris://trpc.agent.test.a2a/a2a"),
        a2aagent.WithA2AClientExtraOptions(a2aClientOptions...),
    )
    if err != nil {
        fmt.Printf("Failed to create a2a agent: %v", err)
        return
    }

    // 4. 使用 A2A Agent 进行对话
    agentRunner := runner.NewRunner("a2a-chat", a2aAgent,
        runner.WithSessionService(sessionService))
}
```

## 配置选项

### 命令行参数
- `-model`: 模型名称（默认: "deepseek-chat"）
  - 支持的模型：`deepseek-chat`, `deepseek-v3-0324`, `gpt-4o-mini` 等
- `-streaming`: 启用流式模式（默认: true）

### Polaris URL 格式

**服务端 Host 格式**:
```
polaris://service-name/base-path
```
- `service-name`: Polaris 上注册的服务名（如 `trpc.agent.test.a2a`）
- `base-path`: A2A 协议的路径前缀（如 `/a2a`）

**客户端 Agent Card URL 格式**:
```
polaris://service-name/base-path
```
- 实际请求路径：`/base-path/.well-known/agent-card.json`
- 例如：`polaris://trpc.agent.test.a2a/a2a` → `/a2a/.well-known/agent-card.json`

### tRPC 接入选项
- `a2atrpc.RegisterA2AServer()`: 注册 A2A 服务端到 tRPC 服务器
- `a2atrpc.NewA2ATRPCHTTPReqHandler()`: 创建 tRPC HTTP 请求处理器（支持 Polaris）
- `a2aclient.WithHTTPReqHandler()`: 配置自定义 HTTP 请求处理器

## tRPC + Polaris 故障排除

### 常见问题

1. **Polaris selector 插件未初始化**
   ```
   错误：panic: failed to setup client: client: selector polaris not exist
   ```

   **解决方案**：在 `trpc_go.yaml` 中添加 Polaris 插件配置
   ```yaml
   plugins:
     selector:
       polaris:
         protocol: grpc
     registry:
       polaris:
         protocol: grpc
   ```

2. **Polaris 服务未找到**
   ```
   错误：Polaris-1015(ErrCodeServiceNotFound): service {namespace: "Development",
         service: "trpc.agent.test.a2a"} not found
   ```

   **解决方案**：
   - 确认服务已在 Polaris 上注册
   - 检查 `global.namespace` 与 Polaris 命名空间是否一致
   - 访问 Polaris 控制台验证服务状态：http://polaris.woa.com

3. **Agent Card 404 错误**
   ```
   错误：http client codec StatusCode: Not Found
   ```

   **解决方案**：
   - 检查 BasePath 配置是否正确
   - 服务端 Host：`polaris://service-name/a2a`
   - 客户端 URL：`polaris://service-name/a2a`
   - Agent Card 路径：`/a2a/.well-known/agent-card.json`

4. **端口冲突问题**
   ```bash
   # 检查端口占用
   netstat -tlnp | grep 8088

   # 修改配置文件中的端口
   vim trpc_go.yaml
   ```



## 相关文档

- [trpc-agent-go 项目文档](../README.md)
- [A2A 协议规范](../../docs/a2a-protocol.md)