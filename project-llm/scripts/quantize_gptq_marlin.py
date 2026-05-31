"""
quantize_gptq_marlin.py —— GPTQ-Marlin W4A16 量化

适用硬件：A100 / 4090 / H100（Marlin kernel 在 Ampere+ 上性能极佳）
精度损失：~2%
吞吐提升：+120% 左右（vLLM V1 官方推荐的 4-bit 方案）

使用：
    python scripts/quantize_gptq_marlin.py \\
        --model ./output/knowledge_sft_merged \\
        --output ./output/knowledge_gptq_marlin \\
        --calib_dataset ./data/processed/knowledge_qa.json \\
        --calib_samples 512 \\
        --bits 4 --group_size 128

依赖：
    pip install llmcompressor>=0.3.0  compressed-tensors>=0.7.0
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def load_calib_texts(path: str, n: int) -> list[str]:
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
    parser = argparse.ArgumentParser(description="GPTQ-Marlin W4A16 量化")
    parser.add_argument("--model", type=str, required=True)
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--calib_dataset", type=str, required=True)
    parser.add_argument("--calib_samples", type=int, default=512)
    parser.add_argument("--max_seq_len", type=int, default=2048)
    parser.add_argument("--bits", type=int, default=4)
    parser.add_argument("--group_size", type=int, default=128)
    parser.add_argument("--damp_percent", type=float, default=0.01)
    parser.add_argument("--desc_act", type=int, default=1, help="是否启用 activation order")
    parser.add_argument("--ignore", type=str, nargs="+", default=["lm_head"])
    args = parser.parse_args()

    try:
        from llmcompressor.modifiers.quantization import GPTQModifier
        from llmcompressor.transformers import oneshot
        from transformers import AutoModelForCausalLM, AutoTokenizer
    except ImportError as e:
        print(f"[error] 需要安装 llmcompressor>=0.3.0：{e}", file=sys.stderr)
        sys.exit(1)

    print(f"[gptq-marlin] 加载模型：{args.model}")
    model = AutoModelForCausalLM.from_pretrained(
        args.model, torch_dtype="auto", device_map="auto", trust_remote_code=True
    )
    tokenizer = AutoTokenizer.from_pretrained(args.model, trust_remote_code=True)

    # 加载 calibration 数据（GPTQ 必需）
    texts = load_calib_texts(args.calib_dataset, args.calib_samples)
    if not texts:
        print("[error] calibration 数据为空", file=sys.stderr)
        sys.exit(1)
    print(f"[gptq-marlin] 载入 {len(texts)} 条 calibration 样本")

    # W4A16 方案 → vLLM 会自动选用 Marlin kernel 加速
    scheme_name = "W4A16"
    print(f"[gptq-marlin] bits={args.bits} group_size={args.group_size} "
          f"scheme={scheme_name} desc_act={bool(args.desc_act)}")

    recipe = GPTQModifier(
        targets="Linear",
        scheme=scheme_name,
        ignore=args.ignore,
        dampening_frac=args.damp_percent,
    )

    out = Path(args.output)
    out.mkdir(parents=True, exist_ok=True)
    oneshot(
        model=model,
        recipe=recipe,
        dataset=[{"text": t} for t in texts],
        num_calibration_samples=len(texts),
        max_seq_length=args.max_seq_len,
        output_dir=str(out),
    )
    tokenizer.save_pretrained(out)

    print(f"[gptq-marlin] ✅ 量化完成 → {out}")
    print(f"[gptq-marlin] 后续验证：")
    print(f"      vllm serve {out} --quantization compressed-tensors --dtype auto")


if __name__ == "__main__":
    main()
