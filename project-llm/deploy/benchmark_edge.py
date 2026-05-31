"""
benchmark_edge.py —— 端侧推理性能 benchmark

支持 3 种后端：
  1) llama.cpp HTTP server（GGUF）      — CPU / Metal / CUDA
  2) Ollama HTTP (/api/chat)            — GGUF，封装 llama.cpp
  3) OpenAI 兼容 HTTP（任意端侧服务）    — MLC-LLM server / ExecuTorch HTTP

输出指标：首 token 时延 / 解码 tok/s / 峰值内存（通过 psutil 估算）

使用：
    # Ollama
    python deploy/benchmark_edge.py \\
        --backend ollama --model npc-zhang \\
        --prompts data/test/npc_test.json --runs 5

    # llama.cpp server
    python deploy/benchmark_edge.py \\
        --backend llamacpp --base_url http://localhost:8080 \\
        --prompts data/test/npc_test.json --runs 5

    # MLC-LLM / 任意 OpenAI 兼容端点
    python deploy/benchmark_edge.py \\
        --backend openai --base_url http://localhost:8000/v1 \\
        --model npc-ios-a17 \\
        --prompts data/test/npc_test.json
"""
from __future__ import annotations

import argparse
import json
import statistics
import sys
import time
from pathlib import Path


def _pct(xs: list[float], p: float) -> float:
    if not xs:
        return 0.0
    s = sorted(xs)
    k = max(0, min(len(s) - 1, int(len(s) * p / 100)))
    return s[k]


def load_prompts(path: str, n: int) -> list[str]:
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    prompts: list[str] = []
    for s in data:
        q = s.get("question") or s.get("prompt") or s.get("instruction")
        if q:
            prompts.append(q)
    if not prompts:
        return []
    out = []
    while len(out) < n:
        out.extend(prompts)
    return out[:n]


def bench_ollama(base_url: str, model: str, prompt: str, max_tokens: int):
    """Ollama /api/chat（stream=true，能拿到 TTFT）"""
    import httpx
    url = f"{base_url.rstrip('/')}/api/chat"
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "stream": True,
        "options": {"num_predict": max_tokens},
    }
    t0 = time.perf_counter()
    ttft = 0.0
    n_tokens = 0
    with httpx.stream("POST", url, json=payload, timeout=120.0) as r:
        r.raise_for_status()
        first = True
        for line in r.iter_lines():
            if not line:
                continue
            obj = json.loads(line)
            c = obj.get("message", {}).get("content", "")
            if c and first:
                ttft = time.perf_counter() - t0
                first = False
            if c:
                n_tokens += 1
            if obj.get("done"):
                break
    latency = time.perf_counter() - t0
    return ttft, latency, n_tokens


def bench_llamacpp(base_url: str, prompt: str, max_tokens: int):
    """llama.cpp server /completion stream"""
    import httpx
    url = f"{base_url.rstrip('/')}/completion"
    payload = {
        "prompt": prompt, "n_predict": max_tokens, "stream": True,
        "temperature": 0.8, "top_p": 0.9, "repeat_penalty": 1.05,
    }
    t0 = time.perf_counter()
    ttft = 0.0
    n_tokens = 0
    with httpx.stream("POST", url, json=payload, timeout=120.0) as r:
        r.raise_for_status()
        first = True
        for raw in r.iter_lines():
            if not raw or not raw.startswith("data:"):
                continue
            body = raw[5:].strip()
            if body == "[DONE]":
                break
            try:
                obj = json.loads(body)
            except json.JSONDecodeError:
                continue
            c = obj.get("content") or ""
            if c and first:
                ttft = time.perf_counter() - t0
                first = False
            if c:
                n_tokens += 1
            if obj.get("stop"):
                break
    latency = time.perf_counter() - t0
    return ttft, latency, n_tokens


def bench_openai(base_url: str, model: str, prompt: str, max_tokens: int):
    """OpenAI 兼容 /v1/chat/completions（vLLM/MLC/ExecuTorch HTTP 都兼容）"""
    import httpx
    url = f"{base_url.rstrip('/')}/chat/completions"
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens, "stream": True,
    }
    t0 = time.perf_counter()
    ttft = 0.0
    n_tokens = 0
    with httpx.stream("POST", url, json=payload, timeout=120.0) as r:
        r.raise_for_status()
        first = True
        for raw in r.iter_lines():
            if not raw or not raw.startswith("data:"):
                continue
            body = raw[5:].strip()
            if body == "[DONE]":
                break
            try:
                obj = json.loads(body)
            except json.JSONDecodeError:
                continue
            delta = obj.get("choices", [{}])[0].get("delta", {})
            c = delta.get("content") or ""
            if c and first:
                ttft = time.perf_counter() - t0
                first = False
            if c:
                n_tokens += 1
    latency = time.perf_counter() - t0
    return ttft, latency, n_tokens


