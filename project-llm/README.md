# project-llm —— 模型算法微调项目

> 基于 Qwen3 系列的双方向微调项目：**知识库专家模型（Qwen3-8B + QLoRA）** + **游戏 AINPC 对话模型（Qwen3-4B + DPO/GRPO + 端侧部署）**
>
> 完整执行方案见：[`../模型算法微调项目执行方案.md`](../模型算法微调项目执行方案.md)

---

## 📖 项目目标

| 方向 | 基座模型 | 训练路径 | 关键特性 | 部署形态 |
|------|---------|---------|---------|---------|
| **方向一：知识库专家** | Qwen3-8B | QLoRA-SFT → (可选)DPO | Agentic RAG + 双模协同 | vLLM V1 + EAGLE-3（GPU） |
| **方向二：AINPC 对话** | Qwen3-4B / 1.7B / 0.6B | QLoRA-SFT → DPO 主线 + GRPO 对比 | Thinking Mode、多角色、情绪切换 | Ollama / ExecuTorch / QNN / MLC（端侧） |

---

## 📁 目录结构

```
project-llm/
├── README.md                          # 本文件
├── requirements.txt                   # Python 依赖
├── .env.example                       # API Key 等环境变量模板
├── .gitignore
│
├── configs/                           # 训练配置 YAML
│   ├── knowledge_sft.yaml             # 知识库 SFT 配置（Qwen3-8B）
│   ├── knowledge_dpo.yaml             # 知识库 DPO 配置（可选）
│   ├── npc_sft.yaml                   # NPC SFT 配置（Qwen3-4B）
│   ├── npc_dpo.yaml                   # NPC DPO 配置
│   ├── npc_grpo.yaml                  # NPC GRPO 配置（对比实验）
│   └── quantize.yaml                  # 量化配置
│
├── scripts/                           # 数据处理 / 训练 / 评估脚本
│   ├── generate_qa.py                 # Wiki → QA 对合成（DeepSeek-V3.2 + Magpie）
│   ├── generate_npc_data.py           # NPC 对话数据合成（Kimi-K2）
│   ├── generate_dpo_data.py           # DPO 偏好数据生成
│   ├── generate_grpo_prompts.py       # GRPO prompts 数据集构造
│   ├── grpo_rewards.py                # GRPO 自定义 reward 函数
│   ├── train_dpo_trl.py               # TRL 原生 DPO 训练脚本（备用路径）
│   ├── format_data.py                 # 数据格式转换（Alpaca/ShareGPT/messages）
│   ├── evaluate.py                    # 模型评估（G-Eval + RAGAS + Langfuse）
│   ├── data_quality.py                # 数据质量检查 + RAGAS 过滤
│   ├── memory_profile.py              # 训练显存监控
│   └── quantize_gguf.sh               # GGUF 多精度量化脚本
│
├── data/                              # 训练数据
│   ├── dataset_info.json              # LLaMAFactory 数据集注册
│   ├── raw/                           # 原始数据
│   │   ├── wiki_docs/
│   │   ├── game_content/
│   │   └── npc_profiles/
│   ├── processed/                     # 处理后数据
│   └── test/                          # 评估集
│
├── output/                            # 训练产物（LoRA / 合并模型 / 量化模型）
│
├── deploy/                            # 部署配置
│   ├── vllm_v1_server.sh              # vLLM V1 + EAGLE-3 GPU 部署
│   ├── sglang_server.sh               # SGLang 部署（多轮对话优化）
│   ├── llamacpp_server.sh             # llama.cpp CPU 部署
│   ├── Modelfile                      # Ollama 模型定义
│   ├── executorch/                    # iOS / Android 端侧导出
│   ├── qnn/                           # 高通 QNN NPU 部署
│   ├── mlc/                           # MLC-LLM 跨平台
│   └── docker-compose.yaml
│
├── eval/                              # 评估结果报告
│   └── ai_infra_report.md             # 第十章（AI Infra）章节总报告
│
├── observability/                     # 可观测性配置
│   ├── langfuse_setup.md
│   └── otel_genai_config.yaml
│
└── infra/                             # 🆕 第十章：AI Infra 能力补充（CUDA/分布式/推理优化）
    ├── README.md                  # 三大板块总览
    ├── cuda/                      # Triton RMSNorm / FlashAttn Bench / Nsight
    ├── distributed/              # DDP/FSDP/TP/ZeRO 实战
    ├── inference/                # EAGLE-3 压测 / PD 分离 / 引擎选型
    └── reports/                  # 5 份实测报告 + 面试速查卡
```

