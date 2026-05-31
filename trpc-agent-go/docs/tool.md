## 内部接入

内网版本通过空白导入自动启用 tRPC 生态集成：

```go
import (
    // 启用内网增强，只需空白导入一次
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
    
    // 其余代码与外网版本完全一致
    "trpc.group/trpc-go/trpc-agent-go/tool/function"
    "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)
```

**自动获得的能力：**
- ✅ 内网 HTTP 客户端（支持北极星、服务发现）
- ✅ 链路追踪和监控集成
- ✅ 配置文件支持（`trpc_go.yaml`）

### MCP 内网服务发现

内网版本支持多种服务发现方式：

```go
mcpToolSet := mcp.NewMCPToolSet(
    mcp.ConnectionConfig{
        Transport: "sse",
        // 北极星服务发现
        ServerURL: "polaris://weather.service.production/sse",
        
        // 其他方式：
        // ServerURL: "ip://10.0.0.1:8080/sse",      // 直连 IP
        // ServerURL: "dns://api.weather.com/sse",   // DNS 解析
        
        Timeout: 10 * time.Second,
    },
)
```

内网版本的 tRPC 集成让工具系统能够无缝接入内网生态，获得服务发现、负载均衡、监控等特性。

### tRPC 客户端名映射（命中 trpc_go.yaml）

如需让 MCP 工具侧 HTTP 客户端使用 `trpc_go.yaml` 里的某个 client 配置（启用拦截器、限流、连接池等），通过 `WithMCPOptions` 传递 `trpc-mcp-go` 的选项设置客户端名称：

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"                 // 启用内网 tRPC 注入

    // 工具与 MCP 客户端
    "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
    trpcmcp "trpc.group/trpc-go/trpc-mcp-go"
)

toolSet := mcp.NewMCPToolSet(
    mcp.ConnectionConfig{
        Transport: "streamable",                               // 或 "sse"
        ServerURL: "polaris://mcp.service.production/mcp",     // polaris/dns/ip 均可
        Timeout:   30 * time.Second,                            // 统一控制初始化/列表/调用的超时
    },
    mcp.WithMCPOptions(
        trpcmcp.WithServiceName("mcp_tools_client"),            // 命中 trpc_go.yaml 的 client.service.name
        // HTTP 行为与重试
        trpcmcp.WithHTTPHeaders(http.Header{"Authorization": []string{"Bearer xxx"}}),
        trpcmcp.WithClientPath("/mcp"),                         // 自定义 path（streamable）或 "/sse"（SSE）
        trpcmcp.WithClientGetSSEEnabled(true),                  // 启用 GET SSE（兼容老规格）
        trpcmcp.WithSimpleRetry(3),                             // 简单重试
    ),
)
```

示例 `trpc_go.yaml`（client 侧）：

```yaml
client:
  service:
    - name: mcp_tools_client
      protocol: http
      timeout: 0                  # 不强制请求级超时，交由 ctx 控制
      conn_type: httppool
      httppool:
        idle_conn_timeout: 50s    # 客户端空闲连接超时默认 50s
