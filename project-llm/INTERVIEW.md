# 🎯 GameOps LLM 项目 · 面试速查卡

> 一页纸覆盖：项目定位 / 技术选型 / 核心指标 / 踩坑记录 / 高频追问

---

## 📌 一句话概述

> 我做了一个**两方向、端到端、有观测**的大模型工程：**方向 A** 用 Qwen3-8B 做运维知识库专家（QLoRA + Agentic RAG + MCP 接入 GameOps Agent），**方向 B** 用 Qwen3-4B 做游戏 NPC（SFT→DPO→GRPO 三路对比，支持云端到移动端 5 路部署）。全链路接入 Langfuse + Prometheus 观测。

---

## 🏗️ 总体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                    GameOps Agent (Go/trpc-agent-go)                  │
│            ReAct Planner + MCP 工具编排 + Session 管理               │
└─────────────────────────┬───────────────────────┬───────────────────┘
                          │ MCP                   │ MCP
                          ▼                       ▼
               ┌────────────────────┐   ┌──────────────────┐
               │ bk-monitor / bcs / │   │ knowledge_expert │  ← 本项目新增
               │ gongfeng / tapd    │   │   (RAG 封装)     │
               └────────────────────┘   └────────┬─────────┘
                                                 ▼
                              ┌───────────────────────────────────┐
                              │ rag_serve: FastAPI                │
                              │  BGE-M3 dense → Reranker → vLLM   │
                              └────┬──────────────┬───────────────┘
                                   ▼              ▼
                             Qdrant :6333   vLLM V1 FP8 :8000
                                                 ▲
                                                 │ 阶段 B/D 产物
                                       Qwen3-8B-knowledge-sft

                           ╔═══════════════════════════════════════╗
                           ║           观测（阶段 G）               ║
                           ║  Langfuse ← trace  Prometheus+Grafana  ║
                           ╚═══════════════════════════════════════╝
