# OpenClaw（内网分发版）

`openclaw/` 提供的是一个可长期运行的 Agent 服务形态。
它不是“单次调用一个模型”的小脚本，而是把下面这些东西组装到了一起：

- Gateway：统一接收消息、管理会话、暴露健康检查和调试入口。
- Channel：把企业微信、终端输入、Webhook 等外部消息接进来。
- Agent：负责提示词、工具调用、技能加载和推理调度。
- Session / Memory：负责保存上下文和长期记忆。
- Tool / Skill：负责“让机器人真的能做事”。

这份 README 以“企业微信智能机器人长连接”作为主线来写，
因为这是当前内网分发版最推荐、也最容易第一次跑通的路径。
如果你第一次接触 `trpc-claw`，建议按本文顺序一路做完。

更细的安装说明看 [`INSTALL.md`](INSTALL.md)。
版本变化和每个 release 的主要能力看 [`CHANGELOG.md`](CHANGELOG.md)。
更细的企业微信插件说明看 [`channel/wecom/README.md`](channel/wecom/README.md)。
如果你要在代码里主动推送阶段报告、截图或文件，
看 [`channel/wecom/outbound_message_api.md`](channel/wecom/outbound_message_api.md)。
如果你要了解 agent 或业务代码如何触发后台 subagent，
看 [`SUBAGENT_RUNTIME.md`](SUBAGENT_RUNTIME.md)。
更细的微信 Channel 说明看 [`channel/weixin/README.md`](channel/weixin/README.md)。
如果你想直接按微信路径从安装走到扫码聊天，
看 [`channel/weixin/get-started.md`](channel/weixin/get-started.md)。
如果你想把微信扫码入口接到 AGUI 这类前端，
看 [`channel/weixin/frontend_qr_entry.md`](channel/weixin/frontend_qr_entry.md)。
更细的联调手册看 [`channel/wecom/runbook.md`](channel/wecom/runbook.md)。
如果你想直接做一个“依赖预装齐”的 Linux 本地容器，
看 [`docker/README.md`](docker/README.md)。

## 先记住三件事

1. 默认安装出来的主配置就是企微 AI 长连接模板
   `openclaw.wecom.ai.websocket.yaml`。
2. 第一次联调一定先跑 `mock`，先验证“企业微信链路”没问题，
   再切真实模型，这样排障范围最小。
3. 如果你已经把本机配置改乱了，最直接的恢复命令是：

```bash
trpc-claw upgrade -f --profile wecom-ai-websocket
```

它会直接覆盖当前机器正在使用的：

- `~/.trpc-agent-go/openclaw/openclaw.yaml`
- `~/.trpc-agent-go/openclaw/trpc_go.yaml`

## 默认安装后你已经得到什么

镜像安装脚本：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash
```

默认会安装这些内容：

- 二进制：`~/.local/bin/trpc-claw`
- 主配置：`~/.trpc-agent-go/openclaw/openclaw.yaml`
- tRPC 配置：`~/.trpc-agent-go/openclaw/trpc_go.yaml`
- 模板目录：`~/.trpc-agent-go/openclaw/profiles/`
- bundled skills：`~/.trpc-agent-go/openclaw/skills/bundled/`
- local skills 目录：`~/.trpc-agent-go/openclaw/skills/local/`
- 外部技能目录：`~/.codex/skills/`（存在时自动补进来）

local skills 是教会 `trpc-claw` 长期能力的默认位置。
当用户希望机器人记住某个工作流、连接某个工具或 API、
复用某个 MCP server、遵循团队流程，或者把领域规则保留下来
供后续任务使用时，优先创建或更新 skill，
而不是把这个场景写成专用运行时代码。

轻量事实、偏好和简单常驻规则继续放到 memory。
当“记住”的内容需要可执行流程、工具、示例、参考资料或失败恢复时，
再沉淀成 skill。

运行时代码和配置负责稳定边界，例如权限、密钥处理、文件访问、
校验和生命周期管理。skill 负责不断演进的上下文：
能力何时触发、如何操作、哪些示例重要，以及遇到常见失败时如何恢复。

其中 `bundled/` 里现在同时包含：

- OpenClaw 默认 skills
- Anthropic 官方 skills 的内置快照

Anthropic 那一组统一做了 `anthropic-` 前缀，
例如 `anthropic-docx`、`anthropic-pdf`、`anthropic-webapp-testing`。
这样做是为了避免和现有 OpenClaw skill、用户自定义 skill
撞名，也尽量减少触发时的“水土不服”。

默认主配置会直接打开这些能力：

- 企业微信 AI 长连接：`bot_mode: "ai"` +
  `connection_mode: "websocket"`
- 流式回复：`enable_stream: true`
- 收到 `enter_chat` 事件时自动回复欢迎卡片和快捷入口：
  `enter_chat_welcome: true`
- 企业微信回复默认不再自动附加固定前缀；
  如果需要，仍然可以显式开启 `reply_prefix.enabled: true`
  来挂常驻 `/help`、`/persona`、`/status`
  和可配置链接入口
- 本地命令执行：`tools.enable_local_exec = true`
- 并行工具调用：`tools.enable_parallel_tools = true`
- 常用工具：
  `duckduckgo`、`webfetch_http`、`file`、`wikipedia`、`arxivsearch`
- 默认 skills 根目录：
  `state_dir/skills/bundled` + `state_dir/skills/local` +
  `~/.codex/skills`
- 默认 Session 后端：`sqlite`
- 默认 Memory 后端：`sqlitevec`
- 默认 persona：`default`
- 默认 `skills.coding_agent.execution_mode = "host"`
- 如果不显式写 `default_workdir`，
  当前工作目录会直接作为默认 coding workspace
- 默认临时目录走 `state_dir/runtime/tmp`，
  非源码产物默认建议写到 `scratch_root/out`
- 默认 debug recorder：开启，目录在 `state_dir/debug`
- 默认 Langfuse tracing：开启但非强依赖，
  admin 会显示 `trace_id` 并默认把 UI 指到
  `http://127.0.0.1:3000`

如果你只是想本地先玩一把，不接企业微信，也可以显式装成 `mock`：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --profile mock
```

如果你安装后还想顺手补齐常见文件依赖，也可以一条命令一起做：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --bootstrap-deps
```

这条路径会按当前 release 里 bundled skills 的依赖元数据来规划安装，
并额外带上 `common-file-tools`。
它只会自动处理相对安全的系统包和托管 Python 依赖；
像浏览器 runtime、全局 npm 包、账号凭据这类更容易扰动环境的步骤，
仍然保持手动。

## 为什么优先推荐企微长连接

企业微信目前支持多种机器人接入方式，`openclaw` 也都支持：

| 模式 | 对应 profile | 什么时候用 |
| --- | --- | --- |
| AI 长连接 | `wecom-ai-websocket` | 第一次上手，最推荐 |
| AI URL 回调 | `wecom-ai` | 组织要求必须走回调 |
| 通知机器人 | `wecom-notification` | 只做通知型消息 |
| 微信私聊 | `weixin` | 走微信二维码登录和私聊 |
| 本地试玩 | `mock` | 先验证二进制、配置和工具 |

第一次上手优先推荐长连接，原因很简单：

- 它只需要企业微信后台给你的 `bot id` 和 `secret`。
- 它不需要先准备太湖域名。
- 它不需要先做 URL 回调校验。
- 它最容易直接体验流式回复。

