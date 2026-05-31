#!/usr/bin/env bash
# =============================================================================
# vLLM V1 引擎 + EAGLE-3 投机解码 + FP8/GPTQ-Marlin 多量化部署
#
# 要求：
#   vllm >= 0.7.0   (V1 引擎默认启用，EAGLE-3 原生支持)
#   GPU：H100 / H200 / L40S（FP8）或 A100 / 4090（GPTQ-Marlin）
#
# 支持 4 种启动档位（通过 PROFILE 环境变量切换）：
#   bf16          : baseline（无量化、无投机解码），用于对比基线
#   fp8           : FP8 E4M3 量化（推荐 H100/L40S）
#   gptq_marlin   : GPTQ-Marlin INT4（推荐 A100/4090）
#   fp8_eagle3    : FP8 + EAGLE-3 投机解码（面试亮点档，吞吐最高）
#
# 使用：
#   bash deploy/vllm_v1_server.sh                         # 默认 fp8_eagle3
#   PROFILE=bf16 bash deploy/vllm_v1_server.sh            # baseline
#   PROFILE=gptq_marlin MODEL_PATH=./output/knowledge_gptq_marlin bash deploy/vllm_v1_server.sh
# =============================================================================
set -euo pipefail

# ---- 基础参数 ----
PROFILE="${PROFILE:-fp8_eagle3}"
PORT="${PORT:-8000}"
TP_SIZE="${TP_SIZE:-1}"
MAX_MODEL_LEN="${MAX_MODEL_LEN:-32768}"
GPU_UTIL="${GPU_UTIL:-0.90}"
SERVED_NAME="${SERVED_NAME:-knowledge-expert}"

# ---- V1 引擎全局开关 ----
export VLLM_USE_V1=1
export VLLM_ATTENTION_BACKEND="${VLLM_ATTENTION_BACKEND:-FLASH_ATTN}"
# Hopper 架构可尝试 FA3：export VLLM_ATTENTION_BACKEND=FLASHINFER

# ---- 针对 profile 选择默认参数 ----
case "$PROFILE" in
    bf16)
        MODEL_PATH="${MODEL_PATH:-./output/knowledge_sft_merged}"
        EXTRA=""
        ;;
    fp8)
        MODEL_PATH="${MODEL_PATH:-./output/knowledge_fp8}"
        # vLLM 会自动识别 compressed-tensors FP8 格式
        EXTRA=""
        ;;
    gptq_marlin)
        MODEL_PATH="${MODEL_PATH:-./output/knowledge_gptq_marlin}"
        EXTRA="--quantization compressed-tensors"
        ;;
    fp8_eagle3)
        MODEL_PATH="${MODEL_PATH:-./output/knowledge_fp8}"
        SPEC_MODEL="${SPEC_MODEL:-yuhuili/EAGLE3-Qwen3-8B}"
        NUM_SPEC_TOKENS="${NUM_SPEC_TOKENS:-5}"
        # vLLM 0.7+ 新参数形式（JSON 配置）
        EXTRA="--speculative-config {\"method\":\"eagle3\",\"model\":\"${SPEC_MODEL}\",\"num_speculative_tokens\":${NUM_SPEC_TOKENS}}"
        ;;
    *)
        echo "[error] 未知 PROFILE='$PROFILE'，支持：bf16 | fp8 | gptq_marlin | fp8_eagle3"
        exit 1
        ;;
esac

echo "==================== vLLM V1 Serving ===================="
echo "  PROFILE         : $PROFILE"
echo "  MODEL_PATH      : $MODEL_PATH"
echo "  TP_SIZE         : $TP_SIZE"
echo "  MAX_MODEL_LEN   : $MAX_MODEL_LEN"
echo "  PORT            : $PORT"
echo "  SERVED_NAME     : $SERVED_NAME"
echo "  ATTN_BACKEND    : $VLLM_ATTENTION_BACKEND"
echo "  EXTRA           : $EXTRA"
echo "========================================================="

# 通用参数：
#   --enable-prefix-caching   : V1 内置前缀缓存，命中率高的业务可开
#   --enable-chunked-prefill  : V1 默认启用，长上下文显著降低 TTFT 尾延迟
#   --disable-log-requests    : 降噪（压测时推荐开）
vllm serve "$MODEL_PATH" \
    --port "$PORT" \
    --tensor-parallel-size "$TP_SIZE" \
    --dtype auto \
    --max-model-len "$MAX_MODEL_LEN" \
    --gpu-memory-utilization "$GPU_UTIL" \
    --enable-prefix-caching \
    --enable-chunked-prefill \
    --served-model-name "$SERVED_NAME" \
    --trust-remote-code \
    $EXTRA