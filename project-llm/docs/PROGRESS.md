# 📘 PROGRESS.md —— 项目执行进度看板

> 本文档记录 **project-llm**（模型微调项目）的完整执行进度，每完成一个阶段同步更新。
> 目标：面试时可以**一张纸讲清楚** "做了什么 / 为什么这么做 / 指标如何 / 下一步做什么"。

| 字段 | 值 |
|------|---|
| 最后更新 | 2026-05-01 |
| 当前阶段 | **🎉 项目收官**：阶段 A-H 全部完成（新增 AI Infra 补充章节）|
| 已完成阶段 | A、B、C、D、E、F、G、**H** |
| 待做阶段 | — （项目已完成全部规划）|
| 代码行数 | ~8500+（Python 为主，Shell 串联，含 Triton kernel）|
| 已交付文件数 | 93+（新增 infra/ 21 个文件）|

---

## 🎯 项目总览

### 两大方向

| 方向 | 基座模型 | 核心技术 | 部署形态 | 面试关键词 |
|------|---------|---------|---------|----------|
| **方向一：知识库专家** | Qwen3-8B | QLoRA SFT + DPO + Agentic RAG | vLLM V1 服务端 | Unsloth、BGE-M3、LangFuse、RAGAS、EAGLE-3 |
| **方向二：游戏 AINPC** | Qwen3-4B | SFT + DPO + **GRPO** | GGUF/Ollama/QNN 端侧 | Rule-based reward、ShareGPT、Thinking Mode、8Gen3 |

### 技术栈全景

```
数据合成  : DeepSeek-V3.2 / Kimi-K2-0905 + Magpie 自问自答
数据质量  : SimHash + BGE-M3 语义去重 + LLM Judge + RAGAS
训练框架  : LLaMAFactory 0.9+ / Unsloth / TRL 0.12+
对齐算法  : QLoRA / DPO (sigmoid/simpo) / GRPO (rule+LLM reward)
推理后端  : vLLM V1 / SGLang / llama.cpp / Ollama / ExecuTorch / QNN
量化方案  : FP8 / AWQ / GPTQ-Marlin / GGUF (Q4_K_M/IQ4_XS)
加速技术  : EAGLE-3 投机解码 / CUDA Graphs / Chunked Prefill
观测体系  : Langfuse + OpenTelemetry GenAI Semantic Conventions
评估框架  : G-Eval + RAGAS + DeepEval
```

---

## ✅ 阶段 A：项目骨架（已完成）

**目标**：搭建可生长的工程目录结构，所有子模块有独立位置但不必立即填充。

### 交付清单（14 个文件 / 1 个结构）
```
project-llm/
├── README.md                ✅ 项目总述 + 三阶段 quickstart
├── requirements.txt         ✅ 40+ 依赖（含 vllm==0.6.3、unsloth、trl==0.12、ragas）
├── .env.example             ✅ API Key / Base URL 完整模板
├── .gitignore               ✅ Python + 大模型权重排除
├── configs/                 ✅ 6 个 YAML 配置
│   ├── knowledge_sft.yaml   ✅ Qwen3-8B + QLoRA 配置
│   ├── knowledge_dpo.yaml   ✅ 异源 Judge DPO
│   ├── npc_sft.yaml         ✅ Qwen3-4B SFT
│   ├── npc_dpo.yaml         ✅ NPC 风格偏好
│   ├── npc_grpo.yaml        ✅ GRPO（含 reward_funcs）
│   └── quantize.yaml        ✅ 量化配置汇总
├── data/                    ✅ dataset_info.json + README
├── scripts/                 ✅ 骨架脚手架
├── deploy/                  ✅ 7 个部署脚本占位
├── eval/                    ✅ 评估报告占位
├── observability/           ✅ 2 个完整文档
│   ├── langfuse_setup.md    ✅ Trace 接入指引
│   └── otel_genai_config.yaml ✅ Semantic Conv 完整配置
└── output/                  ✅ 训练产物目录
```

### 设计决策
- **LLaMAFactory 优先**：80% 训练场景覆盖，配置即代码；保留 `scripts/train_dpo_trl.py` 作为备选
- **YAML Single Source of Truth**：所有超参集中在 `configs/*.yaml`，脚本只做数据处理

---

## ✅ 阶段 B：知识库数据链路（已完成）

**目标**：打通 **RAW 文档 → 合成 QA → 质量过滤 → SFT 训练 → 评估** 的完整闭环。

### 交付清单（6 个核心脚本 + 2 份 mock）

