# eval/ 评估报告目录

本目录用于存放训练后模型的评估结果。文件以 Markdown 为主，便于面试直接展示。

## 预期产出

| 文件 | 内容 | 对应阶段 |
|------|------|---------|
| `knowledge_eval_report.md` | 知识库模型 G-Eval + RAGAS 评估 | 方向一 W2 |
| `npc_eval_report.md` | NPC 模型角色一致性 / 端侧性能评估 | 方向二 W3 |
| `dpo_vs_grpo_report.md` | DPO vs GRPO 对比实验（核心亮点） | 方向二 W4 |
| `inference_perf_report.md` | 多平台推理性能（vLLM V1 / llama.cpp / QNN） | W4 |

## 评估三件套（2026 标配）

1. **G-Eval**（`deepeval>=2.0.0`）—— LLM-as-Judge 自动化打分
2. **RAGAS**（`ragas>=0.2.0`）—— Faithfulness / Answer Relevance / Context Precision
3. **Langfuse**（`langfuse>=2.60.0`）—— 在线 Trace + 评估 Dashboard

由 `scripts/evaluate.py` 统一驱动输出。
