# LKE + A2A（tRPC 集成）示例

本示例演示如何将 LKE SDK 适配为 `trpc-agent-go` 的 Agent，并通过内网 `a2atrpc` 注册到 tRPC 服务。

## 架构范围

- 适配层：`lke.New(botAppKey, ...) -> agent.Agent`（每次请求创建一次 `lkeClient`）
- 暴露层：`lkeAgent -> A2A Server -> a2atrpc.RegisterA2AServer -> tRPC`
- 本示例只实现服务端；A2A 客户端调用方仅在文档中说明

## 运行方式

在仓库根目录执行：

```bash
cd examples/lke/a2a
go run main.go
```

服务地址和服务名由 `examples/lke/a2a/trpc_go.yaml` 提供。默认配置为：

- 服务名：`trpc.app.agent.lke`
- 监听地址：`127.0.0.1:18901`

服务启动后可通过 `http://127.0.0.1:18901` 获取 Agent Card 并进行 A2A 调用。

## 示例要点

- `lke_original.go`：模拟业务侧已经存在的 LKE 代码（回调处理 + 工具）。
- `lke_adapter.go`：模拟业务侧补的“适配/胶水层”，把这些代码挂到 `lke.WithClientSetup(...)` 里，并通过 `lke.New(...)` 产出一个可运行的 `agent.Agent`。
- `main.go`：将 `agent.Agent` 对外暴露为 A2A 服务（通过 `a2atrpc.RegisterA2AServer(...)` 注册）。

## 调用方接入说明（文档示例）

调用方服务中可使用 `a2aagent.New(...)` 指向上面的 Agent Card 地址，例如：

```go
remoteAgent, err := a2aagent.New(
    a2aagent.WithAgentCardURL("http://127.0.0.1:18901"),
)
// 然后将 remoteAgent 作为 subAgent 或 runner 目标使用
```

## 可替换项

- 将 `WithMock(true)` 替换为真实调用参数配置
- 将 `local_action` 替换为实际工具实现
- 将 `trpc_go.yaml` 中的服务名/端口改为你的环境配置
- 在调用方服务中接入会话态、鉴权与审计逻辑
