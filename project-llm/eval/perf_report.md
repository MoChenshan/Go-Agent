# 🚀 推理性能 Benchmark Report

> 本文档记录方向一（知识库专家）模型的**推理性能对比**结果。
> 由 `scripts/benchmark_serving.py` 自动追加结果行；手工填写"实机配置"一栏。

---

## 📋 实机配置

| 项 | 值 |
|---|---|
| GPU | ⬜ H100 80GB / L40S 48GB / A100 80GB / 4090 24GB |
| CPU | ⬜ |
| vLLM 版本 | 0.7.x（V1 引擎） |
| 模型 | Qwen3-8B（SFT 合并后） |
| 上下文长度 | 32768 |
| 测试数据 | `data/test/knowledge_test.json` |
| 压测并发 | 16 |
| 请求数 | 200 |

---

## 📊 四档对比（用 benchmark_serving.py 自动生成）

<!-- 下方表格由 scripts/benchmark_serving.py 自动 append -->

---

## 🎯 预期结论（填完数据后改写）

| 维度 | baseline (BF16) | FP8 | GPTQ-Marlin | FP8 + EAGLE-3 |
|------|----------------|-----|-------------|---------------|
| 吞吐 (tok/s) | 1.00× | ~1.60× | ~2.20× | **~3.15×** |
| TTFT | 100% | 85% | 75% | **60%** |
| 显存占用 | 100% | 55% | 30% | 32% |
| 精度损失 (G-Eval) | 0 | < 1% | ~2% | < 1% |

**最终推荐**：
- 🏆 **H100 / L40S** → FP8 + EAGLE-3（吞吐最高）
- 🥈 **A100 / 4090** → GPTQ-Marlin + EAGLE-3（性价比最高）
- 🥉 成本敏感场景 → GPTQ-Marlin（单卡可部署）

---

## 🧪 复现脚本

```bash
cd project-llm

# 1) 先量化（二选一或全跑）
python scripts/quantize_fp8.py \
    --model ./output/knowledge_sft_merged \
    --output ./output/knowledge_fp8

python scripts/quantize_gptq_marlin.py \
    --model ./output/knowledge_sft_merged \
    --output ./output/knowledge_gptq_marlin \
    --calib_dataset ./data/processed/knowledge_qa.json

# 2) 四档 serve + benchmark
bash scripts/run_perf_benchmark.sh
```

---

## 🧠 深度分析点（面试讲解要点）

### 1. FP8 vs INT4 如何选？
- FP8 精度损失 <1%，但仅 Hopper / Ada 架构支持 tensor core
- INT4 (GPTQ-Marlin) 吞吐更高、显存更省，但 PPL 会上升 ~2%
- **决策**：业务对首 Token 延迟敏感选 FP8；对吞吐敏感选 GPTQ-Marlin

### 2. EAGLE-3 为什么能 +75%？
- 把 "N 次 target forward" 压缩为 "1 次 target + 1 次 draft forward"
- draft 模型仅 target 的 1~3% 参数量，几乎免费
- 典型 accept_rate ~70% 情况下，单次可多吐出 3~4 个 token

### 3. 为什么需要 chunked prefill？
- V0 的 prefill 与 decode 互相阻塞，长 prompt 会阻塞所有 decode
- V1 的 chunked prefill 把长 prompt 切块，允许 prefill/decode 交错
- 效果：高并发 + 长上下文场景下 TTFT p95 下降 30~50%
