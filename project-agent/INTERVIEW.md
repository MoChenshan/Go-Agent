# 🎯 GameOps Agent 项目 · 面试速查卡

> 对标 **大厂 LLM Agent / 大模型应用工程** 两类 JD（方向 A：基础架构 / 方向 B：应用工程），每条回答都给到 **代码出处**（grep 可达）。
> 配套：项目根 `README.md`（架构）、`PROGRESS.md`（每日里程碑）、`docs/observability.md`（OTel 细节）、`eval/README.md`（评测）。

---

## 📌 一句话概述

> 我做了一个**Go 写的多 Agent 协作运维系统**：基于 `trpc-agent-go v1.8.1` 的 ReAct + 多 Agent 协调器，集成 **MCP**（蓝鲸监控/BCS/工蜂/TAPD）+ **A2A** 跨 Agent 通信 + **同源 RAG**（project-llm 提供的 BGE-M3 知识库）+ **HITL 中断点**（写操作强制人审 + HMAC-SHA256 链式审计）+ **OTel GenAI v1.30 全链路 trace**。线上跑了 4 类 Agent：Coordinator / Diagnosis / Repair / Knowledge / FileAnalyst。

---

## 🏗️ 总体架构

```
                           ┌─────────────────┐
                           │  SSE / AG-UI    │   6 类事件 + HITL 流式中断
                           └────────┬────────┘
                                    ▼
   ┌───────────────────────── Coordinator Agent ─────────────────────────┐
   │  ReAct Planner（中文化系统提示）+ transfer_to_agent 子 Agent 路由     │
   │  WithEndInvocationAfterTransfer (双保险，避免父 Agent 抢话)          │
   └─────┬───────────┬───────────┬───────────┬──────────────────────────┘
         ▼           ▼           ▼           ▼
    Diagnosis    Repair      Knowledge   FileAnalyst        ← A2A 共享 Session
    (只读排障)  (HITL+审计)  (RAG 问答)   (CSV/log/perf)
         │           │           │           │
         ▼           ▼           ▼           ▼
   ┌───────────── TargetedTool 池（按 Target 切片可见）──────────────────┐
   │ MCP 工具：bk_monitor / bcs / gongfeng / tapd / devops / taiji_kb   │
   │ Function 工具：~28 个（含 async job_*，落到 panjf2000/ants 协程池） │
   │ Skill 工具：perf_report / log_pattern / csv_compare（Markdown 化）  │
   └─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
   ┌──────────────── 安全 / 审计 / 观测三件套 ───────────────────────────┐
   │ input_guard.go (5 条 OWASP LLM01 规则) + output_guard               │
   │ audit/hmac.go (HMAC-SHA256 + 多 kid 轮换 + 链式 prev_sig)           │
   │ OTel v1.30 GenAI Semantic Conv + 6 种 Sampler + Prometheus 10 条     │
   │ Langfuse session 级 trace + Grafana 7-panel dashboard               │
   └─────────────────────────────────────────────────────────────────────┘
```

---

# 一、方向 A · LLM Agent JD 对照（19 题）

> JD 关键词：**LLM Agent**、**Prompt 工程**、**任务规划 / to-do list**、**长记忆**、**工具调用**、**评测体系**、**Function Calling**、**ReAct/CoT/CoVe**

## A. Agent / Prompt / 工具

### Q1：你做的 Agent，"Agentic" 体现在哪？
> 不是 prompt 包一层就完事。我的 Agent 同时具备：
> 1. **自主规划**：Coordinator 根据用户意图 `transfer_to_agent` 路由到子 Agent（[`src/agents/coordinator/`](src/agents/coordinator/)），子 Agent 内部用中文化 ReAct（[`src/agents/react.go`](src/agents/react.go)）走 Reasoning→Action→Observation→Next。
> 2. **工具自主选择**：基于 OpenAI Function Calling，工具 schema 由 Go 结构体 tag 自动反射生成。
> 3. **状态保持**：`src/session/session.go` 的 events（短期）+ summary（长期）双层。
> 4. **失败自愈**：异步工具走状态机重试 + watchdog；同步工具走 backoff fallback。
> 5. **可观测**：每一步 ReAct 都打 `gen_ai.*` span 进 Langfuse。

### Q2：Prompt 模板怎么构建的？
> 三层拆解：
> - **System 层**：每个子 Agent 一个独立 markdown，例如 [`src/agents/repair_agent/system_prompt.md`](src/agents/repair_agent/system_prompt.md)（32 KB，含 Severity 自动升级规则、HPA bypass 规则、写操作前置条件）。
> - **工具描述层**：每个 `function.NewFunctionTool` 的 description 必须写"什么时候该调用 / 什么时候不该调用 / 失败如何 fallback"，否则 LLM 必选错。
> - **Few-shot 层**：通过 Coordinator system prompt 给 1~2 个意图分流示例（不放在 system 太多会顶 prefix-cache）。

> **踩坑**：vLLM V1 切换后 prefix-cache 命中从 78% 掉到 30%，定位是 system prompt 里嵌了动态时间戳，把固定段前置 + 时间戳放 user 段后命中恢复。

### Q3：CoT / ReAct / CoVe 你是怎么选的？
| 场景 | 用啥 | 为啥 |
|------|------|------|
| 排障多步推理 | **ReAct** | 需要中间调工具拿事实，纯 CoT 会幻觉 |
| 知识 QA | **轻 CoT + RAG** | 工具已外置成 RAG，省 token |
| 输出前自检 | **CoVe** | LLM Judge 阶段会让模型自己 verify 引用是否充分 |

> Reasoning 段不让模型把整段思考都吐出来——只暴露最终答案 + tool_call，思考链留在 trace 内部，避免泄漏 system prompt。

### Q4：长任务做规划（to-do list）怎么实现？
> **不靠 prompt 让模型生成 to-do list**——这种方式状态没法持久化。我的做法：
> - **同步链路**：Coordinator 是规划器，每一轮把上一轮的 tool 结果作为 events 喂回，模型隐式维护进度。
> - **异步链路**：[`src/async/runner.go`](src/async/runner.go) 的 `Job` 就是 to-do item，状态机 `pending→running→{succeeded/failed/cancelled/timed_out}` 持久化在 Store 里，janitor 协程定时清扫终态、watchdog 看门狗触发超时。LLM 通过 `job_submit / job_status / job_cancel` 三个工具操作这张 list。
> - **HITL**：写操作强制中断回前端等用户 approve（[`src/services/sse/sse.go`](src/services/sse/sse.go) `tool_request_approval` 事件），approve/reject 后才往下走。

### Q5：模型规划 vs 规则规划，你怎么权衡？
| 维度 | 模型规划 | 规则规划 |
|------|---------|---------|
| 长尾意图 | ✅ | ❌ |
| 可解释 | ❌ | ✅ |
| 成本 | 高（多轮） | 极低 |
| 故障恢复 | LLM Judge 兜底 | 显式 fallback |

> **混合**：Coordinator 用模型做意图分流和 transfer，子 Agent 内部用 **规则化的 Severity 分级**（写工具内置严重度计算，LLM 不重复造轮子）+ **规则化的工具白名单**（`tools.FilterByTargets`，按 Agent target 切片）。模型只做"该不该走这条路"，规则保障"走上这条路出不了大错"。

### Q6：Function Calling 是怎么实现的？为什么不用裸 OpenAI tools？
> 自研了 [`src/tools/targeted.go`](src/tools/targeted.go) 的 `TargetedTool` 包装：
> - 每个工具声明 `Targets`（哪些 Agent 可见）。
> - `app.go` 注册时 `tools.FilterByTargets(allLocalTools, diagnosis.FocusedTargets)` —— 给 Diagnosis Agent 切出 10 个只读工具，给 Repair Agent 切出 8 个写工具，**互不干涉**。
> - 这就是天然的工具白名单：模型连工具名都看不到，谈何选错。
> - 底层最终落到 `function.NewFunctionTool`（`trpc-agent-go`），schema 由 Go struct tag 自动反射成 JSON Schema 喂给 LLM。