如果你的目标是“尽快让同事感觉这个机器人真的能玩”，
长连接是成本最低的起步方式。

## 5 分钟快速开始

这一节只做一件事：把企业微信智能机器人从安装带到第一次成功回消息。

### 第 1 步：安装 `trpc-claw`

如果你是第一次装，直接：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash
```

如果你机器上以前装过旧版本，想明确把当前主配置切回默认长连接模板，
直接：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- -f --profile wecom-ai-websocket
```

或者，在你已经装好新版 `trpc-claw` 的前提下，也可以直接用 CLI：

```bash
trpc-claw upgrade -f --profile wecom-ai-websocket
```

### 第 2 步：补常见文件依赖

这一步不是“启动长连接”必需，
但如果你想让机器人更快支持 PDF、Office、文件处理等典型场景，
建议安装后顺手做掉。

先看 bundled skills 还缺什么：

```bash
trpc-claw inspect deps --bundled
```

再补 bundled skills 的默认安全依赖：

```bash
trpc-claw bootstrap deps --bundled --apply
```

如果最后只剩系统包安装需要提权，
优先执行输出里那条 `sudo apt|dnf|yum ...`。
如果你直接整条命令重跑成 `sudo trpc-claw ... --bundled`，
现在也会自动回看原用户安装目录里的 bundled skills，
不再因为 `HOME=/root` 就丢失技能目录。

如果你想先做一次基本体检，也可以：

```bash
trpc-claw doctor
```

### 第 3 步：准备环境变量

推荐把模型配置和企业微信参数都写进 `~/.bashrc`
或 `~/.zshrc`，然后再启动 `trpc-claw`。

```bash
export OPENAI_MODEL='gpt-5.2'
export OPENAI_API_KEY='replace-with-your-api-key'
# OpenAI-compatible 模型网关。
export OPENAI_BASE_URL='https://your-openai-compatible-endpoint/v1'

# 企业微信长连接必填。
export WECOM_STREAM_BOT_ID='replace-with-your-aibotid'
export WECOM_STREAM_SECRET='replace-with-your-aibot-secret'

# 常见可选项。
export WECOM_BOT_NAME='OpenClaw'
# 群聊里按整个群共享历史；改成 isolated 可切成群内按用户隔离。
export WECOM_GROUP_SESSION_MODE='shared'
# export WECOM_STREAM_WS_URL='wss://openws.work.weixin.qq.com'

# 可选：Langfuse。
# OpenClaw 会主动 push traces 到 LANGFUSE_HOST，不需要 Langfuse 反向来拉。
# LANGFUSE_HOST 是进程访问的 OTLP HTTP host:port，不带 http:// 或 https://。
export LANGFUSE_HOST='127.0.0.1:3000'
export LANGFUSE_PUBLIC_KEY='replace-with-your-langfuse-public-key'
export LANGFUSE_SECRET_KEY='replace-with-your-langfuse-secret-key'
export LANGFUSE_INSECURE='true'
# 可选：浏览器打开的 Langfuse UI 地址；不配时会从 LANGFUSE_HOST 推导。
export LANGFUSE_UI_BASE_URL='http://127.0.0.1:3000'
# 可选：用于 admin 拼出 trace deep-link。
export LANGFUSE_INIT_PROJECT_ID='local-dev'
```

然后重新加载：

```bash
source ~/.bashrc
```

如果你改完 shell 环境后，想直接确认当前实例
“能不能看到”某个变量，现在可以直接问 agent，例如：

```text
你能读到 TAIHU_PAT_TOKEN 吗
```

默认模板会自动带上 `env_probe`。
它会安全检查当前 `trpc-claw` 进程、
`TRPC_CLAW_ENV_FILE`、`runtime/env.sh`、
`~/.bashrc`、`~/.zshrc`、`~/.profile`
这些受信任来源，但不会回显变量值。
如果它在这些文件里检测到简单静态声明，
还会把变量补进当前 `trpc-claw` 进程环境，
让后续 `exec_command`
和普通 runtime tools
可以直接使用，而不是只会诊断“看到了但还没生效”。

这几个变量分别是什么意思：

- `OPENAI_MODEL`：默认 openai profile 使用的模型名。
- `OPENAI_BASE_URL`：模型服务的基础地址。
- `OPENAI_API_KEY`：模型服务密钥。
- `WECOM_STREAM_BOT_ID`：企业微信 AI 机器人的 `bot id`。
- `WECOM_STREAM_SECRET`：企业微信 AI 机器人的长连接密钥。
- `WECOM_BOT_NAME`：可选，用来帮助移除消息里的 `@机器人名`。
- `WECOM_STREAM_WS_URL`：可选，用来覆盖默认长连接地址。
- `LANGFUSE_HOST`：Langfuse OTLP HTTP 上报入口，
  只写 `host:port`，不要带 scheme。要真正上报 trace，
  它和下面两个 key 都需要存在。
- `LANGFUSE_PUBLIC_KEY` / `LANGFUSE_SECRET_KEY`：
  Langfuse 项目 API key。
- `LANGFUSE_INSECURE`：HTTP 上报时设成 `true`；
  HTTPS 上报不需要设置。
- `LANGFUSE_UI_BASE_URL`：可选，浏览器打开 Langfuse 的地址，
  要带 `http://` 或 `https://`。不设置时会从 `LANGFUSE_HOST`
  和 `LANGFUSE_INSECURE` 推导。
- `LANGFUSE_INIT_PROJECT_ID`：可选，用来让 admin 自动拼出
  Langfuse trace deep-link。
- `LANGFUSE_TRACE_URL_TEMPLATE`：可选，直接覆盖 trace 链接模板；
  模板里要包含 `{{trace_id}}`。
- `LANGFUSE_ENABLED`：可选，默认 `true`。
- `LANGFUSE_REQUIRED`：可选，默认 `false`。保持默认时，
  Langfuse 配置缺失或初始化失败不会阻断主流程。
- `LANGFUSE_OBSERVATION_LEAF_VALUE_MAX_BYTES`：可选，默认 `4096`，
  用来限制单个 observation 叶子字段的长度。

如果你是用 `systemd`、`supervisor`、守护进程脚本之类方式启动，
一定要确保这些环境变量也被那套启动方式显式加载。
否则你在交互 shell 里 `echo` 得到的值，
并不等于后台进程里真的能读到。

### 第 4 步：在企业微信后台申请 AI 长连接机器人

你前面写进环境变量的：

- `WECOM_STREAM_BOT_ID`
- `WECOM_STREAM_SECRET`

都来自企业微信后台的机器人创建页面。

#### 4.1 进入工作台，创建智能机器人

<img src="https://cdn.jsdelivr.net/gh/WineChord/typora-images/img/trpc-agent-go-openclaw/wecom-setup/1.png" alt="创建智能机器人" style="display: block; margin: 20px auto; max-width: 100%; max-height: 520px;" />

#### 4.2 选择手动创建

<img src="https://cdn.jsdelivr.net/gh/WineChord/typora-images/img/trpc-agent-go-openclaw/wecom-setup/2.png" alt="手动创建机器人" style="display: block; margin: 20px auto; max-width: 100%; max-height: 520px;" />

#### 4.3 切到 API 模式创建

<img src="https://cdn.jsdelivr.net/gh/WineChord/typora-images/img/trpc-agent-go-openclaw/wecom-setup/3.png" alt="切换到 API 模式创建" style="display: block; margin: 20px auto; max-width: 100%; max-height: 520px;" />