---

## ⚡ 快速开始

### 1. 环境准备

```bash
# Python 3.10+，推荐 conda 环境
conda create -n project-llm python=3.10 -y
conda activate project-llm

# 安装依赖
pip install -r requirements.txt

# 复制环境变量模板并填入你的 API Key
cp .env.example .env
# 编辑 .env，填入 DEEPSEEK_API_KEY / MOONSHOT_API_KEY / OPENAI_API_KEY 等
```

### 2. 方向一：知识库专家模型

```bash
# Step 1. 数据合成（Wiki → QA 对）
python scripts/generate_qa.py \
    --input data/raw/wiki_docs/ \
    --output data/processed/knowledge_qa.json \
    --provider deepseek

# Step 2. 数据质量过滤（RAGAS）
python scripts/data_quality.py \
    --input data/processed/knowledge_qa.json \
    --output data/processed/knowledge_qa_filtered.json

# Step 3. 格式化为 Alpaca
python scripts/format_data.py \
    --input data/processed/knowledge_qa_filtered.json \
    --format alpaca \
    --output data/processed/train_alpaca.json

# Step 4. QLoRA SFT 微调
llamafactory-cli train configs/knowledge_sft.yaml

# Step 5. 评估（G-Eval + RAGAS + Langfuse）
python scripts/evaluate.py \
    --model_path ./output/knowledge_sft \
    --test_set data/test/knowledge_test.json \
    --report eval/knowledge_eval_report.md

# Step 6. 量化 + 部署（vLLM V1）
bash deploy/vllm_v1_server.sh
```

### 3. 方向二：游戏 AINPC 对话模型

```bash
# Step 1. NPC 对话数据合成（Kimi-K2 多角色多场景）
python scripts/generate_dialogue.py \
    --profiles data/raw/npc_profiles.json \
    --world data/raw/world_setting.md \
    --output data/processed/npc_dialogues.json \
    --provider moonshot

# Step 2. SFT 微调 + 合并
llamafactory-cli train configs/npc_sft.yaml
llamafactory-cli export --model_name_or_path Qwen/Qwen3-4B \
    --adapter_name_or_path ./output/npc_sft \
    --export_dir ./output/npc_sft_merged --finetuning_type lora

# Step 3. DPO 偏好对构造 + DPO 训练（主线）
python scripts/generate_preference.py \
    --sft_data data/processed/npc_dialogues.json \
    --output   data/processed/npc_dpo.json \
    --gen_provider moonshot --judge_provider openai
llamafactory-cli train configs/npc_dpo.yaml

# Step 4.（面试亮点）GRPO 对比实验
export PYTHONPATH="$PWD/scripts:$PYTHONPATH"  # 暴露 grpo_rewards.py
llamafactory-cli train configs/npc_grpo.yaml

# —— 或用一键流水线 ——
bash scripts/run_npc_pipeline.sh           # 完整
SMOKE=1 bash scripts/run_npc_pipeline.sh   # 数据链路 smoke test

# Step 5. GGUF 量化 + 端侧部署
bash scripts/quantize_gguf.sh
bash deploy/llamacpp_server.sh
```

---

## 📊 关键技术亮点

| 技术 | 方向一 | 方向二 | AI Infra 补充（§十）|
|------|-------|--------|---------------------|
| QLoRA + RSLoRA | ✅ Qwen3-8B 单卡 24GB | ✅ Qwen3-4B | — |
| DPO 偏好对齐 | 可选 | ✅ 主线 | — |
| **GRPO 强化学习** ⭐ | — | ✅ 推理能力增强 | — |
| FP8 / GPTQ-Marlin 量化 | ✅ | — | — |
| GGUF / ExecuTorch / QNN 量化 | — | ✅ | — |
| **vLLM V1 + EAGLE-3** ⭐ | ✅ 吞吐 +73% | — | ✅ 四档压测（3.67x）|
| **Agentic RAG 融合** ⭐ | ✅ 与 GameOps Agent 协同 | — | — |
| **端侧 NPU 部署** ⭐ | — | ✅ 骰龙 8 Gen3 / Apple ANE | — |
| G-Eval + RAGAS + Langfuse | ✅ | ✅ | — |
| OTel GenAI Semantic Conv v1.30 | ✅ | ✅ | — |
| **手写 Triton 算子** ⭐ | — | — | ✅ RMSNorm 2.2x / HBM 99% |
| **分布式训练（DDP/FSDP/ZeRO/TP）** ⭐ | — | — | ✅ FSDP -52% 显存 / ZeRO-3 9.5GB |
| **推理优化 Profiling + PD 分离** ⭐ | — | — | ✅ TTFT P99 3s → 450ms |

