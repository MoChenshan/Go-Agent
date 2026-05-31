# OpenClaw WeCom Runbook

这份文档的目标只有一件事：把内网 `openclaw` 的企业微信链路一步一步跑通。

当前 `wecom` Channel 已经支持：

1. `notification` 机器人
2. `ai + connection_mode: webhook`
3. `ai + connection_mode: websocket`

如果你这次的目标是验证“企微长连接 + Gateway 流式回复”，直接走第 3
种，也就是：

- `bot_mode: ai`
- `connection_mode: websocket`
- `enable_stream: true`

建议先用 `mock` 模式验证通路，再切真实模型。这样出了问题时，能更快判断是：

1. 企微后台参数不对
2. WebSocket 长连接没连上
3. Gateway streaming 没打通
4. 模型调用本身没通

## 1. 先决定要测哪条链路

### 路线 A：notification

- 固定 `webhook_url` 回复
- 适合先跑最简单的回调和回复

### 路线 B：ai + webhook

- 企微通过 HTTP 回调把消息推给 OpenClaw
- OpenClaw 通过每次消息里的 `response_url` 回复
- `response_url` 是一次性的

### 路线 C：ai + websocket

- OpenClaw 主动连接企微 WebSocket 长连接
- 企微通过这条连接推送消息
- OpenClaw 通过同一条连接按 `req_id` 回写 `markdown` 或 `stream`
- 企微 `stream` 回包要求同一 `stream.id` 下持续发送完整内容快照，
  不是只发增量后缀
- 这是当前“长连接接入”的主路线

## 2. 跑通前你需要准备什么

### 所有模式都需要

1. 一台能运行 `openclaw` 的机器
2. 一个已经在企业微信后台创建好的机器人

### notification 额外需要

1. `token`
2. `encoding_aes_key`
3. `webhook_url`
4. 一个对外可访问的 HTTP 回调地址

### ai + webhook 额外需要

1. `token`
2. `encoding_aes_key`
3. 一个对外可访问的 HTTP 回调地址

### ai + websocket 额外需要

1. `aibotid`
2. `secret`

建议先准备这些环境变量：

```bash
export WECOM_STREAM_BOT_ID=...
export WECOM_STREAM_SECRET=...
# optional:
# export WECOM_GROUP_SESSION_MODE=shared  # or isolated
# optional:
# export WECOM_STREAM_WS_URL=wss://openws.work.weixin.qq.com
```

如果你要同时保留 webhook 模式，再额外准备：

```bash
export WECOM_TOKEN=...
export WECOM_ENCODING_AES_KEY=...
export WECOM_AI_CALLBACK_PATH=/wecom/ai/callback
```

所以当前最省事的方式，是直接用仓库里的
`openclaw.wecom.ai.websocket.yaml`。

## 3. 先确认二进制里真的带了 wecom 插件

在 `openclaw/` 目录下执行：

```bash
go run -tags openclaw_sqlitevec ./cmd/openclaw inspect plugins
```

你需要在输出里看到：

```text
Channels:
- stdin
- wecom
```

如果这里没有 `wecom`，后面所有配置都不会生效。

## 4. 选对样例配置

仓库里有三份 WeCom 样例：

1. `openclaw/openclaw.wecom.notification.yaml`
2. `openclaw/openclaw.wecom.ai.yaml`
3. `openclaw/openclaw.wecom.ai.websocket.yaml`

如果你现在要测长连接，直接用第 3 份：

```bash
cp openclaw.wecom.ai.websocket.yaml openclaw.local.yaml
```

第一次联调建议只看这几个字段：

1. `bot_mode: "ai"`
2. `connection_mode: "websocket"`
3. `aibotid`
4. `secret`
5. `enable_stream: true`

按企业微信官方长连接文档，WebSocket 建连只需要
`aibotid + secret`。`token` / `encoding_aes_key`
只属于 webhook 回调模式。

当前样例默认就已经按这套写好了。

如果你要额外覆盖 `ws_url` 或 `bot_name`，直接写字面量，例如：

```yaml
ws_url: "wss://openws.work.weixin.qq.com"
bot_name: "AI助手"
```

不要把 `${...}` 只写在注释里。当前配置加载器会先展开占位符，
注释里的 `${NAME}` 也会被当成必填环境变量。