#### 4.4 选择“长连接”接入方式

<img src="https://cdn.jsdelivr.net/gh/WineChord/typora-images/img/trpc-agent-go-openclaw/wecom-setup/long-connection.png" alt="长连接流式企业微信机器人申请入口" style="display: block; margin: 20px auto; max-width: 100%; max-height: 560px;" />

这一页里最重要的就是两个值：

- 页面上的 `bot id`，对应 `WECOM_STREAM_BOT_ID`
- 页面上的 `secret`，对应 `WECOM_STREAM_SECRET`

把它们填回你前面第 3 步准备好的环境变量里即可。

### 第 5 步：确认当前二进制真的带了企微插件

这一步的目的是避免“配置写对了，但当前二进制里根本没编进 `wecom`”。

```bash
trpc-claw inspect plugins
```

你至少要在输出里看到 `wecom`。

如果你想再做一次更快的本机体检，可以再跑：

```bash
trpc-claw doctor
```

### 第 6 步：第一次联调先跑 `mock`

先不要急着一上来就接真实模型。
第一次联调的目标，只是验证：

- 企业微信机器人参数对不对
- 长连接能不能连上
- Gateway 能不能收到消息
- 回复链路通不通

最稳的做法是先跑：

```bash
trpc-claw -mode mock
```

启动后先做健康检查：

```bash
curl -sS 'http://127.0.0.1:8080/healthz'
```

然后在企业微信里给机器人发一条最简单的文本，例如：

```text
hello
```

日志里通常应该能看到类似信息：

- `OpenClaw gateway is registered to "trpc.openclaw.gateway"`
- `wecom: using ai bot websocket mode`

停止进程时，第一次 `Ctrl-C` 会走优雅退出；如果你想立刻结束，
再按一次 `Ctrl-C` 就会强制退出。

如果这里就报错，优先看这几类问题：

- `config: env var WECOM_STREAM_BOT_ID is not set`
- `config: env var WECOM_STREAM_SECRET is not set`
- `address already in use`
- 持续刷 `wecom websocket: session failed`

### 第 7 步：`mock` 跑通以后再切真实模型

当 `mock` 已经能在企业微信里正常回消息，再切真实模型：

```bash
trpc-claw -mode openai -model gpt-5
```

如果你不传 `-model`，默认模板里的 `model.name`
当前写的是 `gpt-5.2`。
你可以根据自己实际可用的模型服务改成别的 OpenAI-compatible 模型。

### 第 8 步：优先测这三类消息

第一次切真实模型，不要一上来就测太复杂的自动化。
建议按这个顺序发消息：

1. 纯文本：例如“请分三行介绍一下你自己”
2. 图片：例如“这张图里是什么”
3. PDF 或 Excel：例如“总结这份文件的主要内容”

这样你能很快判断：

- 纯文本链路有没有通
- 多模态输入有没有通
- 文件下载与解析有没有通

## 企业微信里实际能玩什么

下面这三张图展示的是已经跑通后的典型效果。

### 图片识别

<img src="https://cdn.jsdelivr.net/gh/WineChord/typora-images/img/trpc-agent-go-openclaw/wecom-demo/%E4%BC%81%E4%B8%9A%E5%BE%AE%E4%BF%A1_%E5%9B%BE%E7%89%87%E8%AF%86%E5%88%AB.png" alt="企业微信图片识别" style="display: block; margin: 20px auto; max-width: 100%; max-height: 620px;" />

### PDF 处理

<img src="https://cdn.jsdelivr.net/gh/WineChord/typora-images/img/trpc-agent-go-openclaw/wecom-demo/%E4%BC%81%E4%B8%9A%E5%BE%AE%E4%BF%A1_PDF%E5%A4%84%E7%90%86.png" alt="企业微信 PDF 处理" style="display: block; margin: 20px auto; max-width: 100%; max-height: 620px;" />

### Excel 处理

<img src="https://cdn.jsdelivr.net/gh/WineChord/typora-images/img/trpc-agent-go-openclaw/wecom-demo/%E4%BC%81%E4%B8%9A%E5%BE%AE%E4%BF%A1_EXCEL%E5%A4%84%E7%90%86.png" alt="企业微信 EXCEL 处理" style="display: block; margin: 20px auto; max-width: 100%; max-height: 620px;" />

这三类体验能直接跑起来，背后主要依赖默认模板里的两项设置：

- `embed_image_url: false`
- `embed_file_url: false`

这意味着企业微信 Channel 会先把图片和文件真正下载下来，
再把内容交给 Gateway、Tool 和 Model，而不是只把一个 URL 扔过去。

## 一眼看懂默认 `openclaw.yaml`

默认安装出来的主配置文件是：

- `~/.trpc-agent-go/openclaw/openclaw.yaml`

如果你不确定当前进程到底在用哪份配置，
可以直接跑：

```bash
trpc-claw -h
```

或者看启动日志里的：

- `Config:   tRPC = ...`
- `Config:   OpenClaw = ...`
- `Config:   state_dir = ...`

### 1. `agent`：人格、提示词、Agent 行为

默认模板大致是这样：

```yaml
agent:
  instruction: "You are a helpful assistant."
  persona: "pragmatic"
  # instruction_dir: "${TRPC_CLAW_STATE_DIR}/prompts/instruction"
  # system_prompt_dir: "${TRPC_CLAW_STATE_DIR}/prompts/system"
  # persona_dir: "${TRPC_CLAW_STATE_DIR}/personas"
```

这里的分工是：

- `instruction`：给模型的高层行为指导；可继续直接写字符串。
- `instruction_files` / `instruction_dir`：额外加载的 instruction
  片段；默认会从 `state_dir/prompts/instruction` 读取。
- `system_prompt`：额外的 inline system prompt。
- `system_prompt_files` / `system_prompt_dir`：system prompt
  片段；默认会从 `state_dir/prompts/system` 读取。运行时身份、
  coding guidance 这类默认 prompt 现在都来自这里的文件。
- `persona`：从 `persona_dir` 里按名称或 ID 选择 persona 文件。
  不配时默认就是 `pragmatic`；`off` 才表示关闭 persona 注入。
  默认 persona 文件会自动初始化到 `state_dir/personas`。

默认初始化的 persona 文件有：

- `pragmatic`
- `friendly`
- `professional`
- `concise`
- `coach`
- `creative`
- `candid`
- `quirky`
- `nerdy`
- `snarky`
- `girlfriend`
- `boyfriend`
- `off`

其中 `off` 的意思是：
关闭 persona 文件注入，只保留你自己写的
`instruction` / `system_prompt` / prompt files。

也就是说，`persona` 和 `instruction` / `system_prompt`
以及 prompt files 是共存关系，不是互斥关系。

另外，assistant 的全局默认名字现在来自
`state_dir/IDENTITY.md`。
企业微信里发送 `/name global <称呼>`
会更新这份文件；发送 `/name <称呼>`
只覆盖当前会话。
运行时 prompt 里的 `trpc-claw`
现在只作为 runtime product 使用，不再和 assistant 名字混用。

### 2. `runtime_profiles`：按用户或租户切运行期配置

如果同一个 `trpc-claw` 实例要服务不同用户、租户或部署形态，
可以把运行期差异收敛到 `runtime_profiles`。profile 可以覆盖
prompt、workspace、skill、knowledge、tool 和隔离策略。

最小形态是“不同用户不同 prompt”：

