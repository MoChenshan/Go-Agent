"""DDP / FSDP 最小 Demo —— 验证梯度同步、参数切分与显存节省

对应方案文档：模型算法微调项目执行方案.md § 10.2.3

启动方式：
    # 本机多进程 CPU 模拟（无需 GPU）
    MODE=ddp  torchrun --nproc_per_node=4 --backend=gloo infra/distributed/ddp_fsdp_demo.py
    MODE=fsdp torchrun --nproc_per_node=2 --backend=gloo infra/distributed/ddp_fsdp_demo.py

    # Colab / Kaggle 免费双 T4
    MODE=ddp  torchrun --nproc_per_node=2 infra/distributed/ddp_fsdp_demo.py
    MODE=fsdp torchrun --nproc_per_node=2 infra/distributed/ddp_fsdp_demo.py
    MODE=fsdp_cpu_offload torchrun --nproc_per_node=2 infra/distributed/ddp_fsdp_demo.py

环境变量：
    MODE: ddp | fsdp | fsdp_cpu_offload
    MODEL: HF 模型 ID（默认 Qwen/Qwen3-0.6B），SMOKE=1 时改用 TinyLlama 占位
    SMOKE: 1 时用一个极小的 Toy MLP，完全无外网依赖

面试要点：
    DDP  → 每卡持完整参数/梯度/优化器，只在 grad 做 all-reduce
    FSDP → 参数/梯度/优化器全部按 rank 切分（= ZeRO-3），forward all-gather、backward reduce-scatter
    FSDP + CPU Offload → 极致显存节省，代价是 PCIe 通信 → 吞吐下降
"""
from __future__ import annotations

import os
import sys
import time

import torch
import torch.distributed as dist
import torch.nn as nn


def _is_smoke() -> bool:
    return os.environ.get("SMOKE") == "1"


def _build_model():
    """根据 SMOKE / 环境变量选择模型。SMOKE=1 下构造 Toy MLP 避免外网依赖。"""
    if _is_smoke():
        return nn.Sequential(
            nn.Linear(256, 512), nn.GELU(),
            nn.Linear(512, 512), nn.GELU(),
            nn.Linear(512, 256),
        )
    try:
        from transformers import AutoModelForCausalLM
        model_id = os.environ.get("MODEL", "Qwen/Qwen3-0.6B")
        return AutoModelForCausalLM.from_pretrained(
            model_id, torch_dtype=torch.bfloat16
        )
    except Exception as exc:  # 没网络时自动回落到 Toy
        print(f"[warn] 加载 HF 模型失败（{exc}），回退到 Toy MLP")
        return nn.Sequential(nn.Linear(256, 512), nn.GELU(), nn.Linear(512, 256))


def _smoke_inputs(device: str):
    """构造 Toy MLP 的输入。"""
    x = torch.randn(4, 256, device=device)
    y = torch.randn(4, 256, device=device)
    return x, y


def _hf_inputs(device: str):
    ids = torch.randint(0, 151936, (2, 512), device=device)
    return ids, ids


def main():
    backend = "nccl" if torch.cuda.is_available() else "gloo"
    dist.init_process_group(backend=backend)
    rank = dist.get_rank()
    world = dist.get_world_size()
    device = f"cuda:{rank}" if torch.cuda.is_available() else "cpu"
    if torch.cuda.is_available():
        torch.cuda.set_device(rank)

    if rank == 0:
        print(f"[init] backend={backend} world={world} device={device} smoke={_is_smoke()}")

    # ---- 构建模型 ----
    model = _build_model().to(device)
    mode = os.environ.get("MODE", "fsdp").lower()

    if mode == "ddp":
        from torch.nn.parallel import DistributedDataParallel as DDP
        model = DDP(
            model,
            device_ids=[rank] if torch.cuda.is_available() else None,
            output_device=rank if torch.cuda.is_available() else None,
        )
        if rank == 0:
            print("[DDP] 每张卡保留完整参数+梯度+优化器状态，只在反向 all-reduce grad")
    elif mode in ("fsdp", "fsdp_cpu_offload"):
        from torch.distributed.fsdp import (
            FullyShardedDataParallel as FSDP,
            MixedPrecision,
            ShardingStrategy,
            BackwardPrefetch,
            CPUOffload,
        )
        mixed = MixedPrecision(
            param_dtype=torch.bfloat16,
            reduce_dtype=torch.bfloat16,
            buffer_dtype=torch.bfloat16,
        )
        cpu_offload = CPUOffload(offload_params=(mode == "fsdp_cpu_offload"))
        model = FSDP(
            model,
            sharding_strategy=ShardingStrategy.FULL_SHARD,   # ZeRO-3 等价
            mixed_precision=mixed,
            backward_prefetch=BackwardPrefetch.BACKWARD_PRE,
            cpu_offload=cpu_offload,
            use_orig_params=True,
        )
        if rank == 0:
            shard_params = sum(p.numel() for p in model.parameters())
            print(f"[FSDP/{mode}] 每张卡只持 ~{shard_params / 1e6:.2f}M 参数 "
                  f"({shard_params * 2 / 1024 ** 2:.1f} MB BF16)")
    else:
        raise SystemExit(f"未知 MODE={mode}，可选 ddp | fsdp | fsdp_cpu_offload")

    # ---- 训练循环 ----
    optimizer = torch.optim.AdamW(model.parameters(), lr=1e-5)
    is_toy = _is_smoke() or not hasattr(model, "module") and not hasattr(model, "forward")
    get_inputs = _smoke_inputs if _is_smoke() else _hf_inputs

    for step in range(3):
        if torch.cuda.is_available():
            torch.cuda.reset_peak_memory_stats()

        inputs, labels = get_inputs(device)

        t0 = time.time()
        if _is_smoke():
            out = model(inputs)
            loss = nn.functional.mse_loss(out, labels)
        else:
            try:
                out = model(inputs, labels=labels)
                loss = out.loss
            except Exception:
                # Toy MLP fallback
                out = model(inputs)
                loss = nn.functional.mse_loss(out, labels)
        loss.backward()
        optimizer.step()
        optimizer.zero_grad()
        step_time = time.time() - t0

        if rank == 0:
            msg = f"step={step} loss={loss.item():.4f} time={step_time * 1000:.0f}ms"
            if torch.cuda.is_available():
                peak = torch.cuda.max_memory_allocated() / 1024 ** 2
                msg += f" peak_mem={peak:.0f}MB"
            print(msg)

    dist.destroy_process_group()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"[fatal] rank error: {exc}", file=sys.stderr)
        raise
