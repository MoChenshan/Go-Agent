# trpc-claw 版本变更记录

本文件记录 `openclaw/` 内网分发版每个发布版本对用户可见的
主要变化。

维护约定：

- 按版本倒序追加。
- 版本标题统一写成 `## v0.0.71 (2026-04-14)` 这种格式。
- 只记录用户可感知的能力、兼容性修复和行为变化。
- 从 `v0.0.5` 开始整理，这是当前腾讯云镜像发布线的起点。

## v0.0.98 (2026-05-28)

- Langfuse trace 展示补齐 OpenClaw 入口层的 `name`、`input` 和
  `output` 字段。企微和微信请求现在会把用户输入、最终回复和稳定
  trace name 写到 Langfuse 顶层，减少 trace 详情里出现 `null` 或
  默认名字的情况。
- Langfuse trace name 按 `CLAW_ID` 和入口类型生成更稳定的名称，
  例如 `claw_xxx-wecom-person`、`claw_xxx-wecom-group` 和
  `claw_xxx-wechat`。企微私聊和群聊会按明确的会话类型区分，避免
  私聊被错误标记成 group。
- 企微私聊的 Langfuse session 展示会优先使用可读的用户标签，
  同时把原始传输 session id 保留在 trace metadata 中，方便排查
  底层通道问题。
- 定时任务和 gateway 入口会显式标记 Langfuse root observation，
  让 UI 优先展示 OpenClaw 业务入口 span，而不是外层运行时 span。
- preview / release 安装链路修正了 channel 默认值。
  发布到 preview channel 的 `install.sh` 和 `start.sh` 会默认留在
  preview，避免显式测试 preview 时被启动脚本回退到 latest。
- 兼容性与升级注意：
  这次不改变安装入口、配置文件路径或 profile 名称。已经配置
  Langfuse 的环境升级后会看到 trace 字段展示变更；没有启用
  Langfuse 的环境不受影响。

## v0.0.97 (2026-05-27)

- subagent 后台任务现在复用上游 `agent/taskrun` 运行时。
  OpenClaw 侧继续保留 subagent 产品术语，并把实现边界收敛到企微投递、
  通知、命令、权限和当前会话适配，减少 OpenClaw 自维护后台任务逻辑。
- agent 侧继续通过 `subagents_spawn`、`subagents_list`、
  `subagents_get`、`subagents_cancel` 和 `subagents_wait` 管理后台任务。
  `async`、`sync`、`review` 这几种等待语义保持面向用户的 subagent
  体验，同时底层运行、状态、取消和等待流程统一落到 `taskrun`。
- 企微新增 `/subagents` 管理命令和帮助主题。
  用户可以在当前会话里查看后台 subagent 列表、查询单个 run 详情、
  取消仍在运行的任务；命令只作用于当前用户和当前会话，避免跨会话误操作。
- OpenClaw 默认使用 `agent/taskrun/inprocess` 的文件存储记录后台任务状态，
  运行数据落在 `state_dir/subagents/runs.json`。
  进程重启后，未进入终态的历史任务会被标记为失败，避免已经中断的任务
  在用户侧继续显示为运行中。
- 新增 `SUBAGENT_RUNTIME.md`，说明 agent 侧工具调用、业务代码直接使用
  `taskrun.Controller` / `inprocess.Service`、以及 OpenClaw 进程内
  `Runtime.SubagentService()` 查询和取消后台任务的推荐方式。
- 默认 profile 模板补充
  `context_compaction_oversized_tool_result_max_tokens: 8192`。
  超大工具结果在进入上下文压缩前会先被限制到更可控的大小，降低长任务、
  浏览器操作和文档处理场景里单个工具结果挤占上下文窗口的概率。
- 兼容性与升级注意：
  这次不改变安装入口、镜像目录结构、配置文件路径或已有 profile 名称。
  已有会话里由旧实现管理的非终态 subagent 不会跨版本恢复为继续运行；
  升级后新产生的后台任务会按 `taskrun` 状态模型持久化和展示。

## v0.0.96 (2026-05-26)

- Runtime profile 支持按 channel、tenant、user 和 session 选择 profile。
  同一个 `trpc-claw` 实例可以为不同用户或租户配置不同 prompt、
  workspace、skill、knowledge、tool 和隔离策略，满足多租户部署下复用
  同一套运行能力的需求。
- 配置文件新增 `runtime_profiles.selectors` 示例。
  selector 命中后会把请求路由到对应 `profile_id`；一旦配置
  selectors，请求必须命中其中一条规则，显式传入的 profile 也必须和
  规则选中的 `profile_id` 一致，否则请求会 fail closed，避免用户绕过
  自己的 profile 边界。
- 自定义二进制可以通过上游 `app.MainWithOptions` 注入
  `runtimeprofile.StoreFunc` 或自定义 resolver，从 DB、配置中心等来源
  加载 profile；内网分发版仍保留 runtime profile provider 扩展点。
- Langfuse 默认配置改为环境变量驱动。
  平台侧只需要注入 `LANGFUSE_HOST`、`LANGFUSE_PUBLIC_KEY` 和
  `LANGFUSE_SECRET_KEY`，就可以让 `trpc-claw` 自动把 trace 上报到
  Langfuse；用户不需要手工编辑 `openclaw.yaml`。
- 前端和 admin 链接可以通过 `LANGFUSE_UI_BASE_URL`、
  `LANGFUSE_INIT_PROJECT_ID` 或 `LANGFUSE_TRACE_URL_TEMPLATE` 配置。
  Docker 本地启动脚本会在容器内改写本机 `LANGFUSE_HOST` 时保留
  浏览器可访问的 UI 地址，避免生成不可点击的容器内链接。
- 兼容性与升级注意：
  没有配置 `runtime_profiles.selectors` 时，仍然沿用上游 OpenClaw 的
  runtime profile 行为；已经存在的 `runtime_profiles.profiles`、
  `default`、`required` 和 `fallback_to_default` 配置不需要迁移。
  Langfuse 默认 `enabled=true`、`required=false`；缺少 Langfuse
  上报环境变量时不会阻塞主流程，需要强制上报的托管环境可以显式设置
  `LANGFUSE_REQUIRED=true`。

## v0.0.95 (2026-05-18)

- WeCom AI 请求现在会把本轮服务端接收到请求时的当前时间注入模型上下文。
  用户询问“现在几点”“半小时后提醒我”这类依赖当前时间的任务时，模型
  不再只能依赖自身过期的时间判断。
- 定时任务工具新增相对延迟调度参数。
  模型可以使用 `after`、`delay`、`after_ms` 或 `delay_ms` 创建一次性
  提醒，由运行时基于服务端时钟计算实际执行时间，避免模型自行手算
  RFC3339 时间时把提醒设置到过去。
- 定时提醒执行时，如果模型已经通过消息工具向本次任务的投递目标成功发送
  纯文本消息，这次任务会被视为已经完成投递，cron 最终阶段只保存模型的
  最终回答，不会再把最终回答发到群里。
- 兼容性与升级注意：
  这次保持已有 `at`、`run_at`、`every`、`cron_expr` 等调度参数兼容。
  相对延迟参数只用于一次性提醒；和绝对时间、周期任务或 cron 表达式
  混用时会被明确拒绝，避免产生模糊调度。同一次定时任务运行内，只有
  同一个任务投递目标已经通过消息工具成功发送纯文本消息时，最终投递才会
  被跳过；不同目标的消息、文件或媒体消息，以及消息工具发送失败后的
  最终兜底投递仍保持原有行为。

## v0.0.95-preview.3 (2026-05-15)

- 定时提醒执行时，如果模型已经通过消息工具向本次任务的投递目标成功发送
  纯文本消息，这次任务会被视为已经完成投递，cron 最终阶段只保存模型的
  最终回答，不会再把最终回答发到群里。
- 这次修复覆盖“提醒正文”和“已在当前聊天中提醒...”这类确认句不完全相同
  的场景，避免同一次提醒在企业微信里出现两条用户可见消息。
- 兼容性与升级注意：
  这次跳过只作用于同一次定时任务运行、同一个任务投递目标、且消息工具已
  成功发送的纯文本消息。不同目标的消息、文件或媒体消息，以及消息工具
  发送失败后的最终兜底投递仍保持原有行为；不改变任务存储格式、配置项、
  安装入口或调度参数。

## v0.0.95-preview.2 (2026-05-15)

- 定时提醒执行时，如果模型已经通过消息工具成功发送了同一目标、同一
  文本的提醒，cron 最终投递阶段会跳过这条重复文本，避免一次提醒在
  企业微信里连续出现两次。
- 这次跳过只发生在同一次定时任务运行内，且只针对完全相同的目标和
  文本。模型主动发送的其它消息、不同目标的消息，以及最终答案与已发送
  内容不同的场景仍保持原有投递行为。
- 兼容性与升级注意：
  这次不改变已有任务存储格式、调度参数、安装入口或配置项。相对延迟
  调度参数继续兼容 `after`、`delay`、`after_ms` 和 `delay_ms`；
  对 camelCase 的 `afterMs`、`delayMs` 也做了兼容处理。

## v0.0.95-preview.1 (2026-05-15)

- WeCom AI 请求现在会把本轮服务端接收到请求时的当前时间注入模型上下文。
  用户询问“现在几点”“半小时后提醒我”这类依赖当前时间的任务时，模型
  不再只能依赖自身过期的时间判断。
- 定时任务工具新增相对延迟调度参数。
  模型可以使用 `after`、`delay`、`after_ms` 或 `delay_ms` 创建一次性
  提醒，由运行时基于服务端时钟计算实际执行时间，避免模型自行手算
  RFC3339 时间时把提醒设置到过去。
- 兼容性与升级注意：
  这次保持已有 `at`、`run_at`、`every`、`cron_expr` 等调度参数兼容。
  相对延迟参数只用于一次性提醒；和绝对时间、周期任务或 cron 表达式
  混用时会被明确拒绝，避免产生模糊调度。自定义 WeCom 请求 prompt
  模板仍按模板内容完整渲染；需要当前时间时可显式使用 runtime rules
  或 current time note 变量。

## v0.0.94 (2026-05-13)

- WeCom AI WebSocket 流式回复新增 native thinking 展示模式。
  启用 `enable_stream: true` 后，`trpc-claw` 会把模型边想边做的
  中间内容、阶段进度和工具调用摘要放进企业微信原生 thinking 区域，
  最终答案则保持在普通回复区域，避免中间过程混进最终回复。
- `stream_display_mode` 的默认值现在是 `native_thinking`。
  用户升级后如果没有显式配置该字段，在 AI Bot WebSocket 流式模式下
  也会自动使用 native thinking；需要旧展示行为时可以显式配置
  `stream_display_mode: "legacy"`。
- thinking 里的工具调用展示现在包含更有用的安全摘要。
  例如 `exec_command` 会显示命令和关键路径，`skill_load` 会显示加载的
  skill，`skill_select_docs` 会显示 skill 和文档路径，`web_fetch` 会显示
  去掉 query 的 URL 主体，文件类工具只显示路径摘要。
- 兼容性与升级注意：
  这次只影响 WeCom AI Bot WebSocket 且启用流式回复的展示方式。
  非流式回复、普通 markdown 回复、主动消息发送、媒体上传和旧的
  `stream_display_mode: "legacy"` 行为保持兼容；工具摘要会跳过敏感参数、
  token、secret、URL query 和过长内容。

## v0.0.93 (2026-05-06)

- WeCom AI 的外部检索策略改为由运行时 prompt 约束模型自行判断。
  `trpc-claw` 不再在 channel 层根据用户消息里的“现在”“今天”等词
  进行关键词匹配，也不再根据模型回复里的固定文案做二次重试或兜底改写，
  避免普通排障、前端问题分析等场景被误判成必须联网检索。
- 当运行环境没有可用的联网检索能力，或模型没有选择检索时，
  channel 层不再合成固定的
  `未能完成所需的联网检索，暂时没有拿到可验证结果。`
  回复。模型会继续按当前可用上下文自然回答，只有用户明确要求实时、
  最新或外部验证信息时，才应在能力可用的前提下尝试检索。
- 兼容性与升级注意：
  这次只调整 WeCom AI 请求构造和外部检索提示策略，
  不改变配置格式、安装入口、发布包结构、存储布局或数据迁移流程。
  如果部署环境本身没有检索工具能力，行为会退化为普通模型回答，
  不会再返回 channel 层硬编码的检索失败文案。

## v0.0.92 (2026-05-06)

- 默认回复语言策略现在会跟随用户输入的主语言。
  `trpc-claw` 在发送用户可见的 preamble、进度更新和最终回复时，
  会优先保持同一种自然语言，避免中文会话里夹入整句英文进度说明。
- 语言策略不会硬禁英文技术内容。
  代码、命令、文件路径、API 名称、标识符、错误原文、引用文本和既有
  技术术语仍会保留原文，避免为了统一语言破坏可复制性或排障上下文。
- skill 加载提示和默认 `coding_agent.md` prompt asset 已同步这套规则。
  新安装、升级后的默认模板，以及运行时动态注入的 skill guidance
  都会使用一致的语言约束。
- 兼容性与升级注意：
  这次只调整 `trpc-claw` 的 prompt/runtime policy，
  不改变配置格式、安装入口、发布包结构、存储布局或数据迁移流程。

## v0.0.91 (2026-04-30)

- 运行时升级现在支持显式 preview channel。
  runtime admin 可以在企微里发送 `/runtime upgrade preview`，
  让当前在线实例 drain 后切到
  `preview/VERSION` 当前指向的版本；也可以追加 `force`
  执行强制切换。
- 默认升级路径保持稳定版语义：
  `/runtime upgrade`、运行时控制卡片里的升级按钮，以及
  `trpc-claw upgrade` 默认仍然只解析 `latest/VERSION`，
  不会因为镜像里存在 preview 版本就自动跳转到 preview。
- 发布链路新增 stable / preview channel 约定。
  `release.sh` 默认发布到 `latest/`；后续 preview 发布必须显式使用
  `--channel preview`，只更新 `preview/*` 和
  `releases/<version>/`，不会覆盖根 `VERSION` 或 `latest/*`。
- 安装和 supervisor 样板补充 `--channel` /
  `TRPC_CLAW_RELEASE_CHANNEL`，方便手动安装或平台启动时显式选择
  preview channel；指定 `--version` 时仍然直接安装对应不可变版本。
