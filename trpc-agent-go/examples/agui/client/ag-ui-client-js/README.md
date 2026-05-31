# ag-ui-client-js

[@tencent/ag-ui-client-js](https://mirrors.tencent.com/#/private/npm/detail?repo_id=537&project_name=%40tencent%2Fag-ui-client-js) 是一个腾讯内部面向浏览器的 AG-UI 客户端 SDK，用于通过 HTTP/SSE 与 AG-UI 服务端通信并实时消费事件。

本 example 使用 `@tencent/ag-ui-client-js` 构建了静态前端页面，展示如何调用 AG-UI SSE 接口并实时打印事件。

## 运行步骤

1. **设置内网镜像源并安装依赖**

```bash
npm config set registry https://mirrors.tencent.com/npm/
npm install
```

2. **（可选）修改接入地址**

若服务端不在默认 `http://127.0.0.1:8080/agui`，请在 `main.js` 中修改 `endpoint`。

3. **运行脚本**

```bash
npm run dev
```

## 核心逻辑

- `HttpAgent` 会向 `/agui` 发送一次 Run 请求（包含简单问题或指令）。
- 通过事件回调实时输出运行阶段、工具调用、增量响应。
- 示例中默认调用 `deepseek-chat`，并演示工具 `calculator` 的多次调用过程。

## 示例输出

![front](../../../.resources/agui/client/ag-ui-client-js/img/front.png)
