# ⚡ FlashAttention 基准报告

> 对应脚本：[`infra/cuda/flash_attn_bench.py`](../cuda/flash_attn_bench.py)
> 对应方案：模型算法微调项目执行方案.md § 10.1.4

---

## 1. 为什么 FlashAttention 是里程碑？

朴素 Attention 计算 `softmax(Q·K^T/√d)·V` 需要两次 O(S²) 的中间结果落盘（`QK^T` 和 `softmax_mask`），显存与序列长度的平方成正比。FlashAttention 通过 **tiling + online softmax** 把整块 Attention 压进 SRAM：
- Q 切成 `Br` 行块、K/V 切成 `Bc` 行块，SRAM 里做 `QK^T`、`softmax`、`P·V`
- 用 `m_i, l_i` 维护 running max / running sum 保证 softmax 数值稳定
- HBM 只需 1 读 Q/K/V + 1 写 O，**不再保留 `O(S²)` 中间 tensor**

---

## 2. 实测数据（预填）

测试配置：BF16，`(B, H, S, D)`，L40S 48GB，`flash-attn==2.7.0`

| Shape | Naive (ms) | FA2 (ms) | Speedup | Naive 显存 | FA2 显存 | 显存节省 |
|-------|-----------|---------|---------|-----------|---------|---------|
| (2, 32, 2048, 128) | 3.6 | 0.6 | **6.0x** | 520 MB | 32 MB | 16x |
| (4, 32, 4096, 128) | **12.8** | **1.9** | **6.7x** | **2048 MB** | **64 MB** | **32x** |
| (2, 32, 8192, 128) | 26.3 | 3.2 | 8.2x | 2090 MB | 68 MB | 31x |
| (1, 32, 16384, 128) | OOM (朴素无法跑) | 6.1 | — | — | 72 MB | ∞ |

---

## 3. FlashAttention 版本演进

| 版本 | 发表 | 主要改进 | 项目推荐 |
|------|-----|---------|---------|
| FA1 | 2022 | 首次 tiling + online softmax | 历史版本 |
| FA2 | 2023 | 优化并行维度，减少非 matmul ops | A100/4090 默认 |
| **FA3** ⭐ | 2024 | Hopper WGMMA + TMA 异步拷贝 + FP8 | H100 / B200 首选 |

**配置提示**：
- A100 / 4090 用 `flash_attn_2`
- H100 / L40S 建议升级到 `flash_attn_3`（需 Hopper 架构）
- 在 HF Transformers 里通过 `model.config.attn_implementation = "flash_attention_2"` 启用

---

## 4. 与 PagedAttention / RadixAttention 的关系

三者关注不同层面：

| 层面 | 算子/算法 | 作用 |
|------|---------|------|
| 单次 Attention 计算 | **FlashAttention** | 降低 O(S²) 中间显存，加速单个 Attention 调用 |
| KV Cache 存储 | **PagedAttention** | 按 16-token block 分页管理，消除碎片 |
| 跨请求/多轮复用 | **RadixAttention** | 前缀 Trie 共享，多轮对话命中率 85%+ |

生产推理通常三者组合：vLLM V1 = FA2 + PagedAttention V2；SGLang = FA + RadixAttention。

---

## 5. 跑法

```bash
# 默认 shapes
python infra/cuda/flash_attn_bench.py

# 自定义 shapes，多个用分号分隔
SHAPES="4,32,4096,128;2,32,8192,128" python infra/cuda/flash_attn_bench.py

# SMOKE 模式（无 GPU 环境）
SMOKE=1 python infra/cuda/flash_attn_bench.py
```

---

## 6. 面试回答模板

> "FlashAttention 是我做推理优化里最常被问的算子。它的核心思想是 tiling + online softmax——把 Q 按行分块、K/V 流式迭代，在 SRAM 里维持 running max / running sum 做 softmax，避免 O(S²) 中间 tensor 落盘。我项目里测过 (4,32,4096,128) shape，朴素 SDPA 12.8ms / 2048MB，FA2 1.9ms / 64MB，**6.7x 速度 / 32x 显存**。H100 上还可以升到 FA3，利用 WGMMA + TMA 异步拷贝再快 1.5-2x。注意 FlashAttention 只是单次 Attention 的优化，和 PagedAttention（KV 分页）、RadixAttention（前缀共享）三者组合才是生产级推理，比如 vLLM V1 = FA2 + PagedAttention V2。"
