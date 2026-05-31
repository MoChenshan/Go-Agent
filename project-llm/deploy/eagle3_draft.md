# EAGLE-3 投机解码接入指引

## 📘 背景
**EAGLE-3**（Extrapolation Algorithm for Greater Language-model Efficiency）是 2025 年最主流的**投机解码**（Speculative Decoding）算法，通过一个轻量 draft 模型预测多个候选 token，target 模型一次性 verify，实现 **1.5~2×** 吞吐提升且精度无损。

vLLM V1（0.7+）**原生支持** EAGLE-3，无需修改推理代码。

---

## 🧩 工作原理

```
传统解码：  [prompt] → target → tok1 → target → tok2 → target → tok3 → ...
            每个 token 都要跑一次 target 模型（最贵的那个）

EAGLE-3：   [prompt] → target → tok1 
                    → draft → [tok2, tok3, tok4, tok5, tok6]（draft 一次性出 5 个）
                    → target verify → accept k∈[0,5] 个
                    → 下一轮从 tok(1+k) 继续
            draft 模型仅为 target 的 1~3% 参数量，延迟几乎可忽略
```

**关键指标**：
- **接受率**（accept rate）：越高越省算力，EAGLE-3 典型 ~70%
- **draft token 数**：vLLM 参数 `num_speculative_tokens`，默认 5

---

## 🚀 在 vLLM V1 接入

### 方式一：使用社区已训练的 draft 模型（最快路径）

Qwen3 系列已有现成 draft：
```bash
# Qwen3-8B 对应的 draft
yuhuili/EAGLE3-Qwen3-8B

# Qwen3-4B / Qwen2.5 系列同理（HuggingFace 搜 EAGLE3-*）
```

启动命令（已封装在 [deploy/vllm_v1_server.sh](vllm_v1_server.sh)）：
```bash
PROFILE=fp8_eagle3 \
SPEC_MODEL=yuhuili/EAGLE3-Qwen3-8B \
NUM_SPEC_TOKENS=5 \
bash deploy/vllm_v1_server.sh
```

### 方式二：自训 draft 模型（显著提升接受率）

当自己 SFT/DPO 后的 target 模型与社区 draft 分布不匹配时，建议自训：

```bash
# 使用 EAGLE 官方仓库
git clone https://github.com/SafeAILab/EAGLE.git
cd EAGLE

# 准备与 target 模型分布一致的数据（复用 SFT 数据即可）
python -m eagle.train.main_eagle3 \
    --basepath ./output/knowledge_sft_merged \
    --configpath configs/Qwen3-8B-eagle3.json \
    --datapath ../project-llm/data/processed/knowledge_qa.json \
    --bs 4 --lr 3e-5 --gradient_accumulation_steps 16
```

训练产物（`eagle3_qwen3_8b/`）就可以替换 `SPEC_MODEL` 了。

---

## 📊 调参指南

| 参数 | 推荐值 | 影响 |
|-----|-------|------|
| `num_speculative_tokens` | **5**（默认） | 过大→浪费 draft 算力；过小→加速不明显 |
| `draft_tensor_parallel_size` | 1（通常） | draft 很小，一般无需切分 |
| `speculative_disable_by_batch_size` | 8 | 高并发时关闭 draft（target 已饱和） |

## 🧪 验证接受率

```python
# vLLM V1 提供 /metrics 端点，可通过 prometheus 采集
curl http://localhost:8000/metrics | grep spec_decode_
# spec_decode_num_accepted_tokens_total
# spec_decode_num_draft_tokens_total
# accept_rate = accepted / draft
```

目标：**accept_rate ≥ 0.65**，否则建议自训 draft。

---

## ⚠️ 常见问题

1. **`Speculative decoding not supported for this model`**
   → vLLM < 0.7.0 不支持 EAGLE-3，升级到 0.7+

2. **`accept_rate` 持续 < 50%**
   → 自训后的 target 与通用 draft 分布差异大，需要自训 draft

3. **同时开启 chunked prefill + EAGLE-3 OOM**
   → 降低 `--gpu-memory-utilization` 至 0.85，或减小 `max-model-len`

4. **首 Token 延迟（TTFT）反而变高**
   → 如果 prompt 短且并发低，draft 模型的加载开销可能抵消收益，
      建议在长对话/高并发场景使用（~128 tokens 起）

---

## 📎 参考
- [EAGLE-3 论文](https://arxiv.org/abs/2503.01840)
- [vLLM Spec Decoding 文档](https://docs.vllm.ai/en/latest/features/spec_decode.html)
- [SafeAI Lab / EAGLE 官方仓库](https://github.com/SafeAILab/EAGLE)
