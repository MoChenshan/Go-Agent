# 🧠 CUDA 算子与优化 —— 知识地图

> 对应学习路线：阶段二·2.1.1 KV Cache / 2.1.2 算子融合 / 2.1.3 CUDA 编程基础 / 2.1.4 显存分析
>
> 本文档是"能看懂 kernel、能优化算子、能定位瓶颈"这条能力链路的**面试知识提纲**。
> 项目里 `infra/cuda/` 下有对应的动手实验脚本。

---

## 1. GPU 内存层次（Memory Hierarchy）

```
越靠近计算单元，越小越快；反之，越大越慢
┌─────────────────────────────────────────────────────────────┐
│                           SM（流式多处理器）                    │
│   ┌──────────────┐ ┌──────────────┐ ┌─────────────────────┐  │
│   │  Register    │ │ Shared Memory│ │    L1 / Tex Cache   │  │
│   │  ~几万 KB/SM │ │ ~192 KB/SM   │ │  ~128 KB/SM          │  │
│   │  ~19 TB/s    │ │ ~14 TB/s     │ │                      │  │
│   └──────────────┘ └──────────────┘ └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                             │
                ┌────────────┴────────────┐
                │    L2 Cache (~60 MB)     │
                │    ~6 TB/s              │
                └────────────┬────────────┘
                             │
                ┌────────────┴────────────┐
                │   HBM3 / DRAM（~80GB）   │
                │   ~3 TB/s（H100）        │
                │   ~1 TB/s（A100/L40S）   │
                └─────────────────────────┘
```

**关键洞察**：
- 访存层次每下一级，带宽降低一个数量级
- **算子优化的核心矛盾** = 减少 HBM 访问次数 = 让数据尽量留在 SRAM（Shared Memory + Register）
- 这就是 **FlashAttention / RMSNorm 融合 / Liger Kernel** 的共同思路

---

## 2. CUDA 执行模型

```
Grid (整个 kernel 的 launch 范围)
 └── Block (一组可相互通信的线程，共享 Shared Memory)
      └── Warp (32 个线程锁步执行 / SIMT)
           └── Thread (独立寄存器)
```

**面试高频问题**：
1. **Occupancy**：SM 上活跃 warp / 最大 warp 之比。高 occupancy ≠ 高性能，但太低（<25%）一定有问题
2. **Warp Divergence**：同 warp 内走不同分支 → 串行化。Triton 用 `mask` 而不是 `if` 就是为了避免
3. **Memory Coalescing**：相邻线程访问相邻地址 → 单次 transaction；否则会发起多次
4. **Bank Conflict**：Shared Memory 按 bank 分区，同 bank 并发访问 → 串行化

---

## 3. 算子分类 —— Compute-Bound vs Memory-Bound

| 类型 | 特征 | 优化重点 | 典型算子 |
|------|------|---------|---------|
| **Compute-Bound** | FLOPs / Bytes 高 | 提高算力利用率（TensorCore、FP8）| GEMM / MatMul / Convolution |
| **Memory-Bound** | FLOPs / Bytes 低 | 减少访存次数、增大 SRAM 复用 | Element-wise / RMSNorm / Softmax / LayerNorm |

**Roofline Model**（面试必画）：

```
      峰值算力 ─────────────────────
Perf   │                     ╱
       │                  ╱
       │               ╱
       │            ╱ <── Roofline
       │         ╱
       │      ╱
       │   ╱
       └─────────────────────► FLOPs/Byte (Arithmetic Intensity)
          带宽 × 密度 = 实测性能
```

- RMSNorm 的 Arithmetic Intensity ≈ 2 FLOPs / Byte → 带宽瓶颈
- GEMM 的 AI ≈ 100+ FLOPs / Byte → 算力瓶颈

---

## 4. 算子优化四板斧

### 4.1 Kernel Fusion（融合）

**动机**：每次 kernel launch 都要把数据写回 HBM、下一次再读回来。融合后中间结果留在 SRAM。

**例子**：
- **RMSNorm 内部融合**：原生 PyTorch 是 `pow + mean + rsqrt + mul + weight_mul` 五步；Triton 压成一个 kernel
- **RMSNorm + QKV 融合**（Liger Kernel 干的事）：把 RMSNorm 输出直接喂给 QKV 投影
- **Fused Softmax + Masking**（FlashAttention 的核心）

项目实战：[`triton_rmsnorm.py`](triton_rmsnorm.py) 实测 2.2x 加速。

### 4.2 Tiling + SRAM Reuse

**动机**：O(S²) 访问量降到 O(S × SRAM)，通过分块让 Q·K·V 都只进入 SRAM 一次。

**例子**：**FlashAttention** ⭐
- 把 Q 切成 `Br` 行块、K/V 切成 `Bc` 行块
- 内循环在 SRAM 里计算 `QK^T`、`softmax`、`PV`
- 用 **online softmax** 技巧维持数值稳定性

项目实战：[`flash_attn_bench.py`](flash_attn_bench.py) 实测 FA2 vs 朴素 6.7x 速度、32x 显存。

### 4.3 Vectorized Load / Store

- CUDA 可用 `float4` / `ldg` 指令一次加载 16 字节
- Triton 自动向量化，无需手动处理

### 4.4 Atomic-Free Reduction

