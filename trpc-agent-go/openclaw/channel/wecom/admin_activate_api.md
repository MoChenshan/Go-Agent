# WeCom Admin Activation API

这份文档描述当前 `trpc-claw` admin 侧已经实现的
WeCom Bot 激活能力。

它解决的问题很窄：

- 页面或外部系统想让用户快速在企微里定位到 Bot
- admin 触发一条固定激活消息到该用户的 Bot 单聊
- 用户随后直接在那个会话里继续聊天

这不是一个通用发消息接口。

如果你要主动发送自定义文本、图片、截图或文件，
不要扩展 activation 文案，直接看
[`outbound_message_api.md`](outbound_message_api.md)。
代码侧应使用 `occhannel.MessageSender`，
admin 侧联调可以使用 `POST /api/channels/wecom/debug/send`。

当前实现只支持：

- WeCom AI Bot
- `connection_mode: websocket`
- 单聊目标
- 固定服务端文案模板

## 1. 接口概览

当前开放两个 admin API：

1. `GET /api/channels/wecom/runtimes`
2. `POST /api/channels/wecom/activate`

它们同时服务两类调用方：

- 外部页面 / 外部系统
- `Channels -> WeCom Runtime` 页面里的手工触发入口

`Channels` 页面里的按钮与表单，复用的就是同一个
`POST /api/channels/wecom/activate`。

## 2. Runtime Discovery

接口：

```text
GET /api/channels/wecom/runtimes
```

用途：

- 列出当前 admin 可见的 WeCom runtime
- 返回对外 opaque 的 `runtime_key`
- 返回 runtime 是否支持 / 当前是否可用
- 返回该 runtime 的默认创建者企微 ID

响应示例：

```json
{
  "runtimes": [
    {
      "runtime_key": "wecom_rt_6249a5d53d3881a3",
      "title": "WeCom Runtime · wecom-ai-websocket",
      "name": "wecom-ai-websocket",
      "bot_mode": "ai",
      "connection_mode": "websocket",
      "chat_policy": "open",
      "default_wecom_user_id": "wineguo",
      "activation": {
        "supported": true,
        "available": true
      }
    }
  ]
}
```

字段说明：

- `runtime_key`
  discovery 返回的 opaque runtime 标识。
  后续调用 `activate` 时原样带回即可。
- `default_wecom_user_id`
  当前 runtime 的默认创建者企微 ID。
  当前实现从 runtime state dir 下的 `.runtime.env`
  读取 `TRPC_CLAW_USER_NAME`。
- `activation.supported`
  是否从能力上支持激活。
  当前要求 `bot_mode=ai` 且
  `connection_mode=websocket`。
- `activation.available`
  当前是否可立即发送。
  一般表示 WeCom websocket 长连接当前在线。

## 3. Activate

接口：

```text
POST /api/channels/wecom/activate
```

支持两种调用形式：

1. JSON 请求，返回 JSON 响应
2. admin 页面表单提交，返回 303 redirect

### 3.1 JSON 请求

如果当前 runtime 已经提供了 `default_wecom_user_id`，
最小请求示例可以是：

```json
{
  "runtime_key": "wecom_rt_6249a5d53d3881a3"
}
```

显式指定用户的请求示例：

```json
{
  "runtime_key": "wecom_rt_6249a5d53d3881a3",
  "wecom_user_id": "T12345678",
  "scene": "api",
  "client_request_id": "f61d8b7c-c17b-4fd4-94d4-8d6b7f89b31f"
}
```

请求字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `runtime_key` | 是 | discovery 返回的 opaque runtime 标识 |
| `wecom_user_id` | 否 | 目标企微 user id；省略时回退到 runtime 默认创建者 RTX |
| `scene` | 否 | 调用场景标识；当前实现会接收该字段，但不会把它用于文案、路由或日志行为 |
| `client_request_id` | 否 | 调用方幂等键；同一请求重复提交时返回缓存结果 |

用户解析顺序：

1. 如果请求显式带了 `wecom_user_id`，优先使用显式值
2. 否则回退到该 runtime 的 `default_wecom_user_id`
3. 如果两者都没有，返回 `invalid_request`

