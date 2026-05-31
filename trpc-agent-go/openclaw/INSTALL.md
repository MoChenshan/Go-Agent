# trpc-claw 一键安装

`trpc-claw` 的预编译包发布在腾讯云镜像的 generic 仓库里。

默认安装入口：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash
```

如果你想在 Linux 上直接构建一个本地的“预装 bundled skill
依赖”的 `trpc-claw` 容器，
看 [`docker/README.md`](docker/README.md)。

如果你想先看各个版本主要新增了什么能力，
可以直接看 [`CHANGELOG.md`](CHANGELOG.md)。

默认安装会选择 `wecom-ai-websocket` 模板，也就是安装完成后的
`openclaw.yaml` 默认内容来自
`openclaw.wecom.ai.websocket.yaml`。

这份默认模板里的 `model.name` / `model.base_url`
直接写成了 `OPENAI_MODEL` / `OPENAI_BASE_URL`
环境变量引用；
启动时会先展开它们，没设置就会直接报错。
企业微信参数仍然优先读取 `WECOM_STREAM_*` 环境变量。
如果你走 webhook 回调模式，再额外使用 `WECOM_*`。
推荐先把这些变量写进 `~/.bashrc` 或 `~/.zshrc`，
再启动 `trpc-claw`。
默认模板也已经显式开启：

- `agent.instruction = "You are a helpful assistant."`
- `agent.system_prompt = "You are tRPC-Claw."`
- `tools.enable_local_exec = true`
- `tools.enable_parallel_tools = true`
- `tools.providers` 已默认打开 `duckduckgo`、`webfetch_http`
- `tools.toolsets` 已默认打开 `file`、`wikipedia`、`arxivsearch`
- `memory.backend = "sqlitevec"`
- `memory.fallback_to_sqlite_on_embedding_unsupported = true`

如果你只是想在本地终端先体验 mock / stdin，显式切成 `mock`：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --profile mock
```

如果你要直接安装 WebSocket 长连接模板：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --profile wecom-ai-websocket
```

也支持另外两个显式模板：

```bash
# 企业微信 webhook AI
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --profile wecom-ai

# 企业微信 notification
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --profile wecom-notification

# 微信私聊
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --profile weixin
```

指定版本：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --version v0.0.46
```

## 安装后会放到哪里

默认会写入：

- 二进制：`~/.local/bin/trpc-claw`
- 主配置：`~/.trpc-agent-go/openclaw/openclaw.yaml`
- tRPC 配置：`~/.trpc-agent-go/openclaw/trpc_go.yaml`
- 配置模板目录：`~/.trpc-agent-go/openclaw/profiles/`
- bundled skills：`~/.trpc-agent-go/openclaw/skills/bundled/`
- 自定义 skills 目录：`~/.trpc-agent-go/openclaw/skills/local/`
- 外部 skills 目录：`~/.codex/skills/`（存在时自动补进来）

其中 `bundled/` 默认同时带上：

- OpenClaw 自带 skills
- Anthropic 官方 skills 快照

Anthropic 那组 skill 统一加了 `anthropic-` 前缀，
例如 `anthropic-docx`、`anthropic-pdf`。
这样可以避开同名 skill 冲突，也更容易和用户自己的 skill
区分开。

如果这些文件已经存在，安装脚本默认不会覆盖。
如需覆盖，可加 `-f`（等价于 `--force-config`）。
如果你想强制把当前机器上的 `openclaw.yaml` /
`trpc_go.yaml` 切回最新版默认模板，
直接重新安装时带上：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- -f
```

安装脚本现在只保留 `trpc-claw`。
如果你之前装过旧版本，升级时会尽量清理同目录下由安装脚本生成的
旧 `openclaw` alias。

## 安装、升级、覆盖配置的区别

最容易混淆的是这四种动作：

1. 首次安装：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash
```

效果：

- 安装二进制
- 如果配置文件不存在，则写入默认配置
- 如果配置文件已存在，则保留现有配置