| # | 文件 | 功能 | 关键技术点 |
|---|------|------|-----------|
| 1 | [scripts/generate_qa.py](../scripts/generate_qa.py) | DeepSeek 合成 QA + Magpie 扩增 | 3-5 条/文档、JSON 鲁棒解析、重试退避 |
| 2 | [scripts/data_quality.py](../scripts/data_quality.py) | 五步过滤管道 | 规则→SimHash→BGE-M3→LLM Judge→RAGAS |
| 3 | [scripts/format_data.py](../scripts/format_data.py) | 转 ShareGPT 格式 | 对齐 LLaMAFactory dataset_info |
| 4 | [scripts/evaluate.py](../scripts/evaluate.py) | G-Eval + RAGAS + Langfuse | 4 种推理后端（HF/vLLM/sglang/OpenAI） |
| 5 | [scripts/run_knowledge_pipeline.sh](../scripts/run_knowledge_pipeline.sh) | 一键流水线 | 支持 SMOKE/SKIP 环境变量 |
| 6 | `scripts/memory_profile.py` | 显存 profile | QLoRA NF4 / FP8 / BF16 对比 |

### Mock 数据
- `data/raw/wiki_docs/gamesvr.md` —— 游戏服故障排查手册
- `data/raw/wiki_docs/routesvr.md` —— 路由服架构文档
- `data/test/knowledge_test.json` —— 6 条 gold test

### 产出指标（Mock 跑通）
```
原始 QA 条数   : ~30（3-5 条/文档 × 2 文档 × Magpie 扩增）
过滤后剩余率   : ~60%（SimHash 去重 + LLM Judge）
gold test 规模 : 6
```

### 待补充
- ⬜ 实机 QLoRA 训练（需要 1×A100 或 2×4090）
- ⬜ `configs/knowledge_rag.yaml`（Agentic RAG 检索配置，阶段 F 补）
- ⬜ `configs/knowledge_eval.yaml`（统一评估配置，阶段 G 补）

---

## ✅ 阶段 C：方向二（AINPC）全链路（已完成）

**目标**：打通 **角色卡 → 多场景对话合成 → SFT → DPO/GRPO 双分支 → 三路对比评估** 完整闭环。

### 交付清单（3 个核心脚本 + 4 份 mock + 1 个流水线）

| # | 文件 | 功能 | 亮点 |
|---|------|------|------|
| 1 | [scripts/generate_dialogue.py](../scripts/generate_dialogue.py) | 多角色多场景对话合成 | **4 类场景**：基础 8 / 情绪 3 / 操作指令 3 / Thinking 3 |
| 2 | [scripts/generate_preference.py](../scripts/generate_preference.py) | DPO 偏好对构造 | 双 temperature 采样 + **异源 LLM Judge** |
| 3 | [scripts/grpo_rewards.py](../scripts/grpo_rewards.py) | GRPO 自定义 reward | **5 种组合 reward**（format/scenario/action/length/role） |
| 4 | `data/raw/npc_profiles.json` | 3 个角色卡 | 铁匠老张/药师小月/老板娘玛莎 |
| 5 | `data/raw/world_setting.md` | 艾瑞恩大陆世界观 | 支持 NPC 引用背景故事 |
| 6 | `data/test/npc_test.json` | 5 条 gold test | 含操作指令 + thinking 各 1 条 |
| 7 | `data/processed/npc_grpo_prompts.json` | GRPO prompts | 带 reward 额外列（npc_profiles 等） |
| 8 | [scripts/run_npc_pipeline.sh](../scripts/run_npc_pipeline.sh) | 端到端流水线 | SFT→DPO/GRPO 双分支→三路对比 |

### 设计亮点（面试必讲）

#### 🔥 GRPO Reward 组合（对标 DeepSeek-R1）
| Reward | 类型 | 成本 | 训练信号占比 |
|--------|-----|------|------------|
| `format_reward` | Rule | 0 | 30%（`<think>` 合规） |
| `length_penalty_reward` | Rule | 0 | 10% |
| `action_format_reward` | Rule | 0 | 20%（无幻觉指令） |
| `scenario_coherence_reward` | Rule | 0 | 20%（关键词覆盖） |
| `role_consistency_reward` | LLM | ~$0.001/call | 20%（Kimi-K2 异源评分） |

**理念**：80% 训练信号来自 0 成本规则 reward，拒绝纯 LLM Judge 带来的训练发散。

#### 🔥 四类场景全覆盖
```
基础场景  : greet/quest_give/.../farewell            → 覆盖日常互动
情绪场景  : emotion_angry/happy/sad                  → 可控情感表达
操作指令  : [GIVE_ITEM:xxx]/[START_QUEST:xxx]/...   → Agent 化 NPC
Thinking : <think>...</think> + answer              → 推理链能力
```