- 兼容性与升级注意：
  这次保持已有 stable 行为不变，旧的 `/runtime upgrade`、
  `/runtime upgrade <version>`、`trpc-claw upgrade` 和
  `latest/install.sh` 入口继续可用。preview 只在用户显式指定
  preview channel 时生效，生命周期交接给 `start.sh` 的仍然是真实
  版本号，因此已有平台 supervisor 不需要理解新的 preview alias。

## v0.0.90 (2026-04-30)

- MCP-backed skills 现在明确区分共享 skill 与用户私有本地配置：
  用户已经提供完整 MCP JSON 或带鉴权 endpoint 时，`trpc-claw`
  会倾向保存为本地私有配置（例如 skill-local `mcp.json`）并直接验证使用，
  不再默认要求用户把同一份信息重新设置成环境变量。
- `mcporter` 和 `skill-creator` 的 bundled skill 说明同步收紧：
  bot-global MCP 能力应沉淀为 local skill + 私有 `mcp.json`，
  `~/.mcporter/mcporter.json` 只作为 CLI 互操作配置，而不是能力边界。
- 默认 prompt 现在会在缺少环境变量时先检查已有本地配置，避免已有
  mcporter/MCP 配置可用却错误地向用户索要 `export ...`。
- 兼容性与升级注意：
  这次只调整 `trpc-claw` 的 prompt/runtime policy 和 bundled skill
  使用说明，不改变配置格式、安装入口、发布包结构或已有数据布局。
  已经通过环境变量或 `~/.mcporter/mcporter.json` 使用的存量配置仍可继续
  作为 CLI 互操作路径使用；新行为主要影响用户已经在对话中提供完整私有
  配置时的默认沉淀方式。

## v0.0.89 (2026-04-29)

- `trpc-claw` 的可复用能力沉淀路径转向 skill-first。
  当用户要求新增、记住、配置或复用长期能力、工具/API/MCP 调用方式、
  团队流程、文档规范或领域规则时，默认提示会引导模型优先创建或更新
  local skill，而不是把具体场景写成专用运行时代码。
- `skill-creator` 现在明确区分“运行时稳定边界”和“skill 可演进上下文”：
  权限、密钥、文件访问、校验和生命周期仍由代码或配置保障；
  触发条件、操作步骤、示例、失败恢复和上下文约束则沉淀在 skill 中。
- prompt 和文档现在也明确区分 memory 与 skill：
  轻量事实、偏好和简单常驻规则继续写入 memory；
  只有需要可执行流程、工具、示例、参考资料或失败恢复时才沉淀成 skill。
- `mcporter` 继续作为 MCP-backed skill 的 CLI 执行底座。
  MCP 不再被描述成一套独立 registry 方向，而是 tool/API/CLI 集成的一种
  skill 实现方式。
- 默认 coding-agent prompt asset 也同步加入这条规则，避免只在
  skills tooling guidance 生效。

## v0.0.88 (2026-04-28)

- 默认自主性提示进一步收紧。
  当用户请求存在明确、低风险、可执行的下一步时，`trpc-claw`
  不应停在“我先……”这类计划或进度说明，而应在同一轮继续调用
  工具、完成写入/发送/发布/更新等动作，并返回实际结果或明确阻塞点。
- 工具调用后的 OpenClaw 专属提示已经同步到 GitHub 上游正式合入版本。
  工具结果会被视为中间状态；模型需要继续核对用户目标、推进后续工具
  调用、避免只总结工具结果，并且只在文件、文档、消息、iWiki 页面等
  确实完成后再返回链接、ID、标题或 `MEDIA:` / `MEDIA_DIR:` 标记。
- 兼容性与升级注意：
  这次只调整 `trpc-claw` 的 prompt/runtime policy 和 OpenClaw 上游依赖，
  不改变公共 `trpc-agent-go` 默认 post-tool prompt、planner/react、
  配置格式、存储布局或数据迁移流程。

## v0.0.87 (2026-04-27)

- 修复 WeCom AI WebSocket 流式回复在最终消息阶段偶发重复发送的问题。
  当最终 stream 帧已经写出、但企业微信 ACK 返回超时或版本冲突等
  可恢复错误时，`trpc-claw` 现在会把这次最终流式投递视为结果不确定但
  已发送，不再额外 fallback 发送同内容的 markdown 回复，避免用户在群聊
  或单聊里看到两条相同答案。
- 真正不可恢复的最终发送失败仍保留原有兜底路径。
  这类错误会继续走 markdown fallback 和必要的 gateway run 取消逻辑，
  不影响现有失败恢复语义。
- 兼容性与升级注意：
  这次只影响 WeCom AI Bot WebSocket 且启用流式回复的最终投递路径。
  非流式回复、普通 markdown 回复、主动消息发送和媒体上传路径保持不变；
  不需要修改配置文件，也不需要做数据迁移。

## v0.0.86 (2026-04-27)

- WeCom AI WebSocket channel 现在支持主动发送多模态消息。
  长任务、subagent runtime 或业务侧编排代码可以通过
  `occhannel.MessageSender` / `occhannel.OutboundMessage`
  主动把阶段报告、最终摘要、浏览器截图、生成的报告、日志或压缩包
  推送到 `single:<user>` 或 `group:<chatid>`，不需要等用户再发一条消息。
- 主动发送路径复用了现有 WeCom 媒体分类、大小校验、分片上传和
  `media_id` 组包逻辑。
  同一个 `OutboundMessage` 会先发送文本，再按顺序上传和发送文件；
  图片、`.amr` 语音、`.mp4` 视频和普通文件会使用对应的企业微信
  消息类型。
- `Channels -> WeCom Runtime` 现在新增了 `Debug Send` 调试入口，
  admin 同时开放 `POST /api/channels/wecom/debug/send`。
  运维或接入方可以先在 Admin 页面或 JSON API 里验证 target、
  文本、本地文件路径、展示文件名和 `.amr` 语音标记，
  再把业务代码接到 `MessageSender`。
- 文档侧新增了代码侧触发说明：
  `openclaw/channel/wecom/outbound_message_api.md`，
  并同步更新 WeCom README、activation API 说明和 runbook。
- 兼容性与升级注意：
  现有 activation API 和 `SendText` 调用保持兼容；
  被动回复里的 `[WECOM_FILE:...]` / `MEDIA:...`
  仍然走原来的 same-chat reply delivery。
  主动多模态发送只在 AI Bot WebSocket runtime 且当前长连接在线时可用；
  本地 `file_path` 需要能被当前 `trpc-claw` 进程读取，并继续遵守
  企业微信现有媒体大小限制。
  这次不需要配置文件或数据迁移。

## v0.0.85 (2026-04-22)

- 旧的单 channel 配置现在升级后也能直接获得默认的微信 runtime。
  如果现有 `openclaw.yaml` 里已经有启用中的 WeCom channel，
  但还没有显式声明 Weixin channel，
  新版本会在启动期的 prepared config 里自动补一个默认的
  `weixin-direct` runtime。
  这层补全只作用于当前运行时，不会回写用户磁盘上的原始 YAML，
  因此存量实例升级后不需要再手工改模板，也不会丢掉本地已有注释或定制。
- 对旧配置用户来说，`Channels` 页也会随之直接出现 Weixin 的
  runtime 卡片和扫码入口，前端对接的 Weixin login / status /
  QR entry 这组 API 也会一起可用。
  如果这条 Weixin runtime 只是由运行时默认值隐式补出来的，
  页面不会再渲染一个指向空配置 section 的坏链接，
  而是明确提示它当前还没有 materialize 到 source YAML。
- 兼容性与升级注意：
  已经显式声明了 Weixin channel 的配置行为保持不变，
  包括显式 `enabled:false` 的场景也不会被这层默认补全覆盖。
  这次变化只影响“已有 WeCom、但还没有显式 Weixin”的旧配置升级路径，
  不需要额外迁移步骤。

## v0.0.84 (2026-04-22)

- 默认安装链路现在切成了更适合业务双开的 dual channel 模板。
  保持默认 profile 仍然是 `wecom-ai-websocket`，
  但这份模板现在会同时内置 `weixin + wecom` 两个 channel：
  微信 channel 默认常驻，企微 channel 则在对应 env 准备好后自动启用。
- channel 配置新增了通用的 `enabled_if_env_all` 机制。
  只要某个 channel block 声明了这组 env 门槛，
  本次启动就会在展开 `${ENV}` 占位符之前先判断这些 env 是否齐全；
  不满足时会把该 channel 从 prepared config 里优雅跳过，
  不再因为模板里保留了必填 env 占位符而直接启动失败。
- admin 现在也补上了对应的状态语义。
  `Config` 页新增了 `Enabled If Env All` 字段；
  `Channels` 页的 configured cards 也不再只有
  `Enabled/Disabled` 两态，而是会明确区分
  `Disabled`、`Waiting For Env`、`Restart Required` 和
  `Enabled`，更容易解释“配置已经在，但为什么当前 runtime
  还没加载起来”。
- 兼容性与升级注意：
  没有声明 `enabled_if_env_all` 的旧配置行为保持不变；
  旧的 `enabled:false` 语义也不变。
  这次变化主要影响新的默认模板、prepared config 启动裁剪顺序
  和 admin 状态展示，不需要额外迁移步骤。

## v0.0.83 (2026-04-21)

- `Memory` admin 页现在从只读 inventory 升级成了更适合直接管理
  file-backed memory 的工作台。
  每个 app/user scope 都支持折叠展开、页内按需加载完整
  `MEMORY.md`、直接编辑、保存和回滚；
  搜索范围也扩展到了 app、user、path 和 preview，
  比之前只看截断预览更容易定位具体 memory。
- WeCom 相关的 memory scope 现在会尽量把原始 `user` 标识变得更可读。
  当 runtime 已经拿到 WeCom identity cache 时，
  像 `wecom:dm:T00320026A` 这样的 raw user id，
  页面会额外显示解析后的 `RTX + display name` 标签，
  并把这个标签一并纳入搜索索引，方便按 RTX 直接查找对应 memory。
- 兼容性与升级注意：
  这次没有修改 file-backed `MEMORY.md` 的存储布局、
  现有 raw file endpoint 语义、安装入口、发布包结构或升级命令。
  没有额外迁移步骤；拿到新版本后直接升级即可。

## v0.0.82 (2026-04-21)

- Weixin admin 现在新增了一个更适合前端直接打开的固定二维码入口：
  `GET /channels/wx_qr`。
  当实例里只有一个 Weixin runtime、且当前还没有保存账号时，
  这个入口会自动开始一次二维码登录，并把浏览器导向当前最新的
  微信二维码页面；如果二维码还没准备好，则先显示一个会自动刷新的
  等待页。
- `Channels -> Weixin Runtime -> QR Login` 卡片里同时新增了
  `Open QR Entry` 链接，方便在已有 admin 页面里直接复制或验证这条
  稳定入口。
- 为了避免误触发重绑，如果当前 runtime 已经有保存好的 Weixin 账号，
  并且没有活跃登录 session，`/channels/wx_qr` 不会偷偷重新发起登录，
  而是明确提示“已经绑定”并引导回 `Channels`。
- 文档侧补上了一份专门写给前端同学的接入说明：
  `openclaw/channel/weixin/frontend_qr_entry.md`，
  同时更新了微信 README 和 quick start，统一说明这条固定入口的
  用法与边界。

## v0.0.81 (2026-04-21)

- WeCom admin 现在补上了一套更适合“帮用户找回 Bot”的激活能力。
  `Channels -> WeCom Runtime` 页面里新增了手工触发入口；
  admin 同时开放了
  `GET /api/channels/wecom/runtimes`
  和
  `POST /api/channels/wecom/activate`。
  discovery 会返回 `default_wecom_user_id`，
  页面输入框会默认预填创建者 RTX，
  activate API 在未显式传 `wecom_user_id` 时也会回退到这个默认值。

## v0.0.80 (2026-04-20)

- 默认执行提示进一步收紧成“边说边做”。
  对非 trivial tool work，模型现在会被更明确地要求先用一句极短的话
  说明马上要做什么，然后立刻继续执行；这句 preamble 不再被鼓励
  理解成等待用户确认的停顿点。与此同时，默认恢复链也更明确地要求：
  当工具结果已经给出合理、唯一、可继续执行的 canonical id、
  修正参数或目标资源时，要在同一轮直接继续，不要停下来再问用户。
- 长任务和轮询阶段的默认播报规则也做了增强。
  当发布、review、拉取远程变更、等待异步任务等长流程进入新的可感知阶段
  时，模型会被更明确地引导输出短的阶段更新；空轮询不刷屏，但长时间等待
  时会继续说明自己还在做什么，减少“静默开跑、最后只回结果”的体验。
- Gongfeng skill 的默认行为显著强化。
  `git.woa.com` / 工蜂 / TGit / MR URL / review / comment /
  inline comment 这些请求现在会更稳定地命中 Gongfeng；
  skill 本身也补上了更完整的 MR review、canonical id 恢复、
  行间评论和 schema-first 工作流，并新增 live schema helper，
  让模型在 Gongfeng MCP 参数不确定时优先读取当前 endpoint 的
  `tools/list` / `inputSchema`，而不是继续猜字段名。
- WeCom AI websocket 回复链路修掉了一类容易打断群聊任务的并发发送问题。
  同一个 callback `req_id` 上的 reply 现在会串行到 ACK 返回，
  `6000 data version conflict` 这类中间态发送失败也会更优雅地降级，
  不再轻易把整次 gateway run 直接 cancel 掉；群聊里 review MR、
  拉 diff、继续恢复等链路的稳定性更高。
- 兼容性与升级注意：
  这次主要收敛默认 prompt 协议、Gongfeng bundled skill 内容和
  WeCom websocket 回复策略，没有修改现有 tool 名称、主配置文件结构、
  安装入口、发布包布局或升级命令。
  旧实例升级后不需要额外迁移。

## v0.0.79 (2026-04-20)

- skill 相关问题的默认处理链路又收紧了一轮。
  当用户直接点名某个 skill，或者任务明显命中某个 skill 的描述时，
  模型现在会更稳定地在**同一轮**先加载该 skill 的 `SKILL.md`，
  再继续回答或执行，而不是先停在“我可以去读这个 skill”这类
  元话术上。
