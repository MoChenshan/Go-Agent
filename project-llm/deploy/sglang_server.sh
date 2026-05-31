#!/usr/bin/env bash
# =============================================================================
# SGLang 部署 —— 多轮对话场景（RadixAttention 前缀复用）
# 适用：知识库 Agent 多轮对话 / NPC 多轮剧情对话
# =============================================================================
set -euo pipefail

MODEL_PATH="${MODEL_PATH:-./output/knowledge_fp8}"
PORT="${PORT:-30000}"
TP_SIZE="${TP_SIZE:-1}"

# 启用 EAGLE-3 投机解码（sglang >= 0.4.0）
SPEC_ARGS=(--speculative-algorithm EAGLE3
           --speculative-draft-model-path yuhuili/EAGLE3-Qwen2.5-8B
           --speculative-num-steps 5
           --speculative-eagle-topk 8
           --speculative-num-draft-tokens 32)

python -m sglang.launch_server \
  --model-path "$MODEL_PATH" \
  --port "$PORT" \
  --tp "$TP_SIZE" \
  --enable-radix-cache \
  --mem-fraction-static 0.85 \
  --context-length 32768 \
  --served-model-name knowledge-expert \
  --trust-remote-code \
  "${SPEC_ARGS[@]}"