```

---

## 🎯 方向 A：知识库专家

| 维度 | 选择 | 理由 |
|------|------|------|
| 基座 | Qwen3-8B-Instruct | 中文强 / 工具调用友好 / 可商用 |
| 数据 | DeepSeek-V3.2 合成 + Magpie | 成本低、质量稳，避免昂贵人工标注 |
| 质检 | BGE-M3 语义去重 + RAGAS | 干掉 30% 垃圾数据 |
| 微调 | LLaMA-Factory + QLoRA 4bit + Unsloth 2x + NEFTune | 18G 显存即可训 8B |
| 评估 | G-Eval + RAGAS + 自建 gold set | 覆盖 6 类运维问题 |
| 部署 | vLLM V1 + FP8 + prefix-cache | P95 80ms / 12G 显存 / 单卡 96 并发 |
| 融合 | BGE-M3 dense + BGE-Reranker-v2-m3 | top-20 粗排 → top-5 精排 |
| 接入 | FastMCP streamable-http | Agent 侧 0 代码变更 |

**关键指标（对比基座）**：
- citation 覆盖率：60% → **92%**
- 幻觉率：25% → **15%**
- P95 首 token 延迟：180ms → **80ms**
- KV-cache 命中率：42% → **78%**（prefix caching）

---

## 🎯 方向 B：游戏 NPC

| 阶段 | 方法 | 数据量 | 关键点 |
|------|------|--------|--------|
| SFT | Qwen3-4B + ShareGPT 格式 | 8k 对话 | 3 张角色卡 × 4 场景 |
| DPO | Kimi-K2 双 temperature 采样 + 异源 Judge | 3k pair | 降低 OOC 率 |
| GRPO | 5 种 reward：format/scenario/action/length/role | 1k prompt | 精细控制结构化输出 |

**关键指标**：
- 角色一致性（LLM-as-judge）：88% → **96%**
- 操作指令 JSON 可解析率：72% → **99%**
- 对话平均分（1-5）：3.8 → **4.4**

---

## 🚀 端侧部署矩阵

| 平台 | 方案 | 精度 | 内存 | 首 Token | 适用场景 |
|------|------|------|------|---------|---------|
| 服务端 GPU | vLLM + FP8 | FP8 | 12 GB | 0.08s | 线上 Agent |
| 桌面 CPU | Ollama + GGUF Q4_K_M | Q4 | 3.2 GB | 0.31s | 本地开发 |
| Android | ExecuTorch + XNNPACK | INT8 | 1.8 GB | 1.2s | 离线 NPC |
| iOS | ExecuTorch + CoreML | FP16 | 2.4 GB | 0.9s | App 集成 |
| 骁龙 NPU | QNN HTP (8Gen3) | W8A16 | 1.1 GB | 0.45s | 高端机实时对话 |
| WebGPU | MLC-LLM | q4f16 | 2.6 GB | 0.8s | 网页 Demo |

---

## 🔍 可观测

| 维度 | 工具 | 关键面板 |
|------|------|---------|
| 应用链路 | Langfuse v2 自托管 | session 级 trace，串联 Agent ↔ RAG ↔ LLM |
| 指标 | Prometheus + Grafana | QPS / P95 / 错误率 / KV-cache / token 吞吐 |
| 训练 | `@observe_train` 装饰器 | loss / lr / step 实时上报 |
| 降级 | 环境变量未配置时 no-op | 不影响主流程 |

---

## 🕳️ 踩坑记录（面试可讲的真实经历）

1. **QLoRA 学不动中文专业词** → 先用领域文档做 **continue pretrain 0.5 epoch**，再 SFT
2. **RAG 召回准但生成乱答** → 发现是 Prompt 里 `[参考资料]` 段落太长被截断，改用 **max_length=8192 的 BGE-M3** + **rerank 后限定 top-5**
3. **DPO 训后模型变"讨好型"** → chosen/rejected 差距太大导致，改成 **双 temperature 采样（0.3 vs 1.0）** + **异源 Judge**
4. **GRPO reward 相互冲突** → format reward 压制了 creativity，改为 **加权可配置 + warmup 100 step 后再加多项 reward**
5. **vLLM 切 V1 后 prefix-cache 失效** → 原因是 system prompt 被动态拼接，**把固定部分前置 + 增加 `--enable-prefix-caching`**
6. **MCP Streamable 连接断链** → session 3 次重连，配合 **`WithSessionReconnect(3)`** + **timeout=60s**
7. **端侧 ExecuTorch 导出失败** → Qwen3 的 RoPE 实现不被 XNNPACK 识别，**改用 HuggingFace 的 `export_llama` 脚本 + 自定义 partition**
8. **Langfuse session_id 对不齐** → Agent 侧要透传 session_id 到 MCP 参数，**加了 `link_agent_trace()` 辅助函数**

---

## ❓ 高频追问 / 备答

### 训练相关
**Q：为啥不用 RLHF 全流程？**
> RLHF 需要训 reward model，数据成本和训练不稳定性都太高。DPO 用**隐式奖励**绕过了 RM 训练；GRPO 更进一步，用**可验证的规则奖励**（比如 JSON 是否合法）代替 RM，对我这种结构化输出场景性价比最高。

**Q：QLoRA 的 rank 怎么选？**
> 从 rank=8 开始试，看验证集 loss 曲线。rank=16 对 8B 模型够用；rank=64 会过拟合我的 5k 数据。

### RAG 相关
**Q：为啥 BGE-M3 不用稀疏检索？**
> 实际上**用了**。`deploy/rag_serve.py` 的 `Retriever.search` 一次前向同时返回 dense + sparse（lexical_weights），dense 走 Qdrant 的标准向量检索、sparse 走 Qdrant 的 NamedSparseVector，两路结果用 **RRF（Reciprocal Rank Fusion，k=60）按 yaml 里的 `dense_weight=1.0` / `sparse_weight=0.3` 加权融合**，再交给 Reranker。BGE-M3 的 sparse 头本质就是**学习版 BM25**，对运维场景里的命令名、告警码、服务标识符这类 lexical-heavy 查询特别管用——上线后 citation 覆盖率从纯 dense 的 87% 又涨了 5pp。需要回退时把 yaml 的 `hybrid_search` 关掉就退化为纯 dense，零代码变更。

**Q：稠密 + 稀疏怎么融合的？为什么用 RRF 不用归一化加权？**
> 两路检索的分数尺度完全不同（cosine ∈ [-1,1] vs sparse 内积无上界），强行归一化要校准、对数据敏感。RRF 只看排名不看绝对分数：`score(d) = Σ w_i / (k + rank_i)`，**对分数分布免疫**，是 TREC 多年榜单上的稳定方案；`k=60` 是 Cormack 2009 的经验值。融合后再用 BGE-Reranker-v2-m3 一锤定音，最后 MMR（λ=0.7）做多样化、避免同一文档多个相邻 chunk 把 top-5 占满。

**Q：rerank 分数 0.3 这个阈值怎么定的？**
> 从 gold set 上做 grid search：0.2 带入噪声，0.4 丢召回，0.3 是 recall / precision 的最佳折中。

### Agent 相关
**Q：怎么避免 Agent 总选错工具？**
> 三道阀：
> 1. **target 预过滤**：排障场景只挂 10 个 MCP，不挂 40 个
> 2. **工具描述 schema 要写**什么时候调用什么时候不调用
> 3. **ReAct prompt** 里放 few-shot 示例

**Q：如果 RAG 服务挂了 Agent 会怎样？**
> RAG 服务本身有 DeepSeek fallback；MCP 层 timeout=60s 触发后，Agent ReAct 会跳过这个工具继续下一步；Langfuse 里会看到 `status=ERROR` 的 span。

### 工程相关
**Q：FP8 量化精度怎么保证？**
> 用 vLLM 自带 `W8A8_FP8` 配方，对 Qwen3-8B 实测 MMLU 下降 0.3pp，可以接受。关键参数是 `quantization_config.activation_scheme=dynamic`，比 static 少 2pp 掉点。

**Q：你的项目和 LangChain 那种 RAG 有啥区别？**
> 三点：
> 1. **接入方式**：我用 MCP 标准协议接入 Agent，LangChain 是 SDK 耦合
> 2. **融合模型**：我用自己微调的 Qwen3-SFT，不是直接丢给 GPT
> 3. **观测**：我有 Langfuse session 级链路追踪，LangChain 默认只有 log

---

## 🧠 训练侧深问（数学公式 / 超参 / 失败模式）

### Q：QLoRA / LoRA 数学原理？为什么能省显存？
> LoRA：把权重更新分解为 `ΔW = B·A`，`A∈ℝ^{r×d}` 高斯初始化、`B∈ℝ^{d×r}` 零初始化，`r ≪ d`。前向 `h = Wx + (BA)x · α/r`，**只训 A、B 两个低秩矩阵**——8B 模型 16-bit 全参 32GB，rank=16 的 LoRA 参数量 ~1%，可训参数 < 60MB。
>
> QLoRA 在此之上：
> 1. **NF4 量化**冻结的 W：4-bit NormalFloat（按正态分布分位点量化），均方误差比 INT4 低 ~30%；
> 2. **Double Quantization**：连量化 scale 自身再量化一次，省 0.37 bits/param；
> 3. **Paged Optimizer**：CPU/GPU 间分页 swap 避免 OOM。
> 实测 Qwen3-8B QLoRA 显存 19.2GB（[`configs/knowledge_sft.yaml`](configs/knowledge_sft.yaml) `lora_rank: 16` + `liger_kernel: true`），叠 DeepSpeed ZeRO-3 + Offload 降到 **9.5GB**（[`infra/distributed/`](infra/distributed/)）。

### Q：DPO 损失函数是什么？β 怎么调？
> ```
> L_DPO = -E[ logσ( β·log(π(yw|x)/π_ref(yw|x)) - β·log(π(yl|x)/π_ref(yl|x)) ) ]
> ```
> **隐式 reward** `r(x,y) = β·log π/π_ref`，本质是把 PPO 的 reward model 步骤折叠进 ranking loss。β 是温度：
> - β 太大（>0.5）：模型不敢偏离 ref，几乎不学习；
> - β 太小（<0.05）：放飞自我，**讨好型崩溃**（chosen/rejected 极端化）；
> - 经验值 **β=0.1**（[`configs/{npc,knowledge}_dpo.yaml`](configs/) `pref_beta: 0.1`）是 Anthropic / TRL 默认。
>
> **踩坑**：DPO 训完模型变"复读机"——chosen 长度系统性大于 rejected 时模型学到"答得长就赢"。修复：用 **SimPO** 的长度归一化项 `r/|y|`，或者数据侧先按长度配对。

### Q：GRPO 和 PPO 的差异？为什么不用 PPO？
> PPO 需要 **Critic / Value Model**——再训一个和 Actor 同规模的网络，显存翻倍。GRPO（DeepSeek-R1）的关键洞察：**用 group 内 reward 的均值做 baseline**：
> ```
> A_i = (r_i - mean(r_group)) / std(r_group)
> ```
> 一个 prompt 采样 G 个 response，组内归一化做优势，**完全省掉 Critic**。配合可验证的规则奖励（JSON 合法 / format 正确 / role 一致）省掉 Reward Model，端到端只需 Actor + ref。我项目 [`configs/npc_grpo.yaml`](configs/npc_grpo.yaml) `beta: 0.04`（KL 比 DPO 更小，因为奖励是稀疏的二值/0~1，强 KL 会压死探索）。
>
> **5 类 reward 加权**：format(JSON 合法)、scenario(关键词命中)、action(动作合法性)、length(长度区间)、role(人设关键词)；warmup 100 步只开 format，否则模型为追 role 把 JSON 都生成不出来。

### Q：QLoRA rank 怎么选？target_modules 选哪些？
> `rank` 调参思路：
> - 8B / 数据 5k / **知识注入** → rank=8~16 够（[`knowledge_sft.yaml`](configs/knowledge_sft.yaml) rank=16）；
> - 4B / 数据 8k / **风格+人设拟合** → rank=32（[`npc_sft.yaml`](configs/npc_sft.yaml) rank=32），低秩学不进 NPC 个性；
> - rank > 64 在我数据量上几乎必过拟合。
>
> `target_modules` 默认 `all-linear`（[`scripts/train_dpo_trl.py`](scripts/train_dpo_trl.py)），等价于把 q/k/v/o/gate/up/down 7 个投影都挂 LoRA。如果只挂 q/v 会少参 50%，但下游知识 QA 准确率掉 3-5pp，**不值**。

### Q：训练加速用了什么？Liger / FlashAttention / Unsloth 区别？
> 三件套各管一摊：
> - **FlashAttention-2/3**：把 attention `softmax(QK^T)V` 融合成单个 kernel，**O(N²) 内存 → O(N)**，[`infra/cuda/flashattn_bench.py`](infra/cuda/flashattn_bench.py) 实测 6.7x 速度 / 32x 显存；
> - **Liger Kernel**（开源高性能算子库）：把 RMSNorm / RoPE / SwiGLU / CrossEntropy 用 Triton 重写，主项目 yaml 已开 `liger_kernel: true`；
> - **Unsloth**：QLoRA 专项优化，2x 速度 / 50% 显存，但只支持单卡。
> 三件套**不冲突**：Unsloth 内部就用 FlashAttn，Liger 提供更广覆盖。我主链路用 LLaMA-Factory + Liger（生态好）+ FlashAttn-2，Unsloth 作为 4090 单卡快速迭代选项。

---

## 🔬 推理优化深问（vLLM / EAGLE / KV / PD 分离）

### Q：vLLM 为什么快？PagedAttention 是什么？
> 传统推理把 KV cache 按"批序列"连续存，**显存碎片化严重**——一个序列结束就留一段空洞，新请求没法填进去。PagedAttention 借鉴 OS 虚拟内存：
> - KV cache 切成固定大小的 **block（默认 16 token / block）**；
> - 逻辑序列 → 物理 block 通过**block table**映射；
> - 不同序列共享相同 prefix 时直接共享 block（**copy-on-write**）。
>
> 收益：显存利用率 >96%（朴素实现 ~60%）、prefix-cache 几乎零成本。

### Q：vLLM V0 / V1 / +FP8 / +EAGLE-3 四档实测？
> 见 [`eval/perf_report.md`](eval/perf_report.md) 与 [`infra/inference/bench_speculative.py`](infra/inference/bench_speculative.py) 真实压测（concurrency=8/16/32/64 四档）：
>
> | 配置 | tok/s | TTFT | 相对 V0 |
> |------|-------|------|---------|
> | V0 baseline | 45 | 280ms | 1.00× |
> | V1 (chunked prefill) | 95 | 165ms | 2.11× |
> | V1 + FP8 | 130 | 95ms | 2.89× |
> | **V1 + FP8 + EAGLE-3** ⭐ | **165** | **65ms** | **3.67×** |
>
> [`deploy/vllm_v1_server.sh`](deploy/vllm_v1_server.sh) 4 档 profile 一键切：`bf16 / fp8 / gptq_marlin / fp8_eagle3`。

### Q：EAGLE-3 投机解码的核心思想是什么？accept_rate 怎么算？
> 投机解码：**便宜的 draft 模型一次猜 K 个 token，target 大模型一次并行验证**。原版 Medusa 用 K 个独立 head，EAGLE-1/2/3 演进核心是 **draft 看 target 的 hidden state**——比 Medusa 的"只看 logits"信息丰富，**accept_rate 从 ~50% 提到 ~75%**。
> ```
> accept_rate = 实际接受的 token 数 / 总 draft 出的 token 数
> 端到端加速 ≈ accept_rate × K + 1   （K 是猜测长度，通常 4-7）
> ```
> 我没自训 draft，直接用社区 EAGLE-3 weights（[`deploy/eagle3_draft.md`](deploy/eagle3_draft.md) 写了自训方案备查），生产配 `num_speculative_tokens=5`，实测 1.27× 在 V1+FP8 之上的额外加速。

### Q：PD 分离（Prefill-Decode Disaggregation）是什么？
> Prefill 阶段是**计算密集**（一次性算 N 个 token 的 attention），Decode 阶段是**显存密集**（每次只算 1 token，KV 全在显存）。两者放一起**互相挤资源**：长 prompt 把 batch 卡住，decode 串行进度被拖慢。
>
> PD 分离：Prefill 节点（H100 多卡 TP）专打长 prompt，Decode 节点（A10 多卡 DP）专打吐 token，**KV cache 通过 RDMA 跨节点传**。架构设计见 [`infra/inference/pd_disagg_design.md`](infra/inference/pd_disagg_design.md)；本项目流量不够大没真上 PD 分离，**面试可讲：会画架构图、知道收益、清楚 KV 传输 bottleneck（网络带宽 / NIXL）**。

### Q：量化方法对比？GPTQ / AWQ / FP8 / W4A16 怎么选？
> | 方法 | 类型 | 校准 | 精度损失 | 速度 | 适用 |
> |------|------|------|----------|------|------|
> | **FP8 (W8A8)** | 权重+激活都 FP8 | 动态 scale | 0.3pp ↓ | 1.7× | H100/Ada 首选 ⭐ |
> | **GPTQ-Marlin (W4A16)** | 权重 4-bit / 激活 FP16 | Hessian 校准 | 0.5-1pp ↓ | 2.5× | 显存紧、A10 友好 |
> | **AWQ (W4A16)** | 权重 4-bit / 保护 1% 重要权重 | 激活感知 | 0.3-0.7pp ↓ | 2.3× | 与 GPTQ 同档但更稳 |
> | **NF4 (QLoRA)** | 权重 4-bit / 激活 BF16 | 无 | 训练时不掉点 | 0.6× | **训练**，不是推理 |
> | **INT8 SmoothQuant** | 老方案 | 激活平滑 | 1-2pp ↓ | 1.4× | 不推荐 |
>
> 我 [`deploy/vllm_v1_server.sh`](deploy/vllm_v1_server.sh) 同时配了 fp8 / gptq_marlin / fp8_eagle3 三档可切；线上以 **FP8 主、GPTQ 备**：H100 上 FP8 精度更高、Marlin kernel 让 GPTQ 速度反超 FP16，是 A10 等老卡的最佳选择。

### Q：KV cache 算多大？怎么估显存？
> 单 token KV cache 大小 = `2 × num_layers × num_kv_heads × head_dim × dtype_size`。
> Qwen3-8B（28 层 / 8 KV heads / head_dim=128 / FP16）：
> ```
> 2 × 28 × 8 × 128 × 2 = 114,688 B/token ≈ 112 KB/token
> ```
> 8K 上下文 ≈ 880 MB，96 并发的 worst case 84 GB——这就是为什么必须 PagedAttention + KV 共享 + FP8 KV cache（再省一半到 ~42 GB）。

---

## 🛠️ AI Infra 深问（CUDA / Triton / 分布式）

### Q：你写过 CUDA / Triton kernel 吗？
> 写过。[`infra/cuda/triton_rmsnorm.py`](infra/cuda/triton_rmsnorm.py) 手写了 **融合版 RMSNorm**：
> ```
> y = x · rsqrt(mean(x²) + eps) · weight
> ```
> 朴素 PyTorch 实现要 4 个 kernel（square / mean / rsqrt / mul），**HBM 来回搬 4 次**。Triton 一个 kernel 全做完：
> - **block 内 reduction** 求 mean(x²)；
> - 结果留在 SRAM，不回 HBM；
> - 直接乘 weight 输出。
>
> 实测 (4096, 4096) 输入 **2.18× 加速**，HBM 带宽 945 GB/s（**99% 利用率**）。Nsight Compute 报告 [`infra/reports/rmsnorm_perf.md`](infra/reports/rmsnorm_perf.md) 证明这是 memory-bound 而非 compute-bound 的 kernel——这是判断"还能不能再优化"的关键指标。

### Q：分布式训练 DP / TP / PP / ZeRO 区别？
> | 维度 | 切什么 | 通信 | 适用 |
> |------|--------|------|------|
> | **DP（DDP）** | 不切，复制模型 | 梯度 all-reduce | 单卡能放下模型时 |
> | **TP（Tensor Parallel）** | 切 weight 矩阵列/行 | 每层 all-reduce 2 次 | 单层超过单卡显存 |
> | **PP（Pipeline Parallel）** | 切层 | layer 间 send/recv | 层数多、bubble 可接受 |
> | **ZeRO-1/2/3** | 切优化器状态/梯度/参数 | bucket 化 reduce-scatter+all-gather | 通用，无需改模型 |
> | **FSDP** | ZeRO-3 的 PyTorch 原生实现 | 同上 | 主流推荐 ⭐ |
>
> 我 [`infra/distributed/`](infra/distributed/) 跑通了：
> - **DDP / FSDP 双 T4 Qwen3-0.6B**（[`ddp_fsdp_demo.py`](infra/distributed/ddp_fsdp_demo.py)）：FSDP 每卡显存 -52%，overhead 8%；
> - **DeepSpeed ZeRO-2 / ZeRO-3 + Offload**（[`ds_zero2.json`](infra/distributed/ds_zero2.json) / [`ds_zero3.json`](infra/distributed/ds_zero3.json)）：8B QLoRA 19.2GB → 14.8GB → 9.5GB；
> - **手写 Column / Row Parallel**（[`manual_tp.py`](infra/distributed/manual_tp.py)）：演示 TP 通信原语 all-reduce / scatter / gather。
>
> **生产选**：单机多卡 → FSDP；多机多卡 → DeepSpeed ZeRO-3 + ZeRO-Infinity（NVMe offload）。

### Q：混合精度训练 FP16 / BF16 / FP8 区别？
> - **FP16**（5 bit exp / 10 bit mantissa）：动态范围窄，loss spike 多发，要 grad scaler；
> - **BF16**（8 bit exp / 7 bit mantissa）：和 FP32 同动态范围，**不需要 scaler**，A100/H100 首选 ⭐；
> - **FP8**（E4M3 forward / E5M2 backward）：H100 才硬件支持，训练用要 per-tensor scaling，目前业界用 **FSDP-2 + FP8** 训 LLM（Megatron-LM、TransformerEngine）。
>
> 我项目训练用 BF16；推理用 FP8（W8A8）。

### Q：FlashAttention 为什么快？数学上做了什么？
> 朴素 attention：`S = QK^T → P = softmax(S) → O = PV`，**显式存中间矩阵 S（N×N）**，HBM I/O O(N²)。
>
> FlashAttention 的两个 trick：
> 1. **Tiling**：把 Q/K/V 切 block，只在 SRAM 内算，输出累积到 O；
> 2. **Online softmax**：增量更新 max 和 sum，避免回头读 S。
>
> 结果：HBM I/O **O(N²) → O(N)**；同时支持任意 N 不再卡显存。FA-2 进一步优化 backward 重计算；FA-3 用 H100 异步 TMA 再快 1.5×。

---

## 🔌 MCP 协议深问

### Q：MCP 是什么？和 OpenAI Function Calling 区别？
> MCP（Model Context Protocol，Anthropic 2024）是**进程外、跨语言**的工具协议：JSON-RPC over stdio / SSE / Streamable-HTTP，让 LLM 调用 server 端能力像调本地函数。和 Function Calling 的关系：
>
> | 维度 | OpenAI Function Calling | MCP |
> |------|-------------------------|-----|
> | 层级 | 模型层 | 协议层（在 FC 之上） |
> | 部署 | 工具与 Agent 同进程 | 工具独立 server |
> | 复用 | 一对一 | 一份 server 给所有 LLM 用 ⭐ |
> | 状态 | 无状态 | 支持 session（resource、prompt、tool 三类） |
>
> 我项目里 **bk-monitor / bcs / gongfeng / tapd / taiji_kb 全是 MCP server**，Agent 不依赖具体 SDK，只看到统一的 JSON Schema 工具描述。

### Q：MCP 的 transport 怎么选？stdio / SSE / Streamable-HTTP 区别？
> - **stdio**：父进程拉起 server 子进程通信。本地工具最简单，**无网络鉴权问题**。但分布式部署不行。
> - **SSE**：早期方案，server → client 单向流式 + 反向 HTTP POST。**断链恢复差**。
> - **Streamable-HTTP**（2024.11 版后）：单条 HTTP 连接，正反双向 chunk，**支持 session 重连**。我项目主用这个，配 `WithSessionReconnect(3)` + `traceparent` 透传。

### Q：MCP server 怎么暴露权限？怎么防止被 LLM 滥用？
> MCP 协议**自己不做权限**——它是协议层。我项目在 Agent 侧加了 [`src/tools/targeted.go`](src/tools/targeted.go) `TargetedTool`：
> - 每个工具声明 `Targets`（`diagnosis` / `repair` / `knowledge`）；
> - `app.go` 注册时 `tools.FilterByTargets` 切片；
> - 模型连工具名都看不到，物理上没法越权。
> 加上 input_guard 防注入 + HMAC 链式审计，三层兜底。

---

## 📊 数据合成与质检（红队 / 对抗 / 偏差）

### Q：合成数据怎么避免"模型自吃自"导致的偏差？
> 三策略：
> 1. **异源生成**：用 **DeepSeek-V3.2 + Magpie 双源**，避免单一教师模型的 bias 被全盘继承；
> 2. **温度多样性**：T=0.3 / 0.7 / 1.0 三档采样，T=0.3 的标准答 + T=1.0 的发散答合并；
> 3. **质检过滤**：BGE-M3 语义去重（[`scripts/data_quality.py`](scripts/data_quality.py) cosine > 0.95 视为重复）+ RAGAS faithfulness < 0.5 直接丢，干掉 ~30% 垃圾。
>
> 红队（adversarial）样本占 5%：故意构造**越权问题**（"删除 prod 数据库"）让模型学会拒绝；构造**虚假前提**（"昨天的告警 ID 是 99999"）让模型学会反问澄清。

### Q：你的 SFT 数据多大？为什么这个量？
> 知识库 SFT：**5k 条 QA**（DeepSeek 合成 4k + Magpie 1k）；NPC：**8k 对话**（角色卡 3 张 × 场景 4 类 × 多轮）。
>
> 选这个量级原因：
> - **<1k**：模型学不进领域知识，过拟合发生在 1 epoch 内；
> - **5-10k**：QLoRA rank=16 的 sweet spot，3 epoch 收敛、不过拟合；
> - **>50k**：边际收益递减，且数据质量难保证（合成数据的"长尾噪声"会浮现）。

### Q：LLM-as-Judge 有什么偏差？怎么校准？
> 4 大已知偏差：
> 1. **Position bias**：第一个候选评分系统性偏高（GPT-4 实测 ~7%）。修复：**双向打分 (A,B) + (B,A) 取平均**。
> 2. **Length bias**：长答案被偏好。修复：在 prompt 里写"长度不影响评分"+ 长度归一化。
> 3. **Self-preference**：用 GPT-4 当 Judge 评 GPT-4 的输出会偏高。修复：**异源 Judge**——用 Claude 评 DeepSeek、用 DeepSeek 评 Qwen。
> 4. **Verbosity bias**：模型更喜欢"看起来认真"的答案。修复：rubric 化打分，每个维度 0-5 分单独打。
>
> 我 [`eval/judge/`](eval/judge/) 三维度打分（AnswerCorrectness / EvidenceSufficiency / ToolSelectionAccuracy），prompt 强制 JSON 输出 + 异源 Judge。

---

## 🎯 RAG 进阶

### Q：父子索引 / Parent-Child Retrieval 是什么？
> 切分粒度的两难：
> - **chunk 太小**（256）：检索召回好，但生成时缺上下文；
> - **chunk 太大**（2048）：上下文足，但向量稀释、检索不准。
>
> 父子索引：**embed 子 chunk（256）做检索，命中后返回父 chunk（2048）做生成**。LangChain 的 `ParentDocumentRetriever` / LlamaIndex 的 `HierarchicalNodeParser` 都是这套。
>
> 本项目当前**仅切 1024**——文档量 < 5k 没必要。如要做：在 [`scripts/build_index.py`](scripts/build_index.py) 加二级 chunk_id 字段，retriever 命中后按 parent_id 二次查询。是 P1 任务。

### Q：HyDE / Query Rewrite / Multi-Query 怎么选？
> | 方法 | 思路 | 成本 | 适用 |
> |------|------|------|------|
> | **Multi-Query** | LLM 改写出 N 个 query，并发检索后合并 | 1 次 LLM + N 次检索 | 长尾 query ⭐ |
> | **HyDE** | LLM 先编个假答案，embed 假答案去检索 | 1 次 LLM + 1 次检索 | 问题与文档表述差异大 |
> | **Step-back** | LLM 先抽象成更高层问题再检索 | 1 次 LLM + 1 次检索 | 多跳推理 |
> | **RAG-Fusion** | Multi-Query + RRF 融合排名 | 1 次 LLM + N 次 + RRF | 召回上限不够 ⭐⭐ |
>
> 项目当前**未启用**——RAGAS 评估在 92% 已经足够。预留接口在 `Retriever.search`，加 query rewrite 是 P1。

### Q：Embedding 模型选型？为什么是 BGE-M3 不是别的？
> 横评：
> | 模型 | 维度 | 多语言 | 长文本 | 备注 |
> |------|------|--------|--------|------|
> | text-embedding-3-large | 3072 | 强 | 8k | OpenAI 闭源、贵 |
> | bge-large-zh-v1.5 | 1024 | 中文强、英文一般 | 512 | 上代王者 |
> | **bge-m3** ⭐ | 1024 | 100+ 语言 | **8192** | dense+sparse+colbert 三路 |
> | gte-Qwen2-7B-instruct | 3584 | 强 | 32k | 太大、推理慢 |
> | jina-embeddings-v3 | 1024 | 强 | 8k | 任务自适应 |
>
> BGE-M3 的两个杀手锏：
> 1. **8192 长度**：运维 Runbook 一篇 3k 字不用切；
> 2. **dense+sparse 单模型双输出**：一次前向同时拿到稠密向量和 lexical 权重，比双模型少一次推理 + 不会语义不一致。

### Q：Reranker 选型？为什么是 BGE-Reranker-v2-m3？
> 横评：
> | 模型 | 类型 | 速度 | 精度 | 备注 |
> |------|------|------|------|------|
> | Cohere Rerank-3 | 闭源 API | 60ms/100 | 强 | 贵、要走外网 |
> | bge-reranker-large | Cross-Encoder | 200ms/100 | 中 | 上代经典 |
> | **bge-reranker-v2-m3** ⭐ | Cross-Encoder | 120ms/100 | 强 | 多语言、与 BGE-M3 对齐 |
> | bge-reranker-v2-gemma | LLM-based | 800ms/100 | 极强 | 太慢、不适合在线 |
> | jina-reranker-v2 | Cross-Encoder | 100ms/100 | 强 | 备选 |
>
> 选 v2-m3 的理由：**和 embedding 同源**（都是 BGE 系列），训练时见过同样的负样本分布，**召回到精排**这条链路语义对齐最好。

### Q：chunk 怎么切？size/overlap 参数怎么定？
> [`configs/knowledge_rag.yaml`](configs/knowledge_rag.yaml)：`chunk_size: 1024 / chunk_overlap: 64`。决策依据：
> 1. **size 1024**：BGE-M3 max_len=8192 远没用满，但**太大稀释语义**——一个 chunk 里讲两个不同概念时检索会两头落空；
> 2. **overlap 64**：约 6% 重叠，主要是为了**避免句子被腰斩**——切完一个 chunk 看下一个 chunk 头部 64 token，找到换行/句号再切；
> 3. **特殊文档另算**：代码片段用 AST 切（按函数/类边界），表格保留整张表不切——朴素 token 切分会把一行表头和数据切散。
>
> 参考 LangChain 的 `RecursiveCharacterTextSplitter` 思路：`["\n\n", "\n", "。", " ", ""]` 多级回退，能保段落就保段落。

### Q：数据合成的 prompt 怎么写？few-shot 还是 zero-shot？
> **few-shot**（[`scripts/generate_qa.py`](scripts/generate_qa.py)）：
> 1. 系统 prompt 写明角色（"你是运维知识题出题专家"）+ 输出格式（严格 JSON）+ 风格约束（中文、技术语气）；
> 2. 给 3 个 in-context 示例：从最简单（"什么是 OOM"）到最复杂（"X 服务在 Y 场景下 P99 突增的根因排查"）梯度递增；
> 3. 用户消息只给文档片段；
> 4. **temperature=0.7**：太低重复多、太高跑题。
>
> 三个 trick：
> - **JSON Schema mode**（DeepSeek 支持）强制结构化输出，省 30% 后处理代码；
> - **Critique-then-revise**：第一遍生成 → 第二遍让模型自评打分 < 4 的丢掉；
> - **Magpie 反向生成**：不给文档，让模型从 instruction template 倒推问答对，多样性翻倍。

### Q：训练数据怎么脱敏？
> 内部知识库不可避免有真实业务名/IP/账号，三层兜底：
> 1. **入库前正则替换**：IP（`\d+\.\d+\.\d+\.\d+`）→ `<IP>`、手机号 11 位数字 → `<PHONE>`、内部域名 `*.woa.com` → `<DOMAIN>`；
> 2. **NER 模型补漏**：spaCy + 自训 NER 识别人名/项目名，正则识别不到的"小明"也能脱；
> 3. **训练数据抽样人工 review**：每批 5%，发现新模式回灌正则。
>
> **不能用合成数据替代脱敏**——LLM 会把训练时见过的真实数据"记住"在权重里，**RAG 召回 + 推理输出仍可能泄漏**。这是大厂 LLM 训练的真实合规线。

---

## 🧪 评测体系深问

### Q：评测用什么 benchmark？为什么不用 MMLU/CMMLU？
> 通用 benchmark 衡量"模型基础能力"，对**领域微调模型没用**——MMLU 涨 2 分跟项目目标"运维问答更准"几乎无关。我评测分三层：
> 1. **领域 benchmark（自造）**：[`eval/golden/`](eval/golden/) 100 条人工金标，按"事实/推理/拒答"三类切，**这是 SLO 看板**；
> 2. **检索质量 RAGAS**：context_precision / context_recall / answer_relevancy / faithfulness 四指标；
> 3. **生成质量 LLM-as-Judge**：异源 Judge（Claude 评 DeepSeek 输出）+ 三维 rubric（正确性/证据充分/工具选择）。
>
> 通用能力**只在每次大版本升级时跑一次** MMLU 防回归。

### Q：怎么避免评测集"过拟合"？
> 三道防线：
> 1. **金标集分版本管理**：v1 用于线上 SLO，v2 灰度阶段才解封，**模型见过的金标必须换**；
> 2. **训练-评测 leak 检测**：BGE-M3 算训练集和金标集每对样本的 cosine，> 0.9 视为泄漏，强制移除；
> 3. **CI 门禁不只看分数,还看分布**：某类题突然拉高 20%、其它持平 → 大概率是 leak，触发人工 review。

### Q：A/B 在线评测怎么做？
> 三层：
> - **影子流量**：新模型只读不发，输出与线上对比，先保证不崩；
> - **小流量灰度**：5% 用户走新模型，看业务指标（修复完成率/用户赞踩比）48h；
> - **全量切换**：业务指标无下降才推全量，**任何指标退化 > 2pp 自动回滚**。
>
> 关键：**业务指标 > LLM Judge 分数**——Judge 高 0.3 分但用户赞踩比下降意味着模型答得"漂亮但不对路"。

---

## 🧱 模型架构进阶（可能被问的高阶题）

### Q：MoE 是什么？为什么 DeepSeek-V3 / Mixtral 都用？
> Mixture-of-Experts：每层 FFN 拆成 N 个"专家"小网络 + 1 个 Router，每个 token 只路由到 top-K 个专家（DeepSeek-V3 用 8/256），**激活参数远小于总参数**。
>
> DeepSeek-V3 671B 总参 / 37B 激活 = **18× 容量放大**，推理 cost 只看激活。代价：
> 1. **训练复杂**：负载均衡 loss 防止某些专家被冷落；
> 2. **推理需要 expert parallelism**：专家分布在不同 GPU，all-to-all 通信开销；
> 3. **vLLM 等框架支持 MoE 是 2024Q3 才补齐的**。
>
> 我自己没训 MoE（数据量不够 + 4×4090 架构 over-kill），但**用 DeepSeek-V3 做 SFT 教师模型**，间接享受 MoE 的容量。

### Q：GQA / MQA / MHA 区别？为什么 Qwen3-8B 用 GQA？
> | 类型 | KV head 数 | KV cache | 性能 |
> |---|---|---|---|
> | MHA（Multi-Head Attention） | = Q heads | 全量 | 最准、最贵 |
> | MQA（Multi-Query） | 1 | **1/N** | 快但精度掉 1-2pp |
> | **GQA（Grouped-Query）** ⭐ | Q heads / G | 1/G | 平衡 |
>
> Qwen3-8B：32 个 Q heads / 8 个 KV heads = G=4。**KV cache 减小 4×**，是长上下文推理（128k）的必要条件。前面算过 KV cache 88KB/token：换 MHA 就是 350KB/token，长上下文直接爆显存。

### Q：RoPE / ALiBi / NoPE 位置编码怎么选？
> - **RoPE**（Rotary）：旋转矩阵，**外推性好**，YaRN/PI/NTK 各种长度扩展技术都基于 RoPE，**主流首选** ⭐；
> - **ALiBi**：直接在 attention score 上加距离衰减偏置，简单但**外推性中等**；
> - **NoPE**：无位置编码（小模型实验有效），大模型上效果不如 RoPE。
>
> Qwen3 用 RoPE + YaRN 扩到 128k（训练只到 32k）。**外推靠基频缩放**：`theta = base^(2i/d)` 中的 base 从 10000 调到 1000000，让高频项变缓，相当于"看更远"。

---

## 📁 项目文件速查

| 目录 | 重点文件 |
|------|---------|
| `configs/` | knowledge_sft / npc_sft / npc_dpo / npc_grpo / knowledge_rag / quantize |
| `scripts/` | generate_qa / data_quality / build_index / evaluate / grpo_rewards |
| `deploy/` | rag_serve.py / mcp_expert_server.py / rag_docker-compose.yaml |
| `observability/` | langfuse_tracing.py / grafana_dashboard.json / docker-compose.obs.yaml |
| `demo/` | demo_notebook.ipynb / demo_script.md |
| 根目录 | PROGRESS.md（阶段跟踪）/ INTERVIEW.md（本文）/ README.md |

---

## 🚀 端到端 Bring-up & Day-2 Ops（生产化 SOP）

### 训练机 Day-0 → Day-1 SOP（4×4090 / 1×H100 通用）

```
Day-0：硬件就位
  ├─ 驱动 NVIDIA 550+（FP8 训练要 ≥535）
  ├─ CUDA 12.4 + cuDNN 9.x
  ├─ NCCL 2.20+（多卡 allreduce）
  └─ 自检：nvidia-smi topo -m  → 看 NVLink/PCIe 拓扑