```yaml
runtime_profiles:
  required: true
  default: default
  profiles:
    default:
      app_name: "default"
      prompt:
        instruction: "You are a helpful assistant."
    tenant_alpha:
      app_name: "tenant-alpha"
      prompt:
        instruction: "You are the tenant Alpha assistant."
  selectors:
    - profile_id: "tenant_alpha"
      channels: ["wecom"]
      users: ["replace-with-wecom-userid"]
```

更常见的业务助手或客服机器人不只切 prompt，还会按租户切工作区、
Skill、知识库、工具集合和凭据引用。下面是脱敏后的结构示例：

```yaml
runtime_profiles:
  required: true
  default: default
  profiles:
    default:
      app_name: "default"
      prompt:
        instruction: "You are a helpful assistant."
      tools:
        include: ["web_fetch", "message"]
      isolation:
        mode: "shared"

    tenant_alpha:
      app_name: "tenant-alpha-support"
      prompt:
        instruction: "You are the support assistant for tenant Alpha."
      workspace:
        workdir: "/srv/trpc-claw/workspaces/tenant-alpha"
        allowed_roots:
          - "/srv/trpc-claw/workspaces/tenant-alpha"
      skills:
        roots:
          - "/srv/trpc-claw/skills/tenant-alpha"
        include:
          - "order-support"
          - "refund-policy"
          - "file-review"
      knowledge:
        indexes:
          - "tenant-alpha-faq"
          - "tenant-alpha-product-docs"
        filter:
          tenant: "tenant-alpha"
      tools:
        toolsets:
          - "tenant-alpha-business-tools"
        credential_refs:
          tenant_order_mcp: "tenant-alpha/order-api"
          tenant_knowledge_search: "tenant-alpha/knowledge-api"
      credentials:
        allowed_refs:
          - "tenant-alpha/order-api"
          - "tenant-alpha/knowledge-api"
      isolation:
        mode: "profile_cache"
        agent_cache: true
        toolset_cache: true

    tenant_beta:
      app_name: "tenant-beta-support"
      prompt:
        instruction: "You are the support assistant for tenant Beta."
      skills:
        roots:
          - "/srv/trpc-claw/skills/tenant-beta"
        include: ["tenant-beta-guide"]
      knowledge:
        indexes: ["tenant-beta-faq"]
        filter:
          tenant: "tenant-beta"
      tools:
        include: ["message", "tenant_knowledge_search"]
      isolation:
        mode: "profile_cache"

  selectors:
    - profile_id: "tenant_alpha"
      channels: ["wecom"]
      users: ["replace-with-alpha-userid"]
    - profile_id: "tenant_beta"
      channels: ["wecom"]
      users: ["replace-with-beta-userid"]
    - profile_id: "tenant_alpha"
      tenants: ["tenant-alpha"]
```

`profiles` 是真正的运行期配置。`selectors` 是 profile 的选择规则：
一条规则里的 `channels`、`tenants`、`users`、`sessions`
都会同时匹配才会选中对应 `profile_id`。一旦配置了 selectors，
请求必须命中其中一条规则；显式传入的 profile 也必须和规则选中的
`profile_id` 一致，否则请求会 fail closed，避免用户绕过自己的
profile 边界。没有配置 selectors 时，仍沿用上游 OpenClaw 的
`runtime_profiles.default` 和显式 profile 行为。

selector 按配置顺序匹配，建议把更具体的规则放前面。例如
`channel + user` 规则应放在只有 `tenant` 的兜底规则之前。
`tools.toolsets` 按全局 `tools.toolsets` 里的 `name` 选择工具集合；
它不会自己创建 toolset。若同时配置 `tools.include` / `tools.exclude`，
这些规则会继续按最终暴露给模型的工具名过滤。

从实际接入形态看，常见会有几种用法：

- **少量固定用户**：直接在 `openclaw.yaml` 里写 `users` 到 profile
  的映射，适合 demo、内测机器人、单团队助手。
- **企业微信客服机器人**：用企微回调里的 user id 命中 profile，
  每个租户的 prompt、Skill、知识库、MCP 工具和文件工作区都放在
  profile 下；企微 token、webhook、模型 key 等仍放在全局 channel
  或环境变量里，不要写进 profile 文档示例。
- **AGUI、HTTP 或自研入口**：入口层可以把租户信息放进
  `openclaw.runtime_profile` 扩展里的 `tenant_id`，再用
  `selectors.tenants` 选择 profile；如果同时传了显式 `profile_id`，
  它必须和 selector 选中的 profile 一致。
- **配置中心或 DB 驱动**：大量租户不建议把所有用户列表写进 YAML。
  自定义二进制可以通过 `app.MainWithOptions` 注入
  `runtimeprofile.StoreFunc`，也可以在内网分发版里注册 runtime profile
  provider，从 DB 或配置中心读取 profile，并把业务 token、API 地址、
  知识库索引、工具白名单转成同一套 profile 字段。YAML 只保留默认
  profile 或本地兜底。

自定义二进制的代码形态大致如下，适合已经通过 blank import 注册私有
channel、model、tool 或 toolset 插件的仓库：

```go
package main

import (
	"context"
	"os"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/app"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/runtimeprofile"

	_ "example.com/your/openclaw/plugins/channels/wecom"
	_ "example.com/your/openclaw/plugins/model"
	_ "example.com/your/openclaw/plugins/tools"
)

func main() {
	store := runtimeprofile.StoreFunc(func(ctx context.Context) (
		runtimeprofile.Config,
		error,
	) {
		return loadRuntimeProfiles(ctx)
	})
	os.Exit(app.MainWithOptions(
		os.Args[1:],
		app.WithRuntimeProfileStore(store, true),
	))
}
```

`workspace` 表示这个 profile 可见的业务工作区，例如用户仓库、业务
素材或文件处理目录。用户上传文件、Skill 临时产物和会话状态仍由
`state_dir` 下的 session/scratch 目录管理，避免污染业务仓库。

内网分发版默认内置的 profile 来源是这份 `openclaw.yaml`。如果你维护
自定义二进制，也可以通过 runtime profile provider 把 DB 或配置中心里的
profile 转成同一套 runtime options。

### 3. `model`：默认模型怎么跟环境变量对齐

默认模板的模型部分大致是这样：

```yaml
model:
  mode: "openai"
  name: "${OPENAI_MODEL}"
  base_url: "${OPENAI_BASE_URL}"
```

这里可以这样理解：

- `OPENAI_MODEL`：运行时会先展开到 `model.name`。
- `OPENAI_BASE_URL`：运行时会先展开到 `model.base_url`。
- `OPENAI_API_KEY`：不写进 YAML，运行时直接从环境变量读取。

这样做的好处是：

- `openclaw.yaml` 里不会再偷偷带一个默认模型。
- 启动前只改环境变量，就能切模型，不用每次改
  `openclaw.yaml`。
- `.runtime.env` 或 shell 里已经 `source` 好的环境变量，
  会在启动预处理阶段先被展开，再传给下游模型 SDK。
- 如果环境变量没配，启动会直接报错，问题定位更明确。

### 4. `channels`：企微长连接最关键的配置

默认模板的企业微信部分大致是这样：

```yaml
channels:
  - type: "wecom"
    name: "wecom-ai-websocket"
    config:
      bot_mode: "ai"
      connection_mode: "websocket"
      aibotid: "${WECOM_STREAM_BOT_ID}"
      secret: "${WECOM_STREAM_SECRET}"
      enable_stream: true
      enter_chat_welcome: true
      chat_policy: "open"
      embed_image_url: false
      embed_file_url: false
```