成功响应示例：

```json
{
  "ok": true,
  "runtime_key": "wecom_rt_6249a5d53d3881a3",
  "wecom_user_id": "wineguo",
  "target": "single:wineguo",
  "message_kind": "activation",
  "sent_at": "2026-04-20T21:46:20.175364761+08:00"
}
```

如果命中了 `client_request_id` 幂等缓存，响应里会额外带：

```json
{
  "deduplicated": true
}
```

错误码：

| HTTP 状态码 | `error.code` | 说明 |
| --- | --- | --- |
| `400` | `invalid_request` | `runtime_key` 缺失、请求格式非法、目标 user 无效，或既没有显式用户也没有默认创建者 RTX |
| `404` | `runtime_not_found` | `runtime_key` 无效 |
| `409` | `runtime_not_supported` | 该 runtime 不是 WeCom AI websocket 模式 |
| `409` | `runtime_not_connected` | 当前 WeCom websocket 长连接不可用 |
| `403` | `target_not_allowed` | 目标用户不满足当前 runtime 的 chat policy |
| `429` | `cooldown_active` | 同一 runtime + 用户在冷却窗口内重复触发 |
| `502` | `delivery_failed` | 上游发送失败 |

### 3.2 表单请求

admin 页面使用普通表单 POST 到同一个 endpoint，
表单字段与 JSON 字段一致，额外会带：

- `return_path`

成功时：

- 返回 `303 See Other`
- redirect 回清洗后的 `return_path`
- 如果 `return_path` 非法或为空，则回退到 `/channels`
- 通过 `notice=...` 展示提示

失败时：

- 返回 `303 See Other`
- redirect 回清洗后的 `return_path`
- 如果 `return_path` 非法或为空，则回退到 `/channels`
- 通过 `error=...` 展示错误

## 4. 当前 admin 页面行为

`Channels -> WeCom Runtime` 卡片下已经有手工触发入口。

页面行为是：

1. 输入框 label 为 `WeCom User ID`
2. 如果当前 runtime 读取到了 `default_wecom_user_id`，
   输入框会默认预填该值
3. 默认值可编辑，手工覆盖后优先使用显式输入
4. 如果 runtime 当前不可用，按钮会 disabled

也就是说：

- creator 自己想快速在企微里把 Bot 拉出来时，
  不需要先查自己的企微 ID
- 如果外部系统已经拿到了明确的用户 ID，
  仍然可以显式传入并覆盖默认值

## 5. 触发条件与限制

触发前服务端会做这些校验：

1. `runtime_key` 必须存在
2. runtime 必须支持 activation
3. runtime 当前必须在线可发
4. 目标用户必须通过当前 runtime 的 chat policy
5. 同一 `(runtime_key, resolved_wecom_user_id)` 在
   30 秒冷却窗口内只允许成功一次

当前固定下发文案由服务端生成，不接受调用方自定义文本。

当前固定文案是：

```text
你好。

你已经成功定位到当前企微 Bot。
后续直接在这个会话里给我发消息即可。
```

当前只支持单聊目标，不支持群聊和模板卡片。

## 6. 联调示例

先拿 runtime：

```bash
curl http://127.0.0.1:19789/api/channels/wecom/runtimes
```

然后直接走默认创建者 fallback：

```bash
curl \
  -H 'Content-Type: application/json' \
  -d '{"runtime_key":"wecom_rt_6249a5d53d3881a3","scene":"api"}' \
  http://127.0.0.1:19789/api/channels/wecom/activate
```

如果你要显式指定用户：

```bash
curl \
  -H 'Content-Type: application/json' \
  -d '{"runtime_key":"wecom_rt_6249a5d53d3881a3","wecom_user_id":"T12345678","scene":"api"}' \
  http://127.0.0.1:19789/api/channels/wecom/activate
```

## 7. 兼容性说明

这套接口没有改动现有 WeCom inbound message 链路，
只是给已有的 `SendText()` 能力加了一个 admin 侧入口。

它也没有把 admin 暴露成“万能发消息平台”：

- 接口名是 `activate`，不是 `send`
- 文案固定
- 目标限定单聊
- eligibility 和频控都在服务端统一处理