2. 重新安装并强制覆盖配置：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- -f
```

效果：

- 安装或更新二进制
- 强制覆盖 `openclaw.yaml` / `trpc_go.yaml`
- 默认覆盖成 `wecom-ai-websocket`

3. 只升级二进制，不改当前配置：

```bash
trpc-claw upgrade
trpc-claw upgrade --version <tag>
trpc-claw upgrade --channel preview
```

效果：

- 不带 `--version` 时，拉取镜像里 stable `latest/VERSION` 的版本
- 带 `--version` 时，直接安装指定版本
- 带 `--channel preview` 时，显式解析 `preview/VERSION`
- 升级或切换二进制版本
- 保留当前配置目录内容

4. 用 CLI 升级并强制把配置切回某个默认模板：

```bash
trpc-claw upgrade -f
trpc-claw upgrade -f --profile wecom-ai-websocket
trpc-claw upgrade -f --profile mock
trpc-claw upgrade -f --profile weixin
```

效果：

- 如果有新版本，先升级二进制
- 即使已经是最新版，也会重跑安装脚本
- 强制覆盖 `openclaw.yaml` / `trpc_go.yaml`
- `--profile` 决定覆盖成哪个默认模板

可以简单理解成：

- `install.sh` 是“完整安装器”
- `trpc-claw upgrade` 是“CLI 包装过的安装器”
- `-f` / `--force-config` 的语义始终是“允许默认模板覆盖现有主配置”

## 怎么升级

如果你已经装过 `trpc-claw`，后续想直接升级到镜像里的最新版本，
可以运行：

```bash
trpc-claw upgrade
```

这个命令会做两件事：

1. 读取 `latest/VERSION`，检查当前是否有更新版本。
2. 如果有新版本，复用安装脚本把二进制升级到最新版，
   同时保留当前配置目录，不主动覆盖已有配置。

如果你已经知道想切到哪个版本，也可以直接指定：

```bash
trpc-claw upgrade --version <tag>
```

这会跳过 latest 版本探测，直接复用安装脚本把当前二进制切到
指定版本，同时继续保留现有配置目录。

如果你想主动切到 preview channel，可以显式指定：

```bash
trpc-claw upgrade --channel preview
```

默认升级不会读取 `preview/VERSION`。

如果你想在 CLI 里直接把默认模板重新覆盖回
`~/.trpc-agent-go/openclaw/openclaw.yaml` /
`trpc_go.yaml`，现在也支持：

```bash
trpc-claw upgrade -f
```

这会在“已经是最新版”的情况下也重新跑一次安装脚本，
并用默认 profile（`wecom-ai-websocket`）覆盖主配置。

如果你想显式指定要覆盖成哪个模板：

```bash
trpc-claw upgrade -f --profile wecom-ai-websocket
trpc-claw upgrade -f --profile mock
```

注意：`--profile` 只有和 `-f` / `--force-config` 一起用时才会生效，
否则升级默认还是保留你当前机器上的已有配置。

如果你的目标非常明确，就是把当前机器的主配置切回
“默认长连接模板”，最直接的命令就是：

```bash
trpc-claw upgrade -f --profile wecom-ai-websocket
```

它最终会覆盖这两个文件：

- `~/.trpc-agent-go/openclaw/openclaw.yaml`
- `~/.trpc-agent-go/openclaw/trpc_go.yaml`

也就是说，当前实际运行时使用的主配置会被直接换掉，
不是只更新 `profiles/` 目录里的模板副本。

如果你当前机器上的 `trpc-claw` 版本比较老，
`trpc-claw upgrade --help` 里还看不到
`-f` / `--force-config` / `--profile`，
那说明本机二进制还没带这条能力。
这种情况下，先用安装脚本兜底：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- -f --profile wecom-ai-websocket
```

等你装到带这条能力的新版本之后，
后面就可以直接用 `trpc-claw upgrade -f ...`。

启动 `trpc-claw` 服务时，也会顺手做一次轻量的最新版检查。
如果检测到新版本，日志里会提示你执行 `trpc-claw upgrade`。

如果你不想在启动时做这个检查，可以关闭：

```bash
export TRPC_CLAW_DISABLE_UPGRADE_CHECK='1'
```

## 怎么运行

### 直接前台运行当前二进制

```bash
trpc-claw
```

当前入口会自动发现：

- `~/.trpc-agent-go/openclaw/openclaw.yaml`
- `~/.trpc-agent-go/openclaw/trpc_go.yaml`

启动日志里会打印实际使用的配置路径和 `state_dir`。
如果你不确定当前到底在吃哪份配置，
先看启动日志里的：

- `Config:   tRPC = ...`
- `Config:   OpenClaw = ...`
- `Config:   state_dir = ...`

其中 `OpenClaw = ...` 指向的就是当前实际生效的
`openclaw.yaml`。

### 作为受控运行时入口

如果你要的是“当前在线实例支持无损升级 / 重启”，
推荐继续保留平台 `start.sh` 作为唯一外层入口，
并直接从仓库里的 [`start.sh`](start.sh) 样板改起。