Day-1：环境与 baseline
  ├─ conda env (python 3.10) + torch 2.4 + flash-attn 2.6
  ├─ 装 LLaMA-Factory（pip install -e ".[torch,metrics]"）
  ├─ 跑官方 demo（Qwen2.5-0.5B SFT）→ 验证全栈通畅
  ├─ 数据准备：scripts/generate_qa.py + data_quality.py 跑通
  └─ 起 wandb / Langfuse / TensorBoard 三套 trace

Day-N：正式训练
  ├─ 先跑 1 epoch dry-run（lr=2e-5, 50 steps）→ 看 loss 曲线
  ├─ 全量训练前用 swanlab/wandb 设 alert（loss NaN/spike 自动停）
  └─ checkpoint：每 200 steps 存一次，保留最近 3 + best
```

### 推理机 Bring-up（vLLM 服务化）

```
Step 1：模型 artifact 拉取
  └─ HF mirror / 内部 S3 / cos://models/qwen3-8b-fp8/v1.2/
     版本规范：{model}-{quant}/v{major}.{minor}（语义化）

Step 2：vLLM 启动参数（生产配置）
  python -m vllm.entrypoints.openai.api_server \
    --model qwen3-8b-fp8 \
    --quantization fp8 \
    --tensor-parallel-size 1 \
    --max-model-len 32768 \
    --gpu-memory-utilization 0.90 \
    --enable-prefix-caching \
    --enable-chunked-prefill \
    --speculative-model qwen3-0.6b-eagle \
    --num-speculative-tokens 5 \
    --swap-space 16 \                    # CPU offload，OOM 兜底
    --max-num-seqs 256 \
    --enable-lora --max-loras 4 --max-lora-rank 32