### Q7：工具调用失败怎么处理？
> 三层兜底：
> 1. **同步工具**：`cenkalti/backoff` 指数退避；MCP 层 timeout=60s；调失败的 tool span 标 `gen_ai.tool.error_type`。
> 2. **异步工具**：[`src/async/runner.go`](src/async/runner.go) 状态机 + watchdog 阶梯轮询（[`src/async/fast_poll_waiter.go`](src/async/fast_poll_waiter.go) 0.5s→1s→2s→5s 退避），失败回 `failed` 状态保留 `error_msg` 给 LLM 判断要不要重试。
> 3. **Agent 层**：ReAct prompt 里写"工具失败可以换工具或直接告诉用户"，避免模型死磕一个失败工具。

### Q8：Agent 之间怎么通信？了解 A2A 协议吗？
> 了解且**真实接入**：[`src/services/a2a/a2a_real.go`](src/services/a2a/a2a_real.go) 实现了 `trpc-a2a-go v0.2.5` 的服务端，子 Agent 通过 transfer_to_agent 走 A2A，**Session 跨 Agent 共享**——这是为什么 Coordinator transfer 给 Repair 后，Repair 能看到 Diagnosis 之前查到的告警事实。

### Q9：评测体系怎么搭？
> 三层：
> - **离线 Golden Set**：[`eval/`](eval/) 目录下 ~50 条标准案例（输入+期望工具调用序列+期望 citations）。
> - **LLM Judge**：[`eval/judge/`](eval/judge/) 三个维度——`AnswerCorrectness` / `EvidenceSufficiency` / `ToolSelectionAccuracy`，prompt 里强制 JSON 输出。
> - **CI 门禁**：每次 PR 触发评测，三个维度任一低于阈值阻断合并。
> - **线上指标**：[`deploy/grafana/panels.yaml`](deploy/grafana/panels.yaml) 7 张面板，含 `tool_selection_accuracy / hitl_stage / reject_total`。

### Q10：怎么发现 Agent 走错了？
> Coordinator transfer 是显式 span，标 `gen_ai.transfer.from / to / reason`。监控 `tool_selection_accuracy` 指标 + `gameops_input_guard_blocked_total`（被拦截的注入）+ Langfuse 上 status=ERROR 的 span，三处异常都能发出告警（`deploy/alerts/prometheus_rules.yaml`）。

### Q11：长记忆怎么做？短期/长期？
> 不靠 prompt 拼历史——这条路 token 爆炸还易污染。**分层**：
> - **短期 = events**：每轮 user/assistant/tool 消息存进 SessionService 的 events 列表，按窗口大小喂回模型。
> - **长期 = summary**：[`src/session/session.go`](src/session/session.go) 用 `inmemory.WithSummarizer` 配三档触发器：
>   - `summary.CheckEventThreshold(20)` —— 满 20 条就触发；
>   - `summary.CheckTokenThreshold(4000)` —— 满 4k token 就触发；
>   - `summary.CheckTimeThreshold(5min)` —— 静默 5 分钟也触发。
>   - 异步走 `WithAsyncSummaryNum / WithSummaryQueueSize`，**不阻塞主链路**；LLM 不可用就降级为纯 events，零回归。
> - **跨 Session**：A2A 共享同一个 session_id，让多个 Agent 看到同一份 events + summary。

## B. 八股 / 算法（与项目弱相关，话术准备）

### Q12：Python GIL？
> CPython 的全局解释器锁——同一进程内字节码同步执行。CPU-bound 用 multiprocessing / Cython / C 扩展释放 GIL；I/O-bound 用 asyncio / 线程池没大问题。Python 3.12 的 `Per-Interpreter GIL`（PEP 684）和 3.13 的 free-threaded build（PEP 703）正在解。**我项目用 Go 写就是为了避开 GIL**。

### Q13：互斥锁、信号量、读写锁？
> Go 里 `sync.Mutex` 是非递归的、`sync.RWMutex` 读写分离（读多写少首选）、`golang.org/x/sync/semaphore` 加权信号量。`async/runner.go` 里 Store 操作用 RWMutex 保证多 worker 并发查 Job 不相互阻塞。

### Q14：手撕题（多线程交替打印 / 生产消费 / 限流）？
> 现场写。Go 版生产消费用 channel + WaitGroup；交替打印用两个 channel 互相唤醒；限流后面单独答。

### Q15：单测覆盖怎么做的？
> 项目 `go test ./...` 跑 ~80 组单测，覆盖率 60%+。关键策略：
> - **table-driven**：每个工具配 5+ 子测，Sampler 配 6 子测、Severity 自动升级配 10 子测。
> - **Fake/Stub**：MCP/LLM 都是 interface，单测用 fake 实现替换。
> - **CI 门禁**：评测 + lint + 单测三件套，PR 任一红就阻断。

### Q16：AST、插桩、单测自动生成？
> 这部分**不在本项目射程**——本项目主要是 Agent 系统集成，单测自动生成更适合代码工具方向。如果让我做，思路是 `go/ast` 解析函数签名 → 用 LLM 生成 table-driven 用例 → goimports + go vet 验证编译 → 跑 `go test -cover` 看分支覆盖、不达标就回炉。

### Q17：C++ / Rust 经验？
> 项目主语言是 Go。C++ 写过本科课设；如果引擎方向需要，能上手 Pybind11 / Cython / cgo。

### Q18：你最想问什么？
> （反问环节，准备 3 条）
> 1. 团队的 Agent 系统是 in-process 多 Agent 还是跨服务？工具是 in-tree 还是 MCP 协议？
> 2. 评测怎么做的？是离线 LLM Judge 还是线上 A/B？
> 3. 模型路由（便宜模型 vs 贵模型）有没有？

### Q19：你的项目最大的难点 / 亮点？
> 难点：**HITL 中断流式协议**——SSE 流要中途停下来等用户，前端 approve/reject 后再续上，状态对齐很麻烦。亮点：**HMAC-SHA256 链式审计**（`src/audit/hmac.go`，多 kid 轮换 + prev_sig 链 + 离线 `auditverify` CLI），写操作篡改任何一条都会让链断裂——这是 Repair Agent 上生产的硬门槛。

---

# 二、方向 B · 大模型应用工程 JD 对照（30 题）

> JD 关键词：**RAG**、**Agent 工具调用**、**Prompt 工程**、**评测**、**LLM 部署**、**限流/服务治理**、**MySQL/Redis 八股**

## A. RAG（4 题）—— 主战场是 [`project-llm/INTERVIEW.md`](../project-llm/INTERVIEW.md)，这里讲 Agent 侧消费


### Q：怎么把 RAG 接进 Agent 的？
> 通过 **MCP 协议**接入 `taiji_knowledge`（`trpc/knowledge/taiji/knowledge.go`）。Agent 不直连向量库，只看到一个"问知识库"工具：
> - 工具描述写明"运维 Runbook / 历史 Incident / FAQ 都在这查"。
> - LLM 自主决定要不要调（不像 LangChain 那种 forced retrieval）。
> - 知识库内部走 dense+sparse 混合 + RRF + Reranker（细节见 project-llm）。

### Q：检索质量不行怎么办？
> 三层：
> 1. **检索层**：dense+sparse 加权融合，`sparse_weight=0.3` 对运维 lexical-heavy 查询特别管用。
> 2. **生成层**：rerank score≥0.3 阈值过滤，低分 chunk 直接丢；citations 强制返回。
> 3. **Agent 层**：LLM Judge `EvidenceSufficiency` 维度评估"是否有足够引用"，不够时回 fallback 文案+排查方向，禁止编造。

### Q：知识库怎么更新？
> 离线：`scripts/build_index.py` 全量重建。增量：watch git 仓库 commit，diff 出新增文档走 incremental upsert。**当前是全量**——文档 < 5k，重建 < 5 分钟，没必要做增量。

### Q：长尾 query 怎么办？
> 两条路（**部分已实现**）：
> - **Query Rewrite**：LLM 把"游戏卡顿了"改写为"游戏延迟 / 帧率 / 帧时间 / OOM 排查"等多 query，并发检索后合并。
> - **HyDE**：让 LLM 先假设答案再检索答案的 embedding。当前未启用，是后续 P1。

## B. Agent / Prompt（5 题）

> 大部分见方向 A Q1-Q11。补充方向 B 特有的：

### Q：多 Agent 协调怎么不打架？
> 三道闸：
> 1. **transfer 严格单向**：Coordinator → Diagnosis/Repair/Knowledge，子 Agent 不互相 transfer。
> 2. **`WithEndInvocationAfterTransfer(true)`**：父 Agent transfer 完立即终结自己这一轮，避免父再抢话。
> 3. **工具切片**：每个子 Agent 只看见自己 target 的工具，物理上无法越权。

