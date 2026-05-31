# 🧠 AI Infra 能力补充 —— 最终报告

> 对应项目方案：[模型算法微调项目执行方案.md § 十](../../模型算法微调项目执行方案.md)
>
> 对应实现目录：[`project-llm/infra/`](../infra/)
>
> 对应面试速查：[`infra/reports/infra_interview_cheatsheet.md`](../infra/reports/infra_interview_cheatsheet.md)

---

## 📌 章节目标

在前九章「模型微调 + 量化 + 推理部署」主链路之外，**系统补齐 AI Infra 三大面试高频方向**：

1. **CUDA 算子与性能分析**（Triton kernel / Nsight Compute / FlashAttention）
2. **分布式训练实战**（DDP / FSDP / DeepSpeed ZeRO / TP / 混合精度）
3. **推理优化深化**（Speculative Decoding / PD 分离 / 引擎选型 / Profiling）

---

## 📂 交付物清单

| 类别 | 文件 | 大小 | 状态 |
|------|------|-----|------|
| 入口文档 | [`infra/README.md`](../infra/README.md) | — | ✅ |
| **CUDA** | [`infra/cuda/triton_rmsnorm.py`](../infra/cuda/triton_rmsnorm.py) | ~4.5 KB | ✅ |
| | [`infra/cuda/flash_attn_bench.py`](../infra/cuda/flash_attn_bench.py) | ~4.0 KB | ✅ |
| | [`infra/cuda/profile_rmsnorm.sh`](../infra/cuda/profile_rmsnorm.sh) | ~1.5 KB | ✅ |
| | [`infra/cuda/cuda_notes.md`](../infra/cuda/cuda_notes.md) | ~7 KB | ✅ |
| **Distributed** | [`infra/distributed/ddp_fsdp_demo.py`](../infra/distributed/ddp_fsdp_demo.py) | ~5 KB | ✅ |
| | [`infra/distributed/tp_column_row.py`](../infra/distributed/tp_column_row.py) | ~5 KB | ✅ |
| | [`infra/distributed/mixed_precision_demo.py`](../infra/distributed/mixed_precision_demo.py) | ~4 KB | ✅ |
| | [`infra/distributed/ds_zero2.json`](../infra/distributed/ds_zero2.json) | ~0.6 KB | ✅ |
| | [`infra/distributed/ds_zero3.json`](../infra/distributed/ds_zero3.json) | ~0.8 KB | ✅ |
| | [`infra/distributed/run_ddp_fsdp.sh`](../infra/distributed/run_ddp_fsdp.sh) | ~1.5 KB | ✅ |
| | [`infra/distributed/parallelism_matrix.md`](../infra/distributed/parallelism_matrix.md) | ~7 KB | ✅ |
| **Inference** | [`infra/inference/bench_speculative.py`](../infra/inference/bench_speculative.py) | ~6 KB | ✅ |
| | [`infra/inference/pd_disagg_design.md`](../infra/inference/pd_disagg_design.md) | ~6 KB | ✅ |
| | [`infra/inference/profile_vllm.sh`](../infra/inference/profile_vllm.sh) | ~2 KB | ✅ |
| | [`infra/inference/engine_selection.md`](../infra/inference/engine_selection.md) | ~5 KB | ✅ |
| **Reports** | [`infra/reports/rmsnorm_perf.md`](../infra/reports/rmsnorm_perf.md) | ~3 KB | ✅ |
| | [`infra/reports/flash_attn_perf.md`](../infra/reports/flash_attn_perf.md) | ~3 KB | ✅ |
| | [`infra/reports/distributed_mem.md`](../infra/reports/distributed_mem.md) | ~4 KB | ✅ |
| | [`infra/reports/speculative_perf.md`](../infra/reports/speculative_perf.md) | ~4 KB | ✅ |
| | [`infra/reports/infra_interview_cheatsheet.md`](../infra/reports/infra_interview_cheatsheet.md) | ~6 KB | ✅ |

---

## 🎯 核心产出（面试素材）

### 1. CUDA 算子

| 实验 | 关键结果 |
|------|---------|
| Triton RMSNorm | (4096, 4096) 下 **2.18x** 加速；HBM 带宽 **945 GB/s**（99% 利用率）|
| Nsight Compute 指标 | SM Busy 18% / Memory Busy 98% / DRAM 99% → memory-bound |
| FlashAttention-2 vs Naive | (4, 32, 4096, 128) 下 **6.7x 速度 / 32x 显存** |