这几个字段建议先这样理解：

- `bot_mode: "ai"`：说明你在接的是智能机器人。
- `connection_mode: "websocket"`：说明走的是长连接，不是 URL 回调。
- `aibotid` / `secret`：企业微信后台给你的核心凭据。
- `enable_stream: true`：允许流式回复。
- `enter_chat_welcome: true`：收到 `enter_chat` 事件时自动回复欢迎
  卡片和快捷入口。
- `chat_policy: "open"`：默认开放，第一次联调最省事。
- `embed_image_url: false` /
  `embed_file_url: false`：优先把真实文件内容拿下来再处理。

如果你将来想限制只有少数同学能聊，再把：

```yaml
chat_policy: "allowlist"
allow_users:
  - "replace-with-your-userid"
```

打开即可。

### 5. `skills`：默认就带上 bundled + local

默认模板里：

```yaml
skills:
  root: "${TRPC_CLAW_STATE_DIR}/skills/bundled"
  extra_dirs:
    - "${TRPC_CLAW_STATE_DIR}/skills/local"
    - "${HOME}/.codex/skills"
    - "./.agents/skills"
  watch: true
  watch_bundled: false
  watch_debounce_ms: 250
  load_mode: "turn"
  coding_agent:
    execution_mode: "host"
    # default_workdir: "/data/projects/myrepo"
    # scratch_root: "${TRPC_CLAW_STATE_DIR}/workspaces/scratch"
```

这几行的意思分别是：

- `skills/bundled`：安装脚本同步下来的默认 skills。
- 这里面现在同时包含 OpenClaw 默认 skills 和
  Anthropic 官方 skills 快照。
- Anthropic skills 统一使用 `anthropic-*` 命名空间，
  用来隔离同名 skill 和降低互相误触发的概率。
- `skills/local`：你自己手工新增的本机 skills。
- `~/.codex/skills`：Codex / skill hub 安装下来的额外 skills。
- `./.agents/skills`：方便项目内跟仓库一起带技能。
- `watch: true`：运行时默认会 watch 这些本地目录，
  新增或修改 skill 后下一轮消息会自动看到。
- `watch_bundled: false`：默认不盯 bundled 目录，减少无意义事件。
- `watch_debounce_ms: 250`：批量写文件时做一次短暂 debounce，
  避免 refresh 风暴。
- `load_mode: "turn"`：每一轮消息都按需重新加载，更稳妥。

`coding_agent.execution_mode` 是一个可选的重执行后端设置。
它不是默认 repo 工作流的第一入口；
默认的本地 repo 检查、改代码、跑构建、跑测试，
还是优先走 `exec_command` 和普通 runtime tools。

可选值：

- `sandbox`：始终留在 sandbox 里跑，最保守。
- `auto`：先 sandbox，遇到限制再尝试 host。
- `host`：优先走宿主机，更适合真实演示写文件和自检流程。

当前默认是：

```yaml
execution_mode: "host"
```

这是故意的。
因为企业微信里演示“让机器人真的改代码、跑命令、落文件”时，
`host` 的体验明显更顺。
如果你希望更严格，再改成 `auto` 或 `sandbox`。

另外两个更偏 Codex 风格的配置是：

- `default_workdir`：显式指定默认代码工作区。
  用户在企微里没指明仓库时，
  `exec_command` 和其他 repo-aware runtime tools
  会优先围绕这里工作。
- `scratch_root`：给临时 toy repo、示例工程、一次性脚本
  预留的 scratch 根目录。

如果你不写 `default_workdir`，
运行时会直接把当前工作目录当成默认代码工作区，
并把 git root / 最近的 `AGENTS.md`
一起放进 prompt 和 tooling guidance。
同时默认会把真正的临时文件导到 `state_dir/runtime/tmp`，
把非源码产物优先导到 `scratch_root/out`，
避免默认把中间文件直接散落到代码目录。

### 6. `tools`：默认把常用能力都打开

默认模板里，工具默认就是开启状态：

```yaml
tools:
  enable_local_exec: true
  enable_openclaw_tools: true
  enable_parallel_tools: true
  providers:
    - type: "envprobe"
    - type: "duckduckgo"
    - type: "webfetch_http"
  toolsets:
    - type: "file"
    - type: "wikipedia"
    - type: "arxivsearch"
```

这代表安装完成后，机器人默认已经能做这些事情：

- 安全检查当前实例能不能看到某个环境变量
- 调本地命令
- 并行调多个工具
- 搜索网页
- 抓网页内容
- 读写本地文件
- 查 Wikipedia
- 查 arXiv

其中 `file` toolset 默认工作目录是 `state_dir`。
如果你想让它直接读写当前项目目录，
可以把 `base_dir` 改成 `"."`。
如果你只想让它看文件不改文件，
可以把：

```yaml
read_only: true
```

打开。

### 7. `session` 和 `memory`：默认就有本地持久化

默认模板里：

```yaml
session:
  backend: "sqlite"
  summary:
    enabled: true
  config:
    path: "${TRPC_CLAW_STATE_DIR}/sessions.sqlite"

memory:
  backend: "sqlitevec"
  fallback_to_sqlite_on_embedding_unsupported: true
  auto:
    enabled: true
  config:
    path: "${TRPC_CLAW_STATE_DIR}/memories_vec.sqlite"
    embedder:
      type: "openai"
      model: "text-embedding-3-small"
```

这意味着：

- 会话历史默认会落到本地 SQLite。
- 会话摘要默认开启，会按阈值自动压缩长对话。
- 长期记忆默认会落到 `sqlitevec`。
- 自动长期记忆抽取默认开启。
- `sqlitevec` 默认会使用 OpenAI-compatible embedding 接口。
- 如果当前 embeddings provider 看起来不支持 `/embeddings`，
  默认会自动降级到 `sqlite` 并打印 warning，
  避免每轮自动长期记忆都反复报错。

如果你只是想先玩最小链路，也可以先把
`session.summary.enabled` 或 `memory.auto.enabled` 手动改成 `false`。
如果你后面想恢复自动摘要或自动长期记忆，再打开它。

### 8. `knowledges`：可选的知识库检索

默认模板里不带 `knowledges` 配置。如果你想让机器人能从向量数据库里检索文档来回答问题，
可以手动加上。`knowledges.providers` 是一个列表，每一项声明一个知识库后端：

```yaml
knowledges:
  providers:
    - name: "docs"
      description: "项目文档和 API 参考"   # 可选，用于自定义工具描述
      max_results: 5                        # 可选，默认 10
      config:
        embedder:
          type: "openai"
          model: "text-embedding-3-small"
          dimensions: 1536
        vector_store:
          type: "inmemory"
```

每个 provider 的核心字段：

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 是 | 知识库名称，多个知识库时会用来生成工具名 |
| `type` | 否 | provider 类型，默认 `builtin`（embedder + vector_store） |
| `description` | 否 | 自定义搜索工具的描述，帮助 LLM 理解何时调用 |
| `max_results` | 否 | 单次搜索最多返回的文档数，默认 10 |
| `config` | 是 | provider 的具体配置，格式取决于 `type` |

builtin 类型的 `config` 下需要配 `embedder` 和 `vector_store`。

**pgvector 示例**（需要先有一个灌好数据的 PostgreSQL 表）：

