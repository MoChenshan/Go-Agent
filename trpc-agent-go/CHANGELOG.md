# Change Log

## [1.9.2](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.9.2) (2026-05-19)

### Bug Fixes

- **ecosystem/codeexecutor**: 默认延迟初始化 pcg123 sandbox，避免低频
  使用 code execution 的多副本服务提前占用空闲沙箱；保留
  `WithLazyInit(false)` 以恢复启动时 fail-fast 行为 (!689)
- **ecosystem/codeexecutor**: 使用已有 NFS client 执行真实 health check
  RPC，避免只检查 endpoint 可达性而漏掉长连接异常 (!694)
- **pcg123**: 过滤 workspace 根目录下由运行时生成的 metadata 临时输出，
  避免 `Collect` / `CollectOutputs` 将内部临时文件返回给调用方 (!698)

## [1.9.1](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.9.1) (2026-05-11)

### Dependencies

- dependencies: 对齐 GitHub root tag `v1.9.1`，将
  `trpc.group/trpc-go/trpc-agent-go` root 模块依赖升级到 v1.9.1
- examples: 将同步上游提交时遗留的 `v1.9.1-0.20260509092836-a5463fa0f90f`
  伪版本收敛为正式 tag；root 模块使用 v1.9.1，子模块继续使用已发布的
  v1.9.0 tag

## [1.9.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.9.0) (2026-05-08)

### Features