```

### 超时与连接（MCP 工具）

- 请求级超时：tRPC 注入的 HTTP 处理器在 GET（SSE）场景会使用 `client.WithTimeout(0)`，不设置 tRPC 请求级超时；POST 也建议使用 `ctx` 控制端到端时限。
- 统一超时：在工具侧使用 `ConnectionConfig.Timeout`，内部会为 Initialize/ListTools/CallTool 等操作创建带超时的 ctx。
- Idle 超时：
  - 客户端：HTTP 传输 `IdleConnTimeout` 默认 50s；
  - 服务端：`server.service.idletime` 默认 60s；
  - 说明：服务端 Idle 仅对“keep-alive 空闲”生效；请求进行中的流式连接通常不会触发。但若链路上长时间无数据且无应用层心跳，某些代理/负载均衡可能按自身空闲策略断开。因此：
    - MCP SSE：内网版 `SSEServer` 默认启用 keepalive，并每 30s 发送一次 `: keepalive` 注释行（可通过 `WithKeepAlive/WithKeepAliveInterval` 调整），以防链路闲置被中间层关闭。
    - A2A 流式：建议在无业务事件输出时也周期性输出心跳或注释行（flush），提升跨代理的稳定性；端到端时限仍建议用调用 `ctx` 或 `ConnectionConfig.Timeout` 控制。

### 服务器侧（MCP Server / A2A Server）超时建议

- A2A Server 以 tRPC thttp server 运行（参考 a2a.md）：
  - 建议在 `trpc_go.yaml` 配置 `service.timeout/idletime/disable_request_timeout`；
  - 优先级：`method.timeout` > `service.timeout` > `server.timeout`，并与调用 `ctx` 取最小值；
  - IdleTimeout 仅在“连接空闲”时生效，流式/长连接不受其影响。
- MCP Server（内网版 trpc-mcp-go 提供 tRPC 集成）：
  - Streamable HTTP：使用 `git.woa.com/trpc-go/trpc-mcp-go/trpc` 的注册函数把 `mcp.NewServer(...)` 绑定到 tRPC 的 thttp service，复用 `trpc_go.yaml` 的 server 超时与拦截器（如 `service.timeout/idletime/disable_request_timeout`）。
  - SSE：`mcp.NewSSEServer(...)` 实现了 `http.Handler`（有 `ServeHTTP`），可将其挂在 `http.ServeMux` 上，再用 `thttp.RegisterNoProtocolServiceMux` 绑定到 tRPC service，从而同样复用 `trpc_go.yaml`。
  - 与 A2A 相同，`service.timeout/idletime/disable_request_timeout` 可配置；流式/长连接不受 IdleTimeout 影响。

可选代码项速查（与 YAML/ctx 的关系）

- `trpcmcp.WithServiceName(name)`：绑定 trpc_go.yaml 的 client.service.name。
- `trpcmcp.WithHTTPHeaders(h)`：统一附加 HTTP 头。
- `trpcmcp.WithClientPath(p)`：设置 URL 路径（streamable/sse）。
- `trpcmcp.WithClientGetSSEEnabled(bool)`：切换 GET SSE。
- `trpcmcp.WithSimpleRetry(n)` / `trpcmcp.WithRetry(cfg)`：开启重试，作用于底层传输。
- `ConnectionConfig.Timeout`：初始化、列工具、调用工具时为 ctx 统一设置超时（若调用方 ctx 已有 deadline 则不覆盖）。

## MCP 市场与资源

您可以从以下市场找到合适的 MCP 服务:

### 公司内部

- **TICO**: https://mcp.tico.woa.com/mcp
- **Knot**: https://knot.woa.com/mcp/market
- **Venus/Vedas**: https://ai.woa.com/#/vedas/mcp-market/list
- **03 MCP 网关**: https://03.woa.com/mcpServer
- **腾讯太极 MCP Server 市场**: https://taiji.woa.com/web-llm/web/mcp_application_list?wsId=11331
- **蓝鲸 API 网关**: https://bkapigw.woa.com/mcp-market

### 开源与公共 MCP 市场

- **Model Context Protocol 官方**: https://github.com/modelcontextprotocol/servers
- **github MCP Registry**: https://github.com/mcp
- **MCP.so**: https://mcp.so
- **Smithery**: https://smithery.ai
- **AIBase**: https://www.aibase.com/zh/repos/topic/mcp
- **OpenTools**: https://opentools.com/registry?category=all&sort=-installs
- **MCP Market**: https://mcpmarket.com/zh
- **阿里云百炼**: https://bailian.console.aliyun.com/?tab=mcp#/mcp-market
- **百度 MCP World**: https://www.mcpworld.com
- **魔搭社区**: https://modelscope.cn/mcp