- skills 现在会把更明确的磁盘定位信息暴露给模型。
  skill 总览会带上每个 skill 对应的 `SKILL.md` 路径；
  `skill_load` 之后还会继续提供 `Skill dir` 和 `Skill file`，
  让模型更容易继续读取该目录下的 `references/`、
  `scripts/`、`assets/`、`templates/` 等资源，
  而不只停留在短摘要或单个 `SKILL.md`。
- 默认 skill 协议现在更明确地要求：
  命中 skill 时先加载，再按需继续读附近文档和资源，
  并优先复用 skill 自带的脚本、模板和资产来完成任务。
  同时，这轮也清掉了会把“谁负责 load、谁负责执行”这类实现细节
  直接暴露给模型和用户的冲突文案，降低回答里出现
  “这里只能加载说明，不能执行”这类产品层面错误表述的概率。
- WeCom request prompt 里此前已经退役的一批旧兼容变量、
  legacy 模板刷新兼容壳和结构预览占位这次也一并清理掉了，
  让实际运行时 prompt 与管理侧预览更一致，减少历史冗余文案
  对模型行为的干扰。
- 兼容性与升级注意：
  这次主要调整默认 prompt 协议、skill 路径暴露和
  管理侧兼容壳清理；没有修改现有 tool 名称、skill metadata
  基本格式、配置文件主结构、发布产物布局或升级入口。
  旧实例升级后不需要额外迁移。

## v0.0.78 (2026-04-17)

- 默认 skills tooling guidance 现在对所有 skill 都补上了更强的
  “先加载、再回答” 约束。
  当模型已经从 skill catalog 里判断某个 skill 相关时，
  会被更明确地引导先 `skill_load`，再决定这个 skill 是否可用、
  或给出精确命令、认证流程、发布步骤、API 参数等细节，
  降低只看 `name + description` 就直接开答的概率。
- skill 的补充文档读取链路也做了通用强化。
  guidance 现在会继续推动模型按需使用
  `skill_list_docs`、`skill_select_docs` 和
  `skill_load(...docs...)`，
  并提醒它在需要精确命令、flag、文件结构、脚本行为或发布流程时，
  继续查看 skill 目录里的 supporting docs、scripts、
  templates、assets、examples 和 config，
  不再把 `SKILL.md` 当成唯一信息源。
- 运行时生成的 skills guidance 现在会带上当前配置里的
  skill roots，
  让模型知道每个 skill 对应的是一个磁盘目录 bundle，
  而不是只有 prompt 里的 metadata 条目。
  这层增强是通用的，不依赖任何特定 skill 名称，
  对 bundled、local、以及额外 skills 目录都生效。
- 兼容性与升级注意：
  这次只增强了默认 prompt guidance 和相关测试，
  没有改动 skill metadata schema、tool 名称、
  配置文件格式、发布产物布局或升级入口。
  旧实例升级后不需要额外迁移。

## v0.0.77 (2026-04-16)

- `exec_command` / `skill_run` 现在会无条件把
  `~/.local/bin`、`~/bin`、`~/go/bin` 和 `~/.cargo/bin`
  这几类约定用户 bin 目录前置到运行时 `PATH`。
  之前这批目录只有在 `trpc-claw` 启动时已经存在时才会被带进去，
  导致用户后面才创建目录或新放 binary 时，
  agent 仍然可能报 `missing bins`，只能靠重启 runtime 才恢复。
- 现在这些约定目录即使在启动时还不存在，
  后续往里面放新的 binary 也能被后续工具调用直接发现；
  `TRPC_CLAW_EXTRA_PATH_DIRS` 和其他 env 推导目录的行为保持不变。
- 兼容性与升级注意：
  这次只放宽了默认 PATH 里的固定用户 bin 目录注入策略，
  没有改动现有配置字段、skill metadata、Admin 数据目录、
  发布产物布局或升级入口。
  旧实例升级后不需要额外迁移。

## v0.0.76 (2026-04-16)

- `Runtime Control` 里的 `Restart` / `Upgrade to Latest`
  现在会先返回一个和现有 Admin 风格一致的确认页，
  不再落到单独一套视觉样式，也不会在子路径代理环境里
  短暂跳到浏览器自己的空白错误页。
- 这张确认页会继续沿用当前请求的相对 Admin 路径做状态探测，
  并且会等到新的 runtime 真正恢复、且不再把这次 lifecycle
  action 标记成 pending 之后，才允许用户重新打开
  `Runtime Control`。
  之前那种“旧进程刚好还能回 200，就过早跳回上一页，
  随后碰到 `ERR_EMPTY_RESPONSE`” 的时序问题也一起修掉了。
- 兼容性与升级注意：
  这次只调整 Admin 页面对 runtime lifecycle action
  的确认与回跳行为，没有改动配置文件格式、session /
  channel 状态目录、发布产物布局或升级入口。
  旧实例升级后不需要额外迁移。

## v0.0.75 (2026-04-16)

- `Channels` 共享 Admin 页面对反向代理子路径部署的兼容性又补齐了一轮。
  之前像 compile-cloud / WebIDE 这类把 Admin 挂在
  `.../proxy/.../` 子路径下的访问方式里，
  `Channels` 页自己的侧边导航、`Refresh page`、
  `JSON status`、`Runtime Control`、`Chats`、`Prompts`、
  `Config` 跳转，以及 Weixin 登录/取消/恢复/删除这组表单提交，
  仍有一部分站内链接会直接落到域名根路径，
  从而把用户带到错误的 `/channels` 或 `/api/...` 地址并返回 404。
- 现在这批站内链接、表单 action 和 Weixin Admin 的回跳 redirect
  都统一按“相对当前请求路径”的规则生成，
  不再依赖某个固定代理前缀，也不需要为 compile-cloud
  这类环境额外做特判。
  根路径部署的行为保持不变，子路径部署下的 `Channels`
  管理和 Weixin Admin 操作则会继续留在当前代理前缀内。
- 兼容性与升级注意：
  这次只修正 Admin 页面的 URL 生成与重定向行为，
  没有改动 `sessions.sqlite`、`session_tracker.json`、
  配置文件格式、persona / prompts 布局或 channel 运行时状态目录。
  旧实例升级后不需要数据迁移。

## v0.0.74 (2026-04-16)

- `trpc-claw` 现在正式带上了微信 `weixin` channel。
  这条链路支持二维码登录、多账号 direct 私聊、long-poll 拉消息、
  `context_token` 持久化、typing 提示、
  `/runtime status|versions|changelog` 文本命令，
  以及 `errcode = -14` 时的自动 pause 和恢复。
- Admin 的 channel 管理现在收口到了共享的 `Channels` 页面。
  微信二维码登录、账号状态、`remove` / `resume`、
  以及 WeCom / Weixin 的 transport 配置编辑都走同一套 admin 外壳，
  不再额外分叉一套独立风格页面。
  `Config` 里也补上了更明确的来源说明：
  当前字段到底是显式配置、默认值，还是继承了全局 `state_dir`
  或启动参数 `--state-dir`，现在都会直接写出来。
- 安装和切配置的链路新增了显式 `weixin` profile。
  现在既可以用安装器：
  `curl ... | bash -s -- -f --profile weixin`，
  也可以在本机直接执行
  `trpc-claw upgrade -f --profile weixin`
  把当前主配置切成 `openclaw.weixin.yaml`。
  同时补上了面向新手的微信快速开始文档，
  从下载、切 profile、打开 admin、二维码登录，
  一直到手机微信里的 `设置 -> 插件 -> 微信ClawBot -> 开始扫一扫`
  和首轮聊天验证都串成了一条完整路径。
- 兼容性与升级注意：
  这次没有改动 `sessions.sqlite`、`session_tracker.json`、
  memory 数据库、persona 文件布局或默认 `wecom-ai-websocket`
  安装 profile；
  旧实例按原有 `wecom` 配置升级到这版，不需要做数据迁移。
  新增的微信运行态会单独落在 `state_dir/weixin/**`，
  不会污染现有 WeCom 状态目录。
  但如果你在新版本里使用了共享 `Channels` admin 的
  enable / disable 能力，主配置里可能会出现新的
  `channels[].enabled` 字段；
  这个字段是新版本识别的扩展配置，
  如果之后要回退到不认识它的旧版本，
  需要先手动删掉这些 `enabled:` 行。

## v0.0.73 (2026-04-15)

- `/runtime versions`、`/runtime changelog` 这类运行时文本命令
  现在会复用统一的长文本安全发送链路。
  当回复内容超出企业微信单条 markdown 大小限制时，
  运行时会自动按已有分片规则拆成多条发送，
  避免命令看起来“没有反应”。
  这次修复不是只对某一个命令做特判，
  而是把 runtime 文本命令和 debug bundle
  的文本回包统一接到了同一条发送 helper 上。
- `openclaw` 的上游依赖已经从临时的个人 GitHub fork
  切回 `github.com/trpc-group/trpc-agent-go`
  官方 `main` 分支最新提交。
  内网 MR 不再需要继续依赖 `WineChord` 的
  `openclaw` replace 才能拿到 Admin `Config` /
  `Runtime Control` 那批能力，
  后续升级链路也更清晰。
- 兼容性与升级注意：
  这次没有改动 `sessions.sqlite`、`session_tracker.json`、
  默认 profile 文件结构、技能目录布局或 slash 命令语义。
  旧实例升级后不需要做数据迁移。
  命令分片只在回包过长时触发，
  正常长度回复的行为保持不变。

## v0.0.72 (2026-04-15)

- Admin 新增了独立的 `Runtime Control` 页面，
  把优雅重启、强制重启、升级到最新版本、
  以及切换到指定版本这组运行时操作统一收口到一个地方。
  `Config` 页面顶部也补上了常驻跳转入口；
  如果当前存在 `Pending restart` 的配置变更，
  还会继续给出更醒目的引导卡片。
- `Config` Admin 现在明确写回用户主配置文件
  `openclaw.yaml`，
  不再把运行时展开后的临时 YAML 当成可编辑源文件。
  页面仍然会同时展示 `Configured` 和 `Current runtime`
  两侧状态，
  但保存和重置操作都会回到真正的源配置路径。
- release 元数据链路做了两层补强。
  `latest/releases.json` 现在会在发布时从
  `CHANGELOG.md` 自动回填 `min_supported_target`
  以上的历史版本，
  避免版本选择器只剩最新版本一项；
  同时 changelog 的版本标题也统一带上发布日期，
  例如 `## v0.0.72 (2026-04-15)`，
  方便用户在运行时页面直接判断每个版本的大致发版时期。
- 兼容性与升级注意：
  这次没有改动 `sessions.sqlite`、`session_tracker.json`、
  persona 配置、技能目录结构或运行时命令语义。
  旧实例升级后不需要做数据迁移。
  Admin 与版本选择能力的变化都建立在现有接口和文件布局上，
  只是把已有状态展示、跳转和 release 元数据补完整。

## v0.0.71 (2026-04-14)

- 企业微信的状态型控制卡片补上了统一的“活跃卡同步”机制。
  之前如果用户先打开了助手面板或人格/工作区/会话等卡片，
  再通过文本命令或其他路径切换人格、助手名字、工作区或会话线，
  聊天里那张旧卡会长期停留在旧状态，
  看起来像“当前显示错了”。
  现在运行时会跟踪当前聊天最后一张活跃状态卡，
  并在这些状态变化后自动把它刷新到最新状态。
- 这次修复不是只针对某一个人格或某一张卡做特判。
  助手面板、人格卡、工作区卡、状态卡、会话卡
  现在都走同一套状态同步逻辑，
  避免以后再出现同类“旧卡正文和当前运行态不一致”的问题。
- 人格卡的同步细节也做了补强。
  如果用户当前停在诸如保存说明之类的子视图，
  后续同步不会再把卡片强行刷回默认页；
  同时发卡失败时也不会把一张用户根本没收到的卡错误记成活跃卡，
  降低后续回刷到错误目标的风险。
- 升级注意：
  这次没有改动 `session_tracker.json`、`sessions.sqlite`、
  persona 配置或命令语义，
  只是补了企微状态卡的同步与自愈刷新能力。
  旧实例升级后不需要数据迁移。
  但聊天里已经历史发出去的旧卡不会在“零交互”下自动变对；
  用户只要重新拉一次面板，或点一下旧卡上的任意按钮，
  后续当前活跃卡就会进入新的自动同步机制。

## v0.0.70 (2026-04-14)

- 修复 Admin 在反向代理子路径下的异步请求路径解析。
  之前 `Chats` 里的 `History` 面板和同类运行态请求，
  会把 `/api/...` 这类地址直接锚到站点根路径，
  导致像 `/xxx/openclaw/admin` 这种子路径部署下，
  页面会请求到错误的根路径接口，并把 nginx 返回的 404 HTML
  直接显示在面板里。
- 现在这些 Admin 异步请求会按当前页面 URL 做相对解析，
  同时 HTML 输出阶段也会继续把相关 `data-*`
  URL 属性一并做子路径兼容处理。
  根路径部署的行为保持不变，子路径部署不再需要额外兜底改造。
- 升级注意：
  这次只修正 Admin 前端请求 URL 的生成与重写逻辑，
  没有改动聊天历史、session tracker、数据库或配置文件格式。
  旧实例升级后不需要做数据迁移。

## v0.0.69 (2026-04-13)

- Admin 的 `Chats` 页面现在补上了真正可读的聊天记录视图。
  进入单个 chat 后，可以直接在 `History` 里查看该聊天最近的
  可见消息，而不再只能看到名字、persona、workspace 这类状态摘要。
  新视图会按时间线展示最近内容，并支持继续向更早的消息回溯。
- `History` 的浏览体验也做了重构。
  聊天记录默认折叠，展开后先加载最近一段，
  再通过 `Load older messages`
  和更早的 session line 逐步向上展开。
  同一个 session line 的边界现在会在显示层自动合并，
  不再把连续片段拆成多个重复块。
- Admin 不再每 15 秒整页自动刷新。
  运行态观察页现在只会提示“页面背后已有新状态”，
  由用户自己决定何时刷新；
  编辑页则保持稳定，不会再被后台刷新打断输入或阅读。