```yaml
knowledges:
  providers:
    - name: "project_docs"
      description: "检索项目文档、设计文档和 API 参考"
      max_results: 5
      config:
        embedder:
          type: "openai"
          model: "${EMBEDDING_MODEL}"
          dimensions: ${EMBEDDING_DIMENSION}
          base_url: "${OPENAI_BASE_URL}"
          api_key: "${OPENAI_API_KEY}"
        vector_store:
          type: "pgvector"
          host: "${PGVECTOR_HOST}"
          port: ${PGVECTOR_PORT}
          user: "${PGVECTOR_USER}"
          password: "${PGVECTOR_PASSWORD}"
          database: "${PGVECTOR_DATABASE}"
          table: "my_documents"
```

配置了知识库后，OpenClaw 会自动给 Agent 注册搜索工具：

- 单个知识库 → 工具名 `knowledge_search`
- 多个知识库 → 每个知识库生成独立工具，如 `knowledge_search_docs`

`description` 字段的作用是告诉 LLM 这个知识库里有什么内容。
如果不写，默认描述是 `Search for relevant information in the knowledge base.`。
写一个明确的描述可以提高 LLM 主动调用知识库的概率。

### 8. `debug_recorder`：排障时先看这里

默认模板里：

```yaml
debug_recorder:
  enabled: true
  dir: "${TRPC_CLAW_STATE_DIR}/debug"
  mode: "full"
```

这意味着大部分运行时调试信息会落到：

- `~/.trpc-agent-go/openclaw/debug/`

当你遇到下面这些问题时，这个目录很有用：

- 模型怎么理解了这条消息
- Tool 为什么没按预期调用
- Skill 为什么没被选中
- 文件处理链路卡在什么阶段

现在新模板会同时默认打开：

```yaml
observability:
  langfuse:
    enabled: ${LANGFUSE_ENABLED:-true}
    required: ${LANGFUSE_REQUIRED:-false}
    ui_base_url: "${LANGFUSE_UI_BASE_URL:-}"
    trace_url_template: "${LANGFUSE_TRACE_URL_TEMPLATE:-}"
    observation_leaf_value_max_bytes: ${LANGFUSE_OBSERVATION_LEAF_VALUE_MAX_BYTES:-4096}
```

这条链路的职责边界建议这样理解：

- `debug_recorder`：本地原始账本，优先用于排障取证。
- Langfuse：OTel trace backend，用来看整条请求里的模型调用、
  工具调用、耗时和 trace 树。
- admin：只做索引和跳转，不代理 trace 存储本体。

如果你想在 admin 里的 `Debug Sessions` / `Recent Traces`
直接看到每条请求对应的 `trace_id` 和 `View in Langfuse`，
`debug_recorder.enabled` 需要保持开启，
因为这层本地索引就是靠它维护的。

也就是说，`trpc-claw` 不会额外暴露一个给 Langfuse 来“拉”的接口。
它会在进程内把 traces 直接 push 到 `LANGFUSE_HOST`，
admin 再根据 `trace_id` 给你补一条 `View in Langfuse` 的外链。

最小上报配置是这三个变量：

```bash
export LANGFUSE_HOST='langfuse.example.com:443'
export LANGFUSE_PUBLIC_KEY='pk-lf-...'
export LANGFUSE_SECRET_KEY='sk-lf-...'
```

如果还希望 admin 能直接跳到 Langfuse trace，再加其中一种：

```bash
# 推荐：让 OpenClaw 用 project id 自动拼 /project/<id>/traces/<trace_id>
export LANGFUSE_UI_BASE_URL='https://langfuse.example.com'
export LANGFUSE_INIT_PROJECT_ID='project-id'

# 或者：平台直接给完整模板。
export LANGFUSE_TRACE_URL_TEMPLATE='https://langfuse.example.com/project/project-id/traces/{{trace_id}}'
```

如果你没配 Langfuse key，因为模板默认是
`${LANGFUSE_REQUIRED:-false}`，主流程不会被拦住，
但 admin 页面会明确显示 Langfuse 没 ready。

## 当前机器上最重要的几个路径

第一次安装后，最常用的是这几个路径：

- 二进制：`~/.local/bin/trpc-claw`
- 主配置：`~/.trpc-agent-go/openclaw/openclaw.yaml`
- tRPC 配置：`~/.trpc-agent-go/openclaw/trpc_go.yaml`
- 状态目录：`~/.trpc-agent-go/openclaw/`
- debug 目录：`~/.trpc-agent-go/openclaw/debug/`
- Session DB：`~/.trpc-agent-go/openclaw/sessions.sqlite`
- Memory DB：`~/.trpc-agent-go/openclaw/memories_vec.sqlite`
- bundled skills：`~/.trpc-agent-go/openclaw/skills/bundled/`
- local skills：`~/.trpc-agent-go/openclaw/skills/local/`
- lifecycle 目录：`~/.trpc-agent-go/openclaw/runtime/lifecycle/`
- prestart hook：
  `~/.trpc-agent-go/openclaw/hooks/prestart.sh`

`trpc-claw -h` 的第一屏也会直接告诉你当前自动探测到的：

- Binary
- OpenClaw config
- tRPC config
- state_dir

## 常见命令速查

最常见的几条命令如下：

```bash
trpc-claw
trpc-claw doctor
trpc-claw inspect plugins
trpc-claw inspect deps --bundled
trpc-claw inspect config-keys -config ~/.trpc-agent-go/openclaw/openclaw.yaml
trpc-claw bootstrap deps --bundled --apply
trpc-claw upgrade
trpc-claw upgrade --version <tag>
trpc-claw upgrade --channel preview
trpc-claw upgrade -f --profile wecom-ai-websocket
```

它们分别适合在这些场景里用：

- `trpc-claw`：按自动发现到的配置直接启动。
- `trpc-claw doctor`：做基本运行时检查。
- `trpc-claw inspect plugins`：确认当前二进制里编进了哪些插件。
- `trpc-claw inspect deps --bundled`：看默认 bundled skills
  还缺哪些宿主机依赖。
- `trpc-claw inspect config-keys`：看当前版本支持哪些 YAML 键。
- `bootstrap deps --bundled`：自动补默认 bundled skills 的安全依赖。
- `upgrade`：升级到镜像里的最新版本，但保留当前配置。
- `upgrade --version <tag>`：安装指定版本，但保留当前配置。
- `upgrade --channel preview`：显式切到 preview channel。
- `upgrade -f --profile ...`：升级或重置默认模板。

关于 `bootstrap deps`，当前有几个行为值得知道：

- 官方 OpenClaw skill metadata 里的 `brew`、`apt/dnf/yum`、`go`、
  `node/npm`、托管 Python、以及 `download` 型安装动作，
  现在都能进入 plan。
- 显式 `-skill ...` 不会再偷偷夹带默认 dependency profiles。
- `--apply` 是 best-effort：
  能在用户态完成的安装和下载会先做，
  需要 root 的步骤会在最后汇总为 deferred，
  不会第一步就整次退出。
- 下载型 skill 的产物会落在
  `<state_dir>/tools/<skill>/...`。

## 安装、升级、覆盖配置，到底有什么区别

这件事第一次最容易混淆。
你可以这样理解：

### 1. 首次安装

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash
```

效果：

- 安装二进制
- 如果配置文件不存在，就写默认模板
- 如果配置文件已经存在，就保留你当前的配置

### 2. 重新安装并强制覆盖主配置

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- -f --profile wecom-ai-websocket
```

效果：

- 安装或升级二进制
- 强制覆盖 `openclaw.yaml` / `trpc_go.yaml`
- 当前实际运行的主配置会直接换成默认长连接模板