```bash
mkdir -p /data/cic/workspace && cd /data/cic/workspace && \
  /app/start.sh > /app/start.log 2>&1
```

这份样板脚本默认会做这些事：

- 如果 `trpc-claw` 不存在，先自动安装到稳定路径
- 每次拉起子进程前先尝试 `source`
  `TRPC_CLAW_ENV_FILE`
- 每次拉起子进程前先尝试 `source`
  `~/.trpc-agent-go/openclaw/hooks/prestart.sh`
- 当 `trpc-claw` 写出
  `~/.trpc-agent-go/openclaw/runtime/lifecycle/intent.env`
  并带 lifecycle exit code 退出时，`start.sh`
  负责 restart / upgrade 并重新拉起同一路径上的 `trpc-claw`

也就是说，平台继续拥有 supervisor 角色，
`trpc-claw` 继续是唯一业务 binary。
样板脚本既维护在仓库里的 [`start.sh`](start.sh)，
也会随 release 发布到镜像的 `latest/start.sh`
和 `releases/<version>/start.sh`。

如果你需要自定义路径，也支持这几个环境变量：

- `TRPC_CLAW_PRESTART_HOOK`
- `TRPC_CLAW_RELEASE_BASE_URL`
- `TRPC_CLAW_INITIAL_VERSION`
- `TRPC_CLAW_ENV_FILE`
- `TRPC_CLAW_WORKSPACE_DIR`

这条链路里需要特别区分：

- `trpc-claw upgrade` 是离线安装命令，适合 shell 里手动升级。
- `/runtime ...` 是在线实例控制命令，适合在企微里对当前运行中的实例
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

其中无损动作会先 drain 已接收请求；
强制动作会直接取消当前任务。
`/help runtime` 和 `/runtime help`
会展开运行时控制的完整说明。
指定版本升级当前只允许 `>= v0.0.48`。
默认升级只跟随 stable `latest/VERSION`；
只有显式发送 `/runtime upgrade preview` 才会切到 preview channel。
`/runtime versions`
会顺带展示每个版本在 release index 里的摘要。
升级完成通知会尽量把目标版本的 changelog 摘要一起回给原会话。
`/status` 和状态卡片会额外展示当前运行版本。
如果已经配置 `user_identity_lookup_command`，
或本地已有企微身份缓存，
运行时卡片里的“操作人”会优先展示解析后的英文名 / 账号名。
如果当前实例跑在企业微信 AI 长连接模式下，
新进程建连成功后会复用发起该动作时持久化下来的 `response_url`，
再补一条“重启 / 升级已完成”的消息；
这个完成通知当前只支持 `bot_mode: ai` +
`connection_mode: websocket`。

默认 profile 也已经显式开启：

- `skills.root = ${TRPC_CLAW_STATE_DIR}/skills/bundled`
- `skills.extra_dirs` 包含
  `${TRPC_CLAW_STATE_DIR}/skills/local`
  和 `${HOME}/.codex/skills`
- `agent.persona = "default"`
  可以直接改成 `friendly`、`pragmatic`、
  `professional`、`concise`、`coach`、`creative`、
  `candid`、`quirky`、`nerdy`、`snarky`、
  `girlfriend`、`off`
  `off` 表示关闭 persona preset，只保留
  `instruction` / `system_prompt`
- `skills.coding_agent.execution_mode = "host"`
  可选 `sandbox`、`auto`、`host`
- 如果不显式写 `default_workdir`，
  运行时会直接把当前工作目录当成默认 coding workspace
- 临时文件默认会走 `state_dir/runtime/tmp`，
  非源码产物默认建议写到 `scratch_root/out`

所以装完后 bundled skills 会直接生效。
默认工具也已经直接能用：

- `exec_command`
- `duckduckgo_search`
- `web_fetch`
- `fs_*`
- `wiki_*`
- `arxiv_*`

其中默认模板为了先把能力放出来，
会把 `webfetch_http` 配成 `allow_all_domains: true`，
也会把 `file` toolset 配成可写，
默认根目录就是 `state_dir`。
如果你想收紧权限，直接改模板里对应注释即可。