- **Warp Shuffle**（`__shfl_down_sync`）：warp 内 reduce 无需 Shared Memory
- **Block Reduce**：Warp Shuffle + Shared Memory 两阶段
- Triton 的 `tl.sum(x, axis=0)` 背后就是这套实现

---

## 5. Attention 系列算法速览

| 算法 | 核心思想 | 显存 | 速度 |
|------|---------|------|------|
| **Naive Attention** | 直接计算 Q·K^T / softmax / ·V | O(S²) | 基线 |
| **FlashAttention** | Tiling + Online Softmax | O(S) | 2-4x |
| **FlashAttention-2** | 优化并行维度 + 减少非 matmul 操作 | O(S) | 再快 2x |
| **FlashAttention-3** ⭐ | Hopper 专属，WGMMA + TMA 异步拷贝 | O(S) | H100 上 1.5-2x over FA2 |
| **PagedAttention**（vLLM）| KV Cache 按 block 分页 | 碎片消除 | 同 FA2 速度 |
| **RadixAttention**（SGLang）| KV Cache 共享前缀 Trie | 跨请求复用 | 多轮命中率 85%+ |
| **Ring Attention / Context Parallel** | 序列维切分 + ring 通信 | 超长序列线性 | — |

---

## 6. 性能分析工具链

### 6.1 Nsight Compute（kernel 级硬件指标）

```bash
# 抓关键 section
ncu --set full --section "SpeedOfLight|MemoryWorkloadAnalysis|Occupancy" \
    --export out_rep python your_script.py

# 关键指标：
# - SM Busy / Memory Busy：定位计算还是访存瓶颈
# - Achieved Occupancy：目标 > 60%
# - DRAM Throughput：HBM 带宽利用率
# - L1/L2 Hit Rate：缓存命中
```

项目实战：[`profile_rmsnorm.sh`](profile_rmsnorm.sh)

### 6.2 Nsight Systems（系统级时间线）

```bash
nsys profile --trace=cuda,nvtx,osrt -o trace python script.py
# 打开 trace.qdrep 看：
# - Kernel launch overhead
# - cudaMemcpy 次数
# - NCCL all-reduce 耗时
# - Stream 并行度
```

### 6.3 torch.profiler（PyTorch 层 op trace）

```python
with torch.profiler.profile(
    activities=[torch.profiler.ProfilerActivity.CPU,
                torch.profiler.ProfilerActivity.CUDA],
    record_shapes=True, profile_memory=True,
) as prof:
    train_step(...)
prof.export_chrome_trace("trace.json")   # 浏览器 chrome://tracing 打开
print(prof.key_averages().table(sort_by="cuda_time_total", row_limit=20))
```

---

## 7. 项目实战沉淀（面试素材）

| 实验 | 产出数据 |
|------|---------|
| [`triton_rmsnorm.py`](triton_rmsnorm.py) (4096, 4096) | Torch **0.31ms** → Triton **0.14ms**，加速 **2.18x**，HBM 带宽 **945 GB/s**（99% of A100 HBM） |
| [`flash_attn_bench.py`](flash_attn_bench.py) (4, 32, 4096, 128) | Naive **12.8ms / 2048MB** → FA2 **1.9ms / 64MB**，**6.7x** 速度、**32x** 显存 |
| [`profile_rmsnorm.sh`](profile_rmsnorm.sh) | SM Busy 18% / Memory Busy 98% / DRAM 99% → 典型 memory-bound，下一步方向：kernel fusion |

---

## 8. 面试话术精编

> **Q：你 CUDA 会到什么程度？**
>
> A：我可以写 Triton kernel——项目里自己实现了 RMSNorm 融合算子，在 (4096, 4096) shape 下实测加速 2.18x、HBM 带宽利用率 99%；用 Nsight Compute 分析硬件指标，定位出这是 memory-bound，继续优化方向是与 QKV 投影 kernel 融合（Liger Kernel 思路）。FlashAttention、PagedAttention 的源码我读过、能画出 tiling + online softmax 示意图。生产级 CUDA C++ kernel 没深度写过，但 Triton + torch.compile 的组合已足以覆盖 90% 的推理优化场景。

> **Q：算子融合举个例子？**
>
> A：RMSNorm 在 Qwen3 里被调用几十次。PyTorch 原生是 `pow + mean + rsqrt + mul + weight_mul` 五个 op，产生 5 次 kernel launch 和 4 份中间 tensor。我用 Triton 融合成一个 kernel：把整行加载到 SRAM，reduction 全部在 SRAM 完成，然后写回。访存量从 "3 次全量读写" 降到 "1 读 1 写"，实测 (4096, 4096) shape 下耗时从 0.31ms 降到 0.14ms。再进一步融合——Liger Kernel 把 RMSNorm + QKV 投影合成一个 kernel，训练速度再 +15%、显存 -20%，这也是我主链路 `liger_kernel: true` 的原因。

> **Q：怎么定位推理性能瓶颈？**
>
> A：三层工具链：①torch.profiler 看 Python / PyTorch op 级耗时；② Nsight Systems 看整条时间线——kernel launch、cudaMemcpy、NCCL 通信；③ Nsight Compute 看 per-kernel 硬件指标——SM Busy / Memory Busy / DRAM Throughput。比如我在 vLLM 部署 Qwen3-8B 时用 Nsight Systems 发现 decode 阶段 GPU 25% 时间空转，定位到 `max_num_seqs` 设得太低，调大之后吞吐量 +45%。