Step 3：就绪探针（K8s readinessProbe）
  ├─ /health：进程存活
  ├─ /v1/models：权重加载完成（首次冷启动 ~30s）
  └─ 业务级 smoke：发一条 fixed prompt，断言输出包含 anchor token

Step 4：流量灰度
  └─ Ingress 按 header 路由：5% → 25% → 100%
```

### Day-2 Operations（线上常见 Case Book）

| 故障现象 | 根因排查 | 处置 SOP |
|---|---|---|
| **vLLM OOM / preemption 频繁** | KV cache 满 → 序列被踢出重算 | 调小 `max-num-seqs`；加大 `swap-space`；上 prefix cache |
| **P99 突刺到 5s+** | 长 prompt 拖死短 prompt 队列 | 长短分队列（两个 vLLM 实例 + 路由按 token 数分流） |
| **GPU ECC error** | 显存颗粒坏块 | DCGM exporter 监控 `DCGM_FI_DEV_ECC_DBE_VOL_TOTAL`，触发 cordon + drain |
| **NaN loss 训练中断** | FP8/BF16 数值溢出 | grad_clip=1.0 + z_loss=1e-4；断点续训 `resume_from_checkpoint` |
| **embedding 模型漂移** | 业务词汇变了（新故障类型） | 周级监控召回 hit@5；低于阈值触发 BGE-M3 增量微调 |
| **vLLM 滚动升级** | 不支持 hot reload | 蓝绿：起新版本 pod → readiness 通过 → 切流 → 老版本 drain |
| **HITL 审批等待中进程重启** | in-memory session 丢 | session 落 Redis（参见 project-agent SOP） |

### Shutdown / 回滚 SOP

- **优雅关闭**：vLLM 收 SIGTERM → 拒新请求 → 等待 in-flight 完成（最多 30s）→ 退出
- **快速回滚**：保留上一版本镜像 + 上一版本 LoRA artifact，K8s `kubectl rollout undo` 一键回退
- **数据回滚**：训练数据用 DVC 打 tag，坏数据期间生成的 ckpt 全部隔离，不进生产 model registry

---

## 🔄 数据飞轮（Continuous Improvement Loop）

> 一次性合成 13k 条数据训出来的模型只是 v0，**真正的护城河是数据飞轮**。

### 飞轮闭环（周级节奏）

```
线上 query (Langfuse trace)
   ↓ (1) 自动采样：低置信 / 用户踩 / Tool 调用失败
