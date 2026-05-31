# 🧩 AI Infra 能力补充（第十章）

> **定位**：在前九章「模型微调 + 量化 + 推理部署」主链路之外，补齐 **CUDA 算子 / 分布式训练 / 推理引擎内核** 三大面试高频话题，让每一块 AI Infra 都有实打实的动手痕迹。
>
> **资源约束**：单卡 24GB 完成 80% 实验；剩余 20% 用 Colab/Kaggle 免费双 T4 或本机多进程 CPU 模拟。
>
> **投入**：5-7 天（可与主链路训练空档期并行）。
>
> **参考文档**：[模型算法微调项目执行方案.md § 十](../../模型算法微调项目执行方案.md)

---

## 📁 目录结构

```
infra/
├── README.md                    # 本文件：三大板块总览
│
├── cuda/                        # 10.1 CUDA 算子与性能分析（1.5 天）
│   ├── triton_rmsnorm.py        # 手写 Triton 融合 RMSNorm 算子
│   ├── flash_attn_bench.py      # FlashAttention 基准测试 + 原理笔记
│   ├── profile_rmsnorm.sh       # Nsight Compute 性能分析脚本
│   └── cuda_notes.md            # CUDA 内存层次 + 算子优化知识地图
│
├── distributed/                 # 10.2 分布式训练实战（2 天）
│   ├── ddp_fsdp_demo.py         # DDP / FSDP 最小 Demo（CPU/GPU 均可）
│   ├── ds_zero2.json            # DeepSpeed ZeRO-2 配置（接入主项目）
│   ├── ds_zero3.json            # DeepSpeed ZeRO-3 + CPU offload 配置
│   ├── tp_column_row.py         # 手写 Column/Row Parallel Linear（TP 原理）
│   ├── mixed_precision_demo.py  # BF16 + GradCkpt + Liger 组合实测
│   ├── run_ddp_fsdp.sh          # torchrun 启动脚本
│   └── parallelism_matrix.md    # DP/ZeRO/TP/PP/EP/CP 对照表
│
├── inference/                   # 10.3 推理优化深化（2 天）
│   ├── bench_speculative.py     # EAGLE-3 / Medusa / 基线 端到端并发压测
│   ├── pd_disagg_design.md      # Prefill/Decode 分离架构设计 + vLLM 启动
│   ├── profile_vllm.sh          # Nsight Systems + Prometheus 压测+诊断
│   └── engine_selection.md      # vLLM/SGLang/TRT-LLM/LMDeploy 选型矩阵
│
└── reports/                     # 实测结果沉淀（面试素材）
    ├── rmsnorm_perf.md          # Triton RMSNorm 对比 PyTorch 的实测数据
    ├── flash_attn_perf.md       # FA2 vs Naive 速度 / 显存对比
    ├── distributed_mem.md       # DDP vs FSDP vs ZeRO-2/3 显存对比
    ├── speculative_perf.md      # vLLM V0/V1 + FP8 + EAGLE-3 四档对比
    └── infra_interview_cheatsheet.md  # 面试速查卡（5 个核心问题话术）
```

---

## 🎯 三大板块交付概览

### 10.1 CUDA 算子与性能分析 ⭐

| 能力项 | 实践 | 预期产出 |
|-------|------|---------|
| 手写 Triton 融合算子 | `cuda/triton_rmsnorm.py` | 对 PyTorch 原生 **2.2x 加速**，HBM 带宽利用率 **97%+** |
| 算子性能分析 | Nsight Compute 硬件指标解读 | SM Busy / Memory Busy / Occupancy 真实数据 |
| FlashAttention 实测 | `cuda/flash_attn_bench.py` | FA2 vs Naive **6.7x 速度 / 32x 显存** |
| 源码阅读 + 示意图 | FlashAttention / PagedAttention 原理 | 可在白板画出 tiling + online softmax |

### 10.2 分布式训练实战 ⭐⭐

| 能力项 | 实践 | 预期产出 |
|-------|------|---------|
| 并行策略全景 | `parallelism_matrix.md` | DP/ZeRO-1/2/3/FSDP/TP/PP/EP/CP/3D 对照表（必背）|
| DDP/FSDP 最小 Demo | `ddp_fsdp_demo.py`（双 T4）| FSDP 每卡显存节省 **52%** |
| DeepSpeed ZeRO-2/3 接入主项目 | `ds_zero2.json` + `ds_zero3.json` | Qwen3-8B QLoRA 显存从 19.2GB → **9.5GB** |
| 手写 TP（Column/Row Parallel）| `tp_column_row.py` | 理解 TP 通信模式与 NVLink 依赖 |
| 混合训练组合优化 | `mixed_precision_demo.py` | 32K 长文 QLoRA 单卡可行性验证 |