如果你要新增自己的 skill，优先放到
`~/.trpc-agent-go/openclaw/skills/local/`，
不要直接修改 `bundled/` 里的官方内容，
这样后续升级不会把你的自定义 skill 冲掉。
当你想让机器人长期学会某个可复用能力时，也优先走这条路径：
把工作流、工具/API/MCP 调用方式、团队流程或领域规则沉淀成
local skill，让 skill 描述何时触发、如何执行、遇到失败怎么恢复。
轻量事实、偏好和简单常驻规则仍然放到 memory；
只有需要可执行流程、工具、示例、参考资料或失败恢复时才创建 skill。
运行时代码仍然只负责权限、密钥、文件访问、校验和生命周期这些
稳定边界。
默认 watcher 开着时，
把 skill 放进 `skills/local/` 后，
下一轮消息会自动感知；
如果你显式关闭了 `skills.watch`，
就需要走 admin refresh。
如果你是通过 Codex / skill hub 装额外技能，
默认也会自动把 `~/.codex/skills/` 带进来。

如果你的 `PATH` 里还没有 `~/.local/bin`，安装脚本也会在最后提示。
此外，运行时的 `exec_command`
和普通 runtime tools
会默认继承当前 `trpc-claw` 进程环境，
并自动把当前 `trpc-claw` 所在目录、
当前 `state_dir/tools`、
当前 `state_dir/toolchain/bin`、
当前 `state_dir/toolchain/python/bin`、
`~/.local/bin`、`~/bin`、`~/go/bin`、
`~/.cargo/bin`，以及
`GOBIN`、`GOPATH`、`CARGO_HOME`、
`PNPM_HOME`、`NODE_HOME`、`N_PREFIX`、
`VIRTUAL_ENV` 这类环境变量推导出的 bin 目录前置到 `PATH`。
这些约定 bin 目录不需要在启动前预先创建；
运行时会直接把它们带进 `PATH`，
后面再往目录里放新的 binary 也能被发现。
如果你的镜像还有额外的自定义前缀，
可以通过 `TRPC_CLAW_EXTRA_PATH_DIRS`
按 `PATH` 一样用 `:` 分隔追加进去。
对 skills，默认模板还会自动生成一段
`skills.tooling_guidance`：
- 把 skills 明确成 discovery / knowledge-loading 入口
- 提示模型先 `skill_load`，需要精确参数时继续读 skill docs
- 强调正常 repo 执行、构建、测试、验证继续走
  `exec_command` 和普通 runtime tools
如果你自己显式填写 `skills.tooling_guidance`，
这段快捷配置就会不再注入。
如果你走企业微信 AI bot，
还可以在聊天里直接发 `/workspace <目录|off>`，
按聊天维度临时覆盖默认代码工作区。
默认 `fs_*` 文件工具并不会跟着 `/workspace`
自动切到任意 repo；
它仍然受自己的 `base_dir` 限制。
所以代码仓库任务默认更推荐
`exec_command` 和普通 repo-aware runtime tools。
如果你是用守护进程或服务管理器拉起 `trpc-claw`，
记得在启动脚本里也显式加载这些环境变量，
不要只写在交互 shell 配置里。

## 常用 deps

GitHub 版 OpenClaw 的依赖检测 / 安装命令，
内网 `trpc-claw` 也支持。
安装脚本默认只提示，不会自动改系统依赖；
如果你明确想在安装后立即执行，
可以给安装脚本加 `--bootstrap-deps`。

先检查 bundled skills 缺哪些依赖：

```bash
trpc-claw inspect deps --bundled
```

安装 bundled skills 的默认安全依赖：

```bash
trpc-claw bootstrap deps --bundled --apply
```

如果最后只剩系统包步骤需要提权，
优先直接执行输出里那条 `sudo ... install ...`。
如果你习惯整条命令重新加 `sudo`，
现在也会优先回看原用户安装目录下的 bundled skills，
不会再因为 `HOME` 切到 `/root` 就直接报找不到 skills root。

这套命令会复用上游 OpenClaw 的跨平台依赖逻辑：

- macOS 优先走 `brew`
- Linux 会按机器实际情况选择 `apt` / `dnf` / `yum`
- Python 相关依赖会放到 `state_dir` 下的托管环境里

同时也保留一层保守边界：

- 会自动处理系统包和托管 Python 包
- 不会默认帮你拉浏览器 runtime
- 不会默认做全局 npm 安装
- 不会碰账号凭据和登录态

如果你是源码态自己 `go run -tags openclaw_sqlitevec`，
还要确保本机能给 CGO 提供 SQLite 头文件：

- macOS：`brew install sqlite`
- Debian / Ubuntu：`apt install libsqlite3-dev`
- RHEL / CentOS：`yum install sqlite-devel`

