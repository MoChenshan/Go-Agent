# AG-UI SSE 客户端

这个精简的终端示例展示了在没有任何 UI 框架的情况下如何消费 AG-UI 事件。它会打开 SSE 流、通过 AG-UI Go SDK 解析每个帧，并将事件实时打印出来，便于观察智能体的推理过程。

## 运行客户端

在 `examples/agui` 目录执行：

```bash
go run .
```

可通过 `--endpoint` 参数指向其他服务端地址。

## 输出示例

提交 `calculate 1.2+3.5` 后，将得到类似下述的输出（ID 已截断以便阅读）：

```text
Simple AG-UI client. Endpoint: http://127.0.0.1:8080/agui
Type your prompt and press Enter (Ctrl+D to exit).
You> calculate 1.2+3.5
Agent> [RUN_STARTED]
Agent> [TEXT_MESSAGE_START]
Agent> [TEXT_MESSAGE_CONTENT] I'll calculate 1.2 + 3.5 for you.
Agent> [TEXT_MESSAGE_END]
Agent> [TOOL_CALL_START] tool call 'calculator' started, id: call_00_rwe3...
Agent> [TOOL_CALL_ARGS] tool args: {"a": 1.2, "b": 3.5, "operation": "add"}
Agent> [TOOL_CALL_END] tool call completed, id: call_00_rwe3...
Agent> [TOOL_CALL_RESULT] tool result: {"result":4.7}
Agent> [TEXT_MESSAGE_START]
Agent> [TEXT_MESSAGE_CONTENT] The result of 1.2 + 3.5 is **4.7**.
Agent> [TEXT_MESSAGE_END]
Agent> [RUN_FINISHED]
```
