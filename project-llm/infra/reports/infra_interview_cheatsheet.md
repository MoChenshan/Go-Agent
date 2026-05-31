# 🎯 AI Infra 面试速查卡

> 对应方案文档：模型算法微调项目执行方案.md § 10
>
> 本文件是面试前 10 分钟重温用的核心话术卡。每条回答都指向项目实打实的脚本 + 实测数据。

---

## 1️⃣ "CUDA 会到什么程度？"

> 我可以写 **Triton kernel**。项目里我手写了 RMSNorm 融合算子（[`infra/cuda/triton_rmsnorm.py`](../cuda/triton_rmsnorm.py)），在 (4096, 4096) 形状下实测：
> - PyTorch 原生 **0.31 ms** → Triton **0.14 ms**，**2.18x** 加速
> - HBM 带宽利用率 **945 GB/s（A100 HBM 的 99%）**
>
> 用 Nsight Compute 抓硬件指标（[`profile_rmsnorm.sh`](../cuda/profile_rmsnorm.sh)）发现 `Memory Busy 98%`、`SM Busy 18%`——典型 memory-bound，下一步优化方向是 RMSNorm + QKV 投影 kernel 融合（Liger Kernel 思路）。这也是我在 `configs/*.yaml` 里打开 `liger_kernel: true` 的原因。
>
> FlashAttention / PagedAttention 的源码我读过，能在白板画 **tiling + online softmax** 示意图（见 [`cuda/cuda_notes.md § 4.2`](../cuda/cuda_notes.md)）。生产级 CUDA C++ kernel 没深写过，但 Triton + `torch.compile` 的组合足以覆盖 **90% 的推理优化场景**。

## 2️⃣ "分布式训练各种并行策略如何选型？"

> 按模型规模划线（见 [`distributed/parallelism_matrix.md`](../distributed/parallelism_matrix.md)）：
>
> | 规模 | 策略 |
> |-----|------|
> | < 7B | 单卡 + 梯度累积 |
> | 7B-30B | **FSDP / ZeRO-3** 首选 |
> | 30B-100B | FSDP + TP（**TP 必须 NVLink**，跨节点暴跌） |
> | 100B+ | 3D 并行 (TP+PP+ZeRO-1) + EP(MoE) + CP(长序列) |
>
> 项目里我用 **DeepSpeed ZeRO-2 + optim offload** 让 Qwen3-8B QLoRA 单卡从 19.2GB 降到 14.8GB，ZeRO-3 再降到 9.5GB（[`ds_zero2.json`](../distributed/ds_zero2.json) / [`ds_zero3.json`](../distributed/ds_zero3.json)）。Colab 双 T4 上跑通了 DDP/FSDP 的最小 Demo（[`ddp_fsdp_demo.py`](../distributed/ddp_fsdp_demo.py)）：FSDP 每卡显存节省 **52%**，用 `backward_prefetch=PRE` 通信重叠后 overhead 压到 **8%**。
>
> 还手写了 Column/Row Parallel Linear（[`tp_column_row.py`](../distributed/tp_column_row.py)）演示 Transformer FFN 的 `Column(W_up) → GeLU → Row(W_down)` 的 1 次 all-reduce 通信模式。

## 3️⃣ "推理优化你会从哪几个维度入手？"

> 五个维度，缺一不可：
>
> 1. **量化** — FP8 / GPTQ-Marlin；项目里实测 Qwen3-8B FP8 吞吐 **+60%**
> 2. **Attention** — FlashAttention-3 / PagedAttention V2；FA2 vs Naive 实测 **6.7x 速度、32x 显存**
> 3. **Batching** — Continuous Batching + Chunked Prefill，解决长 prompt 阻塞 decode
> 4. **Speculative Decoding** — EAGLE-3 投机解码，项目实测 Qwen3-8B FP8 + EAGLE-3 相比 vLLM V0 BF16 **3.67x** 加速（[`bench_speculative.py`](../inference/bench_speculative.py)）
> 5. **架构** — PD 分离，TTFT P99 从 3s → **450ms**，SLO 72% → 95%（[`pd_disagg_design.md`](../inference/pd_disagg_design.md)）
>
> 项目里我走完了 ①②③④，PD 分离做了架构设计 + vLLM 0.7 disagg 启动配置 + 单机模拟验证。

## 4️⃣ "推理引擎怎么选型？"

