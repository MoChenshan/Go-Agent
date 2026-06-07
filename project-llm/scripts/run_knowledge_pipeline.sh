#!/usr/bin/env bash
# =============================================================================
# run_knowledge_pipeline.sh —— 方向一（知识库）端到端流水线
#
# 步骤：
#   1. 数据合成         : generate_qa.py    (raw/wiki → processed/knowledge_qa.json)
#   2. 数据质量过滤     : data_quality.py   (knowledge_qa → knowledge_qa_filtered.json)
#   3. 格式化           : format_data.py    (→ train_alpaca.json)
#   4. QLoRA-SFT 训练   : llamafactory-cli  (configs/knowledge_sft.yaml)
#   5. 合并 LoRA        : llamafactory-cli  (output/knowledge_sft_merged/)
#   6. 评估             : evaluate.py       (→ eval/knowledge_eval_report.md)
#
# 环境变量（可覆盖默认行为）：
#   PROVIDER        数据合成 provider（默认 deepseek）
#   JUDGE_PROVIDER  LLM Judge provider（默认 moonshot）
#   SKIP_QA         1 = 跳过合成（直接用已有数据）
#   SKIP_TRAIN      1 = 跳过训练（只评估）
#   SMOKE           1 = smoke 模式：只跑 2 chunk，用于验证链路
#   DISABLE_JUDGE   1 = 关闭 LLM Judge
#
# 使用：
#   bash scripts/run_knowledge_pipeline.sh
#   SMOKE=1 bash scripts/run_knowledge_pipeline.sh   # 快速走通
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PROVIDER="${PROVIDER:-deepseek}"
JUDGE_PROVIDER="${JUDGE_PROVIDER:-moonshot}"
SMOKE="${SMOKE:-0}"

RAW_DIR="data/raw/wiki_docs"
QA_RAW="data/processed/knowledge_qa.json"
QA_FILT="data/processed/knowledge_qa_filtered.json"
TRAIN_ALPACA="data/processed/knowledge_qa.json"   # 与 dataset_info.json 对齐
TEST_SET="data/test/knowledge_test.json"

REPORT_DQ="eval/data_quality_report.md"
REPORT_EVAL="eval/knowledge_eval_report.md"

MAX_CHUNKS=0
N_PER_CHUNK=8
if [[ "$SMOKE" == "1" ]]; then
    MAX_CHUNKS=2
    N_PER_CHUNK=3
    echo "[smoke] max_chunks=$MAX_CHUNKS  n_per_chunk=$N_PER_CHUNK"
fi

# -----------------------------------------------------------------------------
# Step 1. QA 合成
# -----------------------------------------------------------------------------
if [[ "${SKIP_QA:-0}" != "1" ]]; then
    echo ""
    echo "========== [1/6] 数据合成 (generate_qa.py) =========="
    python scripts/generate_qa.py \
        --input  "$RAW_DIR" \
        --output "$QA_RAW" \
        --provider "$PROVIDER" \
        --n_per_chunk "$N_PER_CHUNK" \
        --max_chunks "$MAX_CHUNKS"
else
    echo "[skip] generate_qa (SKIP_QA=1)"
fi

# -----------------------------------------------------------------------------
# Step 2. 质量过滤
# -----------------------------------------------------------------------------
echo ""
echo "========== [2/6] 数据质量过滤 (data_quality.py) =========="
JUDGE_FLAG=1
if [[ "${DISABLE_JUDGE:-0}" == "1" || "$SMOKE" == "1" ]]; then
    JUDGE_FLAG=0
fi
python scripts/data_quality.py \
    --input  "$QA_RAW" \
    --output "$QA_FILT" \
    --report "$REPORT_DQ" \
    --judge_provider "$JUDGE_PROVIDER" \
    --enable_judge "$JUDGE_FLAG" \
    --enable_ragas 0

# -----------------------------------------------------------------------------
# Step 3. 格式化（QA → Alpaca，dataset_info.json 已配好 alpaca schema）
# -----------------------------------------------------------------------------
echo ""
echo "========== [3/6] 格式化 (format_data.py) =========="
# 将 {question, answer} 重命名为 {instruction, output}，供 LLaMAFactory alpaca 模板读取
python - <<PY
import json
src = json.load(open("$QA_FILT", "r", encoding="utf-8"))
dst = [{"instruction": x["question"], "input": "", "output": x["answer"]} for x in src
       if x.get("question") and x.get("answer")]
json.dump(dst, open("$TRAIN_ALPACA", "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print(f"[format] {len(dst)} alpaca items -> $TRAIN_ALPACA")
PY

# -----------------------------------------------------------------------------
# Step 4. QLoRA-SFT 训练
# -----------------------------------------------------------------------------
if [[ "${SKIP_TRAIN:-0}" != "1" ]]; then
    echo ""
    echo "========== [4/6] 训练 (llamafactory-cli train knowledge_sft.yaml) =========="
    llamafactory-cli train configs/knowledge_sft.yaml
else
    echo "[skip] training (SKIP_TRAIN=1)"
fi

# -----------------------------------------------------------------------------
# Step 5. 合并 LoRA
# -----------------------------------------------------------------------------
if [[ "${SKIP_TRAIN:-0}" != "1" ]]; then
    echo ""
    echo "========== [5/6] 合并 LoRA (llamafactory-cli export) =========="
    llamafactory-cli export \
        --model_name_or_path Qwen/Qwen3-8B \
        --adapter_name_or_path ./output/knowledge_sft \
        --export_dir ./output/knowledge_sft_merged \
        --finetuning_type lora \
        --template qwen3 \
        --trust_remote_code
fi

# -----------------------------------------------------------------------------
# Step 6. 评估
# -----------------------------------------------------------------------------
echo ""
echo "========== [6/6] 评估 (evaluate.py) =========="
MODEL_PATH="${MODEL_PATH:-./output/knowledge_sft_merged}"
python scripts/evaluate.py \
    --model_path "$MODEL_PATH" \
    --test_set "$TEST_SET" \
    --report "$REPORT_EVAL" \
    --mode knowledge \
    --engine "${EVAL_ENGINE:-hf}" \
    --max_samples "${EVAL_MAX_SAMPLES:-20}"

echo ""
echo "========== ✅ 方向一流水线完成 =========="
echo "数据质量报告: $REPORT_DQ"
echo "模型评估报告: $REPORT_EVAL"
