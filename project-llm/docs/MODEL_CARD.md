# Model Card — project-llm-qwen3-8b-npc

> 基于 Qwen3-8B 在 NPC 对话 + 运维知识两个领域微调的衍生模型。
> 本卡参考 [Hugging Face Model Card 模板](https://huggingface.co/docs/hub/model-cards) + Google Model Cards 论文。

## 1. 基本信息

| 项 | 值 |
|---|---|
| 模型名称 | `project-llm-qwen3-8b-npc` |
| 基座模型 | Qwen3-8B（[原始许可](https://huggingface.co/Qwen/Qwen3-8B)） |
| 参数量 | 8.2B |
| 上下文长度 | 32K（YaRN 扩展可至 128K） |
| 训练方法 | SFT(LoRA r=64) → DPO → GRPO（仅 NPC 域） |
| 量化版本 | FP8 / AWQ-W4A16 / GPTQ-Marlin-W4A16 |
| 发布日期 | 2026-04 |
| 维护者 | project-llm authors |

## 2. 预期用途

### ✅ 适合的场景
- **游戏 NPC 对话**：剧情对话、任务引导、角色扮演（中文为主）
- **游戏运维知识 QA**：服务器异常、配置项查询、SOP 检索（配合 RAG）
- **内部技术问答助手**：基于 iWiki 的 RAG QA

### ❌ 不适合的场景
- 医疗、法律、金融等高风险决策场景
- 长链路数学推理（推荐 DeepSeek-R1 / Qwen3-Math 系列）
- 代码生成主任务（推荐 Qwen3-Coder / Code-32B）
- 任何需要"模型自主执行外部操作"的场景（请配合 Agent 框架 + HITL）

## 3. 训练数据

详见 [DATASET_CARD.md](DATASET_CARD.md)。

| 阶段 | 样本量 | 来源 | 占比 |
|---|---|---|---|
| SFT | 28k | 合成 18k + 公开 SFT 子集 10k | 100% |
| DPO | 4k 对 | DeepSeek-V3 评分构造偏好对 | 100% |
| GRPO | 1.2k prompts | NPC 角色一致性 reward | 100% |

数据已做：
- 完全去除个人信息（PII regex + Presidio 二次过滤）
- 完全去除有毒内容（OpenAI moderation + 中文敏感词词典）
- 与基座模型预训练数据交叉去重（simhash）

## 4. 评测结果（v1.0）

| 维度 | 指标 | 基座 Qwen3-8B | 本模型 | Δ |
|---|---|---|---|---|
| **NPC 角色一致性** | LLM-Judge ≥4 / 5 | 71.2% | **86.4%** | +15.2 |
| **NPC 多轮连贯性** | LLM-Judge ≥4 / 5 | 68.5% | **82.1%** | +13.6 |
| **运维 QA EM** | Exact Match | 41.0% | **52.3%** | +11.3 |
| **运维 QA F1** | Token F1 | 56.7% | **66.9%** | +10.2 |
| **RAG Faithfulness** | RAGAS | 0.78 | **0.89** | +0.11 |
| **MMLU 通用能力** | 5-shot | 70.1 | 69.3 | -0.8（容忍范围） |
| **CMMLU 中文** | 0-shot | 76.4 | 75.9 | -0.5 |
| **GSM8K 数学** | 8-shot | 78.0 | 76.5 | -1.5（专精损失，符合预期） |

完整评测脚本：`make eval`，金标数据：[eval/golden_50.jsonl](../eval/golden_50.jsonl)。

## 5. 偏差、风险与限制

| 风险 | 现象 | 控制措施 |
|---|---|---|
| 角色越界 | NPC 输出超出世界观（提及现实政治/品牌） | system prompt 强约束 + RAGAS faithfulness ≥0.85 阈值熔断 |
| 注入攻击 | "忽略以上指令"模式 | OutputGuard + InputGuard 拦截 + GRPO 加入对抗样本 |
| PII 泄露 | 训练数据残留邮箱/手机 | 数据预处理 + 推理时 OutputGuard 正则 |
| 幻觉 | 编造 SOP 或集群信息 | 强制 RAG（top_k=5 + reranker），RAGAS Faithfulness < 0.8 拒答 |
| 文化偏见 | 中文为主，英文/方言能力下降 | 仅承诺中文场景；其他语言降级到基座 |

## 6. 性能与成本

| 部署形态 | 硬件 | 吞吐 | P95 延迟 | 月成本估算 |
|---|---|---|---|---|
| **A10×2 + AWQ-W4A16 + EAGLE-3** | 2×A10 24G | ~520 tok/s | 280ms | ~¥6k |
| **L20×1 + AWQ-W4A16** | 1×L20 48G | ~340 tok/s | 320ms | ~¥4k |
| **H100×1 + FP8 + EAGLE-3** | 1×H100 80G | ~1800 tok/s | 95ms | ~¥35k |
| **端侧（ExecuTorch + INT4）** | iPhone 15 Pro | ~25 tok/s | 首 token 1.2s | 0 |

详细 benchmark：`make bench-speculative`。

## 7. 责任与使用授权

- License：Apache-2.0（衍生权重需遵守 Qwen3 原始许可）
- 引用本模型时请同时引用 Qwen3 论文
- 禁止用于：违反目标法域法律法规的场景；制造/传播虚假信息；针对特定群体的歧视性应用

## 8. 联系方式

- 维护者：project-llm-team
- 漏洞与安全：security@example.com
- 反馈/追问：通过 issue 提交