- **codeexecutor**: 对齐 GitHub v1.9.0，新增 E2B sandbox codeexecutor，
  支持远端沙箱代码执行，并补充 workspace bootstrap、init hooks 与
  resolver artifact injection 能力 (#1722, #1660, #1693)
- **model**: 对齐 GitHub v1.9.0，新增 AWS Bedrock、DeepSeek v4、
  Anthropic adaptive thinking / streamed tool argument deltas、Hedge model、
  request extra fields、reasoning token 与 timing 回调等能力
  (#1748, #1694, #1699, #1719, #1621, #1751, #1703, #1745)
- **memory / session**: 对齐 GitHub v1.9.0，新增 mem0、MySQL vector
  memory、noop session service、on-demand session recall、summary filter
  allowlist controls 等能力 (#1538, #1597, #1714, #1628, #1663)
- **knowledge**: 对齐 GitHub v1.9.0，新增 Docling extractor、AST repo
  indexing、code indexing tool，优化 knowledge tool schema 与 embedding
  dimensions 兼容性 (#1588, #1610, #1652, #1636, #1666)
- **server/agui / evaluation**: 对齐 GitHub v1.9.0，新增 prompt iter、
  Langfuse remote experiment、metric routes、tool argument streaming、SSE
  heartbeat、batched tool results 和 run lifecycle events 等能力
  (#1447, #1637, #1684, #1717, #1705, #1706, #1711)
- **agent / runner / processor**: 对齐 GitHub v1.9.0，新增 per-request app
  name override、per-run code executor overrides、user message rewriter 和
  on-demand session recall 等能力 (#1586, #1599, #1685, #1628)
- **a2a / graph / tool**: 对齐 GitHub v1.9.0，增强 transfer graph
  interrupt、custom data part handler、response rewrite、foreign messages
  preserve、`todo_write`、Claude Code toolset、MCP broker 低层 client
  option 注入等能力 (#1581, #1627, #1679, #1639, #1661, #1656, #1727)
- **openclaw**: 新增微信通道、稳定微信 QR 入口、企微激活管理 API、
  env-gated dual channels 和私有 MCP 配置优先级等内网能力
  (!623, !637, !634, !639)
- **openclaw**: 增强 Skill-first 能力路径、可配置 skill tool profile、
  runtime prompt hot reload、runtime config、memory admin、chat history 等
  管理面能力 (#1596, #1608, #1626, #1650, #1724, !603, !607, !608,
  !610, !631, !663)
- **trpc/telemetry/zhiyan-llm**: 对齐 `llm_go_sdk` 语义，补充 tool call
  属性上报，并发布 `trpc/telemetry/zhiyan-llm/v1.8.1` ~ `v1.8.2`
  (!621, !633, !635)
- **ecosystem/codeexecutor**: 重构 NFS / RPC 执行链路，支持 multiplexed
  Transport layer，增强输入暂存与执行通道复用能力 (!598)

### Examples

- examples: 持续同步上游 `trpc-group/trpc-agent-go` v1.9.0 相关示例代码
  (!600, !606, !613, !619, !622, !630, !632, !636, !640, !642, !646,
  !648, !658, !662, !666, !670, !676, !678)
- examples: 对齐 GitHub v1.9.0，新增或更新 streamtool、SBTI、
  sqlitevec、code execution、A2UI、todo、knowledge、evaluation 和 AG-UI
  示例 (#1609, #1625, #1632, #1669, #1676, #1717, #1661, #1734, #1755)

### Bug Fixes

- **trpc**: 修复 `CloneContext` 丢失 OpenTelemetry span 的问题 (!624)
- **trpc/internal/http**: 允许 `polaris://` host label 使用下划线，
  恢复内网 Polaris 域名兼容性
- **agent/knot**: 修剪 SSE data payload，避免下游解析异常 (!655)
- **wecom**: 避免 final stream fallback 重复投递 (!653)
- **wecom**: 简化外部信息检索判定策略，降低误触发风险
- **model/openai**: 对齐 GitHub v1.9.0，修复非标准 HTTP 200 embedded
  error、mixed reasoning chunks、zero tool call delta index、non-JSON stream
  events 和 DeepSeek tool reasoning backfill 等兼容性问题
  (#1633, #1641, #1709, #1741, #1732)
- **runner / processor / graph / session**: 对齐 GitHub v1.9.0，修复
  tool state delta、graph session state、`WithMessages` dedupe、Redis
  `KEEPTTL`、summary threshold 等兼容性问题
  (#1620, #1618, #1691, #1654, #1648)
- **a2a / agui / telemetry**: 对齐 GitHub v1.9.0，修复流式事件顺序、
  tunnel transfer、reasoning role、telemetry error label 和 structured
  response error 上报等问题 (#1611, #1689, #1607, #1612, #1624)

### Docs

- docs: 对齐 GitHub v1.9.0，更新 session management、reasoning / usage、
  memory、DeepSeek v4、AG-UI、code execution、tool callback 和 external
  module consumption check 等文档
  (#1669, #1688, #1701, #1715, #1726, #1729, #1738, #1744, #1743, #1755)
- docs: 同步 extractor 文档到 iWiki，补充 HY3 preview `/openapi/v2`
  BaseURL、微信前端绑定状态 API 和 OpenClaw namespaced release tag 文档
  (!667, !647, !641, !651)

### Dependencies

- dependencies: 对齐外网 GitHub root tag 与子模块 tag `v1.9.0`，统一将
  所有 `trpc.group/trpc-go/trpc-agent-go` 相关依赖升级到 v1.9.0（包含
  子模块与示例） (!680)

### Misc

- **openclaw**: 发布 `trpc-claw v0.0.65` ~ `v0.0.93`，更新内网镜像、
  release notes、预览 runtime channel 和默认运行配置 (!601, !615,
  !650, !660)

## [1.8.1](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.8.1) (2026-04-07)

### Bug Fixes

- **openclaw**: 加强企微媒体下载 URL 校验，并将容器镜像默认用户切换为
  专用非 root 账号，提升运行时安全性 (!587)
- **examples/skill, examples/telemetry/agent**: 收紧文件读取和文件工具路径
  校验，拒绝路径穿越与非受控绝对路径访问，并补充回归测试 (!587)
- **wecom**: 收紧自主补全 fallback 识别，仅对明确的用户补充请求触发恢复，
  避免将正常进度说明误判为 fallback (!588)
- **wecom**: 补回“稍后查看 / 稍后交付”等延后执行回复的恢复逻辑，避免在
  尚未产出结果时错误结束当前轮次 (!590)
- **trpc/internal/http**: 恢复显式 `dns://host[:port]` BaseURL 兼容，
  继续限制 `ip://`、`unix://` 和 `passthrough://` 的自动映射 (!593)

### Docs

- docs: 对齐 `WithBaseURL(...)` 路由语义说明，补充 `dns://` selector
  与 `polaris://` 使用约束，修正文档与代码不一致问题 (!593)

### Dependencies

- dependencies: 更新 go mod 依赖版本

## [1.8.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.8.0) (2026-04-07)

### Features

- **trpc/model/hunyuan**: 新增搜索增强选项，支持混元模型搜索增强能力
  (!506)
- **trpc/model/taiji**: 新增 `WithQueryID` 配置项，支持太极模型请求
  级别的 QueryID 透传 (!491)
- **trpc**: 新增 OpenAI 错误兼容选项，支持将 OpenAI 格式错误映射到
  tRPC 错误码体系 (!570)
- **zhiyan-llm**: 新增 `WithBusinessScenario` 配置项，支持按业务场景
  区分智研 LLM 上报 (!533)
- **openclaw**: 实现文件级 Memory 后端，支持持久化记忆存储 (!528)
- **openclaw**: 新增运行时生命周期控制能力，支持优雅启停 (!545)
- **openclaw**: 新增环境探测工具，支持运行时环境信息采集 (!552)
- **openclaw**: 自动引导浏览器运行时，支持 managed browser 自动
  初始化和清理 (!559, !562)
- **openclaw**: 增强上下文窗口使用量计算与自动压缩，显示百分比
  状态信息 (!548, !551, !561)
- **openclaw**: 支持模型生成配置（generation config），提供更灵活的
  推理参数控制 (!564)
- **openclaw**: 跨 turn 持久化已加载 Skill，避免重复加载 (!566)
- **openclaw**: 增强自主目标完成能力，清理硬编码和无用代码
  (!582, !584)
- **openclaw**: 限制 webfetch 载荷大小并启用上下文压缩 (!581)
- **openclaw**: 运行时工件与工作区隔离，保持工作目录整洁 (!573)
- **openclaw**: 运行时生成 doc helper wrapper (!549)
- **openclaw**: 注入 memory prompt 到代码上下文 (!507)
- **openclaw**: 增强指令级记忆搜索能力 (!504)
- **openclaw**: 挂载运行时 A2A 接口 (!497)
- **openclaw**: 接入 Langfuse 管理端链路追踪 (!518)
- **openclaw**: 新增本地 Docker 镜像构建工作流 (!520)
- **openclaw**: 要求显式模型环境变量引用，对齐默认 profile 与模型
  环境变量 (!523, !524)
- **openclaw/backends**: 更新分词器加载内嵌资源 (!510)
- **claw**: 新增内网 Skill（iWiki/TAPD/工蜂/KM/Rainbow），移除内网
  MCP 默认配置 (!536, !569, !563)
- **wecom**: 新增 AI Bot WebSocket 服务器，支持企微长连接流式回复
  (!502, !525)
- **wecom**: 支持 WebSocket 文件返回 (!514)
- **wecom**: 支持多词 mention 后的斜杠命令处理 (!574)
- **wecom**: 支持共享会话中参与者身份解析 (!534)
- **wecom**: 支持定时固定文本任务引导 (!535)
- **ecosystem/codeexecutor**: 支持 `host://` copy 模式的 StageInputs
  (!543)

### Examples

- examples: 持续同步上游 `trpc-group/trpc-agent-go` 相关示例代码
  (!503, !508, !512, !516, !517, !519, !522, !527, !553, !560, !571,
  !578, !583)
- examples: 移除重复的 SQLiteVec memory 服务并格式化代码 (!511)
- examples: 新增 OpenAI 错误兼容选项示例 (!570)

### Bug Fixes

- **ecosystem/codeexecutor**: 修复宿主目录输入被拷贝到错误目标的
  问题 (!550)
- **agent/knot**: 修复 Knot 认证问题 (!532)
- **skills**: 修正 mcp.json 路径以包含 bundled 子目录 (!580)
- **openclaw**: 修复 hostexec 进程组清理问题 (!556)
- **openclaw**: 修复上游 quote projection 问题 (!546)
- **openclaw**: 修复上游 admin proxy 问题 (!568)
- **openclaw**: 修复 admin links 在 proxy 子路径下的保留问题 (!565)
- **wecom**: 修复回复投递根节点匹配 (!577)
- **wecom**: 修复网关流式渲染逻辑 (!572)，后续 revert 并重新处理
  (!575)
- **wecom**: 修复企微投递目标对齐和共享会话流程 (!530, !531)
- **wecom**: 修复工作区状态文案 (!521)
- **wecom**: 移除遗留 WebSocket 命令兼容代码 (!513)
- **lsc**: 修复示例构建错误和 gofmt 格式问题 (!529)
- fix: 修复单元测试 (!558)
- fix: 修复工作流问题 (!555)

### Docs

- docs: 更新 token-usage-streaming 文档，新增太极平台 DeepSeek 模型
  说明 (!547, !554)
- docs: 解决 model.md 合并冲突 (!557)
- docs: 新增 OpenAI 错误兼容选项说明 (!570)

### Dependencies

- dependencies: 更新 go mod 依赖版本

### Misc

- **openclaw**: 发布 `trpc-claw v0.0.42` ~ `v0.0.46` (!537, !539,
  !541, !542, !544)
- **openclaw**: 更新发布说明
- **openclaw**: 刷新企微 onboarding 和 persona UX
- **openclaw**: 对齐企微投递和工作区默认配置 (!515)
- **openclaw**: 移除冗余的 Knot skill finder
- trpc/stat: 上报版本号更新为 `v1.8.0`

## [1.7.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.7.0) (2026-03-17)

### Features

- **openclaw**: 新增 OpenClaw 内网分发版，提供 Gateway、Channel、
  Skill、Session、Memory 一体化运行形态，支持企业微信长连接、通知
  机器人、Mock profile、自升级，以及默认开启 summary / auto memory 和
  依赖自检安装能力 (!452, !470, !488, !490, !493, !494)
- **knowledge/iwiki**: 新增 iWiki 知识库接入，支持通过太湖鉴权、HTTP
  头透传和 tRPC 客户端配置接入内网语义检索服务 (!484)
- **ecosystem/codeexecutor**: 支持 CFS volume 挂载与输入归一化，增强
  持久工作区和文件暂存能力 (!464, !471)
- **agent/lke**: 增强 LKE Agent 适配器与 basic / a2a 示例，支持按请求
  注入 handler、client setup 与 run options (!475)
- **model/taiji**: 支持通过 `chat_template_kwargs` 配置 GLM 思考模式，
  补足太极模型能力开关 (!499)
- **graph / session / memory / skill**: 对齐 GitHub v1.7.0，新增可选
  DAG 执行引擎、节点间流式传输、请求级 summary provider、episodic
  memory、sqlite / sqlitevec / redis backend，以及按 Agent 作用域隔离的
  skill load / run 能力
- **evaluation / agui / a2a**: 对齐 GitHub v1.7.0，新增 Judge Runner、
  在线评估服务、`numRuns` 并行执行、reasoning event、tool-result 输入
  翻译和 A2A 流式消息输出等能力
- **tool / artifact / knowledge**: 对齐 GitHub v1.7.0，新增 hostexec /
  file 工具增强、artifact 强制持久化与 COS 前缀存储、pgvector RRF
  融合、protobuf reader 等能力

### Examples

- examples: 持续同步上游 `trpc-group/trpc-agent-go` v1.7.0 相关示例代码
  (!485, !482, !479, !478, !477, !472, !468, !465, !462, !460, !456, !454,
  !449)
- examples/knowledge: 新增 iWiki 示例，展示内网知识检索接入方式 (!484)
- examples/graph: 新增 DAG engine、interrupt、streaming node consumer、
  runner plugin node callbacks 等示例，展示 v1.7.0 图编排能力
- examples/summary / skill / evaluation: 对齐 GitHub v1.7.0，新增
  summary toolcalls / subagent、skill isolation / tool profile、
  evalset recorder 等示例
- examples/lke: 更新 basic 与 a2a 示例，展示新版 LKE 适配器接入方式
  (!475)
- examples: 为 LLM client 示例补充 tool calling 支持，并清理冗余文件
  (!459, !466)

### Bug Fixes

- **trpc/telemetry/zhiyan-llm**: 对齐语义约定中的 TTFT 和 tool name
  上报，提升链路观测准确性 (!450, !451)
- **trpc/agent/eino**: 修复重复流式事件发射问题 (!457)
- **openclaw/wecom**: 修复硬编码凭据、`response_url` 并发竞争和会话处理
  稳定性问题 (!480, !474, !495)
- **openclaw/skill/weather**: 稳定天气 skill 查询链路，降低模糊地点查询
  失败率 (!496)
- **runner / event / model / session / skill**: 对齐 GitHub v1.7.0，
  修复 orphan tool call 清洗、event data race、Gemini function call
  重试、A2A 内容往返、MySQL / Redis / summary 等兼容性问题

### Docs

- docs: 新增 OpenClaw README、安装说明、发布流程和企微 runbook，补充
  内网长期运行形态使用指引 (!452, !488, !490, !493)
- docs/knowledge: 新增 iWiki 接入文档 (!484)
- docs/session: 重构 Session 文档结构，拆分为多篇主题文档以便查阅
  (!448)
- docs/token-usage-streaming: 更新流式 Token Usage 说明，补充太极
  `openai_infer` 场景测试记录 (!489)
- docs: 对齐 GitHub v1.7.0，补充 callback 动态请求体修改、Agent 调用
  次数限制、评估 metric registry / ExpectedRunner / per-call options /
  online service、AG-UI reasoning event 与取消收尾等说明

### Dependencies

- dependencies: 统一将所有 `trpc.group/trpc-go/trpc-agent-go` 相关依赖升级到
  v1.7.0（包含子模块与示例） (!500, !487, !481)

### Misc

- **openclaw**: 发布 `trpc-claw v0.0.5`，补充内网镜像分发与升级链路
  (!490)
- trpc/stat: 上报版本号更新为 `v1.7.0`

## [1.6.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.6.0) (2026-02-26)

### Features

- **agent/lke**: 新增 LKE Agent 适配器，支持通过 LKE API 调用智能体服务 (!442)
- **knowledge**: 支持灵山知识库的 tRPC 客户端选项配置，增强服务接入灵活性 (!440)
- **knowledge**: 新增 OCR 文档同步能力，支持文档图片内容提取 (!425)
- **ecosystem/codeexecutor**: 支持探测失败阈值配置，增强代码执行器容错能力 (!423)
- **ecosystem/codeexecutor**: 支持高可用能力，提供更稳定的代码执行服务 (!420)
- **telemetry/galileo**: 支持 Galileo Evaluation 集成，增强评估能力 (!422)

### Examples

- examples: 持续同步上游 `trpc-group/trpc-agent-go` 相关示例代码
  (!444, !439, !438, !437, !436, !435, !434, !432, !431, !428, !427, !424, !419, !418, !417, !416, !415, !414, !413)
- examples: 新增 LKE Agent 示例，展示 LKE 服务集成 (!442)
- examples: 新增 Knot Agent 示例 (!441)

### Bug Fixes

- **agent/knot**: 修复 go.mod 依赖问题 (!443, !441)

### Docs

- docs: 新增 AG-UI 使用文档
- docs: 新增 Skill 技能介绍文档
- docs/model: 添加 BaseURL 路径使用说明，明确带路径配置方式 (!426)
- docs/model: 澄清内部适配器包的术语定义 (!412)
- docs: 更新 README 中的监控平台名称 (!421)

### Dependencies

- eino: 升级 trpc-agent-go 版本 (!433)

### Misc

- trpc/stat: 上报版本号更新为 `v1.6.0`

## [1.5.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.5.0) (2026-02-02)

### Features

- **codeexecutor**: 集成 pcg-123 引擎，支持 Agent 技能代码执行能力 (!382)
- **knowledge**: 支持 Venus Reranker，增强知识库重排序能力 (!389)
- **model/hunyuan**: 新增 `WithDisableThinking` 配置项，支持禁用混元模型思考模式 (!392)
- **agent/taiji**: 支持自定义 tRPC 客户端选项，增强太极 Agent 配置灵活性 (!374)
- **trpc/model**: 更新模型上下文窗口信息，优化模型配置 (!371)
- **git-fls**: 新增 git-fls 模块支持，提供 Git 文件列表服务能力 (!405)

### Examples

- examples: 新增 trpc agent cmdline skill 示例，展示命令行技能集成 (!397)
- examples: 持续同步上游 `trpc-group/trpc-agent-go` 相关示例代码
  (!408, !407, !406, !402, !400, !399, !395, !391, !388, !387, !385, !383, !381, !378, !377, !376, !375, !373, !370)
- examples: 同步 agui 示例并升级到最新版本 (!400)
- examples: 清理未使用的示例文件 (!372)
- examples: 修复示例构建问题 (!369)

### Bug Fixes

- **telemetry/zhiyan-llm**: 修复 Agent Span 转换中的 I/O token 属性问题 (!394)
- **telemetry/zhiyan-llm**: 移除冗余的 I/O token 属性 (!393)
- **telemetry/galileo**: 替换 ToResponseError 函数实现 (!390)

### Docs

- docs: 新增 tRPC Agent 使用指南 (!384)
- docs: 新增 Memory 和 Session 的模型配置指南 (!386)
- docs: 更新智研 LLM 在 AG-UI 中的配置指南 (!398)
- docs: 新增智研 LLM README 文档 (!396)
- docs: 更新 AG-UI 使用说明 (!380)
- docs: 更新混元模型禁用思考模式配置说明 (!392)
- docs: 修复文档拼写错误 (!379)
- docs: 更新 README.md，优化项目介绍

### Dependencies

- dependencies: 统一更新所有子模块依赖版本，保持与主版本一致
- examples: 同步上游依赖更新

### Misc

- trpc/stat: 上报版本号更新为 `v1.5.0`

## [1.2.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.2.0) (2026-01-14)

### Features

- **knowledge**: 支持lingshan知识库集成，增强企业级知识库接入能力 (!352)
- **knowledge**: 支持 TCVector 通过 serviceName 构建客户端，并新增更多
  vectorstore 示例配合 trpc_go.yaml (!333)
- **tools**: 新增混元文生图工具支持，支持通过 AI 生成图像 (!315, !334)
- **telemetry**: 新增智研 LLM tRPC 插件，增强可观测性能力 (!357)
- **agent/vedas**: 新增 `ForceIntentionAuto` 配置项，支持默认意图处理 (!340)
- **model**: 新增内网兼容模型和 tRPC HTTP Client 示例 (!355)

### Examples

- examples/agui: 新增多 Agent 服务端示例，展示多 Agent 协同场景 (!342)
- examples/agui: 新增 E2E 测试，提升代码质量 (!361)
- examples/agui: 支持 CopilotKit 使用远程 Agent 服务 (!328)
- examples: 持续同步上游 `trpc-group/trpc-agent-go` 相关示例代码
  (!364, !358, !354, !353, !350, !347, !346, !343, !337, !332, !331, !330, !329)
- examples: 重命名 agui 示例名称从 trpc 到 default (!349)
- examples: 为示例测试做准备工作 (!348)

### Bug Fixes

- **storage**: 升级 Postgres 插件版本以修复查询问题 (!356)
- **storage**: 为存储组件导入 `git.woa.com/trpc-go/trpc-agent-go/trpc` (!351)
- **knowledge**: 修复 topk reranker 的变更问题 (!336)

### Docs

- docs/knowledge: 重构知识库文档，优化使用说明 (!341)
- docs: 更新混元 2.0 的 token 使用说明 (!363)
- docs: 新增 OpenAI stream_options 和 usage 格式说明 (!359)
- docs: 更新 GLM 模型的 token 流式使用说明 (!344)
- docs: 新增缺失的跟踪条目 (!345)
- docs: 更新概览图片 (!338)
- docs: 更新 README.md，优化功能生态列表

### Dependencies

- dependencies: 统一将所有 `trpc.group/trpc-go/trpc-agent-go` 相关依赖升级到
  v1.2.0（包含子模块与示例）
- galileo: 更新 Galileo go.mod 和 README.md (!339)



## [1.1.1](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.1.1) (2025-12-31)

### Features

- **graph**: 对齐 GitHub v1.1.1，新增 `EmitFinalModelResponse` 配置项，
  支持按需输出最终模型响应以恢复历史行为

### Bug Fixes

- **session/summary**: 对齐 GitHub v1.1.1，增强 Summary Prompt 校验，
  避免无效配置导致运行异常

### Examples

- examples/evaluation: 对齐 GitHub v1.1.1，支持通过环境变量配置 Judge
  模型 Key

### Docs

- docs: 对齐 GitHub v1.1.1，补充 Judge 模型 Key 环境变量配置说明

### Dependencies

- dependencies: 统一将所有 `trpc.group/trpc-go/trpc-agent-go` 相关依赖升级到
  v1.1.1（包含子模块与示例）

### Misc

- session/internal/summary: 对齐 GitHub v1.1.1，重构异步 Summary Job
  单测处理逻辑，提升稳定性
- trpc/stat: 上报版本号更新为 `v1.1.1`

## [1.1.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v1.1.0) (2025-12-29)

### Features

- **agent/knot**: 新增 Knot Agent 集成能力，支持通过 Knot API 调用智能体
  服务 (!298)
  - 提供 `WithKnotApiKey`、`WithKnotModel` 等配置选项
  - 支持流式事件输出与 AG-UI 协议对接
- **agui**: 支持使用 `WithSpanAttributes` 设置自定义 Span 属性，增强
  可观测性集成灵活性 (!317)

### Examples

- examples: 持续同步上游 `trpc-group/trpc-agent-go` v1.1.0 相关示例代码
  (!311, !312, !313, !314, !316, !318, !320, !322, !323)
- examples/evaluation: 新增 tooltrajectory 评估示例，展示工具调用轨迹
  评估能力
- examples/graph: 新增 isolated_subagent 示例，展示子 Agent 隔离运行场景
- examples/openapitool: 新增 OpenAPI 工具示例，支持从 OpenAPI 规范自动
  生成工具
- examples/session: 新增 appendevent 示例，展示会话事件追加能力

### Docs

- docs: 更新 A2A 协议文档，简化使用说明 (!321)
- docs: 更新 README，新增可观测性模块说明，优化快速开始示例为 AG-UI
  服务启动方式 (!319)

### Dependencies

- dependencies: 统一将所有 `trpc.group/trpc-go/trpc-agent-go` 相关依赖升级到
  v1.1.0（包含子模块与示例）
- dependencies: 升级 `trpc.group/trpc-go/trpc-mcp-go` 到 v0.0.11
- dependencies: 升级 `trpc.group/trpc-go/trpc-a2a-go` 到 v0.2.5

### Misc

- trpc/stat: 上报版本号更新为 `v1.1.0`

## [0.8.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.8.0) (2025-12-18)

### Features

- **agui**: 支持 StartSpan 回调，将 AG-UI 每次运行与 Trace 自动关联 (!307)
- **vedas-agent**: 新增 Vedas Agent 集成能力，支持与内网 Vedas 生态对接
  (!269)
- **core**: 支持在 goroutine 中克隆 Context，降低并发场景下上下文复用的
  风险 (!302)
- **artifact**: 对齐 GitHub v0.8.0，新增 S3 兼容的 Artifact 存储能力
- **storage**: 对齐 GitHub v0.8.0，新增 Milvus VectorStore 与 MongoDB
  Storage 能力
- **session/summary**: 对齐 GitHub v0.8.0，增强总结能力（过滤 Key、
  Hook、异步上下文保持等）
- **graph**: 对齐 GitHub v0.8.0，增强 Graph 并发隔离与自定义事件发射等能力

### Examples

- examples: 持续同步上游 `trpc-group/trpc-agent-go` v0.8.0 相关示例代码
  (!291, !292, !295, !296, !297, !299, !301, !303, !306, !309)
- examples/agui: 补充自定义 translator 的 Context 参数传递能力，并升级
  AG-UI 依赖修复已知安全风险 (!289, !294)
- examples: 修复 redis polaris 示例，并升级 goredis 版本以适配新依赖
  (!290, !293)

### Bug Fixes

- **debugserver**: 调整 DebugServer 目录结构，迁移实现到 internal，
  降低对外暴露面 (!305, !308)
- **knowledge/trag/source**: 适配 tRAG Source 自动 ID 生成规则变更，
  修复相关单测

### Docs

- docs: 新增太极 special parameters 说明，补充使用指引 (!300)

### Dependencies

- dependencies: 统一将所有 `trpc.group/trpc-go/trpc-agent-go` 相关依赖升级到
  v0.8.0（包含子模块与示例）
- redis: 继续固定 go-redis 版本到 v9.16.0，兼容内网 goredis 插件
- redis: goredis 升级到 v3.3.8，保持与 go-redis 版本兼容

### Misc

- trpc/stat: 上报版本号更新为 `v0.8.0`

## [0.7.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.7.0) (2025-12-04)

### Features

- **agent/taiji**: 支持在太极 Agent 中统计调用耗时信息，便于在日志和
  可观测系统中分析链路性能 (!268)
- **tool/trag**: 支持关闭分片（chunking）以及在数据写入之前注入自定义
  Hook，方便与内网 TRAG 系统做深度集成 (!274)
- **server/openai**: 新增 OpenAI 兼容协议的 tRPC 服务器实现，配套
  examples 展示如何在内网环境对接兼容 OpenAI 的大模型服务 (!271)
- **log**: 设置基于 Context 的 Logger，统一内部日志上下文传递方式 (!283)
- **core**: 升级内外网 trpc-agent-go 版本到 v0.7.0，保持与 GitHub
  版本一致

### Examples

- examples/agui: 新增 report 与 thinkaggregate 示例，展示在 AG-UI
  中上报和聚合多轮对话信息的用法 (!286)
- examples: 持续从上游 `trpc-group/trpc-agent-go` 同步示例代码，跟进
  多个版本的变更 (!273, !275, !279, !280, !281, !284, !285)

### Bug Fixes

- **knowledge/retriever**: 修复在替换 main 包后引入的兼容性问题，避免
  内网检索服务在升级后出现编译失败或运行错误 (!278)
- **model**: 适配内网模型 HTTP 客户端实现，改为使用
  `model.DefaultNewHTTPClient`，避免与最新 openai-go 版本不兼容 (!287)

### Docs

- docs: 新增 TDesign Chat 与 AG-UI 的联动文档，说明如何在前端聊天
  场景中集成 Agent 能力 (!277)
- docs: 更新 README，补充环境变量配置说明，便于在内外网环境统一配置
  运行时参数 (!282)
- docs: 更新模型测试状态文档，将部分模型标记为“未测试”，提醒内网用户
  注意使用风险 (!272)

### Dependencies

- dependencies: 统一将所有 `trpc.group/trpc-go/trpc-agent-go` 相关依赖
  升级到 v0.7.0（包含子模块与示例）
- redis: 将 go-redis 从 v9.17.0 降级到 v9.16.0，以规避新版本在内网
  环境中的兼容性问题 (!270)

### Misc

- trpc/stat: 上报版本号更新为 `v0.7.0`

## [0.6.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.6.0) (2025-11-25)

### Features

- **tools**: 新增 TRAG 工具支持 (!241)
  - 为 Agent 提供 TRAG（Tencent Retrieval-Augmented Generation）工具集成能力

- **telemetry**: 持续增强可观测性功能
  - 太极 Agent：新增 telemetry 上报支持 (!254)
  - Galileo：模块迁移至 `git.woa.com/galileo/trpc-agent-go-galileo` (!264)
  - 智研-LLM：修复 span kind 设置，正确处理 invoke agent 操作 (!253)
  - 智研-LLM：修复 tool span 的 input/output 记录 (!252)
  - 智研-LLM：修复 chat span 上报问题 (!250)
  - 智研-LLM：发布 v0.5.1 版本 (!260)

- **errors**: 新增 tRPC 与 Agent 之间的错误转换支持 (!248)
  - 提供统一的错误码转换机制，改善跨层错误处理

- **environment**: 新增环境配置支持 (!244)
  - 提供标准化的环境变量设置能力

### Examples

- examples: 持续同步上游 trpc-group/trpc-agent-go 的示例代码 (!245, !246, !247, !249, !251, !261, !262, !263)

### Docs

- docs: 新增大模型流式 chunk token usage 格式说明文档 (!259)
- docs: 新增 HTTP Service 集成指南和 iWiki 同步 (!256)
- docs: 新增 Skill 开发文档 (!255)
- README: 改进格式和代码可运行性 (!265)

### Dependencies

- dependencies: 升级所有 trpc.group 依赖到最新版本
  - `trpc.group/trpc-go/trpc-agent-go`: 升级到 v0.6.0
- dependencies: 统一更新所有子模块的依赖版本，保持与主版本一致

### Misc

- trpc/stat: 上报版本号更新为 `v0.6.0`

## [0.5.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.5.0) (2025-11-13)

### Features

- **storage**: 新增数据库存储支持
  - MySQL：实现基于 trpc-database 客户端的 MySQL 存储插件 (!220)
  - PostgreSQL：新增 PostgreSQL 存储注入支持 (!218)

- **telemetry**: 增强可观测性功能
  - Galileo：新增 metrics 指标上报能力 (!227)
  - Galileo：重构模块结构，将 trace 和 metrics 功能分离
  - 智研：更新依赖版本并重构指标初始化逻辑 (!240, !238)

- **agui**: 增强 AG-UI 功能
  - 新增 messages snapshot 事件支持 (!233)
  - 新增前端 Web 示例（ag-ui-client-js） (!219)
  - 新增智研 LLM 集成示例 (!214)
  - 修复太极 Agent 在 AG-UI 上的响应问题 (!232)

- **trpc**: 提供 app/agent 名称的降级上报机制 (!230)

### Examples

- examples/agui: 新增 AG-UI 前端 Web 示例，展示 JavaScript 客户端集成 (!219)
- examples/agui: 新增 messages snapshot 完整示例 (!233)
- examples/agui: 新增智研 LLM SDK 集成示例 (!214)
- examples/telemetry: 新增 Galileo metrics 示例 (!227, !235)
- examples/telemetry: 新增多上报示例 (!234)
- examples/callbacks: 新增用户认证示例，展示如何在 callbacks 中实现鉴权 (!225)
- examples/graph: 新增 external tool 示例 (!228)
- examples: 持续同步上游 trpc-group/trpc-agent-go 的示例代码 (!217, !222, !225, !226, !228, !229)
- examples: 移除重复示例代码 (!242)

### Bug Fixes

- http: 修复内部 HTTP 处理器吞掉错误的问题 (!224)
- examples: 修复 trpchttpservice 示例问题 (!223)
- agent: 修复太极 Agent 在 AG-UI 上的响应格式问题 (!232)

### Docs

- docs: 新增评估（evaluation）模块文档 (!239)
- docs: 新增 MCP marketplace 资源链接 (!231)
- docs: 新增混元模型思考模式配置说明 (!236)
- docs: 明确 WithHTTPClientName 在 YAML 路由中的配置限制 (!237)
- docs: 更新 AG-UI、模型配置、可观测性和工具相关文档
- examples/telemetry/zhiyan: 更新 trpc-plugin README (!221)

### Dependencies

- dependencies: 升级所有 trpc.group 依赖到最新版本
  - `trpc.group/trpc-go/trpc-agent-go`: 升级到 v0.5.0
- dependencies: 统一更新所有子模块的依赖版本，保持与主版本一致
- examples: 同步上游依赖更新

### Misc

- trpc/stat: 上报版本号更新为 `v0.5.0`

## [0.4.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.4.0) (2025-10-28)

### Features

- **agent**: 实现太极Agent支持，提供 A2A 协议接入能力 (!193, !172)
  - 支持在每次调用中使用不同的太极配置 (!205)
  - 完善 Taiji 和 Eino Agent 的事件发射调用信息 (!192, !187)
  - 支持 `maxEventSize` 配置项，限制事件大小 (!194)

- **server/taijia2a**: 新增太极 Agent A2A 代理服务器，支持通过 A2A 协议访问太极 Agent (!193)

- **storage**: 新增 Redis 和 TCVector 存储的完整测试覆盖 (!202)

- **telemetry**: 增强可观测性支持
  - Galileo：更新 GenAI 属性和事件，支持新的 telemetry 定义 (!185, !203)
  - Galileo：添加 `telemetry.sdk.name` 属性 (!203)
  - 智研-LLM：修复 session id 和 user id 的上报问题 (!213)

- **agui**: 增强 AG-UI 功能
  - 修复 CopilotKit 的 CSS 样式问题 (!212)
  - 升级 AG-UI SDK 到最新版本 (!212)
  - 新增 AG-UI 与 Langfuse 集成示例 (!189)

- **knowledge**: 提升知识库功能，改进最大文档返回数量限制 (!191)

- **model**: 注册内部模型的上下文窗口信息 (!198)

### Examples

- examples: 新增 A2A Polaris 集成示例，展示如何在 Polaris 环境中使用 A2A 协议 (!211)
- examples: 新增 AG-UI 与 Langfuse 集成的完整示例 (!189)
- examples: 在 DebugServer 示例中添加 ADK Web 监听地址配置 (!197)
- examples: 持续同步上游 trpc-group/trpc-agent-go 的示例代码 (!215, !210, !209, !208, !207, !206, !204, !199, !196, !195)

### Bug Fixes

- storage/redis: 修复 Redis 插件在 Polaris 环境下的兼容性问题 (!200)
- telemetry/zhiyan-llm: 修复 session id 和 user id 的上报错误 (!213)
- agui: 修复 CopilotKit 中的 CSS 样式问题 (!212)

### Docs

- docs: 更新模型配置文档，补充额外选项并明确 tool_choice 使用方法 (!183)
- docs: 在 README 中添加 GitHub 和 iWiki 链接，便于用户访问 (!201)
- docs: 更新 DebugServer 文档，说明 ADK Web 监听地址配置 (!197)

### Dependencies

- dependencies: 升级所有 trpc.group 依赖到最新版本
  - `trpc.group/trpc-go/trpc-a2a-go`: 升级到 v0.2.5
  - `trpc.group/trpc-go/trpc-agent-go`: 升级到 v0.4.0
  - `trpc.group/trpc-go/trpc-mcp-go`: 升级到 v0.0.7
- dependencies: 统一更新所有子模块的依赖版本，保持与主版本一致

### Misc

- trpc/stat: 上报版本号更新为 `v0.4.0`

## [0.3.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.3.0) (2025-10-14)

### Features
- agui: 发布内网 AG-UI 接入模块，提供 `AddAGUIServerToMux`/`RegisterAGUIServer(ToMux)` 等便捷方法，支持将 AG-UI HTTP SSE 服务挂载到 tRPC 服务路由。(!142, !181)
- examples/agui: 新增 React 与 tRPC 服务端示例，配套最小化客户端示例（raw、CopilotKit、Node.js 客户端）。(!142, !180)

### Docs
- docs: 新增《AG-UI 内网集成指南》，给出服务注册与客户端消费示例。(!142)

### Dependencies
- examples: 同步上游示例，配合依赖升级保持一致性。(!180, !176, !173)
- gomod: 统一将关联模块依赖升级为 `trpc.group/trpc-go/trpc-agent-go v0.3.0`，与 GitHub 版本保持一致。

### Misc
- trpc/stat: 上报版本号更新为 `v0.3.0`。

## [0.2.2](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.2.2) (2025-09-25)


### Bug Fixes

- agent/eino: 修复 Go 1.25.1 兼容性和流式响应问题 (!150)
  - 降级 kin-openapi 从 v0.132.0 到 v0.124.0 并使用 replace 指令
  - 升级 sonic 到 v1.14.1 以支持 Go 1.25.1 兼容性
  - 修复 MockModel 流式响应缺失 Done 标志导致挂起的问题

### Examples

- examples: 同步 trpc-group/trpc-agent-go 多个版本的示例代码 (!155, !153, !149, !147)
- examples: 更新 knowledge 和 a2a 示例功能 (!143)
- examples: 新增 callbacks 功能和图表 IO 约定示例
- examples: 增强 telemetry、agent 和 graph 相关示例
- examples: 增强 debugserver 与 tRPC 的集成 (!152)


### Docs

- docs: 更新概览图片以强调 callbacks 功能 (!148)
- docs: 新增超时错误处理的相关说明文档 (!146)
- docs: 更新整体介绍和模型配置文档

### Dependencies

- dependencies: 更新 go.mod 依赖版本以保持与github同步
- lsc: 执行 go mod tidy 清理依赖 (!145)
- dependencies: 升级多个模块的依赖版本，包括 examples、knowledge、agent/eino、telemetry 等

## [0.2.1](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.2.1) (2025-09-19)

### Features

- docs: 新增 Taiji 和 TRAG 相关文档 (!139)
- knowledge: 新增 WithEmbedder 选项，增强知识库功能 (!137)
- internal/http: 为 Galileo 事件显示实现 json.Marshaler 接口 (!138)

### Examples

- examples: 同步 trpc-group/trpc-agent-go 多个版本的示例代码 (!140, !136, !134, !133)

### Compatibility

- lsc: 降级 Go 版本到 1.21 以提升兼容性 (!135)
- examples: 降级 Go 版本到 1.21 (!141)
- docs: 降级 Go 版本到 1.21 (!127)

### Bug Fixes

- knowledge: 修复 golangci-lint 检查问题 (!137)
- gomod: 修复 ES 未知版本问题 (!130)

### Dependencies

- dependencies: 与上游 trpc-group/trpc-agent-go 保持同步更新

## [0.2.0](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.2.0) (2025-09-16)

### Features

- examples: 新增 tRPC Agent HTTP service 示例 (!123)
- session: 新增 Redis session 配置支持 trpc_go.yaml (!114) 
- examples: 新增自定义 Agent 示例和调试 Agent 功能 (!125)
- knowledge: 新增知识库管理系统示例 (!125)
- graph: 新增多个 Graph 流程示例，包括 checkpoint、diamond、fanout、interrupt 等 (!113, !117, !119, !125)
- examples: 新增 RunWithMessages 示例 (!125)
- artifact: 新增图像生成和显示工具示例 (!113)
- multiagent: 增强多 Agent 链式协同示例 (!113)

### Examples

- examples: 同步 trpc-group/trpc-agent-go 多个版本的示例代码 (!107, !108, !110, !113, !117, !119, !125)
- a2a: 重构 A2A 功能，新增 a2a-trpc 示例并移除旧的 a2a 目录 (!115)
- memory: 优化内存模块示例，新增工具函数 (!125)
- runner: 优化运行器示例，提取工具函数 (!125)
- humaninloop: 增强人机交互示例功能 (!117)
- telemetry: 优化 Langfuse 遥测示例 (!125)

### Docs
- docs: 更新 README 文档内容 (!109)
- docs: 新增 session 模块文档 (!114)
- docs: 更新外部文件路径以包含 mkdocs 目录 (!118)
- docs: 准备文档同步功能配置 (!112)

### Bug Fixes

- trpc-agent-go: 修复代码检查问题 (!111)
- a2a: 修复 A2A 示例相关问题 (!115)

### Dependencies

- dependencies: 更新多个 go.mod 依赖版本以保持与上游同步 (!107, !108, !110, !113, !115, !117, !119, !125)
- dependencies: 升级trpc.group/trpc-go/trpc-agent-go到v0.2.0


## [0.1.2](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.1.2) (2025-09-04)

### Features

- codeexecutor: 完成 pcg-123 代码执行器生态系统集成 (!89)
- agent: 完成 eino 生态系统集成 (!77)
- knowledge: 实现 Taiji 知识库 (!80)
- examples: 新增内存模块示例和文档 (!86)
- trpc: 为可观测添加智研的上报 (!84)


### Bug Fixes
- gomod: 修复 galileo 可观测问题 (!90)
- example: 移除不必要的 RunOptions 以修复编译错误 (!83)
- trpc-agent-go: 修复 iwiki 的相对路径 (!96)


### Docs
- docs: 更新 Taiji 平台的模型配置示例 (!97)
- docs: 内外网文档同步 (!91)
- docs: 更新生态系统路径 (!93)
- docs: 删除测试内容并移除同步中不存在的文件 (!94)
- knowledge: 更新知识库示例 (!85)


## [0.0.4](https://git.woa.com/trpc-go/trpc-agentgo/tree/v0.0.4) (2025-08-18)

### Bug Fixes

- storage: 修复 go-redis 版本升级导致的兼容性问题 (!81)

### Dependencies

- Gomod: 升级 stat runtime report 到 v0.5.21 (!79)
- Gomod: 升级trpc mcp和a2a 版本 (!82)
- Gomod: 升级trpc-agent-go版本 (!82)

### Docs

- docs: 更新图表文档 (!78)

## [0.0.3](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.0.3) (2025-08-14)

### Features

- trpc-agent-go: 集成 Langfuse 用于可观测性 (!74)
- trpc-agent-go: 集成 TRAG 知识管理系统 (!54)
- trpc-agent-go: 为 redis 存储添加额外选项 (!73)

### Docs

- docs: 更新知识系统使用指南并与 LLM Agent 集成 (!72)
- docs: 添加知识管理系统指南 (!69)
- docs: 更新概述到 README 并将 runner 放在首位 (!70)
- docs: 添加 model.md (!71)
- docs: 添加生态系统贡献 (!66)
- docs/observability: 修改为 Galileo 为伽利略 (!68)
- docs: 从 README 中移除环境设置说明 (!65)

### Bug Fixes

- knowledge: 修复 trag 问题 (!75)

### CI/CD

- ci: 更新 .code.yml 中的文件路径正则表达式以排除 examples 目录 (!67)

## [0.0.2](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.0.2) (2025-08-07)

### Features

- trpc-agent-go: upgrade trpc-mcp-go dependencies (!61)
- docs: add event doc (!62)
- examples: remove the tool directory because there is already multi_tools (!60)

## [0.0.1](https://git.woa.com/trpc-go/trpc-agent-go/tree/v0.0.1) (2025-08-07)

### Features

- **内网增强版本发布**: 这是 tRPC Agent Go 的内网版本，专为内网 git.woa.com 用户提供.
- **无缝集成**: 提供与 GitHub 版本 (`trpc.group/trpc-go/trpc-agent-go`) 完全一致的 API，只需空白导入即可启用内网监控和统计等增强功能.
- **tRPC 生态深度集成**: 自动启用内网监控和统计等增强功能，与现有 tRPC 代码完全兼容.
- **MCP 工具集成**: 集成 `trpc-mcp-go` 支持，提供模型上下文协议 (Model Context Protocol) 功能.
- **Telemetry 支持**: 集成 Galileo 监控平台支持，提供完整的可观测性能力.
- **Session 管理**: 支持基于 Redis 的分布式 Session 管理.
- **多 Agent 支持**: 提供 ChainAgent、ParallelAgent、CycleAgent 等多 Agent 协同工作能力.
- **知识库集成**: 支持 RAG 流程，集成 pgvector/tencent-cloud-vectordb 等向量数据库.
- **调试支持**: 提供 DebugServer 功能，支持与 adk web 对接进行 Agent 调试.
- **完整示例**: 提供 20+ 个完整示例，涵盖 Agent、Runner、Session、Memory、Tool、Planner、Knowledge、MultiAgent、Telemetry、DebugServer 等各个模块.

### Documentation

- **完整文档体系**: 新增 15+ 个详细文档，包括概述、Agent、Runner、Session、Memory、Tool、Planner、Knowledge、MultiAgent、Telemetry、DebugServer 等.
- **iWiki 同步**: 自动同步文档到内网 iWiki 平台，提供更好的文档访问体验.
- **示例文档**: 为每个功能模块提供详细的示例代码和使用说明.