### 10.3 推理优化深化 ⭐⭐⭐

| 能力项 | 实践 | 预期产出 |
|-------|------|---------|
| Speculative Decoding 三方案 | `bench_speculative.py` | Qwen3-8B FP8 + EAGLE-3 **3.67x** 加速（vLLM V0 BF16 基线）|
| PD 分离架构设计 | `pd_disagg_design.md` | TTFT P99 从 3s → **450ms**、SLO 72% → 95% |
| 推理引擎选型矩阵 | `engine_selection.md` | 9 大引擎适用场景一图总结 |
| 端到端 Profiling | `profile_vllm.sh` | 用 Nsight Systems 定位真实瓶颈案例 |

---

## 🚀 快速开始

```bash
# 1) CUDA 算子（单卡即可）
python infra/cuda/triton_rmsnorm.py
bash   infra/cuda/profile_rmsnorm.sh     # 需要 Nsight Compute
python infra/cuda/flash_attn_bench.py

# 2) 分布式训练（本机 CPU 多进程 / Colab 双 T4）
MODE=ddp  torchrun --nproc_per_node=2 --backend=gloo infra/distributed/ddp_fsdp_demo.py
MODE=fsdp torchrun --nproc_per_node=2 --backend=gloo infra/distributed/ddp_fsdp_demo.py
torchrun --nproc_per_node=2 infra/distributed/tp_column_row.py

# 将 ZeRO 配置叠加到主项目训练
llamafactory-cli train configs/knowledge_sft.yaml \
    --deepspeed infra/distributed/ds_zero2.json

# 3) 推理优化 Benchmark（需已部署 vLLM 服务）
python infra/inference/bench_speculative.py \
    --baseline-url http://localhost:8000/v1/chat/completions \
    --eagle3-url  http://localhost:8001/v1/chat/completions \
    --prompts eval/bench_prompts.txt --concurrency 16

bash infra/inference/profile_vllm.sh      # Nsight Systems 抓时间线
```

---

## 📚 对应学习路线映射

| 学习路线章节 | 对应实践 | 覆盖程度 |
|------------|---------|---------|
| 阶段二·2.1.1 KV Cache | `inference/bench_speculative.py` + PagedAttention 源码走读 | ★★★★ |
| 阶段二·2.1.2 算子融合 / CUDA Graph | `cuda/triton_rmsnorm.py` | ★★★★ |
| 阶段二·2.1.3 CUDA 编程基础 | Triton + Nsight Compute + 硬件指标 | ★★★ |
| 阶段二·2.1.4 显存分析与优化 | `distributed/mixed_precision_demo.py` + ZeRO 配置 | ★★★★★ |
| 阶段三·3.1-3.3 推理引擎 | `inference/engine_selection.md` + PD 分离 | ★★★★★ |
| 阶段四·4.1 分布式训练 | `distributed/*` 全集 | ★★★★ |

---

## 🎤 面试话术索引

1. **"CUDA 会到什么程度？"** → [`reports/infra_interview_cheatsheet.md § 1`](reports/infra_interview_cheatsheet.md)
2. **"分布式并行如何选型？"** → [`reports/infra_interview_cheatsheet.md § 2`](reports/infra_interview_cheatsheet.md)
3. **"推理优化的五个维度？"** → [`reports/infra_interview_cheatsheet.md § 3`](reports/infra_interview_cheatsheet.md)
4. **"推理引擎怎么选？"** → [`reports/infra_interview_cheatsheet.md § 4`](reports/infra_interview_cheatsheet.md)
5. **"算子融合举例？"** → [`reports/infra_interview_cheatsheet.md § 5`](reports/infra_interview_cheatsheet.md)

---

## ⚠️ 使用说明

- 所有脚本都以 **"可在项目单卡环境跑通 smoke test"** 为底线；真实性能数据请结合硬件平台再次验证
- `reports/` 下的 Markdown 已预填「预期数据」，实机跑完后请同步替换为真实测量值
- CUDA 相关脚本需安装 `triton>=3.1.0` 与匹配 CUDA Runtime；Nsight Compute 通常随 CUDA 12.x Toolkit 自带
- 分布式脚本在无 GPU 环境下使用 `gloo` 后端 + CPU 亦可跑通（仅用于原理演示）

