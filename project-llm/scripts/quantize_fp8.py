"""
quantize_fp8.py —— FP8 E4M3 量化（llmcompressor 官方工具）

适用硬件：H100 / H200 / L40S / Ada（需 FP8 tensor core）
精度损失：<1%（近乎无损）
吞吐提升：+60% ~ +80%（相对 BF16）

使用：
    python scripts/quantize_fp8.py \\
        --model ./output/knowledge_sft_merged \\
        --output ./output/knowledge_fp8 \\
        --scheme FP8_DYNAMIC \\
        --calib_dataset ./data/processed/knowledge_qa.json \\
        --calib_samples 512

依赖：
    pip install llmcompressor>=0.3.0  transformers>=4.46
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def load_calib_texts(path: str, n: int) -> list[str]:
    """从 ShareGPT 格式加载 n 条文本用于 FP8 静态 calibration"""
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    texts: list[str] = []
    for sample in data[:n]:
        convs = sample.get("conversations") or []
        buf = []
        for c in convs:
            role = c.get("from", "")
            val = c.get("value", "")
            if role in ("human", "user"):
                buf.append(f"<|im_start|>user\n{val}<|im_end|>")
            elif role in ("gpt", "assistant"):
                buf.append(f"<|im_start|>assistant\n{val}<|im_end|>")
            elif role == "system":
                buf.append(f"<|im_start|>system\n{val}<|im_end|>")
        if buf:
            texts.append("\n".join(buf))
    return texts


def main():
    parser = argparse.ArgumentParser(description="FP8 E4M3 量化")
    parser.add_argument("--model", type=str, required=True, help="HF 权重路径（合并后 SFT/DPO 模型）")
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--scheme", type=str, default="FP8_DYNAMIC",
                        choices=["FP8_DYNAMIC", "FP8_STATIC"],
                        help="DYNAMIC 无需 calib；STATIC 精度略优但需 calib")
    parser.add_argument("--calib_dataset", type=str, default="",
                        help="ShareGPT 数据（仅 FP8_STATIC 需要）")
    parser.add_argument("--calib_samples", type=int, default=512)
    parser.add_argument("--max_seq_len", type=int, default=2048)
    parser.add_argument("--ignore", type=str, nargs="+", default=["lm_head"])
    args = parser.parse_args()

    try:
        from llmcompressor.modifiers.quantization import QuantizationModifier
        from llmcompressor.transformers import oneshot
        from transformers import AutoModelForCausalLM, AutoTokenizer
    except ImportError as e:
        print(f"[error] 需要安装 llmcompressor>=0.3.0：{e}", file=sys.stderr)
        sys.exit(1)

    print(f"[fp8] 加载模型：{args.model}")
    model = AutoModelForCausalLM.from_pretrained(
        args.model, torch_dtype="auto", device_map="auto", trust_remote_code=True
    )
    tokenizer = AutoTokenizer.from_pretrained(args.model, trust_remote_code=True)

    print(f"[fp8] scheme={args.scheme}  ignore={args.ignore}")
    recipe = QuantizationModifier(
        targets="Linear", scheme=args.scheme, ignore=args.ignore,
    )

    calib_kwargs: dict = {}
    if args.scheme == "FP8_STATIC":
        if not args.calib_dataset:
            print("[error] FP8_STATIC 需要 --calib_dataset", file=sys.stderr)
            sys.exit(1)
        texts = load_calib_texts(args.calib_dataset, args.calib_samples)
        print(f"[fp8] 载入 {len(texts)} 条 calibration 样本")
        calib_kwargs = {
            "dataset": [{"text": t} for t in texts],
            "num_calibration_samples": len(texts),
            "max_seq_length": args.max_seq_len,
        }

    out = Path(args.output)
    out.mkdir(parents=True, exist_ok=True)
    oneshot(model=model, recipe=recipe, output_dir=str(out), **calib_kwargs)
    tokenizer.save_pretrained(out)

    print(f"[fp8] ✅ 量化完成 → {out}")
    print(f"[fp8] 后续验证：")
    print(f"      vllm serve {out} --dtype auto --port 8000")


if __name__ == "__main__":
    main()
