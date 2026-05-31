"""
ExecuTorch 导出脚本 —— Android (XNNPACK Backend，走 CPU + GPU delegate)

依赖：
    pip install executorch>=0.5.0 torch>=2.4.0

使用：
    python deploy/executorch/export_android_xnn.py \\
        --model_path ./output/npc_merged \\
        --output     ./output/npc_edge/npc-android-xnn.pte \\
        --quant_bits 4 --group_size 128 --seq_len 2048

产物说明：
    .pte  —— ExecuTorch 可执行格式，Android 端通过 JNI + ExecuTorch Runtime 加载
    典型体积：Qwen3-4B INT4g128 XNN ≈ 2.3 GB / Qwen3-1.7B INT4 ≈ 1.0 GB

参考实现：
    https://github.com/pytorch/executorch/blob/main/examples/models/llama/README.md
"""
from __future__ import annotations

import argparse
import json
import shutil
import sys
from pathlib import Path


def build_model_params_json(hf_dir: Path, out_dir: Path) -> Path:
    """
    ExecuTorch 的 export_llama 需要一份 `params.json`（定义 hidden_size / n_layers 等），
    从 HF config.json 映射生成。
    """
    cfg = json.loads((hf_dir / "config.json").read_text(encoding="utf-8"))
    params = {
        "dim": cfg["hidden_size"],
        "n_layers": cfg["num_hidden_layers"],
        "n_heads": cfg["num_attention_heads"],
        "n_kv_heads": cfg.get("num_key_value_heads", cfg["num_attention_heads"]),
        "vocab_size": cfg["vocab_size"],
        "norm_eps": cfg.get("rms_norm_eps", 1e-5),
        "max_seq_len": cfg.get("max_position_embeddings", 8192),
        "rope_theta": cfg.get("rope_theta", 1000000.0),
        "use_scaled_rope": False,
    }
    out_path = out_dir / "params.json"
    out_path.write_text(json.dumps(params, indent=2), encoding="utf-8")
    return out_path


def main():
    parser = argparse.ArgumentParser(description="Export Qwen3 to ExecuTorch .pte for Android XNNPACK")
    parser.add_argument("--model_path", type=str, default="./output/npc_merged",
                        help="HF 合并后的模型目录")
    parser.add_argument("--output", type=str, default="./output/npc_edge/npc-android-xnn.pte")
    parser.add_argument("--quant_bits", type=int, default=4, choices=[4, 8])
    parser.add_argument("--group_size", type=int, default=128)
    parser.add_argument("--seq_len", type=int, default=2048)
    parser.add_argument("--use_kv_cache", action="store_true", default=True)
    parser.add_argument("--use_sdpa_with_kv_cache", action="store_true", default=True,
                        help="开启 SDPA-with-KV-Cache，XNNPACK 推理可加速 2-3×")
    parser.add_argument("--tokenizer_path", type=str, default="",
                        help="HF tokenizer 路径（默认用 model_path）")
    args = parser.parse_args()

    hf_dir = Path(args.model_path)
    if not hf_dir.exists():
        print(f"[error] 模型路径不存在：{hf_dir}", file=sys.stderr)
        sys.exit(1)

    out_path = Path(args.output)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    # 准备 params.json
    params_json = build_model_params_json(hf_dir, out_path.parent)
    print(f"[exec] params.json → {params_json}")

    # 通过 ExecuTorch 官方 llama 导出链路（最稳）
    try:
        # executorch 0.5+ 的 CLI 入口：executorch.examples.models.llama.export_llama
        from executorch.examples.models.llama import export_llama as el
    except ImportError as e:
        print("[error] 未安装 executorch，或版本 < 0.5：", e, file=sys.stderr)
        print("  pip install --pre executorch", file=sys.stderr)
        sys.exit(1)

    # 构造参数（等价于命令行）
    cli_args = [
        "--model", "qwen3",
        "--checkpoint", str(hf_dir),
        "--params", str(params_json),
        "--output_name", str(out_path),
        "--max_seq_length", str(args.seq_len),
        "--dtype-override", "fp32",
        "-X",  # --xnnpack 后端
        "-qmode", f"{args.quant_bits}bit" if args.quant_bits == 4 else "8da4w",
        "--group_size", str(args.group_size),
    ]
    if args.use_kv_cache:
        cli_args.append("-kv")
    if args.use_sdpa_with_kv_cache:
        cli_args.append("--use_sdpa_with_kv_cache")

    print("[exec] 调用 export_llama 参数：", " ".join(cli_args))
    rc = el.main(cli_args) if hasattr(el, "main") else el.export_llama(cli_args)
    if rc not in (None, 0):
        print(f"[error] export_llama 返回码 {rc}", file=sys.stderr)
        sys.exit(int(rc) if isinstance(rc, int) else 1)

    # tokenizer 一同导出（Android 端会读）
    tok_src = Path(args.tokenizer_path) if args.tokenizer_path else hf_dir
    for f in ("tokenizer.json", "tokenizer_config.json", "special_tokens_map.json"):
        s = tok_src / f
        if s.exists():
            shutil.copy2(s, out_path.parent / f)

    size_mb = out_path.stat().st_size / (1024 * 1024)
    print(f"[exec] ✅ 导出完成：{out_path}  （{size_mb:.1f} MB）")
    print(f"[exec] 产物目录：{out_path.parent}")
    print(f"[exec] Android 加载示例（Kotlin + JNI）见 deploy/executorch/README_android.md")


if __name__ == "__main__":
    main()
