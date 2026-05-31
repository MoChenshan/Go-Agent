参考 [智研 LLM监控上报](https://iwiki.woa.com/p/4013657225) 相关文档
- [LLM应用监控-网络地址配置](https://iwiki.woa.com/p/4013780362)
- [LLM监控GO SDK上报](https://iwiki.woa.com/p/4013906196)
- [LLM监控span attribute](https://iwiki.woa.com/p/4016237678)

 [例子](../../../examples/telemetry/zhiyan/zhiyan-llm-trpc-plugin/README.md)

## 属性长度限制与截断排查

`zhiyan-llm` 复用 `git.woa.com/zhiyan-monitor/sdk/llm_go_sdk v0.1.15` 的 OpenTelemetry span limits。该 SDK 默认
`attribute_value_length_limit` 为 `8192`，最大值为 `128 * 1024`。如果显式配置超过最大值，最终会被限制到
`128KiB`。

LLM request/response 内容较大时，OpenTelemetry 可能在属性写入阶段截断字符串。截断发生后 exporter 只能看到已截断的
attribute，因此本插件会在 request、response 或 tool definitions JSON 解析失败时记录 warn 日志；当属性长度达到当前
limit，或解析错误形态类似 `unexpected EOF` 时，日志会提示可能已被 `AttributeValueLengthLimit` 截断。

可通过 tRPC YAML 调整：

```yaml
plugins:
  telemetry:
    zhiyan-llm:
      attribute_value_length_limit: 8192
```

也可在代码中通过 `WithAttributeValueLengthLimit(limit)` 调整。建议先使用 SDK 默认值 `8192`；如监控页面仍缺少输入或输出
内容，再按实际 prompt/response 大小调大，但不要超过 SDK v0.1.15 最大值 `128KiB`。