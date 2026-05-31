# 🎯 面试随身卡 · 框架 vs 自研拆解 + 高频缺口题

> 配套阅读：
> - 面经原题 + 题后补注 → [markDown1779416409534.md](markDown1779416409534.md)
> - **框架内部机制速查（被追问"框架是怎么做的"必读）** → [framework_internals.md](framework_internals.md)
> - 项目深度答辩稿 → [../project-agent/INTERVIEW.md](../project-agent/INTERVIEW.md)
> - 项目架构图 → [../project-agent/ARCHITECTURE.md](../project-agent/ARCHITECTURE.md)
>
> 这份卡的目标：**面试前 5 分钟扫一眼，不会被"这块是你写的还是框架给的"问翻车**。

---

## 一、万能开场白（30 秒压缩版）

> 我做了一个 **Go 写的多 Agent 运维系统 GameOps Agent**，基于公司内部的 **trpc-agent-go v1.8.1** 框架。框架给了 Agent / Runner / Session / Memory / Knowledge / Tool / Callback / A2A / AG-UI 这套抽象；我**在它之上自研了 6 个生产级模块**：HITL 两段式确认、HMAC 链式审计、`pkg/resilience` 韧性原语、`src/async` 异步 Runner、`src/idempotency` 幂等键、`eval/` 双 Judge 评测体系——这些是框架不提供、运维写操作必须的。**项目跑通了 Coordinator + 4 个子 Agent，对接了 5 个内部平台（蓝鲸/BCS/工蜂/蓝盾/TAPD）**。

---

## 二、框架 vs 自研 · 全模块拆解表

> 三档颜色：🟢 完全用框架 / 🟡 框架基础上做了关键封装 / 🔴 完全自研

| 能力 | 档 | 框架（trpc-agent-go）给了什么 | 我做了什么 / 关键文件 |
|---|---|---|---|
| Agent 抽象 | 🟡 | `LLMAgent / ChainAgent / ParallelAgent / Runner / Planner` 接口 | 5 个 Agent 装配 + 中文化 ReAct prompt：`src/agents/coordinator/`, `react.go`, `common.go` |
| Session | 🟡 | `session.Service` + InMemory/Redis 实现 | Redis 后端 + 三档自动总结触发器：`src/session/redis_session.go`, `backend.go` |
| Memory | 🟢 | `memory.Service` + InMemory/Redis/GOES | 直接用框架默认 |
| Knowledge / RAG | 🟡 | `BuiltinKnowledge` + Embedder + Chunker + 4 种 VectorStore | 装配器 + stub 降级 + iWiki MCP 包装：`src/knowledge/builder.go`, `iwiki_tool.go` |
| Tool 系统 | 🟡 | `tool.Tool` + `function.NewFunctionTool` + MCP Client | **TargetedTool 按 target 给不同 Agent 切片可见性**：`src/tools/targeted.go` |
| MCP 接入 | 🟢 | `trpc-mcp-go` 全套 | YAML 声明式配置：`mcp_servers.yaml` |
| Callback / Plugin | 🟡 | 4 个 hook 点：pre/post-model、pre/post-tool | 4 个 callback 实现：`src/plugin/{input_guard,output_guard,safety_guard,audit_hook}.go` |
| A2A 协议 | 🟢 | `trpc-a2a-go v0.2.5` 服务端/客户端 | build tag stub/real 双实现，离线 CI 可编译 |
| AG-UI Web | 🟢 | `server/agui` 一键挂载 | 直接用 |
| 可观测性 | 🟡 | OTel TracerProvider/MeterProvider 已埋 LLM/Tool span | **GenAI Semantic Conv v1.30 字段补齐 + 业务自定义 Metrics**：`src/observability/{genai_span,metrics_more,metrics_toolcall}.go` |
| **HITL 两段式** | 🔴 | 无 | **完全自研**：`src/plugin/safety_guard.go` + `src/services/sse/sse.go` 推 `confirmation_required` 事件 |
| **HMAC 链式审计** | 🔴 | 无（仅日志） | **完全自研**：`src/audit/hmac.go`（链式 prev_sig + 多 kid 轮换 + 跨重启 state 持久化）+ `src/cmd/auditverify/` 离线验签 CLI |
| **韧性原语** | 🔴 | 无 | 自研 5 件套：`pkg/resilience/{retry,breaker,bulkhead,ratelimit,chain}.go` |
| **幂等性** | 🔴 | 无 | 自研：`src/idempotency/store.go`（Webhook + 工具调用幂等键，InMem/Redis 双后端） |
| **异步 Runner** | 🔴 | 框架仅同步 | 自研：`src/async/{runner,job,store}.go` + `job_submit/status/cancel/wait` 4 工具 |
| **Cost 归因** | 🔴 | 无 | 自研：`src/cost/cost.go` Token → 美元 → user/agent/tool 三维聚合 |
| **HTTP 客户端** | 🔴 | 无 | 自研 5 个：`src/infrastructure/{bcsapi,bkapi,devopsapi,gongfengapi,tapdapi}/`，每家含 Mock 模式 + 鉴权 + 重试 |
| **Webhook + Report** | 🔴 | 无 | 自研：`src/services/webhook/` + `src/report/` 蓝鲸/TAPD 推送 → 自动诊断 → Markdown 报告 |
| **评测** | 🟡 | `evaluation` 子包基础 ADK Eval | LLM-Judge + **算法 Judge（零 LLM 成本，自研亮点）**：`eval/judge_llm.go`, `judge_tool_selection.go`, `judge_prompt_store.go` |
| **CI 门禁** | 🔴 | 无 | 自研：`.gitlab-ci.yml` + `scripts/ci/comment-judge-summary.sh` MR 自动评论 + schema_version 守护 |
| **Skills** | 🟡 | 框架 Skills 机制 | 3 个 Skill 实现：`skills/{csv_compare,log_pattern,perf_report}/SKILL.md` |
| **Preflight** | 🔴 | 无 | 自研：`src/cmd/preflight/` 启动前自检所有平台 REAL/MOCK/DISABLED 状态 |