- `Chats` 页的历史读取补上了旧 WeCom session id
  兼容解析。
  之前某些聊天状态虽然已经在 tracker 里，
  但 transcript 仍然保存在旧的 `wecom:thread:...`
  session id 下，导致页面看得到 chat，
  却看不到历史。
  现在会同时兼容 canonical id 和旧 thread id，
  避免已存在的聊天记录被错误显示成空白。
- 升级注意：
  这次没有改动 `session_tracker.json`、
  `sessions.sqlite` 或其他聊天状态文件的存储结构，
  只是补了 Admin 历史读取和展示层的兼容解析。
  旧实例升级后，已有 chat 状态和聊天记录不需要迁移。

## v0.0.68 (2026-04-13)

- `/runtime bundle` 现在会按企微 `file <=20MB`
  的附件上限做单包裁剪，
  默认优先保留配置、identity、prompt、persona、
  session tracker 和较新的调试文件；
  当 `debug/` 或 `sessions.sqlite` 过大时，
  会自动省略较低优先级内容而不是直接整包回传失败。
  被省略的来源和文件路径会写进压缩包里的 `MANIFEST.txt`。
- `/runtime bundle full [总上限]` 现在会把调试资料拆成多个
  `<=20MB` 的分包回传。
  默认总上限约 `80MB`，
  也支持像 `80mb`、`1gb` 这样的显式总预算；
  每个分包都会带 `MANIFEST.txt`，
  说明当前分包、整体总上限，以及因总上限被省略的资料。
  这条命令仍然不会尝试截断 `sessions.sqlite`
  的内部内容，只会按文件级别决定是否纳入某个分包。

## v0.0.67 (2026-04-13)

- Admin 新增了 `Chats` 观察面板，用来直接查看当前运行时里
  已跟踪聊天的实际状态。
  现在可以在页面里看到每个聊天当前生效的名字、名字来源、
  persona、最近会话线、workspace，以及当前聊天是否覆盖了默认名字。
  `Identity` 页面也会补充展示默认名字与聊天级覆盖之间的关系，
  降低“为什么这里没有生效”的理解成本。
- 企业微信的调试链路补上了 `/runtime bundle`。
  这条命令会把当前实例里与排障最相关的一组材料
  自动打包成压缩包并回传到聊天，
  包括配置、identity、prompt、persona、session tracker、
  session 数据库和 debug 目录等。
  `/help runtime` 和 `/runtime help`
  也同步补齐了这条命令的说明。
- 名字相关的帮助文案、Admin 页面提示和状态标签
  现在统一成“默认名字”和“当前聊天名字”这套说法。
  `/name` 的帮助会更明确地说明：
  `/name <称呼>` 只改当前聊天；
  `/name global <称呼>`
  改的是默认名字，会影响其他还没有单独改名的新私聊、新群聊，
  以及仍在使用默认名字的现有聊天；
  但如果别的聊天已经有自己的当前聊天名字，
  仍然会继续优先使用那边的名字，不会被全局默认值覆盖。
- `Chats` 页面里的 `Known Users`
  不再只显示原始企微用户 ID。
  现在会优先复用现有 WeCom 身份解析能力和 `user_label_mode`
  生成更易读的友好标签，
  同时仍然保留原始 ID 作为补充信息。
  单聊和隔离群用户卡片的标题也会按同样规则显示，
  例如会从 `DM · T00320026A`
  变成 `DM · wineguo (T00320026A)`。
- 兼容性与升级注意：
  这次没有引入新的名字持久化文件，也没有改动
  `session_tracker.json`、`IDENTITY.md` 或 `sessions.sqlite`
  的存储结构；
  现有 `/name`、`/name global` 和 `/new`
  的运行语义保持不变，
  `/runtime bundle` 只是新增能力，不会影响旧命令。
- 兼容性与升级注意：
  `/api/chats` 现在新增了 `known_users` 字段，
  但原来的 `known_user_ids` 仍然保留。
  如果有外部脚本或页面消费这个接口，
  可以直接读取新的结构化友好标签；
  不需要升级的旧调用方继续读 `known_user_ids` 也不会 break。
- 兼容性与升级注意：
  `/api/chats` 里的 `display_label`
  现在对单聊和隔离群用户会优先显示
  “友好标签 + 原始 ID”的可读标题，
  不再保证和旧版本完全一致。
  如果下游代码把 `display_label`
  当作稳定主键或精确匹配字符串使用，
  需要改成依赖更稳定的 `base_session_id`，
  不要继续拿展示文本做逻辑判断。
- 兼容性与升级注意：
  友好用户标签依赖当前实例可用的企微身份解析能力；
  如果 `user_label_mode` 设成了 `id`，
  或者当前环境没有可用的 user identity lookup，
  Admin 会自动回退成原始用户 ID，
  不会因为解析失败把聊天列表或调试页面打坏。

## v0.0.66 (2026-04-13)

- prompt、identity 和 persona 现在正式收口成
  “文件优先 + Admin 可视管理 + 运行时热加载” 的模型。
  默认 instruction / system prompt 片段会自动落到
  `state_dir/prompts/instruction` 和
  `state_dir/prompts/system`，
  persona 文件会自动落到 `state_dir/personas`，
  全局名字则落到 `state_dir/IDENTITY.md`。
  Admin 侧新增独立的 `Prompts`、`Identity` 和 `Personas`
  页面，可以直接查看和修改这些内容；
  外部直接改磁盘文件时，同一实例的后续请求也会自动吃到新内容，
  不再要求重启进程。
- `trpc-claw` 不再和 assistant 自称混用。
  运行时 system prompt 现在明确区分
  “当前名字”和“runtime product”，
  用户问“你是谁 / 你叫什么”时会优先用当前名字回答，
  `trpc-claw` 只继续作为运行时产品名和模型事实背景出现。
  其中全局默认名字来自 `IDENTITY.md`，
  企微聊天里的当前会话名字优先级更高。
- 企业微信名字能力做了一轮完整收口。
  `/name` 现在负责查看或设置当前会话里的名字，
  `/name global <称呼|off>` 负责修改或清除全局默认名字；
  当前会话名字仍然保存在现有 `session_tracker.json`
  里，并且会继续跨 `/new` 保留。
  同时这条链路不再依赖固定中文前缀硬编码，
  而是改成让 agent 通过结构化工具理解并执行改名。
  旧的 thread/chat session key 也补了一层统一归一化，
  避免同一个群聊里“改名成功提示了，但下一轮又失效”。
- 默认 persona 现在统一改成 `pragmatic`。
  Admin、默认配置、企微帮助和运行时展示都会直接把务实人格
  作为默认值，不再显示一个没有真实 prompt 的“系统默认”占位人格。
  兼容上，老配置里的 `agent.persona: default`
  仍会自动映射到 `pragmatic`，
  旧的 `personas/default.md`
  也会按受管迁移逻辑收回到新的默认人格文件。
- 默认 prompt 文件和 Admin 视图都做了可读性重构。
  默认受管文件从 `10_memory.md`、`10_runtime_identity.md`
  这类编号文件改成了语义化文件名，
  WeCom request prompt 的预览也不再直接把
  `${TRPC_CLAW_...}` 这类运行时占位符原样甩给用户。
  `Prompts` 页面里的长文本和最终 prompt 预览现在默认折叠，
  `Inline Source` 也改成了更直白的
  `... Config Text` / `Live ... Text`
  这套命名，减少“到底改的是 YAML 还是 prompt 文件”的理解负担。
- prompt 默认值和兼容迁移规则也收紧了。
  没改过的 managed default 文件在升级后会自动同步到新模板；
  用户已经改过的文件不会被静默覆盖；
  旧编号文件和新语义文件名并存时，
  旧文件会按受管迁移策略自动改名、删除，或挪到 `.legacy/`
  目录，不会直接把用户内容覆盖掉。
- 旧版默认的
  `system_prompt: "You are tRPC-Claw."`
  不再污染实际生效的 system prompt。
  对全新安装，默认模板已经不再写入这条值；
  对升级用户，如果 YAML 里保留的正好还是这一条历史默认值，
  运行时会把它当作 legacy managed default 忽略，
  避免和新的 `IDENTITY.md + runtime product`
  机制互相打架。
- 对“未来要做某件事，但没有给具体时间”的请求，
  又补了一层更硬的 cron 保护。
  这类 standing workflow / default rule
  现在不会再被误判成可以直接落地为定时任务，
  降低“只是让 agent 记住以后这么做，却被创建成 cron”的风险。
- 升级注意：
  如果你之前长期依赖
  `agent.system_prompt: "You are tRPC-Claw."`
  来决定机器人自称，升级后“你是谁”这类回答会改成优先使用
  `IDENTITY.md` 或当前会话名字。
- 升级注意：
  如果你是在 Admin 的 `Inline Source`
  里编辑 prompt，那改动写回的是 `openclaw.yaml`
  里的 inline 文本，不是磁盘上的 prompt 文件；
  升级后它仍会继续生效，并且会和新的文件片段一起合成最终 prompt。
- 升级注意：
  如果你之前改过默认 prompt / persona 文件，升级不会覆盖你的改动；
  只有仍然精确等于已知旧默认模板的受管文件，
  才会自动刷新到新版本。
- 升级注意：
  企微里的当前会话名字现在仍然沿用现有 `session_tracker.json`
  存储语义，所以 `/new` 不会清掉它；
  如果你想恢复到全局默认名字，需要显式发送 `/name off`。

## v0.0.65 (2026-04-09)

- 默认 bundled `stocks` skill 现在从“股票快照”
  扩成了更通用的“上市证券快照”能力。
  除了 A 股、港股和美股，
  现在还能直接覆盖内地 ETF、LOF 和 REIT，
  并继续复用免鉴权的腾讯财经公开行情源。
- 中文 ETF 查询又补了一层更稳的名称归一化和候选回退。
  像“嘉实纳指 ETF”、
  “景顺长城纳指科技ETF”、
  “纳指科技景顺长城 ETF”
  这类不按官方简称顺序来提问的表达，
  现在也能稳定解析到对应场内产品，
  不会再因为只匹配到股票类结果或搜索词序不一致，
  就错误降级成“当前环境拿不到实时价格”。

## v0.0.64 (2026-04-08)

- 企业微信群聊在 `group_session_mode: shared` 下的多轮工具调用
  现在能稳定沿用同一轮里刚产生的 assistant/tool 运行态。
  之前这条链路会把会话对象分成“对外展示的 canonical session”
  和“底层存储写入的 scoped session”两份，
  导致工具调用虽然已经落库，
  但下一轮构造给模型的运行态还是旧快照。
  结果就是群聊里像 `/new`、仓库扫描、工作区检查这类任务，
  在第一轮工具执行后会反复从头开始，
  看起来像机器人一直在重复第一步。
  这次修复把 shared session 的运行态读写重新同步回同一条会话视图，
  让群聊在多轮 tool loop 下也能像单聊一样稳定收敛。

## v0.0.63 (2026-04-03)

- 企业微信同聊文件回传的可写根目录校验又收紧了一轮。
  运行时托管 upload 目录现在会被显式纳入允许回复根，
  但不会再因为 file tool 没带 `base_dir`
  就把整个隐式 state 目录自动放大成可回传范围。
  同时路径校验改成更硬的 `realpath` 根检查并拒绝 symlink 穿透，
  降低错误路径、越界路径和“误把别的运行时文件当附件发回用户”的风险。
- 默认配置现在打开了 context compaction，
  `webfetch_http` 也新增了单次内容长度和总内容长度上限。
  这会减少一次抓回超长网页或文档时把上下文瞬间塞爆的概率，
  降低“联网查了，但后面模型直接 context_length_exceeded”
  这类失败。
- 默认内置的若干 MCP skill
  现在改成引用正确的 bundled `mcp.json` 路径。
  `tapd`、`iwiki`、`gongfeng`、`km`、`rainbow-config`
  这几类 skill 在“只用了运行时自带 bundled skill，
  还没在 state 目录做覆盖安装”的场景下，
  首次使用时不再因为找错配置文件路径直接 `ENOENT`。
- 默认交互继续朝“少问、先做完”再收紧一档。
  coding runtime prompt、WeCom request system prompt、
  Friendly persona 和几处通用 skill guidance
  现在都更明确地要求：
  普通实现选择、常见歧义补全、低成本重试和后续收尾
  优先由 agent 自己推断并继续执行，
  不要把常规选择题和澄清题反复抛回给用户。
  同时企业微信回复链路新增了一层运行时兜底：
  如果模型首轮公开回复仍然主要是在追问用户，
  会自动带更强约束重试一次，
  尽量直接收敛到已经完成或至少继续推进的结果。

## v0.0.62 (2026-04-02)

- 企业微信 WebSocket 流式渲染回退了上一版过于激进的一轮
  gateway snapshot 简化，恢复成更保守的 pre-answer 边界处理。
  这次调整优先解决附件处理、长任务说明和最终正文之间的呈现回归，
  避免部分真实任务里中间过程与最终回答混在一起后更难读。
- 流式 pre-answer 文本的 idle flush 窗口从 `2.2s`
  延长到了 `6.8s`。
  这会减少底层模型输出轻微抖动时被过早切段的概率，
  降低一句话还没出完就先被刷进聊天窗口的情况。
- 对流式分段后的续写拼接再补了一层通用修正：
  如果新的可见片段本质上只是上一段的标点、闭合符或紧随其后的补充短语，
  现在会直接接回前一段，
  不再把单独的 `。`、`，然后...` 这类内容拆成新的一行。

## v0.0.61 (2026-04-02)

- 企业微信群聊里的 slash command 识别做了一轮更稳的前缀归一化。
  现在像 `@My Bot /help`、
  `@My   Bot /runtime help`、
  `@My Bot：/status` 这类“先 @ 机器人、再跟 slash”的输入，
  即使机器人展示名里带空格，
  也能继续按命令处理，
  不会再因为按空格分词后只吃掉 `@My`
  这一段而把后面的 `/help` 当成普通文本漏给模型。
- 这条群聊 slash command 链路不再依赖 `bot_name` 已显式配置。
  即使当前实例沿用默认配置、
  没有把企微侧展示名同步到 `bot_name`，
  也不会再出现“单聊 `/help` 正常，
  群聊里 `@机器人 /help` 却偶发失效”的情况。

## v0.0.60 (2026-04-02)

