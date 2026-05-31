# Weixin Channel Plugin

微信 Channel 插件，实现 openclaw `Channel` 接口。
它面向的是 `ClawBot / iLink` 这套微信后端能力，而不是企微协议。

如果你现在想尽快把微信私聊链路跑通，建议先看这份 README，
再参考 [`get-started.md`](get-started.md) 和
[`config-examples.yaml`](config-examples.yaml)。
如果你要把微信扫码入口接到 AGUI 这类前端，
再看 [`frontend_qr_entry.md`](frontend_qr_entry.md)。
这份文档同时包含固定二维码入口和后台绑定状态 API 的接法。

## 当前支持范围

- 二维码登录：
  `trpc-claw weixin login`、
  运行中 admin 的 `Channels` 页面、
  或固定入口 `/channels/wx_qr`
- 账号查看和清理：`trpc-claw weixin list` /
  `trpc-claw weixin remove <ACCOUNT_ID>` /
  admin 页面里的 remove / resume
- 私聊 direct chat
- 文本入站和文本分片回包
- 语音转写文本入站
- long-poll 收消息
- 多账号并行
- `accountID + peerID -> context_token` 持久化
- `errcode = -14` 时自动 pause 一小时
- typing 提示
- admin 页面里的微信管理区：
  账号状态、二维码登录状态、remove、resume
- 纯文本 runtime 命令：
  `/runtime status`、
  `/runtime versions`、
  `/runtime changelog [version]`
- 主动发文本能力，delivery target 会编码
  `account_id + peer_id`

## 当前还不支持

- 群聊
- 图片 / 文件 / 视频发送
- 非文本媒体入站透传到 Agent
- `/runtime restart`、`/runtime upgrade`

当前遇到图片、文件、视频等非文本消息时，
插件会明确回复“当前只支持文本输入”，而不是做 transport-specific
硬编码特判。

## 快速开始

### 1. 配置 channel

最小配置示例：

```yaml
channels:
  - type: "weixin"
    name: "weixin-direct"
    config:
      base_url: "https://ilinkai.weixin.qq.com"
      poll_timeout: "35s"
      error_backoff: "30s"
      enable_typing: true
      enable_runtime_commands: true
      # allow_users:
      #   - "user_a@im.wechat"
```

注意：

- 微信 channel 的参数必须放在 `channels[].config` 下面。
- 账号 token 不写进 YAML。
- 账号通过 CLI 或 admin 二维码登录后写入
  `state_dir/weixin/accounts/*.json`。

### 2. 登录微信账号

推荐路径是先启动 `trpc-claw`，再打开 admin 的共享
`Channels` 页面：

```text
http://127.0.0.1:19789/channels
```

当前旧的 `/weixin` 链接仍然可以访问，但会自动跳转到 `/channels`。

如果 admin 因端口冲突自动换口，启动日志里会打印：

```text
Admin UI: http://127.0.0.1:<port>
```

这个页面支持：

- 开始二维码登录
- 打开固定二维码入口
- 查看最近一次二维码状态
- remove 账号
- resume 一个 paused 账号
- 查看 last inbound / outbound / error

如果你要给前端一个“点开就能去扫码”的固定入口，
可以直接打开：

```text
http://127.0.0.1:19789/channels/wx_qr
```

这个入口会复用当前 Weixin admin session，
在当前 runtime 还没有保存账号时自动开始一次二维码登录，
然后把浏览器导向最新的微信二维码页面。
如果同一个实例里配了多个 Weixin runtime，
也可以显式带上：
`/channels/wx_qr?runtime_key=weixin-1`。
更细的前端接入说明看
[`frontend_qr_entry.md`](frontend_qr_entry.md)。

CLI 登录入口仍然保留：

```bash
trpc-claw weixin login
```

常用可选参数：

```bash
trpc-claw weixin login --base-url https://ilinkai.weixin.qq.com
trpc-claw weixin login --timeout 8m
```

命令会输出二维码 URL 和扫码状态；二维码过期时会自动刷新最多 3 次。
登录完成后，账号信息会自动保存到状态目录。

### 3. 查看当前账号

```bash
trpc-claw weixin list
```

输出会包含：

- account id
- user id
- paused 状态
- last error

### 4. 删除一个账号

```bash
trpc-claw weixin remove <ACCOUNT_ID>
```

### 5. 启动 openclaw

`weixin` channel 可以在 0 账号状态下启动。
所以更推荐的顺序是：

1. 先启动 `trpc-claw`
2. 打开 admin 的 `/channels`
3. 在 `Weixin Runtime` 里扫码登录
4. 等运行中的 channel 自动发现新账号

如果 `trpc-claw` 已经在运行，
`weixin` channel 也会自动发现新增/删除账号，
默认刷新间隔是 5 秒左右，不需要手动重启进程。

## 配置字段说明

- `state_dir`
  覆盖微信 channel 自己的状态目录。
  不配置时默认是 `${global_state_dir}/weixin`。
- `base_url`
  iLink API 基础地址。
  默认值是 `https://ilinkai.weixin.qq.com`。
- `poll_timeout`
  单次 `getupdates` 的 long-poll 超时。
  默认值是 `35s`。
- `error_backoff`
  拉取失败后的退避时间。
  默认值是 `30s`。
- `enable_typing`
  是否发送 typing 提示。
  默认值是 `true`。
- `enable_runtime_commands`
  是否启用 `/runtime ...` 纯文本命令。
  默认值是 `true`。
- `allow_users`
  允许访问的微信 user id 白名单。
  不配置时表示不做额外限制。
- `release_base_url`
  runtime 版本索引和 changelog 的读取地址。
  不配置时使用默认 release 源。

## 状态目录

默认状态目录结构如下：

- `state_dir/weixin/accounts/index.json`
- `state_dir/weixin/accounts/<account-id>.json`
- `state_dir/weixin/accounts/<account-id>.sync.json`
- `state_dir/weixin/accounts/<account-id>.context.json`
- `state_dir/weixin/runtime/<account-id>.status.json`

这些文件分别保存：

- 账号索引
- token / base URL / user ID
- `getupdates` cursor
- `context_token`
- paused / 最近收发时间 / 最近错误

所有文件都按运行时状态管理，不需要手工编辑。

## 行为说明

- session key 固定为 `weixin:dm:<account-id>:<peer-id>`，
  避免多账号之间串会话。
- 回复时会优先复用最近一次入站消息保存的 `context_token`。
- 长文本会自动分片发送，避免单条过长失败。
- 如果微信后端返回 `errcode = -14`，
  该账号会被标记为 paused 一小时，并在 `list` /
  `/runtime status` 里可见。

## 调试建议

- 第一次联调先只测 direct text，不要同时追群聊和媒体。
- 如果 `list` 里看到 `paused` 或 `last_error`，
  先重新登录对应账号，再继续看后端错误。
- 如果账号因为 `errcode = -14` 进入 paused，
  现在也可以直接在 admin 的 `Channels -> Weixin Runtime`
  区块里点 `Resume Now`。
- 如果用户反馈“重启后不回了”，优先检查：
  `accounts/*.json`、
  `*.sync.json`、
  `*.context.json`、
  `runtime/*.status.json`
  是否还在。
