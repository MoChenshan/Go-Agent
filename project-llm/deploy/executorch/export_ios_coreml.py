"""
ExecuTorch 导出脚本 —— iOS (CoreML Backend，走 Apple A17/A18 ANE)

依赖：
    pip install executorch>=0.5.0 coremltools>=7.2 torch>=2.4.0
    仅 macOS 支持（coremltools 限制）

使用：
    python deploy/executorch/export_ios_coreml.py \\
        --model_path ./output/npc_merged \\
        --output     ./output/npc_edge/npc-ios-coreml.pte \\
        --quant_bits 4 --compute_unit ALL

compute_unit 说明：
    CPU_ONLY       : 仅 CPU（调试用）
    CPU_AND_GPU    : CPU+GPU（兼容旧设备）
    CPU_AND_NE     : CPU+ANE（A17+ 推荐，功耗最低）
    ALL            : 自动选择（默认推荐）

参考：
    https://github.com/pytorch/executorch/blob/main/examples/models/llama/README.md
    https://apple.github.io/coremltools/
"""
from __future__ import annotations

import argparse
import platform
import shutil
import sys
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description="Export Qwen3 to ExecuTorch .pte for iOS CoreML")
    parser.add_argument("--model_path", type=str, default="./output/npc_merged")
    parser.add_argument("--output", type=str, default="./output/npc_edge/npc-ios-coreml.pte")
    parser.add_argument("--quant_bits", type=int, default=4, choices=[4, 8])
    parser.add_argument("--group_size", type=int, default=32,
                        help="CoreML 推荐 group_size=32（ANE 友好）")
    parser.add_argument("--seq_len", type=int, default=2048)
    parser.add_argument("--compute_unit", type=str, default="ALL",
                        choices=["CPU_ONLY", "CPU_AND_GPU", "CPU_AND_NE", "ALL"])
    parser.add_argument("--minimum_deployment_target", type=str, default="iOS17",
                        help="iOS17 = A17 Pro 起步；iOS18 = 更优化但覆盖面窄")
    args = parser.parse_args()

    if platform.system() != "Darwin":
        print("[warn] 当前非 macOS，CoreML 导出将失败。可在 macOS 上跑该脚本。",
              file=sys.stderr)

    hf_dir = Path(args.model_path)
    if not hf_dir.exists():
        print(f"[error] 模型路径不存在：{hf_dir}", file=sys.stderr)
        sys.exit(1)

    out_path = Path(args.output)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    try:
        from executorch.examples.models.llama import export_llama as el
    except ImportError as e:
        print("[error] 未安装 executorch：", e, file=sys.stderr)
        print("  pip install --pre executorch coremltools", file=sys.stderr)
        sys.exit(1)

    # 通过 CoreML partitioner 导出
    cli_args = [
        "--model", "qwen3",
        "--checkpoint", str(hf_dir),
        "--output_name", str(out_path),
        "--max_seq_length", str(args.seq_len),
        "--dtype-override", "fp16",
        "--coreml",  # CoreML 后端
        "--coreml-compute-units", args.compute_unit.lower().replace("_", ""),
        "--coreml-quantize", f"b{args.quant_bits}",
        "--coreml-ios", args.minimum_deployment_target[-2:],  # "17" / "18"
        "-kv",
        "--use_sdpa_with_kv_cache",
        "--group_size", str(args.group_size),
    ]

    print("[exec] 调用 export_llama 参数：", " ".join(cli_args))
    rc = el.main(cli_args) if hasattr(el, "main") else el.export_llama(cli_args)
    if rc not in (None, 0):
        print(f"[error] export_llama 返回码 {rc}", file=sys.stderr)
        sys.exit(int(rc) if isinstance(rc, int) else 1)

    # tokenizer 随包导出
    for f in ("tokenizer.json", "tokenizer_config.json", "special_tokens_map.json"):
        s = hf_dir / f
        if s.exists():
            shutil.copy2(s, out_path.parent / f)

    size_mb = out_path.stat().st_size / (1024 * 1024)
    print(f"[exec] ✅ 导出完成：{out_path}  （{size_mb:.1f} MB）")
    print(f"[exec] iOS 加载示例（Swift + ExecuTorch.framework）见 deploy/executorch/README_ios.md")


if __name__ == "__main__":
    main()
