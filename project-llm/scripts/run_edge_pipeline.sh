#!/usr/bin/env bash
# =============================================================================
# run_edge_pipeline.sh —— 端侧部署一键流水线
#
# 串联：SFT 合并产物 → GGUF 多精度量化 → Ollama 注册 → benchmark
#
# 后续端侧（ExecuTorch / QNN / MLC）由于依赖实机和厂商 SDK，由本地分别触发：
#   bash deploy/mlc/compile.sh        TARGET=android/iphone/webgpu
#   python deploy/executorch/export_android_xnn.py
#   python deploy/executorch/export_ios_coreml.py
#   bash deploy/qnn/convert.sh
#
# 使用：
#   bash scripts/run_edge_pipeline.sh                  # 默认
#   SMOKE=1 bash scripts/run_edge_pipeline.sh          # 只产 Q4_K_M 做 smoke
# =============================================================================
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MODEL_HF="${MODEL_HF:-./output/npc_merged}"
GGUF_DIR="${GGUF_DIR:-./output/npc_gguf}"
SMOKE="${SMOKE:-0}"
OLLAMA_NAME="${OLLAMA_NAME:-npc-zhang}"

# ---- Step 1：HF → GGUF 多精度量化 ----
echo ""
echo "=========================== [1/3] GGUF 量化 ==========================="
if [[ "$SMOKE" == "1" ]]; then
    echo "[smoke] 仅生成 Q4_K_M"
    # smoke 时只跑 Q4_K_M（在 quantize_gguf.sh 中改精度数组）
    PRECISIONS="Q4_K_M" bash scripts/quantize_gguf.sh "$MODEL_HF" "$GGUF_DIR" || true
else
    bash scripts/quantize_gguf.sh "$MODEL_HF" "$GGUF_DIR" || {
        echo "[warn] GGUF 量化失败，可能是本地未装 llama.cpp；跳过"
    }
fi

# ---- Step 2：Ollama 注册（需预装 ollama） ----
echo ""
echo "=========================== [2/3] Ollama 注册 ==========================="
if command -v ollama >/dev/null 2>&1; then
    Q4_FILE="$GGUF_DIR/npc-q4_k_m.gguf"
    if [[ -f "$Q4_FILE" ]]; then
        # 生成针对当前产物路径的 Modelfile
        TMP_MF="$(mktemp)"
        sed "s|FROM .*|FROM $Q4_FILE|" deploy/Modelfile > "$TMP_MF"
        ollama create "$OLLAMA_NAME" -f "$TMP_MF"
        rm -f "$TMP_MF"
        echo "[ollama] 已注册：$OLLAMA_NAME"
    else
        echo "[warn] $Q4_FILE 不存在，跳过 ollama 注册"
    fi
else
    echo "[warn] 未找到 ollama 命令，跳过（见 https://ollama.com/download）"
fi

# ---- Step 3：benchmark_edge 压测 ----
echo ""
echo "=========================== [3/3] 端侧 Benchmark ==========================="
if command -v ollama >/dev/null 2>&1 && ollama list | grep -q "$OLLAMA_NAME"; then
    python deploy/benchmark_edge.py \
        --backend ollama \
        --model "$OLLAMA_NAME" \
        --prompts data/test/npc_test.json \
        --runs 5 \
        --tag "local_ollama_q4_k_m" || true
else
    echo "[warn] 跳过 ollama benchmark"
fi

echo ""
echo "========== ✅ 端侧流水线完成 =========="
echo "产物目录：$GGUF_DIR"
echo "报告：eval/edge_perf_report.md"
echo ""
echo "下一步（实机）："
echo "  Android (MLC)        : TARGET=android bash deploy/mlc/compile.sh"
echo "  iOS     (MLC)        : TARGET=iphone  bash deploy/mlc/compile.sh"
echo "  Android (ExecuTorch) : python deploy/executorch/export_android_xnn.py"
echo "  iOS     (ExecuTorch) : python deploy/executorch/export_ios_coreml.py"
echo "  骁龙 8Gen3 (QNN)     : bash deploy/qnn/convert.sh"
