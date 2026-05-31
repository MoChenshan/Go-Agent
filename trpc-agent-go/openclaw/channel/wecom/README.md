# WeCom Channel Plugin

企业微信（WeCom）Channel 插件，实现 openclaw `Channel` 接口，支持两种企业微信机器人类型。

如果你现在的目标是把内网 `openclaw` 和企业微信一步一步联调通，优先看
[`runbook.md`](runbook.md)。
如果你要对接 admin 侧“激活 Bot”能力，或者想复用
`Channels -> WeCom Runtime` 里的手工触发入口，优先看
[`admin_activate_api.md`](admin_activate_api.md)。
如果你要从代码里主动发送阶段报告、浏览器截图、压缩包等文本和附件，
优先看 [`outbound_message_api.md`](outbound_message_api.md)。
如果你只想先拿一份能改的配置，直接从
[`../../openclaw.wecom.notification.yaml`](../../openclaw.wecom.notification.yaml)
、[`../../openclaw.wecom.ai.yaml`](../../openclaw.wecom.ai.yaml)
或
[`../../openclaw.wecom.ai.websocket.yaml`](../../openclaw.wecom.ai.websocket.yaml)
开始。

推荐做法：把 Token / AES Key / Webhook 这类敏感值放进
`~/.bashrc`，用 `WECOM_*` 环境变量注入模板，而不是直接写死在
YAML 里。

当前 admin 侧已经额外提供了一组很窄的 WeCom activation 能力：

- `GET /api/channels/wecom/runtimes`
- `POST /api/channels/wecom/activate`

它的定位是“帮助用户在企微里快速定位到 Bot”，
不是通用 outbound send API。

如果要主动发自定义文本、图片或文件，应走
`occhannel.MessageSender` / `occhannel.OutboundMessage`。
`Channels -> WeCom Runtime` 里的 Debug Send 表单只用于联调这条链路。

## 🤖 支持的机器人类型

本插件支持两种企业微信机器人模式，通过 `bot_mode` 配置项选择：

### 1️⃣ 消息通知机器人 (Notification Bot)

