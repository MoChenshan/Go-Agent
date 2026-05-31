#!/usr/bin/env bash
# vLLM 推理服务端到端 Profiling —— 定位真实瓶颈
# 对应方案文档：模型算法微调项目执行方案.md § 10.3.5
#
# 用法：
#   VLLM_URL=http://localhost:8000 bash infra/inference/profile_vllm.sh metrics
#   bash infra/inference/profile_vllm.sh nsys                  # 需要 NVIDIA Nsight Systems

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

VLLM_URL="${VLLM_URL:-http://localhost:8000}"
ACTION="${1:-metrics}"
REPORT_DIR="infra/reports"
mkdir -p "$REPORT_DIR"

case "$ACTION" in
    metrics)
        echo "[1] 抓取 vLLM Prometheus /metrics"
        if ! command -v curl >/dev/null; then
            echo "[fatal] 需要 curl"; exit 1
        fi
        curl -sS "$VLLM_URL/metrics" > "$REPORT_DIR/vllm_metrics.txt" || true
        echo "[2] 筛选关键指标"
        grep -E "vllm:(time_to_first_token|time_per_output_token|num_requests_(running|waiting)|gpu_cache_usage|kv_cache_usage|e2e_request_latency)" \
             "$REPORT_DIR/vllm_metrics.txt" || true
        echo "[done] 完整结果见 $REPORT_DIR/vllm_metrics.txt"
        ;;
    nsys)
        if ! command -v nsys >/dev/null; then
            echo "[fatal] 未安装 Nsight Systems（nsys）"; exit 127
        fi
        echo "[nsys] 抓取 vLLM 服务时间线（按 Ctrl+C 结束）"
        nsys profile \
            --trace=cuda,nvtx,osrt,cudnn,cublas \
            --capture-range=cudaProfilerApi \
            --sample=cpu \
            --output="$REPORT_DIR/vllm_timeline" \
            --force-overwrite=true \
            python -m vllm.entrypoints.openai.api_server \
                --model "${MODEL_PATH:-./output/knowledge_fp8}" \
                --port 8000
        echo "[done] $REPORT_DIR/vllm_timeline.qdrep （用 Nsight Systems GUI 打开）"
        ;;
    bench)
        echo "[bench] 并发压测当前 vLLM 端点"
        python infra/inference/bench_speculative.py \
            --endpoints current="${VLLM_URL}/v1/chat/completions" \
            --concurrency "${CONCURRENCY:-16}" \
            --max-tokens "${MAX_TOKENS:-256}"
        ;;
    *)
        echo "Usage: $0 {metrics|nsys|bench}"
        echo "  metrics : 抓 Prometheus /metrics 并筛选关键指标"
        echo "  nsys    : 用 Nsight Systems 抓推理时间线（需重启服务）"
        echo "  bench   : 并发压测当前端点"
        exit 1
        ;;
esac