## 5. 先确认依赖是干净的

开始联调前，先确认 `openclaw/go.mod` 里没有临时本地 `replace`。

如果你看到 `=> ../`、`=> ../../` 或任何其他本地路径，先清理掉再测。
WeCom Channel 和 Gateway 相关依赖应直接指向仓库主线对应版本。

## 6. 启动前先加载环境变量

```bash
source ~/.bashrc
```

如果你要直接测真实模型，再确认模型提供方需要的环境变量
已经就绪，例如 `OPENAI_MODEL`、`OPENAI_BASE_URL`、
`OPENAI_API_KEY`。

## 7. 启动服务

### 先跑 mock

先验证长连接能连上、文本能回、流式能回，不要一上来就接真实模型：

```bash
cd openclaw

go run -tags openclaw_sqlitevec ./cmd/openclaw \
  -config ./openclaw.wecom.ai.websocket.yaml \
  -mode mock
```

### 再跑真实模型

等 mock 跑通以后，再切真实模型：

```bash
cd openclaw
source ~/.bashrc

go run -tags openclaw_sqlitevec ./cmd/openclaw \
  -config ./openclaw.wecom.ai.websocket.yaml
```

如果你只想切模型模式，也可以显式加：

```bash
-mode openai
```

如果你现在只是直接 `go run ./cmd/openclaw` 前台联调，
先不要在这个阶段点 `/runtime restart` 或 `/runtime upgrade`。
这两个动作会让当前进程退出，
并依赖外层的 `start.sh`
接住生命周期退出码后再自动拉起新进程。
只有当容器 / supervisor 的外层入口已经换成
会消费 `intent.env` 的 `start.sh`，
才去做这一轮端到端验证。

## 8. 启动后要看什么日志

WebSocket 长连接模式下，最重要的几类日志是：

1. `wecom: using ai bot websocket mode`
2. `OpenClaw gateway is registered to "trpc.openclaw.gateway"`
3. 文件类请求里，应该能看到：
   `wecom: fetched media ...`
   `wecom: materialized ...`
4. 不应该持续刷：
   `wecom websocket: session failed`

如果启动时报 `address already in use`，是 `trpc_go.yaml` 里的 8080
端口被占了，不是 WeCom 配置本身有问题。

## 9. 真实手动测试怎么做

### 第一轮：只测最小文本

1. 保持 `openclaw` 进程在前台
2. 在企业微信里给这个 AI Bot 发一条最简单的文本：
   `hello`
3. 看客户端有没有收到回复
4. 看日志里是否没有报 WebSocket session 错误

如果这一轮通了，说明：

- 长连接接入通了
- 基本的 Gateway 调用通了
- WebSocket 回复协议也通了

### 第二轮：测流式回复

当前样例里已经是：

```yaml
enable_stream: true
```

这时再发一条容易产生增量输出的问题，比如：

```text
请分三行介绍一下你自己，每一行都短一点
```

你要观察的是：

1. 企微客户端里有没有打字机式的逐步更新
2. 不是只等很久后一次性出完整结果
3. 在正文出来之前，是否先看到很短的阶段提示，例如：
   `正在读取文件` / `正在提取表格内容` / `正在整理答案`

如果这里只收到最终一条，不代表 WeCom WebSocket 不通，更可能是：

- 底层 Gateway 没走流式
- 或模型本身没有产生明显的增量输出

如果你看到首条内容正常出现，但后续只覆盖成半句、残句或者停在前半段，
优先检查 WeCom 回包是不是把 `stream.content` 当成 delta 发了。
企业微信官方协议要求这里发送的是完整快照。

### 第三轮：测图片输入

直接给机器人发一张图片，再附一句文字，例如：

```text
这张图里是什么？
```

预期链路是：

1. WeCom 消息进入 Channel
2. WeCom Channel 下载图片
3. 转成 Gateway `ContentParts`
4. Gateway 返回流式或普通回复
5. 回复通过 WebSocket 回写到企微

如果这一步失败，优先看日志里有没有：

```text
wecom: sending multimodal request with
```

有这句，说明图片已经被转成 multimodal 请求了。

### 第四轮：测文件输入