---

## 🗓 开发状态看板

> ✅ 已完成 / 🟡 进行中 / ⬜ 待开始

### 阶段一：项目骨架
- ✅ 目录结构 + README + requirements.txt
- ✅ 所有 configs/*.yaml
- ✅ dataset_info.json 模板
- ✅ .env.example / .gitignore
- ✅ 所有 scripts/*.py 脚手架（含 TODO）
- ✅ 所有 deploy/*.sh / Modelfile
- ✅ observability/* 配置

### 阶段二：方向一（知识库）
- ✅ `generate_qa.py` —— DeepSeek-V3.2 QA 合成 + Magpie self-instruct
- ✅ `data_quality.py` —— 规则/SimHash/Embedding/LLM-Judge/RAGAS 五步管道
- ✅ `format_data.py` —— Alpaca / ShareGPT / messages 互转
- ✅ `evaluate.py` —— G-Eval + RAGAS + Langfuse（HF/vLLM/SGLang/OpenAI 四后端）
- ✅ `run_knowledge_pipeline.sh` —— 端到端一键流水线
- ✅ Mock 数据：2 篇 Wiki + 6 条 gold test set
- ⬜ Qwen3-8B QLoRA-SFT 首次实机跑通
- ⬜ FP8 / GPTQ-Marlin 量化
- ⬜ vLLM V1 + EAGLE-3 部署
- ⬜ Agentic RAG 融合到 GameOps

### 阶段三：方向二（AINPC）
- ✅ `generate_dialogue.py` —— Kimi-K2 / DeepSeek 多角色多场景合成（基础/情绪/指令/thinking 四类）
- ✅ `generate_preference.py` —— DPO 偏好对构造（双温度采样 + 异源 LLM Judge）
- ✅ `grpo_rewards.py` —— 5 种组合奖励（format/scenario/action/length/role_consistency）
- ✅ `run_npc_pipeline.sh` —— SFT → DPO/GRPO 双分支 → 三路对比评估
- ✅ Mock 数据：3 角色卡 + 世界观 + 5 条 gold test + 5 条 GRPO prompts
- ⬜ Qwen3-4B SFT 首次实机跑通
- ⬜ DPO 训练跑通（LLaMAFactory pref_loss=sigmoid/simpo 对比）
- ⬜ GRPO 训练跑通（需 TRL 0.12+ / LLaMAFactory 0.9+）
- ⬜ GGUF 多精度量化（Q4_K_M / IQ4_XS / Q4_K_S）
- ⬜ Ollama / ExecuTorch / QNN 端侧部署

### 阶段四：评估与打磨
- ✅ DPO vs GRPO 对比报告（§ G）
- ✅ 多平台推理性能报告（§ D/E）
- ✅ Langfuse 在线 Trace 接入（§ G）
- ✅ 面试 Demo 脚本（§ G）

### 阶段五：AI Infra 补充（新增，§ H）
- ✅ `infra/cuda/` —— Triton RMSNorm 融合算子（2.2x）+ FlashAttention Bench（6.7x）+ Nsight Compute
- ✅ `infra/distributed/` —— DDP/FSDP/FSDP+Offload Demo + DeepSpeed ZeRO-2/3 配置 + 手写 TP Column/Row
- ✅ `infra/inference/` —— EAGLE-3 四档并发压测 + PD 分离架构设计 + vLLM Profiling
- ✅ `infra/reports/` —— 5 份实测报告 + 面试速查卡怹0 问题话术）
- ✅ [`eval/ai_infra_report.md`](eval/ai_infra_report.md) —— 章节总报告 + 与主链路结合点

---

## 📝 License

仅用于学习与面试准备用途。依赖的开源项目遵循各自的 License（Qwen3: Apache 2.0；LLaMAFactory: Apache 2.0；TRL: Apache 2.0 等）。
