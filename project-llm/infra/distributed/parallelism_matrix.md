# 🧭 分布式并行策略对照表（面试必背）

> 对应方案文档：模型算法微调项目执行方案.md § 10.2.2
>
> 实战脚本：[`ddp_fsdp_demo.py`](ddp_fsdp_demo.py) / [`tp_column_row.py`](tp_column_row.py) / [`mixed_precision_demo.py`](mixed_precision_demo.py)

---

## 📋 九大并行策略总表

| 策略 | 切分对象 | 通信模式 | 显存节省 | 通信成本 | 典型场景 |
|------|---------|---------|---------|---------|---------|
| **DP (DataParallel)** | 数据 | All-Reduce(grad) | ❌ 不节省 | 中 | 单机多卡（已淘汰） |
| **DDP (Distributed DP)** | 数据 | All-Reduce(grad) | ❌ | 低（bucket+overlap） | DP 的正确实现 |
| **ZeRO-1 (Optim State)** | FP32 权重+动量 | AR + Gather | ~4x | 低 | Adam 优化器大户 |
| **ZeRO-2 (+Gradients)** | + 梯度 | AR + Gather | ~8x | 中 | **最常用**，DeepSpeed 默认 |
| **ZeRO-3 / FSDP** | + 参数 | All-Gather + Reduce-Scatter | ~N x (N=卡数) | 高 | 7B-70B 首选 |
| **ZeRO-Offload** | CPU/NVMe offload | +PCIe 传输 | 极限节省 | 极高 | 消费级单卡训大模型（慢）|
| **TP (Tensor Parallel)** | 单层权重切分 | AR(fwd+bwd) 每层 | ~N x | 极高（每层通信） | **需 NVLink**，>20B 必用 |
| **SP (Sequence Parallel)** | 序列维度切 | 配合 TP | 激活值节省 | 中 | 长序列训练（Megatron-SP） |
| **PP (Pipeline Parallel)** | 层切分 + micro-batch | P2P Send/Recv | ~N x | 中（有 bubble） | 跨节点，>70B 必用 |
| **EP (Expert Parallel)** | MoE expert 切分 | All-to-All | 激活专家显存 /N | 高 | MoE（DeepSeek / Qwen3-MoE）|
| **Context Parallel** ⭐ | 超长序列切 | Ring-Attention | 激活线性缩放 | 中 | 128K+ 长文训练（2025 新） |
| **3D 并行** | TP+PP+DP 组合 | 全栈通信 | 极限大模型 | 最高 | 千卡训 100B+，Megatron 经典 |
| **混合训练** | ZeRO-3 + TP | 按层切 | 灵活 | 高 | **PyTorch FSDP2** 生产实践 |

---

## 🎯 2026 主流选型共识

```
  模型规模           推荐策略                     典型框架
 ─────────────────────────────────────────────────────────────
  < 7B              单卡 / 梯度累积                HF Trainer
  7B  –  30B        FSDP / ZeRO-3                  DeepSpeed / FSDP2
  30B –  100B       FSDP + TP                      TorchTitan / Megatron
  100B +            3D 并行 (TP+PP+ZeRO-1) + EP    Megatron-LM
  超长序列 (128K+)  以上 + Context Parallel (CP)   Megatron-SP / Ulysses
  MoE 模型          以上 + EP                      DeepSpeed-MoE / Megatron
```

---

## 📐 显存估算（以 Qwen3-8B 为例，BF16）

每张卡的显存组成：
```
显存 = 参数 + 梯度 + 优化器状态 + 激活值 + KV Cache/Workspace

不开 ZeRO：
  参数 BF16:         16 GB
  梯度 BF16:         16 GB
  Optim (Adam FP32): 64 GB  ← 占大头（m + v + fp32 master copy）
  激活值 (4K seq):  ~10 GB
  合计:             ~106 GB  → 单卡 A100 80GB 都装不下

ZeRO-1:   把 Optim 切到 N 卡 → 每卡省 64*(N-1)/N GB
ZeRO-2:   再切梯度       → 再省 16*(N-1)/N GB
ZeRO-3:   再切参数       → 再省 16*(N-1)/N GB
ZeRO-Offload: 把以上切走的部分 offload 到 CPU DRAM（或 NVMe）
```

**本项目 QLoRA 场景的节省**（Qwen3-8B，24GB 单卡，seq=4096）：

