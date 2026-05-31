#!/usr/bin/env bash
# =============================================================================
# GGUF 多精度量化脚本（NPC 端侧部署）
# 依赖：llama.cpp (需预先 git clone 并 cmake --build)
# 使用：bash scripts/quantize_gguf.sh [input_hf_model] [output_dir]
# =============================================================================
set -euo pipefail

INPUT_MODEL="${1:-./output/npc_merged}"
OUTPUT_DIR="${2:-./output/npc_gguf}"
LLAMA_CPP_DIR="${LLAMA_CPP_DIR:-$HOME/llama.cpp}"

if [[ ! -d "$LLAMA_CPP_DIR" ]]; then
    echo "[ERROR] llama.cpp 未找到：$LLAMA_CPP_DIR"
    echo "  git clone https://github.com/ggerganov/llama.cpp \"$LLAMA_CPP_DIR\""
    echo "  cd \"$LLAMA_CPP_DIR\" && cmake -B build && cmake --build build --config Release -j"
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

# ---- Step 1：HF → GGUF F16 ----
F16_FILE="$OUTPUT_DIR/npc-f16.gguf"
echo "[1/2] Convert HF → GGUF F16 → $F16_FILE"
python "$LLAMA_CPP_DIR/convert_hf_to_gguf.py" "$INPUT_MODEL" \
    --outfile "$F16_FILE" \
    --outtype f16

# ---- Step 2：多精度量化 ----
QUANTIZE_BIN="$LLAMA_CPP_DIR/build/bin/llama-quantize"

declare -A PRECISIONS=(
    ["Q4_K_M"]="npc-q4_k_m.gguf"       # CPU 服务器推荐（均衡）
    ["IQ4_XS"]="npc-iq4_xs.gguf"       # 2026 新推荐端侧版（体积更小质量更好）
    ["Q4_K_S"]="npc-q4_k_s.gguf"       # 手机端
    ["Q2_K"]="npc-q2_k.gguf"           # 极端低资源（慎用，质量下降明显）
)

for prec in "${!PRECISIONS[@]}"; do
    out_file="$OUTPUT_DIR/${PRECISIONS[$prec]}"
    echo "[2/2] Quantize $prec → $out_file"
    "$QUANTIZE_BIN" "$F16_FILE" "$out_file" "$prec"
done

echo ""
echo "==========  量化完成  =========="
ls -lh "$OUTPUT_DIR"/*.gguf
