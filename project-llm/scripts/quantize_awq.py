"""AWQ-W4A16 量化脚本（使用 autoawq）。

为什么要 AWQ：
- A10/L20 等无 FP8 硬件卡上，AWQ-W4A16 能把模型显存压缩到约 25%
  （8B → 约 5GB），同时精度回退通常在 0.5pp 以内（CMMLU/MMLU）
- vLLM v1 与 sglang 都原生支持 AWQ kernel（速度比 GPTQ 略低 5%~15%，
  但离线量化时间显著更短，对小校准集鲁棒）

校准集：
- 默认从 data/processed/npc_sft.jsonl + ops_sft.jsonl 各取 256 条
- 也可通过 --calib 指定自定义 jsonl 文件

输出：
    {out}/
        config.json
        model.safetensors
        tokenizer.*
        quantize_config.json   ← AWQ 元数据

用法：
    python scripts/quantize_awq.py \
        --model ckpt/sft_merged \
        --out   ckpt/serve_awq \
        --bits 4 --group-size 128 --zero-point true \
        --calib data/processed/calib.jsonl --n-calib 512
"""

import argparse
import json
import os
import random
import sys
from typing import List


def load_calib(path: str, n: int, seed: int = 42) -> List[str]:
    """从 jsonl 抽样 n 条文本作为校准集。"""
    rng = random.Random(seed)
    pool: List[str] = []
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except json.JSONDecodeError:
                continue
            text = ""
            if "messages" in rec:
                text = "\n".join(m.get("content", "") for m in rec["messages"])
            elif "text" in rec:
                text = rec["text"]
            elif "prompt" in rec:
                text = rec["prompt"]
            text = text.strip()
            if text:
                pool.append(text[:2048])
    if n > 0 and len(pool) > n:
        pool = rng.sample(pool, n)
    return pool


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", required=True, help="待量化模型路径（merged HF 权重）")
    ap.add_argument("--out", required=True)
    ap.add_argument("--bits", type=int, default=4, choices=[4])
    ap.add_argument("--group-size", type=int, default=128)
    ap.add_argument("--zero-point", default="true", choices=["true", "false"])
    ap.add_argument("--version", default="GEMM", choices=["GEMM", "GEMV"])
    ap.add_argument("--calib", default="data/processed/npc_sft.jsonl")
    ap.add_argument("--n-calib", type=int, default=256)
    args = ap.parse_args()

    try:
        from awq import AutoAWQForCausalLM  # type: ignore
        from transformers import AutoTokenizer  # type: ignore
    except Exception as e:  # noqa: BLE001
        print(f"ERROR: autoawq / transformers 未安装: {e}", file=sys.stderr)
        print("pip install autoawq transformers", file=sys.stderr)
        return 2

    os.makedirs(args.out, exist_ok=True)
    quant_config = {
        "zero_point": args.zero_point == "true",
        "q_group_size": args.group_size,
        "w_bit": args.bits,
        "version": args.version,
    }
    print(f"quant_config = {quant_config}")

    print(f"loading calibration from {args.calib} (n={args.n_calib})")
    if not os.path.exists(args.calib):
        print(f"ERROR: calib file not found: {args.calib}", file=sys.stderr)
        return 2
    calib_data = load_calib(args.calib, args.n_calib)
    if not calib_data:
        print("ERROR: empty calibration set", file=sys.stderr)
        return 2
    print(f"calibration samples: {len(calib_data)}")

    print(f"loading model from {args.model}")
    tok = AutoTokenizer.from_pretrained(args.model, trust_remote_code=True)
    model = AutoAWQForCausalLM.from_pretrained(args.model, device_map="auto", trust_remote_code=True)

    print("running AWQ quantization (this may take 10-60min depending on size)...")
    model.quantize(tok, quant_config=quant_config, calib_data=calib_data)

    print(f"saving quantized model to {args.out}")
    model.save_quantized(args.out)
    tok.save_pretrained(args.out)

    # 写一份 manifest，便于日后 trace
    with open(os.path.join(args.out, "quantize_manifest.json"), "w", encoding="utf-8") as f:
        json.dump({
            "method": "awq",
            "source_model": os.path.abspath(args.model),
            "config": quant_config,
            "n_calib": len(calib_data),
            "calib_source": args.calib,
        }, f, ensure_ascii=False, indent=2)
    print("done ✓")
    return 0


if __name__ == "__main__":
    sys.exit(main())
