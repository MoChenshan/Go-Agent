#!/usr/bin/env bash
# =============================================================================
# run_npc_pipeline.sh —— 方向二（AINPC）端到端流水线
#
# 四阶段：
#   1. 对话合成       : generate_dialogue.py  (profiles + world → npc_sft.json)
#   2. SFT 训练       : llamafactory-cli train configs/npc_sft.yaml
#   3. SFT 合并       : llamafactory-cli export → output/npc_sft_merged
#   4a. DPO 分支      : generate_preference.py → 训练 configs/npc_dpo.yaml
#   4b. GRPO 分支     : 训练 configs/npc_grpo.yaml (共享 SFT merged)
#   5. 对比评估       : evaluate.py 分别评估 sft / dpo / grpo
#
# 环境变量：
#   GEN_PROVIDER      合成/生成 provider（默认 moonshot）
#   JUDGE_PROVIDER    Judge provider（默认 openai）
#   SMOKE             1 = smoke 模式（每角色 1 条对话，跳过训练仅跑数据链路）
#   SKIP_SFT          1 = 跳过 SFT 阶段
#   SKIP_DPO          1 = 跳过 DPO 分支
#   SKIP_GRPO         1 = 跳过 GRPO 分支
#
# 使用：
#   bash scripts/run_npc_pipeline.sh                  # 完整
#   SMOKE=1 bash scripts/run_npc_pipeline.sh          # 数据链路 smoke test
#   SKIP_GRPO=1 bash scripts/run_npc_pipeline.sh      # 只 SFT + DPO
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GEN_PROVIDER="${GEN_PROVIDER:-moonshot}"
JUDGE_PROVIDER="${JUDGE_PROVIDER:-openai}"
SMOKE="${SMOKE:-0}"

# --- 产物路径 ---
PROFILES="data/raw/npc_profiles.json"
WORLD="data/raw/world_setting.md"
SFT_JSON="data/processed/npc_dialogues.json"    # 与 dataset_info.json 对齐
DPO_JSON="data/processed/npc_dpo.json"
TEST_SET="data/test/npc_test.json"

SFT_DIR="./output/npc_sft"
SFT_MERGED="./output/npc_sft_merged"
DPO_DIR="./output/npc_dpo"
GRPO_DIR="./output/npc_grpo"

REPORT_SFT="eval/npc_sft_report.md"
REPORT_DPO="eval/npc_dpo_report.md"
REPORT_GRPO="eval/npc_grpo_report.md"

MAX_PAIRS=0
N_PER_PAIR=2
INCLUDE_THINKING=1
INCLUDE_CONTROL=1
if [[ "$SMOKE" == "1" ]]; then
    MAX_PAIRS=6
    N_PER_PAIR=1
    INCLUDE_THINKING=0
    echo "[smoke] max_pairs=$MAX_PAIRS  n_per_pair=$N_PER_PAIR"
fi

# -----------------------------------------------------------------------------
# Step 1. 对话合成（ShareGPT 格式）
# -----------------------------------------------------------------------------
if [[ "${SKIP_DIALOGUE:-0}" != "1" ]]; then
    echo ""
    echo "========== [1] NPC 对话合成 (generate_dialogue.py) =========="
    python scripts/generate_dialogue.py \
        --profiles "$PROFILES" \
        --world "$WORLD" \
        --output "$SFT_JSON" \
        --provider "$GEN_PROVIDER" \
        --n_per_pair "$N_PER_PAIR" \
        --include_control "$INCLUDE_CONTROL" \
        --include_thinking "$INCLUDE_THINKING" \
        --max_pairs "$MAX_PAIRS"
else
    echo "[skip] generate_dialogue (SKIP_DIALOGUE=1)"
fi

# -----------------------------------------------------------------------------
# Step 2. SFT 训练
# -----------------------------------------------------------------------------
if [[ "${SKIP_SFT:-0}" != "1" && "$SMOKE" != "1" ]]; then
    echo ""
    echo "========== [2] NPC SFT (QLoRA) =========="
    llamafactory-cli train configs/npc_sft.yaml

    echo ""
    echo "========== [3] 合并 SFT LoRA =========="
    llamafactory-cli export \
        --model_name_or_path Qwen/Qwen3-4B \
        --adapter_name_or_path "$SFT_DIR" \
        --export_dir "$SFT_MERGED" \
        --finetuning_type lora \
        --trust_remote_code
else
    echo "[skip] SFT (SMOKE 或 SKIP_SFT=1)"
fi

# -----------------------------------------------------------------------------
# Step 4a. DPO 分支
# -----------------------------------------------------------------------------
if [[ "${SKIP_DPO:-0}" != "1" ]]; then
    echo ""
    echo "========== [4a] 构造 DPO 偏好对 =========="
    python scripts/generate_preference.py \
        --sft_data "$SFT_JSON" \
        --output "$DPO_JSON" \
        --gen_provider "$GEN_PROVIDER" \
        --judge_provider "$JUDGE_PROVIDER" \
        --max_samples "${DPO_MAX_SAMPLES:-200}"

    if [[ "$SMOKE" != "1" ]]; then
        echo ""
        echo "========== [4a] DPO 训练 =========="
        llamafactory-cli train configs/npc_dpo.yaml
    fi
else
    echo "[skip] DPO (SKIP_DPO=1)"
fi

# -----------------------------------------------------------------------------
# Step 4b. GRPO 分支（面试亮点）
# -----------------------------------------------------------------------------
if [[ "${SKIP_GRPO:-0}" != "1" && "$SMOKE" != "1" ]]; then
    echo ""
    echo "========== [4b] GRPO 训练 (DeepSeek-R1 同款 RL) =========="
    # reward 函数通过 scripts/grpo_rewards.py 暴露；确保 PYTHONPATH 能导入
    export PYTHONPATH="$ROOT/scripts:${PYTHONPATH:-}"
    llamafactory-cli train configs/npc_grpo.yaml
else
    echo "[skip] GRPO (SMOKE 或 SKIP_GRPO=1)"
fi

# -----------------------------------------------------------------------------
# Step 5. 三路对比评估
# -----------------------------------------------------------------------------
if [[ "${SKIP_EVAL:-0}" != "1" && "$SMOKE" != "1" ]]; then
    echo ""
    echo "========== [5] 三路对比评估 =========="
    ENGINE="${EVAL_ENGINE:-hf}"
    MAX_SAMPLES="${EVAL_MAX_SAMPLES:-20}"

    if [[ -d "$SFT_MERGED" ]]; then
        python scripts/evaluate.py --model_path "$SFT_MERGED" \
            --test_set "$TEST_SET" --report "$REPORT_SFT" \
            --mode npc --engine "$ENGINE" --max_samples "$MAX_SAMPLES"
    fi
    if [[ -d "$DPO_DIR" ]]; then
        python scripts/evaluate.py --model_path "$DPO_DIR" \
            --test_set "$TEST_SET" --report "$REPORT_DPO" \
            --mode npc --engine "$ENGINE" --max_samples "$MAX_SAMPLES"
    fi
    if [[ -d "$GRPO_DIR" ]]; then
        python scripts/evaluate.py --model_path "$GRPO_DIR" \
            --test_set "$TEST_SET" --report "$REPORT_GRPO" \
            --mode npc --engine "$ENGINE" --max_samples "$MAX_SAMPLES" \
            --enable_thinking
    fi
fi

echo ""
echo "========== ✅ 方向二流水线完成 =========="
echo "SFT   报告: $REPORT_SFT"
echo "DPO   报告: $REPORT_DPO"
echo "GRPO  报告: $REPORT_GRPO"
