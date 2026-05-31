# WeCom Outbound Message API

这份文档说明如何从代码侧主动触发 WeCom 消息发送。

这里说的“主动发送”指的是：当前进程已经和企业微信 AI Bot 建立
WebSocket 长连接，业务代码不等用户新发消息，直接把阶段报告、
截图或文件推到某个 WeCom 单聊 / 群聊。

它和普通回复链路不同：

- 普通回复：用户发消息后，Channel 通过当前请求的 `req_id` 回写。
- 主动发送：业务代码调用 `occhannel.MessageSender`，
  Channel 通过企业微信官方 `aibot_send_msg` 主动推送。
- 模型最终回复里的 `[WECOM_FILE:...]` / `MEDIA:...`
  仍然走 same-chat reply delivery，不需要改成主动发送 API。

## 1. 适用条件

当前 WeCom 主动多模态发送只在下面条件同时满足时可用：

- `bot_mode: ai`
- `connection_mode: websocket`
- 当前 WeCom WebSocket 长连接在线
- 调用方拿到的 Channel 实现了 `occhannel.MessageSender`

如果运行在 notification bot、AI webhook，或 WebSocket 当前断开，
`SendMessage` 会返回错误。

## 2. Target 编码

`SendMessage` 的 `target` 是 WeCom Channel 自己的目标编码。

| 目标 | 格式 | 示例 |
| --- | --- | --- |
| 单聊 | `single:<wecom_user_id>` | `single:T12345678` |
| 单聊（RTX） | `single:<rtx>` | `single:zhangsan` |
| 群聊 | `group:<chatid>` | `group:wrK3L2...` |

注意：

- 单聊目标必须是企业微信能识别的 user id / RTX，不是展示昵称。
- 群聊目标必须是企业微信消息里的 chat id，不是群名称。
- 对当前聊天回投时，优先复用 Channel 已经生成的 delivery target，
  不要自己从展示文本里猜群名或用户名。

## 3. Payload 语义

代码侧使用：

```go
type OutboundMessage struct {
	Text  string
	Files []OutboundFile
}

type OutboundFile struct {
	Path    string
	Name    string
	AsVoice bool
}
```

发送规则：

- `Text` 可选。如果非空，会先按 markdown 主动发送。
- `Files` 可选。多个文件会按 slice 顺序逐个上传、逐个主动发送。
- 如果同时设置 `Text` 和 `Files`，发送顺序是：先文本，再文件。
- `Path` 是 `trpc-claw` 进程可读的本地路径。
- `Name` 可选，用来覆盖上传后展示的文件名。
- 如果 `Name` 没有扩展名，Channel 会沿用源文件扩展名。
- `AsVoice` 只会让兼容的 `.amr` 文件按语音发送，不做转码。

媒体类型由文件扩展名和大小共同决定：

| 类型 | 条件 |
| --- | --- |
| 图片 | `.jpg` / `.jpeg` / `.png` / `.gif`，且不超过 2 MB |
| 语音 | `.amr`，且不超过 2 MB |
| 视频 | `.mp4`，且不超过 10 MB |
| 普通文件 | 其他文件，或超过图片/语音/视频上限但不超过 20 MB |

如果文件为空、不可读或超过最终类型上限，`SendMessage` 返回错误，
不会继续发送该文件。

## 4. 代码示例

下面示例假设调用方已经拿到了一个 `occhannel.Channel`。
这通常发生在 OpenClaw runtime 内部、subagent runtime、
cron / lifecycle hook，或你自己的 channel 编排代码里。

```go
package report

import (
	"context"
	"fmt"

	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	wecomSingleTargetPrefix = "single:"
)

func SendStageReport(
	ctx context.Context,
	ch occhannel.Channel,
	wecomUserID string,
	reportPath string,
	screenshotPath string,
) error {
	sender, ok := ch.(occhannel.MessageSender)
	if !ok {
		return fmt.Errorf(
			"channel %s does not support outbound messages",
			ch.ID(),
		)
	}

	target := wecomSingleTargetPrefix + wecomUserID
	msg := occhannel.OutboundMessage{
		Text: "阶段任务已完成，下面是报告和浏览器截图。",
		Files: []occhannel.OutboundFile{
			{
				Path: reportPath,
				Name: "stage-report.md",
			},
			{
				Path: screenshotPath,
				Name: "browser-screenshot.png",
			},
		},
	}
	return sender.SendMessage(ctx, target, msg)
}
```

如果只发文本，也可以继续用兼容接口：

```go
func SendTextOnly(
	ctx context.Context,
	ch occhannel.Channel,
	wecomUserID string,
	text string,
) error {
	sender, ok := ch.(occhannel.TextSender)
	if !ok {
		return fmt.Errorf(
			"channel %s does not support outbound text",
			ch.ID(),
		)
	}
	return sender.SendText(ctx, wecomSingleTargetPrefix+wecomUserID, text)
}
```

## 5. Admin Debug API

如果只是想联调这条发送链路，不想先写业务代码，
可以在 admin 页面打开：

```text
http://127.0.0.1:19789/channels
```

在 `WeCom Runtime` 卡片里使用 `Send Debug Message`。

同一个能力也有 JSON API，适合 smoke test：

```bash
runtime_key="$(curl -s http://127.0.0.1:19789/api/channels/status \
  | jq -r '.wecom[0].runtime_key')"

curl \
  -H 'Content-Type: application/json' \
  -d '{
    "runtime_key": "'"$runtime_key"'",
    "target": "single:T12345678",
    "text": "阶段任务已完成。",
    "file_path": "/workspace/out/screenshot.png",
    "file_name": "screenshot.png"
  }' \
  http://127.0.0.1:19789/api/channels/wecom/debug/send
```

请求字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `runtime_key` | 是 | admin status / runtime discovery 返回的 runtime key |
| `target` | 是 | `single:<user>` 或 `group:<chatid>` |
| `text` | 否 | 要主动发送的文本 |
| `file_path` | 否 | 要主动发送的本地文件路径 |
| `file_name` | 否 | 上传后的展示文件名 |
| `as_voice` | 否 | 对兼容 `.amr` 文件按语音发送 |

`text` 和 `file_path` 至少需要一个。

这个 debug API 的定位是调试和运维验证。
业务代码里优先直接使用 `occhannel.MessageSender`，
这样可以复用已经拿到的 Channel 实例、context、trace 和错误处理。

## 6. 常见问题

### 可以在 webhook 模式主动发文件吗？

不可以。当前主动多模态发送依赖企业微信 AI Bot WebSocket 的
`aibot_send_msg` 和媒体上传协议。

### `Name` 可以改变媒体类型吗？

可以影响类型判断。例如源文件是 `.png`，`Name` 写成
`screenshot` 时会自动补成 `screenshot.png`，仍按图片发送。
如果显式写成 `screenshot.bin`，就会按普通文件发送。

### 发送文件时会不会把文件复制到工作区？

不会。`SendMessage` 直接读取 `Path` 指向的本地文件并上传到企业微信。
调用方需要保证该路径对当前 `trpc-claw` 进程可读。

### 可以一次发多个文件吗？

可以。`OutboundMessage.Files` 里的文件会按顺序逐个上传和发送。