### Q：Agent 怎么避免无限循环 / 死磕一个工具？
> ReAct 配 `max_iterations=10`；同名工具同参数连续调 3 次自动注入"这个工具刚才调过了，结果是 X，请换思路"提示。

## C. 限流 / 服务治理（4 题）—— 真实缺口

> **诚实回答**：项目当前**未自研限流模块**，依赖：
> - **上游 MCP server 自带**：`taiji_knowledge` 用了 `golang.org/x/time/rate.Limiter` 做 QPS 限流（trpc-agent-go 框架自带）。
> - **下游 LLM**：openai-compatible API 自身有 RPM/TPM 限制。
> - **业务侧 fallback**：DeepSeek-V3.2 主备切换。

### Q：你会怎么自研一个限流？给 4 种方案对比：
| 算法 | 优点 | 缺点 | 适用 |
|------|------|------|------|
| **计数器/固定窗口** | 实现 1 行，O(1) | 边界毛刺（窗口切换瞬间 2x） | 粗粒度 |
| **滑动窗口** | 平滑 | 内存 O(N) | API 网关 |
| **令牌桶** `rate.Limiter` | 允许突发 | 单机；分布式要 Redis | 单机首选 |
| **漏桶** | 严格匀速 | 不允许突发 | 下游打不动时保护下游 |

### Q：分布式限流怎么做？
> Redis Lua 实现令牌桶——KEYS=bucket_key，ARGV=now/rate/burst，Lua 脚本里 `GETSET` 当前 token 数 + 时间戳，原子扣减。要点：
> - **Lua 保证原子**，不用 WATCH/MULTI。
> - **NTP 时钟漂移**：用 Redis 的 TIME 命令而不是客户端时间。
> - **热 key**：分桶 hash 到 N 个 key 上散列。
> - **降级**：Redis 挂了 fallback 到本地 `rate.Limiter`，宁松不严。

### Q：服务降级 / 熔断？
> Hystrix 那套思路：错误率窗口 > 50% 切 Open，半开试探 1 个请求成功后 Closed。Go 生态用 `sony/gobreaker` 或自研。本项目暂时只有 LLM 主备 fallback + MCP timeout，**没有完整熔断**——是后续 P1 看流量再加。

### Q：成本怎么控？token / 模型路由 / Prompt cache？
> 我在 [`src/cost/cost.go`](src/cost/cost.go) 写了一个**生产级三件套**：
>
> 1. **Token 会计**：`EstimateTokens` 按字符类别分桶估算（CJK 1.6:1 / ASCII 4:1），误差 ±10%，**不引入 tiktoken cgo 依赖**，纯 Go 离线可跑。`Tracker.Record` 累计每 session 的输入/输出/cached token + 美元成本，goroutine 安全（atomic + RWMutex）。
> 2. **模型路由**：`Router.Pick` 五条短路规则——预算耗尽 → cheap；enterprise+reasoning → premium；prompt > 8k → premium；多 tool+reasoning → premium；其它 cheap。配套单测覆盖 5 种 case。
> 3. **Prompt Cache**：基于 `container/list` 的 O(1) **LRU + 懒过期 TTL**。SHA-256 全 prompt 指纹做 key，命中**直接跳过 LLM 调用**，P95 从 ~800ms 降到 ~5ms。和 vLLM server-side prefix-cache 不冲突（那层是 KV 复用，这层是"完全相同请求一次都不发"）。
>
> 全部 17 个用例 PASS（含并发竞态测试）；Snapshot 接口给 Prometheus 打 `gameops_cost_usd_total{session_id, model}` gauge。

## D. LLM 部署（5 题）—— 主战场 project-llm

> 见 [`project-llm/INTERVIEW.md`](../project-llm/INTERVIEW.md) 的 vLLM/EAGLE/PD/FP8/Triton 段。这里 Agent 侧补一句：

### Q：Agent 用的是什么模型？怎么切换？
> 配置层 `app.yaml` 写 `provider: openai-compatible / anthropic / gemini / ollama`，依赖层 `trpc-agent-go/model/{anthropic,gemini,ollama,provider}` 已在 go.mod。运行期通过环境变量 `LLM_BASE_URL / LLM_API_KEY / LLM_MODEL` 一键切换；ollama 走本地 8B 模型用于内网无外网场景。

## E. 评测（2 题）

> 见方向 A Q9-Q10。

## F. MySQL / Redis 八股（4 题）

### Q：MySQL 索引为什么用 B+ 树不用 B 树 / 红黑树？
> B+ 树：非叶节点只存索引、叶节点链表 → 范围查询友好、磁盘 IO 少（一次 IO 一个 page，B+ 树扇出大、树高低）。红黑树是内存结构，磁盘场景树高 logN 太深。

### Q：索引失效场景？
> 5 个：
> 1. `LIKE '%xxx'` 前导通配；
> 2. 隐式类型转换（`WHERE id='1'`，id 是 int）；
> 3. 函数 / 表达式 `WHERE date(t)=...`；
> 4. `OR` 任一边没索引；
> 5. 联合索引最左前缀缺失。

### Q：Redis 持久化 RDB vs AOF？
> RDB 全量快照 fork+COW，恢复快但丢数据；AOF 增量日志，丢得少但回放慢。生产 RDB+AOF 混合（4.0+ aof-use-rdb-preamble）。

### Q：布隆过滤器？
> bitmap + k 个哈希，**有假阳无假阴**。用法：缓存穿透防护——查 DB 前先问布隆，"绝对不存在"直接返。本项目没用——QPS 不够、命中率高，不需要这一层。

## G. 反问 + 项目最大亮点

> 见方向 A Q18-Q19。

---

## 🕳️ 踩坑记录（与上述两份 JD 题面不重合的"加分项"）

1. **Coordinator 抢答**：Coordinator transfer 给子 Agent 后，自己在最后一轮也输出答案，前端拿到两份。修复：`WithEndInvocationAfterTransfer(true)` + 子 Agent 输出走 SSE 独立通道。
2. **MCP Streamable 断链**：Session 重连时 trace context 丢失。修复：在 MCP client 注入 `WithSessionReconnect(3)` + 透传 `traceparent` header。
3. **HITL 流被代理截断**：Nginx 默认 `proxy_buffering on` 把 SSE 流缓冲住等用户超时。修复：写明 `X-Accel-Buffering: no`。
4. **prefix-cache 命中暴跌**：见 Q2。
5. **审计 HMAC 切 key 时丢链**：换 kid 时 prev_sig 用旧 key 算的，新 kid 验签失败。修复：`audit/hmac.go` 多 kid 注册表，验签时按 kid 字段查表。
6. **JSON Schema 反射对 `omitempty` 处理**：`*int` 字段 nil 时 LLM 会传字符串 "null"。修复：自定义 unmarshal 容错。
7. **OTel TraceProvider 启动顺序**：`init` 阶段就要装好，否则前几条 span 落不到 collector。修复：`app.go` Boot 阶段第一步装 OTel。
8. **panjf2000/ants 池满阻塞主链路**：异步任务突发把 1000 池打爆。修复：`PoolSize` 配 + `JobQueueSize` 缓冲 + 拒绝策略走 `failed` 状态而不是阻塞。

---

## 📁 文件速查

| 关键能力 | 代码出处 |
|---------|---------|
| Coordinator | [`src/agents/coordinator/`](src/agents/coordinator/) |
| ReAct | [`src/agents/react.go`](src/agents/react.go) |
| 子 Agent | [`src/agents/{diagnosis,repair,knowledge,file_analyst}_agent/`](src/agents/) |
| 工具白名单 | [`src/tools/targeted.go`](src/tools/targeted.go) + `app.go` `FilterByTargets` |
| 异步执行器 | [`src/async/runner.go`](src/async/runner.go) + `fast_poll_waiter.go` + `job.go` |
| Session 分层记忆 | [`src/session/session.go`](src/session/session.go) |
| HMAC 审计链 | [`src/audit/hmac.go`](src/audit/hmac.go) |
| Prompt 注入防御 | [`src/plugin/input_guard.go`](src/plugin/input_guard.go) + [`deploy/guard_rules.yaml`](deploy/guard_rules.yaml) |
| OTel + Sampler | [`src/observability/`](src/observability/) + [`docs/observability.md`](docs/observability.md) |
| SSE / HITL | [`src/services/sse/sse.go`](src/services/sse/sse.go) |
| A2A | [`src/services/a2a/a2a_real.go`](src/services/a2a/a2a_real.go) |
| 评测 | [`eval/`](eval/) |
| 监控规则 | [`deploy/alerts/prometheus_rules.yaml`](deploy/alerts/prometheus_rules.yaml) |
| Grafana | [`deploy/grafana/panels.yaml`](deploy/grafana/panels.yaml) |
| **成本控制** | [`src/cost/cost.go`](src/cost/cost.go) + [`src/cost/cost_test.go`](src/cost/cost_test.go) |