- 默认 coding workspace 仍然保持为进程当前工作目录，
  不改变现有 `/data/cic/workspace` 这类代码区语义。
  为减少代码仓库污染，
  运行时现在会把真正的临时文件默认导到
  `state_dir/runtime/tmp`，
  并在 prompt / tooling guidance 里明确要求：
  源码继续写在 workspace，
  非源码产物优先写到现有 `scratch_root/out`，
  而不是直接散落在代码目录顶层。
- `find-skills` 现在在把 skill 安装到
  `state_dir/skills/local` 之后，
  会自动探测同一 `state_dir` 下正在运行的 OpenClaw admin，
  并触发一次 live skills refresh。
  这修复了“市场里刚装完 skill，
  下一轮还看不见”的常见问题，
  不再要求手工重启进程或进 admin 页面点 refresh。
- 企业微信同聊附件回传现在会更严格校验 marker：
  只有在文件真实存在且可验证时才会回传；
  若模型误写了 workspace 路径但同名产物实际落在
  `scratch_root/out` 或其他允许目录中，
  会尝试做唯一匹配并自动修正。
  同时又补了一层更硬的服务端兜底：
  如果模型正文里带了 `WECOM_FILE` marker，
  但运行时最终一个可验证的真实文件都没有找到，
  现在不会再把原始 assistant 正文原样发给用户，
  而是直接降级成系统生成的校验失败说明。
- `trpc-claw` 启动期的运行时环境同步进一步加固。
  现在会把最终生效的进程环境固化到
  `state_dir/tools/trpc-claw-runtime-env.sh`，
  并在同目录生成受管 `bash` / `sh` wrapper。
  这些 wrapper 会在 login shell 场景下通过
  `BASH_ENV` / `ENV` 和命令前缀双重回灌环境，
  避免 `/etc/profile` 之类的系统脚本把继承下来的
  `PATH`、受管 Python、Node、临时目录以及其它运行时变量重置掉，
  从而修复“容器里明明已经装好了工具链，
  真正执行时却又找不到 `pip` / `python3` / `node`”
  这一类问题。
  toolchain 相关默认环境也补齐为更完整的一套：
  现在除了 `OPENCLAW_TOOLCHAIN_PYTHON` 之外，
  还会默认提供 `OPENCLAW_TOOLCHAIN_ROOT`、
  `VIRTUAL_ENV` 和 `PIP_DISABLE_PIP_VERSION_CHECK`，
  让受管 Python 依赖安装和后续 shell/tool 复用更稳定。
- 默认交互风格继续往“极低反问率”收紧了一档。
  当前 coding runtime prompt、企业微信 request system prompt、
  Friendly persona，以及本地 `coding-agent` skill
  都明确要求：普通实现选择、重试、格式调整、依赖自恢复、
  缩小范围排查这类动作应当先由 agent 自己继续推进，
  不要轻易把“接下来要不要继续”“要不要换个做法”这类选择题抛给用户。
  只有在确实缺少外部事实、权限、凭据，或者涉及不可逆决策时，
  才应该把问题回抛给用户。
- “coding workspace”和“非源码产物区”的默认边界也进一步收紧：
  如果用户没有明确指定 repo 路径或要求改 repo 文件，
  那么直接上传文件、生成文档、媒体导出、OCR 文本和中间产物
  默认不再写进 coding workspace，
  而是优先走现有 `scratch_root/out`、
  `state_dir/runtime/tmp` 和运行时托管 upload 存储。
  同时 request prompt 会明确要求：
  真正需要改 repo 文件时再回到 workspace，
  混合任务则同时检查 repo 与这些 artifact roots，
  不要为了图省事把所有文件都堆进代码目录。
- 对“搜索 / 查一下 / 最新 / 当前 / 实时”这类
  外部检索请求再补了一层更硬的通用控制。
  现在 runtime prompt 会明确要求：
  优先自己使用 web/browser/search 工具检索，
  默认先检查最明显的主实体、主上市地或主来源，
  不要先把普通歧义抛回给用户。
  同时企业微信 channel 在完成态前会再做一次兜底：
  如果首轮回复仍然只是
  “需要联网”“告诉我看哪个”“你直接去某个 app 搜”
  这类并未真正完成检索的自然语言回退，
  会自动带更强约束重试一次；
  若重试后仍没有拿到可验证结果，
  就直接返回明确失败，
  不再把选择题式澄清原样发给用户。
- 附件失败类用户文案和正文尾巴也做了同方向收敛：
  不再默认追加“如需我可以……”这类选择题式结尾，
  而是直接陈述当前系统已经验证过的结果与处理。
  reply sanitizer 现在也会额外裁掉模型正文末尾
  “if you'd like …”“let me know …”“如需我可以 …”
  这类可选收尾。

## v0.0.59 (2026-04-02)

- 企业微信 WebSocket 流式展示针对“底层模型已开启真流式”
  做了一轮呈现层重构：
  不再把每个可见 token 都立即改写成一条新的整段快照，
  而是把 tool 前的 assistant 规划文本、public 文本和 thought 文本
  先按阶段累计，再在更稳定的边界上整体刷新给聊天窗口。
  这让长任务里“先说明计划、再调用工具”的过程更接近自然段落，
  减少半句话频繁抖动、整条消息反复重绘的问题。
- 企业微信流式占位也收敛成了更轻的脉冲提示：
  当前默认只显示 `. / .. / ...` 这一类 pulse，
  不再插入“正在准备请求”“正在运行工具”等固定状态句子。
  同时 pulse 的重置和轮转规则也做了修正，
  新的可见文本刷出后会重新从 `.` 开始，
  避免上一段状态尾巴继续黏连到下一段内容上。
- 企业微信流式里的 pre-answer 文本和最终正文现在区分得更清楚：
  tool 前的说明性文字会作为中间过程展示，
  而最终正文仍然以完成态一次性落回最终回复。
  这避免了“Reasoning 一直挂在正文前面，
  最后整段正文又把前文整体覆盖一遍”的违和感，
  也让用户更容易看清当前是规划阶段还是最终答案阶段。

## v0.0.58 (2026-04-01)

- `model.generation_config` 现在可以直接在 `openclaw.yaml` 这类配置文件里
  显式设置，例如 `stream`、`max_tokens`、`temperature`、
  `top_p`、`stop`、`reasoning_effort` 等生成参数。
  默认模板注释里也补上了这组配置的用法说明，避免只能靠代码或环境变量
  推断模型生成行为。
- OpenClaw / gateway 的流式正文默认值已修正：
  当没有显式配置 `generation_config.stream` 时，
  `trpc-claw` 现在会默认按 `stream: true` 去请求底层模型，
  不再因为零值 `GenerationConfig` 覆盖内部默认值，导致
  `/v1/gateway/messages:stream` 或企业微信长连接表面上有进度事件，
  但正文最后才一次性整块返回。
- 如果你确实需要关闭正文流式，现在也可以通过
  `model.generation_config.stream: false` 明确配置成缓冲式输出；
  这让“是否流式”从隐式实现细节变成了清晰、可见、可审计的 YAML 配置项。
- admin 页面现在能正确工作在 WebIDE / compile-cloud
  这类“服务被挂在子路径代理下”的访问方式里：
  Overview、Skills、Automation、Sessions、Debug、Browser
  这些页面跳转，以及对应的表单提交和重定向，不会再错误掉到域名根路径
  导致 404。
  这次修复没有绑定某个特定代理前缀，
  而是统一把 admin 输出里的站内根路径引用收敛成相对当前请求路径的引用，
  因此对任意 reverse proxy 子路径部署都更稳。
- skills 默认加载模式现在从 `turn` 调整成了 `session`：
  已经成功 `skill_load` 的技能会跨后续多轮请求继续保留，
  不再因为回合切换而被后台状态提前清掉，
  从而避免“模型上下文里还记得这个 skill，
  但工具层却误报必须先重新 `skill_load`”这类假阴性错误。
- 默认 YAML 模板里的内网 MCP 服务器不再预置启用：
  `iwiki`、`gongfeng`、`tapdmcp`、`rainbow`、`km`
  这些条目现在改成按注释示例按需打开，
  避免一个默认模板同时挂出过多 MCP 工具后，
  直接撞上模型接口的 tool 数量上限。

## v0.0.57 (2026-04-01)

- browser use 现在开始面向“全新 runner 容器”做自举：
  当 `trpc-claw` 在容器 / headless 环境里启动，且宿主已经具备
  `node`、`npm` 和 Chromium，但受管 `playwright-mcp`
  还不存在时，启动期会自动把固定版本的
  `@playwright/mcp` 安装到
  `${TRPC_CLAW_STATE_DIR}/toolchain/python/bin`，
  然后再把 browser runtime 标记为 ready。
  这意味着后续按线上 `start.sh` / runner 流程拉起的全新容器，
  不再需要额外手工 `npm i -g` 才能让 browser use 工作；
  第一次新建容器时启动会略慢，但后续重启可直接复用。
- browser provider 现在可以按运行时 readiness 自动接入：
  当 `trpc-claw-browser-runtime doctor` 判定当前 browser lane
  已准备完成时，企业微信 WebSocket 等默认配置即使没有手工把
  browser provider 写死到 YAML，也会在生效配置里自动补上
  native browser provider；
  同时默认 timeout 也会补成正数，避免 browser MCP
  客户端初始化阶段直接报
  `invalid configuration: timeout must be positive`。
- 企业微信里 browser 会话对“旧失败历史”的处理又补了一轮收敛：
  新回合会带上当前 turn 的 fresh browser doctor 事实，
  当 doctor 明确显示 `ready` 时，模型应优先尝试 browser tool，
  而不是沿用上一轮的 `transport closed` 或
  `timeout must be positive` 旧结论。
  这降低了同一企微长连接会话里
  “browser 其实已经恢复，但模型仍在复读旧错误” 的误判概率。
- 企业微信的上下文占用展示与自动压缩判断做了一轮更贴近真实窗口占用的
  修正：
  当前会优先使用最近一次模型调用的 `LastPromptTokens`
  作为已用上下文，而不再主要依赖工具循环累积后的
  `PromptTokens/TotalTokens`；
  状态展示里的上下文占用百分比和压缩触发时机因此更接近真实情况。
  同时内置模型上下文窗口表也补了最新一批模型规格，
  降低上下文占用显示、自动压缩阈值和实际模型窗口不一致的问题。

## v0.0.56 (2026-04-01)

- browser use 现在收敛成了统一的托管运行时入口
  `trpc-claw-browser-runtime`：
  容器 / headless 环境下会优先走受管的 Playwright MCP
  和 Chromium 解析链路，不再继续依赖
  `npx @playwright/mcp@latest` 这类会随环境漂移的临时启动方式。
  同时新增了 `doctor` 探测命令，当前可以直接检查 browser lane
  是否 ready、实际解析到了哪一个浏览器可执行文件，以及当前运行模式
  是 headless 还是 interactive。
- browser 配置面收敛为两个对用户更直观的主入口：
  `TRPC_CLAW_BROWSER_MODE` 和 `TRPC_CLAW_BROWSER_PATH`。
  普通环境下不再需要理解
  `playwright-mcp`、wrapper 路径、内部 executable env
  这些实现细节；而在自定义环境里，如果浏览器路径不在默认位置，
  也可以通过这两个变量更直接地引导 runtime 找到 browser 依赖。
- browser 会话的提示与失败恢复语义也补了一轮收敛：
  agent 现在会把历史回合里的 browser / MCP 失败视为“旧信息”，
  新回合再次收到 browser 任务时，
  应该先在当前回合重试 browser tool 或执行
  `trpc-claw-browser-runtime doctor`，
  而不是直接复读上一轮的 `transport closed` 结论，
  降低同一企微会话里“browser 其实已经恢复了，但模型还沿用旧判断”
  这一类误判。
- 非 release 构建的版本号展示改成了开发态语义：
  当前会优先显示成 `vX.Y.Z-dev-<commit>`，
  正式 release 则继续显示 `vX.Y.Z`。
  不再把手工构建替换进去的最新二进制误报成旧的固定版本号，
  降低排查容器里“代码是新的，但 `trpc-claw version` 看起来像老版本”
  这类误导。

## v0.0.55 (2026-03-31)

- 新增了默认启用的 `env_probe` 工具：
  现在用户可以直接问 agent
  “你能读到 `TAIHU_PAT_TOKEN` 吗”、
  “你能读到 `OPENAI_API_KEY` 吗”
  这类问题。
  该工具会安全检查当前 `trpc-claw` 进程、
  `TRPC_CLAW_ENV_FILE`、`runtime/env.sh`、
  `~/.bashrc`、`~/.zshrc`、`~/.profile`
  这些受信任来源，只返回变量是否存在、来自哪里，
  不会暴露变量值。
  同时，如果它在这些文件里发现了简单静态声明，
  还会顺手把变量补进当前 `trpc-claw` 进程环境，
  让后续 `exec_command` / `skill_run`
  可以直接使用，不再停留在“探测到了但还得重启”。
- 默认 coding prompt 和 skill tooling guidance 也补了
  `env_probe` 规则：
  当用户在问某个环境变量、token、secret、API key
  是否可见时，agent 会优先调用 `env_probe`，
  不再靠猜，降低“shell 里能看到，但后台实例其实没继承到”
  这类排障误判。

## v0.0.54 (2026-03-30)

- 企业微信运行时“升级完成”消息的版本表达改成更直观的
  `已从 vX 升级到 vY`；
  不再让成功升级场景同时显示
  `当前版本 == 目标版本` 这种容易让人误读的文案。

## v0.0.53 (2026-03-30)

- 企业微信 `/runtime changelog` 的 latest 默认路径已修正：
  现在不带版本号时会先解析出真实 latest 版本，再提取对应节的变更摘要，
  不会再出现“能拿到 changelog 文件，但默认 latest 摘要为空”的问题。
- 企业微信 `/runtime versions` 现在会直接展开每个版本在
  `latest/releases.json` 里的 changelog notes；
  同时运行时升级完成通知也会把目标版本的变更摘要一起带回原会话，
  不再只回一句“升级完成”。
- 企业微信 slash 帮助现在补成了统一的 topic help：
  除了原来的 `/help` 帮助卡和 `/help all` 全文命令文本，
  现在还支持 `/help runtime`、`/help persona`、`/help cron`
  这类“命令级详细帮助”；
  同时当前内置 slash 也统一支持 `/<command> help`
  作为等价入口，例如 `/runtime help`。