给机器人发一个小文件，例如 PDF 或文本文件，再问：

```text
请总结这个文件
```

预期和图片一样，也是先走文件下载 / 解密 / ContentParts，再走
Gateway。

如果是企微长连接里的图片 / 文件，当前实现会优先使用消息体里自带的
`aeskey` 解密后再送下游；这和 webhook 模式的
`encoding_aes_key` 不是一回事。

### 第五轮：测运行时控制

前提：

- 当前实例是由平台 `start.sh` 拉起的
- 这个 `start.sh` 会消费
  `state_dir/runtime/lifecycle/intent.env`
- 你自己在 `runtime_admin_users` 里，或者
  `runtime_admin_policy: inherit` 且你本来就有聊天权限

建议按这个顺序测：

1. 先发 `/runtime status`，确认当前版本和空闲状态能正常返回
2. 再发 `/help runtime` 或 `/runtime help`，
   确认详细帮助里能看到 restart / upgrade / versions / changelog
3. 再发 `/runtime changelog`，确认变更摘要能正常拉到
4. 然后发 `/runtime`，确认控制卡片能打开
5. 最后再测 `/runtime restart` 或 `/runtime upgrade`

预期行为：

- 无损动作会先 drain，等待已接收请求处理完
- drain 期间新的普通请求会被拒收，但 `/status`、`/runtime status`
  和卡片操作仍然可用
- 强制动作会直接取消当前任务
- 升级 latest 或指定版本后，`start.sh` 会重新拉起新的 `trpc-claw`
  子进程
- `/status` 和状态卡片会额外展示当前运行版本
- 如果当前是 `bot_mode: ai` +
  `connection_mode: websocket`，
  新进程在长连接建连成功后会复用持久化下来的 `response_url`，
  再补一条“已完成”的消息

### 第六轮：测 admin 激活入口和 API

如果你这次除了聊天链路，还要验证“页面点一下，把 Bot 主动拉到企微会话
里”，再补这一轮。

先看 `Channels` 页面：

```text
http://127.0.0.1:19789/channels
```

你应该能在 `WeCom Runtime` 卡片下看到：

1. `WeCom User ID` 输入框
2. `Send Activation` 按钮

如果当前 runtime 的 `.runtime.env` 里有
`TRPC_CLAW_USER_NAME`，
输入框会默认预填这个创建者 RTX，但仍然可以手改。

再看 discovery API：

```bash
curl http://127.0.0.1:19789/api/channels/wecom/runtimes
```

你应该能看到类似响应：

```json
{
  "runtimes": [
    {
      "runtime_key": "wecom_rt_6249a5d53d3881a3",
      "default_wecom_user_id": "wineguo",
      "activation": {
        "supported": true,
        "available": true
      }
    }
  ]
}
```

然后测 activate API。

完整字段约定和错误码说明，直接看
[`admin_activate_api.md`](admin_activate_api.md)。

如果你只想验证“创建者自己点入口”的默认链路，
可以只传 `runtime_key`：

```bash
curl \
  -H 'Content-Type: application/json' \
  -d '{"runtime_key":"wecom_rt_6249a5d53d3881a3","scene":"api"}' \
  http://127.0.0.1:19789/api/channels/wecom/activate
```

当前实现会按下面顺序解析目标用户：

1. 优先用请求里的 `wecom_user_id`
2. 如果没传，则回退到 runtime 的 `default_wecom_user_id`
3. 如果两者都没有，返回 `invalid_request`

如果你想显式指定目标用户，再传：

```bash
curl \
  -H 'Content-Type: application/json' \
  -d '{"runtime_key":"wecom_rt_6249a5d53d3881a3","wecom_user_id":"T12345678","scene":"api"}' \
  http://127.0.0.1:19789/api/channels/wecom/activate
```

预期成功响应类似：

```json
{
  "ok": true,
  "runtime_key": "wecom_rt_6249a5d53d3881a3",
  "wecom_user_id": "wineguo",
  "target": "single:wineguo",
  "message_kind": "activation"
}
```

如果这里失败，优先检查：

1. runtime 是否真的是 `bot_mode=ai` +
   `connection_mode=websocket`
