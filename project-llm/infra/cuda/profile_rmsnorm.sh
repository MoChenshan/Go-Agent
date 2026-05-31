#!/usr/bin/env bash
# Nsight Compute 性能分析：用硬件指标定位 Triton RMSNorm kernel 瓶颈
# 对应方案文档：模型算法微调项目执行方案.md § 10.1.3
#
# 用法：
#   bash infra/cuda/profile_rmsnorm.sh
#
# 前置：
#   - 已安装 CUDA 12.x（Nsight Compute 随 Toolkit 自带）
#   - 已安装 triton>=3.1.0
#   - 机器有权限访问 GPU 性能计数器（需 root 或开启 nvidia perf counters）

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

REPORT_DIR="infra/reports"
mkdir -p "$REPORT_DIR"
OUT="$REPORT_DIR/rmsnorm_profile"

if ! command -v ncu >/dev/null 2>&1; then
    echo "[error] 未找到 'ncu' 命令（Nsight Compute CLI）。请确认 CUDA Toolkit 安装完整。"
    exit 127
fi

echo "[1/3] 运行 Nsight Compute full profiling ..."
ncu --set full --target-processes all \
    --section "SpeedOfLight|MemoryWorkloadAnalysis|LaunchStats|Occupancy" \
    --export "$OUT" \
    --force-overwrite \
    python infra/cuda/triton_rmsnorm.py

echo "[2/3] 汇总 per-kernel 关键指标 ..."
ncu --import "${OUT}.ncu-rep" \
    --print-summary per-kernel \
    --csv 2>/dev/null > "${OUT}.summary.csv" || true

echo "[3/3] 输出关键指标片段（可直接截图到面试报告）："
ncu --import "${OUT}.ncu-rep" --page details --print-summary per-kernel | \
    grep -E "SM Busy|Memory Busy|Achieved Occupancy|DRAM Throughput|L2 Hit Rate|L1/TEX Hit Rate" \
    | head -n 40

echo ""
echo "[done] 完整报告："
echo "  - ${OUT}.ncu-rep        （用 Nsight Compute GUI 打开查看）"
echo "  - ${OUT}.summary.csv    （CSV 汇总，便于写进报告）"
echo ""
echo "典型解读模板："
echo "  SM Busy 低 + Memory Busy 高 + DRAM Throughput > 90% → memory-bound（RMSNorm 正常状态）"
echo "  继续优化方向：kernel fusion（RMSNorm + QKV 投影合并），参考 Liger Kernel 源码"
