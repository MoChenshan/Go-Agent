#!/usr/bin/env bash
# =============================================================================
# run_perf_benchmark.sh —— 四档推理性能自动对比
#
# 自动完成：启动 vLLM 服务 → 等待就绪 → 并发压测 → 杀服务 → 换下一档
# 产物： eval/perf_report.md （自动 append 四行对比数据）
#
# 使用：
#   bash scripts/run_perf_benchmark.sh                            # 默认四档
#   PROFILES="bf16 fp8" bash scripts/run_perf_benchmark.sh        # 仅跑两档
#   CONCURRENCY=32 NUM_REQUESTS=500 bash scripts/run_perf_benchmark.sh
#
# 依赖：GPU + vllm>=0.7.0 + llmcompressor（量化产物已就绪）
# =============================================================================
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PROFILES="${PROFILES:-bf16 fp8 gptq_marlin fp8_eagle3}"
CONCURRENCY="${CONCURRENCY:-16}"
NUM_REQUESTS="${NUM_REQUESTS:-200}"
DATASET="${DATASET:-data/test/knowledge_test.json}"
REPORT="${REPORT:-eval/perf_report.md}"
PORT="${PORT:-8000}"
SERVED_NAME="${SERVED_NAME:-knowledge-expert}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-180}"

wait_ready() {
    local url="http://localhost:$PORT/v1/models"
    local t=0
    while [ $t -lt $WAIT_TIMEOUT ]; do
        if curl -sf "$url" >/dev/null 2>&1; then
            echo "[ready] vLLM 服务就绪（用时 ${t}s）"
            return 0
        fi
        sleep 3
        t=$((t + 3))
    done
    echo "[error] vLLM 启动超时"
    return 1
}

run_one_profile() {
    local profile="$1"
    echo ""
    echo "===================== Profile: $profile ====================="

    # 启动服务（后台）
    PROFILE="$profile" PORT="$PORT" SERVED_NAME="$SERVED_NAME" \
        bash deploy/vllm_v1_server.sh \
        > "eval/vllm_${profile}.log" 2>&1 &
    local pid=$!
    echo "[serve] vLLM pid=$pid  log=eval/vllm_${profile}.log"

    if ! wait_ready; then
        kill -9 "$pid" 2>/dev/null || true
        return 1
    fi

    # 压测
    python scripts/benchmark_serving.py \
        --base_url "http://localhost:$PORT/v1" \
        --model "$SERVED_NAME" \
        --dataset "$DATASET" \
        --concurrency "$CONCURRENCY" \
        --num_requests "$NUM_REQUESTS" \
        --report "$REPORT" \
        --tag "$profile"
    local rc=$?

    # 停止服务
    kill -INT "$pid" 2>/dev/null || true
    sleep 2
    kill -9 "$pid" 2>/dev/null || true

    return $rc
}

mkdir -p eval

for p in $PROFILES; do
    run_one_profile "$p" || echo "[warn] profile=$p 压测失败，继续下一个"
done

echo ""
echo "========== ✅ 全部 profile 完成 =========="
echo "报告：$REPORT"