> ✅ **被追问"这是你写的吗"时，先指档位再答细节**：
> - 🟢 直接用：诚实说"用了框架的 xx"，加一句"我评估过 LangGraph/CrewAI 没选，原因是 yy"，把"调框架"答出"决策能力"。
> - 🟡 关键封装：必须能讲清"为什么默认实现不够、我加了什么、解决了什么具体问题"。
> - 🔴 完全自研：详细展开实现细节 + 为什么不复用开源（如 `pkg/resilience` 不直接用 `sony/gobreaker`：要嵌 OTel + 业务 metric）。

---

## 三、6 个自研亮点 · 极简话术（每个 1 分钟讲完）

### 亮点 1｜HITL 两段式确认（🔴）

**问题**：写操作（Helm 回滚 / MR merge / 流水线重跑）一旦 LLM 误判，爆炸半径大。

**方案**：
1. LLM 第一次调写工具不带 `confirmed`，工具直接返回 **Plan**（action / severity / side_effect / impact_scope / rollback_plan / human_prompt）；
2. 经 SSE `confirmation_required` 事件推给前端，**锁输入框 + 渲染高亮气泡**；
3. 用户回复"确认"，LLM 带 `confirmed=true` 重入同一工具，才真正执行；
4. `safety_guard.go` 在 pre-tool callback 拦截，确保任何写工具都过这一关。

**为什么自研**：框架 callback 是同步的，HITL 需要"中断流式 SSE → 等异步 approve → 续上"，状态机靠 invocation_id + step_id 双键对齐。

### 亮点 2｜HMAC-SHA256 链式审计（🔴）

**问题**：写操作必须不可篡改，运维事故复盘要能找到 root cause + 操作人。

**方案**：每条审计 = `payload + prev_sig + kid + sig`，sig=HMAC(payload || prev_sig, key[kid])。
- 多 kid 轮换：环境变量配 `HMAC_KEYS=kid1:k1,kid2:k2`，新 kid 加密、旧 kid 验签；
- 跨重启持久化：`state.json` 原子写（write-rename），保证 prev_sig 不丢；
- 离线验签：`go run ./src/cmd/auditverify -file audit.jsonl -keys kid1:k1` 全量重算，**断链立刻报警**。

**为什么自研**：开源审计库（如 audit-go）不带 LLM 场景特化的 Plan/Approve/Execute 三段式语义。