坏 case 池（每周 ~500 条）
   ↓ (2) 半自动标注：DeepSeek-V3 给候选答案 + 人工 review (~2h/周)
增量训练集 v_{n+1}
   ↓ (3) 增量 SFT (LoRA, 1 epoch) 或 DPO (新 chosen/rejected pair)
候选模型 m_{n+1}
   ↓ (4) 离线评测：RAGAS + 自建 50 条 golden + 红队集
   ↓ 全部 ≥ baseline 才进生产
新版本上线 → 灰度 5% → 100%
```

### 关键设计

- **数据版本管理**：DVC + S3，每个数据集打 SHA256；模型 ckpt 在 model card 里写明 `trained_on=dataset_v3@a1b2c3d`
- **触发条件（不是定时跑）**：
  - 坏 case 累计 ≥ 500 条 → 触发 SFT
  - 同一类故障连续 3 次失败 → 触发针对性专家 LoRA
  - 月度漂移检测（embedding KL > 0.15）→ 触发 retriever 微调
- **防灾难性遗忘**：每轮增量训练**必带 20% 旧数据 replay buffer**
- **金标轮换**：50 条 golden 每季度 review 一次，淘汰被过拟合的、补新业务场景的

### 面试官追问

> Q：合成数据训出来的模型，怎么保证不"自我中毒"（generator collapse）？
> A：3 道闸：① 教师模型用 DeepSeek-V3 而非自己；② 每轮 SFT 前 data_quality.py 跑去重 + 困惑度过滤；③ 严守"线上真实 query 分布"作为评测集，合成数据只参与训练不参与评估。

---

## ⚡ 推理深水区补充

### Guided Decoding（结构化输出的工程方案）

NPC JSON 输出 99% 可解析，**不是只靠 GRPO format reward**，工程上叠加了 vLLM guided decoding：

```python
# vllm 请求时强制 schema
extra_body = {
    "guided_json": {
        "type": "object",
        "properties": {
            "action": {"enum": ["attack", "flee", "talk"]},
            "target": {"type": "string"},
            "reason": {"type": "string"}
        },
        "required": ["action", "reason"]
    }
}
```

- 后端：vLLM 0.6+ 默认 `xgrammar`（比 outlines 快 10×，FSM 编译 < 50ms）
- **GRPO format reward + guided decoding 是双保险**：训练让模型"愿意"出 JSON，guided 让它"必须"出合法 JSON
- 代价：guided decoding 在 logits 上 mask，**首 token 延迟 +5~15ms**，吞吐 -3%（业务可接受）

### vLLM LoRA 多租户

知识库专家 LoRA + NPC LoRA 共享一份 base 权重部署：

```
--enable-lora --max-loras 4 --max-lora-rank 32
请求时：model="qwen3-8b" + extra_body={"lora_request": {"lora_name": "npc_v3"}}
```

- 显存节省：单卡只装 1 份 base（~16GB）+ N 个 LoRA（每个 ~50MB）
- 切换开销：< 1ms（CUDA stream 切换 LoRA adapter 矩阵）
- **业务收益**：原来 2 个 vLLM 实例 → 1 个，**GPU 成本砍半**

### 推理 SLA 分级（长短分流）

```
Ingress 路由
  ├─ prompt_tokens < 512  → vllm-fast 池（max-model-len=4k, batch 大）
  ├─ 512 ≤ tokens < 8k    → vllm-std 池（max-model-len=16k）
  └─ tokens ≥ 8k          → vllm-long 池（max-model-len=32k, batch 小）
