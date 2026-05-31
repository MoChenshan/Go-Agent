# CopilotKit 前端示例

本示例演示如何将 Go 实现的 AG-UI 服务端与基于 [CopilotKit](https://docs.copilotkit.ai/) 的 React 前端对接。前端通过 `@ag-ui/client` HTTP agent 订阅 AG-UI 端点暴露的 Server-Sent Events，并借助 CopilotKit 提供的 sidebar 组件展示助手界面。

## 启动 CopilotKit 客户端

```bash
pnpm install   # 或 npm install
pnpm dev       # 或 npm run dev
```

在执行 `pnpm dev` 前可以设置以下环境变量：

- `AG_UI_ENDPOINT`：自定义 AG-UI 服务端地址（默认 `http://127.0.0.1:8080/agui`）。

启动后访问 `http://localhost:3000` 即可体验全屏助手界面。输入框默认占位符为 “Calculate 2*(10+11), first explain the idea, then calculate, and finally give the conclusion.”，按 Enter 能直接运行该示例，也可输入自己的请求。工具调用及其结果会以内联形式展示在对话内容中。

![agui-copilotkit](../../../.resources/agui/client/copilotkit/img/agui-copilotkit.png)
