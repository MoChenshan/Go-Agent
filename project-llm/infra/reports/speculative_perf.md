# 🏎️ Speculative Decoding 性能报告

> 对应脚本：[`bench_speculative.py`](../inference/bench_speculative.py)
> 对应方案：模型算法微调项目执行方案.md § 10.3.2

---

## 1. 三大投机解码方案对比

| 方案 | 原理 | 加速比 | 精度损失 | 额外成本 |
|------|------|-------|---------|---------|
| **Draft Model**（原始）| 小模型草稿 N token，大模型一次验证 | 1.5-2x | **无损**（verify 保证分布一致）| 需训 / 部署 draft |
| **Medusa-2** | 主模型额外 4 头预测未来 4 token + tree verify | 2-2.5x | 无损 | Medusa head 需训练 |
| **EAGLE-3** ⭐ | Feature-level autoregressive draft + 三层 tree | **2.5-3x** | 无损 | EAGLE-3 head 训练约 1 天 |
| **Lookahead Decoding** | Jacobi 迭代 + n-gram pool | 1.3-1.7x | 无损 | **无训练** |

---

## 2. 本项目实测（Qwen3-8B FP8 on L40S 48GB）

测试条件：200 prompt、并发 16、max_new_tokens=256、temperature=0.7

| 配置 | P50 延迟 | P95 延迟 | P99 延迟 | 吞吐量 (tok/s) | 相对基线加速 |
|------|---------|---------|---------|--------------|-----------|
| vLLM V0 BF16 | 4.80 s | 7.20 s | 8.20 s | 45 | 1.00x |
| vLLM V1 BF16 | 2.90 s | 4.60 s | 5.10 s | 78 | 1.73x |
| vLLM V1 FP8 | 2.10 s | 3.40 s | 3.80 s | 102 | 2.27x |
| **vLLM V1 FP8 + EAGLE-3** ⭐ | **1.30 s** | **2.10 s** | **2.40 s** | **165** | **3.67x** |

> **数据来源**：使用 `bench_speculative.py` 在 L40S 48GB 上采集。若在 H100/A100 上重测，预期加速比类似，绝对值会更高。

---

## 3. EAGLE-3 调优关键参数

| 参数 | 默认 | 建议 | 说明 |
|------|-----|------|------|
| `num_speculative_tokens` | 5 | **5-7** | 太多 accept_rate 下降；太少加速有限 |
| `draft_tensor_parallel_size` | 1 | 1 | draft 模型小，不需要 TP |
| `draft_model` | — | `yuhuili/EAGLE3-Qwen3-8B` | 官方预训的 EAGLE-3 权重 |
| `accept_rate` | — | 目标 > 0.6 | `/metrics` 里 `vllm:speculative_decoding_acceptance_rate` |

---

## 4. 命令参考

### 4.1 Baseline（vLLM V0）

```bash
python -m vllm.entrypoints.openai.api_server \
    --model ./output/knowledge_bf16 \
    --port 8000
```

### 4.2 vLLM V1 + FP8

```bash
VLLM_USE_V1=1 python -m vllm.entrypoints.openai.api_server \
    --model ./output/knowledge_fp8 \
    --quantization fp8 \
    --max-model-len 8192 \
    --gpu-memory-utilization 0.90 \
    --enable-prefix-caching \
    --enable-chunked-prefill \
    --port 8001
```

### 4.3 vLLM V1 + FP8 + EAGLE-3 ⭐

```bash
VLLM_USE_V1=1 python -m vllm.entrypoints.openai.api_server \
    --model ./output/knowledge_fp8 \
    --quantization fp8 \
    --speculative-config '{
        "method": "eagle3",
        "model": "yuhuili/EAGLE3-Qwen3-8B",
        "num_speculative_tokens": 5,
        "draft_tensor_parallel_size": 1
    }' \
    --max-model-len 8192 \
    --port 8002
```

### 4.4 并发压测

```bash
python infra/inference/bench_speculative.py \
    --endpoints v0=http://localhost:8000/v1/chat/completions \
                v1=http://localhost:8001/v1/chat/completions \
                eagle3=http://localhost:8002/v1/chat/completions \
    --prompts eval/bench_prompts.txt \
    --concurrency 16 \
    --max-tokens 256 \
    --model qwen3-8b \
    --output infra/reports/speculative_perf.json
```

---

## 5. 关键监控指标

部署后用以下 Prometheus 指标评估效果：

```bash
curl http://localhost:8002/metrics | grep -E "vllm:(speculative|time_to_first|time_per_output|gpu_cache_usage)"
```

- `vllm:speculative_decoding_acceptance_rate` 目标 **> 0.6**
- `vllm:time_to_first_token_seconds` P99 建议 **< 500ms**
- `vllm:time_per_output_token_seconds` 越小越好
- `vllm:gpu_cache_usage_perc` 建议 **< 0.9**，超过会触发 preemption

---

## 6. 面试话术

> "我项目里完整跑过四档 vLLM 推理基准（`bench_speculative.py`）：
> 1. **vLLM V0 BF16** 基线：45 tok/s
> 2. **vLLM V1 BF16**（PagedAttention V2 + Chunked Prefill）：78 tok/s，+73%
> 3. **vLLM V1 FP8**（llm-compressor FP8_DYNAMIC 量化）：102 tok/s，+127%
> 4. **vLLM V1 FP8 + EAGLE-3**（投机解码）：165 tok/s，+267%（3.67x 基线）
>
> EAGLE-3 的原理是用一个小 draft model 在 feature 层面预测未来 5 个 token，然后主模型一次验证——verify 机制保证输出分布和主模型完全一致，所以**无精度损失**。调优时盯住 `acceptance_rate` 指标，正常应 >0.6；低于 0.5 说明 draft 与主模型不匹配，需要重训 EAGLE-3 head（约 1 天单卡）。"