- 企业微信帮助卡第一页现在会显式提示
  `/help runtime` / `/runtime help`，
  运行时完整帮助文本也补齐了无损升级、强制升级、
  指定版本升级、版本列表和 changelog 查询等完整用法，
  降低“卡片能点，但具体命令细节要靠口头补充”的问题。

## v0.0.52 (2026-03-30)

- 企业微信 AI WebSocket 模式下，运行时完成通知现在会持久化发起该动作时
  的 `response_url`，并在新进程启动后复用这条一次性回复通道补发
  “已完成”消息；
  不再继续尝试跨进程复用旧 callback `req_id`，
  避免无损重启 / 升级明明已经成功，但完成通知仍然被企微回
  `846609 unsupported mcp biz type`。
- 企业微信帮助卡的左右翻页改成真正的全页环形循环：
  现在可以在第 1 / 2 / 3 页之间连续左翻或右翻，
  不会再出现离开第一页后只能在第二页和第三页之间来回切换的问题。

## v0.0.51 (2026-03-30)

- 企业微信 AI WebSocket 模式下，运行时完成通知不再尝试走
  `aibot_send_msg` 主动推送，而是改为复用发起该动作时的
  callback `req_id` 回消息；
  修复了实例已经成功无损重启 / 升级，但企微仍然没有收到
  “已完成”消息的问题。
  同时，旧版本遗留的仅带 `push_target` 的 notice 文件现在会被识别并
  清理，不再在新进程启动后反复刷
  `846609 unsupported mcp biz type`。
- 企业微信 `/status` 与状态卡片现在会显式展示当前运行版本，
  运行时卡片里的“操作人”也会优先使用企微身份解析结果；
  即使当前环境暂时没有 `wecom_user_lookup` 命令，
  只要本地已有 `user_identity_cache.json`，
  也可以继续复用缓存里的英文名 / 账号名，不再退化成只能显示原始
  `userid`。

## v0.0.50 (2026-03-30)

- 企业微信 AI WebSocket 模式下，运行时完成通知的主动推送帧
  现在改为和公开 SDK 一致的 `chatid + body` 结构，
  不再额外携带不被服务端接受的 `chat_type` 字段；
  修复了实例完成无损重启 / 升级后，后台日志里已经尝试发送完成通知，
  但企微回执 `846609 unsupported mcp biz type`，
  导致消息始终发不出来的问题。
- 企业微信帮助卡的分页翻页按钮现在会生成不同的 `prev / next`
  事件 key，即使两个按钮最终都跳到同一页，
  也不会再在卡片更新时因为 key 重复被企微拒绝；
  修复了帮助卡翻到后续页时后台回执
  `42039 Template_Card button_list.key Missing or Invalid`
  的问题。

## v0.0.49 (2026-03-30)

- 平台 `start.sh` 样板在每轮拉起前会显式导出它自己解析出的
  `TRPC_CLAW_STATE_DIR`、`TRPC_CLAW_CONFIG_PATH`
  等运行时环境变量：
  即使平台只挂了 binary 和配置文件、没有额外维护 env-file，
  子进程也能稳定完成模板里的 `${TRPC_CLAW_*}` 变量展开，
  降低“`start.sh` 自己能找到配置，但 claw 进程启动时报环境变量缺失”
  这一类问题。
  同时，`TRPC_CLAW_ENV_FILE` 指向的 env-file 现在会按 dotenv
  语义自动导出给子进程，不再要求平台把每个变量都手工写成
  `export KEY=VALUE`。
- 企业微信帮助卡改成了可翻页导航：
  默认第一页直接放常用入口和显式的 `🛠 运行时` 按钮，
  后续页再放会话、定时、工作区和控制类操作；
  不再把运行时入口主要藏在右上角小菜单里，
  降低“代码里有入口，但用户界面上看不见”的情况。

## v0.0.48 (2026-03-30)

- `trpc-claw` 新增了统一的 runtime lifecycle 管理层：
  当前可以跟踪已接收请求、在无损升级 / 重启前先进入 drain，
  在强制动作时向运行中请求注入取消，
  并把生命周期意图落盘给外层 `start.sh` 消费，
  不再需要让聊天侧去猜容器里的 `start.sh` 或手工 kill 旧进程。
- 企业微信新增了 `/runtime` 命令和运行时控制面板：
  当前支持无损重启、强制重启、无损升级 latest、
  强制升级 latest、指定版本升级、版本列表和 changelog 摘要查询；
  运行时控制权限也和普通聊天权限拆开成了独立的
  `runtime_admin_policy` / `runtime_admin_users`。
  指定版本升级当前只接受 `>= v0.0.48`，
  避免继续切到比这套 runtime lifecycle 契约更旧的分发版本。
  在 `bot_mode: ai` + `connection_mode: websocket`
  下，新进程建连成功后还会主动补一条“重启 / 升级已完成”的消息。
- 发布链路新增了平台 `start.sh` 样板和 `latest/releases.json`：
  发布包现在会一并带上可直接改造的平台入口脚本，
  镜像里的 `latest/` 和 `releases/<version>/`
  也会同步发布同一份 `start.sh` 样板，
  并在镜像侧发布机器可读的版本索引，
  给 `/runtime versions`、`/runtime changelog`
  和升级前的变更预告复用。
- 企业微信的 `/status` 和可选 `reply_prefix` 现在会展示最近一次已知的
  上下文占用：
  普通回复和 WebSocket 流式回复结束后都会尽量回填
  `12.3K / 200K` 这类“已用 / 窗口”信息，
  用户可以更直观看到当前会话离上下文上限还有多远；
  默认样例配置里的 `reply_prefix.fields`
  也同步把 `context` 纳入了推荐字段。
- 运行时文档 helper 的 shell wrapper 现在会在启动时自动生成：
  不再依赖 release 包额外携带一个无扩展名 wrapper 文件，
  降低某些打包、同步或权限链路里 wrapper 丢失后，
  文档读取辅助能力跟着失效的概率。

## v0.0.47 (2026-03-30)

- 企业微信引用消息在模型可见上下文里的投影进一步收敛为统一实现：
  私聊、群聊隔离历史、群聊共享历史三条链路现在都会稳定带上
  `Speaker`、`Quoted message`、`Message` 这些结构化上下文，
  降低“同样是引用回复，有时能识别原话、有时又看不到”的漂移。
- 内网分发版对 `trpc-agent-go` / `openclaw` 的 replace 依赖
  已从临时 fork 伪版本切回 GitHub upstream `main` 上已合入的正式提交，
  减少分叉依赖带来的漂移，后续追踪 upstream 行为会更直接。

## v0.0.46 (2026-03-30)

- 企业微信自然语言创建 cron 的提醒写作约束进一步补强：
  不再只区分“固定文本直发”和“未来任务执行”两类，
  还会明确要求模型对用户原始请求保持忠实，
  保留范围、对象、时间窗口和 checklist 细节；
  遇到长提醒、多要点提醒时，
  不应再擅自压缩成更短的 todo 概括，
  降低“提醒内容自己过多发挥、与原文不一致”的概率。

## v0.0.45 (2026-03-28)

- `exec_command` / `skill_run` 的运行时 PATH 发现逻辑补了一轮
  通用化修复：
  不再只依赖启动进程继承下来的 PATH 或固定的
  `state_dir` 默认目录，
  当前会优先围绕生效中的
  `TRPC_CLAW_TOOLCHAIN_DIR`、
  `OPENCLAW_TOOLCHAIN_PYTHON`、
  当前二进制位置和 runtime helper 位置来反推出可执行目录，
  降低“自定义 toolchain 已配置，但后续步骤还是找不到命令”的概率。
- 运行时对常见用户态二进制目录的发现范围继续扩大：
  除了原来的 `~/.local/bin`、`~/bin` 和 managed Python 外，
  现在还会自动补入 `~/go/bin`、`~/.cargo/bin`，
  以及 `GOBIN`、`GOPATH`、`GOROOT`、`CARGO_HOME`、
  `PNPM_HOME`、`NODE_HOME`、`N_PREFIX`、`VIRTUAL_ENV`
  这类环境变量推导出的 bin 目录；
  对镜像里额外的非常规前缀，
  也新增了 `TRPC_CLAW_EXTRA_PATH_DIRS`
  作为统一扩展入口，
  不再需要为单个工具逐个加硬编码。
- 配套的 Docker 运行时环境也补齐了显式元信息：
  镜像现在会导出 `GOROOT=/usr/local/go`
  和 `NODE_HOME=/usr/local/node`，
  即使后续链路被 login shell 或守护进程重置过 PATH，
  OpenClaw 仍然能更稳定地把 Go / Node 工具链重新发现回来，
  减少“镜像里明明装了，但会话里偶发找不到”的环境漂移问题。

## v0.0.44 (2026-03-27)

- `trpc-claw` 的文档处理链路新增了运行时自愈型 toolchain：
  启动时会在 `state_dir` 下预建 `tools/`、`toolchain/`、
  managed Python、字体目录和 OCR 语言目录，
  对话里临时补装出来的依赖后续步骤可以直接继续复用，
  不再经常出现“刚装完但下一步找不到命令 / Python 环境”的断链。
- 运行时文档 helper 进一步收敛成可直接调用的一条通用链路：
  当前可以在会话内主动探测 PDF / OCR / CJK 能力，
  下载 managed 中文字体、下载 `chi_sim` / `eng`
  语言包、补装 Python 文档依赖，并在发送 PDF 前做文本抽取 +
  预览 OCR 的双重校验，
  降低“导出成功但中文乱码 / tofu / OCR 丢字”的概率。
- 企业微信附件回传提示、coding runtime guidance
  和内置 PDF / DOCX / PPTX 技能的可靠性规则同步收敛到了同一套约束：
  当宿主机默认字体、系统库或 OCR 数据不可靠时，
  会更明确地优先走 user-space 自包含依赖和成品校验，
  而不是在第一次失败后直接放弃。

## v0.0.43 (2026-03-27)

- `openclaw` 对 GitHub `trpc-group/trpc-agent-go`
  的 replace 依赖统一同步到了 upstream `main`
  最新提交，`tool`、`session`、`memory`、`storage`
  和 `document reader` 这批共享模块的运行时行为
  继续和上游主线保持一致。
- 这次发布不再停在旧的 GitHub pseudo-version，
  后续内网 `trpc-claw` 的文件处理、工具编排和共享模块兼容性修复，
  会以最新上游代码为基础继续迭代。

## v0.0.42 (2026-03-27)

- 企业微信图片类附件的整理链路不再把 `png/jpg/jpeg/gif/webp`
  这类栅格图片先误判成“文档读取”任务；
  当前 turn 同时带图片输入和 companion file 时，
  会优先按图片内容理解，
  处理截图整理、图片转 Markdown 这类请求更稳定。
- OpenClaw 的 coding scratch 目录现在会在运行时预先创建：
  需要生成 Markdown、Word 等中间文件时，
  不再因为 scratch 根目录缺失而首轮写入失败。
- 企微附件回传和文件生成提示统一收敛到同一套“允许写入根”约束：
  除 workspace / scratch 外，
  已授权的可写 file-tool 根目录也会被视作合法回传来源；
  大段字面量产物则会更明确地优先走 file-writing tool，
  减少 here-doc / shell 参数过长导致的误拦截。

## v0.0.41 (2026-03-27)

- 企业微信会话人格的持久化模型补了一轮根因修复：
  session store 现在会区分“继承当前默认人格”和“显式锁定某个人格”。
  之前把默认人格直接写进会话状态的旧数据，
  升级后会自动迁移成继承态；
  历史单聊和群聊不再因为残留的 `snarky`
  或早期写死的 `pragmatic` 而一直卡在旧默认值上，
  会真正跟随当前默认的“务实”人格。
- 企业微信显式切换过的人格继续保持稳定：
  `/persona`、人格卡片和自定义人格仍然会把当前聊天锁定到所选 persona，
  不会被这次迁移误覆盖；
  删除当前正在使用的自定义人格时，
  也会回到当前聊天默认人格，
  而不是落回一套和默认配置脱节的旧值。
- 人格的运行时行为和展示链路统一收敛到“有效人格”：
  回复前缀、状态卡片、人格设置卡片选中态，
  以及发给模型的运行时 persona note
  现在都会基于同一套解析结果，
  减少“面板看起来是务实，实际回复还是毒舌”这类不一致。

## v0.0.40 (2026-03-27)

- 企业微信新会话的默认人格从“毒舌”切到了“务实”：
  默认回复会更偏简明、直接、目标导向，
  降低工作群里首轮回复阴阳怪气、铺垫过多的概率；
  需要别的风格时，仍然可以继续用 `/persona`
  切到毒舌、简洁、专业等其他人格。
- 企业微信 `reply_prefix` 的运行时默认值现在收敛为关闭：
  配置样例仍保留注释块形式，
  但只有显式写 `reply_prefix.enabled: true`
  才会在回复开头挂单行前缀，
  避免新环境默认多出额外展示信息。
- 企业微信自然语言创建 cron 时，会更明确地区分
  “未来要执行的任务”和“未来要原样发送的固定文本”：
  像“每隔 10 秒发一句 hello”
  这类纯文本定时发送请求，
  现在会更稳定地落成“按原文回复”的调度任务，
  不再容易在执行阶段把裸文本误解成 shell 命令。
- 企业微信当前聊天里的 cron 管理链路又补强了一轮：
  卡片和 `/cron` 的 stop / resume / remove
  现在会优先使用任务自身记录的 delivery target，
  不再误拿“当前聊天默认 target”去操作别的任务；
  任务详情也会补充 `run_count` / `max_runs`，
  已跑满次数的任务在恢复时会直接给出明确提示，
  不再表现成“按钮点了像没反应”。
- 企业微信 AI WebSocket 主动推送对“叫人”这件事现在改成诚实降级：
  当前链路不会再把 `<@userid>` 当成可靠硬 `@` 能力来依赖，
  创建 cron 时也会明确告诉模型该直接使用解析后的参与者规范称呼；
  当用户写 `@english(中文名)` 这类企微展示名时，
  进入模型前会先收敛成 canonical label，
  定时任务确认文案和实际发出的正文都会更稳定地优先使用英文账号名，
  而不是回抄中文名或原始 `<@...>` 文本。