2. 当前 websocket 长连接是否真的在线
3. 目标用户是否满足当前 runtime 的 `chat_policy`
4. 同一个用户是不是刚刚已经触发过，命中了 30 秒冷却

### 第七轮：测主动文本和附件发送

如果你这次要验证“长任务阶段报告 + 相关制品主动发给用户”，
再补这一轮。

完整代码侧触发方式看
[`outbound_message_api.md`](outbound_message_api.md)。
这里先用 admin debug API 做 smoke test。

先确认 `Channels` 页面里能看到：

1. `Debug Target`
2. `Text`
3. `Local File Path`
4. `Send Debug Message`

也可以直接走 JSON API：

```bash
runtime_key="$(curl -s http://127.0.0.1:19789/api/channels/status \
  | jq -r '.wecom[0].runtime_key')"

curl \
  -H 'Content-Type: application/json' \
  -d '{
    "runtime_key": "'"$runtime_key"'",
    "target": "single:T12345678",
    "text": "WeCom outbound media smoke.",
    "file_path": "/workspace/out/wecom-debug-smoke.txt",
    "file_name": "wecom-debug-smoke.txt"
  }' \
  http://127.0.0.1:19789/api/channels/wecom/debug/send
```

预期成功响应类似：

```json
{
  "ok": true,
  "runtime_key": "wecom_rt_6249a5d53d3881a3",
  "target": "single:T12345678",
  "message_kind": "debug_send"
}
```

然后看日志：

```bash
grep -E "upload outbound file|upload init|upload finish|msgtype=file" \
  /app/start.log
```

你应该能看到：

- `msgtype=markdown` 的主动文本推送
- `upload init` / `upload finish`
- `msgtype=file` 或 `msgtype=image` 的主动媒体推送

如果这里失败，优先检查：

1. `runtime_key` 是否来自当前运行中的 WeCom runtime
2. `target` 是否是 `single:<user>` 或 `group:<chatid>`
3. `file_path` 是否是当前 `trpc-claw` 进程可读路径
4. 当前 websocket 长连接是否在线

## 10. 现在到底测到了什么

当前仓库已经具备这几层验证：

1. 图片 / 文件输入解析有单测
2. Gateway streaming 到 WeCom reply 有单测
3. WebSocket 入站订阅 / 回包有单测
4. 新增样例配置、runbook、安装模板已经补齐

但要注意一件事：

“真实企微环境里的 图片/文件输入 + WebSocket 流式回写” 这种组合场景，
最终还是要靠你现在这轮手动联调来做最后确认。

## 11. 最常见的问题怎么查

### 文本能回，图片 / 文件不回

优先看日志里有没有：

```text
wecom: sending multimodal request with
```

如果没有，说明消息还没进到 Gateway multimodal 请求。
如果有，再看下游 Gateway / 模型是否支持对应输入。

### 文本能回，但不是流式

先确认：

1. `enable_stream: true`
2. 内网 `openclaw/go.mod` 确实 replace 到了外网 GitHub 版
3. 外网 GitHub 版就是你刚改的 streaming gateway 分支

### 启动后反复断开重连

重点看：

```text
wecom websocket: session failed
```

如果持续出现，优先排查：

1. `aibotid`
2. `secret`
3. 默认 `ws_url`
4. 出网 / DNS / TLS 问题

### webhook 模式的回调 URL 校验失败

这只影响 `connection_mode: webhook`，不影响 `websocket`。

通常是：

1. `token` 不一致
2. `encoding_aes_key` 不一致
3. `callback_path` 不一致
4. 反向代理改写了 query 参数

## 12. 一条最稳的联调路径

如果你只想照着做，不想自己决定顺序，就按下面这条路径：

1. `source ~/.bashrc`
2. `cd openclaw`
3. `go run -tags openclaw_sqlitevec ./cmd/openclaw inspect plugins`
4. `go run -tags openclaw_sqlitevec ./cmd/openclaw -config ./openclaw.wecom.ai.websocket.yaml -mode mock`
5. 在企微里先发 `hello`
6. 再发一条短问题，观察是否流式更新
7. 再发一张图片
8. 再发一个小文件
9. mock 全通后，确认真实模型所需环境变量已经设置完成
10. 用同一份配置再起一次真实模型版本
