## 企业微信 AI Bot websocket 接入指南

`trpc/server/wecom` 用来把一个 `runner.Runner` 直接接到
企业微信 AI Bot 的 websocket 长连接。

它主要负责三件事：

- 建立并维护到企业微信 AI Bot 的 websocket 长连接
- 把企业微信收到的文本消息转换成 `runner.Run(...)` 的输入
- 把 Runner 产出的事件流转换成企业微信可消费的回复消息

完整示例代码见 [examples/wecom](../examples/wecom/)。

### 适用场景

- 你已经有一个 Agent 或 Runner，希望最快接到企业微信 AI Bot
- 你想直接复用框架内置的 slash 命令支持，如 `/help`、
  `/new`、`/cancel`
- 你希望直接消费 Runner 事件流，而不是自己处理企业微信
  websocket 协议细节

### 先理解这一层在做什么

和 `AG-UI`、`A2A`、`HTTP service` 这类“对外暴露一个服务接口”的
方式不同，WeCom AI Bot websocket 是“当前进程主动连出去”。

也就是说：

- 它不是通过 `trpc_go.yaml` 注册一个新的 HTTP 服务
- 它更像一个消息桥接器
- 你的业务进程启动后，会主动连接企业微信 AI Bot 的长连接地址

如果你的业务本身已经运行在 tRPC-Go 进程里，也可以继续这么做。
只要在进程里创建好 Runner，再调用 `server.Run(ctx)` 即可。

### 快速开始

最小接入代码如下：

```go
import (
	"context"
	"log"
	"os"
	"strings"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	twecom "git.woa.com/trpc-go/trpc-agent-go/trpc/server/wecom"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

func main() {
	agentInstance := llmagent.New(
		"assistant",
		llmagent.WithModel(openai.New("deepseek-chat")),
		llmagent.WithInstruction("Answer clearly and keep responses concise."),
	)

	r := runner.NewRunner("wecom-agent-demo", agentInstance)
	defer r.Close()

	server, err := twecom.New(r, twecom.Config{
		BotID:        strings.TrimSpace(os.Getenv("WECOM_STREAM_BOT_ID")),
		Secret:       strings.TrimSpace(os.Getenv("WECOM_STREAM_SECRET")),
		BotName:      strings.TrimSpace(os.Getenv("WECOM_BOT_NAME")),
		WebSocketURL: strings.TrimSpace(os.Getenv("WECOM_STREAM_WS_URL")),
		EnableStream: true,
	})
	if err != nil {
		log.Fatalf("failed to create wecom server: %v", err)
	}

	if err := server.Run(context.Background()); err != nil {
		log.Fatalf("wecom server stopped: %v", err)
	}
}
```

不要省略匿名导入
`_ "git.woa.com/trpc-go/trpc-agent-go/trpc"`，
它负责启用内网 tRPC 注入。

如果你想直接跑仓库里的示例：

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

### 环境变量

WeCom AI Bot websocket 这条链路，建议统一使用下面这组名字：

- `WECOM_STREAM_BOT_ID`：企业微信 AI Bot 的 `bot id`
- `WECOM_STREAM_SECRET`：企业微信 AI Bot websocket 长连接密钥
- `WECOM_BOT_NAME`：可选。群聊里会用它移除 `@机器人名`
- `WECOM_STREAM_WS_URL`：可选。覆盖默认长连接地址

如果你跑的是示例，还通常需要模型相关环境变量：

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`

### Config 字段说明

`twecom.Config` 的常用字段如下：

- `BotID`：必填，对应 `WECOM_STREAM_BOT_ID`
- `Secret`：必填，对应 `WECOM_STREAM_SECRET`
- `BotName`：可选，对应 `WECOM_BOT_NAME`
- `WebSocketURL`：可选，对应 `WECOM_STREAM_WS_URL`
- `EnableStream`：是否启用流式回复
- `HeartbeatInterval`：可选，websocket 心跳间隔，默认 `30s`
- `ReconnectDelay`：可选，重连间隔，默认 `3s`

### 默认行为

当前这套实现默认支持这些行为：

- 收到普通文本消息后，转成一次 Runner 请求
- 收到 `enter_chat` 事件时，回复欢迎文案
- 支持 `/help`、`/new`、`/cancel`
- 开启 `BotName` 后，群聊文本里会自动移除 `@机器人`
- 开启 `EnableStream` 后，会把 Runner 的增量文本持续回推给企业微信

### 当前限制

- 当前只实现企业微信 AI Bot websocket 模式
- 当前主要处理文本消息，以及 `mixed` 中的文本片段
- 暂不处理附件、图片、素材上传等能力

### 相关资料

- 示例代码：[examples/wecom](../examples/wecom/)
- 示例说明：[examples/wecom/README.md](../examples/wecom/README.md)
