"""train_sft.py — 不依赖 LlamaFactory 的裸 trl SFT 训练入口（备选）。

适用场景：
  - 评审/面试现场无 LlamaFactory 环境
  - 想自定义训练 loop / metric / callback
  - CI 上做 smoke 训练（10 step ≤1min）

依赖：transformers>=4.40, trl>=0.9, peft>=0.10, accelerate>=0.30

用法：
    # 真训
    python scripts/train_sft.py \\
        --base Qwen/Qwen3-4B \\
        --data data/processed/sft_demo.jsonl \\
        --output output/npc_sft_trl

    # smoke（CI 用）
    python scripts/train_sft.py --base sshleifer/tiny-gpt2 \\
        --data data/processed/sft_demo.jsonl \\
        --output output/sft_smoke --max-steps 5 --no-lora
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path


def load_dataset_jsonl(path: Path):
    from datasets import Dataset

    rows = []
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        item = json.loads(line)
        # ShareGPT -> trl messages
        msgs = []
        for turn in item.get("conversations", []):
            role_map = {"human": "user", "gpt": "assistant", "system": "system"}
            msgs.append({"role": role_map.get(turn["from"], turn["from"]), "content": turn["value"]})
        if msgs:
            rows.append({"messages": msgs})
    return Dataset.from_list(rows)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True, help="基座模型 HF id 或本地路径")
    ap.add_argument("--data", required=True, help="ShareGPT JSONL")
    ap.add_argument("--output", required=True)
    ap.add_argument("--lora-r", type=int, default=16)
    ap.add_argument("--lora-alpha", type=int, default=32)
    ap.add_argument("--no-lora", action="store_true")
    ap.add_argument("--max-steps", type=int, default=-1)
    ap.add_argument("--epochs", type=float, default=1.0)
    ap.add_argument("--lr", type=float, default=2e-4)
    ap.add_argument("--batch-size", type=int, default=2)
    ap.add_argument("--cutoff-len", type=int, default=2048)
    ap.add_argument("--bf16", action="store_true")
    ap.add_argument("--report-to", default="tensorboard")
    args = ap.parse_args()

    try:
        import torch
        from transformers import AutoModelForCausalLM, AutoTokenizer
        from trl import SFTConfig, SFTTrainer
    except ImportError as e:  # pragma: no cover
        print(f"ERROR: 缺少依赖：{e}\n  pip install 'transformers>=4.40' 'trl>=0.9' 'peft>=0.10' accelerate", file=sys.stderr)
        return 1

    out = Path(args.output)
    out.mkdir(parents=True, exist_ok=True)

    print(f"[1/4] loading tokenizer + model: {args.base}")
    tok = AutoTokenizer.from_pretrained(args.base, trust_remote_code=True)
    if tok.pad_token is None:
        tok.pad_token = tok.eos_token
    dtype = torch.bfloat16 if args.bf16 else torch.float32
    model = AutoModelForCausalLM.from_pretrained(
        args.base, torch_dtype=dtype, trust_remote_code=True
    )

    print(f"[2/4] loading dataset: {args.data}")
    ds = load_dataset_jsonl(Path(args.data))
    print(f"      n_samples={len(ds)}")

    peft_cfg = None
    if not args.no_lora:
        try:
            from peft import LoraConfig

            peft_cfg = LoraConfig(
                r=args.lora_r,
                lora_alpha=args.lora_alpha,
                lora_dropout=0.05,
                target_modules="all-linear",
                bias="none",
                task_type="CAUSAL_LM",
            )
            print(f"[3/4] LoRA enabled: r={args.lora_r}, alpha={args.lora_alpha}")
        except ImportError:
            print("[3/4] peft 不可用，回退全参微调")

    cfg = SFTConfig(
        output_dir=str(out),
        num_train_epochs=args.epochs,
        max_steps=args.max_steps,
        per_device_train_batch_size=args.batch_size,
        gradient_accumulation_steps=4,
        learning_rate=args.lr,
        lr_scheduler_type="cosine",
        warmup_ratio=0.1,
        bf16=args.bf16,
        logging_steps=5,
        save_steps=200,
        save_total_limit=2,
        max_seq_length=args.cutoff_len,
        report_to=args.report_to,
        gradient_checkpointing=True,
        remove_unused_columns=False,
    )

    trainer = SFTTrainer(
        model=model,
        tokenizer=tok,
        train_dataset=ds,
        args=cfg,
        peft_config=peft_cfg,
    )
    print(f"[4/4] start training, output_dir={out}")
    trainer.train()
    trainer.save_model(str(out))
    tok.save_pretrained(out)
    print("✅ training done")
    return 0


if __name__ == "__main__":
    sys.exit(main())