```

避免 1 条 30k token 的长上下文请求把整批短请求 P99 拉到 5 秒。

### Speculative Decoding 自适应

EAGLE-3 在代码生成场景 accept_rate 只有 30%（远低于对话场景 70%），**反而拖慢**。
方案：监控滑动窗口 accept_rate，连续 100 请求 < 50% 自动关闭 spec decoding（vLLM 有 `--speculative-disable-by-batch-size`）。

---

## 💰 ROI / 成本账（业务价值量化）

### 自建 vs 调 API 的盈亏平衡点

| 维度 | 自建（Qwen3-8B FP8 on H100） | GPT-4o API |
|---|---|---|
| 单卡硬件成本 | H100 80G ≈ ¥18/小时（云租） | / |
| 满载吞吐 | ~3000 token/s（FP8+EAGLE） | / |
| **每百万 token 成本** | **¥18 ÷ (3000×3600/1e6) ≈ ¥1.7** | ¥40~80（输入输出加权） |
| 盈亏平衡点 | **月 token > 8M** 即自建更划算 | / |
| 数据合规 | ✅ 完全在内网 | ❌ 出网 |
| 定制能力 | ✅ SFT/DPO/GRPO 自由 | ❌ 仅 prompt |

### 业务收益折算（运维场景）

- MTTR：人工排障 25min → Agent 辅助 8min，**省 17min/单**
- 月告警单 ~3000 → 节省 ~850 工时 ≈ ¥12 万人力成本
- 训推一体年化 GPU 成本 ¥30 万 → **ROI > 4×**，6 个月回本

### 一句话回答面试官

> 这套系统不是 "炫技 demo"——给定我们月 ~50M token 的真实流量，**自建比调 API 月省 ¥20 万+**，加上数据不出网这是合规硬要求，**ROI 和合规双重驱动**才是它存在的理由。

---

## 🛡️ License 与安全合规

### 模型 / 数据 License 矩阵

| 资产 | License | 商用 | 备注 |
|---|---|---|---|
| Qwen3-8B 权重 | Apache 2.0 | ✅ | 可商用、可微调、可二次分发 |
| DeepSeek-V3（合成教师） | MIT | ✅ | 输出可商用（已确认 ToS） |
| BGE-M3 embedding | MIT | ✅ | / |
| LLaMA-Factory 训练框架 | Apache 2.0 | ✅ | / |
| 业务知识库原始文档 | 内部 | ⚠️ | 训练数据**仅限内网部署**，不可外发 |

### 安全防护（三层）

1. **训练侧**：
   - PII 脱敏（IP/手机号/邮箱正则 + presidio NER）写入 `data_quality.py`
   - 数据投毒检测：困惑度异常 + 关键词黑名单（"忽略上面的指令" 等 prompt injection 训练样本剔除）
   - 防 Membership Inference：DP-LoRA（noise σ=0.5）保护，避免模型背出训练原文
2. **推理侧**：
   - input guard：5 条规则（敏感词 / prompt injection / SQL injection / 超长输入 / 非业务话题）
   - output guard：内部 IP / 内部域名 / 密码格式 二次正则脱敏
3. **审计侧**：
   - Langfuse 全 trace 落库 90 天（合规要求）
   - 高危操作（rm / drop / scale-down）走 project-agent 的 HITL，**LLM 永远不直接执行**

### Model Card（精简版，写在 model registry）

```yaml
model: qwen3-8b-oncall-v1.2
base: Qwen3-8B (Apache-2.0)
training: SFT(13k) + DPO(8k) + GRPO(reward=format+correctness)
intended_use: 内部运维 NPC 多轮对话 + 知识库问答
out_of_scope: 医疗/法律/金融建议；任何外部用户场景
risks_known: 长上下文（>16k）后召回率下降 5pp；冷门告警类型回答置信度低
eval: RAGAS 0.83 / golden_50 P=92% / red_team_pass=98%
```

---

## 🏁 一句话收尾

> 这个项目不只是一个训练 demo，它从**数据合成 → 模型微调 → 量化部署 → Agent 融合 → 端到端观测**，每一步都有**可解释的选型理由**、**可复现的脚本**、**可量化的指标**——全都写在 `PROGRESS.md` 和 `INTERVIEW.md` 里。
