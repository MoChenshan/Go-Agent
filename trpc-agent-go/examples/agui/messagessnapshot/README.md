# 消息快照示例

本示例演示如何同时开放常规 AG-UI 聊天端点和消息快照端点，以便客户端在需要时重放完整的对话历史。

- `server/`：运行代理、将事件持久化到内存会话存储并启用 `MessagesSnapshot` 的 Go 服务。
- `client/`：一个最小化的 TypeScript 脚本，先触发一次聊天运行，然后为同一线程获取快照历史。

## 运行服务端

从仓库根目录执行：

```bash
cd trpc-agent-go/examples/agui/messagessnapshot/server
go run .
```

服务器默认监听 `http://127.0.0.1:8080`，对外提供以下端点：

- Chat 端点：`http://127.0.0.1:8080/agui`
- Snapshot 端点：`http://127.0.0.1:8080/history`

可以通过 `-path` 与 `-messages-snapshot-path` 标志重写端点路径。

输出示例：

```log
2025-11-04T20:44:19+08:00       INFO    server/main.go:83       AG-UI: serving agent "agui-agent" on http://127.0.0.1:8080/agui
2025-11-04T20:44:19+08:00       INFO    server/main.go:84       AG-UI: messages snapshot available at http://127.0.0.1:8080/history
```

## 运行客户端

在新终端中执行：

```bash
cd trpc-agent-go/examples/agui/messagessnapshot/client
pnpm install
pnpm dev
```

脚本支持的环境变量：

| 变量 | 说明 | 默认值 |
|----------|-------------|---------|
| `AG_UI_ENDPOINT` | Chat 端点 URL | `http://127.0.0.1:8080/agui` |
| `AG_UI_HISTORY_ENDPOINT` | Snapshot 端点 URL | `http://127.0.0.1:8080/history` |
| `AG_UI_USER_ID` | 转发给服务端的用户标识 | `demo-user` |
| `AG_UI_PROMPT` | 聊天运行时使用的提示词 | 示例数学问题 |
| `AG_UI_THREAD_ID` | 会话线程 ID | 自动生成的时间戳 |

示例：

```bash
AG_UI_USER_ID=alice pnpm dev
```

脚本会先将提示词发送到聊天端点并打印最新响应，然后使用相同的 `threadId`/`userId` 组合调用快照端点，记录服务端返回的完整消息历史。

输出示例：

```log
⚙️ Send chat request to -> http://127.0.0.1:8080/agui
🤖 assistant: 我来帮您计算 2*(10+11)，并详细解释计算过程。

首先，根据数学运算规则，我们需要先计算括号内的表达式：
🛠️ tool(call_00_oP5kNP9GJRa9iBvQZDYPedmy): {"result":21}
🤖 assistant: 括号内的计算结果是 21，所以原表达式变为 2*21。

接下来计算乘法：
🛠️ tool(call_00_I8o1e1miFctbs7Ku66UBl34e): {"result":42}
🤖 assistant: **计算过程解释：**
1. 根据数学运算优先级，先计算括号内的表达式：10 + 11 = 21
2. 然后将结果乘以2：2 × 21 = 42

**最终结论：**
2*(10+11) = **42**
⚙️ Load history -> http://127.0.0.1:8080/history
👤 user(demo-user): 请帮我计算2*(10+11)，并解释计算过程，然后给出最终结论。
🤖 assistant: 我来帮您计算 2*(10+11)，并详细解释计算过程。

首先，根据数学运算规则，我们需要先计算括号内的表达式：
🛠️ tool(call_00_oP5kNP9GJRa9iBvQZDYPedmy): {"result":21}
🤖 assistant: 括号内的计算结果是 21，所以原表达式变为 2*21。

接下来计算乘法：
🛠️ tool(call_00_I8o1e1miFctbs7Ku66UBl34e): {"result":42}
🤖 assistant: **计算过程解释：**
1. 根据数学运算优先级，先计算括号内的表达式：10 + 11 = 21
2. 然后将结果乘以2：2 × 21 = 42

**最终结论：**
2*(10+11) = **42**
```
