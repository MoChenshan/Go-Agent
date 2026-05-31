# A2A tRPC Simple 示例

本示例展示了如何将 Agent-to-Agent (A2A) 通信功能接入 tRPC 框架，实现基于 tRPC 的分布式智能体通信解决方案。

## 快速开始

### 前置条件

1. **API 密钥**：设置 OpenAI 或 DeepSeek API 密钥
2. **Go 环境**：Go 1.18+

### 运行示例

```bash
# 1. 设置 API 密钥
export OPENAI_API_KEY="your-api-key"
export OPENAI_BASE_URL="https://api.deepseek.com/v1"

# 2. 进入示例目录
cd examples/a2aagent/trpc/simple

# 3. 运行示例
go run main.go -model=deepseek-v3-0324

# 4. 开始对话
You: tell me a joke
🤖 Agent: Why don't scientists trust atoms? Because they make up everything! 😄
```

## 示例说明

本示例启动一个远程 A2A Agent 服务器，然后通过 A2A 客户端连接并与之对话。演示了：
- 如何创建和启动 A2A 服务端
- 如何通过 A2A 客户端连接远程 Agent
- 如何使用 tRPC 进行服务间通信

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

### 1. tRPC 服务端接入

将 A2A 服务端注册到 tRPC 服务器：

```go
import (
    "git.code.oa.com/trpc-go/trpc-go"
    "git.code.oa.com/trpc-go/trpc-go/server"
    a2a "trpc.group/trpc-go/trpc-agent-go/server/a2a"
    a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
)

// 创建 tRPC 服务
server := trpc.NewServer()

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
- 使用 `a2atrpc.RegisterA2AServer` 将 A2A 服务端注册到 tRPC 服务器
- 通过 tRPC 服务名进行服务标识，获取 tRPC 服务的配置

### 2. tRPC 客户端接入

使用 tRPC HTTP 客户端进行 A2A 通信：

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
a2aAgent, err := a2aagent.New(
    a2aagent.WithAgentCardURL(httpURL),
    a2aagent.WithA2AClientExtraOptions(a2aClientOptions...),
)
```

**关键接入点**:
- 使用 `a2atrpc.NewA2ATRPCHTTPReqHandler` 创建 tRPC HTTP 处理器
- 通过 `WithA2AClientExtraOptions` 注入 tRPC 客户端配置
- 利用 tRPC 的连接管理和超时控制

### 3. tRPC 配置集成

通过 `trpc_go.yaml` 配置文件管理服务：

```yaml
server:
  service:
    - name: trpc.app.agent.joker        # A2A 服务端标识
      ip: 127.0.0.1                     
      port: 8088                        
      protocol: http_no_protocol        # HTTP 协议支持

client:
  service:
    - name: trpc.app.client.joker       # A2A 客户端标识
      target: ip://127.0.0.1:8088       
      protocol: http                    
      timeout: 60000                    # A2A 通信超时配置
```

**配置要点**:
- 服务端使用 `http_no_protocol` 协议类型
- 客户端使用 `http` 协议进行通信
- 通过服务名进行服务发现和路由

## tRPC 接入步骤

### 步骤 1: 服务端接入

1. **创建 LLM 智能体**
   ```go
   import (
       "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
   )
   
   remoteAgent := llmagent.New(
       agentName,
       llmagent.WithModel(modelInstance),
       llmagent.WithDescription(desc),
   )
   ```

2. **获取 tRPC 服务配置**
   ```go
   import (
       a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
   )
   
   host := a2atrpc.GetServiceHost("trpc.app.agent.joker")
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
   
   a2atrpc.RegisterA2AServer(server, "trpc.app.agent.joker", a2aServer)
   ```

### 步骤 2: 客户端接入

1. **创建 tRPC HTTP 处理器**
   ```go
   trpcHTTPHandler := a2atrpc.NewA2ATRPCHTTPReqHandler("trpc.app.client.joker")
   ```

2. **配置 A2A 客户端选项**
   ```go
   a2aClientOptions := []a2aclient.Option{
       a2aclient.WithHTTPReqHandler(trpcHTTPHandler),
   }
   ```

3. **创建 A2A 智能体**
   ```go
   a2aAgent, err := a2aagent.New(
       a2aagent.WithAgentCardURL(httpURL),
       a2aagent.WithA2AClientExtraOptions(a2aClientOptions...),
   )
   ```

### 步骤 3: 配置文件设置

在 `trpc_go.yaml` 中配置服务和客户端：

```yaml
server:
  service:
    - name: trpc.app.agent.joker
      ip: 127.0.0.1
      port: 8088
      protocol: http_no_protocol

client:
  service:
    - name: trpc.app.client.joker
      target: ip://127.0.0.1:8088
      protocol: http
      timeout: 60000
```

## 构建和运行

```bash
# 构建示例
cd examples/a2a-trpc
go build -o a2a-trpc-demo .

# 使用默认设置运行 (deepseek-chat 模型，端口 8088)
./a2a-trpc-demo

# 使用自定义模型运行
./a2a-trpc-demo -model gpt-4o-mini
```