### 亮点 3｜TargetedTool 工具白名单（🟡）

**问题**：所有工具都喂给 LLM → schema 太长，prefix-cache 命中率掉，且子 Agent 越权风险。

**方案**：
```go
type TargetedTool interface {
    tool.Tool
    Targets() []string  // 这个工具属于哪些 target
}
func FilterByTargets(all []tool.Tool, targets []string) []tool.Tool { /* 子集 */ }
```
- DiagnosisAgent 拿 `[bk-monitor, bcs-read, tapd-read]` → 看到 10 个只读工具；
- RepairAgent 拿 `[bcs-write, gongfeng, devops, tapd-write]` → 看到 7 个写工具；
- **物理隔离，模型连工具名都看不到对方的，杜绝越权。**

### 亮点 4｜算法 Judge（零 LLM 成本，🟡）

**问题**：LLM-Judge 跑一次 12 条 golden case ≈ 0.2 美元 + 30 秒，CI 每次 MR 都跑成本爆炸。

**方案**：`eval/judge_tool_selection.go` 算法 Judge：
- precision/recall：实际工具调用集合 vs 期望集合的 F1；
- redundancy_penalty：连续相同 tool 超 2 次扣分；
- 综合 = `f1 × (1 - redundancy_penalty)`；
- **完全离线、毫秒级、确定性可复现**，CI 强阈值 0.85，红了直接 reject MR。

LLM-Judge 只在 nightly + 主分支跑，做 Faithfulness / AnswerCorrectness 等需要语义理解的维度。**离线+在线、便宜+昂贵两套并行**。

### 亮点 5｜异步 Runner + 状态机（🔴）

**问题**：长任务（Helm 部署滚动更新等 5-30 分钟）同步等会卡住 worker，HTTP 连接也撑不住。

**方案**：`src/async/runner.go` Job 状态机：
```
pending → running → { succeeded | failed | cancelled | timed_out }
```
- Store 持久化（InMem / Redis），进程崩溃后 watchdog 把超时 running 转 failed；
- 暴露 4 个工具给 LLM：`job_submit / job_status / job_cancel / job_wait`；
- LLM 提交后立刻拿 job_id 返回用户"任务已开始"，后台轮询完成态再回调。

### 亮点 6｜resilience 韧性原语（🔴）

**问题**：BCS/蓝鲸 API 偶发 5xx，需要重试；但又不能无限重试压垮下游。

**方案**：`pkg/resilience/` 5 件套：
| 原语 | 文件 | 用途 |
|---|---|---|
| Retry | `retry.go` | 指数退避 + 抖动 |
| Breaker | `breaker.go` | 三态熔断（closed/open/half-open） |
| Bulkhead | `bulkhead.go` | 信号量隔离，限制并发 |
| RateLimit | `ratelimit.go` | 令牌桶 |
| Chain | `chain.go` | 组合上面 4 个，**洋葱式包裹** |

**为什么不直接用 `sony/gobreaker`**：要嵌 OTel span + 业务 Prometheus metric，且要支持按 tool name 维度独立统计。

---

## 四、缺口题清单（INTERVIEW.md + 面经都没专门展开的高频题）

### 缺口 1｜Reflection / Reflexion / Tree-of-Thoughts

| 模式 | 一句话 | 我项目用了吗 | 为什么 |
|---|---|---|---|
| Self-Reflection | 单次自我批评后重答 | ❌ | 运维场景错误代价高，自反思反而增加幻觉 |
| Reflexion | 带跨 episode 经验池的 RL 自反思 | ❌ | 需要奖励信号，运维场景没现成 reward |
| Tree-of-Thoughts | 推理树展开 + BFS/DFS prune | ❌ | 成本指数级，token 预算扛不住 |
| **替代方案（我用的）** | 外部 LLM-Judge + HITL 两段式 | ✅ | 错误用人审兜底，比自反思更安全 |

> 被追问时的话术："这些模式我都看过论文（ReAct/Reflexion/ToT 都是 NeurIPS 23），但运维不是创造性任务，**用确定性更强的 ReAct + HITL + Judge 组合**。"

### 缺口 2｜Agentic RAG vs 经典 RAG