---

## 🟢 Go 后端八股（必问）

### Q：Go 的 GMP 调度模型？
> **G**oroutine（用户态协程，KB 级栈）/ **M**achine（OS 线程）/ **P**rocessor（逻辑处理器，承载本地 G 队列）。`P` 个数 = `GOMAXPROCS` 默认核数；G 跑在 P 上，P 绑定 M。
>
> 关键机制：
> - **work-stealing**：本地 P 的 G 队列空了去隔壁偷一半；
> - **handoff**：M 阻塞（syscall）时 P 解绑后被另一个 M 接走，**不阻塞其它 G**；
> - **抢占式调度**（Go 1.14+）：基于信号 SIGURG，避免长 G 饿死同 P 上其他 G。

### Q：channel 实现原理？
> `runtime.hchan` 结构体里维护 ring buffer + sendq + recvq 两个 G 等待队列 + 锁。
> - 有缓冲 channel：buffer 没满直接拷数据 + 唤醒一个 recvq；
> - 无缓冲 / 满了：当前 G 入队 + `gopark` 让出 P。
>
> **常见坑**：close 已 closed 的 channel panic；**只能由发送方 close**；nil channel 永远阻塞（select 里常用作"禁用某分支"）。

### Q：Go 的逃逸分析？什么会逃到堆上？
> 编译期分析变量生命周期，不能在栈上释放就逃到堆。常见场景：
> 1. **取地址被外部引用**：`return &local`；
> 2. **interface 包装**：`var x interface{} = local`，因为 interface 本身需要堆指针；
> 3. **闭包捕获**：被 goroutine 引用的局部变量；
> 4. **map / slice 容量过大**：编译器保守判定；
> 5. **可变长 stack frame**：递归不固定深度。
>
> 用 `go build -gcflags="-m -m"` 看每行的逃逸决策。我项目里 hot path 的 `tools.Result` 故意做成值传递避免逃逸。

### Q：`sync.Pool` 适用场景？踩过什么坑？
> **GC 压力大、对象重复分配的 hot path**。比如 JSON 编解码 buffer。
>
> 三个坑：
> 1. **每次 GC 会清空 Pool**：不能存"必须存活"的状态；
> 2. **Get 后必须 Put**：用完忘 Put 等于普通 new；
> 3. **存大对象适得其反**：Pool 自带锁竞争，对象小到 GC 比锁还快时反慢。

### Q：context 是怎么实现取消传播的？
> 树形结构 `cancelCtx`，`cancel()` 时遍历 children 全部取消 + close `done` channel。**永远把 ctx 作为函数第一参数**，永远 `defer cancel()`，否则泄漏 goroutine。我项目所有 MCP / LLM 调用都接 ctx，timeout 触发时整条链路一起断。

### Q：你为什么用 Go 不用 Python 写 Agent？
> 三点：
> 1. **GIL 不存在**：高并发 SSE / MCP 多 server 并发调用，Go 直接线性扩；
> 2. **静态类型**：工具 schema 直接由 struct tag 反射生成，比 Pydantic 编译期就发现错误；
> 3. **部署简单**：单二进制，对运维友好——这是 GameOps 场景的硬需求。

---

## 🧮 手撕高频（数据结构 / 算法）

### Q：手写 LRU？
> 见 [`src/cost/cost.go`](src/cost/cost.go) 的 `PromptCache`——`container/list`（双向链表）+ `map[string]*list.Element`，Get/Put 都是 O(1)。配套 6 个单测（基本/淘汰/TTL/Stats/Update/Service 端到端）已 PASS。**面试现场可以直接默写**。

### Q：手写优先队列 / Top-K？
> 用 `container/heap` 实现 `heap.Interface` 五个方法（Len/Less/Swap/Push/Pop）。Top-K 维护一个**最小堆 size=K**，新元素 > 堆顶就替换。本项目评测时给"最难 K 条 case"用过这个套路。

### Q：手写跳表 / 一致性哈希？
> 跳表：每层链表逐层稀疏，期望 O(log N)。Redis ZSet 底层就是它（不用红黑树是因为范围查询快、实现简单）。
>
> 一致性哈希：N 个节点哈希到 [0, 2³²) 环上，key 落到顺时针第一个节点；**虚拟节点**解决数据倾斜（每个真实节点 100~200 个 vnode）。本项目分布式限流的 key 分桶就是这套。

### Q：限流算法（已问过）+ 滑动窗口手写？
> `[]int64` 存最近 N 个请求的纳秒时间戳，新请求来时**二分查找 < (now-window)** 的位置切片掉、O(log N)。或用 `container/list` 头删尾增 O(1)，按需选。

---

## ☸️ 部署与运维（K8s / Canary / A/B）

### Q：怎么部署 Agent？K8s 还是裸机？
> K8s。每个 Agent 是 Deployment + Service：
> - **资源**：Coordinator/子 Agent 都是 CPU 型负载（0.5C/512M 起），HPA 按 QPS 触发；
> - **依赖**：Redis（session）+ Qdrant（向量库）+ Langfuse（trace）走 StatefulSet；
> - **配置**：`trpc_go.yaml` 走 ConfigMap，敏感信息走 Secret + Vault；
> - **健康检查**：`/healthz` 端口走 readiness/liveness probe。

### Q：HPA 怎么配？为什么不只看 CPU？
> CPU 对 LLM Agent 是个伪信号——大多数时间在等下游 LLM 返回。我用 **自定义指标**（custom metrics）：
> - 主指标：**等待响应的 in-flight 请求数**（gauge），> 5/pod 扩容；
> - 辅指标：P95 latency > 2s 触发；
> - 限流指标：`gameops_input_guard_blocked_total` 突增告警，不是扩容信号。

### Q：Canary / 灰度怎么做？
> 三层 + 一个工具：
> 1. **流量层**：Istio VirtualService 按 header `x-canary=true` 打到新版本，**1% → 10% → 50% → 100%** 阶梯；
> 2. **特征层**：Coordinator 启动时读 `feature_flags.yaml`，按 user_id hash 决定开不开新工具；
> 3. **数据层**：评测系统 [`eval/`](eval/) 拉取灰度版本流量做离线对比；
> 4. **回滚**：Argo Rollouts 自动看 SLO（错误率 < 1% / P95 < 2s），不达标 5 分钟自动回滚。

### Q：A/B 测试怎么衡量 Agent 效果？
> 三类指标分层：
> - **业务指标**：故障平均修复时间（MTTR）、HITL approve 率、用户满意度（赞踩按钮）；
> - **质量指标**：LLM Judge 三维度分数、citation 覆盖率；
> - **成本指标**：每 session token 消耗、模型路由 cheap:premium 比例。
> 实验组与对照组同时跑 1 周，**最小可检测效应（MDE）+ 显著性**用 Welch's t-test。

### Q：Agent 怎么做端到端 SLO？
> 4 个 SLI：
> 1. **可用性**：5xx 比例 < 0.1%；
> 2. **首字延迟**：P95 < 1s；
> 3. **回答正确率**：LLM Judge ≥ 4.0；
> 4. **审计完整性**：写操作必须有 HMAC 签名 + 链不断。
>
> 任一连续 5 分钟违反触发 PagerDuty。Error Budget 28 天 0.1% × 5 分钟周期 = ~40 分钟容忍。

---

## 🌐 网络（HTTP/2 / TLS / gRPC）

### Q：为什么 SSE 不用 WebSocket？
> SSE 优势：
> 1. **基于普通 HTTP**，所有代理/CDN/防火墙天然支持；
> 2. **断线自动重连**（浏览器原生 `EventSource`）；
> 3. **单向（server → client）**，符合 LLM 流式输出语义。
>
> 用 WebSocket 反而要自己处理 ping/pong、心跳、粘包。HITL 需要双向通信时，**approve/reject 走 POST 单独接口**而不是同一条 WS——分离协议反而更稳。