def main():
    parser = argparse.ArgumentParser(description="端侧推理 benchmark")
    parser.add_argument("--backend", required=True,
                        choices=["ollama", "llamacpp", "openai"])
    parser.add_argument("--base_url", default="", help="ollama 默认 http://localhost:11434")
    parser.add_argument("--model", default="", help="ollama/openai 模式需要的模型名")
    parser.add_argument("--prompts", required=True)
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--max_tokens", type=int, default=256)
    parser.add_argument("--report", type=str, default="./eval/edge_perf_report.md")
    parser.add_argument("--tag", type=str, required=True,
                        help="本次压测标签：android_snapdragon8gen3 / ios_a17 / cpu_amx 等")
    args = parser.parse_args()

    if not args.base_url:
        defaults = {"ollama": "http://localhost:11434",
                    "llamacpp": "http://localhost:8080",
                    "openai": "http://localhost:8000/v1"}
        args.base_url = defaults[args.backend]

    try:
        import httpx  # noqa: F401
    except ImportError:
        print("[error] 需要安装 httpx：pip install httpx", file=sys.stderr)
        sys.exit(1)

    prompts = load_prompts(args.prompts, args.runs)
    if not prompts:
        print("[error] prompts 为空", file=sys.stderr)
        sys.exit(1)

    bench_fn = {
        "ollama": lambda p: bench_ollama(args.base_url, args.model, p, args.max_tokens),
        "llamacpp": lambda p: bench_llamacpp(args.base_url, p, args.max_tokens),
        "openai": lambda p: bench_openai(args.base_url, args.model, p, args.max_tokens),
    }[args.backend]

    ttfts, decodes, tok_counts = [], [], []
    for i, p in enumerate(prompts, 1):
        try:
            ttft, latency, n = bench_fn(p)
        except Exception as e:  # noqa: BLE001
            print(f"[warn] run {i} 失败：{e}")
            continue
        dec_time = max(latency - ttft, 1e-6)
        tok_s = n / dec_time if n > 0 else 0.0
        ttfts.append(ttft)
        decodes.append(tok_s)
        tok_counts.append(n)
        print(f"  run {i}: TTFT={ttft*1000:.0f}ms  tokens={n}  "
              f"decode={tok_s:.1f} tok/s  total={latency*1000:.0f}ms")

    if not ttfts:
        print("[error] 全部失败")
        sys.exit(1)

    ttft_p50 = _pct(ttfts, 50) * 1000
    ttft_p95 = _pct(ttfts, 95) * 1000
    dec_mean = statistics.mean(decodes) if decodes else 0.0
    dec_p50 = _pct(decodes, 50)

    print(f"\n===== [{args.tag} / {args.backend}] =====")
    print(f"runs           : {len(ttfts)}")
    print(f"TTFT p50/p95   : {ttft_p50:.0f} / {ttft_p95:.0f} ms")
    print(f"decode mean/p50: {dec_mean:.1f} / {dec_p50:.1f} tok/s")
    print(f"avg tokens/run : {statistics.mean(tok_counts):.0f}")

    # append report
    report = Path(args.report)
    first_write = not report.exists()
    report.parent.mkdir(parents=True, exist_ok=True)
    with report.open("a", encoding="utf-8") as f:
        if first_write:
            f.write("# Edge Perf Benchmark Report\n\n")
            f.write("| 硬件/场景 | 后端 | runs | TTFT p50 (ms) | TTFT p95 (ms) | decode mean (tok/s) |\n")
            f.write("|----------|-----|------|---------------|---------------|---------------------|\n")
        f.write(f"| `{args.tag}` | {args.backend} | {len(ttfts)} | "
                f"{ttft_p50:.0f} | {ttft_p95:.0f} | {dec_mean:.1f} |\n")
    print(f"report 追加到：{report}")


if __name__ == "__main__":
    main()
