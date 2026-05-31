"""BF16 混合精度 + Gradient Checkpointing + Liger Kernel 组合实测

对应方案文档：模型算法微调项目执行方案.md § 10.2.7

目的：验证各显存优化技术的叠加效果，产出面试素材表格。

跑法：
    python infra/distributed/mixed_precision_demo.py                    # 默认配置
    MODEL=Qwen/Qwen3-0.6B SEQ_LEN=2048 python ...                       # 自定义
    SMOKE=1 python ...                                                  # Toy MLP smoke

推荐在目标训练环境下按顺序依次打开：
    1) baseline BF16
    2) + gradient checkpointing
    3) + Liger Kernel (可选)
    4) + flash_attn_2
    5) + deepspeed zero_2/3 (需多卡)
每次记录 peak memory + 单步时间，填入 reports/distributed_mem.md
"""
from __future__ import annotations

import argparse
import os
import time

import torch
import torch.nn as nn


def _build_model(model_id: str, use_bf16: bool = True):
    if os.environ.get("SMOKE") == "1":
        return nn.Sequential(
            nn.Linear(512, 2048), nn.GELU(),
            nn.Linear(2048, 2048), nn.GELU(),
            nn.Linear(2048, 512),
        ).to(torch.bfloat16 if use_bf16 else torch.float32)
    from transformers import AutoModelForCausalLM
    return AutoModelForCausalLM.from_pretrained(
        model_id,
        torch_dtype=torch.bfloat16 if use_bf16 else torch.float32,
    )


def profile_config(
    model_id: str,
    seq_len: int,
    batch: int,
    grad_ckpt: bool,
    use_liger: bool,
    use_flash: bool,
    device: str,
    steps: int = 3,
):
    tag_parts = ["bf16"]
    if grad_ckpt:
        tag_parts.append("gc")
    if use_liger:
        tag_parts.append("liger")
    if use_flash:
        tag_parts.append("fa2")
    tag = "+".join(tag_parts)

    if torch.cuda.is_available():
        torch.cuda.empty_cache()
        torch.cuda.reset_peak_memory_stats()

    model = _build_model(model_id).to(device)

    # 组合优化
    if grad_ckpt and hasattr(model, "gradient_checkpointing_enable"):
        model.gradient_checkpointing_enable(
            gradient_checkpointing_kwargs={"use_reentrant": False}
        )
    if use_liger:
        try:
            from liger_kernel.transformers import apply_liger_kernel_to_qwen3  # noqa
            apply_liger_kernel_to_qwen3(model)
        except Exception as exc:
            print(f"[warn] Liger Kernel 应用失败（{exc}），跳过")
    if use_flash and hasattr(model, "config"):
        try:
            model.config.attn_implementation = "flash_attention_2"
        except Exception:
            pass

    # 构造输入
    if os.environ.get("SMOKE") == "1":
        x = torch.randn(batch, 512, device=device, dtype=torch.bfloat16)
        y_tgt = torch.randn(batch, 512, device=device, dtype=torch.bfloat16)
    else:
        x = torch.randint(0, 151936, (batch, seq_len), device=device)
        y_tgt = x

    optim = torch.optim.AdamW(model.parameters(), lr=1e-5)

    # 预热
    for _ in range(2):
        optim.zero_grad()
        if os.environ.get("SMOKE") == "1":
            out = model(x)
            loss = nn.functional.mse_loss(out, y_tgt)
        else:
            out = model(x, labels=y_tgt)
            loss = out.loss
        loss.backward()
        optim.step()

    # 正式测量
    if torch.cuda.is_available():
        torch.cuda.synchronize()
    t0 = time.time()
    for _ in range(steps):
        optim.zero_grad()
        if os.environ.get("SMOKE") == "1":
            out = model(x)
            loss = nn.functional.mse_loss(out, y_tgt)
        else:
            out = model(x, labels=y_tgt)
            loss = out.loss
        loss.backward()
        optim.step()
    if torch.cuda.is_available():
        torch.cuda.synchronize()
    elapsed = (time.time() - t0) / steps * 1000
    peak = torch.cuda.max_memory_allocated() / 1024 ** 2 if torch.cuda.is_available() else 0.0

    print(f"{tag:>20}  | seq={seq_len:5d} batch={batch} "
          f"peak={peak:8.1f} MB  step={elapsed:7.1f} ms")
    del model
    if torch.cuda.is_available():
        torch.cuda.empty_cache()
    return {"tag": tag, "peak_mb": peak, "step_ms": elapsed}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", default=os.environ.get("MODEL", "Qwen/Qwen3-0.6B"))
    parser.add_argument("--seq-len", type=int, default=int(os.environ.get("SEQ_LEN", 1024)))
    parser.add_argument("--batch", type=int, default=int(os.environ.get("BATCH", 1)))
    args = parser.parse_args()

    device = "cuda" if torch.cuda.is_available() else "cpu"
    print(f"[config] model={args.model} seq_len={args.seq_len} batch={args.batch} device={device}")

    # 按推荐顺序逐项叠加
    combos = [
        dict(grad_ckpt=False, use_liger=False, use_flash=False),
        dict(grad_ckpt=True,  use_liger=False, use_flash=False),
        dict(grad_ckpt=True,  use_liger=True,  use_flash=False),
        dict(grad_ckpt=True,  use_liger=True,  use_flash=True),
    ]

    results = []
    for combo in combos:
        try:
            r = profile_config(
                model_id=args.model, seq_len=args.seq_len, batch=args.batch,
                device=device, **combo,
            )
            results.append(r)
        except torch.cuda.OutOfMemoryError:
            print(f"[OOM] {combo} at seq={args.seq_len}")
            break

    # 打印汇总表格（面试素材）
    print("\n=== Memory / Speed Summary ===")
    for r in results:
        print(f"  {r['tag']:<25} peak={r['peak_mb']:.1f} MB  step={r['step_ms']:.1f} ms")


if __name__ == "__main__":
    main()
