#!/usr/bin/env bash
# vLLM v1 + LoRA 多租户启动脚本
#
# 一份基座模型 + 多个 LoRA adapter 同进程加载，按请求 query 参数选择 adapter，
# 显存占用 ~ 基座(FP16/AWQ) + 每个 adapter（数十 MB），适合：
#   - 同模型多业务（NPC / 客服 / 内部知识 QA 各一份 LoRA）
#   - A/B 测试（v1 / v2 LoRA 对比）
#   - 灰度回滚（保留旧 adapter，新流量切到新 adapter）
#
# 环境变量：
#   BASE_MODEL          基座模型路径（默认：/ckpt/serve_awq）
#   PORT                监听端口（默认 8000）
#   GPU_MEM_UTIL        GPU 显存占用上限（默认 0.85）
#   MAX_LORAS           同时加载 LoRA 数量（默认 8）
#   MAX_LORA_RANK       LoRA rank 上限（默认 64）
#   MAX_MODEL_LEN       上下文长度（默认 32768）
#   LORA_DIR            LoRA adapter 根目录（默认 /ckpt/loras）
#
# LoRA 目录结构（每个子目录 = 一个 adapter，目录名即 adapter id）：
#   /ckpt/loras/
#     ├── npc_v2/        adapter_config.json + adapter_model.safetensors
#     ├── ops_v1/
#     └── customer_v1/

set -euo pipefail

BASE_MODEL="${BASE_MODEL:-/ckpt/serve_awq}"
PORT="${PORT:-8000}"
GPU_MEM_UTIL="${GPU_MEM_UTIL:-0.85}"
MAX_LORAS="${MAX_LORAS:-8}"
MAX_LORA_RANK="${MAX_LORA_RANK:-64}"
MAX_MODEL_LEN="${MAX_MODEL_LEN:-32768}"
LORA_DIR="${LORA_DIR:-/ckpt/loras}"

if [[ ! -d "${BASE_MODEL}" ]]; then
  echo "ERROR: BASE_MODEL not found: ${BASE_MODEL}" >&2
  exit 2
fi
if [[ ! -d "${LORA_DIR}" ]]; then
  echo "WARN: LORA_DIR not found: ${LORA_DIR}（将以无 adapter 模式启动）" >&2
fi

# 收集所有 adapter 目录
LORA_MODULES=()
if [[ -d "${LORA_DIR}" ]]; then
  while IFS= read -r d; do
    name="$(basename "${d}")"
    LORA_MODULES+=("${name}=${d}")
  done < <(find "${LORA_DIR}" -mindepth 1 -maxdepth 1 -type d | sort)
fi

echo "===== vLLM LoRA Multi-Tenant ====="
echo "base_model:      ${BASE_MODEL}"
echo "port:            ${PORT}"
echo "max_loras:       ${MAX_LORAS}"
echo "max_lora_rank:   ${MAX_LORA_RANK}"
echo "loras (count=${#LORA_MODULES[@]}):"
for m in "${LORA_MODULES[@]:-}"; do echo "  - ${m}"; done
echo "================================="

ARGS=(
  --host 0.0.0.0
  --port "${PORT}"
  --model "${BASE_MODEL}"
  --max-model-len "${MAX_MODEL_LEN}"
  --gpu-memory-utilization "${GPU_MEM_UTIL}"
  --enable-lora
  --max-loras "${MAX_LORAS}"
  --max-lora-rank "${MAX_LORA_RANK}"
  --enforce-eager false
  # vLLM v1：默认开启 chunked prefill；EAGLE-3 投机解码可叠加
)

if [[ ${#LORA_MODULES[@]} -gt 0 ]]; then
  ARGS+=( --lora-modules "${LORA_MODULES[@]}" )
fi

exec python -m vllm.entrypoints.openai.api_server "${ARGS[@]}"
