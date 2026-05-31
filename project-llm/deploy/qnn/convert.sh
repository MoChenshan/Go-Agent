#!/usr/bin/env bash
# =============================================================================
# 高通 QNN NPU 部署 —— HF → ONNX → DLC 完整转换脚本
# 目标硬件：骁龙 8 Gen3 / 8 Elite 的 HTP NPU（INT8 + INT16 混合精度）
#
# 依赖：
#   QNN SDK 2.26+        （https://qpm.qualcomm.com/#/main/tools/details/qualcomm_ai_engine_direct）
#   Python 3.10
#   optimum >= 1.23     （pip install "optimum[onnxruntime]">=1.23）
#   onnx    >= 1.17
#
# 环境变量：
#   QNN_SDK_ROOT  (必填)  : QNN SDK 根目录
#   MODEL_HF               : HF 合并后的模型目录
#   OUT_DIR                : 输出目录
#
# 使用：
#   export QNN_SDK_ROOT=/opt/qcom/aistack/qnn/2.26.0
#   bash deploy/qnn/convert.sh
# =============================================================================
set -euo pipefail

MODEL_HF="${MODEL_HF:-./output/npc_merged}"
OUT_DIR="${OUT_DIR:-./output/npc_qnn}"
QUANT_CONFIG="${QUANT_CONFIG:-deploy/qnn/quant_config.json}"
CALIB_DATASET="${CALIB_DATASET:-data/processed/npc_sharegpt.json}"
CALIB_SAMPLES="${CALIB_SAMPLES:-128}"
SEQ_LEN="${SEQ_LEN:-2048}"

MODEL_ONNX="$OUT_DIR/npc.onnx"
MODEL_DLC="$OUT_DIR/npc.dlc"
MODEL_DLC_Q="$OUT_DIR/npc-quantized.dlc"
MODEL_BIN="$OUT_DIR/npc-htp.bin"

# ---- 前置检查 ----
if [[ -z "${QNN_SDK_ROOT:-}" ]]; then
    echo "[error] 请设置 QNN_SDK_ROOT 环境变量"
    exit 1
fi
if [[ ! -d "$QNN_SDK_ROOT" ]]; then
    echo "[error] QNN_SDK_ROOT 目录不存在：$QNN_SDK_ROOT"
    exit 1
fi

# 加载 QNN 环境变量（设置 PATH / LD_LIBRARY_PATH / PYTHONPATH）
# shellcheck disable=SC1091
source "$QNN_SDK_ROOT/bin/envsetup.sh"

mkdir -p "$OUT_DIR"

# ---- Step 1: HF → ONNX（optimum） ----
echo "[1/4] HF → ONNX  ($MODEL_HF → $MODEL_ONNX)"
optimum-cli export onnx \
    --model "$MODEL_HF" \
    --task text-generation-with-past \
    --opset 17 \
    --device cpu \
    --no-post-process \
    --trust-remote-code \
    "$OUT_DIR/onnx_raw/"
# optimum 会产出多个 onnx 子图，取主模型
cp "$OUT_DIR/onnx_raw/model.onnx" "$MODEL_ONNX"
cp "$OUT_DIR/onnx_raw/"*.json "$OUT_DIR/" 2>/dev/null || true

# ---- Step 2: 抽取 calibration 样本（QNN INT8 量化需要） ----
CALIB_RAW="$OUT_DIR/calib_inputs.raw"
echo "[2/4] 准备 calibration 数据 → $CALIB_RAW"
python - <<PY
import json, numpy as np, struct, sys
from pathlib import Path
from transformers import AutoTokenizer

tok = AutoTokenizer.from_pretrained("$MODEL_HF", trust_remote_code=True)
data = json.loads(Path("$CALIB_DATASET").read_text(encoding="utf-8"))
samples = data[:$CALIB_SAMPLES]

with open("$CALIB_RAW", "wb") as f:
    for s in samples:
        convs = s.get("conversations", [])
        text = "\n".join(c.get("value", "") for c in convs)[:2000]
        ids = tok(text, return_tensors="np", max_length=$SEQ_LEN,
                  truncation=True, padding="max_length")["input_ids"].astype(np.int32)
        f.write(ids.tobytes())
print(f"[calib] wrote {len(samples)} samples → $CALIB_RAW")
PY

# ---- Step 3: ONNX → DLC + INT8 量化 ----
echo "[3/4] ONNX → DLC (未量化)"
qnn-onnx-converter \
    --input_network "$MODEL_ONNX" \
    --output_path "$MODEL_DLC" \
    --input_dim "input_ids" "1,$SEQ_LEN"

echo "[3.5/4] DLC → quantized DLC (INT8 + 混合精度)"
qnn-model-lib-generator \
    --model "$MODEL_DLC" \
    --output_dir "$OUT_DIR/libs" \
    --lib_targets aarch64-android

qnn-net-run --quantization_overrides "$QUANT_CONFIG" \
    --input_list "$CALIB_RAW" \
    --model "$MODEL_DLC" \
    --output_dir "$OUT_DIR/calib_out/" \
    --backend libQnnHtp.so || true

# 实际量化命令（依赖 QNN SDK 自带工具）
qnn-onnx-converter \
    --input_network "$MODEL_ONNX" \
    --output_path "$MODEL_DLC_Q" \
    --input_dim "input_ids" "1,$SEQ_LEN" \
    --quantization_overrides "$QUANT_CONFIG" \
    --input_list "$CALIB_RAW" \
    --act_bitwidth 8 \
    --weight_bitwidth 8 \
    --bias_bitwidth 32

# ---- Step 4: DLC → HTP binary ----
echo "[4/4] DLC → HTP binary → $MODEL_BIN"
qnn-context-binary-generator \
    --model "$MODEL_DLC_Q" \
    --backend "$QNN_SDK_ROOT/lib/x86_64-linux-clang/libQnnHtp.so" \
    --binary_file "$MODEL_BIN" \
    --config_file <(echo '{
        "graphs": [{
            "graph_names": ["npc"],
            "fp16_relaxed_precision": 1,
            "O": 3,
            "htp_performance_mode": "burst"
        }]
    }')

echo ""
echo "========================= ✅ QNN 转换完成 ========================="
echo "  量化 DLC : $MODEL_DLC_Q"
echo "  HTP Bin  : $MODEL_BIN"
ls -lh "$OUT_DIR/"*.dlc "$OUT_DIR/"*.bin 2>/dev/null || true
echo ""
echo "→ 集成 Android APP：参考 deploy/qnn/README_android.md（QnnSystemContext API 加载 .bin）"