### Q：HTTP/2 比 HTTP/1.1 强在哪？
> 1. **多路复用**：单 TCP 连接并发多 stream，**消除队头阻塞（HOL）**；
> 2. **头部压缩**：HPACK 压缩 header；
> 3. **server push**（实践中很少用）；
> 4. **二进制分帧**。
>
> 但 HTTP/2 在 TCP 层仍有 HOL（一个包丢了整条 stream 卡住），所以 HTTP/3 上 QUIC（UDP）。本项目对外 HTTP/1.1+SSE（兼容），内部 trpc-go 默认 gRPC over HTTP/2。

### Q：TLS 握手过程？为什么 1.3 比 1.2 快？
> TLS 1.2：4 个 RTT（TCP 1 + TLS 3：ClientHello/ServerHello+证书/客户端 finished）。
> TLS 1.3：**1 RTT**（合并多步 + ECDHE 默认）；**0-RTT** session resumption（但有重放风险，敏感写操作禁用）。

---

## 🟡 Python 八股（备答 - 与 Go 主项目互补）

### Q：asyncio 怎么工作？为什么能省线程？
> 单线程 + event loop + coroutine。`await` 让出控制权，loop 调度别的协程。**不是真并行，是高效切换**。CPU-bound 还是要 multiprocessing。

### Q：装饰器 / 上下文管理器 / 元类？
> 装饰器：`@dec` = `f = dec(f)`，本质高阶函数。我项目 [`observability/langfuse_tracing.py`](observability/langfuse_tracing.py) 的 `@observe_train` 就是装饰器实现训练步骤埋点。
> 上下文管理器：`with` 语句的 `__enter__` / `__exit__`，资源 RAII。
> 元类：类的类，`type(name, bases, dict)`，Pydantic / Django ORM 都靠它做声明式语法。

---

## ⚙️ 异步任务系统（生产级）—— 最容易被深挖的工程题

### Q：长时任务（30 分钟扩容）怎么做？为什么不让 LLM 同步等？
> LLM 框架的 tool 调用契约**就是同步**——LLM 等 tool 返回再继续推理，工具卡 30 分钟意味着整个对话挂起，HTTP 长连接、客户端 timeout、token 上下文窗都顶不住。我做了一套**异步任务系统** [`src/async/`](src/async/)，把语义从"同步等返回"改成 **"提交 JobID + 后续轮询/通知"**：
>
> ```
> 工具语义 = 阻塞                        →  Submit(JobID) → Poll(JobID) → Wait(JobID, timeout)
>                                            ↓                ↓
>                                          状态机          Webhook 回调
> ```
>
> 5 个 tool 暴露给 LLM：`job_submit / job_status / job_wait / job_cancel / job_list`。LLM 拿到 JobID 后可以**继续推理别的事**，等需要结果时再 `job_wait`，30 分钟内中途随时 cancel。

### Q：异步 Job 的状态机怎么设计？
> 5 状态 + 4 终态原则（[`src/async/job.go`](src/async/job.go)）：
>
> ```
>          submit                run                 success
>     ──────────► pending ─────────► running ─────────► succeeded
>                    │                  │
>                    │                  ├──── error ────► failed
>                    │                  │
>                    │                  ├──── panic ────► failed (含 ErrJobPanicked 前缀)
>                    │                  │
>                    │                  └──── cancel ───► cancelled
>                    │
>                    └────── ctx done ──────────────────► cancelled (pending 阶段被取消)
> ```
>
> 关键设计原则：
> 1. **终态不可逆**：succeeded/failed/cancelled 一旦写入永不变；
> 2. **panic = failed**：`recover()` 捕获后用 `errors.Is(err, ErrJobPanicked)` 标记前缀，调用方可分辨"业务错"还是"代码炸了"；
> 3. **Job.Clone**：所有 Get 接口返回深拷贝，避免跨 goroutine 读到中途修改的状态。

### Q：异步任务怎么做幂等？
> `Submit(toolName, args, timeout, idempotencyKey)` 第 4 个参数（[`src/async/runner.go:198`](src/async/runner.go)）。逻辑：
> 1. `idempotencyKey == ""` → 不去重，每次都新建 Job；
> 2. `idempotencyKey != ""` → MemStore 先扫一遍 `findByIdempotencyKey`，**命中则返回已有 JobID 不再起 worker**。
>
> 真实场景：`devops-flow` 工具触发扩容，LLM 因为某种原因（用户重发、上游重试）连续 submit 两次"对 prod-game 扩 5 个 pod"——第二次直接拿到第一次的 JobID，不会真的扩 10 个。**生产环境血的教训**。

### Q：worker goroutine 怎么防泄漏？
> 三道阀（[`src/async/runner.go`](src/async/runner.go)）：
> 1. **每个 Job 独立 ctx + cancelFn**：终态时 Runner 必调 cancelFn 一次（`once.Do`），保证 ctx 树释放；
> 2. **超时硬约束**：`context.WithTimeout(parent, jobTimeout)`，timeout 触发后 worker `select { case <-ctx.Done(): }` 必然返回；
> 3. **Janitor goroutine**：每 60s 扫一遍 Store，把超过 TTL 的终态 Job 清理掉，避免 MemStore 无限增长。
>
> `app.go` Shutdown 时 `r.Stop()` 关 done channel + WaitGroup 等所有 worker 退出，**不留孤儿**。

### Q：Wait 是怎么实现"通知 + 超时"双语义的？
> 不是简单的轮询。每个 Job 内部带一个 `done chan struct{}`，终态写入时 close(done)。`Wait(jobID, timeout)`：
>
> ```go
> select {
> case <-job.done:    // 通知到达
>     return job.Clone(), nil
> case <-time.After(timeout):
>     return nil, ErrWaitTimeout
> case <-ctx.Done():  // 调用方 cancel
>     return nil, ctx.Err()
> }
> ```
>
> O(1) 唤醒、零 CPU 等待、可级联 cancel。配套测试 [`runner_test.go`](src/async/runner_test.go) 覆盖了"提交后秒完成 + 提交后超时 + 提交后调用方 cancel"三种 case。

### Q：进程重启之后 Job 怎么办？
> 当前实现是 **MemStore + 显式声明丢弃**（[`src/async/store.go`](src/async/store.go) 注释明确写了原因）：进程重启后即使持久化了 Job 状态，**绑定的 context/goroutine 也已失效**，看似在 running 实际没人推进。所以重启后**所有非终态 Job 标记为 failed("agent restarted")**，比假装恢复要诚实。
>
> 生产升级路径：换 Redis Store + 单独的 worker 进程池（不和 Agent 同生命周期），或者直接用 Temporal/Cadence 这类工作流引擎托管。**P1 任务,不在当前迭代**。

---

## 🔐 审计可靠性（重试 / 退避 / 关闭语义）

### Q：审计远端 sink 怎么保证不丢？
> [`src/audit/remote_sink.go`](src/audit/remote_sink.go) 两层兜底：
> 1. **本地落盘优先**：HMAC 链先写 local file（fsync），再异步推远端，**远端永远不阻塞业务主流程**；
> 2. **远端失败重试**：5xx/429 走指数退避（200ms → 400ms → 800ms ...），4xx 不重试（认为是参数错，重试也没用），见 [`remote_sink_test.go`](src/audit/remote_sink_test.go) 的 `TestRemoteSink_RetryOn5xx` / `NoRetryOn4xx` / `Retry429` 三个用例。

### Q：组件 Close 之后还有写入怎么办？
> 标准答案是 **panic**（重复 close channel 那种）；标准的生产答案是 **drop with WARN 日志**——上游异步 emit 路径很难强一致同步，[`TestRemoteSink_WriteAfterClose`](src/audit/remote_sink_test.go) 专门验证关闭后 Write 不 panic 而是悄悄 drop。**容错优先于纯净**。

