#!/usr/bin/env bash
# 一键启动 DDP / FSDP / TP / Mixed Precision 分布式实验
# 对应方案文档：§ 10.2
#
# 用法：
#   bash infra/distributed/run_ddp_fsdp.sh ddp                    # 本机 CPU 2 进程跑 DDP
#   bash infra/distributed/run_ddp_fsdp.sh fsdp 2                 # 2 进程 FSDP
#   bash infra/distributed/run_ddp_fsdp.sh tp 2                   # TP Column+Row
#   bash infra/distributed/run_ddp_fsdp.sh mixed                  # 单卡混合精度对比

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

CMD="${1:-ddp}"
NPROC="${2:-2}"
BACKEND=$(python -c "import torch; print('nccl' if torch.cuda.is_available() else 'gloo')")
echo "[info] backend=${BACKEND} nproc=${NPROC}"

case "$CMD" in
    ddp)
        MODE=ddp torchrun --nproc_per_node=$NPROC --backend=$BACKEND \
            infra/distributed/ddp_fsdp_demo.py
        ;;
    fsdp)
        MODE=fsdp torchrun --nproc_per_node=$NPROC --backend=$BACKEND \
            infra/distributed/ddp_fsdp_demo.py
        ;;
    fsdp_offload)
        MODE=fsdp_cpu_offload torchrun --nproc_per_node=$NPROC --backend=$BACKEND \
            infra/distributed/ddp_fsdp_demo.py
        ;;
    tp)
        torchrun --nproc_per_node=$NPROC --backend=$BACKEND \
            infra/distributed/tp_column_row.py
        ;;
    mixed)
        python infra/distributed/mixed_precision_demo.py
        ;;
    smoke)
        echo "[smoke] running single-process sanity on all scripts ..."
        SMOKE=1 python infra/distributed/ddp_fsdp_demo.py 2>/dev/null || true
        SMOKE=1 python infra/distributed/tp_column_row.py
        SMOKE=1 python infra/distributed/mixed_precision_demo.py
        ;;
    *)
        echo "Unknown cmd: $CMD"
        echo "Usage: $0 {ddp|fsdp|fsdp_offload|tp|mixed|smoke} [nproc]"
        exit 1
        ;;
esac
