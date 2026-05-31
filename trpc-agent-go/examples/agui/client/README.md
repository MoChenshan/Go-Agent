# 客户端示例

`examples/agui/client` 目录存放了可以直接运行的 AG-UI 客户端。目前提供一个 Node.js 版本，后续如有新增可按同样结构扩展。

## 可用客户端

| 子目录 | 说明 |
|--------|------|
| `tdesign-chat/` | 使用 TDesign + Vite + React 开发的聊天 UI。 |
| `copilotkit/` | 使用 `CopilotKit` 开发的 Web UI。 |
| `ag-ui-client-js/` | 使用 `@tencent/ag-ui-client-js` SDK 开发的静态前端页面。 |
| `raw/` | 演示客户端如何解析 AG-UI 协议。 |
| `event_emitter/` | Go 客户端示例：展示自定义事件、进度更新、流式文本等事件消费。 |

## 运行步骤

1. **确保服务端已启动**：先在 `../server` 中选择合适的示例启动，使 `/agui` SSE 接口可用。
2. **进入客户端目录**（以 `copilotkit` 为例）：

```bash
cd copilotkit
pnpm install
pnpm dev
```