### Q：HMAC 链审计为什么能防"删审计日志"？
> 每条记录的签名 = `HMAC(key, prevSig || payload)`，**当前签名是上一条签名的函数**。攻击者要篡改第 100 条审计日志，必须重新计算第 100/101/.../N 条所有签名,这要求泄漏 HMAC key——而 key 不在线上服务里，存在独立的 KMS。
>
> 配套：**多 kid 轮换**（[`src/audit/hmac.go`](src/audit/hmac.go)）允许平滑换 key 不断链。state 文件丢了/损坏不 panic，**降级为新链从空开始 + 告警**——可靠性优先于审计完美。

---

## 🧪 集成测试（生产形态硬指标）

### Q：你的项目有端到端集成测试吗？
> 有，[`src/integration/`](src/integration/) 三大类：
> 1. **`webhook_integration_test.go`**：模拟蓝鲸告警 → webhook 接 → Coordinator 路由 → diagnosis_agent 执行 → 异步返回 → SSE 回吐；
> 2. **`async_integration_test.go`**：Submit→Wait→Cancel→并发 Submit 全链路，验证状态机不破；
> 3. **`repair_flow_test.go`**：诊断到修复完整 HITL 闭环，含两段式 approve/reject 流程，最终生成"goroutine leak → MR filed"的真实场景。
>
> 关键设计：`webhook.SyncForTest=true` 把异步 Runner 退化为同步执行，让测试可断言；生产环境关掉。

### Q：单元测试覆盖率多少？怎么保证不为了凑数？
> 核心模块：`async`（92%）、`audit`（88%）、`cost`（95%）、`tools`（82%）。CI 门禁：**核心包覆盖率不得 < 80%**。
>
> 反"凑数"措施：
> 1. **不用 `go test -cover` 的简单百分比**：用 `go-cover-treemap` 看分支覆盖；
> 2. **mutation testing**（[mutate](github.com/zegl/go-mutesting)）抽样改一个 if 条件看测试还能不能 fail，**fail 不掉的测试 = 假测试**；
> 3. **强制 race detector**：CI 里 `go test -race` 必须过，goroutine 系统的回归测试必经此关。

---

## 🤝 Multi-Agent 协作 / Coordinator 路由

### Q：Coordinator 怎么决定路由到哪个子 Agent？
> Coordinator 是 LLM Agent，**自己不执行业务工具**，唯一可调的是框架内置的 `transfer_to_agent`（[`coordinator/system_prompt.md`](src/agents/coordinator/system_prompt.md)）。决策依据 prompt 里写死的路由表：
>
> | 子 Agent | 触发场景 | 例子 |
> |---|---|---|
> | `knowledge_agent` | 运维文档/架构原理/FAQ/故障复盘 | "CrashLoopBackOff 怎么排查" |
> | `diagnosis_agent` | 实时排障/异常诊断 | "为什么这个 Pod 起不来" |
> | `repair_agent` | 修复/扩容/重启 | "把 prod-game 扩到 10 个" |
> | `chitchat_agent` | 闲聊/兜底 | "你好" |
>
> Coordinator **绝不直接回答业务问题**，否则就出现了"父 Agent 抢了子 Agent 的活"——这是 D14 修过的真实坑（PROGRESS.md 有记录）。

### Q：transfer_to_agent 是同步还是异步？怎么传上下文？
> 同步——Coordinator 调 `transfer_to_agent(name, message)` 后**整个 invocation 流转给子 Agent**，子 Agent 输出的 event 通过 Coordinator 的 event chan 透传到上层。会话状态（user_id/session_id）由 Runner 注入 ctx，子 Agent 自动继承。
>
> **不会**走第二次 LLM 调用（不是父 Agent 又问一遍子 Agent），框架层是真正的"控制权移交"。

### Q：子 Agent 之间能互相调用吗？
> 协议上可以，[trpc-agent-go](D:/UGit/Go-Agent/trpc-agent-go) 框架支持 sub_agent 链式 transfer。**但我项目禁掉了**——只允许 Coordinator → 子 Agent 单向，子 Agent 之间想协作必须返回 Coordinator 由其再调度。原因：**单向流让审计日志成为线性树**，互相调会形成 DAG，故障复盘时定位根因极难。

### Q：A2A 协议是什么？和 MCP 区别？
> **A2A**（Agent-to-Agent，Google 2024.4）：让不同公司/组织/语言的 Agent 互相调用，类似 Agent 版的 OpenAPI——AgentCard 描述能力、Task 协议管理生命周期、可流式可异步。
>
> 和 MCP 区别：
> | 维度 | MCP | A2A |
> |---|---|---|
> | 调用方 → 被调方 | LLM → 工具 | Agent → Agent |
> | 状态 | 工具大多无状态 | Task 有完整生命周期 |
> | 协作模式 | 单次 RPC | 长任务、可中断、可推送 |
>
> 项目当前**只用 MCP**，A2A 是 P2 探索方向（跨业务部门的 Agent 协作场景）。

### Q：5 个 Agent 同时启动一个 LLM 会怎样？资源够吗？
> 不会同时——Coordinator 路由后**只有一个子 Agent 在 active**。但子 Agent 内部可以并发多个 tool（多 MCP 并发查），这是真正的资源消耗大头。
>
> 实测 4C/8G 单机能扛 ~30 QPS（瓶颈在 LLM 上游），P95 ~1.8s。再高需要 K8s HPA 横向扩 Coordinator pod，子 Agent 是无状态的，扩容随便。

---

## 🎛️ 训练工程细节（容易被深挖）

### Q：`neat_packing: true` 是什么？为什么开？
> NPC SFT [`configs/npc_sft.yaml`](../project-llm/configs/npc_sft.yaml) 的 LLaMA-Factory 选项。背景：
> - SFT 数据样本长度差异大（NPC 对话有的 50 token，有的 1500 token）；
> - 朴素做法 padding 到 max_len，**短样本浪费 70% 算力**算 padding；
> - 普通 packing：把短样本拼接到一条 sequence，用 attention_mask 隔离——**但 cross-attention 仍可能跨样本泄漏**；
> - **neat_packing**：在 attention 层面给每个样本独立 attention mask（block-diagonal），物理上消除跨样本污染，**训练速度 +35%、loss 一致**。

### Q：`gradient_accumulation_steps` 怎么配？
> 有效 batch_size = `per_device_batch × num_gpus × accum_steps`。我配置：
> - knowledge_dpo：`per_device=4, accum=8` → 单卡有效 32（DPO 对负样本敏感，batch 不能太小）；
> - npc_sft：`per_device=8, accum=2` → 单卡有效 16（SFT 数据多，小 batch 收敛快）。
>
> **踩坑**：accum 太大显存反而炸——梯度在 accum 期间不释放，BatchNorm 行为也变化。Liger Kernel 的 fused chunk_size 帮你管这个。

### Q：cosine + warmup 学习率为什么是默认？
> 三个原因：
> 1. **Warmup**（前 10%）：刚启动梯度方差大，小 lr 让 Adam 状态稳定，否则 loss 第一步就 NaN；
> 2. **Cosine 退火**：后期 lr 平滑降到 0，相当于"先大步探、后小步精修"；
> 3. **不需要调超参**：相比 step decay 的 milestone 难选，cosine 只需要总 step 数。
>
> 经验：DPO/GRPO 因为奖励稀疏，warmup 比 SFT 更重要——我都设 0.1（10% step warmup）。

---

## 🎤 行为面 / STAR 故事（用于 30 分钟面试收尾）

### Q：项目最难的点是什么？
> **HITL 流式输出和异步任务的"语义对齐"**。LLM 输出是 SSE 流，HITL 需要在中间打断、等待审批、再继续；同时 repair 工具是 30min 异步 Job——三种异步语义（流式 token / 阻塞中断 / 后台 Job）在同一个 invocation 里交织。
>
> **解法**：
> 1. SSE 流上加一类专用 event `interrupt`，前端识别后弹审批框；
> 2. Coordinator 阻塞在 `<-resume` channel，业务侧 POST `/resume?session_id=X` 后唤醒；
> 3. 异步 Job 单独通过 `job_status` tool 轮询，**不和主流交织**。
>
> 三种异步语义解耦后，每条都可以独立测，集成测试 [`repair_flow_test.go`](src/integration/repair_flow_test.go) 跑得稳。

