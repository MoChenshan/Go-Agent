"""merge_lora.py — 合并 LoRA / QLoRA 权重到 base model。

用途：训练完 SFT/DPO/GRPO 后，把 LoRA adapter 与 base model 合并，
得到可直接被 vLLM / SGLang / llama.cpp 加载的标准权重目录。

依赖：peft >= 0.10, transformers >= 4.40

用法：
    python scripts/merge_lora.py \\
        --base Qwen/Qwen3-4B \\
        --lora output/npc_grpo \\
        --out  output/npc_merged
"""
from __future__ import annotations

import argparse
import os
import shutil
import sys
from pathlib import Path


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True, help="基座 HF repo 或本地路径")
    ap.add_argument("--lora", required=True, help="LoRA adapter 目录")
    ap.add_argument("--out", required=True, help="合并后输出目录")
    ap.add_argument("--dtype", default="bfloat16", choices=["bfloat16", "float16", "float32"])
    ap.add_argument("--max-shard-size", default="4GB")
    args = ap.parse_args()

    out = Path(args.out)
    if out.exists():
        print(f"[warn] {out} 已存在，将覆盖")
        shutil.rmtree(out, ignore_errors=True)
    out.mkdir(parents=True, exist_ok=True)

    # 延迟 import 让 --help 即使无 GPU 环境也能跑通
    try:
        import torch
        from peft import PeftModel
        from transformers import AutoModelForCausalLM, AutoTokenizer
    except ImportError as e:  # pragma: no cover
        print(f"ERROR: 缺少依赖: {e}", file=sys.stderr)
        print("       pip install 'transformers>=4.40' 'peft>=0.10' torch", file=sys.stderr)
        return 1

    dtype = {
        "bfloat16": torch.bfloat16,
        "float16": torch.float16,
        "float32": torch.float32,
    }[args.dtype]

    print(f"[1/4] loading base model: {args.base}")
    tok = AutoTokenizer.from_pretrained(args.base, trust_remote_code=True)
    base = AutoModelForCausalLM.from_pretrained(
        args.base,
        torch_dtype=dtype,
        trust_remote_code=True,
        device_map="cpu",  # 合并阶段用 CPU 即可，避免 OOM
        low_cpu_mem_usage=True,
    )

    print(f"[2/4] attaching LoRA adapter: {args.lora}")
    model = PeftModel.from_pretrained(base, args.lora)

    print(f"[3/4] merge_and_unload ...")
    merged = model.merge_and_unload()

    print(f"[4/4] saving to {out}")
    merged.save_pretrained(
        out,
        max_shard_size=args.max_shard_size,
        safe_serialization=True,
    )
    tok.save_pretrained(out)

    # 复制 adapter 里的额外配置（chat_template、generation_config 等）
    for fname in ("tokenizer_config.json", "generation_config.json", "chat_template.json"):
        src = Path(args.lora) / fname
        dst = out / fname
        if src.exists() and not dst.exists():
            shutil.copy2(src, dst)

    size_mb = sum(p.stat().st_size for p in out.rglob("*") if p.is_file()) / 1024 / 1024
    print(f"✅ merged checkpoint ready: {out}  (~{size_mb:.0f} MB, {len(list(out.iterdir()))} files)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
