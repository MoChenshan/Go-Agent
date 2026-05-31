# 单 service 多 AG-UI Server 示例

该示例演示如何在同一个 `http_no_protocol` tRPC service 上注册多个 AG-UI server，并通过不同 `basePath` 区分路由。

本示例包含两个 agent：

- `calculator-agent`: 提供 `calculator` 工具。
- `time-agent`: 提供 `current_time` 工具。

## 运行

```bash
cd examples/agui/server/multiagent
go run .
```

默认监听地址由 `trpc_go.yaml` 决定，示例默认端口为 `8080`。启动后会暴露两个端点：

- 计算器：`http://127.0.0.1:8080/calc/agui`
- 时间：`http://127.0.0.1:8080/time/agui`

你可以用任意 AG-UI 客户端访问，也可以用 `curl -N` 直接查看 SSE 流，例如：

```bash
curl -N --location 'http://127.0.0.1:8080/calc/agui' \
  -H 'Content-Type: application/json' \
  -d '{"threadId":"thread-1","runId":"run-1","messages":[{"role":"user","content":"10+11等于多少？"}]}'
```

```bash
curl -N --location 'http://127.0.0.1:8080/time/agui' \
  -H 'Content-Type: application/json' \
  -d '{"threadId":"thread-1","runId":"run-2","messages":[{"role":"user","content":"现在几点了？"}]}'
```