### Q：印象最深的一个 bug？
> **D14 的 "Coordinator 抢答"**——Coordinator 收到用户问题后，在调 `transfer_to_agent` 之前，自己先用预训知识"擅自回答"了一段，然后才转给子 Agent，导致用户看到**两条互相矛盾的回答**。
>
> 排查路径：Langfuse trace 看到 Coordinator span 里居然有 `chat.completion.chunk` 输出（Coordinator 不应该有内容输出，只应该 transfer）→ 定位到 system_prompt 没明确禁止"自己回答"→ 加了 **"你绝不直接回答业务问题，唯一允许的输出是 transfer_to_agent 调用"** 的强约束 prompt + few-shot 反例。
>
> 上线后回归 100 条 query，0 抢答。**这是 prompt 工程"消极示例"的价值——光给正例不够，必须给反例**。

### Q：如果重做项目，你会怎么改？
> 三件事：
> 1. **Job 持久化从一开始就用 Redis Store**：MemStore 重启丢任务的设计虽然诚实，但生产升级路径长；早期就上 Redis 能省后期重构成本；
> 2. **审计 schema 加版本号**：现在的 HMAC 链 payload 是裸 JSON，加字段时旧链验签会断；下次设计直接 `{"v":1, "data":{...}}`；
> 3. **评测金标用对抗样本占 30% 而非 5%**：项目早期金标都是"正常 query"，**模型学得过于讨好**——遇到越权/反讽/拼写错误时翻车率高，金标对抗样本占比要够。

### Q：开源框架（trpc-agent-go）有哪些不满意？你贡献了什么？
> 不满意：
> 1. **MCP transport 重连策略写死**：断开 3 次就放弃，长任务场景不够，**给社区提了 PR 把 retry 策略改成可配置**；
> 2. **session 默认 in-memory**：分布式部署必须自己接 Redis，**写了 [`src/session/session.go`](src/session/session.go) 封装统一接口**，业务零改动切换。
>
> 收获：读源码学到框架设计的"协议先于实现"思维——所有 Agent 间交互都先走协议层（event chan、tool schema），再考虑实现。

### Q：未来 1 年想成长的方向？
> 三块：
> 1. **AI Infra 深入**：当前会写 Triton kernel + 配 DeepSpeed，但**多机训练经验不够**——想搞透 ZeRO-3 + 3D 并行 + FP8 训练，目标是能单独负责 100B+ 模型的训练优化；
> 2. **Agent 复杂场景**：单 session 单 Agent 已经稳定，**长程多 Agent 协作（A2A）+ 自主规划（Planning + Reflection）** 是下一步；
> 3. **生产成本与效率**：当前 cost 模块只是开始，**端到端 cost-aware 调度**（按预算选模型路由 / 缓存 / 量化）是工业落地的硬指标。

### Q：你为什么对这个岗位感兴趣？
> （**按 JD 定制**——以下是两套模板,面试前根据具体 JD 替换关键词）
> - **基础架构方向**：项目里 AI Infra 占了 1/3 比重（Triton kernel / 分布式训练 / vLLM 推理 / FP8 量化），和团队"为模型训练和推理提供高性能基础设施"的描述高度匹配；想从"业务侧用 vLLM"升级到"贡献 vLLM 上游 PR"。
> - **AI Agent 平台方向**：项目本身就是 RAG + Agent + 多模态扩展空间，团队做端到端 Agent 平台和我落地经验完全对齐；想从"单一业务的 Agent"扩到"通用 Agent 平台"。

---

## 🧭 面试速查清单（按场景查）

| 面试官追问 | 第一个翻 | 第二个翻 | 兜底回答 |
|---|---|---|---|
| 限流怎么做？ | "未实现 + 设计方案" | 4 算法对比 | 诚实标注 P1 |
| 熔断怎么做？ | "未实现 + LLM 主备 fallback" | gobreaker 思路 | 诚实标注 |
| 异步任务？ | `src/async/` | 状态机 + 幂等 + Janitor | 完整生产级 |
| 审计可靠性？ | `src/audit/remote_sink.go` | 5xx/429 重试 + drop after close | RetryOn5xx 用例 |
| 集成测试？ | `src/integration/` | 三类 e2e | mutation testing |
| 成本控制？ | `src/cost/cost.go` | 17 单测 PASS | LRU+TTL 实现可默写 |
| 路由抢答？ | D14 STAR 故事 | "正例+反例" few-shot | Langfuse trace 定位 |
| HITL 中断？ | repair_flow_test.go | SSE event interrupt | 三种异步语义解耦 |
| Multi-Agent？ | coordinator/system_prompt.md | transfer_to_agent | 单向树结构 |
| 训练数学？ | project-llm/INTERVIEW.md "训练侧深问" | DPO loss / GRPO baseline | β=0.1 / 0.04 |
| 推理优化？ | project-llm 推理优化深问 | V0→V1→FP8→EAGLE-3 实测 | 3.67× 加速 |
| AI Infra？ | infra/ 目录 | Triton 2.18× + DeepSpeed | Nsight Compute 报告 |

---

## 🚀 端到端 Bring-up & Day-2 Ops

### Cold Start：从空机器到完整服务（依赖启动顺序）

```
Layer 0  基础设施（先起）
  ├─ Redis (session/idempotency/rate-limit 共用) ──┐
  ├─ Qdrant (vector store)                        │
  ├─ PostgreSQL (audit / Langfuse backend)        │── 全部 healthy 才进 Layer 1
  ├─ OTel Collector + Jaeger + Prometheus         │
  └─ Langfuse                                     ─┘

Layer 1  外部依赖
  ├─ vLLM (project-llm bring-up SOP)
  └─ MCP Servers (bk_monitor / bcs / 内部工具)
       └─ 每个 MCP server 独立健康检查 endpoint

Layer 2  Agent 主进程
  ├─ 加载 ConfigMap + Vault secrets
  ├─ 校验 tool whitelist（启动期 fail-fast，schema 不通过直接退出）
  ├─ 注册 A2A endpoint + 拉取 Coordinator 路由表
  └─ readiness probe = 上述 3 步全过 + 一次 dummy LLM call 成功
```

### 配置变更生效策略

| 配置类型 | 生效方式 | 例子 |
|---|---|---|
| **prompt / 路由规则** | hot reload（fsnotify + atomic.Value 双 buffer） | system_prompt.md 改完即生效 |
| **tool whitelist** | 滚动重启（安全敏感，必须 fail-fast 验证） | 新增工具上线 |
| **限流阈值** | hot reload（Redis 中心配置） | 突发流量临时调宽 |
| **模型路由** | hot reload | 主备模型切换 |
| **MCP server 端点** | 滚动重启（涉及连接池重建） | 新接入 MCP |

### Day-2 Operations Case Book

| 现象 | 排查 | 处置 |
|---|---|---|
| **MCP 调用全面慢** | OTel trace 看 span，bk_monitor p99 飙升 | bulkhead 隔离 + 该工具临时降级到只读缓存 |
| **某 session 卡死不返回** | Janitor 扫描 + Goroutine dump | 强制 cancel；后排查是 LLM hang 还是 MCP hang |
| **Redis 抖动 → 幂等失效** | 监控 idempotency 命中率 | 切本地 LRU 兜底（最终一致，业务可接受短窗口重复） |
| **LLM 配额耗尽** | cost.go 报警 | 降级到小模型路由 + 通知业务方 |
| **Coordinator 路由错误率↑** | Langfuse 抽样 trace | rollback prompt 到上一版（PromptOps 一键回滚） |
| **HITL 审批积压** | session count 报警 | 自动催办 + 超时拒绝（见下文 HITL 章） |

### Graceful Shutdown SOP（重点！）

```go
// 收 SIGTERM 后的处理顺序
1. Ingress 摘流量（K8s preStop hook + readiness 翻 false，等 30s）
2. Runner.Stop():
   ├─ 拒绝新 session
   ├─ in-flight session：
   │    ├─ 普通对话 → 等完成（最多 60s）
   │    ├─ 跑 Job 中 → 标记 paused，状态落 Redis，重启后恢复
   │    └─ 等 HITL 中 → SSE 发 reconnect 事件，状态落 Redis，
   │                   下次实例任意拉起来都能续（关键：HITL 状态必须外置）
3. flush 审计 buffer (audit/remote_sink 调用 FlushAndClose)
4. flush OTel span（强制 batch span processor flush）
5. 关连接池（Redis / DB / MCP）
6. 进程退出
```

> **核心原则**：**"在等审批"是合法状态，进程随时可以重启**。这要求：HITL 上下文 100% 外置（Redis），SSE 客户端要支持 reconnect+resume_from(event_id)。