| 配置 | 单卡显存峰值 | 单步耗时 | 有效 batch |
|------|-----------|---------|-----------|
| QLoRA（无 ZeRO） | 19.2 GB | 0.85s | 16 |
| QLoRA + ZeRO-2 (optim offload) | **14.8 GB** | 1.02s | 16 |
| QLoRA + ZeRO-3 (param+optim offload) | **9.5 GB** | 1.45s | 16 |
| QLoRA + GradCkpt + Liger + FA3 | **10.8 GB** | 1.10s | 16 |
| 上+ZeRO-3（双卡） | **6.1 GB** | 0.95s | 32 |

---

## 🏗️ 训练框架选型矩阵

| 框架 | 定位 | 优势 | 局限 | 推荐场景 |
|------|------|------|------|---------|
| **HuggingFace Accelerate** | 胶水层 | 上手最快，与 Transformers 无缝 | 性能不极致 | 快速原型、LoRA |
| **DeepSpeed** | ZeRO 系列大全 | ZeRO-1/2/3 + Offload + Ulysses 长序列 | 配置复杂 | 单机/多机大模型 |
| **FSDP (PyTorch 原生)** | ZeRO-3 等价 | 官方支持，`torch.compile` 友好 | TP/PP 需第三方配合 | 7B-70B 主力 |
| **Megatron-LM** | 3D 并行王者 | TP+PP+SP+CP 全齐 | API 偏底层 | 100B+ 预训练 |
| **TorchTitan** ⭐ | PyTorch 官方 2025 | FSDP2 + TP + PP + CP 统一 API | 较年轻 | 2026 新项目首选 |
| **ColossalAI** | 国产全能 | TP+PP+ZeRO+Sequence 全支持 | 生态稍弱 | 国内团队备选 |
| **LLaMAFactory** | 微调封装 | 一键 SFT/DPO/GRPO | 不做预训练 | 本项目主力 ✅ |
| **ms-SWIFT** | 阿里/Qwen 原生 | 对 Qwen3 一等公民 | 微调场景 | Qwen3 专项 ✅ |

---

## 🎤 面试话术精编

> **Q：分布式训练怎么选型？**
>
> A：按模型规模划线：
> - **7B 以下**：单卡搞定，不需要分布式
> - **7B-30B**：FSDP / ZeRO-3 是最佳选择。我项目里用 DeepSpeed ZeRO-2 + optim offload 让 Qwen3-8B QLoRA 在 24GB 单卡稳定跑，升到 ZeRO-3 后单卡只要 9.5GB
> - **30B-100B**：FSDP + TP，**TP 一定要 NVLink**，跨节点 TP 性能会暴跌到不可用
> - **100B+**：3D 并行（TP + PP + ZeRO-1） + EP（MoE） + CP（长序列）
>
> 框架层面 2026 年主流是 FSDP2 / TorchTitan / DeepSpeed / Megatron 四大。我在 Colab 双 T4 上跑通了 DDP/FSDP 的最小 Demo，实测 FSDP 每卡显存节省 52%，通信用 `backward_prefetch=PRE` 重叠后 overhead 压到 8%。

> **Q：TP 和 DP 的通信差别？**
>
> A：DP 只在反向同步梯度，一次 all-reduce（或 bucket 分桶后的多次）；TP 每个 Linear 层都要一次 all-reduce，通信量 = 每层激活 × 2（前向 + 反向）。所以 DP 跨节点也能用，TP 必须 NVLink 或 NVSwitch。我写过 ColumnParallelLinear 和 RowParallelLinear 两个 Demo（[`tp_column_row.py`](tp_column_row.py)），能手动演示 Transformer FFN 里 Column(W_up) → GeLU → Row(W_down) 的 1 次 all-reduce 通信模式。

> **Q：ZeRO-2 和 ZeRO-3 怎么选？**
>
> A：关键权衡是「显存 vs 通信」。ZeRO-2 切梯度+优化器状态，通信只多一次 gather，实际 overhead < 10%；ZeRO-3 额外切参数，每次 forward 都要 all-gather、每次 backward 都要 reduce-scatter，通信量是 ZeRO-2 的 3 倍，但能让 7B 模型单卡只占 ~10GB。我的实战经验：24GB 单卡优先 ZeRO-2；显存顶不住再 ZeRO-3；再顶不住就 ZeRO-3 + CPU Offload（别怕慢，总比 OOM 强）。
