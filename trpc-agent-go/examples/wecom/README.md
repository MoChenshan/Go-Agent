# 企业微信 AI Bot Server 示例

本示例展示如何使用
`git.woa.com/trpc-go/trpc-agent-go/trpc/server/wecom`
把一个 `runner.Runner` 直接接到企业微信 AI Bot websocket
长连接。

当前示例聚焦第一版纯 Agent 场景：

- 使用企业微信 AI Bot websocket 长连接收消息
- 将文本消息转成 runner 请求
- 支持流式回复
- 支持 `/help`、`/new`、`/cancel`

## 前置要求

- Go 1.21 或更高版本
- 已开通企业微信 AI Bot，并拿到长连接所需的
  AI Bot ID 和 Secret
- 可用的 OpenAI 兼容模型配置

模型相关环境变量由 OpenAI SDK 自动读取，通常至少需要：

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`（如果不是默认地址）

示例代码已经包含匿名导入
`_ "git.woa.com/trpc-go/trpc-agent-go/trpc"`，
用于启用内网 tRPC 注入。你如果自己拷贝接入代码，不要漏掉它。

## 环境变量

- `WECOM_STREAM_BOT_ID`: 企业微信 AI Bot 的 `bot id`
- `WECOM_STREAM_SECRET`: 企业微信 AI Bot websocket 长连接密钥
- `WECOM_BOT_NAME`: 机器人名称，可选；群聊里会用于去掉 `@机器人`
- `WECOM_STREAM_WS_URL`: 可选，用来覆盖默认长连接地址

## 运行示例

```bash
cd examples/wecom

export WECOM_STREAM_BOT_ID="your-bot-id"
export WECOM_STREAM_SECRET="your-bot-secret"
export WECOM_BOT_NAME="assistant"
# export WECOM_STREAM_WS_URL="wss://openws.work.weixin.qq.com"

export OPENAI_API_KEY="your-openai-compatible-key"
export OPENAI_BASE_URL="https://api.deepseek.com/v1"

go run . -model deepseek-chat
```

如果不想走流式回复，可以关闭：

```bash
go run . -model deepseek-chat -stream=false
```

## 机器人行为

- 私聊文本消息会直接转给 runner
- 群聊文本消息会自动去掉 `@WECOM_BOT_NAME`
- `/help` 显示命令说明
- `/new` 重置当前会话
- `/cancel` 取消当前请求

## 当前限制

- 目前只实现企业微信 AI Bot websocket 模式
- 目前只处理文本和 mixed 里的文本片段
- 暂不处理附件、图片、素材上传等能力
