"""
memory_profile.py —— 训练过程显存监控

产出面试可用的性能数据：显存峰值、时序曲线、QLoRA vs FP16 LoRA 对比。

用法（作为 callback 嵌入训练）：
    from scripts.memory_profile import MemoryProfiler
    profiler = MemoryProfiler("logs/memory_knowledge_sft.csv")
    for step, batch in enumerate(loader):
        ...
        if step % 10 == 0:
            profiler.log(step)
    profiler.summary()

也可作为 CLI 分析历史 log：
    python scripts/memory_profile.py --log logs/memory_knowledge_sft.csv --plot
"""
from __future__ import annotations

import argparse
import time
from pathlib import Path


class MemoryProfiler:
    """训练显存 Profiler，每 N 步记录一次"""

    def __init__(self, log_file: str = "memory_log.csv") -> None:
        self.log_file = log_file
        Path(log_file).parent.mkdir(parents=True, exist_ok=True)
        with open(log_file, "w", encoding="utf-8") as f:
            f.write("step,allocated_mb,reserved_mb,peak_mb,timestamp\n")

    def log(self, step: int) -> None:
        import torch
        if not torch.cuda.is_available():
            return
        allocated = torch.cuda.memory_allocated() / 1024 ** 2
        reserved = torch.cuda.memory_reserved() / 1024 ** 2
        peak = torch.cuda.max_memory_allocated() / 1024 ** 2
        with open(self.log_file, "a", encoding="utf-8") as f:
            f.write(f"{step},{allocated:.1f},{reserved:.1f},{peak:.1f},{time.time()}\n")

    def summary(self) -> dict:
        import pandas as pd
        df = pd.read_csv(self.log_file)
        stats = {
            "peak_mb": float(df["peak_mb"].max()),
            "allocated_mean_mb": float(df["allocated_mb"].mean()),
            "reserved_mean_mb": float(df["reserved_mb"].mean()),
            "n_samples": int(len(df)),
        }
        print(f"[memory] 峰值  : {stats['peak_mb']:.0f} MB")
        print(f"[memory] 平均分配: {stats['allocated_mean_mb']:.0f} MB")
        print(f"[memory] 平均预留: {stats['reserved_mean_mb']:.0f} MB")
        return stats


def plot_log(log_file: str, out_png: str | None = None) -> None:
    """绘制显存时序曲线（可选）"""
    try:
        import matplotlib.pyplot as plt
        import pandas as pd
    except ImportError:
        print("[memory] 请先 pip install matplotlib pandas")
        return
    df = pd.read_csv(log_file)
    fig, ax = plt.subplots(figsize=(10, 4))
    ax.plot(df["step"], df["allocated_mb"], label="allocated")
    ax.plot(df["step"], df["reserved_mb"], label="reserved")
    ax.plot(df["step"], df["peak_mb"], label="peak", linestyle="--")
    ax.set_xlabel("step")
    ax.set_ylabel("Memory (MB)")
    ax.set_title(f"Memory profile: {log_file}")
    ax.legend()
    fig.tight_layout()
    if out_png:
        fig.savefig(out_png, dpi=120)
        print(f"[memory] 图表已保存 → {out_png}")
    else:
        plt.show()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--log", type=str, required=True, help="memory csv log path")
    parser.add_argument("--plot", action="store_true", help="绘制时序图")
    parser.add_argument("--out_png", type=str, default=None)
    args = parser.parse_args()

    prof = MemoryProfiler.__new__(MemoryProfiler)
    prof.log_file = args.log
    prof.summary()

    if args.plot:
        plot_log(args.log, args.out_png)


if __name__ == "__main__":
    main()
