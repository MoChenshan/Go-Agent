# OpenClaw Plugins（Go 代码扩展）

本目录用于共建 OpenClaw 的 Go 插件（编译期注册）：

- Channel（对接企业微信/Slack/自建 IM 等）
- Tool Provider / ToolSet（提供工具能力）
- Session / Memory Backend（存储后端）
- Model Provider（模型接入）

插件不是“运行时动态加载”，而是通过 Go 的 `init()` + 空白导入 `_ "..."` 在编译期
注册到 `trpc.group/trpc-go/trpc-agent-go/openclaw/registry`。

## Env Probe Tool Provider

`openclaw/plugins/envprobe` 提供内置的环境变量探测工具：

- 插件类型：`envprobe`
- 工具名：`env_probe`
- 用途：安全检查当前实例能不能看到某个环境变量，
  以及它是不是只声明在 `~/.bashrc` / `~/.zshrc` /
  `~/.profile` / `TRPC_CLAW_ENV_FILE` / `runtime/env.sh`
  这类受信任来源里
- 行为特点：只返回存在性和来源，不暴露变量值；
  如果它在受信任文件里发现了简单静态声明，
  还会把该变量补进当前 `trpc-claw` 进程环境，
  让后续 `exec_command` / `skill_run` 可以直接使用

## WeCom（企业微信）Channel

`openclaw/channel/wecom` 提供企业微信 Channel 插件：

- 插件类型：`wecom`
- 开启方式：在 `openclaw.yaml` 的 `channels:` 中添加 `type: wecom`
- 支持 Notification Bot，以及 AI Bot 的 `webhook` / `websocket`
  两种接入方式
- AI Bot 可开启 `enable_stream: true`，在 Gateway 具备流式接口时输出增量回复

## Weixin（微信）Channel

`openclaw/channel/weixin` 提供微信 Channel 插件：

- 插件类型：`weixin`
- 开启方式：在 `openclaw.yaml` 的 `channels:` 中添加 `type: weixin`
- 当前重点支持：二维码登录、私聊文本收发、long-poll、多账号并行、
  `context_token` 持久化、typing 提示、admin `/weixin` 管理页、
  `/runtime status` / `/runtime versions` / `/runtime changelog`
- 账号不是写进 YAML，而是通过 `trpc-claw weixin login` 或 admin
  `/weixin` 登录落到
  `state_dir/weixin/accounts/*.json`
- 运行中的 Channel 会自动发现新增/删除账号；更完整的配置和使用方式见
  [`../channel/weixin/README.md`](../channel/weixin/README.md)
