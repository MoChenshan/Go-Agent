# Debug Server 使用指南

## 概述

Debug Server 是一个用于调试与验证 Agent 行为的 HTTP 服务端实现，它兼容 [ADK Web UI](https://github.com/google/adk-web) 的接口协议，可以通过可视化界面验证：

- Agent 的对话响应
- 工具（Tool）调用与返回
- 会话（Session）创建与管理
- 流式响应（Server-Sent Events，SSE）

OpenAPI 规范：`trpc/server/debug/openapi.json`（仓库内文件）。

## 设计说明与适用范围

- 目标定位：用于配合 ADK Web 做快速可视化调试，不推荐直接用于生产环境。
- Runner 构造：Debug Server 接收 `Agent`，并根据前端请求的应用名在服务端延迟创建 `runner.Runner`；不支持让用户在此处手动传入 `Runner` 实例。
- 单一会话后端：由于 Debug Server 自身提供会话 REST（list/create/get），要求使用同一个 `session.Service` 作为全局后端，并在内部创建的所有 runner 之间共享。
  - 通过 `debug.WithSessionService(...)` 配置（默认内存实现）。
  - 为保持一致性，Debug Server 会在创建 runner 时强制注入同一会话后端（追加 `runner.WithSessionService(s.sessionSvc)`），覆盖你通过 `WithRunnerOptions` 传入的会话后端。
  - 此处不支持按 app/runner 的多会话后端。
- 生产建议：面向生产（如 AG‑UI）建议自建接受预配置 `runner.Runner` 的服务，或按需使用 `server/a2a`，以便完整控制会话/记忆/工件、鉴权与扩展能力。

## 架构图

```
User Interface
+---------------------------+
|      ADK Web UI           |  ← Access via browser: http://localhost:4200
|        (React)            |
+-----------+---------------+
            | HTTP/SSE Request
            v
+-----------------------------+
|     Debug Server            |  ← Listening on http://localhost:8000
|                             |
|       API Routing           |
|       Session Management    |
|       CORS Handling         |
+-----------+-----------------+
            | Call Agent
            v
+---------------------------------+
|    tRPC-Agent-Go                |
|                                 |
| +-------------+ +--------------+|
| | LLM Agent   | | Tool System  ||
| | • Model Call| | • Calculator ||
| | • Streaming | | • Time Query ||
| | • Prompting | | • Custom Tool||
| +-------------+ +--------------+|
+-----------+---------------------+
            | External Call
            v
+----------------------------------+
|     External Services            |
|                                  |
| • LLM API   (OpenAI/DeepSeek)    |
| • Database   (Redis/MySQL)       |
| • Other API  (Search/File System)|
+----------------------------------+
```

数据流向：

```
用户输入 → Web UI → Debug Server → Agent → LLM/工具 → 流式响应 → Web UI
```

## 使用步骤

1. 创建 Agent。
2. 将 Agent 作为构造参数，创建 Debug Server（包路径：`git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug`），Debug Server 能提供 `http.Handler`。
3. 将 Debug Server 的 `Handler()` 注册到你的 HTTP 服务端（可以是标准 `net/http` 或 tRPC-Go 的 `http_no_protocol` 服务）。
4. 启动后端服务。
5. 安装并启动 ADK Web UI，把后端地址指向你的服务。
6. 在浏览器中进行可视化调试与验证。

可运行示例：

- `examples/debugserver`：https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/debugserver
- `examples/graph_debugserver`：https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/graph_debugserver

## 调试结果展示

通过 ADK Web UI，你可以直接测试调用场景，Web 界面会显示 event 和 trace 信息。

![event](./img/debugserver/event.png)

![trace](./img/debugserver/trace.png)