| 维度 | 经典 RAG | Agentic RAG（我用的） |
|---|---|---|
| 检索时机 | 固定一次 retrieve | LLM 自主决定调不调、调几次 |
| Query | 用户原文 | 可以 rewrite / decompose |
| 多轮 | ❌ | ✅ 上一轮答案不满意可再次检索 |
| 证据评估 | 无 | LLM 看完 context 觉得不够可继续 retrieve |

我项目里 KnowledgeAgent 是 Agentic RAG：`knowledge_search` 是个 tool，LLM 看 ReAct 中间结果决定要不要再查。

### 缺口 3｜Query Rewrite / HyDE / Self-RAG / CRAG

- **Query Rewrite**：LLM 把"游戏卡了"扩写成"延迟 / 帧率 / OOM / GC" 4 个 query 并行检索后 RRF 合并 → 我**部分实现**，靠 prompt 让 LLM 多检索几轮；
- **HyDE**（Hypothetical Doc Embedding）：先让 LLM 假设答案再用答案 embed 检索，对长尾 query 有效 → 我**未实现，是 P1**；
- **Self-RAG**：训练时让模型学会自己打 `<retrieve>` token → 训练成本高，**不适合应用层**；
- **CRAG**（Corrective RAG）：retrieve 后用轻量分类器评估相关性，差就走 web search 兜底 → **可借鉴**，运维场景 fallback 改成"问 iWiki"。

### 缺口 4｜RAG 评测 4 维（RAGAS 标准）

| 维度 | 评什么 | 我实现没 |
|---|---|---|
| Faithfulness | 答案每个事实是否在 context 里 | ✅ `eval/judge_llm.go` |
| Answer Relevancy | 答案是否回答了 query | ✅ |
| Context Recall | 期望文档是否被检索到 | ✅ `eval/judge_tool_selection.go` 同理 |
| Context Precision | top-k 里相关文档排前面没 | ⚠️ 部分 |

### 缺口 5｜LLM-Judge 的偏置

面试官追问"LLM Judge 你怎么保证不偏？"——必须能答：

| 偏置 | 描述 | 我怎么应对 |
|---|---|---|
| Position Bias | 评 A vs B 时偏向先呈现的 | 评测时 A/B 顺序随机化 + 平均两次 |
| Length Bias | 偏向长答案 | prompt 显式说"不评长度，只评事实" |
| Self-Enhancement | 评自己生成的答案打高分 | 用不同模型做 Judge（DeepSeek 评 GPT，反之亦然） |
| Verbosity Bias | 偏向措辞华丽 | 强制 JSON 格式化打分，禁止自由发挥 |

我用的 prompt 在 `eval/judge_prompt.yaml`，**热加载、可灰度**。

### 缺口 6｜Handoff vs Tool-call 调子 Agent 的本质区别

| 方式 | 调用方 | 子 Agent 能看到什么 | 何时用 |
|---|---|---|---|
| **Handoff（A2A transfer）** | 主 Agent 把控制权完全交出 | 共享 Session events | 子 Agent 要继续多轮交互 |
| **Tool-call**（子 Agent as Tool） | 主 Agent 只调一次拿结果 | 仅本次 input | 子 Agent 是工具型（如翻译） |

我项目用 **Handoff**——Coordinator 把控制权交给 Diagnosis 后，Diagnosis 走完整 ReAct，且能看到用户原始消息历史。

### 缺口 7｜KV Cache / PagedAttention / Speculative Decoding

| 概念 | 一句话 | 项目侧能优化吗 |
|---|---|---|
| KV Cache | 注意力 K/V 复用上轮缓存，避免重算 | 需模型推理引擎支持（vLLM/SGLang） |
| PagedAttention | 分页管理 KV，避免连续显存碎片，OOM 友好 | 同上 |
| Prefix Cache | system prompt 不变就复用 KV | **我直接受益**：固定 system prompt 前置，时间戳放 user 段，prefix-cache 命中率从 30% 回到 78% |
| Speculative Decoding | 小模型猜 N token，大模型并行验证 | 需推理侧支持，应用层不直接控 |

### 缺口 8｜Function Calling 模型侧训练

被问"模型怎么学会调工具的"——答：