### 3. 只升级二进制，不改当前配置

```bash
trpc-claw upgrade
trpc-claw upgrade --version <tag>
trpc-claw upgrade --channel preview
```

效果：

- 不带 `--version` 时，拉取 stable `latest/VERSION` 的版本
- 带 `--version` 时，直接安装指定版本
- 带 `--channel preview` 时，显式解析 `preview/VERSION`
- 升级或切换二进制版本
- 保留当前配置目录内容

### 4. 用 CLI 升级并顺手把主配置重置回默认模板

```bash
trpc-claw upgrade -f --profile wecom-ai-websocket
```

效果：

- 如果有新版本，先升级二进制
- 即使已经是最新版，也会重跑安装脚本
- 直接覆盖当前机器正在使用的主配置

如果你本机二进制太老，
`trpc-claw upgrade --help` 里还看不到
`-f` / `--force-config` 或 `--profile`，
就先退回安装脚本方式。

## 在线实例的升级和重启怎么做

如果你的目标是“让当前正在跑的企微实例”支持无损升级、
强制升级和重启，推荐继续保留平台 `start.sh`
作为唯一外层入口，并直接从仓库里的
[`start.sh`](start.sh) 样板改起。

```bash
mkdir -p /data/cic/workspace && cd /data/cic/workspace && \
  /app/start.sh > /app/start.log 2>&1
```

这份样板脚本做的是：

- 容器入口仍然是平台 `start.sh`
- `start.sh` 内部始终执行稳定路径的 `trpc-claw`
- 首次启动时如果本机还没有 `trpc-claw`，脚本会自动补装
- 每次拉起子进程前，脚本都会先尝试 `source`
  平台环境文件 `TRPC_CLAW_ENV_FILE`
- 每次拉起子进程前，脚本都会尝试 `source`
  `~/.trpc-agent-go/openclaw/hooks/prestart.sh`
- 当 `trpc-claw` 写出 `intent.env` 并带 lifecycle exit code
  退出时，`start.sh` 负责决定是 restart 还是 upgrade，
  然后重新拉起同一路径上的 `trpc-claw`

如果你只是想快速看脚本骨架，最关键的那行其实就是：

```bash
exec /root/.local/bin/trpc-claw -config /root/.trpc-agent-go/openclaw/openclaw.yaml
```

只不过真正可用的 `start.sh` 还需要把“首次安装、hook、读取
intent.env、升级后重启”这些 supervisor 逻辑补齐；
仓库里的 [`start.sh`](start.sh) 已经把这些默认流程写好了，
并且在注释里留了明确扩展点。
如果你更习惯直接从镜像拿样板，
`latest/start.sh` 和每个 `releases/<version>/start.sh`
也会和 release 一起发布。

这条链路里要区分两类命令：

- `trpc-claw upgrade` 是离线安装命令，适合你在 shell 里手动升级。
- `/runtime ...` 是在线实例控制命令，适合在企微里对“当前运行中的实例”
  发起无损升级、强制升级、无损重启、强制重启。

当前企微侧支持：

- `/help runtime`
- `/runtime`
- `/runtime help`
- `/runtime status`
- `/runtime restart`
- `/runtime restart force`
- `/runtime upgrade`
- `/runtime upgrade force`
- `/runtime upgrade <version>`
- `/runtime upgrade preview`
- `/runtime versions`
- `/runtime changelog`
- `/runtime changelog <version>`

其中：

- `/help runtime` 和 `/runtime help` 会展开运行时控制的完整说明。
- 无损动作会先 drain，等已接收请求处理完再切换。
- 强制动作会直接取消当前任务，再尽快切换。
- 指定版本升级当前只接受 `>= v0.0.48`。
- `/runtime upgrade` 默认只跟随 stable `latest/VERSION`；
  只有显式发送 `/runtime upgrade preview` 才会切到 preview channel。
- `/runtime versions` 会顺带展示每个版本在 release index 里的摘要。
- 升级完成通知会尽量把目标版本的 changelog 摘要一起回给原会话。
- `/status` 和状态卡片会额外展示当前运行版本。
- 如果已配置 `user_identity_lookup_command`，
  或本地已经存在企微身份缓存，
  运行时卡片里的“操作人”会优先展示解析后的英文名 / 账号名。
- 如果当前是企业微信 AI 长连接模式，
  新进程建连成功后会复用发起该动作时持久化下来的 `response_url`，
  补一条“重启 / 升级已完成”的消息；
  这个完成通知当前只支持 `bot_mode: ai` +
  `connection_mode: websocket`。
- `start.sh` 默认会读取
  `~/.trpc-agent-go/openclaw/runtime/lifecycle/intent.env`
  作为 restart / upgrade 的交接文件。
- 运行时面板除了 slash 命令，也可以从企微帮助卡第一页直接点
  `🛠 运行时` 打开。

## 常见问题与排障思路

### 1. 启动就报环境变量缺失

典型报错：

```text
config: env var WECOM_STREAM_BOT_ID is not set
```

说明当前进程环境里没有这个变量。
优先检查：

- 你有没有 `source ~/.bashrc`
- 你是不是通过 `systemd` / `supervisor` 启动的
- 守护进程脚本里有没有真的导出这些变量
- 也可以直接问 agent：
  `你能读到 OPENAI_API_KEY 吗`
  或
  `你能读到 TAIHU_PAT_TOKEN 吗`
  让它先用 `env_probe` 安全确认
  “当前进程里有没有”和
  “只是写进了 shell 文件，还是已经被主进程继承了”

### 2. `inspect plugins` 里没有 `wecom`

这说明当前二进制根本没编进企业微信 Channel。
不要继续折腾 YAML，先换成正确的发行版二进制。

### 3. `mock` 能启动，但企业微信里没回消息

优先看：

- 企业微信后台里是不是拿错了 `bot id` / `secret`
- 日志里有没有持续刷 `wecom websocket: session failed`
- 目标机器是不是能主动连出企业微信长连接地址

### 4. 只收到一条最终回复，看不出流式效果

这不一定是长连接没通。
更常见的是：

- 当前用的是 `mock`，返回太快
- 底层模型没有明显地产生足够长的流式内容
- 企业微信客户端版本太旧

### 5. 文本和文件像是被拆成两次处理

先不要急着改逻辑。
企业微信里文字和附件本来就可能分开发送。
长连接模板里已经把下面这项作为注释示例留好了：

```yaml
aggregate_window: "2s"
```

如果你碰到“文字和文件总是拆成两次处理”，
可以把它取消注释打开。
它的作用就是把短时间内拆开的片段重新聚合。

### 6. 让机器人执行本地二进制时，总觉得不够丝滑

这里要分两层理解：

第一层是 `exec_command` 和普通 runtime tools。
它们默认会继承当前 `trpc-claw` 进程环境。
运行时还会自动把：

- 当前 `trpc-claw` 所在目录
- 当前 `state_dir/tools`
- 当前 `state_dir/toolchain/bin`
- 当前 `state_dir/toolchain/python/bin`
- `~/.local/bin`
- `~/bin`
- 常见用户态工具目录，如 `~/go/bin`、`~/.cargo/bin`
- 以及 `GOBIN`、`GOPATH`、`CARGO_HOME`、`PNPM_HOME`、
  `NODE_HOME`、`N_PREFIX`、`VIRTUAL_ENV`
  这类环境变量推导出的 bin 目录

