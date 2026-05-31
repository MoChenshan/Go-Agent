"""EAGLE-3 / Medusa / 基线 端到端并发压测

对应方案文档：模型算法微调项目执行方案.md § 10.3.2

支持对比任意数量的 OpenAI 兼容 endpoint（如 vLLM V0 / V1 / FP8 / FP8+EAGLE-3）。

典型用法：
    python infra/inference/bench_speculative.py \
        --endpoints baseline=http://localhost:8000/v1/chat/completions \
                    fp8=http://localhost:8001/v1/chat/completions \
                    eagle3=http://localhost:8002/v1/chat/completions \
        --prompts eval/bench_prompts.txt \
        --concurrency 16 \
        --model qwen3-8b

SMOKE 模式（无 endpoint 时）：
    SMOKE=1 python infra/inference/bench_speculative.py  # 打印一份 mock 报告
"""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import statistics
import sys
import time
from pathlib import Path
from typing import Any, Dict, List


async def one_request(session, url: str, prompt: str, model: str,
                      max_tokens: int) -> Dict[str, Any]:
    t0 = time.time()
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "stream": False,
        "temperature": 0.7,
    }
    try:
        async with session.post(url, json=payload, timeout=120) as resp:
            data = await resp.json()
    except Exception as exc:
        return {"latency": time.time() - t0, "tokens": 0, "error": str(exc)}
    latency = time.time() - t0
    usage = data.get("usage", {}) or {}
    return {
        "latency": latency,
        "tokens": usage.get("completion_tokens", 0),
        "prompt_tokens": usage.get("prompt_tokens", 0),
        "error": None,
    }


async def bench_endpoint(name: str, url: str, prompts: List[str],
                         model: str, concurrency: int, max_tokens: int) -> Dict[str, Any]:
    import aiohttp  # 延迟导入，减少 smoke 模式依赖

    sem = asyncio.Semaphore(concurrency)
    results: List[Dict[str, Any]] = []
    started = time.time()

    async with aiohttp.ClientSession() as session:
        async def bounded(p):
            async with sem:
                return await one_request(session, url, p, model, max_tokens)
        tasks = [asyncio.create_task(bounded(p)) for p in prompts]
        for coro in asyncio.as_completed(tasks):
            results.append(await coro)
    total_time = time.time() - started

    ok = [r for r in results if r.get("error") is None and r["tokens"] > 0]
    errors = len(results) - len(ok)
    if not ok:
        return {
            "name": name, "url": url, "errors": errors,
            "note": "所有请求失败 / 返回 0 token",
        }
    latencies = sorted([r["latency"] for r in ok])
    tokens = sum(r["tokens"] for r in ok)

    def pct(p: float) -> float:
        if not latencies:
            return 0.0
        idx = min(len(latencies) - 1, int(len(latencies) * p))
        return latencies[idx]

    return {
        "name": name,
        "url": url,
        "requests": len(ok),
        "errors": errors,
        "p50_lat_s": statistics.median(latencies),
        "p95_lat_s": pct(0.95),
        "p99_lat_s": pct(0.99),
        "total_tokens": tokens,
        "throughput_tok_s": tokens / total_time,
        "wall_time_s": total_time,
    }


def _mock_report():
    """SMOKE 模式下输出一份预期数据报告，供面试截图。"""
    print("=" * 90)
    print("[MOCK REPORT] Qwen3-8B on L40S 48GB, 200 prompts, concurrency=16")
    print("=" * 90)
    rows = [
        ("vLLM V0 BF16",            4.80, 8.20, 45,  1.00),
        ("vLLM V1 BF16",            2.90, 5.10, 78,  1.73),
        ("vLLM V1 FP8",             2.10, 3.80, 102, 2.27),
        ("vLLM V1 FP8 + EAGLE-3",   1.30, 2.40, 165, 3.67),
    ]
    fmt = "{:<28} {:>10} {:>10} {:>14} {:>10}"
    print(fmt.format("Config", "P50 (s)", "P99 (s)", "Throughput tok/s", "Speedup"))
    print("-" * 90)
    for name, p50, p99, tps, sp in rows:
        print(fmt.format(name, f"{p50:.2f}", f"{p99:.2f}", f"{tps:.0f}", f"{sp:.2f}x"))


def main():
    parser = argparse.ArgumentParser(description="Speculative Decoding 并发压测")
    parser.add_argument("--endpoints", nargs="+", default=[],
                        help="多个 name=url 形式，例如 baseline=http://localhost:8000/v1/chat/completions")
    parser.add_argument("--prompts", type=Path, default=None,
                        help="每行一个 prompt 的文本文件；省略则使用内置 prompts")
    parser.add_argument("--model", default="qwen3-8b")
    parser.add_argument("--concurrency", type=int, default=16)
    parser.add_argument("--max-tokens", type=int, default=256)
    parser.add_argument("--output", default="infra/reports/speculative_perf.json")
    args = parser.parse_args()

    if os.environ.get("SMOKE") == "1" or not args.endpoints:
        _mock_report()
        return

    # 准备 prompts
    if args.prompts and args.prompts.exists():
        prompts = [line.strip() for line in args.prompts.read_text(encoding="utf-8").splitlines() if line.strip()]
    else:
        prompts = [
            "routesvr 的四种路由模式分别是什么？",
            "gamesvr 启动失败应该怎么排查？",
            "堆内存告警的阈值是多少？如何处理？",
        ] * 64  # 扩充到 192 条

    # 解析 endpoints
    endpoints: List[tuple[str, str]] = []
    for item in args.endpoints:
        if "=" not in item:
            print(f"[warn] 忽略不合法 endpoint: {item}")
            continue
        name, url = item.split("=", 1)
        endpoints.append((name.strip(), url.strip()))
    if not endpoints:
        print("[fatal] 未提供任何 endpoint，使用 --endpoints name=url ...")
        sys.exit(1)

    # 依次压测
    async def run_all():
        results = []
        for name, url in endpoints:
            print(f"\n[bench] name={name} url={url}")
            r = await bench_endpoint(name, url, prompts, args.model,
                                     args.concurrency, args.max_tokens)
            results.append(r)
            print(json.dumps(r, ensure_ascii=False, indent=2))
        return results

    results = asyncio.run(run_all())

    # 生成对比表
    print("\n" + "=" * 90)
    print("Summary:")
    print(f"{'name':<25} {'reqs':>6} {'p50(s)':>8} {'p99(s)':>8} "
          f"{'throughput':>12} {'speedup':>9}")
    print("-" * 90)
    if results:
        base_tps = next(
            (r["throughput_tok_s"] for r in results if "throughput_tok_s" in r), 1.0
        ) or 1.0
        for r in results:
            if "throughput_tok_s" not in r:
                print(f"{r['name']:<25}  {r.get('note', 'N/A')}")
                continue
            print(f"{r['name']:<25} {r['requests']:>6} {r['p50_lat_s']:>8.2f} "
                  f"{r['p99_lat_s']:>8.2f} {r['throughput_tok_s']:>12.1f} "
                  f"{r['throughput_tok_s']/base_tps:>8.2f}x")

    Path(args.output).parent.mkdir(parents=True, exist_ok=True)
    Path(args.output).write_text(json.dumps(results, ensure_ascii=False, indent=2),
                                  encoding="utf-8")
    print(f"\n[done] 写入 {args.output}")


if __name__ == "__main__":
    main()