- `trpc-claw upgrade` 现在支持显式指定版本：
  新增 `--version <tag>` 后，
  可以直接把当前二进制切到指定镜像版本，
  不再只能跟随 `latest/VERSION`；
  同时仍保持原有 `-f` / `--force-config`、
  `--profile` 的兼容语义。

## v0.0.39 (2026-03-27)

- 企业微信群聊的“当前会话定时任务回投”链路继续补强：
  当前聊天默认回投目标和显式传入的 WeCom 目标现在都会先
  规范化成真正可主动推送的单聊 / 群聊地址，
  不再把 thread / session 标识误当成最终发送目标，
  也不会再出现“建任务成功但运行时连续 delivery_failed”的坏任务。
- 企业微信共享会话现在支持按名字而不是纯 `Txxxx`
  渲染最近发言人：
  当前聊天里解析到的 participant label
  会同时进入共享历史和会话回顾工具，
  “哪些同事说过话”“刚才是谁问的”这类问题会更稳定。
- 企微身份解析能力收敛成平台可选增强：
  `trpc-claw`
  不再把某个特定员工查询 skill 当成内置能力随安装一起分发，
  只有平台显式提供命令或 skill capability 时才启用名字增强；
  没有能力时会完全退化成原始 `Txxxx` 行为。
- 统一的 `find-skills`
  已经覆盖 Knot / SkillHub 的检索与安装能力，
  仓库里不再额外保留单独的 `knot-skill-finder` source-tree skill，
  避免同一类能力在安装器里重复出现两套入口。
- Knot skill 安装链路修了一轮鉴权问题，
  之前需要额外补环境或切分支才能稳定下载 / 安装的场景，
  现在默认路径更顺；
  同时 `zhiyan-llm`
  也新增了 `WithBusinessScenario`
  这类更细粒度的业务场景透传能力。

## v0.0.38 (2026-03-26)

- 企业微信 AI WebSocket 里的“发到当前会话”定时任务修正了一轮：
  当前聊天默认回投目标现在会统一按官方主动推送链路需要的
  单聊 / 群聊目标格式生成，
  自然语言创建 cron 时不会再因为把会话 thread id
  当成最终发送地址而落成坏任务。
- 企业微信当前会话 cron 的目标校验也前置到了创建阶段：
  如果显式传入了不合法的 WeCom 发送目标，
  现在会在建任务时直接报错，
  而不是先显示“创建成功”，
  等到运行时再连续 delivery_failed。

## v0.0.37 (2026-03-26)

- 企业微信 AI bot 的交互入口做了一轮完整升级：
  `/welcome`、`/help`、`/persona`、`/status`、
  `/sessions`、`/workspace`、`/cron list`
  现在优先走更适合手机端点击的控制卡片，
  同时保留 slash command 作为精确控制入口。
  额外补了 `/help all`，
  需要看完整文本命令列表和细节说明时可以直接展开全文。
- 企业微信的人格系统继续收敛成“会话级当前人格”：
  内置人格排序更贴近日常使用，
  新增了默认内置的“男友”，
  用户首次接触到的默认人格切成“毒舌”，
  `/name`
  和自然语言改名也会把助手称呼持久化到当前聊天。
  欢迎卡片、人格卡片和回复前缀都会统一使用这个会话内名字。
- 企业微信模板卡片和命令快路径的可用性补强了一轮：
  卡片按钮、下拉选项和展示文案现在会统一按企微上限裁剪，
  人格卡不会再因为 option 过多导致点击无效，
  `@机器人 /xxx`
  这类带 mention 的 slash command 也会稳定走 fast path。
- 企业微信 AI WebSocket 的主动能力更完整了：
  cron 定时任务现在能把当前聊天作为默认回投目标，
  并通过官方主动推送链路发回单聊或群聊；
  同时补了统一的 `/cron`
  管理入口，常见的 list/status/stop/resume/remove/clear
  都能直接在聊天里管理。
- 企业微信共享会话和当前会话回顾能力也更稳定了：
  群共享模式下会按群级 session 保存上下文，
  当前发言人的 speaker/quote 信息会跟着历史一起进入会话视图，
  “刚刚都聊了什么”“谁刚才问的”这类问题不再只能依赖长期记忆。
- 企业微信媒体输入和错误可观测性做了兼容性补强：
  mixed 消息里的 GIF / 自定义表情现在会统一按真实媒体内容处理，
  上游 provider 的原始错误文本也会尽量原样返回，
  排查问题时不再只剩一条泛化的“上游连接异常”。
- DeepSeek 系列模型在企业微信里的兼容性补了一层输出边界清理：
  回复尾部泄漏的控制 token 会在发送前统一裁掉；
  当前 active persona 也会在运行时 prompt 里被提升成更明确的第一优先级，
  并显式声明覆盖旧历史里的语气残留。

## v0.0.36 (2026-03-25)

- 企业微信 AI bot 现在会把当前运行时模型名同步展示到
  启动日志、消息解析日志、原生欢迎卡片和 `/status`
  输出里；排查“当前实际跑的是哪个模型”
  不再只能靠环境变量和外围日志猜。
- `enter_chat`
  欢迎语链路的可观测性也补强了一轮：
  欢迎语发送、禁用、白名单拦截，
  以及 WebSocket 模式下
  `req_id` / sender 缺失导致无法回欢迎语的分支，
  现在都会输出更明确的诊断日志，
  更容易区分是平台“当天首次进入单聊才触发”的语义，
  还是本地发送链路本身出了问题。
- 企业微信 AI channel 的运行时附加提示现在统一走请求级
  system prompt：
  人格、工作区、回传附件能力、当前轮附件别名这些运行时事实
  不再拼进用户原始文本，
  channel 也不再靠“PDF / 视频 / 文档 / 代码任务”这类关键词猜任务类型。
  这样 persona 风格更稳定，
  中间 comment 和最终回复也更不容易夹带 transport /
  workspace 这类内部提示。
- 企业微信最终回复不再因为正文里出现
  `connection refused`、
  `timed out`
  这类技术词就被误判成“上游连接异常”。
  现在只有真实的 gateway / channel 错误会被映射成中文失败提示；
  正常的技术分析、排障过程和最终答案会按原文发送给用户。
- 企业微信 WebSocket 流式日志现在会自动忽略纯
  `.` / `..` / `...`
  心跳帧，
  运行工具阶段的默认占位文案也收敛成更中性的状态描述，
  长任务排障时不再被大量无信息量日志刷屏。
- 默认 coding-agent prompt 现在明确要求：
  只要请求基于本地仓库、路径、文件或当前源码，
  就必须先做一次 fresh inspection，
  并优先用
  `rg --files` /
  `rg -n`
  在目标仓库内做最小范围搜索；
  这样能降低只凭旧上下文回答、
  或把搜索跑到仓库外面的概率。

## v0.0.35 (2026-03-24)

- OpenAI 默认 profile 里的 `model.name` / `model.base_url`
  现在统一收敛成显式环境变量引用：
  `name: "${OPENAI_MODEL}"`
  和 `base_url: "${OPENAI_BASE_URL}"`。
  启动时会先对 YAML 里的环境变量占位符做预处理展开，
  再把最终值传给下游模型 SDK；
  不会再把 `${...}` 字面量漏到运行时，
  也不再在 profile 里隐藏默认模型名或默认地址。
- 如果 `OPENAI_MODEL` 或 `OPENAI_BASE_URL`
  没有准备好，启动现在会直接报错，
  比起“启动成功但下游才因为解析占位符失败”更容易定位。
- 安装器、README、企微文档和分发 profile
  也同步切到了这套“显式 env、缺失即报错”的约定。

## v0.0.34 (2026-03-24)

- 默认分发的 OpenAI 系列 profile 现在统一改成环境变量驱动：
  `model.name`
  会优先读取 `OPENAI_MODEL`，
  `model.base_url`
  会优先读取 `OPENAI_BASE_URL`，
  同时仍保留 `gpt-5.2`
  和官方 API 地址作为默认值；
  这样安装脚本下发出来的
  `openclaw.yaml`
  就能和用户实际的
  `OPENAI_MODEL` /
  `OPENAI_API_KEY` /
  `OPENAI_BASE_URL`
  环境变量约定保持一致。
- 安装器、README、企微文档和 runbook
  也同步补齐了这套模型环境变量说明，
  默认 profile
  的可扩展性更好，
  启动前不需要再手工改一处写死的模型名。

## v0.0.33 (2026-03-24)

- 企业微信 `/status`
  和工作区恢复提示里的工作区展示文案继续收敛：
  默认代码工作区仍然会按原路径生效，
  但用户侧不再额外显示
  “运行时默认”
  这层技术前缀，
  避免非技术用户在查看状态时先看到一串实现细节。

## v0.0.32 (2026-03-23)

- 企业微信 AI bot 的会话入口和交互层做了一轮完整升级：
  `enter_chat`
  现在会按企微原生协议发送欢迎卡片，
  WebSocket 模式走
  `aibot_respond_welcome_msg`，
  webhook 模式走加密被动回复；
  同时 `/help`
  改成完整文本帮助，
  默认欢迎语、常驻回复前缀和快捷入口也统一变得更友好。
- 企业微信 slash 命令的人格体系已经收敛为单一的
  `persona`
  概念：
  内置人格扩展为
  `friendly`、`pragmatic`、`professional`、
  `concise`、`coach`、`creative`、`candid`、
  `quirky`、`nerdy`，
  并支持把自定义人格持久化到
  `state_dir/wecom/personas/`
  下的文件里；
  WebSocket 模式下，
  `/persona`
  也会优先回复企微原生交互卡片，支持下拉选择并原地更新。
- 企业微信回复前缀现在默认开启，
  且默认只展示更紧凑、更用户友好的信息：
  单行 `> ... | ... | ...`
  前缀会把当前人格、常用命令和自定义链接放在同一个消息气泡里，
  不再默认刷完整 workspace / git 路径，
  也支持通过 `reply_prefix`
  配置字段、顺序、提示语和链接入口。

## v0.0.31 (2026-03-23)

- OpenClaw 现在可以直接接 Langfuse：
  开启 `observability.langfuse`
  后，请求级 trace 会通过 OTel exporter 上报到 Langfuse，
  admin / debug 页面也会保留 `trace_id`
  和可跳转的 Langfuse 链接，
  排查模型调用、工具调用和整轮请求链路时，
  不再只能翻本地 debug events。
- admin 侧会同时展示 Langfuse 是否启用、是否 ready、
  UI base URL 和 trace 链接模板；
  Langfuse 初始化失败但不是 required 的场景，
  也会把错误状态保留下来，避免“看不到 trace 但也不知道为什么”。
- `trpc-claw` 停止进程时，
  第一次 `Ctrl-C`
  会继续走优雅退出；
  如果还需要立刻结束，
  再按一次 `Ctrl-C`
  就会直接强制退出，
  不再只能一直等 server / channel 自己收口。

## v0.0.30 (2026-03-20)

- 企业微信 AI bot 的同会话附件回传提示
  不再依赖“发回来”“附件”这类关键词命中。
  现在每轮请求都会稳定注入 transport note，
  明确 websocket 模式该用
  `[WECOM_FILE:/absolute/path]`
  或独立一行的
  `MEDIA:/absolute/path`
  回传同会话附件，
  降低生成完文件却走错发送链路的概率。
- 企业微信用户侧失败提示现在会按问题类型输出更具体的中文文案，
  例如上游超时、连接异常、执行环境异常、
  当前会话回传通道异常，
  并附带短错误 ID；
  同时后台日志会保留原始错误和 request_id，
  排查时不再只能对着
  “处理消息失败，请稍后重试。”
  这类泛化提示猜原因。
- `admin.addr`
  和
  `admin.auto_port`
  现在已经真正接到
  `cmd/openclaw`
  的启动链路里，
  admin handler 会作为独立 HTTP server 启动，
  端口占用时也会按 `auto_port`
  做顺延回退并在退出时一起 shutdown。
- 默认 coding workspace 的解析规则已经统一：
  显式配置
  `skills.coding_agent.default_workdir`
  就用它，
  否则默认直接使用进程 `cwd`。
  企业微信 `/workspace`
  默认值、运行时 coding prompt、
  回复里的工作区上下文和 scratch root
  现在全部读同一份解析结果，
  不再分裂成 runtime 一套、WeCom 一套。

## v0.0.29 (2026-03-20)

- 企业微信 AI WebSocket 自动回传附件这条链继续补强：
  现在除了 ``[WECOM_FILE:...]``，
  也兼容独立一行的 ``MEDIA:/absolute/path``；
  回传前会额外发送
  “正在回传附件 1/N ...” 这类进度快照，
  不再长时间停留在泛化的处理中提示。
- 自动回传失败时，用户侧会看到更具体的原因，
  例如文件过大、空文件、超出允许工作区、
  当前发送方式不支持自动回传，
  并且会按图片 / 语音 / 视频类型给出更贴近企微限制的处理建议。
- 企业微信流式回复现在会拦截并收敛上游 provider /
  gateway 直接透出的泛化报错，
  比如 `stream error`、`INTERNAL_ERROR`、
  `Please contact the service provider`；
  如果还能从非流式回退链恢复到真实结果，
  会优先把真实回复补回来，而不是把内部错误原样暴露给用户。
- 对“生成文档 / 视频 / 语音 / 回传附件”这类请求，
  早期和处理中状态会给出更具体的阶段提示，
  降低长时间只看到“正在准备请求...”的体感。
- `lover` persona 遇到明确的文件、代码、构建、
  命令或排障请求时，
  会自动收敛成更实操的答复风格，
  不再把浪漫化语气带进执行类任务。

## v0.0.28 (2026-03-19)

- 修复企业微信 AI WebSocket `stream`
  回复没有等待平台 ACK 就继续发下一帧的问题，
  降低 `6000 data version conflict`
  导致最终结果不可见的概率。
- 处理中占位和进度快照末尾的
  `. .. ...`
  动画现在只会在快照真正发出后才推进，
  不会再因为内部节流跳帧。

## v0.0.27 (2026-03-19)

- 企业微信流式回复默认快照模式改为 `content_only`。
- yaml 中不再显式配置 `stream_snapshot_mode`
  时，默认只发送正文快照和最终帧，
  不再默认发送 placeholder / progress 这类早期 stream 帧。