这些约定 bin 目录不需要在启动前预先创建。
运行时会直接把它们前置到 `PATH`；
如果后面才往这些目录里放新的 binary，
后续工具调用也能直接发现。

如果你的镜像还额外塞了别的前缀目录，
可以直接设置 `TRPC_CLAW_EXTRA_PATH_DIRS`，
按普通 `PATH` 一样用 `:` 分隔多个目录。

前置到 `PATH`。

第二层是“`trpc-claw` 进程自己启动时有没有带上你以为的环境变量”。
真正最常见的问题，通常是这里。
如果你的服务是被守护进程拉起来的，
要确保那个启动方式也加载了你预期的 PATH 和业务环境变量。

### 7. `coding-agent` 想更严格地受限（可选）

把：

```yaml
skills:
  coding_agent:
    execution_mode: "host"
```

改成：

- `auto`
- 或 `sandbox`

就可以。

如果你的目标是“在企微里更稳定地演示真实 coding 流程”，
保留 `host` 往往更合适。
如果你的目标是“严格限制写文件和命令权限”，
就改成 `sandbox`。

### 8. 企业微信里按聊天切换代码工作区

企业微信 AI bot 现在支持这些 slash 命令：

- `/workspace <目录>`：把当前聊天的代码工作区切到这个目录
- `/workspace off`：清掉当前聊天覆盖，恢复运行时默认工作区

这和全局配置的区别是：

- `skills.coding_agent.default_workdir` 是进程级默认值
- `/workspace ...` 是当前企微聊天会话级覆盖

实际推荐用法是：

1. `trpc-claw` 从一个常用 monorepo 或工作目录启动
2. 把这个目录作为默认 coding workspace
3. 某个聊天临时要切到别的仓库时，再发 `/workspace /path/to/repo`

这样用户在企微里直接说“帮我改下这个仓库里的接口”时，
模型既能拿到默认 repo 上下文，
也不会把工作区信息无差别塞进普通图片/PDF对话里；
只有明显是代码任务，或者你显式设了 `/workspace`，
才会附带运行时代码工作区提示。

还有一个容易踩的点：

- 默认 `fs_*` 文件工具仍然只认它自己的 `base_dir`
  （模板里默认是 `state_dir`）
- 它不会因为默认 workspace 或 `/workspace`
  自动切到任意 repo
- 它不是任意 repo 浏览器

所以做代码仓库任务时，
运行时提示会优先把模型往 `exec_command`
和其他 repo-aware runtime tools 上引，
而不是让它误以为 `fs_read_file`
可以临时切到任何仓库目录。

## 如果你必须走 webhook 或通知机器人

虽然本文主线是长连接，但仓库里其实已经给了三份企微模板：

- [`openclaw.wecom.ai.websocket.yaml`](openclaw.wecom.ai.websocket.yaml)
- [`openclaw.wecom.ai.yaml`](openclaw.wecom.ai.yaml)
- [`openclaw.wecom.notification.yaml`](openclaw.wecom.notification.yaml)

选择建议：

- 想最快跑通：优先 `wecom-ai-websocket`
- 必须走 URL 回调：用 `wecom-ai`
- 只做通知机器人：用 `wecom-notification`

如果你改走 webhook 回调，还需要额外准备：

- `WECOM_TOKEN`
- `WECOM_ENCODING_AES_KEY`
- 回调路径
- 一个企业微信后台可访问的回调地址

这条路线要处理太湖域名、URL 校验和连通性，
明显比长连接更复杂，所以不放在本文主线里展开。
需要时直接看 [`channel/wecom/runbook.md`](channel/wecom/runbook.md)。

## 从源码运行

如果你不是安装预编译版，而是要在仓库里直接跑源码，
先记住两件事：

1. `openclaw/` 是独立 Go module，要先 `cd openclaw`
2. 默认模板启用了 `sqlitevec`，
   源码态运行时建议带上 `openclaw_sqlitevec` build tag

如果本机还没有 SQLite 开发头文件，先安装：

- macOS：`brew install sqlite`
- Debian / Ubuntu：`apt install libsqlite3-dev`
- RHEL / CentOS：`yum install sqlite-devel`

然后可以这样跑：

```bash
cd openclaw

go run -tags openclaw_sqlitevec ./cmd/openclaw inspect plugins

go run -tags openclaw_sqlitevec ./cmd/openclaw \
  -config ./openclaw.wecom.ai.websocket.yaml \
  -mode mock
```

等 `mock` 跑通后，再切真实模型：

```bash
go run -tags openclaw_sqlitevec ./cmd/openclaw \
  -config ./openclaw.wecom.ai.websocket.yaml \
  -mode openai \
  -model gpt-5
```

## 目录结构与共建入口

如果你接下来不只是“使用”，还想“共建”，最常看的目录是这些：

- `openclaw/cmd/openclaw`
  内网分发版二进制入口
- `openclaw/channel`
  Channel 实现目录，例如企业微信 `channel/wecom`
- `openclaw/plugins`
  Go 插件目录
- `openclaw/skills`
  文件型技能目录

### Go 插件是怎么工作的

Go 不是运行时热加载 Go 代码的语言。
所以这里的“插件”本质上是编译期注册：

1. 某个插件包在 `init()` 里调用注册函数
2. `cmd/openclaw` 用空白导入把这个包编进二进制
3. 运行时根据 YAML 里的 `type` 找到对应 factory

如果你想确认“当前二进制里到底编进了哪些插件”，
最直接的命令就是：

```bash
trpc-claw inspect plugins
```

### 如何共建一个新的 Channel 插件

推荐把新 Channel 放到 `openclaw/plugins/<type>/`，
或者按现有风格放到 `openclaw/channel/<type>/`。

最小注册骨架如下：

```go
package mychannel

import (
  "context"

  occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
  "trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
)

const typeName = "mychannel"

func init() {
  if err := registry.RegisterChannel(typeName, newChannel); err != nil {
    panic(err)
  }
}

func newChannel(
  deps registry.ChannelDeps,
  spec registry.PluginSpec,
) (occhannel.Channel, error) {
  _ = deps
  _ = spec
  return &channel{}, nil
}

type channel struct{}

func (c *channel) ID() string { return typeName }

func (c *channel) Run(ctx context.Context) error {
  <-ctx.Done()
  return nil
}
```

然后在 `openclaw/cmd/openclaw/main.go` 里加一行空白导入，
再在 YAML 里启用它。

### 如何共建一个新的 Skill

Skill 是文件夹，不一定要写 Go。
最小结构通常就是：

```text
openclaw/skills/<skill-name>/
  SKILL.md
  scripts/
    run.sh
```

如果你只是想扩展一两个场景能力，
从 Skill 入手通常比改 Go 插件更轻。

仓库里已经有不少可参考的真实技能目录，例如：

- `openclaw/skills/github/`
- `openclaw/skills/weather/`
- `openclaw/skills/coding-agent/`

## 下一步建议怎么做

如果你是第一次接手这套分发版，推荐顺序如下：

1. 按本文把企微长连接 + `mock` 跑通
2. 再切真实模型
3. 跑 `inspect deps` + `bootstrap deps`
4. 在企业微信里验证文本、图片、PDF、Excel
5. 再根据需要调整 `persona`、`tools`、`skills`
6. 最后才去碰 webhook、通知机器人、或者自定义插件

做到这一步，你基本已经把“能装、能起、能聊、能执行、能看调试信息”
这一整条链路跑顺了。