## 环境配置

为所选模型设置必要的 API 密钥：

```bash
# DeepSeek 配置
export OPENAI_API_KEY="your-deepseek-api-key"
export OPENAI_BASE_URL="https://api.deepseek.com/v1"

# OpenAI 配置
export OPENAI_API_KEY="your-openai-api-key"
# OpenAI 不需要设置 OPENAI_BASE_URL
```

## 示例交互

```bash
$ go run main.go -model=deepseek-v3-0324

🚀 Remote A2A Agent Server Started
==================================================
Service:     trpc.app.agent.joker
Host:        127.0.0.1:8088
Agent Name:  agent_joker
Description: i am a remote agent, i can tell a joke
==================================================

http_no_protocol service: trpc.app.agent.joker launch success, tcp: 127.0.0.1:8088, serving ...

🤖 A2A Agent Connected
==================================================
Name:        agent_joker
Description: i am a remote agent, i can tell a joke
URL:         http://127.0.0.1:8088
==================================================

💬 Chat with the remote agent
Commands:
  /new  - Start a new session
  /exit - Quit the chat

You: tell me a joke
🤖 Agent: Why don't scientists trust atoms?

Because they make up everything! 😄

You: /new
🆕 Started new session: session_1729512345678
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

### 远程 A2A Agent
- 启动远程 A2A Agent 服务器
- 通过 A2A 客户端连接远程 Agent
- 展示 A2A 协议的透明性

### 自动智能体发现
- 从 `/.well-known/agent-card.json` 端点获取智能体卡片
- 验证智能体元数据和能力
- 基于发现的信息配置客户端

### 交互式聊天界面
- 与远程智能体实时对话
- 使用 `/new` 命令进行会话管理
- 使用 `/exit` 命令优雅退出
- 支持流式响应显示

## tRPC 接入关键代码

### 服务端接入代码

```go
func runRemoteAgent(server *server.Server, agentName, desc string) string {
    // 1. 创建 LLM 智能体
    remoteAgent := buildRemoteAgent(agentName, desc)

    // 2. 获取 tRPC 服务配置的主机地址
    host := a2atrpc.GetServiceHost("trpc.app.agent.joker")

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
    // 1. 创建 tRPC HTTP 请求处理器
    trpcHTTPHandler := a2atrpc.NewA2ATRPCHTTPReqHandler("trpc.app.client.joker")

    // 2. 配置 A2A 客户端选项
    a2aClientOptions := []a2aclient.Option{
        a2aclient.WithHTTPReqHandler(trpcHTTPHandler),
    }

    // 3. 创建 A2A 智能体客户端
    httpURL := fmt.Sprintf("http://%s", host)
    a2aAgent, err := a2aagent.New(
        a2aagent.WithAgentCardURL(httpURL),
        a2aagent.WithA2AClientExtraOptions(a2aClientOptions...),
    )
    if err != nil {
        fmt.Printf("Failed to create a2a agent: %v", err)
        return
    }

    // 4. 使用 A2A Agent 进行对话
    sessionService := inmemory.NewSessionService()
    agentRunner := runner.NewRunner("a2a-chat", a2aAgent,
        runner.WithSessionService(sessionService))
}
```

## 配置选项

### 命令行参数
- `-model`: 模型名称（默认: "deepseek-chat"）
- `-streaming`: 启用流式模式（默认: true）

### tRPC 接入选项
- `a2atrpc.GetServiceHost()`: 获取 tRPC 服务配置的主机地址
- `a2atrpc.RegisterA2AServer()`: 注册 A2A 服务端到 tRPC 服务器
- `a2atrpc.NewA2ATRPCHTTPReqHandler()`: 创建 tRPC HTTP 请求处理器
- `a2aclient.WithHTTPReqHandler()`: 配置自定义 HTTP 请求处理器

## tRPC 接入故障排除

### 常见接入问题

1. **tRPC 服务配置问题**
   ```bash
   # 检查配置文件
   cat trpc_go.yaml
   
   # 验证服务名配置
   grep "trpc.app.agent.joker" trpc_go.yaml
   ```

2. **服务注册问题**
   ```go
   // 确保服务名与配置文件一致
   a2atrpc.RegisterA2AServer(server, "trpc.app.agent.joker", a2aServer)
   ```

3. **客户端连接问题**
   ```go
   // 确保客户端服务名正确
   trpcHTTPHandler := a2atrpc.NewA2ATRPCHTTPReqHandler("trpc.app.client.joker")
   ```

4. **端口冲突问题**
   ```bash
   # 检查端口占用
   netstat -tlnp | grep 8088
   
   # 修改配置文件中的端口
   vim trpc_go.yaml
   ```

### 调试建议

- 启用 tRPC 框架的调试日志
- 检查 A2A 智能体卡片是否正确发布到 `/.well-known/agent.json`
- 验证 tRPC 客户端超时配置是否合理
- 确认环境变量（API 密钥）设置正确

## 相关文档

- [trpc-agent-go 项目文档](../README.md)
- [A2A 协议规范](../../docs/a2a-protocol.md)