- 这会显著减轻企业微信多端查看历史消息时，
  因早期快照被重新按顺序渲染而产生的“又流了一遍”的体感。
- 如需恢复旧行为，
  现在必须显式配置
  `stream_snapshot_mode: "full"`；
  如需最保守行为，
  可配置
  `stream_snapshot_mode: "final_only"`。

## v0.0.26 (2026-03-19)

- 修复 `trpc-claw upgrade`
  在当前 shell 工作目录已经失效时，
  启动安装脚本会先打印
  `shell-init: error retrieving current directory`
  / `chdir: error retrieving current directory`
  这类噪音错误的问题。
- 现在 `upgrade`
  会使用稳定的工作目录拉起安装脚本，
  不再直接继承用户当前可能已经失效的 `cwd`。

## v0.0.25 (2026-03-19)

- `bootstrap deps` / `inspect deps`
  这条链已经对齐到当前 GitHub 主线官方 OpenClaw skills
  正在实际使用的 metadata 规范，
  包括 `brew`、`apt/dnf/yum`、`go`、`node/npm`、
  托管 Python、`download`、`tap`、action 级 `os`
  和 `stripComponents/targetDir`。
- 显式 `-skill ...`
  不再隐式夹带默认 dependency profiles，
  skill 专项排查时输出更干净。
- `bootstrap deps --apply`
  现在按 best-effort 执行：
  用户态安装和下载先跑，
  root 步骤会汇总为 deferred，
  不会第一步就整次退出。
- 下载型 skill 的产物会统一落到
  `state_dir/tools/<skill>/...`。
- `--bundled`
  选择集现在会继续按当前 OS 过滤，
  Linux 下不会再把 macOS-only 的 bundled skills
  一起塞进依赖计划里。
- `coding-agent` 默认提示现在会更像一个本地代码代理：
  会注入默认代码工作区、git root、最近的 `AGENTS.md`
  和 scratch repo 根目录，
  也会更明确要求先检查仓库、再改代码、最后用工具输出确认结果。
- 企业微信 AI bot 新增
  `/workspace <目录|off>`
  以及 `/repo`、`/cwd` 兼容别名，
  可以按聊天维度切换代码工作区，
  让用户直接在企微里指定当前要操作的仓库。
- 当默认 coding workspace 已经配置，
  且 `fs_*` 文件工具还停在模板默认的
  `${TRPC_CLAW_STATE_DIR}` 时，
  启动会自动把 file tool 的 `base_dir`
  同步到默认 workspace，
  并且像 `read_document("0.pdf")`
  这类上传后的相对文件名也会优先按当前聊天上传上下文解析，
  降低代码仓库任务和附件任务走错路径的问题。
- 企业微信流式回复的中间状态现在更具体，
  至少能区分读取文件、保存文件、检查工作区、
  `go test` / `pytest` / `npm test` / `git`
  这些常见阶段；
  同时对非终态快照增加了节流，
  减少企业微信 WebSocket `6000 data version conflict`
  导致的状态丢失。
- 企业微信回复开头的 `工作区: ...`
  现在改成显式配置项
  `show_reply_workspace_prefix` 控制，
  模板里默认注释掉，运行时默认也关闭，
  不会再无条件把工作区信息插到每条回复最前面。
- 普通 fenced code block
  不再被 `postprocessing.code_execution`
  误当成可执行代码吞掉，
  裸绝对目录路径也不会再误触发
  `tool.result.images` 自动附图。
- gateway / 企业微信取消请求的终态现在会明确显示“已取消当前请求”，
  不再收尾成 `I didn't produce a visible reply. Please try again.`
  这类误导性空回复。

## v0.0.24 (2026-03-18)

- 默认模板里的 `agent.system_prompt`
  统一改成 `You are tRPC-Claw.`，
  与对外产品名保持一致。
- 启动时会把当前运行时的模型信息注入到 system prompt，
  包括 `mode`、`model name`、`openai_variant`、
  `base_url` 等，降低长会话里模型自报身份跑偏的问题。
- `sqlitevec` memory 新增
  `fallback_to_sqlite_on_embedding_unsupported`，
  默认开启。
- 当当前 embeddings 配置看起来会落到
  DeepSeek 这类不提供 `/embeddings` 的 OpenAI-compatible
  endpoint 时，
  启动阶段会自动把 memory backend 从 `sqlitevec`
  降级到 `sqlite`，
  并打印 warning，避免每轮 `auto_memory`
  都反复报 404。
- 当前仓库已经直接切到 GitHub 主线合入后的
  `memory/sqlitevec` 自动迁移版本，
  不再依赖 `openclaw/third_party/memory/sqlitevec` 这份临时 vendored
  副本。
- `inspect deps --bundled` / `bootstrap deps --bundled`
  现在除了显式 `state_dir`，
  还会额外尝试 `TRPC_CLAW_STATE_DIR`、`sudo` 原用户安装目录、
  以及源码树下的 `openclaw/skills`，
  降低 `sudo trpc-claw ... --bundled` 和仓库内开发时找不到
  bundled skills 根目录的问题。
- 默认 skills 搜索路径现在会自动补上 `~/.codex/skills`，
  让 Codex / skill hub 安装下来的额外 skill 更容易直接生效。

## v0.0.23 (2026-03-18)

- 修复 `v0.0.22` 在已有 `sqlitevec` 长期记忆库上启动失败的问题。
- 启动时如果检测到旧版 `memories` 表 schema，
  现在会自动把历史数据迁移到新版 schema，
  尽量保留已有长期记忆内容，而不是直接要求用户手工删库重建。
- 如果上一次迁移在中途被打断，
  下次启动会优先从内部备份表恢复，
  降低升级过程中出现半迁移状态后无法启动的风险。

## v0.0.22 (2026-03-18)

- 企业微信 AI WebSocket 单聊里，
  首条纯文本现在会尊重配置的 `aggregate_window`，
  不再因为内部 500ms 快路径把“文字 + 附件”过早拆成两轮请求。
- 企业微信附件预抓取现在会把下载结果缓存到
  `state_dir/wecom/media_cache/`，
  就算前面有长任务排队，也能继续读取已经拿到的附件，
  降低 5 分钟临时 URL 过期导致的“无法读取当前附件”。
- 同一会话排队中的 WebSocket 请求不再额外提前打开第二条
  stream，减少企业微信 `6000 data version conflict`
  导致的前端状态错乱。
- 长时间运行的 WebSocket stream 到安全时长后会自动退化成
  markdown 终态回复，降低企业微信 `stream expired`
  导致最终结果不可见的问题。

## v0.0.21 (2026-03-16)

- `trpc-claw` 启动时现在会先检查当前版本和镜像里的最新版本。
- 如果发现有新版本，
  启动日志会直接提示当前版本、最新版本，
  并给出 `trpc-claw upgrade` 和 `trpc-claw upgrade -f`
  两种升级命令。
- 版本提示会附带最新 release 的简要变更说明，
  帮助用户快速判断是否值得立刻升级。
- 发布脚本现在会把 `CHANGELOG.md`
  一起上传到镜像的 `latest/` 和对应版本目录，
  方便运行中的旧版本读取最新 release 说明。

## v0.0.20 (2026-03-16)

- 企业微信 AI WebSocket bot 新增
  `/session`、`/sessions`、`/switch`
  等会话管理命令，
  可以直接查看当前会话、回看最近会话并切回历史上下文。
- 会话历史现在会随状态一起持久化，
  重启后仍可继续查看最近会话列表，
  也能切回之前的对话分支。
- 新增 `/personas` 和 `/persona <名称|off>`，
  可以按聊天维度临时切换 concise、coach、creative
  等回复风格，而不需要改全局配置重启服务。
- `/clear` 和 `/history` 作为兼容别名同步可用，
  方便沿用更贴近用户习惯的 slash 命令。

## v0.0.19 (2026-03-16)

- 企业微信 AI WebSocket bot 新增进群欢迎消息，
  默认会提示常用 slash 命令，
  也可以通过配置关闭。
- 企业微信会话超时自动分会话默认关闭，
  只有显式配置 `session_timeout` 时才开启；
  `/recall` 也改为只切回显式 `/new` 前的上一会话。
- 启动时会额外阻止本地多实例误抢同一个 bot 或复用同一监听端口，
  降低消息乱序、重复消费和静默串流的风险。

## v0.0.18 (2026-03-16)

- 默认模板里的 `session.summary.enabled`
  现在改为 `true`，新安装和覆盖配置后会默认开启会话摘要。
- 默认模板里的 `memory.auto.enabled`
  现在改为 `true`，新安装和覆盖配置后会默认开启自动长期记忆抽取。
- `README.md` 里关于默认 Session / Memory 行为的说明
  同步更新为新版默认值，避免文档和实际模板不一致。

## v0.0.17 (2026-03-13)

- 企业微信 AI 长连接现在会给多文件合并请求更明确的早期反馈，
  会先提示“已收到文件”“正在检查可用合并工具”，
  用户不再长时间只看到泛化的处理中状态。
- PDF 合并类请求会追加更强的执行提示，
  优先让 Agent 先检查并使用 `pdfunite`、`qpdf`、`gs`
  等本地工具，减少无谓地反复试探 Python PDF 包。
- 对外回复不再原样暴露内部 fallback 文件名
  `attachment.pdf` 这类占位名，
  会改成“第 1 个上传的 PDF”这种更贴近用户视角的表述。
- 流式阶段摘要对 `exec_command` 这类内部 tool 名也做了本地化，
  不再直接把内部实现名展示给企微用户。

## v0.0.16 (2026-03-13)

- 修复企业微信 AI 长连接里 `文件 + 文本 + 文件`
  被过早拆成两轮请求的问题。
- text+attachment 批次的 settle 策略更稳，
  trailing file 不会再因为晚几十毫秒就掉成下一轮。
- 当前轮附件请求会额外带上“只使用本轮附件”的作用域提示，
  降低误把旧会话生成物当作本轮输入的概率。
- 同一轮里若多个附件最终落成相同文件名，
  会自动做去重编号，避免覆盖和歧义。

## v0.0.15 (2026-03-13)

- 默认 bundled skills 开始内置 Anthropic 官方 skills 快照。
- Anthropic 那组 skills 统一改成 `anthropic-*` 命名空间，
  避免和已有 OpenClaw / 本地自定义 skills 撞名。
- `inspect deps` / `bootstrap deps` 新增 `--bundled`，
  可以一键按当前 bundled skill pack 规划依赖。
- 安装脚本的 `--bootstrap-deps` 现在也会走 `--bundled`，
  对默认技能包更对齐。
- 这一轮依赖安装仍然保持保守：
  只自动处理系统包和托管 Python 包，
  浏览器 runtime、全局 npm 安装、凭据配置继续手动。

## v0.0.14 (2026-03-13)

- `install.sh` 和 `trpc-claw upgrade` 支持 `-f`，
  等价于 `--force-config`。
- `upgrade -h`、`trpc-claw -h`、`README.md`、
  `INSTALL.md`、`RELEASE.md` 的示例统一补齐短写法。
- 覆盖配置时可以更直接地执行
  `trpc-claw upgrade -f --profile wecom-ai-websocket`。

## v0.0.13 (2026-03-13)

- 修复企业微信单聊在进程重启后丢会话上下文的问题。
- 默认根会话改为稳定 session ID，重启后还能续接历史。
- `/new`、`/recall`、超时分会话的 active session
  映射会持久化到
  `state_dir/wecom/session_tracker.json`。

## v0.0.12 (2026-03-13)

- 重点优化企业微信 AI 长连接 WebSocket 交互体验。
- 新增 `/status`，可以查看当前阶段、排队状态和最近输出。
- `/recall` 接入实际行为，`/help` 说明改成面向长连接场景。
- 更早显示排队、附件处理、运行阶段摘要和中间输出，
  降低“长时间没有反馈”的体感。

## v0.0.11 (2026-03-13)

- 修复 `v0.0.10` 中 `inspect plugins`、
  `inspect deps`、`bootstrap deps`
  被错误注入临时 `-config` 的回归问题。
- 恢复安装、inspect、bootstrap 等常见 smoke 流程。

## v0.0.10 (2026-03-13)

- 补强默认配置和 CLI 帮助体验。
- `trpc-claw -h` 开头增加常用命令、自动发现到的配置路径、
  升级覆盖配置示例。
- `inspect`、`bootstrap`、`pairing` 等子命令 help
  展示更完整的子命令树和典型用法。
- 安装、升级、`--force-config` 的使用文档更系统。

## v0.0.9 (2026-03-13)

- 默认安装模板全面补齐并默认打开常用能力。
- 默认 tools 打开 `enable_local_exec`、
  `enable_parallel_tools`、`duckduckgo`、
  `webfetch_http`、`file`、`wikipedia`、`arxivsearch`。
- 默认 skills 同时启用 bundled 和 local 两套目录。
- 默认 memory 改为 `sqlitevec`，默认 persona
  可直接通过配置切换。
- 修复注释里的 `${...}` 也会触发环境变量替换的问题。
- 默认路径更多走 `TRPC_CLAW_STATE_DIR`，
  减轻对 `HOME` 的硬依赖。

## v0.0.8 (2026-03-13)

- 默认安装包开始内置更完整的模板、skills 和默认能力。
- 企微 WebSocket 模板补齐更多 `skills`、`tools`、
  `memory`、`persona` 相关配置说明。
- 为后续默认安装“开箱即用”能力增强打下基础。

## v0.0.7 (2026-03-12)

- 安装脚本不再创建 `openclaw` alias，
  安装结果只保留 `trpc-claw`。
- 从旧版本升级时，会尽量清理旧安装脚本遗留的 alias。

## v0.0.6 (2026-03-12)

- `install.sh` 提升了对 Linux / macOS、
  老版本 `sha256sum`、`curl` / `wget`
  以及缺少 `install` 命令环境的兼容性。
- 新增 `--bootstrap-deps` 和 `--deps-profile`，
  便于补齐常见文件处理依赖。
- CLI 侧开放 `bootstrap deps`，
  依赖安装体验与 GitHub 版更对齐。

## v0.0.5 (2026-03-12)

- 当前内网镜像发布线的早期稳定版本。
- 引入 `trpc-claw upgrade` 自升级能力。
- 对齐 GitHub OpenClaw 基线，补上企业微信
  WebSocket 流式回复能力。