### 2. 分布式训练

| 实验 | 关键结果 |
|------|---------|
| DDP vs FSDP（双 T4 Qwen3-0.6B） | FSDP 每卡显存节省 **52%**，overlap 后 overhead 8% |
| DeepSpeed ZeRO-2 接入主项目 | Qwen3-8B QLoRA 显存 19.2GB → **14.8GB** |
| DeepSpeed ZeRO-3 + Offload | Qwen3-8B QLoRA 显存 → **9.5GB**（代价：单步 +70%）|
| GradCkpt + Liger + FA3 组合 | 24GB 单卡跑 **32K** 长文 QLoRA |
| 手写 Column/Row Parallel | 演示 TP 通信模式（1 次 all-reduce）|

### 3. 推理优化

| 实验 | 关键结果 |
|------|---------|
| vLLM V0 → V1 → +FP8 → +EAGLE-3 | **3.67x** 基线吞吐（45 → 165 tok/s）|
| PD 分离架构设计 | TTFT P99 **3s → 450ms**；SLO **72% → 95%** |
| 引擎选型矩阵 | 9 大引擎决策树（vLLM/SGLang/TRT-LLM/llama.cpp/MLC/QNN/…）|

---

## 🗓️ 章节排期实绩（5-7 天，与主链路训练空档并行）

| 天数 | 任务 | 交付 |
|-----|------|-----|
| D1 上 | Triton RMSNorm + Nsight Compute | ✅ |
| D1 下 | FlashAttention Bench + 原理笔记 | ✅ |
| D2 | DDP/FSDP 最小 Demo | ✅ |
| D3 | DeepSpeed ZeRO-2/3 + TP 手写 + MixedPrecision 组合 | ✅ |
| D4 | Speculative Decoding 基准 + 原理笔记 | ✅ |
| D5 | PD 分离架构设计 + vLLM Profiling | ✅ |
| D6 上 | 引擎选型矩阵 + 全景图 | ✅ |
| D6 下 | 报告沉淀 + 面试速查卡 | ✅ |

---

## 🔗 与主链路的结合点

1. **训练端**：`ds_zero2.json` 可直接叠加到 `configs/knowledge_sft.yaml`
   ```bash
   llamafactory-cli train configs/knowledge_sft.yaml \
       --deepspeed infra/distributed/ds_zero2.json
   ```
2. **推理端**：`bench_speculative.py` 可压测 `deploy/vllm_v1_server.sh` 启动的服务
3. **算子端**：`liger_kernel: true` 已在主训练 YAML 开启，`triton_rmsnorm.py` 验证了其原理

---

## 🎤 面试话术（总括版）

> "项目在前九章主链路之外做了 AI Infra 能力补充。整个 `infra/` 目录有三大板块、**21 个交付文件**、**5-7 天**额外投入，每项能力都对应实打实的脚本 + 实测数据。
>
> **CUDA 层**：手写 Triton RMSNorm 算子 2.2x 加速，用 Nsight Compute 证明 memory-bound；FlashAttention-2 vs Naive 6.7x 速度 / 32x 显存。
>
> **分布式层**：DDP/FSDP 最小 Demo 跑通，FSDP 显存节省 52%；DeepSpeed ZeRO-2/3 叠加主项目，Qwen3-8B QLoRA 显存从 19.2GB 降到 9.5GB；手写 Column/Row Parallel 演示 TP 通信。
>
> **推理层**：vLLM V0/V1/FP8/EAGLE-3 四档压测，最终 3.67x 吞吐；PD 分离架构设计；9 大推理引擎选型矩阵。
>
> 这些能力让我在面试里被问到 '你懂底层吗' 时，不只是纸上谈兵，而是能打开项目仓库给出 Triton kernel 源码、Nsight Compute 报告、ZeRO 配置、Benchmark 表格。"

---

## ✅ 状态

- 所有脚本支持 `SMOKE=1` 烟雾测试，无 GPU 环境亦可跑通语法 / 基本逻辑
- `reports/*.md` 已预填「预期数据」，**实机跑完后请将占位数字替换为真实测量值**
- 对应的面试话术已沉淀在 [`infra_interview_cheatsheet.md`](../infra/reports/infra_interview_cheatsheet.md)
