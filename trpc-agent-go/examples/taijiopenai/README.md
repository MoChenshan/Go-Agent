# Taiji OpenAI Error Compatibility Example

这个示例演示如何继续使用 external `model/openai`，同时挂上
`taiji.WithOpenAIErrorCompat()`，让 Taiji 的非标准错误回包能够被
OpenAI SDK 正确识别，并最终在 runner 事件流中透出错误事件。

它和 [taijiagent](../taijiagent) 的区别是：

- `taijiagent` 使用的是内网的 Taiji Agent 接入。
- 本示例使用的是 external `model/openai`。
- 本示例的重点是展示 `taiji.WithOpenAIErrorCompat()` 的接法。

## 必需环境变量

```bash
export TAIJI_API_KEY="your-taiji-token"
```

## 可选环境变量

```bash
export TAIJI_BASE_URL="http://api.taiji.woa.com/openapi"
export TAIJI_MODEL="DeepSeek-V3_1-Online-64k"
```

同样也可以直接用命令行参数覆盖这些值。

## 运行方式

```bash
cd examples/taijiopenai
go run . \
  -api-key "$TAIJI_API_KEY" \
  -base-url "${TAIJI_BASE_URL:-http://api.taiji.woa.com/openapi}" \
  -model "${TAIJI_MODEL:-DeepSeek-V3_1-Online-64k}"
```

## 命令行参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-api-key` | 从 `TAIJI_API_KEY` 读取 | Taiji API key |
| `-base-url` | `http://api.taiji.woa.com/openapi` | Taiji OpenAI 兼容接口地址 |
| `-model` | `DeepSeek-V3_1-Online-64k` | 模型名 |
| `-streaming` | `true` | 是否启用流式输出 |

## 核心代码

核心接法就是在 external `openai.New(...)` 里增加一个 option：

```go
llm := openai.New(
    modelName,
    openai.WithBaseURL(baseURL),
    openai.WithAPIKey(apiKey),
    taiji.WithOpenAIErrorCompat(),
)
```

这样当 Taiji 返回：

```json
{
  "error": {
    "code": "messages array size error: 3",
    "message": "messages array size error: 3",
    "ret_code": -2001,
    "type": "RequestFormatError"
  }
}
```

即使 HTTP 状态码仍是 `200`，也会先被兼容层修正，再由 OpenAI SDK 当作错误处理。