**官方文档**: [消息通知机器人配置说明](https://developer.work.weixin.qq.com/document/path/99110)

**适用场景**:
- ✅ 系统告警通知
- ✅ 定时消息推送
- ✅ 简单的问答机器人
- ⚠️ 接收群消息需要白名单企业

**特点**:
- 使用固定的 `webhook_url` 发送消息
- 简单稳定，长期有效
- 接收消息使用标准加密回调协议

### 2️⃣ 智能机器人 (AI Bot)

**官方文档**: [智能机器人接口说明](https://developer.work.weixin.qq.com/document/path/100719)

**适用场景**:
- ✅ AI 客服对话
- ✅ 大模型集成
- ✅ 流式消息回复（打字机效果）
- ✅ 完整的双向交互

**特点**:
- `connection_mode: webhook` 时使用动态 `response_url` 回复消息
- `connection_mode: websocket` 时通过长连接 `req_id` 回写消息
- 支持流式消息推送
- 专为 AI Agent 设计

### 3️⃣ AI Bot 接入模式

AI Bot 现在支持两种接入方式，通过 `connection_mode` 选择：

- `webhook`（默认）: 企业微信通过 HTTP 回调把消息推给 OpenClaw
- `websocket`: OpenClaw 主动连接企业微信 WebSocket 长连接，持续接收消息

重要区分：

- 企业微信 WebSocket 解决的是**消息接入（ingress）**
- Gateway streaming 解决的是**增量回复（egress）**
- WebSocket 长连接模式下，WeCom Channel 会持续重写同一条处理中消息：
  用户可见的中间 comment / thought 会累计保留，
  纯心跳状态只显示
  `.` / `..` / `...`
  这类 pulse，
  不再靠“PDF / 视频 / 文档任务”这类关键词在 channel 层硬编码猜流程
- 企业微信 `stream` 回包的 `content` 是同一 `stream.id` 下的
  **完整内容快照**，不是 delta 片段
- 如果你希望 `enable_stream: true` 真正输出打字机效果，底层
  Gateway 还需要支持 `StreamMessage` / 流式事件；如果底层只有普通
  `SendMessage`，Channel 会自动降级成一次性最终回复
- `aggregate_window` 现在表示**最大等待窗口**。当同一批次里已经同时拿到
  文本和附件时，Channel 会进入更短的 settle 期并提前发起请求，同时对
  附件做预抓取，减少固定空等
- WebSocket 长连接路径现在还会额外做几件事来提升体感：
  首条纯文本会尊重配置的 `aggregate_window`，
  避免“文字先到、文件后到”被过早拆单；
  同一会话排队时不再额外抢先开启第二条 stream，
  降低企业微信 `6000 data version conflict` 的概率；
  附件预抓取会把下载结果缓存到 `state_dir/wecom/media_cache/`，
  排队较久时仍能继续读取已经拿到的文件；
  超过安全时长的长任务 stream 会自动退化成 markdown 终态回复，
  降低企业微信 `stream expired` 对最终结果可见性的影响；
  附件物化前会先给出读取提示，`run.progress` 会显示当前步骤，
  用户还可以随时发送 `/status` 查看当前阶段和最近输出

---

## 📝 配置示例

### 消息通知机器人配置

```yaml
channels:
  - type: "wecom"
    name: "system-alerts"
    config:
      # === Bot Mode ===
      bot_mode: "notification"  # 🔥 必填：选择机器人类型

      # === Common Configuration ===
      corp_id: "${WECOM_CORP_ID}"
      agent_id: "${WECOM_AGENT_ID}"
      token: "${WECOM_TOKEN}"
      encoding_aes_key: "${WECOM_ENCODING_AES_KEY}"

      # === Notification Bot Specific ===
      webhook_url: "${WECOM_WEBHOOK_URL}"

      # === Callback Configuration ===
      callback_path: "${WECOM_NOTIFICATION_CALLBACK_PATH}"
      # callback_port: 8080  # 可选，默认使用共享 mux

      # === Permission Control ===
      chat_policy: "open"  # open | disabled | allowlist
      allow_users:
        - "user001"

      # === Custom Messages ===
      bot_name: "${WECOM_BOT_NAME}"
```

### 智能机器人配置（Webhook 回调）

```yaml
channels:
  - type: "wecom"
    name: "ai-assistant"
    config:
      # === Bot Mode ===
      bot_mode: "ai"  # 🔥 必填：选择机器人类型

      # === Common Configuration ===
      corp_id: "${WECOM_CORP_ID}"
      agent_id: "${WECOM_AGENT_ID}"
      token: "${WECOM_TOKEN}"
      encoding_aes_key: "${WECOM_ENCODING_AES_KEY}"

      # === AI Bot Specific ===
      connection_mode: "webhook"  # 可选，默认 webhook
      aibotid: "${WECOM_AIBOTID}" # webhook 模式可选
      enable_stream: true         # 启用流式回复（可选）
      enter_chat_welcome: true    # 回复原生欢迎语卡片和快捷问题
      reply_prefix:
        enabled: true
        fields: ["persona", "commands", "hint", "links"]
        hint: "直接发问题、图片或文件给我"
        links:
          - label: "Web IDE"
            url: "https://ide.example.com/session/${USER}"
        commands: ["/help", "/persona", "/status"]

      # === Callback Configuration ===
      callback_path: "${WECOM_AI_CALLBACK_PATH}"

      # === Permission Control ===
      chat_policy: "allowlist"
      allow_users:
        - "user001"
        - "user002"
      runtime_admin_policy: "allowlist"  # inherit | allowlist
      runtime_admin_users:
        - "ops001"

      # === Multimodal Configuration ===
      # 图片/文件 URL 嵌入开关（控制如何将多媒体消息传递给 Agent）
      embed_image_url: false  # 图片：false=内容传递, true=嵌入文本
      embed_file_url: false   # 文件：false=内容传递, true=嵌入文本

      # === Session & Aggregation ===
      # session_timeout: "10m"  # 可选：开启超时自动分会话
      aggregate_window: "2s"    # 消息聚合窗口（默认 2s，设为 0 禁用）

      # === Custom Messages ===
      bot_name: "${WECOM_BOT_NAME}"
```

### 智能机器人配置（WebSocket 长连接）

```yaml
channels:
  - type: "wecom"
    name: "ai-assistant-ws"
    config:
      # === Bot Mode ===
      bot_mode: "ai"

      # === Common Configuration ===
      corp_id: "${WECOM_CORP_ID}"
      agent_id: "${WECOM_AGENT_ID}"

      # === AI Bot WebSocket Specific ===
      connection_mode: "websocket"   # webhook | websocket
      aibotid: "${WECOM_STREAM_BOT_ID}" # websocket 模式必填
      secret: "${WECOM_STREAM_SECRET}"  # websocket 模式必填
      enable_stream: true
      enter_chat_welcome: true    # 走原生欢迎语协议，不占普通回复链路
      reply_prefix:
        enabled: true
        fields: ["persona", "commands", "hint", "links"]
        hint: "直接发问题、图片或文件给我"
        links:
          - label: "Web IDE"
            url: "https://ide.example.com/session/${USER}"
        commands: ["/help", "/persona", "/status"]

      # === Optional WebSocket Tuning ===
      # ws_url: "wss://openws.work.weixin.qq.com"
      heartbeat_interval: "30s"

      # === Permission Control ===
      chat_policy: "allowlist"
      allow_users:
        - "user001"
        - "user002"
      runtime_admin_policy: "allowlist"  # inherit | allowlist
      runtime_admin_users:
        - "ops001"

      # === Multimodal Configuration ===
      embed_image_url: false
      embed_file_url: false

      # === Session & Aggregation ===
      # session_timeout: "10m" # Optional: enable auto-split after inactivity
      aggregate_window: "2s"

      # === Custom Messages ===
      bot_name: "${WECOM_BOT_NAME}"
```

推荐先写进 `~/.bashrc`：

```bash
# 常用模型环境变量：
export OPENAI_MODEL='gpt-5.2'
export OPENAI_API_KEY='replace-with-your-api-key'
export OPENAI_BASE_URL='https://your-openai-compatible-endpoint/v1'

# webhook 模式需要：
export WECOM_TOKEN='replace-with-your-token'
export WECOM_ENCODING_AES_KEY='replace-with-your-43-char-key'
export WECOM_AI_CALLBACK_PATH='/wecom/ai/callback'

# notification 模式再补这些：
# export WECOM_NOTIFICATION_CALLBACK_PATH='/wecom/notification/callback'
# export WECOM_WEBHOOK_URL='https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...'

# AI Bot websocket 模式只需要这些：
# export WECOM_STREAM_BOT_ID='replace-with-your-aibotid'
# export WECOM_STREAM_SECRET='replace-with-your-aibot-secret'
# export WECOM_GROUP_SESSION_MODE='shared'  # or isolated
# export WECOM_STREAM_WS_URL='wss://openws.work.weixin.qq.com'
```

写完并 `source ~/.bashrc` 之后，如果你想直接确认当前企微实例
“到底有没有看到这个变量”，现在也可以直接问：

```text
你能读到 OPENAI_API_KEY 吗
你能读到 TAIHU_PAT_TOKEN 吗
```

默认模板会自动启用 `env_probe`。
它会安全检查当前 `trpc-claw` 进程和几个受信任 env 来源，
只返回存在性和来源，不会回显变量值。
如果它在这些来源里检测到简单静态声明，
还会把变量补进当前 `trpc-claw` 进程环境，
让后续工具调用可以直接使用。

WebSocket 长连接模式按企业微信官方文档只要求
`aibotid + secret`。`WECOM_TOKEN` /
`WECOM_ENCODING_AES_KEY` 只在 webhook 模式下使用。
分发版默认 profile 里的 `model.name` / `model.base_url`
会直接写成 `OPENAI_MODEL` / `OPENAI_BASE_URL`
环境变量引用；
启动时会先展开它们，`.runtime.env`
和你当前 shell 里 `source`
好的环境变量都会生效；没配就会直接启动失败。
默认分发 profile 里的 `group_session_mode`
已经写成 `${WECOM_GROUP_SESSION_MODE}`。
如果这个环境变量没设置，配置预处理会保留空值，
然后 wecom channel 会按默认值回退到 shared；
需要群内按用户隔离时，
只要额外 `export WECOM_GROUP_SESSION_MODE='isolated'` 即可。

---

## 🔧 配置字段说明

### 必填字段（所有模式）

| 字段 | 说明 |
|------|------|
| `bot_mode` | **机器人类型**，必填<br>• `notification` - 消息通知机器人<br>• `ai` - 智能机器人 |

### 必填字段（webhook 模式）

| 字段 | 说明 |
|------|------|
| `token` | 回调验证 Token（在企业微信后台配置） |
| `encoding_aes_key` | 回调加密密钥（43 字符，企业微信后台生成） |

如果模板里写的是 `${WECOM_TOKEN}` 这种形式，而环境变量没设置，
OpenClaw 启动会直接报错，这是预期行为。

### 消息通知机器人专用字段

| 字段 | 说明 |
|------|------|
| `webhook_url` | **必填**，群机器人 Webhook 地址<br>格式: `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx` |

### 智能机器人专用字段

| 字段 | 说明 |
|------|------|
| `connection_mode` | AI Bot 接入方式。`webhook`（默认）表示走 HTTP 回调；`websocket` 表示由 OpenClaw 主动连接企微长连接。 |
| `aibotid` | 智能机器人 ID。`webhook` 模式可选（通常可从回调消息中提取）；`websocket` 模式必填。 |
| `secret` | AI Bot WebSocket 长连接密钥。仅 `connection_mode: websocket` 时必填。 |
| `ws_url` | AI Bot WebSocket 地址。可选，默认 `wss://openws.work.weixin.qq.com`。 |
| `heartbeat_interval` | WebSocket 心跳间隔。可选，默认 `30s`。 |
| `enable_stream` | 可选，是否启用流式回复（默认 false）。只有底层 Gateway 支持流式接口时才会真正增量输出，否则自动降级为最终回复。开启后，Channel 还会把 Gateway 的阶段进度事件转成简短的处理中提示。 |
| `enter_chat_welcome` | 可选，收到 `enter_chat` 事件时，按企微原生欢迎语协议回复欢迎卡片和快捷问题。默认 `true`。Webhook 模式走被动回复，WebSocket 模式走 `aibot_respond_welcome_msg`。 |
| `reply_prefix` | 可选，统一回复前缀配置。默认关闭。开启后会以单行 `> ... | ... | ...` 形式挂在回复开头；流式回复也保持在同一个消息气泡里。 |
| `persona_dir` | 可选，自定义人格文件目录。未配置时默认落到 `state_dir/wecom/personas`。 |

`connection_mode: websocket` 时不需要配置
`token` / `encoding_aes_key` 才能建连。

同一个 `aibotid` 在同一时刻只应由一个 WebSocket 进程消费。
如果你同时启动了多个实例，新的实例会直接报错退出，避免消息被不同
进程随机分流。

### 可选字段（所有模式）

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `corp_id` | 空 | 企业 ID（可选） |
| `agent_id` | 空 | 应用 AgentID（可选） |
| `callback_port` | `0` | HTTP 回调监听端口<br>• 0: 使用共享 mux（推荐）<br>• >0: 独立端口<br>仅 `connection_mode: webhook` 使用 |
| `callback_path` | `/wecom/callback` | HTTP 回调路径，仅 `connection_mode: webhook` 使用 |
| `bot_name` | 空 | 机器人名称（用于去除 @提及） |
| `chat_policy` | `open` | 权限策略 |
| `allow_users` | `[]` | 白名单用户列表 |
| `group_session_mode` | `shared` | 群聊会话范围。`shared` 表示整个群共享一段历史；`isolated` 表示群里每个用户单独一段历史。 |
| `runtime_admin_policy` | `inherit` | 运行时控制权限策略。`inherit` 表示沿用 `chat_policy`；`allowlist` 表示只有 `runtime_admin_users` 可以触发升级 / 重启。 |
| `runtime_admin_users` | `[]` | 运行时控制管理员列表，仅 `runtime_admin_policy: allowlist` 时生效。 |

### `reply_prefix` 配置

`reply_prefix` 用来控制每条回复前固定展示的上下文信息。它默认关闭；
显式开启后会这样生效：

- 一次性最终回复
- WebSocket 流式回复会把同一行前缀和正文放在同一个消息气泡里

显式开启后的默认字段顺序如下：

```yaml
reply_prefix:
  enabled: true
  fields:
    - persona
    - context
    - commands
    - hint
    - links
```

支持的字段：

| 字段值 | 说明 |
|--------|------|
| `assistant` | 显示助手名称，例如 `🤖 Streambot2` |
| `persona` | 显示当前人格，例如 `🎭 人格：伙伴` |
| `context` | 显示最近一次已知的上下文占用，例如 `🧠 上下文：12.3K / 200K (6.2%)` |
| `workspace` | 显示紧凑版工作区标签；默认不展示路径，也不展示 Git 根路径 |
| `links` | 显示自定义链接列表，例如 `🔗 Web IDE: https://...` |
| `commands` | 显示快捷命令入口，例如 `⚡ 常用：/help /persona /status` |
| `hint` | 显示一段更友好的提示语，例如 `💬 直接发问题、图片或文件给我` |

字段可以按你的展示习惯重排。例如只想保留人格、链接和
快捷命令：

```yaml
reply_prefix:
  fields: ["persona", "context", "links", "commands"]
```

如果你想开启固定前缀，可以显式写：

```yaml
reply_prefix:
  enabled: true
```

常见的 Web IDE / 快捷命令配置示例：

```yaml
reply_prefix:
  enabled: true
  fields: ["persona", "context", "commands", "hint", "links"]
  hint: "直接发问题、图片或文件给我"
  links:
    - label: "Web IDE"
      url: "https://ide.example.com/session/${USER}"
    - label: "运行日志"
      url: "https://logs.example.com/openclaw"
  commands:
    - "/new"
    - "/help"
    - "/persona"
    - "/status"
    - "/workspace off"
```

如果你确实想把工作区状态也挂到前缀里，建议显式加上：

```yaml
reply_prefix:
  fields: ["persona", "workspace", "commands", "hint", "links"]
```

这时前缀会显示紧凑标签，例如 `📂 默认工作区` 或 `📂 openclaw`，
不会默认把完整路径和 Git 根路径刷到每条回复里。最终效果类似：

```text
> 🎭 人格：伙伴 | 🧠 上下文：12.3K / 200K (6.2%) | ⚡ 常用：/help /persona /status | 💬 直接发问题、图片或文件给我
```

### 多模态配置字段（AI 机器人推荐）

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `embed_image_url` | `false` | **图片 URL 处理方式**<br>• `false`（默认）: URL 通过 ContentParts 传递，由 wecom channel 下载并转成图片字节<br>• `true`: URL 嵌入文本 `[image_url:URL]`，Agent 通过工具下载 |
| `embed_file_url` | `false` | **文件 URL 处理方式**<br>• `false`（默认）: URL 通过 ContentParts 传递，由 wecom channel 下载；企微加密文件会先解密<br>• `true`: URL 嵌入文本 `[file_url:URL]`，Agent 通过工具下载 |

> **推荐配置**：`embed_image_url: false`（图片由 wecom
> channel 下载后直接传给模型），`embed_file_url: false`
> （PDF / Excel 等文件优先走文件内容传递）

---

## 🔐 企业微信后台配置

### 消息通知机器人

1. 创建机器人：[消息通知机器人配置说明](https://developer.work.weixin.qq.com/document/path/99110)
2. 配置回调 URL：**必须添加 `robot_callback_format=json` 参数**

```
http://您的域名/wecom/notification/callback?robot_callback_format=json
```

3. 获取 `token` 和 `encoding_aes_key`
4. 获取群机器人 `webhook_url`

### 智能机器人

1. 创建智能机器人：[智能机器人接口说明](https://developer.work.weixin.qq.com/document/path/100719)
2. 根据接入方式选择：

   - **Webhook 模式**：配置 HTTP 回调 URL（自动使用 JSON 格式）
   - **WebSocket 模式**：按[流式消息回复机制 / 长连接接入](https://developer.work.weixin.qq.com/document/path/101463)配置 `aibotid` 和 `secret`

3. Webhook 模式的回调 URL：

```
http://您的域名/wecom/ai/callback
```

4. Webhook 模式需要获取 `token` 和 `encoding_aes_key`
5. WebSocket 模式只需要 `aibotid` 和 `secret`

---

## 🚀 工作原理

### 消息通知机器人模式

```
企业微信群 ──POST──▶ 加密回调 ──▶ 解密 ──▶ Gateway ──▶ 固定 webhook_url ──▶ 回复
```

### 智能机器人模式

```
用户 @机器人 ──▶ 企业微信 ──加密回调──▶ 解密 ──▶ 提取 response_url
                                              │
                                              ▼
                                          Gateway
                                              │
                                              ▼
                                   动态 response_url ──▶ 回复（可流式）
```

`enter_chat` 例外：欢迎语不走 `response_url`。Webhook 模式会在
回调响应里直接返回加密欢迎语；WebSocket 模式会使用
`aibot_respond_welcome_msg`。

### 智能机器人模式（WebSocket 长连接）

```
OpenClaw ──WebSocket subscribe──▶ 企业微信长连接网关
   ▲                                   │
   │                                   ▼
   └───────────── req_id + 回调消息 ─── 用户 @机器人

OpenClaw ──▶ Gateway ──▶ WebSocket 回复（welcome / markdown /
stream / media）
```

长连接模式下，建连帧、心跳帧和回包都按官方 `cmd + headers.req_id`
协议发送。

---

## 📊 两种机器人模式详细对比

| 对比维度 | 消息通知机器人 (Notification) | 智能机器人 (AI Bot) |
|----------|------------------------------|---------------------|
| **官方文档** | [99110](https://developer.work.weixin.qq.com/document/path/99110) / [101031](https://developer.work.weixin.qq.com/document/path/101031) | [100719](https://developer.work.weixin.qq.com/document/path/100719) / [101138](https://developer.work.weixin.qq.com/document/path/101138) |
| **主要用途** | 系统告警、定时推送、简单问答 | AI 对话、大模型集成、客服场景 |
| **接入方式** | HTTP 回调 | HTTP 回调 或 WebSocket 长连接 |
| **发送方式** | 固定 `webhook_url` | `response_url`（webhook）或 WebSocket `req_id`（websocket） |
| **URL 有效期** | 永久有效 | `response_url` 在 webhook 模式下 1 小时内有效；websocket 模式不适用 |
| **URL 调用次数** | 无限制 | `response_url` 在 webhook 模式下**仅能调用一次**；websocket 模式不适用 |
| **消息格式** | `markdown_v2` | `markdown` |
| **是否需要 chatid** | ✅ 需要 | ❌ 不需要 |
| **流式回复** | ❌ 不支持 | ✅ 支持（需 `enable_stream: true`，且 Gateway 具备流式能力） |
| **接收群消息** | ⚠️ 需白名单企业 | ✅ 直接支持 |
| **回调参数** | `robot_callback_format=json` | 自动 JSON 格式 |

### ⚠️ 关键差异说明

1. **response_url 仅能调用一次**
   - AI 机器人的 `response_url` 是一次性的，调用后失效
   - 如果发送"思考中..."提示，会消耗掉唯一的回复机会
   - 本插件在 AI 模式下自动跳过 thinking hint

2. **消息格式不同**
   - Notification Bot: `{"msgtype": "markdown_v2", "markdown_v2": {...}, "chatid": "xxx"}`
   - AI Bot `response_url`: `{"msgtype": "markdown", "markdown": {"content": "..."}}`
   - AI Bot `websocket`: 除 `markdown` / `stream` 外，还支持
     `file` / `image` / `voice` / `video`

3. **chatid 字段**
   - Notification Bot 必须携带 `chatid` 指定目标群
   - AI Bot 不需要也不能携带 `chatid`，URL 已绑定会话

---

## 📦 支持的消息类型

### 接收消息类型对比

| 消息类型 | Notification Bot | AI Bot | 代码支持 | 备注 |
|----------|-----------------|--------|----------|------|
| `text` | ✅ | ✅ | ✅ 已实现 | 提取 `content` 字段 |
| `image` | ✅ | ✅ | ✅ 已实现 | 转换为 `[image:URL]` |
| `voice` | ⚠️ 仅 media_id | ✅ 自动转文字 | ✅ 已实现 | AI Bot 返回 `content` 字段（语音转文字） |
| `file` | ❌ | ✅ | ✅ 已实现 | 仅单聊回调；返回加密 URL（5分钟有效） |
| `mixed` | ❌ | ✅ | ✅ 已实现 | 图文混合消息 |
| `stream` | ❌ | ✅ | ✅ 已实现 | 流式消息类型标识 |
| `quote` | ❌ | ✅ | ✅ 已实现 | 引用回复消息 |
| `video` | ✅ | ✅ | ✅ 已实现 | 转换为 `[video:media_id]` |
| `location` | ✅ | ✅ | ✅ 已实现 | 转换为 `[location:...]` |
| `link` | ✅ | ✅ | ✅ 已实现 | 转换为 `[link:title,url]` |
| `event` | ✅ | ✅ | ✅ 已实现 | `enter_chat` 默认回欢迎提示，其余事件跳过 |

### AI Bot 语音消息特殊处理

AI 机器人的语音消息会**自动转成文字**，通过 `voice.content` 字段返回：

```json
{
  "msgtype": "voice",
  "voice": {
    "media_id": "xxx",
    "content": "用户语音转换后的文字内容"
  }
}
```

本插件优先读取 `content` 字段，如果为空则降级到 `media_id`。

### AI Bot 文件消息处理

文件消息返回加密 URL，**有效期仅 5 分钟**：

```json
{
  "msgtype": "file",
  "file": {
    "url": "https://xxx.weixin.qq.com/encrypted_file_url"
  }
}
```

注意：

- 企业微信官方当前只在**单聊**中回调本地文件消息
- 群聊里直接发送文件，不会产生 `msgtype=file` 回调
- 群聊如需让机器人读取文件，推荐**引用该文件消息再提问**
  当前实现会把引用里的 `quote.file` / `quote.image` 传给 Gateway

#### 文件/图片 URL 处理模式

通过 `embed_image_url` 和 `embed_file_url` 配置控制 URL 如何传递给 Agent：

| 配置 | 值 | 行为 |
|------|-----|------|
| `embed_image_url: false` | 默认 | 图片 URL 通过 ContentParts 传递，由 wecom channel 下载并转成图片字节 |
| `embed_image_url: true` | | 图片 URL 嵌入文本 `[image_url:URL]`，Agent 需调用工具下载 |
| `embed_file_url: false` | 默认 | 文件 URL 通过 ContentParts 传递，由 wecom channel 下载；企微加密文件会先解密 |
| `embed_file_url: true` | | 文件 URL 嵌入文本 `[file_url:URL]`，Agent 需调用工具下载 |

**推荐配置**（适用于大模型 Agent）：
```yaml
embed_image_url: false  # 图片由 wecom channel 下载，模型直接处理
embed_file_url: false   # PDF / Excel 等文件直接作为文件内容传给模型
```

### 发送消息格式对比

| 发送方式 | Notification Bot | AI Bot |
|----------|-----------------|--------|
| **Payload 格式** | `markdown_v2` | `markdown` / `stream` / 媒体消息 |
| **必需字段** | `chatid` | 无（URL 已绑定会话） |
| **发送地址** | 固定 `webhook_url` | `response_url`（webhook）或 WebSocket `req_id`（websocket） |
| **超长分片** | ✅ 支持 | ✅ 支持 |
| **流式推送** | ❌ | ✅ 仅 websocket 模式支持 |
| **文件 / 媒体回传** | ❌ | ✅ 仅 websocket 模式支持 |

#### Notification Bot 发送格式示例

```json
{
  "chatid": "群ID",
  "msgtype": "markdown_v2",
  "markdown_v2": {
    "content": "消息内容"
  }
}
```

#### AI Bot 发送格式示例

```json
{
  "msgtype": "markdown",
  "markdown": {
    "content": "消息内容"
  }
}
```

#### AI Bot WebSocket 文件 / 媒体回传

企业微信长连接文档现在已经支持通过
`aibot_upload_media_init` / `aibot_upload_media_chunk` /
`aibot_upload_media_finish` 上传媒体，再用
`aibot_respond_msg` 回传 `file` / `image` / `voice` / `video`。

本插件在 `bot_mode: ai` + `connection_mode: websocket`
下已实现这条链路：

- 自动上传本地产物并换取 `media_id`
- 按文件类型和大小自动选择 `image` / `voice` / `video` /
  `file`
- 回传限制与官方长连接文档一致：
  `image <=2MB`、`voice(amr) <=2MB`、`video(mp4) <=10MB`、
  `file <=20MB`
- `response_url` / webhook 模式仍然只发送 markdown
- 仅允许回传 active coding workspace、runtime artifact output
  root、runtime temp root、runtime-managed upload storage，或其他
  显式允许的非 repo 根目录里的本地产物，避免误发宿主机上的任意
  文件
- Agent 在最终回复里追加
  ``[WECOM_FILE:/absolute/path/to/file]`` marker 时，
  channel 会自动剥离 marker，并尝试把对应文件回传到企微
- 为了兼容现有 skill 输出，独立一行的
  ``MEDIA:/absolute/path/to/file`` 也会被识别为回传目标；
  常规回复里仍然更推荐使用 ``[WECOM_FILE:...]``
- 上传文件和非源码产物默认不应写进 coding workspace；
  一次性中间产物放 runtime temp root，准备回传的最终文件优先放
  runtime artifact output root
- 如果回传失败，用户侧会看到更具体的失败原因，例如
  文件过大、路径不合法、超出当前会话允许的回传目录或当前发送方
  式不支持
- 大文件上传期间，Channel 会额外发送
  “正在回传附件 1/N ...” 这类进度快照，而不是只停留在泛化的
  “准备请求中...”

---

## 🎮 内置命令

| 命令 | 说明 |
|------|------|
| `/help [all\|text\|commands\|<topic>]` | 显示帮助卡、全文命令文本，或某个命令的详细说明 |
| `/status` | 查看当前阶段、排队情况和最近输出 |
| `/session` | 查看当前会话、人格和自动分会话设置 |
| `/sessions [数量]` | 查看最近会话列表 |
| `/switch <序号>` | 切换到某个历史会话 |
| `/name [称呼\|off]` | 查看、设置或清除当前会话里的称呼 |
| `/name global [称呼\|off]` | 查看、设置或清除全局默认称呼 |
| `/persona` | 查看当前人格，或在 WebSocket 模式下打开人格选择卡片 |
| `/persona list` | 查看所有人格 |
| `/persona <名称或设定>` | 优先按名称切换；若不存在，则直接按这段设定创建新人格 |
| `/persona show <名称>` | 查看人格内容 |
| `/persona save <名称> <设定>` | 指定名称新增或更新自定义人格，并立即启用 |
| `/persona delete <名称>` | 删除自定义人格 |
| `/workspace [目录|off]` | 查看、设置或清除当前聊天的代码工作区 |
| `/runtime` | 打开运行时控制卡片，或查看当前运行时状态 |
| `/runtime help` | 查看运行时控制的完整说明 |
| `/runtime restart [force]` | 发起无损重启或强制重启 |
| `/runtime upgrade [force|版本|preview]` | 发起无损升级、强制升级，或切到指定版本 / preview |
| `/runtime versions` | 查看当前可用版本列表 |
| `/runtime changelog [版本]` | 查看 latest 或指定版本的变更摘要 |
| `/cancel` | 取消当前正在执行的请求 |
| `/new` | 开始新会话，清空当前上下文 |
| `/recall` | 切回 `/new` 前的上一会话 |

内置人格现在统一为这一组预设：

| 命令名 | 展示名 | 说明 |
|------|------|------|
| `pragmatic` | 务实 | 更任务导向，优先给能落地的方案 |
| `snarky` | 毒舌 | 更辛辣、更阴阳怪气，适合犀利吐槽和挑错 |
| `girlfriend` | 女友 | 更亲密、更体贴，像会照顾情绪的恋人 |
| `boyfriend` | 男友 | 更亲密、更可靠，像会撑场子的恋人 |
| `quirky` | 脑洞 | 更有趣、更有记忆点，适合创意表达 |
| `creative` | 创意 | 更适合脑暴、命名和多方案发散 |
| `nerdy` | 学究 | 更爱讲原理、机制和背景 |
| `friendly` | 伙伴 | 更温暖、更协作，适合陪聊和结对推进 |
| `coach` | 教练 | 更强调拆解、推进和下一步 |
| `candid` | 坦率 | 更直白，敢指出风险和问题 |
| `concise` | 简洁 | 更短、更快，优先给结论和动作 |
| `professional` | 专业 | 更正式、更工整，适合工作沟通和文档 |

常见用法：

- 长任务卡在处理中时，发送 `/status` 查看当前阶段
- 想找回更早的上下文时，先发 `/sessions`，再用 `/switch <序号>`
  切回目标会话
- 想切成不同预设风格时，发送
  `/persona friendly`、`/persona pragmatic`、
  `/persona professional`、`/persona coach`、
  `/persona snarky`、`/persona girlfriend` 等
- 想只在当前聊天里改我的名字时，发送
  `/name 阿爪`
- 想把默认名字改给后续新会话时，发送
  `/name global 阿爪`
- 想恢复当前聊天或全局默认名字时，分别发送
  `/name off`、`/name global off`
- 想直接新增人格时，发送
  `/persona 热心一点，先给结论，再给两个可执行建议`
- 想指定一个人格名称时，发送
  `/persona save 爱心 热心一点，先给结论，再给两个可执行建议`
- 误发了一条新需求，发现上一条还在跑时，等待提示出现后可用 `/cancel`
  中断当前请求
- 刚执行了 `/new` 又想回到上一轮上下文时，发送 `/recall`

`connection_mode: websocket` 时，`/persona` 会优先回复企微原生
`button_interaction` 卡片：常用人格可直接点按钮切换，更多人格可通过下拉后点
`切换更多` 生效，卡片会原地更新当前人格状态。
`/help` 会优先回复可翻页的帮助卡；如果想一次看全文命令文本，
直接发送 `/help all`。如果想展开某个命令的完整说明，
可以发送 `/help runtime`、`/help persona`、`/help cron`
这类 topic help；当前内置 slash 也支持 `/<命令> help`
作为等价入口，例如 `/runtime help`。

## 🛠 运行时控制 (`/runtime`)

如果当前实例是由平台 `start.sh` 拉起，
并且这个 `start.sh` 会消费
`state_dir/runtime/lifecycle/intent.env`，
企微里现在可以直接对“正在运行的实例”发起生命周期动作。

常用入口：

- 发送 `/runtime`
- 发送 `/help runtime`
- 发送 `/runtime help`
- 打开帮助卡第一页，直接点 `🛠 运行时`

语义说明：

- `/runtime help`：查看运行时控制完整说明；`/help runtime` 等价
- `/runtime restart`：无损重启，先 drain 已接收请求，再重启当前版本
- `/runtime restart force`：强制重启，直接取消当前任务并尽快重启
- `/runtime upgrade`：无损升级到 latest
- `/runtime upgrade force`：强制升级到 latest
- `/runtime upgrade <version>`：升级到指定版本，当前只允许
  `>= v0.0.48`
- `/runtime upgrade <version> force`：强制升级到指定版本
- `/runtime upgrade preview`：升级到 preview channel 当前指向的版本
- `/runtime versions`：查看 `latest/releases.json` 里的可用版本和每版摘要
- `/runtime changelog [version]`：查看 latest 或指定版本的变更摘要

需要特别区分：

- `trpc-claw upgrade` 是 shell 里的离线安装命令。
- `/runtime ...` 是对当前在线实例发起的运行时动作。
- 默认 runtime 升级只跟随 stable `latest/VERSION`；
  preview 必须通过 `/runtime upgrade preview` 显式触发。

drain 期间的行为：

- 新的普通用户请求会被拒收，避免切换过程中继续接单
- `/status`、`/runtime status`、卡片操作等控制入口仍然可用；
  `/status` 和状态卡片也会带上当前运行版本
- 强制动作会向当前请求注入取消，用户侧会收到更明确的中断提示
- 如果启用了 `user_identity_lookup_command`，
  或本地已经有 `wecom/user_identity_cache.json`，
  运行时卡片里的“操作人”会优先展示解析后的英文名 / 账号名
- 如果当前实例跑在 `bot_mode: ai` +
  `connection_mode: websocket`，
  新进程在重连企业微信长连接成功后，
  会复用发起该动作时持久化下来的 `response_url`，
  向原会话补一条“重启 / 升级已完成”的消息；
  升级完成消息也会尽量带上目标版本的 changelog 摘要；
  webhook / notification 模式当前还不支持这条完成通知

---

## 🔒 权限控制 (`chat_policy`)

| 值 | 说明 |
|----|------|
| `open` | 所有人可用（默认） |
| `disabled` | 禁止所有人使用 |
| `allowlist` | 仅 `allow_users` 中的用户可用 |

`chat_policy` 只控制“谁能和机器人聊天”。
运行时升级 / 重启还有一层独立权限：

| 配置 | 含义 |
|----|------|
| `runtime_admin_policy: inherit` | 运行时控制权限沿用 `chat_policy` |
| `runtime_admin_policy: allowlist` | 只有 `runtime_admin_users` 可以触发 `/runtime restart` / `/runtime upgrade` |

如果只是普通使用者，不在运行时管理员列表里，
仍然可以看 `/runtime status`、版本列表和 changelog，
但不能真正触发升级或重启。

---

## 🔐 消息加解密

插件完整实现了企业微信 webhook 回调的消息加解密协议：

- **签名验证**：SHA1(sort(token, timestamp, nonce, encrypt))
- **AES-CBC 解密**：使用 EncodingAESKey 派生的 256-bit 密钥
- **PKCS#7 填充/去填充**
- **CorpID 校验**：解密后验证消息中的 CorpID 与配置一致

参考文档：[企业微信加解密方案](https://developer.work.weixin.qq.com/document/path/90968)

### 📂 文件解密

Webhook 模式下，文件解密使用 `EncodingAESKey`。
WebSocket 长连接模式下，图片 / 文件消息会在消息体里携带独立的
`aeskey`，需要优先使用这个 `aeskey` 解密；它不是 webhook 模式的
`encoding_aes_key`。

本插件提供了完整的文件解密能力，支持两种使用方式：

#### 方式一：通过 `msgCrypt` 实例解密（插件内部使用）

```go
mc := newMsgCrypt(token, encodingAESKey, corpID)

// 下载加密文件
encryptedData, _ := downloadFile(fileURL)

// 解密
decryptedData, err := mc.DecryptFile(encryptedData)

// 也可以获取 AES Key 供其他模块使用
aesKey := mc.GetAESKey()
```

#### 方式二：独立函数解密（外部工具使用）

适用于独立的下载工具、Agent 插件等场景，无需依赖 `msgCrypt` 实例：

```go
import "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"

// 1. 解析 EncodingAESKey 为 AES 密钥
aesKey, err := wecom.ParseEncodingAESKey("43字符的EncodingAESKey")
if err != nil {
    log.Fatal(err)
}

// 2. 下载加密文件
resp, _ := http.Get(fileURL)
encryptedData, _ := io.ReadAll(resp.Body)

// 3. 解密文件
decryptedData, err := wecom.DecryptFileWithKey(aesKey, encryptedData)
if err != nil {
    log.Fatal(err)
}

// 4. 使用解密后的文件
os.WriteFile("output.xlsx", decryptedData, 0644)
```

如果你拿到的是 WebSocket 回调消息里的 `aeskey`，先解析这个
`aeskey`，再调用相同的 `DecryptFileWithKey(...)` 即可。

#### 辅助函数：检测企微文件 URL

```go
// 自动检测 URL 是否为企微加密文件
if wecom.IsWecomFileURL(rawURL) {
    // 需要解密
    decrypted, _ := wecom.DecryptFileWithKey(aesKey, data)
} else {
    // 普通文件，直接使用
}
```

#### 文件解密 API 列表

| 函数/方法 | 说明 |
|-----------|------|
| `mc.DecryptFile(data)` | `msgCrypt` 方法，使用实例内的 AES 密钥解密文件 |
| `mc.GetAESKey()` | 获取 AES 密钥，供外部模块使用 |
| `ParseEncodingAESKey(key)` | 公开函数，将 43 字符的 EncodingAESKey 解析为 32 字节 AES 密钥 |
| `DecryptFileWithKey(aesKey, data)` | 公开函数，使用指定 AES 密钥解密文件（独立使用，无需 msgCrypt） |
| `IsWecomFileURL(url)` | 公开函数，检测 URL 是否为企微加密文件 URL |

> **加密算法**：AES-256-CBC，IV 为 AES Key 前 16 字节，PKCS#7
> 填充（上限 32 字节）。
> Webhook 文件默认使用 `EncodingAESKey`；WebSocket 长连接里的
> 附件使用消息体中的 `aeskey`。参考：
> [智能机器人长连接](https://developer.work.weixin.qq.com/document/path/101463)

---

## 📤 回复消息

### 消息通知机器人模式
- 回复通过 Webhook API 以 `markdown_v2` 格式发送
- 超长回复自动按 2000 字符分片，优先在换行符处截断
- 分片消息自动添加"(接上条消息)"前缀

### 智能机器人模式
- `connection_mode: webhook` 时，回复通过 `response_url` 发送
- `connection_mode: websocket` 时，回复通过同一条 WebSocket 连接按
  `req_id` 回写
- 支持流式消息（`enable_stream: true`）
- WebSocket 流式回包按企业微信官方协议发送完整快照，而不是仅发送
  新增后缀
- `enable_stream: true` 依赖底层 Gateway 的流式接口；没有流式接口时会自动降级为单次 markdown 回复
- 文档/表格类请求在正文出来前，WeCom 会优先展示简短阶段提示，例如
  “正在读取文件”“正在提取表格内容”“正在整理答案”
- `response_url` 仅在 webhook 模式的当前对话中有效（临时 URL）

### 主动消息发送

当 `bot_mode=ai` 且 `connection_mode=websocket` 时，
WeCom Channel 还实现了 `occhannel.MessageSender`。
代码侧可以主动发送：

- `OutboundMessage.Text`：阶段报告、最终摘要、进度通知
- `OutboundMessage.Files`：截图、代码包、报告、日志等本地文件

目标编码使用：

- `single:<wecom_user_id>`，例如 `single:T12345678`
- `group:<chatid>`，例如 `group:wrK3L2...`

发送路径复用现有 WebSocket 媒体上传、分类、大小限制和
`media_id` 组包逻辑。图片、语音、视频和普通文件会按扩展名及大小
自动选择企业微信消息类型。

完整代码示例、字段说明和 admin debug API 见
[`outbound_message_api.md`](outbound_message_api.md)。

---

## 🔄 并发控制

- 同一会话（chat + user）的消息串行处理，避免并发冲突
- 支持 inflight 请求追踪，配合 `/cancel` 命令使用

---

## 🎨 自定义选项

通过 `Option` 函数自定义消息文案（程序化创建时使用）：

```go
ch, err := wecom.New(deps, spec,
    wecom.WithProcessingMessage("思考中..."),
    wecom.WithHelpMessage("自定义帮助"),
    wecom.WithCancelOKMessage("已取消当前请求"),
    wecom.WithNotAllowedMessage("无权限"),
)
```

---

## 📚 常见问题

### Q: 如何选择机器人类型？

**消息通知机器人** 适合：
- ✅ 简单的通知推送
- ✅ 系统告警
- ✅ 不需要流式回复的场景

**智能机器人** 适合：
- ✅ AI 对话场景
- ✅ 需要流式回复（打字机效果）
- ✅ 与大模型深度集成

### Q: 可以同时使用两种机器人吗？

**可以！** OpenClaw 框架原生支持多个 Channel 实例，通过不同的 `name` 和 `callback_path` 区分。

#### 双机器人配置示例

```yaml
channels:
  # ========== 智能机器人（AI Bot）==========
  # 用途：AI 对话、问答、客服场景
  # 企业微信后台回调 URL: http://您的域名/wecom/ai/callback
  - type: "wecom"
    name: "style_ai_robot"
    config:
      bot_mode: "ai"
      enable_stream: true  # 启用流式回复
      token: "此处填写AI机器人的回调Token"
      encoding_aes_key: "此处填写AI机器人的43字符回调加密密钥"
      callback_port: 0  # Shared Mux 模式
      callback_path: "/wecom/ai/callback"  # 🔥 必须与企业微信后台配置一致
      bot_name: "AI助手"
      chat_policy: "open"

  # ========== 消息通知机器人（Notification Bot）==========
  # 用途：主动推送告警、定时通知、审批提醒
  # 企业微信后台回调 URL: http://您的域名/wecom/notify/callback?robot_callback_format=json
  - type: "wecom"
    name: "style_notify_robot"
    config:
      bot_mode: "notification"
      webhook_url: "此处填写群机器人Webhook地址"  # 群机器人 Webhook
      token: "此处填写通知机器人的回调Token"
      encoding_aes_key: "此处填写通知机器人的43字符回调加密密钥"
      callback_port: 0  # Shared Mux 模式
      callback_path: "/wecom/notify/callback"  # 🔥 必须与企业微信后台配置一致
      bot_name: "通知机器人"
      chat_policy: "open"
```

#### 企业微信后台配置（网关路径）

| 机器人类型 | 企业微信后台回调 URL | 说明 |
|-----------|---------------------|------|
| **智能机器人** | `http://您的域名/wecom/ai/callback` | 无需额外参数 |
| **消息通知机器人** | `http://您的域名/wecom/notify/callback?robot_callback_format=json` | ⚠️ 必须添加 `robot_callback_format=json` |

#### 多环境配置示例

```
# 开发环境
AI Bot:      http://ad-ai.woa.com/wecom/dev/callback
Notify Bot:  http://ad-ai.woa.com/wecom/dev/notify/callback?robot_callback_format=json

# 测试环境
AI Bot:      http://ad-ai.woa.com/wecom/test/callback
Notify Bot:  http://ad-ai.woa.com/wecom/test/notify/callback?robot_callback_format=json

# 生产环境
AI Bot:      http://ad-ai.woa.com/wecom/prod/callback
Notify Bot:  http://ad-ai.woa.com/wecom/prod/notify/callback?robot_callback_format=json
```

#### 关键配置项说明

| 配置项 | 说明 | 注意事项 |
|--------|------|----------|
| `name` | Channel 实例名称 | **必须唯一**，用于区分不同机器人 |
| `callback_path` | 回调路径 | **必须唯一**，与企业微信后台 URL 路径一致 |
| `token` / `encoding_aes_key` | 加解密密钥 | 每个机器人独立，从企业微信后台获取 |
| `webhook_url` | Webhook 地址 | 仅消息通知机器人需要，从群机器人设置获取 |

#### 工作原理

```
                                   ┌─────────────────────────────┐
                                   │       OpenClaw Server       │
                                   │                             │
企业微信 ─────▶ /wecom/ai/callback ────────▶ style_ai_robot      │
                                   │            (AI Bot)         │
                                   │                             │
企业微信 ─────▶ /wecom/notify/callback ────▶ style_notify_robot  │
                                   │       (Notification Bot)    │
                                   └─────────────────────────────┘
```

框架会为每个 Channel 注册独立的 HTTP 路由，互不干扰。

### Q: 智能机器人的 response_url 是什么？

智能机器人的每次回调中都会携带一个临时的 `response_url`，用于回复当前对话。插件会自动检测并使用这个 URL，无需手动配置。

### Q: 企业微信 WebSocket 和 Gateway SSE / StreamMessage 是一回事吗？

不是。

- 企业微信 WebSocket 是 **企微 -> WeCom Channel** 的长连接接入方式
- Gateway SSE / `StreamMessage` 是 **WeCom Channel -> Gateway** 的流式调用能力

只有这两层都打通，`enable_stream: true` 才能形成真正的端到端增量回复。

### Q: 消息通知机器人能接收群消息吗？

默认情况下**不能**。只有企业微信白名单企业才能配置回调 URL 接收群消息。如果你的企业不在白名单中，建议使用智能机器人。

---

## 📁 文件结构

```
wecom/
├── wecom.go              # 主入口：Channel 结构、注册、配置解析、消息处理主流程
├── wecom_test.go         # 核心测试（含骨架测试 + 功能测试）
├── aggregator.go         # 消息聚合器：按时间窗口合并同一用户的多条消息
├── aggregator_test.go    # 聚合器测试
├── commands.go           # 通用 slash 命令（/help, /cancel, /workspace）
├── commands_test.go      # 命令解析测试
├── control_cards.go      # 欢迎卡片、控制面板和帮助卡翻页导航
├── crypto.go             # 企业微信消息加解密 + 文件解密实现
├── message.go            # 消息类型定义（WebhookMessage 等，支持两种模式）
├── runtime_commands.go   # /runtime 命令解析与回复
├── runtime_lifecycle.go  # 运行时权限、状态格式化和准入控制
├── sender.go             # Webhook / WebSocket 回复发送器
├── streaming.go          # Gateway 流式调用适配与增量回复
├── streaming_test.go     # 流式回复测试
├── websocket.go          # AI Bot WebSocket 长连接接入实现
├── websocket_test.go     # WebSocket 接入测试
├── utils.go              # 工具函数（session/request ID 构建、消息分片等）
├── utils_test.go         # 工具函数测试
├── config-examples.yaml  # 配置示例文件
└── README.md             # 本文档
```

---

## 🔗 相关链接

- [消息通知机器人官方文档](https://developer.work.weixin.qq.com/document/path/99110)
- [智能机器人官方文档](https://developer.work.weixin.qq.com/document/path/100719)
- [智能机器人长连接官方文档](https://developer.work.weixin.qq.com/document/path/101463)
- [企业微信加解密方案](https://developer.work.weixin.qq.com/document/path/90968)
- [接收消息与事件](https://developer.work.weixin.qq.com/document/path/99399)
