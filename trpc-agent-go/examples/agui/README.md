# AG-UI Examples

该目录汇总了运行 AG-UI 的端到端示例，帮助你快速验证“服务端推送 AG-UI 事件 → 客户端消费 SSE 流”的完整链路。

- [`client/`](client/) – AG-UI 通用客户端示例.
- [`server/`](server/) – AG-UI 通用服务端示例.
- [`messagessnapshot/`](messagessnapshot/) – 示例展示了如何通过消息快照功能获取会话历史记录。

## 快速上手

1. **启动服务端**（默认示例，基于 tRPC-Go）：

```bash
cd server/default
go run .
```

服务将在 `http://127.0.0.1:8080/agui` 提供服务。

2. **运行客户端**（以 copilotkit 为例）：

```bash
cd client/copilotkit
pnpm install
pnpm dev
```

或运行 TDesign chat 客户端：

```bash
cd client/tdesign-chat
pnpm install
pnpm dev
```