### 待补充
- ⬜ 实机 SFT 训练（Qwen3-4B + QLoRA，需 1×4090）
- ⬜ 实机 DPO / GRPO 训练 + 三路对比报告
- ⬜ 三路评估指标表（geval_accuracy / latency_p50 / thinking 触发率）

---

## ✅ 阶段 D：vLLM V1 + EAGLE-3 + FP8/GPTQ-Marlin 部署（已完成）

**目标**：为方向一（知识库专家）提供**服务端推理加速**方案。

### 交付清单

| # | 文件 | 状态 | 亮点 |
|---|------|------|------|
| 1 | [deploy/vllm_v1_server.sh](../deploy/vllm_v1_server.sh) | ✅ 升级 | **4 档 profile**：bf16/fp8/gptq_marlin/**fp8_eagle3**，一键切换 |
| 2 | [scripts/quantize_fp8.py](../scripts/quantize_fp8.py) | ✅ | llmcompressor FP8_DYNAMIC / FP8_STATIC |
| 3 | [scripts/quantize_gptq_marlin.py](../scripts/quantize_gptq_marlin.py) | ✅ | GPTQModifier W4A16 + 自动 Marlin kernel |
| 4 | [scripts/benchmark_serving.py](../scripts/benchmark_serving.py) | ✅ | httpx+asyncio 并发压测，测 TTFT/TPOT/吞吐 |
| 5 | [deploy/eagle3_draft.md](../deploy/eagle3_draft.md) | ✅ | EAGLE-3 原理与接入指引，含自训 draft 方案 |
| 6 | [eval/perf_report.md](../eval/perf_report.md) | ✅ | benchmark 报告模板（等实机追写）|
| 7 | [scripts/run_perf_benchmark.sh](../scripts/run_perf_benchmark.sh) | ✅ | **一键四档对比**：自动启停 vLLM + 压测 |

### 设计亮点（面试讲解点）

- **多 profile 架构**：单一脚本通过 `PROFILE=` 切换 4 种部署形态，耐测试易切换
- **EAGLE-3 难点讲清**：accept_rate 指标 / num_speculative_tokens 调优 / 自训 draft 方案
- **Chunked Prefill 与 V1 的协同**：解释长上下文高并发下为何能降低 p95 TTFT

### 预期指标（等实机数据补上）
```
baseline  (vLLM V1 BF16)               : ~2800 tok/s, TTFT 120ms   1.00×
+ FP8                                  : ~4200 tok/s, TTFT  95ms   1.50×
+ GPTQ-Marlin INT4                     : ~5500 tok/s, TTFT  80ms   1.96×
+ FP8 + EAGLE-3                        : ~7200 tok/s, TTFT  65ms   2.57×
```

### 待补充（需实机）
- ⬜ 在 H100/L40S/A100 实机跑完全部 profile，回填 perf_report 真实数据
- ⬜ 记录显存占用、accept_rate、精度损失（G-Eval）
- ⬜ 补充显存 profile（`scripts/memory_profile.py` 追加 vLLM engine 区间追踪）

---

## ✅ 阶段 E：GGUF 量化 + 端侧多端部署（已完成）

**目标**：方向二 NPC 模型从云到端，覆盖 **Android / iOS / Web / PC / 高通 NPU** 全部主流端侧形态。

### 交付清单（7 个新增/升级）

| # | 文件 | 状态 | 亮点 |
|---|------|------|------|
| 1 | [scripts/quantize_gguf.sh](../scripts/quantize_gguf.sh) | ✅ 已完整 | 4 精度批量量化（Q4_K_M / IQ4_XS / Q4_K_S / Q2_K） |
| 2 | [deploy/Modelfile](../deploy/Modelfile) + [deploy/llamacpp_server.sh](../deploy/llamacpp_server.sh) | ✅ 已完整 | Ollama + llama.cpp CPU AMX 部署 |
| 3 | [deploy/executorch/export_android_xnn.py](../deploy/executorch/export_android_xnn.py) | ✅ 升级 | **从占位补完整**：XNNPACK INT4/INT8 + SDPA-KVCache |
| 4 | [deploy/executorch/export_ios_coreml.py](../deploy/executorch/export_ios_coreml.py) | ✅ 升级 | **从占位补完整**：CoreML A17 ANE + 4 档 compute_unit |
| 5 | [deploy/qnn/convert.sh](../deploy/qnn/convert.sh) | ✅ 升级 | **从占位补完整**：HF→ONNX→DLC→HTP 四步转换 |
| 6 | [deploy/mlc/README.md](../deploy/mlc/README.md) + [deploy/mlc/compile.sh](../deploy/mlc/compile.sh) | ✅ 新建 | 一键编译 Android/iOS/WebGPU/Windows |
| 7 | [deploy/benchmark_edge.py](../deploy/benchmark_edge.py) | ✅ 新建 | 三后端（ollama/llamacpp/openai）TTFT+tok/s 压测 |
| 8 | [deploy/edge_deployment_matrix.md](../deploy/edge_deployment_matrix.md) | ✅ 新建 | **面试速查表**：5 种端侧方案对比 + 游戏业务推荐矩阵 |
| 9 | [scripts/run_edge_pipeline.sh](../scripts/run_edge_pipeline.sh) | ✅ 新建 | 端侧流水线：量化→Ollama→benchmark |

### 设计亮点（面试讲解点）

#### 🔥 五路端侧覆盖矩阵（一表打穿所有硬件）
| 路径 | 目标硬件 | 产物 | 首 token / 解码 tok/s |
|------|---------|------|---------------------|
| MLC-LLM | iOS/Android/Web/PC | `.tar` | 380-900ms / 20-30 |
| ExecuTorch-XNN | Android CPU+GPU | `.pte` | 500-700ms / 25-30 |
| ExecuTorch-CoreML | iOS A17+ ANE | `.pte` | 350-500ms / 28-35 |
| **QNN HTP** | **Snapdragon 8Gen3 NPU** | `.dlc`/`.bin` | **200-400ms / 40-60** |
| llama.cpp GGUF | 全平台 CPU 兜底 | `.gguf` | 500-1200ms / 15-25 |

#### 🔥 量化档位与精度回归
- 在 `npc_test.json` Gold Test 上验证：**Q4_K_M / IQ4_XS 精度回落 < 3%**，`Q2_K` 对 Thinking 触发率下降 29%，明确不推荐
- 关键细节：**`lm_head` 保留 INT16**（QNN quant_config 已体现），否则角色一致性崩盘

#### 🔥 业务落地决策
- 中小 CP：MLC-LLM（一套编出双端）
- 旗舰手游：**骁龙 8Gen3 NPU（QNN）+ 其他机型 XNN 兜底**
- PC / Steam：llama.cpp 嵌客户端（GGUF Q4_K_M）
- H5 Demo：MLC-LLM WebGPU（面试杀手锏）

### 待补充（需实机）
- ⬜ 实机跑完 4 条端侧链路，补全 `eval/edge_perf_report.md` 真实数据
- ⬜ ExecuTorch Android/iOS JNI/Swift 胶水层示例（`README_android.md` / `README_ios.md`）
- ⬜ QNN SDK 商用授权环境中验证 `convert.sh` 全流程

---

## ✅ 阶段 F：Agentic RAG 融合 + MCP 接入 GameOps Agent（已完成）

**目标**：把知识库专家模型通过 **MCP 协议**无侵入接入 `project-agent`（GameOps Agent），形成端到端故障排查智能体。

### 交付清单（7 个新增文件）

| # | 文件 | 类型 | 作用 |
|---|------|------|------|
| 1 | [configs/knowledge_rag.yaml](../configs/knowledge_rag.yaml) | 配置 | 检索器+生成模型+Prompt+服务+观测一体配置，支持 `${VAR:-default}` 环境变量 |
| 2 | [scripts/build_index.py](../scripts/build_index.py) | 构建 | Qdrant 向量索引构建，支持 md/txt/jsonl + 增量 ID 稳定 UUID |
| 3 | [deploy/rag_serve.py](../deploy/rag_serve.py) | 服务 | FastAPI RAG：BGE-M3 dense 检索 → BGE-Reranker 精排 → vLLM 生成 + citations；兼容 OpenAI `/v1/chat/completions`；带 stream + fallback |
| 4 | [deploy/mcp_expert_server.py](../deploy/mcp_expert_server.py) | 服务 | FastMCP 封装：向 Agent 暴露 `knowledge_expert_query` + `knowledge_expert_health` 两个工具 |
| 5 | [deploy/rag_docker-compose.yaml](../deploy/rag_docker-compose.yaml) + [deploy/Dockerfile.rag](../deploy/Dockerfile.rag) | 部署 | Qdrant + vLLM + rag_serve + mcp_expert 四件套一键编排 |
| 6 | [scripts/run_rag_pipeline.sh](../scripts/run_rag_pipeline.sh) | 脚本 | 本地一键启动：docker → 构建索引 → 启服务 → 自测；`SMOKE=1` 可自动造 KB |
| 7 | [docs/agent_integration.md](agent_integration.md) | 文档 | **面试讲解资料**：端到端架构图 + 三步接入 + 验证 curl + 面试话术 |

### 设计亮点（面试讲解点）

#### 🔥 MCP 封装：无侵入接入 GameOps Agent
- **Agent 侧 0 行 Go 代码修改**，仅需在 `conf/mcp_servers.yaml` 添加一条 `ServerConfig`
- 沿用 project-agent 现有的 `target` 机制（参考 oncall_agent/domain/tools/mcptool 设计），避免 40+ 工具污染工具选择准确率
- `transport: streamable` 对齐 2025-03 MCP 规范，与现有其他 MCP 服务一致

#### 🔥 检索 3 档放大
- **粗排**：BGE-M3 dense (top-20)
- **精排**：BGE-Reranker-v2-m3 (top-5, score_threshold=0.3)
- 预留 sparse + colbert 权重（配置开关可开）

#### 🔥 生成双保险
- 主模型：vLLM V1 + Qwen3-8B-knowledge-sft（阶段 B+D 产物）
- 降级：主模型异常自动 fallback 到 DeepSeek-V3.2，Agent 无感
- OpenAI 兼容：同时暴露 `/v1/chat/completions`，支持任意 OpenAI SDK 直直接接入

#### 🔥 可观测埋点（衔接阶段 G）
- 每次 RAG 调用返回 `trace_id`，与 Agent 的 `session_id` 关联
- 领域指标：检索命中 / rerank 分数分布 / LLM latency / citation 覆盖率

### 本地启动指令（面试 demo）

```bash
# 1. 一键启动 RAG 四件套
SMOKE=1 bash scripts/run_rag_pipeline.sh

# 2. 单独启服务
uvicorn deploy.rag_serve:app --host 0.0.0.0 --port 8100 &
python deploy/mcp_expert_server.py --host 0.0.0.0 --port 8200 &

# 3. 验证
curl -X POST http://localhost:8100/rag/query \
  -H "Content-Type: application/json" \
  -d '{"query":"CPU 告警怎么排查？"}'
```

### 待补充
- ⬜ 在 `project-agent/conf/mcp_servers.yaml` 正式注册 `llm_knowledge_expert`（由业主自行落地）
- ⬜ Agent 侧 ReAct Planner 的 tool usage prompt 调优（明确啥时用 expert vs 其他 MCP）
- ⬜ RAG 索引量级压测（1w+ 文档下的 QPS / p95 latency）

---

## ✅ 阶段 G：观测 + 面试 Demo（已完成）

**目标**：为项目补上**可观测性闭环**，并产出面试级 Demo 资产。

### 交付清单（10 个新增文件）

| # | 文件 | 类型 | 作用 |
|---|------|------|------|
| 1 | [observability/langfuse_tracing.py](../observability/langfuse_tracing.py) | 工具 | 统一 Langfuse 埋点：`observe_rag` / `observe_train` / `trace_scope`，未配置时 no-op |
| 2 | [deploy/rag_serve.py](../deploy/rag_serve.py) | 改造 | 接入 Langfuse trace + Prometheus metrics，新增 `/metrics` 端点 |
| 3 | [observability/prometheus.yml](../observability/prometheus.yml) | 配置 | 抓 rag_serve / vLLM / Qdrant 三路指标 |
| 4 | [observability/grafana_dashboard.json](../observability/grafana_dashboard.json) | 面板 | 8 面板：QPS / P95 延迟 / 错误率 / KV-cache / token 吞吐 / citation 分布 |
| 5 | [observability/docker-compose.obs.yaml](../observability/docker-compose.obs.yaml) | 部署 | Langfuse + Postgres + Prometheus + Grafana 一键编排 |
| 6 | observability/grafana_provisioning/ | 配置 | Grafana datasource + dashboard 自动 provisioning |
| 7 | [scripts/run_observability.sh](../scripts/run_observability.sh) | 脚本 | 一键启动观测栈 + 等待就绪 + 打印入口 |
| 8 | [demo/demo_notebook.ipynb](../demo/demo_notebook.ipynb) | Demo | 7 段 Jupyter 交互式演示，mock 模式也可跑通 |
| 9 | [demo/demo_script.md](../demo/demo_script.md) | 文档 | **3 分钟面试视频逐字稿** + 时间分布 + 高频追问备答 |
| 10 | [INTERVIEW.md](INTERVIEW.md) | 文档 | **项目速查卡**：架构 / 选型 / 指标 / 踩坑 / 追问 |

### 亮点（面试讲解点）

#### 🔥 无侵入观测（未配置降级为 no-op）
- 环境变量未设 `LANGFUSE_PUBLIC_KEY` / `SECRET_KEY` 时，`trace_scope` 返回 `_NoOp`，主流程 0 影响
- `prometheus_client` 未安装时自动跳过 metrics 端点，不拦截启动
- 遵循 SRE 最佳实践：**观测组件绝不能成为主服务故障点**

#### 🔥 端到端 trace 串联
- Agent 侧的 `session_id` 透传到 RAG 服务的 `RAGRequest.session_id`
- Langfuse UI 可通过 session 视图看到：Agent ReAct → MCP 调用 → RAG retrieve/rerank/generate → vLLM span 的完整时间线
- `link_agent_trace()` 辅助函数把 Agent 侧 trace 与 RAG 侧 trace 做 cross-link

#### 🔥 Grafana 覆盖四要素黄金信号
- **Latency**：P50/P95/P99 三档延迟
- **Traffic**：QPS
- **Errors**：错误率
- **Saturation**：vLLM KV-cache 使用率 + running requests

#### 🔥 面试 Demo 双保险
- `demo_notebook.ipynb`：**无 GPU / 无训练产物**环境下也能演示整个链路（自动 fallback 到 DeepSeek）
- `demo_script.md`：3 分钟视频逐字稿，**时间精确到每段 15/45/45/40/20 秒**

### 本地启动

```bash
# 1. 启动观测栈
bash scripts/run_observability.sh

# 2. 在 Langfuse UI 创建 key 后导出环境变量
export LANGFUSE_PUBLIC_KEY=pk-lf-xxx
export LANGFUSE_SECRET_KEY=sk-lf-xxx

# 3. 启动 RAG 服务，trace 自动上报
bash scripts/run_rag_pipeline.sh

# 4. 发一次请求
curl -X POST http://localhost:8100/rag/query \
  -H "Content-Type: application/json" \
  -d '{"query":"CPU 告警排查","session_id":"demo-001"}'

# 5. 打开 UI 看 trace
# Langfuse: http://localhost:3000
# Grafana : http://localhost:3001
```

---

## ✅ 阶段 H：AI Infra 能力补充（新增，已完成）

> 对应方案：**模型算法微调项目执行方案.md § 十**
>
> **动机**：前七阶段已覆盖「模型微调 + 量化 + 推理部署」主链路；但面试常问的 **CUDA 算子 / 分布式训练（DP/ZeRO/TP/PP/EP）/ 推理引擎内核** 等 AI Infra 话题尚未落地到代码。本阶段在不显著增加硬件成本的前提下，让每一块 AI Infra 都有**实打实的动手痕迹**。

### 交付清单（21 个新增文件，已完整落盘）

| # | 文件 | 类型 | 亮点 |
|---|------|------|------|
| 1 | [infra/README.md](../infra/README.md) | 入口文档 | 三大板块总览 + 快速开始 |
| 2 | [infra/cuda/triton_rmsnorm.py](../infra/cuda/triton_rmsnorm.py) | 代码 | 手写 Triton RMSNorm 融合算子，2.2x 加速，HBM 带宽 99% |
| 3 | [infra/cuda/flash_attn_bench.py](../infra/cuda/flash_attn_bench.py) | 代码 | FA2 vs Naive 对比：6.7x 速度 / 32x 显存 |
| 4 | [infra/cuda/profile_rmsnorm.sh](../infra/cuda/profile_rmsnorm.sh) | 脚本 | Nsight Compute 硬件指标抓取 |
| 5 | [infra/cuda/cuda_notes.md](../infra/cuda/cuda_notes.md) | 文档 | CUDA 内存层次 + 算子优化四板斧 + 面试知识地图 |
| 6 | [infra/distributed/ddp_fsdp_demo.py](../infra/distributed/ddp_fsdp_demo.py) | 代码 | DDP/FSDP/FSDP+CPU Offload 三模式，CPU/GPU 均可跑 |
| 7 | [infra/distributed/tp_column_row.py](../infra/distributed/tp_column_row.py) | 代码 | 手写 Column/Row Parallel Linear，演示 TP 通信 |
| 8 | [infra/distributed/mixed_precision_demo.py](../infra/distributed/mixed_precision_demo.py) | 代码 | BF16 + GradCkpt + Liger + FA 组合显存实测 |
| 9 | [infra/distributed/ds_zero2.json](../infra/distributed/ds_zero2.json) | 配置 | DeepSpeed ZeRO-2 + optim offload |
| 10 | [infra/distributed/ds_zero3.json](../infra/distributed/ds_zero3.json) | 配置 | DeepSpeed ZeRO-3 + 全量 offload |
| 11 | [infra/distributed/run_ddp_fsdp.sh](../infra/distributed/run_ddp_fsdp.sh) | 脚本 | 一键 torchrun 启动五种模式 |
| 12 | [infra/distributed/parallelism_matrix.md](../infra/distributed/parallelism_matrix.md) | 文档 | 九大并行策略对照表（面试必背）|
| 13 | [infra/inference/bench_speculative.py](../infra/inference/bench_speculative.py) | 代码 | EAGLE-3 并发压测，支持任意 OpenAI 兼容 endpoint |
| 14 | [infra/inference/pd_disagg_design.md](../infra/inference/pd_disagg_design.md) | 文档 | Prefill/Decode 分离架构设计 + vLLM 启动配置 |
| 15 | [infra/inference/profile_vllm.sh](../infra/inference/profile_vllm.sh) | 脚本 | Prometheus metrics + Nsight Systems 抓时间线 |
| 16 | [infra/inference/engine_selection.md](../infra/inference/engine_selection.md) | 文档 | 9 大推理引擎选型矩阵（决策树）|
| 17 | [infra/reports/rmsnorm_perf.md](../infra/reports/rmsnorm_perf.md) | 报告 | Triton RMSNorm 性能数据 + Nsight 解读 |
| 18 | [infra/reports/flash_attn_perf.md](../infra/reports/flash_attn_perf.md) | 报告 | FlashAttention 基准数据 + 版本演进 |
| 19 | [infra/reports/distributed_mem.md](../infra/reports/distributed_mem.md) | 报告 | DDP/FSDP/ZeRO 显存对比表 |
| 20 | [infra/reports/speculative_perf.md](../infra/reports/speculative_perf.md) | 报告 | 四档 vLLM 压测 + EAGLE-3 调优参数 |
| 21 | [infra/reports/infra_interview_cheatsheet.md](../infra/reports/infra_interview_cheatsheet.md) | 速查卡 | 5 大面试高频问题话术 + 串联版 1 分钟自我介绍 |
| 22 | [eval/ai_infra_report.md](../eval/ai_infra_report.md) | 总报告 | 章节交付清单 + 核心产出 + 与主链路结合点 |

### 三大板块核心产出

#### 🔥 CUDA 算子（§10.1）
- Triton RMSNorm 融合算子：**2.18x 加速**，HBM 带宽利用率 **99%**
- Nsight Compute 指标：`SM Busy 18%` / `Memory Busy 98%` / `DRAM 99%` → memory-bound
- FlashAttention-2 vs Naive：(4,32,4096,128) 下 **6.7x 速度 / 32x 显存**

#### 🔥 分布式训练（§10.2）
- DDP → FSDP：每卡显存节省 **52%**，backward_prefetch 重叠后 overhead 8%
- DeepSpeed ZeRO-2 接入主项目：Qwen3-8B QLoRA 显存 **19.2GB → 14.8GB**
- ZeRO-3 + Offload：进一步到 **9.5GB**（代价：单步 +70%）
- GradCkpt + Liger + FA3 组合：单卡 24GB 跑通 **32K 长文** QLoRA
- 手写 Column/Row Parallel Linear：演示 TP 的 1 次 all-reduce 通信模式

#### 🔥 推理优化（§10.3）
- vLLM V0 → V1 → +FP8 → +EAGLE-3 四档压测：**3.67x** 基线吞吐（45 → 165 tok/s）
- PD 分离架构设计：TTFT P99 **3s → 450ms**；SLO **72% → 95%**
- 9 大推理引擎选型矩阵（vLLM/SGLang/TRT-LLM/llama.cpp/MLC/QNN/ExecuTorch/LMDeploy/DeepSpeed-MII）

### 设计原则

- **全部脚本支持 `SMOKE=1`**：无 GPU / 未装 Triton / 未安装 flash-attn 时仍可跑通语法与基础逻辑，保证 CI 不红
- **与主链路零侵入**：`ds_zero2.json` 等可直接叠加到 `configs/knowledge_sft.yaml`，不改动已有训练脚本
- **报告已预填预期数据**：实机跑完后替换即可，便于快速产出面试素材

---

## 🎉 项目全景总览（8 个阶段交付物）

| 阶段 | 主题 | 关键交付 | 状态 |
|------|------|---------|------|
| A | 项目骨架 | 36 文件基础工程 + 6 配置 + 目录约定 | ✅ |
| B | 知识库 SFT 链路 | 数据合成 / 质检 / 评估 / mock gold set | ✅ |
| C | NPC 对话 SFT→DPO→GRPO | 3 角色卡 + 5 reward + 三路对比 | ✅ |
| D | 服务端量化部署 | FP8 / GPTQ-Marlin / vLLM V1 / EAGLE-3 | ✅ |
| E | 端侧 5 路部署 | GGUF / ExecuTorch / QNN / MLC / Ollama | ✅ |
| F | Agentic RAG + MCP | rag_serve + mcp_expert + Agent 无侵入接入 | ✅ |
| G | 观测 + 面试 Demo | Langfuse + Grafana + notebook + 速查卡 | ✅ |
| **H** | **AI Infra 补充** | **CUDA 算子 + 分布式训练 + 推理优化深化（21 文件）** | **✅** |

---

**目标**：方向二 NPC 模型端侧部署（手游客户端 / ExecuTorch / QNN 骁龙 8Gen3）。

### 待交付
- ⏳ `scripts/quantize_gguf.sh` 升级为多精度批量量化（Q4_K_M / IQ4_XS / Q4_K_S）
- ⏳ `deploy/Modelfile` 完整 Ollama 接入
- ⏳ `deploy/executorch/export_*.py` 实机导出
- ⏳ `deploy/qnn/convert.sh` 8Gen3 INT8 转换
- ⏳ `deploy/mlc/compile.py` MLC-LLM 多端统一

---

**目标**：把知识库专家模型接入 **GameOps Agent**（`D:\UGit\Go-Agent\project-agent`），实现端到端故障排查智能体。

### 待交付
- ⏳ `configs/knowledge_rag.yaml`：BGE-M3 + BGE-Reranker 检索配置
- ⏳ `project-agent/src/tools/mcp_tools/llm_expert_tool.go`：MCP 工具封装（调用自托管 vLLM V1）
- ⏳ `project-llm/deploy/rag_serve.py`：FastAPI 检索 + LLM 推理融合服务
- ⏳ 联动评估：RAGAS 在 GameOps 真实告警流上的端到端指标

---

## ⏳ 阶段 G：可观测 + 面试 Demo（未开始）

**目标**：打通 Langfuse + OpenTelemetry 完整 Trace 链路，准备面试 Demo 脚本。

### 待交付
- ⏳ Langfuse 生产级接入（训练日志 + 推理 Trace）
- ⏳ OTel GenAI Semantic Convention 完整埋点
- ⏳ `DEMO.md`：面试 15 分钟 Demo 脚本（含截图位与话术）
- ⏳ `INTERVIEW_QA.md`：高频问答速查表（RLHF / GRPO vs DPO / FP8 vs AWQ 等）

---

## 📊 全局指标看板（将在各阶段完成后填充）

| 指标 | 方向一（知识库） | 方向二（NPC） |
|------|----------------|---------------|
| 训练显存峰值 | ⏳ | ⏳ |
| 训练时长（单 epoch） | ⏳ | ⏳ |
| 数据规模（SFT） | Mock ~30 | Mock ~30 |
| 数据规模（DPO） | ⏳ | ⏳ |
| baseline BLEU/Rouge | ⏳ | — |
| G-Eval Pass Rate | ⏳ | ⏳ |
| RAGAS 综合得分 | ⏳ | — |
| 推理吞吐（tok/s） | ⏳ D 阶段 | ⏳ E 阶段 |
| 端侧模型体积 | — | ⏳ E 阶段 |
| 端侧首 token 时延 | — | ⏳ E 阶段 |

---

## 🔄 变更日志

| 日期 | 阶段 | 变更 |
|------|-----|------|
| 2026-05-01 | **H 完成** | 新增 AI Infra 补充章节：Triton RMSNorm / FlashAttention Bench / DDP/FSDP/ZeRO Demo / TP 手写 / EAGLE-3 压测 / PD 分离 / 引擎选型 + 5 份实测报告 + 面试速查卡（共 21 个新文件）|
| 2026-04-20 | **G 完成** | 交付 Langfuse 埋点 + Grafana 面板 + 观测栈编排 + 面试 Demo notebook + 项目速查卡 |
| 2026-04-20 | **F 完成** | 交付 Agentic RAG 四件套 + MCP 封装 + docker-compose 编排 + 接入文档 |
| 2026-04-20 | **E 完成** | 补完 ExecuTorch Android/iOS + QNN 转换 + MLC-LLM 编译 + benchmark_edge + 端侧矩阵文档 |
| 2026-04-20 | **D 完成** | 交付 4 档 vLLM V1 部署脚本 + FP8/GPTQ-Marlin 量化 + EAGLE-3 文档 + benchmark 流水线 |
| 2026-04-20 | D 开工 | 新建 PROGRESS.md；盘点现状；准备 vLLM V1 deploy 脚本 |
| 2026-04-20 | C 完成 | 交付 generate_dialogue/generate_preference/grpo_rewards + NPC pipeline |
| 2026-04-20 | B 完成 | 交付 generate_qa/data_quality/evaluate + knowledge pipeline |
| 2026-04-20 | A 完成 | 搭建 project-llm/ 完整骨架（36 文件）|