如果你想在安装时就顺手做一次 bootstrap：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --bootstrap-deps
```

## 推荐的环境变量写法

建议把模型和企业微信相关参数放到 shell 启动文件里，而不是
直接写死在 `openclaw.yaml` 里。当前默认模板已经按这个方式
预留好了。

推荐写进 `~/.bashrc`：

```bash
export OPENAI_MODEL='gpt-5.2'
export OPENAI_API_KEY='replace-with-your-api-key'
export OPENAI_BASE_URL='https://your-openai-compatible-endpoint/v1'

# 默认 websocket 长连接模板需要：
export WECOM_STREAM_BOT_ID='replace-with-your-aibotid'
export WECOM_STREAM_SECRET='replace-with-your-aibot-secret'
# 群聊里按整个群共享历史；改成 isolated 可切成群内按用户隔离。
export WECOM_GROUP_SESSION_MODE='shared'
# export WECOM_STREAM_WS_URL='wss://openws.work.weixin.qq.com'

# webhook 模式再补这些：
# export WECOM_TOKEN='replace-with-your-token'
# export WECOM_ENCODING_AES_KEY='replace-with-your-43-char-key'
# export WECOM_AI_CALLBACK_PATH='/wecom/ai/callback'

# notification 模式再补这些：
# export WECOM_NOTIFICATION_CALLBACK_PATH='/wecom/notification/callback'
# export WECOM_WEBHOOK_URL='https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...'

# 可选：
# export WECOM_CORP_ID='ww1234567890'
# export WECOM_AGENT_ID='1000002'
# export WECOM_BOT_NAME='OpenClaw'
```

写完后重新加载：

```bash
source ~/.bashrc
```

如果你想直接确认当前实例能不能看到某个变量，
现在也可以直接问 agent，例如：

```text
你能读到 OPENAI_API_KEY 吗
你能读到 TAIHU_PAT_TOKEN 吗
```

默认模板现在会自动启用 `env_probe`。
它会安全检查当前进程、`TRPC_CLAW_ENV_FILE`、
`runtime/env.sh`、`~/.bashrc`、`~/.zshrc`、
`~/.profile` 这些受信任来源，但不会暴露变量值。
如果它在这些文件里检测到简单静态声明，
还会把变量补进当前 `trpc-claw` 进程环境，
让后续新的 `exec_command`
和普通 runtime tools
可以直接使用。

## 切换到企业微信

默认安装后已经是企业微信 AI WebSocket 长连接模板。

如果你之前切到了别的 profile，可以再切回来：

```bash
cp ~/.trpc-agent-go/openclaw/profiles/openclaw.wecom.ai.websocket.yaml \
  ~/.trpc-agent-go/openclaw/openclaw.yaml
vim ~/.trpc-agent-go/openclaw/openclaw.yaml
```

如果你要切回 webhook 模式模板：

```bash
cp ~/.trpc-agent-go/openclaw/profiles/openclaw.wecom.ai.yaml \
  ~/.trpc-agent-go/openclaw/openclaw.yaml
vim ~/.trpc-agent-go/openclaw/openclaw.yaml
```

然后按
[channel/wecom/runbook.md](./channel/wecom/runbook.md)
完成企业微信后台配置。

如果直接启动时报类似
`config: env var WECOM_STREAM_BOT_ID is not set`，
说明当前 shell 还没有加载 WebSocket 模式需要的环境变量。

如果你用的是 WebSocket 长连接模板，
`WECOM_TOKEN` / `WECOM_ENCODING_AES_KEY` 不是建连必填项。
它们只用于 webhook 回调模式。

## 支持的平台

正式发布包默认会同时构建：

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

所有这些包都会开启 `CGO_ENABLED=1`，并带上 `sqlite`
memory backend。

## 常用安装参数

切回本地 mock：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --profile mock
```

自定义安装目录：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- \
      --bin-dir "$HOME/bin" \
      --config-dir "$HOME/.config/openclaw"
```

显式安装 preview channel：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --channel preview
```

## 下载逻辑

安装脚本会做下面几件事：

1. 从 `latest/VERSION` 读取最新版本号；
   如果显式传了 `--channel preview`，则读取 `preview/VERSION`。
2. 根据当前机器的 `OS/ARCH` 选择正确的压缩包。
3. 下载对应版本的 `checksums.txt` 并校验包完整性。
4. 解压并写入二进制、配置和模板。

其中默认主配置会选
`profiles/openclaw.wecom.ai.websocket.yaml`，
所以装完就可以直接去改企业微信参数。

默认 base URL 是：

`https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw`

如果你要验证其他镜像路径，也可以自己覆盖：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- \
      --base-url 'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw'
```