---

## ⚙️ Running 态硬骨头（生产化必答题）

### 1. Sticky Session vs 状态外置

现状：默认 inmemory session → **必须 sticky**（K8s Ingress 按 session_id 哈希）
代价：HPA 扩缩容时部分 session 被打到新 pod 会丢上下文
**生产方案**：
- session 状态 1:1 镜像写 Redis（write-through），inmemory 只做读缓存
- 任意 pod 收到 session_id 都能从 Redis 重建 → 真正无状态化
- 写放大代价：Redis QPS ~3× session QPS，可接受

### 2. 长 session 的信息坍缩问题

现象：跑了 50 轮后 summary 已经压缩 N 次，早期细节（"上次他说的故障 ID 是 X"）丢失
**多层 memory 设计**：
- L0 working memory：最近 10 轮原文
- L1 summary：每 10 轮压一次（保留关键实体 + 时间锚）
- L2 entity memory：从全 session 抽出实体（故障 ID / 服务名 / 决策）单独存 KV
- L3 procedural memory：跨 session 的"用户习惯/偏好"，写入 Memory 模块

提问时按 L0 → 命中 L2 → 命中 L1 多路召回，避免单一 summary 漏信息。

### 3. MCP 级联失败防护（Bulkhead Pattern）

```go
// 每个 MCP server 独立 worker pool + 独立超时
type MCPBulkhead struct {
    pool   chan struct{}        // 限制并发 = 20
    timeout time.Duration        // 该工具独立超时
    cb     *gobreaker.CircuitBreaker  // 独立熔断
}
// bk_monitor 慢 5s 不会拖累 bcs / git_query 等其他工具
```

效果：单工具故障爆炸半径限制在该工具自身，**整体可用性从 99% × 99% × ... 退化为 max(99%) ≈ 99%**

### 4. 会话分布式锁（防并发改写）

同一 session 同时收到 2 个请求（用户快速点击 / 重试）→ events 写入 race
方案：Redis 分布式锁 `session:lock:{sid}` SET NX EX 30，未拿到锁返回 409。

---

## 🛡️ 限流 / 熔断 / 配额（设计方案，标注 P1 未实现）

> 诚实声明：当前未实现，但**设计方案完整**，能上手即写。

### 限流（4 维 Plugin 化）

仿照 input_guard 的 plugin 机制：

```go
type RateLimiter interface {
    Allow(ctx context.Context, key string) (bool, retryAfter time.Duration)
}

// 4 个 key 维度（取并集，任一 deny 就 deny）
key_user    = "rl:user:{uid}"          # 100 req/min
key_session = "rl:session:{sid}"       # 20 req/min（防恶意刷单 session）
key_tool    = "rl:tool:{tname}"        # 全局工具 QPS（保护下游 MCP）
key_model   = "rl:model:{mname}"       # 模型 token-bucket（保护 vLLM）
```

算法选型：
- **滑动窗口（Redis Lua 脚本）**：精度高，热点 key QPS < 1k 用
- **令牌桶（uber/ratelimit）**：突发流量友好，模型限流用
- **漏桶**：平滑严格，下游 MCP 保护用
- **分布式漏桶（Redis + ZSET）**：跨实例统一视图，user 级用

### 熔断（gobreaker，差异化策略）

```go
breaker_per_tool   = gobreaker.New(...)  // 工具级
breaker_per_model  = gobreaker.New(...)  // 模型级（主备 fallback）

// LLM 特殊性：业务错（schema 不合法）vs 系统错（5xx/timeout）必须分开统计
// schema 不合法不应触发熔断（提示词问题不该熔断模型）
ReadyToTrip: func(c gobreaker.Counts) bool {
    return c.SystemErrorRate() > 0.5 && c.Requests > 20
}
```

降级链：主模型 → 备模型 → 缓存 → 兜底文案 + HITL

### 配额（Cost-aware Quota）

cost.go 现在是单 session 累计，扩展为：

```
quota_keys:
  - user:{uid}:daily_token       # 用户日配额
  - dept:{deptid}:monthly_cost   # 部门月预算
  - tenant:{tid}:concurrent_jobs # 租户并发任务上限
```

配额耗尽时：
- 软超额（80%）：路由到便宜模型（Qwen3-8B 替 GPT-4）
- 硬超额（100%）：拒绝新请求 + 通知用户

### 公共组件抽象

把现在散落在 MCP / async / audit 各自的退避策略抽到 `pkg/resilience`：

```go
pkg/resilience/
  ├─ retry.go        # 指数退避 + jitter + max attempts
  ├─ breaker.go      # gobreaker wrapper（统一指标埋点）
  ├─ bulkhead.go     # 并发隔离
  └─ ratelimit.go    # 多算法 + 多 key 维度
```

---

## 🤝 HITL 流程深水区

### 审批超时 / 撤回 / 二次确认

| 场景 | 设计 |
|---|---|
| **审批超时** | session 设 `await_deadline=now+1h`，Janitor 扫描超时自动 reject + 通知用户重新发起 |
| **审批人 RBAC** | session 上带 `required_approvers=["sre-oncall"]`，从 IAM 拉权限校验，非授权人点 approve 直接拒 |
| **高危操作 2 人审批** | 高危 tool（drop/rm/scale-down）走"复审"模式，event chain 记 approver_1 + approver_2 |
| **审批撤回（回滚）** | approve 后 5 秒内允许 cancel：HITL 把 Job 提交后挂在 `pending_commit` 队列，5s 后才真 dispatch |
| **重启续审** | 见 Graceful Shutdown，HITL 状态 100% 外置 Redis |
| **审计回放** | HMAC 链 + Langfuse trace 串起来，能可视化重建"用户问→Agent 决策→申请审批→审批人→执行→结果"全链路，满足 SOX/等保审计 |

### Audit 链的可视化（合规硬要求）

```
event_0  user_query           hmac=h0
event_1  llm_decision         hmac=h1=HMAC(h0||payload)
event_2  hitl_request         hmac=h2=HMAC(h1||payload)
event_3  approver_action      hmac=h3=HMAC(h2||approver_signature||...)
event_4  job_dispatch         hmac=h4
event_5  tool_result          hmac=h5
```

任意一环篡改 hmac chain 断裂 → 审计页面红框告警。

---

## 💰 ROI / License / 合规

### 业务价值（oncall 场景实测）

| 指标 | Before | After | 收益 |
|---|---|---|---|
| 单告警 MTTR | 25 min | 8 min | **-68%** |
| 月节省工时 | / | ~850 h | ≈ ¥12 万人力 |
| 误操作高危事故 | 月 ~2 次 | 0（HITL 拦截） | 不可量化但**审计部门最爱** |
| 新人上手 | 3 月 | 1 月 | onboarding 加速 |

### License 矩阵

| 组件 | License | 风险 |
|---|---|---|
| trpc-agent-go | Apache 2.0 | ✅ 商用无限制 |
| Qwen3-8B（自部署） | Apache 2.0 | ✅ |
| DeepSeek API（兜底） | 商业协议 | ⚠️ 数据出网，仅限脱敏后调用 |
| MCP servers（内部） | 内部 | ✅ |
| Langfuse | MIT | ✅ |
| OTel / gobreaker / uber-go/ratelimit | Apache/MIT | ✅ |

### 合规清单

- ✅ **数据不出网**：自建 vLLM 兜底，DeepSeek 仅在脱敏后用
- ✅ **审计 90 天留存**：Langfuse + HMAC chain
- ✅ **高危操作必审批**：HITL 强制门禁
- ✅ **PII 脱敏**：input_guard / output_guard 双向
- ✅ **Prompt Injection 防御**：5 条规则 + 越狱样本回归测试
- ⚠️ **GDPR/PIPL**：仅内部用户场景已合规；如开放外部用户需补 DPA + 数据主体请求 API（P1 待办）

---

## 🏁 一句话收尾

> 这不是一个"用 LangChain 拼接的 Demo"，而是一个**生产形态的多 Agent 系统**：MCP 工具白名单、HITL 中断点、HMAC 链式审计、OTel 全链路观测、CI 评测门禁、Prompt 注入防御——每一项都有真实代码、真实单测、真实监控面板。开源框架（trpc-agent-go）是底座，**业务侧的安全/审计/评测/可观测才是工程价值所在**。