1. **SFT 阶段**：构造 `<tool_call>{"name":"x","args":{...}}</tool_call>` 格式样本喂模型；
2. **RLHF / DPO**：用偏好对（好的工具选择 vs 差的）做对齐；
3. **推理时**：模型输出特殊 token，runtime 解析后真实调用，结果以 `<tool_result>` 喂回；
4. **OpenAI tools / Anthropic tool_use** 都是这个范式，只是 schema 略不同。

### 缺口 9｜算法手撕必背清单（按面经命中率排序）

| 题 | 模板关键词 | 项目相关性 |
|---|---|---|
| ⭐⭐⭐ K 个一组反转链表（已被字节问） | dummy + 哨兵 + 翻转子段 | 无 |
| ⭐⭐⭐ 最长无重复字符子串 | 滑动窗口 + map | 无 |
| ⭐⭐⭐ Top-K 频次 / 第 K 大 | 小顶堆 / quickselect | 评测 top-k |
| ⭐⭐ LRU / LFU | 双链表 + map | query embedding 缓存可讲 |
| ⭐⭐ 二分搜索旋转数组 | 判断哪半有序 | 无 |
| ⭐⭐ 全排列 / 组合和 | 回溯 + 剪枝 | 无 |
| ⭐⭐ 字典树 prefix | trie 节点结构 | 工具名前缀匹配可讲 |
| ⭐⭐ 多线程交替打印 | channel + signal | Go 直接 channel |
| ⭐⭐ 生产消费者 | buffered channel + WaitGroup | `src/async/runner.go` 真有 |
| ⭐ 限流（令牌桶） | time.Ticker + chan | `pkg/resilience/ratelimit.go` 真有 |
| ⭐ NumPy 手撕 multi-head attention（字节寄面） | Q@K^T/sqrt(d) + softmax + @V | 偏算法岗，背熟公式 |

---

## 五、面试翻车保命 5 句话

1. "**这块是框架给的，我做的是 xx 增强**"——先说边界。
2. "**我评估过 yy 方案，最终选 zz，因为 ww**"——展示决策能力。
3. "**这个我目前没做，但思路是 xx**"——诚实 + 设计能力。
4. "**线上数据是 xxx，CI 阈值是 yyy**"——真实数字打底。
5. "**这是我下一步要补的**"——主动暴露短板，比被挖出来好。

---

## 六、被追问 "这个为什么不用 LangGraph / LangChain / CrewAI"

| 框架 | 不选理由 |
|---|---|
| LangChain | 抽象太厚 / 调试难 / Python 栈与公司 Go 基础设施不匹配 |
| LangGraph | Python 栈 + 状态机表达力强但**侵入性高**，且 OTel 集成弱 |
| CrewAI | 偏向"角色扮演式协作"，**没有可观测性 / HITL / 审计** 这些生产硬需求 |
| AutoGPT | 实验玩具 / 无工程化 |
| **trpc-agent-go（我选的）** | 公司基础设施一等公民 / 原生 OTel + A2A + AG-UI / Go 栈一致 / 内网模型平台直连 |

> 加分话术："我不是为了 Go 而 Go——是为了**生产部署、合规审计、跨服务 A2A**这三个硬约束选 Go。"

---

## 七、最终自检 checklist（开始面试前 3 分钟过一遍）

- [ ] 能 30 秒讲完项目"是什么 + 用了什么框架 + 我做了什么"
- [ ] 能 1 分钟讲完任意一个 🔴 自研亮点（HITL / HMAC / resilience / async / cost / preflight）
- [ ] 能立刻说出 5 个内部平台名（蓝鲸 BK / BCS / 工蜂 / 蓝盾 DevOps / TAPD）
- [ ] 能说出 4 个子 Agent 的职责（Coordinator/Diagnosis/Repair/Knowledge/FileAnalyst）
- [ ] 知道 3 种 Agent 执行模式各对应哪个子 Agent（Coordinator=路由、Diag=ReAct、Repair=Plan→HITL→Execute）
- [ ] 能复述 4 个 callback hook 点（pre/post-model、pre/post-tool）
- [ ] 能讲清 RAG 4 维评测（Faithfulness/Answer Relevancy/Context Recall/Context Precision）
- [ ] 能背 1 道手撕题（K 个一组反转链表已被字节问过，必背）
- [ ] 准备好 3 个反问问题