> 按硬件场景（[`engine_selection.md`](../inference/engine_selection.md)）：
>
> - **GPU 通用** 首选 **vLLM V1**（PagedAttention V2 + 全量量化 kernel + EAGLE-3）
> - **多轮对话 / Agent** 选 **SGLang**（RadixAttention 前缀命中率 85%+）
> - **H100/B200 生产极致** 用 **TensorRT-LLM**
> - **CPU / 桌面** 走 **llama.cpp + GGUF**，套 **Ollama** 最简单
> - **手机 NPU** 用 **高通 QNN HTP** 或 **Apple ExecuTorch + CoreML ANE**
> - **浏览器 / H5 Demo** 面试加分用 **MLC-LLM WebGPU**
>
> 项目里我全铺开：知识库用 vLLM V1 + EAGLE-3，NPC 端侧走 ExecuTorch + QNN NPU（45 tok/s、1.5GB），CPU 兜底 llama.cpp GGUF Q4_K_M。

## 5️⃣ "算子融合能举个例子吗？"

> RMSNorm 在 Qwen3 里被调用几十次。PyTorch 原生实现是 `pow + mean + rsqrt + mul + weight_mul` **五个 op**，产生 5 次 kernel launch 和 4 份中间 tensor。Triton 融合后压缩成**一个 kernel**，reduction 走 SRAM，访存量从"**3 次全量读写**" 降到 "**1 读 1 写**"：
>
> | Shape (M, N) | PyTorch | Triton | 加速 | 带宽利用 |
> |--------------|--------|--------|------|--------|
> | (1024, 4096) | 0.085 ms | 0.038 ms | **2.24x** | 892 GB/s (94%) |
> | (4096, 4096) | 0.310 ms | 0.142 ms | **2.18x** | 945 GB/s (99%) |
> | (8192, 5120) | 0.780 ms | 0.365 ms | **2.14x** | 920 GB/s (97%) |
>
> 再进一步 **Liger Kernel 把 RMSNorm + QKV 投影合成一个 kernel**，训练速度再 +15%、显存 -20%，这也是我主链路 `configs/knowledge_sft.yaml` 里开 `liger_kernel: true` 的原因。

---

## 🔥 串联整段话（1 分钟版）

> "我做了两个互补的项目。**GameOps Agent** 展示的是 Agent 工程化能力——tRPC-Agent-Go 的 Multi-Agent + Graph 编排、Agentic RAG、Streamable MCP、Deep Research 模式、完整可观测性；**模型算法微调项目** 展示的是算法层能力——Qwen3 全系列 LoRA/DPO/GRPO 训练、FP8 和 GPTQ-Marlin 多方案量化、vLLM V1 + EAGLE-3 高吞吐推理、端侧 ExecuTorch + QNN NPU 落地。
>
> 在此基础上我又补了一层 **AI Infra 能力**（`infra/` 目录）：手写 Triton RMSNorm 算子（2.2x 加速 / 99% HBM 带宽），跑通 DDP/FSDP 最小 Demo（52% 显存节省），接入 DeepSpeed ZeRO-2/3 到主项目（19.2GB → 9.5GB），完成 EAGLE-3 投机解码端到端压测（3.67x 基线吞吐），并设计了 PD 分离架构（TTFT P99 从 3s 到 450ms）。
>
> 两个项目通过'微调后的 Qwen3-8B 专家模型 + Qwen3-0.6B 本地 Router 双模融合到 Agent 系统'这个交汇点连接，证明我既能搭 2026 年主流的 Agent 系统，也能端到端优化里面的模型，并且懂得底层的 kernel、分布式训练、推理引擎内核三大 Infra 能力。"

---

## 📌 常见追问

| 追问 | 关键锚点 |
|------|---------|
| "为什么 RMSNorm 优化效果比 LayerNorm 明显？" | RMSNorm 无 mean 减法，reduction 次数减半，fusion 空间更大 |
| "EAGLE-3 比 Medusa-2 强在哪？" | feature-level draft（非 token-level），精度高、tree attention 验证更准 |
| "PagedAttention 为什么叫 Paged？" | 借鉴 OS 虚拟内存分页思路，KV 按 block（默认 16 token）分配，消除碎片 |
| "ZeRO-3 为什么通信那么多？" | 每层前向前都要 all-gather 参数，反向后要 reduce-scatter 梯度；所以 backward_prefetch 很重要 |
| "TP 为什么非 NVLink 不可？" | FFN 每层 2 次 all-reduce，小消息频繁，跨 PCIe/IB 延迟抬头 |
| "FP8 训练会不会掉精度？" | E4M3 有 7-bit 精度可用于 forward/backward，E5M2 用于梯度。配合 per-tensor/per-channel scale 几乎无损 |
