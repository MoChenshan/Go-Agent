#!/usr/bin/env bash
# =============================================================================
# MLC-LLM 一键编译脚本（四端统一入口）
# 使用：TARGET=android|iphone|webgpu|windows bash deploy/mlc/compile.sh
# =============================================================================
set -euo pipefail

TARGET="${TARGET:-android}"
MODEL_HF="${MODEL_HF:-./output/npc_merged}"
OUT_DIR="${OUT_DIR:-./output/npc_mlc}"
QUANT="${QUANT:-q4f16_1}"
CONV_TEMPLATE="${CONV_TEMPLATE:-qwen3}"
CTX_SIZE="${CTX_SIZE:-8192}"

# 目标端 → (device, host)
declare -A DEVICE_MAP=(
    [android]="android android"
    [iphone]="iphone iphone"
    [webgpu]="webgpu webgpu"
    [windows]="vulkan windows"
    [linux]="cuda linux"
)

if [[ -z "${DEVICE_MAP[$TARGET]:-}" ]]; then
    echo "[error] 未知 TARGET='$TARGET'，支持：${!DEVICE_MAP[*]}"
    exit 1
fi
read -r DEVICE HOST <<< "${DEVICE_MAP[$TARGET]}"

echo "==================== MLC-LLM Compile ===================="
echo "  TARGET : $TARGET  (device=$DEVICE host=$HOST)"
echo "  QUANT  : $QUANT"
echo "  MODEL  : $MODEL_HF"
echo "  OUT    : $OUT_DIR"
echo "========================================================="

mkdir -p "$OUT_DIR"

# ---- Step 1：权重转换（只需一次，多端复用） ----
if [[ ! -f "$OUT_DIR/ndarray-cache.json" ]]; then
    echo "[1/3] mlc_llm convert_weight"
    mlc_llm convert_weight "$MODEL_HF" \
        --quantization "$QUANT" \
        -o "$OUT_DIR"
else
    echo "[1/3] 已存在权重 cache，跳过 convert_weight"
fi

# ---- Step 2：生成 mlc-chat-config（含 conv template） ----
if [[ ! -f "$OUT_DIR/mlc-chat-config.json" ]]; then
    echo "[2/3] mlc_llm gen_config"
    mlc_llm gen_config "$MODEL_HF" \
        --quantization "$QUANT" \
        --conv-template "$CONV_TEMPLATE" \
        --context-window-size "$CTX_SIZE" \
        -o "$OUT_DIR"
else
    echo "[2/3] 已存在 mlc-chat-config.json，跳过 gen_config"
fi

# ---- Step 3：编译目标端 library ----
ARTIFACT="$OUT_DIR/npc-${TARGET}.tar"
echo "[3/3] mlc_llm compile → $ARTIFACT"
mlc_llm compile "$OUT_DIR/mlc-chat-config.json" \
    --device "$DEVICE" \
    --host   "$HOST" \
    -o "$ARTIFACT"

ls -lh "$ARTIFACT"
echo ""
echo "========== ✅ MLC-LLM 编译完成 =========="
echo "产物：$ARTIFACT"
echo "集成：参考 deploy/mlc/README.md"
