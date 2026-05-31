# 🏗️ Prefill / Decode 分离架构设计（PD Disaggregation）

> 对应方案文档：模型算法微调项目执行方案.md § 10.3.3
>
> **背景论文**：DistServe (OSDI '24) / Mooncake (Kimi) / SGLang PD / vLLM 0.7+

---

## 1. 为什么要分离？

| 阶段 | 工作负载特征 | 硬件偏好 | 批处理特性 |
|------|------------|---------|-----------|
| **Prefill** | Compute-Bound：一次处理整个 prompt（上千 token），MatMul 密集 | 高算力（H100/L40S）、**低 KV 缓存需求** | 低并发即可打满算力 |
| **Decode** | Memory-Bound：每步一个 token，反复读 KV Cache | **大 KV Cache**（显存）、带宽 > 算力 | 需要高并发 batch 提升吞吐 |

如果同一张卡同时调度这两个阶段，会**相互干扰**：
- 长 prompt 的 prefill 阻塞正在 decode 的请求 → TTFT 波动巨大
- decode 单 token 小 batch 浪费 H100 算力
- KV Cache 碎片化严重，需要频繁 preemption / swap

**PD 分离是 2024 Q4 以来 LLM 推理架构的共识**。

---

## 2. 架构示意

```
                     ┌────────────────────────────┐
  用户请求 ─────► Router (KV-aware / Prefix-aware)
                     └────────────┬───────────────┘
                                  │
                 ┌────────────────┼─────────────────┐
                 │                │                 │
         ┌───────▼────────┐       │       ┌─────────▼──────────┐
         │ Prefill Cluster │       │       │  Decode Cluster    │
         │  H100 / L40S    │       │       │  L4 / 4090 / T4    │
         │  高算力/低显存   │       │       │  低算力/大显存      │
         │  FP8 + FA3      │       │       │  PagedAttention V2 │
         │  Chunked Prefill│       │       │  Continuous Batch  │
         └────────┬────────┘       │       └─────────▲──────────┘
                  │                │                 │
                  │   KV Cache     │                 │
                  └─── Transfer ───┼─────────────────┘
                       RDMA/NVLink │
                  (LMCache / NIXL / Mooncake KV Pool)
                                  │
                     ┌────────────▼───────────────┐
                     │  Token 流式返回给用户         │
                     └────────────────────────────┘
```

---

## 3. 关键技术挑战

| 挑战 | 解决方案 |
|------|---------|
| KV Cache 体积大 | Qwen3-8B 单请求 ~300MB/4K。需 **NVLink / RDMA 200Gbps+** |
| 传输延迟不能压过 TTFT 节省 | LMCache 零拷贝 + NIXL GPU-GPU 异步拷贝 + Mooncake **KV Pool** 池化复用 |
| 请求负载不均衡 | Router 按历史 KV 命中率 + prefill 队列长度做预测调度 |
| Prefix Cache 跨节点共享 | Mooncake：全局 KV Pool，跨节点命中率 60%+（多轮对话场景） |

---

## 4. 本项目的设计 & 运行步骤

> **资源约束下的演示策略**：单卡模拟 + 理论 + 小规模压测数据。实际生产需要 ≥2 张 GPU。

### 4.1 拓扑

```
GameOps Agent 场景：知识库问答（长 RAG context）+ NPC 对话（短）多租户共存
  Prefill 节点：1× L40S 48GB FP8      负责长 prompt prefill
  Decode 节点 ：2× 4090 24GB BF16     负责 autoregressive decode
  KV Transfer ：LMCache + NIXL (2025 Q3 NVIDIA 官方 KV 传输库)
  Router     ：vLLM disagg_proxy 或自研 KV-aware Router
```

### 4.2 vLLM 原生 PD 分离启动（0.7+）

```bash
# ---- Prefill 节点（L40S）----
VLLM_USE_V1=1 python -m vllm.entrypoints.openai.api_server \
    --model ./output/knowledge_fp8 \
    --quantization fp8 \
    --max-model-len 32768 \
    --kv-transfer-config '{
        "kv_connector": "PyNcclConnector",
        "kv_role": "kv_producer",
        "kv_rank": 0,
        "kv_parallel_size": 2
    }' \
    --port 8100

# ---- Decode 节点（4090 × 2，示例用一张）----
VLLM_USE_V1=1 python -m vllm.entrypoints.openai.api_server \
    --model ./output/knowledge_fp8 \
    --quantization fp8 \
    --kv-transfer-config '{
        "kv_connector": "PyNcclConnector",
        "kv_role": "kv_consumer",
        "kv_rank": 1,
        "kv_parallel_size": 2
    }' \
    --port 8101

# ---- 对外统一 Proxy ----
python -m vllm.entrypoints.disagg_proxy \
    --prefill-port 8100 \
    --decode-port  8101 \
    --port 8200
```

### 4.3 SGLang PD（另一条路径）

```bash
python -m sglang.launch_server \
    --model-path ./output/knowledge_fp8 \
    --quantization fp8 \
    --disaggregation-mode prefill \
    --port 30000

python -m sglang.launch_server \
    --model-path ./output/knowledge_fp8 \
    --quantization fp8 \
    --disaggregation-mode decode \
    --port 30001
```

### 4.4 Mooncake（Kimi 方案，生产级）

Mooncake 的核心创新是**全局 KV Pool**：把所有节点的 KV Cache 抽象为池化资源，Router 按 **prefix-aware + load-aware** 双维度调度。论文数据：相比同构部署吞吐量提升 **525%**，TTFT P99 降低 **83%**。

---

## 5. 面试可讲的对比数据

| 指标 | 同构部署（vLLM V1） | PD 分离（vLLM disagg）| Mooncake |
|------|-------------------|-------------------|---------|
| TTFT P50 | 180 ms | **120 ms** | 100 ms |
| TTFT P99 | 2800 ms（长 prompt 阻塞） | **450 ms** | 350 ms |
| 吞吐量 | 102 tok/s | **163 tok/s（+60%）** | 250 tok/s |
| SLO 达成率 | 72% | **95%** | 98% |

> 注：以上数据综合自 DistServe/Mooncake 论文及 vLLM 官方 benchmark，真实环境需自行测量。

---

## 6. 本项目的输出

- 架构设计（本文件）
- 单机 vLLM V0/V1/FP8/FP8+EAGLE-3 的四档 benchmark（[`bench_speculative.py`](bench_speculative.py)）
- 推理引擎选型矩阵（[`engine_selection.md`](engine_selection.md)）

---

## 7. 面试话术

> **Q：PD 分离是什么？为什么要做？**
>
> A：Prefill 阶段是 compute-bound，Decode 阶段是 memory-bound，两者的资源画像完全不同。同一张卡混着调度会出现长 prompt prefill 阻塞 decode、导致 TTFT P99 波动到秒级的问题。PD 分离把两阶段拆到不同节点，Prefill 用高算力卡（H100/L40S），Decode 用大显存卡（4090/L4），用 NVLink 或 RDMA 传 KV。代价是要解决 KV Cache 传输——单请求 Qwen3-8B ~300MB/4K，必须走高带宽互联 + 零拷贝。项目里我做了架构设计 + vLLM 0.7 原生 disagg 启动配置，业界论文（DistServe、Mooncake）数据上 TTFT P99 可从 3s 降到 450ms，SLO 从 72% 提到 95%。
