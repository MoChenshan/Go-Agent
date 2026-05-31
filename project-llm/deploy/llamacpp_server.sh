#!/usr/bin/env bash
# =============================================================================
# llama.cpp CPU 部署 —— NPC 模型（GGUF 量化版）
# 适用：服务器 CPU（含 AMX 指令集） / 云主机 无 GPU 场景
# =============================================================================
set -euo pipefail

LLAMA_CPP_DIR="${LLAMA_CPP_DIR:-$HOME/llama.cpp}"
MODEL_GGUF="${MODEL_GGUF:-./output/npc_gguf/npc-4b-q4_k_m.gguf}"
PORT="${PORT:-8080}"
THREADS="${THREADS:-16}"           # 建议 = 物理核数
CTX_SIZE="${CTX_SIZE:-8192}"

# AMX 指令集（Intel Sapphire Rapids / Emerald Rapids）
export GGML_AMX=1

"$LLAMA_CPP_DIR/build/bin/llama-server" \
  --model "$MODEL_GGUF" \
  --host 0.0.0.0 \
  --port "$PORT" \
  --threads "$THREADS" \
  --ctx-size "$CTX_SIZE" \
  --parallel 4 \
  --cont-batching \
  --metrics \
  --jinja                          # 使用模型自带 chat template（Qwen3）
